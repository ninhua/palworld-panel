package api

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"palpanel/internal/gameevents"
	"palpanel/internal/pallocalize"
	"palpanel/internal/saveindex"
)

const saveHistoryEvidenceLimit = 5000

// semanticSaveHistoryDiff keeps the raw field diff available for diagnostics,
// but builds the default event ledger from player/guild/base-owned entities.
// World spawns, wild Pals, map-object containers and unresolved containers are
// deliberately suppressed from the human-facing summary and event total.
func (s Server) semanticSaveHistoryDiff(
	ctx context.Context,
	fromID string,
	toID string,
	category string,
	query string,
	limit int,
	offset int,
) (saveindex.HistoryDiff, error) {
	raw, err := s.saveIndex.HistoryDiff(fromID, toID, saveindex.HistoryDiffOptions{
		Category: category,
		Query:    query,
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		return saveindex.HistoryDiff{}, err
	}

	allEvents, err := s.saveIndex.AllHistoryEvents(fromID, toID)
	if err != nil {
		return saveindex.HistoryDiff{}, err
	}
	trusted := filterTrustedSaveHistoryEvents(allEvents)
	trusted = s.attachSaveHistoryEvidence(ctx, raw.From, raw.To, trusted)
	raw.Summary = summarizeTrustedSaveHistoryEvents(trusted)

	filtered := make([]saveindex.HistoryEvent, 0, len(trusted))
	category = strings.ToLower(strings.TrimSpace(category))
	query = strings.ToLower(strings.TrimSpace(query))
	for _, event := range trusted {
		if category != "" && category != "all" && event.Category != category {
			continue
		}
		if query != "" && !saveHistorySemanticEventMatches(event, query) {
			continue
		}
		filtered = append(filtered, event)
	}

	raw.EventTotal = len(filtered)
	eventOffset := offset
	if eventOffset > len(filtered) {
		eventOffset = len(filtered)
	}
	end := eventOffset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	raw.Events = append([]saveindex.HistoryEvent(nil), filtered[eventOffset:end]...)
	raw.Limit = limit
	raw.Offset = offset
	return raw, nil
}

func filterTrustedSaveHistoryEvents(events []saveindex.HistoryEvent) []saveindex.HistoryEvent {
	filtered := make([]saveindex.HistoryEvent, 0, len(events))
	for _, event := range events {
		if saveHistoryEventIsTrusted(event) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func saveHistoryEventIsTrusted(event saveindex.HistoryEvent) bool {
	switch strings.ToLower(strings.TrimSpace(event.Category)) {
	case "players", "guilds", "bases":
		return true
	case "items":
		return saveHistoryOwnerIsTrusted(event.ActorType, event.ActorID)
	case "pals":
		if event.Kind == "pal_transferred" {
			return saveHistoryOwnerIsTrusted(event.ActorType, event.ActorID) ||
				saveHistoryOwnerIsTrusted(event.TargetType, event.TargetID)
		}
		return saveHistoryOwnerIsTrusted(event.ActorType, event.ActorID)
	default:
		return false
	}
}

func saveHistoryOwnerIsTrusted(ownerType, ownerID string) bool {
	switch strings.ToLower(strings.TrimSpace(ownerType)) {
	case "player", "base", "guild":
		return strings.TrimSpace(ownerID) != ""
	default:
		return false
	}
}

func summarizeTrustedSaveHistoryEvents(events []saveindex.HistoryEvent) saveindex.HistoryDiffSummary {
	var summary saveindex.HistoryDiffSummary
	playersAdded := map[string]struct{}{}
	playersRemoved := map[string]struct{}{}
	playersChanged := map[string]struct{}{}
	guildsAdded := map[string]struct{}{}
	guildsRemoved := map[string]struct{}{}
	guildsChanged := map[string]struct{}{}
	basesAdded := map[string]struct{}{}
	basesRemoved := map[string]struct{}{}
	basesChanged := map[string]struct{}{}
	palsAdded := map[string]struct{}{}
	palsRemoved := map[string]struct{}{}
	palsChanged := map[string]struct{}{}

	for _, event := range events {
		entityID := saveHistoryEntityID(event)
		switch event.Category {
		case "players":
			switch event.Kind {
			case "player_joined":
				playersAdded[entityID] = struct{}{}
			case "player_missing":
				playersRemoved[entityID] = struct{}{}
			default:
				playersChanged[entityID] = struct{}{}
			}
		case "guilds":
			switch event.Kind {
			case "guild_created":
				guildsAdded[entityID] = struct{}{}
			case "guild_removed":
				guildsRemoved[entityID] = struct{}{}
			default:
				guildsChanged[entityID] = struct{}{}
			}
		case "bases":
			switch event.Kind {
			case "base_created":
				basesAdded[entityID] = struct{}{}
			case "base_removed":
				basesRemoved[entityID] = struct{}{}
			default:
				basesChanged[entityID] = struct{}{}
			}
		case "pals":
			switch event.Kind {
			case "pal_acquired":
				palsAdded[entityID] = struct{}{}
			case "pal_lost":
				palsRemoved[entityID] = struct{}{}
			case "pal_transferred":
				// A world-to-player transfer represents a newly owned Pal rather
				// than a world-spawn mutation. Other transfers are changes.
				if !saveHistoryOwnerIsTrusted(event.ActorType, event.ActorID) &&
					saveHistoryOwnerIsTrusted(event.TargetType, event.TargetID) {
					palsAdded[entityID] = struct{}{}
				} else if saveHistoryOwnerIsTrusted(event.ActorType, event.ActorID) &&
					!saveHistoryOwnerIsTrusted(event.TargetType, event.TargetID) {
					palsRemoved[entityID] = struct{}{}
				} else {
					palsChanged[entityID] = struct{}{}
				}
			default:
				palsChanged[entityID] = struct{}{}
			}
		case "items":
			if event.Delta < 0 || event.Kind == "item_lost" {
				summary.ItemsDecreased++
			} else {
				summary.ItemsIncreased++
			}
		}
	}

	removeSummaryOverlap(playersChanged, playersAdded, playersRemoved)
	removeSummaryOverlap(guildsChanged, guildsAdded, guildsRemoved)
	removeSummaryOverlap(basesChanged, basesAdded, basesRemoved)
	removeSummaryOverlap(palsChanged, palsAdded, palsRemoved)

	summary.PlayersAdded = len(playersAdded)
	summary.PlayersRemoved = len(playersRemoved)
	summary.PlayersChanged = len(playersChanged)
	summary.GuildsAdded = len(guildsAdded)
	summary.GuildsRemoved = len(guildsRemoved)
	summary.GuildsChanged = len(guildsChanged)
	summary.BasesAdded = len(basesAdded)
	summary.BasesRemoved = len(basesRemoved)
	summary.BasesChanged = len(basesChanged)
	summary.PalsAdded = len(palsAdded)
	summary.PalsRemoved = len(palsRemoved)
	summary.PalsChanged = len(palsChanged)
	// Container mutations are an implementation detail. Trusted item events
	// retain player/base/guild inventory changes; raw container deltas remain in
	// the collapsed diagnostic section and exports.
	summary.ContainersAdded = 0
	summary.ContainersRemoved = 0
	summary.ContainersChanged = 0
	return summary
}

func removeSummaryOverlap(changed map[string]struct{}, groups ...map[string]struct{}) {
	for _, group := range groups {
		for identifier := range group {
			delete(changed, identifier)
		}
	}
}

func saveHistoryEntityID(event saveindex.HistoryEvent) string {
	id := event.ID
	markers := []string{}
	switch event.Category {
	case "players":
		markers = []string{":level", ":guild", ":nickname"}
	case "guilds":
		markers = []string{":member:", ":base:", ":owner"}
	case "bases":
		markers = []string{":structures", ":worker:", ":containers", ":status", ":location"}
	case "pals":
		markers = []string{":owner", ":progress", ":assignment", ":passives"}
	}
	for _, marker := range markers {
		if index := strings.Index(id, marker); index >= 0 {
			return id[:index]
		}
	}
	return id
}

func (s Server) attachSaveHistoryEvidence(
	ctx context.Context,
	from saveindex.HistorySnapshot,
	to saveindex.HistorySnapshot,
	events []saveindex.HistoryEvent,
) []saveindex.HistoryEvent {
	fromTime, toTime, ok := saveHistorySnapshotRange(from, to)
	if !ok {
		return events
	}
	service, err := s.gameEventService()
	if err != nil {
		return events
	}
	records, err := service.ListRange(ctx, fromTime.Add(-2*time.Minute), toTime.Add(2*time.Minute), []string{
		"PAL_CAPTURED",
		"ITEM_CRAFTED",
		"PLAYER_LOGIN",
	}, saveHistoryEvidenceLimit)
	if err != nil || len(records) == 0 {
		return events
	}

	used := make(map[string]struct{}, len(records))
	result := append([]saveindex.HistoryEvent(nil), events...)
	for index := range result {
		event := &result[index]
		best := -1
		bestScore := 0
		for recordIndex, record := range records {
			if _, exists := used[record.EventID]; exists {
				continue
			}
			score := saveHistoryEvidenceScore(*event, record)
			if score > bestScore {
				best = recordIndex
				bestScore = score
			}
		}
		if best < 0 {
			continue
		}
		record := records[best]
		used[record.EventID] = struct{}{}
		attachSaveHistoryEvidenceRecord(event, record)
	}
	return result
}

func saveHistorySnapshotRange(from, to saveindex.HistorySnapshot) (time.Time, time.Time, bool) {
	left, leftOK := saveHistorySnapshotTimestamp(from)
	right, rightOK := saveHistorySnapshotTimestamp(to)
	if !leftOK || !rightOK {
		return time.Time{}, time.Time{}, false
	}
	if right.Before(left) {
		left, right = right, left
	}
	return left, right, true
}

func saveHistorySnapshotTimestamp(snapshot saveindex.HistorySnapshot) (time.Time, bool) {
	for _, value := range []string{snapshot.CapturedAt, snapshot.GeneratedAt} {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func saveHistoryEvidenceScore(event saveindex.HistoryEvent, record gameevents.Record) int {
	if record.Status != "completed" && record.Status != "processing" {
		return 0
	}
	switch record.Type {
	case "PLAYER_LOGIN":
		if event.Kind != "player_joined" || !saveHistoryRecordMatchesOwner(record, event.ActorID, event.ActorLabel) {
			return 0
		}
		return 100
	case "PAL_CAPTURED":
		if event.Category != "pals" || (event.Kind != "pal_acquired" && event.Kind != "pal_transferred") {
			return 0
		}
		ownerID, ownerLabel := event.ActorID, event.ActorLabel
		if event.Kind == "pal_transferred" {
			if saveHistoryOwnerIsTrusted(event.ActorType, event.ActorID) ||
				!saveHistoryOwnerIsTrusted(event.TargetType, event.TargetID) {
				return 0
			}
			ownerID, ownerLabel = event.TargetID, event.TargetLabel
		}
		if !saveHistoryRecordMatchesOwner(record, ownerID, ownerLabel) {
			return 0
		}
		score := 80
		palID := saveHistoryPayloadString(record.Payload, "pal_id")
		palName := saveHistoryPayloadString(record.Payload, "pal_name")
		characterID := event.Metadata["character_id"]
		if saveHistoryLooseMatch(palID, characterID) {
			score += 20
		} else if saveHistoryLooseMatch(palName, event.SubjectLabel) ||
			saveHistoryLooseMatch(palName, pallocalize.PalName(characterID)) {
			score += 10
		} else if palID != "" || palName != "" {
			return 0
		}
		return score
	case "ITEM_CRAFTED":
		if event.Kind != "item_gained" || !saveHistoryRecordMatchesOwner(record, event.ActorID, event.ActorLabel) {
			return 0
		}
		itemName := saveHistoryPayloadString(record.Payload, "item_name")
		itemID := firstNonEmpty(event.Metadata["item_id"], event.SubjectID, event.SubjectLabel)
		if itemName == "" || !(saveHistoryLooseMatch(itemName, itemID) ||
			saveHistoryLooseMatch(itemName, pallocalize.ItemName(itemID)) ||
			saveHistoryLooseMatch(itemName, event.SubjectLabel)) {
			return 0
		}
		return 70
	default:
		return 0
	}
}

func attachSaveHistoryEvidenceRecord(event *saveindex.HistoryEvent, record gameevents.Record) {
	if event.Metadata == nil {
		event.Metadata = map[string]string{}
	}
	confidence := "correlated"
	label := "PalDefender 日志关联"
	switch record.Type {
	case "PAL_CAPTURED":
		confidence = "confirmed"
		label = "PalDefender 捕获日志确认"
		event.Inferred = false
	case "PLAYER_LOGIN":
		confidence = "confirmed"
		label = "PalDefender 登录日志确认"
		event.Inferred = false
	case "ITEM_CRAFTED":
		label = "PalDefender 开始制作日志关联"
	}
	event.Metadata["confidence"] = confidence
	event.Metadata["evidence_source"] = "paldefender_log"
	event.Metadata["evidence_event_id"] = record.EventID
	event.Metadata["evidence_event_type"] = record.Type
	event.Metadata["evidence_occurred_at"] = firstNonEmpty(record.OccurredAt, record.CreatedAt)
	event.Details = append(event.Details, saveindex.HistoryFieldChange{
		Field:  "证据",
		Before: "",
		After:  label + saveHistoryEvidenceTimeSuffix(record),
	})
}

func saveHistoryEvidenceTimeSuffix(record gameevents.Record) string {
	value := firstNonEmpty(record.OccurredAt, record.CreatedAt)
	if value == "" {
		return ""
	}
	return " · " + value
}

func saveHistoryRecordMatchesOwner(record gameevents.Record, ownerID, ownerLabel string) bool {
	for _, left := range []string{record.PlayerUID, record.SteamID, record.Nickname} {
		for _, right := range []string{ownerID, ownerLabel} {
			if saveHistoryIdentityEqual(left, right) {
				return true
			}
		}
	}
	return false
}

func saveHistoryIdentityEqual(left, right string) bool {
	left = saveHistoryCanonicalIdentity(left)
	right = saveHistoryCanonicalIdentity(right)
	return left != "" && right != "" && left == right
}

func saveHistoryCanonicalIdentity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "steam_")
	value = strings.ReplaceAll(value, "-", "")
	return value
}

func saveHistoryLooseMatch(left, right string) bool {
	left = saveHistorySearchToken(left)
	right = saveHistorySearchToken(right)
	if left == "" || right == "" {
		return false
	}
	return left == right || strings.Contains(left, right) || strings.Contains(right, left)
}

func saveHistorySearchToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character > 127 {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func saveHistoryPayloadString(payload map[string]any, key string) string {
	value, found := payload[key]
	if !found {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func saveHistorySemanticEventMatches(event saveindex.HistoryEvent, query string) bool {
	parts := []string{
		event.ID,
		event.Category,
		event.Kind,
		event.ActorType,
		event.ActorID,
		event.ActorLabel,
		event.SubjectType,
		event.SubjectID,
		event.SubjectLabel,
		event.TargetType,
		event.TargetID,
		event.TargetLabel,
		event.Before,
		event.After,
	}
	for _, detail := range event.Details {
		parts = append(parts, detail.Field, detail.Before, detail.After)
	}
	keys := make([]string, 0, len(event.Metadata))
	for key := range event.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key, event.Metadata[key])
	}
	return strings.Contains(strings.ToLower(strings.Join(parts, " ")), query)
}

func init() {
	patchFeatures = append(patchFeatures,
		"save-history-owned-entity-filter",
		"save-history-world-noise-suppression",
		"save-history-paldefender-evidence",
		"save-history-semantic-summary",
	)
}
