package boss

import (
	"testing"
	"time"
)

func TestNormalizeAutoExecutionRetryAction(t *testing.T) {
	for _, input := range []string{"retry_failed", "retry-failed", "retry", "retry_wave", "retry_failed_wave"} {
		actual, ok := normalizeAutoExecutionControlAction(input)
		if !ok || actual != AutoExecutionControlRetryFailed {
			t.Fatalf("normalize %q = %q, %v; want %q, true", input, actual, ok, AutoExecutionControlRetryFailed)
		}
	}
}

func TestAutoExecutionRetryDelay(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]any
		want     time.Duration
	}{
		{name: "default", metadata: map[string]any{}, want: 5 * time.Second},
		{name: "number", metadata: map[string]any{"auto_execute_retry_delay_seconds": float64(12)}, want: 12 * time.Second},
		{name: "string", metadata: map[string]any{"auto_execute_retry_delay_seconds": "30"}, want: 30 * time.Second},
		{name: "negative", metadata: map[string]any{"auto_execute_retry_delay_seconds": -1}, want: 0},
		{name: "clamped", metadata: map[string]any{"auto_execute_retry_delay_seconds": 99999}, want: time.Hour},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if actual := autoExecutionRetryDelay(test.metadata); actual != test.want {
				t.Fatalf("retry delay = %s; want %s", actual, test.want)
			}
		})
	}
}

func TestAutoExecutionMetadataInt(t *testing.T) {
	metadata := map[string]any{
		"float":  float64(3),
		"int":    4,
		"int64":  int64(5),
		"string": "6",
	}
	for key, want := range map[string]int64{"float": 3, "int": 4, "int64": 5, "string": 6, "missing": 0} {
		if actual := autoExecutionMetadataInt(metadata, key); actual != want {
			t.Fatalf("metadata int %q = %d; want %d", key, actual, want)
		}
	}
}
