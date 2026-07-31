package economy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const legacyAstrBotSourceType = "astrbot_sqlite_v1"

var ErrInvalidLegacyAstrBotDatabase = errors.New("invalid AstrBot PalPanel database")

type LegacyAstrBotCandidate struct {
	QQID               string `json:"qq_id"`
	PlayerUID          string `json:"player_uid,omitempty"`
	Nickname           string `json:"nickname,omitempty"`
	BindingStatus      string `json:"binding_status,omitempty"`
	SourceBalance      int64  `json:"source_balance"`
	PreviouslyImported int64  `json:"previously_imported"`
	ImportDelta        int64  `json:"import_delta"`
	Status             string `json:"status"`
}

type LegacyAstrBotPreview struct {
	SourceSHA256       string                   `json:"source_sha256"`
	Accounts           int                      `json:"accounts"`
	BoundAccounts      int                      `json:"bound_accounts"`
	ImportableAccounts int                      `json:"importable_accounts"`
	UnboundAccounts    int                      `json:"unbound_accounts"`
	ZeroBalance        int                      `json:"zero_balance"`
	DecreasedBalance   int                      `json:"decreased_balance"`
	AlreadyCurrent     int                      `json:"already_current"`
	SourcePoints       int64                    `json:"source_points"`
	ImportablePoints   int64                    `json:"importable_points"`
	Checkins           int                      `json:"checkins"`
	Candidates         []LegacyAstrBotCandidate `json:"candidates"`
}

type LegacyAstrBotImportResult struct {
	BatchID             string `json:"batch_id"`
	SourceSHA256        string `json:"source_sha256"`
	ImportedAccounts    int    `json:"imported_accounts"`
	ImportedPoints      int64  `json:"imported_points"`
	ImportedCheckins    int    `json:"imported_checkins"`
	UnboundAccounts     int    `json:"unbound_accounts"`
	DecreasedBalance    int    `json:"decreased_balance"`
	AlreadyCurrent      int    `json:"already_current"`
	ZeroBalanceAccounts int    `json:"zero_balance_accounts"`
}

type legacyAstrBotCheckin struct {
	PlayerUID string
	LocalDate string
	Points    int64
}

func (s *Service) InspectLegacyAstrBot(ctx context.Context, path string) (LegacyAstrBotPreview, error) {
	if err := s.ensureLegacyImportSchema(ctx); err != nil {
		return LegacyAstrBotPreview{}, err
	}
	sha, err := fileSHA256(path)
	if err != nil {
		return LegacyAstrBotPreview{}, err
	}
	legacy, err := openLegacyAstrBot(path)
	if err != nil {
		return LegacyAstrBotPreview{}, err
	}
	defer legacy.Close()
	candidates, checkins, err := s.readLegacyAstrBot(ctx, legacy)
	if err != nil {
		return LegacyAstrBotPreview{}, err
	}
	preview := LegacyAstrBotPreview{SourceSHA256: sha, Accounts: len(candidates), Checkins: len(checkins)}
	preview.Candidates = make([]LegacyAstrBotCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		preview.SourcePoints += candidate.SourceBalance
		if candidate.PlayerUID == "" {
			candidate.Status = "unbound"
			preview.UnboundAccounts++
			preview.Candidates = append(preview.Candidates, candidate)
			continue
		}
		preview.BoundAccounts++
		previous, err := s.legacyImportedBalance(ctx, candidate.QQID)
		if err != nil {
			return LegacyAstrBotPreview{}, err
		}
		candidate.PreviouslyImported = previous
		switch {
		case candidate.SourceBalance == 0:
			candidate.Status = "zero_balance"
			preview.ZeroBalance++
		case candidate.SourceBalance < previous:
			candidate.Status = "source_balance_decreased"
			preview.DecreasedBalance++
		case candidate.SourceBalance == previous:
			candidate.Status = "already_current"
			preview.AlreadyCurrent++
		default:
			candidate.ImportDelta = candidate.SourceBalance - previous
			candidate.Status = "ready"
			preview.ImportableAccounts++
			preview.ImportablePoints += candidate.ImportDelta
		}
		preview.Candidates = append(preview.Candidates, candidate)
	}
	return preview, nil
}

func (s *Service) ImportLegacyAstrBot(ctx context.Context, path, actor string) (LegacyAstrBotImportResult, error) {
	preview, err := s.InspectLegacyAstrBot(ctx, path)
	if err != nil {
		return LegacyAstrBotImportResult{}, err
	}
	legacy, err := openLegacyAstrBot(path)
	if err != nil {
		return LegacyAstrBotImportResult{}, err
	}
	defer legacy.Close()
	candidates, checkins, err := s.readLegacyAstrBot(ctx, legacy)
	if err != nil {
		return LegacyAstrBotImportResult{}, err
	}
	batchID := newID()
	result := LegacyAstrBotImportResult{
		BatchID: batchID, SourceSHA256: preview.SourceSHA256,
		UnboundAccounts: preview.UnboundAccounts, DecreasedBalance: preview.DecreasedBalance,
		AlreadyCurrent: preview.AlreadyCurrent, ZeroBalanceAccounts: preview.ZeroBalance,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LegacyAstrBotImportResult{}, err
	}
	defer rollback(tx)
	now := s.timestamp()
	if _, err := tx.ExecContext(ctx, `INSERT INTO economy_import_batches(id,source_type,source_sha256,status,actor,created_at,completed_at) VALUES(?,?,?,?,?,?,?)`, batchID, legacyAstrBotSourceType, preview.SourceSHA256, "running", strings.TrimSpace(actor), now, ""); err != nil {
		return LegacyAstrBotImportResult{}, err
	}
	for _, candidate := range candidates {
		if candidate.PlayerUID == "" {
			continue
		}
		previous, err := legacyImportedBalanceTx(ctx, tx, candidate.QQID)
		if err != nil {
			return LegacyAstrBotImportResult{}, err
		}
		if err := s.ensureAccountTx(ctx, tx, candidate.PlayerUID, candidate.Nickname, ""); err != nil {
			return LegacyAstrBotImportResult{}, err
		}
		if candidate.SourceBalance > previous {
			delta := candidate.SourceBalance - previous
			_, _, err = s.adjustTx(ctx, tx, Adjustment{
				PlayerUID: candidate.PlayerUID, Nickname: candidate.Nickname, Delta: delta,
				Reason: "AstrBot旧积分迁移", ReferenceType: "legacy_astrbot_balance",
				ReferenceID: candidate.QQID + ":" + fmt.Sprint(candidate.SourceBalance), Actor: strings.TrimSpace(actor),
				Metadata: map[string]any{"qq_id": candidate.QQID, "source_sha256": preview.SourceSHA256, "source_balance": candidate.SourceBalance, "previously_imported": previous},
			})
			if err != nil {
				return LegacyAstrBotImportResult{}, err
			}
			result.ImportedAccounts++
			result.ImportedPoints += delta
		}
		if candidate.SourceBalance >= previous {
			if _, err := tx.ExecContext(ctx, `INSERT INTO economy_import_accounts(source_type,source_account_id,player_uid,imported_balance,source_sha256,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(source_type,source_account_id) DO UPDATE SET player_uid=excluded.player_uid,imported_balance=excluded.imported_balance,source_sha256=excluded.source_sha256,updated_at=excluded.updated_at`, legacyAstrBotSourceType, candidate.QQID, candidate.PlayerUID, candidate.SourceBalance, preview.SourceSHA256, now); err != nil {
				return LegacyAstrBotImportResult{}, err
			}
		}
	}
	for _, checkin := range checkins {
		if checkin.PlayerUID == "" || checkin.LocalDate == "" {
			continue
		}
		if err := s.ensureAccountTx(ctx, tx, checkin.PlayerUID, "", ""); err != nil {
			return LegacyAstrBotImportResult{}, err
		}
		cursor, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO economy_checkins(player_uid,local_date,points,created_at) VALUES(?,?,?,?)`, checkin.PlayerUID, checkin.LocalDate, checkin.Points, now)
		if err != nil {
			return LegacyAstrBotImportResult{}, err
		}
		if affected, _ := cursor.RowsAffected(); affected > 0 {
			result.ImportedCheckins += int(affected)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE economy_import_batches SET status='completed',accounts_imported=?,points_imported=?,checkins_imported=?,completed_at=? WHERE id=?`, result.ImportedAccounts, result.ImportedPoints, result.ImportedCheckins, now, batchID); err != nil {
		return LegacyAstrBotImportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return LegacyAstrBotImportResult{}, err
	}
	return result, nil
}

func (s *Service) ensureLegacyImportSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS economy_import_accounts (
			source_type TEXT NOT NULL,
			source_account_id TEXT NOT NULL,
			player_uid TEXT NOT NULL,
			imported_balance INTEGER NOT NULL DEFAULT 0 CHECK(imported_balance >= 0),
			source_sha256 TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY(source_type,source_account_id)
		)`,
		`CREATE TABLE IF NOT EXISTS economy_import_batches (
			id TEXT PRIMARY KEY,
			source_type TEXT NOT NULL,
			source_sha256 TEXT NOT NULL,
			status TEXT NOT NULL,
			accounts_imported INTEGER NOT NULL DEFAULT 0,
			points_imported INTEGER NOT NULL DEFAULT 0,
			checkins_imported INTEGER NOT NULL DEFAULT 0,
			actor TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			completed_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_economy_import_batches_created ON economy_import_batches(created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create economy import schema: %w", err)
		}
	}
	return nil
}

func (s *Service) legacyImportedBalance(ctx context.Context, qqID string) (int64, error) {
	var balance int64
	err := s.db.QueryRowContext(ctx, `SELECT imported_balance FROM economy_import_accounts WHERE source_type=? AND source_account_id=?`, legacyAstrBotSourceType, qqID).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return balance, err
}

func legacyImportedBalanceTx(ctx context.Context, tx *sql.Tx, qqID string) (int64, error) {
	var balance int64
	err := tx.QueryRowContext(ctx, `SELECT imported_balance FROM economy_import_accounts WHERE source_type=? AND source_account_id=?`, legacyAstrBotSourceType, qqID).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return balance, err
}

func openLegacyAstrBot(path string) (*sql.DB, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 16)
	_, readErr := io.ReadFull(file, header)
	_ = file.Close()
	if readErr != nil || string(header) != "SQLite format 3\x00" {
		return nil, ErrInvalidLegacyAstrBotDatabase
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(`PRAGMA query_only=ON`); err != nil {
		_ = database.Close()
		return nil, ErrInvalidLegacyAstrBotDatabase
	}
	return database, nil
}

func (s *Service) readLegacyAstrBot(ctx context.Context, database *sql.DB) ([]LegacyAstrBotCandidate, []legacyAstrBotCheckin, error) {
	rows, err := database.QueryContext(ctx, `SELECT a.qq_id,a.balance,COALESCE(b.player_uid,''),COALESCE(b.nickname,''),COALESCE(b.status,'') FROM accounts a LEFT JOIN bindings b ON b.qq_id=a.qq_id ORDER BY a.qq_id`)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: accounts/bindings schema is unavailable", ErrInvalidLegacyAstrBotDatabase)
	}
	defer rows.Close()
	candidates := make([]LegacyAstrBotCandidate, 0)
	for rows.Next() {
		var item LegacyAstrBotCandidate
		if err := rows.Scan(&item.QQID, &item.SourceBalance, &item.PlayerUID, &item.Nickname, &item.BindingStatus); err != nil {
			return nil, nil, err
		}
		item.QQID = strings.TrimSpace(item.QQID)
		item.PlayerUID = normalizePlayerUID(item.PlayerUID)
		item.Nickname = strings.TrimSpace(item.Nickname)
		if item.SourceBalance < 0 {
			return nil, nil, fmt.Errorf("%w: negative balance for QQ %s", ErrInvalidLegacyAstrBotDatabase, item.QQID)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	checkins := make([]legacyAstrBotCheckin, 0)
	checkinRows, err := database.QueryContext(ctx, `SELECT b.player_uid,c.local_date,c.points FROM checkins c JOIN bindings b ON b.qq_id=c.qq_id WHERE b.player_uid<>'' ORDER BY c.created_at`)
	if err == nil {
		defer checkinRows.Close()
		for checkinRows.Next() {
			var item legacyAstrBotCheckin
			if err := checkinRows.Scan(&item.PlayerUID, &item.LocalDate, &item.Points); err != nil {
				return nil, nil, err
			}
			item.PlayerUID = normalizePlayerUID(item.PlayerUID)
			item.LocalDate = strings.TrimSpace(item.LocalDate)
			checkins = append(checkins, item)
		}
		if err := checkinRows.Err(); err != nil {
			return nil, nil, err
		}
	}
	return candidates, checkins, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
