package startergift

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"palpanel/internal/db"
	"palpanel/internal/paldefender"
	"palpanel/internal/playerpresence"
)

type fakeDispatcher struct {
	mu              sync.Mutex
	itemBatches     [][]ItemGrant
	templateBatches [][]string
	failTemplates   int
	resolveErr      error
	resolvedID      string
	resolvedAliases [][]string
}

func (f *fakeDispatcher) ResolvePlayer(_ context.Context, aliases []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolvedAliases = append(f.resolvedAliases, append([]string(nil), aliases...))
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	if f.resolvedID != "" {
		return f.resolvedID, nil
	}
	for _, alias := range aliases {
		if strings.TrimSpace(alias) != "" {
			return alias, nil
		}
	}
	return "", ErrPlayerNotReady
}

func TestResolvePalDefenderPlayerIgnoresAccountStatus(t *testing.T) {
	players := []paldefender.RESTPlayer{
		{UserID: "steam_76561198000000001", PlayerUID: "11111111-2222-3333-4444-555555555555", Status: "Offline"},
	}
	resolved, err := resolvePalDefenderPlayer(players, []string{"11111111222233334444555555555555"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "steam_76561198000000001" {
		t.Fatalf("resolved=%q", resolved)
	}
}

func TestResolvePalDefenderPlayerAcceptsEmptyStatusAndSteamAlias(t *testing.T) {
	players := []paldefender.RESTPlayer{
		{UserID: "steam_76561198000000002", PlayerUID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
	}
	resolved, err := resolvePalDefenderPlayer(players, []string{"steam_76561198000000002"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "steam_76561198000000002" {
		t.Fatalf("resolved=%q", resolved)
	}
}

func TestResolvePalDefenderPlayerRejectsUnrelatedAccount(t *testing.T) {
	players := []paldefender.RESTPlayer{{UserID: "steam_other", PlayerUID: "other-player", Status: "online"}}
	if _, err := resolvePalDefenderPlayer(players, []string{"steam_expected", "expected-player"}); !errors.Is(err, ErrPlayerNotReady) {
		t.Fatalf("err=%v", err)
	}
}

func (f *fakeDispatcher) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.itemBatches), len(f.templateBatches)
}

func (f *fakeDispatcher) GiveItems(_ context.Context, _ string, items []ItemGrant) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.itemBatches = append(f.itemBatches, append([]ItemGrant(nil), items...))
	return nil
}

func (f *fakeDispatcher) GivePalTemplates(_ context.Context, _ string, names []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.templateBatches = append(f.templateBatches, append([]string(nil), names...))
	if f.failTemplates > 0 {
		f.failTemplates--
		return errors.New("temporary PalDefender failure")
	}
	return nil
}

func openTestStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func testScope(id string) playerpresence.Scope {
	return playerpresence.Scope{ID: "server-world:" + id, WorldID: id}
}

func saveTestConfig(t *testing.T, store *db.Store, scope playerpresence.Scope) Config {
	t.Helper()
	config := Config{
		Enabled: true,
		Items: []ItemGrant{
			{ItemID: "Money", Count: 100},
			{ItemID: "PalSphere", Count: 10},
		},
		PalTemplates:      []string{"starter-a", "starter-b", "starter-c"},
		ItemBatchSize:     1,
		TemplateBatchSize: 2,
		BatchDelayMS:      100,
	}
	if _, err := SaveConfig(context.Background(), store, scope, config); err != nil {
		t.Fatal(err)
	}
	return config
}

func observePresence(t *testing.T, store *db.Store, scope playerpresence.Scope, now time.Time, players []playerpresence.OnlinePlayer) {
	t.Helper()
	if _, err := playerpresence.ObserveScoped(context.Background(), store, scope, now, players); err != nil {
		t.Fatal(err)
	}
}

func TestBaselineExistingPlayersAndBatchNewPlayer(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-a")
	config := saveTestConfig(t, store, scope)
	ctx := context.Background()
	now := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	alice := playerpresence.OnlinePlayer{PlayerUID: "uid-alice", SteamID: "1", Nickname: "Alice"}
	bob := playerpresence.OnlinePlayer{PlayerUID: "uid-bob", SteamID: "2", Nickname: "Bob"}

	if err := BaselineKnownPlayers(ctx, store, scope, []playerpresence.OnlinePlayer{alice}); err != nil {
		t.Fatal(err)
	}
	observePresence(t, store, scope, now, []playerpresence.OnlinePlayer{alice, bob})
	if err := Observe(ctx, store, scope, now, []playerpresence.OnlinePlayer{alice, bob}, nil); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDispatcher{}
	if err := processOne(ctx, store, scope, fake, identity(bob.SteamID)); err != nil {
		t.Fatal(err)
	}
	if len(fake.itemBatches) != 2 || len(fake.templateBatches) != 2 {
		t.Fatalf("batches items=%#v templates=%#v", fake.itemBatches, fake.templateBatches)
	}
	snapshot, err := LoadSnapshot(ctx, store, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Grants) != 1 || snapshot.Grants[0].Status != "success" {
		t.Fatalf("grants=%#v", snapshot.Grants)
	}
	grant := snapshot.Grants[0]
	if grant.ItemTotal != len(config.Items) || grant.TemplateTotal != len(config.PalTemplates) {
		t.Fatalf("frozen plan totals=%#v", grant)
	}
}

func TestEnabledConfigGrantsFirstPlayerInNewWorld(t *testing.T) {
	store := openTestStore(t)
	worldA := testScope("world-a")
	worldB := testScope("world-b")
	saveTestConfig(t, store, worldA)
	now := time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC)
	bob := playerpresence.OnlinePlayer{SteamID: "2", Nickname: "Bob"}

	observePresence(t, store, worldB, now, []playerpresence.OnlinePlayer{bob})
	if err := Observe(context.Background(), store, worldB, now, []playerpresence.OnlinePlayer{bob}, nil); err != nil {
		t.Fatal(err)
	}
	snapshot, err := LoadSnapshot(context.Background(), store, worldB)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Grants) != 1 || snapshot.Grants[0].PlayerID != "2" || snapshot.Grants[0].Status != "pending" {
		t.Fatalf("new-world first player was not queued: %#v", snapshot.Grants)
	}
	oldSnapshot, err := LoadSnapshot(context.Background(), store, worldA)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldSnapshot.Grants) != 0 {
		t.Fatalf("world-a inherited world-b grants: %#v", oldSnapshot.Grants)
	}
}

func TestConfigIsScopedAndFutureWorldsInheritLatestTemplate(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	worldA := testScope("world-a")
	worldB := testScope("world-b")
	worldC := testScope("world-c")

	configA := Config{Enabled: true, Items: []ItemGrant{{ItemID: "Money", Count: 100}}, ItemBatchSize: 20, TemplateBatchSize: 5, BatchDelayMS: 500}
	if _, err := SaveConfig(ctx, store, worldA, configA); err != nil {
		t.Fatal(err)
	}
	inherited, err := LoadSnapshot(ctx, store, worldB)
	if err != nil {
		t.Fatal(err)
	}
	if len(inherited.Config.Items) != 1 || inherited.Config.Items[0].ItemID != "Money" {
		t.Fatalf("world-b did not inherit template: %#v", inherited.Config)
	}

	configB := Config{Enabled: true, Items: []ItemGrant{{ItemID: "PalSphere", Count: 20}}, ItemBatchSize: 10, TemplateBatchSize: 2, BatchDelayMS: 300}
	if _, err := SaveConfig(ctx, store, worldB, configB); err != nil {
		t.Fatal(err)
	}
	snapshotA, err := LoadSnapshot(ctx, store, worldA)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshotA.Config.Items) != 1 || snapshotA.Config.Items[0].ItemID != "Money" {
		t.Fatalf("world-a config was overwritten: %#v", snapshotA.Config)
	}
	snapshotC, err := LoadSnapshot(ctx, store, worldC)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshotC.Config.Items) != 1 || snapshotC.Config.Items[0].ItemID != "PalSphere" {
		t.Fatalf("future world did not inherit latest template: %#v", snapshotC.Config)
	}
}

func TestFailedBatchResumesWithinSameWorld(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-a")
	saveTestConfig(t, store, scope)
	ctx := context.Background()
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	bob := playerpresence.OnlinePlayer{SteamID: "2", Nickname: "Bob"}
	observePresence(t, store, scope, now, []playerpresence.OnlinePlayer{bob})
	if err := Observe(ctx, store, scope, now, []playerpresence.OnlinePlayer{bob}, nil); err != nil {
		t.Fatal(err)
	}

	fake := &fakeDispatcher{failTemplates: 1}
	if err := processOne(ctx, store, scope, fake, identity(bob.SteamID)); err == nil {
		t.Fatal("expected template batch failure")
	}
	snapshot, err := LoadSnapshot(ctx, store, scope)
	if err != nil {
		t.Fatal(err)
	}
	grant := snapshot.Grants[0]
	if grant.Status != "failed" || grant.NextItem != 2 || grant.NextTemplate != 0 {
		t.Fatalf("failed progress=%#v", grant)
	}
	if err := Retry(ctx, store, scope, bob.SteamID); err != nil {
		t.Fatal(err)
	}
	if err := processOne(ctx, store, scope, fake, identity(bob.SteamID)); err != nil {
		t.Fatal(err)
	}
	snapshot, err = LoadSnapshot(ctx, store, scope)
	if err != nil {
		t.Fatal(err)
	}
	grant = snapshot.Grants[0]
	if grant.Status != "success" || grant.NextTemplate != 3 || grant.Attempts != 2 {
		t.Fatalf("resumed grant=%#v", grant)
	}
}

func TestForgetWaitsForOfflineBeforeRearming(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-a")
	saveTestConfig(t, store, scope)
	ctx := context.Background()
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	bob := playerpresence.OnlinePlayer{SteamID: "2", Nickname: "Bob"}
	observePresence(t, store, scope, now, []playerpresence.OnlinePlayer{bob})
	if err := Observe(ctx, store, scope, now, []playerpresence.OnlinePlayer{bob}, nil); err != nil {
		t.Fatal(err)
	}
	if err := Forget(ctx, store, scope, bob.SteamID); err != nil {
		t.Fatal(err)
	}
	if err := Observe(ctx, store, scope, now.Add(15*time.Second), []playerpresence.OnlinePlayer{bob}, nil); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := LoadSnapshot(ctx, store, scope); err != nil || len(snapshot.Grants) != 1 {
		t.Fatalf("still-online rearm must retain the existing grant=%#v err=%v", snapshot.Grants, err)
	}
	if err := Observe(ctx, store, scope, now.Add(30*time.Second), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := Observe(ctx, store, scope, now.Add(45*time.Second), []playerpresence.OnlinePlayer{bob}, nil); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := LoadSnapshot(ctx, store, scope); err != nil || len(snapshot.Grants) != 1 || snapshot.Grants[0].Status != "pending" {
		t.Fatalf("reentered grants=%#v err=%v", snapshot.Grants, err)
	}
}

func TestWorkerOutlivesCanceledObservationContext(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-worker-context")
	saveTestConfig(t, store, scope)
	now := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC)
	bob := playerpresence.OnlinePlayer{PlayerUID: "uid-bob", SteamID: "2", Nickname: "Bob"}
	fake := &fakeDispatcher{resolvedID: "uid-bob"}
	ctx, cancel := context.WithCancel(context.Background())
	if err := Observe(ctx, store, scope, now, []playerpresence.OnlinePlayer{bob}, fake); err != nil {
		t.Fatal(err)
	}
	cancel()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := LoadSnapshot(context.Background(), store, scope)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Grants) == 1 && snapshot.Grants[0].Status == "success" {
			items, templates := fake.counts()
			if items != 2 || templates != 2 {
				t.Fatalf("worker batches items=%d templates=%d", items, templates)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("detached worker did not finish: %#v", mustSnapshot(t, store, scope).Grants)
}

func TestPlayerReadinessIsRetriedWithoutFreezingGrant(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-player-ready")
	saveTestConfig(t, store, scope)
	now := time.Date(2026, 7, 25, 15, 0, 0, 0, time.UTC)
	bob := playerpresence.OnlinePlayer{PlayerUID: "ABC-DEF", SteamID: "7656119", Nickname: "Bob"}
	if err := Observe(context.Background(), store, scope, now, []playerpresence.OnlinePlayer{bob}, nil); err != nil {
		t.Fatal(err)
	}

	fake := &fakeDispatcher{resolveErr: ErrPlayerNotReady}
	if err := processOne(context.Background(), store, scope, fake, identity(bob.SteamID)); err != nil {
		t.Fatal(err)
	}
	grant := mustSnapshot(t, store, scope).Grants[0]
	if grant.Status != "pending" || !strings.Contains(grant.LastError, "has not registered") || grant.Attempts != 0 {
		t.Fatalf("not-ready grant=%#v", grant)
	}

	fake.mu.Lock()
	fake.resolveErr = nil
	fake.resolvedID = "ABCDEF"
	fake.mu.Unlock()
	if err := processOne(context.Background(), store, scope, fake, identity(bob.SteamID)); err != nil {
		t.Fatal(err)
	}
	grant = mustSnapshot(t, store, scope).Grants[0]
	if grant.Status != "success" || grant.Attempts != 1 {
		t.Fatalf("retried grant=%#v", grant)
	}
}

func TestLegacyPlayerNotFoundFailureAutomaticallyRequeues(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-legacy-failure")
	saveTestConfig(t, store, scope)
	now := time.Date(2026, 7, 25, 16, 0, 0, 0, time.UTC)
	bob := playerpresence.OnlinePlayer{SteamID: "7656119", Nickname: "Bob"}
	if err := Observe(context.Background(), store, scope, now, []playerpresence.OnlinePlayer{bob}, nil); err != nil {
		t.Fatal(err)
	}
	state, err := loadState(context.Background(), store, scope)
	if err != nil {
		t.Fatal(err)
	}
	key := identity(bob.SteamID)
	grant := state.Grants[key]
	grant.Status = "failed"
	grant.LastError = "PalDefender REST API returned PLAYER_NOT_FOUND: player was not found"
	state.Grants[key] = grant
	if err := saveState(context.Background(), store, scope, state); err != nil {
		t.Fatal(err)
	}

	fake := &fakeDispatcher{resolvedID: bob.SteamID}
	if err := processOne(context.Background(), store, scope, fake, key); err != nil {
		t.Fatal(err)
	}
	grantView := mustSnapshot(t, store, scope).Grants[0]
	if grantView.Status != "success" || grantView.LastError != "" {
		t.Fatalf("legacy failure was not recovered: %#v", grantView)
	}
}

func mustSnapshot(t *testing.T, store *db.Store, scope playerpresence.Scope) Snapshot {
	t.Helper()
	snapshot, err := LoadSnapshot(context.Background(), store, scope)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestNormalizeConfigRejectsUnsafeOrOversizedPlans(t *testing.T) {
	if _, err := normalizeConfig(Config{Enabled: true, Items: []ItemGrant{{ItemID: "../Money", Count: 1}}}); err == nil {
		t.Fatal("expected invalid item id")
	}
	items := make([]ItemGrant, MaxItems+1)
	for index := range items {
		items[index] = ItemGrant{ItemID: "Money", Count: int64(index + 1)}
	}
	if _, err := normalizeConfig(Config{Items: items}); err == nil {
		t.Fatal("expected oversized item plan")
	}
}

func TestInspectPlayersExplainsKnownCandidateAndGrant(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-decisions")
	saveTestConfig(t, store, scope)
	ctx := context.Background()
	known := playerpresence.OnlinePlayer{PlayerUID: "uid-known", SteamID: "100", Nickname: "Known"}
	candidate := playerpresence.OnlinePlayer{PlayerUID: "uid-new", SteamID: "200", Nickname: "Candidate"}
	if err := BaselineKnownPlayers(ctx, store, scope, []playerpresence.OnlinePlayer{known}); err != nil {
		t.Fatal(err)
	}
	decisions, err := InspectPlayers(ctx, store, scope, []playerpresence.OnlinePlayer{known, candidate})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]PlayerDecision{}
	for _, decision := range decisions {
		byID[decision.PlayerID] = decision
	}
	if byID["100"].Decision != "known_existing" || byID["100"].IsNew {
		t.Fatalf("known decision=%#v", byID["100"])
	}
	if byID["200"].Decision != "unseen_candidate" || !byID["200"].IsNew || !byID["200"].Eligible {
		t.Fatalf("candidate decision=%#v", byID["200"])
	}
	if len(byID["200"].Evidence) < 4 || !strings.Contains(byID["200"].Reason, "不在") {
		t.Fatalf("candidate evidence=%#v", byID["200"])
	}
}

func TestManualNextLoginMarkCanBeRetestedWithoutDeletingPlayer(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-manual-next-login")
	saveTestConfig(t, store, scope)
	ctx := context.Background()
	now := time.Date(2026, 7, 28, 1, 0, 0, 0, time.UTC)
	player := playerpresence.OnlinePlayer{PlayerUID: "uid-debug", SteamID: "300", Nickname: "Debug"}
	if err := BaselineKnownPlayers(ctx, store, scope, []playerpresence.OnlinePlayer{player}); err != nil {
		t.Fatal(err)
	}
	if err := Observe(ctx, store, scope, now.Add(-15*time.Second), []playerpresence.OnlinePlayer{player}, nil); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAction(ctx, store, scope, player, "next_login"); err != nil {
		t.Fatal(err)
	}
	decisions, err := InspectPlayers(ctx, store, scope, []playerpresence.OnlinePlayer{player})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Decision != "marked_new_next_login" || !decisions[0].Rearmed {
		t.Fatalf("marked decision=%#v", decisions)
	}
	if err := Observe(ctx, store, scope, now, []playerpresence.OnlinePlayer{player}, nil); err != nil {
		t.Fatal(err)
	}
	if len(mustSnapshot(t, store, scope).Grants) != 0 {
		t.Fatal("mark must not fire until an offline-to-online transition")
	}
	if err := Observe(ctx, store, scope, now.Add(15*time.Second), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := Observe(ctx, store, scope, now.Add(30*time.Second), []playerpresence.OnlinePlayer{player}, nil); err != nil {
		t.Fatal(err)
	}
	grant := mustSnapshot(t, store, scope).Grants[0]
	if grant.DetectionSource != "manual-next-login" || !grant.Manual || !strings.Contains(grant.DetectionReason, "离线后再次进入") {
		t.Fatalf("manual grant=%#v", grant)
	}
}

func TestSupplementPreservesProgressAndReissueResetsPlan(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-actions")
	config := saveTestConfig(t, store, scope)
	ctx := context.Background()
	player := playerpresence.OnlinePlayer{PlayerUID: "uid-action", SteamID: "400", Nickname: "Action"}
	nowText := time.Date(2026, 7, 28, 2, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	state := EmptyState()
	state.ScopeID = scope.ID
	state.Initialized = true
	markSeen(&state, playerAliases(player))
	grant := newGrantWithReason(player, config, nowText, "automatic", "test", false)
	grant.Status = "failed"
	grant.Phase = "failed"
	grant.NextItem = 1
	grant.NextTemplate = 1
	grant.LastError = "test failure"
	state.Grants[identity(player.SteamID)] = grant
	if err := saveState(ctx, store, scope, state); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAction(ctx, store, scope, player, "supplement"); err != nil {
		t.Fatal(err)
	}
	continued := mustSnapshot(t, store, scope).Grants[0]
	if continued.NextItem != 1 || continued.NextTemplate != 1 || continued.Status != "pending" || continued.Phase != "queued" {
		t.Fatalf("supplement reset progress=%#v", continued)
	}
	if err := ApplyAction(ctx, store, scope, player, "reissue"); err != nil {
		t.Fatal(err)
	}
	reissued := mustSnapshot(t, store, scope).Grants[0]
	if reissued.NextItem != 0 || reissued.NextTemplate != 0 || !reissued.Manual || reissued.DetectionSource != "manual" {
		t.Fatalf("reissue did not reset=%#v", reissued)
	}
	if len(reissued.Events) == 0 || reissued.ProgressPercent != 0 {
		t.Fatalf("reissue events/progress=%#v", reissued)
	}
}

func TestGrantTimelineRecordsResolutionBatchesAndCompletion(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-timeline")
	saveTestConfig(t, store, scope)
	ctx := context.Background()
	player := playerpresence.OnlinePlayer{PlayerUID: "uid-timeline", SteamID: "500", Nickname: "Timeline"}
	now := time.Date(2026, 7, 28, 3, 0, 0, 0, time.UTC)
	if err := Observe(ctx, store, scope, now, []playerpresence.OnlinePlayer{player}, nil); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDispatcher{resolvedID: "pd-user-500"}
	if err := processOne(ctx, store, scope, fake, identity(player.SteamID)); err != nil {
		t.Fatal(err)
	}
	grant := mustSnapshot(t, store, scope).Grants[0]
	if grant.Status != "success" || grant.ProgressPercent != 100 || grant.ResolvedPlayerID != "pd-user-500" {
		t.Fatalf("completed grant=%#v", grant)
	}
	phases := map[string]bool{}
	for _, event := range grant.Events {
		phases[event.Phase] = true
	}
	for _, phase := range []string{"queued", "resolving_player", "ready", "items", "templates", "completed"} {
		if !phases[phase] {
			t.Fatalf("timeline missing %s: %#v", phase, grant.Events)
		}
	}
}

func TestReconcilePlayerAliasesMergesLegacyDuplicateGrants(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-identity-reconcile")
	ctx := context.Background()
	config := saveTestConfig(t, store, scope)
	playerUID := "f23d556c-0000-0000-0000-000000000000"
	steamID := "steam_76561199032061430"
	state := EmptyState()
	state.ScopeID = scope.ID
	state.Initialized = true
	uidPlayer := playerpresence.OnlinePlayer{PlayerUID: playerUID, SteamID: playerUID, Nickname: "tiantian"}
	steamPlayer := playerpresence.OnlinePlayer{PlayerUID: playerUID, SteamID: steamID, Nickname: "tiantian"}
	older := newGrantWithReason(uidPlayer, config, "2026-07-28T01:00:00Z", "manual", "older", true)
	newer := newGrantWithReason(steamPlayer, config, "2026-07-28T02:00:00Z", "manual", "newer", true)
	state.Grants[identity(playerUID)] = older
	state.Grants[identity(steamID)] = newer
	state.Seen[identity(playerUID)] = true
	if err := saveState(ctx, store, scope, state); err != nil {
		t.Fatal(err)
	}

	if err := ReconcilePlayerAliases(ctx, store, scope, []playerpresence.OnlinePlayer{steamPlayer}); err != nil {
		t.Fatal(err)
	}
	snapshot := mustSnapshot(t, store, scope)
	if len(snapshot.Grants) != 1 {
		t.Fatalf("grants=%#v", snapshot.Grants)
	}
	grant := snapshot.Grants[0]
	if grant.PlayerUID != playerUID || grant.SteamID != steamID || grant.DetectionReason != "newer" {
		t.Fatalf("merged grant=%#v", grant)
	}
	decisions, err := InspectPlayers(ctx, store, scope, []playerpresence.OnlinePlayer{steamPlayer})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].PlayerUID != playerUID || decisions[0].SteamID != steamID {
		t.Fatalf("decisions=%#v", decisions)
	}
}

func TestCancelNextLoginKeepsSeenAndClearsRearm(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-cancel-next-login")
	ctx := context.Background()
	player := playerpresence.OnlinePlayer{PlayerUID: "uid-cancel", SteamID: "steam-cancel", Nickname: "Cancel"}
	if err := ApplyAction(ctx, store, scope, player, "next_login"); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAction(ctx, store, scope, player, "cancel_next_login"); err != nil {
		t.Fatal(err)
	}
	decisions, err := InspectPlayers(ctx, store, scope, []playerpresence.OnlinePlayer{player})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Rearmed || decisions[0].Decision != "known_existing" || decisions[0].IsNew {
		t.Fatalf("decision=%#v", decisions)
	}
}

func TestNextLoginRetainsGrantAndExposesCancelState(t *testing.T) {
	store := openTestStore(t)
	scope := testScope("world-retain-next-login-grant")
	config := saveTestConfig(t, store, scope)
	ctx := context.Background()
	player := playerpresence.OnlinePlayer{PlayerUID: "uid-retain", SteamID: "steam-retain", Nickname: "Retain"}
	state := EmptyState()
	state.ScopeID = scope.ID
	state.Initialized = true
	markSeen(&state, playerAliases(player))
	grant := newGrantWithReason(player, config, "2026-07-29T01:00:00Z", "automatic", "original", false)
	grant.Status = "success"
	grant.Phase = "completed"
	state.Grants[identity(player.SteamID)] = grant
	if err := saveState(ctx, store, scope, state); err != nil {
		t.Fatal(err)
	}

	if err := ApplyAction(ctx, store, scope, player, "next_login"); err != nil {
		t.Fatal(err)
	}
	snapshot := mustSnapshot(t, store, scope)
	if len(snapshot.Grants) != 1 || snapshot.Grants[0].Status != "success" {
		t.Fatalf("existing grant was not retained: %#v", snapshot.Grants)
	}
	decisions, err := InspectPlayers(ctx, store, scope, []playerpresence.OnlinePlayer{player})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || !decisions[0].Rearmed || decisions[0].Decision != "marked_new_next_login" || decisions[0].GrantStatus != "success" {
		t.Fatalf("decision=%#v", decisions)
	}
}

func TestCoalescePlayerIdentitiesMergesOfflineAndOnlineAliases(t *testing.T) {
	players := coalescePlayerIdentities([]playerpresence.OnlinePlayer{
		{PlayerUID: "f23d556c-0000-0000-0000-000000000000", Nickname: "tiantian"},
		{PlayerUID: "F23D556C000000000000000000000000", SteamID: "steam_76561199032061430", Nickname: "tiantian"},
	})
	if len(players) != 1 {
		t.Fatalf("players = %#v, want one merged identity", players)
	}
	if players[0].SteamID != "steam_76561199032061430" {
		t.Fatalf("SteamID = %q", players[0].SteamID)
	}
	if identity(players[0].PlayerUID) != "f23d556c000000000000000000000000" {
		t.Fatalf("PlayerUID = %q", players[0].PlayerUID)
	}
}
