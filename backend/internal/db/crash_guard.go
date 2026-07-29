package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// CrashGuardState is the singleton persisted state used to distinguish
// intentional lifecycle operations from unexpected exits and to keep a crash
// loop tripped across PalPanel restarts.
type CrashGuardState struct {
	Tripped          bool   `json:"tripped"`
	TrippedAt        string `json:"tripped_at,omitempty"`
	Reason           string `json:"reason,omitempty"`
	ExpectedUntil    string `json:"-"`
	ExpectedReason   string `json:"-"`
	LastRuntimeMode  string `json:"-"`
	LastStatus       string `json:"-"`
	LastRestartCount int    `json:"-"`
	LastSignature    string `json:"-"`
	RecoveredAt      string `json:"-"`
	UpdatedAt        string `json:"updated_at"`
}

type CrashGuardEvent struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	RuntimeMode  string `json:"runtime_mode"`
	Occurrences  int    `json:"occurrences"`
	ExitCode     int    `json:"exit_code"`
	OOMKilled    bool   `json:"oom_killed"`
	RestartCount int    `json:"restart_count"`
	StartedAt    string `json:"started_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
	Message      string `json:"message"`
	CreatedAt    string `json:"created_at"`
}

func migrateCrashGuard(ctx context.Context, tx *sql.Tx) error {
	return execAll(ctx, tx,
		`CREATE TABLE IF NOT EXISTS crash_guard_state (
			singleton INTEGER PRIMARY KEY CHECK(singleton=1),
			tripped INTEGER NOT NULL DEFAULT 0,
			tripped_at TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			expected_until TEXT NOT NULL DEFAULT '',
			expected_reason TEXT NOT NULL DEFAULT '',
			last_runtime_mode TEXT NOT NULL DEFAULT '',
			last_status TEXT NOT NULL DEFAULT '',
			last_restart_count INTEGER NOT NULL DEFAULT 0 CHECK(last_restart_count>=0),
			last_signature TEXT NOT NULL DEFAULT '',
			recovered_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`INSERT OR IGNORE INTO crash_guard_state(singleton,updated_at) VALUES(1,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`,
		`CREATE TABLE IF NOT EXISTS crash_guard_events (
			id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128 AND id NOT GLOB '*[^A-Za-z0-9_-]*'),
			kind TEXT NOT NULL CHECK(kind IN ('unexpected_exit','container_restart','oom_kill')),
			runtime_mode TEXT NOT NULL,
			occurrences INTEGER NOT NULL DEFAULT 1 CHECK(occurrences BETWEEN 1 AND 1000000),
			exit_code INTEGER NOT NULL DEFAULT 0,
			oom_killed INTEGER NOT NULL DEFAULT 0,
			restart_count INTEGER NOT NULL DEFAULT 0 CHECK(restart_count>=0),
			started_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_crash_guard_events_created ON crash_guard_events(created_at DESC,id DESC)`,
	)
}

func (s *Store) GetCrashGuardState(ctx context.Context) (CrashGuardState, error) {
	var state CrashGuardState
	var tripped int
	err := s.db.QueryRowContext(ctx, `SELECT tripped,tripped_at,reason,expected_until,expected_reason,last_runtime_mode,last_status,last_restart_count,last_signature,recovered_at,updated_at
		FROM crash_guard_state WHERE singleton=1`).Scan(&tripped, &state.TrippedAt, &state.Reason, &state.ExpectedUntil,
		&state.ExpectedReason, &state.LastRuntimeMode, &state.LastStatus, &state.LastRestartCount, &state.LastSignature, &state.RecoveredAt, &state.UpdatedAt)
	state.Tripped = tripped != 0
	return state, err
}

func (s *Store) PutCrashGuardState(ctx context.Context, state CrashGuardState) error {
	if state.LastRestartCount < 0 {
		return fmt.Errorf("invalid crash guard restart count")
	}
	if state.UpdatedAt == "" {
		state.UpdatedAt = now()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE crash_guard_state SET tripped=?,tripped_at=?,reason=?,expected_until=?,expected_reason=?,
		last_runtime_mode=?,last_status=?,last_restart_count=?,last_signature=?,recovered_at=?,updated_at=? WHERE singleton=1`,
		boolInt(state.Tripped), state.TrippedAt, state.Reason, state.ExpectedUntil, state.ExpectedReason,
		state.LastRuntimeMode, state.LastStatus, state.LastRestartCount, state.LastSignature, state.RecoveredAt, state.UpdatedAt)
	return err
}

func validCrashGuardEvent(event CrashGuardEvent) error {
	if !validConfigRevisionID(event.ID) {
		return fmt.Errorf("invalid crash guard event id")
	}
	switch event.Kind {
	case "unexpected_exit", "container_restart", "oom_kill":
	default:
		return fmt.Errorf("invalid crash guard event kind")
	}
	if strings.TrimSpace(event.RuntimeMode) == "" || event.Occurrences < 1 || event.Occurrences > 1000000 || event.RestartCount < 0 {
		return fmt.Errorf("invalid crash guard event")
	}
	return nil
}

func (s *Store) CreateCrashGuardEvent(ctx context.Context, event CrashGuardEvent) error {
	if err := validCrashGuardEvent(event); err != nil {
		return err
	}
	if event.CreatedAt == "" {
		event.CreatedAt = now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO crash_guard_events
		(id,kind,runtime_mode,occurrences,exit_code,oom_killed,restart_count,started_at,finished_at,message,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, event.ID, event.Kind, event.RuntimeMode, event.Occurrences, event.ExitCode,
		boolInt(event.OOMKilled), event.RestartCount, event.StartedAt, event.FinishedAt, event.Message, event.CreatedAt)
	return err
}

func scanCrashGuardEvent(scanner interface{ Scan(...any) error }) (CrashGuardEvent, error) {
	var event CrashGuardEvent
	var oom int
	err := scanner.Scan(&event.ID, &event.Kind, &event.RuntimeMode, &event.Occurrences, &event.ExitCode, &oom,
		&event.RestartCount, &event.StartedAt, &event.FinishedAt, &event.Message, &event.CreatedAt)
	event.OOMKilled = oom != 0
	return event, err
}

func (s *Store) ListCrashGuardEvents(ctx context.Context, limit int) ([]CrashGuardEvent, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,runtime_mode,occurrences,exit_code,oom_killed,restart_count,started_at,finished_at,message,created_at
		FROM crash_guard_events ORDER BY created_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CrashGuardEvent, 0)
	for rows.Next() {
		event, err := scanCrashGuardEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, event)
	}
	return items, rows.Err()
}

func (s *Store) CountCrashGuardOccurrencesSince(ctx context.Context, since string) (int, error) {
	threshold, err := time.Parse(time.RFC3339Nano, since)
	if err != nil {
		return 0, fmt.Errorf("parse crash guard threshold: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT occurrences,created_at FROM crash_guard_events`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var occurrences int
		var createdAt string
		if err := rows.Scan(&occurrences, &createdAt); err != nil {
			return 0, err
		}
		created, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return 0, fmt.Errorf("parse crash guard event timestamp: %w", err)
		}
		if !created.Before(threshold) {
			count += occurrences
		}
	}
	return count, rows.Err()
}

func (s *Store) ResolveCrashGuardAlerts(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alerts SET status='resolved',ack_at=? WHERE source='crash-guard' AND status!='resolved'`, now())
	return err
}

func (s *Store) PruneCrashGuardEvents(ctx context.Context, keep int) error {
	if keep < 1 {
		keep = 50
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM crash_guard_events WHERE id IN (
		SELECT id FROM crash_guard_events ORDER BY created_at DESC,id DESC LIMIT -1 OFFSET ?
	)`, keep)
	return err
}
