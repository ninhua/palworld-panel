package playerpresence

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"palpanel/internal/db"
)

const (
	StorageKey = "player_presence:server"
	Version    = 1

	MaxSampleGap = 60 * time.Second
	StaleAfter   = 45 * time.Second
	MaxSessions  = 20
)

var observeMu sync.Mutex

type OnlinePlayer struct {
	PlayerUID string `json:"player_uid,omitempty"`
	SteamID   string `json:"steam_id,omitempty"`
	Nickname  string `json:"nickname,omitempty"`
}

type Session struct {
	StartedAt       string `json:"started_at"`
	EndedAt         string `json:"ended_at"`
	DurationSeconds int64  `json:"duration_seconds"`
}

type Record struct {
	PlayerUID        string    `json:"player_uid,omitempty"`
	SteamID          string    `json:"steam_id,omitempty"`
	Nickname         string    `json:"nickname,omitempty"`
	Online           bool      `json:"online"`
	SessionStartedAt string    `json:"session_started_at,omitempty"`
	LastSeenAt       string    `json:"last_seen_at,omitempty"`
	LastOnlineAt     string    `json:"last_online_at,omitempty"`
	LastOfflineAt    string    `json:"last_offline_at,omitempty"`
	SessionSeconds   int64     `json:"session_seconds"`
	TotalSeconds     int64     `json:"total_seconds"`
	Sessions         []Session `json:"sessions,omitempty"`
}

type State struct {
	Version    int               `json:"version"`
	ScopeID    string            `json:"scope_id,omitempty"`
	ObservedAt string            `json:"observed_at,omitempty"`
	Available  bool              `json:"available"`
	Players    map[string]Record `json:"players"`
}

func EmptyState() State {
	return State{Version: Version, Players: map[string]Record{}}
}

func Load(ctx context.Context, store *db.Store) (State, error) {
	return loadFromKey(ctx, store, StorageKey, "")
}

func LoadScoped(ctx context.Context, store *db.Store, scope Scope) (State, error) {
	state, found, err := loadScopedState(ctx, store, scope)
	if err != nil || found {
		return state, err
	}
	if scope.AllowLegacyMigration {
		legacy, legacyFound, legacyErr := loadFromKeyFound(ctx, store, StorageKey, scope.ID)
		if legacyErr != nil {
			return EmptyState(), legacyErr
		}
		if legacyFound {
			if err := saveScoped(ctx, store, scope, legacy); err != nil {
				return EmptyState(), err
			}
			return legacy, nil
		}
	}
	state = EmptyState()
	state.ScopeID = scope.ID
	return state, nil
}

func loadScopedState(ctx context.Context, store *db.Store, scope Scope) (State, bool, error) {
	return loadFromKeyFound(ctx, store, scope.StorageKey(), scope.ID)
}

func loadFromKey(ctx context.Context, store *db.Store, key, scopeID string) (State, error) {
	state, _, err := loadFromKeyFound(ctx, store, key, scopeID)
	return state, err
}

func loadFromKeyFound(ctx context.Context, store *db.Store, key, scopeID string) (State, bool, error) {
	state := EmptyState()
	state.ScopeID = scopeID
	if store == nil {
		return state, false, nil
	}
	raw, found, err := store.GetKV(ctx, key)
	if err != nil {
		return state, false, err
	}
	if !found || strings.TrimSpace(raw) == "" {
		return state, false, nil
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return EmptyState(), false, err
	}
	if state.Version == 0 {
		state.Version = Version
	}
	if state.Players == nil {
		state.Players = map[string]Record{}
	}
	state.ScopeID = scopeID
	return state, true, nil
}

func Observe(ctx context.Context, store *db.Store, now time.Time, online []OnlinePlayer) (State, error) {
	observeMu.Lock()
	defer observeMu.Unlock()

	state, err := Load(ctx, store)
	if err != nil {
		return EmptyState(), err
	}
	state = Advance(state, now, online)
	encoded, err := json.Marshal(state)
	if err != nil {
		return EmptyState(), err
	}
	if err := store.SetKV(ctx, StorageKey, string(encoded)); err != nil {
		return EmptyState(), err
	}
	return state, nil
}

func ObserveScoped(ctx context.Context, store *db.Store, scope Scope, now time.Time, online []OnlinePlayer) (State, error) {
	observeMu.Lock()
	defer observeMu.Unlock()

	state, err := LoadScoped(ctx, store, scope)
	if err != nil {
		return EmptyState(), err
	}
	state = Advance(state, now, online)
	state.ScopeID = scope.ID
	if err := saveScoped(ctx, store, scope, state); err != nil {
		return EmptyState(), err
	}
	return state, nil
}

func saveScoped(ctx context.Context, store *db.Store, scope Scope, state State) error {
	state.ScopeID = scope.ID
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return store.SetKV(ctx, scope.StorageKey(), string(encoded))
}

func Advance(previous State, now time.Time, online []OnlinePlayer) State {
	now = now.UTC()
	nowText := now.Format(time.RFC3339Nano)
	next := cloneState(previous)
	next.Version = Version
	next.Available = true

	elapsed := elapsedSeconds(previous.ObservedAt, now)
	if elapsed > 0 {
		for key, record := range next.Players {
			if !record.Online {
				continue
			}
			record.SessionSeconds += elapsed
			record.TotalSeconds += elapsed
			next.Players[key] = record
		}
	}

	seen := make(map[string]struct{}, len(online))
	for _, current := range online {
		key := findRecordKey(next.Players, current)
		if key == "" {
			key = canonicalKey(current)
		}
		if key == "" {
			continue
		}
		record := next.Players[key]
		wasOnline := record.Online
		if strings.TrimSpace(current.PlayerUID) != "" {
			record.PlayerUID = strings.TrimSpace(current.PlayerUID)
		}
		if strings.TrimSpace(current.SteamID) != "" {
			record.SteamID = strings.TrimSpace(current.SteamID)
		}
		if strings.TrimSpace(current.Nickname) != "" {
			record.Nickname = strings.TrimSpace(current.Nickname)
		}
		if !wasOnline {
			record.Online = true
			record.SessionStartedAt = nowText
			record.LastOnlineAt = nowText
			record.SessionSeconds = 0
		}
		record.LastSeenAt = nowText
		next.Players[key] = record
		seen[key] = struct{}{}
	}

	for key, record := range next.Players {
		if !record.Online {
			continue
		}
		if _, found := seen[key]; found {
			continue
		}
		record.Online = false
		record.LastOfflineAt = nowText
		if record.SessionStartedAt != "" {
			record.Sessions = append(record.Sessions, Session{
				StartedAt:       record.SessionStartedAt,
				EndedAt:         nowText,
				DurationSeconds: record.SessionSeconds,
			})
			if len(record.Sessions) > MaxSessions {
				record.Sessions = append([]Session(nil), record.Sessions[len(record.Sessions)-MaxSessions:]...)
			}
		}
		next.Players[key] = record
	}

	next.ObservedAt = nowText
	return next
}

func ParseRESTPlayers(bodyValue any) []OnlinePlayer {
	body := bodyValue
	if data, ok := bodyValue.(map[string]any); ok {
		body = data
		if nested, nestedOK := data["body"].(map[string]any); nestedOK {
			body = nested
		}
	}

	var list []any
	switch value := body.(type) {
	case map[string]any:
		list, _ = value["players"].([]any)
	case []any:
		list = value
	}

	players := make([]OnlinePlayer, 0, len(list))
	for _, raw := range list {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		player := OnlinePlayer{
			PlayerUID: stringValue(item["player_uid"], item["playerUid"], item["playerId"]),
			SteamID:   stringValue(item["steam_id"], item["userId"], item["userid"]),
			Nickname:  stringValue(item["nickname"], item["name"], item["playerName"]),
		}
		if canonicalKey(player) == "" {
			continue
		}
		players = append(players, player)
	}
	return players
}

func Find(state State, identifier string) (Record, bool) {
	needle := identityKey(identifier)
	if needle == "" {
		return Record{}, false
	}
	if record, found := state.Players[needle]; found {
		return record, true
	}
	for _, record := range state.Players {
		if needle == identityKey(record.PlayerUID) || needle == identityKey(record.SteamID) {
			return record, true
		}
	}
	return Record{}, false
}

func Records(state State) []Record {
	items := make([]Record, 0, len(state.Players))
	for _, record := range state.Players {
		items = append(items, record)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Online != items[j].Online {
			return items[i].Online
		}
		left := strings.ToLower(strings.TrimSpace(items[i].Nickname))
		right := strings.ToLower(strings.TrimSpace(items[j].Nickname))
		if left != right {
			return left < right
		}
		return canonicalRecordKey(items[i]) < canonicalRecordKey(items[j])
	})
	return items
}

func IsStale(state State, now time.Time) bool {
	observed, err := time.Parse(time.RFC3339Nano, state.ObservedAt)
	if err != nil {
		return true
	}
	return now.UTC().Sub(observed) > StaleAfter
}

func cloneState(previous State) State {
	next := previous
	next.Players = make(map[string]Record, len(previous.Players))
	for key, record := range previous.Players {
		record.Sessions = append([]Session(nil), record.Sessions...)
		next.Players[key] = record
	}
	return next
}

func elapsedSeconds(observedAt string, now time.Time) int64 {
	previous, err := time.Parse(time.RFC3339Nano, observedAt)
	if err != nil {
		return 0
	}
	elapsed := now.Sub(previous)
	if elapsed <= 0 {
		return 0
	}
	if elapsed > MaxSampleGap {
		elapsed = MaxSampleGap
	}
	return int64(elapsed / time.Second)
}

func findRecordKey(records map[string]Record, player OnlinePlayer) string {
	aliases := []string{identityKey(player.PlayerUID), identityKey(player.SteamID)}
	for _, alias := range aliases {
		if alias == "" {
			continue
		}
		if _, found := records[alias]; found {
			return alias
		}
	}
	for key, record := range records {
		for _, alias := range aliases {
			if alias != "" && (alias == identityKey(record.PlayerUID) || alias == identityKey(record.SteamID)) {
				return key
			}
		}
	}
	return ""
}

func canonicalKey(player OnlinePlayer) string {
	if key := identityKey(player.PlayerUID); key != "" {
		return key
	}
	return identityKey(player.SteamID)
}

func canonicalRecordKey(record Record) string {
	return canonicalKey(OnlinePlayer{PlayerUID: record.PlayerUID, SteamID: record.SteamID})
}

func identityKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "")
	switch value {
	case "", "none", "null", "undefined":
		return ""
	}
	if strings.Trim(value, "0") == "" {
		return ""
	}
	return value
}

func stringValue(values ...any) string {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case json.Number:
			return typed.String()
		case float64:
			if typed != 0 {
				encoded, _ := json.Marshal(typed)
				return string(encoded)
			}
		}
	}
	return ""
}
