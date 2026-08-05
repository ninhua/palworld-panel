package server

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCrashGuardSQLiteBusyDetection(t *testing.T) {
	for _, message := range []string{
		"database is locked (5) (SQLITE_BUSY)",
		"database table is locked",
		"wrapped: SQLITE_LOCKED",
	} {
		if !crashGuardSQLiteBusy(errors.New(message)) {
			t.Fatalf("%q was not classified as SQLite lock contention", message)
		}
	}
	if crashGuardSQLiteBusy(errors.New("disk I/O error")) {
		t.Fatal("unrelated SQLite error was classified as lock contention")
	}
}

func TestCrashGuardSQLiteBusyRetryEventuallySucceeds(t *testing.T) {
	attempts := 0
	err := retryCrashGuardSQLiteBusy(t.Context(), "test crash guard write", func() error {
		attempts++
		if attempts < crashGuardBusyRetryAttempts {
			return errors.New("database is locked (5) (SQLITE_BUSY)")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != crashGuardBusyRetryAttempts {
		t.Fatalf("attempts = %d, want %d", attempts, crashGuardBusyRetryAttempts)
	}
}

func TestCrashGuardSQLiteBusyRetryReportsOperation(t *testing.T) {
	err := retryCrashGuardSQLiteBusy(t.Context(), "record crash guard event", func() error {
		return errors.New("database is locked (5) (SQLITE_BUSY)")
	})
	if err == nil || !strings.Contains(err.Error(), "record crash guard event remained locked") {
		t.Fatalf("unexpected final error: %v", err)
	}
}

func TestCrashGuardStableObservationDoesNotRewriteState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	manager, statePath, _, cleanup := newCrashGuardTestManager(t)
	defer cleanup()
	ctx := t.Context()
	now := time.Date(2026, 8, 6, 2, 0, 0, 0, time.UTC)
	manager.crashGuardNow = func() time.Time { return now }
	writeCrashGuardDockerState(t, statePath, 0, "running", false, 0)
	if err := manager.ObserveCrashGuard(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := manager.store.GetCrashGuardState(ctx)
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(5 * time.Minute)
	if err := manager.ObserveCrashGuard(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := manager.store.GetCrashGuardState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.UpdatedAt != before.UpdatedAt {
		t.Fatalf("stable observation rewrote SQLite state: before=%s after=%s", before.UpdatedAt, after.UpdatedAt)
	}
}
