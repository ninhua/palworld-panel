package gameevents

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "palpanel/internal/playeridentity"
)

const gameEventPlayerIdentityMigration = "gameevents-player-uid-canonical-v1"

var gameEventPlayerIdentityReady sync.Map

func init() {
	go func() {
		ticker := time.NewTicker(350 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			serviceCache.Range(func(_, value any) bool {
				service, ok := value.(*Service)
				if !ok || service == nil || service.db == nil {
					return true
				}
				if _, ready := gameEventPlayerIdentityReady.Load(service); ready {
					return true
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				err := service.EnsurePlayerIdentityConsistency(ctx)
				cancel()
				if err == nil {
					gameEventPlayerIdentityReady.Store(service, struct{}{})
				}
				return true
			})
		}
	}()
}

// EnsurePlayerIdentityConsistency canonicalizes stored event identities and
// suppresses PalDefender's outbound SendMsg log lines from the bridge dead
// letter queue. It is safe to run repeatedly.
func (s *Service) EnsurePlayerIdentityConsistency(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return err
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
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
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM palpanel_identity_migrations WHERE migration_key=?)`, gameEventPlayerIdentityMigration).Scan(&applied); err != nil {
		return err
	}
	if applied != 0 {
		return tx.Commit()
	}

	for _, statement := range gameEventIdentityMigrationStatements(s.timestamp()) {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply game event player identity migration: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO palpanel_identity_migrations(migration_key,applied_at) VALUES(?,?)`, gameEventPlayerIdentityMigration, s.timestamp()); err != nil {
		return err
	}
	return tx.Commit()
}

func gameEventIdentityMigrationStatements(now string) []string {
	quotedNow := quoteSQLiteText(now)
	return []string{
		`UPDATE game_events SET player_uid=pal_uid_canonical(player_uid) WHERE player_uid<>''`,
		`UPDATE game_event_bridge_dead_letters SET player_uid=pal_uid_canonical(player_uid) WHERE player_uid<>''`,
		`UPDATE game_event_bridge_observations SET player_uid=pal_uid_canonical(player_uid) WHERE player_uid<>''`,
		`UPDATE game_event_bridge_dead_letters
		 SET status='dismissed',reason='ignored PalDefender outbound system reply',dismissed_at=` + quotedNow + `,updated_at=` + quotedNow + `
		 WHERE status='pending' AND (instr(lower(raw_line),'[player::sendmsg::')>0 OR instr(lower(sample),'[player::sendmsg::')>0)`,
		`DELETE FROM game_event_bridge_observations
		 WHERE status='parse_failed' AND instr(lower(sample),'[player::sendmsg::')>0`,
		`CREATE TRIGGER IF NOT EXISTS trg_game_events_player_uid_canonical
		 AFTER INSERT ON game_events
		 WHEN NEW.player_uid<>pal_uid_canonical(NEW.player_uid)
		 BEGIN
		  UPDATE game_events SET player_uid=pal_uid_canonical(NEW.player_uid) WHERE event_id=NEW.event_id;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_bridge_dead_letters_ignore_sendmsg
		 AFTER INSERT ON game_event_bridge_dead_letters
		 WHEN instr(lower(NEW.raw_line),'[player::sendmsg::')>0 OR instr(lower(NEW.sample),'[player::sendmsg::')>0
		 BEGIN
		  UPDATE game_event_bridge_dead_letters
		  SET status='dismissed',reason='ignored PalDefender outbound system reply',
		      dismissed_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		  WHERE id=NEW.id;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_bridge_observations_ignore_sendmsg
		 AFTER INSERT ON game_event_bridge_observations
		 WHEN NEW.status='parse_failed' AND instr(lower(NEW.sample),'[player::sendmsg::')>0
		 BEGIN
		  DELETE FROM game_event_bridge_observations WHERE id=NEW.id;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_bridge_dead_letters_player_uid_canonical
		 AFTER INSERT ON game_event_bridge_dead_letters
		 WHEN NEW.player_uid<>pal_uid_canonical(NEW.player_uid)
		 BEGIN
		  UPDATE game_event_bridge_dead_letters SET player_uid=pal_uid_canonical(NEW.player_uid) WHERE id=NEW.id;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_bridge_observations_player_uid_canonical
		 AFTER INSERT ON game_event_bridge_observations
		 WHEN NEW.player_uid<>pal_uid_canonical(NEW.player_uid)
		 BEGIN
		  UPDATE game_event_bridge_observations SET player_uid=pal_uid_canonical(NEW.player_uid) WHERE id=NEW.id;
		 END`,
	}
}

func quoteSQLiteText(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
