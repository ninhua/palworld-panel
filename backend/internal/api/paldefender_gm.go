package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/paldefender"
	"palpanel/internal/pallocalize"
)

const palDefenderGMRequestLimit = 256 << 10

func (s Server) palDefenderGMStatus(c *gin.Context) {
	ok(c, s.defender.GMStatus(c.Request.Context()))
}

func (s Server) palDefenderGMPlayers(c *gin.Context) {
	response, err := s.defender.RESTPlayers(c.Request.Context())
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, response)
}

func (s Server) palDefenderGMPlayer(c *gin.Context) {
	response, err := s.defender.RESTPlayer(c.Request.Context(), c.Param("id"))
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, response)
}

func (s Server) palDefenderGMItems(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit < 1 || limit > 5000 {
		fail(c, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 5000")
		return
	}
	items := pallocalize.SearchItems(c.Query("q"), limit)
	ok(c, gin.H{"items": items, "returned": len(items)})
}

func (s Server) palDefenderGMInventory(c *gin.Context) {
	response, err := s.defender.RESTInventory(c.Request.Context(), c.Param("id"))
	if err != nil {
		failPalDefenderGM(c, err)
		return
	}
	ok(c, response)
}

func (s Server) palDefenderGMGiveItems(c *gin.Context) {
	var request paldefender.GiveItemsRequest
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RESTGiveItems(ctx, c.Param("id"), request)
	})
}

func (s Server) palDefenderGMRemoveItems(c *gin.Context) {
	var request paldefender.RemoveItemsRequest
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RCONRemoveItems(ctx, c.Param("id"), request)
	})
}

func (s Server) palDefenderGMTeleport(c *gin.Context) {
	var request paldefender.TeleportRequest
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RCONTeleport(ctx, c.Param("id"), request)
	})
}

func (s Server) palDefenderGMSendMessage(c *gin.Context) {
	var input struct {
		SendType string `json:"SendType"`
		Message  string `json:"Message"`
	}
	if err := bindPalDefenderGMJSON(c, &input); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	request := paldefender.SendMessageRequest{SendType: input.SendType, Message: input.Message}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RESTSendMessage(ctx, c.Param("id"), request)
	})
}

func (s Server) palDefenderGMBroadcast(c *gin.Context) {
	var request struct {
		Message string `json:"message" binding:"required"`
		Alert   bool   `json:"alert"`
	}
	if err := bindPalDefenderGMJSON(c, &request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return s.defender.RESTBroadcast(ctx, request.Message, request.Alert)
	})
}

func (s Server) palDefenderGMKick(c *gin.Context) {
	s.palDefenderGMPunishment(c, s.defender.RESTKick)
}

func (s Server) palDefenderGMBan(c *gin.Context) {
	s.palDefenderGMPunishment(c, s.defender.RESTBan)
}

func (s Server) palDefenderGMUnban(c *gin.Context) {
	s.palDefenderGMPunishment(c, s.defender.RESTUnban)
}

func (s Server) palDefenderGMPunishment(c *gin.Context, action func(ctx context.Context, identifier string, request paldefender.PunishmentRequest) (paldefender.RESTActionResult, error)) {
	var request paldefender.PunishmentRequest
	if c.Request.ContentLength != 0 {
		if err := bindPalDefenderGMJSON(c, &request); err != nil {
			fail(c, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
	}
	s.runPalDefenderGMWrite(c, request, func(ctx context.Context) (any, error) {
		return action(ctx, c.Param("id"), request)
	})
}

func failPalDefenderGM(c *gin.Context, err error) {
	switch {
	case errors.Is(err, paldefender.ErrRESTTokenMissing):
		fail(c, http.StatusConflict, "paldefender_rest_not_configured", err.Error())
		return
	case errors.Is(err, paldefender.ErrPalDefenderNotInstalled):
		fail(c, http.StatusConflict, "paldefender_not_installed", err.Error())
		return
	case errors.Is(err, paldefender.ErrPalDefenderNotLoaded):
		fail(c, http.StatusConflict, "paldefender_not_loaded", err.Error())
		return
	case errors.Is(err, paldefender.ErrPalDefenderRESTDisabled):
		fail(c, http.StatusConflict, "paldefender_rest_disabled", err.Error())
		return
	case errors.Is(err, paldefender.ErrRESTInvalidConfiguration):
		fail(c, http.StatusConflict, "paldefender_rest_invalid_configuration", err.Error())
		return
	case errors.Is(err, paldefender.ErrRESTTimeout), errors.Is(err, context.DeadlineExceeded):
		fail(c, http.StatusGatewayTimeout, "paldefender_rest_timeout", paldefender.ErrRESTTimeout.Error())
		return
	case errors.Is(err, paldefender.ErrRESTInvalidResponse), errors.Is(err, paldefender.ErrRESTResponseTooLarge):
		fail(c, http.StatusBadGateway, "paldefender_invalid_response", paldefender.ErrRESTInvalidResponse.Error())
		return
	case errors.Is(err, paldefender.ErrRESTUnavailable):
		fail(c, http.StatusServiceUnavailable, "paldefender_rest_unavailable", paldefender.ErrRESTUnavailable.Error())
		return
	case errors.Is(err, paldefender.ErrRCONDisabled), errors.Is(err, paldefender.ErrRCONPasswordMissing), errors.Is(err, paldefender.ErrRCONBase64Unsupported):
		fail(c, http.StatusConflict, "paldefender_rcon_not_ready", err.Error())
		return
	case errors.Is(err, paldefender.ErrRCONUnavailable):
		fail(c, http.StatusServiceUnavailable, "paldefender_rcon_unavailable", paldefender.ErrRCONUnavailable.Error())
		return
	case errors.Is(err, paldefender.ErrRCONAuthentication), errors.Is(err, paldefender.ErrRCONInvalidResponse):
		fail(c, http.StatusBadGateway, "paldefender_rcon_failed", err.Error())
		return
	case errors.Is(err, paldefender.ErrRCONCommandRejected):
		fail(c, http.StatusBadGateway, "paldefender_rcon_command_rejected", err.Error())
		return
	case errors.Is(err, paldefender.ErrExportPlayerUnavailable):
		fail(c, http.StatusConflict, "paldefender_export_player_unavailable", err.Error())
		return
	case errors.Is(err, paldefender.ErrExportNoPals):
		fail(c, http.StatusConflict, "paldefender_export_no_pals", err.Error())
		return
	case errors.Is(err, paldefender.ErrExportCommandRejected):
		fail(c, http.StatusBadGateway, "paldefender_export_rejected", err.Error())
		return
	case errors.Is(err, paldefender.ErrExportPalsTimeout):
		fail(c, http.StatusGatewayTimeout, "paldefender_export_timeout", err.Error())
		return
	}
	var restErr *paldefender.RESTError
	if errors.As(err, &restErr) {
		status := restErr.Status
		if status == http.StatusUnauthorized || status == http.StatusForbidden || status >= http.StatusInternalServerError {
			status = http.StatusBadGateway
		}
		if status < http.StatusBadRequest || status >= 600 {
			status = http.StatusBadGateway
		}
		code := normalizePalDefenderErrorCode(restErr.Code)
		if code == "" {
			code = "rest_failed"
		}
		fail(c, status, "paldefender_"+code, restErr.Message)
		return
	}
	if errors.Is(err, paldefender.ErrInvalidRESTRequest) {
		fail(c, http.StatusBadRequest, "paldefender_invalid_request", err.Error())
		return
	}
	fail(c, http.StatusBadGateway, "paldefender_rest_failed", "PalDefender REST request failed")
}

func bindPalDefenderGMJSON(c *gin.Context, value any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, palDefenderGMRequestLimit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain exactly one JSON value")
		}
		return err
	}
	return nil
}

func normalizePalDefenderErrorCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	var normalized strings.Builder
	for _, char := range code {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			normalized.WriteRune(char)
		case char == '-' || char == '_' || char == ' ':
			if normalized.Len() > 0 {
				normalized.WriteByte('_')
			}
		}
	}
	return strings.Trim(normalized.String(), "_")
}
