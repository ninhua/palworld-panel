package astrbotclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"palpanel/internal/appconfig"
)

func TestClientAllowsHTTPAstrBotEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := New(appconfig.Config{
		AstrBotPluginURL:    server.URL,
		AstrBotPanelID:      "panel",
		AstrBotSharedSecret: "secret",
	})
	if _, err := client.post(context.Background(), "/v1/test", map[string]any{"ok": true}); err != nil {
		t.Fatalf("HTTP AstrBot endpoint should be accepted: %v", err)
	}
}

func TestClientRejectsUnsupportedAstrBotScheme(t *testing.T) {
	client := New(appconfig.Config{
		AstrBotPluginURL:    "ftp://example.com/plugin",
		AstrBotPanelID:      "panel",
		AstrBotSharedSecret: "secret",
	})
	if _, err := client.post(context.Background(), "/v1/test", map[string]any{}); err == nil {
		t.Fatal("expected unsupported AstrBot URL scheme to be rejected")
	}
}

func TestVerifyRejectsTamperingAndExpiredTimestamp(t *testing.T) {
	body := []byte(`{"qq_id":"10001"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "nonce"
	supplied := sign("secret", "POST", "/v1/test", timestamp, nonce, body)
	if !Verify("secret", "POST", "/v1/test", timestamp, nonce, supplied, body) {
		t.Fatal("valid signature was rejected")
	}
	if Verify("secret", "POST", "/v1/test", timestamp, nonce, supplied, append(body, 'x')) {
		t.Fatal("tampered body was accepted")
	}
	expired := strconv.FormatInt(time.Now().Add(-2*time.Minute).Unix(), 10)
	if Verify("secret", "POST", "/v1/test", expired, nonce, sign("secret", "POST", "/v1/test", expired, nonce, body), body) {
		t.Fatal("expired timestamp was accepted")
	}
}
