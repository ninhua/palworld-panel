package api

import (
	"github.com/gin-gonic/gin"

	"palpanel/internal/buildinfo"
)

const (
	patchSourceRepository = "uitok/palworld-panel"
	patchSourceRef        = "v1.3.0"
	patchTargetVersion    = "v1.3.0"
	patchVersion          = "0.8.60"
	panelRepository       = "ninhua/palworld-panel"
)

var patchFeatures = []string{"patch-info-api", "base-custom-names", "base-storage-browser", "player-notes", "guild-detail-browser", "base-worker-browser", "base-feed-box-summary", "insecure-endpoint-support", "panel-self-update", "external-package-updater", "exec-hot-updater", "startup-health-rollback", "audit-log-response-display", "player-presence-history", "host-save-migrator", "diagnostic-console", "config-revision-history", "save-history-diff", "crash-loop-guard", "incident-center", "signed-incident-webhook", "uid-remap-custom-version-sentinel", "palops-offline-map", "palops-map-poi", "self-hosted-maplibre", "offline-vendor-mirror", "diagnostic-health-checks", "redacted-support-bundle", "maplibre-raster-fallback", "save-history-response-normalization", "panel-version-display", "palops-resource-layout-discovery", "maplibre-v5-webgl-fallback", "async-host-save-migration", "semantic-save-history-events", "distinct-map-marker-icons", "save-snapshot-pal-tracking", "task-management-ui", "task-template-presets", "player-task-progress-browser", "generated-api-contract-sync", "patch-version-contract-guard"}

func (s Server) patchInfo(c *gin.Context) {
	info := buildinfo.Current()
	ok(c, gin.H{
		"upstream": gin.H{
			"repository": patchSourceRepository,
			"ref":        patchSourceRef,
			"commit":     info.Commit,
		},
		"compatibility": gin.H{
			"target_version": patchTargetVersion,
			"verified":       true,
		},
		"patch": gin.H{
			"version":    patchVersion,
			"repository": panelRepository,
			"features":   patchFeatures,
		},
		"build": info,
	})
}
