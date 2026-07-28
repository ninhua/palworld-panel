package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/pallocalize"
	"palpanel/internal/saveindex"
)

func init() {
	patchFeatures = append(patchFeatures, "global-inventory-browser")
}

type globalInventoryLocation struct {
	OwnerType     string `json:"owner_type"`
	OwnerID       string `json:"owner_id"`
	OwnerName     string `json:"owner_name"`
	GuildName     string `json:"guild_name,omitempty"`
	ContainerID   string `json:"container_id"`
	ContainerType string `json:"container_type"`
	ContainerName string `json:"container_name"`
	Slot          int    `json:"slot"`
	Count         int64  `json:"count"`
}

type globalInventoryItem struct {
	ItemID     string                    `json:"item_id"`
	ItemName   string                    `json:"item_name"`
	ItemIcon   string                    `json:"item_icon"`
	Category   string                    `json:"category"`
	TotalCount int64                     `json:"total_count"`
	Locations  []globalInventoryLocation `json:"locations"`
}

type globalInventorySummary struct {
	ItemTypes            int   `json:"item_types"`
	TotalCount           int64 `json:"total_count"`
	ContainerCount       int   `json:"container_count"`
	LocationCount        int   `json:"location_count"`
	UnresolvedContainers int   `json:"unresolved_containers"`
	Returned             int   `json:"returned"`
	Offset               int   `json:"offset"`
	Limit                int   `json:"limit"`
}

type inventoryOwner struct {
	Type      string
	ID        string
	Name      string
	GuildName string
}

func (s Server) listGlobalInventory(c *gin.Context) {
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

	ownerType := strings.ToLower(strings.TrimSpace(c.Query("owner_type")))
	if ownerType == "" {
		ownerType = "all"
	}
	if ownerType != "all" && ownerType != "base" && ownerType != "player" && ownerType != "unknown" {
		fail(c, http.StatusBadRequest, "inventory_owner_type_invalid", "owner_type must be all, base, player, or unknown")
		return
	}
	sortBy := strings.ToLower(strings.TrimSpace(c.Query("sort")))
	if sortBy == "" {
		sortBy = "count_desc"
	}
	if sortBy != "count_desc" && sortBy != "name_asc" && sortBy != "name_desc" {
		fail(c, http.StatusBadRequest, "inventory_sort_invalid", "sort must be count_desc, name_asc, or name_desc")
		return
	}

	items, categories := buildGlobalInventory(index, customNames, ownerType)
	items = filterGlobalInventory(items, c.Query("q"), c.Query("category"))
	sortGlobalInventory(items, sortBy)

	limit, offset := inventoryLimitOffset(c)
	totalItems := len(items)
	if offset > totalItems {
		offset = totalItems
	}
	end := offset + limit
	if end > totalItems {
		end = totalItems
	}
	paged := items[offset:end]
	summary := summarizeGlobalInventory(items, paged, offset, limit)

	ok(c, gin.H{
		"items":      paged,
		"summary":    summary,
		"filters":    gin.H{"categories": categories, "owner_types": []string{"all", "base", "player", "unknown"}},
		"status":     status,
		"source_id":  sourceID,
		"unattended": s.unattendedInventoryView(c, sourceID),
	})
}

func inventoryLimitOffset(c *gin.Context) (int, int) {
	limit := 200
	if value, err := strconv.Atoi(strings.TrimSpace(c.Query("limit"))); err == nil && value > 0 {
		limit = value
	}
	if limit > 500 {
		limit = 500
	}
	offset := 0
	if value, err := strconv.Atoi(strings.TrimSpace(c.Query("offset"))); err == nil && value > 0 {
		offset = value
	}
	return limit, offset
}

func buildGlobalInventory(index saveindex.Index, customNames map[string]string, ownerType string) ([]globalInventoryItem, []string) {
	playerOwners := make(map[string]inventoryOwner)
	for _, player := range index.Players {
		name := strings.TrimSpace(player.Nickname)
		if name == "" {
			name = strings.TrimSpace(player.PlayerUID)
		}
		owner := inventoryOwner{Type: "player", ID: player.PlayerUID, Name: name}
		for _, id := range []string{player.PlayerUID, player.SteamID} {
			if key := normalizeQuery(id); key != "" {
				playerOwners[key] = owner
			}
		}
	}

	baseOwners := make(map[string]inventoryOwner)
	containerOwners := make(map[string]inventoryOwner)
	for _, base := range index.Bases {
		name := strings.TrimSpace(customNames[base.ID])
		if name == "" {
			name = pallocalize.BaseName(base.Name)
		}
		owner := inventoryOwner{
			Type:      "base",
			ID:        base.ID,
			Name:      name,
			GuildName: pallocalize.GuildName(base.GuildName),
		}
		baseOwners[normalizeQuery(base.ID)] = owner
		for _, containerID := range base.Containers {
			if key := normalizeQuery(containerID); key != "" {
				containerOwners[key] = owner
			}
		}
	}

	entityByID := make(map[string]saveindex.MapEntity, len(index.MapEntities))
	for _, entity := range index.MapEntities {
		entityByID[normalizeQuery(entity.ID)] = entity
	}

	byItem := make(map[string]*globalInventoryItem)
	seenContainers := make(map[string]struct{})
	for _, container := range index.Containers {
		containerKey := normalizeQuery(container.ContainerID)
		if containerKey == "" {
			continue
		}
		if _, duplicate := seenContainers[containerKey]; duplicate {
			continue
		}
		seenContainers[containerKey] = struct{}{}

		owner := resolveInventoryOwner(container, playerOwners, baseOwners, containerOwners)
		if ownerType != "all" && owner.Type != ownerType {
			continue
		}
		containerType, containerName := baseStorageContainerIdentity(container, entityByID)
		for _, slot := range container.Slots {
			itemID := strings.TrimSpace(slot.ItemID)
			count := int64(slot.Count)
			if itemID == "" || count <= 0 {
				continue
			}
			key := normalizeQuery(itemID)
			item := byItem[key]
			if item == nil {
				item = &globalInventoryItem{
					ItemID:   itemID,
					ItemName: pallocalize.ItemName(itemID),
					ItemIcon: pallocalize.ItemIcon(itemID),
					Category: inventoryItemCategory(itemID),
				}
				byItem[key] = item
			}
			item.TotalCount += count
			item.Locations = append(item.Locations, globalInventoryLocation{
				OwnerType:     owner.Type,
				OwnerID:       owner.ID,
				OwnerName:     owner.Name,
				GuildName:     owner.GuildName,
				ContainerID:   container.ContainerID,
				ContainerType: containerType,
				ContainerName: containerName,
				Slot:          slot.Slot,
				Count:         count,
			})
		}
	}

	items := make([]globalInventoryItem, 0, len(byItem))
	categorySet := make(map[string]struct{})
	for _, item := range byItem {
		sort.Slice(item.Locations, func(i, j int) bool {
			left, right := item.Locations[i], item.Locations[j]
			if left.OwnerType != right.OwnerType {
				return left.OwnerType < right.OwnerType
			}
			if left.OwnerName != right.OwnerName {
				return strings.ToLower(left.OwnerName) < strings.ToLower(right.OwnerName)
			}
			if left.ContainerID != right.ContainerID {
				return left.ContainerID < right.ContainerID
			}
			return left.Slot < right.Slot
		})
		categorySet[item.Category] = struct{}{}
		items = append(items, *item)
	}
	categories := make([]string, 0, len(categorySet))
	for category := range categorySet {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	return items, categories
}

func resolveInventoryOwner(container saveindex.Container, players, bases, containerBases map[string]inventoryOwner) inventoryOwner {
	if owner, found := containerBases[normalizeQuery(container.ContainerID)]; found {
		return owner
	}
	switch strings.ToLower(strings.TrimSpace(container.OwnerType)) {
	case "player":
		if owner, found := players[normalizeQuery(container.OwnerID)]; found {
			return owner
		}
		return inventoryOwner{Type: "player", ID: container.OwnerID, Name: fallbackInventoryOwnerName(container.OwnerID, "未知玩家")}
	case "base":
		if owner, found := bases[normalizeQuery(container.OwnerID)]; found {
			return owner
		}
		return inventoryOwner{Type: "base", ID: container.OwnerID, Name: fallbackInventoryOwnerName(container.OwnerID, "未知基地")}
	default:
		return inventoryOwner{Type: "unknown", ID: container.OwnerID, Name: "未识别容器"}
	}
}

func fallbackInventoryOwnerName(id, fallback string) string {
	if value := strings.TrimSpace(id); value != "" {
		return value
	}
	return fallback
}

func filterGlobalInventory(items []globalInventoryItem, query, category string) []globalInventoryItem {
	query = normalizeQuery(query)
	category = strings.TrimSpace(category)
	out := make([]globalInventoryItem, 0, len(items))
	for _, item := range items {
		if category != "" && category != "all" && item.Category != category {
			continue
		}
		if query != "" && !globalInventoryItemMatches(item, query) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func globalInventoryItemMatches(item globalInventoryItem, query string) bool {
	if containsAny(query, item.ItemID, item.ItemName, item.Category) {
		return true
	}
	for _, location := range item.Locations {
		if containsAny(query, location.OwnerName, location.GuildName, location.ContainerName, location.ContainerType, location.ContainerID) {
			return true
		}
	}
	return false
}

func sortGlobalInventory(items []globalInventoryItem, sortBy string) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		switch sortBy {
		case "name_asc":
			return strings.ToLower(left.ItemName+left.ItemID) < strings.ToLower(right.ItemName+right.ItemID)
		case "name_desc":
			return strings.ToLower(left.ItemName+left.ItemID) > strings.ToLower(right.ItemName+right.ItemID)
		default:
			if left.TotalCount != right.TotalCount {
				return left.TotalCount > right.TotalCount
			}
			return strings.ToLower(left.ItemName+left.ItemID) < strings.ToLower(right.ItemName+right.ItemID)
		}
	})
}

func summarizeGlobalInventory(all, returned []globalInventoryItem, offset, limit int) globalInventorySummary {
	containers := make(map[string]struct{})
	unresolved := make(map[string]struct{})
	var totalCount int64
	locationCount := 0
	for _, item := range all {
		totalCount += item.TotalCount
		for _, location := range item.Locations {
			locationCount++
			containers[normalizeQuery(location.ContainerID)] = struct{}{}
			if location.OwnerType == "unknown" {
				unresolved[normalizeQuery(location.ContainerID)] = struct{}{}
			}
		}
	}
	return globalInventorySummary{
		ItemTypes:            len(all),
		TotalCount:           totalCount,
		ContainerCount:       len(containers),
		LocationCount:        locationCount,
		UnresolvedContainers: len(unresolved),
		Returned:             len(returned),
		Offset:               offset,
		Limit:                limit,
	}
}

func inventoryItemCategory(itemID string) string {
	value := strings.ToLower(strings.TrimSpace(itemID))
	switch {
	case strings.Contains(value, "blueprint") || strings.Contains(value, "schematic"):
		return "蓝图"
	case strings.Contains(value, "sphere"):
		return "帕鲁球"
	case strings.Contains(value, "ammo") || strings.Contains(value, "bullet") || strings.Contains(value, "arrow") || strings.Contains(value, "missile"):
		return "弹药"
	case strings.Contains(value, "weapon") || strings.Contains(value, "sword") || strings.Contains(value, "spear") || strings.Contains(value, "gun") || strings.Contains(value, "bow"):
		return "武器"
	case strings.Contains(value, "armor") || strings.Contains(value, "helm") || strings.Contains(value, "cloth"):
		return "防具"
	case strings.Contains(value, "accessory") || strings.Contains(value, "ring") || strings.Contains(value, "pendant"):
		return "饰品"
	case strings.Contains(value, "medicine") || strings.Contains(value, "drug") || strings.Contains(value, "remedy"):
		return "药品"
	case strings.Contains(value, "seed"):
		return "种子"
	case strings.Contains(value, "food") || strings.Contains(value, "berry") || strings.Contains(value, "meat") || strings.Contains(value, "bread") || strings.Contains(value, "milk") || strings.Contains(value, "egg"):
		return "食物"
	case strings.Contains(value, "key") || strings.Contains(value, "note") || strings.Contains(value, "journal"):
		return "关键物品"
	case strings.Contains(value, "wood") || strings.Contains(value, "stone") || strings.Contains(value, "fiber") || strings.Contains(value, "ore") || strings.Contains(value, "ingot") || strings.Contains(value, "material") || strings.Contains(value, "parts"):
		return "材料"
	default:
		return "其他"
	}
}
