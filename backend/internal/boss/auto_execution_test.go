package boss

import (
	"context"
	"database/sql"
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

func TestAutoExecutionLeaseTimestampIsFixedWidthUTC(t *testing.T) {
	first := autoExecutionLeaseTimestamp(time.Date(2026, 8, 3, 0, 0, 0, 0, time.FixedZone("SGT", 8*60*60)))
	second := autoExecutionLeaseTimestamp(time.Date(2026, 8, 3, 0, 0, 0, 1, time.FixedZone("SGT", 8*60*60)))
	if len(first) != len(second) {
		t.Fatalf("lease timestamps have different widths: %q %q", first, second)
	}
	if first >= second {
		t.Fatalf("lease timestamps are not lexically ordered: %q >= %q", first, second)
	}
	if first != "2026-08-02T16:00:00.000000000Z" {
		t.Fatalf("unexpected UTC timestamp: %q", first)
	}
}

func TestAutoExecutionLeaseIsExclusiveAndExpires(t *testing.T) {
	database, err := sql.Open("sqlite", "file:boss-auto-lease-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	service := &Service{db: database, now: time.Now}
	ctx := context.Background()
	now := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)

	acquired, err := service.acquireAutoExecutionLease(ctx, "worker-a", now)
	if err != nil || !acquired {
		t.Fatalf("worker-a acquire = %v, %v", acquired, err)
	}
	acquired, err = service.acquireAutoExecutionLease(ctx, "worker-b", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("worker-b acquired an unexpired lease")
	}
	acquired, err = service.acquireAutoExecutionLease(ctx, "worker-b", now.Add(autoExecutionLeaseDuration+time.Nanosecond))
	if err != nil || !acquired {
		t.Fatalf("worker-b expiry acquire = %v, %v", acquired, err)
	}
	if err := service.releaseAutoExecutionLease(ctx, "worker-a"); err != nil {
		t.Fatal(err)
	}
	acquired, err = service.acquireAutoExecutionLease(ctx, "worker-a", now.Add(autoExecutionLeaseDuration+time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("stale holder release removed the current holder lease")
	}
	if err := service.releaseAutoExecutionLease(ctx, "worker-b"); err != nil {
		t.Fatal(err)
	}
	acquired, err = service.acquireAutoExecutionLease(ctx, "worker-a", now.Add(autoExecutionLeaseDuration+2*time.Second))
	if err != nil || !acquired {
		t.Fatalf("worker-a reacquire = %v, %v", acquired, err)
	}
}
