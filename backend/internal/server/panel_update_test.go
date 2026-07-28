package server

import (
	"archive/tar"
	"compress/gzip"
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
