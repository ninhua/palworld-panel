package boss

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestBossDailyScheduleWarningAndSummon(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "boss-schedule.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	base := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return base }
	ctx := context.Background()

	template, err := service.CreateTemplate(ctx, TemplateInput{
		Name: "每日试炼", PalID: "JetDragon", Level: 60, Count: 1,
		HPMultiplier: 5, AttackMultiplier: 2, DefenseMultiplier: 1.5,
		SpawnRadius: 500, Location: Location{X: 10, Y: 20, Z: 30}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	schedule, err := service.CreateSchedule(ctx, ScheduleInput{
		Name: "每天十点", TemplateID: template.ID, Mode: ScheduleModeDaily,
		DailyTime: "10:00", Timezone: "Asia/Shanghai", WarningMinutes: 30,
		WarningTitle: "Boss预警", WarningMessage: "{{boss}}将在{{minutes}}分钟后开始",
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if schedule.NextRunAt != "2026-08-01T02:00:00Z" {
		t.Fatalf("next run = %q", schedule.NextRunAt)
	}

	warningResult, err := service.RunDueSchedules(ctx, time.Date(2026, 8, 1, 1, 31, 0, 0, time.UTC), "test-runner")
	if err != nil {
		t.Fatal(err)
	}
	if warningResult.Warnings != 1 || warningResult.Summons != 0 {
		t.Fatalf("warning result = %+v", warningResult)
	}
	duplicateWarning, err := service.RunDueSchedules(ctx, time.Date(2026, 8, 1, 1, 45, 0, 0, time.UTC), "test-runner")
	if err != nil {
		t.Fatal(err)
	}
	if duplicateWarning.Warnings != 0 {
		t.Fatalf("duplicate warning result = %+v", duplicateWarning)
	}

	dueResult, err := service.RunDueSchedules(ctx, time.Date(2026, 8, 1, 2, 0, 5, 0, time.UTC), "test-runner")
	if err != nil {
		t.Fatal(err)
	}
	if dueResult.Summons != 1 || dueResult.Failed != 0 {
		t.Fatalf("due result = %+v", dueResult)
	}
	summons, err := service.ListSummons(ctx, SummonFilter{TemplateID: template.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(summons) != 1 || summons[0].Metadata["schedule_id"] != schedule.ID {
		t.Fatalf("scheduled summons = %+v", summons)
	}
	updated, err := service.GetSchedule(ctx, schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.NextRunAt != "2026-08-02T02:00:00Z" || updated.LastRunAt != "2026-08-01T02:00:00Z" {
		t.Fatalf("updated schedule = %+v", updated)
	}
	events, err := service.ListScheduleEvents(ctx, ScheduleEventFilter{ScheduleID: schedule.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].EventType != ScheduleEventSummon || events[1].EventType != ScheduleEventWarning {
		t.Fatalf("events = %+v", events)
	}
}

func TestBossCronScheduleAndValidation(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "boss-cron.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.now = func() time.Time { return time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) }
	ctx := context.Background()
	template, err := service.CreateTemplate(ctx, TemplateInput{
		Name: "周末试炼", PalID: "JetDragon", Level: 50, Count: 1,
		HPMultiplier: 1, AttackMultiplier: 1, DefenseMultiplier: 1,
		SpawnRadius: 100, Location: Location{}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateSchedule(ctx, ScheduleInput{
		Name: "每周六晚八点", TemplateID: template.ID, Mode: ScheduleModeCron,
		Cron: "0 20 * * 6", Timezone: "Asia/Shanghai", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.NextRunAt != "2026-08-01T12:00:00Z" {
		t.Fatalf("cron next run = %q", created.NextRunAt)
	}
	if _, err := service.CreateSchedule(ctx, ScheduleInput{
		Name: "错误Cron", TemplateID: template.ID, Mode: ScheduleModeCron,
		Cron: "61 20 * * *", Timezone: "Asia/Shanghai", Enabled: true,
	}); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("invalid cron error = %v", err)
	}
	archived, err := service.ArchiveSchedule(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if archived.Enabled || archived.ArchivedAt == "" || archived.NextRunAt != "" {
		t.Fatalf("archived schedule = %+v", archived)
	}
}

func TestNextScheduleTimePure(t *testing.T) {
	daily, err := nextScheduleTime(Schedule{Mode: ScheduleModeDaily, DailyTime: "20:00", Timezone: "Asia/Shanghai"}, time.Date(2026, 8, 1, 11, 59, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got := daily.Format(time.RFC3339); got != "2026-08-01T12:00:00Z" {
		t.Fatalf("daily next = %s", got)
	}
	cron, err := nextScheduleTime(Schedule{Mode: ScheduleModeCron, Cron: "*/15 20 * * 6", Timezone: "Asia/Shanghai"}, time.Date(2026, 8, 1, 12, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got := cron.Format(time.RFC3339); got != "2026-08-01T12:15:00Z" {
		t.Fatalf("cron next = %s", got)
	}
	if _, err := parseCron("61 20 * * *"); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("invalid cron error = %v", err)
	}
}
