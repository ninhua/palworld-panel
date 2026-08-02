package boss

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	autoExecutionActivityScope   = "server"
	autoExecutionRecoveryActor   = "boss-activity-recovery"
	activityGuardPendingClaimTTL = 30 * time.Second
)

type autoExecutionActivityGuard struct {
	SummonID   string
	PlanKey    string
	AcquiredAt string
	UpdatedAt  string
}

var autoExecutionActivityGuardSchemas sync.Map

func (s *Service) ensureAutoExecutionActivityGuardSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("boss automatic activity guard database is unavailable")
	}
	if _, ok := autoExecutionActivityGuardSchemas.Load(s); ok {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS boss_activity_guards (
		scope TEXT PRIMARY KEY,
		summon_id TEXT NOT NULL,
		plan_key TEXT NOT NULL,
		acquired_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY(summon_id) REFERENCES boss_summons(id) ON DELETE CASCADE
	)`)
	if err != nil {
		return fmt.Errorf("ensure boss automatic activity guard schema: %w", err)
	}
	autoExecutionActivityGuardSchemas.Store(s, struct{}{})
	return nil
}

// reconcileAutoExecutionActivityGuard returns the summon that currently owns
// automatic Boss activity execution. Terminal, deleted, or pending opted-out
// owners are released. Paused or already-started owners retain the activity lock so
// another event cannot start inside a paused event's lifetime.
func (s *Service) reconcileAutoExecutionActivityGuard(ctx context.Context) (string, error) {
	if err := s.ensureAutoExecutionActivityGuardSchema(ctx); err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer rollback(tx)

	guard, found, err := readAutoExecutionActivityGuardTx(ctx, tx)
	if err != nil {
		return "", err
	}
	if !found {
		active, activeFound, err := oldestActiveBossSummonTx(ctx, tx)
		if err != nil {
			return "", err
		}
		if !activeFound {
			if err := tx.Commit(); err != nil {
				return "", err
			}
			return "", nil
		}
		now := s.timestamp()
		inserted, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO boss_activity_guards(scope,summon_id,plan_key,acquired_at,updated_at) VALUES(?,?,?,?,?)`,
			autoExecutionActivityScope, active.ID, autoExecutionPlanKey(active), now, now)
		if err != nil {
			return "", err
		}
		affected, err := inserted.RowsAffected()
		if err != nil {
			return "", err
		}
		if affected == 0 {
			current, currentFound, err := readAutoExecutionActivityGuardTx(ctx, tx)
			if err != nil {
				return "", err
			}
			if err := tx.Commit(); err != nil {
				return "", err
			}
			if currentFound {
				return current.SummonID, nil
			}
			return "", nil
		}
		details, _ := json.Marshal(map[string]any{
			"activity_guard_scope": autoExecutionActivityScope,
			"activity_plan_key":    autoExecutionPlanKey(active),
			"recovered":            true,
		})
		if _, err := tx.ExecContext(ctx, `INSERT INTO boss_summon_events(
			summon_id,from_status,to_status,actor,message,details_json,created_at
		) VALUES(?,?,?,?,?,?,?)`, active.ID, active.Status, active.Status, autoExecutionRecoveryActor,
			"Boss activity lock recovered from active summon", string(details), now); err != nil {
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return active.ID, nil
	}

	retained, err := s.autoExecutionActivityOwnerRetainedTx(ctx, tx, guard)
	if err != nil {
		return "", err
	}
	if !retained {
		if _, err := tx.ExecContext(ctx, `DELETE FROM boss_activity_guards WHERE scope=? AND summon_id=?`, autoExecutionActivityScope, guard.SummonID); err != nil {
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return "", nil
	}

	if _, err := tx.ExecContext(ctx, `UPDATE boss_activity_guards SET updated_at=? WHERE scope=? AND summon_id=?`, s.timestamp(), autoExecutionActivityScope, guard.SummonID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return guard.SummonID, nil
}

func (s *Service) claimAutoExecutionActivity(ctx context.Context, summon Summon) (bool, string, error) {
	return s.claimBossExecutionActivity(ctx, summon, autoExecutionActor, true)
}

// claimBossExecutionActivity serializes every real wave execution through the
// same database row. Automatic calls additionally enforce plan ordering, while
// manual calls may claim a free server but can never bypass an existing owner.
func (s *Service) claimBossExecutionActivity(ctx context.Context, summon Summon, actor string, enforcePlanOrder bool) (bool, string, error) {
	if strings.TrimSpace(summon.ID) == "" {
		return false, "", ErrSummonNotFound
	}
	if err := s.ensureAutoExecutionActivityGuardSchema(ctx); err != nil {
		return false, "", err
	}

	planKey := autoExecutionPlanKey(summon)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, "", err
	}
	defer rollback(tx)

	guard, found, err := readAutoExecutionActivityGuardTx(ctx, tx)
	if err != nil {
		return false, "", err
	}
	if found {
		if guard.SummonID == summon.ID {
			if _, err := tx.ExecContext(ctx, `UPDATE boss_activity_guards SET plan_key=?,updated_at=? WHERE scope=? AND summon_id=?`, planKey, s.timestamp(), autoExecutionActivityScope, summon.ID); err != nil {
				return false, "", err
			}
			if err := tx.Commit(); err != nil {
				return false, "", err
			}
			return true, summon.ID, nil
		}
		retained, err := s.autoExecutionActivityOwnerRetainedTx(ctx, tx, guard)
		if err != nil {
			return false, "", err
		}
		if retained {
			if err := tx.Commit(); err != nil {
				return false, "", err
			}
			return false, guard.SummonID, nil
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM boss_activity_guards WHERE scope=? AND summon_id=?`, autoExecutionActivityScope, guard.SummonID); err != nil {
			return false, "", err
		}
	}

	active, activeFound, err := oldestActiveBossSummonTx(ctx, tx)
	if err != nil {
		return false, "", err
	}
	if activeFound && active.ID != summon.ID {
		if err := tx.Commit(); err != nil {
			return false, "", err
		}
		return false, active.ID, nil
	}
	if activeFound && active.ID == summon.ID {
		// An already-started activity must recover ownership even when an older
		// pending item has the same plan key.
		enforcePlanOrder = false
	}

	if enforcePlanOrder {
		leader, err := autoExecutionPlanLeaderTx(ctx, tx, planKey)
		if err != nil {
			return false, "", err
		}
		if leader != "" && leader != summon.ID {
			if err := tx.Commit(); err != nil {
				return false, "", err
			}
			return false, leader, nil
		}
	}

	now := s.timestamp()
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO boss_activity_guards(scope,summon_id,plan_key,acquired_at,updated_at) VALUES(?,?,?,?,?)`,
		autoExecutionActivityScope, summon.ID, planKey, now, now)
	if err != nil {
		return false, "", err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, "", err
	}
	if affected == 0 {
		current, currentFound, readErr := readAutoExecutionActivityGuardTx(ctx, tx)
		if readErr != nil {
			return false, "", readErr
		}
		if err := tx.Commit(); err != nil {
			return false, "", err
		}
		if currentFound {
			return current.SummonID == summon.ID, current.SummonID, nil
		}
		return false, "", nil
	}

	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = autoExecutionActor
	}
	details, _ := json.Marshal(map[string]any{
		"activity_guard_scope": autoExecutionActivityScope,
		"activity_plan_key":    planKey,
		"automatic":            metadataBool(summon.Metadata, "auto_execute"),
	})
	if _, err := tx.ExecContext(ctx, `INSERT INTO boss_summon_events(
		summon_id,from_status,to_status,actor,message,details_json,created_at
	) VALUES(?,?,?,?,?,?,?)`, summon.ID, summon.Status, summon.Status, actor,
		"Boss activity lock acquired", string(details), now); err != nil {
		return false, "", err
	}
	if err := tx.Commit(); err != nil {
		return false, "", err
	}
	return true, summon.ID, nil
}

func (s *Service) releaseAutoExecutionActivityGuard(ctx context.Context, summonID string) error {
	if strings.TrimSpace(summonID) == "" || s == nil || s.db == nil {
		return nil
	}
	if err := s.ensureAutoExecutionActivityGuardSchema(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM boss_activity_guards WHERE scope=? AND summon_id=?`, autoExecutionActivityScope, strings.TrimSpace(summonID))
	return err
}

func (s *Service) autoExecutionCandidateByID(ctx context.Context, summonID string) (autoExecutionCandidate, bool, error) {
	var item autoExecutionCandidate
	var metadataJSON string
	err := s.db.QueryRowContext(ctx, `SELECT id,requested_at,metadata_json
		FROM boss_summons WHERE id=? AND status IN ('pending','active')`, strings.TrimSpace(summonID)).Scan(
		&item.SummonID, &item.Requested, &metadataJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return autoExecutionCandidate{}, false, nil
	}
	if err != nil {
		return autoExecutionCandidate{}, false, err
	}
	item.Metadata = decodeAutoExecutionActivityMetadata(metadataJSON)
	item.Policy = autoExecutionPolicyFromMetadata(item.Metadata)
	return item, true, nil
}

func readAutoExecutionActivityGuardTx(ctx context.Context, tx *sql.Tx) (autoExecutionActivityGuard, bool, error) {
	var item autoExecutionActivityGuard
	err := tx.QueryRowContext(ctx, `SELECT summon_id,plan_key,acquired_at,updated_at FROM boss_activity_guards WHERE scope=?`, autoExecutionActivityScope).Scan(
		&item.SummonID, &item.PlanKey, &item.AcquiredAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return autoExecutionActivityGuard{}, false, nil
	}
	return item, err == nil, err
}

func (s *Service) autoExecutionActivityOwnerRetainedTx(ctx context.Context, tx *sql.Tx, guard autoExecutionActivityGuard) (bool, error) {
	var status, metadataJSON string
	err := tx.QueryRowContext(ctx, `SELECT status,metadata_json FROM boss_summons WHERE id=?`, strings.TrimSpace(guard.SummonID)).Scan(&status, &metadataJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	now := time.Now()
	if s != nil && s.now != nil {
		now = s.now()
	}
	return retainAutoExecutionActivityGuardAt(status, decodeAutoExecutionActivityMetadata(metadataJSON), guard.AcquiredAt, now), nil
}

func oldestActiveBossSummonTx(ctx context.Context, tx *sql.Tx) (Summon, bool, error) {
	var summon Summon
	var metadataJSON string
	err := tx.QueryRowContext(ctx, `SELECT id,request_key,template_id,status,metadata_json,requested_at
		FROM boss_summons WHERE status='active' ORDER BY requested_at ASC,id ASC LIMIT 1`).Scan(
		&summon.ID, &summon.RequestKey, &summon.TemplateID, &summon.Status, &metadataJSON, &summon.RequestedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Summon{}, false, nil
	}
	if err != nil {
		return Summon{}, false, err
	}
	summon.Metadata = decodeAutoExecutionActivityMetadata(metadataJSON)
	return summon, true, nil
}

func autoExecutionPlanLeaderTx(ctx context.Context, tx *sql.Tx, planKey string) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,request_key,template_id,status,metadata_json,requested_at
		FROM boss_summons
		WHERE status IN ('pending','active')
		ORDER BY requested_at ASC,id ASC`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var summon Summon
		var metadataJSON string
		if err := rows.Scan(&summon.ID, &summon.RequestKey, &summon.TemplateID, &summon.Status, &metadataJSON, &summon.RequestedAt); err != nil {
			return "", err
		}
		summon.Metadata = decodeAutoExecutionActivityMetadata(metadataJSON)
		if !metadataBool(summon.Metadata, "auto_execute") {
			continue
		}
		if autoExecutionPlanKey(summon) == planKey {
			return summon.ID, nil
		}
	}
	return "", rows.Err()
}

func autoExecutionPlanKey(summon Summon) string {
	for _, item := range []struct {
		key    string
		prefix string
	}{
		{key: "auto_execute_plan_key", prefix: "plan:"},
		{key: "schedule_id", prefix: "schedule:"},
		{key: "plan_id", prefix: "plan:"},
	} {
		value, ok := summon.Metadata[item.key].(string)
		value = strings.ToLower(strings.TrimSpace(value))
		if ok && value != "" {
			return item.prefix + value
		}
	}
	return "request:" + strings.ToLower(strings.TrimSpace(summon.RequestKey))
}

func retainAutoExecutionActivityGuard(status string, metadata map[string]any) bool {
	return retainAutoExecutionActivityGuardAt(status, metadata, "", time.Time{})
}

func retainAutoExecutionActivityGuardAt(status string, metadata map[string]any, acquiredAt string, now time.Time) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == SummonStatusActive {
		// Once an activity has started, opting out of future automatic waves must
		// not let a second activity overlap the already-spawned encounter.
		return true
	}
	if status != SummonStatusPending {
		return false
	}
	if metadataBool(metadata, "auto_execute") {
		return true
	}
	// Manual execution claims the guard immediately before changing a wave to
	// active. Keep that pending claim briefly so another process cannot delete it
	// in the small cross-transaction window. A crashed manual claim self-cleans.
	acquired, ok := parseBossTimestamp(acquiredAt)
	if !ok || now.IsZero() {
		return false
	}
	if now.Before(acquired) {
		return true
	}
	return now.Sub(acquired) <= activityGuardPendingClaimTTL
}

func autoExecutionErrorRetainsActivityGuard(err error) bool {
	return errors.Is(err, ErrExecutionReconcileRequired) || errors.Is(err, ErrExecutionUncertain)
}

func decodeAutoExecutionActivityMetadata(value string) map[string]any {
	metadata := map[string]any{}
	if strings.TrimSpace(value) != "" {
		_ = json.Unmarshal([]byte(value), &metadata)
	}
	return metadata
}
