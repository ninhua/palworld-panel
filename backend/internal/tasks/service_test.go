package tasks

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestNormalizeDefinition(t *testing.T) {
	input, filters, err := normalizeDefinition(DefinitionInput{
		Name: " 捕获棉悠悠 ", EventType: "pal_captured", TargetAmount: 3,
		RewardPoints: 30, Cycle: "DAILY", AmountField: "count",
		Filters: map[string]any{"pal_id": "SheepBall"}, Enabled: true,
	})
	if err != nil {
		t.Fatalf("normalizeDefinition returned error: %v", err)
	}
	if input.Name != "捕获棉悠悠" || input.EventType != "PAL_CAPTURED" || input.Cycle != "daily" {
		t.Fatalf("unexpected normalized input: %#v", input)
	}
	if filters != `{"pal_id":"SheepBall"}` {
		t.Fatalf("filters json = %s", filters)
	}
}

func TestNormalizeDefinitionRejectsInvalidValues(t *testing.T) {
	cases := []DefinitionInput{
		{Name: "", EventType: "PAL_CAPTURED", TargetAmount: 1, Cycle: "daily"},
		{Name: "x", EventType: "bad event", TargetAmount: 1, Cycle: "daily"},
		{Name: "x", EventType: "PAL_CAPTURED", TargetAmount: 0, Cycle: "daily"},
		{Name: "x", EventType: "PAL_CAPTURED", TargetAmount: 1, RewardPoints: -1, Cycle: "daily"},
		{Name: "x", EventType: "PAL_CAPTURED", TargetAmount: 1, Cycle: "monthly"},
		{Name: "x", EventType: "PAL_CAPTURED", TargetAmount: 1, Cycle: "daily", AmountField: "bad field"},
		{Name: "x", EventType: "PAL_CAPTURED", TargetAmount: 1, Cycle: "daily", Filters: map[string]any{"pal": map[string]any{"id": "x"}}},
	}
	for index, input := range cases {
		if _, _, err := normalizeDefinition(input); !errors.Is(err, ErrInvalidDefinition) {
			t.Fatalf("case %d expected ErrInvalidDefinition, got %v", index, err)
		}
	}
}

func TestEventAmountAndFilters(t *testing.T) {
	payload := map[string]any{
		"count":  float64(4),
		"pal_id": "SheepBall",
		"boss": map[string]any{
			"id": "BOSS_001",
		},
	}
	if amount, ok := eventAmount(payload, "count"); !ok || amount != 4 {
		t.Fatalf("eventAmount = %d, %v", amount, ok)
	}
	if !matchesFilters(payload, map[string]any{"pal_id": "SheepBall", "boss.id": "BOSS_001"}) {
		t.Fatal("expected nested filters to match")
	}
	if matchesFilters(payload, map[string]any{"pal_id": "Lamball"}) {
		t.Fatal("unexpected filter match")
	}
	payload["fraction"] = 1.5
	if _, ok := eventAmount(payload, "fraction"); ok {
		t.Fatal("fractional event amount should be rejected")
	}
	payload["number"] = json.Number("7")
	if amount, ok := eventAmount(payload, "number"); !ok || amount != 7 {
		t.Fatalf("json number amount = %d, %v", amount, ok)
	}
}

func TestCycleKeys(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{location: location}
	instant := time.Date(2026, 7, 29, 18, 30, 0, 0, time.UTC) // Thursday in Shanghai.
	if got := service.cycleKey("daily", instant); got != "2026-07-30" {
		t.Fatalf("daily cycle = %s", got)
	}
	if got := service.cycleKey("weekly", instant); got != "2026-07-27" {
		t.Fatalf("weekly cycle = %s", got)
	}
	if got := service.cycleKey("once", instant); got != "once" {
		t.Fatalf("once cycle = %s", got)
	}
}
