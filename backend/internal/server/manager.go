package server

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/docker"
	"palpanel/internal/jobs"
	"palpanel/internal/palconfig"
)

const (
	kvRuntimeMode = "runtime_mode"
	kvStartup     = "startup_config"
	kvInstalled   = "installed"
	kvPID         = "windows_pid"
	kvProcess     = "windows_process"
	kvServerDir   = "server_directory_import"
)

type windowsProcessRecord struct {
	PID          int    `json:"pid"`
	Executable   string `json:"executable"`
	CreationTime uint64 `json:"creation_time"`
}

type windowsProcessInfo struct {
	Running      bool
	Executable   string
	CreationTime uint64
}

type Manager struct {
	cfg                       appconfig.Config
	store                     *db.Store
	runner                    docker.Runner
	remoteBuildIDFunc         func(context.Context) (string, string, error)
	installOrUpdateFunc       func(context.Context, string) error
	jobs                      *jobs.Executor
	operationMu               *sync.Mutex
	inspectProcess            func(int) (windowsProcessInfo, error)
	terminateProcess          func(context.Context, int) error
	downloadClient            *http.Client
	worldResetTimeout         time.Duration
	worldResetPoll            time.Duration
	gracefulStopTimeout       time.Duration
	gracefulStopPoll          time.Duration
	lifecycleWait             func(context.Context, time.Duration) error
	jobHeartbeatInterval      time.Duration
	goos                      string
	configApplyAfterStop      func()
	configApplyHealth         func(context.Context) error
	configApplyStatus         func(context.Context) (Status, error)
	configApplyStop           func(context.Context) error
	configApplyStart          func(context.Context) error
	configApplyJournalPersist func(context.Context, configApplyJournal) error
	configPrivateRemove       func(string) error
	configDraftTTL            time.Duration
}

type Status struct {
	Installed      bool                   `json:"installed"`
	PendingRestart bool                   `json:"pending_restart"`
	RuntimeMode    string                 `json:"runtime_mode"`
	SetupStep      string                 `json:"setup_step"`
	ConfigExists   bool                   `json:"config_exists"`
	Container      docker.ContainerStatus `json:"container"`
	StartupArgs    []string               `json:"startup_args"`
	Ports          map[string]int         `json:"ports"`
	Warnings       []string               `json:"warnings"`
	Paths          map[string]string      `json:"paths"`
	ServerImported bool                   `json:"server_imported"`
}

type Prerequisite struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	OK       bool   `json:"ok"`
	Required bool   `json:"required"`
	Message  string `json:"message,omitempty"`
}

type BackupInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt string `json:"created_at"`
	Reason    string `json:"reason,omitempty"`
	Status    string `json:"status"`
}

type BackupManifest struct {
	Version   int                  `json:"version"`
	Reason    string               `json:"reason"`
	CreatedAt string               `json:"created_at"`
	Files     []BackupManifestFile `json:"files"`
}

type BackupManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type BackupVerifyResult struct {
	Name         string   `json:"name"`
	Valid        bool     `json:"valid"`
	Format       string   `json:"format"`
	CheckedFiles int      `json:"checked_files"`
	Errors       []string `json:"errors"`
}

type LogQuery struct {
	Channel string
	Tail    int
	Search  string
	Level   string
	Since   string
}

type LogResult struct {
	Logs      string `json:"logs"`
	Source    string `json:"source"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type RestartNotifier func(ctx context.Context, wait int, message string) error

func NewManager(cfg appconfig.Config, store *db.Store, runner docker.Runner, executors ...*jobs.Executor) Manager {
	cfg = cfg.WithServerDirectoryState()
	recoverPanelUpdateIfNeeded(store)
	executor := jobs.New(store, 4)
	if len(executors) > 0 && executors[0] != nil {
		executor = executors[0]
	}
	return Manager{
		cfg: cfg, store: store, runner: runner,
		jobs:                 executor,
		operationMu:          &sync.Mutex{},
		inspectProcess:       inspectWindowsProcess,
		terminateProcess:     terminateWindowsProcessTree,
		downloadClient:       &http.Client{Timeout: 5 * time.Minute},
		worldResetTimeout:    180 * time.Second,
		worldResetPoll:       time.Second,
		gracefulStopTimeout:  15 * time.Second,
		gracefulStopPoll:     time.Second,
		lifecycleWait:        waitForLifecycleDuration,
		jobHeartbeatInterval: 15 * time.Second,
		goos:                 runtime.GOOS,
		configDraftTTL:       24 * time.Hour,
	}
}

func (m Manager) RuntimeMode(ctx context.Context) (string, error) {
	mode, ok, err := m.store.GetKV(ctx, kvRuntimeMode)
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(mode) == "" {
		return RecommendedRuntimeForOS(runtime.GOOS), nil
	}
	if mode != RuntimeWineDocker && mode != RuntimeWindowsSteamCMD {
		return RecommendedRuntimeForOS(runtime.GOOS), nil
	}
	return mode, nil
}

func (m Manager) SetRuntimeMode(ctx context.Context, mode string) error {
	mode = strings.TrimSpace(mode)
	if mode != RuntimeWineDocker && mode != RuntimeWindowsSteamCMD {
		return fmt.Errorf("unsupported runtime mode: %s", mode)
	}
	return m.store.SetKV(ctx, kvRuntimeMode, mode)
}

func (m Manager) StartupConfig(ctx context.Context) (StartupConfig, error) {
	raw, _, err := m.store.GetKV(ctx, kvStartup)
	if err != nil {
		return StartupConfig{}, err
	}
	return DecodeStartupConfig(raw, m.cfg), nil
}

func (m Manager) SetStartupConfig(ctx context.Context, cfg StartupConfig) (StartupConfig, error) {
	cfg = cfg.Normalize(m.cfg)
	if issues := cfg.Validate(); hasErrors(issues) {
		return cfg, fmt.Errorf("startup config has validation errors")
	}
	raw, err := EncodeStartupConfig(cfg, m.cfg)
	if err != nil {
		return cfg, err
	}
	return cfg, m.store.SetKV(ctx, kvStartup, raw)
}

func (m Manager) Prerequisites(ctx context.Context) ([]Prerequisite, error) {
	mode, err := m.RuntimeMode(ctx)
	if err != nil {
		return nil, err
	}
	checks := []Prerequisite{
		{ID: "data_dir", Label: "Data directory", OK: dirExists(m.cfg.DataDir), Required: true, Message: m.cfg.DataDir},
		{ID: "server_dir", Label: "Server directory", OK: dirExists(m.cfg.ServerDirectory()), Required: true, Message: m.cfg.ServerDirectory()},
	}
	if mode == RuntimeWineDocker {
		dockerCapability := detectDocker(ctx, m.cfg.DockerBinary)
		cliOK := dockerCapability.CLIInstalled
		if !cliOK {
			if _, statErr := os.Stat(`C:\Program Files\Docker\Docker\resources\bin\docker.exe`); statErr == nil {
				cliOK = true
			}
		}
		cliMessage := dockerCapability.CLIPath
		if cliMessage == "" {
			cliMessage = m.cfg.DockerBinary
		}
		daemonMessage := dockerCapability.Version
		if daemonMessage == "" {
			daemonMessage = dockerCapability.Error
		}
		checks = append(checks,
			Prerequisite{ID: "docker", Label: "Docker CLI", OK: cliOK, Required: true, Message: cliMessage},
			Prerequisite{ID: "docker_daemon", Label: "Docker daemon", OK: dockerCapability.DaemonReachable, Required: true, Message: daemonMessage},
		)
	} else {
		steamCMDErr := validatePEExecutable(m.cfg.SteamCMDBinaryPath())
		checks = append(checks,
			Prerequisite{ID: "windows", Label: "Windows host", OK: runtime.GOOS == "windows", Required: true, Message: runtime.GOOS},
			Prerequisite{ID: "steamcmd", Label: "SteamCMD", OK: steamCMDErr == nil, Required: false, Message: m.cfg.SteamCMDBinaryPath()},
		)
	}
	return checks, nil
}

func (m Manager) Install(ctx context.Context) (db.Job, error) {
	return m.startLifecycleJob(ctx, "install", "queued install", func(jobCtx context.Context, jobID string) {
		if m.runInstallOrUpdateJob(jobCtx, jobID, false, false, nil) {
			m.update(jobID, "completed", 100, "install completed", "")
		}
	})
}

func (m Manager) Update(ctx context.Context) (db.Job, error) {
	return m.UpdateWithPreUpdate(ctx, nil)
}

func (m Manager) UpdateWithPreUpdate(ctx context.Context, preUpdate func(context.Context) error) (db.Job, error) {
	return m.startLifecycleJob(ctx, "update", "queued update", func(jobCtx context.Context, jobID string) {
		if m.runInstallOrUpdateJob(jobCtx, jobID, true, true, preUpdate) {
			info, _ := m.VersionInfo(jobCtx)
			m.update(jobID, "completed", 100, "update completed; current build "+info.CurrentBuildID, "")
		}
	})
}

func (m Manager) Bootstrap(ctx context.Context) (db.Job, error) {
	return m.startLifecycleJob(ctx, "bootstrap", "queued bootstrap", func(jobCtx context.Context, jobID string) {
		if !m.runInstallOrUpdateJob(jobCtx, jobID, false, false, nil) {
			return
		}
		m.update(jobID, "running", 80, "initializing configuration", "")
		if err := m.InitializeConfig(jobCtx); err != nil {
			m.update(jobID, "failed", 80, "config initialization failed", err.Error())
			return
		}
		m.update(jobID, "completed", 100, "bootstrap completed", "")
	})
}

func (m Manager) runInstallOrUpdateJob(ctx context.Context, jobID string, backupFirst bool, update bool, preUpdate func(context.Context) error) bool {
	mode, err := m.RuntimeMode(ctx)
	if err != nil {
		m.update(jobID, "failed", 10, "runtime mode read failed", err.Error())
		return false
	}
	action := "install"
	if update {
		action = "update"
	}
	wasRunning := false
	if update {
		status, err := m.Status(ctx)
		if err != nil {
			m.update(jobID, "failed", 5, "server status read failed", err.Error())
			return false
		}
		wasRunning = status.Container.Status == "running"
		if wasRunning {
			if preUpdate != nil {
				m.update(jobID, "running", 5, "announcing update and saving world", "")
				if err := preUpdate(ctx); err != nil {
					m.update(jobID, "failed", 5, "pre-update notification/save failed", err.Error())
					return false
				}
			}
			m.update(jobID, "running", 10, "stopping server before update", "")
			if err := m.stopUnlocked(ctx); err != nil {
				m.update(jobID, "failed", 10, "stop before update failed", err.Error())
				return false
			}
		}
	}
	var backup BackupInfo
	if backupFirst {
		m.update(jobID, "running", 15, "creating backup before update", "")
		backup, err = m.createBackupArchive("pre-update")
		if err != nil {
			m.update(jobID, "failed", 15, "backup failed", err.Error())
			return false
		}
		verified, err := verifyBackupArchive(backup.Path, backup.Name)
		if err != nil || !verified.Valid {
			message := "backup verification failed"
			if err != nil {
				message += ": " + err.Error()
			} else if len(verified.Errors) > 0 {
				message += ": " + strings.Join(verified.Errors, "; ")
			}
			m.update(jobID, "failed", 15, "backup verification failed", message+"; retained at "+backup.Path)
			return false
		}
	}
	protected := map[string]string{}
	if update {
		protected, err = m.snapshotUpdateProtectedFiles()
		if err != nil {
			m.update(jobID, "failed", 20, "protected file snapshot failed", err.Error()+retainedBackupMessage(backup))
			return false
		}
	}
	if mode == RuntimeWindowsSteamCMD {
		m.update(jobID, "running", 25, "preparing SteamCMD", "")
		if m.installOrUpdateFunc == nil {
			if err := m.ensureSteamCMD(ctx); err != nil {
				m.update(jobID, "failed", 25, "steamcmd setup failed", err.Error())
				return false
			}
		}
	} else {
		m.update(jobID, "running", 20, "building wine runner image", "")
		if m.installOrUpdateFunc == nil {
			if err := m.runner.BuildImage(ctx); err != nil {
				m.update(jobID, "failed", 20, "build failed", err.Error())
				return false
			}
		}
	}
	stageMessage := action + "ing Palworld Windows dedicated server"
	m.update(jobID, "running", 60, stageMessage, "")
	if err := m.runJobStageWithHeartbeat(ctx, jobID, 60, stageMessage, func() error {
		return m.installOrUpdateRuntime(ctx, mode)
	}); err != nil {
		m.update(jobID, "failed", 60, action+" failed", err.Error()+retainedBackupMessage(backup))
		return false
	}
	if mode == RuntimeWindowsSteamCMD {
		if err := m.validateWindowsServerInstall(); err != nil {
			m.update(jobID, "failed", 70, action+" verification failed", err.Error()+retainedBackupMessage(backup))
			return false
		}
	} else if !fileExists(m.cfg.PalServerExePath()) {
		m.update(jobID, "failed", 70, action+" verification failed", "PalServer.exe is missing after SteamCMD completed"+retainedBackupMessage(backup))
		return false
	}
	if update {
		if err := m.verifyUpdateProtectedFiles(protected); err != nil {
			m.update(jobID, "failed", 70, "protected server data changed during update", err.Error()+retainedBackupMessage(backup)+"; no automatic restore was attempted")
			return false
		}
		m.update(jobID, "running", 75, "verifying installed build against Steam public branch", "")
		version, err := m.refreshVersion(ctx, false)
		if err != nil {
			m.update(jobID, "failed", 75, "post-update version verification failed", err.Error()+retainedBackupMessage(backup))
			return false
		}
		if version.CurrentBuildID == "" || version.LatestBuildID == "" || version.UpdateAvailable {
			m.update(jobID, "failed", 75, "post-update build mismatch", fmt.Sprintf("installed Build %q does not match Steam public Build %q%s", version.CurrentBuildID, version.LatestBuildID, retainedBackupMessage(backup)))
			return false
		}
	}
	if err := m.store.SetKV(ctx, kvInstalled, "true"); err != nil {
		m.update(jobID, "failed", 80, "install state persistence failed", err.Error())
		return false
	}
	if update && wasRunning {
		m.update(jobID, "running", 85, "starting server after update", "")
		if err := m.startUnlocked(ctx); err != nil {
			m.update(jobID, "failed", 85, "restart after update failed", err.Error())
			return false
		}
	}
	m.update(jobID, "running", 95, action+" completed", "")
	return true
}

func (m Manager) InitializeConfig(ctx context.Context) error {
	if fileExists(m.cfg.PalWorldSettingsPath()) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.cfg.PalWorldSettingsPath()), 0o755); err != nil {
		return err
	}
	settings := palconfig.Defaults()
	if fileExists(m.cfg.DefaultPalWorldSettingsPath()) {
		next, err := palconfig.Read(m.cfg.DefaultPalWorldSettingsPath())
		if err != nil {
			return err
		}
		for key, value := range next {
			settings[key] = value
		}
	}
	applyPanelDefaults(settings, m.cfg)
	return palconfig.Write(m.cfg.PalWorldSettingsPath(), settings)
}

func applyPanelDefaults(settings palconfig.Settings, cfg appconfig.Config) {
	settings["RESTAPIEnabled"] = "True"
	settings["RESTAPIPort"] = strconv.Itoa(cfg.RESTPort)
	settings["RCONPort"] = strconv.Itoa(cfg.EffectiveRCONPort())
	if cfg.PalworldRESTPass != "" {
		settings["AdminPassword"] = cfg.PalworldRESTPass
		settings["RCONEnabled"] = "True"
	}
}

func (m Manager) ValidateStartup(ctx context.Context) []ValidationIssue {
	var issues []ValidationIssue
	startup, err := m.StartupConfig(ctx)
	if err != nil {
		return []ValidationIssue{{Severity: "error", Message: err.Error()}}
	}
	issues = append(issues, startup.Validate()...)
	mode, modeErr := m.RuntimeMode(ctx)
	if modeErr != nil {
		issues = append(issues, ValidationIssue{Field: "runtime", Severity: "error", Message: modeErr.Error()})
	} else if mode == RuntimeWindowsSteamCMD {
		if err := m.validateWindowsServerInstall(); err != nil {
			issues = append(issues, ValidationIssue{Field: "server", Severity: "error", Message: err.Error()})
		}
	} else if !fileExists(m.cfg.PalServerExePath()) {
		issues = append(issues, ValidationIssue{Field: "server", Severity: "error", Message: "PalServer.exe not found; install server first"})
	}
	if !fileExists(m.cfg.PalWorldSettingsPath()) {
		issues = append(issues, ValidationIssue{Field: "config", Severity: "warning", Message: "PalWorldSettings.ini not found; run initialize-config"})
	}
	settings, err := palconfig.Read(m.cfg.PalWorldSettingsPath())
	if err == nil {
		for _, issue := range palconfig.Validate(settings) {
			issues = append(issues, ValidationIssue{Field: issue.Field, Severity: issue.Severity, Message: issue.Message})
		}
	}
	return issues
}

func (m Manager) Start(ctx context.Context) error {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	return m.startUnlocked(ctx)
}

func (m Manager) startUnlocked(ctx context.Context) error {
	if issues := m.ValidateStartup(ctx); hasErrors(issues) {
		return fmt.Errorf("startup validation failed: %s", validationIssueSummary(issues))
	}
	startup, err := m.StartupConfig(ctx)
	if err != nil {
		return err
	}
	mode, err := m.RuntimeMode(ctx)
	if err != nil {
		return err
	}
	if mode == RuntimeWindowsSteamCMD {
		err = m.startWindows(ctx, startup.Args(m.cfg))
	} else {
		err = m.runner.StartWithArgs(ctx, startup.Args(m.cfg))
	}
	if err == nil {
		_ = m.store.SetKV(ctx, "pending_restart", "false")
	}
	return err
}

func validationIssueSummary(issues []ValidationIssue) string {
	var parts []string
	for _, issue := range issues {
		if issue.Severity != "error" {
			continue
		}
		if issue.Field != "" {
			parts = append(parts, issue.Field+": "+issue.Message)
		} else {
			parts = append(parts, issue.Message)
		}
	}
	if len(parts) == 0 {
		return "unknown validation error"
	}
	return strings.Join(parts, "; ")
}

func (m Manager) Stop(ctx context.Context) error {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	return m.stopUnlocked(ctx)
}

func (m Manager) stopUnlocked(ctx context.Context) error {
	mode, err := m.RuntimeMode(ctx)
	if err != nil {
		return err
	}
	if mode == RuntimeWindowsSteamCMD {
		return m.stopWindows(ctx)
	}
	return m.runner.Stop(ctx)
}

func (m Manager) Restart(ctx context.Context) error {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	return m.restartUnlocked(ctx)
}

func (m Manager) restartUnlocked(ctx context.Context) error {
	mode, err := m.RuntimeMode(ctx)
	if err != nil {
		return err
	}
	startup, err := m.StartupConfig(ctx)
	if err != nil {
		return err
	}
	if mode == RuntimeWindowsSteamCMD {
		if err := m.stopWindows(ctx); err != nil {
			return fmt.Errorf("stop before restart: %w", err)
		}
		err = m.startWindows(ctx, startup.Args(m.cfg))
	} else {
		err = m.runner.RestartWithArgs(ctx, startup.Args(m.cfg))
	}
	if err == nil {
		_ = m.store.SetKV(ctx, "pending_restart", "false")
	}
	return err
}

func (m Manager) SafeRestart(ctx context.Context, waitSeconds int, message string, notify RestartNotifier) (db.Job, error) {
	if waitSeconds < 5 || waitSeconds > 300 {
		return db.Job{}, fmt.Errorf("waittime must be between 5 and 300 seconds")
	}
	if strings.TrimSpace(message) == "" {
		message = "Server maintenance restart"
	}
	return m.startLifecycleJob(ctx, "safe_restart", "queued safe restart", func(jobCtx context.Context, jobID string) {
		m.update(jobID, "running", 10, "saving world and notifying players", "")
		if notify != nil {
			if err := notify(jobCtx, waitSeconds, message); err != nil {
				m.update(jobID, "running", 20, "notification failed; continuing with managed restart", err.Error())
			}
		}
		m.update(jobID, "running", 35, "waiting for player countdown", "")
		select {
		case <-time.After(time.Duration(waitSeconds) * time.Second):
		case <-jobCtx.Done():
			m.update(jobID, "failed", 35, "restart interrupted", jobCtx.Err().Error())
			return
		}
		m.update(jobID, "running", 55, "stopping server", "")
		if err := m.stopUnlocked(jobCtx); err != nil {
			m.update(jobID, "failed", 55, "stop failed", err.Error())
			return
		}
		m.update(jobID, "running", 75, "starting server", "")
		if err := m.startUnlocked(jobCtx); err != nil {
			m.update(jobID, "failed", 75, "start failed", err.Error())
			return
		}
		m.update(jobID, "completed", 100, "safe restart completed", "")
	})
}

// SafeStop asks Palworld to save and shut down gracefully, then verifies that
// the managed process exited. If the official REST shutdown path is
// unavailable or the process remains alive after the grace period, PalPanel
// falls back to its normal managed stop operation.
func (m Manager) SafeStop(ctx context.Context, waitSeconds int, message string, notify RestartNotifier) (db.Job, error) {
	if waitSeconds < 5 || waitSeconds > 300 {
		return db.Job{}, fmt.Errorf("waittime must be between 5 and 300 seconds")
	}
	if strings.TrimSpace(message) == "" {
		message = "Server is shutting down"
	}
	return m.startLifecycleJob(ctx, "safe_stop", "queued safe stop", func(jobCtx context.Context, jobID string) {
		if status, err := m.Status(jobCtx); err == nil && !serverStatusRunning(status) {
			m.update(jobID, "completed", 100, "server is already stopped", "")
			return
		}

		m.update(jobID, "running", 10, "saving world and notifying players", "")
		if notify != nil {
			if err := notify(jobCtx, waitSeconds, message); err != nil {
				m.update(jobID, "running", 20, "graceful shutdown request failed; managed stop fallback remains armed", err.Error())
			}
		}

		m.update(jobID, "running", 35, "waiting for player countdown", "")
		wait := m.lifecycleWait
		if wait == nil {
			wait = waitForLifecycleDuration
		}
		if err := wait(jobCtx, time.Duration(waitSeconds)*time.Second); err != nil {
			m.update(jobID, "failed", 35, "safe stop interrupted", err.Error())
			return
		}

		m.update(jobID, "running", 60, "waiting for graceful server exit", "")
		deadline := time.Now().Add(m.gracefulStopTimeout)
		for {
			status, err := m.Status(jobCtx)
			if err == nil && !serverStatusRunning(status) {
				m.update(jobID, "completed", 100, "safe stop completed", "")
				return
			}
			if !time.Now().Before(deadline) {
				break
			}
			if err := wait(jobCtx, m.gracefulStopPoll); err != nil {
				m.update(jobID, "failed", 60, "safe stop interrupted", err.Error())
				return
			}
		}

		m.update(jobID, "running", 85, "graceful shutdown timed out; applying managed stop", "")
		if err := m.stopUnlocked(jobCtx); err != nil {
			m.update(jobID, "failed", 85, "managed stop fallback failed", err.Error())
			return
		}
		m.update(jobID, "completed", 100, "safe stop completed with managed stop fallback", "")
	})
}

func waitForLifecycleDuration(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func serverStatusRunning(status Status) bool {
	state := strings.ToLower(strings.TrimSpace(status.Container.Status))
	if !status.Container.Exists {
		return false
	}
	switch state {
	case "exited", "dead", "missing", "stopped":
		return false
	default:
		return true
	}
}

func (m Manager) Logs(ctx context.Context, query LogQuery) (LogResult, error) {
	channel := strings.ToLower(strings.TrimSpace(query.Channel))
	switch channel {
	case "game":
		return m.fileChannelLogs(ctx, query, "paldefender-game", m.latestGameLogPath)
	case "paldefender-rest":
		return m.fileChannelLogs(ctx, query, "paldefender-rest", m.latestPalDefenderRESTLogPath)
	case "", "launcher":
	default:
		return LogResult{}, fmt.Errorf("unsupported log channel %q", query.Channel)
	}

	mode, err := m.RuntimeMode(ctx)
	if err != nil {
		return LogResult{}, err
	}
	tail := query.Tail
	logs, fileInfo, fileErr := tailFileInfo(m.cfg.ServerLogPath(), tail)
	if fileErr == nil && fileInfo != nil {
		result := LogResult{
			Logs:      filterLogs(logs, query),
			Source:    "file",
			Available: true,
			UpdatedAt: fileInfo.ModTime().UTC().Format(time.RFC3339Nano),
		}
		if strings.TrimSpace(logs) == "" {
			result.Reason = m.emptyLogReason(ctx)
		}
		return result, nil
	}
	if mode == RuntimeWineDocker {
		dockerLogs, dockerErr := m.runner.Logs(ctx, tail)
		if dockerErr == nil {
			result := LogResult{Logs: filterLogs(dockerLogs, query), Source: "docker", Available: true}
			if strings.TrimSpace(dockerLogs) == "" {
				result.Reason = m.emptyLogReason(ctx)
			}
			return result, nil
		}
	}
	reason := "no_collection_source"
	status, statusErr := m.Status(ctx)
	if statusErr == nil && !status.Container.Exists {
		reason = "not_started"
	}
	return LogResult{Source: "none", Available: false, Reason: reason}, nil
}

func (m Manager) fileChannelLogs(ctx context.Context, query LogQuery, source string, path func() (string, error)) (LogResult, error) {
	logPath, err := path()
	if err != nil {
		return LogResult{}, err
	}
	if logPath == "" {
		return LogResult{Source: "none", Available: false, Reason: "no_collection_source"}, nil
	}
	logs, info, err := tailFileInfo(logPath, query.Tail)
	if err != nil {
		return LogResult{}, err
	}
	result := LogResult{
		Logs:      filterLogs(logs, query),
		Source:    source,
		Available: true,
		UpdatedAt: info.ModTime().UTC().Format(time.RFC3339Nano),
	}
	if strings.TrimSpace(logs) == "" {
		result.Reason = m.emptyLogReason(ctx)
	}
	return result, nil
}

func (m Manager) latestGameLogPath() (string, error) {
	path, err := m.latestLogFile(m.cfg.PalDefenderDir(), filepath.Join("Logs"))
	if err != nil || path != "" {
		return path, err
	}
	return m.latestLogFile(m.cfg.ServerDirectory(), filepath.Join("Pal", "Saved", "Logs"))
}

func (m Manager) latestPalDefenderRESTLogPath() (string, error) {
	return m.latestLogFile(m.cfg.PalDefenderDir(), filepath.Join("Logs", "RESTAPI"))
}

func (m Manager) latestLogFile(base, relative string) (string, error) {
	root := filepath.Join(base, relative)
	if err := m.cfg.ValidateManagedPath(root, false); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var selected string
	var selectedTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if selected == "" || info.ModTime().After(selectedTime) {
			selected = filepath.Join(root, entry.Name())
			selectedTime = info.ModTime()
		}
	}
	return selected, nil
}

func (m Manager) emptyLogReason(ctx context.Context) string {
	status, err := m.Status(ctx)
	if err != nil {
		return "no_available_output"
	}
	if status.Container.Status == "running" || status.Container.Status == "starting" || status.Container.Status == "restarting" {
		return "waiting_for_output"
	}
	if !status.Container.Exists {
		return "not_started"
	}
	return "no_available_output"
}

func (m Manager) Status(ctx context.Context) (Status, error) {
	mode, err := m.RuntimeMode(ctx)
	if err != nil {
		return Status{}, err
	}
	container := docker.ContainerStatus{Exists: false, Status: "missing"}
	var statusErr error
	if mode == RuntimeWineDocker {
		container, err = m.runner.Status(ctx)
		if err != nil {
			statusErr = err
			container = docker.ContainerStatus{Exists: false, Status: "error"}
		}
	} else {
		container, statusErr = m.windowsStatus(ctx)
	}
	installedValue, ok, err := m.store.GetKV(ctx, kvInstalled)
	if err != nil {
		return Status{}, err
	}
	pendingValue, _, err := m.store.GetKV(ctx, "pending_restart")
	if err != nil {
		return Status{}, err
	}
	installed := ok && installedValue == "true"
	var installErr error
	if mode == RuntimeWindowsSteamCMD {
		installErr = m.validateWindowsServerInstall()
		installed = installErr == nil
	} else if !installed {
		installed = fileExists(m.cfg.PalServerExePath())
	}
	configExists := fileExists(m.cfg.PalWorldSettingsPath())
	startup, _ := m.StartupConfig(ctx)
	warnings := m.statusWarnings(mode, installed, configExists)
	if statusErr != nil {
		warnings = append(warnings, statusErr.Error())
	}
	if installErr != nil {
		warnings = append(warnings, installErr.Error())
	}
	return Status{
		Installed:      installed,
		PendingRestart: parseBool(pendingValue),
		RuntimeMode:    mode,
		SetupStep:      setupStep(installed, configExists, container.Status),
		ConfigExists:   configExists,
		Container:      container,
		StartupArgs:    startup.Args(m.cfg),
		Ports: map[string]int{
			"game":  startup.Normalize(m.cfg).Port,
			"query": m.cfg.QueryPort,
			"rcon":  m.cfg.EffectiveRCONPort(),
			"rest":  m.cfg.RESTPort,
		},
		Warnings:       warnings,
		ServerImported: m.cfg.ServerDirectoryImported(),
		Paths: map[string]string{
			"data":               m.cfg.DataDir,
			"server":             m.cfg.ServerDirectory(),
			"palworld_settings":  m.cfg.PalWorldSettingsPath(),
			"default_settings":   m.cfg.DefaultPalWorldSettingsPath(),
			"pal_mod_settings":   m.cfg.PalModSettingsPath(),
			"workshop_mods":      m.cfg.WorkshopModsDir(),
			"legacy_mods":        m.cfg.LegacyModsDir(),
			"steamcmd":           m.cfg.SteamCMDBinaryPath(),
			"paldefender":        m.cfg.PalDefenderDir(),
			"windows_server_log": m.cfg.ServerLogPath(),
			"paldefender_win64":  m.cfg.Win64Dir(),
		},
	}, nil
}

func (m Manager) MarkPendingRestart(ctx context.Context) error {
	return m.store.SetKV(ctx, "pending_restart", "true")
}

func (m Manager) Backup(ctx context.Context) (db.Job, error) {
	return m.startLifecycleJob(ctx, "backup", "queued backup", func(jobCtx context.Context, jobID string) {
		m.update(jobID, "running", 20, "creating backup archive", "")
		backup, err := m.createBackupArchive("manual")
		if err != nil {
			m.update(jobID, "failed", 20, "backup failed", err.Error())
			return
		}
		if err := m.maybeUploadBackup(jobCtx, jobID, backup); err != nil {
			m.update(jobID, "failed", 70, "backup created but WebDAV upload failed; local backup retained", err.Error())
			return
		}
		m.update(jobID, "completed", 100, "backup created: "+backup.Name, "")
	})
}

func (m Manager) ListBackups() ([]BackupInfo, error) {
	entries, err := os.ReadDir(m.cfg.BackupsDir)
	if err != nil {
		return nil, err
	}
	out := []BackupInfo{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".zip") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, BackupInfo{
			Name:      entry.Name(),
			Path:      filepath.Join(m.cfg.BackupsDir, entry.Name()),
			SizeBytes: info.Size(),
			CreatedAt: info.ModTime().UTC().Format(time.RFC3339Nano),
			Reason:    backupReason(entry.Name()),
			Status:    "available",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (m Manager) RestoreBackup(ctx context.Context, name string) (db.Job, error) {
	pathAbs, name, err := m.backupPath(name)
	if err != nil {
		return db.Job{}, err
	}
	if !fileExists(pathAbs) {
		return db.Job{}, fmt.Errorf("backup not found")
	}
	return m.startLifecycleJob(ctx, "restore", "queued backup restore", func(jobCtx context.Context, jobID string) {
		m.update(jobID, "running", 5, "verifying backup archive", "")
		result, err := verifyBackupArchive(pathAbs, name)
		if err != nil {
			m.update(jobID, "failed", 5, "backup verify failed", err.Error())
			return
		}
		if !result.Valid {
			m.update(jobID, "failed", 5, "backup verify failed", strings.Join(result.Errors, "; "))
			return
		}
		m.update(jobID, "running", 10, "stopping server before restore", "")
		if err := m.stopUnlocked(jobCtx); err != nil {
			m.update(jobID, "failed", 10, "stop failed", err.Error())
			return
		}
		m.update(jobID, "running", 30, "creating pre-restore backup", "")
		if _, err := m.createBackupArchive("pre-restore"); err != nil {
			m.update(jobID, "failed", 30, "pre-restore backup failed", err.Error())
			return
		}
		m.update(jobID, "running", 65, "restoring backup archive", "")
		validateTarget := func(path string) error { return m.cfg.ValidateManagedPath(path, false) }
		if err := extractZipSafeValidated(pathAbs, m.cfg.ServerDirectory(), validateTarget); err != nil {
			m.update(jobID, "failed", 65, "restore failed", err.Error())
			return
		}
		if err := m.store.SetKV(jobCtx, "pending_restart", "true"); err != nil {
			m.update(jobID, "failed", 90, "restore state persistence failed", err.Error())
			return
		}
		m.update(jobID, "completed", 100, "backup restored; start the server after verifying files", "")
	})
}

func (m Manager) BackupDownloadPath(name string) (string, error) {
	path, _, err := m.backupPath(name)
	if err != nil {
		return "", err
	}
	if !fileExists(path) {
		return "", fmt.Errorf("backup not found")
	}
	return path, nil
}

func (m Manager) DeleteBackup(name string) error {
	path, _, err := m.backupPath(name)
	if err != nil {
		return err
	}
	if !fileExists(path) {
		return fmt.Errorf("backup not found")
	}
	if err := m.cfg.ValidateManagedPath(path, false); err != nil {
		return err
	}
	if err := m.cfg.ValidateManagedPath(path, false); err != nil {
		return err
	}
	return os.Remove(path)
}

func (m Manager) VerifyBackup(name string) (BackupVerifyResult, error) {
	path, cleanName, err := m.backupPath(name)
	if err != nil {
		return BackupVerifyResult{}, err
	}
	if !fileExists(path) {
		return BackupVerifyResult{}, fmt.Errorf("backup not found")
	}
	return verifyBackupArchive(path, cleanName)
}

func (m Manager) startJob(ctx context.Context, typ, message string, fn func(context.Context, string)) (db.Job, error) {
	return m.jobs.Submit(ctx, jobs.ClassGeneral, typ, message, fn)
}

func (m Manager) startLifecycleJob(ctx context.Context, typ, message string, fn func(context.Context, string)) (db.Job, error) {
	return m.jobs.Submit(ctx, jobs.ClassLifecycle, typ, message, func(jobCtx context.Context, jobID string) {
		m.operationMu.Lock()
		defer m.operationMu.Unlock()
		fn(jobCtx, jobID)
	})
}

func (m Manager) runJobStageWithHeartbeat(ctx context.Context, jobID string, progress int, message string, run func() error) error {
	interval := m.jobHeartbeatInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	done := make(chan struct{})
	heartbeatDone := make(chan struct{})
	started := time.Now()
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.update(jobID, "running", progress, fmt.Sprintf("%s (still running; elapsed %s)", message, time.Since(started).Round(time.Second)), "")
			}
		}
	}()
	err := run()
	close(done)
	<-heartbeatDone
	return err
}

func (m Manager) update(jobID, status string, progress int, message, errText string) {
	if err := m.jobs.Update(jobID, status, progress, message, errText); err != nil {
		log.Printf("job %s update failed: %v", jobID, err)
	}
}

func (m Manager) installOrUpdateRuntime(ctx context.Context, mode string) error {
	if m.installOrUpdateFunc != nil {
		return m.installOrUpdateFunc(ctx, mode)
	}
	if mode == RuntimeWindowsSteamCMD {
		return m.installOrUpdateWindows(ctx)
	}
	return m.runner.InstallOrUpdate(ctx)
}

func (m Manager) startWindows(ctx context.Context, args []string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("windows_steamcmd runtime requires Windows host")
	}
	if !fileExists(m.cfg.PalServerExePath()) {
		return fmt.Errorf("PalServer.exe not found")
	}
	if err := os.MkdirAll(filepath.Dir(m.cfg.ServerLogPath()), 0o755); err != nil {
		return err
	}
	logFile, err := newRollingLogWriter(m.cfg.ServerLogPath(), 20*1024*1024, 5)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(logFile, "%s [palpanel] starting PalServer.exe\n", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("write PalServer lifecycle log: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = logFile.Close()
		return err
	}
	cmd := exec.Command(m.cfg.PalServerExePath(), args...)
	cmd.Dir = m.cfg.ServerDirectory()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	prepareWindowsProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	tracked, trackingErr := trackWindowsProcess(cmd.Process)
	if tracked == nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = logFile.Close()
		return fmt.Errorf("track started PalServer process: %w", trackingErr)
	}
	if trackingErr != nil {
		log.Printf("PalServer PID %d could not be assigned to a Windows Job Object; using native tree cleanup: %v", cmd.Process.Pid, trackingErr)
	}
	info, err := m.processInfo(cmd.Process.Pid)
	if err != nil || !info.Running {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = logFile.Close()
		finishTrackedWindowsProcess(tracked)
		if err != nil {
			return fmt.Errorf("inspect started PalServer process: %w", err)
		}
		return fmt.Errorf("PalServer exited before its process identity could be recorded")
	}
	record := windowsProcessRecord{PID: cmd.Process.Pid, Executable: info.Executable, CreationTime: info.CreationTime}
	if err := m.persistWindowsProcess(record); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = logFile.Close()
		finishTrackedWindowsProcess(tracked)
		return fmt.Errorf("persist PalServer process identity: %w", err)
	}
	go func() {
		waitErr := cmd.Wait()
		if waitErr != nil {
			_, _ = fmt.Fprintf(logFile, "%s [palpanel] PalServer.exe exited: %v\n", time.Now().UTC().Format(time.RFC3339Nano), waitErr)
		} else {
			_, _ = fmt.Fprintf(logFile, "%s [palpanel] PalServer.exe exited normally\n", time.Now().UTC().Format(time.RFC3339Nano))
		}
		_ = logFile.Close()
		finishTrackedWindowsProcess(tracked)
		m.clearWindowsProcessIfMatch(record)
	}()
	return nil
}

func (m Manager) stopWindows(ctx context.Context) error {
	record, ok, err := m.loadWindowsProcess(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	running, err := m.verifyWindowsProcess(record)
	if err != nil {
		return err
	}
	if running {
		if err := m.terminateProcessTree(ctx, record.PID); err != nil {
			return err
		}
	}
	return m.clearWindowsProcess(ctx)
}

func (m Manager) windowsStatus(ctx context.Context) (docker.ContainerStatus, error) {
	record, ok, err := m.loadWindowsProcess(ctx)
	if err != nil {
		return docker.ContainerStatus{Exists: false, Status: "error"}, err
	}
	if !ok {
		return docker.ContainerStatus{Exists: false, Status: "missing"}, nil
	}
	running, err := m.verifyWindowsProcess(record)
	if err != nil {
		return docker.ContainerStatus{Exists: false, Status: "error"}, err
	}
	if !running {
		_ = m.clearWindowsProcess(ctx)
		return docker.ContainerStatus{Exists: false, Status: "missing"}, nil
	}
	return docker.ContainerStatus{Exists: true, Status: "running"}, nil
}

func (m Manager) processInfo(pid int) (windowsProcessInfo, error) {
	inspect := m.inspectProcess
	if inspect == nil {
		inspect = inspectWindowsProcess
	}
	return inspect(pid)
}

func (m Manager) terminateProcessTree(ctx context.Context, pid int) error {
	terminate := m.terminateProcess
	if terminate == nil {
		terminate = terminateWindowsProcessTree
	}
	return terminate(ctx, pid)
}

func (m Manager) persistWindowsProcess(record windowsProcessRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.store.SetKV(ctx, kvProcess, string(raw)); err != nil {
		return err
	}
	if err := m.store.SetKV(ctx, kvPID, strconv.Itoa(record.PID)); err != nil {
		_ = m.store.SetKV(context.Background(), kvProcess, "")
		return err
	}
	return nil
}

func (m Manager) loadWindowsProcess(ctx context.Context) (windowsProcessRecord, bool, error) {
	raw, ok, err := m.store.GetKV(ctx, kvProcess)
	if err != nil {
		return windowsProcessRecord{}, false, err
	}
	if ok && strings.TrimSpace(raw) != "" {
		var record windowsProcessRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return windowsProcessRecord{}, false, fmt.Errorf("invalid managed Windows process record: %w", err)
		}
		if record.PID <= 0 || strings.TrimSpace(record.Executable) == "" {
			return windowsProcessRecord{}, false, fmt.Errorf("invalid managed Windows process identity")
		}
		return record, true, nil
	}
	legacyPID, ok, err := m.store.GetKV(ctx, kvPID)
	if err != nil {
		return windowsProcessRecord{}, false, err
	}
	if !ok || strings.TrimSpace(legacyPID) == "" {
		return windowsProcessRecord{}, false, nil
	}
	pid, err := strconv.Atoi(strings.TrimSpace(legacyPID))
	if err != nil || pid <= 0 {
		return windowsProcessRecord{}, false, fmt.Errorf("invalid legacy Windows process ID")
	}
	return windowsProcessRecord{PID: pid, Executable: m.cfg.PalServerExePath()}, true, nil
}

func (m Manager) verifyWindowsProcess(record windowsProcessRecord) (bool, error) {
	info, err := m.processInfo(record.PID)
	if err != nil {
		if isWindowsProcessMissing(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect managed PalServer process %d: %w", record.PID, err)
	}
	if !info.Running {
		return false, nil
	}
	if !sameWindowsExecutable(info.Executable, record.Executable) || !sameWindowsExecutable(info.Executable, m.cfg.PalServerExePath()) {
		return false, fmt.Errorf("refusing to manage PID %d because its executable identity does not match PalServer.exe", record.PID)
	}
	if record.CreationTime != 0 && info.CreationTime != record.CreationTime {
		return false, fmt.Errorf("refusing to manage PID %d because its creation time changed", record.PID)
	}
	return true, nil
}

func (m Manager) clearWindowsProcess(ctx context.Context) error {
	if err := m.store.SetKV(ctx, kvPID, ""); err != nil {
		return err
	}
	return m.store.SetKV(ctx, kvProcess, "")
}

func (m Manager) clearWindowsProcessIfMatch(record windowsProcessRecord) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	current, ok, err := m.loadWindowsProcess(ctx)
	if err != nil || !ok || current.PID != record.PID {
		return
	}
	if current.CreationTime != 0 && record.CreationTime != 0 && current.CreationTime != record.CreationTime {
		return
	}
	_ = m.clearWindowsProcess(ctx)
}

func (m Manager) statusWarnings(mode string, installed, configExists bool) []string {
	var warnings []string
	if mode == RuntimeWineDocker && runtime.GOOS != "linux" {
		warnings = append(warnings, "Docker Desktop on Windows/macOS is not recommended by official docs for production save-data IO; create backups before updates.")
	}
	if !installed {
		warnings = append(warnings, "server is not installed")
	}
	if !configExists {
		warnings = append(warnings, "PalWorldSettings.ini is not initialized")
	}
	if dirExists(m.cfg.LegacyModsDir()) {
		warnings = append(warnings, "legacy mod path detected; official server mods now use server-root Mods/Workshop and Mods/PalModSettings.ini")
	}
	return warnings
}

func (m Manager) createBackupArchive(reason string) (BackupInfo, error) {
	if err := os.MkdirAll(m.cfg.BackupsDir, 0o755); err != nil {
		return BackupInfo{}, err
	}
	now := time.Now().UTC()
	createdAt := now.Format(time.RFC3339Nano)
	name := fmt.Sprintf("%s-%s.zip", now.Format("20060102T150405.000000000Z"), reason)
	path := filepath.Join(m.cfg.BackupsDir, name)
	out, err := os.Create(path)
	if err != nil {
		return BackupInfo{}, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(path)
		}
	}()
	zw := zip.NewWriter(out)
	var files []BackupManifestFile
	for _, root := range []string{
		filepath.Join(m.cfg.ServerDirectory(), "Pal", "Saved"),
		m.cfg.ModsDir(),
		m.cfg.PalDefenderDir(),
		filepath.Join(m.cfg.Win64Dir(), "PalDefender.dll"),
		filepath.Join(m.cfg.Win64Dir(), "d3d9.dll"),
	} {
		added, err := addPathToZip(zw, m.cfg.ServerDirectory(), root)
		if err != nil {
			_ = zw.Close()
			_ = out.Close()
			return BackupInfo{}, err
		}
		files = append(files, added...)
	}
	manifest := BackupManifest{Version: 1, Reason: reason, CreatedAt: createdAt, Files: files}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = zw.Close()
		_ = out.Close()
		return BackupInfo{}, err
	}
	w, err := zw.Create(".palpanel-backup.json")
	if err != nil {
		_ = zw.Close()
		_ = out.Close()
		return BackupInfo{}, err
	}
	if _, err := w.Write(append(manifestBytes, '\n')); err != nil {
		_ = zw.Close()
		_ = out.Close()
		return BackupInfo{}, err
	}
	if err := zw.Close(); err != nil {
		_ = out.Close()
		return BackupInfo{}, err
	}
	if err := out.Close(); err != nil {
		return BackupInfo{}, err
	}
	complete = true
	info, err := os.Stat(path)
	if err != nil {
		return BackupInfo{}, err
	}
	return BackupInfo{Name: name, Path: path, SizeBytes: info.Size(), CreatedAt: info.ModTime().UTC().Format(time.RFC3339Nano), Reason: reason, Status: "available"}, nil
}

func (m Manager) snapshotUpdateProtectedFiles() (map[string]string, error) {
	return snapshotFiles(m.cfg.ServerDirectory(), []string{
		filepath.Join(m.cfg.ServerDirectory(), "Pal", "Saved"),
		m.cfg.ModsDir(),
		m.cfg.PalDefenderDir(),
		filepath.Join(m.cfg.Win64Dir(), "PalDefender.dll"),
		filepath.Join(m.cfg.Win64Dir(), "d3d9.dll"),
	})
}

func (m Manager) verifyUpdateProtectedFiles(before map[string]string) error {
	after, err := m.snapshotUpdateProtectedFiles()
	if err != nil {
		return err
	}
	changes := []string{}
	for path, hash := range before {
		current, ok := after[path]
		if !ok {
			changes = append(changes, "removed "+path)
		} else if current != hash {
			changes = append(changes, "modified "+path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			changes = append(changes, "added "+path)
		}
	}
	if len(changes) == 0 {
		return nil
	}
	sort.Strings(changes)
	if len(changes) > 10 {
		changes = append(changes[:10], fmt.Sprintf("and %d more", len(changes)-10))
	}
	return fmt.Errorf("%s", strings.Join(changes, "; "))
}

func snapshotFiles(base string, roots []string) (map[string]string, error) {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, root := range roots {
		if !fileExists(root) && !dirExists(root) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(baseAbs, path)
			if err != nil {
				return err
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			hasher := sha256.New()
			_, copyErr := io.Copy(hasher, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			out[filepath.ToSlash(rel)] = hex.EncodeToString(hasher.Sum(nil))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func retainedBackupMessage(backup BackupInfo) string {
	if strings.TrimSpace(backup.Path) == "" {
		return ""
	}
	return "; verified backup retained at " + backup.Path
}

func addPathToZip(zw *zip.Writer, base, path string) ([]BackupManifestFile, error) {
	if !fileExists(path) && !dirExists(path) {
		return nil, nil
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	var files []BackupManifestFile
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(baseAbs, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		w, err := zw.Create(rel)
		if err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		hasher := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(w, hasher), in)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		// Files such as PalDefender logs may grow while an online backup is
		// being created. Record the bytes actually written to the archive,
		// rather than a size sampled before the copy started.
		files = append(files, BackupManifestFile{Path: rel, Size: written, SHA256: hex.EncodeToString(hasher.Sum(nil))})
		return nil
	})
	return files, err
}

func extractZipSafe(zipPath, dst string) error {
	return extractZipSafeValidated(zipPath, dst, nil)
}

func extractZipSafeValidated(zipPath, dst string, validate func(string) error) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	dst, err = filepath.Abs(dst)
	if err != nil {
		return err
	}
	if len(reader.File) > 200_000 {
		return fmt.Errorf("zip contains too many entries")
	}
	if validate != nil {
		if err := validate(dst); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(reader.File))
	const maxExtractedBytes int64 = 64 << 30
	var declaredBytes int64
	var extractedBytes int64
	for _, file := range reader.File {
		if file.Name == ".palpanel-backup.json" {
			continue
		}
		if unsafeZipName(file.Name) {
			return fmt.Errorf("zip contains unsafe path: %s", file.Name)
		}
		normalized := strings.ReplaceAll(strings.TrimSuffix(file.Name, "/"), "\\", "/")
		key := strings.ToLower(normalized)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("zip contains duplicate path: %s", file.Name)
		}
		seen[key] = struct{}{}
		if file.UncompressedSize64 > uint64(maxExtractedBytes) || declaredBytes > maxExtractedBytes-int64(file.UncompressedSize64) {
			return fmt.Errorf("zip exceeds the extracted size limit")
		}
		declaredBytes += int64(file.UncompressedSize64)
		mode := file.Mode()
		isDirectory := file.FileInfo().IsDir()
		if mode&os.ModeSymlink != 0 || (!isDirectory && mode.Type() != 0) {
			return fmt.Errorf("zip contains unsupported file type: %s", file.Name)
		}
		target := filepath.Join(dst, filepath.FromSlash(normalized))
		targetAbs, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dst, targetAbs)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("zip contains unsafe path: %s", file.Name)
		}
		if validate != nil {
			if err := validate(targetAbs); err != nil {
				return err
			}
		}
		if isDirectory {
			if err := os.MkdirAll(targetAbs, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
			return err
		}
		if validate != nil {
			if err := validate(targetAbs); err != nil {
				return err
			}
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		permissions := mode.Perm()
		if permissions == 0 {
			permissions = 0o644
		}
		out, err := os.OpenFile(targetAbs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, permissions)
		if err != nil {
			_ = in.Close()
			return err
		}
		remaining := maxExtractedBytes - extractedBytes
		written, copyErr := io.Copy(out, io.LimitReader(in, remaining+1))
		extractedBytes += written
		closeErr := out.Close()
		_ = in.Close()
		if copyErr != nil {
			return copyErr
		}
		if extractedBytes > maxExtractedBytes {
			return fmt.Errorf("zip exceeds the extracted size limit")
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func setupStep(installed, configExists bool, processState string) string {
	if !installed {
		return "prerequisites"
	}
	if !configExists {
		return "installed"
	}
	if processState == "running" {
		return "ready"
	}
	return "configured"
}

func tailFile(path string, tail int) (string, error) {
	logs, _, err := tailFileInfo(path, tail)
	return logs, err
}

func tailFileInfo(path string, tail int) (string, os.FileInfo, error) {
	if tail <= 0 || tail > 5000 {
		tail = 200
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if info.IsDir() {
		return "", nil, fmt.Errorf("log path is a directory")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	const maximumLogTailBytes int64 = 2 * 1024 * 1024
	start := info.Size() - maximumLogTailBytes
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return "", nil, err
	}
	b, err := io.ReadAll(io.LimitReader(file, maximumLogTailBytes))
	if err != nil {
		return "", nil, err
	}
	if start > 0 {
		if newline := strings.IndexByte(string(b), '\n'); newline >= 0 {
			b = b[newline+1:]
		}
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	return strings.Join(lines, "\n"), info, nil
}

func filterLogs(logs string, query LogQuery) string {
	search := strings.ToLower(strings.TrimSpace(query.Search))
	level := strings.ToLower(strings.TrimSpace(query.Level))
	since := strings.TrimSpace(query.Since)
	if search == "" && level == "" && since == "" {
		return logs
	}
	lines := strings.Split(logs, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		lower := strings.ToLower(line)
		if search != "" && !strings.Contains(lower, search) {
			continue
		}
		if level != "" && level != "all" && !strings.Contains(lower, level) {
			continue
		}
		if since != "" && !strings.Contains(line, since) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func backupReason(name string) string {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if i := strings.Index(name, "-"); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return "manual"
}

func (m Manager) backupPath(name string) (string, string, error) {
	trimmed := strings.TrimSpace(name)
	cleanName := filepath.Base(trimmed)
	if cleanName == "." || cleanName == "" || cleanName != trimmed || !strings.HasSuffix(strings.ToLower(cleanName), ".zip") {
		return "", "", fmt.Errorf("invalid backup name")
	}
	baseAbs, err := filepath.Abs(m.cfg.BackupsDir)
	if err != nil {
		return "", "", err
	}
	pathAbs, err := filepath.Abs(filepath.Join(m.cfg.BackupsDir, cleanName))
	if err != nil {
		return "", "", err
	}
	if pathAbs != baseAbs && !strings.HasPrefix(pathAbs, baseAbs+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("invalid backup path")
	}
	return pathAbs, cleanName, nil
}

func verifyBackupArchive(path, name string) (BackupVerifyResult, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return BackupVerifyResult{}, err
	}
	defer reader.Close()

	result := BackupVerifyResult{Name: name, Valid: true, Format: "legacy"}
	byPath := map[string]*zip.File{}
	var manifestFile *zip.File
	for _, file := range reader.File {
		if file.Name == ".palpanel-backup.json" {
			manifestFile = file
			continue
		}
		if unsafeZipName(file.Name) {
			result.Errors = append(result.Errors, "unsafe path: "+file.Name)
		}
		if !file.FileInfo().IsDir() {
			byPath[file.Name] = file
		}
	}
	if manifestFile == nil {
		for name, file := range byPath {
			if err := readZipFile(file); err != nil {
				result.Errors = append(result.Errors, "read failed: "+name+": "+err.Error())
				continue
			}
			result.CheckedFiles++
		}
		result.Valid = len(result.Errors) == 0
		return result, nil
	}

	result.Format = "manifest_v1"
	manifestReader, err := manifestFile.Open()
	if err != nil {
		return BackupVerifyResult{}, err
	}
	var manifest BackupManifest
	if err := json.NewDecoder(manifestReader).Decode(&manifest); err != nil {
		_ = manifestReader.Close()
		result.Valid = false
		result.Errors = append(result.Errors, "manifest parse failed: "+err.Error())
		return result, nil
	}
	_ = manifestReader.Close()
	if manifest.Version != 1 {
		result.Errors = append(result.Errors, fmt.Sprintf("unsupported manifest version: %d", manifest.Version))
	}

	expectedPaths := map[string]bool{}
	for _, expected := range manifest.Files {
		expectedPaths[expected.Path] = true
		file := byPath[expected.Path]
		if file == nil {
			result.Errors = append(result.Errors, "missing file: "+expected.Path)
			continue
		}
		if file.UncompressedSize64 != uint64(expected.Size) {
			result.Errors = append(result.Errors, "size mismatch: "+expected.Path)
		}
		r, err := file.Open()
		if err != nil {
			result.Errors = append(result.Errors, "read failed: "+expected.Path)
			continue
		}
		hasher := sha256.New()
		_, copyErr := io.Copy(hasher, r)
		closeErr := r.Close()
		if copyErr != nil {
			result.Errors = append(result.Errors, "hash failed: "+expected.Path)
			continue
		}
		if closeErr != nil {
			result.Errors = append(result.Errors, "close failed: "+expected.Path)
			continue
		}
		if !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expected.SHA256) {
			result.Errors = append(result.Errors, "sha256 mismatch: "+expected.Path)
		}
		result.CheckedFiles++
	}
	for path := range byPath {
		if !expectedPaths[path] {
			result.Errors = append(result.Errors, "unexpected file: "+path)
		}
	}
	result.Valid = len(result.Errors) == 0
	return result, nil
}

func readZipFile(file *zip.File) error {
	r, err := file.Open()
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(io.Discard, r)
	closeErr := r.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func unsafeZipName(name string) bool {
	normalized := strings.ReplaceAll(name, "\\", "/")
	normalized = strings.TrimSuffix(normalized, "/")
	if strings.TrimSpace(normalized) == "" || strings.HasPrefix(normalized, "/") || strings.Contains(normalized, ":") {
		return true
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(normalized)))
	if clean != normalized || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(filepath.FromSlash(clean)) {
		return true
	}
	for _, component := range strings.Split(clean, "/") {
		if !safeWindowsArchiveComponent(component) {
			return true
		}
	}
	return false
}

func safeWindowsArchiveComponent(component string) bool {
	if component == "" || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") || strings.ContainsAny(component, `<>"|?*`) {
		return false
	}
	for _, character := range component {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	base := component
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	base = strings.ToUpper(base)
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CLOCK$" {
		return false
	}
	return !(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9')
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func parseBool(v string) bool {
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false
	}
	return b
}

var ErrNotReady = errors.New("server is not ready")
