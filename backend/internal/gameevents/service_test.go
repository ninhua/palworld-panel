package gameevents

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	service, err := Open(filepath.Join(t.TempDir(), "palpanel.db"))
	if err != nil {
		t.Fatalf("open game events: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return service
}

func TestClaimCompleteAndDuplicate(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	event := Event{EventID: "event-1", Type: "player_chat", PlayerUID: "player-1", Payload: map[string]any{"message": "!签到"}}
	claim, err := service.Claim(ctx, event)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claim.Duplicate || claim.Record.Status != "processing" || claim.Record.Type != "PLAYER_CHAT" {
		t.Fatalf("unexpected claim: %+v", claim)
	}
	completed, err := service.Complete(ctx, event.EventID, map[string]any{"handled": true})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed.Status != "completed" || completed.Result["handled"] != true {
		t.Fatalf("unexpected completed record: %+v", completed)
	}
	duplicate, err := service.Claim(ctx, event)
	if err != nil {
		t.Fatalf("duplicate claim: %v", err)
	}
	if !duplicate.Duplicate || duplicate.Retry || duplicate.Record.Status != "completed" {
		t.Fatalf("completed event was not deduplicated: %+v", duplicate)
	}
}

func TestFailedEventCanRetry(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	event := Event{EventID: "event-retry", Type: "PLAYER_CHAT", PlayerUID: "player-2", Payload: map[string]any{"message": "!积分"}}
	if _, err := service.Claim(ctx, event); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := service.Fail(ctx, event.EventID, errors.New("temporary")); err != nil {
		t.Fatalf("fail: %v", err)
	}
	retry, err := service.Claim(ctx, event)
	if err != nil {
		t.Fatalf("retry claim: %v", err)
	}
	if retry.Duplicate || !retry.Retry || retry.Record.Attempts != 2 || retry.Record.Status != "processing" {
		t.Fatalf("failed event did not enter retry: %+v", retry)
	}
}

func TestPlayerChatRequiresPlayerUID(t *testing.T) {
	service := newTestService(t)
	_, err := service.Claim(context.Background(), Event{EventID: "bad", Type: "PLAYER_CHAT", Payload: map[string]any{"message": "!签到"}})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("expected invalid event, got %v", err)
	}
}
