package boss

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestRewardTemplateAndSummonLifecycle(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "boss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.now = func() time.Time { return time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC) }
	ctx := context.Background()

	reward, err := service.CreateReward(ctx, RewardInput{
		Name: "首杀奖励", Points: 100,
		Items:        []RewardItem{{ItemID: "LegendSphere", Count: 3}},
		PalTemplates: []string{"boss_reward_pal.json"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	template, err := service.CreateTemplate(ctx, TemplateInput{
		Name: "空涡龙试炼", PalID: "JetDragon", Level: 60, Count: 1,
		HPMultiplier: 5, AttackMultiplier: 2, DefenseMultiplier: 1.5,
		SpawnRadius: 500, Capturable: false, CooldownSeconds: 3600,
		RewardID: reward.ID, Location: Location{X: 100, Y: 200, Z: 300, Label: "Arena"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	request := CreateSummonRequest{TemplateID: template.ID, RequestKey: "manual-20260801-001", Notes: "test"}
	created, err := service.CreateSummon(ctx, request, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if created.Duplicate || created.Summon.Status != SummonStatusPending || created.Summon.ExecutionMode != ExecutionModeRecordOnly {
		t.Fatalf("unexpected summon result: %+v", created)
	}
	duplicate, err := service.CreateSummon(ctx, request, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.Duplicate || duplicate.Summon.ID != created.Summon.ID {
		t.Fatalf("idempotency failed: %+v", duplicate)
	}

	active, err := service.TransitionSummon(ctx, created.Summon.ID, TransitionRequest{Status: SummonStatusActive, Message: "spawn acknowledged"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != SummonStatusActive || active.StartedAt == "" {
		t.Fatalf("unexpected active summon: %+v", active)
	}
	completed, err := service.TransitionSummon(ctx, active.ID, TransitionRequest{Status: SummonStatusCompleted, Result: map[string]any{"winner": "guild-a"}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != SummonStatusCompleted || completed.CompletedAt == "" {
		t.Fatalf("unexpected completed summon: %+v", completed)
	}
	if _, err := service.TransitionSummon(ctx, active.ID, TransitionRequest{Status: SummonStatusFailed}, "admin"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal summon transition error = %v", err)
	}

	events, err := service.ListSummonEvents(ctx, completed.ID, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	summary, err := service.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Rewards != 1 || summary.Templates != 1 || summary.CompletedSummons != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestBossValidationAndArchive(t *testing.T) {
	service, err := Open(filepath.Join(t.TempDir(), "boss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()

	if _, err := service.CreateReward(ctx, RewardInput{Name: "bad", Items: []RewardItem{{ItemID: "bad item", Count: 1}}}); !errors.Is(err, ErrInvalidReward) {
		t.Fatalf("invalid reward error = %v", err)
	}
	if _, err := service.CreateTemplate(ctx, TemplateInput{Name: "bad", PalID: "JetDragon", Level: 0}); !errors.Is(err, ErrInvalidTemplate) {
		t.Fatalf("invalid template error = %v", err)
	}

	reward, err := service.CreateReward(ctx, RewardInput{Name: "奖励", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := service.ArchiveReward(ctx, reward.ID)
	if err != nil {
		t.Fatal(err)
	}
	if archived.Enabled || archived.ArchivedAt == "" {
		t.Fatalf("reward not archived: %+v", archived)
	}
}
