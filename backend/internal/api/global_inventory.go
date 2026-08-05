package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/inventoryscope"
	"palpanel/internal/pallocalize"
	"palpanel/internal/saveindex"
)

func init() {
	patchFeatures = append(patchFeatures,
		"global-inventory-browser",
		"global-inventory-trusted-scope",
		"inventory-untrusted-diagnostics",
		"unattended-inventory-trusted-scope",
	)
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
	Trusted       bool   `json:"trusted"`
	ScopeReason   string `json:"scope_reason,omitempty"`
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
	SuppressedContainers int   `json:"suppressed_containers"`
	SuppressedLocations  int   `json:"suppressed_locations"`
	SuppressedItemTypes  int   `json:"suppressed_item_types"`
	SuppressedTotalCount int64 `json:"suppressed_total_count"`
	TrustedOnly          bool  `json:"trusted_only"`
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

type globalInventoryBuildStats struct {
	SuppressedContainers map[string]struct{}
	SuppressedItems      map[string]struct{}
	SuppressedLocations  int
	SuppressedTotalCount int64
}

func newGlobalInventoryBuildStats() globalInventoryBuildStats {
	return globalInventoryBuildStats{
		SuppressedContainers: map[string]struct{}{},
		SuppressedItems:      map[string]struct{}{},
	}
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
	if ownerType != "all" && ownerType != "base" && ownerType != "player" && ownerType != "guild" && ownerType != "unknown" {
		fail(c, http.StatusBadRequest, "inventory_owner_type_invalid", "owner_type must be all, base, player, guild, or unknown")
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

	items, categories, buildStats := buildGlobalInventory(index, customNames, ownerType)
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
	summary := summarizeGlobalInventory(items, paged, offset, limit, ownerType, buildStats)

	ok(c, gin.H{
		"items":   paged,
		"summary": summary,
		"filters": gin.H{
			"categories":  categories,
			"owner_types": []string{"all", "base", "player", "guild", "unknown"},
		},
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

// buildGlobalInventory treats owner_type=all as all trusted business inventory,
// not all serialized containers. Untrusted containers remain available only via
// owner_type=unknown for diagnostics and are always counted in suppression stats.
func buildGlobalInventory(index saveindex.Index, customNames map[string]string, ownerType string) ([]globalInventoryItem, []string, globalInventoryBuildStats) {
	playerOwners := make(map[string]inventoryOwner)
	for _, player := range index.Players {
		name := strings.TrimSpace(player.Nickname)
		if name == "" {
			name = firstNonEmpty(strings.TrimSpace(player.PlayerUID), strings.TrimSpace(player.SteamID))
		}
		ownerID := firstNonEmpty(strings.TrimSpace(player.PlayerUID), strings.TrimSpace(player.SteamID))
		owner := inventoryOwner{Type: inventoryscope.OwnerPlayer, ID: ownerID, Name: name}
		for _, id := range []string{player.PlayerUID, player.SteamID} {
			if key := inventoryscope.CanonicalID(id); key != "" {
				playerOwners[key] = owner
			}
		}
	}

	baseOwners := make(map[string]inventoryOwner)
	for _, base := range index.Bases {
		name := strings.TrimSpace(customNames[base.ID])
		if name == "" {
			name = pallocalize.BaseName(base.Name)
		}
		owner := inventoryOwner{
			Type:      inventoryscope.OwnerBase,
			ID:        base.ID,
			Name:      name,
			GuildName: pallocalize.GuildName(base.GuildName),
		}
		baseOwners[inventoryscope.CanonicalID(base.ID)] = owner
	}

	guildOwners := make(map[string]inventoryOwner)
	for _, guild := range index.Guilds {
		name := pallocalize.GuildName(guild.Name)
		if strings.TrimSpace(name) == "" {
			name = strings.TrimSpace(guild.ID)
		}
		guildOwners[inventoryscope.CanonicalID(guild.ID)] = inventoryOwner{
			Type: inventoryscope.OwnerGuild,
			ID:   guild.ID,
			Name: name,
		}
	}

	entityByID := make(map[string]saveindex.MapEntity, len(index.MapEntities))
	for _, entity := range index.MapEntities {
		entityByID[normalizeQuery(entity.ID)] = entity
	}

	classifier := inventoryscope.New(index)
	byItem := make(map[string]*globalInventoryItem)
	seenContainers := make(map[string]struct{})
	buildStats := newGlobalInventoryBuildStats()
	for _, container := range index.Containers {
		containerKey := normalizeQuery(container.ContainerID)
		if containerKey == "" {
			continue
		}
		if _, duplicate := seenContainers[containerKey]; duplicate {
			continue
		}
		seenContainers[containerKey] = struct{}{}

		classification := classifier.Resolve(container)
		owner := resolveInventoryOwner(classification, container, playerOwners, baseOwners, guildOwners)
		if !classification.Trusted {
			recordSuppressedInventory(&buildStats, containerKey, container)
		}
		if ownerType == "all" && !classification.Trusted {
			continue
		}
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
				Trusted:       classification.Trusted,
				ScopeReason:   classification.Reason,
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
	return items, categories, buildStats
}

func resolveInventoryOwner(
	classification inventoryscope.Classification,
	container saveindex.Container,
	players map[string]inventoryOwner,
	bases map[string]inventoryOwner,
	guilds map[string]inventoryOwner,
) inventoryOwner {
	if classification.Trusted {
		key := inventoryscope.CanonicalID(classification.ID)
		switch classification.Type {
		case inventoryscope.OwnerPlayer:
			if owner, found := players[key]; found {
				return owner
			}
		case inventoryscope.OwnerBase:
			if owner, found := bases[key]; found {
				return owner
			}
		case inventoryscope.OwnerGuild:
			if owner, found := guilds[key]; found {
				return owner
			}
		}
	}
	return inventoryOwner{
		Type: inventoryscope.OwnerUnknown,
		ID:   firstNonEmpty(strings.TrimSpace(container.OwnerID), strings.TrimSpace(container.ContainerID)),
		Name: "未归属容器（诊断）",
	}
}

func recordSuppressedInventory(stats *globalInventoryBuildStats, containerKey string, container saveindex.Container) {
	if stats == nil {
		return
	}
	hasItems := false
	for _, slot := range container.Slots {
		itemID := strings.TrimSpace(slot.ItemID)
		count := int64(slot.Count)
		if itemID == "" || count <= 0 {
			continue
		}
		hasItems = true
		stats.SuppressedItems[normalizeQuery(itemID)] = struct{}{}
		stats.SuppressedLocations++
		stats.SuppressedTotalCount += count
	}
	if hasItems {
		stats.SuppressedContainers[containerKey] = struct{}{}
	}
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
		if containsAny(query, location.OwnerName, location.GuildName, location.ContainerName, location.ContainerType, location.ContainerID, location.ScopeReason) {
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

func summarizeGlobalInventory(
	all []globalInventoryItem,
	returned []globalInventoryItem,
	offset int,
	limit int,
	ownerType string,
	buildStats globalInventoryBuildStats,
) globalInventorySummary {
	containers := make(map[string]struct{})
	unresolved := make(map[string]struct{})
	var totalCount int64
	locationCount := 0
	for _, item := range all {
		totalCount += item.TotalCount
		for _, location := range item.Locations {
			locationCount++
			containers[normalizeQuery(location.ContainerID)] = struct{}{}
			if !location.Trusted || location.OwnerType == inventoryscope.OwnerUnknown {
				unresolved[normalizeQuery(location.ContainerID)] = struct{}{}
			}
		}
	}
	return globalInventorySummary{
		ItemTypes:            len(all),
		TotalCount:           totalCount,
		ContainerCount:       len(containers),
		LocationCount:        locationCount,
		UnresolvedContainers: max(len(unresolved), len(buildStats.SuppressedContainers)),
		SuppressedContainers: len(buildStats.SuppressedContainers),
		SuppressedLocations:  buildStats.SuppressedLocations,
		SuppressedItemTypes:  len(buildStats.SuppressedItems),
		SuppressedTotalCount: buildStats.SuppressedTotalCount,
		TrustedOnly:          ownerType != inventoryscope.OwnerUnknown,
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
