package gameevents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var gameEventRangeIndexReady sync.Map

// ListRange returns persisted events whose game occurrence time (or ingestion
// time when the source omitted one) falls within the inclusive interval.
// eventTypes may be empty to include every type.
func (s *Service) ListRange(ctx context.Context, from, to time.Time, eventTypes []string, limit int) ([]Record, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("game event service is unavailable")
	}
	if to.Before(from) {
		from, to = to, from
	}
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}

	// Existing databases gain the expression index lazily. The per-service
	// guard avoids taking a schema lock on every history comparison.
	if _, ready := gameEventRangeIndexReady.Load(s); !ready {
		if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_game_events_effective_time ON game_events(COALESCE(NULLIF(occurred_at,''),created_at))`); err != nil {
			return nil, fmt.Errorf("create game event time index: %w", err)
		}
		gameEventRangeIndexReady.Store(s, struct{}{})
	}

	types := make([]string, 0, len(eventTypes))
	seen := make(map[string]struct{}, len(eventTypes))
	for _, value := range eventTypes {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		types = append(types, value)
	}

	query := `SELECT event_id,type,player_uid,nickname,steam_id,occurred_at,payload_json,status,result_json,error,attempts,created_at,updated_at
		FROM game_events
		WHERE COALESCE(NULLIF(occurred_at,''),created_at) >= ?
		  AND COALESCE(NULLIF(occurred_at,''),created_at) <= ?`
	args := []any{from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano)}
	if len(types) > 0 {
		placeholders := make([]string, len(types))
		for index, value := range types {
			placeholders[index] = "?"
			args = append(args, value)
		}
		query += " AND type IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY COALESCE(NULLIF(occurred_at,''),created_at) ASC, event_id ASC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list game events by time range: %w", err)
	}
	defer rows.Close()

	result := make([]Record, 0)
	for rows.Next() {
		record, scanErr := scanRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
