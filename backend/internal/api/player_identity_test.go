package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"palpanel/internal/saveindex"
)

func TestMatchesIDAcceptsPalworldGUIDFormatsAndSteamPrefix(t *testing.T) {
	if !matchesID("f23d556c-0000-0000-0000-000000000000", "F23D556C000000000000000000000000") {
		t.Fatal("Palworld UUID and compact GUID should identify the same player")
	}
	if !matchesID("steam_76561199032061430", "76561199032061430") {
		t.Fatal("Steam ID should match with or without the steam_ prefix")
	}
}

func TestFilterPalsMatchesNormalizedOwnerIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		"GET",
		"/api/pals?owner_player_uid=f23d556c-0000-0000-0000-000000000000",
		nil,
	)
	pals := []saveindex.Pal{{
		InstanceID:     "pal-1",
		OwnerPlayerUID: "F23D556C000000000000000000000000",
	}}

	filtered := filterPals(pals, nil, context)
	if len(filtered) != 1 || filtered[0].InstanceID != "pal-1" {
		t.Fatalf("normalized owner filter returned %#v", filtered)
	}
}

func TestFlattenPalFindsOwnerAcrossGUIDFormats(t *testing.T) {
	players := []saveindex.Player{{
		PlayerUID: "f23d556c-0000-0000-0000-000000000000",
		SteamID:   "steam_76561199032061430",
		Nickname:  "tiantian",
	}}
	pal := saveindex.Pal{
		InstanceID:     "pal-1",
		OwnerPlayerUID: "F23D556C000000000000000000000000",
	}

	view := flattenPal(pal, playerLookup(players))
	if view["owner_nickname"] != "tiantian" || view["owner_steam_id"] != "steam_76561199032061430" {
		t.Fatalf("owner metadata was not resolved: %#v", view)
	}
}

func TestPlayerInventoryContainersMatchesSteamAliasToUIDOwner(t *testing.T) {
	index := saveindex.Index{
		Players: []saveindex.Player{{
			PlayerUID: "f23d556c-0000-0000-0000-000000000000",
			SteamID:   "steam_76561199032061430",
		}},
		Containers: []saveindex.Container{{
			ContainerID:   "inventory-1",
			ContainerType: "items",
			OwnerType:     "player",
			OwnerID:       "F23D556C000000000000000000000000",
		}},
	}

	containers := playerInventoryContainers(index, "76561199032061430")
	if len(containers) != 1 || containers[0].ContainerID != "inventory-1" {
		t.Fatalf("Steam alias did not resolve the player's UID-owned inventory: %#v", containers)
	}
}

func TestFlattenPalIncludesSnapshotStateTraitsAndWorkSuitability(t *testing.T) {
	health := int64(123000)
	sanity := 72.5
	stomach := 44.0
	view := flattenPal(saveindex.Pal{
		InstanceID:      "pal-1",
		CharacterID:     "Anubis",
		Passives:        []string{"Legend", "CraftSpeed_up3"},
		WorkSuitability: []saveindex.WorkSuitability{{Type: "Mining", Level: 3}},
		Health:          &health,
		Sanity:          &sanity,
		FullStomach:     &stomach,
		IsSick:          true,
		Status:          "injured",
	}, nil)

	if view["health"] != &health || view["sanity"] != &sanity || view["full_stomach"] != &stomach {
		t.Fatalf("runtime state was not exposed: %#v", view)
	}
	if view["status"] != "Injured" || view["is_sick"] != true {
		t.Fatalf("status was not normalized: %#v", view)
	}
	passives, ok := view["raw_passives"].([]string)
	if !ok || len(passives) != 2 {
		t.Fatalf("passive traits were not exposed: %#v", view["raw_passives"])
	}
	work, ok := view["work_suitability"].([]gin.H)
	if !ok || len(work) != 1 || work[0]["type"] != "Mining" || work[0]["level"] != 3 {
		t.Fatalf("work suitability was not exposed: %#v", view["work_suitability"])
	}
}
