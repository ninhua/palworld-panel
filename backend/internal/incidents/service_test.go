package incidents

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
)

func TestWebhookTestIsSignedAndDoesNotExposeSecret(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	var receivedBody []byte
	var receivedTimestamp, receivedSignature string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTimestamp = r.Header.Get("X-PalPanel-Timestamp")
		receivedSignature = r.Header.Get("X-PalPanel-Signature")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	store, err := db.Open(filepath.Join(t.TempDir(), "incidents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := New(appconfig.Config{IncidentWebhookURL: server.URL, IncidentWebhookSecret: secret, IncidentWebhookTimeoutSeconds: 5}, store)
	service.now = func() time.Time { return time.Date(2026, 7, 29, 13, 0, 0, 0, time.UTC) }
	if err := service.Test(t.Context()); err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(receivedTimestamp + "."))
	_, _ = mac.Write(receivedBody)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(receivedSignature), []byte(want)) {
		t.Fatalf("signature = %q, want %q", receivedSignature, want)
	}
	if strings.Contains(string(receivedBody), secret) || !strings.Contains(string(receivedBody), "palpanel.incident.test") {
		t.Fatalf("unexpected webhook body: %s", receivedBody)
	}
}

func TestWebhookRejectsRedirects(t *testing.T) {
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/elsewhere", http.StatusFound)
	}))
	defer redirect.Close()
	store, err := db.Open(filepath.Join(t.TempDir(), "incidents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := New(appconfig.Config{IncidentWebhookURL: redirect.URL, IncidentWebhookSecret: "0123456789abcdef", IncidentWebhookTimeoutSeconds: 5}, store)
	if err := service.Test(t.Context()); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
}

func TestWebhookTransportErrorDoesNotExposeTargetURL(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "incidents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	target := "http://127.0.0.1:1/private-hook-name"
	service := New(appconfig.Config{IncidentWebhookURL: target, IncidentWebhookSecret: "0123456789abcdef", IncidentWebhookTimeoutSeconds: 1}, store)
	err = service.Test(t.Context())
	if err == nil {
		t.Fatal("expected connection failure")
	}
	if strings.Contains(err.Error(), "private-hook-name") || strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("target URL leaked through error: %v", err)
	}
}
