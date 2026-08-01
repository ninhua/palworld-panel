package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"palpanel/internal/appconfig"
	panelauth "palpanel/internal/auth"
	"palpanel/internal/db"
	"palpanel/internal/docker"
	"palpanel/internal/mods"
)

func TestModConfigurationHandlersListReadAndWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	cfg := appconfig.Config{
		RuntimeRoot: root, DataDir: filepath.Join(root, "data"), ServerDir: filepath.Join(root, "server"),
		UploadsDir: filepath.Join(root, "uploads"), BackupsDir: filepath.Join(root, "backups"),
		DBPath: filepath.Join(root, "test.db"),
	}
	modRoot := filepath.Join(cfg.ServerDir, "Mods", "Workshop", "mod_one")
	if err := os.MkdirAll(modRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modRoot, "Config.json"), []byte(`{"Enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertMod(t.Context(), db.Mod{ID: "mod_one", Name: "One", PackageName: "One", Path: modRoot}); err != nil {
		t.Fatal(err)
	}
	server := Server{cfg: cfg, store: store, mods: mods.NewManager(cfg, store, docker.NewRunner(cfg))}
	router := gin.New()
	router.GET("/api/mods/:id/files", server.listModConfigFiles)
	router.GET("/api/mods/:id/files/:file", server.getModConfigFile)
	router.PUT("/api/mods/:id/files/:file", server.updateModConfigFile)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/mods/mod_one/files", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", response.Code, response.Body.String())
	}
	var list struct {
		Data []mods.ConfigFile `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil || len(list.Data) != 1 {
		t.Fatalf("list response = %s, %v", response.Body.String(), err)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/mods/mod_one/files/"+list.Data[0].ID, nil))
	var document struct {
		Data mods.ConfigDocument `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"content":"{\"Enabled\":false}","revision":"` + document.Data.File.Revision + `"}`)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/mods/mod_one/files/"+list.Data[0].ID, body))
	if response.Code != http.StatusOK {
		t.Fatalf("write status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil || document.Data.Content != `{"Enabled":false}` {
		t.Fatalf("write response = %s, %v", response.Body.String(), err)
	}
}

func TestModConfigurationRoutesEnforceActualPermissions(t *testing.T) {
	for _, test := range []struct {
		name       string
		role       Role
		wantUpdate int
		wantAction int
	}{
		{name: "viewer", role: RoleViewer, wantUpdate: http.StatusForbidden, wantAction: http.StatusForbidden},
		{name: "operator", role: RoleOperator, wantUpdate: http.StatusOK, wantAction: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, file := newModConfigurationPermissionRouter(t, test.role)
			request := httptest.NewRequest(http.MethodGet, "/api/mods/mod_permission/files/"+file.ID, nil)
			authorizeTestRequest(request)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET status = %d, body = %s", response.Code, response.Body.String())
			}

			body := bytes.NewBufferString(`{"content":"{\"Enabled\":false}","revision":"` + file.Revision + `"}`)
			request = httptest.NewRequest(http.MethodPut, "/api/mods/mod_permission/files/"+file.ID, body)
			request.Header.Set("Content-Type", "application/json")
			authorizeTestRequest(request)
			response = httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantUpdate {
				t.Fatalf("PUT status = %d, want %d: %s", response.Code, test.wantUpdate, response.Body.String())
			}

			body = bytes.NewBufferString(`{"action":"initialize"}`)
			request = httptest.NewRequest(http.MethodPost, "/api/mods/configurations/palzones/actions", body)
			request.Header.Set("Content-Type", "application/json")
			authorizeTestRequest(request)
			response = httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantAction {
				t.Fatalf("POST action status = %d, want %d: %s", response.Code, test.wantAction, response.Body.String())
			}
		})
	}
}

func newModConfigurationPermissionRouter(t *testing.T, role Role) (*gin.Engine, mods.ConfigFile) {
	t.Helper()
	root := t.TempDir()
	cfg := appconfig.Config{
		RuntimeRoot: root, DataDir: filepath.Join(root, "data"), ServerDir: filepath.Join(root, "server"),
		UploadsDir: filepath.Join(root, "uploads"), BackupsDir: filepath.Join(root, "backups"),
		DBPath: filepath.Join(root, "test.db"), RequireAuth: true,
	}
	modRoot := filepath.Join(cfg.ServerDir, "Mods", "Workshop", "mod_permission")
	if err := os.MkdirAll(modRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modRoot, "Config.json"), []byte(`{"Enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.Win64Dir(), "UE4SS", "Mods", "PalZones"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	provisionTestPrincipal(t, store, role)
	if err := store.UpsertMod(t.Context(), db.Mod{ID: "mod_permission", Name: "Permission", PackageName: "Permission", Path: modRoot}); err != nil {
		t.Fatal(err)
	}
	manager := mods.NewManager(cfg, store, docker.NewRunner(cfg))
	files, err := manager.ListModConfigFiles(t.Context(), "mod_permission")
	if err != nil || len(files) != 1 {
		t.Fatalf("files = %#v, %v", files, err)
	}
	server := Server{cfg: cfg, store: store, mods: manager, auth: panelauth.New(store)}
	router := gin.New()
	api := router.Group("/api")
	api.Use(Auth(cfg, server.auth), AuditMiddleware(store))
	server.registerModConfigurationRoutes(api)
	return router, files[0]
}
