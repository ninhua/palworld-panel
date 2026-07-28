package playerpresence

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"palpanel/internal/db"
)

func openPresenceStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func writeWorld(t *testing.T, serverDir, id string, modified time.Time) {
	t.Helper()
	path := filepath.Join(serverDir, "Pal", "Saved", "SaveGames", "0", id)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	level := filepath.Join(path, "Level.sav")
	if err := os.WriteFile(level, []byte(id), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(level, modified, modified); err != nil {
		t.Fatal(err)
	}
}

func writeDedicatedServerName(t *testing.T, serverDir, id string) {
	t.Helper()
	path := filepath.Join(serverDir, "Pal", "Saved", "Config", "WindowsServer", "GameUserSettings.ini")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "[/Script/Pal.PalGameLocalSettings]\r\nDedicatedServerName=" + id + "\r\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveServerScopeUsesDedicatedServerName(t *testing.T) {
	serverDir := t.TempDir()
	now := time.Date(2026, 7, 26, 1, 0, 0, 0, time.UTC)
	writeWorld(t, serverDir, "configured-world", now.Add(-time.Hour))
	writeWorld(t, serverDir, "newer-but-inactive", now)
	writeDedicatedServerName(t, serverDir, "configured-world")
	scope, err := ResolveServerScope(serverDir)
	if err != nil {
		t.Fatal(err)
	}
	if scope.WorldID != "configured-world" || scope.AllowLegacyMigration {
		t.Fatalf("scope=%#v", scope)
	}
}

func TestResolveServerScopeFallsBackToNewestWorld(t *testing.T) {
	serverDir := t.TempDir()
	now := time.Date(2026, 7, 26, 1, 0, 0, 0, time.UTC)
	writeWorld(t, serverDir, "old-world", now.Add(-time.Hour))
	writeWorld(t, serverDir, "new-world", now)
	scope, err := ResolveServerScope(serverDir)
	if err != nil {
		t.Fatal(err)
	}
	if scope.WorldID != "new-world" || scope.AllowLegacyMigration {
		t.Fatalf("scope=%#v", scope)
	}
}

func TestScopedPresenceDoesNotCarryAcrossWorlds(t *testing.T) {
	store := openPresenceStore(t)
	ctx := context.Background()
	worldA := Scope{ID: "server-world:a", WorldID: "a"}
	worldB := Scope{ID: "server-world:b", WorldID: "b"}
	now := time.Date(2026, 7, 26, 2, 0, 0, 0, time.UTC)
	alice := OnlinePlayer{PlayerUID: "alice", Nickname: "Alice"}

	if _, err := ObserveScoped(ctx, store, worldA, now, []OnlinePlayer{alice}); err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveScoped(ctx, store, worldA, now.Add(30*time.Second), []OnlinePlayer{alice}); err != nil {
		t.Fatal(err)
	}
	stateA, err := LoadScoped(ctx, store, worldA)
	if err != nil {
		t.Fatal(err)
	}
	record, found := Find(stateA, "alice")
	if !found || record.TotalSeconds != 30 {
		t.Fatalf("world-a record=%#v found=%v", record, found)
	}
	stateB, err := LoadScoped(ctx, store, worldB)
	if err != nil {
		t.Fatal(err)
	}
	if len(stateB.Players) != 0 || stateB.ObservedAt != "" {
		t.Fatalf("world-b inherited state: %#v", stateB)
	}
}

func TestSingleWorldCanMigrateLegacyState(t *testing.T) {
	store := openPresenceStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 26, 3, 0, 0, 0, time.UTC)
	if _, err := Observe(ctx, store, now, []OnlinePlayer{{SteamID: "1", Nickname: "Legacy"}}); err != nil {
		t.Fatal(err)
	}
	scope := Scope{ID: "server-world:only", WorldID: "only", AllowLegacyMigration: true}
	state, err := LoadScoped(ctx, store, scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := Find(state, "1"); !found || state.ScopeID != scope.ID {
		t.Fatalf("migrated state=%#v", state)
	}
}
