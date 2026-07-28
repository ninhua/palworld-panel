package unattendedinventory

import (
	"testing"

	"palpanel/internal/saveindex"
)

func TestSnapshotFromIndexDeduplicatesContainers(t *testing.T) {
	index := saveindex.Index{
		GeneratedAt: "2026-07-26T10:00:00Z",
		Snapshot:    saveindex.Snapshot{Fingerprint: "fp-1"},
		Containers: []saveindex.Container{
			{ContainerID: "bag-1", Slots: []saveindex.Slot{{ItemID: "Wood", Count: 10}, {ItemID: "Stone", Count: 3}}},
			{ContainerID: "BAG-1", Slots: []saveindex.Slot{{ItemID: "Wood", Count: 99}}},
			{ContainerID: "box-2", Slots: []saveindex.Slot{{ItemID: "Wood", Count: 5}, {ItemID: "Ore", Count: 0}}},
		},
	}
	snapshot := SnapshotFromIndex(index, saveindex.Status{State: "stale", Stale: true})
	if snapshot.Totals["Wood"] != 15 || snapshot.Totals["Stone"] != 3 || snapshot.Stale != true || snapshot.Fingerprint != "fp-1" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
