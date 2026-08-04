package tasks

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPlayerIdentityMigrationMergesTaskProgress(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "tasks.sqlite"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	definition, err := service.CreateDefinition(context.Background(), DefinitionInput{
		Name: "捕捉测试", EventType: "PAL_CAPTURED", TargetAmount: 10,
		RewardPoints: 5, Cycle: "once", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	const (
		compact = "F23D556C000000000000000000000000"
		hyphens = "F23D556C-00000000-00000000-00000000"
	)
	for _, item := range []struct {
		uid      string
		progress int
	}{
		{compact, 3},
		{hyphens, 4},
	} {
		if _, err := service.db.Exec(`INSERT INTO operations_task_progress(task_id,player_uid,cycle_key,progress,completed,completed_at,updated_at)
			VALUES(?,?, 'once', ?, 0, '', '2026-08-03T00:00:00Z')`, definition.ID, item.uid, item.progress); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.EnsureOnlineTrackingSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.Exec(`INSERT INTO operations_task_online_tracking(player_uid,nickname,steam_id,active,online_since,last_seen_at,pending_seconds,total_emitted_minutes,updated_at)
		VALUES
		(?, 'tiantian', 'steam_76561199032061430', 1, '2026-08-03T00:00:00Z', '2026-08-03T00:01:00Z', 20, 4, '2026-08-03T00:01:00Z'),
		(?, 'tiantian', 'steam_76561199032061430', 1, '2026-08-03T00:00:00Z', '2026-08-03T00:02:00Z', 30, 5, '2026-08-03T00:02:00Z')`, compact, hyphens); err != nil {
		t.Fatal(err)
	}

	if err := service.EnsurePlayerIdentityConsistency(context.Background()); err != nil {
		t.Fatal(err)
	}
	progress, err := service.PlayerProgress(context.Background(), hyphens)
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) != 1 || progress[0].Progress != 7 {
		t.Fatalf("merged progress = %+v", progress)
	}
	online, err := service.OnlineTrackingRecords(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 1 || online[0].PlayerUID != compact || online[0].TotalEmittedMinutes != 5 {
		t.Fatalf("merged online tracking = %+v", online)
	}
}
