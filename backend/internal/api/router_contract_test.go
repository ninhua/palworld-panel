package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"

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

func TestNewContractRoutes(t *testing.T) {
	root := t.TempDir()
	cfg := appconfig.Config{
		DataDir:         root,
		ServerDir:       filepath.Join(root, "server"),
		WinePrefixDir:   filepath.Join(root, "wineprefix"),
		ToolsDir:        filepath.Join(root, "tools"),
		SteamCMDDir:     filepath.Join(root, "tools", "steamcmd"),
		UploadsDir:      filepath.Join(root, "uploads"),
		BackupsDir:      filepath.Join(root, "backups"),
		LogsDir:         filepath.Join(root, "logs"),
		DBPath:          filepath.Join(root, "test.db"),
		RequireAuth:     true,
		DockerBinary:    "docker",
		DockerImage:     "test-image",
		DockerContainer: "test-container",
		GamePort:        8211,
		QueryPort:       27015,
		RESTPort:        8212,
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs returned error: %v", err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("db.Open returned error: %v", err)
	}
	defer store.Close()
	provisionTestPrincipal(t, store, RoleViewer)
	runner := docker.NewRunner(cfg)
	serverManager := server.NewManager(cfg, store, runner)
	restClient := palrest.New("", "", "")
	router := NewRouter(
		cfg,
		store,
		serverManager,
		mods.NewManager(cfg, store, runner),
		paldefender.NewManager(cfg, store),
		restClient,
		monitor.New(cfg, store, serverManager, restClient),
		scheduler.New(store, serverManager, restClient),
	)
	assertOpenAPIContract(t, router)
	healthRequest := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	healthRecorder := httptest.NewRecorder()
	router.ServeHTTP(healthRecorder, healthRequest)
	if healthRecorder.Code != http.StatusOK || !strings.Contains(healthRecorder.Body.String(), `"status":"ok"`) ||
		!strings.Contains(healthRecorder.Body.String(), `"version":"dev"`) ||
		!strings.Contains(healthRecorder.Body.String(), `"commit":"unknown"`) ||
		!strings.Contains(healthRecorder.Body.String(), `"build_time":"unknown"`) {
		t.Fatalf("unexpected health response: %d %s", healthRecorder.Code, healthRecorder.Body.String())
	}
	readyRequest := httptest.NewRequest(http.MethodGet, "/api/ready", nil)
	readyRecorder := httptest.NewRecorder()
	router.ServeHTTP(readyRecorder, readyRequest)
	if readyRecorder.Code != http.StatusOK || !strings.Contains(readyRecorder.Body.String(), `"status":"ready"`) {
		t.Fatalf("unexpected readiness response: %d %s", readyRecorder.Code, readyRecorder.Body.String())
	}
	patchInfoRequest := httptest.NewRequest(http.MethodGet, "/api/patch/info", nil)
	patchInfoRecorder := httptest.NewRecorder()
	router.ServeHTTP(patchInfoRecorder, patchInfoRequest)
	if patchInfoRecorder.Code != http.StatusOK ||
		!strings.Contains(patchInfoRecorder.Body.String(), `"repository":"ninhua/palworld-panel"`) ||
		!strings.Contains(patchInfoRecorder.Body.String(), `"target_version":"v1.3.1"`) {
		t.Fatalf("unexpected patch info response: %d %s", patchInfoRecorder.Code, patchInfoRecorder.Body.String())
	}

	for _, path := range []string{
		"/api/server/startup",
		"/api/server/runtime",
		"/api/server/crash-guard",
		"/api/security/paldefender/status",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		authorizeTestRequest(req)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d: %s", path, rec.Code, rec.Body.String())
		}
	}

	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{
		"POST /api/players/:id/kick",
		"POST /api/players/:id/ban",
		"POST /api/players/:id/unban",
		"POST /api/server/force-stop",
		"GET /api/server/crash-guard",
		"POST /api/server/crash-guard/recover",
		"GET /api/server/version",
		"GET /api/server/game-data",
		"GET /api/panel/update/status",
		"POST /api/panel/update/check",
		"POST /api/panel/update",
		"GET /api/system/diagnostics",
		"POST /api/system/diagnostics/http",
		"POST /api/system/diagnostics/shell",
		"GET /api/system/diagnostics/support-bundles/status",
		"GET /api/system/diagnostics/support-bundles",
		"POST /api/system/diagnostics/support-bundles",
		"GET /api/system/diagnostics/support-bundles/:id/download",
		"DELETE /api/system/diagnostics/support-bundles/:id",
		"GET /api/config/palworld/revisions",
		"GET /api/config/palworld/revisions/:id/diff",
		"POST /api/config/palworld/revisions/:id/restore",
		"GET /api/auth/me",
		"GET /api/server/world",
		"POST /api/server/world/reset",
		"POST /api/server/version/check",
		"POST /api/server/update-if-needed",
		"GET /api/server/host",
		"GET /api/server/docker/plan",
		"POST /api/server/docker/install",
		"GET /api/server/docker/mirrors/plan",
		"POST /api/server/docker/mirrors/configure",
		"POST /api/server/import",
		"GET /api/monitor/snapshot",
		"GET /api/monitor/history",
		"GET /api/schedules",
		"POST /api/schedules",
		"PUT /api/schedules/:id",
		"DELETE /api/schedules/:id",
		"POST /api/schedules/:id/run",
		"GET /api/alerts",
		"POST /api/alerts/:id/ack",
		"GET /api/incidents",
		"GET /api/incidents/:id",
		"POST /api/incidents/:id/ack",
		"POST /api/incidents/:id/resolve",
		"POST /api/incidents/:id/reopen",
		"POST /api/system/incidents/webhook/test",
		"POST /api/mods/local/scan",
		"POST /api/mods/local/findings/:id/actions",
		"GET /api/mods/workshop/status",
		"GET /api/mods/workshop/auth/status",
		"POST /api/mods/workshop/auth/start",
		"POST /api/mods/workshop/auth/verify",
		"GET /api/mods/workshop/search",
		"GET /api/mods/workshop/:id",
		"POST /api/mods/workshop/:id/translate",
		"GET /api/ai/translation/config",
		"PUT /api/ai/translation/config",
		"POST /api/ai/translation/test",
		"POST /api/backups/:name/restore",
		"GET /api/backups/:name/download",
		"DELETE /api/backups/:name",
		"POST /api/backups/:name/verify",
		"GET /api/save/index/status",
		"POST /api/save/index/rebuild",
		"GET /api/save/history",
		"GET /api/save/history/diff",
		"GET /api/players",
		"GET /api/players/:id",
		"GET /api/players/:id/inventory",
		"PUT /api/players/:id/annotation",
		"DELETE /api/players/:id/annotation",
		"GET /api/guilds",
		"GET /api/guilds/:id",
		"GET /api/bases",
		"GET /api/bases/:id",
		"GET /api/bases/:id/storage",
		"GET /api/bases/:id/workers",
		"GET /api/bases/:id/feed-boxes",
		"POST /api/bases/:id/clean",
		"GET /api/pals",
		"GET /api/pals/:id",
		"GET /api/map/entities",
		"GET /api/security/paldefender/gm/status",
		"GET /api/security/paldefender/gm/items",
		"GET /api/security/paldefender/gm/players",
		"GET /api/security/paldefender/gm/players/:id",
		"GET /api/security/paldefender/gm/players/:id/inventory",
		"POST /api/security/paldefender/gm/players/:id/items",
		"POST /api/security/paldefender/gm/players/:id/message",
		"POST /api/security/paldefender/gm/players/:id/kick",
		"POST /api/security/paldefender/gm/players/:id/ban",
		"POST /api/security/paldefender/gm/players/:id/unban",
		"POST /api/security/paldefender/gm/broadcast",
	} {
		if !routes[want] {
			t.Fatalf("missing route %s", want)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/mods/local/scan", nil)
	authorizeTestRequest(req)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"findings":[]`) {
		t.Fatalf("viewer local Mod scan expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/mods/local/findings/localmod_test/actions", strings.NewReader(`{"action":"ignore","revision":"old"}`))
	req.Header.Set("Content-Type", "application/json")
	authorizeTestRequest(req)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer local Mod action expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/server/docker/install", nil)
	authorizeTestRequest(req)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer docker install expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/server/docker/mirrors/configure", nil)
	authorizeTestRequest(req)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer docker mirror configure expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/save/index/rebuild", nil)
	authorizeTestRequest(req)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer save index rebuild expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func assertOpenAPIContract(t *testing.T, router *gin.Engine) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read OpenAPI document: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(body, &spec); err != nil {
		t.Fatalf("parse OpenAPI document: %v", err)
	}
	documented := map[string]bool{}
	for path, pathItem := range spec.Paths {
		for method, rawOperation := range pathItem {
			switch method {
			case "get", "post", "put", "delete", "patch":
			default:
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				t.Errorf("OpenAPI operation %s %s is malformed", method, path)
				continue
			}
			key := strings.ToUpper(method) + " /api" + path
			documented[key] = true
			if strings.TrimSpace(fmt.Sprint(operation["operationId"])) == "" {
				t.Errorf("OpenAPI operation %s has no operationId", key)
			}
			if strings.TrimSpace(fmt.Sprint(operation["x-palpanel-permission"])) == "" {
				t.Errorf("OpenAPI operation %s has no permission", key)
			}
		}
	}
	actual := map[string]bool{}
	ginParameter := regexp.MustCompile(`:([A-Za-z_][A-Za-z0-9_]*)`)
	for _, route := range router.Routes() {
		if !strings.HasPrefix(route.Path, "/api/") {
			continue
		}
		path := route.Path
		path = ginParameter.ReplaceAllString(path, `{$1}`)
		actual[route.Method+" "+path] = true
	}
	for key := range actual {
		if !documented[key] {
			t.Errorf("API route is missing from OpenAPI: %s", key)
		}
	}
	for key := range documented {
		if !actual[key] {
			t.Errorf("OpenAPI operation has no API route: %s", key)
		}
	}
}

func TestOpenAPIAuthenticationAndModImportSchemas(t *testing.T) {
	type schemaReference struct {
		Ref string `yaml:"$ref"`
	}
	type response struct {
		Content map[string]struct {
			Schema schemaReference `yaml:"schema"`
		} `yaml:"content"`
	}
	type operation struct {
		Permission string `yaml:"x-palpanel-permission"`
		Parameters []struct {
			Name     string `yaml:"name"`
			In       string `yaml:"in"`
			Required bool   `yaml:"required"`
		} `yaml:"parameters"`
		RequestBody struct {
			Content map[string]struct {
				Schema schemaReference `yaml:"schema"`
			} `yaml:"content"`
		} `yaml:"requestBody"`
		Responses map[string]response `yaml:"responses"`
	}
	var spec struct {
		Paths map[string]struct {
			Get  operation `yaml:"get"`
			Post operation `yaml:"post"`
			Put  operation `yaml:"put"`
		} `yaml:"paths"`
		Components struct {
			SecuritySchemes map[string]struct {
				Type   string `yaml:"type"`
				In     string `yaml:"in"`
				Name   string `yaml:"name"`
				Scheme string `yaml:"scheme"`
			} `yaml:"securitySchemes"`
			Schemas map[string]struct {
				Properties map[string]struct {
					WriteOnly bool `yaml:"writeOnly"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(body, &spec); err != nil {
		t.Fatal(err)
	}

	community := spec.Paths["/community-servers"].Get
	communityParameters := map[string]bool{}
	for _, parameter := range community.Parameters {
		communityParameters[parameter.Name] = true
	}
	for _, name := range []string{"region", "search", "min_players", "max_players", "password", "version", "status", "page", "page_size"} {
		if !communityParameters[name] {
			t.Errorf("GET /community-servers is missing query parameter %s", name)
		}
	}
	for _, response := range []string{"200", "400", "401", "429", "502", "503"} {
		if _, ok := community.Responses[response]; !ok {
			t.Errorf("GET /community-servers is missing response %s", response)
		}
	}
	for _, schema := range []string{"CommunityServer", "CommunityServerResult", "CommunityServerSourceStatus", "CommunityServerResultEnvelope", "CommunityServerSourceStatusEnvelope"} {
		if _, ok := spec.Components.Schemas[schema]; !ok {
			t.Errorf("OpenAPI is missing schema %s", schema)
		}
	}
	for _, schema := range []string{"SafeLifecycleRequest", "BreedingStatus", "AstrBotControlRequest", "AstrBotCommunityServerRequest", "AstrBotServerStatus", "ModConfigFile", "ModConfigDocument", "ModConfigWriteRequest", "ModConfigRestoreRequest", "ModConfigBackup", "ModConfigurationAdapter"} {
		if _, ok := spec.Components.Schemas[schema]; !ok {
			t.Errorf("OpenAPI is missing schema %s", schema)
		}
	}
	for _, operation := range []operation{
		spec.Paths["/mods/configurations/{adapter}"].Get,
		spec.Paths["/mods/configurations/{adapter}"].Put,
		spec.Paths["/mods/configurations/{adapter}/backups"].Get,
		spec.Paths["/mods/configurations/{adapter}/backups/{backup}/restore"].Post,
	} {
		var hasFile bool
		for _, parameter := range operation.Parameters {
			if parameter.Name == "file" && parameter.In == "query" {
				hasFile = true
			}
		}
		if !hasFile {
			t.Error("Mod adapter operation is missing the opaque file query parameter")
		}
	}

	assertRequestSchema := func(path, mediaType, want string) {
		t.Helper()
		got := spec.Paths[path].Post.RequestBody.Content[mediaType].Schema.Ref
		if got != "#/components/schemas/"+want {
			t.Errorf("%s %s request schema = %q, want %s", path, mediaType, got, want)
		}
	}
	assertRequestSchema("/auth/register", "application/json", "AuthCredentials")
	assertRequestSchema("/auth/login", "application/json", "AuthCredentials")
	assertRequestSchema("/auth/api-keys", "application/json", "DevelopmentKeyInput")
	assertRequestSchema("/mods/import/inspect", "application/json", "ModImportInspectRequest")
	assertRequestSchema("/mods/import/inspect", "multipart/form-data", "ModImportUploadRequest")
	assertRequestSchema("/mods/import/inspect/{id}/select", "application/json", "ModImportSelectRequest")
	assertRequestSchema("/mods/import", "application/json", "ModImportRequest")
	assertRequestSchema("/save-sources/import/inspect", "multipart/form-data", "SaveImportInspectRequest")
	assertRequestSchema("/save-sources/import/inspect/{id}/select", "application/json", "SaveImportSelectRequest")
	assertRequestSchema("/save-sources/import", "multipart/form-data", "SaveSourceImportRequest")
	assertRequestSchema("/save-sources/import", "application/json", "SaveImportCommitRequest")
	if got := spec.Paths["/save-sources/import"].Post.Responses["409"].Content["application/json"].Schema.Ref; got != "#/components/schemas/SaveImportConflictEnvelope" {
		t.Errorf("save import 409 response schema = %q", got)
	}
	assertRequestSchema("/security/paldefender/gm/players/{id}/items", "application/json", "PalDefenderGiveItemsRequest")
	assertRequestSchema("/security/paldefender/gm/players/{id}/custom-pals", "application/json", "PalDefenderGiveCustomPalsRequest")
	assertRequestSchema("/security/paldefender/gm/players/{id}/message", "application/json", "PalDefenderMessageRequest")
	assertRequestSchema("/security/paldefender/gm/broadcast", "application/json", "PalDefenderBroadcastRequest")
	for _, path := range []string{
		"/security/paldefender/gm/players/{id}/items",
		"/security/paldefender/gm/players/{id}/custom-pals",
		"/security/paldefender/gm/players/{id}/message",
		"/security/paldefender/gm/players/{id}/kick",
		"/security/paldefender/gm/players/{id}/ban",
		"/security/paldefender/gm/players/{id}/unban",
		"/security/paldefender/gm/broadcast",
	} {
		var found bool
		for _, parameter := range spec.Paths[path].Post.Parameters {
			if parameter.Name == "Idempotency-Key" && parameter.In == "header" && parameter.Required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("POST %s must require the Idempotency-Key header", path)
		}
	}
	assertRequestSchema("/ai/translation/test", "application/json", "AITranslationConfigUpdate")
	assertRequestSchema("/settings/network-proxy/test", "application/json", "NetworkProxyTestRequest")
	assertRequestSchema("/server/safe-stop", "application/json", "SafeLifecycleRequest")
	assertRequestSchema("/server/safe-restart", "application/json", "SafeLifecycleRequest")
	assertRequestSchema("/integrations/astrbot/server-control", "application/json", "AstrBotControlRequest")
	assertRequestSchema("/integrations/astrbot/community-servers", "application/json", "AstrBotCommunityServerRequest")
	assertRequestSchema("/mods/configurations/{adapter}/backups/{backup}/restore", "application/json", "ModConfigRestoreRequest")
	assertRequestSchema("/mods/{id}/files/{file}/backups/{backup}/restore", "application/json", "ModConfigRestoreRequest")
	if got := spec.Paths["/mods/configurations/{adapter}"].Put.RequestBody.Content["application/json"].Schema.Ref; got != "#/components/schemas/ModConfigWriteRequest" {
		t.Errorf("PUT /mods/configurations/{adapter} request schema = %q", got)
	}
	if got := spec.Paths["/mods/{id}/files/{file}"].Put.RequestBody.Content["application/json"].Schema.Ref; got != "#/components/schemas/ModConfigWriteRequest" {
		t.Errorf("PUT /mods/{id}/files/{file} request schema = %q", got)
	}
	if got := spec.Paths["/ai/translation/config"].Put.RequestBody.Content["application/json"].Schema.Ref; got != "#/components/schemas/AITranslationConfigUpdate" {
		t.Errorf("PUT /ai/translation/config request schema = %q", got)
	}
	if got := spec.Paths["/config/palworld"].Put.RequestBody.Content["application/json"].Schema.Ref; got != "#/components/schemas/PalworldConfigUpdateRequest" {
		t.Errorf("PUT /config/palworld request schema = %q", got)
	}
	if got := spec.Paths["/config/palworld/apply"].Post.RequestBody.Content["application/json"].Schema.Ref; got != "#/components/schemas/PalworldConfigApplyRequest" {
		t.Errorf("POST /config/palworld/apply request schema = %q", got)
	}
	if got := spec.Paths["/config/palworld/validate"].Post.RequestBody.Content["application/json"].Schema.Ref; got != "#/components/schemas/PalworldConfigValidateRequest" {
		t.Errorf("POST /config/palworld/validate request schema = %q", got)
	}
	for _, operation := range []struct {
		name string
		ref  string
	}{
		{name: "GET /config/palworld", ref: spec.Paths["/config/palworld"].Get.Responses["200"].Content["application/json"].Schema.Ref},
		{name: "PUT /config/palworld", ref: spec.Paths["/config/palworld"].Put.Responses["200"].Content["application/json"].Schema.Ref},
	} {
		if operation.ref != "#/components/schemas/PalworldConfigEnvelope" {
			t.Errorf("%s response schema = %q", operation.name, operation.ref)
		}
	}
	if got := spec.Paths["/config/palworld/apply"].Post.Responses["202"].Content["application/json"].Schema.Ref; got != "#/components/schemas/JobEnvelope" {
		t.Errorf("POST /config/palworld/apply response schema = %q", got)
	}
	if got := spec.Paths["/config/palworld/schema"].Get.Responses["200"].Content["application/json"].Schema.Ref; got != "#/components/schemas/PalworldConfigSchemaEnvelope" {
		t.Errorf("GET /config/palworld/schema response schema = %q", got)
	}
	if got := spec.Paths["/config/palworld/validate"].Post.Responses["200"].Content["application/json"].Schema.Ref; got != "#/components/schemas/PalworldConfigValidationEnvelope" {
		t.Errorf("POST /config/palworld/validate response schema = %q", got)
	}
	for _, property := range []string{"revision_sha256", "secret_state", "format_issues", "draft"} {
		if _, ok := spec.Components.Schemas["PalworldConfig"].Properties[property]; !ok {
			t.Errorf("PalworldConfig is missing %s", property)
		}
	}
	if got := spec.Paths["/settings/network-proxy"].Put.RequestBody.Content["application/json"].Schema.Ref; got != "#/components/schemas/NetworkProxyConfigUpdate" {
		t.Errorf("PUT /settings/network-proxy request schema = %q", got)
	}
	for _, property := range []string{"install_proxy_url", "community_proxy_url"} {
		if !spec.Components.Schemas["NetworkProxyConfigUpdate"].Properties[property].WriteOnly {
			t.Errorf("NetworkProxyConfigUpdate.%s must be writeOnly", property)
		}
	}
	for _, property := range []string{"api_key", "proxy_url", "custom_headers"} {
		if !spec.Components.Schemas["AITranslationConfigUpdate"].Properties[property].WriteOnly {
			t.Errorf("AITranslationConfigUpdate.%s must be writeOnly", property)
		}
	}
	if permission := spec.Paths["/mods/local/scan"].Post.Permission; permission != "read" {
		t.Errorf("POST /mods/local/scan permission = %q, want read", permission)
	}
	if _, ok := spec.Paths["/mods/local/scan"].Post.Responses["200"]; !ok {
		t.Error("POST /mods/local/scan does not document its 200 response")
	}
	if permission := spec.Paths["/mods/local/findings/{id}/actions"].Post.Permission; permission != "mods:write" {
		t.Errorf("POST /mods/local/findings/{id}/actions permission = %q, want mods:write", permission)
	}
	if permission := spec.Paths["/mods/workshop/auth/start"].Post.Permission; permission != "security:write" {
		t.Errorf("POST /mods/workshop/auth/start permission = %q, want security:write", permission)
	}
	if permission := spec.Paths["/mods/workshop/auth/verify"].Post.Permission; permission != "security:write" {
		t.Errorf("POST /mods/workshop/auth/verify permission = %q, want security:write", permission)
	}
	assertRequestSchema("/mods/local/findings/{id}/actions", "application/json", "LocalModActionRequest")

	responses := spec.Paths["/mods/import"].Post.Responses
	if _, ok := responses["202"]; !ok {
		t.Error("POST /mods/import does not document its 202 Job response")
	}
	if _, ok := responses["200"]; ok {
		t.Error("POST /mods/import must not document a synchronous 200 response")
	}
	cookie := spec.Components.SecuritySchemes["sessionCookie"]
	if cookie.Type != "apiKey" || cookie.In != "cookie" || cookie.Name != "palpanel_session" {
		t.Errorf("session cookie security scheme = %#v", cookie)
	}
	key := spec.Components.SecuritySchemes["developmentKey"]
	if key.Type != "http" || key.Scheme != "bearer" {
		t.Errorf("development key security scheme = %#v", key)
	}
}
