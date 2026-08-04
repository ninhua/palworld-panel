package shop

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"palpanel/internal/economy"
)

type shopAccountResolver interface {
	ResolveShopAccount(context.Context, string, string, string) (economy.ShopAccountResolution, error)
}

type RedemptionAttempt struct {
	ID                 string `json:"id"`
	Source             string `json:"source"`
	EventID            string `json:"event_id,omitempty"`
	Actor              string `json:"actor,omitempty"`
	RequestedPlayerUID string `json:"requested_player_uid,omitempty"`
	RequestedSteamID   string `json:"requested_steam_id,omitempty"`
	ResolvedPlayerUID  string `json:"resolved_player_uid,omitempty"`
	AccountMatch       string `json:"account_match,omitempty"`
	AccountWarning     string `json:"account_warning,omitempty"`
	ProductID          string `json:"product_id,omitempty"`
	ProductName        string `json:"product_name,omitempty"`
	Quantity           int64  `json:"quantity"`
	UnitPrice          int64  `json:"unit_price"`
	RequiredPoints     int64  `json:"required_points"`
	AvailablePoints    int64  `json:"available_points"`
	ReservedPoints     int64  `json:"reserved_points"`
	Result             string `json:"result"`
	FailureCode        string `json:"failure_code,omitempty"`
	Failure            string `json:"failure,omitempty"`
	OrderID            string `json:"order_id,omitempty"`
	CreatedAt          string `json:"created_at"`
}

type RedemptionAttemptFilter struct {
	Result    string
	PlayerUID string
	Limit     int
	Offset    int
}

type RedemptionFailure struct {
	Cause   error
	Attempt RedemptionAttempt
}

func (e *RedemptionFailure) Error() string {
	if e == nil {
		return "shop redemption failed"
	}
	return fmt.Sprintf("%v [diagnostic=%s required=%d available=%d reserved=%d requested_player_uid=%s resolved_player_uid=%s account_match=%s]",
		e.Cause, e.Attempt.ID, e.Attempt.RequiredPoints, e.Attempt.AvailablePoints, e.Attempt.ReservedPoints,
		e.Attempt.RequestedPlayerUID, e.Attempt.ResolvedPlayerUID, e.Attempt.AccountMatch)
}

func (e *RedemptionFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (s *Service) ensureRedemptionDiagnosticsSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS shop_redemption_attempts (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL DEFAULT '',
			event_id TEXT NOT NULL DEFAULT '',
			actor TEXT NOT NULL DEFAULT '',
			requested_player_uid TEXT NOT NULL DEFAULT '',
			requested_steam_id TEXT NOT NULL DEFAULT '',
			resolved_player_uid TEXT NOT NULL DEFAULT '',
			account_match TEXT NOT NULL DEFAULT '',
			account_warning TEXT NOT NULL DEFAULT '',
			product_id TEXT NOT NULL DEFAULT '',
			product_name TEXT NOT NULL DEFAULT '',
			quantity INTEGER NOT NULL DEFAULT 0,
			unit_price INTEGER NOT NULL DEFAULT 0,
			required_points INTEGER NOT NULL DEFAULT 0,
			available_points INTEGER NOT NULL DEFAULT 0,
			reserved_points INTEGER NOT NULL DEFAULT 0,
			result TEXT NOT NULL,
			failure_code TEXT NOT NULL DEFAULT '',
			failure TEXT NOT NULL DEFAULT '',
			order_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_redemption_attempts_created ON shop_redemption_attempts(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_redemption_attempts_result ON shop_redemption_attempts(result,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_shop_redemption_attempts_player ON shop_redemption_attempts(resolved_player_uid,created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure shop redemption diagnostics: %w", err)
		}
	}
	return nil
}

func ResolvePlayerAccount(ctx context.Context, ledger Economy, playerUID, nickname, steamID string) (economy.ShopAccountResolution, error) {
	resolver, ok := ledger.(shopAccountResolver)
	if !ok {
		account, err := ledger.GetAccount(ctx, playerUID)
		if err != nil {
			return economy.ShopAccountResolution{}, err
		}
		return economy.ShopAccountResolution{
			RequestedPlayerUID: playerUID,
			RequestedSteamID:   steamID,
			CanonicalPlayerUID: account.PlayerUID,
			MatchStrategy:      "player_uid_legacy",
			Account:            account,
		}, nil
	}
	return resolver.ResolveShopAccount(ctx, playerUID, nickname, steamID)
}

func (s *Service) CreateOrderWithDiagnostics(ctx context.Context, ledger Economy, request CreateOrderRequest, source, eventID, actor string) (OrderResult, RedemptionAttempt, error) {
	attempt := RedemptionAttempt{
		ID:                 newID("redemption", strings.TrimSpace(eventID)+strings.TrimSpace(request.PlayerUID)+strings.TrimSpace(request.ProductID)),
		Source:             strings.TrimSpace(source),
		EventID:            strings.TrimSpace(eventID),
		Actor:              strings.TrimSpace(actor),
		RequestedPlayerUID: strings.TrimSpace(request.PlayerUID),
		RequestedSteamID:   strings.TrimSpace(request.SteamID),
		ProductID:          strings.TrimSpace(request.ProductID),
		Quantity:           request.Quantity,
		Result:             "rejected",
		CreatedAt:          s.timestamp(),
	}
	if attempt.Source == "" {
		attempt.Source = "panel"
	}
	if err := s.ensureRedemptionDiagnosticsSchema(ctx); err != nil {
		return OrderResult{}, attempt, err
	}

	if product, err := s.GetProduct(ctx, request.ProductID); err == nil {
		attempt.ProductID = product.ID
		attempt.ProductName = product.Name
		attempt.UnitPrice = product.Price
		if request.Quantity > 0 && product.Price <= (1<<63-1)/request.Quantity {
			attempt.RequiredPoints = product.Price * request.Quantity
		}
	}

	resolution, resolveErr := ResolvePlayerAccount(ctx, ledger, request.PlayerUID, request.Nickname, request.SteamID)
	if resolveErr != nil {
		attempt.FailureCode = redemptionFailureCode(resolveErr)
		attempt.Failure = resolveErr.Error()
		_ = s.saveRedemptionAttempt(ctx, attempt)
		return OrderResult{}, attempt, &RedemptionFailure{Cause: resolveErr, Attempt: attempt}
	}
	attempt.ResolvedPlayerUID = resolution.Account.PlayerUID
	attempt.AccountMatch = resolution.MatchStrategy
	attempt.AccountWarning = resolution.Warning
	attempt.AvailablePoints = resolution.Account.Balance
	attempt.ReservedPoints = resolution.ReservedPoints
	request.PlayerUID = resolution.Account.PlayerUID

	result, err := s.CreateOrder(ctx, ledger, request, actor)
	if err != nil {
		if account, accountErr := ledger.GetAccount(ctx, request.PlayerUID); accountErr == nil {
			attempt.AvailablePoints = account.Balance
		}
		attempt.FailureCode = redemptionFailureCode(err)
		attempt.Failure = err.Error()
		_ = s.saveRedemptionAttempt(ctx, attempt)
		return OrderResult{}, attempt, &RedemptionFailure{Cause: err, Attempt: attempt}
	}

	attempt.OrderID = result.Order.ID
	attempt.ProductID = result.Order.ProductID
	attempt.ProductName = result.Order.ProductName
	attempt.Quantity = result.Order.Quantity
	attempt.UnitPrice = result.Order.UnitPrice
	attempt.RequiredPoints = result.Order.TotalPoints
	attempt.AvailablePoints = result.Account.Balance
	attempt.Result = "order_created"
	if result.Duplicate {
		attempt.Result = "duplicate"
	}
	if err := s.saveRedemptionAttempt(ctx, attempt); err != nil {
		return OrderResult{}, attempt, err
	}
	return result, attempt, nil
}

func (s *Service) saveRedemptionAttempt(ctx context.Context, item RedemptionAttempt) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO shop_redemption_attempts(
		id,source,event_id,actor,requested_player_uid,requested_steam_id,resolved_player_uid,account_match,account_warning,
		product_id,product_name,quantity,unit_price,required_points,available_points,reserved_points,result,failure_code,failure,order_id,created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.Source, item.EventID, item.Actor, item.RequestedPlayerUID, item.RequestedSteamID, item.ResolvedPlayerUID,
		item.AccountMatch, item.AccountWarning, item.ProductID, item.ProductName, item.Quantity, item.UnitPrice, item.RequiredPoints,
		item.AvailablePoints, item.ReservedPoints, item.Result, item.FailureCode, item.Failure, item.OrderID, item.CreatedAt)
	return err
}

func (s *Service) ListRedemptionAttempts(ctx context.Context, filter RedemptionAttemptFilter) ([]RedemptionAttempt, error) {
	if err := s.ensureRedemptionDiagnosticsSchema(ctx); err != nil {
		return nil, err
	}
	filter.Limit, filter.Offset = normalizePage(filter.Limit, filter.Offset)
	conditions := []string{"1=1"}
	args := []any{}
	if result := strings.TrimSpace(filter.Result); result != "" {
		conditions = append(conditions, "result=?")
		args = append(args, result)
	}
	if playerUID := strings.TrimSpace(filter.PlayerUID); playerUID != "" {
		conditions = append(conditions, "(resolved_player_uid=? COLLATE NOCASE OR requested_player_uid=? COLLATE NOCASE OR requested_steam_id=? COLLATE NOCASE)")
		args = append(args, playerUID, playerUID, playerUID)
	}
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,event_id,actor,requested_player_uid,requested_steam_id,resolved_player_uid,account_match,account_warning,
		product_id,product_name,quantity,unit_price,required_points,available_points,reserved_points,result,failure_code,failure,order_id,created_at
		FROM shop_redemption_attempts WHERE `+strings.Join(conditions, " AND ")+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RedemptionAttempt, 0)
	for rows.Next() {
		var item RedemptionAttempt
		if err := rows.Scan(&item.ID, &item.Source, &item.EventID, &item.Actor, &item.RequestedPlayerUID, &item.RequestedSteamID,
			&item.ResolvedPlayerUID, &item.AccountMatch, &item.AccountWarning, &item.ProductID, &item.ProductName, &item.Quantity,
			&item.UnitPrice, &item.RequiredPoints, &item.AvailablePoints, &item.ReservedPoints, &item.Result, &item.FailureCode,
			&item.Failure, &item.OrderID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func redemptionFailureCode(err error) string {
	switch {
	case errors.Is(err, economy.ErrInsufficientBalance):
		return "insufficient_balance"
	case errors.Is(err, economy.ErrShopAccountIdentityAmbiguous):
		return "account_identity_ambiguous"
	case errors.Is(err, ErrProductNotFound):
		return "product_not_found"
	case errors.Is(err, ErrProductDisabled):
		return "product_disabled"
	case errors.Is(err, ErrInsufficientStock):
		return "insufficient_stock"
	case errors.Is(err, ErrPlayerLimit):
		return "player_limit"
	case errors.Is(err, ErrInvalidQuantity):
		return "invalid_quantity"
	case errors.Is(err, ErrInvalidProduct):
		return "invalid_product"
	case errors.Is(err, ErrReservationConflict):
		return "reservation_conflict"
	case errors.Is(err, sql.ErrNoRows):
		return "resource_not_found"
	default:
		return "operation_failed"
	}
}
