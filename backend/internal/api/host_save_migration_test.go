package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"palpanel/internal/appconfig"
)

func TestReadHostMigrationRequestClaimsMigrationPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest("POST", "/api/save-sources/import/inspect", strings.NewReader(`{"migration_source_id":"save-1","steam_id":"76561198000000000"}`))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	input, claimed, err := readHostMigrationRequest(context)
	if err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	if input.MigrationSourceID != "save-1" || input.SteamID != "76561198000000000" {
		t.Fatalf("unexpected input: %#v", input)
	}
}

func TestReadHostMigrationRequestPreservesExistingImportPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"inspection_id":"inspect-1","name":"fixture"}`
	request := httptest.NewRequest("POST", "/api/save-sources/import", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	_, claimed, err := readHostMigrationRequest(context)
	if err != nil || claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	restored, err := io.ReadAll(context.Request.Body)
	if err != nil || string(restored) != body {
		t.Fatalf("body=%q err=%v", restored, err)
	}
}

func TestReadHostMigrationRequestRejectsPartialPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest("POST", "/api/save-sources/import", strings.NewReader(`{"migration_source_id":"save-1"}`))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	_, claimed, err := readHostMigrationRequest(context)
	if !claimed || err == nil {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
}

func TestReplaceDedicatedServerNamePreservesCRLF(t *testing.T) {
	payload := []byte("[/Script/Pal.PalGameWorldSettings]\r\nDedicatedServerName=OLDWORLD\r\nOption=1\r\n")
	next := "0123456789ABCDEF0123456789ABCDEF"
	updated, previous, err := replaceDedicatedServerName(payload, next)
	if err != nil {
		t.Fatal(err)
	}
	if previous != "OLDWORLD" {
		t.Fatalf("previous = %q", previous)
	}
	if !bytes.Contains(updated, []byte("DedicatedServerName="+next+"\r\n")) {
		t.Fatalf("updated settings = %q", updated)
	}
	if bytes.Contains(updated, []byte("\n")) && !bytes.Contains(updated, []byte("\r\n")) {
		t.Fatalf("CRLF was not preserved: %q", updated)
	}
}

func TestReplaceDedicatedServerNameRejectsAmbiguousSettings(t *testing.T) {
	payload := []byte("DedicatedServerName=ONE\nDedicatedServerName=TWO\n")
	_, _, err := replaceDedicatedServerName(payload, "0123456789ABCDEF0123456789ABCDEF")
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeployHostMigrationWorldStopsSwitchesAndRestarts(t *testing.T) {
	root := t.TempDir()
	serverDir := filepath.Join(root, "server")
	settingsPath := filepath.Join(serverDir, "Pal", "Saved", "Config", "WindowsServer", "GameUserSettings.ini")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("[/Script/Pal.PalGameWorldSettings]\nDedicatedServerName=ORIGINALWORLD\n")
	if err := os.WriteFile(settingsPath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "Players"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "Level.sav"), []byte("level"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "Players", "host.sav"), []byte("player"), 0o640); err != nil {
		t.Fatal(err)
	}

	previousState := hostMigrationServerState
	previousStop := hostMigrationStopServer
	previousStart := hostMigrationStartServer
	previousCopy := hostMigrationCopyTree
	var calls []string
	hostMigrationServerState = func(Server, context.Context) (string, error) { return "running", nil }
	hostMigrationStopServer = func(Server, context.Context) error { calls = append(calls, "stop"); return nil }
	hostMigrationStartServer = func(Server, context.Context) error { calls = append(calls, "start"); return nil }
	hostMigrationCopyTree = func(source, destination string) error {
		calls = append(calls, "copy")
		return previousCopy(source, destination)
	}
	t.Cleanup(func() {
		hostMigrationServerState = previousState
		hostMigrationStopServer = previousStop
		hostMigrationStartServer = previousStart
		hostMigrationCopyTree = previousCopy
	})

	cfg := appconfig.Config{ServerDir: serverDir}.WithServerDirectoryState()
	activation, err := (Server{cfg: cfg}).deployHostMigrationWorld(context.Background(), source, "save-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "stop,copy,start" {
		t.Fatalf("migration order = %v", calls)
	}
	if !activation.ServerWasRunning || !activation.ServerRestarted {
		t.Fatalf("activation = %#v", activation)
	}
	settings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(settings, []byte("DedicatedServerName="+activation.DedicatedServerName)) {
		t.Fatalf("settings = %q", settings)
	}
	wantRoot := filepath.Join(serverDir, "Pal", "Saved", "SaveGames", "0")
	if filepath.Dir(activation.ServerSavePath) != wantRoot {
		t.Fatalf("server save path = %q, want child of %q", activation.ServerSavePath, wantRoot)
	}
	if _, err := os.Stat(filepath.Join(activation.ServerSavePath, "Players", "host.sav")); err != nil {
		t.Fatal(err)
	}
	activation.commit()
}

func TestDeployHostMigrationWorldRestartsOriginalServerWhenCopyFails(t *testing.T) {
	root := t.TempDir()
	serverDir := filepath.Join(root, "server")
	settingsPath := filepath.Join(serverDir, "Pal", "Saved", "Config", "WindowsServer", "GameUserSettings.ini")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("DedicatedServerName=ORIGINALWORLD\n")
	if err := os.WriteFile(settingsPath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	previousState := hostMigrationServerState
	previousStop := hostMigrationStopServer
	previousStart := hostMigrationStartServer
	previousCopy := hostMigrationCopyTree
	var calls []string
	hostMigrationServerState = func(Server, context.Context) (string, error) { return "running", nil }
	hostMigrationStopServer = func(Server, context.Context) error { calls = append(calls, "stop"); return nil }
	hostMigrationStartServer = func(Server, context.Context) error { calls = append(calls, "start"); return nil }
	hostMigrationCopyTree = func(string, string) error { calls = append(calls, "copy"); return errors.New("copy failed") }
	t.Cleanup(func() {
		hostMigrationServerState = previousState
		hostMigrationStopServer = previousStop
		hostMigrationStartServer = previousStart
		hostMigrationCopyTree = previousCopy
	})

	cfg := appconfig.Config{ServerDir: serverDir}.WithServerDirectoryState()
	_, err := (Server{cfg: cfg}).deployHostMigrationWorld(context.Background(), source, "save-copy-failure")
	if err == nil || !strings.Contains(err.Error(), "copy failed") {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(calls, ",") != "stop,copy,start" {
		t.Fatalf("rollback order = %v", calls)
	}
	settings, readErr := os.ReadFile(settingsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(settings, original) {
		t.Fatalf("settings changed after failed copy: %q", settings)
	}
}

func TestHostMigrationActivationRollbackRestoresOriginalWorld(t *testing.T) {
	root := t.TempDir()
	serverDir := filepath.Join(root, "server")
	settingsPath := filepath.Join(serverDir, "Pal", "Saved", "Config", "WindowsServer", "GameUserSettings.ini")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("DedicatedServerName=ORIGINALWORLD\n")
	if err := os.WriteFile(settingsPath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "Players"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "Level.sav"), []byte("level"), 0o640); err != nil {
		t.Fatal(err)
	}

	previousState := hostMigrationServerState
	previousStop := hostMigrationStopServer
	previousStart := hostMigrationStartServer
	hostMigrationServerState = func(Server, context.Context) (string, error) { return "stopped", nil }
	hostMigrationStopServer = func(Server, context.Context) error { return nil }
	hostMigrationStartServer = func(Server, context.Context) error { return nil }
	t.Cleanup(func() {
		hostMigrationServerState = previousState
		hostMigrationStopServer = previousStop
		hostMigrationStartServer = previousStart
	})

	cfg := appconfig.Config{ServerDir: serverDir}.WithServerDirectoryState()
	server := Server{cfg: cfg}
	activation, err := server.deployHostMigrationWorld(context.Background(), source, "save-rollback")
	if err != nil {
		t.Fatal(err)
	}
	if err := activation.rollback(server, context.Background()); err != nil {
		t.Fatal(err)
	}
	settings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(settings, original) {
		t.Fatalf("restored settings = %q", settings)
	}
	if _, err := os.Stat(activation.ServerSavePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("migrated save still exists: %v", err)
	}
}

func TestExtractHostMigrationHelperArchive(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "package.tar.gz")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(archive)
	tarWriter := tar.NewWriter(gzipWriter)
	body := []byte("uid-remapper")
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:     "release/bin/palworld-uid-remap",
		Mode:     0o755,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "palworld-uid-remap")
	if err := extractHostMigrationHelperArchive(archivePath, destination); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(body) {
		t.Fatalf("helper = %q", actual)
	}
}

func TestBootstrapHostMigrationHelper(t *testing.T) {
	body := []byte("uid-remapper-bootstrap")
	var packageBytes bytes.Buffer
	gzipWriter := gzip.NewWriter(&packageBytes)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:     "release/overlay/bin/palworld-uid-remap",
		Mode:     0o755,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(packageBytes.Bytes())
	}))
	defer server.Close()
	t.Setenv("PALPANEL_UID_REMAPPER_PACKAGE_URL", server.URL)
	previousSHA := hostMigrationHelperSHA256
	digest := sha256.Sum256(body)
	hostMigrationHelperSHA256 = fmt.Sprintf("%x", digest[:])
	t.Cleanup(func() { hostMigrationHelperSHA256 = previousSHA })
	destination := filepath.Join(t.TempDir(), "palworld-uid-remap")
	if err := bootstrapHostMigrationHelper(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(body) {
		t.Fatalf("helper = %q", actual)
	}
}
