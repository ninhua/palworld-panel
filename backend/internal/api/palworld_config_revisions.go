package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/db"
	"palpanel/internal/palconfig"
	"palpanel/internal/server"
)

type palworldConfigRevisionView struct {
	ID             string   `json:"id"`
	RevisionSHA256 string   `json:"revision_sha256"`
	ParentSHA256   string   `json:"parent_sha256,omitempty"`
	Source         string   `json:"source"`
	ChangedFields  []string `json:"changed_fields"`
	CreatedAt      string   `json:"created_at"`
	Current        bool     `json:"current"`
}

func configRevisionView(revision db.ConfigRevision, current bool) palworldConfigRevisionView {
	return palworldConfigRevisionView{
		ID: revision.ID, RevisionSHA256: revision.RevisionSHA256,
		ParentSHA256: revision.ParentSHA256, Source: revision.Source,
		ChangedFields: revision.ChangedFields, CreatedAt: revision.CreatedAt,
		Current: current,
	}
}

func (s Server) listPalworldConfigRevisions(c *gin.Context) {
	if _, err := s.server.EnsurePalworldConfigRevision(c.Request.Context()); err != nil {
		fail(c, http.StatusInternalServerError, "config_revision_capture_failed", err.Error())
		return
	}
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			fail(c, http.StatusBadRequest, "config_revision_limit_invalid", "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	current, err := server.PalworldConfigRevision(s.cfg.PalWorldSettingsPath())
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_revision_failed", err.Error())
		return
	}
	revisions, err := s.store.ListConfigRevisions(c.Request.Context(), limit)
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_revision_list_failed", err.Error())
		return
	}
	items := make([]palworldConfigRevisionView, 0, len(revisions))
	currentMarked := false
	for _, revision := range revisions {
		isCurrent := !currentMarked && revision.RevisionSHA256 == current
		items = append(items, configRevisionView(revision, isCurrent))
		currentMarked = currentMarked || isCurrent
	}
	ok(c, gin.H{"current_revision_sha256": current, "retention": 50, "items": items})
}

func (s Server) getPalworldConfigRevisionDiff(c *gin.Context) {
	revision, err := s.store.GetConfigRevision(c.Request.Context(), strings.TrimSpace(c.Param("id")))
	if err != nil {
		if err == sql.ErrNoRows {
			fail(c, http.StatusNotFound, "config_revision_not_found", "config revision not found")
			return
		}
		fail(c, http.StatusInternalServerError, "config_revision_read_failed", err.Error())
		return
	}
	diff, err := s.server.DiffPalworldConfigRevision(c.Request.Context(), revision)
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_revision_diff_failed", err.Error())
		return
	}
	ok(c, diff)
}

func (s Server) restorePalworldConfigRevision(c *gin.Context) {
	active, err := s.server.PalworldConfigApplyInProgress(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_revision_apply_state_failed", err.Error())
		return
	}
	if active {
		fail(c, http.StatusConflict, "config_apply_in_progress", "a Palworld config apply transaction is already in progress")
		return
	}
	var request struct {
		Confirm bool `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || !request.Confirm {
		fail(c, http.StatusBadRequest, "config_revision_confirmation_required", "confirm must be true")
		return
	}
	if _, err := s.server.EnsurePalworldConfigRevision(c.Request.Context()); err != nil {
		fail(c, http.StatusInternalServerError, "config_revision_capture_failed", err.Error())
		return
	}
	revision, err := s.store.GetConfigRevision(c.Request.Context(), strings.TrimSpace(c.Param("id")))
	if err != nil {
		if err == sql.ErrNoRows {
			fail(c, http.StatusNotFound, "config_revision_not_found", "config revision not found")
			return
		}
		fail(c, http.StatusInternalServerError, "config_revision_read_failed", err.Error())
		return
	}
	target, err := s.server.ReadPalworldConfigRevision(c.Request.Context(), revision)
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_revision_snapshot_failed", err.Error())
		return
	}
	current, err := server.ReadPalworldConfigSnapshot(s.cfg.PalWorldSettingsPath())
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_read_failed", err.Error())
		return
	}
	if target.Revision == current.Revision {
		fail(c, http.StatusConflict, "config_revision_already_current", "selected config revision is already active")
		return
	}
	if hasPalconfigErrors(palconfig.Validate(target.Document.Settings)) {
		fail(c, http.StatusConflict, "config_revision_schema_incompatible", "selected config revision is not valid under the current schema")
		return
	}
	diff, err := s.server.DiffPalworldConfigRevision(c.Request.Context(), revision)
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_revision_diff_failed", err.Error())
		return
	}
	modified := make(map[string]bool, len(diff.Changes))
	for _, change := range diff.Changes {
		modified[change.Field] = true
	}
	draft, err := s.createPalworldConfigDraft(c.Request.Context(), target.Document, modified, current.Revision)
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_revision_restore_draft_failed", err.Error())
		return
	}
	response := s.palworldConfigResponse(c.Request.Context(), target.Document, current.Revision, &draft)
	response["restored_from_revision"] = configRevisionView(revision, false)
	ok(c, response)
}
