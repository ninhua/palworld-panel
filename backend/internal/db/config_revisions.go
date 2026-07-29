package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// ConfigRevision records one durable PalWorldSettings.ini snapshot. SnapshotPath
// is deliberately excluded from JSON because revision files can contain server
// passwords and must never be exposed through the API.
type ConfigRevision struct {
	ID             string   `json:"id"`
	RevisionSHA256 string   `json:"revision_sha256"`
	ParentSHA256   string   `json:"parent_sha256,omitempty"`
	SnapshotPath   string   `json:"-"`
	Source         string   `json:"source"`
	ChangedFields  []string `json:"changed_fields"`
	CreatedAt      string   `json:"created_at"`
}

func migrateConfigRevisions(ctx context.Context, tx *sql.Tx) error {
	return execAll(ctx, tx,
		`CREATE TABLE IF NOT EXISTS config_revisions (
			id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128 AND id NOT GLOB '*[^A-Za-z0-9_-]*'),
			revision_sha256 TEXT NOT NULL CHECK(length(revision_sha256)=64 AND revision_sha256 NOT GLOB '*[^0-9a-f]*'),
			parent_sha256 TEXT NOT NULL DEFAULT '' CHECK(parent_sha256='' OR (length(parent_sha256)=64 AND parent_sha256 NOT GLOB '*[^0-9a-f]*')),
			snapshot_path TEXT NOT NULL CHECK(length(snapshot_path)>0),
			source TEXT NOT NULL CHECK(source IN ('baseline','apply')),
			changed_fields_json TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_config_revisions_created ON config_revisions(created_at DESC,id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_config_revisions_sha ON config_revisions(revision_sha256)`,
	)
}

func validConfigRevisionID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'A' || character > 'Z') &&
			(character < 'a' || character > 'z') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func validConfigRevisionSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validConfigRevisionSnapshotPath(revision ConfigRevision) bool {
	clean := filepath.Clean(strings.TrimSpace(revision.SnapshotPath))
	return clean != "." && filepath.Base(clean) == revision.ID+".ini" && filepath.Base(filepath.Dir(clean)) == "config-revisions"
}

func (s *Store) CreateConfigRevision(ctx context.Context, revision ConfigRevision) error {
	if !validConfigRevisionID(revision.ID) {
		return fmt.Errorf("invalid config revision id")
	}
	if !validConfigRevisionSHA256(revision.RevisionSHA256) {
		return fmt.Errorf("invalid config revision sha256")
	}
	if revision.ParentSHA256 != "" && !validConfigRevisionSHA256(revision.ParentSHA256) {
		return fmt.Errorf("invalid config revision parent sha256")
	}
	if !validConfigRevisionSnapshotPath(revision) {
		return fmt.Errorf("invalid config revision snapshot path")
	}
	if revision.Source != "baseline" && revision.Source != "apply" {
		return fmt.Errorf("invalid config revision source")
	}
	changed, err := encodeModifiedFields(revision.ChangedFields)
	if err != nil {
		return err
	}
	if revision.CreatedAt == "" {
		revision.CreatedAt = now()
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO config_revisions
		(id,revision_sha256,parent_sha256,snapshot_path,source,changed_fields_json,created_at)
		VALUES(?,?,?,?,?,?,?)`, revision.ID, revision.RevisionSHA256, revision.ParentSHA256,
		revision.SnapshotPath, revision.Source, changed, revision.CreatedAt)
	return err
}

func (s *Store) UpdateConfigRevisionMetadata(ctx context.Context, revision ConfigRevision) error {
	if !validConfigRevisionID(revision.ID) || !validConfigRevisionSHA256(revision.RevisionSHA256) {
		return fmt.Errorf("invalid config revision metadata")
	}
	if revision.ParentSHA256 != "" && !validConfigRevisionSHA256(revision.ParentSHA256) {
		return fmt.Errorf("invalid config revision parent sha256")
	}
	if revision.Source != "baseline" && revision.Source != "apply" {
		return fmt.Errorf("invalid config revision source")
	}
	changed, err := encodeModifiedFields(revision.ChangedFields)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE config_revisions SET parent_sha256=?,source=?,changed_fields_json=? WHERE id=? AND revision_sha256=?`,
		revision.ParentSHA256, revision.Source, changed, revision.ID, revision.RevisionSHA256)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func scanConfigRevision(scanner interface{ Scan(...any) error }) (ConfigRevision, error) {
	var revision ConfigRevision
	var changed string
	err := scanner.Scan(&revision.ID, &revision.RevisionSHA256, &revision.ParentSHA256,
		&revision.SnapshotPath, &revision.Source, &changed, &revision.CreatedAt)
	if err == nil {
		err = json.Unmarshal([]byte(changed), &revision.ChangedFields)
	}
	return revision, err
}

func (s *Store) GetConfigRevision(ctx context.Context, id string) (ConfigRevision, error) {
	return scanConfigRevision(s.db.QueryRowContext(ctx, `SELECT id,revision_sha256,parent_sha256,snapshot_path,source,changed_fields_json,created_at
		FROM config_revisions WHERE id=?`, id))
}

func (s *Store) LatestConfigRevision(ctx context.Context) (ConfigRevision, error) {
	return scanConfigRevision(s.db.QueryRowContext(ctx, `SELECT id,revision_sha256,parent_sha256,snapshot_path,source,changed_fields_json,created_at
		FROM config_revisions ORDER BY created_at DESC,id DESC LIMIT 1`))
}

func (s *Store) ListConfigRevisions(ctx context.Context, limit int) ([]ConfigRevision, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,revision_sha256,parent_sha256,snapshot_path,source,changed_fields_json,created_at
		FROM config_revisions ORDER BY created_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ConfigRevision, 0)
	for rows.Next() {
		revision, err := scanConfigRevision(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, revision)
	}
	return items, rows.Err()
}

// PruneConfigRevisions keeps the newest revisions and atomically queues the
// removed private snapshot files for the existing retryable cleanup worker.
func (s *Store) PruneConfigRevisions(ctx context.Context, keep int) ([]ConfigRevision, error) {
	if keep < 1 {
		keep = 50
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,revision_sha256,parent_sha256,snapshot_path,source,changed_fields_json,created_at
		FROM config_revisions ORDER BY created_at DESC,id DESC LIMIT -1 OFFSET ?`, keep)
	if err != nil {
		return nil, err
	}
	var removed []ConfigRevision
	for rows.Next() {
		revision, scanErr := scanConfigRevision(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		removed = append(removed, revision)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, revision := range removed {
		if !validConfigRevisionSnapshotPath(revision) {
			return nil, fmt.Errorf("invalid config revision snapshot path for %s", revision.ID)
		}
		if err := queueConfigPrivateCleanup(ctx, tx, revision.SnapshotPath, "config_revision"); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM config_revisions WHERE id=?`, revision.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return removed, nil
}

func (s *Store) DeleteConfigRevisionAndQueueCleanup(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	revision, err := scanConfigRevision(tx.QueryRowContext(ctx, `SELECT id,revision_sha256,parent_sha256,snapshot_path,source,changed_fields_json,created_at
		FROM config_revisions WHERE id=?`, id))
	if err != nil {
		return err
	}
	if !validConfigRevisionSnapshotPath(revision) {
		return fmt.Errorf("invalid config revision snapshot path for %s", revision.ID)
	}
	if err := queueConfigPrivateCleanup(ctx, tx, revision.SnapshotPath, "config_revision"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM config_revisions WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
