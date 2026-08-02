package boss

import (
	"context"
	"errors"
	"testing"
)

type raidTestExecutor struct {
	waves   []SummonWave
	summons []Summon
	failAt  int
}

func (executor *raidTestExecutor) Status(context.Context) ExecutionAdapterStatus {
	return ExecutionAdapterStatus{Adapter: "test", Available: true, State: "ready", Capabilities: ExecutionCapabilities{MultipleSpawns: true, CustomPalTemplate: true}}
}

func (executor *raidTestExecutor) ExecuteWave(_ context.Context, summon Summon, wave SummonWave) (WaveExecutionResult, error) {
	executor.summons = append(executor.summons, summon)
	executor.waves = append(executor.waves, wave)
	if executor.failAt > 0 && len(executor.waves) == executor.failAt {
		return WaveExecutionResult{Adapter: "test", Commands: []string{"failed"}, Responses: []string{""}, Details: map[string]any{}}, errors.New("boom")
	}
	commands := make([]string, wave.Count)
	responses := make([]string, wave.Count)
	for index := range commands {
		commands[index] = "ok"
		responses[index] = "ok"
	}
	return WaveExecutionResult{Adapter: "test", Commands: commands, Responses: responses, CompletedCommands: wave.Count, Details: map[string]any{"template": metadataString(wave.Metadata, "pal_template_file")}}, nil
}

func TestRaidExecutionUsesResolvedBaseAndMultipleGroups(t *testing.T) {
	inner := &raidTestExecutor{}
	adapter := NewRaidExecutionAdapter(inner, RaidBaseResolverFunc(func(_ context.Context, baseID string) (RaidBaseLocation, error) {
		if baseID != "base-1" {
			t.Fatalf("unexpected base id %q", baseID)
		}
		return RaidBaseLocation{ID: baseID, Name: "据点一", X: 100, Y: 200, Z: 300}, nil
	}))
	capturable := true
	wave := SummonWave{
		Position: 1, Name: "第一波", PalID: "Legacy", Level: 1, Count: 1,
		Metadata: map[string]any{
			"raid_base_id": "base-1",
			"spawn_groups": []map[string]any{
				{"name": "主力", "pal_template_file": "boss_a.json", "pal_id": "BossA", "level": 50, "count": 2, "spawn_radius": 250, "capturable": false},
				{"name": "护卫", "pal_template_file": "guard_b.json", "pal_id": "GuardB", "level": 40, "count": 3, "spawn_radius": 400, "capturable": capturable},
			},
		},
	}
	result, err := adapter.ExecuteWave(context.Background(), Summon{ID: "summon-1", Location: Location{X: 999, Y: 999, Z: 999}}, wave)
	if err != nil {
		t.Fatal(err)
	}
	if result.CompletedCommands != 5 || len(inner.waves) != 2 {
		t.Fatalf("unexpected result: %+v waves=%d", result, len(inner.waves))
	}
	if inner.waves[0].PalID != "BossA" || inner.waves[0].Count != 2 || metadataString(inner.waves[0].Metadata, "pal_template_file") != "boss_a.json" {
		t.Fatalf("unexpected first group: %+v", inner.waves[0])
	}
	if inner.waves[1].PalID != "GuardB" || inner.waves[1].Count != 3 || !inner.waves[1].Capturable {
		t.Fatalf("unexpected second group: %+v", inner.waves[1])
	}
	for _, summon := range inner.summons {
		if summon.Location.Z != 300 || summon.Location.Label != "据点一" {
			t.Fatalf("base was not resolved: %+v", summon.Location)
		}
		if summon.Location.X == 999 || summon.Location.Y == 999 {
			t.Fatalf("legacy coordinates were used: %+v", summon.Location)
		}
	}
}

func TestRaidExecutionBlocksMissingBase(t *testing.T) {
	inner := &raidTestExecutor{}
	adapter := NewRaidExecutionAdapter(inner, RaidBaseResolverFunc(func(context.Context, string) (RaidBaseLocation, error) {
		return RaidBaseLocation{}, errors.New("not found")
	}))
	_, err := adapter.ExecuteWave(context.Background(), Summon{}, SummonWave{
		Metadata: map[string]any{"raid_base_id": "gone", "spawn_groups": []map[string]any{{"pal_template_file": "boss.json", "pal_id": "Boss", "level": 50, "count": 1, "spawn_radius": 1}}},
	})
	if err == nil || len(inner.waves) != 0 {
		t.Fatalf("expected blocked execution, err=%v waves=%d", err, len(inner.waves))
	}
}

func TestRaidExecutionLegacyDelegation(t *testing.T) {
	inner := &raidTestExecutor{}
	adapter := NewRaidExecutionAdapter(inner, nil)
	_, err := adapter.ExecuteWave(context.Background(), Summon{}, SummonWave{PalID: "Legacy", Level: 1, Count: 1, Metadata: map[string]any{}})
	if err != nil || len(inner.waves) != 1 || inner.waves[0].PalID != "Legacy" {
		t.Fatalf("legacy delegation failed: err=%v waves=%+v", err, inner.waves)
	}
}

func TestRaidExecutionMarksLaterFailureUncertain(t *testing.T) {
	inner := &raidTestExecutor{failAt: 2}
	adapter := NewRaidExecutionAdapter(inner, RaidBaseResolverFunc(func(context.Context, string) (RaidBaseLocation, error) {
		return RaidBaseLocation{ID: "base", X: 1, Y: 2, Z: 3}, nil
	}))
	result, err := adapter.ExecuteWave(context.Background(), Summon{}, SummonWave{Metadata: map[string]any{
		"raid_base_id": "base",
		"spawn_groups": []map[string]any{
			{"pal_template_file": "a.json", "pal_id": "A", "level": 10, "count": 1, "spawn_radius": 1},
			{"pal_template_file": "b.json", "pal_id": "B", "level": 10, "count": 1, "spawn_radius": 1},
		},
	}})
	var executionErr *ExecutionError
	if err == nil || !errors.As(err, &executionErr) || !executionErr.Uncertain || result.CompletedCommands != 1 {
		t.Fatalf("expected uncertain partial failure: result=%+v err=%v", result, err)
	}
}
