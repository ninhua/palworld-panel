package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/aitranslation"
	"palpanel/internal/appconfig"
	"palpanel/internal/astrbotclient"
	panelauth "palpanel/internal/auth"
	"palpanel/internal/breeding"
	"palpanel/internal/buildinfo"
	"palpanel/internal/communityservers"
	"palpanel/internal/db"
	"palpanel/internal/id"
	"palpanel/internal/incidents"
	"palpanel/internal/mods"
	"palpanel/internal/monitor"
	"palpanel/internal/networkproxy"
	"palpanel/internal/palconfig"
	"palpanel/internal/paldefender"
	"palpanel/internal/palrest"
	"palpanel/internal/saveindex"
	"palpanel/internal/scheduler"
	"palpanel/internal/server"
	"palpanel/internal/webui"
)

type Server struct {
	cfg             appconfig.Config
	store           *db.Store
	server          server.Manager
	mods            mods.Manager
	defender        paldefender.Manager
	palrest         palrest.Client
	monitor         monitor.Manager
	scheduler       scheduler.Manager
	saveIndex       *saveindex.Manager
	serverSaveIndex *saveindex.Manager
	breeding        *breeding.Service
	community       *communityservers.Service
	communityAPI    *CommunityServersHandler
	astrbot         *astrbotclient.Client
	ai              *aitranslation.Service
	networkProxy    *networkproxy.Service
	auth            *panelauth.Service
	authLimiter     *authRateLimiter
	cache           *ttlCache
	gmIdempotency   *gmIdempotencyStore
	incidents       *incidents.Service
	saveImports     *saveImportInspectionStore
	workAssignments *workAssignmentTokenStore
	webUI           fs.FS
}

func NewRouter(cfg appconfig.Config, store *db.Store, serverManager server.Manager, modsManager mods.Manager, defenderManager paldefender.Manager, restClient palrest.Client, monitorManager monitor.Manager, schedulerManager scheduler.Manager, incidentServices ...*incidents.Service) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	var incidentService *incidents.Service
	if len(incidentServices) > 0 {
		incidentService = incidentServices[0]
	}
	webFiles, _ := webui.Load(cfg.FrontendDist)
	saveManager := saveindex.NewManager(cfg)
	initializeSaveSources(cfg, store, saveManager)
	liveCfg := cfg
	liveCfg.SaveIndexCacheDir = filepath.Join(cfg.SaveIndexCacheDir, "server-live")
	serverSaveManager := saveindex.NewManager(liveCfg)
	if err := cleanupSaveImportInspections(cfg); err != nil {
		log.Printf("save import inspection cleanup failed: %v", err)
	}
	networkProxyService := networkproxy.New(cfg)
	var communityService *communityservers.Service
	var communityAPI *CommunityServersHandler
	if cfg.CommunityServersEnabled {
		service, err := communityservers.New(communityservers.Options{
			BaseURL:         cfg.CommunityServersAPIBaseURL,
			Fetcher:         communityProxyFetcher{baseURL: cfg.CommunityServersAPIBaseURL, proxy: networkProxyService},
			ProxyConfigured: networkProxyService.CommunityProxyConfigured,
			CachePath:       filepath.Join(cfg.DataDir, "cache", "community-servers.json"),
			FreshTTL:        time.Duration(cfg.CommunityServersCacheTTL) * time.Second,
			StaleTTL:        time.Duration(cfg.CommunityServersStaleTTL) * time.Second,
			RateLimit:       cfg.CommunityServersRateLimit,
		})
		if err != nil {
			log.Printf("community server discovery disabled: %v", err)
		} else {
			communityService = service
			communityAPI = NewCommunityServersHandler(service)
		}
	}
	s := Server{cfg: cfg, store: store, server: serverManager, mods: modsManager, defender: defenderManager, palrest: restClient, monitor: monitorManager, scheduler: schedulerManager, saveIndex: saveManager, serverSaveIndex: serverSaveManager, breeding: breeding.New(cfg, store, saveManager), community: communityService, communityAPI: communityAPI, astrbot: astrbotclient.New(cfg), ai: aitranslation.New(cfg, store), networkProxy: networkProxyService, auth: panelauth.New(store), authLimiter: newAuthRateLimiter(), cache: newTTLCache(), gmIdempotency: newGMIdempotencyStore(), incidents: incidentService, saveImports: newSaveImportInspectionStore(defaultSaveImportInspectionTTL), workAssignments: newWorkAssignmentTokenStore(), webUI: webFiles}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(PerformanceMiddleware(cfg))
	r.Use(GzipMiddleware())
	r.Use(CORSMiddleware(cfg.CORSOrigins))
	s.registerRoutes(r)
	if s.astrbot.Enabled() {
		go s.runAstrBotCatalogSync()
	}
	return r
}

func (s Server) health(c *gin.Context) {
	info := buildinfo.Current()
	ok(c, gin.H{
		"status":     "ok",
		"version":    info.Version,
		"commit":     info.Commit,
		"build_time": info.BuildTime,
	})
}

func (s Server) ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		fail(c, http.StatusServiceUnavailable, "database_unavailable", "database is unavailable")
		return
	}
	for _, path := range []string{s.cfg.DataDir, s.cfg.ServerDirectory(), s.cfg.LogsDir, s.cfg.BackupsDir} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			fail(c, http.StatusServiceUnavailable, "data_directory_unavailable", "a required data directory is unavailable")
			return
		}
	}
	version, err := s.store.SchemaVersion(ctx)
	if err != nil {
		fail(c, http.StatusServiceUnavailable, "schema_unavailable", "database schema is unavailable")
		return
	}
	ok(c, gin.H{"status": "ready", "schema_version": version})
}

func (s Server) listJobs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	jobs, err := s.store.ListJobs(c.Request.Context(), limit)
	if err != nil {
		fail(c, http.StatusInternalServerError, "jobs_list_failed", err.Error())
		return
	}
	ok(c, jobs)
}

func (s Server) getJob(c *gin.Context) {
	j, err := s.store.GetJob(c.Request.Context(), c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(c, http.StatusNotFound, "job_not_found", "job not found")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "job_read_failed", err.Error())
		return
	}
	ok(c, j)
}

func (s Server) listAuditLogs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	logs, err := s.store.ListAuditLogs(c.Request.Context(), limit)
	if err != nil {
		fail(c, http.StatusInternalServerError, "audit_list_failed", err.Error())
		return
	}
	ok(c, logs)
}

func (s Server) listAlerts(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	alerts, err := s.store.ListAlerts(c.Request.Context(), limit)
	if err != nil {
		fail(c, http.StatusInternalServerError, "alerts_list_failed", err.Error())
		return
	}
	ok(c, alerts)
}

func (s Server) ackAlert(c *gin.Context) {
	if err := s.store.AckAlert(c.Request.Context(), c.Param("id")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(c, http.StatusNotFound, "alert_not_found", "alert not found")
			return
		}
		fail(c, http.StatusInternalServerError, "alert_ack_failed", err.Error())
		return
	}
	ok(c, gin.H{"acked": true})
}

func (s Server) listSchedules(c *gin.Context) {
	items, err := s.scheduler.List(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "schedules_list_failed", err.Error())
		return
	}
	ok(c, items)
}

func (s Server) createSchedule(c *gin.Context) {
	var req db.Schedule
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	item, err := s.scheduler.Create(c.Request.Context(), req)
	if err != nil {
		fail(c, http.StatusBadRequest, "schedule_create_failed", err.Error())
		return
	}
	created(c, item)
}

func (s Server) updateSchedule(c *gin.Context) {
	var req db.Schedule
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	item, err := s.scheduler.Update(c.Request.Context(), c.Param("id"), req)
	if errors.Is(err, sql.ErrNoRows) {
		fail(c, http.StatusNotFound, "schedule_not_found", "schedule not found")
		return
	}
	if err != nil {
		fail(c, http.StatusBadRequest, "schedule_update_failed", err.Error())
		return
	}
	ok(c, item)
}

func (s Server) deleteSchedule(c *gin.Context) {
	if err := s.scheduler.Delete(c.Request.Context(), c.Param("id")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(c, http.StatusNotFound, "schedule_not_found", "schedule not found")
			return
		}
		fail(c, http.StatusInternalServerError, "schedule_delete_failed", err.Error())
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (s Server) runSchedule(c *gin.Context) {
	job, err := s.scheduler.RunNow(c.Request.Context(), c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(c, http.StatusNotFound, "schedule_not_found", "schedule not found")
		return
	}
	if err != nil {
		fail(c, http.StatusBadRequest, "schedule_run_failed", err.Error())
		return
	}
	accepted(c, job)
}

func (s Server) serverStatus(c *gin.Context) {
	status, _, err := cachedAs(s, c, cacheKey(cacheKeyServerPrefix, "status"), 2*time.Second, s.server.Status)
	if err != nil {
		fail(c, http.StatusInternalServerError, "server_status_failed", err.Error())
		return
	}
	ok(c, status)
}

func (s Server) serverPrerequisites(c *gin.Context) {
	checks, _, err := cachedAs(s, c, cacheKey(cacheKeyServerPrefix, "prerequisites"), 5*time.Second, s.server.Prerequisites)
	if err != nil {
		fail(c, http.StatusInternalServerError, "prerequisites_failed", err.Error())
		return
	}
	ok(c, checks)
}

func (s Server) serverHost(c *gin.Context) {
	host, _, err := cachedAs(s, c, cacheKey(cacheKeyServerPrefix, "host"), 5*time.Second, func(ctx context.Context) (server.HostCapabilities, error) {
		return s.server.HostCapabilities(ctx), nil
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "server_host_failed", err.Error())
		return
	}
	ok(c, host)
}

func (s Server) getRuntime(c *gin.Context) {
	mode, err := s.server.RuntimeMode(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "runtime_read_failed", err.Error())
		return
	}
	ok(c, gin.H{"mode": mode})
}

func (s Server) putRuntime(c *gin.Context) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := s.server.SetRuntimeMode(c.Request.Context(), req.Mode); err != nil {
		fail(c, http.StatusBadRequest, "runtime_write_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	ok(c, gin.H{"mode": req.Mode})
}

func (s Server) importServerDirectory(c *gin.Context) {
	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	result, err := s.server.ImportServerDirectory(c.Request.Context(), req.Path)
	if err != nil {
		fail(c, http.StatusBadRequest, "server_import_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	ok(c, result)
}

func (s Server) serverDockerPlan(c *gin.Context) {
	source := c.DefaultQuery("source", "auto")
	plan, _, err := cachedAs(s, c, cacheKey(cacheKeyServerPrefix, "docker-plan", source), 60*time.Second, func(ctx context.Context) (server.DockerInstallPlan, error) {
		return s.server.DockerInstallPlan(ctx, source)
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "docker_plan_failed", err.Error())
		return
	}
	ok(c, plan)
}

func (s Server) serverDockerInstall(c *gin.Context) {
	var req server.DockerInstallRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	j, err := s.server.InstallDocker(c.Request.Context(), req)
	if err != nil {
		var installErr *server.DockerInstallError
		if errors.As(err, &installErr) {
			fail(c, installErr.Status, installErr.Code, installErr.Msg)
			return
		}
		fail(c, http.StatusInternalServerError, "docker_install_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	accepted(c, j)
}

func (s Server) serverDockerMirrorsPlan(c *gin.Context) {
	mirror := c.DefaultQuery("mirror", "auto")
	plan, _, err := cachedAs(s, c, cacheKey(cacheKeyServerPrefix, "docker-mirror-plan", mirror), 60*time.Second, func(ctx context.Context) (server.DockerMirrorPlan, error) {
		return s.server.DockerMirrorPlan(ctx, mirror)
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "docker_mirror_plan_failed", err.Error())
		return
	}
	ok(c, plan)
}

func (s Server) serverDockerMirrorsConfigure(c *gin.Context) {
	var req server.DockerMirrorRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	j, err := s.server.ConfigureDockerMirrors(c.Request.Context(), req)
	if err != nil {
		var installErr *server.DockerInstallError
		if errors.As(err, &installErr) {
			fail(c, installErr.Status, installErr.Code, installErr.Msg)
			return
		}
		fail(c, http.StatusInternalServerError, "docker_mirror_configure_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	accepted(c, j)
}

func (s Server) serverBootstrap(c *gin.Context) {
	j, err := s.server.Bootstrap(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "bootstrap_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	accepted(c, j)
}

func (s Server) serverLogs(c *gin.Context) {
	tail, _ := strconv.Atoi(c.DefaultQuery("tail", "200"))
	query := server.LogQuery{
		Channel: c.Query("channel"),
		Tail:    tail,
		Search:  c.Query("search"),
		Level:   c.Query("level"),
		Since:   c.Query("since"),
	}
	logs, _, err := cachedAs(s, c, cacheKey(cacheKeyServerPrefix, "logs", query.Channel, query.Tail, query.Search, query.Level, query.Since), 2*time.Second, func(ctx context.Context) (server.LogResult, error) {
		return s.server.Logs(ctx, query)
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "server_logs_failed", err.Error())
		return
	}
	ok(c, logs)
}

func (s Server) monitorSnapshot(c *gin.Context) {
	snapshot, _, err := cachedAs(s, c, cacheKey(cacheKeyServerPrefix, "monitor-snapshot"), 2*time.Second, s.monitor.Snapshot)
	if err != nil {
		fail(c, http.StatusInternalServerError, "monitor_snapshot_failed", err.Error())
		return
	}
	ok(c, snapshot)
}

func (s Server) monitorHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "120"))
	history, _, err := cachedAs(s, c, cacheKey(cacheKeyServerPrefix, "monitor-history", limit), 5*time.Second, func(ctx context.Context) ([]db.MonitorSample, error) {
		return s.monitor.History(ctx, limit)
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "monitor_history_failed", err.Error())
		return
	}
	ok(c, history)
}

func (s Server) serverMetrics(c *gin.Context) {
	metrics, status, err := cachedAs(s, c, cacheKey(cacheKeyServerPrefix, "metrics"), 2*time.Second, func(ctx context.Context) (gin.H, error) {
		resp, err := s.palworldRESTRead().Do(ctx, http.MethodGet, "metrics", nil)
		if err == nil {
			return normalizeRESTMetrics(resp.Body), nil
		}

		metrics := gin.H{
			"server_fps":      0,
			"current_players": 0,
			"max_players":     32,
			"uptime":          0,
			"total_pals":      0,
			"active_bases":    0,
			"frame_time":      0,
			"source":          "monitor_sample",
			"rest_healthy":    false,
			"error":           err.Error(),
		}
		if samples, sampleErr := s.store.ListMonitorSamples(ctx, 1); sampleErr == nil && len(samples) > 0 {
			sample := samples[0]
			metrics["current_players"] = sample.CurrentPlayers
			if sample.MaxPlayers > 0 {
				metrics["max_players"] = sample.MaxPlayers
			}
			metrics["rest_healthy"] = sample.RESTHealthy
			metrics["unavailable_reason"] = sample.UnavailableReason
		}
		return metrics, nil
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, "server_metrics_failed", err.Error())
		return
	}
	if status == cacheStatusStale {
		metrics["stale"] = true
	}
	ok(c, metrics)
}

func (s Server) serverInstall(c *gin.Context) {
	j, err := s.server.Install(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "install_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	accepted(c, j)
}

func (s Server) serverUpdate(c *gin.Context) {
	j, err := s.server.UpdateWithPreUpdate(c.Request.Context(), s.preUpdateHook())
	if err != nil {
		fail(c, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	accepted(c, j)
}

func (s Server) serverUpdateIfNeeded(c *gin.Context) {
	j, err := s.server.UpdateIfNeeded(c.Request.Context(), s.preUpdateHook())
	if err != nil {
		fail(c, http.StatusInternalServerError, "smart_update_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	accepted(c, j)
}

func (s Server) serverVersion(c *gin.Context) {
	info, err := s.server.VersionInfo(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "version_read_failed", err.Error())
		return
	}
	status, statusErr := s.server.Status(c.Request.Context())
	if statusErr == nil && status.Container.Status == "running" {
		if response, restErr := s.palworldRESTRead().Do(c.Request.Context(), http.MethodGet, "info", nil); restErr == nil {
			if body, bodyOK := response.Body.(map[string]any); bodyOK {
				if gameVersion, versionOK := body["version"]; versionOK {
					info = s.server.WithGameVersion(c.Request.Context(), info, fmt.Sprint(gameVersion))
				}
			}
		}
	}
	ok(c, info)
}

func (s Server) serverVersionCheck(c *gin.Context) {
	j, err := s.server.CheckVersion(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "version_check_failed", err.Error())
		return
	}
	accepted(c, j)
}

func (s Server) serverStart(c *gin.Context) {
	if err := s.server.Start(c.Request.Context()); err != nil {
		if crashGuardOperationFailure(c, err) {
			return
		}
		fail(c, http.StatusInternalServerError, "start_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	ok(c, gin.H{"status": "started"})
}

func (s Server) serverStop(c *gin.Context) {
	if err := s.server.Stop(c.Request.Context()); err != nil {
		fail(c, http.StatusInternalServerError, "stop_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	ok(c, gin.H{"status": "stopped"})
}

func (s Server) serverRestart(c *gin.Context) {
	if err := s.server.Restart(c.Request.Context()); err != nil {
		if crashGuardOperationFailure(c, err) {
			return
		}
		fail(c, http.StatusInternalServerError, "restart_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	s.invalidateServerSaveIndex()
	ok(c, gin.H{"status": "restarted"})
}

func (s Server) serverSafeRestart(c *gin.Context) {
	var req struct {
		WaitTime int    `json:"waittime"`
		Message  string `json:"message"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	j, err := s.server.SafeRestart(c.Request.Context(), req.WaitTime, req.Message, func(ctx context.Context, wait int, message string) error {
		client := s.palworldREST()
		if _, err := client.Do(ctx, http.MethodPost, "save", nil); err != nil {
			return err
		}
		_, err := client.Do(ctx, http.MethodPost, "shutdown", gin.H{"waittime": wait, "message": message})
		return err
	})
	if err != nil {
		if crashGuardOperationFailure(c, err) {
			return
		}
		fail(c, http.StatusBadRequest, "safe_restart_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	s.invalidateServerSaveIndex()
	accepted(c, j)
}

func (s Server) serverForceStop(c *gin.Context) {
	if err := s.server.Stop(c.Request.Context()); err != nil {
		fail(c, http.StatusInternalServerError, "server_force_stop_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	status, statusErr := s.server.Status(c.Request.Context())
	if statusErr != nil {
		ok(c, gin.H{"status": "stopped", "status_error": statusErr.Error()})
		return
	}
	ok(c, gin.H{"status": "stopped", "server": status})
}

func (s Server) getStartup(c *gin.Context) {
	startup, err := s.server.StartupConfig(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "startup_read_failed", err.Error())
		return
	}
	ok(c, gin.H{"startup": startup, "args": startup.Args(s.cfg), "issues": startup.Validate()})
}

func (s Server) putStartup(c *gin.Context) {
	var startup server.StartupConfig
	if err := c.ShouldBindJSON(&startup); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	issues := startup.Validate()
	if hasServerValidationErrors(issues) {
		fail(c, http.StatusBadRequest, "startup_invalid", "startup config has validation errors")
		return
	}
	saved, err := s.server.SetStartupConfig(c.Request.Context(), startup)
	if err != nil {
		fail(c, http.StatusBadRequest, "startup_write_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	ok(c, gin.H{"startup": saved, "args": saved.Args(s.cfg), "issues": issues})
}

func (s Server) initializeConfig(c *gin.Context) {
	if err := s.server.InitializeConfig(c.Request.Context()); err != nil {
		fail(c, http.StatusInternalServerError, "config_init_failed", err.Error())
		return
	}
	s.invalidateServerCaches()
	ok(c, gin.H{"path": s.cfg.PalWorldSettingsPath()})
}

func (s Server) serverBackup(c *gin.Context) {
	j, err := s.server.Backup(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "backup_failed", err.Error())
		return
	}
	accepted(c, j)
}

func (s Server) listBackups(c *gin.Context) {
	backups, err := s.server.ListBackups()
	if err != nil {
		fail(c, http.StatusInternalServerError, "backups_list_failed", err.Error())
		return
	}
	ok(c, backups)
}

func (s Server) restoreBackup(c *gin.Context) {
	j, err := s.server.RestoreBackup(c.Request.Context(), c.Param("name"))
	if err != nil {
		fail(c, http.StatusBadRequest, "backup_restore_failed", err.Error())
		return
	}
	accepted(c, j)
}

func (s Server) downloadBackup(c *gin.Context) {
	path, err := s.server.BackupDownloadPath(c.Param("name"))
	if err != nil {
		fail(c, http.StatusBadRequest, "backup_download_failed", err.Error())
		return
	}
	c.FileAttachment(path, filepath.Base(path))
}

func (s Server) deleteBackup(c *gin.Context) {
	if err := s.server.DeleteBackup(c.Param("name")); err != nil {
		fail(c, http.StatusBadRequest, "backup_delete_failed", err.Error())
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (s Server) verifyBackup(c *gin.Context) {
	result, err := s.server.VerifyBackup(c.Param("name"))
	if err != nil {
		fail(c, http.StatusBadRequest, "backup_verify_failed", err.Error())
		return
	}
	ok(c, result)
}

func (s Server) getPalworldConfig(c *gin.Context) {
	if err := s.cleanupExpiredConfigDrafts(c.Request.Context()); err != nil {
		fail(c, http.StatusInternalServerError, "config_draft_cleanup_failed", err.Error())
		return
	}
	snapshot, err := server.ReadPalworldConfigSnapshot(s.cfg.PalWorldSettingsPath())
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_read_failed", err.Error())
		return
	}
	ok(c, s.palworldConfigResponse(c.Request.Context(), snapshot.Document, snapshot.Revision, nil))
}

func (s Server) updatePalworldConfig(c *gin.Context) {
	if err := s.cleanupExpiredConfigDrafts(c.Request.Context()); err != nil {
		fail(c, http.StatusInternalServerError, "config_draft_cleanup_failed", err.Error())
		return
	}
	request, err := decodePalworldConfigUpdate(c)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	snapshot, err := server.ReadPalworldConfigSnapshot(s.cfg.PalWorldSettingsPath())
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_read_failed", err.Error())
		return
	}
	next, modified, err := mergePalworldConfigUpdate(snapshot.Document, request)
	if err != nil {
		fail(c, http.StatusBadRequest, "config_invalid", err.Error())
		return
	}
	issues := palconfig.Validate(next)
	if hasPalconfigErrors(issues) {
		fail(c, http.StatusBadRequest, "config_invalid", "palworld config has validation errors")
		return
	}
	draft, err := s.createPalworldConfigDraft(c.Request.Context(), palconfig.Document{Settings: next, RawValues: snapshot.Document.RawValues}, modified, snapshot.Revision)
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_draft_create_failed", err.Error())
		return
	}
	ok(c, s.palworldConfigResponse(c.Request.Context(), palconfig.Document{Settings: next}, snapshot.Revision, &draft))
}

func (s Server) getPalworldConfigSchema(c *gin.Context) {
	ok(c, gin.H{"version": palconfig.SchemaVersion, "fields": palconfig.Schema()})
}

func (s Server) validatePalworldConfig(c *gin.Context) {
	request, err := decodePalworldConfigUpdate(c)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	current, err := palconfig.ReadDocument(s.cfg.PalWorldSettingsPath())
	if err != nil {
		fail(c, http.StatusInternalServerError, "config_read_failed", err.Error())
		return
	}
	next, _, err := mergePalworldConfigUpdate(current, request)
	if err != nil {
		fail(c, http.StatusBadRequest, "config_invalid", err.Error())
		return
	}
	issues := palconfig.Validate(next)
	ok(c, gin.H{"issues": issues, "valid": !hasPalconfigErrors(issues)})
}

func (s Server) listMods(c *gin.Context) {
	list, err := s.mods.List(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "mods_list_failed", err.Error())
		return
	}
	ok(c, list)
}

func (s Server) scanLocalMods(c *gin.Context) {
	result, err := s.mods.ScanLocal(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "mods_local_scan_failed", err.Error())
		return
	}
	ok(c, result)
}

func (s Server) actOnLocalModFinding(c *gin.Context) {
	var request mods.LocalModActionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		fail(c, http.StatusBadRequest, "invalid_local_action", err.Error())
		return
	}
	result, err := s.mods.ActOnLocalFinding(c.Request.Context(), c.Param("id"), request)
	if err != nil {
		var actionErr mods.LocalModActionError
		if errors.As(err, &actionErr) {
			status := http.StatusConflict
			switch actionErr.Code {
			case "invalid_local_action", "local_action_confirmation_required":
				status = http.StatusBadRequest
			case "local_scan_failed", "local_ignore_failed", "local_import_staging_failed", "local_import_copy_failed", "local_import_failed", "local_repair_failed", "local_delete_failed", "local_rescan_failed":
				status = http.StatusInternalServerError
			}
			fail(c, status, actionErr.Code, actionErr.Error())
			return
		}
		fail(c, http.StatusInternalServerError, "local_action_failed", err.Error())
		return
	}
	ok(c, result)
}

func (s Server) workshopStatus(c *gin.Context) {
	ok(c, gin.H{
		"configured": s.cfg.SteamWebAPIKeyConfigured(),
		"key_source": s.cfg.SteamWebAPIKeySourceName(),
		"app_id":     s.cfg.WorkshopAppID,
	})
}

func (s Server) searchWorkshopMods(c *gin.Context) {
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "24"))
	params := mods.WorkshopSearchParams{
		Query:    c.Query("q"),
		Sort:     c.DefaultQuery("sort", "popular"),
		Cursor:   c.Query("cursor"),
		PageSize: pageSize,
		Tags:     queryTags(c.Query("tags")),
	}
	result, err := s.mods.SearchWorkshop(c.Request.Context(), params)
	if err != nil {
		failWorkshop(c, err)
		return
	}
	ok(c, result)
}

func (s Server) getWorkshopMod(c *gin.Context) {
	item, err := s.mods.WorkshopDetail(c.Request.Context(), c.Param("id"))
	if err != nil {
		failWorkshop(c, err)
		return
	}
	translation, err := s.ai.Cached(c.Request.Context(), item.ID, item.Summary)
	if err != nil {
		fail(c, http.StatusInternalServerError, "ai_translation_cache_read_failed", err.Error())
		return
	}
	item.Translation = translation
	ok(c, item)
}

func (s Server) uploadMod(c *gin.Context) {
	if s.cfg.MaxUploadBytes > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, s.cfg.MaxUploadBytes)
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		fail(c, http.StatusBadRequest, "upload_missing_file", err.Error())
		return
	}
	if s.cfg.MaxUploadBytes > 0 && header.Size > s.cfg.MaxUploadBytes {
		fail(c, http.StatusRequestEntityTooLarge, "upload_too_large", "uploaded mod exceeds PALPANEL_MAX_UPLOAD_MB")
		return
	}
	defer file.Close()
	enable := strings.EqualFold(c.PostForm("enable"), "true")
	mod, err := s.mods.UploadZip(c.Request.Context(), file, header.Filename, enable)
	if err != nil {
		fail(c, http.StatusBadRequest, "mod_upload_failed", err.Error())
		return
	}
	created(c, mod)
}

func (s Server) downloadWorkshop(c *gin.Context) {
	if !s.requireWorkshopLogin(c) {
		return
	}
	var req struct {
		ItemID string `json:"item_id"`
		Enable *bool  `json:"enable"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	enable := false
	if req.Enable != nil {
		enable = *req.Enable
	}
	j, err := s.mods.DownloadWorkshop(c.Request.Context(), req.ItemID, enable)
	if err != nil {
		fail(c, http.StatusBadRequest, "workshop_failed", err.Error())
		return
	}
	accepted(c, j)
}

func queryTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func failWorkshop(c *gin.Context, err error) {
	if errors.Is(err, mods.ErrSteamAPIKeyMissing) {
		fail(c, http.StatusServiceUnavailable, "steam_api_key_missing", "Steam Web API key is not available")
		return
	}
	var steamErr mods.SteamAPIError
	if errors.As(err, &steamErr) {
		status := http.StatusBadGateway
		if steamErr.Code == "steam_timeout" {
			status = http.StatusGatewayTimeout
		}
		fail(c, status, steamErr.Code, steamErr.Error())
		return
	}
	fail(c, http.StatusBadRequest, "workshop_failed", err.Error())
}

func (s Server) listPlayerBans(c *gin.Context) {
	items, err := s.store.ListPlayerAccess(c.Request.Context(), "ban")
	if err != nil {
		fail(c, http.StatusInternalServerError, "bans_list_failed", err.Error())
		return
	}
	if items == nil {
		items = []db.PlayerAccessEntry{}
	}
	ok(c, items)
}

func (s Server) addPlayerBan(c *gin.Context) {
	var req db.PlayerAccessEntry
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req.SteamID = strings.TrimSpace(req.SteamID)
	if req.SteamID == "" {
		fail(c, http.StatusBadRequest, "steam_id_required", "steam_id is required")
		return
	}
	if err := s.store.UpsertPlayerAccess(c.Request.Context(), "ban", req); err != nil {
		fail(c, http.StatusInternalServerError, "ban_write_failed", err.Error())
		return
	}
	ok(c, req)
}

func (s Server) deletePlayerBan(c *gin.Context) {
	if err := s.store.DeletePlayerAccess(c.Request.Context(), "ban", c.Param("steam_id")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(c, http.StatusNotFound, "ban_not_found", "ban entry not found")
			return
		}
		fail(c, http.StatusInternalServerError, "ban_delete_failed", err.Error())
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (s Server) listPlayerWhitelist(c *gin.Context) {
	items, err := s.store.ListPlayerAccess(c.Request.Context(), "whitelist")
	if err != nil {
		fail(c, http.StatusInternalServerError, "whitelist_list_failed", err.Error())
		return
	}
	if items == nil {
		items = []db.PlayerAccessEntry{}
	}
	ok(c, items)
}

func (s Server) putPlayerWhitelist(c *gin.Context) {
	var req struct {
		Players []db.PlayerAccessEntry `json:"players"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	for i := range req.Players {
		req.Players[i].SteamID = strings.TrimSpace(req.Players[i].SteamID)
	}
	if err := s.store.ReplacePlayerAccess(c.Request.Context(), "whitelist", req.Players); err != nil {
		fail(c, http.StatusBadRequest, "whitelist_write_failed", err.Error())
		return
	}
	ok(c, gin.H{"players": req.Players})
}

func (s Server) kickPlayer(c *gin.Context) {
	userID := strings.TrimSpace(c.Param("id"))
	if userID == "" {
		fail(c, http.StatusBadRequest, "player_id_required", "player id is required")
		return
	}
	payload, err := playerActionPayload(c, userID)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	resp, err := s.palworldREST().Do(c.Request.Context(), http.MethodPost, "kick", payload)
	if err != nil {
		fail(c, http.StatusBadGateway, "palworld_kick_failed", err.Error())
		return
	}
	ok(c, gin.H{"player_id": userID, "palworld": resp})
}

func (s Server) banPlayer(c *gin.Context) {
	userID := strings.TrimSpace(c.Param("id"))
	if userID == "" {
		fail(c, http.StatusBadRequest, "player_id_required", "player id is required")
		return
	}
	payload, err := playerActionPayload(c, userID)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	resp, err := s.palworldREST().Do(c.Request.Context(), http.MethodPost, "ban", payload)
	if err != nil {
		fail(c, http.StatusBadGateway, "palworld_ban_failed", err.Error())
		return
	}
	entry := db.PlayerAccessEntry{
		SteamID:  userID,
		Nickname: stringFromMap(payload, "nickname"),
		Reason:   stringFromMap(payload, "reason"),
	}
	if err := s.store.UpsertPlayerAccess(c.Request.Context(), "ban", entry); err != nil {
		fail(c, http.StatusInternalServerError, "ban_record_failed", err.Error())
		return
	}
	ok(c, gin.H{"player_id": userID, "palworld": resp, "ban": entry})
}

func (s Server) unbanPlayer(c *gin.Context) {
	userID := strings.TrimSpace(c.Param("id"))
	if userID == "" {
		fail(c, http.StatusBadRequest, "player_id_required", "player id is required")
		return
	}
	payload, err := playerActionPayload(c, userID)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	resp, err := s.palworldREST().Do(c.Request.Context(), http.MethodPost, "unban", payload)
	if err != nil {
		fail(c, http.StatusBadGateway, "palworld_unban_failed", err.Error())
		return
	}
	if err := s.store.DeletePlayerAccess(c.Request.Context(), "ban", userID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		fail(c, http.StatusInternalServerError, "ban_record_delete_failed", err.Error())
		return
	}
	ok(c, gin.H{"player_id": userID, "palworld": resp, "deleted": true})
}

func (s Server) enableMod(c *gin.Context) {
	mod, err := s.mods.SetEnabled(c.Request.Context(), c.Param("id"), true)
	if errors.Is(err, sql.ErrNoRows) {
		fail(c, http.StatusNotFound, "mod_not_found", "mod not found")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "mod_enable_failed", err.Error())
		return
	}
	ok(c, mod)
}

func (s Server) disableMod(c *gin.Context) {
	mod, err := s.mods.SetEnabled(c.Request.Context(), c.Param("id"), false)
	if errors.Is(err, sql.ErrNoRows) {
		fail(c, http.StatusNotFound, "mod_not_found", "mod not found")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "mod_disable_failed", err.Error())
		return
	}
	ok(c, mod)
}

func (s Server) deleteMod(c *gin.Context) {
	err := s.mods.Delete(c.Request.Context(), c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(c, http.StatusNotFound, "mod_not_found", "mod not found")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "mod_delete_failed", err.Error())
		return
	}
	ok(c, gin.H{"deleted": true})
}

func (s Server) palDefenderReleases(c *gin.Context) {
	releases, err := s.defender.Releases(c.Request.Context())
	if err != nil {
		fail(c, http.StatusBadGateway, "paldefender_releases_failed", err.Error())
		return
	}
	ok(c, releases)
}

func (s Server) palDefenderStatus(c *gin.Context) {
	status, err := s.defender.Status(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "paldefender_status_failed", err.Error())
		return
	}
	ok(c, status)
}

func (s Server) palDefenderInstall(c *gin.Context) {
	j, err := s.defender.Install(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "paldefender_install_failed", err.Error())
		return
	}
	accepted(c, j)
}

func (s Server) palDefenderUpdate(c *gin.Context) {
	j, err := s.defender.Update(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "paldefender_update_failed", err.Error())
		return
	}
	accepted(c, j)
}

func (s Server) palDefenderRollback(c *gin.Context) {
	status, err := s.defender.Rollback(c.Request.Context())
	if err != nil {
		fail(c, http.StatusBadRequest, "paldefender_rollback_failed", err.Error())
		return
	}
	ok(c, status)
}

func (s Server) palDefenderGetConfig(c *gin.Context) {
	cfg, err := s.defender.ReadConfig()
	if err != nil {
		fail(c, http.StatusInternalServerError, "paldefender_config_read_failed", err.Error())
		return
	}
	ok(c, cfg)
}

func (s Server) palDefenderPutConfig(c *gin.Context) {
	var cfg map[string]any
	if err := c.ShouldBindJSON(&cfg); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	saved, err := s.defender.WriteConfig(cfg)
	if err != nil {
		fail(c, http.StatusInternalServerError, "paldefender_config_write_failed", err.Error())
		return
	}
	ok(c, saved)
}

func (s Server) palDefenderApplyPreset(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	_ = c.ShouldBindJSON(&req)
	cfg, err := s.defender.ApplyPreset(req.Name)
	if err != nil {
		fail(c, http.StatusBadRequest, "paldefender_preset_failed", err.Error())
		return
	}
	ok(c, cfg)
}

func (s Server) palDefenderRESTToken(c *gin.Context) {
	var req struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}
	_ = c.ShouldBindJSON(&req)
	token, err := s.defender.CreateRESTToken(c.Request.Context(), req.Name, req.Permissions)
	if err != nil {
		fail(c, http.StatusInternalServerError, "paldefender_token_failed", err.Error())
		return
	}
	ok(c, token)
}

func (s Server) palDefenderReloadConfig(c *gin.Context) {
	if err := s.defender.ReloadConfig(c.Request.Context()); err != nil {
		fail(c, http.StatusBadGateway, "paldefender_reload_failed", err.Error())
		return
	}
	ok(c, gin.H{"reloaded": true})
}

func (s Server) palGet(path string) gin.HandlerFunc {
	return func(c *gin.Context) {
		resp, err := s.palworldRESTRead().Do(c.Request.Context(), http.MethodGet, path, nil)
		if err != nil {
			fail(c, http.StatusBadGateway, "palworld_api_failed", err.Error())
			return
		}
		ok(c, resp)
	}
}

func (s Server) serverGameData(c *gin.Context) {
	timeout := time.Duration(s.cfg.PalworldGameDataTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	maxBytes := s.cfg.PalworldGameDataMaxBytes
	if maxBytes <= 0 {
		maxBytes = 16 * 1024 * 1024
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()
	response, err := s.palworldREST().DoWithLimit(ctx, http.MethodGet, "game-data", nil, maxBytes)
	if errors.Is(err, palrest.ErrResponseTooLarge) {
		fail(c, http.StatusBadGateway, "palworld_game_data_too_large", err.Error())
		return
	}
	if err != nil {
		fail(c, http.StatusBadGateway, "palworld_game_data_failed", err.Error())
		return
	}
	ok(c, response.Body)
}

func (s Server) palPost(path string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload any
		if c.Request.ContentLength > 0 {
			if err := c.ShouldBindJSON(&payload); err != nil {
				fail(c, http.StatusBadRequest, "invalid_json", err.Error())
				return
			}
		}
		resp, err := s.palworldREST().Do(c.Request.Context(), http.MethodPost, path, payload)
		if err != nil {
			fail(c, http.StatusBadGateway, "palworld_api_failed", err.Error())
			return
		}
		if path == "save" {
			s.invalidateServerSaveIndex()
		}
		ok(c, resp)
	}
}

func (s Server) palworldREST() palrest.Client {
	settings, err := palconfig.Read(s.cfg.PalWorldSettingsPath())
	if err != nil {
		settings = nil
	}
	return s.palworldRESTForSettings(settings)
}

func (s Server) palworldRESTForSettings(settings palconfig.Settings) palrest.Client {
	client := s.palrest
	if port := strings.TrimSpace(settings["RESTAPIPort"]); port != "" {
		client.BaseURL = restBaseURLWithPort(client.BaseURL, port)
	}
	if settings != nil {
		client.Password = settings["AdminPassword"]
	}
	if strings.TrimSpace(client.BaseURL) == "" {
		client.BaseURL = fmt.Sprintf("http://127.0.0.1:%d/v1/api", s.cfg.RESTPort)
	}
	return client
}

func restBaseURLWithPort(baseURL string, port string) string {
	if _, err := strconv.Atoi(port); err != nil {
		return strings.TrimRight(baseURL, "/")
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "http://" + net.JoinHostPort("127.0.0.1", port) + "/v1/api"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return strings.TrimRight(baseURL, "/")
	}
	host := parsed.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	parsed.Host = net.JoinHostPort(host, port)
	return strings.TrimRight(parsed.String(), "/")
}

func normalizeRESTMetrics(body any) gin.H {
	data, _ := body.(map[string]any)
	out := gin.H{
		"server_fps":      metricNumber(data, "server_fps", "serverFPS", "serverfps", "serverFps"),
		"current_players": int(metricNumber(data, "current_players", "currentPlayerNum", "currentplayernum", "players")),
		"max_players":     int(metricNumber(data, "max_players", "maxPlayerNum", "maxplayernum")),
		"uptime":          int(metricNumber(data, "uptime", "uptime_seconds")),
		"total_pals":      int(metricNumber(data, "total_pals", "pals")),
		"active_bases":    int(metricNumber(data, "active_bases", "bases", "basecampnum")),
		"days":            int(metricNumber(data, "days")),
		"frame_time":      metricNumber(data, "frame_time", "frameTime", "frametime", "server_frame_time", "serverFrameTime", "serverframetime"),
		"source":          "palworld_rest",
		"rest_healthy":    true,
	}
	if body != nil {
		out["raw"] = body
	}
	return out
}

func metricNumber(data map[string]any, keys ...string) float64 {
	if data == nil {
		return 0
	}
	for _, key := range keys {
		value, ok := data[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		case json.Number:
			if parsed, err := v.Float64(); err == nil {
				return parsed
			}
		case string:
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func (s Server) preUpdateHook() func(context.Context) error {
	return func(ctx context.Context) error {
		client := s.palworldREST()
		if _, err := client.Do(ctx, http.MethodPost, "announce", gin.H{"message": "Server update starting soon. Saving world and stopping server."}); err != nil {
			return fmt.Errorf("announce before update failed: %w", err)
		}
		if _, err := client.Do(ctx, http.MethodPost, "save", nil); err != nil {
			return fmt.Errorf("save before update failed: %w", err)
		}
		return nil
	}
}

func playerActionPayload(c *gin.Context, userID string) (map[string]any, error) {
	payload := map[string]any{"userid": userID}
	if c.Request.ContentLength > 0 {
		var raw map[string]any
		if err := c.ShouldBindJSON(&raw); err != nil {
			return nil, err
		}
		for _, key := range []string{"nickname", "reason", "message"} {
			if value, ok := raw[key]; ok {
				payload[key] = value
			}
		}
	}
	if _, hasMessage := payload["message"]; !hasMessage {
		if reason := stringFromMap(payload, "reason"); reason != "" {
			payload["message"] = reason
		}
	}
	return payload, nil
}

func stringFromMap(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func hasServerValidationErrors(issues []server.ValidationIssue) bool {
	for _, issue := range issues {
		if issue.Severity == "error" {
			return true
		}
	}
	return false
}

func hasPalconfigErrors(issues []palconfig.ValidationIssue) bool {
	for _, issue := range issues {
		if issue.Severity == "error" {
			return true
		}
	}
	return false
}

func AuditMiddleware(store *db.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Request.Method != http.MethodPost && c.Request.Method != http.MethodPut && c.Request.Method != http.MethodDelete {
			return
		}
		principal := CurrentPrincipal(c)
		status := "success"
		if c.Writer.Status() >= 400 {
			status = "failed"
		}
		action := c.Request.Method + " " + c.FullPath()
		if action == c.Request.Method+" " {
			action = c.Request.Method + " " + c.Request.URL.Path
		}
		target := c.Param("id")
		if target == "" {
			target = c.Param("name")
		}
		if target == "" {
			target = c.Param("steam_id")
		}
		_ = store.CreateAuditLog(context.Background(), db.AuditLog{
			ID:      id.New("audit"),
			Actor:   principal.Name,
			Role:    string(principal.Role),
			Action:  action,
			Target:  target,
			Status:  status,
			Message: http.StatusText(c.Writer.Status()),
			IP:      c.ClientIP(),
		})
	}
}

func CORSMiddleware(origins []string) gin.HandlerFunc {
	allowAny := len(origins) == 1 && origins[0] == "*"
	allowed := map[string]bool{}
	for _, origin := range origins {
		allowed[origin] = true
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if allowAny {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" && allowed[origin] {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Vary", "Origin")
		}
		if !allowAny {
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, Idempotency-Key, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
