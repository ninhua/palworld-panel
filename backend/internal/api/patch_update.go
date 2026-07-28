package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"palpanel/internal/server"
)

const patchHotUpdateFeature = "panel-patch-hot-update"

func (s Server) patchUpdateRequest() server.PatchUpdateRequest {
	return server.PatchUpdateRequest{
		TargetVersion:       patchTargetVersion,
		CurrentPatchVersion: patchVersion,
		Repository:          patchRepository,
		RequiredFeature:     patchHotUpdateFeature,
	}
}

func (s Server) patchUpdateStatus(c *gin.Context) {
	status, err := s.server.PatchUpdateStatus(c.Request.Context(), s.patchUpdateRequest())
	if err != nil {
		fail(c, http.StatusBadGateway, "patch_update_status_failed", err.Error())
		return
	}
	ok(c, status)
}

func (s Server) patchUpdateCheck(c *gin.Context) {
	job, err := s.server.CheckPatchUpdate(c.Request.Context(), s.patchUpdateRequest())
	if err != nil {
		fail(c, http.StatusInternalServerError, "patch_update_check_failed", err.Error())
		return
	}
	accepted(c, job)
}

func (s Server) patchUpdate(c *gin.Context) {
	job, err := s.server.ApplyPatchUpdate(c.Request.Context(), s.patchUpdateRequest())
	if err != nil {
		fail(c, http.StatusInternalServerError, "patch_update_failed", err.Error())
		return
	}
	accepted(c, job)
}
