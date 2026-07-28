package pallocalize

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
)

//go:embed catalog.zh-CN.json
var catalogJSON []byte

//go:embed technologies.zh-CN.json
var technologyCatalogJSON []byte

type catalog struct {
	Pals      map[string]string `json:"pals"`
	Items     map[string]string `json:"items"`
	ItemIcons map[string]string `json:"item_icons"`
	Passives  map[string]string `json:"passives"`
}

type normalizedCatalog struct {
	pals         map[string]string
	items        map[string]string
	itemIcons    map[string]string
	passives     map[string]string
	technologies map[string]string
	itemList     []ItemEntry
	palList      []PalEntry
	passiveList  []PassiveEntry
	techList     []TechnologyEntry
}

type ItemEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
}

type PalEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PassiveEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TechnologyEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Level    int    `json:"level"`
	Category string `json:"category"`
	Boss     bool   `json:"boss"`
	IconURL  string `json:"icon_url,omitempty"`
}

var names = loadCatalog()

func loadCatalog() normalizedCatalog {
	var source catalog
	if err := json.Unmarshal(catalogJSON, &source); err != nil {
		panic("decode embedded Palworld localization catalog: " + err.Error())
	}
	itemList := make([]ItemEntry, 0, len(source.Items))
	for id, name := range source.Items {
		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		if id != "" && name != "" {
			itemList = append(itemList, ItemEntry{ID: id, Name: name, Icon: strings.TrimSpace(source.ItemIcons[id])})
		}
	}
	palList := make([]PalEntry, 0, len(source.Pals))
	for id, name := range source.Pals {
		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		if id != "" && name != "" {
			palList = append(palList, PalEntry{ID: id, Name: name})
		}
	}
	passiveList := make([]PassiveEntry, 0, len(source.Passives))
	for id, name := range source.Passives {
		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		if id != "" && name != "" {
			passiveList = append(passiveList, PassiveEntry{ID: id, Name: name})
		}
	}
	var techList []TechnologyEntry
	if err := json.Unmarshal(technologyCatalogJSON, &techList); err != nil {
		panic("decode embedded Palworld technology catalog: " + err.Error())
	}
	techList = normalizeTechnologyEntries(techList)
	technologyNames := make(map[string]string, len(techList))
	for _, entry := range techList {
		technologyNames[normalize(entry.ID)] = entry.Name
	}
	sort.Slice(itemList, func(i, j int) bool {
		return strings.ToLower(itemList[i].ID) < strings.ToLower(itemList[j].ID)
	})
	sort.Slice(palList, func(i, j int) bool {
		return strings.ToLower(palList[i].ID) < strings.ToLower(palList[j].ID)
	})
	sort.Slice(passiveList, func(i, j int) bool {
		return strings.ToLower(passiveList[i].ID) < strings.ToLower(passiveList[j].ID)
	})
	return normalizedCatalog{
		pals:         normalizeKeys(source.Pals),
		items:        normalizeKeys(source.Items),
		itemIcons:    normalizeKeys(source.ItemIcons),
		passives:     normalizeKeys(source.Passives),
		technologies: technologyNames,
		itemList:     itemList,
		palList:      palList,
		passiveList:  passiveList,
		techList:     techList,
	}
}

func normalizeTechnologyEntries(entries []TechnologyEntry) []TechnologyEntry {
	out := make([]TechnologyEntry, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		entry.ID = strings.TrimSpace(entry.ID)
		entry.Name = strings.TrimSpace(entry.Name)
		entry.Category = strings.TrimSpace(entry.Category)
		entry.IconURL = strings.TrimSpace(entry.IconURL)
		key := normalize(entry.ID)
		if entry.ID == "" || entry.Name == "" || entry.Level < 0 || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level < out[j].Level
		}
		return strings.ToLower(out[i].ID) < strings.ToLower(out[j].ID)
	})
	return out
}

func normalizeKeys(source map[string]string) map[string]string {
	out := make(map[string]string, len(source))
	for key, value := range source {
		key = normalize(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}

func lookup(values map[string]string, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if translated := values[normalize(id)]; translated != "" {
		return translated
	}
	return id
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func PalName(characterID string) string {
	return lookup(names.pals, characterID)
}

func ItemName(itemID string) string {
	return lookup(names.items, itemID)
}

func ItemIcon(itemID string) string {
	return strings.TrimSpace(names.itemIcons[normalize(itemID)])
}

func ContainerName(objectID string) string {
	trimmed := strings.TrimSpace(objectID)
	if trimmed == "" {
		return ""
	}
	if translated := strings.TrimSpace(names.technologies[normalize(trimmed)]); translated != "" {
		return translated
	}
	switch normalize(trimmed) {
	case "itemchest":
		return "木箱"
	case "itemchest_02":
		return "金属箱"
	case "itemchest_03":
		return "精炼金属箱"
	case "itemchest_04":
		return "高等文明箱子"
	case "palfoodbox":
		return "饲料箱"
	case "coolerbox":
		return "保冷箱"
	case "coolerpalfoodbox":
		return "低温保鲜饲料箱"
	case "refrigerator":
		return "冰箱"
	}
	localized := MapObjectName(trimmed)
	if localized != trimmed {
		return localized
	}
	return trimmed
}

func SearchItems(query string, limit int) []ItemEntry {
	if limit <= 0 {
		limit = 100
	}
	if limit > 5000 {
		limit = 5000
	}
	query = normalize(query)
	results := make([]ItemEntry, 0, min(limit, len(names.itemList)))
	for _, item := range names.itemList {
		if query != "" && !strings.Contains(normalize(item.ID), query) && !strings.Contains(normalize(item.Name), query) {
			continue
		}
		results = append(results, item)
		if len(results) == limit {
			break
		}
	}
	return results
}

func SearchPals(query string, limit int) []PalEntry {
	limit = normalizeSearchLimit(limit)
	query = normalize(query)
	results := make([]PalEntry, 0, min(limit, len(names.palList)))
	for _, pal := range names.palList {
		if query != "" && !strings.Contains(normalize(pal.ID), query) && !strings.Contains(normalize(pal.Name), query) {
			continue
		}
		results = append(results, pal)
		if len(results) == limit {
			break
		}
	}
	return results
}

func SearchPassives(query string, limit int) []PassiveEntry {
	limit = normalizeSearchLimit(limit)
	query = normalize(query)
	results := make([]PassiveEntry, 0, min(limit, len(names.passiveList)))
	for _, passive := range names.passiveList {
		if query != "" && !strings.Contains(normalize(passive.ID), query) && !strings.Contains(normalize(passive.Name), query) {
			continue
		}
		results = append(results, passive)
		if len(results) == limit {
			break
		}
	}
	return results
}

func SearchTechnologies(query string, limit int) []TechnologyEntry {
	limit = normalizeSearchLimit(limit)
	query = normalize(query)
	results := make([]TechnologyEntry, 0, min(limit, len(names.techList)))
	for _, technology := range names.techList {
		if query != "" &&
			!strings.Contains(normalize(technology.ID), query) &&
			!strings.Contains(normalize(technology.Name), query) &&
			!strings.Contains(normalize(technology.Category), query) {
			continue
		}
		results = append(results, technology)
		if len(results) == limit {
			break
		}
	}
	return results
}

func normalizeSearchLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 5000 {
		return 5000
	}
	return limit
}

func PassiveName(passiveID string) string {
	return lookup(names.passives, passiveID)
}

func GuildName(name string) string {
	trimmed := strings.TrimSpace(name)
	switch normalize(trimmed) {
	case "unnamed guild":
		return "未命名公会"
	case "":
		return ""
	default:
		return trimmed
	}
}

func BaseName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "新規生成拠点テンプレート名") {
		return "未命名据点"
	}
	if strings.HasPrefix(trimmed, "Base ") {
		return "据点 " + strings.TrimSpace(strings.TrimPrefix(trimmed, "Base "))
	}
	return trimmed
}

func MapObjectName(objectID string) string {
	trimmed := strings.TrimSpace(objectID)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "damagablerock") {
		return "可采集岩石"
	}
	switch normalize(trimmed) {
	case "palboxv2":
		return "帕鲁终端"
	case "workbench":
		return "原始作业台"
	case "treasurebox":
		return "宝箱"
	case "treasurebox_requiredlonghold":
		return "宝箱（需长按）"
	case "commondropitem3d":
		return "掉落物品"
	default:
		return trimmed
	}
}
