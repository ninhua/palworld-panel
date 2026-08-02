package tasks

import (
	"strings"
	"testing"
	"time"
)

func TestOnlineSampleSeconds(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	if got := onlineSampleSeconds(now.Add(-45*time.Second), now, false); got != 45 {
		t.Fatalf("expected 45 seconds, got %d", got)
	}
	if got := onlineSampleSeconds(now.Add(-10*time.Minute), now, false); got != 90 {
		t.Fatalf("expected capped 90 seconds, got %d", got)
	}
	if got := onlineSampleSeconds(now.Add(-45*time.Second), now, true); got != 0 {
		t.Fatalf("expected reset sample to emit zero, got %d", got)
	}
}

func TestOnlineEventIDIsStableAndBounded(t *testing.T) {
	first := onlineEventID(strings.Repeat("x", 128), 1, 3)
	second := onlineEventID(strings.Repeat("x", 128), 1, 3)
	if first != second {
		t.Fatalf("expected stable event id")
	}
	if len(first) > 128 {
		t.Fatalf("event id too long: %d", len(first))
	}
	if first == onlineEventID(strings.Repeat("x", 128), 2, 3) {
		t.Fatalf("different minute range should produce a different id")
	}
}

func TestParsePlayerTaskCommand(t *testing.T) {
	cases := []struct {
		message   string
		prefix    string
		allowBare bool
		args      string
		handled   bool
	}{
		{message: "!任务", prefix: "!", args: "", handled: true},
		{message: "任务 2", prefix: "!", allowBare: true, args: "2", handled: true},
		{message: "我的任务 捕捉", prefix: "", args: "捕捉", handled: true},
		{message: "任务进度", prefix: "!", allowBare: false, handled: false},
		{message: "商城", prefix: "", handled: false},
	}
	for _, test := range cases {
		_, args, handled := parsePlayerTaskCommand(test.message, test.prefix, test.allowBare)
		if handled != test.handled || args != test.args {
			t.Fatalf("message %q: handled=%v args=%q", test.message, handled, args)
		}
	}
}

func TestFormatPlayerTaskProgress(t *testing.T) {
	text := formatPlayerTaskProgress(PlayerProgressPage{
		PlayerUID: "player",
		Items:     []Progress{{TaskName: "在线30分钟", Progress: 12, TargetAmount: 30, Cycle: "daily", RewardPoints: 10}},
		Count:     1, Total: 1, Limit: 5,
	}, 1)
	for _, expected := range []string{"在线30分钟", "12/30", "每日", "奖励10积分"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in %q", expected, text)
		}
	}
}
