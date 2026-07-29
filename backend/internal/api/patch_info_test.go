package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"palpanel/internal/buildinfo"
)

func TestPatchInfo(t *testing.T) {
	previous := buildinfo.Current()
	buildinfo.Version = "v1.3.0-test"
	buildinfo.Commit = "5e3c0bce9d33091b3261f82b3e4da062fc35a8a1"
	buildinfo.BuildTime = "2026-07-23T17:13:44+08:00"
	t.Cleanup(func() {
		buildinfo.Version = previous.Version
		buildinfo.Commit = previous.Commit
		buildinfo.BuildTime = previous.BuildTime
	})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	server := Server{}
	router.GET("/api/patch/info", server.patchInfo)

	request := httptest.NewRequest(http.MethodGet, "/api/patch/info", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/patch/info returned %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Upstream struct {
				Repository string `json:"repository"`
				Ref        string `json:"ref"`
				Commit     string `json:"commit"`
			} `json:"upstream"`
			Compatibility struct {
				TargetVersion string `json:"target_version"`
				Verified      bool   `json:"verified"`
			} `json:"compatibility"`
			Patch struct {
				Version    string   `json:"version"`
				Repository string   `json:"repository"`
				Features   []string `json:"features"`
			} `json:"patch"`
			Build buildinfo.Info `json:"build"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.OK {
		t.Fatal("expected ok response")
	}
	if response.Data.Upstream.Repository != patchSourceRepository || response.Data.Upstream.Ref != patchSourceRef {
		t.Fatalf("unexpected upstream: %+v", response.Data.Upstream)
	}
	if response.Data.Upstream.Commit != buildinfo.Commit {
		t.Fatalf("upstream commit = %q, want %q", response.Data.Upstream.Commit, buildinfo.Commit)
	}
	if response.Data.Compatibility.TargetVersion != patchTargetVersion || !response.Data.Compatibility.Verified {
		t.Fatalf("unexpected compatibility: %+v", response.Data.Compatibility)
	}
	if response.Data.Patch.Version != patchVersion || response.Data.Patch.Repository != panelRepository {
		t.Fatalf("unexpected patch metadata: %+v", response.Data.Patch)
	}
	features := make(map[string]bool, len(response.Data.Patch.Features))
	for _, feature := range response.Data.Patch.Features {
		features[feature] = true
	}
	for _, expected := range []string{"patch-info-api", "base-custom-names", "base-storage-browser", "player-notes", "guild-detail-browser", "base-worker-browser", "base-feed-box-summary", "insecure-endpoint-support", "panel-self-update", "external-package-updater", "exec-hot-updater", "startup-health-rollback", "audit-log-response-display", "player-presence-history", "host-save-migrator", "diagnostic-console", "config-revision-history", "save-history-diff", "crash-loop-guard", "incident-center", "signed-incident-webhook"} {
		if !features[expected] {
			t.Fatalf("missing feature %q in %#v", expected, response.Data.Patch.Features)
		}
	}
	if response.Data.Build.Version != buildinfo.Version || response.Data.Build.BuildTime != buildinfo.BuildTime {
		t.Fatalf("unexpected build metadata: %+v", response.Data.Build)
	}
}
