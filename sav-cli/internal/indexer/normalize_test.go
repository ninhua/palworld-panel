package indexer

import (
	"bytes"
	"encoding/binary"
	"testing"

	"palpanel/sav-cli/internal/gvas"
)

const (
	fixturePlayerID    = "00000000-0000-0000-0000-000000000001"
	fixtureGuildID     = "00000000-0000-0000-0000-000000000002"
	fixtureBaseID      = "00000000-0000-0000-0000-000000000003"
	fixturePalID       = "00000000-0000-0000-0000-000000000004"
	fixtureContainerID = "04030201-0807-0605-0c0b-0a09100f0e0d"
)

func TestNormalizeBuildsEntityRelationshipsAndSkipsUnknownFields(t *testing.T) {
	world := map[string]any{
		"FutureOneDotZeroField": map[string]any{"value": "ignored"},
		"GroupSaveDataMap": []any{map[string]any{
			"key": fixtureGuildID,
			"value": map[string]any{
				"GroupType": "EPalGroupType::Guild",
				"RawData": map[string]any{
					"group_id": fixtureGuildID, "guild_name": "Fixture Guild", "admin_player_uid": fixturePlayerID,
					"players":  []any{map[string]any{"player_uid": fixturePlayerID, "player_info": map[string]any{"player_name": "Fixture Player"}}},
					"base_ids": []any{fixtureBaseID},
				},
			},
		}},
		"CharacterSaveParameterMap": []any{
			characterEntry(fixturePalID, false),
			characterEntry(fixturePlayerID, true),
		},
		"CharacterContainerSaveData": []any{map[string]any{
			"key": map[string]any{"ID": fixtureContainerID},
			"value": map[string]any{"Slots": []any{map[string]any{
				"IndividualId": map[string]any{"InstanceId": fixturePalID},
			}}},
		}},
		"ItemContainerSaveData": []any{map[string]any{
			"key": map[string]any{"ID": fixtureContainerID},
			"value": map[string]any{
				"BelongInfo": map[string]any{"GroupID": fixtureGuildID},
				"Slots": []any{map[string]any{
					"SlotIndex": 2, "StackCount": 7, "ItemId": map[string]any{"StaticId": "Stone"},
				}},
			},
		}},
		"BaseCampSaveData": []any{map[string]any{
			"key": fixtureBaseID,
			"value": map[string]any{
				"RawData": map[string]any{
					"id": fixtureBaseID, "name": "Fixture Base", "group_id_belong_to": fixtureGuildID,
					"area_range": 1000.0, "transform": fixtureLocation(100, 200, 300),
				},
				"WorkerDirector": map[string]any{"RawData": workerDirectorFixture()},
			},
		}},
		"MapObjectSaveData": []any{map[string]any{
			"MapObjectInstanceId": "00000000-0000-0000-0000-000000000005",
			"MapObjectId":         "WoodenChest",
			"WorldLocation":       map[string]any{"x": 110.0, "y": 210.0, "z": 300.0},
			"ConcreteModel": map[string]any{"ModuleMap": []any{map[string]any{
				"key":   "EPalMapObjectConcreteModelModuleType::ItemContainer",
				"value": map[string]any{"RawData": fixtureContainerBytes()},
			}}},
		}},
	}
	file := &gvas.File{Properties: map[string]any{"worldSaveData": world}}
	index := Normalize(file, "fixture")

	if index.Counts.Players != 1 || index.Counts.Guilds != 1 || index.Counts.Bases != 1 || index.Counts.Pals != 1 || index.Counts.Containers != 1 {
		t.Fatalf("unexpected normalized counts: %#v", index.Counts)
	}
	if len(index.Bases[0].Workers) != 1 || index.Bases[0].Workers[0].InstanceID != fixturePalID {
		t.Fatalf("worker relationship was not built: %#v", index.Bases[0].Workers)
	}
	if index.Bases[0].StructuresCount != 1 || len(index.Bases[0].Containers) != 1 || index.Bases[0].Containers[0] != fixtureContainerID {
		t.Fatalf("base map/container relationships were not built: %#v", index.Bases[0])
	}
	container := index.Containers[0]
	if container.OwnerType != "map_object" || len(container.Slots) != 1 || container.Slots[0].ItemID != "Stone" || container.Slots[0].Count != 7 {
		t.Fatalf("container was not normalized: %#v", container)
	}
	if index.Counts.MapEntities != 4 {
		t.Fatalf("expected player/base/pal/map object entities, got %#v", index.MapEntities)
	}
	if len(index.Warnings) != 0 {
		t.Fatalf("unknown field should have been skipped without breaking core parsing: %#v", index.Warnings)
	}
}

func TestNormalizeWarnsAndKeepsBaseWhenWorkerDataChanges(t *testing.T) {
	file := &gvas.File{Properties: map[string]any{"worldSaveData": map[string]any{
		"BaseCampSaveData": []any{map[string]any{
			"key": fixtureBaseID,
			"value": map[string]any{
				"RawData":        map[string]any{"id": fixtureBaseID, "name": "Fixture Base", "area_range": 100.0},
				"WorkerDirector": map[string]any{"RawData": []byte{1, 2, 3}},
			},
		}},
	}}}
	index := Normalize(file, "fixture")
	if len(index.Bases) != 1 || len(index.Warnings) == 0 {
		t.Fatalf("expected tolerant base result with explicit warning: %#v", index)
	}
}

func characterEntry(id string, player bool) map[string]any {
	save := map[string]any{
		"IsPlayer": player, "NickName": "Fixture", "Level": 10,
		"LastJumpedLocation":   map[string]any{"x": 100.0, "y": 200.0, "z": 300.0},
		"FutureCharacterField": map[string]any{"value": 1},
	}
	if !player {
		save["OwnerPlayerUId"] = fixturePlayerID
		save["CharacterID"] = "Sheepball"
		save["SlotID"] = map[string]any{"ContainerId": map[string]any{"ID": fixtureContainerID}}
	}
	return map[string]any{
		"key":   map[string]any{"PlayerUId": fixturePlayerID, "InstanceId": id},
		"value": map[string]any{"RawData": map[string]any{"object": map[string]any{"SaveParameter": save}}},
	}
}

func fixtureLocation(x, y, z float64) map[string]any {
	return map[string]any{"translation": map[string]any{"x": x, "y": y, "z": z}}
}

func fixtureContainerBytes() []byte {
	return []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
}

func workerDirectorFixture() []byte {
	var body bytes.Buffer
	body.Write(make([]byte, 16))
	for _, value := range []float64{0, 0, 0, 1, 100, 200, 300, 1, 1, 1} {
		_ = binary.Write(&body, binary.LittleEndian, value)
	}
	body.WriteByte(0)
	body.WriteByte(0)
	body.Write(fixtureContainerBytes())
	return body.Bytes()
}

func TestPalRuntimeStateReadsHealthNeedsSicknessAndWorkSuitability(t *testing.T) {
	saveParameter := map[string]any{
		"Hp":             int64(123000),
		"SanityValue":    72.5,
		"FullStomach":    44.0,
		"WorkerSick":     map[string]any{"State": "Sprain"},
		"PalReviveTimer": 0.0,
		"GotWorkSuitabilityAddRankList": []any{
			map[string]any{"WorkSuitability": "EPalWorkSuitability::Mining", "Rank": 2},
			map[string]any{"WorkSuitability": "EPalWorkSuitability::Mining", "Rank": 3},
			map[string]any{"WorkSuitability": "EPalWorkSuitability::EmitFlame", "Rank": 1},
		},
	}

	health, sanity, stomach, sick, status, work := palRuntimeState(saveParameter, "palbox")
	if health == nil || *health != 123000 || sanity == nil || *sanity != 72.5 || stomach == nil || *stomach != 44 {
		t.Fatalf("unexpected runtime values: health=%v sanity=%v stomach=%v", health, sanity, stomach)
	}
	if !sick || status != "Injured" {
		t.Fatalf("expected injured/sick state, got sick=%v status=%q", sick, status)
	}
	if len(work) != 2 || work[0] != (WorkSuitability{Type: "Mining", Level: 3}) || work[1] != (WorkSuitability{Type: "Kindling", Level: 1}) {
		t.Fatalf("unexpected work suitability: %#v", work)
	}
}

func TestPalRuntimeStateTreatsZeroReviveTimerAsHealthy(t *testing.T) {
	health := int64(1000)
	parsedHealth, _, _, sick, status, _ := palRuntimeState(map[string]any{
		"Hp":             health,
		"PalReviveTimer": 0.0,
	}, "palbox")
	if parsedHealth == nil || *parsedHealth != health || sick || status != "Healthy" {
		t.Fatalf("zero revive timer must not mark the Pal sick/dead: health=%v sick=%v status=%q", parsedHealth, sick, status)
	}
}
