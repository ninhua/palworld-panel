package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/docker"
	"palpanel/internal/palconfig"
)

func TestCrashGuardTripsDockerLoopAndRequiresRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	manager, statePath, commandPath, cleanup := newCrashGuardTestManager(t)
	defer cleanup()
	ctx := t.Context()
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	manager.crashGuardNow = func() time.Time { return now }
	manager.crashGuardThreshold = 3
	manager.crashGuardWindow = 10 * time.Minute

	writeCrashGuardDockerState(t, statePath, 0, "running", false, 0)
	if err := manager.ObserveCrashGuard(ctx); err != nil {
		t.Fatal(err)
	}
	for restart := 1; restart <= 3; restart++ {
		now = now.Add(time.Minute)
		writeCrashGuardDockerState(t, statePath, restart, "running", restart == 3, 137)
		if err := manager.ObserveCrashGuard(ctx); err != nil {
			t.Fatal(err)
		}
	}
	status, err := manager.CrashGuardStatus(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Tripped || status.RecentCrashCount != 3 || len(status.Events) != 3 {
		t.Fatalf("unexpected crash guard status: %#v", status)
	}
	commands, err := os.ReadFile(commandPath)
	if err != nil {
		t.Fatal(err)
	}
	if text := string(commands); !strings.Contains(text, "update --restart=no container") || !strings.Contains(text, "stop --time 15 container") ||
		strings.Index(text, "update --restart=no container") > strings.Index(text, "stop --time 15 container") {
		t.Fatalf("crash loop was not suppressed in safe order: %q", text)
	}
	if err := manager.Start(ctx); !errors.Is(err, ErrCrashGuardTripped) {
		t.Fatalf("Start error = %v, want crash guard trip", err)
	}
	if _, err := manager.ApplyPalworldConfig(ctx, db.ConfigDraft{}, nil); !errors.Is(err, ErrCrashGuardTripped) {
		t.Fatalf("ApplyPalworldConfig error = %v, want crash guard trip", err)
	}
	if _, err := manager.ResetWorld(ctx, "world", "RESET WORLD", WorldResetHooks{}); !errors.Is(err, ErrCrashGuardTripped) {
		t.Fatalf("ResetWorld error = %v, want crash guard trip", err)
	}
	status, err = manager.RecoverCrashGuard(ctx, false)
	if err != nil || status.Tripped || status.RecentCrashCount != 0 {
		t.Fatalf("RecoverCrashGuard = %#v, %v", status, err)
	}
	alerts, err := manager.store.ListAlerts(ctx, 20)
	if err != nil || len(alerts) != 1 || alerts[0].Status != "resolved" {
		t.Fatalf("recovery alerts = %#v, %v", alerts, err)
	}

	now = now.Add(time.Minute)
	writeCrashGuardDockerState(t, statePath, 1, "running", false, 137)
	if err := manager.ObserveCrashGuard(ctx); err != nil {
		t.Fatal(err)
	}
	status, err = manager.CrashGuardStatus(ctx, 20)
	if err != nil || status.Tripped || status.RecentCrashCount != 1 {
		t.Fatalf("post-recovery crash window = %#v, %v", status, err)
	}
}

func TestCrashGuardIgnoresExpectedStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	manager, statePath, _, cleanup := newCrashGuardTestManager(t)
	defer cleanup()
	ctx := t.Context()
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	manager.crashGuardNow = func() time.Time { return now }

	writeCrashGuardDockerState(t, statePath, 0, "running", false, 0)
	if err := manager.ObserveCrashGuard(ctx); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	writeCrashGuardDockerState(t, statePath, 0, "exited", false, 0)
	now = now.Add(time.Second)
	if err := manager.ObserveCrashGuard(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := manager.CrashGuardStatus(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if status.RecentCrashCount != 0 || status.Tripped || len(status.Events) != 0 {
		t.Fatalf("intentional stop was treated as a crash: %#v", status)
	}
}

func newCrashGuardTestManager(t *testing.T) (Manager, string, string, func()) {
	t.Helper()
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	commandPath := filepath.Join(root, "commands.log")
	fakeDocker := filepath.Join(root, "docker")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "` + commandPath + `"
case "$1" in
  inspect) cat "` + statePath + `" ;;
  *) exit 0 ;;
esac
`
	if err := os.WriteFile(fakeDocker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := appconfig.Config{
		DataDir: root, ServerDir: filepath.Join(root, "server"), WinePrefixDir: filepath.Join(root, "wine"),
		ToolsDir: filepath.Join(root, "tools"), SteamCMDDir: filepath.Join(root, "steamcmd"), UploadsDir: filepath.Join(root, "uploads"),
		BackupsDir: filepath.Join(root, "backups"), LogsDir: filepath.Join(root, "logs"), DBPath: filepath.Join(root, "test.db"),
		SaveIndexCacheDir: filepath.Join(root, "save-index"), DockerBinary: fakeDocker, DockerImage: "image", DockerContainer: "container",
		GamePort: 8211, QueryPort: 27015, RESTPort: 8212,
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, cfg.PalServerExePath(), "binary")
	settings := palconfig.Defaults()
	if err := palconfig.Write(cfg.PalWorldSettingsPath(), settings); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(cfg, store, docker.NewRunner(cfg))
	if err := manager.SetRuntimeMode(t.Context(), RuntimeWineDocker); err != nil {
		t.Fatal(err)
	}
	return manager, statePath, commandPath, func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.jobs.Shutdown(shutdownCtx)
		_ = store.Close()
	}
}

func writeCrashGuardDockerState(t *testing.T, path string, restartCount int, status string, oom bool, exitCode int) {
	t.Helper()
	body := `[{"RestartCount":` + strconvItoa(restartCount) + `,"State":{"Status":"` + status + `","OOMKilled":` + boolString(oom) + `,"ExitCode":` + strconvItoa(exitCode) + `,"StartedAt":"2026-07-29T11:00:00Z","FinishedAt":"2026-07-29T11:01:00Z"}}]`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func strconvItoa(value int) string { return fmt.Sprintf("%d", value) }
func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestCrashGuardObservationDoesNotOverwriteExpectedStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	manager, statePath, _, cleanup := newCrashGuardTestManager(t)
	defer cleanup()
	ctx := t.Context()
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	manager.crashGuardNow = func() time.Time { return now }
	writeCrashGuardDockerState(t, statePath, 0, "running", false, 0)
	if err := manager.ObserveCrashGuard(ctx); err != nil {
		t.Fatal(err)
	}

	stateRead := make(chan struct{})
	continueObservation := make(chan struct{})
	manager.crashGuardAfterStateRead = func() {
		close(stateRead)
		<-continueObservation
	}
	observeDone := make(chan error, 1)
	go func() { observeDone <- manager.ObserveCrashGuard(ctx) }()
	<-stateRead

	markDone := make(chan error, 1)
	go func() { markDone <- manager.markCrashGuardExpectedStop(ctx, "manual_stop") }()
	select {
	case err := <-markDone:
		t.Fatalf("expected-stop write bypassed observation lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(continueObservation)
	if err := <-observeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-markDone; err != nil {
		t.Fatal(err)
	}
	state, err := manager.store.GetCrashGuardState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.ExpectedUntil == "" || state.ExpectedReason != "manual_stop" {
		t.Fatalf("expected-stop marker was lost: %#v", state)
	}
}
