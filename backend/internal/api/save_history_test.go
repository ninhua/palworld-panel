package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/saveindex"
)

func TestSaveHistoryAPIListsSanitizedSnapshotsAndDiffs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows: save indexer sidecar holds db file lock preventing temp dir cleanup")
	}
	root := t.TempDir()
	worldDir := filepath.Join(root, "server", "Pal", "Saved", "SaveGames", "0", "WORLD")
	if err := os.MkdirAll(filepath.Join(worldDir, "Players"), 0o755); err != nil {
		t.Fatal(err)
	}
	levelPath := filepath.Join(worldDir, "Level.sav")
	if err := os.WriteFile(levelPath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	var generation atomic.Int32
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"oodle": true}})
			return
		}
		level := 10
		containers := []map[string]any{{"container_id": "box", "owner_type": "player", "owner_id": "uid", "slots": []map[string]any{{"slot": 0, "item_id": "Wood", "count": 10}}}}
		generatedAt := "2026-07-29T10:00:00Z"
		if generation.Load() > 0 {
			level = 12
			generatedAt = "2026-07-29T10:05:00Z"
			containers = []map[string]any{{"container_id": "box", "owner_type": "player", "owner_id": "uid", "slots": []map[string]any{{"slot": 0, "item_id": "Wood", "count": 7}, {"slot": 1, "item_id": "Stone", "count": 4}}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"version": 1, "generated_at": generatedAt, "parser": "test", "warnings": []string{},
				"players": []map[string]any{{"player_uid": "uid", "steam_id": "steam_1", "nickname": "Alice", "level": level, "ip": "10.0.0.8"}},
				"guilds":  []any{}, "bases": []any{}, "pals": []any{}, "containers": containers, "map_entities": []any{},
			},
		})
	}))
	t.Cleanup(indexer.Close)

	cfg := appconfig.Config{
		DataDir: root, ServerDir: filepath.Join(root, "server"), LogsDir: filepath.Join(root, "logs"),
		BackupsDir: filepath.Join(root, "backups"), SaveSourcesDir: filepath.Join(root, "save-sources"),
		SaveIndexCacheDir: filepath.Join(root, "save-index"), DBPath: filepath.Join(root, "test.db"),
		SaveIndexerEnabled: true, SaveIndexerURL: indexer.URL, SaveIndexTimeoutSeconds: 5,
	}.WithServerDirectoryState()
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.UpsertSaveSource(ctx, db.SaveSource{ID: "server", Name: "Current server", Kind: "server"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateSaveSource(ctx, "server"); err != nil {
		t.Fatal(err)
	}
	manager := saveindex.NewManager(cfg)
	if _, _, err := manager.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	generation.Store(1)
	if err := os.WriteFile(levelPath, []byte("second-version"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	// Automatic rebuilds intentionally skip dense history snapshots. This API
	// contract test needs two deterministic baselines, so explicitly capture the
	// current cached index rather than weakening the production sampling policy.
	if err := manager.ForceHistorySnapshot(); err != nil {
		t.Fatal(err)
	}

	server := Server{cfg: cfg, store: store, saveIndex: manager}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/save/history", server.listSaveHistory)
	router.GET("/api/save/history/diff", server.diffSaveHistory)

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/save/history", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("history list: %d %s", list.Code, list.Body.String())
	}
	if strings.Contains(list.Body.String(), "archive_sha256") || strings.Contains(list.Body.String(), worldDir) || strings.Contains(list.Body.String(), "10.0.0.8") {
		t.Fatalf("history response exposed private data: %s", list.Body.String())
	}
	var response struct {
		Data struct {
			Source struct{ ID, Name, Kind string }
			Items  []struct{ ID string }
		} `json:"data"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Items) != 2 || response.Data.Source.ID != "server" {
		t.Fatalf("unexpected history response: %#v", response)
	}
	fromID := response.Data.Items[1].ID
	toID := response.Data.Items[0].ID
	diff := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/save/history/diff?from="+fromID+"&to="+toID+"&category=items&q=stone", nil)
	router.ServeHTTP(diff, request)
	if diff.Code != http.StatusOK || !strings.Contains(diff.Body.String(), `"id":"Stone"`) || !strings.Contains(diff.Body.String(), `"delta":4`) {
		t.Fatalf("history diff: %d %s", diff.Code, diff.Body.String())
	}

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/save/history/diff?from="+fromID+"&to="+fromID, nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("same snapshot status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	archives, err := filepath.Glob(filepath.Join(cfg.SaveIndexCacheDir, "history", "*", "*.json.gz"))
	if err != nil || len(archives) != 2 {
		t.Fatalf("history archives=%#v err=%v", archives, err)
	}
	file, err := os.OpenFile(archives[0], os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("tampered"))
	_ = file.Close()
	corrupt := httptest.NewRecorder()
	router.ServeHTTP(corrupt, request)
	if corrupt.Code != http.StatusConflict || !strings.Contains(corrupt.Body.String(), `"code":"save_history_snapshot_corrupt"`) || strings.Contains(corrupt.Body.String(), root) {
		t.Fatalf("corrupt history response: %d %s", corrupt.Code, corrupt.Body.String())
	}
}

func TestSaveHistoryChangesAlwaysReturnsJSONArrays(t *testing.T) {
	if changes := saveHistoryChanges(nil); changes == nil {
		t.Fatal("nil changes must normalize to an empty slice")
	}
	changes := saveHistoryChanges([]saveindex.HistoryChange{{ID: "player-1", Fields: nil}})
	if len(changes) != 1 || changes[0].Fields == nil {
		t.Fatalf("nil fields were not normalized: %#v", changes)
	}
	body, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `[{"category":"","kind":"","id":"player-1","label":"","fields":[]}]` {
		t.Fatalf("unexpected JSON: %s", body)
	}
}
