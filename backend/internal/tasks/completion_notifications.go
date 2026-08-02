package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	completionNotificationPollInterval = time.Second
	completionNotificationRetryDelay   = 30 * time.Second
	completionNotificationStaleAfter   = time.Minute
)

// CompletionNotice is emitted after a task reaches its target and any point
// reward has been committed successfully.
type CompletionNotice struct {
	TaskID       string `json:"task_id"`
	TaskName     string `json:"task_name"`
	PlayerUID    string `json:"player_uid"`
	Nickname     string `json:"nickname,omitempty"`
	SteamID      string `json:"steam_id,omitempty"`
	CycleKey     string `json:"cycle_key"`
	Progress     int64  `json:"progress"`
	TargetAmount int64  `json:"target_amount"`
	RewardPoints int64  `json:"reward_points"`
	Attempts     int    `json:"attempts"`
}

func (notice CompletionNotice) Message() string {
	name := strings.TrimSpace(notice.TaskName)
	if name == "" {
		name = strings.TrimSpace(notice.TaskID)
	}
	if notice.RewardPoints > 0 {
		return fmt.Sprintf("【任务完成】%s（%d/%d），奖励%d积分已到账。", name, notice.Progress, notice.TargetAmount, notice.RewardPoints)
	}
	return fmt.Sprintf("【任务完成】%s（%d/%d）。", name, notice.Progress, notice.TargetAmount)
}

func (notice CompletionNotice) EventID() string {
	return "task_completion:" + strings.TrimSpace(notice.TaskID) + ":" + strings.TrimSpace(notice.PlayerUID) + ":" + strings.TrimSpace(notice.CycleKey)
}

type CompletionNotifier interface {
	NotifyTaskCompletion(context.Context, CompletionNotice) error
}

type CompletionNotifierFunc func(context.Context, CompletionNotice) error

func (function CompletionNotifierFunc) NotifyTaskCompletion(ctx context.Context, notice CompletionNotice) error {
	if function == nil {
		return errors.New("task completion notifier is unavailable")
	}
	return function(ctx, notice)
}

type completionNotificationRuntime struct {
	mu          sync.RWMutex
	notifier    CompletionNotifier
	started     bool
	schemaMu    sync.Mutex
	schemaReady bool
}

var completionNotificationRuntimes sync.Map

// SetCompletionNotifier installs the game-message delivery callback and starts
// the persistent notification worker once for this task service.
func (s *Service) SetCompletionNotifier(notifier CompletionNotifier) error {
	if s == nil || s.db == nil {
		return errors.New("task service is unavailable")
	}
	if notifier == nil {
		return errors.New("task completion notifier is required")
	}
	actual, _ := completionNotificationRuntimes.LoadOrStore(s, &completionNotificationRuntime{})
	runtime := actual.(*completionNotificationRuntime)
	if err := s.ensureCompletionNotificationRuntimeSchema(context.Background(), runtime); err != nil {
		return err
	}
	runtime.mu.Lock()
	runtime.notifier = notifier
	if !runtime.started {
		runtime.started = true
		go s.runCompletionNotificationWorker(runtime)
	}
	runtime.mu.Unlock()
	return nil
}

func (s *Service) ensureCompletionNotificationRuntimeSchema(ctx context.Context, runtime *completionNotificationRuntime) error {
	runtime.schemaMu.Lock()
	defer runtime.schemaMu.Unlock()
	if runtime.schemaReady {
		return nil
	}
	if err := s.ensureCompletionNotificationSchema(ctx); err != nil {
		return err
	}
	runtime.schemaReady = true
	return nil
}

func (s *Service) ensureCompletionNotificationSchema(ctx context.Context) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS operations_task_completion_notification_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS operations_task_completion_notifications (
			task_id TEXT NOT NULL,
			player_uid TEXT NOT NULL,
			cycle_key TEXT NOT NULL,
			task_name TEXT NOT NULL DEFAULT '',
			nickname TEXT NOT NULL DEFAULT '',
			steam_id TEXT NOT NULL DEFAULT '',
			progress INTEGER NOT NULL DEFAULT 0,
			target_amount INTEGER NOT NULL DEFAULT 0,
			reward_points INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','sent','failed')),
			attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0),
			next_attempt_at TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			sent_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(task_id,player_uid,cycle_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_operations_task_completion_notifications_status
			ON operations_task_completion_notifications(status,next_attempt_at,created_at)`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create task completion notification schema: %w", err)
		}
	}
	now := s.timestamp()
	if _, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO operations_task_completion_notification_meta(key,value,updated_at)
		VALUES('activated_at',?,?)
	`, now, now); err != nil {
		return fmt.Errorf("initialize task completion notification activation: %w", err)
	}
	return nil
}

func (s *Service) runCompletionNotificationWorker(runtime *completionNotificationRuntime) {
	ticker := time.NewTicker(completionNotificationPollInterval)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = s.processCompletionNotifications(ctx, runtime)
		cancel()
		<-ticker.C
	}
}

func (s *Service) processCompletionNotifications(ctx context.Context, runtime *completionNotificationRuntime) error {
	if err := s.ensureCompletionNotificationRuntimeSchema(ctx, runtime); err != nil {
		return err
	}
	now := s.now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		UPDATE operations_task_completion_notifications
		SET status='failed',next_attempt_at=?,last_error='delivery interrupted before completion',updated_at=?
		WHERE status='sending' AND updated_at<?
	`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Add(-completionNotificationStaleAfter).Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if err := s.discoverCompletionNotifications(ctx, now); err != nil {
		return err
	}
	for processed := 0; processed < 20; processed++ {
		notice, found, err := s.claimCompletionNotification(ctx, now)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		runtime.mu.RLock()
		notifier := runtime.notifier
		runtime.mu.RUnlock()
		if notifier == nil {
			_ = s.failCompletionNotification(ctx, notice, "task completion notifier is unavailable", now)
			return nil
		}
		deliveryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		deliveryErr := notifier.NotifyTaskCompletion(deliveryCtx, notice)
		cancel()
		if deliveryErr != nil {
			if err := s.failCompletionNotification(ctx, notice, deliveryErr.Error(), now); err != nil {
				return err
			}
			continue
		}
		if err := s.completeCompletionNotification(ctx, notice, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) discoverCompletionNotifications(ctx context.Context, now time.Time) error {
	timestamp := now.Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO operations_task_completion_notifications(
			task_id,player_uid,cycle_key,task_name,nickname,steam_id,progress,target_amount,reward_points,
			status,attempts,next_attempt_at,last_error,created_at,updated_at,sent_at
		)
		SELECT
			progress.task_id,
			progress.player_uid,
			progress.cycle_key,
			task.name,
			COALESCE(NULLIF(reward.nickname,''),NULLIF(account.nickname,''),''),
			COALESCE(NULLIF(reward.steam_id,''),NULLIF(account.steam_id,''),''),
			progress.progress,
			task.target_amount,
			task.reward_points,
			'pending',0,'','',progress.completed_at,?,''
		FROM operations_task_progress AS progress
		JOIN operations_tasks AS task ON task.id=progress.task_id
		LEFT JOIN operations_task_rewards AS reward
			ON reward.task_id=progress.task_id
			AND reward.player_uid=progress.player_uid
			AND reward.cycle_key=progress.cycle_key
		LEFT JOIN economy_accounts AS account ON account.player_uid=progress.player_uid
		WHERE progress.completed=1
			AND progress.completed_at<>''
			AND progress.completed_at>=(
				SELECT value FROM operations_task_completion_notification_meta WHERE key='activated_at'
			)
			AND (task.reward_points=0 OR reward.status='granted')
	`, timestamp)
	if err != nil {
		return fmt.Errorf("discover task completion notifications: %w", err)
	}
	return nil
}

func (s *Service) claimCompletionNotification(ctx context.Context, now time.Time) (CompletionNotice, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CompletionNotice{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var notice CompletionNotice
	err = tx.QueryRowContext(ctx, `
		SELECT task_id,task_name,player_uid,nickname,steam_id,cycle_key,progress,target_amount,reward_points,attempts
		FROM operations_task_completion_notifications
		WHERE status IN ('pending','failed') AND (next_attempt_at='' OR next_attempt_at<=?)
		ORDER BY created_at,task_id,player_uid,cycle_key
		LIMIT 1
	`, now.Format(time.RFC3339Nano)).Scan(
		&notice.TaskID, &notice.TaskName, &notice.PlayerUID, &notice.Nickname, &notice.SteamID,
		&notice.CycleKey, &notice.Progress, &notice.TargetAmount, &notice.RewardPoints, &notice.Attempts,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CompletionNotice{}, false, nil
	}
	if err != nil {
		return CompletionNotice{}, false, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE operations_task_completion_notifications
		SET status='sending',attempts=attempts+1,last_error='',updated_at=?
		WHERE task_id=? AND player_uid=? AND cycle_key=? AND status IN ('pending','failed')
	`, now.Format(time.RFC3339Nano), notice.TaskID, notice.PlayerUID, notice.CycleKey)
	if err != nil {
		return CompletionNotice{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return CompletionNotice{}, false, err
	}
	if affected != 1 {
		return CompletionNotice{}, false, nil
	}
	if err := tx.Commit(); err != nil {
		return CompletionNotice{}, false, err
	}
	notice.Attempts++
	return notice, true, nil
}

func (s *Service) completeCompletionNotification(ctx context.Context, notice CompletionNotice, now time.Time) error {
	timestamp := now.Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		UPDATE operations_task_completion_notifications
		SET status='sent',next_attempt_at='',last_error='',sent_at=?,updated_at=?
		WHERE task_id=? AND player_uid=? AND cycle_key=? AND status='sending'
	`, timestamp, timestamp, notice.TaskID, notice.PlayerUID, notice.CycleKey)
	return err
}

func (s *Service) failCompletionNotification(ctx context.Context, notice CompletionNotice, message string, now time.Time) error {
	message = strings.TrimSpace(message)
	if len(message) > 1024 {
		message = message[:1024]
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE operations_task_completion_notifications
		SET status='failed',next_attempt_at=?,last_error=?,updated_at=?
		WHERE task_id=? AND player_uid=? AND cycle_key=? AND status='sending'
	`, now.Add(completionNotificationRetryDelay).Format(time.RFC3339Nano), message, now.Format(time.RFC3339Nano), notice.TaskID, notice.PlayerUID, notice.CycleKey)
	return err
}
