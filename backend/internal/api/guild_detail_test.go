package api

import (
	"testing"

	"palpanel/internal/saveindex"
)

func TestGuildMemberDetailViewsIncludeAnnotationsAndOwner(t *testing.T) {
	guild := saveindex.Guild{
		ID:             "guild-1",
		OwnerPlayerUID: "uid-owner",
		Members: []saveindex.GuildMember{
			{PlayerUID: "uid-owner", Nickname: "Owner", LastOnlineTime: "2026-07-20T00:00:00Z"},
			{PlayerUID: "uid-member", Nickname: "Member"},
		},
	}
	players := []saveindex.Player{
		{PlayerUID: "uid-owner", SteamID: "steam-owner", Nickname: "Owner", Level: 50, IsOnline: true},
		{PlayerUID: "uid-member", SteamID: "steam-member", Nickname: "Member", Level: 31},
	}
	annotations := map[string]playerAnnotation{
		"uid-member": {Note: "负责采矿", Tags: []string{"矿工", "活跃"}, UpdatedAt: "2026-07-24T00:00:00Z"},
	}

	views := guildMemberDetailViews(guild, players, onlinePlayersResult{}, annotations)
	if len(views) != 2 {
		t.Fatalf("member views = %#v", views)
	}
	if views[0]["is_owner"] != true || views[0]["steam_id"] != "steam-owner" || views[0]["level"] != 50 {
		t.Fatalf("owner view = %#v", views[0])
	}
	if views[1]["note"] != "负责采矿" || views[1]["has_annotation"] != true {
		t.Fatalf("annotated member view = %#v", views[1])
	}
	tags, _ := views[1]["tags"].([]string)
	if len(tags) != 2 || tags[0] != "矿工" {
		t.Fatalf("member tags = %#v", views[1]["tags"])
	}
}

func TestGuildBaseDetailViewsIncludeCustomNamesAndImplicitGuildBases(t *testing.T) {
	guild := saveindex.Guild{ID: "guild-1", BaseIDs: []string{"base-1"}}
	bases := []saveindex.Base{
		{ID: "base-1", Name: "新規生成拠点テンプレート名0(仮)", GuildID: "guild-1", StructuresCount: 12, Workers: []saveindex.Worker{{InstanceID: "pal-1"}}},
		{ID: "base-2", Name: "新規生成拠点テンプレート名1(仮)", GuildID: "guild-1", StructuresCount: 8},
		{ID: "other", Name: "Other", GuildID: "guild-2"},
	}

	views := guildBaseDetailViews(guild, bases, map[string]string{"base-1": "北境制造中心"})
	if len(views) != 2 {
		t.Fatalf("base views = %#v", views)
	}
	if views[0]["name"] != "北境制造中心" || views[0]["has_custom_name"] != true || views[0]["workers_count"] != 1 {
		t.Fatalf("custom base view = %#v", views[0])
	}
	if views[1]["id"] != "base-2" {
		t.Fatalf("implicit guild base missing: %#v", views)
	}
}
