package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"palpanel/internal/api"
	"palpanel/internal/appconfig"
	panelauth "palpanel/internal/auth"
	"palpanel/internal/buildinfo"
	"palpanel/internal/db"
	"palpanel/internal/debuglog"
	"palpanel/internal/docker"
	"palpanel/internal/incidents"
	"palpanel/internal/jobs"
	"palpanel/internal/mods"
	"palpanel/internal/monitor"
	"palpanel/internal/networkproxy"
	"palpanel/internal/paldefender"
	"palpanel/internal/palrest"
	"palpanel/internal/scheduler"
	"palpanel/internal/server"
	"palpanel/internal/steamcmd"
)

func main() {
	if err := runWithIO(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		if rollbackErr := server.RollbackPanelUpdateOnStartupFailure(err); rollbackErr != nil {
			log.Printf("palpanel update startup rollback: %v", rollbackErr)
		}
		log.Printf("palpanel: %v", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return runWithIO(args, os.Stdin, os.Stdout, os.Stderr)
}

func runWithIO(args []string, input io.Reader, output, errorOutput io.Writer) error {
	if len(args) > 0 && args[0] == "admin" {
		return runAdminWithIO(args[1:], input, output, errorOutput)
	}
	if len(args) > 0 && args[0] == "network-proxy-bridge" {
		return runNetworkProxyBridge(args[1:], output, errorOutput)
	}
	fs := flag.NewFlagSet("palpanel", flag.ContinueOnError)
	fs.SetOutput(errorOutput)
	configPath := fs.String("config", "", "path to palpanel.env")
	runtimeRoot := fs.String("runtime-root", "", "managed runtime root (overrides PALPANEL_RUNTIME_ROOT)")
	initConfig := fs.Bool("init-config", false, "create a secure production configuration")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *showVersion {
		info := buildinfo.Current()
		fmt.Fprintf(output, "palpanel %s (commit %s, built %s)\n", info.Version, info.Commit, info.BuildTime)
		return nil
	}
	if strings.TrimSpace(*runtimeRoot) != "" {
		layout, err := appconfig.ResolveRuntimeLayout(*runtimeRoot)
		if err != nil {
			return err
		}
		previous, existed := os.LookupEnv("PALPANEL_RUNTIME_ROOT")
		if err := os.Setenv("PALPANEL_RUNTIME_ROOT", layout.RuntimeRoot); err != nil {
			return err
		}
		defer func() {
			if existed {
				_ = os.Setenv("PALPANEL_RUNTIME_ROOT", previous)
			} else {
				_ = os.Unsetenv("PALPANEL_RUNTIME_ROOT")
			}
		}()
	}
	if *initConfig {
		path := *configPath
		if path == "" {
			layout, err := appconfig.ResolveRuntimeLayout(os.Getenv("PALPANEL_RUNTIME_ROOT"))
			if err != nil {
				return err
			}
			if layout.Structured {
				path = filepath.Join(layout.RuntimeRoot, "config", "palpanel.env")
			} else {
				path = filepath.Join(layout.ApplicationRoot, "config", "palpanel.env")
			}
		}
		created, err := appconfig.InitFile(path)
		if err != nil {
			return fmt.Errorf("initialize config: %w", err)
		}
		if created {
			absolute, _ := filepath.Abs(path)
			fmt.Fprintf(output, "created %s\n", absolute)
			fmt.Fprintln(output, "open the dashboard to register the first administrator")
		} else {
			fmt.Fprintf(output, "config already exists: %s\n", path)
		}
		return nil
	}

	if *configPath != "" {
		values, err := appconfig.ParseEnvFile(*configPath)
		if err != nil {
			return fmt.Errorf("load config file: %w", err)
		}
		restore, err := appconfig.ApplyFileEnvironment(values)
		if err != nil {
			return fmt.Errorf("load config file: %w", err)
		}
		defer restore()
	}
	cfg, err := appconfig.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		return fmt.Errorf("ensure data directories: %w", err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close()
	debugEnabled := cfg.LogLevel == "debug"
	if value, found, readErr := store.GetKV(context.Background(), "debug_logging_enabled"); readErr != nil {
		return fmt.Errorf("read debug logging state: %w", readErr)
	} else if found {
		if parsed, parseErr := strconv.ParseBool(value); parseErr == nil {
			debugEnabled = parsed
		}
	}
	debugLogger, err := debuglog.New(cfg.DebugLogPath(), debugEnabled)
	if err != nil {
		return fmt.Errorf("open debug log: %w", err)
	}
	defer debugLogger.Close()
	previousLogOutput := log.Writer()
	log.SetOutput(io.MultiWriter(errorOutput, debugLogger))
	defer log.SetOutput(previousLogOutput)
	cfg.DebugLogger = debugLogger
	debugLogger.Printf("startup version=%s data_dir=%s listen=%s", buildinfo.Current().Version, cfg.DataDir, cfg.ListenAddr)
	if err := steamcmd.RecoverProxyOverride(cfg); err != nil {
		return fmt.Errorf("recover SteamCMD proxy settings: %w", err)
	}
	if err := server.RestoreImportedServerDirectory(context.Background(), cfg, store); err != nil {
		return fmt.Errorf("restore imported server directory: %w", err)
	}
	jobExecutor := jobs.New(store, 4)
	interrupted, err := jobExecutor.Reconcile(context.Background())
	if err != nil {
		return fmt.Errorf("reconcile interrupted jobs: %w", err)
	}
	if interrupted > 0 {
		log.Printf("marked %d interrupted jobs as failed", interrupted)
	}

	runner := docker.NewRunner(cfg)
	serverManager := server.NewManager(cfg, store, runner, jobExecutor)
	if err := serverManager.RecoverPalworldConfigApply(context.Background()); err != nil {
		return fmt.Errorf("recover interrupted Palworld config apply: %w", err)
	}
	if err := serverManager.MaintainConfigDrafts(context.Background()); err != nil {
		return fmt.Errorf("clean up expired Palworld config drafts: %w", err)
	}
	modsManager := mods.NewManager(cfg, store, runner, jobExecutor)
	palDefenderManager := paldefender.NewManager(cfg, store, jobExecutor).WithServerState(serverManager)
	restClient := palrest.New(cfg.PalworldRESTBaseURL, cfg.PalworldRESTUser, cfg.PalworldRESTPass)
	monitorManager := monitor.New(cfg, store, serverManager, restClient)
	schedulerManager := scheduler.New(store, serverManager, restClient, jobExecutor)
	incidentService := incidents.New(cfg, store)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	incidentDone := incidentService.Start(ctx)
	configCleanupDone := serverManager.StartConfigDraftCleanup(ctx, 15*time.Minute)
	crashGuardDone := serverManager.StartCrashGuard(ctx)
	monitorDone := monitorManager.Start(ctx)
	schedulerDone := schedulerManager.Start(ctx)
	defer func() {
		stop()
		workerCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = waitForBackground(workerCtx, monitorDone, schedulerDone, configCleanupDone, crashGuardDone, incidentDone)
		_ = jobExecutor.Shutdown(workerCtx)
	}()

	router := api.NewRouter(cfg, store, serverManager, modsManager, palDefenderManager, restClient, monitorManager, schedulerManager, incidentService)
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	info := buildinfo.Current()
	log.Printf("palpanel %s listening on %s", info.Version, cfg.ListenAddr)
	log.Printf("data directory: %s", cfg.DataDir)
	if !cfg.RequireAuth {
		log.Printf("warning: PALPANEL_REQUIRE_AUTH is disabled")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()
	server.StartPanelUpdateStartupVerification(cfg, store)
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("run api: %w", err)
		}
		return nil
	case <-ctx.Done():
		log.Printf("shutdown requested")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := waitForBackground(shutdownCtx, monitorDone, schedulerDone, configCleanupDone, crashGuardDone, incidentDone); err != nil {
		return err
	}
	if err := jobExecutor.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("wait for background jobs: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("run api: %w", err)
	}
	log.Printf("shutdown complete")
	return nil
}

func runNetworkProxyBridge(args []string, output, errorOutput io.Writer) error {
	fs := flag.NewFlagSet("palpanel network-proxy-bridge", flag.ContinueOnError)
	fs.SetOutput(errorOutput)
	configPath := fs.String("config", "", "path to palpanel.env")
	addressFile := fs.String("address-file", "", "private file used to publish the loopback bridge address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || strings.TrimSpace(*configPath) == "" || strings.TrimSpace(*addressFile) == "" {
		return errors.New("network-proxy-bridge requires --config and --address-file")
	}
	values, err := appconfig.ParseEnvFile(*configPath)
	if err != nil {
		return fmt.Errorf("load config file: %w", err)
	}
	restore, err := appconfig.ApplyFileEnvironment(values)
	if err != nil {
		return fmt.Errorf("load config file: %w", err)
	}
	defer restore()
	cfg, err := appconfig.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	rawProxy, err := networkproxy.New(cfg).InstallProxyURL()
	if err != nil {
		return err
	}
	if rawProxy == "" {
		return errors.New("install proxy is not enabled")
	}
	bridge, err := networkproxy.StartBridge(rawProxy)
	if err != nil {
		return err
	}
	defer bridge.Close()
	if err := os.MkdirAll(filepath.Dir(*addressFile), 0o700); err != nil {
		return fmt.Errorf("create proxy bridge runtime directory: %w", err)
	}
	if err := os.WriteFile(*addressFile, []byte(bridge.Address()+"\n"), 0o600); err != nil {
		return fmt.Errorf("publish proxy bridge address: %w", err)
	}
	defer os.Remove(*addressFile)
	fmt.Fprintln(output, "network proxy bridge ready")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

func runAdmin(args []string) error {
	return runAdminWithIO(args, os.Stdin, os.Stdout, os.Stderr)
}

func runAdminWithIO(args []string, input io.Reader, output, errorOutput io.Writer) error {
	if len(args) == 0 || args[0] != "reset-password" {
		return errors.New("usage: palpanel admin reset-password [--config path] [--username name]")
	}
	fs := flag.NewFlagSet("palpanel admin reset-password", flag.ContinueOnError)
	fs.SetOutput(errorOutput)
	configPath := fs.String("config", "", "path to palpanel.env")
	username := fs.String("username", "", "administrator username; defaults to the first account")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *configPath != "" {
		values, err := appconfig.ParseEnvFile(*configPath)
		if err != nil {
			return fmt.Errorf("load config file: %w", err)
		}
		restore, err := appconfig.ApplyFileEnvironment(values)
		if err != nil {
			return fmt.Errorf("load config file: %w", err)
		}
		defer restore()
	}
	cfg, err := appconfig.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open panel database: %w", err)
	}
	defer store.Close()
	name := strings.TrimSpace(*username)
	if name == "" {
		user, err := store.FirstUser(context.Background())
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("no administrator account exists; register in the browser first")
		}
		if err != nil {
			return fmt.Errorf("read administrator: %w", err)
		}
		name = user.Username
	}
	reader := bufio.NewReader(input)
	fmt.Fprintf(errorOutput, "New password for %s: ", name)
	password, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	fmt.Fprint(errorOutput, "Confirm password: ")
	confirmation, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read password confirmation: %w", err)
	}
	password = strings.TrimRight(password, "\r\n")
	confirmation = strings.TrimRight(confirmation, "\r\n")
	if password != confirmation {
		return errors.New("passwords do not match")
	}
	if err := panelauth.New(store).ResetPassword(context.Background(), name, password); err != nil {
		return fmt.Errorf("reset administrator password: %w", err)
	}
	fmt.Fprintf(output, "password reset for %s; all sessions and development keys were revoked\n", name)
	return nil
}

func waitForBackground(ctx context.Context, workers ...<-chan struct{}) error {
	for _, worker := range workers {
		select {
		case <-worker:
		case <-ctx.Done():
			return fmt.Errorf("wait for background workers: %w", ctx.Err())
		}
	}
	return nil
}
