package gameevents

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const bridgeRuntimeRepairV2Interval = 500 * time.Millisecond

var bridgeRuntimeRepairV2Ready sync.Map

func init() {
	go func() {
		ticker := time.NewTicker(bridgeRuntimeRepairV2Interval)
		defer ticker.Stop()
		for range ticker.C {
			serviceCache.Range(func(_, value any) bool {
				service, ok := value.(*Service)
				if !ok || service == nil || service.db == nil {
					return true
				}
				if _, ready := bridgeRuntimeRepairV2Ready.Load(service); ready {
					return true
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				err := service.EnsureBridgeRuntimeRepairV2(ctx)
				cancel()
				if err == nil {
					bridgeRuntimeRepairV2Ready.Store(service, struct{}{})
				}
				return true
			})
		}
	}()
}

// EnsureBridgeRuntimeRepairV2 deliberately does not reuse the v1 migration
// marker. Some installations applied the identity migration before the noise
// filters were present, so the old marker can no longer prove that the runtime
// triggers exist. All statements below are idempotent.
func (s *Service) EnsureBridgeRuntimeRepairV2(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.EnsureBridgeSchema(ctx); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	statements := bridgeRuntimeRepairV2Statements(s.timestamp())
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply bridge runtime repair v2: %w", err)
		}
	}
	return tx.Commit()
}

func bridgeRuntimeRepairV2Statements(now string) []string {
	quotedNow := quoteSQLiteText(now)
	return []string{
		// Repair historical PalDefender outbound replies. These lines are emitted
		// by the server itself and must never enter the player command pipeline.
		`UPDATE game_event_bridge_dead_letters
		 SET status='dismissed',reason='ignored PalDefender outbound system reply (repair v2)',
		     dismissed_at=` + quotedNow + `,updated_at=` + quotedNow + `
		 WHERE status='pending'
		   AND (instr(lower(raw_line),'[player::sendmsg::')>0 OR instr(lower(sample),'[player::sendmsg::')>0)`,
		`DELETE FROM game_event_bridge_observations
		 WHERE status IN ('parse_failed','unmatched_player','error')
		   AND instr(lower(sample),'[player::sendmsg::')>0`,

		// The supplied sample is a player death, not a Pal killed by the player.
		// Until PLAYER_DIED tasks are implemented, keep it out of PAL_KILLED and
		// out of the actionable dead-letter queue.
		`UPDATE game_event_bridge_dead_letters
		 SET status='dismissed',reason='ignored player-death informational log; not a PAL_KILLED event',
		     dismissed_at=` + quotedNow + `,updated_at=` + quotedNow + `
		 WHERE status='pending' AND event_type IN ('PARSE_FAILED','UNKNOWN')
		   AND instr(lower(raw_line),'userid=')>0
		   AND instr(lower(raw_line),'uid=')>0
		   AND instr(lower(raw_line),'was attacked by a wild')>0
		   AND instr(lower(raw_line),'and died')>0`,
		`DELETE FROM game_event_bridge_observations
		 WHERE status='parse_failed'
		   AND instr(lower(sample),'userid=')>0
		   AND instr(lower(sample),'uid=')>0
		   AND instr(lower(sample),'was attacked by a wild')>0
		   AND instr(lower(sample),'and died')>0`,

		// Recover a unique PlayerUID for historical and future login dead letters
		// from the economy account table. The row stays pending so an admin can
		// replay it when PLAYER_LOGIN tasks are actually configured.
		`UPDATE game_event_bridge_dead_letters
		 SET player_uid=(
		       SELECT MIN(player_uid) FROM economy_accounts
		       WHERE status='active' AND lower(steam_id)=lower(game_event_bridge_dead_letters.player_hint)
		     ),
		     steam_id=CASE WHEN steam_id='' THEN player_hint ELSE steam_id END,
		     nickname=CASE WHEN nickname GLOB '*[0-9].[0-9]*' THEN '' ELSE nickname END,
		     reason='login identity recovered from the unique economy account; replay when needed',
		     updated_at=` + quotedNow + `
		 WHERE status='pending' AND event_type='PLAYER_LOGIN' AND player_uid=''
		   AND player_hint<>''
		   AND 1=(SELECT COUNT(*) FROM economy_accounts
		          WHERE status='active' AND lower(steam_id)=lower(game_event_bridge_dead_letters.player_hint))`,

		`CREATE TRIGGER IF NOT EXISTS trg_bridge_v2_ignore_sendmsg
		 AFTER INSERT ON game_event_bridge_dead_letters
		 WHEN instr(lower(NEW.raw_line),'[player::sendmsg::')>0
		   OR instr(lower(NEW.sample),'[player::sendmsg::')>0
		 BEGIN
		  UPDATE game_event_bridge_dead_letters
		  SET status='dismissed',reason='ignored PalDefender outbound system reply (repair v2)',
		      dismissed_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),
		      updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		  WHERE id=NEW.id;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_bridge_v2_delete_sendmsg_observation
		 AFTER INSERT ON game_event_bridge_observations
		 WHEN instr(lower(NEW.sample),'[player::sendmsg::')>0
		 BEGIN
		  DELETE FROM game_event_bridge_observations WHERE id=NEW.id;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_bridge_v2_ignore_player_death
		 AFTER INSERT ON game_event_bridge_dead_letters
		 WHEN NEW.event_type IN ('PARSE_FAILED','UNKNOWN')
		   AND instr(lower(NEW.raw_line),'userid=')>0
		   AND instr(lower(NEW.raw_line),'uid=')>0
		   AND instr(lower(NEW.raw_line),'was attacked by a wild')>0
		   AND instr(lower(NEW.raw_line),'and died')>0
		 BEGIN
		  UPDATE game_event_bridge_dead_letters
		  SET status='dismissed',reason='ignored player-death informational log; not a PAL_KILLED event',
		      dismissed_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),
		      updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		  WHERE id=NEW.id;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_bridge_v2_delete_player_death_observation
		 AFTER INSERT ON game_event_bridge_observations
		 WHEN NEW.status='parse_failed'
		   AND instr(lower(NEW.sample),'userid=')>0
		   AND instr(lower(NEW.sample),'uid=')>0
		   AND instr(lower(NEW.sample),'was attacked by a wild')>0
		   AND instr(lower(NEW.sample),'and died')>0
		 BEGIN
		  DELETE FROM game_event_bridge_observations WHERE id=NEW.id;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_bridge_v2_resolve_login_account
		 AFTER INSERT ON game_event_bridge_dead_letters
		 WHEN NEW.status='pending' AND NEW.event_type='PLAYER_LOGIN'
		   AND NEW.player_uid='' AND NEW.player_hint<>''
		   AND 1=(SELECT COUNT(*) FROM economy_accounts
		          WHERE status='active' AND lower(steam_id)=lower(NEW.player_hint))
		 BEGIN
		  UPDATE game_event_bridge_dead_letters
		  SET player_uid=(SELECT MIN(player_uid) FROM economy_accounts
		                  WHERE status='active' AND lower(steam_id)=lower(NEW.player_hint)),
		      steam_id=CASE WHEN NEW.steam_id='' THEN NEW.player_hint ELSE NEW.steam_id END,
		      nickname='',
		      reason='login identity recovered from the unique economy account; replay when needed',
		      updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		  WHERE id=NEW.id;
		 END`,
	}
}
