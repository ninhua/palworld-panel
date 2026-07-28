package api

import (
	"testing"

	"palpanel/internal/saveindex"
)

func TestGlobalInventoryFeatureRegistered(t *testing.T) {
	for _, feature := range patchFeatures {
		if feature == "global-inventory-browser" {
			return
		}
	}
	t.Fatalf("global-inventory-browser not registered in %#v", patchFeatures)
}

func TestBuildGlobalInventoryAggregatesPlayerBaseAndUnknownLocations(t *testing.T) {
	index := saveindex.Index{
		Players: []saveindex.Player{{PlayerUID: "player-1", SteamID: "steam-1", Nickname: "Alice"}},
		Bases: []saveindex.Base{{
			ID:         "base-1",
			Name:       "MainBase",
			GuildName:  "GuildOne",
			Containers: []string{"base-chest"},
		}},
		MapEntities: []saveindex.MapEntity{{Type: "map_object", ID: "map-chest", Label: "Infra_ItemChest_Grade_02"}},
		Containers: []saveindex.Container{
			{ContainerID: "player-bag", OwnerType: "player", OwnerID: "player-1", Slots: []saveindex.Slot{{Slot: 0, ItemID: "Stone", Count: 7}}},
			{ContainerID: "base-chest", OwnerType: "map_object", OwnerID: "map-chest", Slots: []saveindex.Slot{{Slot: 1, ItemID: "Stone", Count: 20}, {Slot: 2, ItemID: "Wood", Count: 5}}},
			{ContainerID: "orphan", OwnerType: "map_object", OwnerID: "unknown-object", Slots: []saveindex.Slot{{Slot: 0, ItemID: "Stone", Count: 3}}},
		},
	}

	items, categories := buildGlobalInventory(index, map[string]string{"base-1": "北境仓库"}, "all")
	if len(items) != 2 || len(categories) == 0 {
		t.Fatalf("items/categories = %#v / %#v", items, categories)
	}
	var stone globalInventoryItem
	for _, item := range items {
		if item.ItemID == "Stone" {
			stone = item
		}
	}
	if stone.TotalCount != 30 || len(stone.Locations) != 3 {
		t.Fatalf("stone aggregate = %#v", stone)
	}
	owners := map[string]bool{}
	for _, location := range stone.Locations {
		owners[location.OwnerType+":"+location.OwnerName] = true
	}
	for _, expected := range []string{"player:Alice", "base:北境仓库", "unknown:未识别容器"} {
		if !owners[expected] {
			t.Fatalf("missing owner %q in %#v", expected, owners)
		}
	}
}

func TestBuildGlobalInventoryOwnerScopeAndSearch(t *testing.T) {
	index := saveindex.Index{
		Players: []saveindex.Player{{PlayerUID: "player-1", Nickname: "Alice"}},
		Bases:   []saveindex.Base{{ID: "base-1", Name: "MainBase", Containers: []string{"base-chest"}}},
		Containers: []saveindex.Container{
			{ContainerID: "player-bag", OwnerType: "player", OwnerID: "player-1", Slots: []saveindex.Slot{{Slot: 0, ItemID: "BerrySeeds", Count: 4}}},
			{ContainerID: "base-chest", OwnerType: "map_object", OwnerID: "object-1", Slots: []saveindex.Slot{{Slot: 0, ItemID: "BerrySeeds", Count: 9}}},
		},
	}

	items, _ := buildGlobalInventory(index, map[string]string{"base-1": "农场"}, "base")
	if len(items) != 1 || items[0].TotalCount != 9 || len(items[0].Locations) != 1 || items[0].Category != "种子" {
		t.Fatalf("base-scoped inventory = %#v", items)
	}
	if filtered := filterGlobalInventory(items, "农场", "种子"); len(filtered) != 1 {
		t.Fatalf("filtered inventory = %#v", filtered)
	}
	if filtered := filterGlobalInventory(items, "Alice", "all"); len(filtered) != 0 {
		t.Fatalf("unexpected player match in base scope: %#v", filtered)
	}
}

func TestInventoryItemCategory(t *testing.T) {
	cases := map[string]string{
		"BerrySeeds":       "种子",
		"AssaultRifleAmmo": "弹药",
		"PalSphere":        "帕鲁球",
		"Stone":            "材料",
		"FutureItem":       "其他",
	}
	for itemID, expected := range cases {
		if got := inventoryItemCategory(itemID); got != expected {
			t.Fatalf("inventoryItemCategory(%q) = %q, want %q", itemID, got, expected)
		}
	}
}
