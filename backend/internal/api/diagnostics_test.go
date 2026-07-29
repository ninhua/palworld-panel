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

func TestDiagnosticRoutesRequireInteractiveAdminSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		credential := panelauth.CredentialAPIKey
		if c.GetHeader("X-Test-Session") == "yes" {
			credential = panelauth.CredentialSession
		}
		c.Set(principalKey, Principal{Name: "admin", Role: RoleAdmin, Credential: credential})
	})
	router.GET("/diagnostics", RequireInteractiveAdmin(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	apiKey := httptest.NewRecorder()
	router.ServeHTTP(apiKey, httptest.NewRequest(http.MethodGet, "/diagnostics", nil))
	if apiKey.Code != http.StatusForbidden {
		t.Fatalf("API key status = %d: %s", apiKey.Code, apiKey.Body.String())
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "/diagnostics", nil)
	sessionRequest.Header.Set("X-Test-Session", "yes")
	session := httptest.NewRecorder()
	router.ServeHTTP(session, sessionRequest)
	if session.Code != http.StatusNoContent {
		t.Fatalf("session status = %d: %s", session.Code, session.Body.String())
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
