package gameevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type BridgeOffset struct {
	Path            string `json:"path"`
	Offset          int64  `json:"offset"`
	FileSize        int64  `json:"file_size"`
	PrefixHash      string `json:"prefix_hash,omitempty"`
	ResetCount      int64  `json:"reset_count"`
	LastResetReason string `json:"last_reset_reason,omitempty"`
	UpdatedAt       string `json:"updated_at"`
}

type BridgeObservation struct {
	ID         int64  `json:"id"`
	SourcePath string `json:"source_path"`
	Offset     int64  `json:"offset"`
	EventType  string `json:"event_type,omitempty"`
	PlayerUID  string `json:"player_uid,omitempty"`
	Nickname   string `json:"nickname,omitempty"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	Sample     string `json:"sample,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type BridgeDeadLetter struct {
	ID          int64          `json:"id"`
	EventID     string         `json:"event_id"`
	SourcePath  string         `json:"source_path"`
	Offset      int64          `json:"offset"`
	EventType   string         `json:"event_type,omitempty"`
	PlayerHint  string         `json:"player_hint,omitempty"`
	PlayerUID   string         `json:"player_uid,omitempty"`
	SteamID     string         `json:"steam_id,omitempty"`
	Nickname    string         `json:"nickname,omitempty"`
	Payload     map[string]any `json:"payload"`
	RawLine     string         `json:"raw_line,omitempty"`
	Sample      string         `json:"sample,omitempty"`
	Reason      string         `json:"reason"`
	Status      string         `json:"status"`
	Attempts    int            `json:"attempts"`
	LastError   string         `json:"last_error,omitempty"`
	ReplayedAt  string         `json:"replayed_at,omitempty"`
	DismissedAt string         `json:"dismissed_at,omitempty"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

func (s *Service) EnsureBridgeSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS game_event_bridge_offsets (
			path TEXT PRIMARY KEY,
			offset INTEGER NOT NULL DEFAULT 0 CHECK(offset >= 0),
			file_size INTEGER NOT NULL DEFAULT 0 CHECK(file_size >= 0),
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS game_event_bridge_observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_path TEXT NOT NULL DEFAULT '',
			offset INTEGER NOT NULL DEFAULT 0,
			event_type TEXT NOT NULL DEFAULT '',
			player_uid TEXT NOT NULL DEFAULT '',
			nickname TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			sample TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS game_event_bridge_dead_letters (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id TEXT NOT NULL UNIQUE,
			source_path TEXT NOT NULL DEFAULT '',
			offset INTEGER NOT NULL DEFAULT 0,
			event_type TEXT NOT NULL DEFAULT '',
			player_hint TEXT NOT NULL DEFAULT '',
			player_uid TEXT NOT NULL DEFAULT '',
			steam_id TEXT NOT NULL DEFAULT '',
			nickname TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '{}',
			raw_line TEXT NOT NULL DEFAULT '',
			sample TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','replayed','dismissed')),
			attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0),
			last_error TEXT NOT NULL DEFAULT '',
			replayed_at TEXT NOT NULL DEFAULT '',
			dismissed_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_game_event_bridge_observations_created ON game_event_bridge_observations(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_game_event_bridge_observations_status ON game_event_bridge_observations(status,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_game_event_bridge_dead_letters_status ON game_event_bridge_dead_letters(status,id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_game_event_bridge_dead_letters_event_type ON game_event_bridge_dead_letters(event_type,id DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for _, migration := range []struct {
		column     string
		definition string
	}{
		{column: "prefix_hash", definition: "TEXT NOT NULL DEFAULT ''"},
		{column: "reset_count", definition: "INTEGER NOT NULL DEFAULT 0"},
		{column: "last_reset_reason", definition: "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.ensureBridgeOffsetColumn(ctx, migration.column, migration.definition); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ensureBridgeOffsetColumn(ctx context.Context, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(game_event_bridge_offsets)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		if strings.EqualFold(name, column) {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE game_event_bridge_offsets ADD COLUMN `+column+` `+definition); err != nil {
		return fmt.Errorf("add game event bridge offset column %s: %w", column, err)
	}
	return nil
}

func (s *Service) BridgeOffset(ctx context.Context, path string) (BridgeOffset, bool, error) {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return BridgeOffset{}, false, err
	}
	var item BridgeOffset
	err := s.db.QueryRowContext(ctx, `SELECT path,offset,file_size,prefix_hash,reset_count,last_reset_reason,updated_at FROM game_event_bridge_offsets WHERE path=?`, strings.TrimSpace(path)).Scan(
		&item.Path, &item.Offset, &item.FileSize, &item.PrefixHash, &item.ResetCount, &item.LastResetReason, &item.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return BridgeOffset{}, false, nil
	}
	return item, err == nil, err
}

func (s *Service) ListBridgeOffsets(ctx context.Context) ([]BridgeOffset, error) {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT path,offset,file_size,prefix_hash,reset_count,last_reset_reason,updated_at FROM game_event_bridge_offsets ORDER BY updated_at DESC,path ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]BridgeOffset, 0)
	for rows.Next() {
		var item BridgeOffset
		if err := rows.Scan(&item.Path, &item.Offset, &item.FileSize, &item.PrefixHash, &item.ResetCount, &item.LastResetReason, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) SaveBridgeOffset(ctx context.Context, path string, offset, fileSize int64) error {
	return s.SaveBridgeOffsetState(ctx, BridgeOffset{Path: path, Offset: offset, FileSize: fileSize})
}

func (s *Service) SaveBridgeOffsetState(ctx context.Context, item BridgeOffset) error {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return err
	}
	item.Path = strings.TrimSpace(item.Path)
	item.PrefixHash = strings.TrimSpace(item.PrefixHash)
	item.LastResetReason = strings.TrimSpace(item.LastResetReason)
	if item.Offset < 0 {
		item.Offset = 0
	}
	if item.FileSize < 0 {
		item.FileSize = 0
	}
	if item.ResetCount < 0 {
		item.ResetCount = 0
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO game_event_bridge_offsets(path,offset,file_size,prefix_hash,reset_count,last_reset_reason,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET offset=excluded.offset,file_size=excluded.file_size,prefix_hash=excluded.prefix_hash,reset_count=excluded.reset_count,last_reset_reason=excluded.last_reset_reason,updated_at=excluded.updated_at`, item.Path, item.Offset, item.FileSize, item.PrefixHash, item.ResetCount, item.LastResetReason, s.timestamp())
	return err
}

func (s *Service) AddBridgeObservation(ctx context.Context, item BridgeObservation) error {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return err
	}
	item.SourcePath = strings.TrimSpace(item.SourcePath)
	item.EventType = strings.ToUpper(strings.TrimSpace(item.EventType))
	item.PlayerUID = strings.TrimSpace(item.PlayerUID)
	item.Nickname = strings.TrimSpace(item.Nickname)
	item.Status = strings.TrimSpace(item.Status)
	item.Reason = strings.TrimSpace(item.Reason)
	item.Sample = strings.TrimSpace(item.Sample)
	if len(item.Sample) > 512 {
		item.Sample = item.Sample[:512]
	}
	if len(item.Reason) > 512 {
		item.Reason = item.Reason[:512]
	}
	if item.Status == "" {
		item.Status = "ignored"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO game_event_bridge_observations(source_path,offset,event_type,player_uid,nickname,status,reason,sample,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, item.SourcePath, item.Offset, item.EventType, item.PlayerUID, item.Nickname, item.Status, item.Reason, item.Sample, s.timestamp())
	if err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM game_event_bridge_observations WHERE id NOT IN (SELECT id FROM game_event_bridge_observations ORDER BY id DESC LIMIT 1000)`)
	return nil
}

func (s *Service) ListBridgeObservations(ctx context.Context, status string, limit, offset int) ([]BridgeObservation, error) {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return nil, err
	}
	limit, offset = normalizeBridgePagination(limit, offset)
	status = strings.TrimSpace(status)
	rows, err := s.db.QueryContext(ctx, `SELECT id,source_path,offset,event_type,player_uid,nickname,status,reason,sample,created_at FROM game_event_bridge_observations WHERE (?='' OR status=?) ORDER BY id DESC LIMIT ? OFFSET ?`, status, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]BridgeObservation, 0)
	for rows.Next() {
		var item BridgeObservation
		if err := rows.Scan(&item.ID, &item.SourcePath, &item.Offset, &item.EventType, &item.PlayerUID, &item.Nickname, &item.Status, &item.Reason, &item.Sample, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) AddBridgeDeadLetter(ctx context.Context, item BridgeDeadLetter) (BridgeDeadLetter, error) {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return BridgeDeadLetter{}, err
	}
	item.EventID = strings.TrimSpace(item.EventID)
	item.SourcePath = strings.TrimSpace(item.SourcePath)
	item.EventType = strings.ToUpper(strings.TrimSpace(item.EventType))
	item.PlayerHint = strings.TrimSpace(item.PlayerHint)
	item.PlayerUID = strings.TrimSpace(item.PlayerUID)
	item.SteamID = strings.TrimSpace(item.SteamID)
	item.Nickname = strings.TrimSpace(item.Nickname)
	item.RawLine = strings.TrimSpace(item.RawLine)
	item.Sample = strings.TrimSpace(item.Sample)
	item.Reason = strings.TrimSpace(item.Reason)
	if item.EventID == "" {
		return BridgeDeadLetter{}, errors.New("bridge dead letter event_id is required")
	}
	if item.EventType == "" {
		item.EventType = "UNKNOWN"
	}
	if item.Payload == nil {
		item.Payload = map[string]any{}
	}
	if len(item.RawLine) > 8192 {
		item.RawLine = item.RawLine[:8192]
	}
	if len(item.Sample) > 512 {
		item.Sample = item.Sample[:512]
	}
	if len(item.Reason) > 1024 {
		item.Reason = item.Reason[:1024]
	}
	payload, err := json.Marshal(item.Payload)
	if err != nil {
		return BridgeDeadLetter{}, fmt.Errorf("encode bridge dead letter payload: %w", err)
	}
	now := s.timestamp()
	_, err = s.db.ExecContext(ctx, `INSERT INTO game_event_bridge_dead_letters(event_id,source_path,offset,event_type,player_hint,player_uid,steam_id,nickname,payload_json,raw_line,sample,reason,status,attempts,last_error,replayed_at,dismissed_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?, 'pending',0,'','','',?,?) ON CONFLICT(event_id) DO UPDATE SET source_path=excluded.source_path,offset=excluded.offset,event_type=excluded.event_type,player_hint=excluded.player_hint,player_uid=excluded.player_uid,steam_id=excluded.steam_id,nickname=excluded.nickname,payload_json=excluded.payload_json,raw_line=excluded.raw_line,sample=excluded.sample,reason=excluded.reason,updated_at=excluded.updated_at`, item.EventID, item.SourcePath, item.Offset, item.EventType, item.PlayerHint, item.PlayerUID, item.SteamID, item.Nickname, string(payload), item.RawLine, item.Sample, item.Reason, now, now)
	if err != nil {
		return BridgeDeadLetter{}, err
	}
	return s.GetBridgeDeadLetterByEventID(ctx, item.EventID)
}

func (s *Service) GetBridgeDeadLetter(ctx context.Context, id int64) (BridgeDeadLetter, error) {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return BridgeDeadLetter{}, err
	}
	return scanBridgeDeadLetter(s.db.QueryRowContext(ctx, bridgeDeadLetterSelect+` WHERE id=?`, id))
}

func (s *Service) GetBridgeDeadLetterByEventID(ctx context.Context, eventID string) (BridgeDeadLetter, error) {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return BridgeDeadLetter{}, err
	}
	return scanBridgeDeadLetter(s.db.QueryRowContext(ctx, bridgeDeadLetterSelect+` WHERE event_id=?`, strings.TrimSpace(eventID)))
}

func (s *Service) ListBridgeDeadLetters(ctx context.Context, status string, limit, offset int) ([]BridgeDeadLetter, error) {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return nil, err
	}
	limit, offset = normalizeBridgePagination(limit, offset)
	status = strings.TrimSpace(status)
	rows, err := s.db.QueryContext(ctx, bridgeDeadLetterSelect+` WHERE (?='' OR status=?) ORDER BY id DESC LIMIT ? OFFSET ?`, status, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]BridgeDeadLetter, 0)
	for rows.Next() {
		item, err := scanBridgeDeadLetter(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) CountBridgeDeadLetters(ctx context.Context, status string) (int64, error) {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return 0, err
	}
	var count int64
	status = strings.TrimSpace(status)
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM game_event_bridge_dead_letters WHERE (?='' OR status=?)`, status, status).Scan(&count)
	return count, err
}

func (s *Service) MarkBridgeDeadLetterAttempt(ctx context.Context, id int64, lastError string) error {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return err
	}
	lastError = strings.TrimSpace(lastError)
	if len(lastError) > 1024 {
		lastError = lastError[:1024]
	}
	result, err := s.db.ExecContext(ctx, `UPDATE game_event_bridge_dead_letters SET attempts=attempts+1,last_error=?,updated_at=? WHERE id=? AND status='pending'`, lastError, s.timestamp(), id)
	if err != nil {
		return err
	}
	return requireBridgeRow(result, "bridge dead letter is not pending")
}

func (s *Service) CompleteBridgeDeadLetter(ctx context.Context, id int64) error {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return err
	}
	now := s.timestamp()
	result, err := s.db.ExecContext(ctx, `UPDATE game_event_bridge_dead_letters SET status='replayed',attempts=attempts+1,last_error='',replayed_at=?,updated_at=? WHERE id=? AND status='pending'`, now, now, id)
	if err != nil {
		return err
	}
	return requireBridgeRow(result, "bridge dead letter is not pending")
}

func (s *Service) DismissBridgeDeadLetter(ctx context.Context, id int64) error {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return err
	}
	now := s.timestamp()
	result, err := s.db.ExecContext(ctx, `UPDATE game_event_bridge_dead_letters SET status='dismissed',dismissed_at=?,updated_at=? WHERE id=? AND status='pending'`, now, now, id)
	if err != nil {
		return err
	}
	return requireBridgeRow(result, "bridge dead letter is not pending")
}

const bridgeDeadLetterSelect = `SELECT id,event_id,source_path,offset,event_type,player_hint,player_uid,steam_id,nickname,payload_json,raw_line,sample,reason,status,attempts,last_error,replayed_at,dismissed_at,created_at,updated_at FROM game_event_bridge_dead_letters`

type bridgeScanner interface {
	Scan(dest ...any) error
}

func scanBridgeDeadLetter(scanner bridgeScanner) (BridgeDeadLetter, error) {
	var item BridgeDeadLetter
	var payload string
	if err := scanner.Scan(&item.ID, &item.EventID, &item.SourcePath, &item.Offset, &item.EventType, &item.PlayerHint, &item.PlayerUID, &item.SteamID, &item.Nickname, &payload, &item.RawLine, &item.Sample, &item.Reason, &item.Status, &item.Attempts, &item.LastError, &item.ReplayedAt, &item.DismissedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return BridgeDeadLetter{}, err
	}
	item.Payload = map[string]any{}
	if strings.TrimSpace(payload) != "" {
		if err := json.Unmarshal([]byte(payload), &item.Payload); err != nil {
			return BridgeDeadLetter{}, fmt.Errorf("decode bridge dead letter %d payload: %w", item.ID, err)
		}
	}
	return item, nil
}

func normalizeBridgePagination(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func requireBridgeRow(result sql.Result, message string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New(message)
	}
	return nil
}

func ParseBridgeDeadLetterID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("bridge dead letter id must be a positive integer")
	}
	return id, nil
}
