package api

import (
	"testing"

	"palpanel/internal/economy"
)

func TestBridgeParserRepairV2LoginDoesNotExposeIP(t *testing.T) {
	line := `[19:05:35][info] steam_76561199032061430 ('39.166.34.148') connected to the server.`
	parsed, ok := parsePalDefenderLogLine(line, economy.DetailedConfig{})
	if !ok {
		t.Fatal("login line was not parsed")
	}
	if parsed.Type != "PLAYER_LOGIN" {
		t.Fatalf("type=%q", parsed.Type)
	}
	if parsed.PlayerHint != "steam_76561199032061430" {
		t.Fatalf("hint=%q", parsed.PlayerHint)
	}
	if parsed.Nickname != "" {
		t.Fatalf("nickname leaked from IP descriptor: %q", parsed.Nickname)
	}
}

func TestBridgeParserRepairV2AcceptsUIDDescriptor(t *testing.T) {
	nickname, hint := parseBridgeIdentity(`'tiantian' (UserId=steam_76561199032061430, IP=127.0.0.1, UID=F23D556C-00000000-00000000-00000000)`)
	if hint != "steam_76561199032061430" {
		t.Fatalf("hint=%q", hint)
	}
	if nickname == "" {
		t.Fatal("nickname should be preserved")
	}
}
