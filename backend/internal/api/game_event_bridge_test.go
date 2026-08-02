package api

import (
	"strings"
	"testing"

	"palpanel/internal/economy"
	"palpanel/internal/gameevents"
	"palpanel/internal/tasks"
)

func bridgeTestConfig() economy.DetailedConfig {
	return economy.DetailedConfig{
		CommandPrefix: "!", AllowBareCommands: true,
		CheckinAliases: []string{"签到", "qd", "checkin"},
		PointsAliases:  []string{"积分", "jf", "points"},
		HelpAliases:    []string{"帮助", "菜单", "help"},
	}
}

func TestParsePalDefenderCaptureLog(t *testing.T) {
	line := "[2026-08-01 21:12:03] [Info] Alice has captured Pal 'Lamball' (SheepBall) at 1.5 2.5 3.5."
	event, ok := parsePalDefenderLogLine(line, bridgeTestConfig())
	if !ok || event.Type != "PAL_CAPTURED" || event.Payload["pal_id"] != "SheepBall" {
		t.Fatalf("unexpected capture parse: %#v, %t", event, ok)
	}
}

func TestParsePalDefenderConfiguredChatCommands(t *testing.T) {
	cases := []struct {
		line, message string
	}{
		{"[2026-08-01 21:12:03] [Chat] Alice: !签到", "!签到"},
		{"[2026-08-01 21:12:03] [GlobalChat] Alice：签到", "签到"},
		{"[Info] Chat steam_76561198000000000 Alice > !积分", "!积分"},
	}
	for _, test := range cases {
		event, ok := parsePalDefenderLogLine(test.line, bridgeTestConfig())
		if !ok || event.Type != "PLAYER_CHAT" || event.Payload["message"] != test.message {
			t.Fatalf("%q => %#v, %t", test.line, event, ok)
		}
	}
}

func TestParsePalDefenderShopChatCommands(t *testing.T) {
	cases := []struct {
		line, message string
	}{
		{"[Info] Chat Alice: !商城", "!商城"},
		{"[Info] Chat Alice: 兑换 ABCD1234 2", "兑换 ABCD1234 2"},
		{"[Info] Chat Alice: 我的订单", "我的订单"},
	}
	for _, test := range cases {
		event, ok := parsePalDefenderLogLine(test.line, bridgeTestConfig())
		if !ok || event.Type != "PLAYER_CHAT" || event.Payload["message"] != test.message {
			t.Fatalf("%q => %#v, %t", test.line, event, ok)
		}
	}
}

func TestConfiguredChatRejectsOrdinaryText(t *testing.T) {
	if _, ok := parsePalDefenderLogLine("[Info] server startup completed", bridgeTestConfig()); ok {
		t.Fatal("ordinary log line must not become an event")
	}
}

func TestGameTaskEventsDeriveCompletedCheckin(t *testing.T) {
	record := gameevents.Record{
		EventID: "chat-1", Type: "PLAYER_CHAT", PlayerUID: "player-1", Nickname: "Alice",
		OccurredAt: "2026-08-01T12:00:00Z", Payload: map[string]any{"message": "签到"},
	}
	events := gameTaskEvents(record, &economy.CommandResult{
		Handled: true, Command: "checkin", Awarded: true, LocalDate: "2026-08-01", Balance: 42,
	})
	if len(events) != 2 || events[1].Type != "CHECKIN_COMPLETED" || events[1].EventID != "chat-1:checkin" {
		t.Fatalf("unexpected derived events: %#v", events)
	}
	if events[1].Payload["count"] != 1 || events[1].Payload["local_date"] != "2026-08-01" {
		t.Fatalf("unexpected checkin payload: %#v", events[1].Payload)
	}
}

func TestGameTaskEventsDoNotDeriveDuplicateCheckin(t *testing.T) {
	record := gameevents.Record{EventID: "chat-2", Type: "PLAYER_CHAT", PlayerUID: "player-1", Payload: map[string]any{"message": "签到"}}
	events := gameTaskEvents(record, &economy.CommandResult{Handled: true, Command: "checkin", Awarded: false})
	if len(events) != 1 {
		t.Fatalf("duplicate or already-completed checkin must not advance tasks: %#v", events)
	}
}

func TestBridgeProcessingSummaryShowsTaskAndReplyOutcome(t *testing.T) {
	summary := bridgeProcessingSummary(map[string]any{
		"task_event_types": []string{"PLAYER_CHAT", "CHECKIN_COMPLETED"},
		"tasks":            []tasks.ProgressUpdate{{}},
		"reply_delivery":   "sent",
	})
	if !strings.Contains(summary, "CHECKIN_COMPLETED") || !strings.Contains(summary, "匹配任务 1") || !strings.Contains(summary, "回复 sent") {
		t.Fatalf("unexpected processing summary: %q", summary)
	}
}
