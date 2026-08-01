package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/boss"
)

func init() {
	patchFeatures = append(patchFeatures,
		"boss-reward-config",
		"boss-template-config",
		"boss-manual-summon-ledger",
		"boss-summon-state-machine",
		"boss-summon-audit",
	)
}

func (s Server) registerBossRoutes(api *gin.RouterGroup) {
	group := api.Group("/boss")
	group.GET("/summary", Require(PermRead), s.bossSummary)
	group.GET("/rewards", Require(PermRead), s.bossRewards)
	group.POST("/rewards", Require(PermConfigWrite), s.createBossReward)
	group.PUT("/rewards/:id", Require(PermConfigWrite), s.updateBossReward)
	group.DELETE("/rewards/:id", Require(PermConfigWrite), s.archiveBossReward)
	group.GET("/templates", Require(PermRead), s.bossTemplates)
	group.POST("/templates", Require(PermConfigWrite), s.createBossTemplate)
	group.PUT("/templates/:id", Require(PermConfigWrite), s.updateBossTemplate)
	group.DELETE("/templates/:id", Require(PermConfigWrite), s.archiveBossTemplate)
	group.GET("/summons", Require(PermRead), s.bossSummons)
	group.POST("/summons", Require(PermServerControl), s.createBossSummon)
	group.POST("/summons/:id/transition", Require(PermServerControl), s.transitionBossSummon)
	group.GET("/summons/:id/events", Require(PermRead), s.bossSummonEvents)
}

func (s Server) bossService() (*boss.Service, error) {
	return boss.ForPath(s.cfg.DBPath)
}

func (s Server) bossSummary(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.Summary(c.Request.Context())
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) bossRewards(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	items, err := service.ListRewards(c.Request.Context(), bossQueryBool(c, "include_archived"), bossQueryInt(c, "limit", 100), bossQueryInt(c, "offset", 0))
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) createBossReward(c *gin.Context) {
	var input boss.RewardInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.CreateReward(c.Request.Context(), input)
	if err != nil {
		bossFailure(c, err)
		return
	}
	created(c, result)
}

func (s Server) updateBossReward(c *gin.Context) {
	var input boss.RewardInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.UpdateReward(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) archiveBossReward(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.ArchiveReward(c.Request.Context(), c.Param("id"))
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) bossTemplates(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	items, err := service.ListTemplates(c.Request.Context(), bossQueryBool(c, "include_archived"), bossQueryInt(c, "limit", 100), bossQueryInt(c, "offset", 0))
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) createBossTemplate(c *gin.Context) {
	var input boss.TemplateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.CreateTemplate(c.Request.Context(), input)
	if err != nil {
		bossFailure(c, err)
		return
	}
	created(c, result)
}

func (s Server) updateBossTemplate(c *gin.Context) {
	var input boss.TemplateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.UpdateTemplate(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) archiveBossTemplate(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.ArchiveTemplate(c.Request.Context(), c.Param("id"))
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) bossSummons(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	items, err := service.ListSummons(c.Request.Context(), boss.SummonFilter{
		Status: c.Query("status"), TemplateID: c.Query("template_id"),
		Limit: bossQueryInt(c, "limit", 100), Offset: bossQueryInt(c, "offset", 0),
	})
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) createBossSummon(c *gin.Context) {
	var request boss.CreateSummonRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.CreateSummon(c.Request.Context(), request, CurrentPrincipal(c).Name)
	if err != nil {
		bossFailure(c, err)
		return
	}
	if result.Duplicate {
		ok(c, result)
		return
	}
	created(c, result)
}

func (s Server) transitionBossSummon(c *gin.Context) {
	var request boss.TransitionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.TransitionSummon(c.Request.Context(), c.Param("id"), request, CurrentPrincipal(c).Name)
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) bossSummonEvents(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	items, err := service.ListSummonEvents(c.Request.Context(), c.Param("id"), bossQueryInt(c, "limit", 100), bossQueryInt(c, "offset", 0))
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func bossQueryBool(c *gin.Context, key string) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(c.Query(key)))
	return err == nil && value
}

func bossQueryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(c.Query(key)))
	if err != nil {
		return fallback
	}
	return value
}

func bossFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, boss.ErrInvalidReward), errors.Is(err, boss.ErrInvalidTemplate), errors.Is(err, boss.ErrInvalidSummon):
		fail(c, http.StatusBadRequest, "boss_request_invalid", err.Error())
	case errors.Is(err, boss.ErrRewardNotFound), errors.Is(err, boss.ErrTemplateNotFound), errors.Is(err, boss.ErrSummonNotFound), errors.Is(err, sql.ErrNoRows):
		fail(c, http.StatusNotFound, "boss_resource_not_found", err.Error())
	case errors.Is(err, boss.ErrTemplateDisabled), errors.Is(err, boss.ErrInvalidTransition):
		fail(c, http.StatusConflict, "boss_state_conflict", err.Error())
	default:
		fail(c, http.StatusInternalServerError, "boss_operation_failed", err.Error())
	}
}
