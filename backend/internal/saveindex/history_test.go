package saveindex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
	return NewManager(cfg), worldDir
}

func floatPointer(value float64) *float64 { return &value }
