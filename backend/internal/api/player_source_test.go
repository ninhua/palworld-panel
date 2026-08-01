package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/docker"
	"palpanel/internal/mods"
	"palpanel/internal/monitor"
	"palpanel/internal/paldefender"
	"palpanel/internal/palrest"
	"palpanel/internal/scheduler"
	"palpanel/internal/server"
)

func TestPlayersSeparateHistoricalAndServerSources(t *testing.T) {
	root := t.TempDir()
	serverWorld := filepath.Join(root, "server", "Pal", "Saved", "SaveGames", "0", "server-world")
	importWorld := filepath.Join(root, "save-sources", "import-one")
	writePlayerSourceWorld(t, serverWorld)
	writePlayerSourceWorld(t, importWorld)

	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"oodle": true}})
			return
		}
		var request struct {
			SaveDir string `json:"save_dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		playerUID, level := "LIVE-UID", 62
		containers := []map[string]any{{"container_id": "live-container", "owner_type": "player", "owner_id": playerUID, "slots": []any{}}}
		if strings.Contains(request.SaveDir, "import-one") {
			playerUID, level = "HISTORY-UID", 41
			containers = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"version": 1, "generated_at": "2026-07-23T00:00:00Z", "parser": "test", "warnings": []string{},
				"players": []map[string]any{{"player_uid": playerUID, "nickname": "SameName", "level": level}},
				"guilds":  []any{}, "bases": []any{}, "pals": []any{}, "containers": containers, "map_entities": []any{},
			},
		})
	}))
	t.Cleanup(indexer.Close)

	var restCalls atomic.Int32
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		restCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"players": []map[string]any{{
			"name": "SameName", "playerId": "LIVE-UID", "userId": "steam-live",
		}}})
	}))
	t.Cleanup(rest.Close)

	cfg := appconfig.Config{
		DataDir: root, ServerDir: filepath.Join(root, "server"), WinePrefixDir: filepath.Join(root, "wine"),
		ToolsDir: filepath.Join(root, "tools"), SteamCMDDir: filepath.Join(root, "tools", "steamcmd"),
		UploadsDir: filepath.Join(root, "uploads"), BackupsDir: filepath.Join(root, "backups"), LogsDir: filepath.Join(root, "logs"),
		SaveSourcesDir: filepath.Join(root, "save-sources"), SaveIndexCacheDir: filepath.Join(root, "save-index"),
		DBPath: filepath.Join(root, "test.db"), SaveIndexerEnabled: true, SaveIndexerURL: indexer.URL, SaveIndexTimeoutSeconds: 5,
		PalworldRESTBaseURL: rest.URL + "/v1/api", PalworldRESTReadTimeoutMS: 500,
		DockerBinary: "docker", DockerImage: "test", DockerContainer: "test", GamePort: 8211, QueryPort: 27015, RESTPort: 8212,
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := t.Context()
	if err := store.UpsertSaveSource(ctx, db.SaveSource{ID: "server", Name: "Current server", Kind: "server"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSaveSource(ctx, db.SaveSource{ID: "import-one", Name: "Historical import", Kind: "import", Path: importWorld}); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateSaveSource(ctx, "import-one"); err != nil {
		t.Fatal(err)
	}

	runner := docker.NewRunner(cfg)
	serverManager := server.NewManager(cfg, store, runner)
	restClient := palrest.New(rest.URL+"/v1/api", "", "")
	router := NewRouter(
		cfg, store, serverManager, mods.NewManager(cfg, store, runner), paldefender.NewManager(cfg, store), restClient,
		monitor.New(cfg, store, serverManager, restClient), scheduler.New(store, serverManager, restClient),
	)

	rebuild := httptest.NewRecorder()
	rebuildRequest := httptest.NewRequest(http.MethodPost, "/api/save/index/rebuild", nil)
	router.ServeHTTP(rebuild, rebuildRequest)
	if rebuild.Code != http.StatusOK {
		t.Fatalf("rebuild active source: %d %s", rebuild.Code, rebuild.Body.String())
	}

	historical := getPlayerSourceResponse(t, router, "/api/players")
	assertPlayerSourceResponse(t, historical, "HISTORY-UID", 41, false, "active", "import-one", "import", "Historical import", false)
	if restCalls.Load() != 0 {
		t.Fatalf("historical player view called live REST %d times", restCalls.Load())
	}
	cleanupHistorical := httptest.NewRecorder()
	router.ServeHTTP(cleanupHistorical, httptest.NewRequest(http.MethodPost, "/api/bases/base-1/clean", nil))
	if cleanupHistorical.Code != http.StatusConflict || !strings.Contains(cleanupHistorical.Body.String(), "base_cleanup_server_only") {
		t.Fatalf("historical base cleanup: %d %s", cleanupHistorical.Code, cleanupHistorical.Body.String())
	}

	live := getPlayerSourceResponse(t, router, "/api/players?source=server")
	assertPlayerSourceResponse(t, live, "LIVE-UID", 62, true, "server", "server", "server", "Current server", true)
	if restCalls.Load() == 0 {
		t.Fatal("server player view did not call live REST")
	}

	detail := getPlayerSourceResponse(t, router, "/api/players/LIVE-UID?source=server")
	player, _ := detail["player"].(map[string]any)
	if player["player_uid"] != "LIVE-UID" || player["level"] != float64(62) || player["is_online"] != true {
		t.Fatalf("server player detail = %#v", player)
	}
	assertPlayerView(t, detail, "server", "server", "server", "Current server", true)

	inventory := getPlayerSourceResponse(t, router, "/api/players/LIVE-UID/inventory?source=server")
	containers, _ := inventory["containers"].([]any)
	if len(containers) != 1 {
		t.Fatalf("server inventory containers = %#v", inventory["containers"])
	}
	assertPlayerView(t, inventory, "server", "server", "server", "Current server", true)

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/players?source=import-one", nil))
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "player_source_invalid") {
		t.Fatalf("invalid source: %d %s", invalid.Code, invalid.Body.String())
	}
}

func writePlayerSourceWorld(t *testing.T, worldDir string) {
	t.Helper()
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "Level.sav"), []byte("level"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func getPlayerSourceResponse(t *testing.T, handler http.Handler, target string) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", target, recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func assertPlayerSourceResponse(t *testing.T, data map[string]any, playerUID string, level int, online bool, scope, sourceID, kind, name string, overlay bool) {
	t.Helper()
	players, _ := data["players"].([]any)
	if len(players) != 1 {
		t.Fatalf("players = %#v", data["players"])
	}
	player, _ := players[0].(map[string]any)
	if player["player_uid"] != playerUID || player["level"] != float64(level) || player["is_online"] != online {
		t.Fatalf("player = %#v", player)
	}
	assertPlayerView(t, data, scope, sourceID, kind, name, overlay)
}

func assertPlayerView(t *testing.T, data map[string]any, scope, sourceID, kind, name string, overlay bool) {
	t.Helper()
	view, _ := data["view"].(map[string]any)
	if view["scope"] != scope || view["source_id"] != sourceID || view["source_kind"] != kind || view["source_name"] != name || view["online_overlay"] != overlay {
		t.Fatalf("view = %#v", view)
	}
}
