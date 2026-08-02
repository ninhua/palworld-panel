package boss

import (
	"testing"
	"time"
)

func TestAutoExecutionPolicyFromMetadata(t *testing.T) {
	notBefore := "2026-08-03T01:02:03+08:00"
	policy := autoExecutionPolicyFromMetadata(map[string]any{
		"auto_execute":            true,
		"auto_execute_paused":     "false",
		"auto_execute_not_before": notBefore,
	})
	if !policy.Enabled || policy.Paused {
		t.Fatalf("unexpected policy: %+v", policy)
	}
	if policy.NotBefore.Format(time.RFC3339) != notBefore {
		t.Fatalf("not before = %s", policy.NotBefore.Format(time.RFC3339))
	}
}

func TestAutoExecutionDueAtUsesWaveDelay(t *testing.T) {
	summon := Summon{RequestedAt: "2026-08-02T23:00:00+08:00"}
	waves := []SummonWave{
		{Position: 1, Status: WaveStatusCompleted, CompletedAt: "2026-08-02T23:01:00+08:00"},
		{Position: 2, Status: WaveStatusPending, DelaySeconds: 45},
	}
	due, runnable := autoExecutionDueAt(summon, waves)
	if !runnable {
		t.Fatal("expected wave to be runnable")
	}
	want := "2026-08-02T23:01:45+08:00"
	if due.Format(time.RFC3339) != want {
		t.Fatalf("due = %s, want %s", due.Format(time.RFC3339), want)
	}
}

func TestAutoExecutionDueAtBlocksActiveOrFailedWave(t *testing.T) {
	for _, status := range []string{WaveStatusActive, WaveStatusFailed} {
		_, runnable := autoExecutionDueAt(
			Summon{RequestedAt: "2026-08-02T23:00:00+08:00"},
			[]SummonWave{{Position: 1, Status: status}, {Position: 2, Status: WaveStatusPending}},
		)
		if runnable {
			t.Fatalf("status %s must block automatic continuation", status)
		}
	}
}

func TestMetadataBoolAcceptsExplicitRepresentations(t *testing.T) {
	cases := []any{true, "true", float64(1), int(1), int64(1)}
	for _, value := range cases {
		if !metadataBool(map[string]any{"enabled": value}, "enabled") {
			t.Fatalf("value %#v was not accepted", value)
		}
	}
	if metadataBool(map[string]any{"enabled": "yes"}, "enabled") {
		t.Fatal("ambiguous value must not enable automatic execution")
	}
}
