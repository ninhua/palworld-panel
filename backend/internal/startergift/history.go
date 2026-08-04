package startergift

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"palpanel/internal/db"
	"palpanel/internal/playerpresence"
)

const (
	starterGiftHistoryVersion      = 1
	starterGiftHistoryPrefix       = "starter_gift:history:v1:"
	starterGiftHistoryMaximum      = 5000
	starterGiftHistoryPerPlayerMax = 100
)

type GrantHistoryEntry struct {
	ID               string `json:"id"`
	ScopeID          string `json:"scope_id"`
	ArchiveAction    string `json:"archive_action"`
	ArchiveReason    string `json:"archive_reason"`
	ArchivedAt       string `json:"archived_at"`
	CycleFingerprint string `json:"cycle_fingerprint"`
	Grant
}

type GrantHistorySnapshot struct {
	Version int                 `json:"version"`
	ScopeID string              `json:"scope_id"`
	Entries []GrantHistoryEntry `json:"entries"`
}

type PreservedActionResult struct {
	Archived  bool   `json:"archived"`
	HistoryID string `json:"history_id,omitempty"`
	Action    string `json:"action"`
}

var starterGiftHistoryActionMu sync.Mutex

func ApplyActionPreservingHistory(ctx context.Context, store *db.Store, scope playerpresence.Scope, player playerpresence.OnlinePlayer, action string) (PreservedActionResult, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	result := PreservedActionResult{Action: action}
	if starterGiftActionCancelsPlannedReissue(action) {
		starterGiftHistoryActionMu.Lock()
		defer starterGiftHistoryActionMu.Unlock()
		entry, found, err := currentGrantHistoryEntry(ctx, store, scope, player, "next_login")
		if err != nil {
			return result, err
		}
		if err := ApplyAction(ctx, store, scope, player, action); err != nil {
			return result, err
		}
		if found {
			_ = removeGrantHistoryFingerprint(ctx, store, scope, entry.CycleFingerprint, "next_login")
		}
		return result, nil
	}
	if !starterGiftActionReplacesCycle(action) {
		return result, ApplyAction(ctx, store, scope, player, action)
	}

	starterGiftHistoryActionMu.Lock()
	defer starterGiftHistoryActionMu.Unlock()

	entry, found, err := currentGrantHistoryEntry(ctx, store, scope, player, action)
	if err != nil {
		return result, err
	}
	if found {
		archived, historyID, archiveErr := appendGrantHistory(ctx, store, scope, entry)
		if archiveErr != nil {
			return result, archiveErr
		}
		result.Archived = archived
		result.HistoryID = historyID
	}

	if err := ApplyAction(ctx, store, scope, player, action); err != nil {
		// The old cycle is still present. Remove an entry created solely for an
		// action that failed validation so history does not claim a replacement.
		if result.Archived && result.HistoryID != "" {
			_ = removeGrantHistory(ctx, store, scope, result.HistoryID)
		}
		return result, err
	}
	return result, nil
}

func ListGrantHistory(ctx context.Context, store *db.Store, scope playerpresence.Scope) ([]GrantHistoryEntry, error) {
	starterGiftHistoryActionMu.Lock()
	defer starterGiftHistoryActionMu.Unlock()
	history, err := loadGrantHistory(ctx, store, scope)
	if err != nil {
		return nil, err
	}
	items := append([]GrantHistoryEntry(nil), history.Entries...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].ArchivedAt != items[j].ArchivedAt {
			return items[i].ArchivedAt > items[j].ArchivedAt
		}
		return items[i].ID > items[j].ID
	})
	return items, nil
}

func currentGrantHistoryEntry(ctx context.Context, store *db.Store, scope playerpresence.Scope, player playerpresence.OnlinePlayer, action string) (GrantHistoryEntry, bool, error) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state, err := loadState(ctx, store, scope)
	if err != nil {
		return GrantHistoryEntry{}, false, err
	}
	aliases := playerAliases(player)
	key := findGrantKey(state, aliases)
	if key == "" {
		return GrantHistoryEntry{}, false, nil
	}
	record, found := state.Grants[key]
	if !found {
		return GrantHistoryEntry{}, false, nil
	}
	archivedAt := time.Now().UTC().Format(time.RFC3339Nano)
	fingerprint := grantCycleFingerprint(scope, key, record)
	digest := sha256.Sum256([]byte(fingerprint))
	entry := GrantHistoryEntry{
		ID: "gift_history_" + hex.EncodeToString(digest[:12]), ScopeID: scope.ID,
		ArchiveAction: action, ArchiveReason: starterGiftHistoryReason(action), ArchivedAt: archivedAt,
		CycleFingerprint: fingerprint, Grant: grantView(record),
	}
	return entry, true, nil
}

func appendGrantHistory(ctx context.Context, store *db.Store, scope playerpresence.Scope, entry GrantHistoryEntry) (bool, string, error) {
	history, err := loadGrantHistory(ctx, store, scope)
	if err != nil {
		return false, "", err
	}
	for _, existing := range history.Entries {
		if existing.CycleFingerprint == entry.CycleFingerprint {
			return false, existing.ID, nil
		}
	}
	history.Entries = append(history.Entries, entry)
	history.Entries = trimGrantHistory(history.Entries)
	if err := saveGrantHistory(ctx, store, scope, history); err != nil {
		return false, "", err
	}
	return true, entry.ID, nil
}

func removeGrantHistory(ctx context.Context, store *db.Store, scope playerpresence.Scope, id string) error {
	history, err := loadGrantHistory(ctx, store, scope)
	if err != nil {
		return err
	}
	filtered := history.Entries[:0]
	for _, entry := range history.Entries {
		if entry.ID != id {
			filtered = append(filtered, entry)
		}
	}
	history.Entries = filtered
	return saveGrantHistory(ctx, store, scope, history)
}

func removeGrantHistoryFingerprint(ctx context.Context, store *db.Store, scope playerpresence.Scope, fingerprint, action string) error {
	history, err := loadGrantHistory(ctx, store, scope)
	if err != nil {
		return err
	}
	filtered := history.Entries[:0]
	for _, entry := range history.Entries {
		if entry.CycleFingerprint == fingerprint && entry.ArchiveAction == action {
			continue
		}
		filtered = append(filtered, entry)
	}
	history.Entries = filtered
	return saveGrantHistory(ctx, store, scope, history)
}

func loadGrantHistory(ctx context.Context, store *db.Store, scope playerpresence.Scope) (GrantHistorySnapshot, error) {
	history := GrantHistorySnapshot{Version: starterGiftHistoryVersion, ScopeID: scope.ID, Entries: []GrantHistoryEntry{}}
	raw, found, err := store.GetKV(ctx, starterGiftHistoryStorageKey(scope))
	if err != nil {
		return history, err
	}
	if !found || strings.TrimSpace(raw) == "" {
		return history, nil
	}
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		return GrantHistorySnapshot{}, fmt.Errorf("decode starter gift history: %w", err)
	}
	if history.Entries == nil {
		history.Entries = []GrantHistoryEntry{}
	}
	history.Version = starterGiftHistoryVersion
	history.ScopeID = scope.ID
	return history, nil
}

func saveGrantHistory(ctx context.Context, store *db.Store, scope playerpresence.Scope, history GrantHistorySnapshot) error {
	history.Version = starterGiftHistoryVersion
	history.ScopeID = scope.ID
	if history.Entries == nil {
		history.Entries = []GrantHistoryEntry{}
	}
	raw, err := json.Marshal(history)
	if err != nil {
		return err
	}
	return store.SetKV(ctx, starterGiftHistoryStorageKey(scope), string(raw))
}

func starterGiftHistoryStorageKey(scope playerpresence.Scope) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(scope.ID)))
	return starterGiftHistoryPrefix + hex.EncodeToString(digest[:16])
}

func grantCycleFingerprint(scope playerpresence.Scope, key string, record grantRecord) string {
	payload := struct {
		ScopeID                     string      `json:"scope_id"`
		Key                         string      `json:"key"`
		PlayerID                    string      `json:"player_id"`
		PlayerUID                   string      `json:"player_uid"`
		SteamID                     string      `json:"steam_id"`
		Status                      string      `json:"status"`
		FirstSeenAt                 string      `json:"first_seen_at"`
		CompletedAt                 string      `json:"completed_at"`
		PlanItems                   []ItemGrant `json:"plan_items"`
		PlanTemplates               []string    `json:"plan_templates"`
		PlanTechnologyMode          string      `json:"plan_technology_mode"`
		PlanTechnologyPoints        int64       `json:"plan_technology_points"`
		PlanAncientTechnologyPoints int64       `json:"plan_ancient_technology_points"`
	}{
		ScopeID: scope.ID, Key: key, PlayerID: record.PlayerID, PlayerUID: record.PlayerUID, SteamID: record.SteamID,
		Status: record.Status, FirstSeenAt: record.FirstSeenAt, CompletedAt: record.CompletedAt,
		PlanItems: record.PlanItems, PlanTemplates: record.PlanTemplates, PlanTechnologyMode: record.PlanTechnologyMode,
		PlanTechnologyPoints: record.PlanTechnologyPoints, PlanAncientTechnologyPoints: record.PlanAncientTechnologyPoints,
	}
	raw, _ := json.Marshal(payload)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func trimGrantHistory(entries []GrantHistoryEntry) []GrantHistoryEntry {
	if len(entries) == 0 {
		return []GrantHistoryEntry{}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ArchivedAt > entries[j].ArchivedAt })
	perPlayer := map[string]int{}
	result := make([]GrantHistoryEntry, 0, min(len(entries), starterGiftHistoryMaximum))
	for _, entry := range entries {
		player := identity(firstNonEmpty(entry.PlayerUID, entry.SteamID, entry.PlayerID))
		if player == "" {
			player = entry.ID
		}
		if perPlayer[player] >= starterGiftHistoryPerPlayerMax {
			continue
		}
		perPlayer[player]++
		result = append(result, entry)
		if len(result) >= starterGiftHistoryMaximum {
			break
		}
	}
	return result
}

func starterGiftActionCancelsPlannedReissue(action string) bool {
	switch action {
	case "cancel_next_login", "cancel_mark_new", "cancel_rearm":
		return true
	default:
		return false
	}
}

func starterGiftActionReplacesCycle(action string) bool {
	switch action {
	case "reissue", "grant_now", "mark_new_now", "next_login", "mark_new", "rearm":
		return true
	default:
		return false
	}
}

func starterGiftHistoryReason(action string) string {
	switch action {
	case "next_login", "mark_new", "rearm":
		return "管理员安排下次登录重新发放，旧发放周期已归档。"
	case "reissue", "grant_now", "mark_new_now":
		return "管理员执行全量重新发放，旧发放周期已归档。"
	default:
		return "发放周期被替换前归档。"
	}
}
