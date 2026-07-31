package shop

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

type fakeDispatcher struct {
	resolved   string
	resolveErr error
	giveErr    error
	items      []ItemGrant
	templates  []string
}

func (d *fakeDispatcher) ResolvePlayer(context.Context, []string) (string, error) {
	if d.resolveErr != nil {
		return "", d.resolveErr
	}
	if d.resolved == "" {
		return "resolved-player", nil
	}
	return d.resolved, nil
}

func (d *fakeDispatcher) GiveItems(_ context.Context, _ string, items []ItemGrant) error {
	d.items = append([]ItemGrant(nil), items...)
	return d.giveErr
}

func (d *fakeDispatcher) GivePalTemplates(_ context.Context, _ string, templates []string) error {
	d.templates = append([]string(nil), templates...)
	return d.giveErr
}

func TestAutomaticItemDeliveryCommitsReservationWithoutDuplicateGrant(t *testing.T) {
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
	player := "1234567890abcdef1234567890abcdef"
	if _, err := ledger.Adjust(ctx, economy.Adjustment{PlayerUID: player, Delta: 100, Reason: "test", ReferenceType: "test", ReferenceID: "auto-seed", Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	product, err := service.CreateProduct(ctx, ProductInput{
		Name: "自动物品", Price: 20, Stock: 5, Enabled: true, DeliveryMode: DeliveryModePalDefenderItems,
		Payload: map[string]any{"items": []any{map[string]any{"item_id": "PalSphere", "count": 10}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateOrder(ctx, ledger, CreateOrderRequest{IdempotencyKey: "auto-items", ProductID: product.ID, PlayerUID: player, Quantity: 2}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if created.Order.DeliveryState != DeliveryStatePending || created.Account.Balance != 60 {
		t.Fatalf("unexpected created order: %+v", created)
	}
	dispatcher := &fakeDispatcher{resolved: "steam-player"}
	delivered, err := service.DeliverOrder(ctx, ledger, dispatcher, created.Order.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if delivered.Order.Status != "delivered" || delivered.Order.DeliveryState != DeliveryStateSucceeded || delivered.Order.DeliveryAttempts != 1 {
		t.Fatalf("unexpected delivered order: %+v", delivered.Order)
	}
	if len(dispatcher.items) != 1 || dispatcher.items[0].Count != 20 {
		t.Fatalf("grants=%+v want one grant with count 20", dispatcher.items)
	}
	if delivered.Account.Balance != 60 {
		t.Fatalf("balance=%d want 60", delivered.Account.Balance)
	}
	second, err := service.DeliverOrder(ctx, ledger, dispatcher, created.Order.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || len(dispatcher.items) != 1 {
		t.Fatalf("repeat delivery must settle idempotently without another grant: %+v", second)
	}
}

func TestAutomaticDeliveryFailureCanRetryOrCancel(t *testing.T) {
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
	player := "abcdefabcdefabcdefabcdefabcdefab"
	if _, err := ledger.Adjust(ctx, economy.Adjustment{PlayerUID: player, Delta: 100, Reason: "test", ReferenceType: "test", ReferenceID: "failure-seed", Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	product, err := service.CreateProduct(ctx, ProductInput{
		Name: "自动帕鲁", Price: 25, Stock: 1, Enabled: true, DeliveryMode: DeliveryModePalDefenderPalTemplates,
		Payload: map[string]any{"pal_templates": []any{"starter_pal.json"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateOrder(ctx, ledger, CreateOrderRequest{IdempotencyKey: "auto-pal", ProductID: product.ID, PlayerUID: player, Quantity: 1}, "test")
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &fakeDispatcher{giveErr: errors.New("PalDefender unavailable")}
	if _, err := service.DeliverOrder(ctx, ledger, dispatcher, created.Order.ID, "test"); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("err=%v want ErrDeliveryFailed", err)
	}
	failed, err := service.GetOrder(ctx, created.Order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.DeliveryState != DeliveryStateFailed || failed.DeliveryAttempts != 1 || failed.Failure == "" {
		t.Fatalf("unexpected failed state: %+v", failed)
	}
	dispatcher.giveErr = nil
	delivered, err := service.DeliverOrder(ctx, ledger, dispatcher, created.Order.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if delivered.Order.Status != "delivered" || delivered.Order.DeliveryAttempts != 2 || len(dispatcher.templates) != 1 {
		t.Fatalf("unexpected retry result: %+v templates=%v", delivered.Order, dispatcher.templates)
	}
}

func TestProcessingDeliveryRequiresAdministratorReconciliation(t *testing.T) {
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
	player := "00110011001100110011001100110011"
	if _, err := ledger.Adjust(ctx, economy.Adjustment{PlayerUID: player, Delta: 100, Reason: "test", ReferenceType: "test", ReferenceID: "processing-seed", Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	product, err := service.CreateProduct(ctx, ProductInput{
		Name: "核对商品", Price: 10, Stock: 1, Enabled: true, DeliveryMode: DeliveryModePalDefenderItems,
		Payload: map[string]any{"items": []any{map[string]any{"item_id": "PalSphere", "count": 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateOrder(ctx, ledger, CreateOrderRequest{IdempotencyKey: "processing", ProductID: product.ID, PlayerUID: player, Quantity: 1}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.beginDelivery(ctx, created.Order.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CancelOrder(ctx, ledger, created.Order.ID, "test"); !errors.Is(err, ErrDeliveryUnsafeCancel) {
		t.Fatalf("err=%v want ErrDeliveryUnsafeCancel", err)
	}
	reset, err := service.ResetDelivery(ctx, created.Order.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if reset.DeliveryState != DeliveryStatePending {
		t.Fatalf("delivery_state=%s want pending", reset.DeliveryState)
	}
	cancelled, err := service.CancelOrder(ctx, ledger, created.Order.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Order.Status != "cancelled" || cancelled.Account.Balance != 100 {
		t.Fatalf("unexpected cancellation after reconciliation: %+v", cancelled)
	}
}

func TestOpenMigratesManualOnlyDeliverySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "palpanel.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	oldProducts := `CREATE TABLE shop_products (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		price INTEGER NOT NULL CHECK(price > 0),
		stock INTEGER NOT NULL DEFAULT -1 CHECK(stock >= -1),
		per_player_limit INTEGER NOT NULL DEFAULT 0 CHECK(per_player_limit >= 0),
		enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
		delivery_mode TEXT NOT NULL DEFAULT 'manual' CHECK(delivery_mode IN ('manual')),
		payload_json TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`
	oldOrders := `CREATE TABLE shop_orders (
		id TEXT PRIMARY KEY,
		idempotency_key TEXT NOT NULL,
		product_id TEXT NOT NULL,
		product_name TEXT NOT NULL,
		player_uid TEXT NOT NULL,
		nickname TEXT NOT NULL DEFAULT '',
		steam_id TEXT NOT NULL DEFAULT '',
		quantity INTEGER NOT NULL CHECK(quantity > 0),
		unit_price INTEGER NOT NULL CHECK(unit_price > 0),
		total_points INTEGER NOT NULL CHECK(total_points > 0),
		status TEXT NOT NULL CHECK(status IN ('pending','delivered','cancelled')),
		reservation_id TEXT NOT NULL,
		delivery_mode TEXT NOT NULL,
		payload_json TEXT NOT NULL DEFAULT '{}',
		failure TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		delivered_at TEXT NOT NULL DEFAULT '',
		cancelled_at TEXT NOT NULL DEFAULT '',
		UNIQUE(player_uid,idempotency_key)
	)`
	for _, statement := range []string{oldProducts, oldOrders,
		`INSERT INTO shop_products(id,name,price,stock,per_player_limit,enabled,delivery_mode,payload_json,created_at,updated_at) VALUES('legacy-product','旧商品',10,5,0,1,'manual','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
		`INSERT INTO shop_orders(id,idempotency_key,product_id,product_name,player_uid,quantity,unit_price,total_points,status,reservation_id,delivery_mode,payload_json,created_at,updated_at) VALUES('legacy-order','legacy-key','legacy-product','旧商品','00112233445566778899aabbccddeeff',1,10,10,'pending','reservation-legacy','manual','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	service, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	legacy, err := service.GetOrder(ctx, "legacy-order")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.DeliveryMode != DeliveryModeManual || legacy.DeliveryState != DeliveryStateManual || legacy.DeliveryAttempts != 0 {
		t.Fatalf("legacy order was not migrated safely: %+v", legacy)
	}
	if _, err := service.CreateProduct(ctx, ProductInput{
		Name: "自动商品", Price: 10, Stock: 1, Enabled: true, DeliveryMode: DeliveryModePalDefenderItems,
		Payload: map[string]any{"items": []any{map[string]any{"item_id": "PalSphere", "count": 1}}},
	}); err != nil {
		t.Fatalf("automatic delivery mode remained blocked by the old SQLite CHECK constraint: %v", err)
	}
}

func TestUncertainExternalResultStaysLockedForReconciliation(t *testing.T) {
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
	player := "99887766554433221100ffeeddccbbaa"
	if _, err := ledger.Adjust(ctx, economy.Adjustment{PlayerUID: player, Delta: 100, Reason: "test", ReferenceType: "test", ReferenceID: "uncertain-seed", Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	product, err := service.CreateProduct(ctx, ProductInput{
		Name: "超时商品", Price: 10, Stock: 1, Enabled: true, DeliveryMode: DeliveryModePalDefenderItems,
		Payload: map[string]any{"items": []any{map[string]any{"item_id": "PalSphere", "count": 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateOrder(ctx, ledger, CreateOrderRequest{IdempotencyKey: "uncertain", ProductID: product.ID, PlayerUID: player, Quantity: 1}, "test")
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &fakeDispatcher{giveErr: fmt.Errorf("%w: timeout", ErrDeliveryUncertain)}
	if _, err := service.DeliverOrder(ctx, ledger, dispatcher, created.Order.ID, "test"); !errors.Is(err, ErrDeliveryUncertain) {
		t.Fatalf("err=%v want ErrDeliveryUncertain", err)
	}
	order, err := service.GetOrder(ctx, created.Order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if order.DeliveryState != DeliveryStateProcessing || order.Failure == "" {
		t.Fatalf("uncertain result must remain locked for reconciliation: %+v", order)
	}
	if _, err := service.DeliverOrder(ctx, ledger, dispatcher, created.Order.ID, "test"); !errors.Is(err, ErrDeliveryInProgress) {
		t.Fatalf("retry err=%v want ErrDeliveryInProgress", err)
	}
}

func TestBatchDeliveryAndAuditTrail(t *testing.T) {
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
	player := "11112222333344445555666677778888"
	if _, err := ledger.Adjust(ctx, economy.Adjustment{PlayerUID: player, Delta: 1000, Reason: "test", ReferenceType: "test", ReferenceID: "batch-seed", Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	product, err := service.CreateProduct(ctx, ProductInput{
		Name: "批量自动物品", Price: 10, Stock: -1, Enabled: true, DeliveryMode: DeliveryModePalDefenderItems,
		Payload: map[string]any{"items": []any{map[string]any{"item_id": "PalSphere", "count": 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var orderIDs []string
	for index := 0; index < 2; index++ {
		created, createErr := service.CreateOrder(ctx, ledger, CreateOrderRequest{
			IdempotencyKey: fmt.Sprintf("batch-%d", index), ProductID: product.ID, PlayerUID: player, Quantity: 1,
		}, "operator")
		if createErr != nil {
			t.Fatal(createErr)
		}
		orderIDs = append(orderIDs, created.Order.ID)
	}
	dispatcher := &fakeDispatcher{resolved: "resolved-player"}
	batch, err := service.DeliverBatch(ctx, ledger, dispatcher, BatchDeliveryRequest{Limit: 10}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if batch.Selected != 2 || batch.Delivered != 2 || batch.Failed != 0 || batch.Uncertain != 0 || batch.Skipped != 0 {
		t.Fatalf("unexpected batch result: %+v", batch)
	}
	for _, id := range orderIDs {
		order, loadErr := service.GetOrder(ctx, id)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if order.Status != "delivered" || order.DeliveryAttempts != 1 {
			t.Fatalf("order %s was not delivered exactly once: %+v", id, order)
		}
		events, eventErr := service.ListDeliveryEvents(ctx, DeliveryEventFilter{OrderID: id, Limit: 20})
		if eventErr != nil {
			t.Fatal(eventErr)
		}
		seen := map[string]bool{}
		for _, event := range events {
			seen[event.EventType] = true
		}
		for _, eventType := range []string{DeliveryEventCreated, DeliveryEventStarted, DeliveryEventSucceeded, DeliveryEventCompleted} {
			if !seen[eventType] {
				t.Fatalf("order %s audit trail is missing %s: %+v", id, eventType, events)
			}
		}
	}
	summary, err := service.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.DeliveryEvents < 8 || summary.DeliveredOrders != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestBatchRetryRequiresExplicitFailedOptIn(t *testing.T) {
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
	player := "99990000111122223333444455556666"
	if _, err := ledger.Adjust(ctx, economy.Adjustment{PlayerUID: player, Delta: 100, Reason: "test", ReferenceType: "test", ReferenceID: "batch-failed-seed", Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	product, err := service.CreateProduct(ctx, ProductInput{
		Name: "失败重试商品", Price: 10, Stock: -1, Enabled: true, DeliveryMode: DeliveryModePalDefenderItems,
		Payload: map[string]any{"items": []any{map[string]any{"item_id": "PalSphere", "count": 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateOrder(ctx, ledger, CreateOrderRequest{IdempotencyKey: "failed-batch", ProductID: product.ID, PlayerUID: player, Quantity: 1}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	failureDispatcher := &fakeDispatcher{giveErr: errors.New("offline")}
	if _, err := service.DeliverOrder(ctx, ledger, failureDispatcher, created.Order.ID, "operator"); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("err=%v want ErrDeliveryFailed", err)
	}
	withoutFailed, err := service.DeliverBatch(ctx, ledger, &fakeDispatcher{}, BatchDeliveryRequest{Limit: 10}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if withoutFailed.Selected != 0 {
		t.Fatalf("failed orders must not be selected without opt-in: %+v", withoutFailed)
	}
	withFailed, err := service.DeliverBatch(ctx, ledger, &fakeDispatcher{}, BatchDeliveryRequest{IncludeFailed: true, Limit: 10}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if withFailed.Selected != 1 || withFailed.Delivered != 1 {
		t.Fatalf("failed order was not retried: %+v", withFailed)
	}
}
