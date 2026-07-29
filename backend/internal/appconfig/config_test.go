package appconfig

import (
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestLoadRequiresAuthenticationByDefault(t *testing.T) {
	t.Setenv("PALPANEL_REQUIRE_AUTH", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.RequireAuth {
		t.Fatal("expected authentication to be enabled by default")
	}
}

func TestLoadAllowsExplicitDevNoAuth(t *testing.T) {
	t.Setenv("PALPANEL_REQUIRE_AUTH", "false")
	t.Setenv("STEAM_WEB_API_KEY", "steam-key")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PALPANEL_DATA_DIR", cwd)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.RequireAuth {
		t.Fatal("expected auth to be disabled")
	}
	if cfg.SteamWebAPIKey != "steam-key" {
		t.Fatalf("SteamWebAPIKey = %q", cfg.SteamWebAPIKey)
	}
	if cfg.SteamWebAPIKeySource != "environment" {
		t.Fatalf("SteamWebAPIKeySource = %q", cfg.SteamWebAPIKeySource)
	}
	if cfg.WorkshopAppID != "1623730" {
		t.Fatalf("WorkshopAppID = %q", cfg.WorkshopAppID)
	}
	if cfg.PalDefenderRESTBaseURL != "http://127.0.0.1:17993" {
		t.Fatalf("PalDefenderRESTBaseURL = %q", cfg.PalDefenderRESTBaseURL)
	}
	if cfg.PalDefenderRESTPort != 17993 {
		t.Fatalf("PalDefenderRESTPort = %d", cfg.PalDefenderRESTPort)
	}
	if cfg.RCONPort != DefaultRCONPort {
		t.Fatalf("RCONPort = %d", cfg.RCONPort)
	}
	if cfg.EffectiveRCONHost() != "127.0.0.1" {
		t.Fatalf("RCONHost = %q", cfg.EffectiveRCONHost())
	}
}

func TestLoadAllowsContainerRCONHost(t *testing.T) {
	t.Setenv("PALPANEL_REQUIRE_AUTH", "false")
	t.Setenv("PALPANEL_RCON_HOST", "palworld-server")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveRCONHost() != "palworld-server" {
		t.Fatalf("RCONHost = %q", cfg.EffectiveRCONHost())
	}
}

func TestLoadUsesPalDefenderRESTOverrides(t *testing.T) {
	t.Setenv("PALPANEL_REQUIRE_AUTH", "false")
	t.Setenv("PALPANEL_PALDEFENDER_REST_BASE_URL", "http://10.0.0.4:28080/")
	t.Setenv("PALPANEL_PALDEFENDER_REST_PORT", "28080")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PALPANEL_DATA_DIR", cwd)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.PalDefenderRESTBaseURL != "http://10.0.0.4:28080/" {
		t.Fatalf("PalDefenderRESTBaseURL = %q", cfg.PalDefenderRESTBaseURL)
	}
	if cfg.EffectivePalDefenderRESTBaseURL() != "http://10.0.0.4:28080" {
		t.Fatalf("EffectivePalDefenderRESTBaseURL = %q", cfg.EffectivePalDefenderRESTBaseURL())
	}
	if cfg.PalDefenderRESTPort != 28080 {
		t.Fatalf("PalDefenderRESTPort = %d", cfg.PalDefenderRESTPort)
	}
}

func TestLoadDerivesPalworldRESTURLFromConfiguredPort(t *testing.T) {
	t.Setenv("PALPANEL_REQUIRE_AUTH", "false")
	t.Setenv("PALPANEL_REST_PORT", "18212")
	t.Setenv("PALWORLD_REST_BASE_URL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RESTPort != 18212 || cfg.PalworldRESTBaseURL != "http://127.0.0.1:18212/v1/api" {
		t.Fatalf("REST configuration = port %d, base URL %q", cfg.RESTPort, cfg.PalworldRESTBaseURL)
	}
}

func TestLoadUsesBundledSteamWebAPIKeyWhenEnvUnset(t *testing.T) {
	t.Setenv("PALPANEL_REQUIRE_AUTH", "false")
	t.Setenv("STEAM_WEB_API_KEY", "")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PALPANEL_DATA_DIR", cwd)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.SteamWebAPIKeySource != "bundled" || cfg.SteamWebAPIKeySourceName() != "bundled" {
		t.Fatalf("SteamWebAPIKeySource = %q", cfg.SteamWebAPIKeySource)
	}
	if !cfg.SteamWebAPIKeyConfigured() || len(cfg.EffectiveSteamWebAPIKey()) != 32 {
		t.Fatal("bundled Steam Web API key should be configured")
	}
	if _, err := hex.DecodeString(cfg.EffectiveSteamWebAPIKey()); err != nil {
		t.Fatalf("bundled Steam Web API key is not hexadecimal: %v", err)
	}
}

func TestLoadUsesProductionNetworkAndProviderDefaults(t *testing.T) {
	t.Setenv("PALPANEL_REQUIRE_AUTH", "false")
	t.Setenv("PALPANEL_LISTEN_ADDR", "")
	t.Setenv("PALPANEL_STEAM_API_BASE_URL", "")
	t.Setenv("PALPANEL_STEAM_API_TIMEOUT_SECONDS", "")
	t.Setenv("PALPANEL_AI_TRANSLATION_TIMEOUT_SECONDS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.SteamAPIBaseURL != DefaultSteamAPIBaseURL || cfg.SteamAPITimeoutSeconds != 15 {
		t.Fatalf("Steam defaults = %q, %d", cfg.SteamAPIBaseURL, cfg.SteamAPITimeoutSeconds)
	}
	if cfg.AITranslationTimeoutSeconds != 90 {
		t.Fatalf("AITranslationTimeoutSeconds = %d", cfg.AITranslationTimeoutSeconds)
	}
	if cfg.MonitorRetentionDays != DefaultMonitorRetentionDays {
		t.Fatalf("MonitorRetentionDays = %d", cfg.MonitorRetentionDays)
	}
	if !cfg.CommunityServersEnabled || cfg.CommunityServersAPIBaseURL != DefaultCommunityServersAPIBaseURL {
		t.Fatalf("community server defaults = enabled %v, base %q", cfg.CommunityServersEnabled, cfg.CommunityServersAPIBaseURL)
	}
	if cfg.CommunityServersCacheTTL != 60 || cfg.CommunityServersStaleTTL != 86400 || cfg.CommunityServersRateLimit != 30 {
		t.Fatalf("community server cache defaults = %d, %d, %d", cfg.CommunityServersCacheTTL, cfg.CommunityServersStaleTTL, cfg.CommunityServersRateLimit)
	}
}

func TestLoadRejectsInvalidScalarConfiguration(t *testing.T) {
	tests := map[string]string{
		"PALPANEL_REQUIRE_AUTH":                        "sometimes",
		"PALPANEL_STEAM_API_TIMEOUT_SECONDS":           "soon",
		"PALPANEL_AI_TRANSLATION_TIMEOUT_SECONDS":      "0",
		"PALPANEL_RCON_PORT":                           "70000",
		"PALPANEL_LISTEN_ADDR":                         "127.0.0.1:not-a-port",
		"PALPANEL_MONITOR_RETENTION_DAYS":              "-1",
		"PALPANEL_RCON_HOST":                           "bad host:25575",
		"PALPANEL_COMMUNITY_SERVERS_ENABLED":           "sometimes",
		"PALPANEL_COMMUNITY_SERVERS_CACHE_TTL_SECONDS": "0",
		"PALPANEL_COMMUNITY_SERVERS_STALE_TTL_SECONDS": "1",
		"PALPANEL_COMMUNITY_SERVERS_RATE_LIMIT":        "61",
		"PALPANEL_COMMUNITY_SERVERS_PROXY_URL":         "ftp://proxy.example",
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PALPANEL_REQUIRE_AUTH", "false")
			t.Setenv(name, value)
			if _, err := Load(); err == nil {
				t.Fatalf("expected %s=%q to fail", name, value)
			}
		})
	}
}

func TestLoadAcceptsCommunityServerSOCKS5HProxy(t *testing.T) {
	t.Setenv("PALPANEL_REQUIRE_AUTH", "false")
	t.Setenv("PALPANEL_COMMUNITY_SERVERS_PROXY_URL", "socks5h://127.0.0.1:10808")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CommunityServersProxyURL != "socks5h://127.0.0.1:10808" {
		t.Fatalf("proxy URL = %q", cfg.CommunityServersProxyURL)
	}
}

func TestValidateHTTPBaseURLAllowsPublicHTTPAndHTTPS(t *testing.T) {
	for _, raw := range []string{
		"http://example.com/api",
		"http://192.168.1.20:8080/api",
		"https://example.com/api",
	} {
		if err := validateHTTPBaseURL("TEST_URL", raw); err != nil {
			t.Fatalf("expected %q to be accepted: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"ftp://example.com/file",
		"https://user:pass@example.com/api",
		"https://example.com/api?token=secret",
		"/relative",
	} {
		if err := validateHTTPBaseURL("TEST_URL", raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestLoadValidatesIncidentWebhookConfiguration(t *testing.T) {
	t.Setenv("PALPANEL_REQUIRE_AUTH", "false")
	t.Setenv("PALPANEL_INCIDENT_WEBHOOK_URL", "http://127.0.0.1:18090/hook")
	t.Setenv("PALPANEL_INCIDENT_WEBHOOK_SECRET", "0123456789abcdef")
	t.Setenv("PALPANEL_INCIDENT_WEBHOOK_TIMEOUT_SECONDS", "7")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IncidentWebhookURL != "http://127.0.0.1:18090/hook" || cfg.IncidentWebhookTimeoutSeconds != 7 {
		t.Fatalf("webhook config = %#v", cfg)
	}
}

func TestLoadRejectsInsecureRemoteIncidentWebhook(t *testing.T) {
	t.Setenv("PALPANEL_REQUIRE_AUTH", "false")
	t.Setenv("PALPANEL_INCIDENT_WEBHOOK_URL", "http://example.com/hook")
	t.Setenv("PALPANEL_INCIDENT_WEBHOOK_SECRET", "0123456789abcdef")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("expected HTTPS validation error, got %v", err)
	}
}

func TestLoadRejectsIncidentWebhookSecretAndQueryWeakness(t *testing.T) {
	t.Setenv("PALPANEL_REQUIRE_AUTH", "false")
	t.Setenv("PALPANEL_INCIDENT_WEBHOOK_URL", "https://example.com/hook?token=secret")
	t.Setenv("PALPANEL_INCIDENT_WEBHOOK_SECRET", "short")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "query strings") {
		t.Fatalf("expected query validation error, got %v", err)
	}
	t.Setenv("PALPANEL_INCIDENT_WEBHOOK_URL", "https://example.com/hook")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "at least 16") {
		t.Fatalf("expected secret validation error, got %v", err)
	}
}
