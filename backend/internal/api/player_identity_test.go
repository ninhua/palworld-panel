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
