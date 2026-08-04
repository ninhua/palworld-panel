package boss

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	BossLifecycleMonitoring = "monitoring"
	BossLifecycleCompleted  = "completed"
	BossLifecycleTimedOut   = "timed_out"
	BossLifecycleCancelled  = "cancelled"
	BossLifecycleError      = "error"
)

type BossLifecycleOptions struct {
	LogDirectory   string
	PollInterval   time.Duration
	DefaultTimeout time.Duration
	MaxReadBytes   int64
}

type BossLifecycleState struct {
	SummonID      string   `json:"summon_id"`
	WavePosition  int      `json:"wave_position"`
	State         string   `json:"state"`
	ExpectedCount int      `json:"expected_count"`
	ObservedCount int      `json:"observed_count"`
	TargetPalID   string   `json:"target_pal_id"`
	TargetAliases []string `json:"target_aliases"`
	TemplateFile  string   `json:"template_file,omitempty"`
	LogDirectory  string   `json:"log_directory,omitempty"`
	StartedAt     string   `json:"started_at"`
	DeadlineAt    string   `json:"deadline_at"`
	LastEventAt   string   `json:"last_event_at,omitempty"`
	LastScanAt    string   `json:"last_scan_at,omitempty"`
	LastError     string   `json:"last_error,omitempty"`
	UpdatedAt     string   `json:"updated_at"`
}

type bossLifecycleRecord struct {
	BossLifecycleState
	Cursors map[string]bossLifecycleLogCursor
}

type bossLifecycleRuntime struct {
	mu         sync.Mutex
	options    BossLifecycleOptions
	configured bool
	monitors   map[string]context.CancelFunc
	schemaOK   bool
}

var bossLifecycleRuntimes sync.Map

func bossLifecycleRuntimeFor(service *Service) *bossLifecycleRuntime {
	if service == nil {
		return &bossLifecycleRuntime{monitors: map[string]context.CancelFunc{}}
	}
	if cached, ok := bossLifecycleRuntimes.Load(service); ok {
		return cached.(*bossLifecycleRuntime)
	}
	runtime := &bossLifecycleRuntime{monitors: map[string]context.CancelFunc{}}
	actual, _ := bossLifecycleRuntimes.LoadOrStore(service, runtime)
	return actual.(*bossLifecycleRuntime)
}

func (s *Service) ConfigureBossLifecycle(ctx context.Context, options BossLifecycleOptions) error {
	options.LogDirectory = strings.TrimSpace(options.LogDirectory)
	if options.PollInterval <= 0 {
		options.PollInterval = 2 * time.Second
	}
	if options.PollInterval < 500*time.Millisecond {
		options.PollInterval = 500 * time.Millisecond
	}
	if options.DefaultTimeout <= 0 {
		options.DefaultTimeout = time.Hour
	}
	if options.MaxReadBytes <= 0 {
		options.MaxReadBytes = 2 << 20
	}
	runtime := bossLifecycleRuntimeFor(s)
	runtime.mu.Lock()
	runtime.options = options
	runtime.configured = true
	runtime.mu.Unlock()
	if err := s.ensureBossLifecycleSchema(ctx); err != nil {
		return err
	}
	return s.recoverBossLifecycleMonitors(ctx)
}

func (s *Service) ensureBossLifecycleSchema(ctx context.Context) error {
	runtime := bossLifecycleRuntimeFor(s)
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.schemaOK {
		return nil
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS boss_lifecycle_monitors (
			summon_id TEXT PRIMARY KEY,
			wave_position INTEGER NOT NULL CHECK(wave_position BETWEEN 1 AND 20),
			state TEXT NOT NULL CHECK(state IN ('monitoring','completed','timed_out','cancelled','error')),
			expected_count INTEGER NOT NULL CHECK(expected_count > 0),
			observed_count INTEGER NOT NULL DEFAULT 0 CHECK(observed_count >= 0),
			target_pal_id TEXT NOT NULL,
			target_aliases_json TEXT NOT NULL DEFAULT '[]',
			template_file TEXT NOT NULL DEFAULT '',
			log_directory TEXT NOT NULL DEFAULT '',
			cursors_json TEXT NOT NULL DEFAULT '{}',
			started_at TEXT NOT NULL,
			deadline_at TEXT NOT NULL,
			last_event_at TEXT NOT NULL DEFAULT '',
			last_scan_at TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			FOREIGN KEY(summon_id) REFERENCES boss_summons(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS boss_lifecycle_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			summon_id TEXT NOT NULL,
			wave_position INTEGER NOT NULL,
			event_hash TEXT NOT NULL UNIQUE,
			source_path TEXT NOT NULL DEFAULT '',
			source_offset INTEGER NOT NULL DEFAULT 0,
			target_name TEXT NOT NULL DEFAULT '',
			target_pal_id TEXT NOT NULL DEFAULT '',
			raw_line TEXT NOT NULL DEFAULT '',
			detected_at TEXT NOT NULL,
			FOREIGN KEY(summon_id) REFERENCES boss_summons(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_lifecycle_state ON boss_lifecycle_monitors(state,updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_boss_lifecycle_events_summon ON boss_lifecycle_events(summon_id,id)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure boss lifecycle schema: %w", err)
		}
	}
	runtime.schemaOK = true
	return nil
}

func NewBossLifecycleExecutionAdapter(service *Service, next WaveExecutor) WaveExecutor {
	return &bossLifecycleExecutionAdapter{service: service, next: next}
}

type bossLifecycleExecutionAdapter struct {
	service *Service
	next    WaveExecutor
}

func (adapter *bossLifecycleExecutionAdapter) Status(ctx context.Context) ExecutionAdapterStatus {
	if adapter == nil || adapter.next == nil {
		return ExecutionAdapterStatus{Adapter: "boss_lifecycle", State: "not_configured", Message: "Boss lifecycle executor is not configured.", Limitations: []string{}}
	}
	status := adapter.next.Status(ctx)
	if status.Limitations == nil {
		status.Limitations = []string{}
	}
	status.Limitations = append(status.Limitations, "固定坐标 Boss 完成状态依赖 PalDefender 击杀日志；超时不会自动判定死亡。")
	return status
}

func (adapter *bossLifecycleExecutionAdapter) ExecuteWave(ctx context.Context, summon Summon, wave SummonWave) (WaveExecutionResult, error) {
	if adapter == nil || adapter.next == nil {
		return WaveExecutionResult{}, executionFailure(false, "boss lifecycle executor is unavailable")
	}
	if !fixedBossLifecycleEnabled(summon, wave) {
		return adapter.next.ExecuteWave(ctx, summon, wave)
	}
	if adapter.service == nil {
		return WaveExecutionResult{}, executionFailure(false, "boss lifecycle service is unavailable")
	}
	options, err := adapter.service.bossLifecycleOptions()
	if err != nil {
		return WaveExecutionResult{}, executionFailure(false, "%v", err)
	}
	cursors, err := captureBossLifecycleLogCursors(options.LogDirectory)
	if err != nil {
		return WaveExecutionResult{}, executionFailure(false, "prepare Boss death detection: %v", err)
	}
	result, executeErr := adapter.next.ExecuteWave(ctx, summon, wave)
	if executeErr != nil {
		return result, executeErr
	}
	state, err := adapter.service.beginBossLifecycle(ctx, summon, wave, cursors)
	if err != nil {
		return result, executionFailure(true, "Boss was summoned but lifecycle monitoring could not start: %v", err)
	}
	if result.Details == nil {
		result.Details = map[string]any{}
	}
	result.Details["boss_lifecycle_monitoring"] = true
	result.Details["boss_lifecycle"] = state
	return result, nil
}

func (s *Service) bossLifecycleOptions() (BossLifecycleOptions, error) {
	runtime := bossLifecycleRuntimeFor(s)
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if !runtime.configured {
		return BossLifecycleOptions{}, errors.New("Boss lifecycle monitoring is not configured")
	}
	if strings.TrimSpace(runtime.options.LogDirectory) == "" {
		return BossLifecycleOptions{}, errors.New("PalDefender log directory is unavailable")
	}
	return runtime.options, nil
}

func fixedBossLifecycleEnabled(summon Summon, wave SummonWave) bool {
	if !strings.EqualFold(bossLifecycleMetadataString(summon.Metadata, "activity_kind"), "fixed_boss") &&
		!strings.EqualFold(bossLifecycleMetadataString(wave.Metadata, "activity_kind"), "fixed_boss") {
		return false
	}
	if value, found := bossLifecycleMetadataBool(wave.Metadata, "boss_lifecycle_enabled"); found {
		return value
	}
	if value, found := bossLifecycleMetadataBool(summon.Metadata, "boss_lifecycle_enabled"); found {
		return value
	}
	return true
}

func bossLifecycleMonitoring(details map[string]any) bool {
	value, ok := details["boss_lifecycle_monitoring"]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return false
	}
}

func (s *Service) beginBossLifecycle(ctx context.Context, summon Summon, wave SummonWave, cursors map[string]bossLifecycleLogCursor) (BossLifecycleState, error) {
	if err := s.ensureBossLifecycleSchema(ctx); err != nil {
		return BossLifecycleState{}, err
	}
	options, err := s.bossLifecycleOptions()
	if err != nil {
		return BossLifecycleState{}, err
	}
	timeout := options.DefaultTimeout
	if seconds := bossLifecycleMetadataInt(wave.Metadata, "boss_lifecycle_timeout_seconds"); seconds == 0 {
		seconds = bossLifecycleMetadataInt(summon.Metadata, "boss_lifecycle_timeout_seconds")
		if seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}
	} else {
		timeout = time.Duration(seconds) * time.Second
	}
	if timeout < time.Minute {
		timeout = time.Minute
	}
	if timeout > 24*time.Hour {
		timeout = 24 * time.Hour
	}
	aliases := bossLifecycleTargetAliases(wave)
	if len(aliases) == 0 {
		return BossLifecycleState{}, errors.New("Boss lifecycle target PalID is unavailable")
	}
	now := time.Now().UTC()
	state := BossLifecycleState{
		SummonID: summon.ID, WavePosition: wave.Position, State: BossLifecycleMonitoring,
		ExpectedCount: wave.Count, ObservedCount: 0, TargetPalID: wave.PalID, TargetAliases: aliases,
		TemplateFile: bossLifecycleMetadataString(wave.Metadata, "pal_template_file"),
		LogDirectory: options.LogDirectory, StartedAt: now.Format(time.RFC3339Nano),
		DeadlineAt: now.Add(timeout).Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}
	if state.TemplateFile == "" {
		state.TemplateFile = bossLifecycleMetadataString(summon.Metadata, "pal_template_file")
	}
	aliasesJSON, _ := json.Marshal(state.TargetAliases)
	cursorsJSON, _ := json.Marshal(cursors)
	_, err = s.db.ExecContext(ctx, `INSERT INTO boss_lifecycle_monitors(
		summon_id,wave_position,state,expected_count,observed_count,target_pal_id,target_aliases_json,template_file,log_directory,cursors_json,
		started_at,deadline_at,last_event_at,last_scan_at,last_error,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(summon_id) DO UPDATE SET wave_position=excluded.wave_position,state=excluded.state,
		expected_count=excluded.expected_count,observed_count=excluded.observed_count,target_pal_id=excluded.target_pal_id,
		target_aliases_json=excluded.target_aliases_json,template_file=excluded.template_file,log_directory=excluded.log_directory,
		cursors_json=excluded.cursors_json,started_at=excluded.started_at,deadline_at=excluded.deadline_at,
		last_event_at='',last_scan_at='',last_error='',updated_at=excluded.updated_at`,
		state.SummonID, state.WavePosition, state.State, state.ExpectedCount, state.ObservedCount, state.TargetPalID,
		string(aliasesJSON), state.TemplateFile, state.LogDirectory, string(cursorsJSON), state.StartedAt, state.DeadlineAt, "", "", "", state.UpdatedAt)
	if err != nil {
		return BossLifecycleState{}, err
	}
	if err := s.updateBossLifecycleResult(ctx, state); err != nil {
		return BossLifecycleState{}, err
	}
	_ = s.appendExecutionAudit(ctx, summon.ID, summon.Actor,
		fmt.Sprintf("Boss lifecycle monitoring started for wave %d (%d expected deaths)", wave.Position, wave.Count),
		map[string]any{"boss_lifecycle": state})
	s.startBossLifecycleMonitor(state.SummonID)
	return state, nil
}

func (s *Service) GetBossLifecycle(ctx context.Context, summonID string) (BossLifecycleState, error) {
	if err := s.ensureBossLifecycleSchema(ctx); err != nil {
		return BossLifecycleState{}, err
	}
	record, err := s.loadBossLifecycleRecord(ctx, strings.TrimSpace(summonID))
	if errors.Is(err, sql.ErrNoRows) {
		return BossLifecycleState{}, ErrSummonNotFound
	}
	return record.BossLifecycleState, err
}

func (s *Service) recoverBossLifecycleMonitors(ctx context.Context) error {
	if err := s.ensureBossLifecycleSchema(ctx); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT summon_id FROM boss_lifecycle_monitors WHERE state='monitoring' ORDER BY started_at`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var summonIDs []string
	for rows.Next() {
		var summonID string
		if err := rows.Scan(&summonID); err != nil {
			return err
		}
		summonIDs = append(summonIDs, summonID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, summonID := range summonIDs {
		s.startBossLifecycleMonitor(summonID)
	}
	return nil
}

func (s *Service) startBossLifecycleMonitor(summonID string) {
	summonID = strings.TrimSpace(summonID)
	if summonID == "" {
		return
	}
	runtime := bossLifecycleRuntimeFor(s)
	runtime.mu.Lock()
	if _, exists := runtime.monitors[summonID]; exists {
		runtime.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime.monitors[summonID] = cancel
	options := runtime.options
	runtime.mu.Unlock()
	go func() {
		defer func() {
			runtime.mu.Lock()
			delete(runtime.monitors, summonID)
			runtime.mu.Unlock()
		}()
		s.monitorBossLifecycle(ctx, summonID, options)
	}()
}

func (s *Service) monitorBossLifecycle(ctx context.Context, summonID string, options BossLifecycleOptions) {
	ticker := time.NewTicker(options.PollInterval)
	defer ticker.Stop()
	for {
		if terminal := s.scanBossLifecycleOnce(ctx, summonID, options); terminal {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) scanBossLifecycleOnce(ctx context.Context, summonID string, options BossLifecycleOptions) bool {
	record, err := s.loadBossLifecycleRecord(ctx, summonID)
	if err != nil || record.State != BossLifecycleMonitoring {
		return true
	}
	wave, waveErr := s.getSummonWave(ctx, summonID, record.WavePosition)
	if waveErr != nil || wave.Status != WaveStatusActive {
		reason := "Boss wave is no longer active; lifecycle monitoring stopped"
		if waveErr == nil {
			reason = fmt.Sprintf("Boss wave changed to %s outside lifecycle monitoring", wave.Status)
		}
		_ = s.setBossLifecycleTerminal(ctx, record, BossLifecycleCancelled, reason)
		return true
	}
	deadline, _ := time.Parse(time.RFC3339Nano, record.DeadlineAt)
	if !deadline.IsZero() && !time.Now().UTC().Before(deadline) {
		_ = s.setBossLifecycleTerminal(ctx, record, BossLifecycleTimedOut, "Boss death detection timed out; manual reconciliation is required")
		return true
	}
	events, cursors, _, scanErr := scanBossLifecycleLogs(record.LogDirectory, record.Cursors, record.TargetAliases, options.MaxReadBytes)
	now := s.timestamp()
	if scanErr != nil {
		record.LastError = scanErr.Error()
		record.LastScanAt = now
		record.UpdatedAt = now
		_ = s.saveBossLifecycleRecord(ctx, record)
		_ = s.updateBossLifecycleResult(ctx, record.BossLifecycleState)
		return false
	}
	inserted := 0
	lastEventAt := record.LastEventAt
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	for _, event := range events {
		result, insertErr := tx.ExecContext(ctx, `INSERT OR IGNORE INTO boss_lifecycle_events(
			summon_id,wave_position,event_hash,source_path,source_offset,target_name,target_pal_id,raw_line,detected_at
		) VALUES(?,?,?,?,?,?,?,?,?)`, summonID, record.WavePosition, event.Hash, event.Path, event.Offset, event.TargetName, event.TargetID, event.RawLine, event.DetectedAt)
		if insertErr != nil {
			_ = tx.Rollback()
			return false
		}
		count, _ := result.RowsAffected()
		if count > 0 {
			inserted++
			lastEventAt = event.DetectedAt
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM boss_lifecycle_events WHERE summon_id=?`, summonID).Scan(&record.ObservedCount); err != nil {
		_ = tx.Rollback()
		return false
	}
	if record.ObservedCount > record.ExpectedCount {
		record.ObservedCount = record.ExpectedCount
	}
	record.Cursors = cursors
	record.LastEventAt = lastEventAt
	record.LastScanAt = now
	record.LastError = ""
	record.UpdatedAt = now
	aliasesJSON, _ := json.Marshal(record.TargetAliases)
	cursorsJSON, _ := json.Marshal(record.Cursors)
	_, err = tx.ExecContext(ctx, `UPDATE boss_lifecycle_monitors SET observed_count=?,target_aliases_json=?,cursors_json=?,last_event_at=?,last_scan_at=?,last_error='',updated_at=? WHERE summon_id=? AND state='monitoring'`,
		record.ObservedCount, string(aliasesJSON), string(cursorsJSON), record.LastEventAt, record.LastScanAt, record.UpdatedAt, summonID)
	if err != nil {
		_ = tx.Rollback()
		return false
	}
	if err := tx.Commit(); err != nil {
		return false
	}
	_ = s.updateBossLifecycleResult(ctx, record.BossLifecycleState)
	if inserted > 0 {
		_ = s.appendExecutionAudit(ctx, summonID, "lifecycle-monitor",
			fmt.Sprintf("Boss death progress %d/%d", record.ObservedCount, record.ExpectedCount),
			map[string]any{"boss_lifecycle": record.BossLifecycleState})
	}
	if record.ObservedCount >= record.ExpectedCount {
		if err := s.completeBossLifecycle(ctx, record); err != nil {
			record.LastError = "confirm Boss completion: " + err.Error()
			record.UpdatedAt = s.timestamp()
			_ = s.saveBossLifecycleRecord(ctx, record)
			_ = s.updateBossLifecycleResult(ctx, record.BossLifecycleState)
			return false
		}
		return true
	}
	return false
}

func (s *Service) completeBossLifecycle(ctx context.Context, record bossLifecycleRecord) error {
	now := s.timestamp()
	completed := record
	completed.State = BossLifecycleCompleted
	completed.ObservedCount = completed.ExpectedCount
	completed.LastError = ""
	completed.UpdatedAt = now
	wave, err := s.getSummonWave(ctx, completed.SummonID, completed.WavePosition)
	if err != nil {
		return err
	}
	result := normalizedMap(wave.Result)
	result["boss_lifecycle"] = completed.BossLifecycleState
	_, err = s.TransitionSummonWave(ctx, completed.SummonID, completed.WavePosition, WaveTransitionRequest{
		Status: WaveStatusCompleted, Message: fmt.Sprintf("Boss death confirmed by PalDefender logs (%d/%d)", completed.ObservedCount, completed.ExpectedCount), Result: result,
	}, "lifecycle-monitor")
	if err != nil && !errors.Is(err, ErrInvalidWaveTransition) {
		return err
	}
	if err := s.saveBossLifecycleRecord(ctx, completed); err != nil {
		return err
	}
	_ = s.updateBossLifecycleResult(ctx, completed.BossLifecycleState)
	_, _ = s.reconcileAutoExecutionActivityGuard(ctx)
	return nil
}

func (s *Service) setBossLifecycleTerminal(ctx context.Context, record bossLifecycleRecord, state, failure string) error {
	record.State = state
	record.LastError = strings.TrimSpace(failure)
	record.UpdatedAt = s.timestamp()
	if err := s.saveBossLifecycleRecord(ctx, record); err != nil {
		return err
	}
	_ = s.updateBossLifecycleResult(ctx, record.BossLifecycleState)
	_ = s.appendExecutionAudit(ctx, record.SummonID, "lifecycle-monitor", failure, map[string]any{"boss_lifecycle": record.BossLifecycleState})
	return nil
}

func (s *Service) saveBossLifecycleRecord(ctx context.Context, record bossLifecycleRecord) error {
	aliasesJSON, _ := json.Marshal(record.TargetAliases)
	cursorsJSON, _ := json.Marshal(record.Cursors)
	_, err := s.db.ExecContext(ctx, `UPDATE boss_lifecycle_monitors SET state=?,expected_count=?,observed_count=?,target_pal_id=?,target_aliases_json=?,template_file=?,log_directory=?,cursors_json=?,started_at=?,deadline_at=?,last_event_at=?,last_scan_at=?,last_error=?,updated_at=? WHERE summon_id=?`,
		record.State, record.ExpectedCount, record.ObservedCount, record.TargetPalID, string(aliasesJSON), record.TemplateFile,
		record.LogDirectory, string(cursorsJSON), record.StartedAt, record.DeadlineAt, record.LastEventAt, record.LastScanAt,
		record.LastError, record.UpdatedAt, record.SummonID)
	return err
}

func (s *Service) loadBossLifecycleRecord(ctx context.Context, summonID string) (bossLifecycleRecord, error) {
	if err := s.ensureBossLifecycleSchema(ctx); err != nil {
		return bossLifecycleRecord{}, err
	}
	var record bossLifecycleRecord
	var aliasesJSON, cursorsJSON string
	err := s.db.QueryRowContext(ctx, `SELECT summon_id,wave_position,state,expected_count,observed_count,target_pal_id,target_aliases_json,template_file,log_directory,cursors_json,started_at,deadline_at,last_event_at,last_scan_at,last_error,updated_at FROM boss_lifecycle_monitors WHERE summon_id=?`, strings.TrimSpace(summonID)).Scan(
		&record.SummonID, &record.WavePosition, &record.State, &record.ExpectedCount, &record.ObservedCount, &record.TargetPalID,
		&aliasesJSON, &record.TemplateFile, &record.LogDirectory, &cursorsJSON, &record.StartedAt, &record.DeadlineAt,
		&record.LastEventAt, &record.LastScanAt, &record.LastError, &record.UpdatedAt,
	)
	if err != nil {
		return bossLifecycleRecord{}, err
	}
	_ = json.Unmarshal([]byte(aliasesJSON), &record.TargetAliases)
	_ = json.Unmarshal([]byte(cursorsJSON), &record.Cursors)
	if record.TargetAliases == nil {
		record.TargetAliases = []string{record.TargetPalID}
	}
	if record.Cursors == nil {
		record.Cursors = map[string]bossLifecycleLogCursor{}
	}
	return record, nil
}

func (s *Service) updateBossLifecycleResult(ctx context.Context, state BossLifecycleState) error {
	payload := map[string]any{
		"summon_id": state.SummonID, "wave_position": state.WavePosition, "state": state.State,
		"expected_count": state.ExpectedCount, "observed_count": state.ObservedCount, "target_pal_id": state.TargetPalID,
		"target_aliases": state.TargetAliases, "template_file": state.TemplateFile, "started_at": state.StartedAt,
		"deadline_at": state.DeadlineAt, "last_event_at": state.LastEventAt, "last_scan_at": state.LastScanAt,
		"last_error": state.LastError, "updated_at": state.UpdatedAt,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var waveJSON string
	if err := tx.QueryRowContext(ctx, `SELECT result_json FROM boss_summon_waves WHERE summon_id=? AND position=?`, state.SummonID, state.WavePosition).Scan(&waveJSON); err != nil {
		return err
	}
	waveResult := decodeObject(waveJSON)
	waveResult["boss_lifecycle"] = payload
	encodedWave, _ := json.Marshal(waveResult)
	if _, err := tx.ExecContext(ctx, `UPDATE boss_summon_waves SET result_json=?,updated_at=? WHERE summon_id=? AND position=?`, string(encodedWave), state.UpdatedAt, state.SummonID, state.WavePosition); err != nil {
		return err
	}
	var summonJSON string
	if err := tx.QueryRowContext(ctx, `SELECT result_json FROM boss_summons WHERE id=?`, state.SummonID).Scan(&summonJSON); err != nil {
		return err
	}
	summonResult := decodeObject(summonJSON)
	summonResult["boss_lifecycle"] = payload
	encodedSummon, _ := json.Marshal(summonResult)
	if _, err := tx.ExecContext(ctx, `UPDATE boss_summons SET result_json=?,updated_at=? WHERE id=?`, string(encodedSummon), state.UpdatedAt, state.SummonID); err != nil {
		return err
	}
	return tx.Commit()
}

func bossLifecycleTargetAliases(wave SummonWave) []string {
	values := []string{wave.PalID}
	if snapshot, ok := wave.Metadata["pal_template_snapshot"].(map[string]any); ok {
		values = append(values,
			bossLifecycleMetadataString(snapshot, "pal_id"),
			bossLifecycleMetadataString(snapshot, "pal_name"),
			bossLifecycleMetadataString(snapshot, "nickname"),
		)
	}
	values = append(values, bossLifecycleMetadataString(wave.Metadata, "target_pal_name"))
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		normalized := normalizeBossLifecyclePal(value)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func bossLifecycleMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func bossLifecycleMetadataInt(metadata map[string]any, key string) int {
	if metadata == nil {
		return 0
	}
	switch value := metadata[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		result, _ := value.Int64()
		return int(result)
	case string:
		result, _ := strconv.Atoi(strings.TrimSpace(value))
		return result
	default:
		return 0
	}
}

func bossLifecycleMetadataBool(metadata map[string]any, key string) (bool, bool) {
	if metadata == nil {
		return false, false
	}
	value, found := metadata[key]
	if !found {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed, err == nil
	case float64:
		return typed != 0, true
	default:
		return false, false
	}
}

func BossLifecycleLogDirectory(palDefenderDirectory string) string {
	palDefenderDirectory = strings.TrimSpace(palDefenderDirectory)
	if palDefenderDirectory == "" {
		return ""
	}
	return filepath.Join(palDefenderDirectory, "Logs")
}
