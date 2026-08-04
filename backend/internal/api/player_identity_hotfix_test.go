package api

import (
	"testing"

	"palpanel/internal/economy"
)

func TestPalDefenderLoginDoesNotUseIPAddressAsNickname(t *testing.T) {
	line := `[18:07:00][info] steam_76561199032061430 ('39.166.34.148') connected to the server.`
	event, ok := parsePalDefenderLogLine(line, economy.DetailedConfig{})
	if !ok {
		t.Fatal("login line was not parsed")
	}
	if event.Type != "PLAYER_LOGIN" {
		t.Fatalf("type = %q", event.Type)
	}
	if event.PlayerHint != "steam_76561199032061430" {
		t.Fatalf("hint = %q", event.PlayerHint)
	}
	if event.Nickname != "" {
		t.Fatalf("IP address leaked into nickname: %q", event.Nickname)
	}
}
