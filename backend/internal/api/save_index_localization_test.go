package api

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/paldefender"
	"palpanel/internal/saveindex"
)

func TestSaveIndexDisplayLocalization(t *testing.T) {
	pal := saveindex.Pal{
		InstanceID:  "pal-1",
		CharacterID: "PinkCat",
		Level:       2,
		Passives:    []string{"CraftSpeed_up3", "FuturePassive"},
		Raw:         []byte{0, 255},
	}
	view := flattenPal(pal, nil)
	if _, exposed := view["raw"]; exposed {
		t.Fatalf("save-index API must not expose raw Pal payloads: %#v", view)
	}
	if view["name"] != "捣蛋猫" || view["species_name"] != "捣蛋猫" || view["character_id"] != "PinkCat" {
		t.Fatalf("Pal display identity was not localized with its raw ID preserved: %#v", view)
	}
	passives := view["passives"].([]string)
	if len(passives) != 2 || passives[0] != "卓绝技艺" || passives[1] != "FuturePassive" {
		t.Fatalf("passive names were not localized with fallback: %#v", passives)
	}

	base := flattenBase(saveindex.Base{
		Name:      "新規生成拠点テンプレート名0(仮)",
		GuildName: "Unnamed Guild",
		Workers:   []saveindex.Worker{{InstanceID: "worker-1", CharacterID: "Ganesha", Level: 4}},
		Raw:       []byte{0, 255},
	})
	if _, exposed := base["raw"]; exposed {
		t.Fatalf("save-index API must not expose raw base payloads: %#v", base)
	}
	if base["name"] != "未命名据点" || base["guild_name"] != "未命名公会" {
		t.Fatalf("base labels were not localized: %#v", base)
	}
	workers := base["workers"].([]gin.H)
	if workers[0]["name"] != "壶小象" || workers[0]["character_id"] != "Ganesha" {
		t.Fatalf("worker name was not localized: %#v", workers)
	}

	containers := flattenContainers([]saveindex.Container{{
		ContainerID: "container-1",
		Slots:       []saveindex.Slot{{Slot: 0, ItemID: "Stone", Count: 7}},
	}})
	slots := containers[0]["slots"].([]gin.H)
	if slots[0]["item_name"] != "石头" || slots[0]["item_id"] != "Stone" || slots[0]["item_icon"] != "stone" {
		t.Fatalf("item name was not localized: %#v", slots)
	}

	entities := flattenMapEntities([]saveindex.MapEntity{{Type: "map_object", ID: "object-1", Label: "PalBoxV2"}})
	if entities[0]["label"] != "帕鲁终端" || entities[0]["raw_label"] != "PalBoxV2" {
		t.Fatalf("map object name was not localized: %#v", entities)
	}
}

func TestFilterPalsAcceptsChineseName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/api/pals?q=捣蛋猫", nil)
	pals := []saveindex.Pal{{InstanceID: "pal-1", CharacterID: "PinkCat"}}
	if got := filterPals(pals, nil, context); len(got) != 1 {
		t.Fatalf("Chinese Pal query should match internal CharacterID: %#v", got)
	}
}

func TestOverlayOnlinePlayersUsesLiveCoordinatesWithoutDuplicates(t *testing.T) {
	location := saveindex.Coordinates{X: 123, Y: 456, Z: 789}
	live := knownOnlinePlayer(onlinePlayer{
		PlayerUID: "player-uid",
		SteamID:   "steam-id",
		Nickname:  "在线玩家",
		Location:  location,
	})
	online := map[string]onlinePlayer{
		"player-uid": live,
		"steam-id":   live,
	}
	players := mergeSaveAndOnline([]saveindex.Player{{
		PlayerUID: "player-uid",
		SteamID:   "steam-id",
		Nickname:  "存档玩家",
		Location:  saveindex.Coordinates{X: 1, Y: 2, Z: 3},
	}}, online)
	if len(players) != 1 {
		t.Fatalf("online player was duplicated: %#v", players)
	}
	if !players[0].IsOnline || players[0].Nickname != "存档玩家" || players[0].Location != location {
		t.Fatalf("live state did not preserve the save profile: %#v", players[0])
	}
}

func TestOverlayOnlinePlayersMatchesSteamPrefixVariants(t *testing.T) {
	live := knownOnlinePlayer(onlinePlayer{
		PlayerUID: "live-player-uid",
		SteamID:   "steam_76561198370732375",
		Nickname:  "玛卡巴卡",
	})
	online := map[string]onlinePlayer{
		normalizedPlayerKey(live.PlayerUID): live,
		normalizedPlayerKey(live.SteamID):   live,
	}
	players := mergeSaveAndOnline([]saveindex.Player{{
		PlayerUID: "save-player-uid",
		SteamID:   "76561198370732375",
		Nickname:  "玛卡巴卡",
		Level:     37,
	}}, online)

	if len(players) != 1 {
		t.Fatalf("Steam prefix variant created a duplicate player: %#v", players)
	}
	if !players[0].IsOnline || players[0].Level != 37 || players[0].SteamID != live.SteamID {
		t.Fatalf("live identity was not overlaid onto the save player: %#v", players[0])
	}
}

func TestOverlayOnlinePlayersPreservesSaveCoordinatesWhenRESTHasNone(t *testing.T) {
	saveLocation := saveindex.Coordinates{X: 11, Y: 22, Z: 33}
	players := mergeSaveAndOnline([]saveindex.Player{{
		PlayerUID: "player-uid",
		SteamID:   "steam-id",
		Location:  saveLocation,
	}}, map[string]onlinePlayer{
		"player-uid": knownOnlinePlayer(onlinePlayer{PlayerUID: "player-uid", SteamID: "steam-id", Nickname: "在线玩家"}),
	})
	if len(players) != 1 || !players[0].IsOnline || players[0].Location != saveLocation {
		t.Fatalf("REST data without coordinates replaced the save position: %#v", players)
	}
}

func TestOnlinePlayersFromPalDefenderPrefersWorldLocation(t *testing.T) {
	players, reliable := onlinePlayersFromPalDefender(paldefender.RESTPlayersResponse{
		Meta: paldefender.RESTPlayersMeta{PlayerCount: 3, OnlineCount: 2},
		Players: []paldefender.RESTPlayer{
			{
				Name:          "World",
				PlayerUID:     "world-uid",
				UserID:        "world-steam",
				Status:        "Online",
				WorldLocation: paldefender.RESTLocation{X: 10, Y: 20, Z: 30},
				MapLocation:   paldefender.RESTLocation{X: 100, Y: 200, Z: 300},
			},
			{
				Name:        "Map fallback",
				PlayerUID:   "map-uid",
				UserID:      "map-steam",
				Status:      "online",
				MapLocation: paldefender.RESTLocation{X: 40, Y: 50, Z: 60},
			},
			{Name: "Offline", PlayerUID: "offline-uid", Status: "offline"},
		},
	})
	if !reliable {
		t.Fatal("consistent PalDefender metadata should be reliable")
	}
	if len(players) != 5 {
		t.Fatalf("expected online identities plus the offline GM identity, got %#v", players)
	}
	if got := players[identityKey("world-uid")].Location; got != (saveindex.Coordinates{X: 10, Y: 20, Z: 30}) {
		t.Fatalf("world location was not preferred: %#v", got)
	}
	if got := players[identityKey("map-steam")].Location; got != (saveindex.Coordinates{X: 40, Y: 50, Z: 60}) {
		t.Fatalf("map location fallback was not used: %#v", got)
	}
	if offline, ok := players[identityKey("offline-uid")]; !ok || offline.online() {
		t.Fatalf("offline PalDefender identity should be retained without a live signal: %#v", players)
	}
}

func TestOnlinePlayersFromPalDefenderMarksInconsistentOnlineMetadataUntrusted(t *testing.T) {
	players, reliable := onlinePlayersFromPalDefender(paldefender.RESTPlayersResponse{
		Meta: paldefender.RESTPlayersMeta{PlayerCount: 3, OnlineCount: 1},
		Players: []paldefender.RESTPlayer{
			{Name: "Current", PlayerUID: "uid-current", UserID: "steam-current", Status: "Online", WorldLocation: paldefender.RESTLocation{X: 1}},
			{Name: "Ghost", PlayerUID: "uid-ghost", UserID: "steam-ghost", Status: "Online", WorldLocation: paldefender.RESTLocation{X: 2}},
			{Name: "Offline", PlayerUID: "uid-offline", UserID: "steam-offline", Status: "Offline"},
		},
	})
	if reliable {
		t.Fatal("inconsistent PalDefender metadata must not be reliable")
	}

	for _, id := range []string{"uid-current", "uid-ghost"} {
		player := players[identityKey(id)]
		if player.online() || player.PalDefenderOnline || !player.PalDefenderLiveData || player.OnlineStateKnown {
			t.Fatalf("inconsistent PalDefender online claim should be retained but not trusted for %q: %#v", id, player)
		}
		if !coordinatesAvailable(player.Location) {
			t.Fatalf("untrusted claim should retain coordinates for REST-confirmed enrichment of %q: %#v", id, player)
		}
	}
	fallback := palDefenderPlayersForFallback(players, reliable)
	for _, id := range []string{"uid-current", "uid-ghost", "uid-offline"} {
		player := fallback[identityKey(id)]
		if player.online() || player.PalDefenderOnline || coordinatesAvailable(player.Location) {
			t.Fatalf("untrusted PalDefender fallback must be offline for %q: %#v", id, player)
		}
	}
	if fallback[identityKey("uid-current")].GMUserID != "steam-current" {
		t.Fatalf("untrusted fallback should still retain GM identity: %#v", fallback)
	}
}

func TestOnlinePlayersFromPalDefenderTreatsMissingMetaAsUntrusted(t *testing.T) {
	players, reliable := onlinePlayersFromPalDefender(paldefender.RESTPlayersResponse{Players: []paldefender.RESTPlayer{
		{Name: "Ghost", PlayerUID: "uid-ghost", UserID: "steam-ghost", Status: "Online", WorldLocation: paldefender.RESTLocation{X: 2}},
	}})
	if reliable {
		t.Fatal("missing PalDefender metadata must not be reliable")
	}
	player := players[identityKey("uid-ghost")]
	if player.online() || player.PalDefenderOnline || !player.PalDefenderLiveData || player.OnlineStateKnown {
		t.Fatalf("missing PalDefender metadata must not produce a trusted online player: %#v", player)
	}
	fallback := palDefenderPlayersForFallback(players, reliable)[identityKey("uid-ghost")]
	if fallback.online() || fallback.PalDefenderLiveData || coordinatesAvailable(fallback.Location) {
		t.Fatalf("missing metadata must disable PalDefender fallback: %#v", fallback)
	}
}

func TestOnlinePlayersFromPalDefenderRejectsMetaOnlineCountWithoutOnlineRows(t *testing.T) {
	players, reliable := onlinePlayersFromPalDefender(paldefender.RESTPlayersResponse{
		Meta: paldefender.RESTPlayersMeta{PlayerCount: 2, OnlineCount: 1},
		Players: []paldefender.RESTPlayer{
			{Name: "One", PlayerUID: "uid-one", Status: "Offline"},
			{Name: "Two", PlayerUID: "uid-two", Status: "Offline"},
		},
	})
	if reliable {
		t.Fatal("PalDefender Meta online count without matching online rows must be unreliable")
	}
	for _, player := range palDefenderPlayersForFallback(players, reliable) {
		if player.online() || player.PalDefenderOnline || player.PalDefenderLiveData {
			t.Fatalf("unreliable fallback retained a live signal: %#v", player)
		}
	}
}

func TestMergeMapOnlinePlayersRejectsUntrustedPalDefenderFallback(t *testing.T) {
	online := onlinePlayersResult{
		Players:   map[string]onlinePlayer{},
		Available: false,
		Source:    "palworld_rest+paldefender",
		Stale:     true,
	}
	defender := map[string]onlinePlayer{
		identityKey("uid-ghost"): {
			PlayerUID:           "uid-ghost",
			OnlineStateKnown:    false,
			PalDefenderLiveData: true,
			Location:            saveindex.Coordinates{X: 2},
		},
	}

	merged, source, available := mergeMapOnlinePlayers(online, defender, true, false)
	if available || source != online.Source {
		t.Fatalf("untrusted PalDefender fallback must not be advertised as live: source=%q available=%v", source, available)
	}
	if player := merged.Players[identityKey("uid-ghost")]; player.online() || coordinatesAvailable(player.Location) {
		t.Fatalf("untrusted map fallback must remain offline: %#v", player)
	}
}

func TestBuildMapEntitiesPreservesStaticEntities(t *testing.T) {
	index := saveindex.Index{
		Players: []saveindex.Player{{PlayerUID: "player", Nickname: "玩家", Location: saveindex.Coordinates{X: 1}}},
		Bases:   []saveindex.Base{{ID: "base", Name: "基地", Location: saveindex.Coordinates{X: 2}}},
		Pals:    []saveindex.Pal{{InstanceID: "pal", CharacterID: "PinkCat", Level: 17, LocationType: "base", Status: "working", Location: saveindex.Coordinates{X: 3}}},
		MapEntities: []saveindex.MapEntity{{
			Type: "map_object", ID: "object", Label: "PalBoxV2", Location: saveindex.Coordinates{X: 4},
		}},
	}
	entities := buildMapEntities(index, nil)
	if len(entities) != 4 {
		t.Fatalf("static map entities were lost: %#v", entities)
	}
	for _, entity := range entities {
		if entity["source"] != "save" {
			t.Fatalf("unexpected static entity source: %#v", entity)
		}
		if entity["type"] == "pal" {
			if entity["live"] != false || entity["level"] != 17 || entity["location_type"] != "base" || entity["status"] != "working" {
				t.Fatalf("pal snapshot metadata was lost: %#v", entity)
			}
		}
	}
	if countPalMapEntities(entities) != 1 {
		t.Fatalf("pal map entity count = %d, want 1", countPalMapEntities(entities))
	}
}

func TestSaveIndexQueryHelpersAndFilters(t *testing.T) {
	if got := stringFromAny("  ", json.Number("42")); got != "42" {
		t.Fatalf("stringFromAny json number = %q", got)
	}
	if got := stringFromAny(float64(12.5)); got != "12.5" {
		t.Fatalf("stringFromAny float = %q", got)
	}
	if got := numberDefault("bad", float32(2.5)); got != 2.5 {
		t.Fatalf("numberDefault = %v", got)
	}
	for _, test := range []struct {
		value any
		want  float64
		ok    bool
	}{
		{float64(1), 1, true}, {float32(2), 2, true}, {3, 3, true}, {int64(4), 4, true},
		{json.Number("5.5"), 5.5, true}, {" 6.5 ", 6.5, true}, {"bad", 0, false}, {struct{}{}, 0, false},
	} {
		got, ok := numberFromAny(test.value)
		if got != test.want || ok != test.ok {
			t.Errorf("numberFromAny(%#v) = %v, %v", test.value, got, ok)
		}
	}

	newQueryContext := func(target string) *gin.Context {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest("GET", target, nil)
		return context
	}
	context := newQueryContext("/api/players?limit=2&page=3&offset=-1")
	if limit, offset := limitOffset(context); limit != 2 || offset != 0 {
		t.Fatalf("limitOffset explicit offset = %d, %d", limit, offset)
	}
	context = newQueryContext("/api/players?limit=2&page=3")
	if limit, offset := limitOffset(context); limit != 2 || offset != 4 {
		t.Fatalf("limitOffset page = %d, %d", limit, offset)
	}
	page, meta := paginate([]int{1, 2, 3, 4, 5}, 2, 2)
	if len(page) != 2 || page[0] != 3 || meta["page"] != 2 {
		t.Fatalf("paginate = %#v %#v", page, meta)
	}
	page, meta = paginate([]int{1, 2}, 0, 99)
	if len(page) != 0 || meta["offset"] != 2 {
		t.Fatalf("paginate clamped offset = %#v %#v", page, meta)
	}

	status := statusWithOnlineState(saveindex.Status{}, onlinePlayersResult{Stale: true, Error: "offline"})
	if !status.Stale || len(status.Warnings) != 2 {
		t.Fatalf("statusWithOnlineState = %#v", status)
	}
	if got := appendUniqueString(status.Warnings, status.Warnings[0]); len(got) != 2 {
		t.Fatalf("appendUniqueString duplicated value: %#v", got)
	}

	guilds := []saveindex.Guild{{ID: "guild-1", Name: "Guild", Members: []saveindex.GuildMember{{PlayerUID: "online"}, {PlayerUID: "offline"}}}}
	applyGuildOnlineCounts(guilds, []saveindex.Player{{PlayerUID: "online", IsOnline: true}})
	if guilds[0].OnlineMemberCount != 1 {
		t.Fatalf("applyGuildOnlineCounts = %#v", guilds)
	}

	steamOnly := mergeSaveAndOnline(
		[]saveindex.Player{{PlayerUID: "member-uid", SteamID: "steam_member"}},
		map[string]onlinePlayer{"member": knownOnlinePlayer(onlinePlayer{SteamID: "member"})},
	)
	steamGuild := []saveindex.Guild{{ID: "guild-steam", Members: []saveindex.GuildMember{{PlayerUID: "member-uid"}}}}
	applyGuildOnlineCounts(steamGuild, steamOnly)
	if steamGuild[0].OnlineMemberCount != 1 {
		t.Fatalf("SteamID-only online player was not counted through the merged identity: %#v", steamGuild)
	}

	players := []saveindex.Player{
		{PlayerUID: "uid-1", SteamID: "steam-1", Nickname: "Online", GuildID: "guild-1", GuildName: "Guild", IsOnline: true},
		{PlayerUID: "uid-2", SteamID: "steam-2", Nickname: "Offline", IsOnline: false},
	}
	context = newQueryContext("/api/players?q=online&online=true")
	if got := filterPlayers(players, context); len(got) != 1 || got[0].PlayerUID != "uid-1" {
		t.Fatalf("filterPlayers online = %#v", got)
	}
	context = newQueryContext("/api/guilds?q=guild-1")
	if got := filterGuilds(guilds, context); len(got) != 1 {
		t.Fatalf("filterGuilds = %#v", got)
	}
	bases := []saveindex.Base{{ID: "base-1", Name: "Main", GuildID: "guild-1"}, {ID: "base-2", Name: "Other", GuildID: "guild-2"}}
	context = newQueryContext("/api/bases?guild_id=guild-1&q=main")
	if got := filterBases(bases, nil, context); len(got) != 1 || got[0].ID != "base-1" {
		t.Fatalf("filterBases = %#v", got)
	}
	pals := []saveindex.Pal{
		{InstanceID: "pal-1", CharacterID: "Anubis", OwnerPlayerUID: "uid-1", GuildID: "guild-1", ContainerID: "box-1", Status: "stored"},
		{InstanceID: "pal-2", CharacterID: "PinkCat", OwnerPlayerUID: "uid-2", GuildID: "guild-2", ContainerID: "box-2", Status: "party"},
	}
	context = newQueryContext("/api/pals?q=anubis&status=stored&owner_player_uid=uid-1&guild_id=guild-1&container_id=box-1")
	if got := filterPals(pals, players, context); len(got) != 1 || got[0].InstanceID != "pal-1" {
		t.Fatalf("filterPals = %#v", got)
	}
}

func TestOfflineIdentityPlayersClearsLiveSignals(t *testing.T) {
	ping := 35.0
	original := map[string]onlinePlayer{
		"player": {
			PlayerUID:         "player",
			GMUserID:          "gm-player",
			OnlineStateKnown:  true,
			IsOnline:          true,
			RESTOnline:        true,
			PalDefenderOnline: true,
			Location:          saveindex.Coordinates{X: 1, Y: 2, Z: 3},
			Ping:              &ping,
			IP:                "203.0.113.10",
		},
	}
	stale := offlineIdentityPlayers(original)
	player := stale["player"]
	if player.online() || player.RESTOnline || player.PalDefenderOnline {
		t.Fatalf("stale record retained an online signal: %#v", player)
	}
	if coordinatesAvailable(player.Location) || player.Ping != nil || player.IP != "" {
		t.Fatalf("stale record retained live-only fields: %#v", player)
	}
	if player.GMUserID != "gm-player" || !original["player"].online() {
		t.Fatalf("stale conversion lost identity or mutated input: stale=%#v original=%#v", player, original["player"])
	}
}

func TestPalDefenderMapPlayersCachesFailureCooldown(t *testing.T) {
	root := t.TempDir()
	cfg := appconfig.Config{
		DataDir:   root,
		ServerDir: filepath.Join(root, "server"),
		DBPath:    filepath.Join(root, "panel.db"),
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	server := Server{
		cache:    newTTLCache(),
		defender: paldefender.NewManager(cfg, store),
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/api/players", nil)

	if players, available, reliable := server.palDefenderMapPlayers(context); available || reliable || len(players) != 0 {
		t.Fatalf("unavailable PalDefender should degrade to an empty source: %#v available=%v reliable=%v", players, available, reliable)
	}
	key := cacheKey(cacheKeySavePrefix, "paldefender-map-players")
	_, status, ok := server.cache.Get(key)
	if !ok || status != cacheStatusHit {
		t.Fatalf("PalDefender failure was not cached for cooldown: status=%s ok=%v", status, ok)
	}
	if players, available, reliable := server.palDefenderMapPlayers(context); available || reliable || len(players) != 0 {
		t.Fatalf("cached PalDefender failure should remain unavailable: %#v available=%v reliable=%v", players, available, reliable)
	}
}
