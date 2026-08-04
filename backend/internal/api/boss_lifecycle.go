package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"palpanel/internal/boss"
)

func init() {
	patchFeatures = append(patchFeatures,
		"fixed-boss-paldefender-death-detection",
		"fixed-boss-lifecycle-persistent-cursors",
		"fixed-boss-lifecycle-restart-recovery",
		"fixed-boss-auto-completion",
		"fixed-boss-timeout-manual-reconciliation",
		"starter-gift-preserved-reissue-history",
		"starter-gift-history-api",
	)
}

func (s Server) bossLifecycleStatus(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	state, err := service.GetBossLifecycle(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, boss.ErrSummonNotFound) {
			fail(c, http.StatusNotFound, "boss_lifecycle_not_found", "Boss lifecycle monitor was not found")
			return
		}
		bossFailure(c, err)
		return
	}
	ok(c, state)
}
