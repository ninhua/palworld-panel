package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/playerpresence"
	"palpanel/internal/startergift"
)

func (s Server) starterGiftHistory(c *gin.Context) {
	scope, err := s.currentStarterGiftScope()
	if err != nil {
		fail(c, http.StatusConflict, "starter_gift_world_unavailable", err.Error())
		return
	}
	items, err := startergift.ListGrantHistory(c.Request.Context(), s.store, scope)
	if err != nil {
		fail(c, http.StatusInternalServerError, "starter_gift_history_read_failed", err.Error())
		return
	}
	ok(c, gin.H{"items": items, "count": len(items), "scope": scope})
}

func (s Server) preserveStarterGiftAction(c *gin.Context) {
	scope, err := s.currentStarterGiftScope()
	if err != nil {
		fail(c, http.StatusConflict, "starter_gift_world_unavailable", err.Error())
		return
	}
	var input struct {
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	if input.Action != "reissue" && input.Action != "next_login" && input.Action != "cancel_next_login" {
		fail(c, http.StatusBadRequest, "starter_gift_history_action_invalid", "only reissue, next_login and cancel_next_login use the history-preserving endpoint")
		return
	}
	player := s.resolveStarterGiftActionPlayer(c.Request.Context(), c.Param("id"))
	result, err := startergift.ApplyActionPreservingHistory(c.Request.Context(), s.store, scope, player, input.Action)
	if err != nil {
		fail(c, http.StatusBadRequest, "starter_gift_action_failed", err.Error())
		return
	}
	ok(c, gin.H{
		"queued": true, "action": input.Action, "history_archived": result.Archived,
		"history_id": result.HistoryID,
		"message":    "Starter gift state updated; the previous grant cycle remains available in history.",
	})
}

func (s Server) resolveStarterGiftActionPlayer(ctx context.Context, identifier string) playerpresence.OnlinePlayer {
	player := playerpresence.OnlinePlayer{SteamID: identifier}
	if index, _, indexErr := s.serverSaveIndex.Current(ctx); indexErr == nil {
		requested := starterGiftPlayerIdentity(identifier)
		for _, indexed := range index.Players {
			if requested == starterGiftPlayerIdentity(indexed.SteamID) || requested == starterGiftPlayerIdentity(indexed.PlayerUID) {
				return playerpresence.OnlinePlayer{PlayerUID: indexed.PlayerUID, SteamID: indexed.SteamID, Nickname: indexed.Nickname}
			}
		}
	}
	return player
}
