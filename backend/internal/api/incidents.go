package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/db"
)

type incidentActionRequest struct {
	Confirm bool   `json:"confirm"`
	Message string `json:"message"`
}

func (s Server) listIncidents(c *gin.Context) {
	limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limitErr != nil || limit < 1 || limit > 100 || offsetErr != nil || offset < 0 || offset > 1000000 {
		fail(c, http.StatusBadRequest, "incident_pagination_invalid", "limit must be 1-100 and offset must be 0-1000000")
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && status != "open" && status != "acknowledged" && status != "resolved" {
		fail(c, http.StatusBadRequest, "incident_status_invalid", "status must be open, acknowledged, or resolved")
		return
	}
	severity := strings.TrimSpace(c.Query("severity"))
	if severity != "" && severity != "info" && severity != "warning" && severity != "error" && severity != "critical" {
		fail(c, http.StatusBadRequest, "incident_severity_invalid", "severity must be info, warning, error, or critical")
		return
	}
	filter := db.IncidentListFilter{
		Status: status, Severity: severity, Source: strings.TrimSpace(c.Query("source")), Query: strings.TrimSpace(c.Query("q")), Limit: limit, Offset: offset,
	}
	items, total, err := s.store.ListIncidents(c.Request.Context(), filter)
	if err != nil {
		fail(c, http.StatusInternalServerError, "incident_list_failed", err.Error())
		return
	}
	summary, err := s.store.IncidentSummary(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "incident_summary_failed", err.Error())
		return
	}
	webhook := map[string]any{"enabled": false}
	if s.incidents != nil {
		status := s.incidents.Status()
		webhook = map[string]any{"enabled": status.Enabled, "target_host": status.TargetHost, "signed": status.Signed,
			"timeout_seconds": status.TimeoutSeconds, "max_attempts": status.MaxAttempts}
	}
	ok(c, gin.H{"items": items, "total": total, "limit": filter.Limit, "offset": filter.Offset, "summary": summary, "webhook": webhook})
}

func (s Server) getIncident(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if err := s.store.ValidateIncidentID(id); err != nil {
		fail(c, http.StatusBadRequest, "incident_id_invalid", "incident id is invalid")
		return
	}
	item, err := s.store.GetIncident(c.Request.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		fail(c, http.StatusNotFound, "incident_not_found", "incident not found")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "incident_read_failed", err.Error())
		return
	}
	events, err := s.store.ListIncidentEvents(c.Request.Context(), id, 100)
	if err != nil {
		fail(c, http.StatusInternalServerError, "incident_events_failed", err.Error())
		return
	}
	deliveries, err := s.store.ListIncidentDeliveries(c.Request.Context(), id, 20)
	if err != nil {
		fail(c, http.StatusInternalServerError, "incident_deliveries_failed", err.Error())
		return
	}
	ok(c, gin.H{"incident": item, "events": events, "deliveries": deliveries})
}

func (s Server) incidentAction(status string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.Param("id"))
		if err := s.store.ValidateIncidentID(id); err != nil {
			fail(c, http.StatusBadRequest, "incident_id_invalid", "incident id is invalid")
			return
		}
		var request incidentActionRequest
		if err := c.ShouldBindJSON(&request); err != nil || !request.Confirm {
			fail(c, http.StatusBadRequest, "incident_confirmation_required", "confirm must be true")
			return
		}
		actor := CurrentPrincipal(c).Name
		var item db.Incident
		var event db.IncidentEvent
		var err error
		if status == "open" {
			item, event, err = s.store.ReopenIncident(c.Request.Context(), id, actor, request.Message)
		} else {
			item, event, err = s.store.SetIncidentStatus(c.Request.Context(), id, status, actor, request.Message)
		}
		if errors.Is(err, sql.ErrNoRows) {
			fail(c, http.StatusNotFound, "incident_not_found", "incident not found")
			return
		}
		if err != nil {
			fail(c, http.StatusConflict, "incident_transition_failed", err.Error())
			return
		}
		ok(c, gin.H{"incident": item, "event": event})
	}
}

func (s Server) testIncidentWebhook(c *gin.Context) {
	var request struct {
		Confirm bool `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || !request.Confirm {
		fail(c, http.StatusBadRequest, "incident_webhook_confirmation_required", "confirm must be true")
		return
	}
	if s.incidents == nil || !s.incidents.Enabled() {
		fail(c, http.StatusConflict, "incident_webhook_disabled", "incident webhook is disabled")
		return
	}
	if err := s.incidents.Test(c.Request.Context()); err != nil {
		fail(c, http.StatusBadGateway, "incident_webhook_test_failed", err.Error())
		return
	}
	ok(c, gin.H{"delivered": true, "target_host": s.incidents.Status().TargetHost})
}
