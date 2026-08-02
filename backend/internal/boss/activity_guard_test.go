package boss

import (
	"errors"
	"testing"
	"time"
)

func TestAutoExecutionPlanKeyPriority(t *testing.T) {
	cases := []struct {
		name   string
		summon Summon
		want   string
	}{
		{
			name: "explicit key",
			summon: Summon{TemplateID: "template-a", Metadata: map[string]any{
				"auto_execute_plan_key": " Weekend-Raid ",
				"schedule_id":           "schedule-ignored",
			}},
			want: "plan:weekend-raid",
		},
		{
			name: "schedule key",
			summon: Summon{TemplateID: "template-a", Metadata: map[string]any{
				"schedule_id": "Schedule-42",
			}},
			want: "schedule:schedule-42",
		},
		{
			name:   "request fallback ignores template",
			summon: Summon{TemplateID: "Template-A", RequestKey: "request-1", Metadata: map[string]any{}},
			want:   "request:request-1",
		},
		{
			name:   "request fallback",
			summon: Summon{RequestKey: "Request-1", Metadata: map[string]any{}},
			want:   "request:request-1",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := autoExecutionPlanKey(test.summon); got != test.want {
				t.Fatalf("autoExecutionPlanKey() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRetainAutoExecutionActivityGuard(t *testing.T) {
	enabled := map[string]any{"auto_execute": true}
	paused := map[string]any{"auto_execute": true, "auto_execute_paused": true}
	for _, status := range []string{SummonStatusPending, SummonStatusActive} {
		if !retainAutoExecutionActivityGuard(status, enabled) {
			t.Fatalf("status %q should retain enabled guard", status)
		}
		if !retainAutoExecutionActivityGuard(status, paused) {
			t.Fatalf("status %q should retain paused guard", status)
		}
	}
	for _, status := range []string{SummonStatusCompleted, SummonStatusFailed, SummonStatusCancelled} {
		if retainAutoExecutionActivityGuard(status, enabled) {
			t.Fatalf("terminal status %q retained guard", status)
		}
	}
	if !retainAutoExecutionActivityGuard(SummonStatusActive, map[string]any{}) {
		t.Fatal("started activity must retain guard even after automatic execution is disabled")
	}
	if retainAutoExecutionActivityGuard(SummonStatusPending, map[string]any{}) {
		t.Fatal("pending opted-out summon retained automatic guard")
	}
}

func TestAutoExecutionErrorRetainsActivityGuard(t *testing.T) {
	if !autoExecutionErrorRetainsActivityGuard(ErrExecutionReconcileRequired) {
		t.Fatal("reconciliation error must retain activity guard")
	}
	if !autoExecutionErrorRetainsActivityGuard(ErrExecutionUncertain) {
		t.Fatal("uncertain error must retain activity guard")
	}
	if !autoExecutionErrorRetainsActivityGuard(errors.Join(errors.New("wrapped"), ErrExecutionUncertain)) {
		t.Fatal("wrapped uncertain error must retain activity guard")
	}
	if autoExecutionErrorRetainsActivityGuard(ErrExecutionUnavailable) {
		t.Fatal("adapter unavailable must release activity guard")
	}
	if autoExecutionErrorRetainsActivityGuard(ErrExecutionFailed) {
		t.Fatal("explicit failed execution must release activity guard")
	}
}

func TestRetainManualPendingActivityClaimTTL(t *testing.T) {
	now := time.Date(2026, 8, 3, 2, 40, 0, 0, time.UTC)
	recent := now.Add(-10 * time.Second).Format(time.RFC3339Nano)
	stale := now.Add(-activityGuardPendingClaimTTL - time.Second).Format(time.RFC3339Nano)
	if !retainAutoExecutionActivityGuardAt(SummonStatusPending, map[string]any{}, recent, now) {
		t.Fatal("recent manual pending claim was released inside the transition window")
	}
	if retainAutoExecutionActivityGuardAt(SummonStatusPending, map[string]any{}, stale, now) {
		t.Fatal("stale manual pending claim was not released")
	}
	if !retainAutoExecutionActivityGuardAt(SummonStatusPending, map[string]any{"auto_execute": true}, stale, now) {
		t.Fatal("automatic pending owner must survive restart regardless of claim age")
	}
}
