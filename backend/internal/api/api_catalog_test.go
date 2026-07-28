package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAPICatalogListsOnlyAPIRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/api/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/api/catalog", (Server{}).apiCatalog(router))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/catalog", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Count  int               `json:"count"`
			Routes []apiCatalogRoute `json:"routes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Data.Count != 2 || len(response.Data.Routes) != 2 {
		t.Fatalf("unexpected catalog: %+v", response)
	}
	if response.Data.Routes[0].Path != "/api/catalog" || response.Data.Routes[1].Path != "/api/health" {
		t.Fatalf("routes are not stable/sorted: %+v", response.Data.Routes)
	}
	if !response.Data.Routes[0].Patched || response.Data.Routes[0].Summary == "" {
		t.Fatalf("catalog route lacks documentation: %+v", response.Data.Routes[0])
	}
}

func TestAPICatalogCustomRouteDescriptions(t *testing.T) {
	for _, key := range []string{
		"GET /api/patch/info",
		"GET /api/catalog",
		"GET /api/inventory",
		"GET /api/pals",
		"PUT /api/security/paldefender/starter-gift",
		"POST /api/patch/update",
	} {
		descriptor, found := apiCatalogExact[key]
		if !found || descriptor.Summary == "" || descriptor.Description == "" || descriptor.Permission == "" || !descriptor.Patched {
			t.Fatalf("missing complete descriptor for %s: %+v", key, descriptor)
		}
	}
}

func TestAPIAuthenticationClassification(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/api/patch/info", "public"},
		{http.MethodGet, "/api/catalog", "session-or-development-key"},
		{http.MethodGet, "/api/breed/catalog", "breed-session"},
		{http.MethodPost, "/api/integrations/astrbot/server-status", "astrbot-signature"},
	}
	for _, tc := range cases {
		if got := apiAuthentication(tc.method, tc.path); got != tc.want {
			t.Fatalf("%s %s: got %q want %q", tc.method, tc.path, got, tc.want)
		}
	}
}
