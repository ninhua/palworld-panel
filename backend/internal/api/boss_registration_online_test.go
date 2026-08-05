package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"palpanel/internal/boss"
)

func TestParseBossBridgeConfig(t *testing.T) {
	values := parseBossBridgeConfig([]byte("# comment\r\nlisten = 127.0.0.1\nport=18083\ntoken=\"secret-token\"\n"))
	if values["listen"] != "127.0.0.1" || values["port"] != "18083" || values["token"] != "secret-token" {
		t.Fatalf("unexpected bridge config: %#v", values)
	}
}

func TestParseBossRegistrationChatCommand(t *testing.T) {
	tests := []struct {
		message   string
		prefix    string
		allowBare bool
		command   string
		selector  string
		handled   bool
	}{
		{message: "!Boss列表", prefix: "!", command: "list", handled: true},
		{message: "Boss报名 abcd1234", prefix: "!", allowBare: true, command: "register", selector: "abcd1234", handled: true},
		{message: "Boss签到", command: "check", handled: true},
		{message: "Boss报名状态", command: "status", handled: true},
		{message: "Boss取消 arena", command: "cancel", selector: "arena", handled: true},
		{message: "Boss很强", command: "", handled: false},
		{message: "Boss列表", prefix: "!", allowBare: false, command: "", handled: false},
	}
	for _, test := range tests {
		command, selector, handled := parseBossRegistrationChatCommand(test.message, test.prefix, test.allowBare)
		if command != test.command || selector != test.selector || handled != test.handled {
			t.Fatalf("%q => command=%q selector=%q handled=%t", test.message, command, selector, handled)
		}
	}
}

func TestMatchBossBridgePlayerLocation(t *testing.T) {
	players := []bossBridgePlayer{{
		PlayerUID:           "F23D556C-00000000-00000000-00000000",
		AccountName:         "tiantian",
		CachedLocationFound: true,
		CachedLocation:      &boss.Location{X: 100, Y: -200, Z: 300},
	}}
	lookup, err := matchBossBridgePlayerLocation(players, "2026-08-05T00:00:00Z", "F23D556C000000000000000000000000", "", "")
	if err != nil || !lookup.Found || lookup.MatchedBy != "player_uid" || lookup.Location == nil || lookup.Location.Y != -200 {
		t.Fatalf("unexpected lookup: %#v, err=%v", lookup, err)
	}
}

func TestMatchBossBridgePlayerLocationRejectsAmbiguousNickname(t *testing.T) {
	players := []bossBridgePlayer{
		{AccountName: "same", CachedLocationFound: true, CachedLocation: &boss.Location{X: 1}},
		{AccountName: "same", CachedLocationFound: true, CachedLocation: &boss.Location{X: 2}},
	}
	lookup, err := matchBossBridgePlayerLocation(players, "", "", "", "same")
	if err == nil || lookup.Found || !strings.Contains(lookup.Error, "requested online player") {
		t.Fatalf("ambiguous nickname must not match: %#v, err=%v", lookup, err)
	}
}

func TestQueryBossBridgeOnlinePlayers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/players/online", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer bridge-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"job_id": "players_test_1"})
	})
	mux.HandleFunc("/v1/jobs/players_test_1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"job": map[string]any{
			"id": "players_test_1", "status": "completed", "result": map[string]any{"players": []map[string]any{{
				"player_uid": "F23D556C000000000000000000000000", "account_name": "tiantian",
				"cached_location_found": true, "cached_location": map[string]any{"x": 11, "y": 22, "z": 33},
			}}},
		}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("PALPANEL_BRIDGE_URL", server.URL)
	t.Setenv("PALPANEL_BRIDGE_TOKEN", "bridge-secret")

	players, _, err := (Server{}).queryBossBridgeOnlinePlayers(context.Background())
	if err != nil || len(players) != 1 || players[0].CachedLocation == nil || players[0].CachedLocation.Z != 33 {
		t.Fatalf("unexpected players: %#v, err=%v", players, err)
	}
}

func TestValidBossLocation(t *testing.T) {
	if !validBossLocation(boss.Location{X: 1, Y: 2, Z: 3}) {
		t.Fatal("finite location should be valid")
	}
	if validBossLocation(boss.Location{X: 20_000_000}) {
		t.Fatal("out-of-range location should be invalid")
	}
}
