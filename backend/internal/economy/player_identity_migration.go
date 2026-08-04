package economy

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "palpanel/internal/playeridentity"
)

const economyPlayerIdentityMigration = "economy-player-uid-collation-v1"

var economyPlayerIdentityReady sync.Map

func init() {
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			serviceCache.Range(func(_, value any) bool {
				service, ok := value.(*Service)
				if !ok || service == nil || service.db == nil {
					return true
				}
				if _, ready := economyPlayerIdentityReady.Load(service); ready {
					return true
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				err := service.EnsurePlayerIdentityConsistency(ctx)
				cancel()
				if err == nil {
					economyPlayerIdentityReady.Store(service, struct{}{})
				}
				return true
			})
		}
	}()
}

// EnsurePlayerIdentityConsistency merges GUID formatting variants into one
// account and recreates identity-bearing economy tables with PALPLAYERUID
// collation. The operation is transactional and idempotent.
func (s *Service) EnsurePlayerIdentityConsistency(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA legacy_alter_table=ON`); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `PRAGMA legacy_alter_table=OFF`)
		_, _ = conn.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`)
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS palpanel_identity_migrations (
		migration_key TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	var applied int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM palpanel_identity_migrations WHERE migration_key=?)`, economyPlayerIdentityMigration).Scan(&applied); err != nil {
		return err
	}
	if applied != 0 {
		return tx.Commit()
	}

	exists, err := economyTableExists(ctx, tx, "economy_accounts")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	for _, statement := range economyIdentityMigrationStatements() {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply economy player identity migration: %w", err)
		}
	}
	if err := verifyForeignKeys(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO palpanel_identity_migrations(migration_key,applied_at) VALUES(?,?)`, economyPlayerIdentityMigration, s.timestamp()); err != nil {
		return err
	}
	return tx.Commit()
}

func economyIdentityMigrationStatements() []string {
	return []string{
		`ALTER TABLE economy_ledger RENAME TO economy_ledger_identity_legacy`,
		`ALTER TABLE economy_checkins RENAME TO economy_checkins_identity_legacy`,
		`ALTER TABLE economy_reservations RENAME TO economy_reservations_identity_legacy`,
		`ALTER TABLE economy_accounts RENAME TO economy_accounts_identity_legacy`,
		`CREATE TABLE economy_accounts (
			player_uid TEXT PRIMARY KEY COLLATE PALPLAYERUID,
			nickname TEXT NOT NULL DEFAULT '',
			steam_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			balance INTEGER NOT NULL DEFAULT 0 CHECK(balance >= 0),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE economy_ledger (
			id TEXT PRIMARY KEY,
			player_uid TEXT NOT NULL COLLATE PALPLAYERUID,
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
		`CREATE TABLE economy_checkins (
			player_uid TEXT NOT NULL COLLATE PALPLAYERUID,
			local_date TEXT NOT NULL,
			points INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY(player_uid, local_date),
			FOREIGN KEY(player_uid) REFERENCES economy_accounts(player_uid) ON DELETE RESTRICT
		)`,
		`CREATE TABLE economy_reservations (
			id TEXT PRIMARY KEY,
			player_uid TEXT NOT NULL COLLATE PALPLAYERUID,
			amount INTEGER NOT NULL CHECK(amount > 0),
			reference_id TEXT NOT NULL,
			status TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(player_uid, reference_id),
			FOREIGN KEY(player_uid) REFERENCES economy_accounts(player_uid) ON DELETE RESTRICT
		)`,
		`INSERT INTO economy_accounts(player_uid,nickname,steam_id,status,balance,created_at,updated_at)
		 SELECT pal_uid_canonical(player_uid),
		        COALESCE(MAX(NULLIF(nickname,'')),''),
		        COALESCE(MAX(NULLIF(steam_id,'')),''),
		        CASE WHEN MAX(CASE WHEN status='active' THEN 1 ELSE 0 END)=1 THEN 'active' ELSE COALESCE(MAX(status),'active') END,
		        SUM(balance),MIN(created_at),MAX(updated_at)
		 FROM economy_accounts_identity_legacy
		 GROUP BY pal_uid_canonical(player_uid)`,
		`WITH ranked AS (
		 SELECT id,pal_uid_canonical(player_uid) AS player_uid,delta,balance_after,reason,reference_type,reference_id,actor,metadata_json,created_at,
		        ROW_NUMBER() OVER (PARTITION BY pal_uid_canonical(player_uid),reference_type,reference_id ORDER BY created_at,id) AS duplicate_rank
		 FROM economy_ledger_identity_legacy
		)
		INSERT INTO economy_ledger(id,player_uid,delta,balance_after,reason,reference_type,reference_id,actor,metadata_json,created_at)
		SELECT id,player_uid,delta,balance_after,reason,reference_type,
		       CASE WHEN reference_id<>'' AND duplicate_rank>1 THEN reference_id||':identity-merge:'||id ELSE reference_id END,
		       actor,metadata_json,created_at
		FROM ranked`,
		`INSERT INTO economy_checkins(player_uid,local_date,points,created_at)
		 SELECT pal_uid_canonical(player_uid),local_date,MAX(points),MIN(created_at)
		 FROM economy_checkins_identity_legacy
		 GROUP BY pal_uid_canonical(player_uid),local_date`,
		`WITH ranked AS (
		 SELECT id,pal_uid_canonical(player_uid) AS player_uid,amount,reference_id,status,expires_at,created_at,updated_at,
		        ROW_NUMBER() OVER (PARTITION BY pal_uid_canonical(player_uid),reference_id ORDER BY created_at,id) AS duplicate_rank
		 FROM economy_reservations_identity_legacy
		)
		INSERT INTO economy_reservations(id,player_uid,amount,reference_id,status,expires_at,created_at,updated_at)
		SELECT id,player_uid,amount,
		       CASE WHEN duplicate_rank>1 THEN reference_id||':identity-merge:'||id ELSE reference_id END,
		       status,expires_at,created_at,updated_at
		FROM ranked`,
		`UPDATE economy_command_events SET player_uid=pal_uid_canonical(player_uid) WHERE player_uid<>''`,
		`DROP TABLE economy_ledger_identity_legacy`,
		`DROP TABLE economy_checkins_identity_legacy`,
		`DROP TABLE economy_reservations_identity_legacy`,
		`DROP TABLE economy_accounts_identity_legacy`,
		`CREATE INDEX idx_economy_accounts_nickname ON economy_accounts(nickname COLLATE NOCASE)`,
		`CREATE INDEX idx_economy_accounts_steam_id ON economy_accounts(steam_id)`,
		`CREATE INDEX idx_economy_ledger_player_created ON economy_ledger(player_uid, created_at DESC)`,
		`CREATE INDEX idx_economy_ledger_created ON economy_ledger(created_at DESC)`,
		`CREATE UNIQUE INDEX idx_economy_ledger_reference ON economy_ledger(player_uid, reference_type, reference_id) WHERE reference_id <> ''`,
		`CREATE INDEX idx_economy_checkins_date ON economy_checkins(local_date)`,
		`CREATE INDEX idx_economy_reservations_status_expiry ON economy_reservations(status, expires_at)`,
		`CREATE TRIGGER IF NOT EXISTS trg_economy_accounts_player_uid_canonical
		 AFTER INSERT ON economy_accounts
		 WHEN NEW.player_uid<>pal_uid_canonical(NEW.player_uid)
		 BEGIN
		  UPDATE economy_accounts SET player_uid=pal_uid_canonical(NEW.player_uid) WHERE rowid=NEW.rowid;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_economy_ledger_player_uid_canonical
		 AFTER INSERT ON economy_ledger
		 WHEN NEW.player_uid<>pal_uid_canonical(NEW.player_uid)
		 BEGIN
		  UPDATE economy_ledger SET player_uid=pal_uid_canonical(NEW.player_uid) WHERE id=NEW.id;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_economy_checkins_player_uid_canonical
		 AFTER INSERT ON economy_checkins
		 WHEN NEW.player_uid<>pal_uid_canonical(NEW.player_uid)
		 BEGIN
		  UPDATE economy_checkins SET player_uid=pal_uid_canonical(NEW.player_uid) WHERE rowid=NEW.rowid;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_economy_reservations_player_uid_canonical
		 AFTER INSERT ON economy_reservations
		 WHEN NEW.player_uid<>pal_uid_canonical(NEW.player_uid)
		 BEGIN
		  UPDATE economy_reservations SET player_uid=pal_uid_canonical(NEW.player_uid) WHERE id=NEW.id;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_economy_command_events_player_uid_canonical
		 AFTER INSERT ON economy_command_events
		 WHEN NEW.player_uid<>pal_uid_canonical(NEW.player_uid)
		 BEGIN
		  UPDATE economy_command_events SET player_uid=pal_uid_canonical(NEW.player_uid) WHERE event_id=NEW.event_id;
		 END`,
	}
}

func economyTableExists(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`, strings.TrimSpace(table)).Scan(&exists)
	return exists != 0, err
}

func verifyForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowID sql.NullInt64
		var parent string
		var foreignKeyID int
		if err := rows.Scan(&table, &rowID, &parent, &foreignKeyID); err != nil {
			return err
		}
		return fmt.Errorf("economy identity migration produced invalid foreign key: table=%s row=%v parent=%s key=%d", table, rowID, parent, foreignKeyID)
	}
	return rows.Err()
}
