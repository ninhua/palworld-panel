package tasks

import (
	"context"
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

func TestDiagnoseEventExplainsMatchesAndFailures(t *testing.T) {
	service, err := Open(t.TempDir()+"/tasks.db", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.now = func() time.Time { return time.Date(2026, 8, 2, 1, 0, 0, 0, time.UTC) }
	ctx := context.Background()
	_, err = service.CreateDefinition(ctx, DefinitionInput{
		Name: "捕获棉悠悠", EventType: "PAL_CAPTURED", TargetAmount: 3, RewardPoints: 0,
		Cycle: "daily", AmountField: "count", Filters: map[string]any{"pal_id": "SheepBall"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateDefinition(ctx, DefinitionInput{
		Name: "登录任务", EventType: "PLAYER_LOGIN", TargetAmount: 1, RewardPoints: 0,
		Cycle: "daily", AmountField: "count", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	report, err := service.DiagnoseEvent(ctx, Event{
		EventID: "diagnostic-1", Type: "PAL_CAPTURED", PlayerUID: "player-1",
		Payload: map[string]any{"pal_id": "SheepBall", "count": float64(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Matched != 1 || report.WouldApply != 1 {
		t.Fatalf("unexpected report counts: %#v", report)
	}
	var captured, login DiagnosticTaskMatch
	for _, item := range report.Results {
		switch item.TaskName {
		case "捕获棉悠悠":
			captured = item
		case "登录任务":
			login = item
		}
	}
	if captured.Status != "would_apply" || captured.WouldAdd != 2 {
		t.Fatalf("unexpected matched result: %#v", captured)
	}
	if login.Status != "event_type_mismatch" {
		t.Fatalf("unexpected mismatch result: %#v", login)
	}

	missing, err := service.DiagnoseEvent(ctx, Event{Type: "PAL_CAPTURED", PlayerUID: "player-1", Payload: map[string]any{"count": 1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range missing.Results {
		if item.TaskName == "捕获棉悠悠" && (item.Status != "filter_field_missing" || item.Field != "pal_id") {
			t.Fatalf("unexpected missing filter result: %#v", item)
		}
	}
}

func TestDiagnoseFiltersExplainsMissingAndMismatch(t *testing.T) {
	payload := map[string]any{"pal_id": "SheepBall", "count": 1}
	path, expected, actual, status, _, matched := diagnoseFilters(payload, map[string]any{"boss.id": "BOSS_001"})
	if matched || status != "filter_field_missing" || path != "boss.id" || expected != "BOSS_001" || actual != nil {
		t.Fatalf("unexpected missing diagnostic: path=%s expected=%v actual=%v status=%s matched=%v", path, expected, actual, status, matched)
	}
	path, expected, actual, status, _, matched = diagnoseFilters(payload, map[string]any{"pal_id": "BerryGoat"})
	if matched || status != "filter_mismatch" || path != "pal_id" || expected != "BerryGoat" || actual != "SheepBall" {
		t.Fatalf("unexpected mismatch diagnostic: path=%s expected=%v actual=%v status=%s matched=%v", path, expected, actual, status, matched)
	}
}
