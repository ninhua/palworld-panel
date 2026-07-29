package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"palpanel/internal/db"
	"palpanel/internal/id"
)

var ErrCrashGuardTripped = errors.New("crash guard is tripped")

const (
	defaultCrashGuardInterval  = 5 * time.Second
	defaultCrashGuardWindow    = 10 * time.Minute
	defaultCrashGuardThreshold = 3
	crashGuardEventRetention   = 50
	expectedStopTTL            = 10 * time.Minute
)

type CrashGuardStatus struct {
	Enabled             bool                 `json:"enabled"`
	Tripped             bool                 `json:"tripped"`
	TrippedAt           string               `json:"tripped_at,omitempty"`
	Reason              string               `json:"reason,omitempty"`
	ExpectedOperation   bool                 `json:"expected_operation"`
	RecentCrashCount    int                  `json:"recent_crash_count"`
	Threshold           int                  `json:"threshold"`
	WindowSeconds       int                  `json:"window_seconds"`
	LastObservedStatus  string               `json:"last_observed_status,omitempty"`
	LastObservedRuntime string               `json:"last_observed_runtime,omitempty"`
	UpdatedAt           string               `json:"updated_at"`
	Events              []db.CrashGuardEvent `json:"events"`
}

func (m Manager) crashGuardTime() time.Time {
	if m.crashGuardNow != nil {
		return m.crashGuardNow()
	}
	return time.Now()
}

func (m Manager) lockCrashGuard() func() {
	if m.crashGuardMu == nil {
		return func() {}
	}
	m.crashGuardMu.Lock()
	return m.crashGuardMu.Unlock
}

func crashGuardWindowStart(state db.CrashGuardState, now time.Time, window time.Duration) time.Time {
	since := now.Add(-window)
	if recoveredAt, err := time.Parse(time.RFC3339Nano, state.RecoveredAt); err == nil && recoveredAt.After(since) {
		since = recoveredAt.Add(time.Nanosecond)
	}
	return since
}

func (m Manager) CrashGuardStatus(ctx context.Context, limit int) (CrashGuardStatus, error) {
	unlock := m.lockCrashGuard()
	defer unlock()
	return m.crashGuardStatusLocked(ctx, limit)
}

func (m Manager) crashGuardStatusLocked(ctx context.Context, limit int) (CrashGuardStatus, error) {
	state, err := m.store.GetCrashGuardState(ctx)
	if err != nil {
		return CrashGuardStatus{}, err
	}
	now := m.crashGuardTime().UTC()
	window := m.crashGuardWindow
	if window <= 0 {
		window = defaultCrashGuardWindow
	}
	threshold := m.crashGuardThreshold
	if threshold <= 0 {
		threshold = defaultCrashGuardThreshold
	}
	count, err := m.store.CountCrashGuardOccurrencesSince(ctx, crashGuardWindowStart(state, now, window).Format(time.RFC3339Nano))
	if err != nil {
		return CrashGuardStatus{}, err
	}
	events, err := m.store.ListCrashGuardEvents(ctx, limit)
	if err != nil {
		return CrashGuardStatus{}, err
	}
	return CrashGuardStatus{
		Enabled:             true,
		Tripped:             state.Tripped,
		TrippedAt:           state.TrippedAt,
		Reason:              state.Reason,
		ExpectedOperation:   crashGuardExpected(state, now),
		RecentCrashCount:    count,
		Threshold:           threshold,
		WindowSeconds:       int(window.Seconds()),
		LastObservedStatus:  state.LastStatus,
		LastObservedRuntime: state.LastRuntimeMode,
		UpdatedAt:           state.UpdatedAt,
		Events:              events,
	}, nil
}

func (m Manager) StartCrashGuard(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	interval := m.crashGuardInterval
	if interval <= 0 {
		interval = defaultCrashGuardInterval
	}
	go func() {
		defer close(done)
		if err := m.ObserveCrashGuard(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("crash guard initial observation failed: %v", err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := m.ObserveCrashGuard(ctx); err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("crash guard observation failed: %v", err)
				}
			}
		}
	}()
	return done
}

func (m Manager) ObserveCrashGuard(ctx context.Context) error {
	unlock := m.lockCrashGuard()
	state, err := m.store.GetCrashGuardState(ctx)
	if err != nil {
		unlock()
		return err
	}
	if m.crashGuardAfterStateRead != nil {
		m.crashGuardAfterStateRead()
	}
	status, err := m.Status(ctx)
	if err != nil {
		unlock()
		return err
	}
	now := m.crashGuardTime().UTC()
	expected := crashGuardExpected(state, now)
	container := status.Container
	currentStatus := strings.TrimSpace(container.Status)
	if currentStatus == "" {
		currentStatus = "unknown"
	}
	signature := fmt.Sprintf("%s|%s|%d|%t|%d|%s|%s", status.RuntimeMode, currentStatus, container.RestartCount,
		container.OOMKilled, container.ExitCode, container.StartedAt, container.FinishedAt)

	var event *db.CrashGuardEvent
	if !state.Tripped && !expected {
		if status.RuntimeMode == RuntimeWineDocker {
			delta := 0
			if state.LastRuntimeMode == status.RuntimeMode && container.RestartCount > state.LastRestartCount {
				delta = container.RestartCount - state.LastRestartCount
			}
			failedExit := container.LifecycleAvailable && (container.OOMKilled || container.ExitCode != 0) &&
				(currentStatus == "exited" || currentStatus == "dead" || currentStatus == "restarting")
			if delta > 0 || (failedExit && signature != state.LastSignature) {
				occurrences := delta
				if occurrences < 1 {
					occurrences = 1
				}
				kind := "container_restart"
				message := "PalServer container restarted unexpectedly"
				if container.OOMKilled {
					kind = "oom_kill"
					message = "PalServer container was killed by the out-of-memory controller"
				} else if delta == 0 {
					kind = "unexpected_exit"
					message = fmt.Sprintf("PalServer container exited with code %d", container.ExitCode)
				}
				event = &db.CrashGuardEvent{ID: id.New("crash"), Kind: kind, RuntimeMode: status.RuntimeMode,
					Occurrences: occurrences, ExitCode: container.ExitCode, OOMKilled: container.OOMKilled,
					RestartCount: container.RestartCount, StartedAt: container.StartedAt, FinishedAt: container.FinishedAt,
					Message: message, CreatedAt: now.Format(time.RFC3339Nano)}
			}
		} else if state.LastRuntimeMode == status.RuntimeMode && state.LastStatus == "running" && currentStatus != "running" {
			event = &db.CrashGuardEvent{ID: id.New("crash"), Kind: "unexpected_exit", RuntimeMode: status.RuntimeMode,
				Occurrences: 1, Message: "PalServer.exe exited outside a PalPanel lifecycle operation",
				CreatedAt: now.Format(time.RFC3339Nano)}
		}
	}

	state.LastRuntimeMode = status.RuntimeMode
	state.LastStatus = currentStatus
	state.LastRestartCount = container.RestartCount
	state.LastSignature = signature
	state.UpdatedAt = now.Format(time.RFC3339Nano)
	if err := m.store.PutCrashGuardState(ctx, state); err != nil {
		unlock()
		return err
	}
	if event == nil {
		unlock()
		return nil
	}
	if err := m.store.CreateCrashGuardEvent(ctx, *event); err != nil {
		unlock()
		return err
	}
	_ = m.store.PruneCrashGuardEvents(ctx, crashGuardEventRetention)
	window := m.crashGuardWindow
	if window <= 0 {
		window = defaultCrashGuardWindow
	}
	threshold := m.crashGuardThreshold
	if threshold <= 0 {
		threshold = defaultCrashGuardThreshold
	}
	count, err := m.store.CountCrashGuardOccurrencesSince(ctx, crashGuardWindowStart(state, now, window).Format(time.RFC3339Nano))
	if err != nil {
		unlock()
		return err
	}
	shouldTrip := count >= threshold
	reason := ""
	if shouldTrip {
		reason = fmt.Sprintf("PalServer crashed or restarted %d times within %s", count, window)
	}
	unlock()
	if shouldTrip {
		return m.tripCrashGuard(ctx, status.RuntimeMode, reason)
	}
	return nil
}

func crashGuardExpected(state db.CrashGuardState, now time.Time) bool {
	if strings.TrimSpace(state.ExpectedUntil) == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339Nano, state.ExpectedUntil)
	return err == nil && until.After(now)
}

func (m Manager) markCrashGuardExpectedStop(ctx context.Context, reason string) error {
	unlock := m.lockCrashGuard()
	defer unlock()
	state, err := m.store.GetCrashGuardState(ctx)
	if err != nil {
		return err
	}
	now := m.crashGuardTime().UTC()
	state.ExpectedUntil = now.Add(expectedStopTTL).Format(time.RFC3339Nano)
	state.ExpectedReason = strings.TrimSpace(reason)
	state.UpdatedAt = now.Format(time.RFC3339Nano)
	return m.store.PutCrashGuardState(ctx, state)
}

func (m Manager) clearCrashGuardExpectedStop(ctx context.Context) error {
	unlock := m.lockCrashGuard()
	defer unlock()
	state, err := m.store.GetCrashGuardState(ctx)
	if err != nil {
		return err
	}
	state.ExpectedUntil = ""
	state.ExpectedReason = ""
	state.UpdatedAt = m.crashGuardTime().UTC().Format(time.RFC3339Nano)
	return m.store.PutCrashGuardState(ctx, state)
}

func (m Manager) requireCrashGuardReady(ctx context.Context) error {
	unlock := m.lockCrashGuard()
	defer unlock()
	state, err := m.store.GetCrashGuardState(ctx)
	if err != nil {
		return err
	}
	if state.Tripped {
		reason := strings.TrimSpace(state.Reason)
		if reason == "" {
			reason = "manual recovery is required"
		}
		return fmt.Errorf("%w: %s", ErrCrashGuardTripped, reason)
	}
	return nil
}

func (m Manager) tripCrashGuard(ctx context.Context, runtimeMode, reason string) error {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	unlockGuard := m.lockCrashGuard()
	state, err := m.store.GetCrashGuardState(ctx)
	if err != nil {
		unlockGuard()
		return err
	}
	if state.Tripped {
		unlockGuard()
		return nil
	}
	now := m.crashGuardTime().UTC()
	if crashGuardExpected(state, now) {
		unlockGuard()
		return nil
	}
	state.Tripped = true
	state.TrippedAt = now.Format(time.RFC3339Nano)
	state.Reason = reason
	state.ExpectedUntil = ""
	state.ExpectedReason = ""
	state.UpdatedAt = state.TrippedAt
	if err := m.store.PutCrashGuardState(ctx, state); err != nil {
		unlockGuard()
		return err
	}
	unlockGuard()

	var pauseErr error
	if runtimeMode == RuntimeWineDocker {
		pauseErr = m.runner.PauseForCrashLoop(ctx)
	} else {
		record, ok, loadErr := m.loadWindowsProcess(ctx)
		if loadErr != nil {
			pauseErr = loadErr
		} else if ok {
			running, verifyErr := m.verifyWindowsProcess(record)
			if verifyErr != nil {
				pauseErr = verifyErr
			} else if running {
				pauseErr = m.terminateProcessTree(ctx, record.PID)
			}
			_ = m.clearWindowsProcess(ctx)
		}
	}
	message := reason
	if pauseErr != nil {
		message += "; automatic restart suppression failed: " + pauseErr.Error()
	}
	_ = m.store.CreateAlert(ctx, db.Alert{ID: id.New("alert"), Severity: "critical", Title: "PalServer crash loop paused",
		Message: message, Source: "crash-guard", Status: "open", CreatedAt: now.Format(time.RFC3339Nano)})
	return pauseErr
}

func (m Manager) RecoverCrashGuard(ctx context.Context, start bool) (CrashGuardStatus, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	unlockGuard := m.lockCrashGuard()
	state, err := m.store.GetCrashGuardState(ctx)
	if err != nil {
		unlockGuard()
		return CrashGuardStatus{}, err
	}
	if !state.Tripped {
		status, statusErr := m.crashGuardStatusLocked(ctx, 20)
		unlockGuard()
		return status, statusErr
	}
	now := m.crashGuardTime().UTC()
	state.Tripped = false
	state.TrippedAt = ""
	state.Reason = ""
	state.ExpectedUntil = ""
	state.ExpectedReason = ""
	state.LastStatus = ""
	state.LastRestartCount = 0
	state.LastSignature = ""
	state.RecoveredAt = now.Format(time.RFC3339Nano)
	state.UpdatedAt = state.RecoveredAt
	if err := m.store.PutCrashGuardState(ctx, state); err != nil {
		unlockGuard()
		return CrashGuardStatus{}, err
	}
	unlockGuard()
	if start {
		if err := m.startUnlocked(ctx); err != nil {
			unlockFailure := m.lockCrashGuard()
			state.Tripped = true
			state.TrippedAt = now.Format(time.RFC3339Nano)
			state.Reason = "Crash guard recovery start failed: " + err.Error()
			state.UpdatedAt = state.TrippedAt
			_ = m.store.PutCrashGuardState(context.Background(), state)
			unlockFailure()
			return CrashGuardStatus{}, err
		}
	}
	_ = m.store.ResolveCrashGuardAlerts(ctx)
	return m.CrashGuardStatus(ctx, 20)
}
