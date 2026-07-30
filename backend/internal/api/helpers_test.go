package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/appconfig"
	panelauth "palpanel/internal/auth"
	"palpanel/internal/db"
	"palpanel/internal/mods"
	"palpanel/internal/palconfig"
	"palpanel/internal/server"
)

const (
	testDevelopmentKey = "ppk_test-development-key-for-isolated-tests"
	testSessionToken   = "test-session-token-for-isolated-tests"
)

func provisionTestPrincipal(t *testing.T, store *db.Store, role Role) string {
	t.Helper()
	user := db.User{
		ID:           "usr_test",
		Username:     "test-admin",
		PasswordHash: "unused-in-api-key-tests",
		Role:         string(role),
	}
	if err := store.CreateInitialUser(context.Background(), user); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	key := db.APIKey{
		ID:        "key_test",
		UserID:    user.ID,
		Name:      "test",
		Prefix:    testDevelopmentKey[:12],
		TokenHash: panelauth.TokenHash(testDevelopmentKey),
	}
	if err := store.CreateAPIKey(context.Background(), key); err != nil {
		t.Fatalf("create test development key: %v", err)
	}
	return testDevelopmentKey
}

func provisionInteractiveTestSession(t *testing.T, store *db.Store, userID string) string {
	t.Helper()
	now := time.Now().UTC()
	session := db.Session{
		ID:         "ses_test",
		UserID:     userID,
		TokenHash:  panelauth.TokenHash(testSessionToken),
		ExpiresAt:  now.Add(panelauth.SessionLifetime).Format(time.RFC3339Nano),
		LastSeenAt: now.Format(time.RFC3339Nano),
		CreatedAt:  now.Format(time.RFC3339Nano),
	}
	if err := store.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("create test session: %v", err)
	}
	return testSessionToken
}

func authorizeTestRequest(request *http.Request) {
	request.Header.Set("Authorization", "Bearer "+testDevelopmentKey)
}

func authorizeInteractiveTestRequest(request *http.Request) {
	request.AddCookie(&http.Cookie{Name: panelauth.SessionCookieName, Value: testSessionToken})
}

func TestAPIHelperConversionsAndValidation(t *testing.T) {
	tags := queryTags(" one, ,two ")
	if len(tags) != 2 || tags[0] != "one" || tags[1] != "two" || queryTags("  ") != nil {
		t.Fatalf("queryTags = %#v", tags)
	}
	metrics := normalizeRESTMetrics(map[string]any{
		"serverFPS": json.Number("59.5"), "currentPlayerNum": "2", "maxPlayerNum": int64(32),
		"uptime_seconds": float32(10), "pals": 5, "basecampnum": 3, "days": float64(4), "frameTime": "16.7",
	})
	if metrics["server_fps"] != 59.5 || metrics["current_players"] != 2 || metrics["active_bases"] != 3 || metrics["raw"] == nil {
		t.Fatalf("normalizeRESTMetrics = %#v", metrics)
	}
	if metricNumber(nil, "x") != 0 || metricNumber(map[string]any{"x": errors.New("bad")}, "x") != 0 {
		t.Fatal("unexpected metric fallback")
	}
	if got := stringFromMap(map[string]any{"text": " value ", "number": 12}, "text"); got != "value" {
		t.Fatalf("stringFromMap = %q", got)
	}
	if stringFromMap(map[string]any{}, "missing") != "" || stringFromMap(map[string]any{"nil": nil}, "nil") != "" || stringFromMap(map[string]any{"number": 12}, "number") != "12" {
		t.Fatal("unexpected string conversion")
	}
	if !hasServerValidationErrors([]server.ValidationIssue{{Severity: "error"}}) || hasServerValidationErrors([]server.ValidationIssue{{Severity: "warning"}}) {
		t.Fatal("unexpected server issue result")
	}
	if !hasPalconfigErrors([]palconfig.ValidationIssue{{Severity: "error"}}) || hasPalconfigErrors(nil) {
		t.Fatal("unexpected palconfig issue result")
	}
}

func TestPlayerActionPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"nickname":"Player","reason":"test","ignored":true}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	payload, err := playerActionPayload(ctx, "steam-id")
	if err != nil || payload["userid"] != "steam-id" || payload["message"] != "test" || payload["ignored"] != nil {
		t.Fatalf("playerActionPayload = %#v, %v", payload, err)
	}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{"))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if _, err := playerActionPayload(ctx, "steam-id"); err == nil {
		t.Fatal("expected invalid JSON to fail")
	}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	payload, err = playerActionPayload(ctx, "steam-id")
	if err != nil || len(payload) != 1 {
		t.Fatalf("empty payload = %#v, %v", payload, err)
	}
}

func TestWorkshopErrorsUseStableStatusCodes(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{mods.ErrSteamAPIKeyMissing, http.StatusServiceUnavailable, "steam_api_key_missing"},
		{mods.SteamAPIError{Code: "steam_timeout", Message: "timeout"}, http.StatusGatewayTimeout, "steam_timeout"},
		{mods.SteamAPIError{Code: "steam_failed", Message: "failed"}, http.StatusBadGateway, "steam_failed"},
		{errors.New("bad item"), http.StatusBadRequest, "workshop_failed"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		failWorkshop(ctx, test.err)
		if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), test.code) {
			t.Fatalf("failWorkshop(%v) = %d: %s", test.err, recorder.Code, recorder.Body.String())
		}
	}
}

func TestAuditCORSCompressionAndTimingMiddleware(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalKey, Principal{Name: "operator", Role: RoleOperator})
	})
	router.Use(AuditMiddleware(store))
	router.Use(CORSMiddleware([]string{"https://panel.example"}))
	router.Use(PerformanceMiddleware(appconfig.Config{PerfSlowRequestMS: 500, LogLevel: "error"}))
	router.Use(GzipMiddleware())
	router.POST("/items/:id", func(c *gin.Context) { created(c, gin.H{"id": c.Param("id")}) })
	router.GET("/items/:id", func(c *gin.Context) { ok(c, gin.H{"id": c.Param("id")}) })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/items/item-1", nil)
	request.Header.Set("Origin", "https://panel.example")
	request.Header.Set("Accept-Encoding", "gzip")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || recorder.Header().Get("Access-Control-Allow-Origin") != "https://panel.example" || recorder.Header().Get("Server-Timing") == "" || recorder.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("unexpected middleware response: %d %#v", recorder.Code, recorder.Header())
	}
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(reader)
	_ = reader.Close()
	if !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("unexpected compressed body: %s", body)
	}
	audits, err := store.ListAuditLogs(context.Background(), 10)
	if err != nil || len(audits) != 1 || audits[0].Target != "item-1" || audits[0].Actor != "operator" {
		t.Fatalf("unexpected audits: %#v, %v", audits, err)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodOptions, "/items/item-1", nil)
	request.Header.Set("Origin", "https://panel.example")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d", recorder.Code)
	}

	anyRouter := gin.New()
	anyRouter.Use(CORSMiddleware([]string{"*"}))
	anyRouter.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	recorder = httptest.NewRecorder()
	anyRouter.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Header().Get("Access-Control-Allow-Origin") != "*" || recorder.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("unexpected wildcard CORS headers: %#v", recorder.Header())
	}
}

func TestFrontendFilesystemRoutes(t *testing.T) {
	files := fstest.MapFS{
		"index.html":              &fstest.MapFile{Data: []byte("<html>index</html>")},
		"assets/index.js":         &fstest.MapFile{Data: []byte("console.log('asset')")},
		"assets/items/money.webp": &fstest.MapFile{Data: []byte("webp-item")},
		"brand/palpanel-mark.svg": &fstest.MapFile{Data: []byte("<svg>brand</svg>")},
		"favicon.ico":             &fstest.MapFile{Data: []byte("icon")},
	}
	router := gin.New()
	registerFrontendFilesystem(router, files)

	tests := []struct {
		method, path string
		status       int
		cache        string
		body         string
		contentType  string
	}{
		{http.MethodGet, "/", http.StatusOK, "no-cache", "index", "text/html"},
		{http.MethodGet, "/settings", http.StatusOK, "no-cache", "index", "text/html"},
		// Go may use text/javascript or application/javascript depending on the
		// host MIME registry; both are valid JavaScript media types.
		{http.MethodGet, "/assets/index.js", http.StatusOK, "public, max-age=31536000, immutable", "asset", "javascript"},
		{http.MethodGet, "/assets/items/money.webp", http.StatusOK, "public, max-age=31536000, immutable", "webp-item", "image/webp"},
		{http.MethodGet, "/brand/palpanel-mark.svg", http.StatusOK, "public, max-age=3600", "brand", "image/svg+xml"},
		{http.MethodGet, "/favicon.ico", http.StatusOK, "public, max-age=3600", "icon", "image/"},
		{http.MethodGet, "/api/missing", http.StatusNotFound, "", `"code":"not_found"`, "application/json"},
		{http.MethodPost, "/settings", http.StatusNotFound, "", "", ""},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(test.method, test.path, nil)
		router.ServeHTTP(recorder, request)
		if recorder.Code != test.status || recorder.Header().Get("Cache-Control") != test.cache || !strings.Contains(recorder.Body.String(), test.body) {
			t.Errorf("%s %s = %d cache=%q body=%q", test.method, test.path, recorder.Code, recorder.Header().Get("Cache-Control"), recorder.Body.String())
		}
		if test.contentType != "" && !strings.Contains(recorder.Header().Get("Content-Type"), test.contentType) {
			t.Errorf("%s content type = %q", test.path, recorder.Header().Get("Content-Type"))
		}
	}

	withoutUI := gin.New()
	registerFrontendFilesystem(withoutUI, nil)
	recorder := httptest.NewRecorder()
	withoutUI.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/missing", nil))
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"not_found"`) {
		t.Fatalf("API fallback without UI = %d %s", recorder.Code, recorder.Body.String())
	}
}
