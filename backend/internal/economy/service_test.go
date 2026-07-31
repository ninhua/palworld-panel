package economy

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	service, err := Open(filepath.Join(t.TempDir(), "palpanel.db"), "Asia/Shanghai")
	if err != nil {
		t.Fatalf("open economy service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return service
}

func TestCheckinIsIdempotentByPlayerAndLocalDate(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	first, err := service.Checkin(ctx, "player-1", "Tester", "steam-1", "2026-07-31", 10, "test")
	if err != nil {
		t.Fatalf("first checkin: %v", err)
	}
	if !first.Awarded || first.Account.Balance != 10 {
		t.Fatalf("unexpected first checkin: %+v", first)
	}
	second, err := service.Checkin(ctx, "player-1", "Tester", "steam-1", "2026-07-31", 10, "test")
	if err != nil {
		t.Fatalf("second checkin: %v", err)
	}
	if second.Awarded || second.Account.Balance != 10 {
		t.Fatalf("duplicate checkin changed balance: %+v", second)
	}
	ledger, err := service.ListLedger(ctx, "player-1", 10, 0)
	if err != nil {
		t.Fatalf("list ledger: %v", err)
	}
	if len(ledger) != 1 || ledger[0].Delta != 10 || ledger[0].BalanceAfter != 10 {
		t.Fatalf("unexpected ledger: %+v", ledger)
	}
}

func TestAdjustmentReferenceIsIdempotent(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	request := Adjustment{
		PlayerUID:     "player-2",
		Delta:         100,
		Reason:        "admin_grant",
		ReferenceType: "manual",
		ReferenceID:   "grant-001",
		Actor:         "tester",
	}
	first, err := service.Adjust(ctx, request)
	if err != nil {
		t.Fatalf("first adjustment: %v", err)
	}
	second, err := service.Adjust(ctx, request)
	if err != nil {
		t.Fatalf("second adjustment: %v", err)
	}
	if first.Account.Balance != 100 || second.Account.Balance != 100 || !second.AlreadyApplied {
		t.Fatalf("idempotency failed: first=%+v second=%+v", first, second)
	}
	_, err = service.Adjust(ctx, Adjustment{PlayerUID: "player-2", Delta: -101, Reason: "overspend"})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("expected insufficient balance, got %v", err)
	}
}

func TestReservationCommitAndReleaseAreSafeToRetry(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, err := service.Adjust(ctx, Adjustment{PlayerUID: "player-3", Delta: 200, Reason: "seed"}); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	reserved, err := service.Reserve(ctx, "player-3", "", "", "order-1", 50, time.Minute, "shop")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if reserved.Account.Balance != 150 || reserved.Reservation.Status != "reserved" {
		t.Fatalf("unexpected reservation: %+v", reserved)
	}
	committed, err := service.CommitReservation(ctx, reserved.Reservation.ID, "shop")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if committed.Account.Balance != 150 || committed.Reservation.Status != "committed" {
		t.Fatalf("unexpected commit: %+v", committed)
	}
	retried, err := service.CommitReservation(ctx, reserved.Reservation.ID, "shop")
	if err != nil || !retried.Existing || retried.Account.Balance != 150 {
		t.Fatalf("commit retry is not idempotent: result=%+v err=%v", retried, err)
	}

	second, err := service.Reserve(ctx, "player-3", "", "", "order-2", 40, time.Minute, "shop")
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	released, err := service.ReleaseReservation(ctx, second.Reservation.ID, "shop")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released.Account.Balance != 150 || released.Reservation.Status != "released" {
		t.Fatalf("unexpected release: %+v", released)
	}
	releaseRetry, err := service.ReleaseReservation(ctx, second.Reservation.ID, "shop")
	if err != nil || !releaseRetry.Existing || releaseRetry.Account.Balance != 150 {
		t.Fatalf("release retry is not idempotent: result=%+v err=%v", releaseRetry, err)
	}
}

func TestExpiredReservationReturnsPoints(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	clock := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return clock }
	if _, err := service.Adjust(ctx, Adjustment{PlayerUID: "player-4", Delta: 80, Reason: "seed"}); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	reservation, err := service.Reserve(ctx, "player-4", "", "", "order-expire", 30, time.Minute, "shop")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	clock = clock.Add(2 * time.Minute)
	count, err := service.ReleaseExpired(ctx)
	if err != nil {
		t.Fatalf("release expired: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one expired reservation, got %d", count)
	}
	account, err := service.GetAccount(ctx, "player-4")
	if err != nil {
		t.Fatalf("read account: %v", err)
	}
	if account.Balance != 80 {
		t.Fatalf("expired reservation did not restore points: %+v, reservation=%+v", account, reservation)
	}
}

func TestGameCommandEventIsIdempotent(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	request := CommandRequest{
		EventID:   "event-001",
		PlayerUID: "player-5",
		Nickname:  "Tester",
		Message:   "!签到",
		LocalDate: "2026-07-31",
		Points:    12,
	}
	first, err := service.ExecuteCommand(ctx, request)
	if err != nil {
		t.Fatalf("execute command: %v", err)
	}
	second, err := service.ExecuteCommand(ctx, request)
	if err != nil {
		t.Fatalf("repeat command: %v", err)
	}
	if !first.Handled || !first.Awarded || first.Balance != 12 {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if !second.Duplicate || second.Balance != 12 {
		t.Fatalf("duplicate command was not deduplicated: %+v", second)
	}
}

func TestParseCommandUsesConfiguredPrefix(t *testing.T) {
	if command, handled := parseCommand("#签到", "#"); !handled || command != "checkin" {
		t.Fatalf("custom prefix was not accepted: command=%q handled=%t", command, handled)
	}
	if command, handled := parseCommand("!签到", "#"); handled || command != "" {
		t.Fatalf("old prefix should not match: command=%q handled=%t", command, handled)
	}
	if command, handled := parseCommand("#积分 额外参数", "#"); !handled || command != "points" {
		t.Fatalf("custom prefix points command failed: command=%q handled=%t", command, handled)
	}
}

func TestParseCommandAllowsEmptyPrefixWithoutCapturingChat(t *testing.T) {
	if command, handled := parseCommand("签到", ""); !handled || command != "checkin" {
		t.Fatalf("prefix-free checkin failed: command=%q handled=%t", command, handled)
	}
	if command, handled := parseCommand("积分", ""); !handled || command != "points" {
		t.Fatalf("prefix-free points failed: command=%q handled=%t", command, handled)
	}
	if command, handled := parseCommand("今天一起打 Boss 吧", ""); handled || command != "" {
		t.Fatalf("ordinary chat was captured as a command: command=%q handled=%t", command, handled)
	}
}

func TestValidateCommandPrefix(t *testing.T) {
	for _, prefix := range []string{"", "!", "##", "指令:", "／"} {
		if err := validateCommandPrefix(prefix); err != nil {
			t.Fatalf("valid prefix %q rejected: %v", prefix, err)
		}
	}
	for _, prefix := range []string{"two words", "\n", "12345678901234567"} {
		if err := validateCommandPrefix(prefix); !errors.Is(err, ErrInvalidCommandPrefix) {
			t.Fatalf("invalid prefix %q accepted: %v", prefix, err)
		}
	}
}
