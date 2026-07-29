package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/appconfig"
	panelauth "palpanel/internal/auth"
	"palpanel/internal/db"
	"palpanel/internal/docker"
	"palpanel/internal/palconfig"
	"palpanel/internal/palrest"
	"palpanel/internal/server"
)

func TestPalworldConfigReadRedactsSecretsAndReportsBarePassword(t *testing.T) {
	router, cfg, _ := newPalworldConfigSafetyRouter(t)
	writePalworldConfigFixture(t, cfg, `OptionSettings=(ServerName="Visible",AdminPassword=admin-secret,ServerPassword=join-secret,RESTAPIEnabled=True)`)

	recorder := performConfigRequest(t, router, http.MethodGet, "/api/config/palworld", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if strings.Contains(body, "admin-secret") || strings.Contains(body, "join-secret") {
		t.Fatalf("GET leaked a secret: %s", body)
	}
	for _, want := range []string{`"admin_password":{"configured":true}`, `"server_password":{"configured":true}`, `"code":"string_not_quoted"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET response missing %s: %s", want, body)
		}
	}
}

func TestPalworldConfigReadinessVerifierRejectsModifiedBodyMismatch(t *testing.T) {
	configuredPort := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "admin" || password != "new-admin" {
			t.Fatalf("basic auth = %q/%q/%v", user, password, ok)
		}
		if r.URL.Path != "/v1/api/settings" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ServerName":"runtime-name","RESTAPIPort":` + configuredPort + `,"RESTAPIEnabled":true,"AdminPassword":"ignored"}`))
	}))
	defer upstream.Close()
	configuredPort = upstream.URL[strings.LastIndex(upstream.URL, ":")+1:]
	s := Server{palrest: palrest.New(upstream.URL+"/v1/api", "admin", "stale")}
	settings := palconfig.Settings{
		"ServerName": "draft-name", "RESTAPIPort": configuredPort, "RESTAPIEnabled": "True", "AdminPassword": "new-admin",
	}
	err := s.verifyPalworldConfigReadiness(context.Background(), settings, []string{"ServerName", "RESTAPIPort", "RESTAPIEnabled", "AdminPassword"})
	if err == nil || !strings.Contains(err.Error(), "ServerName") {
		t.Fatalf("expected ServerName mismatch, got %v", err)
	}
}

func TestPalworldConfigReadinessVerifierNormalizesObservableValues(t *testing.T) {
	configuredPort := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"RESTAPIPort":` + configuredPort + `,"RESTAPIEnabled":true}`))
	}))
	defer upstream.Close()
	configuredPort = upstream.URL[strings.LastIndex(upstream.URL, ":")+1:]
	s := Server{palrest: palrest.New(upstream.URL+"/v1/api", "admin", "")}
	err := s.verifyPalworldConfigReadiness(context.Background(), palconfig.Settings{
		"RESTAPIPort": configuredPort + ".0", "RESTAPIEnabled": "true", "ServerName": "not returned",
	}, []string{"RESTAPIPort", "RESTAPIEnabled", "ServerName"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPalworldConfigPutCreatesDraftWithoutWritingActiveFile(t *testing.T) {
	router, cfg, store := newPalworldConfigSafetyRouter(t)
	writePalworldConfigFixture(t, cfg, `OptionSettings=(ServerName="Before",AdminPassword="keep-secret",ServerPassword="join-secret")`)
	before, err := os.ReadFile(cfg.PalWorldSettingsPath())
	if err != nil {
		t.Fatal(err)
	}

	recorder := performConfigRequest(t, router, http.MethodPut, "/api/config/palworld", `{"settings":{"ServerName":"After","ServerPassword":"123456"}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", recorder.Code, recorder.Body.String())
	}
	after, err := os.ReadFile(cfg.PalWorldSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("PUT changed active file:\nbefore=%s\nafter=%s", before, after)
	}
	if strings.Contains(recorder.Body.String(), "keep-secret") || strings.Contains(recorder.Body.String(), "123456") {
		t.Fatalf("PUT leaked a secret: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"status":"draft"`) || !strings.Contains(recorder.Body.String(), `"revision_sha256":"`) {
		t.Fatalf("PUT did not return a draft: %s", recorder.Body.String())
	}
	if version, err := store.SchemaVersion(t.Context()); err != nil || version < 7 {
		t.Fatalf("schema version = %d, %v; config draft migration missing", version, err)
	}
}

func TestPalworldConfigPutSupersedesAndDeletesPreviousPrivateDraft(t *testing.T) {
	router, cfg, store := newPalworldConfigSafetyRouter(t)
	writePalworldConfigFixture(t, cfg, `OptionSettings=(ServerName="Before",AdminPassword="secret")`)
	first := performConfigRequest(t, router, http.MethodPut, "/api/config/palworld", `{"settings":{"ServerName":"First"}}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first PUT = %d: %s", first.Code, first.Body.String())
	}
	previous, err := store.LatestConfigDraft(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(previous.DraftPath); err != nil {
		t.Fatal(err)
	}
	second := performConfigRequest(t, router, http.MethodPut, "/api/config/palworld", `{"settings":{"ServerName":"Second"}}`)
	if second.Code != http.StatusOK {
		t.Fatalf("second PUT = %d: %s", second.Code, second.Body.String())
	}
	previous, err = store.GetConfigDraft(t.Context(), previous.ID)
	if err != nil || previous.Status != "superseded" {
		t.Fatalf("previous = %#v, %v", previous, err)
	}
	if _, err := os.Stat(previous.DraftPath); !os.IsNotExist(err) {
		t.Fatalf("superseded private draft still exists: %v", err)
	}
}

func TestPalworldConfigRequestsIgnoreRetryableOldCleanupFailure(t *testing.T) {
	router, cfg, store := newPalworldConfigSafetyRouter(t)
	writePalworldConfigFixture(t, cfg, `OptionSettings=(ServerName="Before",AdminPassword="secret",RESTAPIEnabled=False)`)
	blocked := filepath.Join(cfg.DataDir, "config-drafts", "blocked")
	if err := os.MkdirAll(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(blocked, "child")
	if err := os.WriteFile(child, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueConfigPrivateCleanup(t.Context(), blocked, "config_draft"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(child)
		_ = os.Remove(blocked)
		_ = store.CompleteConfigPrivateCleanup(context.Background(), blocked)
	})

	get := performConfigRequest(t, router, http.MethodGet, "/api/config/palworld", "")
	if get.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", get.Code, get.Body.String())
	}
	put := performConfigRequest(t, router, http.MethodPut, "/api/config/palworld", `{"settings":{"ServerName":"After"}}`)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", put.Code, put.Body.String())
	}
	var envelope struct {
		Data struct {
			Draft db.ConfigDraft `json:"draft"`
		} `json:"data"`
	}
	if err := json.Unmarshal(put.Body.Bytes(), &envelope); err != nil || envelope.Data.Draft.ID == "" {
		t.Fatalf("draft = %#v, %v", envelope, err)
	}
	apply := performConfigRequest(t, router, http.MethodPost, "/api/config/palworld/apply", `{"draft_id":"`+envelope.Data.Draft.ID+`"}`)
	if apply.Code != http.StatusAccepted {
		t.Fatalf("apply = %d: %s", apply.Code, apply.Body.String())
	}
	pending, err := store.ListConfigPrivateCleanup(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range pending {
		if item.Path == blocked && item.Attempts > 0 && item.LastError != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("retryable cleanup was not retained: %#v", pending)
	}
}

func TestPalworldConfigPutReturnsNewDraftWhenSupersededFileCleanupFails(t *testing.T) {
	router, cfg, store := newPalworldConfigSafetyRouter(t)
	writePalworldConfigFixture(t, cfg, `OptionSettings=(ServerName="Before",RESTAPIEnabled=False)`)
	first := performConfigRequest(t, router, http.MethodPut, "/api/config/palworld", `{"settings":{"ServerName":"First"}}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first PUT = %d: %s", first.Code, first.Body.String())
	}
	old, err := store.LatestConfigDraft(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(old.DraftPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(old.DraftPath, 0o700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(old.DraftPath, "child")
	if err := os.WriteFile(child, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(child); _ = os.Remove(old.DraftPath) })
	second := performConfigRequest(t, router, http.MethodPut, "/api/config/palworld", `{"settings":{"ServerName":"Second"}}`)
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"status":"draft"`) {
		t.Fatalf("second PUT = %d: %s", second.Code, second.Body.String())
	}
	pending, err := store.ListConfigPrivateCleanup(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range pending {
		if item.Path == old.DraftPath && item.Attempts > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("superseded cleanup was not retained: %#v", pending)
	}
}

func TestPalworldConfigApplyRejectsStaleDraft(t *testing.T) {
	router, cfg, _ := newPalworldConfigSafetyRouter(t)
	writePalworldConfigFixture(t, cfg, `OptionSettings=(ServerName="Before",AdminPassword="secret")`)
	created := performConfigRequest(t, router, http.MethodPut, "/api/config/palworld", `{"settings":{"ServerName":"After"}}`)
	if created.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", created.Code, created.Body.String())
	}
	var envelope struct {
		Data struct {
			Draft struct {
				ID string `json:"id"`
			} `json:"draft"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil || envelope.Data.Draft.ID == "" {
		t.Fatalf("decode draft: %v: %s", err, created.Body.String())
	}
	writePalworldConfigFixture(t, cfg, `OptionSettings=(ServerName="External change",AdminPassword="secret")`)

	applied := performConfigRequest(t, router, http.MethodPost, "/api/config/palworld/apply", `{"draft_id":"`+envelope.Data.Draft.ID+`"}`)
	if applied.Code != http.StatusConflict || !strings.Contains(applied.Body.String(), `"code":"config_draft_stale"`) {
		t.Fatalf("apply stale status = %d: %s", applied.Code, applied.Body.String())
	}
}

func TestPalworldConfigApplyWhileStoppedKeepsServerStopped(t *testing.T) {
	router, cfg, store := newPalworldConfigSafetyRouter(t)
	if err := store.SetKV(t.Context(), "runtime_mode", server.RuntimeWindowsSteamCMD); err != nil {
		t.Fatal(err)
	}
	writePalworldConfigFixture(t, cfg, `OptionSettings=(ServerName="Before",ServerPassword=123456)`)
	created := performConfigRequest(t, router, http.MethodPut, "/api/config/palworld", `{"settings":{"ServerName":"After"}}`)
	var envelope struct {
		Data struct {
			Draft struct {
				ID string `json:"id"`
			} `json:"draft"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	applied := performConfigRequest(t, router, http.MethodPost, "/api/config/palworld/apply", `{"draft_id":"`+envelope.Data.Draft.ID+`"}`)
	if applied.Code != http.StatusAccepted {
		t.Fatalf("apply status = %d: %s", applied.Code, applied.Body.String())
	}
	var jobEnvelope struct {
		Data db.Job `json:"data"`
	}
	if err := json.Unmarshal(applied.Body.Bytes(), &jobEnvelope); err != nil {
		t.Fatal(err)
	}
	job := waitForConfigJob(t, store, jobEnvelope.Data.ID)
	if job.Status != "completed" {
		t.Fatalf("apply job = %#v", job)
	}
	settingsBody, err := os.ReadFile(cfg.PalWorldSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settingsBody), `ServerName="After"`) || !strings.Contains(string(settingsBody), `ServerPassword="123456"`) {
		t.Fatalf("applied settings = %s", settingsBody)
	}
	status, err := server.NewManager(cfg, store, docker.NewRunner(cfg)).Status(t.Context())
	if err != nil || status.Container.Status == "running" {
		t.Fatalf("server unexpectedly running after stopped apply: %#v, %v", status, err)
	}
}

func newPalworldConfigSafetyRouter(t *testing.T) (http.Handler, appconfig.Config, *db.Store) {
	t.Helper()
	root := t.TempDir()
	cfg := appconfig.Config{
		DataDir: root, ServerDir: filepath.Join(root, "server"), WinePrefixDir: filepath.Join(root, "wine"),
		ToolsDir: filepath.Join(root, "tools"), SteamCMDDir: filepath.Join(root, "steamcmd"),
		UploadsDir: filepath.Join(root, "uploads"), BackupsDir: filepath.Join(root, "backups"), LogsDir: filepath.Join(root, "logs"),
		DBPath: filepath.Join(root, "panel.db"), RequireAuth: true, DockerBinary: filepath.Join(root, "missing-docker"),
		DockerImage: "test", DockerContainer: "test", GamePort: 8211, QueryPort: 27015, RESTPort: 8212,
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	provisionTestPrincipal(t, store, RoleAdmin)
	runner := docker.NewRunner(cfg)
	manager := server.NewManager(cfg, store, runner)
	s := Server{cfg: cfg, store: store, server: manager, auth: nil, cache: newTTLCache()}
	engine := newConfigTestEngine(s, store)
	return engine, cfg, store
}

func newConfigTestEngine(s Server, store *db.Store) http.Handler {
	engine := gin.New()
	s.auth = panelauth.New(store)
	api := engine.Group("/api")
	api.Use(Auth(s.cfg, s.auth))
	s.registerContentRoutes(api)
	return engine
}

func writePalworldConfigFixture(t *testing.T, cfg appconfig.Config, optionSettings string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(cfg.PalWorldSettingsPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	content := palconfig.SectionHeader + "\n" + optionSettings + "\n"
	if err := os.WriteFile(cfg.PalWorldSettingsPath(), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func performConfigRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	authorizeTestRequest(request)
	handler.ServeHTTP(recorder, request)
	return recorder
}

func waitForConfigJob(t *testing.T, store *db.Store, id string) db.Job {
	t.Helper()
	for range 100 {
		job, err := store.GetJob(t.Context(), id)
		if err == nil && (job.Status == "completed" || job.Status == "failed") {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for config job")
	return db.Job{}
}

func TestPalworldConfigRevisionListMarksOnlyNewestMatchingRevisionCurrent(t *testing.T) {
	router, cfg, store := newPalworldConfigSafetyRouter(t)
	writePalworldConfigFixture(t, cfg, `OptionSettings=(ServerName="Current",AdminPassword="secret",RESTAPIEnabled=False)`)
	current, err := server.PalworldConfigRevision(cfg.PalWorldSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	activeContent, err := os.ReadFile(cfg.PalWorldSettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	revisionDir := filepath.Join(cfg.DataDir, "config-revisions")
	if err := os.MkdirAll(revisionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(revisionDir, "rev_current_new.ini"), activeContent, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []db.ConfigRevision{
		{ID: "rev_current_old", RevisionSHA256: current, SnapshotPath: filepath.Join(revisionDir, "rev_current_old.ini"), Source: "apply", CreatedAt: "2026-07-29T00:00:00Z"},
		{ID: "rev_other", RevisionSHA256: strings.Repeat("a", 64), SnapshotPath: filepath.Join(revisionDir, "rev_other.ini"), Source: "apply", CreatedAt: "2026-07-29T01:00:00Z"},
		{ID: "rev_current_new", RevisionSHA256: current, SnapshotPath: filepath.Join(revisionDir, "rev_current_new.ini"), Source: "apply", CreatedAt: "2026-07-29T02:00:00Z"},
	} {
		if err := store.CreateConfigRevision(t.Context(), revision); err != nil {
			t.Fatal(err)
		}
	}
	listed := performConfigRequest(t, router, http.MethodGet, "/api/config/palworld/revisions", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", listed.Code, listed.Body.String())
	}
	var envelope struct {
		Data struct {
			Items []struct {
				ID      string `json:"id"`
				Current bool   `json:"current"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	currentIDs := []string{}
	for _, revision := range envelope.Data.Items {
		if revision.Current {
			currentIDs = append(currentIDs, revision.ID)
		}
	}
	if len(currentIDs) != 1 || currentIDs[0] != "rev_current_new" {
		t.Fatalf("current revisions = %#v; response=%s", currentIDs, listed.Body.String())
	}
}

func TestPalworldConfigRevisionHistoryDiffAndRestoreDraftRedactSecrets(t *testing.T) {
	router, cfg, store := newPalworldConfigSafetyRouter(t)
	writePalworldConfigFixture(t, cfg, `OptionSettings=(ServerName="Before",AdminPassword="old-secret",ServerPassword="join-secret",RESTAPIEnabled=False)`)

	listed := performConfigRequest(t, router, http.MethodGet, "/api/config/palworld/revisions", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", listed.Code, listed.Body.String())
	}
	if strings.Contains(listed.Body.String(), "old-secret") || strings.Contains(listed.Body.String(), "snapshot_path") {
		t.Fatalf("revision list leaked private data: %s", listed.Body.String())
	}
	var listEnvelope struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listEnvelope); err != nil || len(listEnvelope.Data.Items) != 1 {
		t.Fatalf("list decode = %#v, %v", listEnvelope, err)
	}
	baselineID := listEnvelope.Data.Items[0].ID

	writePalworldConfigFixture(t, cfg, `OptionSettings=(ServerName="After",AdminPassword="new-secret",ServerPassword="join-secret",RESTAPIEnabled=False)`)
	diff := performConfigRequest(t, router, http.MethodGet, "/api/config/palworld/revisions/"+baselineID+"/diff", "")
	if diff.Code != http.StatusOK {
		t.Fatalf("diff status = %d: %s", diff.Code, diff.Body.String())
	}
	body := diff.Body.String()
	if !strings.Contains(body, `"field":"ServerName"`) || !strings.Contains(body, `"revision_value":"Before"`) || !strings.Contains(body, `"current_value":"After"`) {
		t.Fatalf("diff missing visible change: %s", body)
	}
	if !strings.Contains(body, `"field":"AdminPassword"`) || !strings.Contains(body, `"secret":true`) || strings.Contains(body, "old-secret") || strings.Contains(body, "new-secret") {
		t.Fatalf("diff leaked or omitted secret metadata: %s", body)
	}

	if err := store.SetKV(t.Context(), "palworld_config_apply_journal", `{}`); err != nil {
		t.Fatal(err)
	}
	blocked := performConfigRequest(t, router, http.MethodPost, "/api/config/palworld/revisions/"+baselineID+"/restore", `{"confirm":true}`)
	if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body.String(), "config_apply_in_progress") {
		t.Fatalf("in-flight restore status = %d: %s", blocked.Code, blocked.Body.String())
	}
	if err := store.DeleteKV(t.Context(), "palworld_config_apply_journal"); err != nil {
		t.Fatal(err)
	}
	restored := performConfigRequest(t, router, http.MethodPost, "/api/config/palworld/revisions/"+baselineID+"/restore", `{"confirm":true}`)
	if restored.Code != http.StatusOK || !strings.Contains(restored.Body.String(), `"status":"draft"`) {
		t.Fatalf("restore status = %d: %s", restored.Code, restored.Body.String())
	}
	if strings.Contains(restored.Body.String(), "old-secret") || strings.Contains(restored.Body.String(), "new-secret") {
		t.Fatalf("restore response leaked secret: %s", restored.Body.String())
	}
	active, err := os.ReadFile(cfg.PalWorldSettingsPath())
	if err != nil || !strings.Contains(string(active), `ServerName="After"`) {
		t.Fatalf("restore draft changed active file: %s, %v", active, err)
	}
	latestDraft, err := store.LatestConfigDraft(t.Context())
	if err != nil || latestDraft.Status != "draft" || latestDraft.BaseSHA256 == "" {
		t.Fatalf("restore draft = %#v, %v", latestDraft, err)
	}
}
