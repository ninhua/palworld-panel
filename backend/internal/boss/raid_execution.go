package boss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	maximumRaidSpawnGroups     = 20
	maximumRaidGroupSpawnCount = 100
	maximumRaidWaveSpawnCount  = 200
)

// RaidBaseLocation is the current persisted base location resolved from the
// active save index. Raid execution deliberately does not fall back to the
// coordinate snapshot stored on the legacy Boss template.
type RaidBaseLocation struct {
	ID        string  `json:"id"`
	Name      string  `json:"name,omitempty"`
	GuildID   string  `json:"guild_id,omitempty"`
	GuildName string  `json:"guild_name,omitempty"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Z         float64 `json:"z"`
}

type RaidBaseResolver interface {
	ResolveRaidBase(context.Context, string) (RaidBaseLocation, error)
}

type RaidBaseResolverFunc func(context.Context, string) (RaidBaseLocation, error)

func (function RaidBaseResolverFunc) ResolveRaidBase(ctx context.Context, baseID string) (RaidBaseLocation, error) {
	if function == nil {
		return RaidBaseLocation{}, errors.New("raid base resolver is not configured")
	}
	return function(ctx, baseID)
}

// RaidSpawnGroup represents one PalTemplate selection inside a raid wave.
// A wave can contain several groups, allowing different Pal species/templates
// to be spawned together.
type RaidSpawnGroup struct {
	Name            string   `json:"name,omitempty"`
	PalTemplateFile string   `json:"pal_template_file"`
	PalID           string   `json:"pal_id"`
	Level           int      `json:"level"`
	Count           int      `json:"count"`
	SpawnRadius     float64  `json:"spawn_radius"`
	Capturable      bool     `json:"capturable"`
	DisableStatuses []string `json:"disable_statuses,omitempty"`
}

type raidSpawnGroupDocument struct {
	Name            string   `json:"name"`
	PalTemplateFile string   `json:"pal_template_file"`
	TemplateFile    string   `json:"template_file"`
	PalID           string   `json:"pal_id"`
	Level           int      `json:"level"`
	Count           int      `json:"count"`
	SpawnRadius     float64  `json:"spawn_radius"`
	Capturable      *bool    `json:"capturable"`
	DisableStatuses []string `json:"disable_statuses"`
}

type raidExecutionAdapter struct {
	inner    WaveExecutor
	resolver RaidBaseResolver
}

func NewRaidExecutionAdapter(inner WaveExecutor, resolver RaidBaseResolver) WaveExecutor {
	return &raidExecutionAdapter{inner: inner, resolver: resolver}
}

func (adapter *raidExecutionAdapter) Status(ctx context.Context) ExecutionAdapterStatus {
	if adapter == nil || adapter.inner == nil {
		return ExecutionAdapterStatus{
			Adapter: "paldefender_rcon_raid",
			State:   "not_configured",
			Message: "Raid execution adapter is not configured.",
			Limitations: []string{
				"A PalDefender wave executor and save-index base resolver are required.",
			},
		}
	}
	status := adapter.inner.Status(ctx)
	status.Adapter = "paldefender_rcon_raid"
	status.Capabilities.MultipleSpawns = true
	status.Capabilities.CustomPalTemplate = true
	if adapter.resolver == nil {
		status.Available = false
		status.State = "base_resolver_missing"
		status.Message = "Raid base resolver is not configured."
	}
	status.Limitations = append(status.Limitations,
		"Raid waves that declare raid_base_id never fall back to stored coordinates; a current save index is required.",
		"Each raid spawn group must reference an existing PalDefender PalTemplate and include its PalID and level snapshot.",
	)
	return status
}

func (adapter *raidExecutionAdapter) ExecuteWave(ctx context.Context, summon Summon, wave SummonWave) (WaveExecutionResult, error) {
	if adapter == nil || adapter.inner == nil {
		return WaveExecutionResult{Adapter: "paldefender_rcon_raid", Details: map[string]any{}}, executionFailure(false, "raid execution adapter is not configured")
	}
	groups, hasGroups, err := raidSpawnGroups(wave)
	if err != nil {
		return WaveExecutionResult{Adapter: "paldefender_rcon_raid", Details: map[string]any{}}, executionFailure(false, "%v", err)
	}
	baseID := raidBaseID(summon, wave)
	if !hasGroups && baseID == "" {
		// Legacy records remain executable exactly as before.
		return adapter.inner.ExecuteWave(ctx, summon, wave)
	}
	if baseID == "" {
		return WaveExecutionResult{Adapter: "paldefender_rcon_raid", Details: map[string]any{}}, executionFailure(false, "raid wave is not bound to a base")
	}
	if adapter.resolver == nil {
		return WaveExecutionResult{Adapter: "paldefender_rcon_raid", Details: map[string]any{}}, executionFailure(false, "raid base resolver is not configured")
	}
	base, err := adapter.resolver.ResolveRaidBase(ctx, baseID)
	if err != nil {
		return WaveExecutionResult{Adapter: "paldefender_rcon_raid", Details: map[string]any{}}, executionFailure(false, "resolve raid base %s: %v", baseID, err)
	}
	if strings.TrimSpace(base.ID) == "" {
		return WaveExecutionResult{Adapter: "paldefender_rcon_raid", Details: map[string]any{}}, executionFailure(false, "raid base %s is unavailable", baseID)
	}
	if !finiteRaidCoordinate(base.X) || !finiteRaidCoordinate(base.Y) || !finiteRaidCoordinate(base.Z) {
		return WaveExecutionResult{Adapter: "paldefender_rcon_raid", Details: map[string]any{}}, executionFailure(false, "raid base %s has invalid coordinates", baseID)
	}

	resolvedSummon := summon
	resolvedSummon.Location = Location{X: base.X, Y: base.Y, Z: base.Z, Label: firstRaidValue(base.Name, base.GuildName, base.ID)}
	if !hasGroups {
		result, executeErr := adapter.inner.ExecuteWave(ctx, resolvedSummon, wave)
		result.Adapter = "paldefender_rcon_raid"
		if result.Details == nil {
			result.Details = map[string]any{}
		}
		result.Details["raid_base"] = base
		return result, executeErr
	}

	result := WaveExecutionResult{
		Adapter:  "paldefender_rcon_raid",
		Commands: []string{}, Responses: []string{},
		Details: map[string]any{
			"raid_base":         base,
			"spawn_group_count": len(groups),
			"spawn_groups":      []map[string]any{},
		},
	}
	groupResults := make([]map[string]any, 0, len(groups))
	maximumRadius := 0.0
	for _, group := range groups {
		if group.SpawnRadius > maximumRadius {
			maximumRadius = group.SpawnRadius
		}
	}
	if maximumRadius < 100 {
		maximumRadius = 100
	}

	for index, group := range groups {
		if err := ctx.Err(); err != nil {
			return result, executionFailure(result.CompletedCommands > 0, "raid execution context ended: %v", err)
		}
		groupSummon := resolvedSummon
		groupSummon.Location.X, groupSummon.Location.Y = spreadCoordinate(base.X, base.Y, maximumRadius, len(groups), index)
		groupWave := wave
		groupWave.Name = firstRaidValue(group.Name, wave.Name)
		groupWave.PalID = group.PalID
		groupWave.Level = group.Level
		groupWave.Count = group.Count
		groupWave.HPMultiplier = 1
		groupWave.AttackMultiplier = 1
		groupWave.DefenseMultiplier = 1
		groupWave.SpawnRadius = group.SpawnRadius
		groupWave.Capturable = group.Capturable
		groupWave.Metadata = raidGroupMetadata(wave.Metadata, group)

		groupResult, groupErr := adapter.inner.ExecuteWave(ctx, groupSummon, groupWave)
		result.Commands = append(result.Commands, groupResult.Commands...)
		result.Responses = append(result.Responses, groupResult.Responses...)
		result.CompletedCommands += groupResult.CompletedCommands
		groupDetails := map[string]any{
			"position":           index + 1,
			"name":               groupWave.Name,
			"pal_template_file":  group.PalTemplateFile,
			"pal_id":             group.PalID,
			"level":              group.Level,
			"requested_count":    group.Count,
			"completed_commands": groupResult.CompletedCommands,
			"center":             map[string]float64{"x": groupSummon.Location.X, "y": groupSummon.Location.Y, "z": groupSummon.Location.Z},
		}
		if groupResult.Details != nil {
			groupDetails["executor"] = groupResult.Details
		}
		groupResults = append(groupResults, groupDetails)
		result.Details["spawn_groups"] = groupResults
		if groupErr != nil {
			uncertain := result.CompletedCommands > 0
			var executionErr *ExecutionError
			if errors.As(groupErr, &executionErr) && executionErr.Uncertain {
				uncertain = true
			}
			return result, executionFailure(uncertain, "raid spawn group %d/%d (%s) failed: %v", index+1, len(groups), groupWave.Name, groupErr)
		}
	}
	result.Details["completed_count"] = result.CompletedCommands
	return result, nil
}

func raidBaseID(summon Summon, wave SummonWave) string {
	for _, metadata := range []map[string]any{wave.Metadata, summon.Metadata} {
		for _, key := range []string{"raid_base_id", "base_id"} {
			if value := metadataString(metadata, key); value != "" {
				return value
			}
		}
	}
	return ""
}

func raidSpawnGroups(wave SummonWave) ([]RaidSpawnGroup, bool, error) {
	if wave.Metadata == nil {
		return nil, false, nil
	}
	raw, found := wave.Metadata["spawn_groups"]
	if !found || raw == nil {
		return nil, false, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, true, fmt.Errorf("encode raid spawn_groups: %w", err)
	}
	var documents []raidSpawnGroupDocument
	if err := json.Unmarshal(payload, &documents); err != nil {
		return nil, true, fmt.Errorf("decode raid spawn_groups: %w", err)
	}
	if len(documents) == 0 || len(documents) > maximumRaidSpawnGroups {
		return nil, true, fmt.Errorf("raid wave must contain between 1 and %d spawn groups", maximumRaidSpawnGroups)
	}
	groups := make([]RaidSpawnGroup, 0, len(documents))
	total := 0
	for index, document := range documents {
		templateFile := firstRaidValue(document.PalTemplateFile, document.TemplateFile)
		normalizedTemplate, err := normalizeBossTemplateFile(templateFile)
		if err != nil {
			return nil, true, fmt.Errorf("spawn group %d has invalid PalTemplate filename: %w", index+1, err)
		}
		palID := strings.TrimSpace(document.PalID)
		if palID == "" || !identifierPattern.MatchString(palID) {
			return nil, true, fmt.Errorf("spawn group %d has invalid PalID", index+1)
		}
		if document.Level < 1 || document.Level > 100 {
			return nil, true, fmt.Errorf("spawn group %d level must be between 1 and 100", index+1)
		}
		if document.Count < 1 || document.Count > maximumRaidGroupSpawnCount {
			return nil, true, fmt.Errorf("spawn group %d count must be between 1 and %d", index+1, maximumRaidGroupSpawnCount)
		}
		if !finiteRaidCoordinate(document.SpawnRadius) || document.SpawnRadius < 0 || document.SpawnRadius > 100000 {
			return nil, true, fmt.Errorf("spawn group %d spawn radius is invalid", index+1)
		}
		total += document.Count
		if total > maximumRaidWaveSpawnCount {
			return nil, true, fmt.Errorf("raid wave total spawn count cannot exceed %d", maximumRaidWaveSpawnCount)
		}
		capturable := wave.Capturable
		if document.Capturable != nil {
			capturable = *document.Capturable
		}
		groups = append(groups, RaidSpawnGroup{
			Name: strings.TrimSpace(document.Name), PalTemplateFile: normalizedTemplate,
			PalID: palID, Level: document.Level, Count: document.Count,
			SpawnRadius: document.SpawnRadius, Capturable: capturable,
			DisableStatuses: normalizedRaidStrings(document.DisableStatuses),
		})
	}
	return groups, true, nil
}

func raidGroupMetadata(source map[string]any, group RaidSpawnGroup) map[string]any {
	result := make(map[string]any, len(source)+2)
	for key, value := range source {
		if key == "spawn_groups" {
			continue
		}
		result[key] = value
	}
	result["pal_template_file"] = group.PalTemplateFile
	if len(group.DisableStatuses) > 0 {
		result["disable_statuses"] = append([]string(nil), group.DisableStatuses...)
	} else {
		delete(result, "disable_statuses")
	}
	return result
}

func normalizedRaidStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func finiteRaidCoordinate(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func firstRaidValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
