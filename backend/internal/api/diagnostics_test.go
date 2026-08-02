package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"palpanel/internal/appconfig"
	panelauth "palpanel/internal/auth"
)

func TestDiagnosticHTTPAllowsLoopbackAndRejectsPublicTargets(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Diagnostic", "ok")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ready"))
	}))
	defer target.Close()

	server := Server{}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/http", server.runDiagnosticHTTP)

	allowedBody, _ := json.Marshal(diagnosticHTTPRequest{Method: http.MethodPost, URL: target.URL, Body: "ping"})
	allowed := httptest.NewRecorder()
	allowedRequest := httptest.NewRequest(http.MethodPost, "/http", bytes.NewReader(allowedBody))
	allowedRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(allowed, allowedRequest)
	if allowed.Code != http.StatusOK || !bytes.Contains(allowed.Body.Bytes(), []byte(`"status_code":201`)) || !bytes.Contains(allowed.Body.Bytes(), []byte(`"body":"ready"`)) {
		t.Fatalf("loopback response = %d: %s", allowed.Code, allowed.Body.String())
	}

	publicBody, _ := json.Marshal(diagnosticHTTPRequest{Method: http.MethodGet, URL: "https://8.8.8.8/"})
	rejected := httptest.NewRecorder()
	rejectedRequest := httptest.NewRequest(http.MethodPost, "/http", bytes.NewReader(publicBody))
	rejectedRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rejected, rejectedRequest)
	if rejected.Code != http.StatusBadRequest || !bytes.Contains(rejected.Body.Bytes(), []byte(`diagnostic_target_rejected`)) {
		t.Fatalf("public response = %d: %s", rejected.Code, rejected.Body.String())
	}
}

func TestDiagnosticRoutesAllowOnlyAdminSessionOrAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		credential := panelauth.CredentialAPIKey
		role := RoleAdmin
		if c.GetHeader("X-Test-Session") == "yes" {
			credential = panelauth.CredentialSession
		}
		if c.GetHeader("X-Test-Role") != "" {
			role = Role(c.GetHeader("X-Test-Role"))
		}
		c.Set(principalKey, Principal{Name: "test", Role: role, Credential: credential})
	})
	router.GET("/diagnostics", RequireDiagnosticHTTPAdmin(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	apiKey := httptest.NewRecorder()
	router.ServeHTTP(apiKey, httptest.NewRequest(http.MethodGet, "/diagnostics", nil))
	if apiKey.Code != http.StatusNoContent {
		t.Fatalf("API key status = %d: %s", apiKey.Code, apiKey.Body.String())
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "/diagnostics", nil)
	sessionRequest.Header.Set("X-Test-Session", "yes")
	session := httptest.NewRecorder()
	router.ServeHTTP(session, sessionRequest)
	if session.Code != http.StatusNoContent {
		t.Fatalf("session status = %d: %s", session.Code, session.Body.String())
	}

	for _, role := range []Role{RoleOperator, RoleViewer} {
		rejected := httptest.NewRecorder()
		rejectedRequest := httptest.NewRequest(http.MethodGet, "/diagnostics", nil)
		rejectedRequest.Header.Set("X-Test-Role", string(role))
		router.ServeHTTP(rejected, rejectedRequest)
		if rejected.Code != http.StatusForbidden || !bytes.Contains(rejected.Body.Bytes(), []byte(`diagnostic_admin_required`)) {
			t.Fatalf("%s API key status = %d: %s", role, rejected.Code, rejected.Body.String())
		}
	}
}

func TestInteractiveAdminStillRejectsAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		credential := panelauth.CredentialAPIKey
		if c.GetHeader("X-Test-Session") == "yes" {
			credential = panelauth.CredentialSession
		}
		c.Set(principalKey, Principal{Name: "admin", Role: RoleAdmin, Credential: credential})
	})
	router.POST("/shell", RequireInteractiveAdmin(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	apiKey := httptest.NewRecorder()
	router.ServeHTTP(apiKey, httptest.NewRequest(http.MethodPost, "/shell", nil))
	if apiKey.Code != http.StatusForbidden || !bytes.Contains(apiKey.Body.Bytes(), []byte(`interactive_admin_required`)) {
		t.Fatalf("API key shell status = %d: %s", apiKey.Code, apiKey.Body.String())
	}

	sessionRequest := httptest.NewRequest(http.MethodPost, "/shell", nil)
	sessionRequest.Header.Set("X-Test-Session", "yes")
	session := httptest.NewRecorder()
	router.ServeHTTP(session, sessionRequest)
	if session.Code != http.StatusNoContent {
		t.Fatalf("session shell status = %d: %s", session.Code, session.Body.String())
	}
}

func TestDiagnosticHTTPAPIKeyRestrictionsAndSessionCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("inbound panel Authorization header was forwarded")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://127.0.0.1/", http.StatusFound)
	}))
	defer redirectTarget.Close()

	run := func(credential panelauth.Credential, method, targetURL string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(diagnosticHTTPRequest{Method: method, URL: targetURL})
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set(principalKey, Principal{Name: "admin", Role: RoleAdmin, Credential: credential})
		})
		router.POST("/http", Server{}.runDiagnosticHTTP)
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/http", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer panel-api-key")
		router.ServeHTTP(recorder, request)
		return recorder
	}

	allowed := run(panelauth.CredentialAPIKey, http.MethodGet, target.URL)
	if allowed.Code != http.StatusOK {
		t.Fatalf("admin API key HTTP status = %d: %s", allowed.Code, allowed.Body.String())
	}
	methodRejected := run(panelauth.CredentialAPIKey, http.MethodPut, target.URL)
	if methodRejected.Code != http.StatusForbidden || !bytes.Contains(methodRejected.Body.Bytes(), []byte(`diagnostic_api_key_method_restricted`)) {
		t.Fatalf("API key PUT status = %d: %s", methodRejected.Code, methodRejected.Body.String())
	}
	schemeRejected := run(panelauth.CredentialAPIKey, http.MethodGet, "https://127.0.0.1/")
	if schemeRejected.Code != http.StatusForbidden || !bytes.Contains(schemeRejected.Body.Bytes(), []byte(`diagnostic_api_key_scheme_restricted`)) {
		t.Fatalf("API key HTTPS status = %d: %s", schemeRejected.Code, schemeRejected.Body.String())
	}
	redirectRejected := run(panelauth.CredentialAPIKey, http.MethodGet, redirectTarget.URL)
	if redirectRejected.Code != http.StatusBadGateway || !bytes.Contains(redirectRejected.Body.Bytes(), []byte(`only allow http redirect targets`)) {
		t.Fatalf("API key HTTPS redirect status = %d: %s", redirectRejected.Code, redirectRejected.Body.String())
	}
	session := run(panelauth.CredentialSession, http.MethodPut, "https://127.0.0.1:1/")
	if session.Code == http.StatusBadRequest || bytes.Contains(session.Body.Bytes(), []byte(`diagnostic_api_key_`)) {
		t.Fatalf("session HTTPS PUT was rejected by API-key restriction: %d: %s", session.Code, session.Body.String())
	}
}

func TestDiagnosticShellRequiresServerOptInAndConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/disabled", Server{}.runDiagnosticShell)
	router.POST("/enabled", Server{cfg: appconfig.Config{DiagnosticShellEnabled: true}}.runDiagnosticShell)

	disabled := httptest.NewRecorder()
	disabledRequest := httptest.NewRequest(http.MethodPost, "/disabled", bytes.NewBufferString(`{"command":"echo ready","confirm":true}`))
	disabledRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(disabled, disabledRequest)
	if disabled.Code != http.StatusForbidden || !bytes.Contains(disabled.Body.Bytes(), []byte(`diagnostic_shell_disabled`)) {
		t.Fatalf("disabled response = %d: %s", disabled.Code, disabled.Body.String())
	}

	unconfirmed := httptest.NewRecorder()
	unconfirmedRequest := httptest.NewRequest(http.MethodPost, "/enabled", bytes.NewBufferString(`{"command":"echo ready","confirm":false}`))
	unconfirmedRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(unconfirmed, unconfirmedRequest)
	if unconfirmed.Code != http.StatusBadRequest || !bytes.Contains(unconfirmed.Body.Bytes(), []byte(`diagnostic_confirmation_required`)) {
		t.Fatalf("unconfirmed response = %d: %s", unconfirmed.Code, unconfirmed.Body.String())
	}
}
