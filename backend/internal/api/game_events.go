package api

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/astrbotclient"
	"palpanel/internal/economy"
	"palpanel/internal/gameevents"
	"palpanel/internal/paldefender"
	"palpanel/internal/tasks"
)

func init() {
	patchFeatures = append(patchFeatures,
		"signed-game-event-ingress",
		"game-event-ledger",
		"game-chat-command-dispatch",
		"game-chat-private-replies",
	)
}

func (s Server) registerGameEventRoutes(api *gin.RouterGroup) {
	api.GET("/game-events", Require(PermRead), s.listGameEvents)
	api.GET("/game-events/:id", Require(PermRead), s.getGameEvent)
}

func (s Server) gameEventService() (*gameevents.Service, error) {
	return gameevents.ForPath(s.cfg.DBPath)
}

// gameIntegrationSignatureAuth deliberately reuses the existing AstrBot HMAC
// credential format so installations do not need a second shared secret during
// the first event-bridge rollout. Unlike astrBotSignatureAuth it does not
// require the AstrBot HTTP client itself to be enabled.
func (s Server) gameIntegrationSignatureAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(s.cfg.AstrBotSharedSecret) == "" || strings.TrimSpace(s.cfg.AstrBotPanelID) == "" {
			fail(c, http.StatusServiceUnavailable, "game_integration_disabled", "configure the PalPanel integration shared secret before sending game events")
			c.Abort()
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
		if err != nil {
			fail(c, http.StatusBadRequest, "request_read_failed", err.Error())
			c.Abort()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		nonce := c.GetHeader("X-PalPanel-Nonce")
		if c.GetHeader("X-PalPanel-Id") != s.cfg.AstrBotPanelID || !astrbotclient.Verify(s.cfg.AstrBotSharedSecret, c.Request.Method, c.Request.URL.Path, c.GetHeader("X-PalPanel-Timestamp"), nonce, c.GetHeader("X-PalPanel-Signature"), body) {
			fail(c, http.StatusUnauthorized, "integration_signature_invalid", "invalid integration signature")
			c.Abort()
			return
		}
		now := time.Now()
		astrBotNonces.Range(func(key, value any) bool {
			if seen, valid := value.(time.Time); !valid || now.Sub(seen) > 2*time.Minute {
				astrBotNonces.Delete(key)
			}
			return true
		})
		if _, loaded := astrBotNonces.LoadOrStore(nonce, now); loaded {
			fail(c, http.StatusUnauthorized, "integration_replay_rejected", "integration nonce was already used")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s Server) ingestGameEvent(c *gin.Context) {
	var event gameevents.Event
	if err := c.ShouldBindJSON(&event); err != nil {
		fail(c, http.StatusBadRequest, "game_event_invalid", err.Error())
		return
	}
	service, err := s.gameEventService()
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_service_failed", err.Error())
		return
	}
	claim, err := service.Claim(c.Request.Context(), event)
	if err != nil {
		if errors.Is(err, gameevents.ErrInvalidEvent) {
			fail(c, http.StatusBadRequest, "game_event_invalid", err.Error())
			return
		}
		fail(c, http.StatusInternalServerError, "game_event_claim_failed", err.Error())
		return
	}
	if claim.Duplicate {
		ok(c, gin.H{"duplicate": true, "event": claim.Record, "result": claim.Record.Result})
		return
	}

	result := map[string]any{"accepted": true, "retry": claim.Retry}
	if claim.Record.Type == "PLAYER_CHAT" {
		message, _ := claim.Record.Payload["message"].(string)
		economyService, economyErr := s.economyService()
		if economyErr != nil {
			_, _ = service.Fail(c.Request.Context(), claim.Record.EventID, economyErr)
			economyFailure(c, economyErr)
			return
		}
		command, commandErr := economyService.ExecuteCommand(c.Request.Context(), economy.CommandRequest{
			EventID: claim.Record.EventID, PlayerUID: claim.Record.PlayerUID, Nickname: claim.Record.Nickname,
			SteamID: claim.Record.SteamID, Message: message,
		})
		if commandErr != nil {
			_, _ = service.Fail(c.Request.Context(), claim.Record.EventID, commandErr)
			economyFailure(c, commandErr)
			return
		}
		result["command"] = command
		if command.Handled && command.Reply != "" && !command.Duplicate {
			delivery, deliveryErr := s.deliverGameEventReply(c.Request.Context(), claim.Record, command.Reply)
			result["reply_delivery"] = delivery
			if deliveryErr != nil {
				result["reply_error"] = deliveryErr.Error()
			}
		}
	}
	taskService, taskErr := s.taskService()
	if taskErr != nil {
		_, _ = service.Fail(c.Request.Context(), claim.Record.EventID, taskErr)
		taskFailure(c, taskErr)
		return
	}
	pointService, pointErr := s.economyService()
	if pointErr != nil {
		_, _ = service.Fail(c.Request.Context(), claim.Record.EventID, pointErr)
		economyFailure(c, pointErr)
		return
	}
	taskUpdates, taskErr := taskService.ProcessEvent(c.Request.Context(), tasks.Event{
		EventID: claim.Record.EventID, Type: claim.Record.Type, PlayerUID: claim.Record.PlayerUID,
		Nickname: claim.Record.Nickname, SteamID: claim.Record.SteamID, OccurredAt: claim.Record.OccurredAt,
		Payload: claim.Record.Payload,
	}, pointService)
	if taskErr != nil {
		_, _ = service.Fail(c.Request.Context(), claim.Record.EventID, taskErr)
		taskFailure(c, taskErr)
		return
	}
	result["tasks"] = taskUpdates
	completed, err := service.Complete(c.Request.Context(), claim.Record.EventID, result)
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_complete_failed", err.Error())
		return
	}
	ok(c, gin.H{"duplicate": false, "event": completed, "result": result})
}

func (s Server) deliverGameEventReply(ctx context.Context, event gameevents.Record, reply string) (string, error) {
	players, err := s.defender.RESTPlayers(ctx)
	if err != nil {
		return "paldefender_unavailable", err
	}
	identifier := ""
	for position := range players.Players {
		player := players.Players[position]
		if strings.EqualFold(player.PlayerUID, event.PlayerUID) || strings.EqualFold(player.UserID, event.PlayerUID) || (event.Nickname != "" && strings.EqualFold(player.Name, event.Nickname)) {
			identifier = strings.TrimSpace(player.UserID)
			if identifier == "" {
				identifier = strings.TrimSpace(player.PlayerUID)
			}
			break
		}
	}
	if identifier == "" {
		return "player_not_online", nil
	}
	if _, err := s.defender.RESTSendMessage(ctx, identifier, paldefender.SendMessageRequest{SendType: "PlayerChat", Message: reply}); err != nil {
		return "send_failed", err
	}
	return "sent", nil
}

func (s Server) listGameEvents(c *gin.Context) {
	service, err := s.gameEventService()
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_service_failed", err.Error())
		return
	}
	records, err := service.List(c.Request.Context(), c.Query("type"), c.Query("player_uid"), economyQueryInt(c, "limit", 50), economyQueryInt(c, "offset", 0))
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_list_failed", err.Error())
		return
	}
	ok(c, records)
}

func (s Server) getGameEvent(c *gin.Context) {
	service, err := s.gameEventService()
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_service_failed", err.Error())
		return
	}
	record, err := service.Get(c.Request.Context(), c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(c, http.StatusNotFound, "game_event_not_found", "game event not found")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_read_failed", err.Error())
		return
	}
	ok(c, record)
}
