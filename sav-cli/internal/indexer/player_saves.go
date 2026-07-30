package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"palpanel/sav-cli/internal/gvas"
	"palpanel/sav-cli/internal/sav"
)

type playerInventoryContainerField struct {
	Name string
	Type string
}

type playerInventoryContainerRef struct {
	ID   string
	Type string
}

var playerInventoryContainerFields = []playerInventoryContainerField{
	{Name: "CommonContainerId", Type: "items"},
	{Name: "DropSlotContainerId", Type: "drop_slot"},
	{Name: "EssentialContainerId", Type: "key_items"},
	{Name: "FoodEquipContainerId", Type: "food"},
	{Name: "PlayerEquipArmorContainerId", Type: "armor"},
	{Name: "WeaponLoadOutContainerId", Type: "weapons"},
}

func normalizePlayerSaves(index *Index, playersDir string) {
	if len(index.Players) == 0 {
		return
	}

	files, err := playerSaveFiles(playersDir)
	if err != nil {
		index.Warnings = append(index.Warnings, "list player saves: "+err.Error())
		return
	}
	containers := make(map[string]int, len(index.Containers))
	for position := range index.Containers {
		containers[canonicalSaveID(index.Containers[position].ContainerID)] = position
	}

	type result struct {
		containerRefs []playerInventoryContainerRef
		partyID       string
		palboxID      string
		auxiliaryPals []Pal
		err           error
	}
	parsed := map[string]result{}
	for playerPosition := range index.Players {
		player := &index.Players[playerPosition]
		filename := playerSaveFilename(player.PlayerUID)
		cacheKey := strings.ToLower(filename)
		inventory, found := parsed[cacheKey]
		if !found {
			path, exists := files[cacheKey]
			if !exists {
				inventory.err = fmt.Errorf("%s is missing", filename)
			} else {
				file, err := readPlayerSave(path)
				if err != nil {
					inventory.err = fmt.Errorf("%s could not be parsed: %w", filepath.Base(path), err)
				} else {
					inventory.containerRefs, inventory.err = playerInventoryContainerIDs(file)
					if inventory.err == nil {
						inventory.partyID, inventory.palboxID = playerPalContainerIDs(file)
						dpsPath := strings.TrimSuffix(path, filepath.Ext(path)) + "_dps.sav"
						if _, statErr := os.Stat(dpsPath); statErr == nil {
							inventory.auxiliaryPals, inventory.err = readAuxiliaryPals(dpsPath, player.PlayerUID, "dimensional_pal_storage", player.PlayerUID+"_DPS")
						}
					}
					if inventory.err != nil {
						inventory.err = fmt.Errorf("%s has invalid inventory data: %w", filepath.Base(path), inventory.err)
					}
				}
			}
			parsed[cacheKey] = inventory
			if inventory.err != nil {
				index.Warnings = append(index.Warnings, "player save warning: "+inventory.err.Error())
			}
		}
		if inventory.err != nil {
			continue
		}
		for _, reference := range inventory.containerRefs {
			if position, ok := containers[canonicalSaveID(reference.ID)]; ok {
				index.Containers[position].OwnerType = "player"
				index.Containers[position].OwnerID = player.PlayerUID
				index.Containers[position].ContainerType = reference.Type
			}
		}
		player.InventorySummary = playerInventorySummary(index.Containers, player.PlayerUID)
		for palPosition := range index.Pals {
			pal := &index.Pals[palPosition]
			if pal.OwnerPlayerUID != player.PlayerUID {
				continue
			}
			switch canonicalSaveID(pal.ContainerID) {
			case canonicalSaveID(inventory.partyID):
				if inventory.partyID != "" {
					pal.LocationType = "player_party"
				}
			case canonicalSaveID(inventory.palboxID):
				if inventory.palboxID != "" {
					pal.LocationType = "palbox"
				}
			}
		}
		index.Pals = append(index.Pals, inventory.auxiliaryPals...)
	}
	globalPath := filepath.Join(filepath.Dir(playersDir), "GlobalPalStorage.sav")
	if _, err := os.Stat(globalPath); err == nil {
		ownerUID := ""
		if len(index.Players) > 0 {
			ownerUID = index.Players[0].PlayerUID
		}
		pals, parseErr := readAuxiliaryPals(globalPath, ownerUID, "global_pal_storage", "GLOBAL_PAL_STORAGE")
		if parseErr != nil {
			index.Warnings = append(index.Warnings, "global pal storage warning: "+parseErr.Error())
		} else {
			index.Pals = append(index.Pals, pals...)
		}
	}
}

func readAuxiliaryPals(path, ownerUID, locationType, containerID string) ([]Pal, error) {
	file, err := readPlayerSave(path)
	if err != nil {
		return nil, err
	}
	entries := asList(getField(file.Properties, "SaveParameterArray"))
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s does not contain SaveParameterArray", filepath.Base(path))
	}
	result := make([]Pal, 0, len(entries))
	for slotIndex, entry := range entries {
		saveParam := getField(entry, "SaveParameter")
		if saveParam == nil {
			saveParam = entry
		}
		individualID := firstNonEmptyAny(getField(entry, "InstanceId"), getField(saveParam, "InstanceId"))
		instanceID := asString(firstNonEmptyAny(getField(individualID, "InstanceId"), getField(saveParam, "InstanceId")))
		characterID := asString(firstNonEmptyAny(getField(saveParam, "CharacterID"), getField(saveParam, "CharacterId")))
		if instanceID == "" || characterID == "" || isZeroGUID(instanceID) {
			continue
		}
		gender := "male"
		if strings.Contains(strings.ToLower(asString(getField(saveParam, "Gender"))), "female") {
			gender = "female"
		}
		health, sanity, fullStomach, isSick, status, workSuitability := palRuntimeState(saveParam, locationType)
		result = append(result, Pal{
			InstanceID: instanceID, CharacterID: characterID, Nickname: asString(getField(saveParam, "NickName")),
			Level: asIntDefault(getField(saveParam, "Level"), 1), OwnerPlayerUID: ownerUID,
			OldOwnerUIDs: stringSlice(firstNonEmptyAny(getField(saveParam, "OldOwnerPlayerUIds"), getField(saveParam, "OldOwnerPlayerUIDs"))),
			ContainerID:  containerID, SlotIndex: slotIndex, LocationType: locationType, Gender: gender,
			Rank: asIntDefault(getField(saveParam, "Rank"), 1), IVHP: asInt(getField(saveParam, "Talent_HP")),
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
		})
	}
	return result, nil
}

func playerPalContainerIDs(file *gvas.File) (partyID, palboxID string) {
	saveData, ok := asMap(getField(file.Properties, "SaveData"))
	if !ok {
		return "", ""
	}
	return containerIDFromAny(getField(saveData, "OtomoCharacterContainerId")),
		containerIDFromAny(getField(saveData, "PalStorageContainerId"))
}

func playerSaveFiles(playersDir string) (map[string]string, error) {
	entries, err := os.ReadDir(playersDir)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".sav") {
			continue
		}
		files[strings.ToLower(entry.Name())] = filepath.Join(playersDir, entry.Name())
	}
	return files, nil
}

func readPlayerSave(path string) (*gvas.File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	gvasData, _, err := sav.DecodeToGVAS(data)
	if err != nil {
		return nil, fmt.Errorf("decode save: %w", err)
	}
	file, err := gvas.Read(gvasData)
	if err != nil {
		return nil, fmt.Errorf("parse GVAS: %w", err)
	}
	return file, nil
}

func playerInventoryContainerIDs(file *gvas.File) ([]playerInventoryContainerRef, error) {
	saveData, ok := asMap(getField(file.Properties, "SaveData"))
	if !ok {
		return nil, fmt.Errorf("SaveData is missing or has an unexpected shape")
	}
	inventoryValue := getField(saveData, "InventoryInfo")
	if inventoryValue == nil {
		return []playerInventoryContainerRef{}, nil
	}
	inventory, ok := asMap(inventoryValue)
	if !ok {
		return nil, fmt.Errorf("InventoryInfo has an unexpected shape")
	}

	unique := map[string]playerInventoryContainerRef{}
	for _, field := range playerInventoryContainerFields {
		containerID := containerIDFromAny(getField(inventory, field.Name))
		canonical := canonicalSaveID(containerID)
		if canonical == "" || isZeroGUID(canonical) {
			continue
		}
		unique[canonical] = playerInventoryContainerRef{ID: containerID, Type: field.Type}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	containerRefs := make([]playerInventoryContainerRef, 0, len(keys))
	for _, key := range keys {
		containerRefs = append(containerRefs, unique[key])
	}
	return containerRefs, nil
}

func playerInventorySummary(containers []Container, playerUID string) map[string]any {
	containerCount := 0
	usedSlots := 0
	totalItems := 0
	itemTypes := map[string]struct{}{}
	byType := map[string]int{}
	for _, container := range containers {
		if !strings.EqualFold(container.OwnerType, "player") || canonicalSaveID(container.OwnerID) != canonicalSaveID(playerUID) {
			continue
		}
		containerCount++
		for _, slot := range container.Slots {
			if strings.TrimSpace(slot.ItemID) == "" || slot.Count <= 0 {
				continue
			}
			usedSlots++
			totalItems += slot.Count
			itemTypes[strings.ToLower(strings.TrimSpace(slot.ItemID))] = struct{}{}
			byType[firstNonEmpty(container.ContainerType, "unknown")] += slot.Count
		}
	}
	return map[string]any{
		"container_count": containerCount,
		"used_slots":      usedSlots,
		"total_items":     totalItems,
		"item_types":      len(itemTypes),
		"by_container":    byType,
	}
}

func playerSaveFilename(playerUID string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(playerUID), "-", "")) + ".sav"
}

func canonicalSaveID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
