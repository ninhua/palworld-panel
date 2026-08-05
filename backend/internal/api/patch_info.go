package api

import (
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/buildinfo"
)

const (
	patchSourceRepository = "uitok/palworld-panel"
	patchSourceRef        = "v1.3.1"
	patchTargetVersion    = "v1.3.1"
	patchVersion          = "0.8.91"
	panelRepository       = "ninhua/palworld-panel"
)

var patchFeatures = []string{"patch-info-api", "base-custom-names", "base-storage-browser", "player-notes", "guild-detail-browser", "base-worker-browser", "base-feed-box-summary", "insecure-endpoint-support", "panel-self-update", "external-package-updater", "exec-hot-updater", "startup-health-rollback", "audit-log-response-display", "player-presence-history", "host-save-migrator", "diagnostic-console", "config-revision-history", "save-history-diff", "crash-loop-guard", "incident-center", "signed-incident-webhook", "uid-remap-custom-version-sentinel", "palops-offline-map", "palops-map-poi", "self-hosted-maplibre", "offline-vendor-mirror", "diagnostic-health-checks", "redacted-support-bundle", "maplibre-raster-fallback", "save-history-response-normalization", "panel-version-display", "palops-resource-layout-discovery", "maplibre-v5-webgl-fallback", "async-host-save-migration", "semantic-save-history-events", "distinct-map-marker-icons", "save-snapshot-pal-tracking", "task-management-ui", "task-template-presets", "player-task-progress-browser", "generated-api-contract-sync", "patch-version-contract-guard", "task-event-type-select", "shop-item-catalog-selector", "shop-pal-template-selector", "shop-payload-builder", "task-chinese-display-labels", "boss-management-ui", "boss-catalog-selectors", "boss-summon-audit-ui", "boss-wave-editor", "boss-wave-snapshot-ledger", "boss-wave-progress-ui", "diagnostic-console-history", "diagnostic-console-templates", "diagnostic-console-copy", "diagnostic-structured-header-editor", "diagnostic-common-header-presets", "diagnostic-bearer-prefix", "boss-paldefender-warning-broadcast", "boss-warning-test-action", "boss-warning-delivery-audit", "checkin-streak-policy", "configurable-checkin-aliases", "bare-game-command-aliases", "paldefender-log-event-bridge", "game-event-bridge-diagnostics", "checkin-task-event-derivation", "diagnostic-header-json-tree", "diagnostic-custom-template-storage", "diagnostic-history-request-dedup", "diagnostic-history-full-value-storage", "diagnostic-response-json-formatting", "diagnostic-response-json-tree", "diagnostic-console-layout-refresh", "diagnostic-header-editor-tabbed-layout", "task-event-dry-run-diagnostics", "task-event-replay", "task-match-reason-reporting", "game-event-bridge-persistent-cursors", "game-event-bridge-rotation-recovery", "game-event-bridge-dead-letters", "game-event-bridge-dead-letter-replay", "player-task-chat-command", "game-task-progress-integration", "online-task-minute-sampler", "online-task-persistent-tracking", "online-task-offline-settlement", "boss-rcon-palsummon-executor", "boss-execution-attempt-ledger", "boss-execution-runtime-lock", "boss-execution-reconciliation", "save-migration-wizard-entry", "upstream-save-migration-primary-ui", "palpanel-bridge-runtime-diagnostics", "china-timezone-default", "patch-feature-deduplication"}

func normalizePatchFeatures(features []string) []string {
	result := make([]string, 0, len(features))
	seen := make(map[string]struct{}, len(features))
	for _, feature := range features {
		feature = strings.TrimSpace(feature)
		if feature == "" {
			continue
		}
		if _, exists := seen[feature]; exists {
			continue
		}
		seen[feature] = struct{}{}
		result = append(result, feature)
	}
	return result
}

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
			"features":   normalizePatchFeatures(patchFeatures),
		},
		"build": info,
	})
}
