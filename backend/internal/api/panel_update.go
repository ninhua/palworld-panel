package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"palpanel/internal/server"
)

func (s Server) panelUpdateRequest() server.PanelUpdateRequest {
	return server.PanelUpdateRequest{
		CurrentVersion: patchTargetVersion + "-custom." + patchVersion,
		Repository:     panelRepository,
	}
}

func (s Server) panelUpdateStatus(c *gin.Context) {
	status, err := s.server.PanelUpdateStatus(c.Request.Context(), s.panelUpdateRequest())
	if err != nil {
		fail(c, http.StatusBadGateway, "panel_update_status_failed", err.Error())
		return
	}
	ok(c, status)
}

func (s Server) panelUpdateCheck(c *gin.Context) {
	job, err := s.server.CheckPanelUpdate(c.Request.Context(), s.panelUpdateRequest())
	if err != nil {
		fail(c, http.StatusInternalServerError, "panel_update_check_failed", err.Error())
		return
	}
	accepted(c, job)
}

func (s Server) panelUpdate(c *gin.Context) {
	job, err := s.server.ApplyPanelUpdate(c.Request.Context(), s.panelUpdateRequest())
	if err != nil {
		fail(c, http.StatusInternalServerError, "panel_update_failed", err.Error())
		return
	}
	accepted(c, job)
}
