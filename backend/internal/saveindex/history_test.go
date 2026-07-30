package saveindex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"palpanel/internal/appconfig"
)

func TestHistoryCapturesPrivateSanitizedSnapshotsAndDiffs(t *testing.T) {
	manager, worldDir := testHistoryManager(t)
	before := Index{
		GeneratedAt: "2026-07-29T10:00:00Z",
		Parser:      "sav-cli/test",
		SourcePath:  "/private/world",
		Snapshot:    Snapshot{Fingerprint: strings.Repeat("a", 32), Files: []SnapshotFile{{Path: "/private/world/Level.sav", Size: 123, MTime: 456}}},
		Warnings:    []string{"private /srv/palworld/Level.sav warning"},
		Players: []Player{{
			PlayerUID: "f23d556c-0000-0000-0000-000000000000",
			SteamID:   "steam_76561198000000001", Nickname: "Alice", Level: 10,
			IP: "10.0.0.9", Ping: floatPointer(12), InventorySummary: map[string]any{"private_path": "/srv/palworld"},
		}},
		Pals:       []Pal{{InstanceID: "pal-1", CharacterID: "SheepBall", Nickname: "Old", Level: 5}},
		Containers: []Container{{ContainerID: "box-1", OwnerType: "player", OwnerID: "f23d", Slots: []Slot{{Slot: 0, ItemID: "Wood", Count: 10}}}},
	}
	after := before
	after.GeneratedAt = "2026-07-29T10:05:00Z"
	after.Players = append([]Player(nil), before.Players...)
	after.Players[0].Level = 12
	after.Pals = append([]Pal(nil), before.Pals...)
	after.Pals = append(after.Pals, Pal{InstanceID: "pal-2", CharacterID: "CatMage", Nickname: "New", Level: 8})
	after.Containers = []Container{{ContainerID: "box-1", OwnerType: "player", OwnerID: "f23d", Slots: []Slot{{Slot: 0, ItemID: "Wood", Count: 7}, {Slot: 1, ItemID: "Stone", Count: 4}}}}

	if err := manager.captureHistorySnapshot(worldDir, strings.Repeat("a", 32), before); err != nil {
		t.Fatal(err)
	}
	if err := manager.captureHistorySnapshot(worldDir, strings.Repeat("b", 32), after); err != nil {
		t.Fatal(err)
	}
	state, err := manager.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Items) != 2 || state.Items[0].Fingerprint != strings.Repeat("b", 32) {
		t.Fatalf("unexpected history state: %#v", state)
	}
	worldKey := historyWorldKey(worldDir)
	path, err := manager.historySnapshotPath(worldKey, state.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode=%o", info.Mode().Perm())
	}
	manifestInfo, err := os.Stat(manager.historyManifestPath(worldKey))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && manifestInfo.Mode().Perm() != 0o600 {
		t.Fatalf("manifest mode=%o", manifestInfo.Mode().Perm())
	}
	directoryInfo, err := os.Stat(manager.historyWorldDirectory(worldKey))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("history directory mode=%o", directoryInfo.Mode().Perm())
	}
	stored, err := manager.readHistorySnapshot(worldKey, state.Items[1])
	if err != nil {
		t.Fatal(err)
	}
	if stored.Index.SourcePath != "" || len(stored.Index.Warnings) != 0 || len(stored.Index.Snapshot.Files) != 0 || stored.Index.Players[0].IP != "" || stored.Index.Players[0].Ping != nil || len(stored.Index.Players[0].InventorySummary) != 0 {
		t.Fatalf("private fields were retained: player=%#v snapshot=%#v warnings=%#v", stored.Index.Players[0], stored.Index.Snapshot, stored.Index.Warnings)
	}

	diff, err := manager.HistoryDiff(state.Items[1].ID, state.Items[0].ID, HistoryDiffOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if diff.Summary.PlayersChanged != 1 || diff.Summary.PalsAdded != 1 || diff.Summary.ItemsIncreased != 1 || diff.Summary.ItemsDecreased != 1 {
		t.Fatalf("unexpected diff summary: %#v", diff.Summary)
	}
	if diff.Total < 4 {
		t.Fatalf("expected entity and item changes, got %#v", diff.Items)
	}

	itemsOnly, err := manager.HistoryDiff(state.Items[1].ID, state.Items[0].ID, HistoryDiffOptions{Category: "items", Query: "stone", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if itemsOnly.Total != 1 || itemsOnly.Items[0].ID != "Stone" || itemsOnly.Items[0].Delta != 4 {
		t.Fatalf("unexpected filtered diff: %#v", itemsOnly)
	}
}

func TestHistoryContainerSummaryDetectsChangesOutsideDisplayLimit(t *testing.T) {
	before := make([]Slot, 0, 21)
	after := make([]Slot, 0, 21)
	for index := 0; index < 21; index++ {
		itemID := fmt.Sprintf("Item%02d", index)
		before = append(before, Slot{Slot: index, ItemID: itemID, Count: 1})
		count := 1
		if index == 20 {
			count = 2
		}
		after = append(after, Slot{Slot: index, ItemID: itemID, Count: count})
	}
	if historyContainerContents(before) == historyContainerContents(after) {
		t.Fatal("truncated container summaries must retain a digest of hidden entries")
	}
}

func TestHistoryCaptureDoesNotMutateLiveIndex(t *testing.T) {
	manager, worldDir := testHistoryManager(t)
	ping := float64(21)
	index := Index{
		SourcePath:  "/private/live/world",
		GeneratedAt: "2026-07-29T10:00:00Z",
		Players:     []Player{{PlayerUID: "uid-1", Nickname: "Alice", IP: "10.0.0.5", Ping: &ping, InventorySummary: map[string]any{"Wood": 2}}},
	}
	if err := manager.captureHistorySnapshot(worldDir, strings.Repeat("e", 32), index); err != nil {
		t.Fatal(err)
	}
	if index.SourcePath != "/private/live/world" || index.Players[0].IP != "10.0.0.5" || index.Players[0].Ping == nil || *index.Players[0].Ping != 21 {
		t.Fatalf("history capture mutated the live index: %#v", index)
	}
}

func TestHistorySeparatesWorldDirectories(t *testing.T) {
	manager, firstWorld := testHistoryManager(t)
	secondWorld := filepath.Join(filepath.Dir(firstWorld), "OTHER")
	if err := os.MkdirAll(filepath.Join(secondWorld, "Players"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondWorld, "Level.sav"), []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if historyWorldKey(firstWorld) == historyWorldKey(secondWorld) {
		t.Fatal("different world directories shared a history key")
	}
	if err := manager.captureHistorySnapshot(firstWorld, strings.Repeat("1", 32), Index{GeneratedAt: "2026-07-29T10:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.captureHistorySnapshot(secondWorld, strings.Repeat("2", 32), Index{GeneratedAt: "2026-07-29T10:01:00Z"}); err != nil {
		t.Fatal(err)
	}
	firstManifest, err := manager.loadHistoryManifest(historyWorldKey(firstWorld))
	if err != nil {
		t.Fatal(err)
	}
	secondManifest, err := manager.loadHistoryManifest(historyWorldKey(secondWorld))
	if err != nil {
		t.Fatal(err)
	}
	if len(firstManifest.Items) != 1 || firstManifest.Items[0].Fingerprint != strings.Repeat("1", 32) || len(secondManifest.Items) != 1 || secondManifest.Items[0].Fingerprint != strings.Repeat("2", 32) {
		t.Fatalf("world histories were not isolated: first=%#v second=%#v", firstManifest.Items, secondManifest.Items)
	}
}

func TestHistoryIgnoresPlayersWithoutStableIdentity(t *testing.T) {
	summary := HistoryDiffSummary{}
	changes := diffPlayers([]Player{{Level: 1}}, []Player{{Level: 2}}, &summary)
	if len(changes) != 0 || summary.PlayersChanged != 0 {
		t.Fatalf("anonymous player rows should not collide: changes=%#v summary=%#v", changes, summary)
	}
}

func TestHistoryRemovalOnlyDeletesSelectedWorld(t *testing.T) {
	manager, firstWorld := testHistoryManager(t)
	secondWorld := filepath.Join(filepath.Dir(firstWorld), "OTHER")
	if err := os.MkdirAll(filepath.Join(secondWorld, "Players"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondWorld, "Level.sav"), []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.captureHistorySnapshot(firstWorld, strings.Repeat("3", 32), Index{GeneratedAt: "2026-07-29T10:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.captureHistorySnapshot(secondWorld, strings.Repeat("4", 32), Index{GeneratedAt: "2026-07-29T10:01:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.RemoveHistoryForWorld(firstWorld); err != nil {
		t.Fatal(err)
	}
	firstManifest, err := manager.loadHistoryManifest(historyWorldKey(firstWorld))
	if err != nil {
		t.Fatal(err)
	}
	secondManifest, err := manager.loadHistoryManifest(historyWorldKey(secondWorld))
	if err != nil {
		t.Fatal(err)
	}
	if len(firstManifest.Items) != 0 || len(secondManifest.Items) != 1 {
		t.Fatalf("history cleanup crossed world boundary: first=%#v second=%#v", firstManifest.Items, secondManifest.Items)
	}
}

func TestHistoryRejectsTraversalAndTamperedArchive(t *testing.T) {
	manager, worldDir := testHistoryManager(t)
	index := Index{GeneratedAt: "2026-07-29T10:00:00Z", Parser: "test", Players: []Player{}}
	if err := manager.captureHistorySnapshot(worldDir, strings.Repeat("c", 32), index); err != nil {
		t.Fatal(err)
	}
	state, err := manager.History()
	if err != nil {
		t.Fatal(err)
	}
	worldKey := historyWorldKey(worldDir)
	if _, err := manager.historySnapshotPath(worldKey, "../../secret"); err == nil {
		t.Fatal("expected traversal id rejection")
	}
	path, err := manager.historySnapshotPath(worldKey, state.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("tampered"))
	_ = file.Close()
	if _, err := manager.HistoryDiff(state.Items[0].ID, state.Items[0].ID, HistoryDiffOptions{}); !errors.Is(err, ErrHistoryCorrupt) {
		t.Fatalf("expected integrity failure, got %v", err)
	}
}

func TestHistorySkipsDuplicateFingerprint(t *testing.T) {
	manager, worldDir := testHistoryManager(t)
	index := Index{GeneratedAt: "2026-07-29T10:00:00Z", Parser: "test"}
	fingerprint := strings.Repeat("d", 32)
	if err := manager.captureHistorySnapshot(worldDir, fingerprint, index); err != nil {
		t.Fatal(err)
	}
	if err := manager.captureHistorySnapshot(worldDir, fingerprint, index); err != nil {
		t.Fatal(err)
	}
	state, err := manager.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Items) != 1 {
		t.Fatalf("duplicate fingerprint created %d snapshots", len(state.Items))
	}
}

func TestHistoryCaptureIfDueSkipsDenseSnapshots(t *testing.T) {
	manager, worldDir := testHistoryManager(t)
	manager.historyMinInterval = 15 * time.Minute

	if err := manager.captureHistorySnapshot(worldDir, strings.Repeat("7", 32), Index{GeneratedAt: "2026-07-30T10:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.captureHistorySnapshotIfDue(worldDir, strings.Repeat("8", 32), Index{GeneratedAt: "2026-07-30T10:01:00Z"}); err != nil {
		t.Fatal(err)
	}
	manifest, err := manager.loadHistoryManifest(historyWorldKey(worldDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Items) != 1 || manifest.Items[0].Fingerprint != strings.Repeat("7", 32) {
		t.Fatalf("dense automatic snapshot was not skipped: %#v", manifest.Items)
	}
}

func TestCompactHistoryItemsKeepsSpacedSnapshotsAndOldestBaseline(t *testing.T) {
	items := []HistorySnapshot{
		{ID: "newest", CapturedAt: "2026-07-30T10:30:00Z"},
		{ID: "dense", CapturedAt: "2026-07-30T10:29:00Z"},
		{ID: "middle", CapturedAt: "2026-07-30T10:15:00Z"},
		{ID: "oldest", CapturedAt: "2026-07-30T10:00:00Z"},
	}
	removed := compactHistoryItems(&items, 15*time.Minute)
	if got := []string{items[0].ID, items[1].ID, items[2].ID}; strings.Join(got, ",") != "newest,middle,oldest" {
		t.Fatalf("unexpected compacted history: %#v", items)
	}
	if len(removed) != 1 || removed[0].ID != "dense" {
		t.Fatalf("unexpected removed snapshots: %#v", removed)
	}

	denseOnly := []HistorySnapshot{
		{ID: "latest", CapturedAt: "2026-07-30T10:03:00Z"},
		{ID: "middle", CapturedAt: "2026-07-30T10:02:00Z"},
		{ID: "baseline", CapturedAt: "2026-07-30T10:01:00Z"},
	}
	compactHistoryItems(&denseOnly, 15*time.Minute)
	if len(denseOnly) != 2 || denseOnly[0].ID != "latest" || denseOnly[1].ID != "baseline" {
		t.Fatalf("dense history did not preserve the oldest baseline: %#v", denseOnly)
	}
}

func testHistoryManager(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	worldDir := filepath.Join(root, "server", "Pal", "Saved", "SaveGames", "0", "WORLD")
	if err := os.MkdirAll(filepath.Join(worldDir, "Players"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "Level.sav"), []byte("level"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := appconfig.Config{
		ServerDir: root + "/server", SaveIndexerEnabled: true,
		SaveIndexCacheDir: filepath.Join(root, "cache"), SaveIndexTimeoutSeconds: 1,
	}.WithServerDirectoryState()
	manager := NewManager(cfg)
	// Most history unit tests intentionally create snapshots back-to-back.
	// Production rebuilds use the configured semantic sampling interval.
	manager.historyMinInterval = 0
	return manager, worldDir
}

func floatPointer(value float64) *float64 { return &value }

func TestHistoryBuildsSemanticPlayerPalInventoryAndBaseEvents(t *testing.T) {
	playerUID := "f23d556c-0000-0000-0000-000000000000"
	before := Index{
		Players: []Player{{PlayerUID: playerUID, Nickname: "Alice", Level: 10, GuildID: "guild-1", GuildName: "Builders"}},
		Bases: []Base{{
			ID: "base-1", Name: "Hill Base", GuildID: "guild-1", GuildName: "Builders",
			StructuresCount: 12, Containers: []string{"base-box"}, Workers: []Worker{},
		}},
		Containers: []Container{
			{ContainerID: "player-bag", OwnerType: "player", OwnerID: playerUID, Slots: []Slot{{ItemID: "Wood", Count: 2}}},
			{ContainerID: "base-box", OwnerType: "map_object", OwnerID: "chest-1", Slots: []Slot{{ItemID: "Stone", Count: 10}}},
		},
	}
	after := before
	after.Players = append([]Player(nil), before.Players...)
	after.Players[0].Level = 12
	after.Bases = append([]Base(nil), before.Bases...)
	after.Bases[0].StructuresCount = 15
	after.Bases[0].Workers = []Worker{{InstanceID: "pal-1", CharacterID: "Anubis", Level: 20}}
	after.Pals = []Pal{{
		InstanceID: "pal-1", CharacterID: "Anubis", Level: 20, OwnerPlayerUID: playerUID,
		ContainerID: "party-1", LocationType: "party", Status: "active",
	}}
	after.Containers = []Container{
		{ContainerID: "player-bag", OwnerType: "player", OwnerID: playerUID, Slots: []Slot{{ItemID: "Wood", Count: 2}, {ItemID: "AssaultRifle", Count: 1}}},
		{ContainerID: "base-box", OwnerType: "map_object", OwnerID: "chest-1", Slots: []Slot{{ItemID: "Stone", Count: 14}}},
	}

	diff := buildHistoryDiff(HistorySnapshot{}, before, HistorySnapshot{}, after, HistoryDiffOptions{Limit: 200})
	for _, expected := range []string{"player_level_up", "pal_acquired", "base_structures_added", "base_worker_assigned", "item_gained"} {
		if !historyHasEvent(diff.Events, expected) {
			t.Fatalf("missing semantic event %q: %#v", expected, diff.Events)
		}
	}
	weapon := historyFindEvent(diff.Events, "item_gained", "AssaultRifle")
	if weapon.ActorLabel != "Alice" || weapon.Delta != 1 || weapon.Metadata["equipment"] != "true" {
		t.Fatalf("unexpected player equipment event: %#v", weapon)
	}
	baseItem := historyFindEvent(diff.Events, "item_gained", "Stone")
	if baseItem.ActorType != "base" || baseItem.ActorLabel != "Hill Base" || baseItem.Delta != 4 {
		t.Fatalf("unexpected base inventory event: %#v", baseItem)
	}
	pal := historyFindEvent(diff.Events, "pal_acquired", "pal-1")
	if pal.ActorLabel != "Alice" || pal.Metadata["character_id"] != "Anubis" || !pal.Inferred {
		t.Fatalf("unexpected pal acquisition event: %#v", pal)
	}
}

func TestHistorySemanticEventsRespectCategoryAndQuery(t *testing.T) {
	before := Index{Containers: []Container{{ContainerID: "bag", OwnerType: "player", OwnerID: "uid-1", Slots: []Slot{}}}}
	after := Index{Containers: []Container{{ContainerID: "bag", OwnerType: "player", OwnerID: "uid-1", Slots: []Slot{{ItemID: "LegendarySword", Count: 1}, {ItemID: "Stone", Count: 5}}}}}
	diff := buildHistoryDiff(HistorySnapshot{}, before, HistorySnapshot{}, after, HistoryDiffOptions{Category: "items", Query: "sword", Limit: 10})
	if diff.EventTotal != 1 || len(diff.Events) != 1 || diff.Events[0].SubjectID != "LegendarySword" {
		t.Fatalf("unexpected filtered semantic events: %#v", diff.Events)
	}
}

func historyHasEvent(events []HistoryEvent, kind string) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func historyFindEvent(events []HistoryEvent, kind, subjectID string) HistoryEvent {
	for _, event := range events {
		if event.Kind == kind && event.SubjectID == subjectID {
			return event
		}
	}
	return HistoryEvent{}
}
