package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/docker"
)

func TestReadAppManifestBuildID(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "appmanifest_2394010.acf")
	writeFile(t, path, `"AppState"
{
	"appid"		"2394010"
	"buildid"		"123456"
}`)
	got, err := readAppManifestBuildID(path)
	if err != nil {
		t.Fatalf("readAppManifestBuildID returned error: %v", err)
	}
	if got != "123456" {
		t.Fatalf("expected build id 123456, got %s", got)
	}
}

func TestParseSteamAppInfoPublicBuildID(t *testing.T) {
	raw := `"2394010"
{
	"common"
	{
		"name"		"Palworld Dedicated Server"
	}
	"depots"
	{
		"branches"
		{
			"public"
			{
				"buildid"		"987654"
				"timeupdated"		"1710000000"
			}
			"previous"
			{
				"buildid"		"111111"
			}
		}
	}
}`
	got, err := parseSteamAppInfoPublicBuildID(raw)
	if err != nil {
		t.Fatalf("parseSteamAppInfoPublicBuildID returned error: %v", err)
	}
	if got != "987654" {
		t.Fatalf("expected public build id 987654, got %s", got)
	}
}

func TestUpdateIfNeededSkipsInstallWhenBuildIsCurrent(t *testing.T) {
	m, cleanup := newVersionTestManager(t, "123456")
	defer cleanup()
	installCalled := false
	m.remoteBuildIDFunc = func(context.Context) (string, string, error) {
		return "123456", "test", nil
	}
	m.installOrUpdateFunc = func(context.Context, string) error {
		installCalled = true
		return nil
	}

	job, err := m.UpdateIfNeeded(context.Background(), nil)
	if err != nil {
		t.Fatalf("UpdateIfNeeded returned error: %v", err)
	}
	done := waitForJob(t, m.store, job.ID)
	if done.Status != "completed" {
		t.Fatalf("expected completed job, got %#v", done)
	}
	if installCalled {
		t.Fatal("install/update should not be called when local build is current")
	}
}

func TestUpdateIfNeededRunsInstallWhenBuildIsStale(t *testing.T) {
	m, cleanup := newVersionTestManager(t, "100")
	defer cleanup()
	installCalled := false
	m.remoteBuildIDFunc = func(context.Context) (string, string, error) {
		return "200", "test", nil
	}
	m.installOrUpdateFunc = func(_ context.Context, _ string) error {
		installCalled = true
		writeAppManifest(t, m, "200")
		return nil
	}

	job, err := m.UpdateIfNeeded(context.Background(), nil)
	if err != nil {
		t.Fatalf("UpdateIfNeeded returned error: %v", err)
	}
	done := waitForJob(t, m.store, job.ID)
	if done.Status != "completed" {
		t.Fatalf("expected completed job, got %#v", done)
	}
	if !installCalled {
		t.Fatal("install/update should be called when local build is stale")
	}
	info, err := m.VersionInfo(context.Background())
	if err != nil {
		t.Fatalf("VersionInfo returned error: %v", err)
	}
	if info.CurrentBuildID != "200" || info.LatestBuildID != "200" || info.UpdateAvailable {
		t.Fatalf("unexpected version info after update: %#v", info)
	}
}

func TestUpdateFailsWhenInstalledBuildDoesNotMatchPublicBranch(t *testing.T) {
	m, cleanup := newVersionTestManager(t, "100")
	defer cleanup()
	m.remoteBuildIDFunc = func(context.Context) (string, string, error) {
		return "200", "test", nil
	}
	m.installOrUpdateFunc = func(_ context.Context, _ string) error {
		writeAppManifest(t, m, "199")
		return nil
	}

	job, err := m.UpdateIfNeeded(context.Background(), nil)
	if err != nil {
		t.Fatalf("UpdateIfNeeded returned error: %v", err)
	}
	done := waitForJob(t, m.store, job.ID)
	if done.Status != "failed" || !strings.Contains(done.Error, "does not match Steam public Build") {
		t.Fatalf("expected actionable build mismatch, got %#v", done)
	}
	backups, err := m.ListBackups()
	if err != nil || len(backups) != 1 || backups[0].Reason != "pre-update" {
		t.Fatalf("verified pre-update backup was not retained: %#v, %v", backups, err)
	}
}

func TestWithGameVersionReportsMajorReleaseCompatibility(t *testing.T) {
	m, cleanup := newVersionTestManager(t, "100")
	defer cleanup()
	if CompatibilityTarget != "1.x" {
		t.Fatalf("expected compatibility target 1.x, got %s", CompatibilityTarget)
	}
	info := VersionInfo{CompatibilityTarget: CompatibilityTarget}
	for _, version := range []string{"v1.0.2.100001", "v1.0.3.100002", "v1.1.0.100003"} {
		info = m.WithGameVersion(context.Background(), info, version)
		if info.GameVersion != version || info.Compatible == nil || !*info.Compatible {
			t.Fatalf("expected %s to match the supported major release, got %#v", version, info)
		}
	}
	info = m.WithGameVersion(context.Background(), info, "v2.0.0")
	if info.Compatible == nil || *info.Compatible || len(info.CompatibilityWarnings) == 0 {
		t.Fatalf("expected a major-version compatibility warning, got %#v", info)
	}
}

func TestValidateWindowsServerInstallAcceptsCurrentCommandExecutable(t *testing.T) {
	m, closeStore := newVersionTestManager(t, "24181105")
	defer closeStore()
	if err := m.validateWindowsServerInstall(); err != nil {
		t.Fatalf("validateWindowsServerInstall returned error: %v", err)
	}
}

func TestValidateWindowsServerInstallAcceptsLegacyCommandExecutable(t *testing.T) {
	m, closeStore := newVersionTestManager(t, "24181105")
	defer closeStore()
	current := filepath.Join(m.cfg.Win64Dir(), "PalServer-Win64-Shipping-Cmd.exe")
	if err := os.Remove(current); err != nil {
		t.Fatalf("Remove current command executable returned error: %v", err)
	}
	writeFile(t, filepath.Join(m.cfg.Win64Dir(), "PalServer-Win64-Test-Cmd.exe"), "MZ-test-palserver-cmd")
	if err := m.validateWindowsServerInstall(); err != nil {
		t.Fatalf("legacy command executable was rejected: %v", err)
	}
}

func newVersionTestManager(t *testing.T, buildID string) (Manager, func()) {
	t.Helper()
	root := t.TempDir()
	cfg := appconfig.Config{
		DataDir:         root,
		ServerDir:       filepath.Join(root, "server"),
		WinePrefixDir:   filepath.Join(root, "wineprefix"),
		ToolsDir:        filepath.Join(root, "tools"),
		SteamCMDDir:     filepath.Join(root, "tools", "steamcmd"),
		UploadsDir:      filepath.Join(root, "uploads"),
		BackupsDir:      filepath.Join(root, "backups"),
		LogsDir:         filepath.Join(root, "logs"),
		DBPath:          filepath.Join(root, "test.db"),
		DockerBinary:    "docker",
		DockerImage:     "test-image",
		DockerContainer: "test-container",
		GamePort:        8211,
		QueryPort:       27015,
		RESTPort:        8212,
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs returned error: %v", err)
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("db.Open returned error: %v", err)
	}
	m := NewManager(cfg, store, docker.NewRunner(cfg))
	if err := store.SetKV(context.Background(), kvRuntimeMode, RuntimeWindowsSteamCMD); err != nil {
		t.Fatalf("SetKV runtime returned error: %v", err)
	}
	writeFile(t, cfg.PalServerExePath(), "MZ-test-palserver")
	writeFile(t, filepath.Join(cfg.Win64Dir(), "PalServer-Win64-Shipping-Cmd.exe"), "MZ-test-palserver-cmd")
	writeAppManifest(t, m, buildID)
	return m, func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.jobs.Shutdown(shutdownCtx)
		_ = store.Close()
	}
}

func writeAppManifest(t *testing.T, m Manager, buildID string) {
	t.Helper()
	writeFile(t, m.appManifestPath(), `"AppState"
{
	"appid"		"2394010"
	"buildid"		"`+buildID+`"
}`)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func waitForJob(t *testing.T, store *db.Store, id string) db.Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := store.GetJob(context.Background(), id)
		if err != nil {
			t.Fatalf("GetJob returned error: %v", err)
		}
		if job.Status == "completed" || job.Status == "failed" {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := store.GetJob(context.Background(), id)
	t.Fatalf("job did not finish: %#v", job)
	return db.Job{}
}
