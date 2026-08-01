package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"palpanel/internal/db"
)

const saveMigrationConfirmation = "MIGRATE PLAYERS"

type UIDMapping struct {
	SourceUID string `json:"source_uid"`
	TargetUID string `json:"target_uid"`
}

type SaveMigrationRequest struct {
	SourcePath   string       `json:"-"`
	Mappings     []UIDMapping `json:"mappings"`
	Confirmation string       `json:"confirmation"`
}

type WorldMigrationHooks struct {
	Prepare    func(context.Context) error
	Invalidate func()
}

func (m Manager) MigrateWorld(ctx context.Context, request SaveMigrationRequest, hooks WorldMigrationHooks) (db.Job, error) {
	request.SourcePath = filepath.Clean(strings.TrimSpace(request.SourcePath))
	if request.Confirmation != saveMigrationConfirmation {
		return db.Job{}, fmt.Errorf("confirmation must be exactly %q", saveMigrationConfirmation)
	}
	if !levelSaveReady(filepath.Join(request.SourcePath, "Level.sav")) {
		return db.Job{}, errors.New("migration source requires a non-empty Level.sav")
	}
	if len(request.Mappings) == 0 {
		return db.Job{}, errors.New("migration requires at least one UID mapping")
	}
	seenSources := map[string]bool{}
	seenTargets := map[string]bool{}
	for _, mapping := range request.Mappings {
		if !canonicalMigrationUID(mapping.SourceUID) || !canonicalMigrationUID(mapping.TargetUID) {
			return db.Job{}, errors.New("migration UIDs must be canonical lowercase GUIDs")
		}
		if mapping.SourceUID == mapping.TargetUID {
			return db.Job{}, fmt.Errorf("source and target UID are identical: %s", mapping.SourceUID)
		}
		if seenSources[mapping.SourceUID] || seenTargets[mapping.TargetUID] {
			return db.Job{}, errors.New("migration contains duplicate source or target UIDs")
		}
		seenSources[mapping.SourceUID] = true
		seenTargets[mapping.TargetUID] = true
	}
	return m.startLifecycleJob(ctx, "save_migration", "queued player save migration", func(jobCtx context.Context, jobID string) {
		m.runSaveMigration(jobCtx, jobID, request, hooks)
	})
}

func (m Manager) runSaveMigration(ctx context.Context, jobID string, request SaveMigrationRequest, hooks WorldMigrationHooks) {
	worldID, err := m.activeWorldID()
	if err != nil || worldID == "" {
		m.update(jobID, "failed", 2, "target world read failed", errorDetail(err, "active server world was not found"))
		return
	}
	targetPath := m.worldPath(worldID)
	if !levelSaveReady(filepath.Join(targetPath, "Level.sav")) {
		m.update(jobID, "failed", 2, "target world is unavailable", "non-empty target Level.sav is required")
		return
	}
	status, err := m.migrationServerStatus(ctx)
	if err != nil {
		m.update(jobID, "failed", 3, "server status read failed", err.Error())
		return
	}
	wasRunning := status.Container.Status == "running"
	if wasRunning {
		m.update(jobID, "running", 5, "saving world before player migration", "")
		if hooks.Prepare != nil {
			if err := hooks.Prepare(ctx); err != nil {
				m.update(jobID, "failed", 5, "world save failed", err.Error())
				return
			}
		}
		m.update(jobID, "running", 10, "stopping server before player migration", "")
		if err := m.migrationServerStop(ctx); err != nil {
			m.update(jobID, "failed", 10, "server stop failed", err.Error())
			return
		}
	}

	m.update(jobID, "running", 20, "creating verified pre-migration backup", "")
	backup, err := m.createBackupArchive("pre-save-migration")
	if err != nil {
		m.failMigrationBeforeSwap(ctx, jobID, wasRunning, 20, "backup failed", err.Error())
		return
	}
	verifiedBackup, err := verifyBackupArchive(backup.Path, backup.Name)
	if err != nil || !verifiedBackup.Valid {
		detail := errorDetail(err, strings.Join(verifiedBackup.Errors, "; "))
		m.failMigrationBeforeSwap(ctx, jobID, wasRunning, 22, "backup verification failed", detail+"; backup retained at "+backup.Path)
		return
	}

	stagingRoot := filepath.Join(m.cfg.ServerDirectory(), "Pal", "Saved", ".palpanel-save-migration", jobID)
	outputPath := filepath.Join(stagingRoot, "converted-world")
	rollbackPath := filepath.Join(stagingRoot, "original-world")
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		m.failMigrationBeforeSwap(ctx, jobID, wasRunning, 28, "migration staging setup failed", err.Error()+"; backup retained at "+backup.Path)
		return
	}
	defer func() { _ = os.RemoveAll(stagingRoot) }()

	m.update(jobID, "running", 40, "converting player UID references", "")
	remap := m.migrationRemap
	if remap == nil {
		remap = m.runUIDRemapper
	}
	if err := remap(ctx, request.SourcePath, outputPath, request.Mappings); err != nil {
		m.failMigrationBeforeSwap(ctx, jobID, wasRunning, 40, "UID conversion failed", err.Error()+"; backup retained at "+backup.Path)
		return
	}
	if !levelSaveReady(filepath.Join(outputPath, "Level.sav")) {
		m.failMigrationBeforeSwap(ctx, jobID, wasRunning, 48, "converted world validation failed", "converted Level.sav is missing; backup retained at "+backup.Path)
		return
	}

	m.update(jobID, "running", 60, "atomically switching converted world", "")
	if err := os.Rename(targetPath, rollbackPath); err != nil {
		m.failMigrationBeforeSwap(ctx, jobID, wasRunning, 60, "target world staging failed", err.Error()+"; backup retained at "+backup.Path)
		return
	}
	if err := os.Rename(outputPath, targetPath); err != nil {
		_ = os.Rename(rollbackPath, targetPath)
		m.failMigrationBeforeSwap(ctx, jobID, wasRunning, 62, "converted world activation failed", err.Error()+"; original world restored; backup retained at "+backup.Path)
		return
	}
	m.invalidateSaveIndexCache()
	if hooks.Invalidate != nil {
		hooks.Invalidate()
	}

	verify := m.migrationVerify
	if verify == nil {
		verify = verifyMigratedWorld
	}
	m.update(jobID, "running", 72, "verifying deployed player migration", "")
	if err := verify(targetPath, request.Mappings); err != nil {
		detail := m.rollbackMigratedWorld(ctx, targetPath, rollbackPath, wasRunning)
		m.update(jobID, "failed", 72, "deployed migration verification failed", err.Error()+"; rolled back to original world"+detail+"; backup retained at "+backup.Path)
		return
	}

	if wasRunning {
		m.update(jobID, "running", 86, "starting server with migrated world", "")
		if err := m.migrationServerStart(ctx); err != nil {
			if stopErr := m.migrationServerStop(ctx); stopErr != nil {
				m.update(jobID, "failed", 86, "migrated server start failed", err.Error()+"; automatic rollback was not attempted because the partially started server could not be stopped: "+stopErr.Error()+"; backup retained at "+backup.Path)
				return
			}
			detail := m.rollbackMigratedWorld(ctx, targetPath, rollbackPath, true)
			m.update(jobID, "failed", 86, "migrated server start failed", err.Error()+"; rolled back to original world"+detail+"; backup retained at "+backup.Path)
			return
		}
	}
	if err := os.RemoveAll(rollbackPath); err != nil {
		m.update(jobID, "failed", 94, "migration cleanup failed", err.Error()+"; migrated world is active; backup retained at "+backup.Path)
		return
	}
	m.invalidateSaveIndexCache()
	if hooks.Invalidate != nil {
		hooks.Invalidate()
	}
	m.update(jobID, "completed", 100, "player save migration completed; first-login verification required; backup retained at "+backup.Path, "")
}

func (m Manager) failMigrationBeforeSwap(ctx context.Context, jobID string, wasRunning bool, progress int, message, detail string) {
	if wasRunning {
		if err := m.migrationServerStart(ctx); err != nil {
			detail += "; original world was not changed, but restarting the server failed: " + err.Error()
		}
	}
	m.update(jobID, "failed", progress, message, detail)
}

func (m Manager) rollbackMigratedWorld(ctx context.Context, targetPath, rollbackPath string, restart bool) string {
	detail := ""
	if err := os.RemoveAll(targetPath); err != nil {
		return "; automatic rollback could not remove the converted world: " + err.Error()
	}
	if err := os.Rename(rollbackPath, targetPath); err != nil {
		return "; automatic rollback could not restore the original world: " + err.Error()
	}
	m.invalidateSaveIndexCache()
	if restart {
		if err := m.migrationServerStart(ctx); err != nil {
			detail = "; original world was restored but server restart failed: " + err.Error()
		}
	}
	return detail
}

func (m Manager) migrationServerStatus(ctx context.Context) (Status, error) {
	if m.migrationStatus != nil {
		return m.migrationStatus(ctx)
	}
	return m.Status(ctx)
}

func (m Manager) migrationServerStop(ctx context.Context) error {
	if m.migrationStop != nil {
		return m.migrationStop(ctx)
	}
	return m.stopUnlocked(ctx)
}

func (m Manager) migrationServerStart(ctx context.Context) error {
	if m.migrationStart != nil {
		return m.migrationStart(ctx)
	}
	return m.startUnlocked(ctx)
}

func (m Manager) runUIDRemapper(ctx context.Context, input, output string, mappings []UIDMapping) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return err
	}
	mappingFile, err := os.CreateTemp(filepath.Dir(output), ".uid-mapping-*.json")
	if err != nil {
		return err
	}
	mappingPath := mappingFile.Name()
	defer os.Remove(mappingPath)
	if err := mappingFile.Chmod(0o600); err != nil {
		_ = mappingFile.Close()
		return err
	}
	if err := json.NewEncoder(mappingFile).Encode(mappings); err != nil {
		_ = mappingFile.Close()
		return err
	}
	if err := mappingFile.Close(); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, m.uidRemapperPath(), "--input", input, "--output", output, "--mapping", mappingPath)
	payload, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("palworld-uid-remap failed: %w: %s", err, strings.TrimSpace(string(payload)))
	}
	return nil
}

func (m Manager) uidRemapperPath() string {
	if configured := strings.TrimSpace(os.Getenv("PALPANEL_UID_REMAPPER_PATH")); configured != "" {
		return configured
	}
	name := "palworld-uid-remap"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), name)
		if fileExists(candidate) {
			return candidate
		}
	}
	if m.cfg.RuntimeRoot != "" {
		candidate := filepath.Join(m.cfg.RuntimeRoot, "bin", name)
		if fileExists(candidate) {
			return candidate
		}
	}
	if m.cfg.RepositoryRoot != "" {
		profile := "release"
		candidate := filepath.Join(m.cfg.RepositoryRoot, "tools", "palworld-uid-remap", "target", profile, name)
		if fileExists(candidate) {
			return candidate
		}
	}
	return name
}

func verifyMigratedWorld(worldPath string, mappings []UIDMapping) error {
	if !levelSaveReady(filepath.Join(worldPath, "Level.sav")) {
		return errors.New("migrated Level.sav is missing or empty")
	}
	for _, mapping := range mappings {
		name := strings.ToUpper(strings.ReplaceAll(mapping.TargetUID, "-", "")) + ".sav"
		if !levelSaveReady(filepath.Join(worldPath, "Players", name)) {
			return fmt.Errorf("migrated player file is missing: %s", name)
		}
	}
	return nil
}

func canonicalMigrationUID(value string) bool {
	if len(value) != 36 || strings.ToLower(value) != value {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func errorDetail(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return "unknown error"
}
