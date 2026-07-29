//go:build linux

package panelupdater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testCurrentVersion = "v1.3.0-custom.0.8.28"
	testTargetVersion  = "v1.3.0-custom.0.8.29"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestRequestRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	operation := OperationDir(dataDir, "job_test")
	if err := os.MkdirAll(operation, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(operation, "palpanel_"+testTargetVersion+"_linux_amd64.tar.gz")
	if err := os.WriteFile(archive, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := Request{
		SchemaVersion: SchemaVersion, JobID: "job_test", ArchivePath: archive,
		ArchiveSHA256: strings.Repeat("a", 64), CurrentVersion: testCurrentVersion,
		TargetVersion: testTargetVersion, HealthPort: 8080,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := WriteRequest(dataDir, request); err != nil {
		t.Fatal(err)
	}
	read, err := ReadRequest(RequestPath(dataDir), StateDir(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	if read.JobID != request.JobID || read.TargetVersion != request.TargetVersion {
		t.Fatalf("unexpected request: %#v", read)
	}
	result := Result{JobID: request.JobID, CurrentVersion: testCurrentVersion, TargetVersion: testTargetVersion, Status: "completed"}
	if err := WriteResult(dataDir, result); err != nil {
		t.Fatal(err)
	}
	consumed, ok, err := ConsumeResult(dataDir)
	if err != nil || !ok || consumed.Status != "completed" {
		t.Fatalf("ConsumeResult = %#v, %v, %v", consumed, ok, err)
	}
	if _, ok, err := ConsumeResult(dataDir); err != nil || ok {
		t.Fatalf("second ConsumeResult = %v, %v", ok, err)
	}
}

func TestWriteResultReplacesSymlinkWithoutFollowingIt(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(StateDir(dataDir), 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, ResultPath(dataDir)); err != nil {
		t.Fatal(err)
	}
	result := Result{JobID: "job_safe", CurrentVersion: testCurrentVersion, TargetVersion: testTargetVersion, Status: "completed"}
	if err := WriteResult(dataDir, result); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(victim)
	if err != nil || string(body) != "preserve" {
		t.Fatalf("victim changed: %q, %v", body, err)
	}
	info, err := os.Lstat(ResultPath(dataDir))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("result path is not a regular file: %v, %v", info, err)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		current string
		target  string
		want    int
	}{
		{testCurrentVersion, testTargetVersion, -1},
		{testTargetVersion, testCurrentVersion, 1},
		{testCurrentVersion, testCurrentVersion, 0},
		{"v1.3.0-custom.0.9.0", "v1.4.0-custom.0.1.0", -1},
	}
	for _, test := range tests {
		got, err := CompareVersions(test.current, test.target)
		if err != nil || got != test.want {
			t.Fatalf("CompareVersions(%q, %q) = %d, %v", test.current, test.target, got, err)
		}
	}
}

func TestParseListenPort(t *testing.T) {
	for input, expected := range map[string]int{
		"": 8080, "127.0.0.1:9000": 9000, "0.0.0.0:8088": 8088, "[::]:8099": 8099,
	} {
		actual, err := ParseListenPort(input)
		if err != nil || actual != expected {
			t.Fatalf("ParseListenPort(%q) = %d, %v", input, actual, err)
		}
	}
	if _, err := ParseListenPort("bad-listen"); err == nil {
		t.Fatal("invalid listen address was accepted")
	}
}

func TestExecuteCompletesFullPackageSwitch(t *testing.T) {
	fixture := newUpdaterFixture(t)
	result := Execute(context.Background(), RequestPath(fixture.config.DataDir), fixture.config)
	if result.Status != "completed" || result.TargetVersion != testTargetVersion {
		t.Fatalf("Execute result = %#v", result)
	}
	current, err := filepath.EvalSymlinks(filepath.Join(fixture.config.InstallRoot, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(current) != testTargetVersion {
		t.Fatalf("current = %s", current)
	}
	if body, err := os.ReadFile(filepath.Join(fixture.config.LibexecDir, "palpanel-updater")); err != nil || !bytes.Contains(body, []byte(testTargetVersion)) {
		t.Fatalf("installed helper = %q, %v", body, err)
	}
	unit, err := os.ReadFile(filepath.Join(fixture.config.SystemdDir, "palpanel.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(unit, []byte(fixture.config.InstallRoot+"/current/bin/palpanel")) {
		t.Fatalf("rendered unit does not use install root: %s", unit)
	}
	if _, err := os.Stat(fixture.archivePath); !os.IsNotExist(err) {
		t.Fatalf("operation archive was not cleaned: %v", err)
	}
	if _, err := os.Stat(InProgressPath(fixture.config.DataDir)); !os.IsNotExist(err) {
		t.Fatalf("in-progress request was not cleaned: %v", err)
	}
	fixture.mu.Lock()
	commands := strings.Join(fixture.commands, "\n")
	fixture.mu.Unlock()
	for _, expected := range []string{"stop palpanel.service", "daemon-reload", "enable palpanel-update.path", "restart palpanel-sav-cli.service"} {
		if !strings.Contains(commands, expected) {
			t.Fatalf("systemctl log missing %q:\n%s", expected, commands)
		}
	}
}

func TestExecuteResumesClaimedRequest(t *testing.T) {
	fixture := newUpdaterFixture(t)
	if err := os.Rename(RequestPath(fixture.config.DataDir), InProgressPath(fixture.config.DataDir)); err != nil {
		t.Fatal(err)
	}
	result := Execute(context.Background(), RequestPath(fixture.config.DataDir), fixture.config)
	if result.Status != "completed" || result.TargetVersion != testTargetVersion {
		t.Fatalf("Execute result = %#v", result)
	}
	current, err := filepath.EvalSymlinks(filepath.Join(fixture.config.InstallRoot, "current"))
	if err != nil || filepath.Base(current) != testTargetVersion {
		t.Fatalf("current = %s, %v", current, err)
	}
}

func TestExecuteCompletesInterruptedActivatedUpdate(t *testing.T) {
	fixture := newUpdaterFixture(t)
	fixture.activateTargetBeforeExecute(t)
	result := Execute(context.Background(), RequestPath(fixture.config.DataDir), fixture.config)
	if result.Status != "completed" || !strings.Contains(result.Message, "恢复中断") {
		t.Fatalf("Execute result = %#v", result)
	}
	current, err := filepath.EvalSymlinks(filepath.Join(fixture.config.InstallRoot, "current"))
	if err != nil || filepath.Base(current) != testTargetVersion {
		t.Fatalf("current = %s, %v", current, err)
	}
	if _, err := os.Stat(InProgressPath(fixture.config.DataDir)); !os.IsNotExist(err) {
		t.Fatalf("in-progress request was not cleaned: %v", err)
	}
}

func TestExecuteRollsBackWhenTargetHealthFails(t *testing.T) {
	fixture := newUpdaterFixture(t)
	fixture.healthTargetFails = true
	fixture.config.HealthTimeout = 15 * time.Millisecond
	result := Execute(context.Background(), RequestPath(fixture.config.DataDir), fixture.config)
	if result.Status != "rolled_back" || !result.RollbackSucceeded || result.ErrorCode != "panel_update_health_failed" {
		t.Fatalf("Execute result = %#v", result)
	}
	current, err := filepath.EvalSymlinks(filepath.Join(fixture.config.InstallRoot, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(current) != testCurrentVersion {
		t.Fatalf("current after rollback = %s", current)
	}
	if _, err := os.Stat(filepath.Join(fixture.config.InstallRoot, testTargetVersion)); !os.IsNotExist(err) {
		t.Fatalf("failed target was not removed: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(fixture.config.LibexecDir, "palpanel-updater"))
	if err != nil || string(body) != "old-helper\n" {
		t.Fatalf("helper rollback = %q, %v", body, err)
	}
}

func TestExecuteRejectsCurrentVersionDirectoryMismatch(t *testing.T) {
	fixture := newUpdaterFixture(t)
	wrong := filepath.Join(fixture.config.InstallRoot, "v1.3.0-custom.0.8.27")
	if err := os.Rename(filepath.Join(fixture.config.InstallRoot, testCurrentVersion), wrong); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(fixture.config.InstallRoot, "current")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(wrong, filepath.Join(fixture.config.InstallRoot, "current")); err != nil {
		t.Fatal(err)
	}
	result := Execute(context.Background(), RequestPath(fixture.config.DataDir), fixture.config)
	if result.Status != "failed" || result.ErrorCode != "panel_update_current_version_mismatch" {
		t.Fatalf("Execute result = %#v", result)
	}
}

func TestExecuteRejectsArchiveNotMatchingOfficialChecksum(t *testing.T) {
	fixture := newUpdaterFixture(t)
	fixture.officialSHA = strings.Repeat("0", 64)
	result := Execute(context.Background(), RequestPath(fixture.config.DataDir), fixture.config)
	if result.Status != "failed" || result.ErrorCode != "panel_update_archive_untrusted" {
		t.Fatalf("Execute result = %#v", result)
	}
	current, err := filepath.EvalSymlinks(filepath.Join(fixture.config.InstallRoot, "current"))
	if err != nil || filepath.Base(current) != testCurrentVersion {
		t.Fatalf("current changed after rejected package: %s, %v", current, err)
	}
}

func TestExtractPackageRejectsTraversal(t *testing.T) {
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	_ = tarWriter.Close()
	_ = gzipWriter.Close()
	archive := filepath.Join(t.TempDir(), "bad.tar.gz")
	if err := os.WriteFile(archive, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := extractPackage(file, t.TempDir(), "expected"); err == nil {
		t.Fatal("path traversal archive was accepted")
	}
}

type updaterFixture struct {
	config            Config
	archivePath       string
	officialSHA       string
	healthTargetFails bool
	mu                sync.Mutex
	commands          []string
}

func newUpdaterFixture(t *testing.T) *updaterFixture {
	t.Helper()
	root := t.TempDir()
	fixture := &updaterFixture{}
	fixture.config = Config{
		DataDir: filepath.Join(root, "data"), InstallRoot: filepath.Join(root, "opt", "palpanel"),
		EtcDir: filepath.Join(root, "etc", "palpanel"), SystemdDir: filepath.Join(root, "systemd"),
		LibexecDir: filepath.Join(root, "libexec"), ServiceUser: "palpanel-test",
		SystemctlPath: "systemctl", HealthTimeout: 100 * time.Millisecond, PollInterval: time.Millisecond,
	}
	for _, directory := range []string{fixture.config.DataDir, fixture.config.InstallRoot, fixture.config.EtcDir, fixture.config.SystemdDir, fixture.config.LibexecDir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	previous := filepath.Join(fixture.config.InstallRoot, testCurrentVersion)
	if err := os.MkdirAll(filepath.Join(previous, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(previous, filepath.Join(fixture.config.InstallRoot, "current")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.config.LibexecDir, "palpanel-updater"), []byte("old-helper\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range managedUnits {
		if err := os.WriteFile(filepath.Join(fixture.config.SystemdDir, name), []byte("old "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	operation := OperationDir(fixture.config.DataDir, "job_update")
	if err := os.MkdirAll(operation, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.archivePath = filepath.Join(operation, "palpanel_"+testTargetVersion+"_linux_amd64.tar.gz")
	createReleaseArchive(t, fixture.archivePath, testTargetVersion)
	archiveBody, err := os.ReadFile(fixture.archivePath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archiveBody)
	fixture.officialSHA = hex.EncodeToString(digest[:])
	request := Request{
		SchemaVersion: SchemaVersion, JobID: "job_update", ArchivePath: fixture.archivePath,
		ArchiveSHA256: fixture.officialSHA, CurrentVersion: testCurrentVersion,
		TargetVersion: testTargetVersion, HealthPort: 18080,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := WriteRequest(fixture.config.DataDir, request); err != nil {
		t.Fatal(err)
	}
	fixture.config.RunCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		fixture.mu.Lock()
		fixture.commands = append(fixture.commands, strings.Join(args, " "))
		fixture.mu.Unlock()
		return nil, nil
	}
	fixture.config.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := ""
		switch {
		case request.URL.Host == "github.com":
			body = fixture.officialSHA + "  " + filepath.Base(fixture.archivePath) + "\n"
		case request.URL.Path == "/api/ready":
			body = `{"ok":true,"data":{"status":"ready"}}`
		case request.URL.Path == "/api/health":
			current, _ := filepath.EvalSymlinks(filepath.Join(fixture.config.InstallRoot, "current"))
			version := filepath.Base(current)
			if fixture.healthTargetFails && version == testTargetVersion {
				return nil, fmt.Errorf("target unavailable")
			}
			body = fmt.Sprintf(`{"ok":true,"data":{"status":"ok","version":%q}}`, version)
		default:
			return nil, fmt.Errorf("unexpected request: %s", request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)),
			Header: make(http.Header), Request: request,
		}, nil
	})}
	fixture.config.ReleaseClient = fixture.config.HTTPClient
	return fixture
}

func (fixture *updaterFixture) activateTargetBeforeExecute(t *testing.T) {
	t.Helper()
	archive, err := os.Open(fixture.archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	extractRoot, err := os.MkdirTemp(fixture.config.InstallRoot, ".interrupted-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(extractRoot)
	rootName := "palpanel_" + testTargetVersion + "_linux_amd64"
	packageRoot, err := extractPackage(archive, extractRoot, rootName)
	if err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(fixture.config.InstallRoot, testTargetVersion)
	if err := os.Rename(packageRoot, targetDir); err != nil {
		t.Fatal(err)
	}
	if err := hardenVersionTree(targetDir); err != nil {
		t.Fatal(err)
	}
	if err := switchCurrent(fixture.config.InstallRoot, targetDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(RequestPath(fixture.config.DataDir), InProgressPath(fixture.config.DataDir)); err != nil {
		t.Fatal(err)
	}
}

func createReleaseArchive(t *testing.T, archivePath, version string) {
	t.Helper()
	rootName := "palpanel_" + version + "_linux_amd64"
	files := map[string]struct {
		body string
		mode int64
	}{
		"bin/palpanel":                     {"#!/bin/sh\necho 'palpanel " + version + "'\n", 0o755},
		"bin/palpanel-updater":             {"#!/bin/sh\necho 'palpanel-updater " + version + "'\n", 0o755},
		"bin/sav-cli":                      {"#!/bin/sh\nexit 0\n", 0o755},
		"bin/palcalc-bridge":               {"#!/bin/sh\nexit 0\n", 0o755},
		"palpanelctl":                      {"#!/bin/sh\nexit 0\n", 0o755},
		"systemd/palpanel.service":         {"[Service]\nUser=palpanel\nGroup=palpanel\nExecStart=/opt/palpanel/current/bin/palpanel --config /etc/palpanel/palpanel.env\nReadWritePaths=/var/lib/palpanel\n", 0o644},
		"systemd/palpanel-sav-cli.service": {"[Service]\nUser=palpanel\nGroup=palpanel\nExecStart=/opt/palpanel/current/bin/sav-cli\n", 0o644},
		"systemd/palpanel-palcalc.service": {"[Service]\nUser=palpanel\nGroup=palpanel\nExecStart=/opt/palpanel/current/bin/palcalc-bridge\n", 0o644},
		"systemd/palpanel-update.service":  {"[Service]\nExecStart=/usr/libexec/palpanel-updater --request /var/lib/palpanel/panel-update/request.json\nReadWritePaths=/opt/palpanel /etc/systemd/system /usr/libexec /var/lib/palpanel\nReadOnlyPaths=/etc/palpanel\n", 0o644},
		"systemd/palpanel-update.path":     {"[Path]\nPathExists=/var/lib/palpanel/panel-update/request.json\nPathExists=/var/lib/palpanel/panel-update/request.in-progress.json\n", 0o644},
	}
	var checksumLines []string
	for name, file := range files {
		digest := sha256.Sum256([]byte(file.body))
		checksumLines = append(checksumLines, hex.EncodeToString(digest[:])+"  ./"+name)
	}
	sortStrings(checksumLines)
	files["checksums.txt"] = struct {
		body string
		mode int64
	}{strings.Join(checksumLines, "\n") + "\n", 0o644}

	output, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(output)
	tarWriter := tar.NewWriter(gzipWriter)
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		file := files[name]
		header := &tar.Header{Name: rootName + "/" + name, Typeflag: tar.TypeReg, Mode: file.mode, Size: int64(len(file.body))}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(file.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
