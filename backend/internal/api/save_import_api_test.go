package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
)

func TestSaveSourceInspectSelectAndImportFlow(t *testing.T) {
	server, router, store := newSaveImportTestServer(t)
	archive := writeSaveImportArchive(t, map[string]string{
		"world-b/Level.sav": "world-b",
		"empty/Level.sav":   "",
		"world-a/Level.sav": "world-a",
	})

	response := performSaveImportMultipart(t, router, "/api/save-sources/import/inspect", archive, "Selected world")
	if response.Code != http.StatusOK {
		t.Fatalf("inspect response = %d: %s", response.Code, response.Body.String())
	}
	inspection := decodeSaveImportInspection(t, response.Body.Bytes())
	if !inspection.RequiresSelection || inspection.SelectedCandidateID != "" || len(inspection.Candidates) != 3 {
		t.Fatalf("inspection = %#v", inspection)
	}
	if !strings.Contains(response.Body.String(), `"selected_candidate_id":""`) {
		t.Fatalf("inspection omitted required selected_candidate_id: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), server.cfg.SaveSourcesDir) {
		t.Fatalf("inspection response leaked a private path: %s", response.Body.String())
	}

	invalidID := inspection.Candidates[0].ID
	validID := ""
	for _, candidate := range inspection.Candidates {
		if candidate.Valid && validID == "" {
			validID = candidate.ID
		}
		if !candidate.Valid {
			invalidID = candidate.ID
		}
	}
	response = performSaveImportJSON(router, "/api/save-sources/import/inspect/"+inspection.ID+"/select", map[string]any{"candidate_id": invalidID})
	if response.Code != http.StatusConflict {
		t.Fatalf("invalid select response = %d: %s", response.Code, response.Body.String())
	}
	response = performSaveImportJSON(router, "/api/save-sources/import", map[string]any{"inspection_id": inspection.ID})
	if response.Code != http.StatusConflict {
		t.Fatalf("unselected import response = %d: %s", response.Code, response.Body.String())
	}
	if items, err := store.ListSaveSources(t.Context()); err != nil || len(items) != 0 {
		t.Fatalf("invalid selection imported a source: %#v, %v", items, err)
	}

	response = performSaveImportJSON(router, "/api/save-sources/import/inspect/"+inspection.ID+"/select", map[string]any{"candidate_id": validID})
	if response.Code != http.StatusOK {
		t.Fatalf("valid select response = %d: %s", response.Code, response.Body.String())
	}
	selected := decodeSaveImportInspection(t, response.Body.Bytes())
	if selected.RequiresSelection || selected.SelectedCandidateID != validID {
		t.Fatalf("selected inspection = %#v", selected)
	}
	response = performSaveImportJSON(router, "/api/save-sources/import", map[string]any{"inspection_id": inspection.ID, "name": "Selected world"})
	if response.Code != http.StatusOK {
		t.Fatalf("import response = %d: %s", response.Code, response.Body.String())
	}
	var imported struct {
		Data db.SaveSource `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.Data.Name != "Selected world" || !pathWithin(server.cfg.SaveSourcesDir, imported.Data.Path) {
		t.Fatalf("imported source = %#v", imported.Data)
	}
	if _, err := os.Stat(filepath.Join(imported.Data.Path, "Level.sav")); err != nil {
		t.Fatalf("imported Level.sav missing: %v", err)
	}
	if imported.Data.Path != filepath.Join(server.cfg.SaveSourcesDir, imported.Data.ID) {
		t.Fatalf("imported source retained archive wrapper path: %s", imported.Data.Path)
	}
	if _, err := os.Stat(filepath.Join(imported.Data.Path, "world-b", "Level.sav")); !os.IsNotExist(err) {
		t.Fatalf("unselected world was retained: %v", err)
	}
	response = performSaveImportJSON(router, "/api/save-sources/import", map[string]any{"inspection_id": inspection.ID})
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate import response = %d: %s", response.Code, response.Body.String())
	}
}

func TestDirectSaveSourceImportPreservesSingleWorldAndRejectsMultiple(t *testing.T) {
	_, router, store := newSaveImportTestServer(t)
	single := writeSaveImportArchive(t, map[string]string{"single/Level.sav": "single"})
	response := performSaveImportMultipart(t, router, "/api/save-sources/import", single, "Direct single")
	if response.Code != http.StatusOK {
		t.Fatalf("single direct import = %d: %s", response.Code, response.Body.String())
	}

	multiple := writeSaveImportArchive(t, map[string]string{
		"one/Level.sav": "one",
		"two/Level.sav": "two",
	})
	response = performSaveImportMultipart(t, router, "/api/save-sources/import", multiple, "Direct multiple")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"save_world_selection_required"`) || !strings.Contains(response.Body.String(), `"inspection_id":"inspect_`) {
		t.Fatalf("multiple direct import = %d: %s", response.Code, response.Body.String())
	}
	var conflict struct {
		Error struct {
			InspectionID string                `json:"inspection_id"`
			Candidates   []saveImportCandidate `json:"candidates"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &conflict); err != nil || conflict.Error.InspectionID == "" || len(conflict.Error.Candidates) != 2 {
		t.Fatalf("selection conflict is not actionable: %#v, %v", conflict, err)
	}
	response = performSaveImportJSON(router, "/api/save-sources/import/inspect/"+conflict.Error.InspectionID+"/select", map[string]any{"candidate_id": conflict.Error.Candidates[0].ID})
	if response.Code != http.StatusOK {
		t.Fatalf("direct conflict selection = %d: %s", response.Code, response.Body.String())
	}
	response = performSaveImportJSON(router, "/api/save-sources/import", map[string]any{"inspection_id": conflict.Error.InspectionID})
	if response.Code != http.StatusOK {
		t.Fatalf("direct conflict continuation = %d: %s", response.Code, response.Body.String())
	}
	items, err := store.ListSaveSources(t.Context())
	if err != nil || len(items) != 2 {
		t.Fatalf("save sources = %#v, %v", items, err)
	}
}

func TestSaveSourceImportRejectsUnsafeClaimedCandidatePath(t *testing.T) {
	server, router, store := newSaveImportTestServer(t)
	root := t.TempDir()
	server.saveImports.put(&saveImportInspection{
		ID:                  "inspect_unsafe",
		Root:                root,
		FileName:            "unsafe.zip",
		SelectedCandidateID: "unsafe",
		Candidates: []saveImportCandidate{{
			ID: "unsafe", RelativePath: "../outside", Valid: true,
		}},
	})
	response := performSaveImportJSON(router, "/api/save-sources/import", map[string]any{"inspection_id": "inspect_unsafe"})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unsafe import response = %d: %s", response.Code, response.Body.String())
	}
	if items, err := store.ListSaveSources(t.Context()); err != nil || len(items) != 0 {
		t.Fatalf("unsafe import stored a source: %#v, %v", items, err)
	}
}

func TestSaveSourceImportInspectionExpiryReturnsGone(t *testing.T) {
	server, router, _ := newSaveImportTestServer(t)
	clock := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	server.saveImports.now = func() time.Time { return clock }
	server.saveImports.put(&saveImportInspection{
		ID: "inspect_expiring", Root: t.TempDir(),
		Candidates: []saveImportCandidate{{ID: "world", RelativePath: "world", Valid: true}},
	})
	clock = clock.Add(2 * time.Minute)
	response := performSaveImportJSON(router, "/api/save-sources/import/inspect/inspect_expiring/select", map[string]any{"candidate_id": "world"})
	if response.Code != http.StatusGone || !strings.Contains(response.Body.String(), `"code":"save_import_inspection_expired"`) {
		t.Fatalf("expired selection response = %d: %s", response.Code, response.Body.String())
	}
}

func TestSaveSourceImportRemovesNestedUnselectedWorlds(t *testing.T) {
	_, router, _ := newSaveImportTestServer(t)
	archive := writeSaveImportArchive(t, map[string]string{
		"outer/Level.sav":        "outer",
		"outer/nested/Level.sav": "nested",
	})
	response := performSaveImportMultipart(t, router, "/api/save-sources/import/inspect", archive, "Outer")
	if response.Code != http.StatusOK {
		t.Fatalf("inspect response = %d: %s", response.Code, response.Body.String())
	}
	inspection := decodeSaveImportInspection(t, response.Body.Bytes())
	outerID := ""
	for _, candidate := range inspection.Candidates {
		if candidate.RelativePath == "outer" {
			outerID = candidate.ID
		}
	}
	response = performSaveImportJSON(router, "/api/save-sources/import/inspect/"+inspection.ID+"/select", map[string]any{"candidate_id": outerID})
	if response.Code != http.StatusOK {
		t.Fatalf("select response = %d: %s", response.Code, response.Body.String())
	}
	response = performSaveImportJSON(router, "/api/save-sources/import", map[string]any{"inspection_id": inspection.ID})
	if response.Code != http.StatusOK {
		t.Fatalf("import response = %d: %s", response.Code, response.Body.String())
	}
	var imported struct {
		Data db.SaveSource `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(imported.Data.Path, "nested", "Level.sav")); !os.IsNotExist(err) {
		t.Fatalf("nested unselected world was retained: %v", err)
	}
}

func TestSaveSourceImportRemovesWorldOptionSav(t *testing.T) {
	_, router, _ := newSaveImportTestServer(t)
	archive := writeSaveImportArchive(t, map[string]string{
		"world/Level.sav":       "level",
		"world/WorldOption.sav": "stale-world-options",
		"world/Players/one.sav": "player",
	})
	response := performSaveImportMultipart(t, router, "/api/save-sources/import", archive, "World option cleanup")
	if response.Code != http.StatusOK {
		t.Fatalf("import response = %d: %s", response.Code, response.Body.String())
	}
	var imported struct {
		Data db.SaveSource `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(imported.Data.Path, "WorldOption.sav")); !os.IsNotExist(err) {
		t.Fatalf("WorldOption.sav was retained after import: %v", err)
	}
	if _, err := os.Stat(filepath.Join(imported.Data.Path, "Level.sav")); err != nil {
		t.Fatalf("Level.sav was removed with WorldOption.sav: %v", err)
	}
	if _, err := os.Stat(filepath.Join(imported.Data.Path, "Players", "one.sav")); err != nil {
		t.Fatalf("player save was removed with WorldOption.sav: %v", err)
	}
}

func TestSaveSourceImportRoutesRequireServerControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalKey, Principal{Name: "viewer", Role: RoleViewer})
		c.Next()
	})
	server := Server{}
	router.POST("/api/save-sources/import/inspect", Require(PermServerControl), server.inspectSaveSourceImport)
	router.POST("/api/save-sources/import/inspect/:id/select", Require(PermServerControl), server.selectSaveSourceImportCandidate)
	router.POST("/api/save-sources/import", Require(PermServerControl), server.importSaveSource)
	for _, path := range []string{
		"/api/save-sources/import/inspect",
		"/api/save-sources/import/inspect/inspect_one/select",
		"/api/save-sources/import",
	} {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Errorf("POST %s as viewer = %d: %s", path, response.Code, response.Body.String())
		}
	}
}

func newSaveImportTestServer(t *testing.T) (*Server, *gin.Engine, *db.Store) {
	t.Helper()
	root := t.TempDir()
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"version":1,"parser":"test-parser","warnings":[],"players":[{"player_uid":"one"}],"guilds":[],"bases":[],"pals":[],"containers":[],"map_entities":[]}}`))
	}))
	t.Cleanup(indexer.Close)
	cfg := appconfig.Config{
		DataDir:                 root,
		DBPath:                  filepath.Join(root, "panel.db"),
		SaveSourcesDir:          filepath.Join(root, "save-sources"),
		SaveIndexCacheDir:       filepath.Join(root, "save-index"),
		SaveIndexerEnabled:      true,
		SaveIndexerURL:          indexer.URL,
		SaveIndexTimeoutSeconds: 2,
		MaxUploadBytes:          1 << 20,
	}
	if err := os.MkdirAll(cfg.SaveSourcesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server := &Server{cfg: cfg, store: store, saveImports: newSaveImportInspectionStore(time.Minute)}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/save-sources/import/inspect", server.inspectSaveSourceImport)
	router.POST("/api/save-sources/import/inspect/:id/select", server.selectSaveSourceImportCandidate)
	router.POST("/api/save-sources/import", server.importSaveSource)
	return server, router, store
}

func writeSaveImportArchive(t *testing.T, entries map[string]string) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "world.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}

func performSaveImportMultipart(t *testing.T, router http.Handler, path, archivePath, name string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(archivePath))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, archive); err != nil {
		t.Fatal(err)
	}
	_ = archive.Close()
	if name != "" {
		_ = writer.WriteField("name", name)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func performSaveImportJSON(router http.Handler, path string, input map[string]any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(input)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeSaveImportInspection(t *testing.T, body []byte) saveImportInspection {
	t.Helper()
	var envelope struct {
		Data saveImportInspection `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
