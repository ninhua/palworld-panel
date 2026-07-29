package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"palpanel/internal/server"
)

func (s Server) crashGuardStatus(c *gin.Context) {
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			fail(c, http.StatusBadRequest, "crash_guard_limit_invalid", "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	status, err := s.server.CrashGuardStatus(c.Request.Context(), limit)
	if err != nil {
		fail(c, http.StatusInternalServerError, "crash_guard_status_failed", err.Error())
		return
	}
	ok(c, status)
}

func (s Server) recoverCrashGuard(c *gin.Context) {
	var request struct {
		Confirm bool  `json:"confirm"`
		Start   *bool `json:"start"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || !request.Confirm {
		fail(c, http.StatusBadRequest, "crash_guard_confirmation_required", "confirm must be true")
		return
	}
	start := true
	if request.Start != nil {
		start = *request.Start
	}
	status, err := s.server.RecoverCrashGuard(c.Request.Context(), start)
	if err != nil {
		fail(c, http.StatusConflict, "crash_guard_recovery_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	ok(c, status)
}

func crashGuardOperationFailure(c *gin.Context, err error) bool {
	if !errors.Is(err, server.ErrCrashGuardTripped) {
		return false
	}
	fail(c, http.StatusConflict, "crash_guard_tripped", err.Error())
	return true
}
