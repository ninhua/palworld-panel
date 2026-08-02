package gameevents

import "testing"

func TestParseBridgeDeadLetterID(t *testing.T) {
	id, err := ParseBridgeDeadLetterID("42")
	if err != nil || id != 42 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	for _, value := range []string{"", "0", "-1", "abc"} {
		if _, err := ParseBridgeDeadLetterID(value); err == nil {
			t.Fatalf("expected error for %q", value)
		}
	}
}

func TestNormalizeBridgePagination(t *testing.T) {
	limit, offset := normalizeBridgePagination(0, -1)
	if limit != 50 || offset != 0 {
		t.Fatalf("got limit=%d offset=%d", limit, offset)
	}
	limit, offset = normalizeBridgePagination(1000, 12)
	if limit != 500 || offset != 12 {
		t.Fatalf("got limit=%d offset=%d", limit, offset)
	}
}
