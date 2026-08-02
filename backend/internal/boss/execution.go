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

var (
	ErrExecutionUnavailable       = errors.New("boss execution adapter is unavailable")
	ErrExecutionBusy              = errors.New("another boss execution is already running")
	ErrExecutionReconcileRequired = errors.New("boss execution requires manual reconciliation")
	ErrExecutionFailed            = errors.New("boss execution failed")
	ErrExecutionUncertain         = errors.New("boss execution result is uncertain")
)

const (
	ExecutionAttemptRunning   = "running"
	ExecutionAttemptSucceeded = "succeeded"
	ExecutionAttemptFailed    = "failed"
	ExecutionAttemptUncertain = "uncertain"
)

type ExecutionCapabilities struct {
	FixedCoordinates  bool `json:"fixed_coordinates"`
	MultipleSpawns    bool `json:"multiple_spawns"`
	Uncapturable      bool `json:"uncapturable"`
	CustomPalTemplate bool `json:"custom_pal_template"`
	ExactMultipliers  bool `json:"exact_multipliers"`
}

type ExecutionAdapterStatus struct {
	Adapter      string                `json:"adapter"`
	Available    bool                  `json:"available"`
	State        string                `json:"state"`
	Message      string                `json:"message,omitempty"`
	Capabilities ExecutionCapabilities `json:"capabilities"`
	Limitations  []string              `json:"limitations"`
}

type ExecutionStatus struct {
	ExecutionAdapterStatus
	Busy                   bool  `json:"busy"`
	RunningAttempts        int64 `json:"running_attempts"`
	UncertainAttempts      int64 `json:"uncertain_attempts"`
	ActiveWaves            int64 `json:"active_waves"`
	ReconciliationRequired bool  `json:"reconciliation_required"`
}

type WaveExecutionResult struct {
	Adapter           string         `json:"adapter"`
	Commands          []string       `json:"commands"`
	Responses         []string       `json:"responses"`
	CompletedCommands int            `json:"completed_commands"`
	Details           map[string]any `json:"details"`
}

type ExecutionAttempt struct {
	ID                string         `json:"id"`
	SummonID          string         `json:"summon_id"`
	WavePosition      int            `json:"wave_position"`
	Adapter           string         `json:"adapter"`
	Status            string         `json:"status"`
	CommandCount      int            `json:"command_count"`
	CompletedCommands int            `json:"completed_commands"`
	Commands          []string       `json:"commands"`
	Responses         []string       `json:"responses"`
	Details           map[string]any `json:"details"`
	Failure           string         `json:"failure,omitempty"`
	Actor             string         `json:"actor,omitempty"`
	StartedAt         string         `json:"started_at"`
	CompletedAt       string         `json:"completed_at,omitempty"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type WaveExecutor interface {
	Status(context.Context) ExecutionAdapterStatus
	ExecuteWave(context.Context, Summon, SummonWave) (WaveExecutionResult, error)
}

type ExecutionError struct {
	Err       error
	Uncertain bool
}

func (e *ExecutionError) Error() string {
	if e == nil || e.Err == nil {
		return ErrExecutionFailed.Error()
	}
	return e.Err.Error()
}

func (e *ExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type executionRuntime struct {
	adapterMu sync.RWMutex
	adapter   WaveExecutor
	semaphore chan struct{}
	schemaMu  sync.Mutex
	schemaOK  bool
	busyMu    sync.RWMutex
	busy      bool
}

var executionRuntimeCache sync.Map

func executionRuntimeFor(service *Service) *executionRuntime {
	if service == nil {
		return &executionRuntime{semaphore: make(chan struct{}, 1)}
	}
	if cached, ok := executionRuntimeCache.Load(service); ok {
		return cached.(*executionRuntime)
	}
	runtime := &executionRuntime{semaphore: make(chan struct{}, 1)}
	actual, _ := executionRuntimeCache.LoadOrStore(service, runtime)
	return actual.(*executionRuntime)
}

func (s *Service) SetExecutionAdapter(adapter WaveExecutor) {
	runtime := executionRuntimeFor(s)
	runtime.adapterMu.Lock()
	runtime.adapter = adapter
	runtime.adapterMu.Unlock()
}

func (s *Service) executionAdapter() WaveExecutor {
	runtime := executionRuntimeFor(s)
	runtime.adapterMu.RLock()
	adapter := runtime.adapter
	runtime.adapterMu.RUnlock()
	return adapter
}

func (s *Service) ensureExecutionSchema(ctx context.Context) error {
	runtime := executionRuntimeFor(s)
	runtime.schemaMu.Lock()
	defer runtime.schemaMu.Unlock()
	if runtime.schemaOK {
		return nil
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS boss_execution_attempts (
			id TEXT PRIMARY KEY,
			summon_id TEXT NOT NULL,
			wave_position INTEGER NOT NULL CHECK(wave_position BETWEEN 1 AND 20),
			adapter TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('running','succeeded','failed','uncertain')),
			command_count INTEGER NOT NULL DEFAULT 0,
			completed_commands INTEGER NOT NULL DEFAULT 0,
			commands_json TEXT NOT NULL DEFAULT '[]',
			responses_json TEXT NOT NULL DEFAULT '[]',
			details_json TEXT NOT NULL DEFAULT '{}',
			failure TEXT NOT NULL DEFAULT '',
			actor TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			completed_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(summon_id) REFERENCES boss_summons(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_execution_attempts_summon ON boss_execution_attempts(summon_id,id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_execution_attempts_status ON boss_execution_attempts(status,updated_at DESC)`,
		`UPDATE boss_execution_attempts SET status='uncertain',failure=CASE WHEN failure='' THEN 'PalPanel restarted while execution was running; verify the game state before retrying' ELSE failure END,completed_at=CASE WHEN completed_at='' THEN CURRENT_TIMESTAMP ELSE completed_at END,updated_at=CURRENT_TIMESTAMP WHERE status='running'`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure boss execution schema: %w", err)
		}
	}
	runtime.schemaOK = true
	return nil
}

func (s *Service) ExecutionStatus(ctx context.Context) (ExecutionStatus, error) {
	if err := s.ensureExecutionSchema(ctx); err != nil {
		return ExecutionStatus{}, err
	}
	adapter := s.executionAdapter()
	adapterStatus := ExecutionAdapterStatus{
		Adapter: "record_only", Available: false, State: "not_configured",
		Message:      "Boss execution adapter is not configured.",
		Capabilities: ExecutionCapabilities{},
		Limitations:  []string{"Only summon records and wave snapshots are available."},
	}
	if adapter != nil {
		adapterStatus = adapter.Status(ctx)
	}
	var running, uncertain, active int64
	if err := s.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN status='running' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='uncertain' THEN 1 ELSE 0 END),0)
		FROM boss_execution_attempts`).Scan(&running, &uncertain); err != nil {
		return ExecutionStatus{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM boss_summon_waves WHERE status='active'`).Scan(&active); err != nil {
		return ExecutionStatus{}, err
	}
	runtime := executionRuntimeFor(s)
	runtime.busyMu.RLock()
	busy := runtime.busy
	runtime.busyMu.RUnlock()
	return ExecutionStatus{
		ExecutionAdapterStatus: adapterStatus,
		Busy:                   busy, RunningAttempts: running, UncertainAttempts: uncertain, ActiveWaves: active,
		ReconciliationRequired: running > 0 || active > 0,
	}, nil
}

func (s *Service) ExecuteNextWave(ctx context.Context, summonID, actor string) (ExecutionAttempt, error) {
	summonID = strings.TrimSpace(summonID)
	actor = strings.TrimSpace(actor)
	if summonID == "" {
		return ExecutionAttempt{}, ErrSummonNotFound
	}
	if err := s.ensureExecutionSchema(ctx); err != nil {
		return ExecutionAttempt{}, err
	}
	runtime := executionRuntimeFor(s)
	select {
	case runtime.semaphore <- struct{}{}:
		defer func() { <-runtime.semaphore }()
	default:
		return ExecutionAttempt{}, ErrExecutionBusy
	}
	runtime.busyMu.Lock()
	runtime.busy = true
	runtime.busyMu.Unlock()
	defer func() {
		runtime.busyMu.Lock()
		runtime.busy = false
		runtime.busyMu.Unlock()
	}()

	adapter := s.executionAdapter()
	if adapter == nil {
		return ExecutionAttempt{}, ErrExecutionUnavailable
	}
	adapterStatus := adapter.Status(ctx)
	if !adapterStatus.Available {
		message := strings.TrimSpace(adapterStatus.Message)
		if message == "" {
			message = ErrExecutionUnavailable.Error()
		}
		return ExecutionAttempt{}, fmt.Errorf("%w: %s", ErrExecutionUnavailable, message)
	}
	summon, err := s.GetSummon(ctx, summonID)
	if err != nil {
		return ExecutionAttempt{}, err
	}
	if terminalStatus(summon.Status) {
		return ExecutionAttempt{}, ErrInvalidTransition
	}

	guardClaimed := false
	retainActivityGuard := false
	claimed, ownerID, err := s.claimBossExecutionActivity(ctx, summon, actor, metadataBool(summon.Metadata, "auto_execute"))
	if err != nil {
		return ExecutionAttempt{}, err
	}
	if !claimed {
		if strings.TrimSpace(ownerID) == "" {
			return ExecutionAttempt{}, ErrExecutionBusy
		}
		return ExecutionAttempt{}, fmt.Errorf("%w: Boss activity is owned by summon %s", ErrExecutionBusy, ownerID)
	}
	guardClaimed = true
	defer func() {
		if !guardClaimed || retainActivityGuard {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.releaseAutoExecutionActivityGuard(releaseCtx, summonID)
	}()

	waves, err := s.ListSummonWaves(ctx, summonID)
	if err != nil {
		return ExecutionAttempt{}, err
	}
	var selected *SummonWave
	for index := range waves {
		if waves[index].Status == WaveStatusActive {
			retainActivityGuard = true
			return ExecutionAttempt{}, ErrExecutionReconcileRequired
		}
		if selected == nil && waves[index].Status == WaveStatusPending {
			copy := waves[index]
			selected = &copy
		}
	}
	if selected == nil {
		return ExecutionAttempt{}, ErrInvalidWaveTransition
	}

	now := s.timestamp()
	attempt := ExecutionAttempt{
		ID: newID("boss_exec"), SummonID: summonID, WavePosition: selected.Position,
		Adapter: adapterStatus.Adapter, Status: ExecutionAttemptRunning, CommandCount: selected.Count,
		Commands: []string{}, Responses: []string{}, Details: map[string]any{}, Actor: actor,
		StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.insertExecutionAttempt(ctx, attempt); err != nil {
		return ExecutionAttempt{}, err
	}
	_, err = s.TransitionSummonWave(ctx, summonID, selected.Position, WaveTransitionRequest{
		Status:  WaveStatusActive,
		Message: fmt.Sprintf("wave %d execution started via %s", selected.Position, adapterStatus.Adapter),
		Result:  map[string]any{"execution_attempt_id": attempt.ID, "adapter": adapterStatus.Adapter},
	}, actor)
	if err != nil {
		_ = s.finishExecutionAttempt(ctx, attempt.ID, ExecutionAttemptFailed, WaveExecutionResult{Adapter: adapterStatus.Adapter}, err.Error())
		current, _ := s.GetExecutionAttempt(ctx, attempt.ID)
		return current, err
	}
	retainActivityGuard = true

	result, executeErr := adapter.ExecuteWave(ctx, summon, *selected)
	if result.Adapter == "" {
		result.Adapter = adapterStatus.Adapter
	}
	if executeErr == nil {
		if err := s.finishExecutionAttempt(ctx, attempt.ID, ExecutionAttemptSucceeded, result, ""); err != nil {
			return ExecutionAttempt{}, err
		}
		_, transitionErr := s.TransitionSummonWave(ctx, summonID, selected.Position, WaveTransitionRequest{
			Status:  WaveStatusCompleted,
			Message: fmt.Sprintf("wave %d executed successfully via %s", selected.Position, result.Adapter),
			Result: map[string]any{
				"execution_attempt_id": attempt.ID,
				"adapter":              result.Adapter,
				"completed_commands":   result.CompletedCommands,
				"details":              result.Details,
			},
		}, actor)
		if transitionErr != nil {
			return ExecutionAttempt{}, transitionErr
		}
		_, _ = s.reconcileAutoExecutionActivityGuard(ctx)
		return s.GetExecutionAttempt(ctx, attempt.ID)
	}

	uncertain := false
	var typed *ExecutionError
	if errors.As(executeErr, &typed) {
		uncertain = typed.Uncertain
	}
	status := ExecutionAttemptFailed
	wrapped := ErrExecutionFailed
	if uncertain || result.CompletedCommands > 0 {
		status = ExecutionAttemptUncertain
		wrapped = ErrExecutionUncertain
	}
	if err := s.finishExecutionAttempt(ctx, attempt.ID, status, result, executeErr.Error()); err != nil {
		return ExecutionAttempt{}, err
	}
	if status == ExecutionAttemptUncertain {
		_ = s.appendExecutionAudit(ctx, summonID, actor,
			fmt.Sprintf("wave %d execution is uncertain; verify spawned Pals before resolving the active wave", selected.Position),
			map[string]any{"execution_attempt_id": attempt.ID, "adapter": result.Adapter, "completed_commands": result.CompletedCommands, "failure": executeErr.Error()})
		current, _ := s.GetExecutionAttempt(ctx, attempt.ID)
		return current, fmt.Errorf("%w: %v", wrapped, executeErr)
	}
	_, transitionErr := s.TransitionSummonWave(ctx, summonID, selected.Position, WaveTransitionRequest{
		Status:  WaveStatusFailed,
		Message: executeErr.Error(),
		Result:  map[string]any{"execution_attempt_id": attempt.ID, "adapter": result.Adapter},
	}, actor)
	if transitionErr == nil {
		retainActivityGuard = false
	}
	current, _ := s.GetExecutionAttempt(ctx, attempt.ID)
	return current, fmt.Errorf("%w: %v", wrapped, executeErr)
}

func (s *Service) insertExecutionAttempt(ctx context.Context, attempt ExecutionAttempt) error {
	commands, _ := json.Marshal(attempt.Commands)
	responses, _ := json.Marshal(attempt.Responses)
	details, _ := json.Marshal(normalizedMap(attempt.Details))
	_, err := s.db.ExecContext(ctx, `INSERT INTO boss_execution_attempts(id,summon_id,wave_position,adapter,status,command_count,completed_commands,commands_json,responses_json,details_json,failure,actor,started_at,completed_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		attempt.ID, attempt.SummonID, attempt.WavePosition, attempt.Adapter, attempt.Status,
		attempt.CommandCount, attempt.CompletedCommands, string(commands), string(responses), string(details),
		attempt.Failure, attempt.Actor, attempt.StartedAt, attempt.CompletedAt, attempt.CreatedAt, attempt.UpdatedAt)
	return err
}

func (s *Service) finishExecutionAttempt(ctx context.Context, id, status string, result WaveExecutionResult, failure string) error {
	commands, _ := json.Marshal(result.Commands)
	responses, _ := json.Marshal(result.Responses)
	details, _ := json.Marshal(normalizedMap(result.Details))
	now := s.timestamp()
	_, err := s.db.ExecContext(ctx, `UPDATE boss_execution_attempts SET status=?,adapter=?,command_count=?,completed_commands=?,commands_json=?,responses_json=?,details_json=?,failure=?,completed_at=?,updated_at=? WHERE id=?`,
		status, result.Adapter, len(result.Commands), result.CompletedCommands, string(commands), string(responses), string(details), strings.TrimSpace(failure), now, now, strings.TrimSpace(id))
	return err
}

func (s *Service) appendExecutionAudit(ctx context.Context, summonID, actor, message string, details map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	summon, err := getSummonTx(ctx, tx, summonID)
	if err != nil {
		return err
	}
	if err := insertSummonEvent(ctx, tx, summonID, summon.Status, summon.Status, actor, message, details, s.timestamp()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) GetExecutionAttempt(ctx context.Context, id string) (ExecutionAttempt, error) {
	if err := s.ensureExecutionSchema(ctx); err != nil {
		return ExecutionAttempt{}, err
	}
	item, err := scanExecutionAttempt(s.db.QueryRowContext(ctx, executionAttemptSelect+` WHERE id=?`, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionAttempt{}, sql.ErrNoRows
	}
	return item, err
}

func (s *Service) ListExecutionAttempts(ctx context.Context, summonID string, limit, offset int) ([]ExecutionAttempt, error) {
	if err := s.ensureExecutionSchema(ctx); err != nil {
		return nil, err
	}
	summonID = strings.TrimSpace(summonID)
	if summonID == "" {
		return nil, ErrSummonNotFound
	}
	if _, err := s.GetSummon(ctx, summonID); err != nil {
		return nil, err
	}
	limit, offset = normalizePage(limit, offset)
	rows, err := s.db.QueryContext(ctx, executionAttemptSelect+` WHERE summon_id=? ORDER BY created_at DESC LIMIT ? OFFSET ?`, summonID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ExecutionAttempt{}
	for rows.Next() {
		item, scanErr := scanExecutionAttempt(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const executionAttemptSelect = `SELECT id,summon_id,wave_position,adapter,status,command_count,completed_commands,commands_json,responses_json,details_json,failure,actor,started_at,completed_at,created_at,updated_at FROM boss_execution_attempts`

func scanExecutionAttempt(scanner interface{ Scan(...any) error }) (ExecutionAttempt, error) {
	var item ExecutionAttempt
	var commandsJSON, responsesJSON, detailsJSON string
	if err := scanner.Scan(&item.ID, &item.SummonID, &item.WavePosition, &item.Adapter, &item.Status,
		&item.CommandCount, &item.CompletedCommands, &commandsJSON, &responsesJSON, &detailsJSON,
		&item.Failure, &item.Actor, &item.StartedAt, &item.CompletedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return ExecutionAttempt{}, err
	}
	_ = json.Unmarshal([]byte(commandsJSON), &item.Commands)
	_ = json.Unmarshal([]byte(responsesJSON), &item.Responses)
	item.Details = decodeObject(detailsJSON)
	if item.Commands == nil {
		item.Commands = []string{}
	}
	if item.Responses == nil {
		item.Responses = []string{}
	}
	return item, nil
}

func executionFailure(uncertain bool, format string, args ...any) error {
	return &ExecutionError{Err: fmt.Errorf(format, args...), Uncertain: uncertain}
}

func executionTimestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
