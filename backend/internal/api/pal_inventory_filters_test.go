package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"palpanel/internal/saveindex"
)

func palFilterContext(rawQuery string) *gin.Context {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest("GET", "/api/pals?"+rawQuery, nil)
	context.Request = request
	return context
}

func TestPalInventoryAdvancedFilters(t *testing.T) {
	players := []saveindex.Player{{PlayerUID: "owner-a", Nickname: "Alice"}}
	pals := []saveindex.Pal{
		{InstanceID: "a", CharacterID: "Anubis", OwnerPlayerUID: "owner-a", Level: 55, Gender: "male", Rank: 5, IVHP: 100, IVAttack: 90, IVDefense: 80, Passives: []string{"Workaholic"}, LocationType: "PalStorage", SlotIndex: 31},
		{InstanceID: "b", CharacterID: "PinkCat", OwnerPlayerUID: "owner-a", Level: 20, Gender: "female", Rank: 1, IVHP: 20, IVAttack: 20, IVDefense: 20, Passives: []string{"Runner"}, LocationType: "Party", SlotIndex: 2},
	}

	filtered := filterPals(pals, players, palFilterContext("min_level=50&min_stars=4&min_iv_average=80&gender=male&location=storage&passive=Workaholic"))
	if len(filtered) != 1 || filtered[0].InstanceID != "a" {
		t.Fatalf("filtered = %#v", filtered)
	}
}

func TestPalTerminalLocation(t *testing.T) {
	tests := []struct {
		pal  saveindex.Pal
		want string
	}{
		{saveindex.Pal{LocationType: "PalStorage", SlotIndex: 31}, "终端第2页 · 第1行第2列"},
		{saveindex.Pal{LocationType: "Party", SlotIndex: 2}, "队伍第3位"},
		{saveindex.Pal{LocationType: "BaseCamp", SlotIndex: 4}, "据点工作位 5"},
		{saveindex.Pal{OnExpedition: true}, "远征中"},
	}
	for _, test := range tests {
		if got := palTerminalLocation(test.pal); got != test.want {
			t.Fatalf("palTerminalLocation(%#v) = %q, want %q", test.pal, got, test.want)
		}
	}
}

func TestSortPalsByIVAndStars(t *testing.T) {
	pals := []saveindex.Pal{
		{InstanceID: "low", Level: 60, Rank: 1, IVHP: 10, IVAttack: 10, IVDefense: 10},
		{InstanceID: "high", Level: 30, Rank: 5, IVHP: 100, IVAttack: 90, IVDefense: 80},
	}
	sortPals(pals, "iv_desc")
	if pals[0].InstanceID != "high" {
		t.Fatalf("iv sort = %#v", pals)
	}
	sortPals(pals, "stars_desc")
	if pals[0].InstanceID != "high" {
		t.Fatalf("stars sort = %#v", pals)
	}
}

func TestFlattenPalIncludesTerminalAndQualityFields(t *testing.T) {
	pal := saveindex.Pal{
		InstanceID: "pal-1", CharacterID: "Anubis", Rank: 5,
		IVHP: 100, IVAttack: 90, IVDefense: 80,
		LocationType: "PalStorage", SlotIndex: 31,
	}
	view := flattenPal(pal, map[string]saveindex.Player{})
	if view["stars"] != 4 || view["iv_average"] != 90 {
		t.Fatalf("quality fields = %#v", view)
	}
	if view["location_kind"] != "storage" || view["terminal_location"] != "终端第2页 · 第1行第2列" {
		t.Fatalf("location fields = %#v", view)
	}
}

func TestPalInventoryAdvancedFiltersFeatureRegistered(t *testing.T) {
	for _, feature := range patchFeatures {
		if feature == "pal-inventory-advanced-filters" {
			return
		}
	}
	t.Fatalf("pal-inventory-advanced-filters not registered in %#v", patchFeatures)
}
