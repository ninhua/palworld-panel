package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"palpanel/internal/db"
	"palpanel/internal/palconfig"
	"palpanel/internal/steamcmd"
)

const configApplyJournalKey = "palworld_config_apply_journal"

type ConfigSnapshot struct {
	Content  []byte
	Document palconfig.Document
	Revision string
}

type configApplyJournal struct {
	DraftID              string `json:"draft_id"`
	RecoveryPath         string `json:"recovery_path"`
	PreviousPending      string `json:"previous_pending"`
	PreviousPendingFound bool   `json:"previous_pending_found"`
	WasRunning           bool   `json:"was_running"`
	Phase                string `json:"phase"`
}

func ReadPalworldConfigSnapshot(path string) (ConfigSnapshot, error) {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		content = nil
		err = nil
	}
	if err != nil {
		return ConfigSnapshot{}, err
	}
	document, err := palconfig.ParseDocument(string(content))
	if err != nil {
		return ConfigSnapshot{}, err
	}
	sum := sha256.Sum256(content)
	return ConfigSnapshot{Content: content, Document: document, Revision: hex.EncodeToString(sum[:])}, nil
}

func PalworldConfigRevision(path string) (string, error) {
	snapshot, err := ReadPalworldConfigSnapshot(path)
	return snapshot.Revision, err
}

type ConfigReadinessVerifier func(context.Context, palconfig.Settings, []string) error

func (m Manager) ApplyPalworldConfig(ctx context.Context, draft db.ConfigDraft, notify RestartNotifier, verifiers ...ConfigReadinessVerifier) (db.Job, error) {
	if err := m.requireCrashGuardReady(ctx); err != nil {
		return db.Job{}, err
	}
	if draft.ID == "" || draft.DraftPath == "" {
		return db.Job{}, fmt.Errorf("config draft is incomplete")
	}
	if draft.Status != "draft" && draft.Status != "failed" {
		return db.Job{}, fmt.Errorf("config draft cannot be applied from status %s", draft.Status)
	}
	if err := m.cfg.ValidateManagedPath(draft.DraftPath, false); err != nil {
		return db.Job{}, fmt.Errorf("validate config draft path: %w", err)
	}
	draftSnapshot, err := ReadPalworldConfigSnapshot(draft.DraftPath)
	if err != nil {
		return db.Job{}, fmt.Errorf("read config draft: %w", err)
	}
	if hasPalconfigValidationErrors(palconfig.Validate(draftSnapshot.Document.Settings)) {
		return db.Job{}, fmt.Errorf("config draft has validation errors")
	}
	claimed, err := m.store.ClaimConfigDraft(ctx, draft.ID, "claiming")
	if err != nil {
		return db.Job{}, err
	}
	if !claimed {
		return db.Job{}, fmt.Errorf("config draft is already claimed")
	}
	var verifier ConfigReadinessVerifier
	if len(verifiers) > 0 {
		verifier = verifiers[0]
	}

	job, err := m.startLifecycleJob(ctx, "palworld_config_apply", "queued Palworld config apply", func(jobCtx context.Context, jobID string) {
		_ = m.store.UpdateConfigDraftStatus(jobCtx, draft.ID, "applying", jobID)
		m.runConfigApply(jobCtx, jobID, draft, draftSnapshot, notify, verifier)
	})
	if err != nil {
		_ = m.store.UpdateConfigDraftStatus(context.Background(), draft.ID, "failed", "")
		return db.Job{}, err
	}
	return job, nil
}

func (m Manager) runConfigApply(ctx context.Context, jobID string, draft db.ConfigDraft, draftSnapshot ConfigSnapshot, notify RestartNotifier, verifier ConfigReadinessVerifier) {
	failBeforeStop := func(progress int, code, message string, cause error) {
		_ = m.store.UpdateConfigDraftStatus(context.Background(), draft.ID, "failed", jobID)
		_ = m.store.UpdateJobWithCode(context.Background(), jobID, "failed", progress, message, cause.Error(), code)
	}

	m.update(jobID, "running", 5, "capturing active config transaction state", "")
	initial, err := ReadPalworldConfigSnapshot(m.cfg.PalWorldSettingsPath())
	if err != nil {
		failBeforeStop(5, "config_snapshot_failed", "config snapshot failed", err)
		return
	}
	if initial.Revision != draft.BaseSHA256 {
		failBeforeStop(5, "config_draft_stale", "config draft is stale", fmt.Errorf("active config changed before apply"))
		return
	}
	if _, err := m.createPalworldConfigRevision(ctx, initial, "baseline", "", nil); err != nil {
		failBeforeStop(5, "config_revision_capture_failed", "config revision capture failed", err)
		return
	}
	pending, pendingFound, err := m.store.GetKV(ctx, "pending_restart")
	if err != nil {
		failBeforeStop(5, "config_state_read_failed", "config state read failed", err)
		return
	}
	status, err := m.statusForConfigApply(ctx)
	if err != nil {
		failBeforeStop(10, "config_status_failed", "server status check failed", err)
		return
	}
	journal := configApplyJournal{
		DraftID: draft.ID, RecoveryPath: draft.DraftPath + ".rollback",
		PreviousPending: pending, PreviousPendingFound: pendingFound, WasRunning: serverStatusRunning(status), Phase: "captured",
	}
	if err := atomicWritePrivate(journal.RecoveryPath, initial.Content); err != nil {
		failBeforeStop(10, "config_recovery_write_failed", "config recovery snapshot failed", err)
		return
	}
	if err := m.persistConfigApplyJournal(ctx, journal); err != nil {
		_ = os.Remove(journal.RecoveryPath)
		failBeforeStop(10, "config_journal_write_failed", "config journal persistence failed", err)
		return
	}

	newInstanceStarted := false
	recordedRevisionID := ""
	rollback := func(progress int, code, message string, cause error) {
		rollbackErr := m.rollbackConfigApply(context.Background(), journal, initial, newInstanceStarted)
		if rollbackErr == nil && recordedRevisionID != "" {
			if err := m.store.DeleteConfigRevisionAndQueueCleanup(context.Background(), recordedRevisionID); err != nil {
				rollbackErr = fmt.Errorf("remove rolled-back config revision: %w", err)
			} else {
				_ = m.CleanupConfigPrivateFiles(context.Background())
			}
		}
		if rollbackErr == nil {
			rollbackErr = m.clearConfigApplyJournal(context.Background(), journal)
		}
		_ = m.store.UpdateConfigDraftStatus(context.Background(), draft.ID, "failed", jobID)
		detail := cause.Error()
		if rollbackErr != nil {
			detail += "; rollback failed: " + rollbackErr.Error()
			code = "config_rollback_failed"
		}
		_ = m.store.UpdateJobWithCode(context.Background(), jobID, "failed", progress, message, detail, code)
	}

	if journal.WasRunning {
		m.update(jobID, "running", 15, "saving world and stopping server", "")
		if err := m.gracefulStopConfigApply(ctx, notify); err != nil {
			rollback(20, "config_stop_failed", "stop before config apply failed", err)
			return
		}
		journal.Phase = "stopped"
		if err := m.persistConfigApplyJournal(ctx, journal); err != nil {
			rollback(20, "config_journal_write_failed", "stopped phase journal persistence failed", err)
			return
		}
		if m.configApplyAfterStop != nil {
			m.configApplyAfterStop()
		}
	}

	postStop, err := ReadPalworldConfigSnapshot(m.cfg.PalWorldSettingsPath())
	if err != nil {
		rollback(25, "config_post_stop_snapshot_failed", "post-stop config check failed", err)
		return
	}
	if postStop.Revision != initial.Revision {
		rollback(25, "config_draft_stale_after_stop", "PalServer changed config during shutdown", fmt.Errorf("active config changed during shutdown"))
		return
	}
	if _, err := m.backupPalworldConfig(initial.Content); err != nil {
		rollback(35, "config_backup_failed", "config backup failed", err)
		return
	}

	journal.Phase = "writing"
	if err := m.persistConfigApplyJournal(ctx, journal); err != nil {
		rollback(40, "config_journal_write_failed", "writing phase journal persistence failed", err)
		return
	}
	if err := atomicWritePrivate(m.cfg.PalWorldSettingsPath(), draftSnapshot.Content); err != nil {
		rollback(50, "config_write_failed", "config write failed", err)
		return
	}
	if err := m.store.SetKV(ctx, "pending_restart", "true"); err != nil {
		rollback(55, "config_state_write_failed", "config state persistence failed", err)
		return
	}
	applied, err := ReadPalworldConfigSnapshot(m.cfg.PalWorldSettingsPath())
	if err != nil || !reflect.DeepEqual(applied.Document.Settings, draftSnapshot.Document.Settings) {
		if err == nil {
			err = fmt.Errorf("applied config differs after disk reparse")
		}
		rollback(65, "config_verify_failed", "applied config verification failed", err)
		return
	}

	if journal.WasRunning {
		journal.Phase = "starting"
		if err := m.persistConfigApplyJournal(ctx, journal); err != nil {
			rollback(70, "config_journal_write_failed", "starting phase journal persistence failed", err)
			return
		}
		if err := m.startForConfigApply(ctx); err != nil {
			rollback(75, "config_start_failed", "config apply start failed", err)
			return
		}
		newInstanceStarted = true
		if err := m.waitForConfigApplyHealth(ctx, draftSnapshot.Document.Settings, draft.ModifiedFields, verifier); err != nil {
			rollback(85, "config_health_failed", "config apply health check failed", err)
			return
		}
	}

	committedSnapshot, err := ReadPalworldConfigSnapshot(m.cfg.PalWorldSettingsPath())
	if err != nil || committedSnapshot.Revision != applied.Revision {
		if err == nil {
			err = fmt.Errorf("active config changed before revision commit")
		}
		rollback(88, "config_revision_verify_failed", "config revision verification failed", err)
		return
	}
	recordedRevision, err := m.RecordAppliedPalworldConfigRevision(ctx, committedSnapshot, initial.Revision, draft.ModifiedFields)
	if err != nil {
		rollback(90, "config_revision_commit_failed", "config revision persistence failed", err)
		return
	}
	if recordedRevision.RevisionSHA256 != initial.Revision {
		recordedRevisionID = recordedRevision.ID
	}

	journal.Phase = "committed"
	if err := m.persistConfigApplyJournal(ctx, journal); err != nil {
		rollback(90, "config_journal_write_failed", "committed phase journal persistence failed", err)
		return
	}
	if err := m.store.UpdateConfigDraftStatusAndQueueCleanup(context.Background(), draft.ID, "completed", jobID, "config_draft"); err != nil {
		m.update(jobID, "completed", 100, "Palworld config applied; committed cleanup will be retried on startup", "")
		return
	}
	cleanupErr := m.clearConfigApplyJournal(context.Background(), journal)
	if cleanupErr == nil {
		_ = removePrivateFile(draft.DraftPath)
	}
	message := "Palworld config applied"
	if !journal.WasRunning {
		message += "; server remains stopped"
	}
	if cleanupErr != nil {
		message += "; committed cleanup will be retried on startup"
	}
	m.update(jobID, "completed", 100, message, "")
}

func (m Manager) rollbackConfigApply(ctx context.Context, journal configApplyJournal, initial ConfigSnapshot, newInstanceStarted bool) error {
	status, err := m.statusForConfigApply(ctx)
	if err != nil {
		return fmt.Errorf("check rollback server status: %w", err)
	}
	if newInstanceStarted || serverStatusRunning(status) {
		if err := m.stopAndConfirmConfigApply(ctx); err != nil {
			return fmt.Errorf("stop applied server before rollback: %w", err)
		}
	}
	if err := atomicWritePrivate(m.cfg.PalWorldSettingsPath(), initial.Content); err != nil {
		return fmt.Errorf("restore active config: %w", err)
	}
	restored, err := ReadPalworldConfigSnapshot(m.cfg.PalWorldSettingsPath())
	if err != nil || restored.Revision != initial.Revision {
		if err == nil {
			err = fmt.Errorf("restored config revision differs")
		}
		return fmt.Errorf("verify restored active config: %w", err)
	}
	if journal.PreviousPendingFound {
		err = m.store.SetKV(ctx, "pending_restart", journal.PreviousPending)
	} else {
		err = m.store.DeleteKV(ctx, "pending_restart")
	}
	if err != nil {
		return fmt.Errorf("restore pending restart state: %w", err)
	}
	if journal.WasRunning {
		if err := m.startForConfigApply(ctx); err != nil {
			return fmt.Errorf("restore pre-apply running state: %w", err)
		}
	}
	return nil
}

func (m Manager) stopAndConfirmConfigApply(ctx context.Context) error {
	status, err := m.statusForConfigApply(ctx)
	if err != nil {
		return err
	}
	if serverStatusRunning(status) {
		if err := m.stopForConfigApply(ctx); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(m.gracefulStopTimeout)
	for {
		status, err = m.statusForConfigApply(ctx)
		if err == nil && !serverStatusRunning(status) {
			return nil
		}
		if !time.Now().Before(deadline) {
			if err != nil {
				return err
			}
			return fmt.Errorf("server remained running after stop")
		}
		wait := m.lifecycleWait
		if wait == nil {
			wait = waitForLifecycleDuration
		}
		if err := wait(ctx, m.gracefulStopPoll); err != nil {
			return err
		}
	}
}

func (m Manager) gracefulStopConfigApply(ctx context.Context, notify RestartNotifier) error {
	_ = m.markCrashGuardExpectedStop(ctx, "config_apply")
	if notify != nil {
		_ = notify(ctx, 5, "Applying Palworld configuration")
	}
	deadline := time.Now().Add(m.gracefulStopTimeout)
	for time.Now().Before(deadline) {
		status, err := m.Status(ctx)
		if err == nil && !serverStatusRunning(status) {
			return nil
		}
		wait := m.lifecycleWait
		if wait == nil {
			wait = waitForLifecycleDuration
		}
		if err := wait(ctx, m.gracefulStopPoll); err != nil {
			return err
		}
	}
	return m.stopForConfigApply(ctx)
}

func (m Manager) waitForConfigApplyHealth(ctx context.Context, settings palconfig.Settings, modifiedFields []string, verifier ConfigReadinessVerifier) error {
	if m.configApplyHealth != nil {
		return m.configApplyHealth(ctx)
	}
	deadline := time.Now().Add(30 * time.Second)
	stable := 0
	for time.Now().Before(deadline) {
		status, err := m.statusForConfigApply(ctx)
		if err == nil && serverStatusRunning(status) && verifier != nil {
			err = verifier(ctx, settings, modifiedFields)
		}
		if err == nil && serverStatusRunning(status) {
			stable++
			if stable >= 3 {
				return nil
			}
		} else {
			stable = 0
		}
		if err := waitForLifecycleDuration(ctx, 250*time.Millisecond); err != nil {
			return err
		}
	}
	return fmt.Errorf("server did not remain running for three consecutive checks")
}

func (m Manager) statusForConfigApply(ctx context.Context) (Status, error) {
	if m.configApplyStatus != nil {
		return m.configApplyStatus(ctx)
	}
	return m.Status(ctx)
}

func (m Manager) stopForConfigApply(ctx context.Context) error {
	if m.configApplyStop != nil {
		return m.configApplyStop(ctx)
	}
	return m.stopUnlocked(ctx)
}

func (m Manager) startForConfigApply(ctx context.Context) error {
	if m.configApplyStart != nil {
		return m.configApplyStart(ctx)
	}
	return m.startUnlocked(ctx)
}

func (m Manager) persistConfigApplyJournal(ctx context.Context, journal configApplyJournal) error {
	if m.configApplyJournalPersist != nil {
		return m.configApplyJournalPersist(ctx, journal)
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	return m.store.SetKV(ctx, configApplyJournalKey, string(raw))
}

func (m Manager) clearConfigApplyJournal(ctx context.Context, journal configApplyJournal) error {
	if err := m.store.QueueConfigPrivateCleanup(ctx, journal.RecoveryPath, "config_recovery"); err != nil {
		return err
	}
	if err := m.store.DeleteKV(ctx, configApplyJournalKey); err != nil {
		return err
	}
	return m.CleanupConfigPrivateFiles(ctx)
}

func (m Manager) RecoverPalworldConfigApply(ctx context.Context) error {
	raw, found, err := m.store.GetKV(ctx, configApplyJournalKey)
	if err != nil || !found {
		return err
	}
	var journal configApplyJournal
	if err := json.Unmarshal([]byte(raw), &journal); err != nil {
		return fmt.Errorf("decode config apply journal: %w", err)
	}
	if journal.Phase == "committed" {
		_ = m.store.UpdateConfigDraftStatusAndQueueCleanup(ctx, journal.DraftID, "completed", "", "config_draft")
		return m.clearConfigApplyJournal(ctx, journal)
	}
	status, err := m.statusForConfigApply(ctx)
	if err != nil {
		return fmt.Errorf("check interrupted config apply status: %w", err)
	}
	if journal.Phase == "captured" && serverStatusRunning(status) {
		_ = m.store.UpdateConfigDraftStatus(ctx, journal.DraftID, "failed", "")
		return m.clearConfigApplyJournal(ctx, journal)
	}
	if journal.Phase != "captured" && journal.Phase != "stopped" && journal.Phase != "writing" && journal.Phase != "starting" {
		return fmt.Errorf("unsupported config apply journal phase %q", journal.Phase)
	}
	if serverStatusRunning(status) {
		if err := m.stopAndConfirmConfigApply(ctx); err != nil {
			return fmt.Errorf("stop interrupted config apply: %w", err)
		}
	}
	if journal.Phase == "captured" && !journal.WasRunning {
		_ = m.store.UpdateConfigDraftStatus(ctx, journal.DraftID, "failed", "")
		return m.clearConfigApplyJournal(ctx, journal)
	}
	content, err := os.ReadFile(journal.RecoveryPath)
	if err != nil {
		return fmt.Errorf("read config recovery snapshot: %w", err)
	}
	if err := atomicWritePrivate(m.cfg.PalWorldSettingsPath(), content); err != nil {
		return fmt.Errorf("restore config recovery snapshot: %w", err)
	}
	if journal.PreviousPendingFound {
		err = m.store.SetKV(ctx, "pending_restart", journal.PreviousPending)
	} else {
		err = m.store.DeleteKV(ctx, "pending_restart")
	}
	if err != nil {
		return err
	}
	if journal.WasRunning {
		if err := m.startForConfigApply(ctx); err != nil {
			return fmt.Errorf("restore pre-apply running state: %w", err)
		}
	}
	_ = m.store.UpdateConfigDraftStatus(ctx, journal.DraftID, "failed", "")
	return m.clearConfigApplyJournal(ctx, journal)
}

func removePrivateFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m Manager) CleanupConfigPrivateFiles(ctx context.Context) error {
	_, err := m.DrainPrivateCleanup(ctx)
	return err
}

func (m Manager) DrainPrivateCleanup(ctx context.Context) (int, error) {
	items, err := m.store.ListConfigPrivateCleanup(ctx, 100)
	if err != nil {
		return 0, err
	}
	failed := 0
	for _, item := range items {
		remove := m.configPrivateRemove
		if remove == nil {
			remove = removePrivateFile
		}
		removeErr := m.cfg.ValidateManagedPath(item.Path, false)
		if removeErr == nil {
			removeErr = remove(item.Path)
		}
		if removeErr != nil {
			failed++
			if err := m.store.RecordConfigPrivateCleanupFailure(ctx, item.Path, removeErr.Error()); err != nil {
				return failed, err
			}
			continue
		}
		if err := m.store.CompleteConfigPrivateCleanup(ctx, item.Path); err != nil {
			return failed, err
		}
	}
	return failed, nil
}

func (m Manager) MaintainConfigDrafts(ctx context.Context) error {
	ttl := m.configDraftTTL
	if ttl < 0 {
		ttl = 24 * time.Hour
	}
	cutoff := time.Now().UTC().Add(-ttl).Format(time.RFC3339Nano)
	if _, err := m.store.ExpireConfigDrafts(ctx, cutoff); err != nil {
		return err
	}
	return m.CleanupConfigPrivateFiles(ctx)
}

func (m Manager) StartConfigDraftCleanup(ctx context.Context, interval time.Duration) <-chan struct{} {
	done := make(chan struct{})
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = m.MaintainConfigDrafts(ctx)
			}
		}
	}()
	return done
}

func (m Manager) backupPalworldConfig(content []byte) (string, error) {
	if len(content) == 0 {
		return "", nil
	}
	if err := os.MkdirAll(m.cfg.BackupsDir, 0o750); err != nil {
		return "", err
	}
	path := filepath.Join(m.cfg.BackupsDir, "palworld-config-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".ini")
	if err := atomicWritePrivate(path, content); err != nil {
		return "", err
	}
	return path, nil
}

func atomicWritePrivate(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := steamcmd.SecurePrivatePath(context.Background(), filepath.Dir(path)); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".palpanel-config-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return steamcmd.SecurePrivatePath(context.Background(), path)
}

func HardenPalworldConfigPrivatePath(ctx context.Context, path string) error {
	return steamcmd.SecurePrivatePath(ctx, path)
}

func hasPalconfigValidationErrors(issues []palconfig.ValidationIssue) bool {
	for _, issue := range issues {
		if issue.Severity == "error" {
			return true
		}
	}
	return false
}
