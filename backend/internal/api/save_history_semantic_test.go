package api

import (
	"testing"

	"palpanel/internal/gameevents"
	"palpanel/internal/saveindex"
)

func TestFilterTrustedSaveHistoryEventsSuppressesWorldNoise(t *testing.T) {
	events := []saveindex.HistoryEvent{
		{ID: "world-item", Category: "items", Kind: "item_gained", ActorType: "unknown", ActorID: "unknown", Delta: 10},
		{ID: "map-item", Category: "items", Kind: "item_gained", ActorType: "map_object", ActorID: "chest-a", Delta: 10},
		{ID: "player-item", Category: "items", Kind: "item_gained", ActorType: "player", ActorID: "player-a", Delta: 10},
		{ID: "wild-pal", Category: "pals", Kind: "pal_acquired", ActorType: "world", ActorID: "world"},
		{ID: "captured-pal:owner", Category: "pals", Kind: "pal_transferred", ActorType: "world", ActorID: "world", TargetType: "player", TargetID: "player-a"},
		{ID: "base-change", Category: "bases", Kind: "base_structures_added", ActorType: "base", ActorID: "base-a"},
	}

	filtered := filterTrustedSaveHistoryEvents(events)
	if len(filtered) != 3 {
		t.Fatalf("filtered = %#v", filtered)
	}
	if filtered[0].ID != "player-item" || filtered[1].ID != "captured-pal:owner" || filtered[2].ID != "base-change" {
		t.Fatalf("unexpected filtered order/content: %#v", filtered)
	}
}

func TestSummarizeTrustedSaveHistoryEventsUsesOwnedEntities(t *testing.T) {
	events := []saveindex.HistoryEvent{
		{ID: "player-a:item-a", Category: "items", Kind: "item_gained", ActorType: "player", ActorID: "player-a", Delta: 2},
		{ID: "player-a:item-b", Category: "items", Kind: "item_lost", ActorType: "player", ActorID: "player-a", Delta: -1},
		{ID: "pal-a:owner", Category: "pals", Kind: "pal_transferred", ActorType: "world", ActorID: "world", TargetType: "player", TargetID: "player-a"},
		{ID: "pal-b:progress", Category: "pals", Kind: "pal_progressed", ActorType: "player", ActorID: "player-a"},
		{ID: "base-a:structures", Category: "bases", Kind: "base_structures_added", ActorType: "base", ActorID: "base-a"},
	}

	summary := summarizeTrustedSaveHistoryEvents(events)
	if summary.ItemsIncreased != 1 || summary.ItemsDecreased != 1 {
		t.Fatalf("item summary = %#v", summary)
	}
	if summary.PalsAdded != 1 || summary.PalsChanged != 1 {
		t.Fatalf("pal summary = %#v", summary)
	}
	if summary.BasesChanged != 1 {
		t.Fatalf("base summary = %#v", summary)
	}
	if summary.ContainersAdded != 0 || summary.ContainersRemoved != 0 || summary.ContainersChanged != 0 {
		t.Fatalf("container noise leaked into semantic summary: %#v", summary)
	}
}

func TestPalDefenderCaptureEvidenceConfirmsOwnedPal(t *testing.T) {
	event := saveindex.HistoryEvent{
		ID:           "pal-a:owner",
		Category:     "pals",
		Kind:         "pal_transferred",
		ActorType:    "world",
		ActorID:      "world",
		TargetType:   "player",
		TargetID:     "ABCDEF",
		TargetLabel:  "tiantian",
		SubjectType:  "pal",
		SubjectID:    "pal-a",
		SubjectLabel: "Anubis",
		Metadata:     map[string]string{"character_id": "Anubis"},
		Inferred:     true,
	}
	record := gameevents.Record{
		EventID:    "pdlog-1",
		Type:       "PAL_CAPTURED",
		PlayerUID:  "ab-cd-ef",
		Nickname:   "tiantian",
		OccurredAt: "2026-08-06T01:00:00Z",
		Status:     "completed",
		Payload:    map[string]any{"pal_id": "Anubis", "pal_name": "阿努比斯"},
	}

	if score := saveHistoryEvidenceScore(event, record); score < 100 {
		t.Fatalf("score = %d", score)
	}
	attachSaveHistoryEvidenceRecord(&event, record)
	if event.Inferred {
		t.Fatal("capture evidence should mark event as confirmed")
	}
	if event.Metadata["confidence"] != "confirmed" || event.Metadata["evidence_source"] != "paldefender_log" {
		t.Fatalf("metadata = %#v", event.Metadata)
	}
	if len(event.Details) != 1 || event.Details[0].Field != "证据" {
		t.Fatalf("details = %#v", event.Details)
	}
}

func TestCraftEvidenceRequiresItemMatch(t *testing.T) {
	event := saveindex.HistoryEvent{
		Category:     "items",
		Kind:         "item_gained",
		ActorType:    "player",
		ActorID:      "player-a",
		ActorLabel:   "Alice",
		SubjectID:    "Sphere_Ultimate",
		SubjectLabel: "Sphere_Ultimate",
		Metadata:     map[string]string{"item_id": "Sphere_Ultimate"},
	}
	matching := gameevents.Record{
		Type:      "ITEM_CRAFTED",
		PlayerUID: "player-a",
		Status:    "completed",
		Payload:   map[string]any{"item_name": "Sphere_Ultimate"},
	}
	other := matching
	other.Payload = map[string]any{"item_name": "Arrow"}
	if saveHistoryEvidenceScore(event, matching) == 0 {
		t.Fatal("matching craft event was not correlated")
	}
	if saveHistoryEvidenceScore(event, other) != 0 {
		t.Fatal("unrelated craft event was correlated")
	}
}

func TestSemanticSummaryKeepsDistinctPlayerIdentityPrefixes(t *testing.T) {
	events := []saveindex.HistoryEvent{
		{ID: "uid:aaaaaaaa:level", Category: "players", Kind: "player_level_up"},
		{ID: "uid:bbbbbbbb:guild", Category: "players", Kind: "player_joined_guild"},
	}
	summary := summarizeTrustedSaveHistoryEvents(events)
	if summary.PlayersChanged != 2 {
		t.Fatalf("players_changed = %d, want 2", summary.PlayersChanged)
	}
}

func TestWorldRefreshVolumeCannotInflateTrustedSummary(t *testing.T) {
	events := make([]saveindex.HistoryEvent, 0, 291)
	for index := 0; index < 290; index++ {
		events = append(events, saveindex.HistoryEvent{
			ID:        "world-item",
			Category:  "items",
			Kind:      "item_gained",
			ActorType: "unknown",
			ActorID:   "unknown",
			Delta:     1,
		})
	}
	events = append(events, saveindex.HistoryEvent{
		ID:        "player-a:Gold",
		Category:  "items",
		Kind:      "item_gained",
		ActorType: "player",
		ActorID:   "player-a",
		Delta:     845,
	})

	trusted := filterTrustedSaveHistoryEvents(events)
	if len(trusted) != 1 {
		t.Fatalf("trusted = %d, want 1", len(trusted))
	}
	summary := summarizeTrustedSaveHistoryEvents(trusted)
	if summary.ItemsIncreased != 1 || summary.ItemsDecreased != 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestCaptureEvidenceDoesNotExplainPlayerToPlayerTransfer(t *testing.T) {
	event := saveindex.HistoryEvent{
		Category:    "pals",
		Kind:        "pal_transferred",
		ActorType:   "player",
		ActorID:     "player-a",
		TargetType:  "player",
		TargetID:    "player-b",
		TargetLabel: "Bob",
		Metadata:    map[string]string{"character_id": "Anubis"},
	}
	record := gameevents.Record{
		Type:      "PAL_CAPTURED",
		PlayerUID: "player-b",
		Status:    "completed",
		Payload:   map[string]any{"pal_id": "Anubis"},
	}
	if score := saveHistoryEvidenceScore(event, record); score != 0 {
		t.Fatalf("score = %d, want 0", score)
	}
}
