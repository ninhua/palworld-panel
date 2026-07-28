package api

import (
	"testing"

	"github.com/gin-gonic/gin"

	"palpanel/internal/saveindex"
)

func TestBaseStorageContainersIncludesMapObjectContainersLinkedByBase(t *testing.T) {
	index := saveindex.Index{
		Bases: []saveindex.Base{{
			ID:         "base-1",
			Name:       "Base 1",
			Containers: []string{"container-map-object", "container-direct"},
		}},
		MapEntities: []saveindex.MapEntity{{Type: "map_object", ID: "map-object-1", Label: "Infra_ItemChest_Grade_02"}},
		Containers: []saveindex.Container{
			{
				ContainerID: "container-map-object",
				OwnerType:   "map_object",
				OwnerID:     "map-object-1",
				Slots:       []saveindex.Slot{{Slot: 0, ItemID: "Stone", Count: 20}},
			},
			{
				ContainerID: "container-direct",
				OwnerType:   "base",
				OwnerID:     "base-1",
				Slots:       []saveindex.Slot{{Slot: 1, ItemID: "Wood", Count: 10}},
			},
			{
				ContainerID: "other-container",
				OwnerType:   "map_object",
				OwnerID:     "map-object-2",
				Slots:       []saveindex.Slot{{Slot: 0, ItemID: "Fiber", Count: 5}},
			},
		},
	}

	containers := baseStorageContainers(index, "base-1")
	if len(containers) != 2 {
		t.Fatalf("baseStorageContainers returned %d containers, want 2: %#v", len(containers), containers)
	}

	ids := map[string]bool{}
	byID := map[string]gin.H{}
	for _, container := range containers {
		containerID := container["container_id"].(string)
		ids[containerID] = true
		byID[containerID] = container
	}
	if !ids["container-map-object"] || !ids["container-direct"] {
		t.Fatalf("unexpected container IDs: %#v", ids)
	}
	if ids["other-container"] {
		t.Fatalf("unrelated container included: %#v", ids)
	}
	mapObjectContainer := byID["container-map-object"]
	if mapObjectContainer["container_type"] != "Infra_ItemChest_Grade_02" || mapObjectContainer["container_name"] != "金属箱" {
		t.Fatalf("map-object container identity = %#v", mapObjectContainer)
	}
	slots := mapObjectContainer["slots"].([]gin.H)
	if slots[0]["item_icon"] != "stone" {
		t.Fatalf("item icon = %#v", slots[0])
	}
	directContainer := byID["container-direct"]
	if directContainer["container_type"] != "base" || directContainer["container_name"] != "基地仓库" {
		t.Fatalf("direct container identity = %#v", directContainer)
	}
}

func TestBaseStorageContainersKeepsDirectOwnerFallback(t *testing.T) {
	index := saveindex.Index{
		Containers: []saveindex.Container{{
			ContainerID: "legacy-container",
			OwnerType:   "base",
			OwnerID:     "base-legacy",
			Slots:       []saveindex.Slot{{Slot: 0, ItemID: "Stone", Count: 1}},
		}},
	}

	containers := baseStorageContainers(index, "base-legacy")
	if len(containers) != 1 {
		t.Fatalf("baseStorageContainers returned %d containers, want 1", len(containers))
	}
}
