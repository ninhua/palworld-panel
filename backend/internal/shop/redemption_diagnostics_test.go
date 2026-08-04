package shop

import (
	"errors"
	"strings"
	"testing"

	"palpanel/internal/economy"
)

func TestFormatRedemptionFailureIncludesBalanceIdentityAndDiagnosticID(t *testing.T) {
	attempt := RedemptionAttempt{
		ID: "redemption-test", RequestedPlayerUID: "UID-A", ResolvedPlayerUID: "UID-B", AccountMatch: "steam_id_fallback",
		ProductName: "测试商品", Quantity: 1, UnitPrice: 15, RequiredPoints: 15, AvailablePoints: 10, ReservedPoints: 90,
	}
	reply := formatRedemptionFailure(attempt, economy.ErrInsufficientBalance, "ABCDEF12")
	for _, expected := range []string{"redemption-test", "需要15积分", "可用10积分", "预留", "UID-A", "UID-B", "steam_id_fallback"} {
		if !strings.Contains(reply, expected) {
			t.Fatalf("reply missing %q: %s", expected, reply)
		}
	}
	if redemptionFailureCode(errors.New("other")) != "operation_failed" {
		t.Fatal("unexpected fallback failure code")
	}
}
