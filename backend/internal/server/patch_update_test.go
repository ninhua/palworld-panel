package server

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComparePatchVersions(t *testing.T) {
	cases := []struct {
		left  string
		right string
		want  int
	}{
		{left: "0.8.3", right: "0.8.2", want: 1},
		{left: "0.8.2", right: "0.8.2", want: 0},
		{left: "0.8.9", right: "0.8.10", want: -1},
	}
	for _, test := range cases {
		if got := comparePatchVersions(test.left, test.right); got != test.want {
			t.Fatalf("comparePatchVersions(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestParsePatchChecksumsAcceptsGNUMarkers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SHA256SUMS")
	digest := strings.Repeat("a", 64)
	body := digest + " *manifest.json\n" + strings.Repeat("b", 64) + "  ./archive.tar.gz\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	checksums, err := parsePatchChecksums(path)
	if err != nil {
		t.Fatal(err)
	}
	if checksums["manifest.json"] != digest || checksums["archive.tar.gz"] != strings.Repeat("b", 64) {
		t.Fatalf("unexpected checksums: %#v", checksums)
	}
}

func TestValidatePatchManifest(t *testing.T) {
	manifest := patchReleaseManifest{
		SchemaVersion: 1,
		Project:       "uitok-palworld-panel",
		PatchVersion:  "0.8.3",
		PatchType:     "source-build",
		Platforms:     []string{"linux-amd64"},
		Features:      []string{"patch-info-api", "panel-patch-hot-update"},
		Files: map[string]struct {
			OriginalSHA256 string `json:"original_sha256"`
			PatchedSHA256  string `json:"patched_sha256"`
		}{"bin/palpanel": {PatchedSHA256: strings.Repeat("a", 64)}},
	}
	manifest.Upstream.Repository = "uitok/palworld-panel"
	manifest.Upstream.Version = "v1.3.0"
	manifest.Compatibility.Mode = "exact"
	manifest.Compatibility.TargetVersion = "v1.3.0"
	manifest.Compatibility.Verified = true
	request := PatchUpdateRequest{TargetVersion: "v1.3.0", CurrentPatchVersion: "0.8.2", Repository: patchUpdateRepository, RequiredFeature: "panel-patch-hot-update"}
	if err := validatePatchManifest(manifest, request, "0.8.3"); err != nil {
		t.Fatal(err)
	}
}

func TestExtractPatchBinaryRejectsTraversalAndFindsBinary(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "patch.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gz)
	body := []byte("patched-binary")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "package/overlay/bin/palpanel", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "palpanel")
	if err := extractPatchBinary(archivePath, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("extracted body = %q", got)
	}
}

func TestPatchSelectionFromPublishedWorkspace(t *testing.T) {
	request := PatchUpdateRequest{
		TargetVersion:       "v1.3.0",
		CurrentPatchVersion: "0.8.4",
		Repository:          patchUpdateRepository,
		RequiredFeature:     "panel-patch-hot-update",
	}
	selection, err := patchSelectionFromWorkspace(request, patchPublishedWorkspace{
		SchemaVersion: 2,
		TargetVersion: "v1.3.0",
		State:         "released",
		Verified:      true,
		ReleaseTag:    "uitok-stable-v1.3.0-p0.8.5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Version != "0.8.5" || selection.Release.TagName != "uitok-stable-v1.3.0-p0.8.5" {
		t.Fatalf("unexpected selection: %#v", selection)
	}
	wantArchive := "uitok-palworld-panel_stable-v1.3.0_patch-0.8.5_linux-amd64.tar.gz"
	if selection.Archive.Name != wantArchive || !strings.HasSuffix(selection.Archive.BrowserDownloadURL, "/"+wantArchive) {
		t.Fatalf("unexpected archive: %#v", selection.Archive)
	}
	if selection.Manifest.Name != "manifest.json" || selection.Checksums.Name != "SHA256SUMS" {
		t.Fatalf("unexpected metadata assets: %#v %#v", selection.Manifest, selection.Checksums)
	}
}

func TestPatchSelectionRejectsUnverifiedWorkspace(t *testing.T) {
	request := PatchUpdateRequest{TargetVersion: "v1.3.0", Repository: patchUpdateRepository}
	_, err := patchSelectionFromWorkspace(request, patchPublishedWorkspace{
		SchemaVersion: 2,
		TargetVersion: "v1.3.0",
		State:         "releasable",
		Verified:      false,
		ReleaseTag:    "uitok-stable-v1.3.0-p0.8.6",
	})
	if err == nil {
		t.Fatal("expected unverified workspace to be rejected")
	}
}

func TestSetPatchRequestHeadersUsesDeploymentTokenAlias(t *testing.T) {
	t.Setenv("PALPANEL_PANEL_PATCH_GITHUB_TOKEN", "deployment-token")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/example/repo/releases", nil)
	if err != nil {
		t.Fatal(err)
	}
	setPatchRequestHeaders(req)
	if got := req.Header.Get("Authorization"); got != "Bearer deployment-token" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestNormalizePatchUpdateRequest(t *testing.T) {
	request, err := normalizePatchUpdateRequest(PatchUpdateRequest{
		TargetVersion:       " v1.3.0 ",
		CurrentPatchVersion: " 0.8.18 ",
		RequiredFeature:     " patch-info-api ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Repository != patchUpdateRepository ||
		request.TargetVersion != "v1.3.0" ||
		request.CurrentPatchVersion != "0.8.18" ||
		request.RequiredFeature != "patch-info-api" {
		t.Fatalf("unexpected normalized request: %#v", request)
	}

	for _, test := range []PatchUpdateRequest{
		{TargetVersion: "1.3.0", CurrentPatchVersion: "0.8.18"},
		{TargetVersion: "v1.3.0", CurrentPatchVersion: "latest"},
		{TargetVersion: "v1.3.0", CurrentPatchVersion: "0.8.18", Repository: "../unsafe"},
	} {
		if _, err := normalizePatchUpdateRequest(test); err == nil {
			t.Fatalf("expected request to be rejected: %#v", test)
		}
	}
}

func TestPatchRequestURLsAndHeaders(t *testing.T) {
	t.Setenv("PALPANEL_PATCH_GITHUB_PROXY", "https://proxy.example/")
	t.Setenv("GH_PROXY_BASE", "ftp://ignored.example")
	t.Setenv("GH_PROXY_FALLBACK", "https://proxy.example")
	t.Setenv("PALPANEL_PANEL_PATCH_GITHUB_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", " github-token ")
	t.Setenv("GH_TOKEN", "")

	endpoint := "https://github.com/example/archive.tar.gz"
	urls := patchRequestURLs(endpoint)
	if len(urls) != 2 || urls[0] != "https://proxy.example/"+endpoint || urls[1] != endpoint {
		t.Fatalf("unexpected request URLs: %#v", urls)
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	setPatchRequestHeaders(req)
	if got := req.Header.Get("Authorization"); got != "Bearer github-token" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != "PalPanel-patch-hot-update" {
		t.Fatalf("User-Agent = %q", got)
	}
}

func TestGetPatchJSONAndDownloadAsset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/metadata":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"schema_version":2,"target_version":"v1.3.0","state":"released","verified":true}`))
		case "/asset":
			_, _ = writer.Write([]byte("patched asset"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	manager := Manager{downloadClient: server.Client()}
	var workspace patchPublishedWorkspace
	if err := manager.getPatchJSON(context.Background(), server.URL+"/metadata", &workspace); err != nil {
		t.Fatal(err)
	}
	if workspace.SchemaVersion != 2 || workspace.TargetVersion != "v1.3.0" || !workspace.Verified {
		t.Fatalf("unexpected workspace: %#v", workspace)
	}

	destination := filepath.Join(t.TempDir(), "asset")
	if err := manager.downloadPatchAsset(context.Background(), server.URL+"/asset", destination, 64); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "patched asset" {
		t.Fatalf("downloaded body = %q", body)
	}
	if err := manager.downloadPatchAsset(context.Background(), server.URL+"/asset", destination, 4); err == nil {
		t.Fatal("expected oversized download to fail")
	}
}

func TestPatchFileHelpersAndRestartMarker(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source")
	destination := filepath.Join(directory, "destination")
	if err := os.WriteFile(source, []byte("panel binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyPatchFile(source, destination, 0o755); err != nil {
		t.Fatal(err)
	}
	digest, err := sha256PatchFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !validPatchSHA256(digest) || validPatchSHA256("not-a-digest") {
		t.Fatalf("unexpected digest validation: %q", digest)
	}
	if err := verifyNamedPatchFile(destination, "palpanel", map[string]string{"palpanel": digest}); err != nil {
		t.Fatal(err)
	}

	marker := patchRestartMarker{
		JobID:          "job-1",
		BinaryPath:     destination,
		BackupPath:     source,
		ExpectedSHA256: digest,
	}
	if err := writePatchRestartMarker(marker); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(patchRestartMarkerPath(destination)); err != nil {
		t.Fatal(err)
	}
	if err := removePatchRestartMarker(destination); err != nil {
		t.Fatal(err)
	}
	if err := removePatchRestartMarker(destination); err != nil {
		t.Fatal(err)
	}
}
