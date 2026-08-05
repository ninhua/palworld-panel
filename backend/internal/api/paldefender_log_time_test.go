package api

import (
	"testing"
	"time"
)

func TestPalDefenderLogOccurredAt(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	fallback := time.Date(2026, 8, 6, 2, 0, 0, 0, location)
	tests := []struct {
		line string
		want time.Time
	}{
		{"[2026-08-06 01:02:03] Player connected", time.Date(2026, 8, 6, 1, 2, 3, 0, location)},
		{"[2026.08.06-01.02.03:456][  0]LogPal: capture", time.Date(2026, 8, 6, 1, 2, 3, 456000000, location)},
		{"2026-08-05T17:02:03Z Player connected", time.Date(2026, 8, 5, 17, 2, 3, 0, time.UTC)},
		{"[2026-08-05T17:02:03Z] Player connected", time.Date(2026, 8, 5, 17, 2, 3, 0, time.UTC)},
	}
	for _, test := range tests {
		if got := palDefenderLogOccurredAt(test.line, fallback); !got.Equal(test.want) {
			t.Fatalf("palDefenderLogOccurredAt(%q) = %s, want %s", test.line, got, test.want)
		}
	}
}

func TestPalDefenderLogOccurredAtFallsBack(t *testing.T) {
	fallback := time.Date(2026, 8, 6, 2, 0, 0, 0, time.UTC)
	if got := palDefenderLogOccurredAt("no timestamp", fallback); !got.Equal(fallback) {
		t.Fatalf("got %s, want %s", got, fallback)
	}
}
