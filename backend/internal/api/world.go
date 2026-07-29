package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"palpanel/internal/server"
)

func (s Server) serverWorld(c *gin.Context) {
	info, err := s.server.WorldInfo(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "world_read_failed", err.Error())
		return
	}
	ok(c, info)
}

func (s Server) serverWorldReset(c *gin.Context) {
	var request struct {
		WorldID      string `json:"world_id"`
		Confirmation string `json:"confirmation"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	job, err := s.server.ResetWorld(c.Request.Context(), request.WorldID, request.Confirmation, server.WorldResetHooks{
		Prepare: func(ctx context.Context) error {
			client := s.palworldREST()
			if _, err := client.Do(ctx, http.MethodPost, "announce", gin.H{"message": "World reset starting. Saving progress and stopping the server."}); err != nil {
				return err
			}
			_, err := client.Do(ctx, http.MethodPost, "save", nil)
			return err
		},
		Invalidate: func() {
			s.invalidateServerCaches()
			s.invalidateSaveCaches()
			s.invalidateSaveIndexes()
		},
	})
	if err != nil {
		if crashGuardOperationFailure(c, err) {
			return
		}
		fail(c, http.StatusBadRequest, "world_reset_rejected", err.Error())
		return
	}
	s.invalidateServerCaches()
	s.invalidateSaveCaches()
	accepted(c, job)
}
