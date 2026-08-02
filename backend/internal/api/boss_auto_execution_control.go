package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"palpanel/internal/boss"
)

// tryBossAutoExecutionControl reuses the existing summon transition endpoint
// for maintenance controls without adding new runtime routes. It returns true
// only when request.status is a supported automatic-execution control action.
func (s Server) tryBossAutoExecutionControl(c *gin.Context, request boss.TransitionRequest) bool {
	if !boss.IsAutoExecutionControlAction(request.Status) {
		return false
	}
	service, err := s.bossService()
	if err != nil {
		bossAutoExecutionControlFailure(c, err)
		return true
	}
	result, err := service.ApplyAutoExecutionControl(
		c.Request.Context(),
		c.Param("id"),
		request.Status,
		CurrentPrincipal(c).Name,
	)
	if err != nil {
		bossAutoExecutionControlFailure(c, err)
		return true
	}
	ok(c, result)
	return true
}

func bossAutoExecutionControlFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, boss.ErrInvalidAutoExecutionControl):
		fail(c, http.StatusBadRequest, "boss_auto_execution_control_invalid", err.Error())
	case errors.Is(err, boss.ErrSummonNotFound):
		fail(c, http.StatusNotFound, "boss_resource_not_found", err.Error())
	case errors.Is(err, boss.ErrAutoExecutionDisabled),
		errors.Is(err, boss.ErrAutoExecutionNoPendingWave),
		errors.Is(err, boss.ErrAutoExecutionNoFailedWave),
		errors.Is(err, boss.ErrInvalidTransition),
		errors.Is(err, boss.ErrInvalidWaveTransition),
		errors.Is(err, boss.ErrExecutionBusy),
		errors.Is(err, boss.ErrExecutionReconcileRequired):
		fail(c, http.StatusConflict, "boss_auto_execution_control_conflict", err.Error())
	default:
		bossFailure(c, err)
	}
}
