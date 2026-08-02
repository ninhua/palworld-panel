package tasks

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type completionNoticeRecorder struct {
	mu      sync.Mutex
	notices []CompletionNotice
}

func (recorder *completionNoticeRecorder) NotifyTaskCompletion(_ context.Context, notice CompletionNotice) error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.notices = append(recorder.notices, notice)
	return nil
}

func TestCompletionNoticeMessage(t *testing.T) {
	withReward := CompletionNotice{TaskID: "capture", TaskName: "捕捉棉悠悠", Progress: 3, TargetAmount: 3, RewardPoints: 20}
	if got, want := withReward.Message(), "【任务完成】捕捉棉悠悠（3/3），奖励20积分已到账。"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	withoutReward := CompletionNotice{TaskID: "visit", TaskName: "到达活动区", Progress: 1, TargetAmount: 1}
	if got, want := withoutReward.Message(), "【任务完成】到达活动区（1/1）。"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestCompletionNotificationSendsOnceAfterRewardGranted(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "tasks.db"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	ctx := context.Background()
	if err := service.ensureCompletionNotificationSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.ExecContext(ctx, `UPDATE operations_task_completion_notification_meta SET value=? WHERE key='activated_at'`, "2026-08-03T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.ExecContext(ctx, `CREATE TABLE economy_accounts(player_uid TEXT PRIMARY KEY,nickname TEXT NOT NULL DEFAULT '',steam_id TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.ExecContext(ctx, `INSERT INTO economy_accounts(player_uid,nickname,steam_id) VALUES('F23D556C000000000000000000000000','tiantian','steam_76561199032061430')`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.ExecContext(ctx, `INSERT INTO operations_tasks(id,name,event_type,target_amount,reward_points,cycle,amount_field,filters_json,enabled,created_at,updated_at,archived_at) VALUES('task_capture','捕捉帕鲁','PAL_CAPTURED',3,20,'daily','','{}',1,?,?, '')`, "2026-08-03T00:00:00Z", "2026-08-03T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.ExecContext(ctx, `INSERT INTO operations_task_progress(task_id,player_uid,cycle_key,progress,completed,completed_at,updated_at) VALUES('task_capture','F23D556C000000000000000000000000','2026-08-03',3,1,'2026-08-03T02:00:00Z','2026-08-03T02:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.ExecContext(ctx, `INSERT INTO operations_task_rewards(task_id,player_uid,cycle_key,nickname,steam_id,points,status,ledger_reference_id,attempts,last_error,created_at,updated_at,granted_at) VALUES('task_capture','F23D556C000000000000000000000000','2026-08-03','tiantian','steam_76561199032061430',20,'granted','reward-ref',1,'','2026-08-03T02:00:00Z','2026-08-03T02:00:00Z','2026-08-03T02:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	recorder := &completionNoticeRecorder{}
	runtime := &completionNotificationRuntime{notifier: recorder}
	now, _ := time.Parse(time.RFC3339, "2026-08-03T02:01:00Z")
	service.now = func() time.Time { return now }
	if err := service.processCompletionNotifications(ctx, runtime); err != nil {
		t.Fatal(err)
	}
	if err := service.processCompletionNotifications(ctx, runtime); err != nil {
		t.Fatal(err)
	}

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.notices) != 1 {
		t.Fatalf("notices = %d, want 1", len(recorder.notices))
	}
	notice := recorder.notices[0]
	if notice.PlayerUID != "F23D556C000000000000000000000000" || notice.RewardPoints != 20 {
		t.Fatalf("unexpected notice: %#v", notice)
	}
	var status string
	if err := service.db.QueryRowContext(ctx, `SELECT status FROM operations_task_completion_notifications WHERE task_id='task_capture'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "sent" {
		t.Fatalf("status = %q, want sent", status)
	}
}

func TestCompletionNotificationWaitsForReward(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "tasks.db"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	if err := service.ensureCompletionNotificationSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.ExecContext(ctx, `UPDATE operations_task_completion_notification_meta SET value=? WHERE key='activated_at'`, "2026-08-03T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.ExecContext(ctx, `CREATE TABLE economy_accounts(player_uid TEXT PRIMARY KEY,nickname TEXT NOT NULL DEFAULT '',steam_id TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.ExecContext(ctx, `INSERT INTO operations_tasks(id,name,event_type,target_amount,reward_points,cycle,amount_field,filters_json,enabled,created_at,updated_at,archived_at) VALUES('task_capture','捕捉帕鲁','PAL_CAPTURED',1,10,'daily','','{}',1,?,?, '')`, "2026-08-03T00:00:00Z", "2026-08-03T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.ExecContext(ctx, `INSERT INTO operations_task_progress(task_id,player_uid,cycle_key,progress,completed,completed_at,updated_at) VALUES('task_capture','player','2026-08-03',1,1,'2026-08-03T02:00:00Z','2026-08-03T02:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.ExecContext(ctx, `INSERT INTO operations_task_rewards(task_id,player_uid,cycle_key,nickname,steam_id,points,status,ledger_reference_id,attempts,last_error,created_at,updated_at,granted_at) VALUES('task_capture','player','2026-08-03','player','steam_1',10,'pending','reward-ref',0,'','2026-08-03T02:00:00Z','2026-08-03T02:00:00Z','')`); err != nil {
		t.Fatal(err)
	}

	recorder := &completionNoticeRecorder{}
	runtime := &completionNotificationRuntime{notifier: recorder}
	now, _ := time.Parse(time.RFC3339, "2026-08-03T02:01:00Z")
	service.now = func() time.Time { return now }
	if err := service.processCompletionNotifications(ctx, runtime); err != nil {
		t.Fatal(err)
	}
	if len(recorder.notices) != 0 {
		t.Fatalf("pending reward produced %d notices", len(recorder.notices))
	}
	if _, err := service.db.ExecContext(ctx, `UPDATE operations_task_rewards SET status='granted',granted_at=? WHERE task_id='task_capture'`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := service.processCompletionNotifications(ctx, runtime); err != nil {
		t.Fatal(err)
	}
	if len(recorder.notices) != 1 {
		t.Fatalf("granted reward produced %d notices, want 1", len(recorder.notices))
	}
}
