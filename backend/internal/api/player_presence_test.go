package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/playerpresence"
)

func TestAttachPlayerPresenceAddsDurationsAndHistory(t *testing.T) {
	serverDir := testPlayerPresenceServerDir(t)
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	state := playerpresence.Advance(playerpresence.EmptyState(), now.Add(-30*time.Second), []playerpresence.OnlinePlayer{{PlayerUID: "UID-1", SteamID: "7656", Nickname: "Alice"}})
	state = playerpresence.Advance(state, now.Add(-15*time.Second), nil)
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetKV(context.Background(), playerpresence.StorageKey, string(raw)); err != nil {
		t.Fatal(err)
	}

	views := []gin.H{{"player_uid": "UID-1", "steam_id": "7656"}}
	Server{cfg: appconfig.Config{ServerDir: serverDir}, store: store}.attachPlayerPresence(context.Background(), views, true)
	if views[0]["total_seconds"] != int64(15) || views[0]["presence_available"] != true {
		t.Fatalf("view = %#v", views[0])
	}
	sessions, ok := views[0]["presence_sessions"].([]playerpresence.Session)
	if !ok || len(sessions) != 1 || sessions[0].DurationSeconds != 15 {
		t.Fatalf("sessions = %#v", views[0]["presence_sessions"])
	}
}

func TestAttachPlayerPresenceDoesNotExposeUnobservedOrImportedPlayers(t *testing.T) {
	serverDir := testPlayerPresenceServerDir(t)
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	state := playerpresence.Advance(playerpresence.EmptyState(), now, []playerpresence.OnlinePlayer{{PlayerUID: "UID-1"}})
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetKV(context.Background(), playerpresence.StorageKey, string(raw)); err != nil {
		t.Fatal(err)
	}

	unobserved := []gin.H{{"player_uid": "UID-2"}}
	server := Server{cfg: appconfig.Config{ServerDir: serverDir}, store: store}
	server.attachPlayerPresence(context.Background(), unobserved, true)
	if unobserved[0]["presence_available"] != false {
		t.Fatalf("unobserved view = %#v", unobserved[0])
	}

	imported := []gin.H{{"player_uid": "UID-1"}}
	server.attachPlayerPresence(context.Background(), imported, false)
	if imported[0]["presence_available"] != false || imported[0]["presence_stale"] != false {
		t.Fatalf("imported view = %#v", imported[0])
	}
}

func testPlayerPresenceServerDir(t *testing.T) string {
	t.Helper()
	serverDir := t.TempDir()
	worldDir := filepath.Join(serverDir, "Pal", "Saved", "SaveGames", "0", "TEST-WORLD")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "Level.sav"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	return serverDir
}
