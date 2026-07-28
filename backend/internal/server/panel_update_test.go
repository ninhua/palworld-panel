package server

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePanelUpdateRequest(t *testing.T) {
	request, err := normalizePanelUpdateRequest(PanelUpdateRequest{CurrentVersion: "v1.3.0-custom.0.8.18"})
	if err != nil {
		t.Fatal(err)
	}
	if request.Repository != panelUpdateRepository {
		t.Fatalf("repository = %q", request.Repository)
	}
	for _, repository := range []string{"../unsafe", "owner/..", "owner/repo/extra"} {
		if _, err := normalizePanelUpdateRequest(PanelUpdateRequest{CurrentVersion: "v1.3.0-custom.0.8.18", Repository: repository}); err == nil {
			t.Fatalf("expected repository %q to be rejected", repository)
		}
	}
}

func TestComparePanelVersions(t *testing.T) {
	if comparePanelVersions("v1.4.0-custom.0.1.0", "v1.3.0-custom.9.9.9") <= 0 {
		t.Fatal("newer upstream version should win")
	}
	if comparePanelVersions("v1.3.0-custom.0.8.19", "v1.3.0-custom.0.8.18") <= 0 {
		t.Fatal("newer custom version should win")
	}
}

func TestPanelReleaseFromLatestURLBuildsRateLimitFreeAssets(t *testing.T) {
	finalURL, err := url.Parse("https://github.com/ninhua/palworld-panel/releases/tag/v1.3.0-custom.0.8.20")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := panelReleaseFromLatestURL("ninhua/palworld-panel", finalURL)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Release.TagName != "v1.3.0-custom.0.8.20" {
		t.Fatalf("tag = %q", selection.Release.TagName)
	}
	if selection.Archive.Name != "palpanel_v1.3.0-custom.0.8.20_linux_amd64.tar.gz" {
		t.Fatalf("archive = %#v", selection.Archive)
	}
	if selection.Checksums.BrowserDownloadURL == "" {
		t.Fatal("checksums URL is empty")
	}
}

func TestResolvePanelReleaseFallsBackWhenGitHubAPIRateLimited(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/repos/ninhua/palworld-panel/releases", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	})
	mux.HandleFunc("/ninhua/palworld-panel/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/ninhua/palworld-panel/releases/tag/v1.3.0-custom.0.8.20", http.StatusFound)
	})
	mux.HandleFunc("/ninhua/palworld-panel/releases/tag/v1.3.0-custom.0.8.20", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	previousAPI, previousWeb := panelGitHubAPIBaseURL, panelGitHubWebBaseURL
	panelGitHubAPIBaseURL, panelGitHubWebBaseURL = server.URL, server.URL
	defer func() {
		panelGitHubAPIBaseURL, panelGitHubWebBaseURL = previousAPI, previousWeb
	}()

	manager := Manager{downloadClient: server.Client()}
	selection, err := manager.resolvePanelRelease(context.Background(), PanelUpdateRequest{Repository: "ninhua/palworld-panel"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Release.TagName != "v1.3.0-custom.0.8.20" {
		t.Fatalf("tag = %q", selection.Release.TagName)
	}
	if selection.Archive.BrowserDownloadURL != server.URL+"/ninhua/palworld-panel/releases/download/v1.3.0-custom.0.8.20/palpanel_v1.3.0-custom.0.8.20_linux_amd64.tar.gz" {
		t.Fatalf("archive URL = %q", selection.Archive.BrowserDownloadURL)
	}
}

func TestExtractPanelBinary(t *testing.T) {
	temp := t.TempDir()
	archivePath := filepath.Join(temp, "panel.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	body := []byte("panel")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "palpanel_v1/bin/palpanel", Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(body); err != nil {
		t.Fatal(err)
	}
	_ = tarWriter.Close()
	_ = gzipWriter.Close()
	_ = file.Close()
	destination := filepath.Join(temp, "palpanel")
	if err := extractPanelBinary(archivePath, destination); err != nil {
		t.Fatal(err)
	}
}
