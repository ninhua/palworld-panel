package gameevents

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestListRangeFiltersTimeAndType(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	ctx := context.Background()
	base := time.Date(2026, 8, 6, 1, 0, 0, 0, time.UTC)
	fixtures := []Event{
		{EventID: "before", Type: "PAL_CAPTURED", PlayerUID: "player-a", OccurredAt: base.Add(-time.Minute).Format(time.RFC3339), Payload: map[string]any{}},
		{EventID: "capture", Type: "PAL_CAPTURED", PlayerUID: "player-a", OccurredAt: base.Add(time.Minute).Format(time.RFC3339), Payload: map[string]any{"pal_id": "Anubis"}},
		{EventID: "craft", Type: "ITEM_CRAFTED", PlayerUID: "player-a", OccurredAt: base.Add(2 * time.Minute).Format(time.RFC3339), Payload: map[string]any{"item_name": "Sphere_Ultimate"}},
		{EventID: "after", Type: "PAL_CAPTURED", PlayerUID: "player-a", OccurredAt: base.Add(10 * time.Minute).Format(time.RFC3339), Payload: map[string]any{}},
	}
	for _, event := range fixtures {
		claim, claimErr := service.Claim(ctx, event)
		if claimErr != nil {
			t.Fatalf("claim %s: %v", event.EventID, claimErr)
		}
		if _, completeErr := service.Complete(ctx, claim.Record.EventID, map[string]any{}); completeErr != nil {
			t.Fatalf("complete %s: %v", event.EventID, completeErr)
		}
	}

	items, err := service.ListRange(ctx, base, base.Add(5*time.Minute), []string{"PAL_CAPTURED"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EventID != "capture" {
		t.Fatalf("items = %#v", items)
	}
}

func TestListRangeFallsBackToCreatedAt(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	ctx := context.Background()
	claim, err := service.Claim(ctx, Event{EventID: "login", Type: "PLAYER_LOGIN", PlayerUID: "player-a", Payload: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(ctx, claim.Record.EventID, map[string]any{}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	items, err := service.ListRange(ctx, now.Add(-time.Minute), now.Add(time.Minute), nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EventID != "login" {
		t.Fatalf("items = %#v", items)
	}
}
