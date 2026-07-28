package api

import (
	"testing"

	"palpanel/internal/saveindex"
)

func TestBaseFeedBoxViewsFiltersAndAggregatesFeedBoxes(t *testing.T) {
	containers := []saveindex.Container{
		{
			ContainerID: "feed-1",
			OwnerType:   "map_object",
			OwnerID:     "object-feed-1",
			Slots: []saveindex.Slot{
				{Slot: 0, ItemID: "Baked_Berries", Count: 30},
				{Slot: 1, ItemID: "Baked_Berries", Count: 20},
				{Slot: 2, ItemID: "", Count: 0},
			},
		},
		{
			ContainerID: "feed-2",
			OwnerType:   "map_object",
			OwnerID:     "object-feed-2",
			Slots: []saveindex.Slot{
				{Slot: 0, ItemID: "Baked_Berries", Count: 10},
				{Slot: 1, ItemID: "Mushroom", Count: 5},
			},
		},
		{
			ContainerID: "chest-1",
			OwnerType:   "map_object",
			OwnerID:     "object-chest-1",
			Slots:       []saveindex.Slot{{Slot: 0, ItemID: "Wood", Count: 999}},
		},
	}
	entities := []saveindex.MapEntity{
		{Type: "map_object", ID: "object-feed-1", Label: "PalFoodBox"},
		{Type: "map_object", ID: "object-feed-2", Label: "CoolerPalFoodBox"},
		{Type: "map_object", ID: "object-chest-1", Label: "Infra_ItemChest_Grade_01"},
	}

	boxes, items, summary := baseFeedBoxViews(containers, entities)
	if len(boxes) != 2 {
		t.Fatalf("feed boxes = %d, want 2", len(boxes))
	}
	if summary.BoxCount != 2 || summary.EmptyBoxCount != 0 || summary.OccupiedSlots != 4 || summary.TotalItems != 65 || summary.ItemTypes != 2 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if len(items) != 2 || items[0].ItemID != "Baked_Berries" || items[0].Count != 60 || items[0].BoxCount != 2 {
		t.Fatalf("unexpected aggregate items: %#v", items)
	}
	if boxes[0].ContainerName == "" || boxes[1].ContainerName == "" {
		t.Fatalf("container names were not resolved: %#v", boxes)
	}
}

func TestBaseFeedBoxViewsKeepsEmptyFeedBoxes(t *testing.T) {
	boxes, items, summary := baseFeedBoxViews(
		[]saveindex.Container{{ContainerID: "feed-empty", OwnerType: "map_object", OwnerID: "object-feed", Slots: nil}},
		[]saveindex.MapEntity{{Type: "map_object", ID: "object-feed", Label: "PalFoodBox"}},
	)
	if len(boxes) != 1 || len(boxes[0].Slots) != 0 || len(items) != 0 {
		t.Fatalf("unexpected empty feed box response: boxes=%#v items=%#v", boxes, items)
	}
	if summary.BoxCount != 1 || summary.EmptyBoxCount != 1 || summary.TotalItems != 0 || summary.ItemTypes != 0 {
		t.Fatalf("unexpected empty summary: %#v", summary)
	}
}

func TestIsFeedBoxContainerType(t *testing.T) {
	for _, value := range []string{"PalFoodBox", "CoolerPalFoodBox", "BP_PalFoodBox_C"} {
		if !isFeedBoxContainerType(value) {
			t.Fatalf("%q should be recognized as a feed box", value)
		}
	}
	if isFeedBoxContainerType("Refrigerator") || isFeedBoxContainerType("Infra_ItemChest_Grade_01") {
		t.Fatal("non-feed storage was recognized as a feed box")
	}
}
