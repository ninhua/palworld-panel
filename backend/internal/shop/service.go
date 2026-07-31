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
	ErrInvalidProduct      = errors.New("shop product is invalid")
	ErrProductNotFound     = errors.New("shop product not found")
	ErrProductDisabled     = errors.New("shop product is disabled")
	ErrInvalidQuantity     = errors.New("quantity must be between 1 and 1000")
	ErrInsufficientStock   = errors.New("shop product stock is insufficient")
	ErrPlayerLimit         = errors.New("shop product per-player limit exceeded")
	ErrInvalidOrder        = errors.New("shop order is invalid")
	ErrOrderNotFound       = errors.New("shop order not found")
	ErrOrderSettled        = errors.New("shop order is already settled")
	ErrReservationConflict = errors.New("shop order reservation state conflicts with the order")
)

const (
	defaultLimit        = 50
	maximumLimit        = 500
	orderReservationTTL = 3650 * 24 * time.Hour
)

type Economy interface {
	GetAccount(context.Context, string) (economy.Account, error)
	Reserve(context.Context, string, string, string, string, int64, time.Duration, string) (economy.ReservationResult, error)
	CommitReservation(context.Context, string, string) (economy.ReservationResult, error)
	ReleaseReservation(context.Context, string, string) (economy.ReservationResult, error)
}

type Service struct {
	db  *sql.DB
	now func() time.Time
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
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotency_key"`
	ProductID      string         `json:"product_id"`
	ProductName    string         `json:"product_name"`
	PlayerUID      string         `json:"player_uid"`
	Nickname       string         `json:"nickname,omitempty"`
	SteamID        string         `json:"steam_id,omitempty"`
	Quantity       int64          `json:"quantity"`
	UnitPrice      int64          `json:"unit_price"`
	TotalPoints    int64          `json:"total_points"`
	Status         string         `json:"status"`
	ReservationID  string         `json:"reservation_id"`
	DeliveryMode   string         `json:"delivery_mode"`
	Payload        map[string]any `json:"payload,omitempty"`
	Failure        string         `json:"failure,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	DeliveredAt    string         `json:"delivered_at,omitempty"`
	CancelledAt    string         `json:"cancelled_at,omitempty"`
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
	Products        int64 `json:"products"`
	EnabledProducts int64 `json:"enabled_products"`
	PendingOrders   int64 `json:"pending_orders"`
	DeliveredOrders int64 `json:"delivered_orders"`
	SpentPoints     int64 `json:"spent_points"`
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
		`CREATE TABLE IF NOT EXISTS shop_products (
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_products_enabled ON shop_products(enabled,updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS shop_orders (
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_orders_created ON shop_orders(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_orders_player ON shop_orders(player_uid,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_orders_product ON shop_orders(product_id,status)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure shop schema: %w", err)
		}
	}
	return nil
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
		DeliveryMode: product.DeliveryMode, Payload: product.Payload, CreatedAt: s.timestamp(), UpdatedAt: s.timestamp(),
	}
	if err := s.insertOrder(ctx, order, product); err != nil {
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

func (s *Service) insertOrder(ctx context.Context, order Order, product Product) error {
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
	_, err = tx.ExecContext(ctx, `INSERT INTO shop_orders(id,idempotency_key,product_id,product_name,player_uid,nickname,steam_id,quantity,unit_price,total_points,status,reservation_id,delivery_mode,payload_json,failure,created_at,updated_at,delivered_at,cancelled_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		order.ID, order.IdempotencyKey, order.ProductID, order.ProductName, order.PlayerUID, order.Nickname, order.SteamID,
		order.Quantity, order.UnitPrice, order.TotalPoints, order.Status, order.ReservationID, order.DeliveryMode, string(payload), order.Failure,
		order.CreatedAt, order.UpdatedAt, order.DeliveredAt, order.CancelledAt)
	if err != nil {
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
	now := s.timestamp()
	result, err := s.db.ExecContext(ctx, `UPDATE shop_orders SET status='delivered',delivered_at=?,updated_at=?,failure='' WHERE id=? AND status='pending'`, now, now, order.ID)
	if err != nil {
		return OrderResult{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return OrderResult{}, ErrOrderSettled
	}
	order.Status, order.DeliveredAt, order.UpdatedAt, order.Failure = "delivered", now, now, ""
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
	result, err := tx.ExecContext(ctx, `UPDATE shop_orders SET status='cancelled',cancelled_at=?,updated_at=? WHERE id=? AND status='pending'`, now, now, order.ID)
	if err != nil {
		return OrderResult{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return OrderResult{}, ErrOrderSettled
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shop_products SET stock=CASE WHEN stock<0 THEN stock ELSE stock+? END,updated_at=? WHERE id=?`, order.Quantity, now, order.ProductID); err != nil {
		return OrderResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return OrderResult{}, err
	}
	order.Status, order.CancelledAt, order.UpdatedAt = "cancelled", now, now
	return OrderResult{Order: order, Account: reservation.Account}, nil
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
	limit, offset = normalizePage(limit, offset)
	conditions := []string{"1=1"}
	args := []any{}
	status = strings.TrimSpace(status)
	if status != "" {
		conditions = append(conditions, "status=?")
		args = append(args, status)
	}
	playerUID = normalizePlayerUID(playerUID)
	if playerUID != "" {
		conditions = append(conditions, "player_uid=?")
		args = append(args, playerUID)
	}
	args = append(args, limit, offset)
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

func (s *Service) Summary(ctx context.Context) (Summary, error) {
	var result Summary
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN enabled=1 THEN 1 ELSE 0 END),0) FROM shop_products`).Scan(&result.Products, &result.EnabledProducts); err != nil {
		return Summary{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='delivered' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='delivered' THEN total_points ELSE 0 END),0) FROM shop_orders`).Scan(&result.PendingOrders, &result.DeliveredOrders, &result.SpentPoints); err != nil {
		return Summary{}, err
	}
	return result, nil
}

func (s *Service) orderByIdempotency(ctx context.Context, playerUID, key string) (Order, error) {
	return scanOrder(s.db.QueryRowContext(ctx, orderSelect+` WHERE player_uid=? AND idempotency_key=?`, playerUID, key))
}

const orderSelect = `SELECT id,idempotency_key,product_id,product_name,player_uid,nickname,steam_id,quantity,unit_price,total_points,status,reservation_id,delivery_mode,payload_json,failure,created_at,updated_at,delivered_at,cancelled_at FROM shop_orders`

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
	var payload string
	err := row.Scan(&order.ID, &order.IdempotencyKey, &order.ProductID, &order.ProductName, &order.PlayerUID, &order.Nickname, &order.SteamID,
		&order.Quantity, &order.UnitPrice, &order.TotalPoints, &order.Status, &order.ReservationID, &order.DeliveryMode, &payload, &order.Failure,
		&order.CreatedAt, &order.UpdatedAt, &order.DeliveredAt, &order.CancelledAt)
	if err != nil {
		return Order{}, err
	}
	_ = json.Unmarshal([]byte(payload), &order.Payload)
	if order.Payload == nil {
		order.Payload = map[string]any{}
	}
	return order, nil
}

func normalizeProductInput(input ProductInput) (ProductInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.DeliveryMode = strings.TrimSpace(strings.ToLower(input.DeliveryMode))
	if input.DeliveryMode == "" {
		input.DeliveryMode = "manual"
	}
	if input.Name == "" || len([]rune(input.Name)) > 80 || len([]rune(input.Description)) > 500 || input.Price <= 0 || input.Price > 1_000_000_000 || input.Stock < -1 || input.PerPlayerLimit < 0 || input.DeliveryMode != "manual" {
		return ProductInput{}, ErrInvalidProduct
	}
	if input.Payload == nil {
		input.Payload = map[string]any{}
	}
	payload, err := json.Marshal(input.Payload)
	if err != nil || len(payload) > 64*1024 {
		return ProductInput{}, ErrInvalidProduct
	}
	return input, nil
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
