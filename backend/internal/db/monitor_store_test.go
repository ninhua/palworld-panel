package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func createV9MonitorDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := configureSQLite(raw); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	for _, migration := range migrations() {
		if migration.version > 9 {
			break
		}
		tx, err := raw.BeginTx(t.Context(), nil)
		if err != nil {
			raw.Close()
			t.Fatal(err)
		}
		if err := migration.apply(t.Context(), tx); err != nil {
			_ = tx.Rollback()
			raw.Close()
			t.Fatalf("apply migration %d: %v", migration.version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,applied_at) VALUES (?,?)`, migration.version, "2026-07-22T00:00:00Z"); err != nil {
			_ = tx.Rollback()
			raw.Close()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			raw.Close()
			t.Fatal(err)
		}
	}
	return raw
}

func insertV9MonitorAlert(t *testing.T, raw *sql.DB, id, source, status string) {
	t.Helper()
	if _, err := raw.Exec(`INSERT INTO alerts(id,severity,title,message,source,status,created_at,ack_at)
		VALUES (?,?,?,?,?,?,?,?)`, id, "warning", id, id, source, status, "2026-07-22T00:00:00Z", ""); err != nil {
		t.Fatal(err)
	}
}

func insertV9MonitorState(t *testing.T, raw *sql.DB, code string, open bool, alertID string) {
	t.Helper()
	if _, err := raw.Exec(`INSERT INTO monitor_alert_states(code,unhealthy_count,healthy_count,open,alert_id,updated_at)
		VALUES (?,?,?,?,?,?)`, code, 2, 0, boolInt(open), alertID, "2026-07-22T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
}

func assertOpenMonitorPair(t *testing.T, store *Store, code, alertID string) {
	t.Helper()
	state, found, err := store.GetMonitorAlertState(t.Context(), code)
	if err != nil || !found || !state.Open || state.AlertID != alertID {
		t.Fatalf("state %s = %#v, found=%v, err=%v", code, state, found, err)
	}
	var count int
	var gotID string
	if err := store.db.QueryRow(`SELECT COUNT(*),COALESCE(MIN(id),'') FROM alerts WHERE source=? AND status='open'`, "monitor:"+code).Scan(&count, &gotID); err != nil {
		t.Fatal(err)
	}
	if count != 1 || gotID != alertID {
		t.Fatalf("open alerts for %s = count %d id %q", code, count, gotID)
	}
}

func TestMonitorMigrationUpgradesLegacyRowsWithSafeDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-monitor.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 8; version++ {
		if _, err := raw.Exec(`INSERT INTO schema_migrations(version,applied_at) VALUES (?,?)`, version, "2026-07-22T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec(`CREATE TABLE monitor_samples (
		id TEXT PRIMARY KEY, created_at TEXT NOT NULL, cpu_available INTEGER NOT NULL DEFAULT 0,
		cpu_percent REAL NOT NULL DEFAULT 0, memory_available INTEGER NOT NULL DEFAULT 0,
		memory_usage_bytes INTEGER NOT NULL DEFAULT 0, memory_limit_bytes INTEGER NOT NULL DEFAULT 0,
		disk_available INTEGER NOT NULL DEFAULT 0, disk_free_bytes INTEGER NOT NULL DEFAULT 0,
		disk_total_bytes INTEGER NOT NULL DEFAULT 0, current_players INTEGER NOT NULL DEFAULT 0,
		max_players INTEGER NOT NULL DEFAULT 0, rest_healthy INTEGER NOT NULL DEFAULT 0,
		rcon_healthy INTEGER NOT NULL DEFAULT 0, game_port_healthy INTEGER NOT NULL DEFAULT 0,
		query_port_healthy INTEGER NOT NULL DEFAULT 0, unavailable_reason TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE alerts (
		id TEXT PRIMARY KEY,severity TEXT NOT NULL,title TEXT NOT NULL,message TEXT NOT NULL,source TEXT NOT NULL,
		status TEXT NOT NULL,created_at TEXT NOT NULL,ack_at TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO monitor_samples(id,created_at,memory_available,memory_usage_bytes,memory_limit_bytes) VALUES ('legacy','2026-07-22T00:00:00Z',1,10,20)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy database returned error: %v", err)
	}
	defer store.Close()
	version, err := store.SchemaVersion(context.Background())
	if err != nil || version != 12 {
		t.Fatalf("schema version = %d, %v", version, err)
	}
	samples, err := store.ListMonitorSamples(context.Background(), 10)
	if err != nil || len(samples) != 1 {
		t.Fatalf("legacy samples = %#v, %v", samples, err)
	}
	got := samples[0]
	if got.MemoryUsageBytes != 10 || got.HostMemoryAvailable || got.WorkloadMemoryAvailable || got.LifecycleAvailable || got.OOMKilled || got.ExitCode != 0 || len(got.RiskReasons) != 0 {
		t.Fatalf("legacy defaults were not preserved: %#v", got)
	}
}

func TestMonitorSampleAndAlertStateRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "monitor-roundtrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	want := MonitorSample{
		ID: "sample", CreatedAt: "2026-07-22T01:00:00Z",
		HostMemoryAvailable: true, HostMemoryTotalBytes: 16 << 30, HostMemoryAvailableBytes: 2 << 30,
		HostSwapTotalBytes: 4 << 30, HostSwapFreeBytes: 1 << 30,
		WorkloadMemoryAvailable: true, WorkloadMemoryUsageBytes: 7 << 30, WorkloadMemoryLimitBytes: 8 << 30,
		LifecycleAvailable: true, OOMKilled: true, ExitCode: 137, RestartCount: 5,
		StartedAt: "2026-07-22T00:00:00Z", FinishedAt: "2026-07-22T00:59:00Z",
		RiskReasons: []MonitorRiskReason{{Code: "oom_killed", Message: "工作负载被 OOM 终止", Severity: "critical"}},
	}
	if err := store.InsertMonitorSample(ctx, want); err != nil {
		t.Fatal(err)
	}
	samples, err := store.ListMonitorSamples(ctx, 1)
	if err != nil || len(samples) != 1 {
		t.Fatalf("samples = %#v, %v", samples, err)
	}
	got := samples[0]
	if !got.HostMemoryAvailable || got.HostMemoryTotalBytes != want.HostMemoryTotalBytes || got.WorkloadMemoryUsageBytes != want.WorkloadMemoryUsageBytes || !got.LifecycleAvailable || !got.OOMKilled || got.ExitCode != 137 || got.RestartCount != 5 || len(got.RiskReasons) != 1 || got.RiskReasons[0].Code != "oom_killed" {
		t.Fatalf("monitor round trip = %#v", got)
	}

	state := MonitorAlertState{Code: "host_memory_pressure", UnhealthyCount: 2, HealthyCount: 0, Open: true, AlertID: "alert-1"}
	if err := store.UpsertMonitorAlertState(ctx, state); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := store.GetMonitorAlertState(ctx, state.Code)
	if err != nil || !found || loaded.UnhealthyCount != 2 || !loaded.Open || loaded.AlertID != "alert-1" || loaded.UpdatedAt == "" {
		t.Fatalf("alert state = %#v, %v, %v", loaded, found, err)
	}
}

func TestMonitorMigrationPreventsDuplicateOpenRoundsBySource(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "monitor-alert-unique.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	first := Alert{ID: "first", Severity: "warning", Title: "first", Message: "first", Source: "monitor:host_memory_pressure"}
	second := Alert{ID: "second", Severity: "warning", Title: "second", Message: "second", Source: first.Source}
	if err := store.CreateAlert(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAlert(ctx, second); err == nil {
		t.Fatal("expected duplicate open monitor alert source to be rejected")
	}
}

func TestMonitorMigration10ReconcilesLegacyAlertStateOrphans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor-v9-orphans.db")
	raw := createV9MonitorDatabase(t, path)

	// An open state pointing to a missing alert must close before a new immediate OOM round.
	insertV9MonitorState(t, raw, "oom_killed", true, "missing-oom")
	// Open alerts with no open state must be re-linked before the third ordinary sample.
	insertV9MonitorAlert(t, raw, "host-a", "monitor:host_memory_pressure", "open")
	insertV9MonitorAlert(t, raw, "host-b", "monitor:host_memory_pressure", "open")
	insertV9MonitorState(t, raw, "host_memory_pressure", false, "")
	// Open alerts without any state must create a state and deduplicate the round.
	insertV9MonitorAlert(t, raw, "workload-a", "monitor:workload_memory_pressure", "open")
	insertV9MonitorAlert(t, raw, "workload-b", "monitor:workload_memory_pressure", "open")
	// An open state with a nonexistent alert can recover from another open alert for its source.
	insertV9MonitorAlert(t, raw, "swap-a", "monitor:swap_exhaustion", "open")
	insertV9MonitorState(t, raw, "swap_exhaustion", true, "missing-swap")
	// Missing and non-open alert targets with no matching open alert must be closed.
	insertV9MonitorState(t, raw, "missing_id", true, "")
	insertV9MonitorAlert(t, raw, "abnormal-resolved", "monitor:abnormal_exit", "resolved")
	insertV9MonitorState(t, raw, "abnormal_exit", true, "abnormal-resolved")
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("open v9 database: %v", err)
	}
	defer store.Close()
	assertOpenMonitorPair(t, store, "host_memory_pressure", "host-a")
	assertOpenMonitorPair(t, store, "workload_memory_pressure", "workload-a")
	assertOpenMonitorPair(t, store, "swap_exhaustion", "swap-a")
	for _, code := range []string{"oom_killed", "missing_id", "abnormal_exit"} {
		state, found, err := store.GetMonitorAlertState(t.Context(), code)
		if err != nil || !found || state.Open || state.AlertID != "" {
			t.Fatalf("closed orphan state %s = %#v, found=%v, err=%v", code, state, found, err)
		}
	}

	// These samples previously collided with the unique open-source index or updated a missing alert.
	oomAlert := Alert{ID: "new-oom", Severity: "error", Title: "oom", Message: "oom", Source: "monitor:oom_killed", Status: "open"}
	if err := store.ApplyMonitorAlertSample(t.Context(), "oom_killed", true, true, oomAlert); err != nil {
		t.Fatalf("immediate sample after migration: %v", err)
	}
	for sample := 1; sample <= 3; sample++ {
		hostAlert := Alert{ID: "new-host", Severity: "warning", Title: "host", Message: "host", Source: "monitor:host_memory_pressure", Status: "open"}
		if err := store.ApplyMonitorAlertSample(t.Context(), "host_memory_pressure", true, false, hostAlert); err != nil {
			t.Fatalf("ordinary sample %d after migration: %v", sample, err)
		}
	}
	assertOpenMonitorPair(t, store, "oom_killed", "new-oom")
	assertOpenMonitorPair(t, store, "host_memory_pressure", "host-a")

	for _, code := range []string{"oom_killed", "host_memory_pressure"} {
		for healthy := 1; healthy <= 3; healthy++ {
			if err := store.ApplyMonitorAlertSample(t.Context(), code, false, false, Alert{}); err != nil {
				t.Fatalf("healthy sample %d for %s: %v", healthy, code, err)
			}
		}
		state, found, err := store.GetMonitorAlertState(t.Context(), code)
		if err != nil || !found || state.Open || state.AlertID != "" {
			t.Fatalf("recovered state %s = %#v, found=%v, err=%v", code, state, found, err)
		}
		var openCount int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM alerts WHERE source=? AND status='open'`, "monitor:"+code).Scan(&openCount); err != nil {
			t.Fatal(err)
		}
		if openCount != 0 {
			t.Fatalf("%s still has %d open alerts after recovery", code, openCount)
		}
	}
}

func TestMonitorMigration10RollsBackReconciliationAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor-v9-rollback.db")
	raw := createV9MonitorDatabase(t, path)
	insertV9MonitorAlert(t, raw, "duplicate-a", "monitor:oom_killed", "open")
	insertV9MonitorAlert(t, raw, "duplicate-b", "monitor:oom_killed", "open")
	if _, err := raw.Exec(`CREATE TRIGGER fail_monitor_reconciliation BEFORE UPDATE ON alerts
		WHEN NEW.status='resolved' BEGIN SELECT RAISE(FAIL, 'injected reconciliation failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	if store, err := Open(path); err == nil {
		store.Close()
		t.Fatal("expected migration 10 to fail")
	}
	inspect, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer inspect.Close()
	var version int
	if err := inspect.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 9 {
		t.Fatalf("schema version = %d, %v", version, err)
	}
	var lifecycleColumns int
	if err := inspect.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('monitor_samples') WHERE name='lifecycle_available'`).Scan(&lifecycleColumns); err != nil || lifecycleColumns != 0 {
		t.Fatalf("lifecycle column count = %d, %v", lifecycleColumns, err)
	}
	var openAlerts int
	if err := inspect.QueryRow(`SELECT COUNT(*) FROM alerts WHERE source='monitor:oom_killed' AND status='open'`).Scan(&openAlerts); err != nil || openAlerts != 2 {
		t.Fatalf("open alert count after rollback = %d, %v", openAlerts, err)
	}
	var states int
	if err := inspect.QueryRow(`SELECT COUNT(*) FROM monitor_alert_states`).Scan(&states); err != nil || states != 0 {
		t.Fatalf("state count after rollback = %d, %v", states, err)
	}
}

func TestApplyMonitorAlertSampleRollsBackAlertWhenStateWriteFails(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "monitor-alert-rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	state := MonitorAlertState{Code: "host_memory_pressure", UnhealthyCount: 2}
	if err := store.UpsertMonitorAlertState(ctx, state); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER fail_monitor_state_write BEFORE UPDATE ON monitor_alert_states
		BEGIN SELECT RAISE(FAIL, 'injected state failure'); END`); err != nil {
		t.Fatal(err)
	}
	alert := Alert{ID: "candidate", Severity: "warning", Title: "monitor", Message: "pressure", Source: "monitor:host_memory_pressure", Status: "open"}
	if err := store.ApplyMonitorAlertSample(ctx, state.Code, true, false, alert); err == nil {
		t.Fatal("expected injected state failure")
	}
	alerts, err := store.ListAlerts(ctx, 10)
	if err != nil || len(alerts) != 0 {
		t.Fatalf("transaction left orphan alerts: %#v, %v", alerts, err)
	}
	loaded, found, err := store.GetMonitorAlertState(ctx, state.Code)
	if err != nil || !found || loaded.UnhealthyCount != 2 || loaded.Open {
		t.Fatalf("state was partially committed: %#v, %v, %v", loaded, found, err)
	}
}
