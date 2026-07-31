package shop

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"palpanel/internal/economy"

	_ "modernc.org/sqlite"
)

var (
	ErrInvalidProduct        = errors.New("shop product is invalid")
	ErrProductNotFound       = errors.New("shop product not found")
	ErrProductDisabled       = errors.New("shop product is disabled")
	ErrInvalidQuantity       = errors.New("quantity must be between 1 and 1000")
	ErrInsufficientStock     = errors.New("shop product stock is insufficient")
	ErrPlayerLimit           = errors.New("shop product per-player limit exceeded")
	ErrInvalidOrder          = errors.New("shop order is invalid")
	ErrOrderNotFound         = errors.New("shop order not found")
	ErrOrderSettled          = errors.New("shop order is already settled")
	ErrReservationConflict   = errors.New("shop order reservation state conflicts with the order")
	ErrDeliveryNotAutomatic  = errors.New("shop order does not use automatic delivery")
	ErrDeliveryInProgress    = errors.New("shop order delivery is in progress and requires reconciliation")
	ErrDeliveryUncertain     = errors.New("shop delivery result is uncertain and requires reconciliation")
	ErrDeliveryUnsafeCancel  = errors.New("shop order may already have been delivered and cannot be cancelled safely")
	ErrDeliveryResetInvalid  = errors.New("shop order delivery cannot be reset from its current state")
	ErrDeliveryPlayerMissing = errors.New("PalDefender could not resolve the order player")
	ErrDeliveryFailed        = errors.New("shop automatic delivery failed")
	ErrInvalidBatch          = errors.New("shop delivery batch is invalid")
)

const (
	defaultLimit        = 50
	maximumLimit        = 500
	orderReservationTTL = 3650 * 24 * time.Hour

	DeliveryModeManual                  = "manual"
	DeliveryModePalDefenderItems        = "paldefender_items"
	DeliveryModePalDefenderPalTemplates = "paldefender_pal_templates"

	DeliveryStateManual     = "manual"
	DeliveryStatePending    = "pending"
	DeliveryStateProcessing = "processing"
	DeliveryStateFailed     = "failed"
	DeliveryStateSucceeded  = "succeeded"

	DeliveryEventCreated   = "created"
	DeliveryEventStarted   = "started"
	DeliveryEventFailed    = "failed"
	DeliveryEventUncertain = "uncertain"
	DeliveryEventSucceeded = "succeeded"
	DeliveryEventReset     = "reset"
	DeliveryEventCompleted = "completed"
	DeliveryEventCancelled = "cancelled"

	maximumBatchDeliveries = 50
)

type Economy interface {
	GetAccount(context.Context, string) (economy.Account, error)
	Reserve(context.Context, string, string, string, string, int64, time.Duration, string) (economy.ReservationResult, error)
	CommitReservation(context.Context, string, string) (economy.ReservationResult, error)
	ReleaseReservation(context.Context, string, string) (economy.ReservationResult, error)
}

type Dispatcher interface {
	ResolvePlayer(context.Context, []string) (string, error)
	GiveItems(context.Context, string, []ItemGrant) error
	GivePalTemplates(context.Context, string, []string) error
}

type Service struct {
	db  *sql.DB
	now func() time.Time
}

type ItemGrant struct {
	ItemID string `json:"item_id"`
	Count  int64  `json:"count"`
}

type DeliveryPayload struct {
	Items        []ItemGrant `json:"items,omitempty"`
	PalTemplates []string    `json:"pal_templates,omitempty"`
}

type DeliveryReceipt struct {
	Mode           string   `json:"mode"`
	ResolvedPlayer string   `json:"resolved_player"`
	ItemGrants     int      `json:"item_grants,omitempty"`
	PalTemplates   []string `json:"pal_templates,omitempty"`
	DeliveredAt    string   `json:"delivered_at"`
}

type Product struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Price          int64          `json:"price"`
	Stock          int64          `json:"stock"`
	PerPlayerLimit int64          `json:"per_player_limit"`
	Enabled        bool           `json:"enabled"`
	DeliveryMode   string         `json:"delivery_mode"`
	Payload        map[string]any `json:"payload,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

type ProductInput struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Price          int64          `json:"price"`
	Stock          int64          `json:"stock"`
	PerPlayerLimit int64          `json:"per_player_limit"`
	Enabled        bool           `json:"enabled"`
	DeliveryMode   string         `json:"delivery_mode"`
	Payload        map[string]any `json:"payload"`
}

type Order struct {
	ID               string         `json:"id"`
	IdempotencyKey   string         `json:"idempotency_key"`
	ProductID        string         `json:"product_id"`
	ProductName      string         `json:"product_name"`
	PlayerUID        string         `json:"player_uid"`
	Nickname         string         `json:"nickname,omitempty"`
	SteamID          string         `json:"steam_id,omitempty"`
	Quantity         int64          `json:"quantity"`
	UnitPrice        int64          `json:"unit_price"`
	TotalPoints      int64          `json:"total_points"`
	Status           string         `json:"status"`
	ReservationID    string         `json:"reservation_id"`
	DeliveryMode     string         `json:"delivery_mode"`
	DeliveryState    string         `json:"delivery_state"`
	DeliveryAttempts int            `json:"delivery_attempts"`
	LastDeliveryAt   string         `json:"last_delivery_at,omitempty"`
	Payload          map[string]any `json:"payload,omitempty"`
	DeliveryReceipt  map[string]any `json:"delivery_receipt,omitempty"`
	Failure          string         `json:"failure,omitempty"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
	DeliveredAt      string         `json:"delivered_at,omitempty"`
	CancelledAt      string         `json:"cancelled_at,omitempty"`
}

type CreateOrderRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	ProductID      string `json:"product_id"`
	PlayerUID      string `json:"player_uid"`
	Nickname       string `json:"nickname"`
	SteamID        string `json:"steam_id"`
	Quantity       int64  `json:"quantity"`
}

type OrderResult struct {
	Order     Order           `json:"order"`
	Account   economy.Account `json:"account"`
	Duplicate bool            `json:"duplicate"`
}

type Summary struct {
	Products             int64 `json:"products"`
	EnabledProducts      int64 `json:"enabled_products"`
	PendingOrders        int64 `json:"pending_orders"`
	DeliveredOrders      int64 `json:"delivered_orders"`
	SpentPoints          int64 `json:"spent_points"`
	FailedDeliveries     int64 `json:"failed_deliveries"`
	ProcessingDeliveries int64 `json:"processing_deliveries"`
	DeliveryEvents       int64 `json:"delivery_events"`
}

type OrderFilter struct {
	Status        string
	PlayerUID     string
	DeliveryState string
	DeliveryMode  string
	Limit         int
	Offset        int
}

type DeliveryEvent struct {
	ID            int64          `json:"id"`
	OrderID       string         `json:"order_id"`
	EventType     string         `json:"event_type"`
	DeliveryState string         `json:"delivery_state"`
	Attempt       int            `json:"attempt"`
	Actor         string         `json:"actor,omitempty"`
	Message       string         `json:"message,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	CreatedAt     string         `json:"created_at"`
}

type DeliveryEventFilter struct {
	OrderID   string
	EventType string
	Limit     int
	Offset    int
}

type BatchDeliveryRequest struct {
	OrderIDs      []string `json:"order_ids"`
	IncludeFailed bool     `json:"include_failed"`
	Limit         int      `json:"limit"`
}

type BatchDeliveryItem struct {
	OrderID       string `json:"order_id"`
	Result        string `json:"result"`
	Status        string `json:"status"`
	DeliveryState string `json:"delivery_state"`
	Error         string `json:"error,omitempty"`
}

type BatchDeliveryResult struct {
	Selected  int                 `json:"selected"`
	Delivered int                 `json:"delivered"`
	Failed    int                 `json:"failed"`
	Uncertain int                 `json:"uncertain"`
	Skipped   int                 `json:"skipped"`
	Items     []BatchDeliveryItem `json:"items"`
}

var serviceCache sync.Map

func ForPath(path string) (*Service, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("shop database path is empty")
	}
	if cached, ok := serviceCache.Load(path); ok {
		return cached.(*Service), nil
	}
	service, err := Open(path)
	if err != nil {
		return nil, err
	}
	actual, loaded := serviceCache.LoadOrStore(path, service)
	if loaded {
		_ = service.Close()
		return actual.(*Service), nil
	}
	return service, nil
}

func Open(path string) (*Service, error) {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open shop database: %w", err)
	}
	database.SetMaxOpenConns(1)
	service := &Service{db: database, now: time.Now}
	if err := service.configure(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := service.ensureSchema(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	return service, nil
}

func (s *Service) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Service) configure(ctx context.Context) error {
	for _, statement := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure shop database: %w", err)
		}
	}
	return nil
}

func (s *Service) ensureSchema(ctx context.Context) error {
	statements := []string{
		createProductsTable("shop_products", true),
		createOrdersTable("shop_orders", true),
		createDeliveryEventsTable("shop_delivery_events", true),
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure shop schema: %w", err)
		}
	}
	if err := s.migrateDeliverySchema(ctx); err != nil {
		return err
	}
	return s.ensureIndexes(ctx)
}

func createProductsTable(name string, ifNotExists bool) string {
	prefix := "CREATE TABLE "
	if ifNotExists {
		prefix += "IF NOT EXISTS "
	}
	return prefix + name + ` (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		price INTEGER NOT NULL CHECK(price > 0),
		stock INTEGER NOT NULL DEFAULT -1 CHECK(stock >= -1),
		per_player_limit INTEGER NOT NULL DEFAULT 0 CHECK(per_player_limit >= 0),
		enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
		delivery_mode TEXT NOT NULL DEFAULT 'manual' CHECK(delivery_mode IN ('manual','paldefender_items','paldefender_pal_templates')),
		payload_json TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`
}

func createOrdersTable(name string, ifNotExists bool) string {
	prefix := "CREATE TABLE "
	if ifNotExists {
		prefix += "IF NOT EXISTS "
	}
	return prefix + name + ` (
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
		delivery_mode TEXT NOT NULL CHECK(delivery_mode IN ('manual','paldefender_items','paldefender_pal_templates')),
		delivery_state TEXT NOT NULL DEFAULT 'manual' CHECK(delivery_state IN ('manual','pending','processing','failed','succeeded')),
		delivery_attempts INTEGER NOT NULL DEFAULT 0 CHECK(delivery_attempts >= 0),
		last_delivery_at TEXT NOT NULL DEFAULT '',
		payload_json TEXT NOT NULL DEFAULT '{}',
		delivery_receipt_json TEXT NOT NULL DEFAULT '{}',
		failure TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		delivered_at TEXT NOT NULL DEFAULT '',
		cancelled_at TEXT NOT NULL DEFAULT '',
		UNIQUE(player_uid,idempotency_key)
	)`
}

func createDeliveryEventsTable(name string, ifNotExists bool) string {
	prefix := "CREATE TABLE "
	if ifNotExists {
		prefix += "IF NOT EXISTS "
	}
	return prefix + name + ` (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		order_id TEXT NOT NULL,
		event_type TEXT NOT NULL CHECK(event_type IN ('created','started','failed','uncertain','succeeded','reset','completed','cancelled')),
		delivery_state TEXT NOT NULL DEFAULT '',
		attempt INTEGER NOT NULL DEFAULT 0 CHECK(attempt >= 0),
		actor TEXT NOT NULL DEFAULT '',
		message TEXT NOT NULL DEFAULT '',
		details_json TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL
	)`
}

func (s *Service) ensureIndexes(ctx context.Context) error {
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_shop_products_enabled ON shop_products(enabled,updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_orders_created ON shop_orders(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_orders_player ON shop_orders(player_uid,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_orders_product ON shop_orders(product_id,status)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_orders_delivery ON shop_orders(status,delivery_state,updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_delivery_events_order ON shop_delivery_events(order_id,id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_delivery_events_type ON shop_delivery_events(event_type,id DESC)`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure shop indexes: %w", err)
		}
	}
	return nil
}

func (s *Service) migrateDeliverySchema(ctx context.Context) error {
	var productSQL string
	if err := s.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='shop_products'`).Scan(&productSQL); err != nil {
		return fmt.Errorf("inspect shop product schema: %w", err)
	}
	hasDeliveryColumns, err := s.tableHasColumn(ctx, "shop_orders", "delivery_state")
	if err != nil {
		return err
	}
	if strings.Contains(productSQL, "paldefender_items") && hasDeliveryColumns {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin shop delivery migration: %w", err)
	}
	defer rollback(tx)
	for _, statement := range []string{
		`DROP TABLE IF EXISTS shop_products_delivery_v2`,
		`DROP TABLE IF EXISTS shop_orders_delivery_v2`,
		createProductsTable("shop_products_delivery_v2", false),
		createOrdersTable("shop_orders_delivery_v2", false),
		`INSERT INTO shop_products_delivery_v2(id,name,description,price,stock,per_player_limit,enabled,delivery_mode,payload_json,created_at,updated_at)
		 SELECT id,name,description,price,stock,per_player_limit,enabled,delivery_mode,payload_json,created_at,updated_at FROM shop_products`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate shop delivery schema: %w", err)
		}
	}
	if hasDeliveryColumns {
		_, err = tx.ExecContext(ctx, `INSERT INTO shop_orders_delivery_v2(id,idempotency_key,product_id,product_name,player_uid,nickname,steam_id,quantity,unit_price,total_points,status,reservation_id,delivery_mode,delivery_state,delivery_attempts,last_delivery_at,payload_json,delivery_receipt_json,failure,created_at,updated_at,delivered_at,cancelled_at)
		 SELECT id,idempotency_key,product_id,product_name,player_uid,nickname,steam_id,quantity,unit_price,total_points,status,reservation_id,delivery_mode,delivery_state,delivery_attempts,last_delivery_at,payload_json,delivery_receipt_json,failure,created_at,updated_at,delivered_at,cancelled_at FROM shop_orders`)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO shop_orders_delivery_v2(id,idempotency_key,product_id,product_name,player_uid,nickname,steam_id,quantity,unit_price,total_points,status,reservation_id,delivery_mode,delivery_state,delivery_attempts,last_delivery_at,payload_json,delivery_receipt_json,failure,created_at,updated_at,delivered_at,cancelled_at)
		 SELECT id,idempotency_key,product_id,product_name,player_uid,nickname,steam_id,quantity,unit_price,total_points,status,reservation_id,delivery_mode,
		 CASE WHEN status='delivered' THEN 'succeeded' ELSE 'manual' END,0,'',payload_json,'{}',failure,created_at,updated_at,delivered_at,cancelled_at FROM shop_orders`)
	}
	if err != nil {
		return fmt.Errorf("copy shop orders during delivery migration: %w", err)
	}
	for _, statement := range []string{
		`DROP TABLE shop_orders`,
		`DROP TABLE shop_products`,
		`ALTER TABLE shop_products_delivery_v2 RENAME TO shop_products`,
		`ALTER TABLE shop_orders_delivery_v2 RENAME TO shop_orders`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("finish shop delivery migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit shop delivery migration: %w", err)
	}
	return nil
}

func (s *Service) tableHasColumn(ctx context.Context, table, column string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, fmt.Errorf("inspect shop table columns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Service) CreateProduct(ctx context.Context, input ProductInput) (Product, error) {
	normalized, err := normalizeProductInput(input)
	if err != nil {
		return Product{}, err
	}
	now := s.timestamp()
	product := Product{
		ID: newID("product", normalized.Name+now), Name: normalized.Name, Description: normalized.Description,
		Price: normalized.Price, Stock: normalized.Stock, PerPlayerLimit: normalized.PerPlayerLimit,
		Enabled: normalized.Enabled, DeliveryMode: normalized.DeliveryMode, Payload: normalized.Payload,
		CreatedAt: now, UpdatedAt: now,
	}
	payload, _ := json.Marshal(product.Payload)
	_, err = s.db.ExecContext(ctx, `INSERT INTO shop_products(id,name,description,price,stock,per_player_limit,enabled,delivery_mode,payload_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		product.ID, product.Name, product.Description, product.Price, product.Stock, product.PerPlayerLimit, boolInt(product.Enabled), product.DeliveryMode, string(payload), product.CreatedAt, product.UpdatedAt)
	if err != nil {
		return Product{}, err
	}
	return product, nil
}

func (s *Service) UpdateProduct(ctx context.Context, id string, input ProductInput) (Product, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Product{}, ErrProductNotFound
	}
	normalized, err := normalizeProductInput(input)
	if err != nil {
		return Product{}, err
	}
	payload, _ := json.Marshal(normalized.Payload)
	result, err := s.db.ExecContext(ctx, `UPDATE shop_products SET name=?,description=?,price=?,stock=?,per_player_limit=?,enabled=?,delivery_mode=?,payload_json=?,updated_at=? WHERE id=?`,
		normalized.Name, normalized.Description, normalized.Price, normalized.Stock, normalized.PerPlayerLimit, boolInt(normalized.Enabled), normalized.DeliveryMode, string(payload), s.timestamp(), id)
	if err != nil {
		return Product{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return Product{}, ErrProductNotFound
	}
	return s.GetProduct(ctx, id)
}

func (s *Service) ArchiveProduct(ctx context.Context, id string) (Product, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE shop_products SET enabled=0,updated_at=? WHERE id=?`, s.timestamp(), strings.TrimSpace(id))
	if err != nil {
		return Product{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return Product{}, ErrProductNotFound
	}
	return s.GetProduct(ctx, id)
}

func (s *Service) GetProduct(ctx context.Context, id string) (Product, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,description,price,stock,per_player_limit,enabled,delivery_mode,payload_json,created_at,updated_at FROM shop_products WHERE id=?`, strings.TrimSpace(id))
	product, err := scanProduct(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Product{}, ErrProductNotFound
	}
	return product, err
}

func (s *Service) ListProducts(ctx context.Context, includeDisabled bool, limit, offset int) ([]Product, error) {
	limit, offset = normalizePage(limit, offset)
	query := `SELECT id,name,description,price,stock,per_player_limit,enabled,delivery_mode,payload_json,created_at,updated_at FROM shop_products`
	args := []any{}
	if !includeDisabled {
		query += ` WHERE enabled=1`
	}
	query += ` ORDER BY enabled DESC,updated_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Product{}
	for rows.Next() {
		item, scanErr := scanProduct(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) CreateOrder(ctx context.Context, ledger Economy, request CreateOrderRequest, actor string) (OrderResult, error) {
	request.PlayerUID = normalizePlayerUID(request.PlayerUID)
	request.ProductID = strings.TrimSpace(request.ProductID)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.Nickname = strings.TrimSpace(request.Nickname)
	request.SteamID = strings.TrimSpace(request.SteamID)
	if request.PlayerUID == "" || request.ProductID == "" || request.IdempotencyKey == "" || len(request.IdempotencyKey) > 128 {
		return OrderResult{}, ErrInvalidOrder
	}
	if request.Quantity < 1 || request.Quantity > 1000 {
		return OrderResult{}, ErrInvalidQuantity
	}
	if existing, err := s.orderByIdempotency(ctx, request.PlayerUID, request.IdempotencyKey); err == nil {
		account, accountErr := ledger.GetAccount(ctx, request.PlayerUID)
		if accountErr != nil {
			return OrderResult{}, accountErr
		}
		return OrderResult{Order: existing, Account: account, Duplicate: true}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return OrderResult{}, err
	}

	product, err := s.GetProduct(ctx, request.ProductID)
	if err != nil {
		return OrderResult{}, err
	}
	if !product.Enabled {
		return OrderResult{}, ErrProductDisabled
	}
	if product.Stock >= 0 && product.Stock < request.Quantity {
		return OrderResult{}, ErrInsufficientStock
	}
	if _, _, err := deliveryPlan(product.DeliveryMode, product.Payload, request.Quantity); err != nil {
		return OrderResult{}, err
	}
	if product.Price > (1<<63-1)/request.Quantity {
		return OrderResult{}, ErrInvalidOrder
	}
	orderID := deterministicOrderID(request.PlayerUID, request.IdempotencyKey)
	total := product.Price * request.Quantity
	reservation, err := ledger.Reserve(ctx, request.PlayerUID, request.Nickname, request.SteamID, orderID, total, orderReservationTTL, actor)
	if err != nil {
		return OrderResult{}, err
	}
	if reservation.Existing {
		if existing, existingErr := s.orderByIdempotency(ctx, request.PlayerUID, request.IdempotencyKey); existingErr == nil {
			return OrderResult{Order: existing, Account: reservation.Account, Duplicate: true}, nil
		} else if !errors.Is(existingErr, sql.ErrNoRows) {
			return OrderResult{}, existingErr
		}
		if reservation.Reservation.Status != "reserved" || reservation.Reservation.Amount != total {
			return OrderResult{}, ErrReservationConflict
		}
	}

	order := Order{
		ID: orderID, IdempotencyKey: request.IdempotencyKey, ProductID: product.ID, ProductName: product.Name,
		PlayerUID: request.PlayerUID, Nickname: request.Nickname, SteamID: request.SteamID, Quantity: request.Quantity,
		UnitPrice: product.Price, TotalPoints: total, Status: "pending", ReservationID: reservation.Reservation.ID,
		DeliveryMode: product.DeliveryMode, DeliveryState: initialDeliveryState(product.DeliveryMode), Payload: product.Payload,
		DeliveryReceipt: map[string]any{}, CreatedAt: s.timestamp(), UpdatedAt: s.timestamp(),
	}
	if err := s.insertOrder(ctx, order, product, actor); err != nil {
		if existing, existingErr := s.orderByIdempotency(ctx, request.PlayerUID, request.IdempotencyKey); existingErr == nil {
			return OrderResult{Order: existing, Account: reservation.Account, Duplicate: true}, nil
		}
		if _, releaseErr := ledger.ReleaseReservation(ctx, reservation.Reservation.ID, actor); releaseErr != nil {
			return OrderResult{}, fmt.Errorf("create shop order: %w; release reservation: %v", err, releaseErr)
		}
		return OrderResult{}, err
	}
	return OrderResult{Order: order, Account: reservation.Account}, nil
}

func (s *Service) insertOrder(ctx context.Context, order Order, product Product, actor string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if product.PerPlayerLimit > 0 {
		var purchased int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(quantity),0) FROM shop_orders WHERE product_id=? AND player_uid=? AND status IN ('pending','delivered')`, product.ID, order.PlayerUID).Scan(&purchased); err != nil {
			return err
		}
		if purchased+order.Quantity > product.PerPlayerLimit {
			return ErrPlayerLimit
		}
	}
	if product.Stock >= 0 {
		result, err := tx.ExecContext(ctx, `UPDATE shop_products SET stock=stock-?,updated_at=? WHERE id=? AND enabled=1 AND stock>=?`, order.Quantity, order.UpdatedAt, product.ID, order.Quantity)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count == 0 {
			return ErrInsufficientStock
		}
	} else {
		result, err := tx.ExecContext(ctx, `UPDATE shop_products SET updated_at=? WHERE id=? AND enabled=1`, order.UpdatedAt, product.ID)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count == 0 {
			return ErrProductDisabled
		}
	}
	payload, _ := json.Marshal(order.Payload)
	receipt, _ := json.Marshal(order.DeliveryReceipt)
	_, err = tx.ExecContext(ctx, `INSERT INTO shop_orders(id,idempotency_key,product_id,product_name,player_uid,nickname,steam_id,quantity,unit_price,total_points,status,reservation_id,delivery_mode,delivery_state,delivery_attempts,last_delivery_at,payload_json,delivery_receipt_json,failure,created_at,updated_at,delivered_at,cancelled_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		order.ID, order.IdempotencyKey, order.ProductID, order.ProductName, order.PlayerUID, order.Nickname, order.SteamID,
		order.Quantity, order.UnitPrice, order.TotalPoints, order.Status, order.ReservationID, order.DeliveryMode, order.DeliveryState,
		order.DeliveryAttempts, order.LastDeliveryAt, string(payload), string(receipt), order.Failure,
		order.CreatedAt, order.UpdatedAt, order.DeliveredAt, order.CancelledAt)
	if err != nil {
		return err
	}
	if err := s.insertDeliveryEvent(ctx, tx, DeliveryEvent{
		OrderID: order.ID, EventType: DeliveryEventCreated, DeliveryState: order.DeliveryState, Attempt: 0, Actor: actor,
		Message: "shop order created and points reserved",
		Details: map[string]any{"product_id": order.ProductID, "quantity": order.Quantity, "total_points": order.TotalPoints},
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) CompleteOrder(ctx context.Context, ledger Economy, id, actor string) (OrderResult, error) {
	order, err := s.GetOrder(ctx, id)
	if err != nil {
		return OrderResult{}, err
	}
	if order.Status == "delivered" {
		reservation, reserveErr := ledger.CommitReservation(ctx, order.ReservationID, actor)
		if reserveErr != nil {
			return OrderResult{}, reserveErr
		}
		return OrderResult{Order: order, Account: reservation.Account, Duplicate: true}, nil
	}
	if order.Status != "pending" {
		return OrderResult{}, ErrOrderSettled
	}
	reservation, err := ledger.CommitReservation(ctx, order.ReservationID, actor)
	if err != nil {
		return OrderResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OrderResult{}, err
	}
	defer rollback(tx)
	now := s.timestamp()
	reconciliationReceipt, _ := json.Marshal(map[string]any{
		"reconciliation": "administrator_confirmed_delivery",
		"actor":          strings.TrimSpace(actor),
		"confirmed_at":   now,
	})
	result, err := tx.ExecContext(ctx, `UPDATE shop_orders SET status='delivered',delivery_state='succeeded',delivery_receipt_json=CASE WHEN delivery_mode<>'manual' AND delivery_state<>'succeeded' THEN ? ELSE delivery_receipt_json END,delivered_at=?,updated_at=?,failure='' WHERE id=? AND status='pending'`, string(reconciliationReceipt), now, now, order.ID)
	if err != nil {
		return OrderResult{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return OrderResult{}, ErrOrderSettled
	}
	if err := s.insertDeliveryEvent(ctx, tx, DeliveryEvent{
		OrderID: order.ID, EventType: DeliveryEventCompleted, DeliveryState: DeliveryStateSucceeded, Attempt: order.DeliveryAttempts, Actor: actor,
		Message: "shop order delivery confirmed and points committed",
		Details: map[string]any{"manual_confirmation": order.DeliveryMode == DeliveryModeManual || order.DeliveryState != DeliveryStateSucceeded},
	}); err != nil {
		return OrderResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return OrderResult{}, err
	}
	order.Status, order.DeliveryState, order.DeliveredAt, order.UpdatedAt, order.Failure = "delivered", DeliveryStateSucceeded, now, now, ""
	return OrderResult{Order: order, Account: reservation.Account}, nil
}

func (s *Service) CancelOrder(ctx context.Context, ledger Economy, id, actor string) (OrderResult, error) {
	order, err := s.GetOrder(ctx, id)
	if err != nil {
		return OrderResult{}, err
	}
	if order.Status == "cancelled" {
		reservation, reserveErr := ledger.ReleaseReservation(ctx, order.ReservationID, actor)
		if reserveErr != nil {
			return OrderResult{}, reserveErr
		}
		return OrderResult{Order: order, Account: reservation.Account, Duplicate: true}, nil
	}
	if order.Status != "pending" {
		return OrderResult{}, ErrOrderSettled
	}
	if order.DeliveryMode != DeliveryModeManual && (order.DeliveryState == DeliveryStateProcessing || order.DeliveryState == DeliveryStateSucceeded) {
		return OrderResult{}, ErrDeliveryUnsafeCancel
	}
	reservation, err := ledger.ReleaseReservation(ctx, order.ReservationID, actor)
	if err != nil {
		return OrderResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OrderResult{}, err
	}
	defer rollback(tx)
	now := s.timestamp()
	result, err := tx.ExecContext(ctx, `UPDATE shop_orders SET status='cancelled',cancelled_at=?,updated_at=?,failure='' WHERE id=? AND status='pending'`, now, now, order.ID)
	if err != nil {
		return OrderResult{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return OrderResult{}, ErrOrderSettled
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shop_products SET stock=CASE WHEN stock<0 THEN stock ELSE stock+? END,updated_at=? WHERE id=?`, order.Quantity, now, order.ProductID); err != nil {
		return OrderResult{}, err
	}
	if err := s.insertDeliveryEvent(ctx, tx, DeliveryEvent{
		OrderID: order.ID, EventType: DeliveryEventCancelled, DeliveryState: order.DeliveryState, Attempt: order.DeliveryAttempts, Actor: actor,
		Message: "shop order cancelled, points released, and stock restored",
	}); err != nil {
		return OrderResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return OrderResult{}, err
	}
	order.Status, order.CancelledAt, order.UpdatedAt = "cancelled", now, now
	return OrderResult{Order: order, Account: reservation.Account}, nil
}

func (s *Service) DeliverOrder(ctx context.Context, ledger Economy, dispatcher Dispatcher, id, actor string) (OrderResult, error) {
	order, err := s.GetOrder(ctx, id)
	if err != nil {
		return OrderResult{}, err
	}
	if order.Status == "delivered" {
		return s.CompleteOrder(ctx, ledger, order.ID, actor)
	}
	if order.Status != "pending" {
		return OrderResult{}, ErrOrderSettled
	}
	if order.DeliveryMode == DeliveryModeManual {
		return OrderResult{}, ErrDeliveryNotAutomatic
	}
	if dispatcher == nil {
		return OrderResult{}, fmt.Errorf("%w: dispatcher is unavailable", ErrDeliveryFailed)
	}
	if order.DeliveryState == DeliveryStateSucceeded {
		return s.CompleteOrder(ctx, ledger, order.ID, actor)
	}
	if order.DeliveryState == DeliveryStateProcessing {
		return OrderResult{}, ErrDeliveryInProgress
	}
	items, templates, err := deliveryPlan(order.DeliveryMode, order.Payload, order.Quantity)
	if err != nil {
		return OrderResult{}, err
	}
	order, err = s.beginDelivery(ctx, order.ID, actor)
	if err != nil {
		return OrderResult{}, err
	}
	aliases := []string{order.PlayerUID, order.SteamID}
	player, err := dispatcher.ResolvePlayer(ctx, aliases)
	if err != nil {
		_ = s.recordDeliveryFailure(ctx, order.ID, actor, err)
		if errors.Is(err, ErrDeliveryPlayerMissing) {
			return OrderResult{}, err
		}
		return OrderResult{}, fmt.Errorf("%w: %v", ErrDeliveryFailed, err)
	}
	switch order.DeliveryMode {
	case DeliveryModePalDefenderItems:
		err = dispatcher.GiveItems(ctx, player, items)
	case DeliveryModePalDefenderPalTemplates:
		err = dispatcher.GivePalTemplates(ctx, player, templates)
	default:
		err = ErrDeliveryNotAutomatic
	}
	if err != nil {
		if errors.Is(err, ErrDeliveryUncertain) {
			_ = s.recordDeliveryUncertain(ctx, order.ID, actor, err)
			return OrderResult{}, err
		}
		_ = s.recordDeliveryFailure(ctx, order.ID, actor, err)
		return OrderResult{}, fmt.Errorf("%w: %v", ErrDeliveryFailed, err)
	}
	receipt := DeliveryReceipt{
		Mode: order.DeliveryMode, ResolvedPlayer: player, ItemGrants: len(items),
		PalTemplates: templates, DeliveredAt: s.timestamp(),
	}
	if err := s.recordDeliverySuccess(ctx, order.ID, actor, receipt); err != nil {
		return OrderResult{}, fmt.Errorf("record successful shop delivery: %w", err)
	}
	return s.CompleteOrder(ctx, ledger, order.ID, actor)
}

func (s *Service) ResetDelivery(ctx context.Context, id, actor string) (Order, error) {
	order, err := s.GetOrder(ctx, id)
	if err != nil {
		return Order{}, err
	}
	if order.Status != "pending" || order.DeliveryMode == DeliveryModeManual || (order.DeliveryState != DeliveryStateProcessing && order.DeliveryState != DeliveryStateFailed) {
		return Order{}, ErrDeliveryResetInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, err
	}
	defer rollback(tx)
	now := s.timestamp()
	receipt, _ := json.Marshal(map[string]any{
		"reconciliation": "reset",
		"actor":          strings.TrimSpace(actor),
		"reset_at":       now,
	})
	result, err := tx.ExecContext(ctx, `UPDATE shop_orders SET delivery_state='pending',delivery_receipt_json=?,failure='',updated_at=? WHERE id=? AND status='pending' AND delivery_state IN ('processing','failed')`, string(receipt), now, order.ID)
	if err != nil {
		return Order{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return Order{}, ErrDeliveryResetInvalid
	}
	if err := s.insertDeliveryEvent(ctx, tx, DeliveryEvent{
		OrderID: order.ID, EventType: DeliveryEventReset, DeliveryState: DeliveryStatePending, Attempt: order.DeliveryAttempts, Actor: actor,
		Message: "administrator confirmed no delivery and reset the order for retry",
	}); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(); err != nil {
		return Order{}, err
	}
	return s.GetOrder(ctx, order.ID)
}

func (s *Service) beginDelivery(ctx context.Context, id, actor string) (Order, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, err
	}
	defer rollback(tx)
	now := s.timestamp()
	result, err := tx.ExecContext(ctx, `UPDATE shop_orders SET delivery_state='processing',delivery_attempts=delivery_attempts+1,last_delivery_at=?,updated_at=?,failure='' WHERE id=? AND status='pending' AND delivery_state IN ('pending','failed')`, now, now, strings.TrimSpace(id))
	if err != nil {
		return Order{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		order, loadErr := scanOrder(tx.QueryRowContext(ctx, orderSelect+` WHERE id=?`, strings.TrimSpace(id)))
		if loadErr != nil {
			return Order{}, loadErr
		}
		if order.DeliveryState == DeliveryStateProcessing {
			return Order{}, ErrDeliveryInProgress
		}
		return Order{}, ErrOrderSettled
	}
	order, err := scanOrder(tx.QueryRowContext(ctx, orderSelect+` WHERE id=?`, strings.TrimSpace(id)))
	if err != nil {
		return Order{}, err
	}
	if err := s.insertDeliveryEvent(ctx, tx, DeliveryEvent{
		OrderID: order.ID, EventType: DeliveryEventStarted, DeliveryState: DeliveryStateProcessing, Attempt: order.DeliveryAttempts, Actor: actor,
		Message: "automatic delivery started",
	}); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(); err != nil {
		return Order{}, err
	}
	return order, nil
}

func (s *Service) recordDeliveryFailure(ctx context.Context, id, actor string, cause error) error {
	return s.recordDeliveryOutcome(ctx, id, actor, DeliveryEventFailed, DeliveryStateFailed, cause, nil)
}

func (s *Service) recordDeliveryUncertain(ctx context.Context, id, actor string, cause error) error {
	return s.recordDeliveryOutcome(ctx, id, actor, DeliveryEventUncertain, DeliveryStateProcessing, cause, nil)
}

func (s *Service) recordDeliverySuccess(ctx context.Context, id, actor string, receipt DeliveryReceipt) error {
	return s.recordDeliveryOutcome(ctx, id, actor, DeliveryEventSucceeded, DeliveryStateSucceeded, nil, receipt)
}

func (s *Service) recordDeliveryOutcome(ctx context.Context, id, actor, eventType, state string, cause error, receipt any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	now := s.timestamp()
	failure := ""
	if cause != nil {
		failure = sanitizeDeliveryFailure(cause)
	}
	receiptJSON := ""
	if receipt != nil {
		body, marshalErr := json.Marshal(receipt)
		if marshalErr != nil {
			return marshalErr
		}
		receiptJSON = string(body)
	}
	var result sql.Result
	if state == DeliveryStateSucceeded {
		result, err = tx.ExecContext(ctx, `UPDATE shop_orders SET delivery_state='succeeded',delivery_receipt_json=?,failure='',last_delivery_at=?,updated_at=? WHERE id=? AND status='pending' AND delivery_state='processing'`, receiptJSON, now, now, strings.TrimSpace(id))
	} else if state == DeliveryStateFailed {
		result, err = tx.ExecContext(ctx, `UPDATE shop_orders SET delivery_state='failed',failure=?,last_delivery_at=?,updated_at=? WHERE id=? AND status='pending' AND delivery_state='processing'`, failure, now, now, strings.TrimSpace(id))
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE shop_orders SET failure=?,last_delivery_at=?,updated_at=? WHERE id=? AND status='pending' AND delivery_state='processing'`, failure, now, now, strings.TrimSpace(id))
	}
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrDeliveryInProgress
	}
	order, err := scanOrder(tx.QueryRowContext(ctx, orderSelect+` WHERE id=?`, strings.TrimSpace(id)))
	if err != nil {
		return err
	}
	details := map[string]any{}
	if receipt != nil {
		details["receipt"] = receipt
	}
	if err := s.insertDeliveryEvent(ctx, tx, DeliveryEvent{
		OrderID: order.ID, EventType: eventType, DeliveryState: state, Attempt: order.DeliveryAttempts, Actor: actor, Message: failure, Details: details,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) GetOrder(ctx context.Context, id string) (Order, error) {
	row := s.db.QueryRowContext(ctx, orderSelect+` WHERE id=?`, strings.TrimSpace(id))
	order, err := scanOrder(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	return order, err
}

func (s *Service) ListOrders(ctx context.Context, status, playerUID string, limit, offset int) ([]Order, error) {
	return s.ListOrdersFiltered(ctx, OrderFilter{Status: status, PlayerUID: playerUID, Limit: limit, Offset: offset})
}

func (s *Service) ListOrdersFiltered(ctx context.Context, filter OrderFilter) ([]Order, error) {
	filter.Limit, filter.Offset = normalizePage(filter.Limit, filter.Offset)
	conditions := []string{"1=1"}
	args := []any{}
	if value := strings.TrimSpace(filter.Status); value != "" {
		conditions = append(conditions, "status=?")
		args = append(args, value)
	}
	if value := normalizePlayerUID(filter.PlayerUID); value != "" {
		conditions = append(conditions, "player_uid=?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.DeliveryState); value != "" {
		conditions = append(conditions, "delivery_state=?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.DeliveryMode); value != "" {
		if value == "automatic" {
			conditions = append(conditions, "delivery_mode<>'manual'")
		} else {
			conditions = append(conditions, "delivery_mode=?")
			args = append(args, value)
		}
	}
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, orderSelect+` WHERE `+strings.Join(conditions, " AND ")+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Order{}
	for rows.Next() {
		item, scanErr := scanOrder(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ListDeliveryEvents(ctx context.Context, filter DeliveryEventFilter) ([]DeliveryEvent, error) {
	filter.Limit, filter.Offset = normalizePage(filter.Limit, filter.Offset)
	conditions := []string{"1=1"}
	args := []any{}
	if value := strings.TrimSpace(filter.OrderID); value != "" {
		conditions = append(conditions, "order_id=?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.EventType); value != "" {
		conditions = append(conditions, "event_type=?")
		args = append(args, value)
	}
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id,order_id,event_type,delivery_state,attempt,actor,message,details_json,created_at FROM shop_delivery_events WHERE `+strings.Join(conditions, " AND ")+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DeliveryEvent{}
	for rows.Next() {
		var event DeliveryEvent
		var details string
		if err := rows.Scan(&event.ID, &event.OrderID, &event.EventType, &event.DeliveryState, &event.Attempt, &event.Actor, &event.Message, &details, &event.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(details), &event.Details)
		if event.Details == nil {
			event.Details = map[string]any{}
		}
		items = append(items, event)
	}
	return items, rows.Err()
}

func (s *Service) DeliverBatch(ctx context.Context, ledger Economy, dispatcher Dispatcher, request BatchDeliveryRequest, actor string) (BatchDeliveryResult, error) {
	orders, err := s.selectBatchOrders(ctx, request)
	if err != nil {
		return BatchDeliveryResult{}, err
	}
	result := BatchDeliveryResult{Selected: len(orders), Items: make([]BatchDeliveryItem, 0, len(orders))}
	for _, order := range orders {
		item := BatchDeliveryItem{OrderID: order.ID, Status: order.Status, DeliveryState: order.DeliveryState}
		if order.Status != "pending" || order.DeliveryMode == DeliveryModeManual || order.DeliveryState == DeliveryStateProcessing || (order.DeliveryState == DeliveryStateFailed && !request.IncludeFailed) {
			item.Result = "skipped"
			item.Error = "order is not eligible for safe automatic delivery"
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}
		delivered, deliverErr := s.DeliverOrder(ctx, ledger, dispatcher, order.ID, actor)
		if deliverErr == nil {
			item.Result = "delivered"
			item.Status = delivered.Order.Status
			item.DeliveryState = delivered.Order.DeliveryState
			result.Delivered++
		} else {
			item.Error = sanitizeDeliveryFailure(deliverErr)
			if current, loadErr := s.GetOrder(ctx, order.ID); loadErr == nil {
				item.Status = current.Status
				item.DeliveryState = current.DeliveryState
			}
			switch {
			case errors.Is(deliverErr, ErrDeliveryUncertain), errors.Is(deliverErr, ErrDeliveryInProgress):
				item.Result = "uncertain"
				result.Uncertain++
			case errors.Is(deliverErr, ErrDeliveryFailed), errors.Is(deliverErr, ErrDeliveryPlayerMissing):
				item.Result = "failed"
				result.Failed++
			default:
				item.Result = "skipped"
				result.Skipped++
			}
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *Service) selectBatchOrders(ctx context.Context, request BatchDeliveryRequest) ([]Order, error) {
	limit := request.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > maximumBatchDeliveries {
		return nil, ErrInvalidBatch
	}
	if len(request.OrderIDs) > maximumBatchDeliveries {
		return nil, ErrInvalidBatch
	}
	if len(request.OrderIDs) > 0 {
		seen := map[string]bool{}
		orders := make([]Order, 0, len(request.OrderIDs))
		for _, id := range request.OrderIDs {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			order, err := s.GetOrder(ctx, id)
			if err != nil {
				if errors.Is(err, ErrOrderNotFound) {
					continue
				}
				return nil, err
			}
			orders = append(orders, order)
			if len(orders) >= limit {
				break
			}
		}
		return orders, nil
	}
	states := "('pending','succeeded')"
	if request.IncludeFailed {
		states = "('pending','failed','succeeded')"
	}
	rows, err := s.db.QueryContext(ctx, orderSelect+` WHERE status='pending' AND delivery_mode<>'manual' AND delivery_state IN `+states+` ORDER BY created_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	orders := []Order{}
	for rows.Next() {
		order, scanErr := scanOrder(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

func (s *Service) Summary(ctx context.Context) (Summary, error) {
	var result Summary
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN enabled=1 THEN 1 ELSE 0 END),0) FROM shop_products`).Scan(&result.Products, &result.EnabledProducts); err != nil {
		return Summary{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='delivered' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='delivered' THEN total_points ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='pending' AND delivery_state='failed' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='pending' AND delivery_state='processing' THEN 1 ELSE 0 END),0) FROM shop_orders`).Scan(&result.PendingOrders, &result.DeliveredOrders, &result.SpentPoints, &result.FailedDeliveries, &result.ProcessingDeliveries); err != nil {
		return Summary{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shop_delivery_events`).Scan(&result.DeliveryEvents); err != nil {
		return Summary{}, err
	}
	return result, nil
}

type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Service) insertDeliveryEvent(ctx context.Context, execer sqlExecer, event DeliveryEvent) error {
	if event.Details == nil {
		event.Details = map[string]any{}
	}
	details, err := json.Marshal(event.Details)
	if err != nil {
		return err
	}
	if event.CreatedAt == "" {
		event.CreatedAt = s.timestamp()
	}
	_, err = execer.ExecContext(ctx, `INSERT INTO shop_delivery_events(order_id,event_type,delivery_state,attempt,actor,message,details_json,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		strings.TrimSpace(event.OrderID), event.EventType, event.DeliveryState, event.Attempt, strings.TrimSpace(event.Actor), sanitizeEventMessage(event.Message), string(details), event.CreatedAt)
	return err
}

func sanitizeEventMessage(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}

func (s *Service) orderByIdempotency(ctx context.Context, playerUID, key string) (Order, error) {
	return scanOrder(s.db.QueryRowContext(ctx, orderSelect+` WHERE player_uid=? AND idempotency_key=?`, playerUID, key))
}

const orderSelect = `SELECT id,idempotency_key,product_id,product_name,player_uid,nickname,steam_id,quantity,unit_price,total_points,status,reservation_id,delivery_mode,delivery_state,delivery_attempts,last_delivery_at,payload_json,delivery_receipt_json,failure,created_at,updated_at,delivered_at,cancelled_at FROM shop_orders`

type scanner interface{ Scan(...any) error }

func scanProduct(row scanner) (Product, error) {
	var product Product
	var enabled int
	var payload string
	err := row.Scan(&product.ID, &product.Name, &product.Description, &product.Price, &product.Stock, &product.PerPlayerLimit, &enabled, &product.DeliveryMode, &payload, &product.CreatedAt, &product.UpdatedAt)
	if err != nil {
		return Product{}, err
	}
	product.Enabled = enabled == 1
	_ = json.Unmarshal([]byte(payload), &product.Payload)
	if product.Payload == nil {
		product.Payload = map[string]any{}
	}
	return product, nil
}

func scanOrder(row scanner) (Order, error) {
	var order Order
	var payload, receipt string
	err := row.Scan(&order.ID, &order.IdempotencyKey, &order.ProductID, &order.ProductName, &order.PlayerUID, &order.Nickname, &order.SteamID,
		&order.Quantity, &order.UnitPrice, &order.TotalPoints, &order.Status, &order.ReservationID, &order.DeliveryMode,
		&order.DeliveryState, &order.DeliveryAttempts, &order.LastDeliveryAt, &payload, &receipt, &order.Failure,
		&order.CreatedAt, &order.UpdatedAt, &order.DeliveredAt, &order.CancelledAt)
	if err != nil {
		return Order{}, err
	}
	_ = json.Unmarshal([]byte(payload), &order.Payload)
	if order.Payload == nil {
		order.Payload = map[string]any{}
	}
	_ = json.Unmarshal([]byte(receipt), &order.DeliveryReceipt)
	if order.DeliveryReceipt == nil {
		order.DeliveryReceipt = map[string]any{}
	}
	return order, nil
}

func normalizeProductInput(input ProductInput) (ProductInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.DeliveryMode = strings.TrimSpace(strings.ToLower(input.DeliveryMode))
	if input.DeliveryMode == "" {
		input.DeliveryMode = DeliveryModeManual
	}
	allowedMode := input.DeliveryMode == DeliveryModeManual || input.DeliveryMode == DeliveryModePalDefenderItems || input.DeliveryMode == DeliveryModePalDefenderPalTemplates
	if input.Name == "" || len([]rune(input.Name)) > 80 || len([]rune(input.Description)) > 500 || input.Price <= 0 || input.Price > 1_000_000_000 || input.Stock < -1 || input.PerPlayerLimit < 0 || !allowedMode {
		return ProductInput{}, ErrInvalidProduct
	}
	if input.Payload == nil {
		input.Payload = map[string]any{}
	}
	normalized, err := normalizeDeliveryPayload(input.DeliveryMode, input.Payload)
	if err != nil {
		return ProductInput{}, err
	}
	input.Payload = normalized
	return input, nil
}

func normalizeDeliveryPayload(mode string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil || len(body) > 64*1024 {
		return nil, ErrInvalidProduct
	}
	if mode == DeliveryModeManual {
		return payload, nil
	}
	var typed DeliveryPayload
	if err := json.Unmarshal(body, &typed); err != nil {
		return nil, ErrInvalidProduct
	}
	switch mode {
	case DeliveryModePalDefenderItems:
		if len(payload) != 1 || payload["items"] == nil || len(typed.Items) == 0 || len(typed.Items) > 100 || len(typed.PalTemplates) != 0 {
			return nil, ErrInvalidProduct
		}
		for _, item := range typed.Items {
			if !validItemID(item.ItemID) || item.Count < 1 || item.Count > 2_147_483_647 {
				return nil, ErrInvalidProduct
			}
		}
	case DeliveryModePalDefenderPalTemplates:
		if len(payload) != 1 || payload["pal_templates"] == nil || len(typed.PalTemplates) == 0 || len(typed.PalTemplates) > 20 || len(typed.Items) != 0 {
			return nil, ErrInvalidProduct
		}
		for index := range typed.PalTemplates {
			typed.PalTemplates[index] = strings.TrimSpace(typed.PalTemplates[index])
			if !validTemplateName(typed.PalTemplates[index]) {
				return nil, ErrInvalidProduct
			}
		}
	default:
		return nil, ErrInvalidProduct
	}
	canonical, err := json.Marshal(typed)
	if err != nil {
		return nil, ErrInvalidProduct
	}
	var result map[string]any
	if err := json.Unmarshal(canonical, &result); err != nil {
		return nil, ErrInvalidProduct
	}
	return result, nil
}

func deliveryPlan(mode string, payload map[string]any, quantity int64) ([]ItemGrant, []string, error) {
	if quantity < 1 {
		return nil, nil, ErrInvalidQuantity
	}
	if mode == DeliveryModeManual {
		return nil, nil, nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, ErrInvalidProduct
	}
	var typed DeliveryPayload
	if err := json.Unmarshal(body, &typed); err != nil {
		return nil, nil, ErrInvalidProduct
	}
	switch mode {
	case DeliveryModePalDefenderItems:
		items := make([]ItemGrant, 0, len(typed.Items))
		for _, item := range typed.Items {
			if item.Count > 2_147_483_647/quantity {
				return nil, nil, ErrInvalidQuantity
			}
			items = append(items, ItemGrant{ItemID: item.ItemID, Count: item.Count * quantity})
		}
		return items, nil, nil
	case DeliveryModePalDefenderPalTemplates:
		if int64(len(typed.PalTemplates))*quantity > 20 {
			return nil, nil, ErrInvalidQuantity
		}
		templates := make([]string, 0, int64(len(typed.PalTemplates))*quantity)
		for count := int64(0); count < quantity; count++ {
			templates = append(templates, typed.PalTemplates...)
		}
		return nil, templates, nil
	default:
		return nil, nil, ErrInvalidProduct
	}
}

func initialDeliveryState(mode string) string {
	if mode == DeliveryModeManual {
		return DeliveryStateManual
	}
	return DeliveryStatePending
}

func validItemID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == ':' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func validTemplateName(value string) bool {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, ".json") {
		value = strings.TrimSuffix(value, ".json")
	}
	if value == "" || len(value) > 64 {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || (index > 0 && (char == '_' || char == '-')) {
			continue
		}
		return false
	}
	return true
}

func sanitizeDeliveryFailure(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}

func normalizePlayerUID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "")
	return value
}

func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maximumLimit {
		limit = maximumLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func deterministicOrderID(playerUID, key string) string {
	sum := sha256.Sum256([]byte(playerUID + "\x00" + key))
	return "order_" + hex.EncodeToString(sum[:16])
}

func newID(prefix, seed string) string {
	sum := sha256.Sum256([]byte(seed + time.Now().UTC().Format(time.RFC3339Nano)))
	return prefix + "_" + hex.EncodeToString(sum[:16])
}

func (s *Service) timestamp() string { return s.now().UTC().Format(time.RFC3339Nano) }
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func rollback(tx *sql.Tx) { _ = tx.Rollback() }
