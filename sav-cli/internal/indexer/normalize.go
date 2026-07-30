package indexer

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"palpanel/sav-cli/internal/gvas"
)

func Normalize(file *gvas.File, sourcePath string) Index {
	index := EmptyIndex(sourcePath, ParserName)

	world, ok := asMap(getField(file.Properties, "worldSaveData"))
	if !ok {
		index.Warnings = append(index.Warnings, "worldSaveData is missing or has an unexpected shape")
		index.Finalize()
		return index
	}

	memberGuild := normalizeGuilds(&index, world)
	normalizeCharacters(&index, world, memberGuild)
	deduplicatePlayers(&index)
	normalizeContainers(&index, world)
	normalizeBases(&index, world)
	applyGuildNames(&index)
	rebuildMapEntities(&index)
	normalizeMapObjects(&index, world)
	index.Finalize()
	return index
}

func deduplicatePlayers(index *Index) {
	if len(index.Players) < 2 {
		return
	}
	players := make([]Player, 0, len(index.Players))
	aliases := map[string]int{}
	for _, player := range index.Players {
		keys := playerIdentityKeys(player)
		position := -1
		for _, key := range keys {
			if existing, ok := aliases[key]; ok {
				position = existing
				break
			}
		}
		if position < 0 {
			position = len(players)
			players = append(players, player)
		} else {
			players[position] = mergePlayerRecords(players[position], player)
		}
		for _, key := range playerIdentityKeys(players[position]) {
			aliases[key] = position
		}
	}
	index.Players = players
}

func playerIdentityKeys(player Player) []string {
	keys := make([]string, 0, 2)
	for _, value := range []string{player.PlayerUID, player.SteamID} {
		var builder strings.Builder
		for _, character := range strings.ToLower(strings.TrimSpace(value)) {
			if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
				builder.WriteRune(character)
			}
		}
		key := builder.String()
		if strings.HasPrefix(key, "steam") {
			key = strings.TrimPrefix(key, "steam")
		}
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func mergePlayerRecords(current, candidate Player) Player {
	if current.PlayerUID == "" {
		current.PlayerUID = candidate.PlayerUID
	}
	if current.SteamID == "" {
		current.SteamID = candidate.SteamID
	}
	if current.Nickname == "" || current.Nickname == current.PlayerUID {
		current.Nickname = candidate.Nickname
	}
	if candidate.Level > current.Level {
		current.Level = candidate.Level
		current.Location = candidate.Location
	}
	if current.GuildID == "" {
		current.GuildID = candidate.GuildID
	}
	if current.GuildName == "" {
		current.GuildName = candidate.GuildName
	}
	if current.LastOnlineTime == "" {
		current.LastOnlineTime = candidate.LastOnlineTime
	}
	if len(current.InventorySummary) == 0 && len(candidate.InventorySummary) > 0 {
		current.InventorySummary = candidate.InventorySummary
	}
	current.IsOnline = current.IsOnline || candidate.IsOnline
	return current
}

func normalizeGuilds(index *Index, world map[string]any) map[string]Guild {
	memberGuild := map[string]Guild{}
	entries := asList(getField(world, "GroupSaveDataMap"))
	rawDecoded := 0
	for _, entryAny := range entries {
		entry, ok := rawMap(entryAny)
		if !ok {
			continue
		}
		value, _ := asMap(entry["value"])
		groupType := asString(getField(value, "GroupType"))
		if groupType != "EPalGroupType::Guild" {
			continue
		}
		raw, err := rawDataMap(getField(value, "RawData"), func(data []byte) (map[string]any, error) {
			return gvas.DecodeGroupRaw(data, groupType)
		})
		if err != nil {
			index.Warnings = append(index.Warnings, "group RawData decode failed: "+err.Error())
			continue
		}
		if msg := asString(raw["decode_error"]); msg != "" {
			index.Warnings = append(index.Warnings, "group RawData decode failed: "+msg)
			continue
		}
		if len(raw) > 0 {
			rawDecoded++
		}
		gid := firstNonEmpty(asString(raw["group_id"]), asString(entry["key"]))
		if gid == "" {
			continue
		}
		members := []GuildMember{}
		for _, memberAny := range asList(raw["players"]) {
			member, ok := asMap(memberAny)
			if !ok {
				continue
			}
			info, _ := asMap(member["player_info"])
			uid := asString(member["player_uid"])
			members = append(members, GuildMember{
				PlayerUID:      uid,
				Nickname:       asString(info["player_name"]),
				LastOnlineTime: formatUnixLike(asInt64(info["last_online_real_time"])),
			})
		}
		baseIDs := []string{}
		for _, id := range asList(raw["base_ids"]) {
			baseIDs = append(baseIDs, asString(id))
		}
		guild := Guild{
			ID:             gid,
			Name:           firstNonEmpty(asString(raw["guild_name"]), asString(raw["group_name"]), gid),
			OwnerPlayerUID: asString(raw["admin_player_uid"]),
			Members:        members,
			BaseIDs:        baseIDs,
			Raw:            raw,
		}
		index.Guilds = append(index.Guilds, guild)
		for _, member := range members {
			memberGuild[member.PlayerUID] = guild
		}
	}
	return memberGuild
}

func normalizeCharacters(index *Index, world map[string]any, memberGuild map[string]Guild) {
	entries := asList(getField(world, "CharacterSaveParameterMap"))
	guildByID := map[string]Guild{}
	for _, guild := range index.Guilds {
		guildByID[guild.ID] = guild
	}
	rawDecoded := 0
	saveParams := 0
	for _, entryAny := range entries {
		entry, ok := rawMap(entryAny)
		if !ok {
			continue
		}
		key, _ := asMap(entry["key"])
		value, _ := asMap(entry["value"])
		raw, err := rawDataMap(getField(value, "RawData"), gvas.DecodeCharacterRaw)
		if err != nil {
			index.Warnings = append(index.Warnings, "character RawData decode failed: "+err.Error())
			continue
		}
		if msg := asString(raw["decode_error"]); msg != "" {
			index.Warnings = append(index.Warnings, "character RawData decode failed: "+msg)
			continue
		}
		if len(raw) > 0 {
			rawDecoded++
		}
		object, _ := asMap(raw["object"])
		saveParam, _ := asMap(getField(object, "SaveParameter"))
		if len(saveParam) == 0 {
			continue
		}
		saveParams++
		playerUID := firstNonEmpty(asString(getField(key, "PlayerUId")), asString(getField(key, "PlayerUID")))
		instanceID := firstNonEmpty(asString(getField(key, "InstanceId")), asString(getField(key, "InstanceID")))
		if instanceID == "" {
			instanceID = firstNonEmpty(playerUID, fmt.Sprintf("character-%d", len(index.Pals)+len(index.Players)+1))
		}
		location := locationFromAny(firstNonEmptyAny(getField(saveParam, "Transform"), getField(saveParam, "Location"), getField(saveParam, "LastJumpedLocation")))
		groupID := asString(raw["group_id"])
		if asBool(getField(saveParam, "IsPlayer")) {
			guild := memberGuild[playerUID]
			if guild.ID == "" {
				guild = guildByID[groupID]
			}
			player := Player{
				PlayerUID:        playerUID,
				SteamID:          asString(firstNonEmptyAny(getField(saveParam, "PlatformUserId"), getField(saveParam, "SteamId"))),
				Nickname:         firstNonEmpty(asString(getField(saveParam, "NickName")), playerUID),
				Level:            asInt(getField(saveParam, "Level")),
				GuildID:          guild.ID,
				GuildName:        guild.Name,
				IsOnline:         false,
				LastOnlineTime:   "",
				Location:         location,
				InventorySummary: map[string]any{},
				Raw:              map[string]any{"key": key, "save_parameter": saveParam},
			}
			index.Players = append(index.Players, player)
			continue
		}
		ownerUID := asString(getField(saveParam, "OwnerPlayerUId"))
		slot := firstNonEmptyAny(getField(saveParam, "SlotID"), getField(saveParam, "SlotId"), getField(saveParam, "EquipItemContainerId"))
		containerID := containerIDFromAny(slot)
		if ownerUID == "" && (groupID == "" || isZeroGUID(groupID)) && containerID == "" {
			continue
		}
		guild := memberGuild[ownerUID]
		if guild.ID == "" {
			guild = guildByID[groupID]
		}
		gender := "male"
		if strings.Contains(strings.ToLower(asString(getField(saveParam, "Gender"))), "female") {
			gender = "female"
		}
		health, sanity, fullStomach, isSick, status, workSuitability := palRuntimeState(saveParam, "palbox")
		pal := Pal{
			InstanceID:      instanceID,
			CharacterID:     asString(firstNonEmptyAny(getField(saveParam, "CharacterID"), getField(saveParam, "CharacterId"))),
			Nickname:        asString(getField(saveParam, "NickName")),
			Level:           asIntDefault(getField(saveParam, "Level"), 1),
			OwnerPlayerUID:  ownerUID,
			OldOwnerUIDs:    stringSlice(firstNonEmptyAny(getField(saveParam, "OldOwnerPlayerUIds"), getField(saveParam, "OldOwnerPlayerUIDs"))),
			GuildID:         guild.ID,
			ContainerID:     containerID,
			SlotIndex:       asInt(firstNonEmptyAny(getField(slot, "SlotIndex"), getField(saveParam, "SlotIndex"))),
			LocationType:    "palbox",
			Location:        location,
			Gender:          gender,
			Rank:            asIntDefault(getField(saveParam, "Rank"), 1),
			IVHP:            asInt(getField(saveParam, "Talent_HP")),
			IVAttack:        asInt(firstNonEmptyAny(getField(saveParam, "Talent_Shot"), getField(saveParam, "Talent_Attack"))),
			IVDefense:       asInt(getField(saveParam, "Talent_Defense")),
			Skills:          stringSlice(firstNonEmptyAny(getField(saveParam, "MasteredWaza"), getField(saveParam, "SkillList"))),
			EquippedSkills:  stringSlice(getField(saveParam, "EquipWaza")),
			Passives:        stringSlice(getField(saveParam, "PassiveSkillList")),
			WorkSuitability: workSuitability,
			Health:          health,
			Sanity:          sanity,
			FullStomach:     fullStomach,
			IsSick:          isSick,
			OnExpedition:    asString(getField(saveParam, "MapObjectConcreteInstanceIdAssignedToExpedition")) != "",
			Status:          status,
			Raw:             map[string]any{"key": key, "save_parameter": saveParam},
		}
		index.Pals = append(index.Pals, pal)
	}
	_, _ = rawDecoded, saveParams
}

var workSuitabilityNames = map[string]string{
	"BaseCampBattle":      "BaseCampBattle",
	"EmitFlame":           "Kindling",
	"Watering":            "Watering",
	"Seeding":             "Planting",
	"GenerateElectricity": "Generating",
	"Handcraft":           "Handiwork",
	"Collection":          "Gathering",
	"Deforest":            "Lumbering",
	"Mining":              "Mining",
	"OilExtraction":       "OilExtraction",
	"ProductMedicine":     "Medicine",
	"Cool":                "Cooling",
	"Transport":           "Transport",
	"MonsterFarm":         "Farming",
	"Anyone":              "Anyone",
}

func palRuntimeState(saveParam any, locationType string) (*int64, *float64, *float64, bool, string, []WorkSuitability) {
	var health *int64
	if raw, found := fieldValue(saveParam, "Hp", "HP"); found {
		value := asInt64(raw)
		health = &value
	}
	var sanity *float64
	if raw, found := fieldValue(saveParam, "SanityValue"); found {
		value := asFloat(raw)
		sanity = &value
	}
	var fullStomach *float64
	if raw, found := fieldValue(saveParam, "FullStomach"); found {
		value := asFloat(raw)
		fullStomach = &value
	}

	_, physicalHealth := fieldValue(saveParam, "PhysicalHealth")
	_, workerSick := fieldValue(saveParam, "WorkerSick")
	reviveValue, revivePresent := fieldValue(saveParam, "PalReviveTimer")
	reviveActive := revivePresent && asFloat(reviveValue) > 0
	isSick := physicalHealth || workerSick || reviveActive
	status := "Healthy"
	if (health != nil && *health <= 0) || reviveActive {
		status = "Dead"
	} else if physicalHealth || workerSick {
		status = "Injured"
	} else if strings.EqualFold(strings.TrimSpace(locationType), "base") {
		status = "Working"
	}
	return health, sanity, fullStomach, isSick, status, palWorkSuitability(saveParam)
}

func palWorkSuitability(saveParam any) []WorkSuitability {
	values := asList(getField(saveParam, "GotWorkSuitabilityAddRankList"))
	out := make([]WorkSuitability, 0, len(values))
	seen := map[string]int{}
	for _, value := range values {
		rawName := strings.TrimPrefix(asString(getField(value, "WorkSuitability")), "EPalWorkSuitability::")
		name := workSuitabilityNames[rawName]
		if name == "" {
			continue
		}
		level := asInt(getField(value, "Rank"))
		if level <= 0 {
			continue
		}
		if position, found := seen[name]; found {
			if level > out[position].Level {
				out[position].Level = level
			}
			continue
		}
		seen[name] = len(out)
		out = append(out, WorkSuitability{Type: name, Level: level})
	}
	return out
}

func fieldValue(data any, keys ...string) (any, bool) {
	values, ok := asMap(data)
	if !ok {
		return nil, false
	}
	for _, key := range keys {
		value, found := values[key]
		if found {
			return unwrap(value), true
		}
	}
	return nil, false
}

func normalizeBases(index *Index, world map[string]any) {
	characterContainers := characterContainerMembers(index, world)
	palsByInstance := map[string]Pal{}
	for _, pal := range index.Pals {
		palsByInstance[pal.InstanceID] = pal
	}
	entries := asList(getField(world, "BaseCampSaveData"))
	rawDecoded := 0
	for i, entryAny := range entries {
		entry, ok := rawMap(entryAny)
		if !ok {
			continue
		}
		value, _ := asMap(entry["value"])
		raw, err := rawDataMap(getField(value, "RawData"), gvas.DecodeBaseCampRaw)
		if err != nil {
			index.Warnings = append(index.Warnings, "base RawData decode failed: "+err.Error())
			continue
		}
		if msg := asString(raw["decode_error"]); msg != "" {
			index.Warnings = append(index.Warnings, "base RawData decode failed: "+msg)
			continue
		}
		if len(raw) > 0 {
			rawDecoded++
		}
		if len(raw) == 0 {
			continue
		}
		base := Base{
			ID:              firstNonEmpty(asString(raw["id"]), asString(entry["key"]), fmt.Sprintf("base-%d", i+1)),
			Name:            firstNonEmpty(asString(raw["name"]), fmt.Sprintf("Base %d", i+1)),
			GuildID:         asString(raw["group_id_belong_to"]),
			Location:        locationFromAny(raw["transform"]),
			StructuresCount: 0,
			Workers:         []Worker{},
			Containers:      []string{},
			Status:          "Safe",
			Raw:             raw,
		}
		workerRaw := bytesFromAny(getField(getField(value, "WorkerDirector"), "RawData"))
		if len(workerRaw) > 0 {
			containerID, err := workerContainerID(workerRaw)
			if err != nil {
				index.Warnings = append(index.Warnings, "base worker director decode failed: "+err.Error())
			} else {
				for _, instanceID := range characterContainers[containerID] {
					pal, ok := palsByInstance[instanceID]
					if !ok {
						continue
					}
					base.Workers = append(base.Workers, Worker{
						InstanceID: pal.InstanceID, CharacterID: pal.CharacterID, Nickname: pal.Nickname, Level: pal.Level,
					})
					for palPosition := range index.Pals {
						if index.Pals[palPosition].InstanceID == instanceID {
							index.Pals[palPosition].LocationType = "base"
							if index.Pals[palPosition].Status == "Healthy" {
								index.Pals[palPosition].Status = "Working"
							}
							break
						}
					}
				}
			}
		}
		index.Bases = append(index.Bases, base)
	}
	_ = rawDecoded
}

func normalizeContainers(index *Index, world map[string]any) {
	for _, entryAny := range asList(getField(world, "ItemContainerSaveData")) {
		entry, ok := rawMap(entryAny)
		if !ok {
			continue
		}
		key, _ := asMap(entry["key"])
		value, _ := asMap(entry["value"])
		containerID := containerIDFromAny(getField(key, "ID"))
		if containerID == "" || isZeroGUID(containerID) {
			continue
		}
		container := Container{ContainerID: containerID, Slots: []Slot{}}
		belong, _ := asMap(getField(value, "BelongInfo"))
		for _, owner := range []struct {
			field    string
			typeName string
		}{
			{field: "BaseCampId", typeName: "base"},
			{field: "BaseCampID", typeName: "base"},
			{field: "GroupId", typeName: "guild"},
			{field: "GroupID", typeName: "guild"},
			{field: "OwnerPlayerUId", typeName: "player"},
		} {
			ownerID := asString(getField(belong, owner.field))
			if ownerID != "" && !isZeroGUID(ownerID) {
				container.OwnerType, container.OwnerID = owner.typeName, ownerID
				break
			}
		}
		for _, slotAny := range asList(getField(value, "Slots")) {
			slotMap, ok := asMap(slotAny)
			if !ok {
				continue
			}
			itemID := asString(getField(getField(slotMap, "ItemId"), "StaticId"))
			count := asInt(getField(slotMap, "StackCount"))
			slotIndex := asInt(getField(slotMap, "SlotIndex"))
			if raw := bytesFromAny(getField(slotMap, "RawData")); len(raw) > 0 {
				decoded, err := gvas.DecodeItemContainerSlotRaw(raw)
				if err != nil {
					index.Warnings = append(index.Warnings, "item container slot RawData decode failed: "+err.Error())
					continue
				}
				item, _ := asMap(decoded["item"])
				itemID = asString(item["static_id"])
				count = asInt(decoded["count"])
				slotIndex = asInt(decoded["slot_index"])
			}
			if itemID == "" && count == 0 {
				continue
			}
			slot := Slot{Slot: slotIndex, ItemID: itemID, Count: count}
			if durabilityValue := getField(slotMap, "Durability"); durabilityValue != nil {
				durability := asFloat(durabilityValue)
				slot.Durability = &durability
			}
			container.Slots = append(container.Slots, slot)
		}
		index.Containers = append(index.Containers, container)
	}
}

func characterContainerMembers(index *Index, world map[string]any) map[string][]string {
	out := map[string][]string{}
	for _, entryAny := range asList(getField(world, "CharacterContainerSaveData")) {
		entry, ok := rawMap(entryAny)
		if !ok {
			continue
		}
		key, _ := asMap(entry["key"])
		value, _ := asMap(entry["value"])
		containerID := containerIDFromAny(getField(key, "ID"))
		if containerID == "" || isZeroGUID(containerID) {
			continue
		}
		for _, slotAny := range asList(getField(value, "Slots")) {
			instanceID := asString(getField(getField(slotAny, "IndividualId"), "InstanceId"))
			if raw := bytesFromAny(getField(slotAny, "RawData")); len(raw) > 0 {
				decoded, err := gvas.DecodeCharacterContainerSlotRaw(raw)
				if err != nil {
					index.Warnings = append(index.Warnings, "character container slot RawData decode failed: "+err.Error())
					continue
				}
				instanceID = asString(decoded["instance_id"])
			}
			if instanceID != "" && !isZeroGUID(instanceID) {
				out[containerID] = appendUnique(out[containerID], instanceID)
			}
		}
	}
	return out
}

func workerContainerID(raw []byte) (string, error) {
	reader := gvas.NewReader(raw)
	if _, err := reader.GUID(); err != nil {
		return "", err
	}
	if _, err := reader.Transform(); err != nil {
		return "", err
	}
	if _, err := reader.Byte(); err != nil {
		return "", err
	}
	if _, err := reader.Byte(); err != nil {
		return "", err
	}
	return reader.GUID()
}

func normalizeMapObjects(index *Index, world map[string]any) {
	containerIndex := map[string]int{}
	for i := range index.Containers {
		containerIndex[index.Containers[i].ContainerID] = i
	}
	baseIndexByID := map[string]int{}
	for i := range index.Bases {
		baseIndexByID[index.Bases[i].ID] = i
	}
	for _, objectAny := range asList(getField(world, "MapObjectSaveData")) {
		object, ok := asMap(objectAny)
		if !ok {
			continue
		}
		label := asString(getField(object, "MapObjectId"))
		model, _ := asMap(getField(object, "Model"))
		raw, err := rawDataMap(getField(model, "RawData"), gvas.DecodeMapObjectModelRaw)
		if err != nil {
			index.Warnings = append(index.Warnings, "map object model RawData decode failed: "+err.Error())
			continue
		}
		id := firstNonEmpty(asString(raw["instance_id"]), asString(getField(object, "MapObjectInstanceId")))
		location := locationFromAny(firstNonEmptyAny(raw["initial_transform_cache"], getField(object, "WorldLocation")))
		if id == "" {
			continue
		}
		if location.X != 0 || location.Y != 0 || location.Z != 0 {
			index.MapEntities = append(index.MapEntities, MapEntity{Type: "map_object", ID: id, Label: label, Location: location})
		}
		baseIndex := -1
		if direct, found := baseIndexByID[asString(raw["base_camp_id_belong_to"])]; found {
			baseIndex = direct
		} else {
			baseIndex = nearestBase(index.Bases, location)
		}
		if baseIndex >= 0 && !strings.HasPrefix(label, "PickupItem_") {
			index.Bases[baseIndex].StructuresCount++
		}
		concrete := getField(object, "ConcreteModel")
		for _, moduleAny := range asList(getField(concrete, "ModuleMap")) {
			module, ok := rawMap(moduleAny)
			if !ok {
				continue
			}
			moduleType := asString(module["key"])
			moduleValue, _ := asMap(module["value"])
			raw := bytesFromAny(getField(moduleValue, "RawData"))
			if len(raw) < 16 {
				continue
			}
			containerID, err := gvas.NewReader(raw).GUID()
			if err != nil || isZeroGUID(containerID) {
				continue
			}
			if moduleType == "EPalMapObjectConcreteModelModuleType::CharacterContainer" {
				locationType := mapObjectPalLocationType(label)
				if locationType != "" {
					for palPosition := range index.Pals {
						if canonicalSaveID(index.Pals[palPosition].ContainerID) == canonicalSaveID(containerID) {
							index.Pals[palPosition].LocationType = locationType
						}
					}
				}
				continue
			}
			if moduleType != "EPalMapObjectConcreteModelModuleType::ItemContainer" {
				continue
			}
			if containerPosition, found := containerIndex[containerID]; found {
				index.Containers[containerPosition].OwnerType = "map_object"
				index.Containers[containerPosition].OwnerID = id
			}
			if baseIndex >= 0 {
				index.Bases[baseIndex].Containers = appendUnique(index.Bases[baseIndex].Containers, containerID)
			}
		}
	}
}

func mapObjectPalLocationType(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "displaycharacter":
		return "viewing_cage"
	case "dimensionpalstorage":
		return "dimensional_pal_storage"
	case "globalpalstorage":
		return "global_pal_storage"
	default:
		return ""
	}
}

func nearestBase(bases []Base, location Coordinates) int {
	bestIndex := -1
	bestDistance := math.MaxFloat64
	for i, base := range bases {
		raw, _ := base.Raw.(map[string]any)
		radius := asFloat(raw["area_range"])
		if radius <= 0 {
			continue
		}
		dx, dy := base.Location.X-location.X, base.Location.Y-location.Y
		distance := dx*dx + dy*dy
		if distance <= radius*radius && distance < bestDistance {
			bestIndex, bestDistance = i, distance
		}
	}
	return bestIndex
}

func applyGuildNames(index *Index) {
	guildNames := map[string]string{}
	for _, guild := range index.Guilds {
		guildNames[guild.ID] = guild.Name
	}
	for i := range index.Bases {
		index.Bases[i].GuildName = guildNames[index.Bases[i].GuildID]
	}
	for i := range index.Players {
		if index.Players[i].GuildName == "" {
			index.Players[i].GuildName = guildNames[index.Players[i].GuildID]
		}
	}
}

func rebuildMapEntities(index *Index) {
	index.MapEntities = []MapEntity{}
	for _, player := range index.Players {
		index.MapEntities = append(index.MapEntities, MapEntity{Type: "player", ID: player.PlayerUID, Label: player.Nickname, Location: player.Location})
	}
	for _, base := range index.Bases {
		index.MapEntities = append(index.MapEntities, MapEntity{Type: "base", ID: base.ID, Label: base.Name, Location: base.Location})
	}
	for _, pal := range index.Pals {
		if pal.Location.X != 0 || pal.Location.Y != 0 || pal.Location.Z != 0 {
			label := firstNonEmpty(pal.Nickname, pal.CharacterID)
			index.MapEntities = append(index.MapEntities, MapEntity{Type: "pal", ID: pal.InstanceID, Label: label, Location: pal.Location})
		}
	}
}

func getField(data any, key string) any {
	m, ok := asMap(data)
	if !ok {
		return nil
	}
	return unwrap(m[key])
}

func rawDataMap(value any, decode func([]byte) (map[string]any, error)) (map[string]any, error) {
	if raw, ok := asMap(value); ok {
		if _, encodedBytes := raw["values"]; !encodedBytes {
			return raw, nil
		}
	}
	rawBytes := bytesFromAny(value)
	if len(rawBytes) == 0 {
		return nil, nil
	}
	return decode(rawBytes)
}

func unwrap(value any) any {
	for i := 0; i < 12; i++ {
		m, ok := value.(map[string]any)
		if !ok {
			return value
		}
		next, ok := m["value"]
		if !ok {
			return value
		}
		value = next
	}
	return value
}

func asMap(value any) (map[string]any, bool) {
	value = unwrap(value)
	m, ok := value.(map[string]any)
	return m, ok
}

func rawMap(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

func asList(value any) []any {
	value = unwrap(value)
	switch v := value.(type) {
	case []any:
		return v
	case map[string]any:
		if values, ok := v["values"].([]any); ok {
			return values
		}
		return nil
	default:
		return nil
	}
}

func asString(value any) string {
	value = unwrap(value)
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func asInt(value any) int {
	return asIntDefault(value, 0)
}

func asIntDefault(value any, fallback int) int {
	value = unwrap(value)
	switch v := value.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}

func asInt64(value any) int64 {
	value = unwrap(value)
	switch v := value.(type) {
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return n
		}
	}
	return 0
}

func asFloat(value any) float64 {
	value = unwrap(value)
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint8:
		return float64(v)
	case uint16:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case string:
		n, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return n
		}
	}
	return 0
}

func asBool(value any) bool {
	value = unwrap(value)
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	case int:
		return v != 0
	case int8:
		return v != 0
	case uint8:
		return v != 0
	default:
		return false
	}
}

func locationFromAny(value any) Coordinates {
	value = unwrap(value)
	m, ok := value.(map[string]any)
	if !ok {
		return Coordinates{}
	}
	if t, ok := m["translation"]; ok {
		m, _ = asMap(t)
	} else if t, ok := m["Translation"]; ok {
		m, _ = asMap(t)
	}
	return Coordinates{X: asFloat(m["x"]), Y: asFloat(m["y"]), Z: asFloat(m["z"])}
}

func stringSlice(value any) []string {
	out := []string{}
	for _, item := range asList(value) {
		s := asString(item)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func bytesFromAny(value any) []byte {
	value = unwrap(value)
	switch v := value.(type) {
	case []byte:
		return v
	case map[string]any:
		return bytesFromAny(v["values"])
	case []int:
		out := make([]byte, 0, len(v))
		for _, item := range v {
			out = append(out, byte(item))
		}
		return out
	case []any:
		out := make([]byte, 0, len(v))
		for _, item := range v {
			out = append(out, byte(asInt(item)))
		}
		return out
	default:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyAny(values ...any) any {
	for _, value := range values {
		if asString(value) != "" {
			return value
		}
	}
	return nil
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func isZeroGUID(value string) bool {
	return strings.TrimSpace(value) == "00000000-0000-0000-0000-000000000000"
}

func containerIDFromAny(value any) string {
	m, ok := asMap(value)
	if !ok {
		return asString(value)
	}
	if id := asString(getField(m, "ID")); id != "" {
		return id
	}
	if id := containerIDFromAny(getField(m, "ContainerId")); id != "" {
		return id
	}
	return asString(value)
}

func formatUnixLike(value int64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}
