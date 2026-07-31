package shop

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"palpanel/internal/economy"
)

func TestOrderLifecycleIsIdempotentAndRestoresCancelledStock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "palpanel.db")
	ledger, err := economy.Open(path, "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	service, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	player := "00112233445566778899aabbccddeeff"
	if _, err := ledger.Adjust(ctx, economy.Adjustment{PlayerUID: player, Delta: 100, Reason: "test", ReferenceType: "test", ReferenceID: "seed", Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	product, err := service.CreateProduct(ctx, ProductInput{Name: "测试商品", Price: 20, Stock: 5, PerPlayerLimit: 4, Enabled: true, DeliveryMode: "manual"})
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.CreateOrder(ctx, ledger, CreateOrderRequest{IdempotencyKey: "order-1", ProductID: product.ID, PlayerUID: player, Quantity: 2}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if first.Order.Status != "pending" || first.Account.Balance != 60 {
		t.Fatalf("unexpected first order: %+v", first)
	}
	duplicate, err := service.CreateOrder(ctx, ledger, CreateOrderRequest{IdempotencyKey: "order-1", ProductID: product.ID, PlayerUID: player, Quantity: 2}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.Duplicate || duplicate.Order.ID != first.Order.ID || duplicate.Account.Balance != 60 {
		t.Fatalf("duplicate was not idempotent: %+v", duplicate)
	}
	current, _ := service.GetProduct(ctx, product.ID)
	if current.Stock != 3 {
		t.Fatalf("stock=%d want 3", current.Stock)
	}

	completed, err := service.CompleteOrder(ctx, ledger, first.Order.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Order.Status != "delivered" || completed.Account.Balance != 60 {
		t.Fatalf("unexpected completion: %+v", completed)
	}

	second, err := service.CreateOrder(ctx, ledger, CreateOrderRequest{IdempotencyKey: "order-2", ProductID: product.ID, PlayerUID: player, Quantity: 1}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if second.Account.Balance != 40 {
		t.Fatalf("balance=%d want 40", second.Account.Balance)
	}
	cancelled, err := service.CancelOrder(ctx, ledger, second.Order.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Order.Status != "cancelled" || cancelled.Account.Balance != 60 {
		t.Fatalf("unexpected cancellation: %+v", cancelled)
	}
	current, _ = service.GetProduct(ctx, product.ID)
	if current.Stock != 3 {
		t.Fatalf("stock=%d want 3 after cancellation", current.Stock)
	}
}

func TestPlayerLimitRejectsAdditionalReservedQuantity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "palpanel.db")
	ledger, err := economy.Open(path, "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	service, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	player := "ffeeddccbbaa99887766554433221100"
	if _, err := ledger.Adjust(ctx, economy.Adjustment{PlayerUID: player, Delta: 1000, Reason: "test", ReferenceType: "test", ReferenceID: "seed", Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	product, err := service.CreateProduct(ctx, ProductInput{Name: "限购商品", Price: 10, Stock: -1, PerPlayerLimit: 2, Enabled: true, DeliveryMode: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateOrder(ctx, ledger, CreateOrderRequest{IdempotencyKey: "limit-1", ProductID: product.ID, PlayerUID: player, Quantity: 2}, "test"); err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateOrder(ctx, ledger, CreateOrderRequest{IdempotencyKey: "limit-2", ProductID: product.ID, PlayerUID: player, Quantity: 1}, "test")
	if !errors.Is(err, ErrPlayerLimit) {
		t.Fatalf("err=%v want ErrPlayerLimit", err)
	}
	account, _ := ledger.GetAccount(ctx, player)
	if account.Balance != 980 {
		t.Fatalf("balance=%d want 980; failed order must be refunded", account.Balance)
	}
}
