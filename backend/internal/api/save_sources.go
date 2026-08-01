package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/appconfig"
	"palpanel/internal/breeding"
	"palpanel/internal/db"
	"palpanel/internal/playerpresence"
	"palpanel/internal/saveindex"
)

type runtimeSaveView struct {
	Available    bool   `json:"available"`
	ServerActive bool   `json:"server_active"`
	SourceID     string `json:"source_id"`
	SourceName   string `json:"source_name"`
	WorldID      string `json:"world_id"`
	State        string `json:"state"`
}

func initializeSaveSources(cfg appconfig.Config, store *db.Store, manager *saveindex.Manager) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	active, err := store.ActiveSaveSource(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		server := db.SaveSource{ID: "server", Name: "当前服务器存档", Kind: "server", Active: true}
		if store.UpsertSaveSource(ctx, server) == nil {
			_ = store.ActivateSaveSource(ctx, server.ID)
		}
		return
	}
	if err == nil && active.Kind != "server" {
		manager.SetSourcePath(active.Path)
	}
}

func (s Server) listSaveSources(c *gin.Context) {
	items, err := s.store.ListSaveSources(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "save_sources_list_failed", err.Error())
		return
	}
	status := s.saveIndex.Status(c.Request.Context())
	runtimeSave := runtimeSaveView{
		SourceID:   "server",
		SourceName: "当前服务器存档",
		State:      "unknown",
	}
	if scope, scopeErr := playerpresence.ResolveServerScope(s.cfg.ServerDirectory()); scopeErr == nil {
		runtimeSave.Available = true
		runtimeSave.WorldID = scope.WorldID
	}
	runtimeSave.SourceName = reconcileRuntimeSaveSourceName(c.Request.Context(), s.store, items, runtimeSave.WorldID)
	if serverStatus, statusErr := s.server.Status(c.Request.Context()); statusErr == nil {
		runtimeSave.State = strings.ToLower(strings.TrimSpace(serverStatus.Container.Status))
		if runtimeSave.State == "" && !serverStatus.Container.Exists {
			runtimeSave.State = "not_found"
		}
		switch runtimeSave.State {
		case "running", "starting", "restarting":
			runtimeSave.ServerActive = true
		}
	}
	ok(c, gin.H{"items": items, "active_status": status, "runtime_save": runtimeSave})
}

func (s Server) importSaveSource(c *gin.Context) {
	s.dispatchSaveSourceImport(c)
}

func (s Server) activateSaveSource(c *gin.Context) {
	source, err := s.store.GetSaveSource(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, http.StatusNotFound, "save_source_not_found", "save source was not found")
		return
	}
	if source.Kind != "server" {
		if _, err := os.Stat(filepath.Join(source.Path, "Level.sav")); err != nil {
			fail(c, http.StatusConflict, "save_source_unavailable", "the imported Level.sav is unavailable")
			return
		}
	}
	previous, previousErr := s.store.ActiveSaveSource(c.Request.Context())
	if err := s.store.ActivateSaveSource(c.Request.Context(), source.ID); err != nil {
		fail(c, http.StatusInternalServerError, "save_source_activate_failed", err.Error())
		return
	}
	if source.Kind == "server" {
		s.saveIndex.SetSourcePath("")
	} else {
		s.saveIndex.SetSourcePath(source.Path)
	}
	index, status, rebuildErr := s.saveIndex.Rebuild(c.Request.Context())
	if rebuildErr != nil {
		if previousErr == nil {
			_ = s.store.ActivateSaveSource(context.Background(), previous.ID)
			if previous.Kind == "server" {
				s.saveIndex.SetSourcePath("")
			} else {
				s.saveIndex.SetSourcePath(previous.Path)
			}
		}
		fail(c, http.StatusBadGateway, "save_source_rebuild_failed", rebuildErr.Error())
		return
	}
	_ = s.store.UpdateSaveSourceIndex(c.Request.Context(), source.ID, index.Snapshot.Fingerprint, index.Parser, index.Warnings, index.GeneratedAt)
	s.triggerAstrBotCatalogSync()
	ok(c, gin.H{"source": source, "status": status, "counts": index.Counts})
}

func (s Server) renameSaveSource(c *gin.Context) {
	source, err := s.store.GetSaveSource(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, http.StatusNotFound, "save_source_not_found", "save source was not found")
		return
	}
	var input struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		fail(c, http.StatusBadRequest, "save_source_name_required", "save source name is required")
		return
	}
	source.Name = strings.TrimSpace(input.Name)
	if len([]rune(source.Name)) > 80 {
		fail(c, http.StatusBadRequest, "save_source_name_too_long", "save source name is too long")
		return
	}
	if err := s.store.UpsertSaveSource(c.Request.Context(), source); err != nil {
		fail(c, http.StatusInternalServerError, "save_source_rename_failed", err.Error())
		return
	}
	ok(c, source)
}

func (s Server) rebuildSaveSource(c *gin.Context) {
	source, err := s.store.GetSaveSource(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, http.StatusNotFound, "save_source_not_found", "save source was not found")
		return
	}
	if !source.Active {
		fail(c, http.StatusConflict, "save_source_not_active", "activate the save source before rebuilding it")
		return
	}
	s.saveIndex.Invalidate()
	index, status, err := s.saveIndex.Rebuild(c.Request.Context())
	if err != nil {
		fail(c, http.StatusBadGateway, "save_source_rebuild_failed", err.Error())
		return
	}
	_ = s.store.UpdateSaveSourceIndex(c.Request.Context(), source.ID, index.Snapshot.Fingerprint, index.Parser, index.Warnings, index.GeneratedAt)
	s.triggerAstrBotCatalogSync()
	ok(c, gin.H{"status": status, "counts": index.Counts})
}

func (s Server) deleteSaveSource(c *gin.Context) {
	source, err := s.store.GetSaveSource(c.Request.Context(), c.Param("id"))
	if err != nil || source.Kind == "server" || source.Active {
		fail(c, http.StatusConflict, "save_source_delete_rejected", "active and server save sources cannot be deleted")
		return
	}
	if err := s.store.DeleteSaveSource(c.Request.Context(), source.ID); err != nil {
		fail(c, http.StatusConflict, "save_source_delete_failed", err.Error())
		return
	}
	if source.Kind == "import" && pathWithin(s.cfg.SaveSourcesDir, source.Path) {
		_ = s.saveIndex.RemoveHistoryForWorld(source.Path)
		_ = os.RemoveAll(filepath.Join(s.cfg.SaveSourcesDir, source.ID))
	}
	ok(c, gin.H{"deleted": true})
}

func (s Server) breedingCatalog(c *gin.Context) {
	raw, err := s.breeding.Catalog(c.Request.Context())
	if err != nil {
		fail(c, http.StatusServiceUnavailable, "palcalc_unavailable", err.Error())
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

func (s Server) submitBreedingJob(c *gin.Context) {
	var input breeding.SubmitInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "breeding_request_invalid", err.Error())
		return
	}
	principal := CurrentPrincipal(c)
	job, err := s.breeding.Submit(c.Request.Context(), breedingSubject(principal), input, nil)
	if err != nil {
		fail(c, http.StatusConflict, "breeding_submit_failed", err.Error())
		return
	}
	accepted(c, job)
}

func (s Server) breedingHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	items, err := s.breeding.History(c.Request.Context(), breedingSubject(CurrentPrincipal(c)), limit)
	if err != nil {
		fail(c, http.StatusInternalServerError, "breeding_history_failed", err.Error())
		return
	}
	for position := range items {
		items[position].RequestJSON = ""
		items[position].ResultJSON = ""
	}
	ok(c, items)
}

func (s Server) breedingResult(c *gin.Context) {
	item, raw, err := s.breeding.Result(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, http.StatusNotFound, "breeding_result_not_found", "breeding result was not found")
		return
	}
	if item.Subject != breedingSubject(CurrentPrincipal(c)) && CurrentPrincipal(c).Role != RoleAdmin {
		fail(c, http.StatusForbidden, "permission_denied", "permission denied")
		return
	}
	if item.Status != "completed" || len(raw) == 0 {
		ok(c, gin.H{"job_id": item.JobID, "status": item.Status})
		return
	}
	var result any
	if err := json.Unmarshal(raw, &result); err != nil {
		fail(c, http.StatusInternalServerError, "breeding_result_invalid", err.Error())
		return
	}
	ok(c, gin.H{"job_id": item.JobID, "status": item.Status, "fingerprint": item.Fingerprint, "stale": s.breedingResultStale(c.Request.Context(), item.Fingerprint), "result": result})
}

func (s Server) breedingResultStale(ctx context.Context, fingerprint string) bool {
	index, status, err := s.saveIndex.Current(ctx)
	return err == nil && status.State == "ready" && fingerprint != "" && index.Snapshot.Fingerprint != "" && fingerprint != index.Snapshot.Fingerprint
}

func (s Server) controlBreedingJob(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := s.breeding.Control(c.Request.Context(), c.Param("id"), action)
		if err != nil {
			fail(c, http.StatusBadGateway, "breeding_control_failed", err.Error())
			return
		}
		var result any
		_ = json.Unmarshal(raw, &result)
		ok(c, result)
	}
}

func breedingSubject(principal Principal) string {
	if principal.UserID != "" {
		return "user:" + principal.UserID
	}
	return "role:" + string(principal.Role) + ":" + principal.Name
}

func pathWithin(root, target string) bool {
	rootAbs, err1 := filepath.Abs(root)
	targetAbs, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil {
		return false
	}
	relative, err := filepath.Rel(rootAbs, targetAbs)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
