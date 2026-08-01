package economy

import "testing"

func TestCalculateCheckinBonus(t *testing.T) {
	config := defaultDetailedConfig(Config{CommandPrefix: "!", DailyCheckinPoints: 10})
	cases := []struct {
		day           int
		streak, cycle int64
		total         int64
	}{
		{day: 1, streak: 0, cycle: 0, total: 10},
		{day: 2, streak: 2, cycle: 0, total: 12},
		{day: 7, streak: 12, cycle: 10, total: 32},
		{day: 8, streak: 12, cycle: 0, total: 22},
		{day: 14, streak: 12, cycle: 10, total: 32},
	}
	for _, test := range cases {
		streak, cycle := calculateCheckinBonus(config, test.day)
		if streak != test.streak || cycle != test.cycle {
			t.Fatalf("day %d bonus = (%d,%d), want (%d,%d)", test.day, streak, cycle, test.streak, test.cycle)
		}
		if got := calculateCheckinPoints(config, test.day, 10); got != test.total {
			t.Fatalf("day %d total = %d, want %d", test.day, got, test.total)
		}
	}
}

func TestParseDetailedCommandAllowsBareAlias(t *testing.T) {
	config := defaultDetailedConfig(Config{CommandPrefix: "!", DailyCheckinPoints: 10})
	for _, input := range []string{"!签到", "签到", "!qd", "qd"} {
		command, handled := parseDetailedCommand(input, config)
		if !handled || command != "checkin" {
			t.Fatalf("%q = (%q,%t), want checkin", input, command, handled)
		}
	}
	config.AllowBareCommands = false
	if _, handled := parseDetailedCommand("签到", config); handled {
		t.Fatal("bare command must be rejected when disabled")
	}
	if command, handled := parseDetailedCommand("!签到", config); !handled || command != "checkin" {
		t.Fatal("prefixed command must still work")
	}
}

func TestDetailedConfigValidation(t *testing.T) {
	config := defaultDetailedConfig(Config{CommandPrefix: "!", DailyCheckinPoints: 10})
	if err := validateDetailedConfig(config); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
	config.CheckinCycleDays = 366
	if err := validateDetailedConfig(config); err == nil {
		t.Fatal("expected invalid cycle days")
	}
}
