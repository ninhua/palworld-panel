package unattendedinventory

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"palpanel/internal/playerpresence"
)

const (
	Version           = 1
	StorageKeyPrefix  = "unattended_inventory:v1:"
	MinimumDuration   = 5 * time.Minute
	MaxObservationGap = 60 * time.Second
	MaxPublicItems    = 100
)

var stateMu sync.Mutex

var ErrSnapshotUnavailable = errors.New("inventory snapshot is unavailable")

type KVStore interface {
	GetKV(context.Context, string) (string, bool, error)
	SetKV(context.Context, string, string) error
}

type Snapshot struct {
	Fingerprint string           `json:"fingerprint,omitempty"`
	GeneratedAt string           `json:"generated_at,omitempty"`
	Stale       bool             `json:"stale,omitempty"`
	Totals      map[string]int64 `json:"totals"`
}

type ItemDelta struct {
	ItemID   string `json:"item_id"`
	Quantity int64  `json:"quantity"`
}

type Session struct {
	WorldID             string      `json:"world_id"`
	StartedAt           string      `json:"started_at"`
	BaselineAt          string      `json:"baseline_at"`
	EligibleAt          string      `json:"eligible_at"`
	EndedAt             string      `json:"ended_at,omitempty"`
	LastObservedAt      string      `json:"last_observed_at"`
	BaselineFingerprint string      `json:"baseline_fingerprint,omitempty"`
	CurrentFingerprint  string      `json:"current_fingerprint,omitempty"`
	SnapshotStale       bool        `json:"snapshot_stale,omitempty"`
	Additions           []ItemDelta `json:"additions"`
	TotalAdded          int64       `json:"total_added"`
}

type State struct {
	Version        int         `json:"version"`
	ScopeID        string      `json:"scope_id"`
	WorldID        string      `json:"world_id"`
	Status         string      `json:"status"`
	StartedAt      string      `json:"started_at,omitempty"`
	BaselineAt     string      `json:"baseline_at,omitempty"`
	EligibleAt     string      `json:"eligible_at,omitempty"`
	LastObservedAt string      `json:"last_observed_at,omitempty"`
	Baseline       Snapshot    `json:"baseline,omitempty"`
	Current        Snapshot    `json:"current,omitempty"`
	Additions      []ItemDelta `json:"additions,omitempty"`
	TotalAdded     int64       `json:"total_added,omitempty"`
	LastCompleted  *Session    `json:"last_completed,omitempty"`
}

type PublicState struct {
	Available                bool        `json:"available"`
	Status                   string      `json:"status"`
	WorldID                  string      `json:"world_id,omitempty"`
	StartedAt                string      `json:"started_at,omitempty"`
	BaselineAt               string      `json:"baseline_at,omitempty"`
	EligibleAt               string      `json:"eligible_at,omitempty"`
	EndedAt                  string      `json:"ended_at,omitempty"`
	LastObservedAt           string      `json:"last_observed_at,omitempty"`
	DurationSeconds          int64       `json:"duration_seconds"`
	EligibleRemainingSeconds int64       `json:"eligible_remaining_seconds"`
	Qualified                bool        `json:"qualified"`
	CurrentOnline            bool        `json:"current_online"`
	PresenceStale            bool        `json:"presence_stale"`
	SnapshotStale            bool        `json:"snapshot_stale"`
	Additions                []ItemDelta `json:"additions"`
	TotalAdded               int64       `json:"total_added"`
}

func StorageKey(scope playerpresence.Scope) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(scope.ID)))
	return fmt.Sprintf("%s%x", StorageKeyPrefix, digest[:16])
}

func Observe(ctx context.Context, store KVStore, scope playerpresence.Scope, now time.Time, onlineCount int, snapshot Snapshot, snapshotErr error) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	state, err := loadUnlocked(ctx, store, scope)
	if err != nil {
		return err
	}
	state = Advance(state, scope, now, onlineCount, snapshot, snapshotErr)
	return saveUnlocked(ctx, store, scope, state)
}

func Load(ctx context.Context, store KVStore, scope playerpresence.Scope) (State, error) {
	stateMu.Lock()
	defer stateMu.Unlock()
	return loadUnlocked(ctx, store, scope)
}

func loadUnlocked(ctx context.Context, store KVStore, scope playerpresence.Scope) (State, error) {
	raw, found, err := store.GetKV(ctx, StorageKey(scope))
	if err != nil {
		return State{}, err
	}
	if !found || strings.TrimSpace(raw) == "" {
		return emptyState(scope), nil
	}
	var state State
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return State{}, err
	}
	if state.ScopeID != scope.ID || state.WorldID != scope.WorldID {
		return emptyState(scope), nil
	}
	state = normalizeState(state, scope)
	return state, nil
}

func saveUnlocked(ctx context.Context, store KVStore, scope playerpresence.Scope, state State) error {
	state = normalizeState(state, scope)
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return store.SetKV(ctx, StorageKey(scope), string(raw))
}

func Advance(previous State, scope playerpresence.Scope, now time.Time, onlineCount int, snapshot Snapshot, snapshotErr error) State {
	now = now.UTC()
	state := normalizeState(previous, scope)
	if state.ScopeID != scope.ID || state.WorldID != scope.WorldID {
		state = emptyState(scope)
	}
	if (state.Status == "active" || state.Status == "waiting") && observationGap(state.LastObservedAt, now) > MaxObservationGap {
		last := state.LastCompleted
		state = emptyState(scope)
		state.LastCompleted = last
	}

	snapshotOK := snapshotErr == nil && snapshot.Totals != nil
	if snapshotOK {
		snapshot = normalizeSnapshot(snapshot)
	}

	if onlineCount > 0 {
		last := state.LastCompleted
		if state.Status == "active" || state.Status == "waiting" {
			if snapshotOK && state.Status == "active" {
				state = updateCurrent(state, snapshot)
			}
			if state.Status == "active" && !now.Before(parseTime(state.EligibleAt)) {
				completed := sessionFromState(state, now)
				last = &completed
			}
		}
		state = emptyState(scope)
		state.Status = "online"
		state.LastObservedAt = formatTime(now)
		state.LastCompleted = last
		return state
	}

	switch state.Status {
	case "active":
		state.LastObservedAt = formatTime(now)
		if snapshotOK {
			state = updateCurrent(state, snapshot)
		}
		return state
	case "waiting":
		state.LastObservedAt = formatTime(now)
		if snapshotOK {
			state.Status = "active"
			state.Baseline = snapshot
			state.Current = snapshot
			state.BaselineAt = formatTime(now)
			state.EligibleAt = formatTime(now.Add(MinimumDuration))
			state.Additions = []ItemDelta{}
			state.TotalAdded = 0
		}
		return state
	default:
		last := state.LastCompleted
		state = emptyState(scope)
		state.LastCompleted = last
		state.StartedAt = formatTime(now)
		state.LastObservedAt = formatTime(now)
		if snapshotOK {
			state.Status = "active"
			state.Baseline = snapshot
			state.Current = snapshot
			state.BaselineAt = formatTime(now)
			state.EligibleAt = formatTime(now.Add(MinimumDuration))
			state.Additions = []ItemDelta{}
		} else {
			state.Status = "waiting"
		}
		return state
	}
}

func Public(state State, now time.Time) PublicState {
	now = now.UTC()
	state = normalizeState(state, playerpresence.Scope{ID: state.ScopeID, WorldID: state.WorldID})
	if state.ScopeID == "" || state.WorldID == "" {
		return PublicState{Available: false, Status: "unavailable", Additions: []ItemDelta{}}
	}
	if state.Status == "active" || state.Status == "waiting" {
		observed := parseTime(state.LastObservedAt)
		end := observed
		if end.IsZero() || now.Before(end) {
			end = now
		}
		started := parseTime(state.StartedAt)
		eligible := parseTime(state.EligibleAt)
		remaining := int64(0)
		if !eligible.IsZero() && now.Before(eligible) {
			remaining = int64(eligible.Sub(now).Seconds())
		}
		return PublicState{
			Available: true, Status: state.Status, WorldID: state.WorldID,
			StartedAt: state.StartedAt, BaselineAt: state.BaselineAt, EligibleAt: state.EligibleAt,
			LastObservedAt:           state.LastObservedAt,
			DurationSeconds:          durationSeconds(started, end),
			EligibleRemainingSeconds: remaining,
			Qualified:                state.Status == "active" && !eligible.IsZero() && !now.Before(eligible),
			PresenceStale:            !observed.IsZero() && now.Sub(observed) > MaxObservationGap,
			SnapshotStale:            state.Current.Stale,
			Additions:                limitDeltas(state.Additions), TotalAdded: state.TotalAdded,
		}
	}
	if state.LastCompleted != nil {
		session := *state.LastCompleted
		return PublicState{
			Available: true, Status: "completed", WorldID: session.WorldID,
			StartedAt: session.StartedAt, BaselineAt: session.BaselineAt, EligibleAt: session.EligibleAt,
			EndedAt: session.EndedAt, LastObservedAt: session.LastObservedAt,
			DurationSeconds: durationSeconds(parseTime(session.StartedAt), parseTime(session.EndedAt)),
			Qualified:       true, CurrentOnline: state.Status == "online",
			SnapshotStale: session.SnapshotStale,
			Additions:     limitDeltas(session.Additions), TotalAdded: session.TotalAdded,
		}
	}
	return PublicState{Available: true, Status: state.Status, WorldID: state.WorldID, CurrentOnline: state.Status == "online", Additions: []ItemDelta{}}
}

func emptyState(scope playerpresence.Scope) State {
	return State{Version: Version, ScopeID: scope.ID, WorldID: scope.WorldID, Status: "idle", Additions: []ItemDelta{}}
}

func normalizeState(state State, scope playerpresence.Scope) State {
	if state.Version == 0 {
		state.Version = Version
	}
	if state.ScopeID == "" {
		state.ScopeID = scope.ID
	}
	if state.WorldID == "" {
		state.WorldID = scope.WorldID
	}
	if state.Status == "" {
		state.Status = "idle"
	}
	if state.Additions == nil {
		state.Additions = []ItemDelta{}
	}
	state.Baseline = normalizeSnapshot(state.Baseline)
	state.Current = normalizeSnapshot(state.Current)
	if state.LastCompleted != nil && state.LastCompleted.Additions == nil {
		state.LastCompleted.Additions = []ItemDelta{}
	}
	return state
}

func normalizeSnapshot(snapshot Snapshot) Snapshot {
	if snapshot.Totals == nil {
		snapshot.Totals = map[string]int64{}
	}
	clean := make(map[string]int64, len(snapshot.Totals))
	for itemID, count := range snapshot.Totals {
		itemID = strings.TrimSpace(itemID)
		if itemID != "" && count > 0 {
			clean[itemID] += count
		}
	}
	snapshot.Totals = clean
	return snapshot
}

func updateCurrent(state State, snapshot Snapshot) State {
	state.Current = snapshot
	state.Additions, state.TotalAdded = additions(state.Baseline.Totals, snapshot.Totals)
	return state
}

func additions(baseline, current map[string]int64) ([]ItemDelta, int64) {
	out := make([]ItemDelta, 0)
	var total int64
	for itemID, count := range current {
		quantity := count - baseline[itemID]
		if quantity <= 0 {
			continue
		}
		out = append(out, ItemDelta{ItemID: itemID, Quantity: quantity})
		total += quantity
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Quantity != out[j].Quantity {
			return out[i].Quantity > out[j].Quantity
		}
		return strings.ToLower(out[i].ItemID) < strings.ToLower(out[j].ItemID)
	})
	return out, total
}

func sessionFromState(state State, ended time.Time) Session {
	return Session{
		WorldID: state.WorldID, StartedAt: state.StartedAt, BaselineAt: state.BaselineAt,
		EligibleAt: state.EligibleAt, EndedAt: formatTime(ended), LastObservedAt: state.LastObservedAt,
		BaselineFingerprint: state.Baseline.Fingerprint, CurrentFingerprint: state.Current.Fingerprint,
		SnapshotStale: state.Current.Stale, Additions: append([]ItemDelta(nil), state.Additions...), TotalAdded: state.TotalAdded,
	}
}

func limitDeltas(items []ItemDelta) []ItemDelta {
	if len(items) > MaxPublicItems {
		items = items[:MaxPublicItems]
	}
	return append([]ItemDelta(nil), items...)
}

func observationGap(value string, now time.Time) time.Duration {
	observed := parseTime(value)
	if observed.IsZero() || now.Before(observed) {
		return 0
	}
	return now.Sub(observed)
}

func durationSeconds(start, end time.Time) int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return int64(end.Sub(start).Seconds())
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return parsed
}
