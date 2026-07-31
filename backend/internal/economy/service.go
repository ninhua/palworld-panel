package economy

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
	_ "time/tzdata"
)

var (
	ErrInvalidPlayerUID    = errors.New("player uid is required")
	ErrInvalidAmount       = errors.New("amount must be greater than zero")
	ErrInvalidDelta        = errors.New("delta must not be zero")
	ErrInvalidReason       = errors.New("reason is required")
	ErrInvalidReference    = errors.New("reference id is required")
	ErrInsufficientBalance = errors.New("insufficient point balance")
	ErrReservationNotFound = errors.New("point reservation not found")
	ErrReservationSettled  = errors.New("point reservation is already settled")
)

const (
	defaultTimezone = "Asia/Shanghai"
	defaultLimit    = 50
	maximumLimit    = 500
)

type Service struct {
	db       *sql.DB
	location *time.Location
	now      func() time.Time
}

type Account struct {
	PlayerUID string `json:"player_uid"`
	Nickname  string `json:"nickname,omitempty"`
	SteamID   string `json:"steam_id,omitempty"`
	Status    string `json:"status"`
	Balance   int64  `json:"balance"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type LedgerEntry struct {
	ID            string         `json:"id"`
	PlayerUID     string         `json:"player_uid"`
	Delta         int64          `json:"delta"`
	BalanceAfter  int64          `json:"balance_after"`
	Reason        string         `json:"reason"`
	ReferenceType string         `json:"reference_type,omitempty"`
	ReferenceID   string         `json:"reference_id,omitempty"`
	Actor         string         `json:"actor,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     string         `json:"created_at"`
}

type Adjustment struct {
	PlayerUID     string
	Nickname      string
	SteamID       string
	Delta         int64
	Reason        string
	ReferenceType string
	ReferenceID   string
	Actor         string
	Metadata      map[string]any
}

type AdjustmentResult struct {
	Account        Account     `json:"account"`
	Ledger         LedgerEntry `json:"ledger"`
	AlreadyApplied bool        `json:"already_applied"`
}

type CheckinResult struct {
	Account   Account      `json:"account"`
	Awarded   bool         `json:"awarded"`
	LocalDate string       `json:"local_date"`
	Points    int64        `json:"points"`
	Ledger    *LedgerEntry `json:"ledger,omitempty"`
}

type Reservation struct {
	ID          string `json:"id"`
	PlayerUID   string `json:"player_uid"`
	Amount      int64  `json:"amount"`
	ReferenceID string `json:"reference_id"`
	Status      string `json:"status"`
	ExpiresAt   string `json:"expires_at"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type ReservationResult struct {
	Reservation Reservation `json:"reservation"`
	Account     Account     `json:"account"`
	Existing    bool        `json:"existing"`
}

type Summary struct {
	Accounts           int64  `json:"accounts"`
	TotalBalance       int64  `json:"total_balance"`
	LedgerEntries      int64  `json:"ledger_entries"`
	ActiveReservations int64  `json:"active_reservations"`
	ReservedPoints     int64  `json:"reserved_points"`
	IssuedLast24Hours  int64  `json:"issued_last_24_hours"`
	SpentLast24Hours   int64  `json:"spent_last_24_hours"`
	CheckinsToday      int64  `json:"checkins_today"`
	CheckinPointsToday int64  `json:"checkin_points_today"`
	LocalDate          string `json:"local_date"`
}

type CommandRequest struct {
	EventID   string `json:"event_id"`
	PlayerUID string `json:"player_uid"`
	Nickname  string `json:"nickname,omitempty"`
	SteamID   string `json:"steam_id,omitempty"`
	Message   string `json:"message"`
	LocalDate string `json:"local_date,omitempty"`
	Points    int64  `json:"points,omitempty"`
}

type CommandResult struct {
	EventID   string `json:"event_id,omitempty"`
	PlayerUID string `json:"player_uid,omitempty"`
	Handled   bool   `json:"handled"`
	Duplicate bool   `json:"duplicate"`
	Command   string `json:"command,omitempty"`
	Reply     string `json:"reply,omitempty"`
	Balance   int64  `json:"balance,omitempty"`
	Awarded   bool   `json:"awarded,omitempty"`
	LocalDate string `json:"local_date,omitempty"`
}

var serviceCache sync.Map

func ForPath(path, timezone string) (*Service, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("economy database path is empty")
	}
	if strings.TrimSpace(timezone) == "" {
		timezone = defaultTimezone
	}
	key := path + "\x00" + timezone
	if cached, ok := serviceCache.Load(key); ok {
		return cached.(*Service), nil
	}
	service, err := Open(path, timezone)
	if err != nil {
		return nil, err
	}
	actual, loaded := serviceCache.LoadOrStore(key, service)
	if loaded {
		_ = service.Close()
		return actual.(*Service), nil
	}
	return service, nil
}

func Open(path, timezone string) (*Service, error) {
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return nil, fmt.Errorf("load economy timezone: %w", err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open economy database: %w", err)
	}
	database.SetMaxOpenConns(1)
	service := &Service{db: database, location: location, now: time.Now}
	if err := service.configure(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := service.ensureSchema(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	if _, err := service.ReleaseExpired(context.Background()); err != nil {
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
			return fmt.Errorf("configure economy database: %w", err)
		}
	}
	return nil
}

func (s *Service) ensureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS economy_accounts (
			player_uid TEXT PRIMARY KEY,
			nickname TEXT NOT NULL DEFAULT '',
			steam_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			balance INTEGER NOT NULL DEFAULT 0 CHECK(balance >= 0),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_economy_accounts_nickname ON economy_accounts(nickname COLLATE NOCASE)`,
		`CREATE INDEX IF NOT EXISTS idx_economy_accounts_steam_id ON economy_accounts(steam_id)`,
		`CREATE TABLE IF NOT EXISTS economy_ledger (
			id TEXT PRIMARY KEY,
			player_uid TEXT NOT NULL,
			delta INTEGER NOT NULL,
			balance_after INTEGER NOT NULL CHECK(balance_after >= 0),
			reason TEXT NOT NULL,
			reference_type TEXT NOT NULL DEFAULT '',
			reference_id TEXT NOT NULL DEFAULT '',
			actor TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			FOREIGN KEY(player_uid) REFERENCES economy_accounts(player_uid) ON DELETE RESTRICT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_economy_ledger_player_created ON economy_ledger(player_uid, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_economy_ledger_created ON economy_ledger(created_at DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_economy_ledger_reference ON economy_ledger(player_uid, reference_type, reference_id) WHERE reference_id <> ''`,
		`CREATE TABLE IF NOT EXISTS economy_checkins (
			player_uid TEXT NOT NULL,
			local_date TEXT NOT NULL,
			points INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY(player_uid, local_date),
			FOREIGN KEY(player_uid) REFERENCES economy_accounts(player_uid) ON DELETE RESTRICT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_economy_checkins_date ON economy_checkins(local_date)`,
		`CREATE TABLE IF NOT EXISTS economy_reservations (
			id TEXT PRIMARY KEY,
			player_uid TEXT NOT NULL,
			amount INTEGER NOT NULL CHECK(amount > 0),
			reference_id TEXT NOT NULL,
			status TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(player_uid, reference_id),
			FOREIGN KEY(player_uid) REFERENCES economy_accounts(player_uid) ON DELETE RESTRICT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_economy_reservations_status_expiry ON economy_reservations(status, expires_at)`,
		`CREATE TABLE IF NOT EXISTS economy_command_events (
			event_id TEXT PRIMARY KEY,
			player_uid TEXT NOT NULL,
			command TEXT NOT NULL,
			response_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create economy schema: %w", err)
		}
	}
	return nil
}

func (s *Service) LocalDate() string {
	return s.now().In(s.location).Format("2006-01-02")
}

func (s *Service) EnsureAccount(ctx context.Context, playerUID, nickname, steamID string) (Account, error) {
	playerUID = normalizePlayerUID(playerUID)
	if playerUID == "" {
		return Account{}, ErrInvalidPlayerUID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer rollback(tx)
	if err := s.ensureAccountTx(ctx, tx, playerUID, nickname, steamID); err != nil {
		return Account{}, err
	}
	account, err := accountTx(ctx, tx, playerUID)
	if err != nil {
		return Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	return account, nil
}

func (s *Service) GetAccount(ctx context.Context, playerUID string) (Account, error) {
	playerUID = normalizePlayerUID(playerUID)
	if playerUID == "" {
		return Account{}, ErrInvalidPlayerUID
	}
	return scanAccount(s.db.QueryRowContext(ctx, `SELECT player_uid,nickname,steam_id,status,balance,created_at,updated_at FROM economy_accounts WHERE player_uid=?`, playerUID))
}

func (s *Service) ListAccounts(ctx context.Context, query string, limit, offset int) ([]Account, error) {
	limit = normalizeLimit(limit)
	if offset < 0 {
		offset = 0
	}
	query = strings.TrimSpace(query)
	var rows *sql.Rows
	var err error
	if query == "" {
		rows, err = s.db.QueryContext(ctx, `SELECT player_uid,nickname,steam_id,status,balance,created_at,updated_at FROM economy_accounts ORDER BY updated_at DESC, player_uid LIMIT ? OFFSET ?`, limit, offset)
	} else {
		pattern := "%" + query + "%"
		rows, err = s.db.QueryContext(ctx, `SELECT player_uid,nickname,steam_id,status,balance,created_at,updated_at FROM economy_accounts WHERE player_uid LIKE ? OR nickname LIKE ? COLLATE NOCASE OR steam_id LIKE ? ORDER BY updated_at DESC, player_uid LIMIT ? OFFSET ?`, pattern, pattern, pattern, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := make([]Account, 0)
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Service) ListLedger(ctx context.Context, playerUID string, limit, offset int) ([]LedgerEntry, error) {
	playerUID = normalizePlayerUID(playerUID)
	if playerUID == "" {
		return nil, ErrInvalidPlayerUID
	}
	limit = normalizeLimit(limit)
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,player_uid,delta,balance_after,reason,reference_type,reference_id,actor,metadata_json,created_at FROM economy_ledger WHERE player_uid=? ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?`, playerUID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]LedgerEntry, 0)
	for rows.Next() {
		entry, err := scanLedger(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Service) Adjust(ctx context.Context, request Adjustment) (AdjustmentResult, error) {
	request.PlayerUID = normalizePlayerUID(request.PlayerUID)
	request.Nickname = strings.TrimSpace(request.Nickname)
	request.SteamID = strings.TrimSpace(request.SteamID)
	request.Reason = strings.TrimSpace(request.Reason)
	request.ReferenceType = strings.TrimSpace(request.ReferenceType)
	request.ReferenceID = strings.TrimSpace(request.ReferenceID)
	request.Actor = strings.TrimSpace(request.Actor)
	if request.PlayerUID == "" {
		return AdjustmentResult{}, ErrInvalidPlayerUID
	}
	if request.Delta == 0 {
		return AdjustmentResult{}, ErrInvalidDelta
	}
	if request.Reason == "" {
		return AdjustmentResult{}, ErrInvalidReason
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AdjustmentResult{}, err
	}
	defer rollback(tx)
	if err := s.ensureAccountTx(ctx, tx, request.PlayerUID, request.Nickname, request.SteamID); err != nil {
		return AdjustmentResult{}, err
	}
	if request.ReferenceID != "" {
		entry, found, err := ledgerByReferenceTx(ctx, tx, request.PlayerUID, request.ReferenceType, request.ReferenceID)
		if err != nil {
			return AdjustmentResult{}, err
		}
		if found {
			account, err := accountTx(ctx, tx, request.PlayerUID)
			if err != nil {
				return AdjustmentResult{}, err
			}
			if err := tx.Commit(); err != nil {
				return AdjustmentResult{}, err
			}
			return AdjustmentResult{Account: account, Ledger: entry, AlreadyApplied: true}, nil
		}
	}
	entry, account, err := s.adjustTx(ctx, tx, request)
	if err != nil {
		return AdjustmentResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdjustmentResult{}, err
	}
	return AdjustmentResult{Account: account, Ledger: entry}, nil
}

func (s *Service) Checkin(ctx context.Context, playerUID, nickname, steamID, localDate string, points int64, actor string) (CheckinResult, error) {
	playerUID = normalizePlayerUID(playerUID)
	if playerUID == "" {
		return CheckinResult{}, ErrInvalidPlayerUID
	}
	if points < 0 {
		return CheckinResult{}, errors.New("checkin points must not be negative")
	}
	if strings.TrimSpace(localDate) == "" {
		localDate = s.LocalDate()
	}
	if _, err := time.Parse("2006-01-02", localDate); err != nil {
		return CheckinResult{}, errors.New("local_date must use YYYY-MM-DD")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CheckinResult{}, err
	}
	defer rollback(tx)
	if err := s.ensureAccountTx(ctx, tx, playerUID, nickname, steamID); err != nil {
		return CheckinResult{}, err
	}
	result, err := s.checkinTx(ctx, tx, playerUID, localDate, points, actor)
	if err != nil {
		return CheckinResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CheckinResult{}, err
	}
	return result, nil
}

func (s *Service) Reserve(ctx context.Context, playerUID, nickname, steamID, referenceID string, amount int64, ttl time.Duration, actor string) (ReservationResult, error) {
	playerUID = normalizePlayerUID(playerUID)
	referenceID = strings.TrimSpace(referenceID)
	if playerUID == "" {
		return ReservationResult{}, ErrInvalidPlayerUID
	}
	if referenceID == "" {
		return ReservationResult{}, ErrInvalidReference
	}
	if amount <= 0 {
		return ReservationResult{}, ErrInvalidAmount
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReservationResult{}, err
	}
	defer rollback(tx)
	if err := s.releaseExpiredTx(ctx, tx); err != nil {
		return ReservationResult{}, err
	}
	if err := s.ensureAccountTx(ctx, tx, playerUID, nickname, steamID); err != nil {
		return ReservationResult{}, err
	}
	reservation, found, err := reservationByReferenceTx(ctx, tx, playerUID, referenceID)
	if err != nil {
		return ReservationResult{}, err
	}
	if found {
		account, err := accountTx(ctx, tx, playerUID)
		if err != nil {
			return ReservationResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return ReservationResult{}, err
		}
		return ReservationResult{Reservation: reservation, Account: account, Existing: true}, nil
	}
	account, err := accountTx(ctx, tx, playerUID)
	if err != nil {
		return ReservationResult{}, err
	}
	if account.Balance < amount {
		return ReservationResult{}, ErrInsufficientBalance
	}
	now := s.timestamp()
	reservation = Reservation{
		ID:          newID(),
		PlayerUID:   playerUID,
		Amount:      amount,
		ReferenceID: referenceID,
		Status:      "reserved",
		ExpiresAt:   s.now().UTC().Add(ttl).Format(time.RFC3339Nano),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO economy_reservations(id,player_uid,amount,reference_id,status,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, reservation.ID, reservation.PlayerUID, reservation.Amount, reservation.ReferenceID, reservation.Status, reservation.ExpiresAt, reservation.CreatedAt, reservation.UpdatedAt); err != nil {
		return ReservationResult{}, err
	}
	_, account, err = s.adjustTx(ctx, tx, Adjustment{
		PlayerUID:     playerUID,
		Delta:         -amount,
		Reason:        "point_reservation",
		ReferenceType: "reservation_hold",
		ReferenceID:   reservation.ID,
		Actor:         actor,
		Metadata:      map[string]any{"business_reference": referenceID},
	})
	if err != nil {
		return ReservationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ReservationResult{}, err
	}
	return ReservationResult{Reservation: reservation, Account: account}, nil
}

func (s *Service) CommitReservation(ctx context.Context, reservationID, actor string) (ReservationResult, error) {
	return s.settleReservation(ctx, reservationID, true, actor)
}

func (s *Service) ReleaseReservation(ctx context.Context, reservationID, actor string) (ReservationResult, error) {
	return s.settleReservation(ctx, reservationID, false, actor)
}

func (s *Service) settleReservation(ctx context.Context, reservationID string, commit bool, actor string) (ReservationResult, error) {
	reservationID = strings.TrimSpace(reservationID)
	if reservationID == "" {
		return ReservationResult{}, ErrReservationNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReservationResult{}, err
	}
	defer rollback(tx)
	reservation, err := reservationByIDTx(ctx, tx, reservationID)
	if errors.Is(err, sql.ErrNoRows) {
		return ReservationResult{}, ErrReservationNotFound
	}
	if err != nil {
		return ReservationResult{}, err
	}
	if reservation.Status != "reserved" {
		account, accountErr := accountTx(ctx, tx, reservation.PlayerUID)
		if accountErr != nil {
			return ReservationResult{}, accountErr
		}
		sameOutcome := (commit && reservation.Status == "committed") || (!commit && (reservation.Status == "released" || reservation.Status == "expired"))
		if err := tx.Commit(); err != nil {
			return ReservationResult{}, err
		}
		result := ReservationResult{Reservation: reservation, Account: account, Existing: true}
		if sameOutcome {
			return result, nil
		}
		return result, ErrReservationSettled
	}
	now := s.timestamp()
	status := "committed"
	if !commit {
		status = "released"
		if _, _, err := s.adjustTx(ctx, tx, Adjustment{
			PlayerUID:     reservation.PlayerUID,
			Delta:         reservation.Amount,
			Reason:        "point_reservation_release",
			ReferenceType: "reservation_release",
			ReferenceID:   reservation.ID,
			Actor:         actor,
			Metadata:      map[string]any{"business_reference": reservation.ReferenceID},
		}); err != nil {
			return ReservationResult{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE economy_reservations SET status=?,updated_at=? WHERE id=? AND status='reserved'`, status, now, reservation.ID); err != nil {
		return ReservationResult{}, err
	}
	reservation.Status = status
	reservation.UpdatedAt = now
	account, err := accountTx(ctx, tx, reservation.PlayerUID)
	if err != nil {
		return ReservationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ReservationResult{}, err
	}
	return ReservationResult{Reservation: reservation, Account: account}, nil
}

func (s *Service) ReleaseExpired(ctx context.Context) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer rollback(tx)
	count, err := s.releaseExpiredTxCount(ctx, tx)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Service) Summary(ctx context.Context) (Summary, error) {
	localDate := s.LocalDate()
	cutoff := s.now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	var summary Summary
	summary.LocalDate = localDate
	queries := []struct {
		query string
		args  []any
		dest  *int64
	}{
		{`SELECT COUNT(*) FROM economy_accounts`, nil, &summary.Accounts},
		{`SELECT COALESCE(SUM(balance),0) FROM economy_accounts`, nil, &summary.TotalBalance},
		{`SELECT COUNT(*) FROM economy_ledger`, nil, &summary.LedgerEntries},
		{`SELECT COUNT(*) FROM economy_reservations WHERE status='reserved'`, nil, &summary.ActiveReservations},
		{`SELECT COALESCE(SUM(amount),0) FROM economy_reservations WHERE status='reserved'`, nil, &summary.ReservedPoints},
		{`SELECT COALESCE(SUM(delta),0) FROM economy_ledger WHERE delta>0 AND created_at>=?`, []any{cutoff}, &summary.IssuedLast24Hours},
		{`SELECT COALESCE(-SUM(delta),0) FROM economy_ledger WHERE delta<0 AND created_at>=?`, []any{cutoff}, &summary.SpentLast24Hours},
		{`SELECT COUNT(*) FROM economy_checkins WHERE local_date=?`, []any{localDate}, &summary.CheckinsToday},
		{`SELECT COALESCE(SUM(points),0) FROM economy_checkins WHERE local_date=?`, []any{localDate}, &summary.CheckinPointsToday},
	}
	for _, item := range queries {
		if err := s.db.QueryRowContext(ctx, item.query, item.args...).Scan(item.dest); err != nil {
			return Summary{}, err
		}
	}
	return summary, nil
}

func (s *Service) ExecuteCommand(ctx context.Context, request CommandRequest) (CommandResult, error) {
	request.EventID = strings.TrimSpace(request.EventID)
	request.PlayerUID = normalizePlayerUID(request.PlayerUID)
	request.Message = strings.TrimSpace(request.Message)
	if request.PlayerUID == "" {
		return CommandResult{}, ErrInvalidPlayerUID
	}
	if request.EventID == "" {
		request.EventID = newID()
	}
	command, handled := parseCommand(request.Message)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CommandResult{}, err
	}
	defer rollback(tx)
	if previous, found, err := commandEventTx(ctx, tx, request.EventID); err != nil {
		return CommandResult{}, err
	} else if found {
		previous.Duplicate = true
		if err := tx.Commit(); err != nil {
			return CommandResult{}, err
		}
		return previous, nil
	}
	if err := s.ensureAccountTx(ctx, tx, request.PlayerUID, request.Nickname, request.SteamID); err != nil {
		return CommandResult{}, err
	}
	result := CommandResult{EventID: request.EventID, PlayerUID: request.PlayerUID, Handled: handled, Command: command}
	switch command {
	case "checkin":
		points := request.Points
		if points <= 0 {
			points = 10
		}
		localDate := strings.TrimSpace(request.LocalDate)
		if localDate == "" {
			localDate = s.LocalDate()
		}
		checkin, err := s.checkinTx(ctx, tx, request.PlayerUID, localDate, points, "game-command")
		if err != nil {
			return CommandResult{}, err
		}
		result.Awarded = checkin.Awarded
		result.Balance = checkin.Account.Balance
		result.LocalDate = localDate
		if checkin.Awarded {
			result.Reply = fmt.Sprintf("签到成功，获得 %d 积分。当前积分：%d。", points, checkin.Account.Balance)
		} else {
			result.Reply = fmt.Sprintf("今天已经签到过了。当前积分：%d。", checkin.Account.Balance)
		}
	case "points":
		account, err := accountTx(ctx, tx, request.PlayerUID)
		if err != nil {
			return CommandResult{}, err
		}
		result.Balance = account.Balance
		result.Reply = fmt.Sprintf("当前积分：%d。", account.Balance)
	case "help":
		account, err := accountTx(ctx, tx, request.PlayerUID)
		if err != nil {
			return CommandResult{}, err
		}
		result.Balance = account.Balance
		result.Reply = "可用命令：!签到、!积分、!帮助。"
	default:
		result.Reply = ""
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return CommandResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO economy_command_events(event_id,player_uid,command,response_json,created_at) VALUES(?,?,?,?,?)`, request.EventID, request.PlayerUID, command, string(payload), s.timestamp()); err != nil {
		return CommandResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CommandResult{}, err
	}
	return result, nil
}

func (s *Service) checkinTx(ctx context.Context, tx *sql.Tx, playerUID, localDate string, points int64, actor string) (CheckinResult, error) {
	if _, err := time.Parse("2006-01-02", localDate); err != nil {
		return CheckinResult{}, errors.New("local_date must use YYYY-MM-DD")
	}
	now := s.timestamp()
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO economy_checkins(player_uid,local_date,points,created_at) VALUES(?,?,?,?)`, playerUID, localDate, points, now)
	if err != nil {
		return CheckinResult{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return CheckinResult{}, err
	}
	checkin := CheckinResult{Awarded: rows == 1, LocalDate: localDate, Points: points}
	if rows == 1 && points > 0 {
		entry, account, err := s.adjustTx(ctx, tx, Adjustment{
			PlayerUID:     playerUID,
			Delta:         points,
			Reason:        "daily_checkin",
			ReferenceType: "daily_checkin",
			ReferenceID:   localDate,
			Actor:         actor,
		})
		if err != nil {
			return CheckinResult{}, err
		}
		checkin.Account = account
		checkin.Ledger = &entry
		return checkin, nil
	}
	account, err := accountTx(ctx, tx, playerUID)
	if err != nil {
		return CheckinResult{}, err
	}
	checkin.Account = account
	return checkin, nil
}

func (s *Service) adjustTx(ctx context.Context, tx *sql.Tx, request Adjustment) (LedgerEntry, Account, error) {
	account, err := accountTx(ctx, tx, request.PlayerUID)
	if err != nil {
		return LedgerEntry{}, Account{}, err
	}
	next := account.Balance + request.Delta
	if next < 0 {
		return LedgerEntry{}, Account{}, ErrInsufficientBalance
	}
	metadata := request.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return LedgerEntry{}, Account{}, fmt.Errorf("encode economy metadata: %w", err)
	}
	now := s.timestamp()
	entry := LedgerEntry{
		ID:            newID(),
		PlayerUID:     request.PlayerUID,
		Delta:         request.Delta,
		BalanceAfter:  next,
		Reason:        request.Reason,
		ReferenceType: request.ReferenceType,
		ReferenceID:   request.ReferenceID,
		Actor:         request.Actor,
		Metadata:      metadata,
		CreatedAt:     now,
	}
	if _, err := tx.ExecContext(ctx, `UPDATE economy_accounts SET balance=?,updated_at=? WHERE player_uid=?`, next, now, request.PlayerUID); err != nil {
		return LedgerEntry{}, Account{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO economy_ledger(id,player_uid,delta,balance_after,reason,reference_type,reference_id,actor,metadata_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, entry.ID, entry.PlayerUID, entry.Delta, entry.BalanceAfter, entry.Reason, entry.ReferenceType, entry.ReferenceID, entry.Actor, string(metadataJSON), entry.CreatedAt); err != nil {
		return LedgerEntry{}, Account{}, err
	}
	account.Balance = next
	account.UpdatedAt = now
	return entry, account, nil
}

func (s *Service) ensureAccountTx(ctx context.Context, tx *sql.Tx, playerUID, nickname, steamID string) error {
	now := s.timestamp()
	_, err := tx.ExecContext(ctx, `INSERT INTO economy_accounts(player_uid,nickname,steam_id,status,balance,created_at,updated_at) VALUES(?,?,?,'active',0,?,?) ON CONFLICT(player_uid) DO UPDATE SET nickname=CASE WHEN excluded.nickname<>'' THEN excluded.nickname ELSE economy_accounts.nickname END,steam_id=CASE WHEN excluded.steam_id<>'' THEN excluded.steam_id ELSE economy_accounts.steam_id END,updated_at=excluded.updated_at`, playerUID, strings.TrimSpace(nickname), strings.TrimSpace(steamID), now, now)
	return err
}

func (s *Service) releaseExpiredTx(ctx context.Context, tx *sql.Tx) error {
	_, err := s.releaseExpiredTxCount(ctx, tx)
	return err
}

func (s *Service) releaseExpiredTxCount(ctx context.Context, tx *sql.Tx) (int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,player_uid,amount,reference_id,status,expires_at,created_at,updated_at FROM economy_reservations WHERE status='reserved' AND expires_at<=? ORDER BY expires_at,id`, s.timestamp())
	if err != nil {
		return 0, err
	}
	reservations := make([]Reservation, 0)
	for rows.Next() {
		reservation, err := scanReservation(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		reservations = append(reservations, reservation)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, reservation := range reservations {
		if _, _, err := s.adjustTx(ctx, tx, Adjustment{
			PlayerUID:     reservation.PlayerUID,
			Delta:         reservation.Amount,
			Reason:        "point_reservation_expired",
			ReferenceType: "reservation_expiry",
			ReferenceID:   reservation.ID,
			Actor:         "system",
			Metadata:      map[string]any{"business_reference": reservation.ReferenceID},
		}); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE economy_reservations SET status='expired',updated_at=? WHERE id=? AND status='reserved'`, s.timestamp(), reservation.ID); err != nil {
			return 0, err
		}
	}
	return int64(len(reservations)), nil
}

func accountTx(ctx context.Context, tx *sql.Tx, playerUID string) (Account, error) {
	return scanAccount(tx.QueryRowContext(ctx, `SELECT player_uid,nickname,steam_id,status,balance,created_at,updated_at FROM economy_accounts WHERE player_uid=?`, playerUID))
}

func ledgerByReferenceTx(ctx context.Context, tx *sql.Tx, playerUID, referenceType, referenceID string) (LedgerEntry, bool, error) {
	entry, err := scanLedger(tx.QueryRowContext(ctx, `SELECT id,player_uid,delta,balance_after,reason,reference_type,reference_id,actor,metadata_json,created_at FROM economy_ledger WHERE player_uid=? AND reference_type=? AND reference_id=?`, playerUID, referenceType, referenceID))
	if errors.Is(err, sql.ErrNoRows) {
		return LedgerEntry{}, false, nil
	}
	return entry, err == nil, err
}

func reservationByReferenceTx(ctx context.Context, tx *sql.Tx, playerUID, referenceID string) (Reservation, bool, error) {
	reservation, err := scanReservation(tx.QueryRowContext(ctx, `SELECT id,player_uid,amount,reference_id,status,expires_at,created_at,updated_at FROM economy_reservations WHERE player_uid=? AND reference_id=?`, playerUID, referenceID))
	if errors.Is(err, sql.ErrNoRows) {
		return Reservation{}, false, nil
	}
	return reservation, err == nil, err
}

func reservationByIDTx(ctx context.Context, tx *sql.Tx, id string) (Reservation, error) {
	return scanReservation(tx.QueryRowContext(ctx, `SELECT id,player_uid,amount,reference_id,status,expires_at,created_at,updated_at FROM economy_reservations WHERE id=?`, id))
}

func commandEventTx(ctx context.Context, tx *sql.Tx, eventID string) (CommandResult, bool, error) {
	var payload string
	err := tx.QueryRowContext(ctx, `SELECT response_json FROM economy_command_events WHERE event_id=?`, eventID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return CommandResult{}, false, nil
	}
	if err != nil {
		return CommandResult{}, false, err
	}
	var result CommandResult
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		return CommandResult{}, false, err
	}
	return result, true, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAccount(row scanner) (Account, error) {
	var account Account
	err := row.Scan(&account.PlayerUID, &account.Nickname, &account.SteamID, &account.Status, &account.Balance, &account.CreatedAt, &account.UpdatedAt)
	return account, err
}

func scanLedger(row scanner) (LedgerEntry, error) {
	var entry LedgerEntry
	var metadataJSON string
	if err := row.Scan(&entry.ID, &entry.PlayerUID, &entry.Delta, &entry.BalanceAfter, &entry.Reason, &entry.ReferenceType, &entry.ReferenceID, &entry.Actor, &metadataJSON, &entry.CreatedAt); err != nil {
		return LedgerEntry{}, err
	}
	entry.Metadata = map[string]any{}
	if strings.TrimSpace(metadataJSON) != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &entry.Metadata); err != nil {
			return LedgerEntry{}, err
		}
	}
	return entry, nil
}

func scanReservation(row scanner) (Reservation, error) {
	var reservation Reservation
	err := row.Scan(&reservation.ID, &reservation.PlayerUID, &reservation.Amount, &reservation.ReferenceID, &reservation.Status, &reservation.ExpiresAt, &reservation.CreatedAt, &reservation.UpdatedAt)
	return reservation, err
}

func parseCommand(message string) (string, bool) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", false
	}
	fields := strings.Fields(message)
	if len(fields) == 0 {
		return "", false
	}
	command := strings.ToLower(strings.TrimSpace(fields[0]))
	command = strings.TrimLeft(command, "!！/／")
	switch command {
	case "签到", "qd", "checkin":
		return "checkin", true
	case "积分", "jf", "points", "point":
		return "points", true
	case "帮助", "help", "菜单", "menu":
		return "help", true
	default:
		return command, false
	}
}

func normalizePlayerUID(value string) string {
	return strings.TrimSpace(value)
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maximumLimit {
		return maximumLimit
	}
	return limit
}

func (s *Service) timestamp() string {
	return s.now().UTC().Format(time.RFC3339Nano)
}

func newID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}
