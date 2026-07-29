package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/db"
	"palpanel/internal/id"
	"palpanel/internal/palconfig"
	"palpanel/internal/server"
)

const configDraftStatus = "draft"

type palworldConfigUpdate struct {
	Settings     map[string]any
	ClearSecrets map[string]bool
}

func decodePalworldConfigUpdate(c *gin.Context) (palworldConfigUpdate, error) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		return palworldConfigUpdate{}, err
	}
	clearValue, hasClearValue := raw["clear_secrets"]
	updates := raw
	if nested, found := raw["settings"]; found {
		settings, ok := nested.(map[string]any)
		if !ok {
			return palworldConfigUpdate{}, fmt.Errorf("settings must be an object")
		}
		updates = settings
	} else {
		delete(updates, "clear_secrets")
	}
	clear := map[string]bool{}
	if hasClearValue {
		values := clearValue
		items, ok := values.([]any)
		if !ok {
			return palworldConfigUpdate{}, fmt.Errorf("clear_secrets must be an array")
		}
		for _, item := range items {
			key, ok := item.(string)
			if !ok || !isPalworldSecret(key) {
				return palworldConfigUpdate{}, fmt.Errorf("clear_secrets contains an unsupported field")
			}
			clear[key] = true
		}
	}
	return palworldConfigUpdate{Settings: updates, ClearSecrets: clear}, nil
}

func mergePalworldConfigUpdate(current palconfig.Document, request palworldConfigUpdate) (palconfig.Settings, map[string]bool, error) {
	updates := map[string]any{}
	modified := map[string]bool{}
	for key, value := range request.Settings {
		if isPalworldSecret(key) {
			text, ok := value.(string)
			if !ok {
				return nil, nil, fmt.Errorf("%s must be a string", key)
			}
			if text == "" && !request.ClearSecrets[key] {
				continue
			}
		}
		updates[key] = value
		modified[key] = true
	}
	for key := range request.ClearSecrets {
		updates[key] = ""
		modified[key] = true
	}
	return palconfig.Merge(current.Settings, updates), modified, nil
}

func (s Server) createPalworldConfigDraft(ctx context.Context, document palconfig.Document, modified map[string]bool, revision string) (db.ConfigDraft, error) {
	draftDir := filepath.Join(s.cfg.DataDir, "config-drafts")
	if err := os.MkdirAll(draftDir, 0o700); err != nil {
		return db.ConfigDraft{}, err
	}
	if err := os.Chmod(draftDir, 0o700); err != nil {
		return db.ConfigDraft{}, err
	}
	if err := server.HardenPalworldConfigPrivatePath(ctx, draftDir); err != nil {
		return db.ConfigDraft{}, err
	}
	draft := db.ConfigDraft{ID: id.New("cfg"), BaseSHA256: revision, Status: configDraftStatus}
	for key := range modified {
		draft.ModifiedFields = append(draft.ModifiedFields, key)
	}
	sort.Strings(draft.ModifiedFields)
	draft.DraftPath = filepath.Join(draftDir, draft.ID+".ini")
	content := []byte(palconfig.SerializeDocument(document, modified))
	file, err := os.OpenFile(draft.DraftPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return db.ConfigDraft{}, err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(draft.DraftPath)
		return db.ConfigDraft{}, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(draft.DraftPath)
		return db.ConfigDraft{}, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(draft.DraftPath)
		return db.ConfigDraft{}, err
	}
	if err := server.HardenPalworldConfigPrivatePath(ctx, draft.DraftPath); err != nil {
		_ = os.Remove(draft.DraftPath)
		return db.ConfigDraft{}, err
	}
	replaced, err := s.store.CreateConfigDraftReplacing(ctx, draft)
	if err != nil {
		_ = os.Remove(draft.DraftPath)
		return db.ConfigDraft{}, err
	}
	_ = replaced
	if err := s.server.CleanupConfigPrivateFiles(ctx); err != nil {
		return db.ConfigDraft{}, fmt.Errorf("remove superseded config draft: %w", err)
	}
	return s.store.GetConfigDraft(ctx, draft.ID)
}

func (s Server) cleanupExpiredConfigDrafts(ctx context.Context) error {
	return s.server.MaintainConfigDrafts(ctx)
}

func (s Server) palworldConfigResponse(ctx context.Context, document palconfig.Document, revision string, createdDraft *db.ConfigDraft) gin.H {
	settings := palconfig.Settings{}
	for key, value := range document.Settings {
		if !isPalworldSecret(key) {
			settings[key] = value
		}
	}
	secretState := gin.H{
		"admin_password":  gin.H{"configured": strings.TrimSpace(document.Settings["AdminPassword"]) != ""},
		"server_password": gin.H{"configured": strings.TrimSpace(document.Settings["ServerPassword"]) != ""},
	}
	draft := createdDraft
	if draft == nil {
		if latest, err := s.store.LatestConfigDraft(ctx); err == nil {
			draft = &latest
		}
	}
	pending := false
	if value, _, err := s.store.GetKV(ctx, "pending_restart"); err == nil {
		pending = strings.EqualFold(value, "true")
	}
	response := gin.H{
		"path": s.cfg.PalWorldSettingsPath(), "settings": settings,
		"revision_sha256": revision, "secret_state": secretState,
		"format_issues": palconfig.FormatIssues(document), "issues": palconfig.Validate(document.Settings),
		"pending_restart": pending,
	}
	if draft != nil {
		response["draft"] = draft
	}
	return response
}

func (s Server) applyPalworldConfig(c *gin.Context) {
	var request struct {
		DraftID string `json:"draft_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.DraftID) == "" {
		fail(c, http.StatusBadRequest, "invalid_json", "draft_id is required")
		return
	}
	if err := s.cleanupExpiredConfigDrafts(c.Request.Context()); err != nil {
		fail(c, http.StatusInternalServerError, "config_draft_cleanup_failed", err.Error())
		return
	}
	draft, err := s.store.GetConfigDraft(c.Request.Context(), strings.TrimSpace(request.DraftID))
	if err != nil {
		if err == sql.ErrNoRows {
			fail(c, http.StatusNotFound, "config_draft_not_found", "config draft not found")
			return
		}
		fail(c, http.StatusInternalServerError, "config_draft_read_failed", err.Error())
		return
	}
	if draft.Status == "expired" {
		fail(c, http.StatusGone, "config_draft_expired", "config draft expired after 24 hours")
		return
	}
	revision, err := server.PalworldConfigRevision(s.cfg.PalWorldSettingsPath())
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_revision_failed", err.Error())
		return
	}
	if revision != draft.BaseSHA256 {
		_ = s.store.UpdateConfigDraftStatusAndQueueCleanup(c.Request.Context(), draft.ID, "stale", "", "config_draft")
		_ = s.server.CleanupConfigPrivateFiles(c.Request.Context())
		fail(c, http.StatusConflict, "config_draft_stale", "active PalWorldSettings.ini changed after the draft was created")
		return
	}
	job, err := s.server.ApplyPalworldConfig(c.Request.Context(), draft, func(ctx context.Context, wait int, message string) error {
		client := s.palworldREST()
		if _, err := client.Do(ctx, http.MethodPost, "save", nil); err != nil {
			return err
		}
		_, err := client.Do(ctx, http.MethodPost, "shutdown", gin.H{"waittime": wait, "message": message})
		return err
	}, func(ctx context.Context, settings palconfig.Settings, modifiedFields []string) error {
		return s.verifyPalworldConfigReadiness(ctx, settings, modifiedFields)
	})
	if err != nil {
		if crashGuardOperationFailure(c, err) {
			return
		}
		fail(c, http.StatusBadRequest, "config_apply_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	accepted(c, job)
}

func (s Server) verifyPalworldConfigReadiness(ctx context.Context, settings palconfig.Settings, modifiedFields []string) error {
	if !strings.EqualFold(strings.TrimSpace(settings["RESTAPIEnabled"]), "True") {
		return nil
	}
	client := s.palworldRESTForSettings(settings)
	response, err := client.Do(ctx, http.MethodGet, "settings", nil)
	if err != nil {
		return err
	}
	body, ok := response.Body.(map[string]any)
	if !ok {
		return fmt.Errorf("Palworld REST settings response is not an object")
	}
	for _, key := range modifiedFields {
		if isPalworldSecret(key) {
			continue
		}
		actual, observable := body[key]
		if !observable {
			continue
		}
		actualText, observable := restSettingText(actual)
		if !observable {
			continue
		}
		expectedValue, err := palconfig.NormalizeFieldValue(key, settings[key])
		if err != nil {
			return err
		}
		actualValue, err := palconfig.NormalizeFieldValue(key, actualText)
		if err != nil || actualValue != expectedValue {
			return fmt.Errorf("Palworld REST settings mismatch for %s", key)
		}
	}
	return nil
}

func restSettingText(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool:
		return strconv.FormatBool(typed), true
	case json.Number:
		return typed.String(), true
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), true
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := restSettingText(item)
			if !ok {
				return "", false
			}
			items = append(items, text)
		}
		return "(" + strings.Join(items, ",") + ")", true
	default:
		return "", false
	}
}

func isPalworldSecret(key string) bool {
	return key == "AdminPassword" || key == "ServerPassword"
}
