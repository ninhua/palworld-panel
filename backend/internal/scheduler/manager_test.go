package scheduler

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"palpanel/internal/db"
	"palpanel/internal/jobs"
	"palpanel/internal/palrest"
	"palpanel/internal/server"
)

func TestNextRunUsesScheduleTimezone(t *testing.T) {
	from := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	next, err := nextRun(db.Schedule{TimeOfDay: "09:00", Timezone: "Asia/Shanghai"}, from)
	if err != nil {
		t.Fatalf("nextRun returned error: %v", err)
	}
	want := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("nextRun = %s, want %s", next, want)
	}
}

type fakeServer struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeServer) record(call string) (db.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return db.Job{ID: "job_" + call, Type: call, Status: "queued"}, f.err
}

func (f *fakeServer) Backup(context.Context) (db.Job, error) {
	return f.record("backup")
}

func (f *fakeServer) SafeRestart(ctx context.Context, wait int, message string, notify server.RestartNotifier) (db.Job, error) {
	if notify != nil {
		if err := notify(ctx, wait, message); err != nil {
			return db.Job{}, err
		}
	}
	return f.record("safe_restart")
}

func (f *fakeServer) UpdateWithPreUpdate(ctx context.Context, hook func(context.Context) error) (db.Job, error) {
	if hook != nil {
		if err := hook(ctx); err != nil {
			return db.Job{}, err
		}
	}
	return f.record("update")
}

func (f *fakeServer) CheckVersion(context.Context) (db.Job, error) {
	return f.record("version_check")
}

type fakePalREST struct {
	mu     sync.Mutex
	paths  []string
	failOn string
	status int
}

func (f *fakePalREST) Do(_ context.Context, method, path string, _ any) (palrest.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, method+" "+path)
	if path == f.failOn {
		return palrest.Response{}, errors.New("upstream failed")
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return palrest.Response{Status: status}, nil
}

func TestManagerCRUDAndRunTypes(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "scheduler.db"))
	if err != nil {
		t.Fatalf("db.Open returned error: %v", err)
	}
	defer store.Close()
	executor := jobs.New(store, 2)
	defer func() { _ = executor.Shutdown(t.Context()) }()
	serverFake := &fakeServer{}
	restFake := &fakePalREST{}
	fixed := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	manager := New(store, serverFake, restFake, executor)
	manager.now = func() time.Time { return fixed }

	created, err := manager.Create(t.Context(), db.Schedule{Type: "backup", IntervalMinutes: 60})
	if err != nil || created.ID == "" || created.Timezone != "UTC" || created.NextRunAt != fixed.Add(time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("Create = %#v, %v", created, err)
	}
	items, err := manager.List(t.Context())
	if err != nil || len(items) != 1 {
		t.Fatalf("List = %#v, %v", items, err)
	}
	updated, err := manager.Update(t.Context(), created.ID, db.Schedule{Type: "backup", Enabled: false, TimeOfDay: "09:00"})
	if err != nil || updated.Timezone != "UTC" || updated.CreatedAt != created.CreatedAt {
		t.Fatalf("Update = %#v, %v", updated, err)
	}

	for _, item := range []db.Schedule{
		{Type: "backup", IntervalMinutes: 5},
		{Type: "safe_restart", IntervalMinutes: 5, WaitTime: 30, Message: "maintenance"},
		{Type: "update", IntervalMinutes: 5},
		{Type: "version_check", IntervalMinutes: 5},
	} {
		item.Timezone = "UTC"
		if _, err := manager.run(t.Context(), item); err != nil {
			t.Fatalf("run(%s) returned error: %v", item.Type, err)
		}
	}
	if len(serverFake.calls) != 4 {
		t.Fatalf("server calls = %#v", serverFake.calls)
	}
	if len(restFake.paths) != 4 {
		t.Fatalf("REST calls = %#v", restFake.paths)
	}

	if err := manager.Delete(t.Context(), created.ID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, err := manager.RunNow(t.Context(), created.ID); err == nil {
		t.Fatal("expected deleted schedule to be missing")
	}
}

func TestManagerValidationDueRunsAndSaveFailure(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "scheduler.db"))
	if err != nil {
		t.Fatalf("db.Open returned error: %v", err)
	}
	defer store.Close()
	executor := jobs.New(store, 1)
	defer func() { _ = executor.Shutdown(t.Context()) }()
	fixed := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	serverFake := &fakeServer{}
	restFake := &fakePalREST{}
	manager := New(store, serverFake, restFake, executor)
	manager.now = func() time.Time { return fixed }

	invalid := []db.Schedule{
		{Type: "unknown", IntervalMinutes: 5},
		{Type: "backup"},
		{Type: "backup", TimeOfDay: "09:00", Timezone: "Mars/Olympus"},
		{Type: "safe_restart", IntervalMinutes: 5, WaitTime: 1},
	}
	for _, item := range invalid {
		if _, err := manager.Create(t.Context(), item); err == nil {
			t.Fatalf("expected invalid schedule to fail: %#v", item)
		}
	}

	due := db.Schedule{ID: "due", Type: "backup", Enabled: true, IntervalMinutes: 30, Timezone: "UTC", NextRunAt: fixed.Add(-time.Minute).Format(time.RFC3339Nano)}
	future := db.Schedule{ID: "future", Type: "backup", Enabled: true, IntervalMinutes: 30, Timezone: "UTC", NextRunAt: fixed.Add(time.Hour).Format(time.RFC3339Nano)}
	disabled := db.Schedule{ID: "disabled", Type: "backup", Enabled: false, IntervalMinutes: 30, Timezone: "UTC", NextRunAt: fixed.Add(-time.Minute).Format(time.RFC3339Nano)}
	for _, item := range []db.Schedule{due, future, disabled} {
		if err := store.UpsertSchedule(t.Context(), item); err != nil {
			t.Fatalf("UpsertSchedule returned error: %v", err)
		}
	}
	if err := manager.RunDue(t.Context()); err != nil {
		t.Fatalf("RunDue returned error: %v", err)
	}
	if len(serverFake.calls) != 1 || serverFake.calls[0] != "backup" {
		t.Fatalf("server calls = %#v", serverFake.calls)
	}

	restFake.failOn = "save"
	job, err := manager.saveJob(t.Context())
	if err != nil {
		t.Fatalf("saveJob returned error: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		job, err = store.GetJob(t.Context(), job.ID)
		if err == nil && job.Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("save job did not fail: %#v, %v", job, err)
		}
		time.Sleep(time.Millisecond)
	}
	alerts, err := store.ListAlerts(t.Context(), 10)
	for len(alerts) == 0 && err == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		alerts, err = store.ListAlerts(t.Context(), 10)
	}
	if err != nil || len(alerts) == 0 {
		t.Fatalf("expected save failure alert: %#v, %v", alerts, err)
	}
}

func TestManagerSkipsDuplicateAndStopsBackgroundLoop(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "scheduler.db"))
	if err != nil {
		t.Fatalf("db.Open returned error: %v", err)
	}
	defer store.Close()
	manager := New(store, &fakeServer{}, &fakePalREST{})
	manager.now = func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC) }
	if _, err := store.CreateJob(t.Context(), "running_backup", "backup", "queued"); err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if _, err := manager.run(t.Context(), db.Schedule{ID: "duplicate", Type: "backup", IntervalMinutes: 5, Timezone: "UTC"}); err == nil {
		t.Fatal("expected duplicate schedule to be skipped")
	}
	alerts, _ := store.ListAlerts(t.Context(), 10)
	if len(alerts) == 0 || alerts[0].Severity != "info" {
		t.Fatalf("unexpected alerts: %#v", alerts)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := manager.Start(ctx)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("background loop did not stop")
	}
}

func TestNextRunDefaultsLegacySchedulesToUTC(t *testing.T) {
	from := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	next, err := nextRun(db.Schedule{TimeOfDay: "09:30"}, from)
	if err != nil {
		t.Fatalf("nextRun returned error: %v", err)
	}
	want := time.Date(2026, 7, 15, 9, 30, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("nextRun = %s, want %s", next, want)
	}
}

func TestNextRunRejectsUnknownTimezone(t *testing.T) {
	_, err := nextRun(db.Schedule{TimeOfDay: "09:30", Timezone: "Mars/Olympus"}, time.Now())
	if err == nil {
		t.Fatal("expected unknown timezone to fail")
	}
}
