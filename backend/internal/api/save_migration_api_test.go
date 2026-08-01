package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/docker"
	"palpanel/internal/mods"
	"palpanel/internal/monitor"
	"palpanel/internal/paldefender"
	"palpanel/internal/palrest"
	"palpanel/internal/playeruid"
	"palpanel/internal/scheduler"
	"palpanel/internal/server"
)

func TestSaveMigrationPreviewCalculatesNoSteamTargetUID(t *testing.T) {
	router, cleanup := newSaveMigrationAPITestRouter(t, true)
	defer cleanup()

	players := requestMigrationJSON(t, router, http.MethodGet, "/api/save-sources/import-one/migration/players", nil, http.StatusOK)
	items := players["players"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["nickname"] != "旧玩家" {
		t.Fatalf("migration players = %#v", players)
	}

	preview := requestMigrationJSON(t, router, http.MethodPost, "/api/save-migrations/preview", map[string]any{
		"source_id":   "import-one",
		"target_mode": "auto",
		"mappings":    []map[string]any{{"source_uid": "25527209-0000-0000-0000-000000000000", "steam_id": "76561198452436974"}},
	}, http.StatusOK)
	if preview["target_mode"] != "nosteam" || preview["mode_source"] != "server_index" || preview["requires_manual_confirmation"] != false || preview["ready"] != true {
		t.Fatalf("preview mode = %#v", preview)
	}
	rows := preview["mappings"].([]any)
	row := rows[0].(map[string]any)
	if row["target_uid"] != "f8f86740-0000-0000-0000-000000000000" || row["steam_uid"] != "25527209-0000-0000-0000-000000000000" {
		t.Fatalf("preview mapping = %#v", row)
	}
}

func TestSaveMigrationStartRejectsUnconfirmedManualMode(t *testing.T) {
	router, cleanup := newSaveMigrationAPITestRouter(t, false)
	defer cleanup()

	body := map[string]any{
		"source_id": "import-one", "target_mode": "nosteam", "confirmation": "MIGRATE PLAYERS",
		"mappings": []map[string]any{{"source_uid": "25527209-0000-0000-0000-000000000000", "steam_id": "76561198452436974"}},
	}
	rejected := requestMigrationRaw(t, router, http.MethodPost, "/api/save-migrations", body)
	if rejected.Code != http.StatusBadRequest || !strings.Contains(rejected.Body.String(), "manual_mode_confirmation_required") {
		t.Fatalf("unconfirmed manual migration = %d %s", rejected.Code, rejected.Body.String())
	}
	body["manual_mode_confirmation"] = "USE NOSTEAM UID"
	body["confirmation"] = "wrong"
	rejected = requestMigrationRaw(t, router, http.MethodPost, "/api/save-migrations", body)
	if rejected.Code != http.StatusBadRequest || !strings.Contains(rejected.Body.String(), "migration_confirmation_invalid") {
		t.Fatalf("invalid migration confirmation = %d %s", rejected.Code, rejected.Body.String())
	}
}

func TestSaveMigrationPreviewRejectsTargetUIDAlreadyOnServer(t *testing.T) {
	router, cleanup := newSaveMigrationAPITestRouter(t, true)
	defer cleanup()

	preview := requestMigrationJSON(t, router, http.MethodPost, "/api/save-migrations/preview", map[string]any{
		"source_id":   "import-one",
		"target_mode": "auto",
		"mappings": []map[string]any{{
			"source_uid": "25527209-0000-0000-0000-000000000000",
			"steam_id":   "76561199049872899",
		}},
	}, http.StatusOK)
	if preview["ready"] != false {
		t.Fatalf("preview should reject an existing target UID: %#v", preview)
	}
	conflicts := preview["conflicts"].([]any)
	if len(conflicts) != 1 || !strings.Contains(conflicts[0].(string), "already exists in the current server save") {
		t.Fatalf("target conflicts = %#v", conflicts)
	}
}

func newSaveMigrationAPITestRouter(t *testing.T, detectableMode bool) (http.Handler, func()) {
	t.Helper()
	root := t.TempDir()
	serverWorld := filepath.Join(root, "server", "Pal", "Saved", "SaveGames", "0", "server-world")
	importWorld := filepath.Join(root, "save-sources", "import-one")
	for _, path := range []string{serverWorld, importWorld} {
		if err := os.MkdirAll(filepath.Join(path, "Players"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "Level.sav"), []byte("level"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	detectionPair, err := playeruid.Calculate("76561199049872899")
	if err != nil {
		t.Fatal(err)
	}
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"oodle": true}})
			return
		}
		var request struct {
			SaveDir string `json:"save_dir"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		player := map[string]any{"player_uid": "25527209-0000-0000-0000-000000000000", "nickname": "旧玩家", "level": 55}
		if strings.Contains(request.SaveDir, "server-world") {
			player = map[string]any{"player_uid": detectionPair.NoSteam, "nickname": "当前玩家", "level": 1}
			if detectableMode {
				player["steam_id"] = "76561199049872899"
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{
			"version": 1, "generated_at": "2026-07-24T00:00:00Z", "parser": "test", "warnings": []string{},
			"players": []any{player}, "guilds": []any{}, "bases": []any{}, "pals": []any{}, "containers": []any{}, "map_entities": []any{},
		}})
	}))

	cfg := appconfig.Config{
		DataDir: root, ServerDir: filepath.Join(root, "server"), WinePrefixDir: filepath.Join(root, "wine"), ToolsDir: filepath.Join(root, "tools"),
		SteamCMDDir: filepath.Join(root, "steamcmd"), UploadsDir: filepath.Join(root, "uploads"), BackupsDir: filepath.Join(root, "backups"), LogsDir: filepath.Join(root, "logs"),
		SaveSourcesDir: filepath.Join(root, "save-sources"), SaveIndexCacheDir: filepath.Join(root, "save-index"), DBPath: filepath.Join(root, "test.db"),
		SaveIndexerEnabled: true, SaveIndexerURL: indexer.URL, SaveIndexTimeoutSeconds: 5, DockerBinary: "docker", DockerImage: "test", DockerContainer: "test",
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSaveSource(t.Context(), db.SaveSource{ID: "server", Name: "当前服务器", Kind: "server"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSaveSource(t.Context(), db.SaveSource{ID: "import-one", Name: "旧世界", Kind: "import", Path: importWorld}); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateSaveSource(t.Context(), "import-one"); err != nil {
		t.Fatal(err)
	}
	runner := docker.NewRunner(cfg)
	serverManager := server.NewManager(cfg, store, runner)
	rest := palrest.New("http://127.0.0.1:1/v1/api", "", "")
	router := NewRouter(cfg, store, serverManager, mods.NewManager(cfg, store, runner), paldefender.NewManager(cfg, store), rest, monitor.New(cfg, store, serverManager, rest), scheduler.New(store, serverManager, rest))
	return router, func() { indexer.Close(); _ = store.Close() }
}

func requestMigrationRaw(t *testing.T, handler http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	return recorder
}

func requestMigrationJSON(t *testing.T, handler http.Handler, method, target string, body any, status int) map[string]any {
	t.Helper()
	recorder := requestMigrationRaw(t, handler, method, target, body)
	if recorder.Code != status {
		t.Fatalf("%s %s = %d %s", method, target, recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
