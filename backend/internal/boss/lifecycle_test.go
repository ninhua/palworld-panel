package boss

import "testing"

func TestFixedBossLifecycleDefaultsAndOverrides(t *testing.T) {
	summon := Summon{Metadata: map[string]any{"activity_kind": "fixed_boss"}}
	wave := SummonWave{PalID: "ChickenPal", Metadata: map[string]any{}}
	if !fixedBossLifecycleEnabled(summon, wave) {
		t.Fatal("fixed Boss lifecycle must default to enabled")
	}
	wave.Metadata["boss_lifecycle_enabled"] = false
	if fixedBossLifecycleEnabled(summon, wave) {
		t.Fatal("wave-level false override was ignored")
	}
	wave.Metadata = map[string]any{}
	summon.Metadata["boss_lifecycle_enabled"] = "false"
	if fixedBossLifecycleEnabled(summon, wave) {
		t.Fatal("summon-level string false override was ignored")
	}
}

func TestBossLifecycleTargetAliases(t *testing.T) {
	wave := SummonWave{
		PalID: "ChickenPal",
		Metadata: map[string]any{
			"target_pal_name": "皮皮鸡",
			"pal_template_snapshot": map[string]any{
				"pal_id":   "ChickenPal",
				"pal_name": "皮皮鸡",
				"nickname": "活动鸡",
			},
		},
	}
	aliases := bossLifecycleTargetAliases(wave)
	if len(aliases) != 3 {
		t.Fatalf("aliases=%#v, want three unique aliases", aliases)
	}
}

func TestBossLifecycleMonitoringFlag(t *testing.T) {
	if !bossLifecycleMonitoring(map[string]any{"boss_lifecycle_monitoring": true}) {
		t.Fatal("boolean monitoring flag was not recognized")
	}
	if !bossLifecycleMonitoring(map[string]any{"boss_lifecycle_monitoring": "true"}) {
		t.Fatal("string monitoring flag was not recognized")
	}
	if bossLifecycleMonitoring(map[string]any{"boss_lifecycle_monitoring": false}) {
		t.Fatal("false monitoring flag was recognized as true")
	}
}
