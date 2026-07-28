package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"palpanel/internal/saveindex"
)

const (
	playerAnnotationsKeyPrefix = "player_annotations:v1:"
	playerNoteMaxRunes         = 500
	playerTagMaxRunes          = 24
	playerTagMaxCount          = 8
)

var playerAnnotationsMu sync.Mutex

type playerAnnotation struct {
	Note      string   `json:"note"`
	Tags      []string `json:"tags"`
	UpdatedAt string   `json:"updated_at"`
}

type playerAnnotationInput struct {
	Note string   `json:"note"`
	Tags []string `json:"tags"`
}

func (s Server) putPlayerAnnotation(c *gin.Context) {
	var input playerAnnotationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "player_annotation_invalid", "a JSON note and tags payload is required")
		return
	}
	annotation, err := normalizePlayerAnnotation(input)
	if err != nil {
		fail(c, http.StatusBadRequest, "player_annotation_invalid", err.Error())
		return
	}
	player, sourceID, found := s.resolvePlayerAnnotationTarget(c)
	if !found {
		return
	}

	playerAnnotationsMu.Lock()
	defer playerAnnotationsMu.Unlock()
	annotations, err := s.loadPlayerAnnotations(c, sourceID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "player_annotations_read_failed", err.Error())
		return
	}
	storageID := playerAnnotationStorageID(player)
	if annotation.Note == "" && len(annotation.Tags) == 0 {
		delete(annotations, storageID)
	} else {
		annotation.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		annotations[storageID] = annotation
	}
	if err := s.savePlayerAnnotations(c, sourceID, annotations); err != nil {
		fail(c, http.StatusInternalServerError, "player_annotation_save_failed", err.Error())
		return
	}
	view := flattenPlayerWithAnnotation(player, onlinePlayersResult{}, annotation)
	ok(c, gin.H{"player": view, "source_id": sourceID})
}

func (s Server) deletePlayerAnnotation(c *gin.Context) {
	player, sourceID, found := s.resolvePlayerAnnotationTarget(c)
	if !found {
		return
	}

	playerAnnotationsMu.Lock()
	defer playerAnnotationsMu.Unlock()
	annotations, err := s.loadPlayerAnnotations(c, sourceID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "player_annotations_read_failed", err.Error())
		return
	}
	delete(annotations, playerAnnotationStorageID(player))
	if err := s.savePlayerAnnotations(c, sourceID, annotations); err != nil {
		fail(c, http.StatusInternalServerError, "player_annotation_delete_failed", err.Error())
		return
	}
	ok(c, gin.H{
		"player":    flattenPlayerWithAnnotation(player, onlinePlayersResult{}, playerAnnotation{}),
		"source_id": sourceID,
		"deleted":   true,
	})
}

func (s Server) resolvePlayerAnnotationTarget(c *gin.Context) (saveindex.Player, string, bool) {
	index, status, view, overlay, valid, err := s.currentPlayerIndex(c)
	if !valid {
		return saveindex.Player{}, "", false
	}
	var online onlinePlayersResult
	if overlay {
		online = s.onlinePlayers(c)
		status = statusWithOnlineState(status, online)
	}
	if err != nil && !status.Stale && status.State != "disabled" {
		fail(c, http.StatusServiceUnavailable, "save_index_unavailable", err.Error())
		return saveindex.Player{}, "", false
	}
	players := playersForView(index.Players, online, overlay)
	if player, found := findPlayerAnnotationTarget(c.Param("id"), players); found {
		return player, view.SourceID, true
	}
	fail(c, http.StatusNotFound, "player_not_found", "player not found")
	return saveindex.Player{}, "", false
}

func findPlayerAnnotationTarget(id string, players []saveindex.Player) (saveindex.Player, bool) {
	for _, player := range players {
		if matchesID(id, player.PlayerUID, player.SteamID) {
			return player, true
		}
	}
	return saveindex.Player{}, false
}

func normalizePlayerAnnotation(input playerAnnotationInput) (playerAnnotation, error) {
	note := strings.TrimSpace(strings.ReplaceAll(input.Note, "\r\n", "\n"))
	if !utf8.ValidString(note) || utf8.RuneCountInString(note) > playerNoteMaxRunes {
		return playerAnnotation{}, annotationValidationError("player note must not exceed 500 characters")
	}
	if len(input.Tags) > playerTagMaxCount {
		return playerAnnotation{}, annotationValidationError("player annotation must not contain more than 8 tags")
	}
	tags := make([]string, 0, len(input.Tags))
	seen := make(map[string]struct{}, len(input.Tags))
	for _, rawTag := range input.Tags {
		tag := strings.TrimSpace(rawTag)
		if tag == "" {
			continue
		}
		if !utf8.ValidString(tag) || utf8.RuneCountInString(tag) > playerTagMaxRunes {
			return playerAnnotation{}, annotationValidationError("player tags must not exceed 24 characters")
		}
		key := strings.ToLower(tag)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		tags = append(tags, tag)
	}
	return playerAnnotation{Note: note, Tags: tags}, nil
}

type annotationValidationError string

func (e annotationValidationError) Error() string { return string(e) }

func (s Server) loadPlayerAnnotations(c *gin.Context, sourceID string) (map[string]playerAnnotation, error) {
	raw, found, err := s.store.GetKV(c.Request.Context(), playerAnnotationsKey(sourceID))
	if err != nil {
		return nil, err
	}
	annotations := map[string]playerAnnotation{}
	if !found || strings.TrimSpace(raw) == "" {
		return annotations, nil
	}
	if err := json.Unmarshal([]byte(raw), &annotations); err != nil {
		return nil, err
	}
	for key, annotation := range annotations {
		normalized, normalizeErr := normalizePlayerAnnotation(playerAnnotationInput{Note: annotation.Note, Tags: annotation.Tags})
		if normalizeErr != nil {
			delete(annotations, key)
			continue
		}
		normalized.UpdatedAt = annotation.UpdatedAt
		annotations[key] = normalized
	}
	return annotations, nil
}

func (s Server) savePlayerAnnotations(c *gin.Context, sourceID string, annotations map[string]playerAnnotation) error {
	key := playerAnnotationsKey(sourceID)
	if len(annotations) == 0 {
		return s.store.DeleteKV(c.Request.Context(), key)
	}
	encoded, err := json.Marshal(annotations)
	if err != nil {
		return err
	}
	return s.store.SetKV(c.Request.Context(), key, string(encoded))
}

func playerAnnotationsKey(sourceID string) string {
	return playerAnnotationsKeyPrefix + strings.TrimSpace(sourceID)
}

func playerAnnotationStorageID(player saveindex.Player) string {
	return firstNonEmpty(player.PlayerUID, player.SteamID)
}

func annotationForPlayer(annotations map[string]playerAnnotation, player saveindex.Player) playerAnnotation {
	for _, id := range []string{player.PlayerUID, player.SteamID} {
		if annotation, found := annotations[id]; found {
			return annotation
		}
	}
	return playerAnnotation{}
}

func flattenPlayerWithAnnotation(player saveindex.Player, online onlinePlayersResult, annotation playerAnnotation) gin.H {
	view := flattenPlayer(player, online)
	view["note"] = annotation.Note
	view["tags"] = append([]string(nil), annotation.Tags...)
	view["has_annotation"] = annotation.Note != "" || len(annotation.Tags) > 0
	view["annotation_updated_at"] = annotation.UpdatedAt
	return view
}

func flattenPlayersWithAnnotations(players []saveindex.Player, online onlinePlayersResult, annotations map[string]playerAnnotation) []gin.H {
	out := make([]gin.H, 0, len(players))
	for _, player := range players {
		out = append(out, flattenPlayerWithAnnotation(player, online, annotationForPlayer(annotations, player)))
	}
	return out
}

func playerAnnotationSearchValues(annotation playerAnnotation) []string {
	values := make([]string, 0, len(annotation.Tags)+1)
	values = append(values, annotation.Note)
	values = append(values, annotation.Tags...)
	return values
}
