package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"palpanel/internal/db"
	"palpanel/internal/docker"
	"palpanel/internal/palconfig"
)

func TestReadPalworldConfigSnapshotHashesParsedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PalWorldSettings.ini")
	before := []byte("[/Script/Pal.PalGameWorldSettings]\nOptionSettings=(ServerName=\"Before\")\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ReadPalworldConfigSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[/Script/Pal.PalGameWorldSettings]\nOptionSettings=(ServerName=\"After\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(before)
	if snapshot.Revision != hex.EncodeToString(want[:]) || snapshot.Document.Settings["ServerName"] != "Before" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestApplyPalworldConfigRechecksAfterStopAndRollsBackState(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	manager.gracefulStopTimeout = 0
	running := true
	manager.configApplyStatus = func(context.Context) (Status, error) {
		status := "exited"
		if running {
			status = "running"
		}
		return Status{Container: docker.ContainerStatus{Exists: true, Status: status}}, nil
	}
	manager.configApplyStop = func(context.Context) error { running = false; return nil }
	manager.configApplyStart = func(context.Context) error { running = true; return nil }
	before, _ := os.ReadFile(manager.cfg.PalWorldSettingsPath())
	if err := manager.store.SetKV(t.Context(), "pending_restart", "false"); err != nil {
		t.Fatal(err)
	}
	draft := createConfigApplyDraft(t, manager, "Changed")
	manager.configApplyAfterStop = func() {
		_ = os.WriteFile(manager.cfg.PalWorldSettingsPath(), []byte(palconfig.SectionHeader+"\nOptionSettings=(ServerName=\"shutdown rewrite\")\n"), 0o600)
	}
	job, err := manager.ApplyPalworldConfig(t.Context(), draft, func(context.Context, int, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForJob(t, manager.store, job.ID)
	if completed.Status != "failed" || completed.ErrorCode != "config_draft_stale_after_stop" {
		t.Fatalf("job = %#v", completed)
	}
	after, _ := os.ReadFile(manager.cfg.PalWorldSettingsPath())
	if string(after) != string(before) {
		t.Fatalf("active config was not rolled back")
	}
	if pending, _, _ := manager.store.GetKV(t.Context(), "pending_restart"); pending != "false" {
		t.Fatalf("pending_restart = %q", pending)
	}
}

func TestApplyPalworldConfigHealthFailureRollsBackFileAndKV(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	manager.gracefulStopTimeout = 0
	running := true
	manager.configApplyStatus = func(context.Context) (Status, error) {
		status := "exited"
		if running {
			status = "running"
		}
		return Status{Container: docker.ContainerStatus{Exists: true, Status: status}}, nil
	}
	stopCalls := 0
	manager.configApplyStop = func(context.Context) error { stopCalls++; running = false; return nil }
	var startedWith []string
	manager.configApplyStart = func(context.Context) error {
		content, err := os.ReadFile(manager.cfg.PalWorldSettingsPath())
		if err != nil {
			return err
		}
		startedWith = append(startedWith, string(content))
		running = true
		return nil
	}
	before, _ := os.ReadFile(manager.cfg.PalWorldSettingsPath())
	if err := manager.store.SetKV(t.Context(), "pending_restart", "false"); err != nil {
		t.Fatal(err)
	}
	draft := createConfigApplyDraft(t, manager, "Changed")
	manager.configApplyHealth = func(context.Context) error { return errors.New("not stable") }
	job, err := manager.ApplyPalworldConfig(t.Context(), draft, func(context.Context, int, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForJob(t, manager.store, job.ID)
	if completed.Status != "failed" {
		t.Fatalf("job = %#v", completed)
	}
	after, _ := os.ReadFile(manager.cfg.PalWorldSettingsPath())
	if string(after) != string(before) {
		t.Fatalf("active config was not rolled back")
	}
	if pending, _, _ := manager.store.GetKV(t.Context(), "pending_restart"); pending != "false" {
		t.Fatalf("pending_restart = %q", pending)
	}
	if stopCalls != 2 {
		t.Fatalf("stop calls = %d, want initial stop and rollback stop", stopCalls)
	}
	if len(startedWith) != 2 || !strings.Contains(startedWith[0], `ServerName="Changed"`) || startedWith[1] != string(before) {
		t.Fatalf("start snapshots = %#v", startedWith)
	}
	revisions, err := manager.store.ListConfigRevisions(t.Context(), 10)
	if err != nil || len(revisions) != 1 || revisions[0].Source != "baseline" {
		t.Fatalf("failed apply revisions = %#v, %v", revisions, err)
	}
}

func TestRecoverPalworldConfigApplyCapturedWhileRunningDoesNotRewriteActiveConfig(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	active, _ := os.ReadFile(manager.cfg.PalWorldSettingsPath())
	recovery := filepath.Join(manager.cfg.DataDir, "config-drafts", "captured.rollback")
	if err := atomicWritePrivate(recovery, []byte("must not be written")); err != nil {
		t.Fatal(err)
	}
	journal := configApplyJournal{DraftID: "cfg_captured", RecoveryPath: recovery, WasRunning: true, Phase: "captured"}
	persistJournalFixture(t, manager, journal)
	manager.configApplyStatus = func(context.Context) (Status, error) {
		return Status{Container: docker.ContainerStatus{Exists: true, Status: "running"}}, nil
	}
	if err := manager.RecoverPalworldConfigApply(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(manager.cfg.PalWorldSettingsPath())
	if string(after) != string(active) {
		t.Fatal("captured recovery rewrote active config while server was running")
	}
	if _, found, _ := manager.store.GetKV(t.Context(), configApplyJournalKey); found {
		t.Fatal("captured journal was not cleared")
	}
}

func TestApplyPalworldConfigPhaseJournalFailureStopsBeforeWriting(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	manager.gracefulStopTimeout = 0
	running := true
	manager.configApplyStatus = func(context.Context) (Status, error) {
		status := "exited"
		if running {
			status = "running"
		}
		return Status{Container: docker.ContainerStatus{Exists: true, Status: status}}, nil
	}
	manager.configApplyStop = func(context.Context) error { running = false; return nil }
	manager.configApplyStart = func(context.Context) error { running = true; return nil }
	manager.configApplyJournalPersist = func(ctx context.Context, journal configApplyJournal) error {
		if journal.Phase == "stopped" {
			return errors.New("journal unavailable")
		}
		raw, _ := json.Marshal(journal)
		return manager.store.SetKV(ctx, configApplyJournalKey, string(raw))
	}
	before, _ := os.ReadFile(manager.cfg.PalWorldSettingsPath())
	draft := createConfigApplyDraft(t, manager, "Changed")
	job, err := manager.ApplyPalworldConfig(t.Context(), draft, nil)
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForJob(t, manager.store, job.ID)
	if completed.Status != "failed" || completed.ErrorCode != "config_journal_write_failed" {
		t.Fatalf("job = %#v", completed)
	}
	after, _ := os.ReadFile(manager.cfg.PalWorldSettingsPath())
	if string(after) != string(before) {
		t.Fatal("config changed after stopped phase journal failure")
	}
}

func TestApplyPalworldConfigRollbackFailureRetainsJournalAndRecovery(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	manager.gracefulStopTimeout = 0
	running := true
	manager.configApplyStatus = func(context.Context) (Status, error) {
		status := "exited"
		if running {
			status = "running"
		}
		return Status{Container: docker.ContainerStatus{Exists: true, Status: status}}, nil
	}
	stops := 0
	manager.configApplyStop = func(context.Context) error {
		stops++
		if stops == 2 {
			return errors.New("cannot stop new instance")
		}
		running = false
		return nil
	}
	manager.configApplyStart = func(context.Context) error { running = true; return nil }
	manager.configApplyHealth = func(context.Context) error { return errors.New("not ready") }
	draft := createConfigApplyDraft(t, manager, "Changed")
	job, err := manager.ApplyPalworldConfig(t.Context(), draft, nil)
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForJob(t, manager.store, job.ID)
	if completed.ErrorCode != "config_rollback_failed" {
		t.Fatalf("job = %#v", completed)
	}
	raw, found, err := manager.store.GetKV(t.Context(), configApplyJournalKey)
	if err != nil || !found {
		t.Fatalf("journal missing after rollback failure: found=%v err=%v", found, err)
	}
	var journal configApplyJournal
	if err := json.Unmarshal([]byte(raw), &journal); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(journal.RecoveryPath); err != nil {
		t.Fatalf("recovery snapshot missing after rollback failure: %v", err)
	}
}

func TestRecoverCommittedConfigApplyDoesNotRequireRecoverySnapshot(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	journal := configApplyJournal{DraftID: "cfg_committed", RecoveryPath: filepath.Join(manager.cfg.DataDir, "missing.rollback"), Phase: "committed"}
	persistJournalFixture(t, manager, journal)
	if err := manager.RecoverPalworldConfigApply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := manager.store.GetKV(t.Context(), configApplyJournalKey); found {
		t.Fatal("committed journal was not cleared")
	}
}

func TestApplyPalworldConfigReadinessVerifierUsesDraftAdminPassword(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	manager.gracefulStopTimeout = 0
	running := true
	manager.configApplyStatus = func(context.Context) (Status, error) {
		status := "exited"
		if running {
			status = "running"
		}
		return Status{Container: docker.ContainerStatus{Exists: true, Status: status}}, nil
	}
	manager.configApplyStop = func(context.Context) error { running = false; return nil }
	manager.configApplyStart = func(context.Context) error { running = true; return nil }
	draft := createConfigApplyDraft(t, manager, "Changed")
	document, err := palconfig.ReadDocument(draft.DraftPath)
	if err != nil {
		t.Fatal(err)
	}
	document.Settings["AdminPassword"] = "new-admin-password"
	if err := os.WriteFile(draft.DraftPath, []byte(palconfig.SerializeDocument(document, map[string]bool{"AdminPassword": true})), 0o600); err != nil {
		t.Fatal(err)
	}
	checks := 0
	job, err := manager.ApplyPalworldConfig(t.Context(), draft, nil, func(_ context.Context, settings palconfig.Settings, _ []string) error {
		checks++
		if settings["AdminPassword"] != "new-admin-password" {
			return errors.New("verifier received stale AdminPassword")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForJob(t, manager.store, job.ID)
	if completed.Status != "completed" || checks != 3 {
		t.Fatalf("job = %#v, readiness checks = %d", completed, checks)
	}
	revisions, err := manager.store.ListConfigRevisions(t.Context(), 10)
	if err != nil || len(revisions) != 2 || revisions[0].Source != "apply" || revisions[1].Source != "baseline" {
		t.Fatalf("successful apply revisions = %#v, %v", revisions, err)
	}
	if revisions[0].ParentSHA256 != revisions[1].RevisionSHA256 || !containsString(revisions[0].ChangedFields, "ServerName") {
		t.Fatalf("revision lineage = %#v", revisions)
	}
}

func TestApplyPalworldConfigCommittedJournalFailureRemovesRolledBackRevision(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	manager.configApplyStatus = func(context.Context) (Status, error) {
		return Status{Container: docker.ContainerStatus{Exists: true, Status: "exited"}}, nil
	}
	manager.configApplyJournalPersist = func(ctx context.Context, journal configApplyJournal) error {
		if journal.Phase == "committed" {
			return errors.New("committed journal unavailable")
		}
		raw, _ := json.Marshal(journal)
		return manager.store.SetKV(ctx, configApplyJournalKey, string(raw))
	}
	before, err := os.ReadFile(manager.cfg.PalWorldSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	draft := createConfigApplyDraft(t, manager, "Changed")
	job, err := manager.ApplyPalworldConfig(t.Context(), draft, nil)
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForJob(t, manager.store, job.ID)
	if completed.Status != "failed" || completed.ErrorCode != "config_journal_write_failed" {
		t.Fatalf("job = %#v", completed)
	}
	after, err := os.ReadFile(manager.cfg.PalWorldSettingsPath())
	if err != nil || string(after) != string(before) {
		t.Fatalf("active config after rollback = %q, %v", after, err)
	}
	revisions, err := manager.store.ListConfigRevisions(t.Context(), 10)
	if err != nil || len(revisions) != 1 || revisions[0].Source != "baseline" {
		t.Fatalf("rolled-back revisions = %#v, %v", revisions, err)
	}
	files, err := filepath.Glob(filepath.Join(manager.cfg.DataDir, "config-revisions", "*.ini"))
	if err != nil || len(files) != 1 {
		t.Fatalf("revision files after rollback = %#v, %v", files, err)
	}
}

func TestEnsurePalworldConfigRevisionDeduplicatesConcurrentCapture(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	const workers = 12
	ctx := t.Context()
	var wait sync.WaitGroup
	errors := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := manager.EnsurePalworldConfigRevision(ctx)
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	revisions, err := manager.store.ListConfigRevisions(t.Context(), 20)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("concurrent revisions = %#v, %v", revisions, err)
	}
	files, err := filepath.Glob(filepath.Join(manager.cfg.DataDir, "config-revisions", "*.ini"))
	if err != nil || len(files) != 1 {
		t.Fatalf("concurrent revision files = %#v, %v", files, err)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestConfigRevisionSecretFieldsAreCaseInsensitive(t *testing.T) {
	for _, field := range []string{"AdminPassword", "adminpassword", "SERVERPASSWORD", "ServerPassword"} {
		if !configSecretField(field) {
			t.Fatalf("secret field %q was not recognized", field)
		}
	}
	if configSecretField("ServerName") {
		t.Fatal("non-secret field was classified as secret")
	}
}

func TestReadPalworldConfigRevisionRejectsTamperedPrivateSnapshot(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	revision, err := manager.EnsurePalworldConfigRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(revision.SnapshotPath, []byte(palconfig.SectionHeader+"\nOptionSettings=(ServerName=\"Tampered\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReadPalworldConfigRevision(t.Context(), revision); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("tampered snapshot error = %v", err)
	}
}

func TestEnsurePalworldConfigRevisionReplacesMissingPrivateSnapshot(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	first, err := manager.EnsurePalworldConfigRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first.SnapshotPath); err != nil {
		t.Fatal(err)
	}
	second, err := manager.EnsurePalworldConfigRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.RevisionSHA256 != first.RevisionSHA256 {
		t.Fatalf("replacement revision = %#v; first=%#v", second, first)
	}
	revisions, err := manager.store.ListConfigRevisions(t.Context(), 10)
	if err != nil || len(revisions) != 1 || revisions[0].ID != second.ID {
		t.Fatalf("revisions after replacement = %#v, %v", revisions, err)
	}
	if _, err := os.Stat(second.SnapshotPath); err != nil {
		t.Fatalf("replacement snapshot missing: %v", err)
	}
}

func TestEnsurePalworldConfigRevisionDoesNotCaptureUncommittedApply(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	baseline, err := manager.EnsurePalworldConfigRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	journal := configApplyJournal{DraftID: "cfg_inflight", RecoveryPath: filepath.Join(manager.cfg.DataDir, "config-drafts", "inflight.rollback"), Phase: "starting"}
	persistJournalFixture(t, manager, journal)
	if err := atomicWritePrivate(manager.cfg.PalWorldSettingsPath(), []byte(palconfig.SectionHeader+"\nOptionSettings=(ServerName=\"Uncommitted\")\n")); err != nil {
		t.Fatal(err)
	}
	captured, err := manager.EnsurePalworldConfigRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if captured.ID != baseline.ID {
		t.Fatalf("in-flight config was captured: baseline=%#v captured=%#v", baseline, captured)
	}
	revisions, err := manager.store.ListConfigRevisions(t.Context(), 10)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("in-flight revisions = %#v, %v", revisions, err)
	}
}

func TestRecordAppliedPalworldConfigRevisionPromotesMatchingBaselineMetadata(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	initial, err := manager.EnsurePalworldConfigRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWritePrivate(manager.cfg.PalWorldSettingsPath(), []byte(palconfig.SectionHeader+"\nOptionSettings=(ServerName=\"Promoted\")\n")); err != nil {
		t.Fatal(err)
	}
	target, err := manager.EnsurePalworldConfigRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := manager.RecordAppliedPalworldConfigRevision(t.Context(), targetSnapshot(t, manager.cfg.PalWorldSettingsPath()), initial.RevisionSHA256, []string{"ServerName"})
	if err != nil {
		t.Fatal(err)
	}
	if promoted.ID != target.ID || promoted.Source != "apply" || promoted.ParentSHA256 != initial.RevisionSHA256 || !containsString(promoted.ChangedFields, "ServerName") {
		t.Fatalf("promoted revision = %#v; target=%#v", promoted, target)
	}
}

func targetSnapshot(t *testing.T, path string) ConfigSnapshot {
	t.Helper()
	snapshot, err := ReadPalworldConfigSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestReadPalworldConfigRevisionRejectsRedirectedSnapshotPath(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	revision, err := manager.EnsurePalworldConfigRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(manager.cfg.DataDir, "config-revisions-other", revision.ID+".ini")
	if err := atomicWritePrivate(other, revisionSnapshotContent(t, revision.SnapshotPath)); err != nil {
		t.Fatal(err)
	}
	revision.SnapshotPath = other
	if _, err := manager.ReadPalworldConfigRevision(t.Context(), revision); err == nil || !strings.Contains(err.Error(), "path mismatch") {
		t.Fatalf("redirected snapshot error = %v", err)
	}
}

func revisionSnapshotContent(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestConfigPrivateCleanupRetriesDeletionFailure(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	path := filepath.Join(manager.cfg.DataDir, "config-drafts", "retry.ini")
	if err := atomicWritePrivate(path, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	if err := manager.store.QueueConfigPrivateCleanup(t.Context(), path, "config_draft"); err != nil {
		t.Fatal(err)
	}
	manager.configPrivateRemove = func(string) error { return errors.New("access denied") }
	failed, err := manager.DrainPrivateCleanup(t.Context())
	if err != nil || failed != 1 {
		t.Fatalf("drain = %d, %v", failed, err)
	}
	pending, err := manager.store.ListConfigPrivateCleanup(t.Context(), 10)
	if err != nil || len(pending) != 1 || pending[0].Attempts != 1 {
		t.Fatalf("pending after failure = %#v, %v", pending, err)
	}
	manager.configPrivateRemove = nil
	if err := manager.CleanupConfigPrivateFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("private file remains: %v", err)
	}
	pending, err = manager.store.ListConfigPrivateCleanup(t.Context(), 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after retry = %#v, %v", pending, err)
	}
}

func TestConfigDraftMaintenanceExpiresAtStartupAndOnTicker(t *testing.T) {
	manager, cleanup := newOperationsManager(t)
	defer cleanup()
	manager.configDraftTTL = time.Millisecond
	create := func(id string) db.ConfigDraft {
		path := filepath.Join(manager.cfg.DataDir, "config-drafts", id+".ini")
		if err := atomicWritePrivate(path, []byte("private")); err != nil {
			t.Fatal(err)
		}
		draft := db.ConfigDraft{ID: id, BaseSHA256: "hash", DraftPath: path, Status: "draft"}
		if err := manager.store.CreateConfigDraft(t.Context(), draft); err != nil {
			t.Fatal(err)
		}
		return draft
	}
	startup := create("startup")
	time.Sleep(10 * time.Millisecond)
	if err := manager.MaintainConfigDrafts(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, err := manager.store.GetConfigDraft(t.Context(), startup.ID); err != nil || got.Status != "expired" {
		t.Fatalf("startup draft = %#v, %v", got, err)
	}
	if _, err := os.Stat(startup.DraftPath); !os.IsNotExist(err) {
		t.Fatalf("startup draft file remains: %v", err)
	}

	periodic := create("periodic")
	ctx, cancel := context.WithCancel(t.Context())
	done := manager.StartConfigDraftCleanup(ctx, 5*time.Millisecond)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, err := manager.store.GetConfigDraft(t.Context(), periodic.ID)
		_, statErr := os.Stat(periodic.DraftPath)
		if err == nil && got.Status == "expired" && os.IsNotExist(statErr) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	got, err := manager.store.GetConfigDraft(t.Context(), periodic.ID)
	if err != nil || got.Status != "expired" {
		t.Fatalf("periodic draft = %#v, %v", got, err)
	}
	if _, err := os.Stat(periodic.DraftPath); !os.IsNotExist(err) {
		t.Fatalf("periodic draft file remains: %v", err)
	}
}

func persistJournalFixture(t *testing.T, manager Manager, journal configApplyJournal) {
	t.Helper()
	raw, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.store.SetKV(t.Context(), configApplyJournalKey, string(raw)); err != nil {
		t.Fatal(err)
	}
}

func createConfigApplyDraft(t *testing.T, manager Manager, serverName string) db.ConfigDraft {
	t.Helper()
	snapshot, err := ReadPalworldConfigSnapshot(manager.cfg.PalWorldSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Document.Settings["ServerName"] = serverName
	dir := filepath.Join(manager.cfg.DataDir, "config-drafts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	draft := db.ConfigDraft{ID: "cfg_test", BaseSHA256: snapshot.Revision, DraftPath: filepath.Join(dir, "cfg_test.ini"), Status: "draft", ModifiedFields: []string{"ServerName"}}
	if err := os.WriteFile(draft.DraftPath, []byte(palconfig.SerializeDocument(snapshot.Document, map[string]bool{"ServerName": true})), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.store.CreateConfigDraft(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	draft, err = manager.store.GetConfigDraft(t.Context(), draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	return draft
}
