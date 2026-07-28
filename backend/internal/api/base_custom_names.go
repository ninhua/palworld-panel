package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"palpanel/internal/db"
	"palpanel/internal/pallocalize"
	"palpanel/internal/saveindex"
)

const (
	baseCustomNamesKeyPrefix = "base_custom_names:v1:"
	baseCustomNameMaxRunes   = 64
)

var baseCustomNamesMu sync.Mutex

type baseCustomNameInput struct {
	Name string `json:"name"`
}

func (s Server) putBaseCustomName(c *gin.Context) {
	var input baseCustomNameInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "base_custom_name_invalid", "a JSON name is required")
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		fail(c, http.StatusBadRequest, "base_custom_name_required", "base custom name is required")
		return
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > baseCustomNameMaxRunes {
		fail(c, http.StatusBadRequest, "base_custom_name_too_long", "base custom name must not exceed 64 characters")
		return
	}

	base, source, found := s.resolveActiveBase(c)
	if !found {
		return
	}
	baseCustomNamesMu.Lock()
	defer baseCustomNamesMu.Unlock()
	names, err := s.loadBaseCustomNames(c, source.ID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "base_custom_names_read_failed", err.Error())
		return
	}
	names[base.ID] = name
	if err := s.saveBaseCustomNames(c, source.ID, names); err != nil {
		fail(c, http.StatusInternalServerError, "base_custom_name_save_failed", err.Error())
		return
	}
	okResponse := flattenBaseWithCustomName(base, name)
	okResponse["source_id"] = source.ID
	ok(c, gin.H{"base": okResponse})
}

func (s Server) deleteBaseCustomName(c *gin.Context) {
	base, source, found := s.resolveActiveBase(c)
	if !found {
		return
	}
	baseCustomNamesMu.Lock()
	defer baseCustomNamesMu.Unlock()
	names, err := s.loadBaseCustomNames(c, source.ID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "base_custom_names_read_failed", err.Error())
		return
	}
	delete(names, base.ID)
	if err := s.saveBaseCustomNames(c, source.ID, names); err != nil {
		fail(c, http.StatusInternalServerError, "base_custom_name_delete_failed", err.Error())
		return
	}
	view := flattenBaseWithCustomName(base, "")
	view["source_id"] = source.ID
	ok(c, gin.H{"base": view, "deleted": true})
}

func (s Server) resolveActiveBase(c *gin.Context) (saveindex.Base, db.SaveSource, bool) {
	source, err := s.store.ActiveSaveSource(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "save_source_read_failed", err.Error())
		return saveindex.Base{}, db.SaveSource{}, false
	}
	index, status, indexErr := s.currentSaveIndex(c)
	if indexErr != nil && !status.Stale && status.State != "disabled" {
		fail(c, http.StatusServiceUnavailable, "save_index_unavailable", indexErr.Error())
		return saveindex.Base{}, db.SaveSource{}, false
	}
	id := c.Param("id")
	for _, base := range index.Bases {
		if matchesID(id, base.ID) {
			return base, source, true
		}
	}
	fail(c, http.StatusNotFound, "base_not_found", "base not found")
	return saveindex.Base{}, db.SaveSource{}, false
}

func (s Server) activeBaseCustomNames(c *gin.Context) (map[string]string, string, error) {
	source, err := s.store.ActiveSaveSource(c.Request.Context())
	if err != nil {
		return nil, "", err
	}
	names, err := s.loadBaseCustomNames(c, source.ID)
	return names, source.ID, err
}

func (s Server) loadBaseCustomNames(c *gin.Context, sourceID string) (map[string]string, error) {
	raw, found, err := s.store.GetKV(c.Request.Context(), baseCustomNamesKey(sourceID))
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	if !found || strings.TrimSpace(raw) == "" {
		return names, nil
	}
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		return nil, err
	}
	return names, nil
}

func (s Server) saveBaseCustomNames(c *gin.Context, sourceID string, names map[string]string) error {
	key := baseCustomNamesKey(sourceID)
	if len(names) == 0 {
		return s.store.DeleteKV(c.Request.Context(), key)
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		return err
	}
	return s.store.SetKV(c.Request.Context(), key, string(encoded))
}

func baseCustomNamesKey(sourceID string) string {
	return baseCustomNamesKeyPrefix + sourceID
}

func flattenBaseWithCustomName(base saveindex.Base, customName string) gin.H {
	view := flattenBase(base)
	customName = strings.TrimSpace(customName)
	view["custom_name"] = customName
	view["has_custom_name"] = customName != ""
	if customName != "" {
		view["name"] = customName
	}
	return view
}

func baseSearchNames(base saveindex.Base, customName string) []string {
	return []string{
		base.ID,
		base.Name,
		pallocalize.BaseName(base.Name),
		customName,
		base.GuildName,
		pallocalize.GuildName(base.GuildName),
		base.GuildID,
	}
}
