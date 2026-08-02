package boss

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidAutoExecutionControl = errors.New("boss auto execution control action is invalid")
	ErrAutoExecutionDisabled       = errors.New("boss auto execution is not enabled for this summon")
	ErrAutoExecutionNoPendingWave  = errors.New("boss summon has no pending wave to skip")
	ErrAutoExecutionNoFailedWave   = errors.New("boss summon has no failed wave to retry")
)

const (
	AutoExecutionControlPause       = "pause"
	AutoExecutionControlResume      = "resume"
	AutoExecutionControlSkipCurrent = "skip_current"
	AutoExecutionControlRetryFailed = "retry_failed"

	defaultAutoExecutionRetryDelay = 5 * time.Second
	maximumAutoExecutionRetryDelay = time.Hour
)

// ApplyAutoExecutionControl applies a persisted control action to an opt-in
// automatic Boss summon. The existing summon transition endpoint calls this
// method for pause, resume, skip_current and retry_failed actions.
func (s *Service) ApplyAutoExecutionControl(ctx context.Context, summonID, action, actor string) (Summon, error) {
	summonID = strings.TrimSpace(summonID)
	actor = strings.TrimSpace(actor)
	if summonID == "" {
		return Summon{}, ErrSummonNotFound
	}

	action, ok := normalizeAutoExecutionControlAction(action)
	if !ok {
		return Summon{}, ErrInvalidAutoExecutionControl
	}

	holder := newID("boss_auto_control")
	acquired, err := s.acquireAutoExecutionLease(ctx, holder, autoExecutionControlNow(s))
	if err != nil {
		return Summon{}, err
	}
	if !acquired {
		return Summon{}, ErrExecutionBusy
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.releaseAutoExecutionLease(releaseCtx, holder)
	}()

	switch action {
	case AutoExecutionControlPause:
		return s.setAutoExecutionPaused(ctx, summonID, true, actor)
	case AutoExecutionControlResume:
		return s.setAutoExecutionPaused(ctx, summonID, false, actor)
	case AutoExecutionControlSkipCurrent:
		return s.skipCurrentAutoExecutionWave(ctx, summonID, actor)
	case AutoExecutionControlRetryFailed:
		return s.retryFailedAutoExecutionWave(ctx, summonID, actor)
	default:
		return Summon{}, ErrInvalidAutoExecutionControl
	}
}

func IsAutoExecutionControlAction(action string) bool {
	_, ok := normalizeAutoExecutionControlAction(action)
	return ok
}

func normalizeAutoExecutionControlAction(action string) (string, bool) {
	action = strings.ToLower(strings.TrimSpace(action))
	action = strings.ReplaceAll(action, "-", "_")
	switch action {
	case AutoExecutionControlPause, "paused", "pause_auto", "pause_automation":
		return AutoExecutionControlPause, true
	case AutoExecutionControlResume, "continue", "continued", "resume_auto", "resume_automation":
		return AutoExecutionControlResume, true
	case AutoExecutionControlSkipCurrent, "skip", "skip_wave", "skip_current_wave":
		return AutoExecutionControlSkipCurrent, true
	case AutoExecutionControlRetryFailed, "retry", "retry_wave", "retry_failed_wave":
		return AutoExecutionControlRetryFailed, true
	default:
		return "", false
	}
}

func autoExecutionControlNow(s *Service) time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Service) setAutoExecutionPaused(ctx context.Context, summonID string, paused bool, actor string) (Summon, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Summon{}, err
	}
	defer rollback(tx)

	var status string
	var metadataJSON string
	if err := tx.QueryRowContext(ctx,
		`SELECT status,metadata_json FROM boss_summons WHERE id=?`, summonID,
	).Scan(&status, &metadataJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Summon{}, ErrSummonNotFound
		}
		return Summon{}, err
	}
	if terminalStatus(status) {
		return Summon{}, ErrInvalidTransition
	}

	metadata := map[string]any{}
	if strings.TrimSpace(metadataJSON) != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			return Summon{}, fmt.Errorf("decode boss summon metadata: %w", err)
		}
	}
	if !metadataBool(metadata, "auto_execute") {
		return Summon{}, ErrAutoExecutionDisabled
	}
	if metadataBool(metadata, "auto_execute_paused") == paused {
		if err := tx.Commit(); err != nil {
			return Summon{}, err
		}
		return s.GetSummon(ctx, summonID)
	}

	metadata["auto_execute_paused"] = paused
	metadata["auto_execute_controlled_at"] = autoExecutionControlNow(s).UTC().Format(time.RFC3339Nano)
	if actor != "" {
		metadata["auto_execute_controlled_by"] = actor
	}
	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return Summon{}, err
	}
	if len(encodedMetadata) > maximumJSON {
		return Summon{}, ErrInvalidSummon
	}

	now := autoExecutionControlNow(s).UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		`UPDATE boss_summons SET metadata_json=?,actor=?,updated_at=? WHERE id=?`,
		string(encodedMetadata), actor, now, summonID,
	); err != nil {
		return Summon{}, err
	}

	action := AutoExecutionControlResume
	message := "Boss automatic wave execution resumed"
	if paused {
		action = AutoExecutionControlPause
		message = "Boss automatic wave execution paused"
	}
	details, _ := json.Marshal(map[string]any{
		"auto_execution_control": action,
		"auto_execute_paused":    paused,
	})
	if _, err := tx.ExecContext(ctx, `INSERT INTO boss_summon_events(
		summon_id,from_status,to_status,actor,message,details_json,created_at
	) VALUES(?,?,?,?,?,?,?)`, summonID, status, status, actor, message, string(details), now); err != nil {
		return Summon{}, err
	}
	if err := tx.Commit(); err != nil {
		return Summon{}, err
	}
	return s.GetSummon(ctx, summonID)
}

func (s *Service) skipCurrentAutoExecutionWave(ctx context.Context, summonID, actor string) (Summon, error) {
	summon, err := s.GetSummon(ctx, summonID)
	if err != nil {
		return Summon{}, err
	}
	if terminalStatus(summon.Status) {
		return Summon{}, ErrInvalidTransition
	}
	if !metadataBool(summon.Metadata, "auto_execute") {
		return Summon{}, ErrAutoExecutionDisabled
	}

	if err := s.ensureExecutionSchema(ctx); err != nil {
		return Summon{}, err
	}
	blocked, err := s.autoExecutionBlocked(ctx, summonID)
	if err != nil {
		return Summon{}, err
	}
	if blocked {
		return Summon{}, ErrExecutionReconcileRequired
	}

	waves, err := s.ListSummonWaves(ctx, summonID)
	if err != nil {
		return Summon{}, err
	}
	position, ok := currentPendingAutoExecutionWave(waves)
	if !ok {
		return Summon{}, ErrAutoExecutionNoPendingWave
	}
	if _, err := s.TransitionSummonWave(ctx, summonID, position, WaveTransitionRequest{
		Status:  WaveStatusSkipped,
		Message: fmt.Sprintf("wave %d skipped by automatic execution control", position),
		Result: map[string]any{
			"auto_execution_control": AutoExecutionControlSkipCurrent,
		},
	}, actor); err != nil {
		return Summon{}, err
	}
	return s.GetSummon(ctx, summonID)
}

// retryFailedAutoExecutionWave safely reopens the first failed wave. It never
// retries uncertain or partially completed execution attempts. The automatic
// worker performs the actual execution after the persisted retry delay.
func (s *Service) retryFailedAutoExecutionWave(ctx context.Context, summonID, actor string) (Summon, error) {
	if err := s.ensureExecutionSchema(ctx); err != nil {
		return Summon{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Summon{}, err
	}
	defer rollback(tx)

	var summonStatus, metadataJSON string
	if err := tx.QueryRowContext(ctx,
		`SELECT status,metadata_json FROM boss_summons WHERE id=?`, summonID,
	).Scan(&summonStatus, &metadataJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Summon{}, ErrSummonNotFound
		}
		return Summon{}, err
	}
	if summonStatus != SummonStatusFailed {
		return Summon{}, ErrAutoExecutionNoFailedWave
	}

	metadata := map[string]any{}
	if strings.TrimSpace(metadataJSON) != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			return Summon{}, fmt.Errorf("decode boss summon metadata: %w", err)
		}
	}
	if !metadataBool(metadata, "auto_execute") {
		return Summon{}, ErrAutoExecutionDisabled
	}

	var activeWaves, unresolvedAttempts int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM boss_summon_waves WHERE summon_id=? AND status='active'`, summonID,
	).Scan(&activeWaves); err != nil {
		return Summon{}, err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM boss_execution_attempts WHERE summon_id=? AND status IN ('running','uncertain')`, summonID,
	).Scan(&unresolvedAttempts); err != nil {
		return Summon{}, err
	}
	if activeWaves > 0 || unresolvedAttempts > 0 {
		return Summon{}, ErrExecutionReconcileRequired
	}

	var position int
	var waveName string
	if err := tx.QueryRowContext(ctx,
		`SELECT position,name FROM boss_summon_waves WHERE summon_id=? AND status='failed' ORDER BY position LIMIT 1`,
		summonID,
	).Scan(&position, &waveName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Summon{}, ErrAutoExecutionNoFailedWave
		}
		return Summon{}, err
	}

	var attemptID, attemptStatus string
	var completedCommands int
	if err := tx.QueryRowContext(ctx,
		`SELECT id,status,completed_commands FROM boss_execution_attempts
		 WHERE summon_id=? AND wave_position=? ORDER BY created_at DESC,id DESC LIMIT 1`,
		summonID, position,
	).Scan(&attemptID, &attemptStatus, &completedCommands); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Summon{}, ErrExecutionReconcileRequired
		}
		return Summon{}, err
	}
	if attemptStatus != ExecutionAttemptFailed || completedCommands != 0 {
		return Summon{}, ErrExecutionReconcileRequired
	}

	var previousFailedAttempts int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM boss_execution_attempts WHERE summon_id=? AND wave_position=? AND status='failed'`,
		summonID, position,
	).Scan(&previousFailedAttempts); err != nil {
		return Summon{}, err
	}

	nowTime := autoExecutionControlNow(s).UTC()
	now := nowTime.Format(time.RFC3339Nano)
	retryDelay := autoExecutionRetryDelay(metadata)
	notBefore := nowTime.Add(retryDelay).Format(time.RFC3339Nano)

	metadata["auto_execute_paused"] = false
	metadata["auto_execute_not_before"] = notBefore
	metadata["auto_execute_retry_count"] = autoExecutionMetadataInt(metadata, "auto_execute_retry_count") + 1
	metadata["auto_execute_retry_wave_position"] = position
	metadata["auto_execute_retry_requested_at"] = now
	metadata["auto_execute_controlled_at"] = now
	if actor != "" {
		metadata["auto_execute_retry_requested_by"] = actor
		metadata["auto_execute_controlled_by"] = actor
	}
	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return Summon{}, err
	}
	if len(encodedMetadata) > maximumJSON {
		return Summon{}, ErrInvalidSummon
	}

	result, err := tx.ExecContext(ctx, `UPDATE boss_summon_waves SET
		status='pending',actor=?,result_json='{}',failure='',started_at='',completed_at='',updated_at=?
		WHERE summon_id=? AND position=? AND status='failed'`, actor, now, summonID, position)
	if err != nil {
		return Summon{}, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return Summon{}, err
		}
		return Summon{}, ErrAutoExecutionNoFailedWave
	}

	restored, err := tx.ExecContext(ctx, `UPDATE boss_summon_waves SET
		status='pending',actor=?,result_json='{}',failure='',started_at='',completed_at='',updated_at=?
		WHERE summon_id=? AND position>? AND status='skipped' AND failure='summon terminated'`,
		actor, now, summonID, position,
	)
	if err != nil {
		return Summon{}, err
	}
	restoredWaves, err := restored.RowsAffected()
	if err != nil {
		return Summon{}, err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE boss_summons SET
		status='active',metadata_json=?,actor=?,failure='',completed_at='',updated_at=? WHERE id=?`,
		string(encodedMetadata), actor, now, summonID,
	); err != nil {
		return Summon{}, err
	}

	details, _ := json.Marshal(map[string]any{
		"auto_execution_control":      AutoExecutionControlRetryFailed,
		"wave_position":               position,
		"wave_name":                   waveName,
		"previous_attempt_id":         attemptID,
		"previous_failed_attempts":    previousFailedAttempts,
		"restored_following_waves":    restoredWaves,
		"retry_delay_seconds":         int64(retryDelay / time.Second),
		"retry_not_before":            notBefore,
		"uncertain_attempts_rejected": true,
	})
	message := fmt.Sprintf("Boss wave %d retry scheduled after a confirmed failed execution", position)
	if _, err := tx.ExecContext(ctx, `INSERT INTO boss_summon_events(
		summon_id,from_status,to_status,actor,message,details_json,created_at
	) VALUES(?,?,?,?,?,?,?)`, summonID, SummonStatusFailed, SummonStatusActive, actor, message, string(details), now); err != nil {
		return Summon{}, err
	}

	if err := tx.Commit(); err != nil {
		return Summon{}, err
	}
	return s.GetSummon(ctx, summonID)
}

func autoExecutionRetryDelay(metadata map[string]any) time.Duration {
	seconds := int64(defaultAutoExecutionRetryDelay / time.Second)
	if value, ok := metadata["auto_execute_retry_delay_seconds"]; ok {
		switch typed := value.(type) {
		case float64:
			seconds = int64(typed)
		case int:
			seconds = int64(typed)
		case int64:
			seconds = typed
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
				seconds = parsed
			}
		}
	}
	if seconds < 0 {
		seconds = 0
	}
	maximum := int64(maximumAutoExecutionRetryDelay / time.Second)
	if seconds > maximum {
		seconds = maximum
	}
	return time.Duration(seconds) * time.Second
}

func autoExecutionMetadataInt(metadata map[string]any, key string) int64 {
	value, ok := metadata[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func currentPendingAutoExecutionWave(waves []SummonWave) (int, bool) {
	for _, wave := range waves {
		switch wave.Status {
		case WaveStatusActive, WaveStatusFailed:
			return 0, false
		case WaveStatusPending:
			return wave.Position, true
		}
	}
	return 0, false
}
