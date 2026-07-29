package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreJobsModsAndKV(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	job, err := store.CreateJob(ctx, "job_1", "install", "queued")
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if job.Status != "queued" {
		t.Fatalf("job status = %q", job.Status)
	}
	if err := store.UpdateJob(ctx, "job_1", "completed", 100, "done", ""); err != nil {
		t.Fatalf("UpdateJob returned error: %v", err)
	}
	gotJob, err := store.GetJob(ctx, "job_1")
	if err != nil {
		t.Fatalf("GetJob returned error: %v", err)
	}
	if gotJob.Status != "completed" || gotJob.Progress != 100 {
		t.Fatalf("unexpected job: %#v", gotJob)
	}

	mod := Mod{
		ID:            "mod_1",
		Name:          "Test",
		Source:        "upload",
		PackageName:   "TestPackage",
		Path:          "x",
		Enabled:       true,
		WorkshopID:    "123456",
		PreviewURL:    "https://cdn.example/mod.jpg",
		SteamURL:      "https://steamcommunity.com/sharedfiles/filedetails/?id=123456",
		Summary:       "summary",
		Tags:          []string{"QoL", "Server"},
		FileSize:      123,
		Subscriptions: 456,
		TimeUpdated:   789,
		LastCheckedAt: "2026-01-02T03:04:05Z",
	}
	if err := store.UpsertMod(ctx, mod); err != nil {
		t.Fatalf("UpsertMod returned error: %v", err)
	}
	mods, err := store.ListMods(ctx)
	if err != nil {
		t.Fatalf("ListMods returned error: %v", err)
	}
	if len(mods) != 1 || !mods[0].Enabled || mods[0].WorkshopID != "123456" || len(mods[0].Tags) != 2 {
		t.Fatalf("unexpected mods: %#v", mods)
	}

	if err := store.SetKV(ctx, "pending_restart", "true"); err != nil {
		t.Fatalf("SetKV returned error: %v", err)
	}
	value, ok, err := store.GetKV(ctx, "pending_restart")
	if err != nil {
		t.Fatalf("GetKV returned error: %v", err)
	}
	if !ok || value != "true" {
		t.Fatalf("unexpected kv: %v %q", ok, value)
	}

	translation := AITranslation{
		WorkshopID: "123456", SourceSHA256: "hash", TargetLanguage: "zh-CN", Provider: "provider", Model: "model", Translation: "译文",
	}
	if err := store.UpsertAITranslation(ctx, translation); err != nil {
		t.Fatalf("UpsertAITranslation returned error: %v", err)
	}
	gotTranslation, err := store.GetAITranslation(ctx, "123456", "hash", "zh-CN", "provider", "model")
	if err != nil || gotTranslation.Translation != "译文" || gotTranslation.CreatedAt == "" {
		t.Fatalf("unexpected AI translation: %#v, %v", gotTranslation, err)
	}
}

func TestConfigDraftClaimIsCompareAndSwap(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "draft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	draft := ConfigDraft{ID: "cfg_1", BaseSHA256: "hash", DraftPath: "draft.ini", Status: "draft"}
	if err := store.CreateConfigDraft(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimConfigDraft(t.Context(), draft.ID, "job_1")
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v", claimed, err)
	}
	claimed, err = store.ClaimConfigDraft(t.Context(), draft.ID, "job_2")
	if err != nil || claimed {
		t.Fatalf("second claim = %v, %v", claimed, err)
	}
}

func TestCreateConfigDraftReplacingSupersedesReusableDrafts(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "draft-replace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, draft := range []ConfigDraft{
		{ID: "old_draft", BaseSHA256: "a", DraftPath: "old-draft.ini", Status: "draft"},
		{ID: "old_failed", BaseSHA256: "b", DraftPath: "old-failed.ini", Status: "failed"},
		{ID: "applying", BaseSHA256: "c", DraftPath: "applying.ini", Status: "applying"},
	} {
		if err := store.CreateConfigDraft(t.Context(), draft); err != nil {
			t.Fatal(err)
		}
	}
	replaced, err := store.CreateConfigDraftReplacing(t.Context(), ConfigDraft{ID: "new", BaseSHA256: "d", DraftPath: "new.ini", Status: "draft"})
	if err != nil {
		t.Fatal(err)
	}
	if len(replaced) != 2 {
		t.Fatalf("replaced = %#v", replaced)
	}
	for _, id := range []string{"old_draft", "old_failed"} {
		draft, err := store.GetConfigDraft(t.Context(), id)
		if err != nil || draft.Status != "superseded" {
			t.Fatalf("%s = %#v, %v", id, draft, err)
		}
	}
	active, err := store.GetConfigDraft(t.Context(), "applying")
	if err != nil || active.Status != "applying" {
		t.Fatalf("applying = %#v, %v", active, err)
	}
}

func TestExpireConfigDraftsReturnsPrivateFilesForCleanup(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "draft-expire.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateConfigDraft(t.Context(), ConfigDraft{ID: "old", BaseSHA256: "a", DraftPath: "old.ini", Status: "draft"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `UPDATE config_drafts SET created_at='2026-07-20T00:00:00Z' WHERE id='old'`); err != nil {
		t.Fatal(err)
	}
	expired, err := store.ExpireConfigDrafts(t.Context(), "2026-07-21T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0].ID != "old" || expired[0].DraftPath != "old.ini" {
		t.Fatalf("expired = %#v", expired)
	}
	draft, err := store.GetConfigDraft(t.Context(), "old")
	if err != nil || draft.Status != "expired" {
		t.Fatalf("old = %#v, %v", draft, err)
	}
}

func TestConfigDraftPersistsModifiedFieldsAndQueuesTerminalCleanup(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "draft-fields.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	draft := ConfigDraft{
		ID: "cfg_fields", BaseSHA256: "hash", DraftPath: "private.ini", Status: "draft",
		ModifiedFields: []string{"ServerName", "RESTAPIPort", "ServerName"},
	}
	if err := store.CreateConfigDraft(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetConfigDraft(t.Context(), draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.ModifiedFields, ",") != "RESTAPIPort,ServerName" {
		t.Fatalf("modified fields = %#v", got.ModifiedFields)
	}
	if err := store.UpdateConfigDraftStatusAndQueueCleanup(t.Context(), draft.ID, "stale", "", "config_draft"); err != nil {
		t.Fatal(err)
	}
	pending, err := store.ListConfigPrivateCleanup(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Path != "private.ini" || pending[0].Attempts != 0 {
		t.Fatalf("pending cleanup = %#v", pending)
	}
	if err := store.RecordConfigPrivateCleanupFailure(t.Context(), "private.ini", "access denied"); err != nil {
		t.Fatal(err)
	}
	pending, err = store.ListConfigPrivateCleanup(t.Context(), 10)
	if err != nil || len(pending) != 1 || pending[0].Attempts != 1 || pending[0].LastError != "access denied" {
		t.Fatalf("failed cleanup = %#v, %v", pending, err)
	}
	if err := store.CompleteConfigPrivateCleanup(t.Context(), "private.ini"); err != nil {
		t.Fatal(err)
	}
	pending, err = store.ListConfigPrivateCleanup(t.Context(), 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("completed cleanup = %#v, %v", pending, err)
	}
}

func TestSupersedeAndExpireQueuePrivateCleanupInSameTransaction(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "draft-cleanup-queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateConfigDraft(t.Context(), ConfigDraft{ID: "old", BaseSHA256: "a", DraftPath: "old.ini", Status: "draft"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateConfigDraftReplacing(t.Context(), ConfigDraft{ID: "new", BaseSHA256: "b", DraftPath: "new.ini", Status: "draft"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `UPDATE config_drafts SET created_at='2026-07-20T00:00:00Z' WHERE id='new'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExpireConfigDrafts(t.Context(), "2026-07-21T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	pending, err := store.ListConfigPrivateCleanup(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].Path != "new.ini" && pending[1].Path != "new.ini" {
		t.Fatalf("pending cleanup = %#v", pending)
	}
}

func TestStoreMigratesLegacyModsTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	d, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	_, err = d.Exec(`CREATE TABLE mods (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT NOT NULL,
		package_name TEXT NOT NULL,
		path TEXT NOT NULL,
		version TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create legacy mods table returned error: %v", err)
	}
	_, err = d.Exec(`INSERT INTO mods (id,name,source,package_name,path,version,enabled,created_at,updated_at)
		VALUES ('mod_legacy','Legacy','workshop','LegacyPkg','/tmp/mod','','1','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert legacy mod returned error: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close legacy db returned error: %v", err)
	}

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer store.Close()

	mods, err := store.ListMods(context.Background())
	if err != nil {
		t.Fatalf("ListMods returned error: %v", err)
	}
	if len(mods) != 1 || mods[0].ID != "mod_legacy" || !mods[0].Enabled {
		t.Fatalf("legacy mod was not preserved: %#v", mods)
	}
	if mods[0].WorkshopID != "" || mods[0].PreviewURL != "" || mods[0].FileSize != 0 {
		t.Fatalf("legacy mod defaults not applied: %#v", mods[0])
	}
	version, err := store.SchemaVersion(context.Background())
	if err != nil || version != 11 {
		t.Fatalf("schema version = %d, %v", version, err)
	}
}

func TestStoreScheduleTimezoneAndMonitorPruning(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "operational.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	item := Schedule{ID: "schedule_1", Type: "backup", Enabled: true, TimeOfDay: "04:00", Timezone: "Asia/Shanghai"}
	if err := store.UpsertSchedule(ctx, item); err != nil {
		t.Fatalf("UpsertSchedule returned error: %v", err)
	}
	got, err := store.GetSchedule(ctx, item.ID)
	if err != nil || got.Timezone != "Asia/Shanghai" {
		t.Fatalf("unexpected schedule: %#v, %v", got, err)
	}
	old := time.Now().UTC().Add(-48 * time.Hour)
	for _, sample := range []MonitorSample{
		{ID: "old", CreatedAt: old.Format(time.RFC3339Nano)},
		{ID: "new", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
	} {
		if err := store.InsertMonitorSample(ctx, sample); err != nil {
			t.Fatalf("InsertMonitorSample returned error: %v", err)
		}
	}
	deleted, err := store.DeleteMonitorSamplesBefore(ctx, time.Now().UTC().Add(-24*time.Hour), 100)
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteMonitorSamplesBefore = %d, %v", deleted, err)
	}
	samples, err := store.ListMonitorSamples(ctx, 10)
	if err != nil || len(samples) != 1 || samples[0].ID != "new" {
		t.Fatalf("unexpected monitor samples: %#v, %v", samples, err)
	}
}

func TestStoreOperationalEntitiesAndErrors(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "entities.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	if err := store.CreateAuditLog(ctx, AuditLog{}); err == nil {
		t.Fatal("expected empty audit id to fail")
	}
	if err := store.CreateAuditLog(ctx, AuditLog{ID: "audit_1", Actor: "admin", Role: "admin", Action: "PUT /api/test", Status: "success"}); err != nil {
		t.Fatalf("CreateAuditLog returned error: %v", err)
	}
	audits, err := store.ListAuditLogs(ctx, 0)
	if err != nil || len(audits) != 1 || audits[0].CreatedAt == "" {
		t.Fatalf("unexpected audits: %#v, %v", audits, err)
	}

	if err := store.UpsertPlayerAccess(ctx, "ban", PlayerAccessEntry{}); err == nil {
		t.Fatal("expected empty player access id to fail")
	}
	if err := store.UpsertPlayerAccess(ctx, "ban", PlayerAccessEntry{SteamID: "123", Nickname: "Player"}); err != nil {
		t.Fatalf("UpsertPlayerAccess returned error: %v", err)
	}
	entries, err := store.ListPlayerAccess(ctx, "ban")
	if err != nil || len(entries) != 1 || entries[0].Nickname != "Player" {
		t.Fatalf("unexpected player entries: %#v, %v", entries, err)
	}
	if err := store.ReplacePlayerAccess(ctx, "ban", []PlayerAccessEntry{{}}); err == nil {
		t.Fatal("expected invalid replacement to fail")
	}
	entries, _ = store.ListPlayerAccess(ctx, "ban")
	if len(entries) != 1 {
		t.Fatalf("failed replacement should roll back: %#v", entries)
	}
	if err := store.ReplacePlayerAccess(ctx, "ban", []PlayerAccessEntry{{SteamID: "456", Reason: "test"}}); err != nil {
		t.Fatalf("ReplacePlayerAccess returned error: %v", err)
	}
	if err := store.DeletePlayerAccess(ctx, "ban", "456"); err != nil {
		t.Fatalf("DeletePlayerAccess returned error: %v", err)
	}
	if err := store.DeletePlayerAccess(ctx, "ban", "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DeletePlayerAccess error = %v", err)
	}

	if err := store.InsertMonitorSample(ctx, MonitorSample{}); err == nil {
		t.Fatal("expected empty monitor id to fail")
	}
	if err := store.UpsertSchedule(ctx, Schedule{}); err == nil {
		t.Fatal("expected empty schedule id to fail")
	}
	if err := store.UpsertSchedule(ctx, Schedule{ID: "schedule"}); err == nil {
		t.Fatal("expected empty schedule type to fail")
	}
	if err := store.UpsertSchedule(ctx, Schedule{ID: "schedule", Type: "backup", Enabled: true, IntervalMinutes: 5, Timezone: "UTC"}); err != nil {
		t.Fatalf("UpsertSchedule returned error: %v", err)
	}
	schedules, err := store.ListSchedules(ctx)
	if err != nil || len(schedules) != 1 || !schedules[0].Enabled {
		t.Fatalf("unexpected schedules: %#v, %v", schedules, err)
	}
	if err := store.DeleteSchedule(ctx, "schedule"); err != nil {
		t.Fatalf("DeleteSchedule returned error: %v", err)
	}
	if err := store.DeleteSchedule(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DeleteSchedule error = %v", err)
	}

	if err := store.CreateAlert(ctx, Alert{}); err == nil {
		t.Fatal("expected empty alert id to fail")
	}
	if err := store.CreateAlert(ctx, Alert{ID: "alert", Severity: "warning", Title: "title", Message: "message", Source: "test"}); err != nil {
		t.Fatalf("CreateAlert returned error: %v", err)
	}
	alerts, err := store.ListAlerts(ctx, 0)
	if err != nil || len(alerts) != 1 || alerts[0].Status != "open" {
		t.Fatalf("unexpected alerts: %#v, %v", alerts, err)
	}
	if err := store.AckAlert(ctx, "alert"); err != nil {
		t.Fatalf("AckAlert returned error: %v", err)
	}
	if err := store.AckAlert(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("AckAlert error = %v", err)
	}

	mod := Mod{ID: "mod", Name: "Mod", Source: "upload", PackageName: "Pkg", Path: "/tmp/mod"}
	if err := store.UpsertMod(ctx, mod); err != nil {
		t.Fatalf("UpsertMod returned error: %v", err)
	}
	gotMod, err := store.GetMod(ctx, mod.ID)
	if err != nil || gotMod.ID != mod.ID {
		t.Fatalf("GetMod = %#v, %v", gotMod, err)
	}
	if err := store.SetModEnabled(ctx, mod.ID, true); err != nil {
		t.Fatalf("SetModEnabled returned error: %v", err)
	}
	if err := store.SetModEnabled(ctx, "missing", true); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("SetModEnabled error = %v", err)
	}
	if err := store.DeleteMod(ctx, mod.ID); err != nil {
		t.Fatalf("DeleteMod returned error: %v", err)
	}
	if err := store.DeleteMod(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DeleteMod error = %v", err)
	}

	if _, ok, err := store.GetKV(ctx, "missing"); err != nil || ok {
		t.Fatalf("missing KV = %v, %v", ok, err)
	}
	if _, err := store.GetAITranslation(ctx, "missing", "hash", "zh-CN", "provider", "model"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetAITranslation error = %v", err)
	}
	if err := store.UpdateJob(ctx, "missing", "failed", 0, "missing", "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpdateJob error = %v", err)
	}
}

func TestSaveSourceIndexMetadataLifecycle(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "save-sources.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	server := SaveSource{ID: "server", Name: "当前服务器存档", Kind: "server"}
	if err := store.UpsertSaveSource(ctx, server); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateSaveSource(ctx, server.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSaveSourceIndex(ctx, server.ID, "sha256:fixture", "sav-cli-test", []string{"one warning"}, "2026-07-16T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	got, err := store.ActiveSaveSource(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fingerprint != "sha256:fixture" || got.ParserVersion != "sav-cli-test" || got.IndexedAt == "" || len(got.Warnings) != 1 {
		t.Fatalf("unexpected indexed source: %#v", got)
	}
	got.Name = "重命名后的服务器存档"
	if err := store.UpsertSaveSource(ctx, got); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetSaveSource(ctx, server.ID)
	if err != nil || got.Name != "重命名后的服务器存档" || got.Fingerprint != "sha256:fixture" {
		t.Fatalf("metadata was not preserved by rename: %#v, %v", got, err)
	}
}

func TestBreedingStorageLifecycle(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "breeding.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.CreateJob(ctx, "job-breed", "breeding", "queued"); err != nil {
		t.Fatal(err)
	}
	result := BreedingResult{
		ID: "result-1", JobID: "job-breed", Subject: "user:admin", SourceID: "server",
		Fingerprint: "sha256:save", RequestJSON: `{"target":"Anubis"}`, Status: "running",
	}
	if err := store.CreateBreedingResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteBreedingResult(ctx, result.JobID, "completed", `{"results":[{"pal_id":"Anubis"}]}`); err != nil {
		t.Fatal(err)
	}
	gotResult, err := store.GetBreedingResultByJob(ctx, result.JobID)
	if err != nil || gotResult.Status != "completed" || gotResult.ResultJSON == "" {
		t.Fatalf("GetBreedingResultByJob = %#v, %v", gotResult, err)
	}
	results, err := store.ListBreedingResults(ctx, result.Subject, 0)
	if err != nil || len(results) != 1 {
		t.Fatalf("ListBreedingResults = %#v, %v", results, err)
	}
	cached, err := store.FindCachedBreedingResult(ctx, result.SourceID, result.Fingerprint, result.RequestJSON)
	if err != nil || cached.JobID != result.JobID {
		t.Fatalf("FindCachedBreedingResult = %#v, %v", cached, err)
	}

	preset := BreedingPreset{ID: "preset-1", Subject: result.Subject, Name: "Fast", ConfigJSON: `{"steps":4}`}
	if err := store.UpsertBreedingPreset(ctx, preset); err != nil {
		t.Fatal(err)
	}
	preset.Name = "Updated"
	if err := store.UpsertBreedingPreset(ctx, preset); err != nil {
		t.Fatal(err)
	}
	presets, err := store.ListBreedingPresets(ctx, result.Subject)
	if err != nil || len(presets) != 1 || presets[0].Name != "Updated" {
		t.Fatalf("ListBreedingPresets = %#v, %v", presets, err)
	}
	if err := store.DeleteBreedingPreset(ctx, result.Subject, preset.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteBreedingPreset(ctx, result.Subject, preset.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing preset error = %v", err)
	}

	container := CustomPalContainer{ID: "container-1", Subject: result.Subject, Name: "Stock", PalsJSON: `[{"character_id":"Anubis"}]`}
	if err := store.UpsertCustomPalContainer(ctx, container); err != nil {
		t.Fatal(err)
	}
	container.Name = "Updated stock"
	if err := store.UpsertCustomPalContainer(ctx, container); err != nil {
		t.Fatal(err)
	}
	gotContainer, err := store.GetCustomPalContainer(ctx, result.Subject, container.ID)
	if err != nil || gotContainer.Name != "Updated stock" {
		t.Fatalf("GetCustomPalContainer = %#v, %v", gotContainer, err)
	}
	containers, err := store.ListCustomPalContainers(ctx, result.Subject)
	if err != nil || len(containers) != 1 {
		t.Fatalf("ListCustomPalContainers = %#v, %v", containers, err)
	}
	if err := store.DeleteCustomPalContainer(ctx, result.Subject, container.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteCustomPalContainer(ctx, result.Subject, container.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing container error = %v", err)
	}

	now := time.Now().UTC()
	session := BreedSession{ID: "session-1", Subject: result.Subject, TokenHash: "hash-1", PlayerUID: "uid-1", ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano)}
	if err := store.CreateBreedSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	gotSession, err := store.GetBreedSession(ctx, session.TokenHash, now)
	if err != nil || gotSession.PlayerUID != session.PlayerUID {
		t.Fatalf("GetBreedSession = %#v, %v", gotSession, err)
	}
	if err := store.DeleteExpiredBreedSessions(ctx, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetBreedSession(ctx, session.TokenHash, now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired session error = %v", err)
	}
}

func TestConfigRevisionHistoryPersistsAndPrunesPrivateSnapshots(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "config-revisions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	oldSHA := strings.Repeat("a", 64)
	newSHA := strings.Repeat("b", 64)
	revisionDir := filepath.Join(t.TempDir(), "config-revisions")
	for _, revision := range []ConfigRevision{
		{ID: "rev_old", RevisionSHA256: oldSHA, SnapshotPath: filepath.Join(revisionDir, "rev_old.ini"), Source: "baseline", ChangedFields: []string{"ServerName"}, CreatedAt: "2026-07-29T00:00:00Z"},
		{ID: "rev_new", RevisionSHA256: newSHA, ParentSHA256: oldSHA, SnapshotPath: filepath.Join(revisionDir, "rev_new.ini"), Source: "apply", ChangedFields: []string{"ServerName", "AdminPassword"}, CreatedAt: "2026-07-29T01:00:00Z"},
	} {
		if err := store.CreateConfigRevision(t.Context(), revision); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.ListConfigRevisions(t.Context(), 10)
	if err != nil || len(items) != 2 || items[0].ID != "rev_new" || len(items[0].ChangedFields) != 2 {
		t.Fatalf("revisions = %#v, %v", items, err)
	}
	removed, err := store.PruneConfigRevisions(t.Context(), 1)
	if err != nil || len(removed) != 1 || removed[0].ID != "rev_old" {
		t.Fatalf("removed = %#v, %v", removed, err)
	}
	pending, err := store.ListConfigPrivateCleanup(t.Context(), 10)
	if err != nil || len(pending) != 1 || pending[0].Path != filepath.Join(revisionDir, "rev_old.ini") || pending[0].Kind != "config_revision" {
		t.Fatalf("cleanup = %#v, %v", pending, err)
	}
	if err := store.CreateConfigRevision(t.Context(), ConfigRevision{ID: "bad", RevisionSHA256: "short", SnapshotPath: filepath.Join(revisionDir, "bad.ini"), Source: "apply"}); err == nil {
		t.Fatal("invalid revision SHA-256 was accepted")
	}
	if err := store.CreateConfigRevision(t.Context(), ConfigRevision{ID: "bad/path", RevisionSHA256: oldSHA, SnapshotPath: filepath.Join(revisionDir, "bad-path.ini"), Source: "apply"}); err == nil {
		t.Fatal("unsafe revision ID was accepted")
	}
	if err := store.CreateConfigRevision(t.Context(), ConfigRevision{ID: "wrong_path", RevisionSHA256: oldSHA, SnapshotPath: filepath.Join(t.TempDir(), "wrong_path.ini"), Source: "apply"}); err == nil {
		t.Fatal("snapshot outside config-revisions was accepted")
	}
}
