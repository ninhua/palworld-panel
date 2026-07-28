package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/pallocalize"
	"palpanel/internal/saveindex"
)

type baseFeedBoxSlot struct {
	Slot       int      `json:"slot"`
	ItemID     string   `json:"item_id"`
	ItemName   string   `json:"item_name"`
	ItemIcon   string   `json:"item_icon"`
	Count      int      `json:"count"`
	Durability *float64 `json:"durability"`
}

type baseFeedBoxView struct {
	ContainerID   string            `json:"container_id"`
	ContainerType string            `json:"container_type"`
	ContainerName string            `json:"container_name"`
	Slots         []baseFeedBoxSlot `json:"slots"`
}

type baseFeedItemSummary struct {
	ItemID   string `json:"item_id"`
	ItemName string `json:"item_name"`
	ItemIcon string `json:"item_icon"`
	Count    int    `json:"count"`
	BoxCount int    `json:"box_count"`
}

type baseFeedBoxSummary struct {
	BoxCount      int `json:"box_count"`
	EmptyBoxCount int `json:"empty_box_count"`
	OccupiedSlots int `json:"occupied_slots"`
	TotalItems    int `json:"total_items"`
	ItemTypes     int `json:"item_types"`
}

func (s Server) getSaveBaseFeedBoxes(c *gin.Context) {
	index, status, err := s.currentSaveIndex(c)
	if err != nil && !status.Stale && status.State != "disabled" {
		fail(c, http.StatusServiceUnavailable, "save_index_unavailable", err.Error())
		return
	}
	customNames, sourceID, namesErr := s.activeBaseCustomNames(c)
	if namesErr != nil {
		fail(c, http.StatusInternalServerError, "base_custom_names_read_failed", namesErr.Error())
		return
	}

	id := c.Param("id")
	for _, base := range index.Bases {
		customName := customNames[base.ID]
		if !matchesID(id, base.ID, base.Name, pallocalize.BaseName(base.Name), customName) {
			continue
		}
		boxes, items, summary := baseFeedBoxViews(baseContainers(index, base.ID), index.MapEntities)
		ok(c, gin.H{
			"base":       flattenBaseWithCustomName(base, customName),
			"feed_boxes": boxes,
			"items":      items,
			"summary":    summary,
			"status":     status,
			"source_id":  sourceID,
		})
		return
	}
	fail(c, http.StatusNotFound, "base_not_found", "base not found")
}

func baseFeedBoxViews(containers []saveindex.Container, entities []saveindex.MapEntity) ([]baseFeedBoxView, []baseFeedItemSummary, baseFeedBoxSummary) {
	entityByID := make(map[string]saveindex.MapEntity, len(entities))
	for _, entity := range entities {
		entityByID[normalizeQuery(entity.ID)] = entity
	}

	boxes := make([]baseFeedBoxView, 0)
	itemTotals := make(map[string]*baseFeedItemSummary)
	itemBoxes := make(map[string]map[string]struct{})
	summary := baseFeedBoxSummary{}

	for _, container := range containers {
		containerType, containerName := baseStorageContainerIdentity(container, entityByID)
		if !isFeedBoxContainerType(containerType) {
			continue
		}
		box := baseFeedBoxView{
			ContainerID:   container.ContainerID,
			ContainerType: containerType,
			ContainerName: containerName,
			Slots:         []baseFeedBoxSlot{},
		}
		for _, slot := range container.Slots {
			itemID := strings.TrimSpace(slot.ItemID)
			if itemID == "" || slot.Count <= 0 {
				continue
			}
			view := baseFeedBoxSlot{
				Slot:       slot.Slot,
				ItemID:     itemID,
				ItemName:   pallocalize.ItemName(itemID),
				ItemIcon:   pallocalize.ItemIcon(itemID),
				Count:      slot.Count,
				Durability: slot.Durability,
			}
			box.Slots = append(box.Slots, view)
			summary.OccupiedSlots++
			summary.TotalItems += slot.Count

			key := normalizeQuery(itemID)
			total := itemTotals[key]
			if total == nil {
				total = &baseFeedItemSummary{ItemID: itemID, ItemName: view.ItemName, ItemIcon: view.ItemIcon}
				itemTotals[key] = total
				itemBoxes[key] = make(map[string]struct{})
			}
			total.Count += slot.Count
			itemBoxes[key][normalizeQuery(container.ContainerID)] = struct{}{}
		}
		sort.Slice(box.Slots, func(i, j int) bool { return box.Slots[i].Slot < box.Slots[j].Slot })
		if len(box.Slots) == 0 {
			summary.EmptyBoxCount++
		}
		boxes = append(boxes, box)
	}

	sort.Slice(boxes, func(i, j int) bool {
		left := strings.ToLower(boxes[i].ContainerName + "\x00" + boxes[i].ContainerID)
		right := strings.ToLower(boxes[j].ContainerName + "\x00" + boxes[j].ContainerID)
		return left < right
	})

	items := make([]baseFeedItemSummary, 0, len(itemTotals))
	for key, total := range itemTotals {
		total.BoxCount = len(itemBoxes[key])
		items = append(items, *total)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return strings.ToLower(items[i].ItemName+items[i].ItemID) < strings.ToLower(items[j].ItemName+items[j].ItemID)
	})

	summary.BoxCount = len(boxes)
	summary.ItemTypes = len(items)
	return boxes, items, summary
}

func isFeedBoxContainerType(containerType string) bool {
	normalized := normalizeQuery(containerType)
	return normalized == "palfoodbox" || normalized == "coolerpalfoodbox" || strings.Contains(normalized, "palfoodbox")
}
