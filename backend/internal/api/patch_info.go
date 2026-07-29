package api

import (
	"github.com/gin-gonic/gin"

	"palpanel/internal/buildinfo"
)

const (
	patchSourceRepository = "uitok/palworld-panel"
	patchSourceRef        = "v1.3.0"
	patchTargetVersion    = "v1.3.0"
	patchVersion          = "0.8.28"
	panelRepository       = "ninhua/palworld-panel"
)

var patchFeatures = []string{"patch-info-api", "base-custom-names", "base-storage-browser", "player-notes", "guild-detail-browser", "base-worker-browser", "base-feed-box-summary", "insecure-endpoint-support", "panel-self-update", "audit-log-response-display", "player-presence-history", "host-save-migrator"}

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
