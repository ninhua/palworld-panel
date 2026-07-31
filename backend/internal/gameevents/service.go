package gameevents

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
)

var (
	ErrInvalidEvent = errors.New("game event is invalid")
	serviceCache    sync.Map
)

type Service struct {
	db  *sql.DB
	now func() time.Time
}

type Event struct {
	EventID    string         `json:"event_id"`
	Type       string         `json:"type"`
	PlayerUID  string         `json:"player_uid,omitempty"`
	Nickname   string         `json:"nickname,omitempty"`
	SteamID    string         `json:"steam_id,omitempty"`
	OccurredAt string         `json:"occurred_at,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type Record struct {
	EventID    string         `json:"event_id"`
	Type       string         `json:"type"`
	PlayerUID  string         `json:"player_uid,omitempty"`
	Nickname   string         `json:"nickname,omitempty"`
	SteamID    string         `json:"steam_id,omitempty"`
	OccurredAt string         `json:"occurred_at,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
	Status     string         `json:"status"`
	Result     map[string]any `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
	Attempts   int            `json:"attempts"`
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
}

type ClaimResult struct {
	Record    Record `json:"record"`
	Duplicate bool   `json:"duplicate"`
	Retry     bool   `json:"retry"`
}

func ForPath(path string) (*Service, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("game event database path is empty")
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
		return nil, fmt.Errorf("open game event database: %w", err)
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
			return fmt.Errorf("configure game event database: %w", err)
		}
	}
	return nil
}

func (s *Service) ensureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS game_events (
			event_id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			player_uid TEXT NOT NULL DEFAULT '',
			nickname TEXT NOT NULL DEFAULT '',
			steam_id TEXT NOT NULL DEFAULT '',
			occurred_at TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL,
			result_json TEXT NOT NULL DEFAULT '{}',
			error TEXT NOT NULL DEFAULT '',
			attempts INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_game_events_created ON game_events(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_game_events_type_created ON game_events(type, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_game_events_player_created ON game_events(player_uid, created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create game event schema: %w", err)
		}
	}
	return nil
}

func (s *Service) Claim(ctx context.Context, event Event) (ClaimResult, error) {
	event = normalizeEvent(event)
	if err := validateEvent(event); err != nil {
		return ClaimResult{}, err
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return ClaimResult{}, fmt.Errorf("encode game event payload: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ClaimResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := s.timestamp()
	_, err = tx.ExecContext(ctx, `INSERT INTO game_events(event_id,type,player_uid,nickname,steam_id,occurred_at,payload_json,status,result_json,error,attempts,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'processing','{}','',1,?,?)`, event.EventID, event.Type, event.PlayerUID, event.Nickname, event.SteamID, event.OccurredAt, string(payload), now, now)
	if err == nil {
		record, readErr := getTx(ctx, tx, event.EventID)
		if readErr != nil {
			return ClaimResult{}, readErr
		}
		if err := tx.Commit(); err != nil {
			return ClaimResult{}, err
		}
		return ClaimResult{Record: record}, nil
	}
	if !isConstraintError(err) {
		return ClaimResult{}, err
	}
	record, err := getTx(ctx, tx, event.EventID)
	if err != nil {
		return ClaimResult{}, err
	}
	if record.Status != "failed" {
		if err := tx.Commit(); err != nil {
			return ClaimResult{}, err
		}
		return ClaimResult{Record: record, Duplicate: true}, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE game_events SET status='processing',error='',attempts=attempts+1,updated_at=? WHERE event_id=?`, now, event.EventID); err != nil {
		return ClaimResult{}, err
	}
	record, err = getTx(ctx, tx, event.EventID)
	if err != nil {
		return ClaimResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ClaimResult{}, err
	}
	return ClaimResult{Record: record, Retry: true}, nil
}

func (s *Service) Complete(ctx context.Context, eventID string, result map[string]any) (Record, error) {
	return s.finish(ctx, eventID, "completed", result, "")
}

func (s *Service) Fail(ctx context.Context, eventID string, eventError error) (Record, error) {
	message := "game event processing failed"
	if eventError != nil {
		message = eventError.Error()
	}
	return s.finish(ctx, eventID, "failed", nil, message)
}

func (s *Service) finish(ctx context.Context, eventID, status string, result map[string]any, message string) (Record, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return Record{}, ErrInvalidEvent
	}
	if result == nil {
		result = map[string]any{}
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return Record{}, fmt.Errorf("encode game event result: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE game_events SET status=?,result_json=?,error=?,updated_at=? WHERE event_id=?`, status, string(payload), message, s.timestamp(), eventID); err != nil {
		return Record{}, err
	}
	return s.Get(ctx, eventID)
}

func (s *Service) Get(ctx context.Context, eventID string) (Record, error) {
	return scanRecord(s.db.QueryRowContext(ctx, `SELECT event_id,type,player_uid,nickname,steam_id,occurred_at,payload_json,status,result_json,error,attempts,created_at,updated_at FROM game_events WHERE event_id=?`, strings.TrimSpace(eventID)))
}

func (s *Service) List(ctx context.Context, eventType, playerUID string, limit, offset int) ([]Record, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	eventType = strings.ToUpper(strings.TrimSpace(eventType))
	playerUID = strings.TrimSpace(playerUID)
	query := `SELECT event_id,type,player_uid,nickname,steam_id,occurred_at,payload_json,status,result_json,error,attempts,created_at,updated_at FROM game_events WHERE (?='' OR type=?) AND (?='' OR player_uid=?) ORDER BY created_at DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, eventType, eventType, playerUID, playerUID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Record, 0)
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

func getTx(ctx context.Context, tx *sql.Tx, eventID string) (Record, error) {
	return scanRecord(tx.QueryRowContext(ctx, `SELECT event_id,type,player_uid,nickname,steam_id,occurred_at,payload_json,status,result_json,error,attempts,created_at,updated_at FROM game_events WHERE event_id=?`, eventID))
}

type scanner interface{ Scan(...any) error }

func scanRecord(row scanner) (Record, error) {
	var record Record
	var payloadJSON, resultJSON string
	err := row.Scan(&record.EventID, &record.Type, &record.PlayerUID, &record.Nickname, &record.SteamID, &record.OccurredAt, &payloadJSON, &record.Status, &resultJSON, &record.Error, &record.Attempts, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		return Record{}, err
	}
	_ = json.Unmarshal([]byte(payloadJSON), &record.Payload)
	_ = json.Unmarshal([]byte(resultJSON), &record.Result)
	if record.Payload == nil {
		record.Payload = map[string]any{}
	}
	if record.Result == nil {
		record.Result = map[string]any{}
	}
	return record, nil
}

func normalizeEvent(event Event) Event {
	event.EventID = strings.TrimSpace(event.EventID)
	if event.EventID == "" {
		event.EventID = newID()
	}
	event.Type = strings.ToUpper(strings.TrimSpace(event.Type))
	event.PlayerUID = strings.TrimSpace(event.PlayerUID)
	event.Nickname = strings.TrimSpace(event.Nickname)
	event.SteamID = strings.TrimSpace(event.SteamID)
	event.OccurredAt = strings.TrimSpace(event.OccurredAt)
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	return event
}

func validateEvent(event Event) error {
	if event.EventID == "" || len(event.EventID) > 160 || event.Type == "" || len(event.Type) > 64 {
		return ErrInvalidEvent
	}
	if event.Type == "PLAYER_CHAT" && event.PlayerUID == "" {
		return ErrInvalidEvent
	}
	if event.OccurredAt != "" {
		if _, err := time.Parse(time.RFC3339, event.OccurredAt); err != nil {
			return ErrInvalidEvent
		}
	}
	return nil
}

func isConstraintError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "constraint") || strings.Contains(text, "unique")
}

func (s *Service) timestamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

func newID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return "evt_" + hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("evt_%d", time.Now().UnixNano())
}
