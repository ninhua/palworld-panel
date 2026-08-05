package unattendedinventory

import (
	"testing"

	"palpanel/internal/saveindex"
)

func TestSnapshotFromIndexUsesTrustedInventoryAndDeduplicates(t *testing.T) {
	index := saveindex.Index{
		GeneratedAt: "2026-08-06T02:00:00Z",
		Snapshot:    saveindex.Snapshot{Fingerprint: "fp-1"},
		Players:     []saveindex.Player{{PlayerUID: "player-1"}},
		Guilds:      []saveindex.Guild{{ID: "guild-1"}},
		Bases:       []saveindex.Base{{ID: "base-1", Containers: []string{"base-box"}}},
		Containers: []saveindex.Container{
			{ContainerID: "bag-1", OwnerType: "player", OwnerID: "player-1", Slots: []saveindex.Slot{{ItemID: "Wood", Count: 10}, {ItemID: "Stone", Count: 3}}},
			{ContainerID: "BAG-1", OwnerType: "player", OwnerID: "player-1", Slots: []saveindex.Slot{{ItemID: "Wood", Count: 99}}},
			{ContainerID: "base-box", OwnerType: "map_object", OwnerID: "object-1", Slots: []saveindex.Slot{{ItemID: "Wood", Count: 5}}},
			{ContainerID: "guild-box", OwnerType: "guild", OwnerID: "guild-1", Slots: []saveindex.Slot{{ItemID: "Ore", Count: 7}}},
			{ContainerID: "wild-box", OwnerType: "map_object", OwnerID: "wild-object", Slots: []saveindex.Slot{{ItemID: "Wood", Count: 1000}}},
			{ContainerID: "spoofed-player", OwnerType: "player", OwnerID: "missing-player", Slots: []saveindex.Slot{{ItemID: "Stone", Count: 500}}},
		},
	}
	snapshot := SnapshotFromIndex(index, saveindex.Status{State: "stale", Stale: true})
	if snapshot.Totals["Wood"] != 15 || snapshot.Totals["Stone"] != 3 || snapshot.Totals["Ore"] != 7 {
		t.Fatalf("trusted totals = %#v", snapshot.Totals)
	}
	if len(snapshot.Totals) != 3 || !snapshot.Stale || snapshot.Fingerprint != "fp-1" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
