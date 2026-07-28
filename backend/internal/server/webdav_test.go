package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/jobs"
)

func TestWebDAVConfigTestAndBackupUpload(t *testing.T) {
	var mu sync.Mutex
	uploads := map[string]string{}
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Basic "+base64.StdEncoding.EncodeToString([]byte("panel:secret")) {
			t.Errorf("Authorization = %q", got)
		}
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.Method {
		case "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
		case "MKCOL":
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			uploads[r.URL.Path] = string(body)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	manager, cleanup := newWebDAVTestManager(t)
	defer cleanup()
	baseURL := server.URL
	username := "panel"
	password := "secret"
	remotePath := "PalPanel/server-01"
	enabled := true
	autoUpload := true
	public, err := manager.UpdateWebDAVConfig(t.Context(), WebDAVConfigUpdate{
		Enabled: &enabled, BaseURL: &baseURL, Username: &username, Password: &password,
		RemotePath: &remotePath, UploadAfterBackup: &autoUpload,
	})
	if err != nil {
		t.Fatalf("UpdateWebDAVConfig returned error: %v", err)
	}
	if !public.Enabled || !public.UploadAfterBackup || !public.PasswordConfigured {
		t.Fatalf("public config = %#v", public)
	}
	encoded, _ := json.Marshal(public)
	if strings.Contains(string(encoded), password) {
		t.Fatalf("public config leaked password: %s", encoded)
	}
	if err := manager.TestWebDAV(t.Context(), WebDAVConfigUpdate{}); err != nil {
		t.Fatalf("TestWebDAV returned error: %v", err)
	}

	backupName := "palpanel-manual-test.zip"
	backupBody := "fixture-backup"
	if err := os.WriteFile(filepath.Join(manager.cfg.BackupsDir, backupName), []byte(backupBody), 0o600); err != nil {
		t.Fatal(err)
	}
	job, err := manager.UploadBackupToWebDAV(t.Context(), backupName)
	if err != nil {
		t.Fatalf("UploadBackupToWebDAV returned error: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		job, err = manager.store.GetJob(t.Context(), job.ID)
		if err == nil && (job.Status == "completed" || job.Status == "failed") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("upload job did not finish: %#v, %v", job, err)
		}
		time.Sleep(time.Millisecond)
	}
	if job.Status != "completed" {
		t.Fatalf("upload job = %#v", job)
	}
	mu.Lock()
	gotUpload := uploads["/PalPanel/server-01/"+backupName]
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	if gotUpload != backupBody {
		t.Fatalf("uploaded body = %q, requests = %#v", gotUpload, gotRequests)
	}

	configInfo, err := os.Stat(manager.webDAVConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && configInfo.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o", configInfo.Mode().Perm())
	}
	configBody, err := os.ReadFile(manager.webDAVConfigPath())
	if err != nil || !strings.Contains(string(configBody), password) {
		t.Fatalf("private config was not persisted: %v", err)
	}
}

func TestWebDAVConfigValidationAndPasswordLifecycle(t *testing.T) {
	manager, cleanup := newWebDAVTestManager(t)
	defer cleanup()
	enabled := true
	plainHTTP := "http://example.com/dav"
	if _, err := manager.UpdateWebDAVConfig(t.Context(), WebDAVConfigUpdate{Enabled: &enabled, BaseURL: &plainHTTP}); err != nil {
		t.Fatalf("public HTTP WebDAV URL should be accepted: %v", err)
	}
	privateHTTP := "http://192.168.1.20/dav"
	disabled := false
	if _, err := manager.UpdateWebDAVConfig(t.Context(), WebDAVConfigUpdate{Enabled: &disabled, BaseURL: &privateHTTP}); err != nil {
		t.Fatalf("private NAS HTTP URL should be accepted: %v", err)
	}
	loopback := "http://127.0.0.1:9000/dav"
	unsafePath := "PalPanel/../escape"
	if _, err := manager.UpdateWebDAVConfig(t.Context(), WebDAVConfigUpdate{Enabled: &enabled, BaseURL: &loopback, RemotePath: &unsafePath}); err == nil {
		t.Fatal("expected unsafe remote path to be rejected")
	}
	password := "keep-me"
	username := "panel"
	remotePath := "PalPanel"
	if _, err := manager.UpdateWebDAVConfig(t.Context(), WebDAVConfigUpdate{Enabled: &enabled, BaseURL: &loopback, Username: &username, Password: &password, RemotePath: &remotePath}); err != nil {
		t.Fatal(err)
	}
	empty := ""
	public, err := manager.UpdateWebDAVConfig(t.Context(), WebDAVConfigUpdate{Password: &empty})
	if err != nil || !public.PasswordConfigured {
		t.Fatalf("empty password should preserve secret: %#v, %v", public, err)
	}
	public, err = manager.UpdateWebDAVConfig(t.Context(), WebDAVConfigUpdate{ClearPassword: true})
	if err != nil || public.PasswordConfigured {
		t.Fatalf("clear password failed: %#v, %v", public, err)
	}
}

func newWebDAVTestManager(t *testing.T) (Manager, func()) {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupsDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(filepath.Join(dataDir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	executor := jobs.New(store, 2)
	manager := Manager{
		cfg: appconfig.Config{
			RuntimeRoot: root,
			DataDir:     dataDir,
			BackupsDir:  backupsDir,
		},
		store:       store,
		jobs:        executor,
		operationMu: &sync.Mutex{},
	}
	cleanup := func() {
		_ = executor.Shutdown(context.Background())
		_ = store.Close()
	}
	return manager, cleanup
}
