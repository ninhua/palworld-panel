package server

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"palpanel/internal/db"
	"palpanel/internal/id"
	"palpanel/internal/palconfig"
)

const configRevisionRetention = 50

var configRevisionWriteMu sync.Mutex

type ConfigRevisionFieldDiff struct {
	Field              string `json:"field"`
	Secret             bool   `json:"secret"`
	RevisionValue      string `json:"revision_value,omitempty"`
	CurrentValue       string `json:"current_value,omitempty"`
	RevisionConfigured bool   `json:"revision_configured"`
	CurrentConfigured  bool   `json:"current_configured"`
}

type ConfigRevisionDiff struct {
	RevisionID     string                    `json:"revision_id"`
	RevisionSHA256 string                    `json:"revision_sha256"`
	CurrentSHA256  string                    `json:"current_sha256"`
	Changes        []ConfigRevisionFieldDiff `json:"changes"`
}

func configSecretField(key string) bool {
	return strings.EqualFold(key, "AdminPassword") || strings.EqualFold(key, "ServerPassword")
}

func changedConfigFields(left, right palconfig.Settings) []string {
	keys := map[string]bool{}
	for key := range left {
		keys[key] = true
	}
	for key := range right {
		keys[key] = true
	}
	changed := make([]string, 0)
	for key := range keys {
		if left[key] != right[key] {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return changed
}

func (m Manager) createPalworldConfigRevision(ctx context.Context, snapshot ConfigSnapshot, source, parent string, changedFields []string) (db.ConfigRevision, error) {
	configRevisionWriteMu.Lock()
	defer configRevisionWriteMu.Unlock()
	return m.createPalworldConfigRevisionLocked(ctx, snapshot, source, parent, changedFields)
}

func (m Manager) createPalworldConfigRevisionLocked(ctx context.Context, snapshot ConfigSnapshot, source, parent string, changedFields []string) (db.ConfigRevision, error) {
	if snapshot.Revision == "" {
		return db.ConfigRevision{}, fmt.Errorf("config revision is empty")
	}
	latest, latestErr := m.store.LatestConfigRevision(ctx)
	if latestErr == nil && latest.RevisionSHA256 == snapshot.Revision {
		stored, readErr := m.ReadPalworldConfigRevision(ctx, latest)
		if readErr == nil && stored.Revision == snapshot.Revision {
			if source == "apply" {
				latest.Source = "apply"
				latest.ParentSHA256 = parent
				latest.ChangedFields = append([]string(nil), changedFields...)
				if err := m.store.UpdateConfigRevisionMetadata(ctx, latest); err != nil {
					return db.ConfigRevision{}, err
				}
				return m.store.GetConfigRevision(ctx, latest.ID)
			}
			return latest, nil
		}
		if err := m.store.DeleteConfigRevisionAndQueueCleanup(ctx, latest.ID); err != nil {
			return db.ConfigRevision{}, fmt.Errorf("remove unusable config revision: %w", err)
		}
		latest, latestErr = m.store.LatestConfigRevision(ctx)
	}
	if latestErr != nil && latestErr != sql.ErrNoRows {
		return db.ConfigRevision{}, latestErr
	}
	if parent == "" && latestErr == nil {
		parent = latest.RevisionSHA256
	}
	revisionDir := filepath.Join(m.cfg.DataDir, "config-revisions")
	if err := os.MkdirAll(revisionDir, 0o700); err != nil {
		return db.ConfigRevision{}, err
	}
	if err := os.Chmod(revisionDir, 0o700); err != nil {
		return db.ConfigRevision{}, err
	}
	if err := HardenPalworldConfigPrivatePath(ctx, revisionDir); err != nil {
		return db.ConfigRevision{}, err
	}
	revision := db.ConfigRevision{
		ID: id.New("cfgr"), RevisionSHA256: snapshot.Revision, ParentSHA256: parent,
		Source: strings.TrimSpace(source), ChangedFields: changedFields,
	}
	if revision.Source == "" {
		revision.Source = "apply"
	}
	revision.SnapshotPath = filepath.Join(revisionDir, revision.ID+".ini")
	if err := atomicWritePrivate(revision.SnapshotPath, snapshot.Content); err != nil {
		return db.ConfigRevision{}, err
	}
	if err := HardenPalworldConfigPrivatePath(ctx, revision.SnapshotPath); err != nil {
		_ = os.Remove(revision.SnapshotPath)
		return db.ConfigRevision{}, err
	}
	if err := m.store.CreateConfigRevision(ctx, revision); err != nil {
		_ = os.Remove(revision.SnapshotPath)
		return db.ConfigRevision{}, err
	}
	_, _ = m.store.PruneConfigRevisions(ctx, configRevisionRetention)
	_ = m.CleanupConfigPrivateFiles(ctx)
	return m.store.GetConfigRevision(ctx, revision.ID)
}

func (m Manager) PalworldConfigApplyInProgress(ctx context.Context) (bool, error) {
	_, active, err := m.store.GetKV(ctx, configApplyJournalKey)
	return active, err
}

func (m Manager) EnsurePalworldConfigRevision(ctx context.Context) (db.ConfigRevision, error) {
	configRevisionWriteMu.Lock()
	defer configRevisionWriteMu.Unlock()
	if active, err := m.PalworldConfigApplyInProgress(ctx); err != nil {
		return db.ConfigRevision{}, err
	} else if active {
		return m.store.LatestConfigRevision(ctx)
	}
	snapshot, err := ReadPalworldConfigSnapshot(m.cfg.PalWorldSettingsPath())
	if err != nil {
		return db.ConfigRevision{}, err
	}
	return m.createPalworldConfigRevisionLocked(ctx, snapshot, "baseline", "", nil)
}

func (m Manager) RecordAppliedPalworldConfigRevision(ctx context.Context, snapshot ConfigSnapshot, parent string, changedFields []string) (db.ConfigRevision, error) {
	return m.createPalworldConfigRevision(ctx, snapshot, "apply", parent, changedFields)
}

func configRevisionPathsEqual(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(filepath.Clean(left))
	rightAbs, rightErr := filepath.Abs(filepath.Clean(right))
	if leftErr != nil || rightErr != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(leftAbs, rightAbs)
	}
	return leftAbs == rightAbs
}

func (m Manager) ReadPalworldConfigRevision(ctx context.Context, revision db.ConfigRevision) (ConfigSnapshot, error) {
	expected := filepath.Join(m.cfg.DataDir, "config-revisions", revision.ID+".ini")
	if !configRevisionPathsEqual(revision.SnapshotPath, expected) {
		return ConfigSnapshot{}, fmt.Errorf("config revision snapshot path mismatch")
	}
	if err := m.cfg.ValidateManagedPath(revision.SnapshotPath, false); err != nil {
		return ConfigSnapshot{}, fmt.Errorf("validate config revision path: %w", err)
	}
	snapshot, err := ReadPalworldConfigSnapshot(revision.SnapshotPath)
	if err != nil {
		return ConfigSnapshot{}, err
	}
	if snapshot.Revision != revision.RevisionSHA256 {
		return ConfigSnapshot{}, fmt.Errorf("config revision snapshot sha256 mismatch")
	}
	return snapshot, nil
}

func (m Manager) DiffPalworldConfigRevision(ctx context.Context, revision db.ConfigRevision) (ConfigRevisionDiff, error) {
	target, err := m.ReadPalworldConfigRevision(ctx, revision)
	if err != nil {
		return ConfigRevisionDiff{}, err
	}
	current, err := ReadPalworldConfigSnapshot(m.cfg.PalWorldSettingsPath())
	if err != nil {
		return ConfigRevisionDiff{}, err
	}
	fields := changedConfigFields(target.Document.Settings, current.Document.Settings)
	changes := make([]ConfigRevisionFieldDiff, 0, len(fields))
	for _, field := range fields {
		targetValue := target.Document.Settings[field]
		currentValue := current.Document.Settings[field]
		item := ConfigRevisionFieldDiff{
			Field: field, Secret: configSecretField(field),
			RevisionConfigured: strings.TrimSpace(targetValue) != "",
			CurrentConfigured:  strings.TrimSpace(currentValue) != "",
		}
		if !item.Secret {
			item.RevisionValue = targetValue
			item.CurrentValue = currentValue
		}
		changes = append(changes, item)
	}
	return ConfigRevisionDiff{
		RevisionID: revision.ID, RevisionSHA256: revision.RevisionSHA256,
		CurrentSHA256: current.Revision, Changes: changes,
	}, nil
}
