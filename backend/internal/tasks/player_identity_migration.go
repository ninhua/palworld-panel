package tasks

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "palpanel/internal/playeridentity"
)

const (
	taskPlayerIdentityMigration       = "tasks-player-uid-collation-v1"
	taskOnlinePlayerIdentityMigration = "tasks-online-player-uid-collation-v1"
)

var taskPlayerIdentityReady sync.Map

func init() {
	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			serviceCache.Range(func(_, value any) bool {
				service, ok := value.(*Service)
				if !ok || service == nil || service.db == nil {
					return true
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				coreReady, onlineReady, err := service.ensurePlayerIdentityConsistency(ctx)
				cancel()
				if err == nil && coreReady && onlineReady {
					taskPlayerIdentityReady.Store(service, struct{}{})
				}
				return true
			})
		}
	}()
}

// EnsurePlayerIdentityConsistency merges formatting variants in task progress,
// event receipts, rewards and online-duration tracking.
func (s *Service) EnsurePlayerIdentityConsistency(ctx context.Context) error {
	_, _, err := s.ensurePlayerIdentityConsistency(ctx)
	return err
}

func (s *Service) ensurePlayerIdentityConsistency(ctx context.Context) (bool, bool, error) {
	if s == nil || s.db == nil {
		return true, true, nil
	}
	if _, ready := taskPlayerIdentityReady.Load(s); ready {
		return true, true, nil
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return false, false, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return false, false, err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA legacy_alter_table=ON`); err != nil {
		return false, false, err
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `PRAGMA legacy_alter_table=OFF`)
		_, _ = conn.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`)
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS palpanel_identity_migrations (
		migration_key TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return false, false, err
	}

	coreReady, err := taskMigrationApplied(ctx, tx, taskPlayerIdentityMigration)
	if err != nil {
		return false, false, err
	}
	if !coreReady {
		exists, existsErr := taskTableExists(ctx, tx, "operations_task_progress")
		if existsErr != nil {
			return false, false, existsErr
		}
		if exists {
			for _, statement := range taskIdentityMigrationStatements() {
				if _, err := tx.ExecContext(ctx, statement); err != nil {
					return false, false, fmt.Errorf("apply task player identity migration: %w", err)
				}
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO palpanel_identity_migrations(migration_key,applied_at) VALUES(?,?)`, taskPlayerIdentityMigration, s.timestamp()); err != nil {
				return false, false, err
			}
			coreReady = true
		}
	}

	onlineReady, err := taskMigrationApplied(ctx, tx, taskOnlinePlayerIdentityMigration)
	if err != nil {
		return false, false, err
	}
	if !onlineReady {
		exists, existsErr := taskTableExists(ctx, tx, "operations_task_online_tracking")
		if existsErr != nil {
			return false, false, existsErr
		}
		if exists {
			for _, statement := range taskOnlineIdentityMigrationStatements() {
				if _, err := tx.ExecContext(ctx, statement); err != nil {
					return false, false, fmt.Errorf("apply online task player identity migration: %w", err)
				}
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO palpanel_identity_migrations(migration_key,applied_at) VALUES(?,?)`, taskOnlinePlayerIdentityMigration, s.timestamp()); err != nil {
				return false, false, err
			}
			onlineReady = true
		}
	}

	if err := taskVerifyForeignKeys(ctx, tx); err != nil {
		return false, false, err
	}
	if err := tx.Commit(); err != nil {
		return false, false, err
	}
	return coreReady, onlineReady, nil
}

func taskIdentityMigrationStatements() []string {
	return []string{
		`ALTER TABLE operations_task_progress RENAME TO operations_task_progress_identity_legacy`,
		`ALTER TABLE operations_task_event_receipts RENAME TO operations_task_event_receipts_identity_legacy`,
		`ALTER TABLE operations_task_rewards RENAME TO operations_task_rewards_identity_legacy`,
		`CREATE TABLE operations_task_progress (
			task_id TEXT NOT NULL,
			player_uid TEXT NOT NULL COLLATE PALPLAYERUID,
			cycle_key TEXT NOT NULL,
			progress INTEGER NOT NULL DEFAULT 0 CHECK(progress >= 0),
			completed INTEGER NOT NULL DEFAULT 0 CHECK(completed IN (0,1)),
			completed_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY(task_id,player_uid,cycle_key),
			FOREIGN KEY(task_id) REFERENCES operations_tasks(id)
		)`,
		`CREATE TABLE operations_task_event_receipts (
			task_id TEXT NOT NULL,
			player_uid TEXT NOT NULL COLLATE PALPLAYERUID,
			cycle_key TEXT NOT NULL,
			event_id TEXT NOT NULL,
			amount INTEGER NOT NULL CHECK(amount > 0),
			created_at TEXT NOT NULL,
			PRIMARY KEY(task_id,player_uid,cycle_key,event_id),
			FOREIGN KEY(task_id) REFERENCES operations_tasks(id)
		)`,
		`CREATE TABLE operations_task_rewards (
			task_id TEXT NOT NULL,
			player_uid TEXT NOT NULL COLLATE PALPLAYERUID,
			cycle_key TEXT NOT NULL,
			nickname TEXT NOT NULL DEFAULT '',
			steam_id TEXT NOT NULL DEFAULT '',
			points INTEGER NOT NULL CHECK(points > 0),
			status TEXT NOT NULL CHECK(status IN ('pending','granted','failed')),
			ledger_reference_id TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0),
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			granted_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(task_id,player_uid,cycle_key),
			FOREIGN KEY(task_id) REFERENCES operations_tasks(id)
		)`,
		`INSERT INTO operations_task_progress(task_id,player_uid,cycle_key,progress,completed,completed_at,updated_at)
		 SELECT legacy.task_id,pal_uid_canonical(legacy.player_uid),legacy.cycle_key,
		        CASE WHEN SUM(legacy.progress)>MAX(task.target_amount) THEN MAX(task.target_amount) ELSE SUM(legacy.progress) END,
		        MAX(legacy.completed),COALESCE(MAX(NULLIF(legacy.completed_at,'')),''),MAX(legacy.updated_at)
		 FROM operations_task_progress_identity_legacy AS legacy
		 JOIN operations_tasks AS task ON task.id=legacy.task_id
		 GROUP BY legacy.task_id,pal_uid_canonical(legacy.player_uid),legacy.cycle_key`,
		`INSERT INTO operations_task_event_receipts(task_id,player_uid,cycle_key,event_id,amount,created_at)
		 SELECT task_id,pal_uid_canonical(player_uid),cycle_key,event_id,MAX(amount),MIN(created_at)
		 FROM operations_task_event_receipts_identity_legacy
		 GROUP BY task_id,pal_uid_canonical(player_uid),cycle_key,event_id`,
		`INSERT INTO operations_task_rewards(task_id,player_uid,cycle_key,nickname,steam_id,points,status,ledger_reference_id,attempts,last_error,created_at,updated_at,granted_at)
		 SELECT task_id,pal_uid_canonical(player_uid),cycle_key,
		        COALESCE(MAX(NULLIF(nickname,'')),''),COALESCE(MAX(NULLIF(steam_id,'')),''),MAX(points),
		        CASE WHEN MAX(CASE WHEN status='granted' THEN 1 ELSE 0 END)=1 THEN 'granted'
		             WHEN MAX(CASE WHEN status='pending' THEN 1 ELSE 0 END)=1 THEN 'pending' ELSE 'failed' END,
		        MAX(ledger_reference_id),SUM(attempts),COALESCE(MAX(NULLIF(last_error,'')),''),
		        MIN(created_at),MAX(updated_at),COALESCE(MAX(NULLIF(granted_at,'')),'')
		 FROM operations_task_rewards_identity_legacy
		 GROUP BY task_id,pal_uid_canonical(player_uid),cycle_key`,
		`DROP TABLE operations_task_progress_identity_legacy`,
		`DROP TABLE operations_task_event_receipts_identity_legacy`,
		`DROP TABLE operations_task_rewards_identity_legacy`,
		`CREATE INDEX idx_operations_task_progress_player ON operations_task_progress(player_uid,updated_at DESC)`,
		`CREATE INDEX idx_operations_task_receipts_event ON operations_task_event_receipts(event_id)`,
		`CREATE INDEX idx_operations_task_rewards_status ON operations_task_rewards(status,updated_at)`,
		`CREATE TRIGGER IF NOT EXISTS trg_task_progress_player_uid_canonical
		 AFTER INSERT ON operations_task_progress
		 WHEN NEW.player_uid<>pal_uid_canonical(NEW.player_uid)
		 BEGIN
		  UPDATE operations_task_progress SET player_uid=pal_uid_canonical(NEW.player_uid) WHERE rowid=NEW.rowid;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_task_receipts_player_uid_canonical
		 AFTER INSERT ON operations_task_event_receipts
		 WHEN NEW.player_uid<>pal_uid_canonical(NEW.player_uid)
		 BEGIN
		  UPDATE operations_task_event_receipts SET player_uid=pal_uid_canonical(NEW.player_uid) WHERE rowid=NEW.rowid;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_task_rewards_player_uid_canonical
		 AFTER INSERT ON operations_task_rewards
		 WHEN NEW.player_uid<>pal_uid_canonical(NEW.player_uid)
		 BEGIN
		  UPDATE operations_task_rewards SET player_uid=pal_uid_canonical(NEW.player_uid) WHERE rowid=NEW.rowid;
		 END`,
	}
}

func taskOnlineIdentityMigrationStatements() []string {
	return []string{
		`ALTER TABLE operations_task_online_tracking RENAME TO operations_task_online_tracking_identity_legacy`,
		`CREATE TABLE operations_task_online_tracking (
			player_uid TEXT PRIMARY KEY COLLATE PALPLAYERUID,
			nickname TEXT NOT NULL DEFAULT '',
			steam_id TEXT NOT NULL DEFAULT '',
			active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0,1)),
			online_since TEXT NOT NULL DEFAULT '',
			last_seen_at TEXT NOT NULL DEFAULT '',
			pending_seconds INTEGER NOT NULL DEFAULT 0 CHECK(pending_seconds >= 0),
			total_emitted_minutes INTEGER NOT NULL DEFAULT 0 CHECK(total_emitted_minutes >= 0),
			updated_at TEXT NOT NULL
		)`,
		`INSERT INTO operations_task_online_tracking(player_uid,nickname,steam_id,active,online_since,last_seen_at,pending_seconds,total_emitted_minutes,updated_at)
		 SELECT pal_uid_canonical(player_uid),COALESCE(MAX(NULLIF(nickname,'')),''),COALESCE(MAX(NULLIF(steam_id,'')),''),
		        MAX(active),COALESCE(MIN(NULLIF(online_since,'')),''),COALESCE(MAX(NULLIF(last_seen_at,'')),''),
		        MAX(pending_seconds),MAX(total_emitted_minutes),MAX(updated_at)
		 FROM operations_task_online_tracking_identity_legacy
		 GROUP BY pal_uid_canonical(player_uid)`,
		`DROP TABLE operations_task_online_tracking_identity_legacy`,
		`CREATE INDEX idx_operations_task_online_tracking_active ON operations_task_online_tracking(active,updated_at DESC)`,
		`CREATE TRIGGER IF NOT EXISTS trg_task_online_player_uid_canonical
		 AFTER INSERT ON operations_task_online_tracking
		 WHEN NEW.player_uid<>pal_uid_canonical(NEW.player_uid)
		 BEGIN
		  UPDATE operations_task_online_tracking SET player_uid=pal_uid_canonical(NEW.player_uid) WHERE rowid=NEW.rowid;
		 END`,
	}
}

func taskMigrationApplied(ctx context.Context, tx *sql.Tx, key string) (bool, error) {
	var applied int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM palpanel_identity_migrations WHERE migration_key=?)`, key).Scan(&applied)
	return applied != 0, err
}

func taskTableExists(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`, strings.TrimSpace(table)).Scan(&exists)
	return exists != 0, err
}

func taskVerifyForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("task identity migration produced an invalid foreign key")
	}
	return rows.Err()
}
