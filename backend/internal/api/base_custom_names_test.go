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

func TestFlattenAndFilterBaseCustomNames(t *testing.T) {
	base := saveindex.Base{ID: "base-1", Name: "新規生成拠点テンプレート名0(仮)", GuildName: "Guild"}
	view := flattenBaseWithCustomName(base, "北境制造中心")
	if view["name"] != "北境制造中心" || view["raw_name"] != base.Name {
		t.Fatalf("custom base name view = %#v", view)
	}
	if view["custom_name"] != "北境制造中心" || view["has_custom_name"] != true {
		t.Fatalf("custom base name metadata = %#v", view)
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/bases?q=北境", nil)
	got := filterBases([]saveindex.Base{base}, map[string]string{base.ID: "北境制造中心"}, context)
	if len(got) != 1 || got[0].ID != base.ID {
		t.Fatalf("custom base name should be searchable: %#v", got)
	}
}

func TestBaseCustomNamesAreIsolatedBySaveSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	server := Server{store: store}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("PUT", "/api/bases/base-1/name", nil)

	if err := server.saveBaseCustomNames(context, "server", map[string]string{"base-1": "主世界基地"}); err != nil {
		t.Fatalf("save server names: %v", err)
	}
	if err := server.saveBaseCustomNames(context, "imported-world", map[string]string{"base-1": "导入世界基地"}); err != nil {
		t.Fatalf("save imported names: %v", err)
	}

	serverNames, err := server.loadBaseCustomNames(context, "server")
	if err != nil || serverNames["base-1"] != "主世界基地" {
		t.Fatalf("server names = %#v, %v", serverNames, err)
	}
	importedNames, err := server.loadBaseCustomNames(context, "imported-world")
	if err != nil || importedNames["base-1"] != "导入世界基地" {
		t.Fatalf("imported names = %#v, %v", importedNames, err)
	}
}

func TestBaseCustomNamesDeleteEmptyCollection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	server := Server{store: store}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("DELETE", "/api/bases/base-1/name", nil)
	if err := server.saveBaseCustomNames(context, "server", map[string]string{"base-1": "Alias"}); err != nil {
		t.Fatalf("save names: %v", err)
	}
	if err := server.saveBaseCustomNames(context, "server", map[string]string{}); err != nil {
		t.Fatalf("delete names: %v", err)
	}
	if _, found, err := store.GetKV(context.Request.Context(), baseCustomNamesKey("server")); err != nil || found {
		t.Fatalf("empty alias collection must delete KV entry: found=%v err=%v", found, err)
	}
}

func TestBaseCustomNameValidationStopsBeforeStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	server := Server{}
	router.PUT("/api/bases/:id/name", server.putBaseCustomName)

	for _, body := range []string{`{"name":"   "}`, `{"name":"` + strings.Repeat("界", 65) + `"}`} {
		request := httptest.NewRequest("PUT", "/api/bases/base-1/name", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid name returned %d: %s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestBaseCustomNameRoutesRequireServerControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalKey, Principal{Name: "viewer", Role: RoleViewer})
		c.Next()
	})
	server := Server{}
	router.PUT("/api/bases/:id/name", Require(PermServerControl), server.putBaseCustomName)
	router.DELETE("/api/bases/:id/name", Require(PermServerControl), server.deleteBaseCustomName)

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		request := httptest.NewRequest(method, "/api/bases/base-1/name", strings.NewReader(`{"name":"Alias"}`))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s base name route returned %d: %s", method, recorder.Code, recorder.Body.String())
		}
	}
}
