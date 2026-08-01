package gameevents

import (
	"context"
	"database/sql"
	"strings"
)

type BridgeOffset struct {
	Path      string `json:"path"`
	Offset    int64  `json:"offset"`
	FileSize  int64  `json:"file_size"`
	UpdatedAt string `json:"updated_at"`
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
		`CREATE INDEX IF NOT EXISTS idx_game_event_bridge_observations_created ON game_event_bridge_observations(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_game_event_bridge_observations_status ON game_event_bridge_observations(status,created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) BridgeOffset(ctx context.Context, path string) (BridgeOffset, bool, error) {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return BridgeOffset{}, false, err
	}
	var item BridgeOffset
	err := s.db.QueryRowContext(ctx, `SELECT path,offset,file_size,updated_at FROM game_event_bridge_offsets WHERE path=?`, strings.TrimSpace(path)).Scan(&item.Path, &item.Offset, &item.FileSize, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return BridgeOffset{}, false, nil
	}
	return item, err == nil, err
}

func (s *Service) SaveBridgeOffset(ctx context.Context, path string, offset, fileSize int64) error {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return err
	}
	if offset < 0 {
		offset = 0
	}
	if fileSize < 0 {
		fileSize = 0
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO game_event_bridge_offsets(path,offset,file_size,updated_at) VALUES(?,?,?,?) ON CONFLICT(path) DO UPDATE SET offset=excluded.offset,file_size=excluded.file_size,updated_at=excluded.updated_at`, strings.TrimSpace(path), offset, fileSize, s.timestamp())
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
	_, _ = s.db.ExecContext(ctx, `DELETE FROM game_event_bridge_observations WHERE id NOT IN (SELECT id FROM game_event_bridge_observations ORDER BY id DESC LIMIT 500)`)
	return nil
}

func (s *Service) ListBridgeObservations(ctx context.Context, status string, limit, offset int) ([]BridgeObservation, error) {
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
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
