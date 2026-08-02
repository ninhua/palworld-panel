package shop

import (
	"strings"
	"testing"
)

func TestPublicProductCode(t *testing.T) {
	if got := PublicProductCode("product_0123456789abcdef"); got != "01234567" {
		t.Fatalf("unexpected code %q", got)
	}
}

func TestParsePlayerShopCommand(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		prefix    string
		allowBare bool
		command   string
		args      string
		handled   bool
	}{
		{name: "prefixed catalog", message: "!商城 2", prefix: "!", command: "catalog", args: "2", handled: true},
		{name: "bare disabled", message: "商城", prefix: "!", handled: false},
		{name: "bare enabled", message: "商城 高级球", prefix: "!", allowBare: true, command: "catalog", args: "高级球", handled: true},
		{name: "orders", message: "我的订单 3", prefix: "", command: "orders", args: "3", handled: true},
		{name: "redeem", message: "兑换 ABCD1234 2", prefix: "", command: "redeem", args: "ABCD1234 2", handled: true},
		{name: "normal chat", message: "今天商城真热闹", prefix: "", handled: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, args, handled := parsePlayerShopCommand(test.message, test.prefix, test.allowBare)
			if command != test.command || args != test.args || handled != test.handled {
				t.Fatalf("got command=%q args=%q handled=%v", command, args, handled)
			}
		})
	}
}

func TestParseRedeemArgumentsSupportsProductNames(t *testing.T) {
	selector, quantity, ok := parseRedeemArguments("高级 帕鲁球 3")
	if !ok || selector != "高级 帕鲁球" || quantity != 3 {
		t.Fatalf("got selector=%q quantity=%d ok=%v", selector, quantity, ok)
	}
	selector, quantity, ok = parseRedeemArguments("高级帕鲁球")
	if !ok || selector != "高级帕鲁球" || quantity != 1 {
		t.Fatalf("default quantity failed")
	}
}

func TestFormatPlayerCatalogIncludesRedeemCodes(t *testing.T) {
	result := formatPlayerCatalog(PlayerCatalogResult{
		Balance: 90,
		Total:   1,
		Limit:   5,
		Items: []PlayerCatalogItem{{
			Code: "ABCDEF12", Name: "高级帕鲁球", Price: 20, Stock: -1, RemainingLimit: 3, Available: true,
		}},
	}, 1)
	for _, expected := range []string{"ABCDEF12", "高级帕鲁球", "20积分", "当前积分：90", "兑换 <兑换码>"} {
		if !strings.Contains(result, expected) {
			t.Fatalf("catalog reply missing %q: %s", expected, result)
		}
	}
}

func TestFormatPurchaseReplyWarnsUncertainDelivery(t *testing.T) {
	reply := formatPurchaseReply(Order{
		ID: "order_1", ProductName: "礼包", Quantity: 1, TotalPoints: 10,
		Status: "pending", DeliveryMode: DeliveryModePalDefenderItems, DeliveryState: DeliveryStateProcessing,
	}, 40, "AABBCCDD")
	if !strings.Contains(reply, "请勿重复兑换") || !strings.Contains(reply, "当前积分：40") {
		t.Fatalf("unexpected reply: %s", reply)
	}
}

func TestPlayerOrderIdempotencyKeyIsBoundedAndStable(t *testing.T) {
	long := strings.Repeat("event-", 40)
	first := playerOrderIdempotencyKey(long)
	second := playerOrderIdempotencyKey(long)
	if first != second || len(first) > 128 || !strings.HasPrefix(first, "game-shop:") {
		t.Fatalf("unexpected key %q", first)
	}
}
