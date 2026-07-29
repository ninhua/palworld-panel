package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"palpanel/internal/db"
	"palpanel/internal/palconfig"
)

const worldResetConfirmation = "RESET WORLD"

var worldIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type WorldInfo struct {
	ActiveWorldID          string `json:"active_world_id"`
	SaveExists             bool   `json:"save_exists"`
	LastModified           string `json:"last_modified,omitempty"`
	ServerRunning          bool   `json:"server_running"`
	ResetAvailable         bool   `json:"reset_available"`
	ResetUnavailableReason string `json:"reset_unavailable_reason,omitempty"`
}

type WorldResetHooks struct {
	Prepare    func(context.Context) error
	Invalidate func()
}

func (m Manager) WorldInfo(ctx context.Context) (WorldInfo, error) {
	worldID, err := m.activeWorldID()
	if err != nil {
		return WorldInfo{}, err
	}
	status, err := m.Status(ctx)
	if err != nil {
		return WorldInfo{}, err
	}
	info := WorldInfo{
		ActiveWorldID: worldID,
		ServerRunning: status.Container.Status == "running",
	}
	if worldID == "" {
		info.ResetUnavailableReason = "world_not_found"
		return info, nil
	}
	if err := validateWorldID(worldID); err != nil {
		info.ResetUnavailableReason = "invalid_world_id"
		return info, nil
	}
	levelPath := filepath.Join(m.worldPath(worldID), "Level.sav")
	levelInfo, err := os.Stat(levelPath)
	if err == nil && !levelInfo.IsDir() && levelInfo.Size() > 0 {
		info.SaveExists = true
		info.LastModified = levelInfo.ModTime().UTC().Format(time.RFC3339Nano)
		info.ResetAvailable = true
		return info, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return WorldInfo{}, err
	}
	info.ResetUnavailableReason = "save_not_found"
	return info, nil
}

func (m Manager) ResetWorld(ctx context.Context, expectedWorldID, confirmation string, hooks WorldResetHooks) (db.Job, error) {
	if err := m.requireCrashGuardReady(ctx); err != nil {
		return db.Job{}, err
	}
	expectedWorldID = strings.TrimSpace(expectedWorldID)
	if confirmation != worldResetConfirmation {
		return db.Job{}, fmt.Errorf("confirmation must be exactly %q", worldResetConfirmation)
	}
	if err := validateWorldID(expectedWorldID); err != nil {
		return db.Job{}, err
	}
	preview, err := m.WorldInfo(ctx)
	if err != nil {
		return db.Job{}, err
	}
	if preview.ActiveWorldID != expectedWorldID {
		return db.Job{}, fmt.Errorf("active world changed from %q to %q; refresh the preview", expectedWorldID, preview.ActiveWorldID)
	}
	if !preview.ResetAvailable {
		return db.Job{}, fmt.Errorf("world reset is unavailable: %s", preview.ResetUnavailableReason)
	}

	return m.startLifecycleJob(ctx, "world_reset", "queued world reset", func(jobCtx context.Context, jobID string) {
		m.runWorldReset(jobCtx, jobID, expectedWorldID, hooks)
	})
}

func (m Manager) runWorldReset(ctx context.Context, jobID, expectedWorldID string, hooks WorldResetHooks) {
	worldID, err := m.activeWorldID()
	if err != nil {
		m.update(jobID, "failed", 2, "active world read failed", err.Error())
		return
	}
	if worldID != expectedWorldID {
		m.update(jobID, "failed", 2, "active world changed", fmt.Sprintf("expected %q but DedicatedServerName now resolves to %q; refresh the preview", expectedWorldID, worldID))
		return
	}
	if err := validateWorldID(worldID); err != nil {
		m.update(jobID, "failed", 2, "active world id is unsafe", err.Error())
		return
	}
	worldPath := m.worldPath(worldID)
	if !levelSaveReady(filepath.Join(worldPath, "Level.sav")) {
		m.update(jobID, "failed", 2, "world save not found", "non-empty Level.sav is required before reset")
		return
	}

	status, err := m.Status(ctx)
	if err != nil {
		m.update(jobID, "failed", 3, "server status read failed", err.Error())
		return
	}
	wasRunning := status.Container.Status == "running"
	if wasRunning {
		m.update(jobID, "running", 5, "saving world and notifying players", "")
		if hooks.Prepare != nil {
			if err := hooks.Prepare(ctx); err != nil {
				m.update(jobID, "failed", 5, "save or notification failed", err.Error())
				return
			}
		}
		m.update(jobID, "running", 12, "stopping server before world reset", "")
		if err := m.stopUnlocked(ctx); err != nil {
			m.update(jobID, "failed", 12, "stop before world reset failed", err.Error())
			return
		}
	}

	m.update(jobID, "running", 25, "creating verified pre-world-reset backup", "")
	backup, err := m.createBackupArchive("pre-world-reset")
	if err != nil {
		m.failBeforeWorldMove(ctx, jobID, wasRunning, 25, "backup failed", err.Error())
		return
	}
	verified, err := verifyBackupArchive(backup.Path, backup.Name)
	if err != nil || !verified.Valid {
		detail := "backup verification failed"
		if err != nil {
			detail += ": " + err.Error()
		} else if len(verified.Errors) > 0 {
			detail += ": " + strings.Join(verified.Errors, "; ")
		}
		m.failBeforeWorldMove(ctx, jobID, wasRunning, 30, "backup verification failed", detail+"; backup retained at "+backup.Path)
		return
	}

	stagingRoot := filepath.Join(m.cfg.ServerDirectory(), "Pal", "Saved", ".palpanel-world-reset")
	stagedPath := filepath.Join(stagingRoot, jobID)
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		m.failBeforeWorldMove(ctx, jobID, wasRunning, 38, "world staging setup failed", err.Error()+"; verified backup retained at "+backup.Path)
		return
	}
	if _, err := os.Stat(stagedPath); err == nil {
		m.failBeforeWorldMove(ctx, jobID, wasRunning, 38, "world staging path already exists", stagedPath+"; verified backup retained at "+backup.Path)
		return
	} else if !os.IsNotExist(err) {
		m.failBeforeWorldMove(ctx, jobID, wasRunning, 38, "world staging path check failed", err.Error()+"; verified backup retained at "+backup.Path)
		return
	}

	m.update(jobID, "running", 45, "moving old world to reset staging", "")
	if err := os.Rename(worldPath, stagedPath); err != nil {
		m.failBeforeWorldMove(ctx, jobID, wasRunning, 45, "world staging move failed", err.Error()+"; verified backup retained at "+backup.Path)
		return
	}
	m.invalidateSaveIndexCache()
	if hooks.Invalidate != nil {
		hooks.Invalidate()
	}

	if wasRunning {
		m.update(jobID, "running", 60, "starting server to generate a new world", "")
		if err := m.startUnlocked(ctx); err != nil {
			_ = m.stopUnlocked(ctx)
			m.failAfterWorldMove(jobID, 60, "new world start failed", err, backup.Path, stagedPath)
			return
		}
		m.update(jobID, "running", 72, "waiting for new Level.sav", "")
		if err := m.waitForNewWorld(ctx, worldPath); err != nil {
			_ = m.stopUnlocked(ctx)
			m.failAfterWorldMove(jobID, 72, "new world generation failed", err, backup.Path, stagedPath)
			return
		}
	}

	m.update(jobID, "running", 92, "removing staged old world", "")
	if err := os.RemoveAll(stagedPath); err != nil {
		m.failAfterWorldMove(jobID, 92, "old world staging cleanup failed", err, backup.Path, stagedPath)
		return
	}
	m.invalidateSaveIndexCache()
	if hooks.Invalidate != nil {
		hooks.Invalidate()
	}
	m.update(jobID, "completed", 100, "world reset completed; verified backup retained at "+backup.Path, "")
}

func (m Manager) failBeforeWorldMove(ctx context.Context, jobID string, wasRunning bool, progress int, message, detail string) {
	if wasRunning {
		if err := m.startUnlocked(ctx); err != nil {
			detail += "; original world was not moved, but restarting the previous server failed: " + err.Error()
		}
	}
	m.update(jobID, "failed", progress, message, detail)
}

func (m Manager) failAfterWorldMove(jobID string, progress int, message string, err error, backupPath, stagedPath string) {
	detail := err.Error() + "; verified backup retained at " + backupPath + "; old world retained at " + stagedPath +
		"; stop the server, inspect the partial new world, then restore the verified backup or move the staged directory back manually"
	m.update(jobID, "failed", progress, message, detail)
}

func (m Manager) waitForNewWorld(ctx context.Context, worldPath string) error {
	timeout := m.worldResetTimeout
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	poll := m.worldResetPoll
	if poll <= 0 {
		poll = time.Second
	}
	deadline := time.Now().Add(timeout)
	levelPath := filepath.Join(worldPath, "Level.sav")
	for {
		if levelSaveReady(levelPath) {
			return nil
		}
		status, err := m.Status(ctx)
		if err == nil && status.Container.Status != "running" && status.Container.Status != "starting" && status.Container.Status != "restarting" {
			return fmt.Errorf("server exited before creating a non-empty Level.sav")
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for a non-empty Level.sav", timeout.Round(time.Second))
		}
		select {
		case <-time.After(poll):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m Manager) activeWorldID() (string, error) {
	settings, err := palconfig.Read(m.cfg.PalWorldSettingsPath())
	if err != nil {
		return "", err
	}
	if configured := strings.TrimSpace(settings["DedicatedServerName"]); configured != "" {
		if err := validateWorldID(configured); err != nil {
			return "", fmt.Errorf("DedicatedServerName is invalid: %w", err)
		}
		return configured, nil
	}

	root := m.worldsRoot()
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var selected string
	var selectedTime time.Time
	for _, entry := range entries {
		if !entry.IsDir() || validateWorldID(entry.Name()) != nil {
			continue
		}
		info, err := os.Stat(filepath.Join(root, entry.Name(), "Level.sav"))
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		if selected == "" || info.ModTime().After(selectedTime) {
			selected = entry.Name()
			selectedTime = info.ModTime()
		}
	}
	return selected, nil
}

func validateWorldID(worldID string) error {
	worldID = strings.TrimSpace(worldID)
	if !worldIDPattern.MatchString(worldID) || filepath.Base(worldID) != worldID || filepath.Clean(worldID) != worldID {
		return fmt.Errorf("world id must be a direct SaveGames/0 child containing only letters, numbers, underscore, or hyphen")
	}
	return nil
}

func (m Manager) worldsRoot() string {
	return filepath.Join(m.cfg.ServerDirectory(), "Pal", "Saved", "SaveGames", "0")
}

func (m Manager) worldPath(worldID string) string {
	return filepath.Join(m.worldsRoot(), worldID)
}

func (m Manager) invalidateSaveIndexCache() {
	if strings.TrimSpace(m.cfg.SaveIndexCacheDir) == "" {
		return
	}
	_ = os.Remove(filepath.Join(m.cfg.SaveIndexCacheDir, "index-cache.json"))
}

func levelSaveReady(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}
