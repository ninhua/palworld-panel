package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/boss"
)

func init() {
	patchFeatures = append(patchFeatures,
		"boss-participant-registration",
		"boss-participant-cap",
		"boss-participant-area-eligibility",
		"boss-participant-idempotency",
		"boss-participant-audit",
		"boss-participant-database-lease",
	)
}

func (s Server) tryBossRegistrationControl(c *gin.Context, request boss.TransitionRequest) bool {
	if !boss.IsRegistrationControlAction(request.Status) {
		return false
	}
	service, err := s.bossService()
	if err != nil {
		bossRegistrationFailure(c, err)
		return true
	}
	action := request.Status
	actor := CurrentPrincipal(c).Name
	switch normalizeBossRegistrationAction(action) {
	case "registration_configure":
		var input boss.RegistrationPolicyUpdate
		if !decodeBossRegistrationInput(c, request.Result, &input) {
			return true
		}
		result, operationErr := service.ConfigureRegistration(c.Request.Context(), c.Param("id"), input, actor)
		if operationErr != nil {
			bossRegistrationFailure(c, operationErr)
			return true
		}
		ok(c, result)
	case "registration_snapshot":
		includeCancelled, _ := request.Result["include_cancelled"].(bool)
		result, operationErr := service.RegistrationSnapshot(c.Request.Context(), c.Param("id"), includeCancelled)
		if operationErr != nil {
			bossRegistrationFailure(c, operationErr)
			return true
		}
		ok(c, result)
	case "participant_register":
		var input boss.ParticipantInput
		if !decodeBossRegistrationInput(c, request.Result, &input) {
			return true
		}
		result, operationErr := service.RegisterParticipant(c.Request.Context(), c.Param("id"), input, actor)
		if operationErr != nil {
			bossRegistrationFailure(c, operationErr)
			return true
		}
		ok(c, result)
	case "participant_check_area":
		var input boss.ParticipantAreaInput
		if !decodeBossRegistrationInput(c, request.Result, &input) {
			return true
		}
		result, operationErr := service.CheckParticipantArea(c.Request.Context(), c.Param("id"), input, actor)
		if operationErr != nil {
			bossRegistrationFailure(c, operationErr)
			return true
		}
		ok(c, result)
	case "participant_cancel":
		var input boss.ParticipantCancelInput
		if !decodeBossRegistrationInput(c, request.Result, &input) {
			return true
		}
		result, operationErr := service.CancelParticipant(c.Request.Context(), c.Param("id"), input, actor)
		if operationErr != nil {
			bossRegistrationFailure(c, operationErr)
			return true
		}
		ok(c, result)
	default:
		bossRegistrationFailure(c, boss.ErrInvalidRegistration)
	}
	return true
}

func normalizeBossRegistrationAction(action string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(action)), "-", "_")
}

func decodeBossRegistrationInput(c *gin.Context, value map[string]any, target any) bool {
	body, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(body, target)
	}
	if err != nil {
		fail(c, http.StatusBadRequest, "boss_registration_invalid", err.Error())
		return false
	}
	return true
}

func bossRegistrationFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, boss.ErrInvalidRegistration):
		fail(c, http.StatusBadRequest, "boss_registration_invalid", err.Error())
	case errors.Is(err, boss.ErrSummonNotFound), errors.Is(err, boss.ErrParticipantNotFound):
		fail(c, http.StatusNotFound, "boss_registration_not_found", err.Error())
	case errors.Is(err, boss.ErrRegistrationClosed), errors.Is(err, boss.ErrRegistrationFull),
		errors.Is(err, boss.ErrRegistrationPolicy), errors.Is(err, boss.ErrRegistrationBusy), errors.Is(err, boss.ErrParticipantStateConflict):
		fail(c, http.StatusConflict, "boss_registration_conflict", err.Error())
	default:
		bossFailure(c, err)
	}
}
