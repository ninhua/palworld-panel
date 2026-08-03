package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/gameevents"
	"palpanel/internal/tasks"
)

func init() {
	patchFeatures = append(patchFeatures,
		"event-task-engine",
		"daily-weekly-once-tasks",
		"idempotent-task-rewards",
		"task-operations-api",
		"task-completion-private-feedback",
		"task-completion-feedback-retry",
	)
}

func (s Server) registerTaskRoutes(api *gin.RouterGroup) {
	group := api.Group("/tasks")
	group.GET("", Require(PermRead), s.listTasks)
	group.POST("", Require(PermConfigWrite), s.createTask)
	group.GET("/progress/:player_uid", Require(PermRead), s.playerTaskProgress)
	group.GET("/online-tracking", Require(PermRead), s.onlineTaskTracking)
	group.POST("/diagnostics/evaluate", Require(PermRead), s.evaluateTaskEvent)
	group.POST("/diagnostics/replay", Require(PermPlayersWrite), s.replayTaskEvent)
	group.POST("/maintenance/retry-rewards", Require(PermPlayersWrite), s.retryTaskRewards)
	group.PUT("/:id", Require(PermConfigWrite), s.updateTask)
	group.DELETE("/:id", Require(PermConfigWrite), s.archiveTask)
}

func (s Server) taskService() (*tasks.Service, error) {
	service, err := tasks.ForPath(s.cfg.DBPath, strings.TrimSpace(os.Getenv("PALPANEL_OPERATIONS_TIMEZONE")))
	if err != nil {
		return nil, err
	}
	if err := service.SetCompletionNotifier(tasks.CompletionNotifierFunc(func(ctx context.Context, notice tasks.CompletionNotice) error {
		deliveryIdentity := strings.TrimSpace(notice.SteamID)
		if deliveryIdentity == "" {
			deliveryIdentity = strings.TrimSpace(notice.PlayerUID)
		}
		delivery, deliveryErr := s.deliverGameEventReply(ctx, gameevents.Record{
			EventID:   notice.EventID(),
			PlayerUID: deliveryIdentity,
			Nickname:  notice.Nickname,
			SteamID:   notice.SteamID,
		}, notice.Message())
		if deliveryErr != nil {
			return deliveryErr
		}
		if delivery != "sent" {
			return fmt.Errorf("task completion feedback was not delivered: %s", delivery)
		}
		return nil
	})); err != nil {
		return nil, err
	}
	return service, nil
}

type gameTaskQueryRequest struct {
	PlayerUID string `json:"player_uid"`
	Query     string `json:"query,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

func (s Server) gameTaskQuery(c *gin.Context) {
	var request gameTaskQueryRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(request.PlayerUID) == "" {
		fail(c, http.StatusBadRequest, "task_player_uid_required", "player_uid is required")
		return
	}
	service, err := s.taskService()
	if err != nil {
		taskFailure(c, err)
		return
	}
	page, err := service.PlayerProgressPage(c.Request.Context(), request.PlayerUID, request.Query, request.Limit, request.Offset)
	if err != nil {
		taskFailure(c, err)
		return
	}
	ok(c, page)
}

func (s Server) onlineTaskTracking(c *gin.Context) {
	service, err := s.taskService()
	if err != nil {
		taskFailure(c, err)
		return
	}
	items, err := service.OnlineTrackingRecords(c.Request.Context(), economyQueryInt(c, "limit", 100))
	if err != nil {
		taskFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) listTasks(c *gin.Context) {
	service, err := s.taskService()
	if err != nil {
		taskFailure(c, err)
		return
	}
	includeArchived, _ := strconv.ParseBool(c.DefaultQuery("include_archived", "false"))
	items, err := service.ListDefinitions(c.Request.Context(), includeArchived)
	if err != nil {
		taskFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) createTask(c *gin.Context) {
	var input tasks.DefinitionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.taskService()
	if err != nil {
		taskFailure(c, err)
		return
	}
	definition, err := service.CreateDefinition(c.Request.Context(), input)
	if err != nil {
		taskFailure(c, err)
		return
	}
	created(c, definition)
}

func (s Server) updateTask(c *gin.Context) {
	var input tasks.DefinitionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.taskService()
	if err != nil {
		taskFailure(c, err)
		return
	}
	definition, err := service.UpdateDefinition(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		taskFailure(c, err)
		return
	}
	ok(c, definition)
}

func (s Server) archiveTask(c *gin.Context) {
	service, err := s.taskService()
	if err != nil {
		taskFailure(c, err)
		return
	}
	definition, err := service.ArchiveDefinition(c.Request.Context(), c.Param("id"))
	if err != nil {
		taskFailure(c, err)
		return
	}
	ok(c, definition)
}

func (s Server) playerTaskProgress(c *gin.Context) {
	service, err := s.taskService()
	if err != nil {
		taskFailure(c, err)
		return
	}
	items, err := service.PlayerProgress(c.Request.Context(), c.Param("player_uid"))
	if err != nil {
		taskFailure(c, err)
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

func (s Server) evaluateTaskEvent(c *gin.Context) {
	var event tasks.Event
	if err := c.ShouldBindJSON(&event); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.taskService()
	if err != nil {
		taskFailure(c, err)
		return
	}
	report, err := service.DiagnoseEvent(c.Request.Context(), event)
	if err != nil {
		taskFailure(c, err)
		return
	}
	ok(c, report)
}

func (s Server) replayTaskEvent(c *gin.Context) {
	var event tasks.Event
	if err := c.ShouldBindJSON(&event); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.Type) == "" || strings.TrimSpace(event.PlayerUID) == "" {
		fail(c, http.StatusBadRequest, "task_event_invalid", "event_id, type, and player_uid are required for replay")
		return
	}
	service, err := s.taskService()
	if err != nil {
		taskFailure(c, err)
		return
	}
	before, err := service.DiagnoseEvent(c.Request.Context(), event)
	if err != nil {
		taskFailure(c, err)
		return
	}
	pointService, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	updates, err := service.ProcessEvent(c.Request.Context(), event, pointService)
	if err != nil {
		taskFailure(c, err)
		return
	}
	ok(c, gin.H{"event": event, "diagnostic": before, "updates": updates, "count": len(updates)})
}

func (s Server) retryTaskRewards(c *gin.Context) {
	service, err := s.taskService()
	if err != nil {
		taskFailure(c, err)
		return
	}
	economyService, err := s.economyService()
	if err != nil {
		economyFailure(c, err)
		return
	}
	result, err := service.RetryPendingRewards(c.Request.Context(), economyService, economyQueryInt(c, "limit", 100))
	if err != nil {
		taskFailure(c, err)
		return
	}
	ok(c, result)
}

func taskFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, tasks.ErrInvalidDefinition):
		fail(c, http.StatusBadRequest, "task_definition_invalid", err.Error())
	case errors.Is(err, sql.ErrNoRows):
		fail(c, http.StatusNotFound, "task_not_found", "task not found")
	case errors.Is(err, tasks.ErrRewardDelivery):
		fail(c, http.StatusBadGateway, "task_reward_delivery_failed", err.Error())
	default:
		fail(c, http.StatusInternalServerError, "task_operation_failed", err.Error())
	}
}
