package db

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestCrashGuardStorePersistsStateEventsAndPrunes(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "guard.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := t.Context()
	state, err := store.GetCrashGuardState(ctx)
	if err != nil || state.Tripped {
		t.Fatalf("initial state = %#v, %v", state, err)
	}
	state.Tripped = true
	state.Reason = "loop"
	state.LastRestartCount = 3
	state.RecoveredAt = "2026-07-29T11:59:00Z"
	state.UpdatedAt = "2026-07-29T12:00:00Z"
	if err := store.PutCrashGuardState(ctx, state); err != nil {
		t.Fatal(err)
	}
	persisted, err := store.GetCrashGuardState(ctx)
	if err != nil || !persisted.Tripped || persisted.RecoveredAt != state.RecoveredAt {
		t.Fatalf("persisted state = %#v, %v", persisted, err)
	}
	for index := 0; index < 3; index++ {
		event := CrashGuardEvent{ID: fmt.Sprintf("event_%c", 'a'+index), Kind: "container_restart", RuntimeMode: "wine_docker",
			Occurrences: index + 1, RestartCount: index + 1, Message: "restart", CreatedAt: fmt.Sprintf("2026-07-29T12:0%d:00Z", index)}
		if err := store.CreateCrashGuardEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	count, err := store.CountCrashGuardOccurrencesSince(ctx, "2026-07-29T12:00:00Z")
	if err != nil || count != 6 {
		t.Fatalf("occurrences = %d, %v", count, err)
	}
	if err := store.PruneCrashGuardEvents(ctx, 2); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListCrashGuardEvents(ctx, 20)
	if err != nil || len(events) != 2 || events[0].ID != "event_c" {
		t.Fatalf("events = %#v, %v", events, err)
	}
}

func TestCrashGuardStoreRejectsInvalidEvent(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "guard.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateCrashGuardEvent(t.Context(), CrashGuardEvent{ID: "../bad", Kind: "other", RuntimeMode: "wine_docker", Occurrences: 1}); err == nil {
		t.Fatal("expected invalid event to be rejected")
	}
}
