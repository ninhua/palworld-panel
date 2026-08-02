package boss

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	autoExecutionActor          = "boss-auto-executor"
	autoExecutionDiscoveryDelay = 2 * time.Second
	autoExecutionPollInterval   = 2 * time.Second
	autoExecutionBatchSize      = 50
)

type autoExecutionPolicy struct {
	Enabled   bool
	Paused    bool
	NotBefore time.Time
}

type autoExecutionCandidate struct {
	SummonID  string
	Requested string
	Metadata  map[string]any
	NotBefore time.Time
	Policy    autoExecutionPolicy
}

type autoExecutionWorker struct {
	stop chan struct{}
	done chan struct{}
}

var (
	autoExecutionDiscoveryOnce sync.Once
	autoExecutionWorkers       sync.Map
)

func init() {
	startAutoExecutionDiscovery()
}

func startAutoExecutionDiscovery() {
	autoExecutionDiscoveryOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(autoExecutionDiscoveryDelay)
			defer ticker.Stop()
			for range ticker.C {
				serviceCache.Range(func(_, value any) bool {
					service, ok := value.(*Service)
					if ok {
						ensureAutoExecutionWorker(service)
					}
					return true
				})
			}
		}()
	})
}

func ensureAutoExecutionWorker(service *Service) {
	if service == nil || service.db == nil {
		return
	}
	worker := &autoExecutionWorker{stop: make(chan struct{}), done: make(chan struct{})}
	actual, loaded := autoExecutionWorkers.LoadOrStore(service, worker)
	if loaded {
		_ = actual
		return
	}
	go service.runAutoExecutionWorker(worker)
}

func (s *Service) runAutoExecutionWorker(worker *autoExecutionWorker) {
	defer close(worker.done)
	defer autoExecutionWorkers.Delete(s)

	ticker := time.NewTicker(autoExecutionPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-worker.stop:
			return
		case <-s.scheduleStop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_ = s.runAutoExecutionCycle(ctx)
			cancel()
		}
	}
}

func (s *Service) runAutoExecutionCycle(ctx context.Context) error {
	if s == nil || s.db == nil || s.executionAdapter() == nil {
		return nil
	}
	if err := s.ensureExecutionSchema(ctx); err != nil {
		return err
	}

	candidates, err := s.listAutoExecutionCandidates(ctx, autoExecutionBatchSize)
	if err != nil {
		return err
	}
	now := s.now()
	for _, candidate := range candidates {
		if !candidate.Policy.Enabled || candidate.Policy.Paused {
			continue
		}
		if !candidate.Policy.NotBefore.IsZero() && now.Before(candidate.Policy.NotBefore) {
			continue
		}

		blocked, err := s.autoExecutionBlocked(ctx, candidate.SummonID)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}

		summon, err := s.GetSummon(ctx, candidate.SummonID)
		if err != nil {
			if errors.Is(err, ErrSummonNotFound) {
				continue
			}
			return err
		}
		waves, err := s.ListSummonWaves(ctx, candidate.SummonID)
		if err != nil {
			return err
		}
		due, runnable := autoExecutionDueAt(summon, waves)
		if !runnable || now.Before(due) {
			continue
		}

		_, err = s.ExecuteNextWave(ctx, candidate.SummonID, autoExecutionActor)
		if err == nil {
			// Execute at most one wave per cycle. This keeps ordering deterministic
			// and gives persisted state a chance to settle before the next scan.
			return nil
		}
		if errors.Is(err, ErrExecutionBusy) || errors.Is(err, ErrExecutionUnavailable) || errors.Is(err, ErrExecutionReconcileRequired) {
			return nil
		}
		// ExecuteNextWave persists failed or uncertain outcomes itself. Do not
		// retry in the same cycle, because the command may already have reached
		// the game server.
		return nil
	}
	return nil
}

func (s *Service) listAutoExecutionCandidates(ctx context.Context, limit int) ([]autoExecutionCandidate, error) {
	if limit <= 0 || limit > autoExecutionBatchSize {
		limit = autoExecutionBatchSize
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,requested_at,metadata_json
		FROM boss_summons
		WHERE status IN ('pending','active')
		ORDER BY requested_at ASC,id ASC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]autoExecutionCandidate, 0, limit)
	for rows.Next() {
		var item autoExecutionCandidate
		var metadataJSON string
		if err := rows.Scan(&item.SummonID, &item.Requested, &metadataJSON); err != nil {
			return nil, err
		}
		item.Metadata = map[string]any{}
		if strings.TrimSpace(metadataJSON) != "" {
			_ = json.Unmarshal([]byte(metadataJSON), &item.Metadata)
		}
		item.Policy = autoExecutionPolicyFromMetadata(item.Metadata)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) autoExecutionBlocked(ctx context.Context, summonID string) (bool, error) {
	var activeWaves int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM boss_summon_waves WHERE summon_id=? AND status='active'`, summonID,
	).Scan(&activeWaves); err != nil {
		return false, err
	}
	if activeWaves > 0 {
		return true, nil
	}

	var unresolvedAttempts int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM boss_execution_attempts WHERE summon_id=? AND status IN ('running','uncertain')`, summonID,
	).Scan(&unresolvedAttempts)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return unresolvedAttempts > 0, nil
}

func autoExecutionPolicyFromMetadata(metadata map[string]any) autoExecutionPolicy {
	policy := autoExecutionPolicy{
		Enabled: metadataBool(metadata, "auto_execute"),
		Paused:  metadataBool(metadata, "auto_execute_paused"),
	}
	for _, key := range []string{"auto_execute_not_before", "auto_execute_after"} {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if parsed, ok := parseBossTimestamp(text); ok {
			policy.NotBefore = parsed
			break
		}
	}
	return policy
}

func metadataBool(metadata map[string]any, key string) bool {
	value, ok := metadata[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return err == nil && parsed
	case float64:
		return typed == 1
	case int:
		return typed == 1
	case int64:
		return typed == 1
	default:
		return false
	}
}

func autoExecutionDueAt(summon Summon, waves []SummonWave) (time.Time, bool) {
	var selected *SummonWave
	var previous *SummonWave
	for index := range waves {
		wave := &waves[index]
		switch wave.Status {
		case WaveStatusActive, WaveStatusFailed:
			return time.Time{}, false
		case WaveStatusPending:
			if selected == nil {
				selected = wave
			}
		case WaveStatusCompleted, WaveStatusSkipped:
			if selected == nil {
				previous = wave
			}
		}
	}
	if selected == nil {
		return time.Time{}, false
	}

	baseText := summon.RequestedAt
	if previous != nil {
		baseText = previous.CompletedAt
		if strings.TrimSpace(baseText) == "" {
			baseText = previous.UpdatedAt
		}
	}
	base, ok := parseBossTimestamp(baseText)
	if !ok {
		return time.Time{}, false
	}
	return base.Add(time.Duration(selected.DelaySeconds) * time.Second), true
}

func parseBossTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
