package appconfig

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"palpanel/internal/debuglog"
)

const DefaultDockerRunnerBaseImage = "scottyhardy/docker-wine:latest@sha256:477aae36af41923cfb5eefb23923b035f8010caa49eaded952316f937dd8a49b"
const DefaultRCONPort = 25575
const DefaultPalDefenderRESTPort = 17993
const DefaultSteamAPIBaseURL = "https://api.steampowered.com"
const DefaultSteamAPITimeoutSeconds = 15
const DefaultSteamCMDDownloadURL = "https://steamcdn-a.akamaihd.net/client/installer/steamcmd.zip"
const DefaultSteamCMDDownloadMaxMB = 64
const DefaultUE4SSVersion = "v3.0.1"
const DefaultUE4SSDownloadURL = "https://github.com/UE4SS-RE/RE-UE4SS/releases/download/v3.0.1/UE4SS_v3.0.1.zip"
const DefaultUE4SSArchiveSHA256 = "4b47d4bceddd2f561a4e395bfa00924ccfc945af576a2d0c613e6537846c57ec"
const DefaultUE4SSDownloadMaxMB = 64
const DefaultAITranslationTimeoutSeconds = 90
const DefaultMonitorRetentionDays = 7
const DefaultCommunityServersAPIBaseURL = "https://api.battlemetrics.com"
const DefaultCommunityServersCacheTTLSeconds = 60
const DefaultCommunityServersStaleTTLSeconds = 86400
const DefaultCommunityServersRateLimit = 30

var DefaultDockerRunnerBaseImageMirrorPrefixes = []string{
	"docker.m.daocloud.io",
	"docker.1ms.run",
	"registry.cyou",
	"dockerproxy.net",
	"dockerproxy.link",
	"docker.jiaxin.site",
	"docker.xuanyuan.me",
	"free.hubfast.cn",
}

type Config struct {
	RuntimeRoot                   string
	RepositoryRoot                string
	DevelopmentMode               bool
	ListenAddr                    string
	DataDir                       string
	ServerDir                     string
	WinePrefixDir                 string
	ToolsDir                      string
	SteamCMDDir                   string
	UE4SSDir                      string
	UploadsDir                    string
	BackupsDir                    string
	LogsDir                       string
	DBPath                        string
	RequireAuth                   bool
	DiagnosticShellEnabled        bool
	CORSOrigins                   []string
	FrontendDist                  string
	MaxUploadBytes                int64
	DockerBinary                  string
	DockerImage                   string
	DockerContainer               string
	DockerRunnerBaseImage         string
	DockerRunnerBaseImageMirrors  []string
	SteamWebAPIKey                string
	SteamWebAPIKeySource          string
	SteamAPIBaseURL               string
	SteamAPITimeoutSeconds        int
	CommunityServersEnabled       bool
	CommunityServersAPIBaseURL    string
	CommunityServersProxyURL      string
	CommunityServersCacheTTL      int
	CommunityServersStaleTTL      int
	CommunityServersRateLimit     int
	SteamCMDDownloadURL           string
	SteamCMDDownloadMaxBytes      int64
	UE4SSVersion                  string
	UE4SSDownloadURL              string
	UE4SSArchiveSHA256            string
	UE4SSDownloadMaxBytes         int64
	WorkshopAppID                 string
	GamePort                      int
	QueryPort                     int
	RCONHost                      string
	RCONPort                      int
	RESTPort                      int
	PalworldRESTBaseURL           string
	PalworldRESTUser              string
	PalworldRESTPass              string
	PalworldRESTReadTimeoutMS     int
	PalworldGameDataTimeoutMS     int
	PalworldGameDataMaxBytes      int64
	PalDefenderRESTBaseURL        string
	PalDefenderRESTPort           int
	SaveIndexerEnabled            bool
	SaveIndexerURL                string
	SaveIndexCacheDir             string
	SaveIndexTimeoutSeconds       int
	SaveHistoryIntervalMinutes    int
	SaveSourcesDir                string
	PalCalcBridgeURL              string
	PalCalcTimeoutSeconds         int
	AstrBotPluginURL              string
	AstrBotPanelID                string
	AstrBotSharedSecret           string
	PerfSlowRequestMS             int
	MonitorRetentionDays          int
	AITranslationTimeoutSeconds   int
	IncidentWebhookURL            string
	IncidentWebhookSecret         string
	IncidentWebhookTimeoutSeconds int
	LogLevel                      string
	DebugLogger                   *debuglog.Logger
	RunnerDir                     string
	serverDirectoryState          *ServerDirectoryState
}

// ServerDirectoryState is shared by the backend managers so a validated
// Windows server import can take effect immediately without restarting the
// panel. Config values are copied into several managers, while this state is
// intentionally shared between those copies.
type ServerDirectoryState struct {
	mu       sync.RWMutex
	path     string
	imported bool
}

func newServerDirectoryState(path string, imported bool) *ServerDirectoryState {
	return &ServerDirectoryState{path: filepath.Clean(path), imported: imported}
}

// WithServerDirectoryState ensures copied Config values share a mutable server
// directory. appconfig.Load already returns an initialized state; this helper
// also keeps focused unit-test configurations usable.
func (c Config) WithServerDirectoryState() Config {
	if c.serverDirectoryState == nil {
		c.serverDirectoryState = newServerDirectoryState(c.ServerDir, false)
	}
	return c
}

func (c Config) ServerDirectory() string {
	if c.serverDirectoryState == nil {
		return filepath.Clean(c.ServerDir)
	}
	c.serverDirectoryState.mu.RLock()
	defer c.serverDirectoryState.mu.RUnlock()
	return c.serverDirectoryState.path
}

func (c Config) ServerDirectoryImported() bool {
	if c.serverDirectoryState == nil {
		return false
	}
	c.serverDirectoryState.mu.RLock()
	defer c.serverDirectoryState.mu.RUnlock()
	return c.serverDirectoryState.imported
}

func (c Config) BindImportedServerDirectory(path string) error {
	if c.serverDirectoryState == nil {
		return fmt.Errorf("server directory state is not initialized")
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return fmt.Errorf("server directory is empty")
	}
	c.serverDirectoryState.mu.Lock()
	c.serverDirectoryState.path = path
	c.serverDirectoryState.imported = true
	c.serverDirectoryState.mu.Unlock()
	return nil
}

func Load() (Config, error) {
	if err := validateScalarEnvironment(); err != nil {
		return Config{}, err
	}
	layout, err := ResolveRuntimeLayout(os.Getenv("PALPANEL_RUNTIME_ROOT"))
	if err != nil {
		return Config{}, err
	}
	root := layout.ApplicationRoot
	if layout.RepositoryRoot != "" {
		root = layout.RepositoryRoot
	}
	mutableBase := root
	dataDefault := filepath.Join(root, "data")
	serverDefault := ""
	toolsDefault := ""
	steamCMDDefault := ""
	ue4ssDefault := ""
	uploadsDefault := ""
	backupsDefault := ""
	logsDefault := ""
	dbDefault := ""
	saveIndexDefault := ""
	winePrefixDefault := ""
	if layout.Structured {
		mutableBase = layout.RuntimeRoot
		dataDefault = filepath.Join(layout.RuntimeRoot, "data")
		serverDefault = filepath.Join(layout.RuntimeRoot, "palworld")
		toolsDefault = filepath.Join(layout.RuntimeRoot, "temp")
		steamCMDDefault = filepath.Join(layout.RuntimeRoot, "steamcmd")
		ue4ssDefault = filepath.Join(layout.RuntimeRoot, "ue4ss")
		uploadsDefault = filepath.Join(layout.RuntimeRoot, "mods", "staging")
		backupsDefault = filepath.Join(dataDefault, "backups")
		logsDefault = filepath.Join(dataDefault, "logs")
		dbDefault = filepath.Join(dataDefault, "database", "palpanel.db")
		saveIndexDefault = filepath.Join(dataDefault, "save-index")
		winePrefixDefault = filepath.Join(layout.RuntimeRoot, "wineprefix")
	}
	dataDir, err := configuredPath("PALPANEL_DATA_DIR", dataDefault, mutableBase)
	if err != nil {
		return Config{}, err
	}
	if serverDefault == "" {
		serverDefault = filepath.Join(dataDir, "server")
		toolsDefault = filepath.Join(dataDir, "tools")
		steamCMDDefault = filepath.Join(dataDir, "tools", "steamcmd")
		ue4ssDefault = filepath.Join(dataDir, "tools", "ue4ss")
		uploadsDefault = filepath.Join(dataDir, "uploads")
		backupsDefault = filepath.Join(dataDir, "backups")
		logsDefault = filepath.Join(dataDir, "logs")
		dbDefault = filepath.Join(dataDir, "palpanel.db")
		saveIndexDefault = filepath.Join(dataDir, "save-index")
		winePrefixDefault = filepath.Join(dataDir, "wineprefix")
	}
	serverDir, err := configuredPath("PALPANEL_SERVER_DIR", serverDefault, mutableBase)
	if err != nil {
		return Config{}, err
	}
	winePrefixDir, err := configuredPath("PALPANEL_WINE_PREFIX_DIR", winePrefixDefault, mutableBase)
	if err != nil {
		return Config{}, err
	}
	toolsDir, err := configuredPath("PALPANEL_TOOLS_DIR", toolsDefault, mutableBase)
	if err != nil {
		return Config{}, err
	}
	steamCMDDir, err := configuredPath("PALPANEL_STEAMCMD_DIR", steamCMDDefault, mutableBase)
	if err != nil {
		return Config{}, err
	}
	ue4ssDir, err := configuredPath("PALPANEL_UE4SS_DIR", ue4ssDefault, mutableBase)
	if err != nil {
		return Config{}, err
	}
	uploadsDir, err := configuredPath("PALPANEL_UPLOADS_DIR", uploadsDefault, mutableBase)
	if err != nil {
		return Config{}, err
	}
	backupsDir, err := configuredPath("PALPANEL_BACKUPS_DIR", backupsDefault, mutableBase)
	if err != nil {
		return Config{}, err
	}
	logsDir, err := configuredPath("PALPANEL_LOGS_DIR", logsDefault, mutableBase)
	if err != nil {
		return Config{}, err
	}
	dbPath, err := configuredPath("PALPANEL_DB_PATH", dbDefault, mutableBase)
	if err != nil {
		return Config{}, err
	}
	saveIndexCacheDir, err := configuredPath("PALPANEL_SAVE_INDEX_CACHE_DIR", saveIndexDefault, mutableBase)
	if err != nil {
		return Config{}, err
	}
	backendDir, err := configuredPath("PALPANEL_BACKEND_DIR", filepath.Join(root, "backend"), root)
	if err != nil {
		return Config{}, err
	}
	frontendDist, err := configuredPath("PALPANEL_FRONTEND_DIST", filepath.Join(root, "frontend", "dist"), root)
	if err != nil {
		return Config{}, err
	}
	runnerDir, err := configuredPath("PALPANEL_RUNNER_DIR", filepath.Join(backendDir, "deployments", "wine-runner"), root)
	if err != nil {
		return Config{}, err
	}

	steamWebAPIKey, steamWebAPIKeySource := resolveSteamWebAPIKey()
	restPort := envInt("PALPANEL_REST_PORT", 8212)
	palDefenderRESTPort := envInt("PALPANEL_PALDEFENDER_REST_PORT", DefaultPalDefenderRESTPort)
	cfg := Config{
		RuntimeRoot:                   layout.RuntimeRoot,
		RepositoryRoot:                layout.RepositoryRoot,
		DevelopmentMode:               layout.Development,
		ListenAddr:                    env("PALPANEL_LISTEN_ADDR", "127.0.0.1:8080"),
		DataDir:                       dataDir,
		ServerDir:                     serverDir,
		WinePrefixDir:                 winePrefixDir,
		ToolsDir:                      toolsDir,
		SteamCMDDir:                   steamCMDDir,
		UE4SSDir:                      ue4ssDir,
		UploadsDir:                    uploadsDir,
		BackupsDir:                    backupsDir,
		LogsDir:                       logsDir,
		DBPath:                        dbPath,
		RequireAuth:                   envBool("PALPANEL_REQUIRE_AUTH", true),
		DiagnosticShellEnabled:        envBool("PALPANEL_DIAGNOSTIC_SHELL_ENABLED", false),
		CORSOrigins:                   envList("PALPANEL_CORS_ORIGINS", []string{"http://127.0.0.1:3000", "http://localhost:3000"}),
		FrontendDist:                  frontendDist,
		MaxUploadBytes:                int64(envInt("PALPANEL_MAX_UPLOAD_MB", 256)) * 1024 * 1024,
		DockerBinary:                  env("PALPANEL_DOCKER_BIN", "docker"),
		DockerImage:                   env("PALPANEL_DOCKER_IMAGE", "palworld-wine-runner:local"),
		DockerContainer:               env("PALPANEL_DOCKER_CONTAINER", "palworld-wine-server"),
		DockerRunnerBaseImage:         env("PALPANEL_DOCKER_RUNNER_BASE_IMAGE", DefaultDockerRunnerBaseImage),
		DockerRunnerBaseImageMirrors:  envList("PALPANEL_DOCKER_RUNNER_BASE_IMAGE_MIRRORS", DefaultDockerRunnerBaseImageMirrorPrefixes),
		SteamWebAPIKey:                steamWebAPIKey,
		SteamWebAPIKeySource:          steamWebAPIKeySource,
		SteamAPIBaseURL:               strings.TrimRight(env("PALPANEL_STEAM_API_BASE_URL", DefaultSteamAPIBaseURL), "/"),
		SteamAPITimeoutSeconds:        envInt("PALPANEL_STEAM_API_TIMEOUT_SECONDS", DefaultSteamAPITimeoutSeconds),
		CommunityServersEnabled:       envBool("PALPANEL_COMMUNITY_SERVERS_ENABLED", true),
		CommunityServersAPIBaseURL:    strings.TrimRight(env("PALPANEL_COMMUNITY_SERVERS_API_BASE_URL", DefaultCommunityServersAPIBaseURL), "/"),
		CommunityServersProxyURL:      strings.TrimSpace(os.Getenv("PALPANEL_COMMUNITY_SERVERS_PROXY_URL")),
		CommunityServersCacheTTL:      envInt("PALPANEL_COMMUNITY_SERVERS_CACHE_TTL_SECONDS", DefaultCommunityServersCacheTTLSeconds),
		CommunityServersStaleTTL:      envInt("PALPANEL_COMMUNITY_SERVERS_STALE_TTL_SECONDS", DefaultCommunityServersStaleTTLSeconds),
		CommunityServersRateLimit:     envInt("PALPANEL_COMMUNITY_SERVERS_RATE_LIMIT", DefaultCommunityServersRateLimit),
		SteamCMDDownloadURL:           strings.TrimSpace(env("PALPANEL_STEAMCMD_DOWNLOAD_URL", DefaultSteamCMDDownloadURL)),
		SteamCMDDownloadMaxBytes:      int64(envInt("PALPANEL_STEAMCMD_DOWNLOAD_MAX_MB", DefaultSteamCMDDownloadMaxMB)) * 1024 * 1024,
		UE4SSVersion:                  strings.TrimSpace(env("PALPANEL_UE4SS_VERSION", DefaultUE4SSVersion)),
		UE4SSDownloadURL:              strings.TrimSpace(env("PALPANEL_UE4SS_DOWNLOAD_URL", DefaultUE4SSDownloadURL)),
		UE4SSArchiveSHA256:            strings.ToLower(strings.TrimSpace(env("PALPANEL_UE4SS_ARCHIVE_SHA256", DefaultUE4SSArchiveSHA256))),
		UE4SSDownloadMaxBytes:         int64(envInt("PALPANEL_UE4SS_DOWNLOAD_MAX_MB", DefaultUE4SSDownloadMaxMB)) * 1024 * 1024,
		WorkshopAppID:                 env("PALPANEL_WORKSHOP_APP_ID", "1623730"),
		GamePort:                      envInt("PALPANEL_GAME_PORT", 8211),
		QueryPort:                     envInt("PALPANEL_QUERY_PORT", 27015),
		RCONHost:                      strings.TrimSpace(env("PALPANEL_RCON_HOST", "127.0.0.1")),
		RCONPort:                      envInt("PALPANEL_RCON_PORT", DefaultRCONPort),
		RESTPort:                      restPort,
		PalworldRESTBaseURL:           env("PALWORLD_REST_BASE_URL", fmt.Sprintf("http://127.0.0.1:%d/v1/api", restPort)),
		PalworldRESTUser:              env("PALWORLD_REST_USER", "admin"),
		PalworldRESTPass:              env("PALWORLD_ADMIN_PASSWORD", ""),
		PalworldRESTReadTimeoutMS:     envInt("PALPANEL_PALWORLD_REST_READ_TIMEOUT_MS", 1200),
		PalworldGameDataTimeoutMS:     envInt("PALPANEL_GAME_DATA_TIMEOUT_MS", 3000),
		PalworldGameDataMaxBytes:      int64(envInt("PALPANEL_GAME_DATA_MAX_MB", 16)) * 1024 * 1024,
		PalDefenderRESTBaseURL:        env("PALPANEL_PALDEFENDER_REST_BASE_URL", fmt.Sprintf("http://127.0.0.1:%d", palDefenderRESTPort)),
		PalDefenderRESTPort:           palDefenderRESTPort,
		SaveIndexerEnabled:            envBool("PALPANEL_SAVE_INDEXER_ENABLED", false),
		SaveIndexerURL:                env("PALPANEL_SAVE_INDEXER_URL", "http://127.0.0.1:8090"),
		SaveIndexCacheDir:             saveIndexCacheDir,
		SaveIndexTimeoutSeconds:       envInt("PALPANEL_SAVE_INDEX_TIMEOUT_SECONDS", 120),
		SaveHistoryIntervalMinutes:    envInt("PALPANEL_SAVE_HISTORY_INTERVAL_MINUTES", 15),
		SaveSourcesDir:                filepath.Join(dataDir, "save-sources"),
		PalCalcBridgeURL:              strings.TrimRight(env("PALPANEL_PALCALC_URL", "http://127.0.0.1:8091"), "/"),
		PalCalcTimeoutSeconds:         envInt("PALPANEL_PALCALC_TIMEOUT_SECONDS", 300),
		AstrBotPluginURL:              strings.TrimRight(env("PALPANEL_ASTRBOT_PLUGIN_URL", "http://127.0.0.1:8092"), "/"),
		AstrBotPanelID:                env("PALPANEL_ASTRBOT_PANEL_ID", "palpanel"),
		AstrBotSharedSecret:           strings.TrimSpace(os.Getenv("PALPANEL_ASTRBOT_SHARED_SECRET")),
		PerfSlowRequestMS:             envInt("PALPANEL_PERF_SLOW_REQUEST_MS", 500),
		MonitorRetentionDays:          envInt("PALPANEL_MONITOR_RETENTION_DAYS", DefaultMonitorRetentionDays),
		AITranslationTimeoutSeconds:   envInt("PALPANEL_AI_TRANSLATION_TIMEOUT_SECONDS", DefaultAITranslationTimeoutSeconds),
		IncidentWebhookURL:            strings.TrimSpace(os.Getenv("PALPANEL_INCIDENT_WEBHOOK_URL")),
		IncidentWebhookSecret:         strings.TrimSpace(os.Getenv("PALPANEL_INCIDENT_WEBHOOK_SECRET")),
		IncidentWebhookTimeoutSeconds: envInt("PALPANEL_INCIDENT_WEBHOOK_TIMEOUT_SECONDS", 10),
		LogLevel:                      strings.ToLower(env("PALPANEL_LOG_LEVEL", "info")),
		RunnerDir:                     runnerDir,
	}
	cfg.serverDirectoryState = newServerDirectoryState(serverDir, false)
	if err := validateListenAddress(cfg.ListenAddr); err != nil {
		return Config{}, err
	}
	if err := validateHTTPBaseURL("PALPANEL_STEAM_API_BASE_URL", cfg.SteamAPIBaseURL); err != nil {
		return Config{}, err
	}
	if cfg.SteamAPITimeoutSeconds < 1 || cfg.SteamAPITimeoutSeconds > 300 {
		return Config{}, fmt.Errorf("PALPANEL_STEAM_API_TIMEOUT_SECONDS must be between 1 and 300")
	}
	if err := validateHTTPBaseURL("PALPANEL_COMMUNITY_SERVERS_API_BASE_URL", cfg.CommunityServersAPIBaseURL); err != nil {
		return Config{}, err
	}
	if err := validateProxyURL("PALPANEL_COMMUNITY_SERVERS_PROXY_URL", cfg.CommunityServersProxyURL); err != nil {
		return Config{}, err
	}
	if cfg.CommunityServersCacheTTL < 5 || cfg.CommunityServersCacheTTL > 3600 {
		return Config{}, fmt.Errorf("PALPANEL_COMMUNITY_SERVERS_CACHE_TTL_SECONDS must be between 5 and 3600")
	}
	if cfg.CommunityServersStaleTTL < cfg.CommunityServersCacheTTL || cfg.CommunityServersStaleTTL > 7*24*60*60 {
		return Config{}, fmt.Errorf("PALPANEL_COMMUNITY_SERVERS_STALE_TTL_SECONDS must be between the cache TTL and 604800")
	}
	if cfg.CommunityServersRateLimit < 1 || cfg.CommunityServersRateLimit > 60 {
		return Config{}, fmt.Errorf("PALPANEL_COMMUNITY_SERVERS_RATE_LIMIT must be between 1 and 60")
	}
	if err := validateHTTPBaseURL("PALPANEL_STEAMCMD_DOWNLOAD_URL", cfg.SteamCMDDownloadURL); err != nil {
		return Config{}, err
	}
	if cfg.SteamCMDDownloadMaxBytes < 1*1024*1024 || cfg.SteamCMDDownloadMaxBytes > 1024*1024*1024 {
		return Config{}, fmt.Errorf("PALPANEL_STEAMCMD_DOWNLOAD_MAX_MB must be between 1 and 1024")
	}
	if err := validateHTTPBaseURL("PALPANEL_UE4SS_DOWNLOAD_URL", cfg.UE4SSDownloadURL); err != nil {
		return Config{}, err
	}
	if cfg.UE4SSVersion == "" {
		return Config{}, fmt.Errorf("PALPANEL_UE4SS_VERSION must not be empty")
	}
	if len(cfg.UE4SSArchiveSHA256) != 64 {
		return Config{}, fmt.Errorf("PALPANEL_UE4SS_ARCHIVE_SHA256 must contain 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(cfg.UE4SSArchiveSHA256); err != nil {
		return Config{}, fmt.Errorf("PALPANEL_UE4SS_ARCHIVE_SHA256 must be hexadecimal")
	}
	if cfg.UE4SSDownloadMaxBytes < 1*1024*1024 || cfg.UE4SSDownloadMaxBytes > 1024*1024*1024 {
		return Config{}, fmt.Errorf("PALPANEL_UE4SS_DOWNLOAD_MAX_MB must be between 1 and 1024")
	}
	if cfg.AITranslationTimeoutSeconds < 1 || cfg.AITranslationTimeoutSeconds > 600 {
		return Config{}, fmt.Errorf("PALPANEL_AI_TRANSLATION_TIMEOUT_SECONDS must be between 1 and 600")
	}
	if cfg.SaveHistoryIntervalMinutes < 1 || cfg.SaveHistoryIntervalMinutes > 24*60 {
		return Config{}, fmt.Errorf("PALPANEL_SAVE_HISTORY_INTERVAL_MINUTES must be between 1 and 1440")
	}
	if cfg.MonitorRetentionDays < 0 || cfg.MonitorRetentionDays > 3650 {
		return Config{}, fmt.Errorf("PALPANEL_MONITOR_RETENTION_DAYS must be between 0 and 3650")
	}
	if cfg.IncidentWebhookTimeoutSeconds < 1 || cfg.IncidentWebhookTimeoutSeconds > 30 {
		return Config{}, fmt.Errorf("PALPANEL_INCIDENT_WEBHOOK_TIMEOUT_SECONDS must be between 1 and 30")
	}
	if cfg.IncidentWebhookURL != "" {
		webhookURL, err := url.Parse(cfg.IncidentWebhookURL)
		if err != nil || webhookURL.Hostname() == "" || webhookURL.User != nil || webhookURL.Fragment != "" || webhookURL.RawQuery != "" || (webhookURL.Scheme != "https" && webhookURL.Scheme != "http") {
			return Config{}, fmt.Errorf("PALPANEL_INCIDENT_WEBHOOK_URL must be an absolute HTTP(S) URL without credentials, query strings, or fragments")
		}
		host := strings.Trim(strings.ToLower(webhookURL.Hostname()), "[]")
		loopback := host == "localhost"
		if address := net.ParseIP(host); address != nil {
			loopback = address.IsLoopback()
		}
		if webhookURL.Scheme == "http" && !loopback {
			return Config{}, fmt.Errorf("PALPANEL_INCIDENT_WEBHOOK_URL must use HTTPS unless the target is loopback")
		}
		if len(cfg.IncidentWebhookSecret) < 16 {
			return Config{}, fmt.Errorf("PALPANEL_INCIDENT_WEBHOOK_SECRET must contain at least 16 characters when webhook delivery is enabled")
		}
	}
	if !validRCONHost(cfg.RCONHost) {
		return Config{}, fmt.Errorf("PALPANEL_RCON_HOST must be a hostname or IP address without a port")
	}
	for name, port := range map[string]int{
		"PALPANEL_GAME_PORT":             cfg.GamePort,
		"PALPANEL_QUERY_PORT":            cfg.QueryPort,
		"PALPANEL_RCON_PORT":             cfg.RCONPort,
		"PALPANEL_REST_PORT":             cfg.RESTPort,
		"PALPANEL_PALDEFENDER_REST_PORT": cfg.PalDefenderRESTPort,
	} {
		if port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("%s must be between 1 and 65535", name)
		}
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("PALPANEL_LOG_LEVEL must be one of debug, info, warn, or error")
	}

	return cfg, nil
}

func (c Config) EnsureDirs() error {
	if c.RuntimeRoot != "" {
		if err := c.ValidateManagedPath(c.RuntimeRoot, true); err != nil {
			return err
		}
		if err := os.MkdirAll(c.RuntimeRoot, 0o755); err != nil {
			return err
		}
	}
	dirs := []string{c.DataDir, c.ServerDirectory(), c.WinePrefixDir, c.ToolsDir, c.SteamCMDDir, c.UE4SSDir, c.UploadsDir, c.BackupsDir, c.LogsDir, c.SaveIndexCacheDir, c.SaveSourcesDir}
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		if c.RuntimeRoot != "" {
			if err := c.ValidateManagedPath(dir, true); err != nil {
				return err
			}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if c.RuntimeRoot != "" {
		if err := c.ValidateManagedPath(c.DBPath, false); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(c.DBPath), 0o755); err != nil {
		return err
	}
	return nil
}

func (c Config) PalServerExePath() string {
	return filepath.Join(c.ServerDirectory(), "PalServer.exe")
}

func (c Config) DefaultPalWorldSettingsPath() string {
	return filepath.Join(c.ServerDirectory(), "DefaultPalWorldSettings.ini")
}

func (c Config) PalWorldSettingsPath() string {
	return filepath.Join(c.ServerDirectory(), "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini")
}

func (c Config) ModsDir() string {
	return filepath.Join(c.ServerDirectory(), "Mods")
}

func (c Config) WorkshopModsDir() string {
	return filepath.Join(c.ModsDir(), "Workshop")
}

// WorkshopSteamCMDConfigDir stores the Linux SteamCMD user-data directory used
// for Docker/Wine Workshop downloads. SteamCMD keeps its machine authorization
// under $HOME/Steam (not beside /opt/steamcmd), and some authorization files
// live outside its config subdirectory, so the complete user-data tree must be
// persisted between short-lived containers.
func (c Config) WorkshopSteamCMDConfigDir() string {
	return filepath.Join(c.DataDir, "steamcmd-workshop-config")
}

func (c Config) PalModSettingsPath() string {
	return filepath.Join(c.ModsDir(), "PalModSettings.ini")
}

func (c Config) LegacyModsDir() string {
	return filepath.Join(c.ServerDirectory(), "Pal", "Content", "Paks", "LogicMods", "Mods")
}

func (c Config) Win64Dir() string {
	return filepath.Join(c.ServerDirectory(), "Pal", "Binaries", "Win64")
}

func (c Config) PalDefenderDir() string {
	return filepath.Join(c.Win64Dir(), "PalDefender")
}

func (c Config) EffectiveRCONPort() int {
	if c.RCONPort > 0 {
		return c.RCONPort
	}
	return DefaultRCONPort
}

func (c Config) EffectiveRCONHost() string {
	host := strings.Trim(strings.TrimSpace(c.RCONHost), "[]")
	if host == "" {
		return "127.0.0.1"
	}
	return host
}

func (c Config) EffectivePalDefenderRESTPort() int {
	if c.PalDefenderRESTPort > 0 {
		return c.PalDefenderRESTPort
	}
	return DefaultPalDefenderRESTPort
}

func (c Config) EffectivePalDefenderRESTBaseURL() string {
	if baseURL := strings.TrimSpace(c.PalDefenderRESTBaseURL); baseURL != "" {
		return strings.TrimRight(baseURL, "/")
	}
	return fmt.Sprintf("http://127.0.0.1:%d", c.EffectivePalDefenderRESTPort())
}

func (c Config) SteamCMDBinaryPath() string {
	return filepath.Join(c.SteamCMDDir, "steamcmd.exe")
}

func (c Config) ServerLogPath() string {
	return filepath.Join(c.LogsDir, "palserver.log")
}

func (c Config) DebugLogPath() string {
	return filepath.Join(c.LogsDir, "palpanel-debug.log")
}

func (c Config) AITranslationKeyPath() string {
	return filepath.Join(c.DataDir, "secrets", "ai-translation.key")
}

func (c Config) NetworkProxyConfigPath() string {
	return filepath.Join(c.DataDir, "secrets", "network-proxy.json")
}

func (c Config) SteamCMDProxyRestorePath() string {
	return filepath.Join(c.DataDir, "secrets", "steamcmd-proxy-restore.json")
}

// SteamWorkshopCredentialsPath stores the Steam account and password used for
// explicit SteamCMD Workshop authentication on native Windows and Linux/Docker. The file is
// deliberately kept outside the SteamCMD installation so SteamCMD updates do
// not replace it and so PalPanel can apply the same private-file policy used by
// other managed secrets.
func (c Config) SteamWorkshopCredentialsPath() string {
	return filepath.Join(c.DataDir, "secrets", "steam-workshop-credentials.json")
}

func (c Config) EffectiveSteamWebAPIKey() string {
	if key := strings.TrimSpace(c.SteamWebAPIKey); key != "" {
		return key
	}
	return bundledSteamWebAPIKey()
}

func (c Config) SteamWebAPIKeyConfigured() bool {
	return strings.TrimSpace(c.EffectiveSteamWebAPIKey()) != ""
}

func (c Config) SteamWebAPIKeySourceName() string {
	if strings.TrimSpace(c.EffectiveSteamWebAPIKey()) == "" {
		return ""
	}
	if source := strings.TrimSpace(c.SteamWebAPIKeySource); source != "" {
		return source
	}
	if strings.TrimSpace(c.SteamWebAPIKey) == "" {
		return "bundled"
	}
	return "environment"
}

func resolveSteamWebAPIKey() (string, string) {
	if key := strings.TrimSpace(os.Getenv("STEAM_WEB_API_KEY")); key != "" {
		return key, "environment"
	}
	return bundledSteamWebAPIKey(), "bundled"
}

func validateHTTPBaseURL(name, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL without credentials, query, or fragment", name)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("%s must use HTTP or HTTPS", name)
	}
}

func validateProxyURL(name, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an absolute proxy URL without query or fragment", name)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return nil
	default:
		return fmt.Errorf("%s must use http, https, socks5, or socks5h", name)
	}
}

func validateScalarEnvironment() error {
	for _, name := range []string{"PALPANEL_REQUIRE_AUTH", "PALPANEL_DIAGNOSTIC_SHELL_ENABLED", "PALPANEL_SAVE_INDEXER_ENABLED", "PALPANEL_COMMUNITY_SERVERS_ENABLED"} {
		raw := strings.TrimSpace(os.Getenv(name))
		if raw == "" {
			continue
		}
		if _, err := strconv.ParseBool(raw); err != nil {
			return fmt.Errorf("%s must be true or false", name)
		}
	}
	for _, name := range []string{
		"PALPANEL_PALDEFENDER_REST_PORT",
		"PALPANEL_MAX_UPLOAD_MB",
		"PALPANEL_STEAM_API_TIMEOUT_SECONDS",
		"PALPANEL_COMMUNITY_SERVERS_CACHE_TTL_SECONDS",
		"PALPANEL_COMMUNITY_SERVERS_STALE_TTL_SECONDS",
		"PALPANEL_COMMUNITY_SERVERS_RATE_LIMIT",
		"PALPANEL_STEAMCMD_DOWNLOAD_MAX_MB",
		"PALPANEL_UE4SS_DOWNLOAD_MAX_MB",
		"PALPANEL_GAME_PORT",
		"PALPANEL_QUERY_PORT",
		"PALPANEL_RCON_PORT",
		"PALPANEL_REST_PORT",
		"PALPANEL_PALWORLD_REST_READ_TIMEOUT_MS",
		"PALPANEL_GAME_DATA_TIMEOUT_MS",
		"PALPANEL_GAME_DATA_MAX_MB",
		"PALPANEL_SAVE_INDEX_TIMEOUT_SECONDS",
		"PALPANEL_SAVE_HISTORY_INTERVAL_MINUTES",
		"PALPANEL_PERF_SLOW_REQUEST_MS",
		"PALPANEL_MONITOR_RETENTION_DAYS",
		"PALPANEL_AI_TRANSLATION_TIMEOUT_SECONDS",
	} {
		raw := strings.TrimSpace(os.Getenv(name))
		if raw == "" {
			continue
		}
		if _, err := strconv.Atoi(raw); err != nil {
			return fmt.Errorf("%s must be an integer", name)
		}
	}
	return nil
}

func validateListenAddress(address string) error {
	_, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("PALPANEL_LISTEN_ADDR must be a host:port address: %w", err)
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return fmt.Errorf("PALPANEL_LISTEN_ADDR port must be between 1 and 65535")
	}
	return nil
}

func validRCONHost(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n\t /") {
		return false
	}
	if strings.HasPrefix(raw, "[") != strings.HasSuffix(raw, "]") {
		return false
	}
	host := strings.Trim(raw, "[]")
	if net.ParseIP(host) != nil {
		return true
	}
	if strings.Contains(host, ":") {
		return false
	}
	for _, char := range host {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '.' || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return host != ""
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func configuredPath(key, fallback, base string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		value = fallback
	}
	path, err := absoluteFrom(base, value)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", key, err)
	}
	return path, nil
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envList(key string, fallback []string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	if raw == "*" {
		return []string{"*"}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
