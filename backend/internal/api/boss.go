package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/boss"
)

func init() {
	patchFeatures = append(patchFeatures,
		"boss-reward-config",
		"boss-template-config",
		"boss-wave-config",
		"boss-wave-snapshot",
		"boss-wave-state-machine",
		"boss-manual-summon-ledger",
		"boss-summon-state-machine",
		"boss-summon-audit",
		"boss-schedule-config",
		"boss-schedule-worker",
		"boss-schedule-warning-ledger",
		"boss-paldefender-warning-broadcast",
		"boss-warning-test-action",
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
	group.GET("/templates/:id/waves", Require(PermRead), s.bossTemplateWaves)
	group.PUT("/templates/:id/waves", Require(PermConfigWrite), s.replaceBossTemplateWaves)
	group.GET("/summons", Require(PermRead), s.bossSummons)
	group.POST("/summons", Require(PermServerControl), s.createBossSummon)
	group.POST("/summons/:id/transition", Require(PermServerControl), s.transitionBossSummon)
	group.GET("/summons/:id/waves", Require(PermRead), s.bossSummonWaves)
	group.POST("/summons/:id/waves/:position/transition", Require(PermServerControl), s.transitionBossSummonWave)
	group.GET("/summons/:id/events", Require(PermRead), s.bossSummonEvents)
	group.GET("/schedules", Require(PermRead), s.bossSchedules)
	group.POST("/schedules", Require(PermConfigWrite), s.createBossSchedule)
	group.PUT("/schedules/:id", Require(PermConfigWrite), s.updateBossSchedule)
	group.DELETE("/schedules/:id", Require(PermConfigWrite), s.archiveBossSchedule)
	group.POST("/schedules/:id/run-now", Require(PermServerControl), s.runBossScheduleNow)
	group.POST("/schedules/:id/test-warning", Require(PermServerControl), s.testBossScheduleWarning)
	group.GET("/schedule-events", Require(PermRead), s.bossScheduleEvents)
	group.POST("/maintenance/run-due", Require(PermServerControl), s.runDueBossSchedules)
}

func (s Server) bossService() (*boss.Service, error) {
	service, err := boss.ForPath(s.cfg.DBPath)
	if err != nil {
		return nil, err
	}
	service.SetWarningBroadcaster(boss.WarningBroadcasterFunc(func(ctx context.Context, message string) error {
		_, broadcastErr := s.defender.RESTBroadcast(ctx, message, true)
		return broadcastErr
	}))
	return service, nil
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

func (s Server) bossTemplateWaves(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	items, err := service.ListTemplateWaves(c.Request.Context(), c.Param("id"))
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) replaceBossTemplateWaves(c *gin.Context) {
	var request struct {
		Waves []boss.WaveInput `json:"waves" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	items, err := service.ReplaceTemplateWaves(c.Request.Context(), c.Param("id"), request.Waves)
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
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

func (s Server) bossSummonWaves(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	items, err := service.ListSummonWaves(c.Request.Context(), c.Param("id"))
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) transitionBossSummonWave(c *gin.Context) {
	position, err := strconv.Atoi(strings.TrimSpace(c.Param("position")))
	if err != nil {
		fail(c, http.StatusBadRequest, "boss_request_invalid", "wave position must be an integer")
		return
	}
	var request boss.WaveTransitionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.TransitionSummonWave(c.Request.Context(), c.Param("id"), position, request, CurrentPrincipal(c).Name)
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

func (s Server) bossSchedules(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	items, err := service.ListSchedules(c.Request.Context(), boss.ScheduleFilter{
		Enabled: c.Query("enabled"), IncludeArchived: bossQueryBool(c, "include_archived"),
		Limit: bossQueryInt(c, "limit", 100), Offset: bossQueryInt(c, "offset", 0),
	})
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) createBossSchedule(c *gin.Context) {
	var input boss.ScheduleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.CreateSchedule(c.Request.Context(), input)
	if err != nil {
		bossFailure(c, err)
		return
	}
	created(c, result)
}

func (s Server) updateBossSchedule(c *gin.Context) {
	var input boss.ScheduleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.UpdateSchedule(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) archiveBossSchedule(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.ArchiveSchedule(c.Request.Context(), c.Param("id"))
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, result)
}

func (s Server) runBossScheduleNow(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.RunScheduleNow(c.Request.Context(), c.Param("id"), CurrentPrincipal(c).Name)
	if err != nil {
		bossFailure(c, err)
		return
	}
	created(c, result)
}

func (s Server) testBossScheduleWarning(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	event, err := service.SendTestWarning(c.Request.Context(), c.Param("id"), CurrentPrincipal(c).Name)
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, event)
}

func (s Server) bossScheduleEvents(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	items, err := service.ListScheduleEvents(c.Request.Context(), boss.ScheduleEventFilter{
		ScheduleID: c.Query("schedule_id"), EventType: c.Query("event_type"), Status: c.Query("status"),
		Limit: bossQueryInt(c, "limit", 100), Offset: bossQueryInt(c, "offset", 0),
	})
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) runDueBossSchedules(c *gin.Context) {
	service, err := s.bossService()
	if err != nil {
		bossFailure(c, err)
		return
	}
	result, err := service.RunDueSchedules(c.Request.Context(), time.Now(), CurrentPrincipal(c).Name)
	if err != nil {
		bossFailure(c, err)
		return
	}
	ok(c, result)
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
	case errors.Is(err, boss.ErrInvalidReward), errors.Is(err, boss.ErrInvalidTemplate), errors.Is(err, boss.ErrInvalidWave), errors.Is(err, boss.ErrInvalidSummon), errors.Is(err, boss.ErrInvalidSchedule):
		fail(c, http.StatusBadRequest, "boss_request_invalid", err.Error())
	case errors.Is(err, boss.ErrRewardNotFound), errors.Is(err, boss.ErrTemplateNotFound), errors.Is(err, boss.ErrWaveNotFound), errors.Is(err, boss.ErrSummonNotFound), errors.Is(err, boss.ErrScheduleNotFound), errors.Is(err, sql.ErrNoRows):
		fail(c, http.StatusNotFound, "boss_resource_not_found", err.Error())
	case errors.Is(err, boss.ErrTemplateDisabled), errors.Is(err, boss.ErrInvalidTransition), errors.Is(err, boss.ErrInvalidWaveTransition), errors.Is(err, boss.ErrScheduleDisabled), errors.Is(err, boss.ErrScheduleRunConflict):
		fail(c, http.StatusConflict, "boss_state_conflict", err.Error())
	case errors.Is(err, boss.ErrWarningBroadcasterMissing):
		fail(c, http.StatusServiceUnavailable, "boss_warning_broadcaster_unavailable", err.Error())
	case errors.Is(err, boss.ErrWarningBroadcastFailed):
		fail(c, http.StatusBadGateway, "boss_warning_broadcast_failed", err.Error())
	default:
		fail(c, http.StatusInternalServerError, "boss_operation_failed", err.Error())
	}
}
