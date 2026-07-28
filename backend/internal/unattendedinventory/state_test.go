package unattendedinventory

import (
	"context"
	"testing"
	"time"

	"palpanel/internal/playerpresence"
)

type memoryStore struct{ values map[string]string }

func (m *memoryStore) GetKV(_ context.Context, key string) (string, bool, error) {
	value, ok := m.values[key]
	return value, ok, nil
}
func (m *memoryStore) SetKV(_ context.Context, key, value string) error {
	m.values[key] = value
	return nil
}

func testScope(id string) playerpresence.Scope {
	return playerpresence.Scope{ID: "server-world:" + id, WorldID: id}
}
func snap(fp string, values map[string]int64) Snapshot {
	return Snapshot{Fingerprint: fp, Totals: values}
}

func observeUntil(state State, scope playerpresence.Scope, start time.Time, minutes int, snapshot Snapshot) State {
	for minute := 1; minute <= minutes; minute++ {
		state = Advance(state, scope, start.Add(time.Duration(minute)*time.Minute), 0, snapshot, nil)
	}
	return state
}

func TestShortUnattendedSessionIsDiscarded(t *testing.T) {
	scope := testScope("world-a")
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	state := Advance(State{}, scope, now, 0, snap("a", map[string]int64{"Wood": 10}), nil)
	state = observeUntil(state, scope, now, 2, snap("b", map[string]int64{"Wood": 15}))
	state = Advance(state, scope, now.Add(3*time.Minute), 1, snap("b", map[string]int64{"Wood": 15}), nil)
	if state.LastCompleted != nil || state.Status != "online" {
		t.Fatalf("state = %#v", state)
	}
}

func TestQualifiedSessionKeepsOnlyPositiveNetAdditions(t *testing.T) {
	scope := testScope("world-a")
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	state := Advance(State{}, scope, now, 0, snap("a", map[string]int64{"Wood": 10, "Stone": 20}), nil)
	state = observeUntil(state, scope, now, 6, snap("b", map[string]int64{"Wood": 17, "Stone": 5, "Ore": 3}))
	state = Advance(state, scope, now.Add(7*time.Minute), 1, snap("b", map[string]int64{"Wood": 17, "Stone": 5, "Ore": 3}), nil)
	if state.LastCompleted == nil || state.LastCompleted.TotalAdded != 10 {
		t.Fatalf("completed = %#v", state.LastCompleted)
	}
	got := state.LastCompleted.Additions
	if len(got) != 2 || got[0].ItemID != "Wood" || got[0].Quantity != 7 || got[1].ItemID != "Ore" || got[1].Quantity != 3 {
		t.Fatalf("additions = %#v", got)
	}
	public := Public(state, now.Add(7*time.Minute))
	if public.Status != "completed" || !public.CurrentOnline || public.DurationSeconds != 420 {
		t.Fatalf("public = %#v", public)
	}
}

func TestMissingSnapshotWaitsAndEligibilityStartsAtBaseline(t *testing.T) {
	scope := testScope("world-a")
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	state := Advance(State{}, scope, now, 0, Snapshot{}, ErrSnapshotUnavailable)
	if state.Status != "waiting" {
		t.Fatalf("state = %#v", state)
	}
	state = Advance(state, scope, now.Add(30*time.Second), 0, snap("a", map[string]int64{"Wood": 10}), nil)
	if state.Status != "active" || parseTime(state.EligibleAt) != now.Add(5*time.Minute+30*time.Second) {
		t.Fatalf("state = %#v", state)
	}
}

func TestObservationGapStartsNewBaseline(t *testing.T) {
	scope := testScope("world-a")
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	state := Advance(State{}, scope, now, 0, snap("a", map[string]int64{"Wood": 10}), nil)
	state = Advance(state, scope, now.Add(2*time.Minute), 0, snap("b", map[string]int64{"Wood": 50}), nil)
	if state.Baseline.Fingerprint != "b" || state.StartedAt != formatTime(now.Add(2*time.Minute)) {
		t.Fatalf("gap must reset session: %#v", state)
	}
}

func TestStorageIsIsolatedByWorld(t *testing.T) {
	store := &memoryStore{values: map[string]string{}}
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	worldA, worldB := testScope("world-a"), testScope("world-b")
	if err := Observe(context.Background(), store, worldA, now, 0, snap("a", map[string]int64{"Wood": 10}), nil); err != nil {
		t.Fatal(err)
	}
	if err := Observe(context.Background(), store, worldB, now, 0, snap("b", map[string]int64{"Stone": 20}), nil); err != nil {
		t.Fatal(err)
	}
	stateA, _ := Load(context.Background(), store, worldA)
	stateB, _ := Load(context.Background(), store, worldB)
	if stateA.Baseline.Totals["Wood"] != 10 || stateB.Baseline.Totals["Stone"] != 20 || StorageKey(worldA) == StorageKey(worldB) {
		t.Fatalf("states = %#v / %#v", stateA, stateB)
	}
}
