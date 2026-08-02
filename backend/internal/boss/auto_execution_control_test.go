package boss

import "testing"

func TestNormalizeAutoExecutionControlAction(t *testing.T) {
	cases := map[string]string{
		"pause":             AutoExecutionControlPause,
		"paused":            AutoExecutionControlPause,
		"pause-automation":  AutoExecutionControlPause,
		"resume":            AutoExecutionControlResume,
		"continue":          AutoExecutionControlResume,
		"resume_auto":       AutoExecutionControlResume,
		"skip":              AutoExecutionControlSkipCurrent,
		"skip-current":      AutoExecutionControlSkipCurrent,
		"skip_current_wave": AutoExecutionControlSkipCurrent,
	}
	for input, expected := range cases {
		actual, ok := normalizeAutoExecutionControlAction(input)
		if !ok || actual != expected {
			t.Fatalf("normalize %q = %q, %v; want %q, true", input, actual, ok, expected)
		}
	}
	if actual, ok := normalizeAutoExecutionControlAction("restart"); ok || actual != "" {
		t.Fatalf("unexpected invalid action result: %q, %v", actual, ok)
	}
}

func TestCurrentPendingAutoExecutionWave(t *testing.T) {
	position, ok := currentPendingAutoExecutionWave([]SummonWave{
		{Position: 1, Status: WaveStatusCompleted},
		{Position: 2, Status: WaveStatusSkipped},
		{Position: 3, Status: WaveStatusPending},
		{Position: 4, Status: WaveStatusPending},
	})
	if !ok || position != 3 {
		t.Fatalf("current pending wave = %d, %v; want 3, true", position, ok)
	}
}

func TestCurrentPendingAutoExecutionWaveRejectsUnsafeState(t *testing.T) {
	for _, status := range []string{WaveStatusActive, WaveStatusFailed} {
		position, ok := currentPendingAutoExecutionWave([]SummonWave{
			{Position: 1, Status: status},
			{Position: 2, Status: WaveStatusPending},
		})
		if ok || position != 0 {
			t.Fatalf("status %q returned %d, %v; want 0, false", status, position, ok)
		}
	}
}

func TestCurrentPendingAutoExecutionWaveRequiresPendingWave(t *testing.T) {
	position, ok := currentPendingAutoExecutionWave([]SummonWave{
		{Position: 1, Status: WaveStatusCompleted},
		{Position: 2, Status: WaveStatusSkipped},
	})
	if ok || position != 0 {
		t.Fatalf("terminal wave list returned %d, %v; want 0, false", position, ok)
	}
}
