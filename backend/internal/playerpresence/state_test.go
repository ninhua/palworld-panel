package playerpresence

import (
	"testing"
	"time"
)

func TestAdvanceTracksSessionsAndOfflineTransitions(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	state := Advance(EmptyState(), start, []OnlinePlayer{{PlayerUID: "UID-1", SteamID: "7656", Nickname: "Alice"}})
	record, found := Find(state, "uid1")
	if !found || !record.Online || record.SessionSeconds != 0 || record.TotalSeconds != 0 {
		t.Fatalf("initial record = %#v found=%v", record, found)
	}

	state = Advance(state, start.Add(15*time.Second), []OnlinePlayer{{PlayerUID: "UID-1", SteamID: "7656", Nickname: "Alice"}})
	record, _ = Find(state, "7656")
	if record.SessionSeconds != 15 || record.TotalSeconds != 15 || !record.Online {
		t.Fatalf("continued record = %#v", record)
	}

	state = Advance(state, start.Add(30*time.Second), nil)
	record, _ = Find(state, "UID-1")
	if record.Online || record.SessionSeconds != 30 || record.TotalSeconds != 30 || record.LastOfflineAt == "" || len(record.Sessions) != 1 || record.Sessions[0].DurationSeconds != 30 {
		t.Fatalf("offline record = %#v", record)
	}

	state = Advance(state, start.Add(45*time.Second), []OnlinePlayer{{SteamID: "7656", Nickname: "Alice"}})
	record, _ = Find(state, "UID-1")
	if !record.Online || record.SessionSeconds != 0 || record.TotalSeconds != 30 || record.LastOnlineAt == "" {
		t.Fatalf("new session record = %#v", record)
	}
}

func TestAdvanceCapsLongObservationGaps(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	state := Advance(EmptyState(), start, []OnlinePlayer{{PlayerUID: "uid-1"}})
	state = Advance(state, start.Add(10*time.Minute), []OnlinePlayer{{PlayerUID: "uid-1"}})
	record, _ := Find(state, "uid-1")
	if record.TotalSeconds != int64(MaxSampleGap/time.Second) {
		t.Fatalf("long gap total = %d", record.TotalSeconds)
	}
}

func TestAdvanceMergesPlayerAliases(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	state := Advance(EmptyState(), start, []OnlinePlayer{{PlayerUID: "ABC-DEF", SteamID: "7656", Nickname: "Alice"}})
	state = Advance(state, start.Add(15*time.Second), []OnlinePlayer{{SteamID: "7656", Nickname: "Alice 2"}})
	if len(state.Players) != 1 {
		t.Fatalf("players = %#v", state.Players)
	}
	record, found := Find(state, "abcdef")
	if !found || record.Nickname != "Alice 2" || record.TotalSeconds != 15 {
		t.Fatalf("merged record = %#v found=%v", record, found)
	}
}

func TestParseRESTPlayers(t *testing.T) {
	players := ParseRESTPlayers(map[string]any{
		"players": []any{
			map[string]any{"playerId": "UID-1", "userId": "7656", "name": "Alice"},
			map[string]any{"playerId": "", "userId": "", "name": "invalid"},
		},
	})
	if len(players) != 1 || players[0].PlayerUID != "UID-1" || players[0].SteamID != "7656" || players[0].Nickname != "Alice" {
		t.Fatalf("players = %#v", players)
	}
}
