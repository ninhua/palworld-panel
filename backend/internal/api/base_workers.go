package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/pallocalize"
	"palpanel/internal/saveindex"
)

func (s Server) getSaveBaseWorkers(c *gin.Context) {
	index, status, err := s.currentSaveIndex(c)
	if err != nil && !status.Stale && status.State != "disabled" {
		fail(c, http.StatusServiceUnavailable, "save_index_unavailable", err.Error())
		return
	}
	customNames, sourceID, namesErr := s.activeBaseCustomNames(c)
	if namesErr != nil {
		fail(c, http.StatusInternalServerError, "base_custom_names_read_failed", namesErr.Error())
		return
	}

	id := c.Param("id")
	for _, base := range index.Bases {
		customName := customNames[base.ID]
		if !matchesID(id, base.ID, base.Name, pallocalize.BaseName(base.Name), customName) {
			continue
		}
		workers := baseWorkerViews(base, index.Pals)
		ok(c, gin.H{
			"base":      flattenBaseWithCustomName(base, customName),
			"workers":   workers,
			"summary":   baseWorkerSummary(workers),
			"status":    status,
			"source_id": sourceID,
		})
		return
	}
	fail(c, http.StatusNotFound, "base_not_found", "base not found")
}

func baseWorkerViews(base saveindex.Base, pals []saveindex.Pal) []gin.H {
	palByInstance := make(map[string]saveindex.Pal, len(pals))
	for _, pal := range pals {
		palByInstance[normalizeQuery(pal.InstanceID)] = pal
	}

	out := make([]gin.H, 0, len(base.Workers))
	for _, worker := range base.Workers {
		pal := palByInstance[normalizeQuery(worker.InstanceID)]
		characterID := firstNonEmpty(worker.CharacterID, pal.CharacterID)
		speciesName := pallocalize.PalName(characterID)
		level := worker.Level
		if level == 0 {
			level = pal.Level
		}
		nickname := firstNonEmpty(worker.Nickname, pal.Nickname)
		out = append(out, gin.H{
			"instance_id":   worker.InstanceID,
			"character_id":  characterID,
			"species_name":  speciesName,
			"name":          firstNonEmpty(nickname, speciesName, characterID, worker.InstanceID),
			"nickname":      nickname,
			"level":         level,
			"gender":        pal.Gender,
			"rank":          pal.Rank,
			"status":        firstNonEmpty(pal.Status, "Unknown"),
			"location_type": pal.LocationType,
			"passives":      localizeStrings(pal.Passives, pallocalize.PassiveName),
			"raw_passives":  append([]string(nil), pal.Passives...),
			"on_expedition": pal.OnExpedition,
		})
	}
	return out
}

func baseWorkerSummary(workers []gin.H) gin.H {
	maxLevel := 0
	totalLevel := 0
	namedCount := 0
	species := make(map[string]struct{})
	for _, worker := range workers {
		level, _ := worker["level"].(int)
		totalLevel += level
		if level > maxLevel {
			maxLevel = level
		}
		if strings.TrimSpace(asString(worker["nickname"])) != "" {
			namedCount++
		}
		if characterID := strings.TrimSpace(asString(worker["character_id"])); characterID != "" {
			species[normalizeQuery(characterID)] = struct{}{}
		}
	}
	averageLevel := 0.0
	if len(workers) > 0 {
		averageLevel = float64(totalLevel) / float64(len(workers))
	}
	return gin.H{
		"total":         len(workers),
		"average_level": averageLevel,
		"max_level":     maxLevel,
		"named_count":   namedCount,
		"species_count": len(species),
	}
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}
