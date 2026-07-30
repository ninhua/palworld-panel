package indexer

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const secondFixtureContainerID = "14131211-1817-1615-1c1b-1a19201f1e1d"

func TestNormalizePlayerSavesAssociatesInventoryContainers(t *testing.T) {
	playersDir := t.TempDir()
	writePlayerSaveFixture(t, playersDir, fixturePlayerID, map[string]string{
		"CommonContainerId":    fixtureContainerID,
		"FoodEquipContainerId": secondFixtureContainerID,
	})
	index := Index{
		Players: []Player{{PlayerUID: fixturePlayerID}},
		Containers: []Container{
			{ContainerID: fixtureContainerID, OwnerType: "guild", OwnerID: fixtureGuildID},
			{ContainerID: secondFixtureContainerID, OwnerType: "map_object", OwnerID: "fixture-object"},
			{ContainerID: fixtureBaseID, OwnerType: "base", OwnerID: fixtureBaseID},
		},
		Warnings: []string{},
	}

	normalizePlayerSaves(&index, playersDir)

	for position := range 2 {
		container := index.Containers[position]
		if container.OwnerType != "player" || container.OwnerID != fixturePlayerID {
			t.Fatalf("inventory container was not associated with player: %#v", container)
		}
	}
	if index.Containers[0].ContainerType != "items" || index.Containers[1].ContainerType != "food" {
		t.Fatalf("inventory container types were not retained: %#v", index.Containers[:2])
	}
	if summary := index.Players[0].InventorySummary; summary["container_count"] != 2 || summary["used_slots"] != 0 {
		t.Fatalf("player inventory summary was not populated: %#v", summary)
	}
	if container := index.Containers[2]; container.OwnerType != "base" || container.OwnerID != fixtureBaseID {
		t.Fatalf("unreferenced container ownership changed: %#v", container)
	}
	if len(index.Warnings) != 0 {
		t.Fatalf("valid player save produced warnings: %#v", index.Warnings)
	}
}

func TestNormalizePlayerSavesSupportsCurrentFriendshipMap(t *testing.T) {
	playersDir := t.TempDir()
	writePlayerSaveFixture(t, playersDir, fixturePlayerID, map[string]string{
		"CommonContainerId": fixtureContainerID,
	})
	index := Index{
		Players:    []Player{{PlayerUID: fixturePlayerID}},
		Containers: []Container{{ContainerID: fixtureContainerID}},
		Warnings:   []string{},
	}

	normalizePlayerSaves(&index, playersDir)

	if len(index.Warnings) != 0 {
		t.Fatalf("current player friendship map produced warnings: %#v", index.Warnings)
	}
	if index.Containers[0].OwnerID != fixturePlayerID {
		t.Fatalf("player inventory was not parsed after friendship map: %#v", index.Containers[0])
	}
}

func TestNormalizePlayerSavesWarnsAndContinuesForMissingAndCorruptSaves(t *testing.T) {
	playersDir := t.TempDir()
	validPlayerID := fixturePlayerID
	missingPlayerID := "00000000-0000-0000-0000-000000000006"
	corruptPlayerID := "00000000-0000-0000-0000-000000000007"
	writePlayerSaveFixture(t, playersDir, validPlayerID, map[string]string{
		"CommonContainerId": fixtureContainerID,
	})
	if err := os.WriteFile(filepath.Join(playersDir, playerSaveFilename(corruptPlayerID)), []byte("not a Palworld save"), 0o600); err != nil {
		t.Fatal(err)
	}
	index := Index{
		Players: []Player{
			{PlayerUID: validPlayerID},
			{PlayerUID: missingPlayerID},
			{PlayerUID: corruptPlayerID},
		},
		Containers: []Container{{ContainerID: fixtureContainerID}},
		Warnings:   []string{},
	}

	normalizePlayerSaves(&index, playersDir)

	if len(index.Players) != 3 {
		t.Fatalf("player save warnings removed world players: %#v", index.Players)
	}
	if container := index.Containers[0]; container.OwnerType != "player" || container.OwnerID != validPlayerID {
		t.Fatalf("valid player save was not applied after another save failed: %#v", container)
	}
	if len(index.Warnings) != 2 {
		t.Fatalf("expected one warning per unavailable player save, got %#v", index.Warnings)
	}
	assertWarningContains(t, index.Warnings, playerSaveFilename(missingPlayerID)+" is missing")
	assertWarningContains(t, index.Warnings, playerSaveFilename(corruptPlayerID)+" could not be parsed")
}

func TestNormalizePlayerSavesWarnsOnceForDuplicatePlayerRecords(t *testing.T) {
	index := Index{
		Players:  []Player{{PlayerUID: fixturePlayerID}, {PlayerUID: fixturePlayerID}},
		Warnings: []string{},
	}

	normalizePlayerSaves(&index, t.TempDir())

	if len(index.Warnings) != 1 {
		t.Fatalf("expected duplicate records to share one player-save warning, got %#v", index.Warnings)
	}
}

func TestDeduplicatePlayersMergesUIDAndSteamAliases(t *testing.T) {
	index := Index{Players: []Player{
		{PlayerUID: "ABCDEF00-0000-0000-0000-000000000001", Nickname: "ABCDEF00-0000-0000-0000-000000000001", Level: 2},
		{PlayerUID: "abcdef00000000000000000000000001", SteamID: "steam_76561198000000001", Nickname: "Builder", Level: 40},
		{SteamID: "76561198000000001", Nickname: "Builder", Level: 39},
	}}

	deduplicatePlayers(&index)

	if len(index.Players) != 1 {
		t.Fatalf("duplicate players were not merged: %#v", index.Players)
	}
	player := index.Players[0]
	if player.Nickname != "Builder" || player.Level != 40 || player.SteamID != "steam_76561198000000001" {
		t.Fatalf("merged player = %#v", player)
	}
}

func TestCopySnapshotIncludesCaseInsensitivePlayerSavesAndSkipsDirectories(t *testing.T) {
	worldDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(worldDir, "Level.sav"), []byte("level"), 0o600); err != nil {
		t.Fatal(err)
	}
	playersDir := filepath.Join(worldDir, "Players")
	if err := os.MkdirAll(filepath.Join(playersDir, "BROKEN.SAV"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(playersDir, "PLAYER.SAV"), []byte("player"), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshot, err := CopySnapshot(worldDir)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(snapshot)
	if body, err := os.ReadFile(filepath.Join(snapshot, "Players", "PLAYER.SAV")); err != nil || string(body) != "player" {
		t.Fatalf("uppercase player save was not copied: %q, %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(snapshot, "Players", "BROKEN.SAV")); !os.IsNotExist(err) {
		t.Fatalf("directory with .sav suffix was copied: %v", err)
	}
	manifest, err := SnapshotManifest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 2 || manifest.Files[1].Path != "Players/PLAYER.SAV" {
		t.Fatalf("uppercase player save missing from manifest: %#v", manifest.Files)
	}
}

func assertWarningContains(t *testing.T, warnings []string, want string) {
	t.Helper()
	for _, warning := range warnings {
		if strings.Contains(warning, want) {
			return
		}
	}
	t.Fatalf("warning containing %q not found in %#v", want, warnings)
}

func writePlayerSaveFixture(t *testing.T, playersDir, playerUID string, containers map[string]string) {
	t.Helper()
	gvasData := playerGVASFixture(t, containers)
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(gvasData); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 12)
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(gvasData)))
	binary.LittleEndian.PutUint32(header[4:8], uint32(compressed.Len()))
	copy(header[8:11], []byte("PlZ"))
	header[11] = '1'
	data := append(header, compressed.Bytes()...)
	if err := os.WriteFile(filepath.Join(playersDir, strings.ToLower(playerSaveFilename(playerUID))), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func playerGVASFixture(t *testing.T, containers map[string]string) []byte {
	t.Helper()
	var body bytes.Buffer
	writeU32(t, &body, 0x53415647)
	writeI32(t, &body, 3)
	writeI32(t, &body, 0x20a)
	writeI32(t, &body, 0x3f0)
	writeU16(t, &body, 5)
	writeU16(t, &body, 1)
	writeU16(t, &body, 1)
	writeU32(t, &body, 0)
	writeFString(t, &body, "++UE5+Release-5.1")
	writeI32(t, &body, 3)
	writeU32(t, &body, 0)
	writeFString(t, &body, "/Script/Pal.PalWorldPlayerSaveGame")

	writeStructProperty(t, &body, "SaveData", "PalPlayerSaveData", func() {
		writeStructProperty(t, &body, "LastTransform", "Transform", func() {
			writeStructProperty(t, &body, "Rotation", "Quat", func() {
				for _, value := range []float64{0, 0, 0, 1} {
					writeFloat64(t, &body, value)
				}
			})
			writeStructProperty(t, &body, "Translation", "Vector", func() {
				for _, value := range []float64{123, 456, 789} {
					writeFloat64(t, &body, value)
				}
			})
			writeFString(t, &body, "None")
		})
		writeStructMapProperty(t, &body, "Local_MaxFriendshipPalIds", func() {
			writeGUIDProperty(t, &body, "PlayerUId", fixturePlayerID)
			writeFString(t, &body, "None")
		}, func() {
			writeGUIDProperty(t, &body, "InstanceId", fixtureBaseID)
			writeFString(t, &body, "None")
		})
		writeStructProperty(t, &body, "InventoryInfo", "PalPlayerInventoryInfo", func() {
			for _, field := range playerInventoryContainerFields {
				containerID, ok := containers[field.Name]
				if !ok {
					continue
				}
				writeStructProperty(t, &body, field.Name, "PalContainerId", func() {
					writeGUIDProperty(t, &body, "ID", containerID)
					writeFString(t, &body, "None")
				})
			}
			writeFString(t, &body, "None")
		})
		writeFString(t, &body, "None")
	})
	writeFString(t, &body, "None")
	return body.Bytes()
}

func writeStructMapProperty(t *testing.T, body *bytes.Buffer, name string, key, value func()) {
	t.Helper()
	writeFString(t, body, name)
	writeFString(t, body, "MapProperty")
	writeU64(t, body, 0)
	writeFString(t, body, "StructProperty")
	writeFString(t, body, "StructProperty")
	body.WriteByte(0)
	writeU32(t, body, 0)
	writeU32(t, body, 1)
	key()
	value()
}

func writeStructProperty(t *testing.T, body *bytes.Buffer, name, structType string, value func()) {
	t.Helper()
	writeFString(t, body, name)
	writeFString(t, body, "StructProperty")
	writeU64(t, body, 0)
	writeFString(t, body, structType)
	body.Write(make([]byte, 16))
	body.WriteByte(0)
	value()
}

func writeGUIDProperty(t *testing.T, body *bytes.Buffer, name, value string) {
	t.Helper()
	writeFString(t, body, name)
	writeFString(t, body, "StructProperty")
	writeU64(t, body, 16)
	writeFString(t, body, "Guid")
	body.Write(make([]byte, 16))
	body.WriteByte(0)
	body.Write(parseFixtureGUID(t, value))
}

func parseFixtureGUID(t *testing.T, value string) []byte {
	t.Helper()
	hexValue := strings.ReplaceAll(value, "-", "")
	if len(hexValue) != 32 {
		t.Fatalf("invalid fixture GUID %q", value)
	}
	decoded := make([]byte, 16)
	for i := range decoded {
		var v byte
		if _, err := fmt.Sscanf(hexValue[i*2:i*2+2], "%02x", &v); err != nil {
			t.Fatalf("parse fixture GUID %q: %v", value, err)
		}
		decoded[i] = v
	}
	return []byte{
		decoded[3], decoded[2], decoded[1], decoded[0],
		decoded[7], decoded[6], decoded[5], decoded[4],
		decoded[11], decoded[10], decoded[9], decoded[8],
		decoded[15], decoded[14], decoded[13], decoded[12],
	}
}

func writeFString(t *testing.T, body *bytes.Buffer, value string) {
	t.Helper()
	writeI32(t, body, int32(len(value)+1))
	body.WriteString(value)
	body.WriteByte(0)
}

func writeI32(t *testing.T, body *bytes.Buffer, value int32) {
	t.Helper()
	if err := binary.Write(body, binary.LittleEndian, value); err != nil {
		t.Fatal(err)
	}
}

func writeU16(t *testing.T, body *bytes.Buffer, value uint16) {
	t.Helper()
	if err := binary.Write(body, binary.LittleEndian, value); err != nil {
		t.Fatal(err)
	}
}

func writeU32(t *testing.T, body *bytes.Buffer, value uint32) {
	t.Helper()
	if err := binary.Write(body, binary.LittleEndian, value); err != nil {
		t.Fatal(err)
	}
}

func writeU64(t *testing.T, body *bytes.Buffer, value uint64) {
	t.Helper()
	if err := binary.Write(body, binary.LittleEndian, value); err != nil {
		t.Fatal(err)
	}
}

func writeFloat64(t *testing.T, body *bytes.Buffer, value float64) {
	t.Helper()
	if err := binary.Write(body, binary.LittleEndian, value); err != nil {
		t.Fatal(err)
	}
}
