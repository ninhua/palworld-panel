package api

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/tasks"
)

func init() {
	patchFeatures = append(patchFeatures,
		"event-task-engine",
		"daily-weekly-once-tasks",
		"idempotent-task-rewards",
		"task-operations-api",
	)
}

func (s Server) registerTaskRoutes(api *gin.RouterGroup) {
	group := api.Group("/tasks")
	group.GET("", Require(PermRead), s.listTasks)
	group.POST("", Require(PermConfigWrite), s.createTask)
	group.GET("/progress/:player_uid", Require(PermRead), s.playerTaskProgress)
	group.POST("/maintenance/retry-rewards", Require(PermPlayersWrite), s.retryTaskRewards)
	group.PUT("/:id", Require(PermConfigWrite), s.updateTask)
	group.DELETE("/:id", Require(PermConfigWrite), s.archiveTask)
}

func (s Server) taskService() (*tasks.Service, error) {
	return tasks.ForPath(s.cfg.DBPath, strings.TrimSpace(os.Getenv("PALPANEL_OPERATIONS_TIMEZONE")))
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
