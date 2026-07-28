package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/palrest"
	"palpanel/internal/playerpresence"
)

func TestObservePlayerPresencePersistsRESTPlayers(t *testing.T) {
	serverDir := testMonitorPlayerPresenceServerDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/players" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"players":[{"playerId":"UID-1","userId":"7656","name":"Alice"}]}`))
	}))
	defer server.Close()

	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	manager := Manager{cfg: appconfig.Config{ServerDir: serverDir}, store: store, now: func() time.Time { return now }}
	client := palrest.New(server.URL, "", "")
	if err := manager.observePlayerPresence(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	scope, err := playerpresence.ResolveServerScope(serverDir)
	if err != nil {
		t.Fatal(err)
	}
	state, err := playerpresence.LoadScoped(context.Background(), store, scope)
	if err != nil {
		t.Fatal(err)
	}
	record, found := playerpresence.Find(state, "7656")
	if !found || !record.Online || record.Nickname != "Alice" {
		t.Fatalf("record = %#v found=%v", record, found)
	}
}

func testMonitorPlayerPresenceServerDir(t *testing.T) string {
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
