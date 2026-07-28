package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"palpanel/internal/db"
	"palpanel/internal/saveindex"
)

func TestNormalizePlayerAnnotation(t *testing.T) {
	annotation, err := normalizePlayerAnnotation(playerAnnotationInput{
		Note: "  重点观察\r\n曾帮助新人  ",
		Tags: []string{"管理员", "活跃", "管理员", "  "},
	})
	if err != nil {
		t.Fatalf("normalizePlayerAnnotation: %v", err)
	}
	if annotation.Note != "重点观察\n曾帮助新人" {
		t.Fatalf("note = %q", annotation.Note)
	}
	if len(annotation.Tags) != 2 || annotation.Tags[0] != "管理员" || annotation.Tags[1] != "活跃" {
		t.Fatalf("tags = %#v", annotation.Tags)
	}

	if _, err := normalizePlayerAnnotation(playerAnnotationInput{Note: strings.Repeat("界", playerNoteMaxRunes+1)}); err == nil {
		t.Fatal("oversized note should fail")
	}
	if _, err := normalizePlayerAnnotation(playerAnnotationInput{Tags: []string{strings.Repeat("界", playerTagMaxRunes+1)}}); err == nil {
		t.Fatal("oversized tag should fail")
	}
	if _, err := normalizePlayerAnnotation(playerAnnotationInput{Tags: make([]string, playerTagMaxCount+1)}); err == nil {
		t.Fatal("too many tags should fail")
	}
}

func TestPlayerAnnotationsAreIsolatedBySaveSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	server := Server{store: store}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPut, "/api/players/uid-1/annotation", nil)
	serverAnnotation := map[string]playerAnnotation{
		"uid-1": {Note: "主世界玩家", Tags: []string{"活跃"}, UpdatedAt: "2026-07-24T00:00:00Z"},
	}
	importAnnotation := map[string]playerAnnotation{
		"uid-1": {Note: "导入存档玩家", Tags: []string{"历史"}, UpdatedAt: "2026-07-24T01:00:00Z"},
	}
	if err := server.savePlayerAnnotations(context, "server", serverAnnotation); err != nil {
		t.Fatalf("save server annotations: %v", err)
	}
	if err := server.savePlayerAnnotations(context, "import-1", importAnnotation); err != nil {
		t.Fatalf("save import annotations: %v", err)
	}

	gotServer, err := server.loadPlayerAnnotations(context, "server")
	if err != nil || gotServer["uid-1"].Note != "主世界玩家" {
		t.Fatalf("server annotations = %#v, %v", gotServer, err)
	}
	gotImport, err := server.loadPlayerAnnotations(context, "import-1")
	if err != nil || gotImport["uid-1"].Note != "导入存档玩家" {
		t.Fatalf("import annotations = %#v, %v", gotImport, err)
	}
}

func TestPlayerAnnotationFlattenAndSearch(t *testing.T) {
	player := saveindex.Player{PlayerUID: "uid-1", SteamID: "steam-1", Nickname: "Alice", GuildName: "Guild"}
	annotation := playerAnnotation{Note: "负责建筑规划", Tags: []string{"建筑师", "活跃"}, UpdatedAt: "2026-07-24T00:00:00Z"}
	view := flattenPlayerWithAnnotation(player, onlinePlayersResult{}, annotation)
	if view["note"] != annotation.Note || view["has_annotation"] != true || view["annotation_updated_at"] != annotation.UpdatedAt {
		t.Fatalf("annotation view = %#v", view)
	}

	gin.SetMode(gin.TestMode)
	for _, query := range []string{"建筑规划", "建筑师"} {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "/api/players?q="+query, nil)
		got := filterPlayersWithAnnotations([]saveindex.Player{player}, map[string]playerAnnotation{"uid-1": annotation}, context)
		if len(got) != 1 {
			t.Fatalf("query %q should match annotation", query)
		}
	}
}

func TestPlayerAnnotationTargetIncludesOnlineOnlyPlayer(t *testing.T) {
	online := onlinePlayersResult{Players: map[string]onlinePlayer{
		normalizedPlayerKey("steam-online"): {
			SteamID:          "steam-online",
			Nickname:         "Online Only",
			OnlineStateKnown: true,
			IsOnline:         true,
			RESTOnline:       true,
		},
	}}
	players := playersForView(nil, online, true)
	player, found := findPlayerAnnotationTarget("steam-online", players)
	if !found {
		t.Fatal("online-only player must be available to annotation routes")
	}
	if player.SteamID != "steam-online" || !player.IsOnline {
		t.Fatalf("resolved player = %#v", player)
	}
}

func TestPlayerAnnotationRoutesRequirePlayersWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalKey, Principal{Name: "viewer", Role: RoleViewer})
		c.Next()
	})
	server := Server{}
	router.PUT("/api/players/:id/annotation", Require(PermPlayersWrite), server.putPlayerAnnotation)
	router.DELETE("/api/players/:id/annotation", Require(PermPlayersWrite), server.deletePlayerAnnotation)

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		request := httptest.NewRequest(method, "/api/players/uid-1/annotation", strings.NewReader(`{"note":"test","tags":["tag"]}`))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s player annotation route returned %d: %s", method, recorder.Code, recorder.Body.String())
		}
	}
}

func TestPlayerAnnotationsDeleteEmptyCollection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	server := Server{store: store}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodDelete, "/api/players/uid-1/annotation", nil)
	if err := server.savePlayerAnnotations(context, "server", map[string]playerAnnotation{"uid-1": {Note: "test"}}); err != nil {
		t.Fatalf("save annotations: %v", err)
	}
	if err := server.savePlayerAnnotations(context, "server", map[string]playerAnnotation{}); err != nil {
		t.Fatalf("delete annotations: %v", err)
	}
	if _, found, err := store.GetKV(context.Request.Context(), playerAnnotationsKey("server")); err != nil || found {
		t.Fatalf("empty annotation collection must delete KV entry: found=%v err=%v", found, err)
	}
}
