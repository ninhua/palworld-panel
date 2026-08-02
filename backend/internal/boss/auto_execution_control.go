package boss

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidAutoExecutionControl = errors.New("boss auto execution control action is invalid")
	ErrAutoExecutionDisabled       = errors.New("boss auto execution is not enabled for this summon")
	ErrAutoExecutionNoPendingWave  = errors.New("boss summon has no pending wave to skip")
)

const (
	AutoExecutionControlPause       = "pause"
	AutoExecutionControlResume      = "resume"
	AutoExecutionControlSkipCurrent = "skip_current"
)

// ApplyAutoExecutionControl applies a persisted control action to an opt-in
// automatic Boss summon. The existing summon transition endpoint calls this
// method for pause, resume and skip_current actions.
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
