package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/db"
	internalid "palpanel/internal/id"
)

const (
	hostMigrationRequestLimit = 64 << 10
	hostMigrationPackageLimit = 512 << 20
)

var (
	hostMigrationHelperSHA256 string
	hostMigrationInstallMu    sync.Mutex
)

type hostMigrationRequest struct {
	MigrationSourceID string `json:"migration_source_id"`
	SteamID           string `json:"steam_id"`
	Name              string `json:"name,omitempty"`
	Confirm           bool   `json:"confirm,omitempty"`
}

type hostMigrationPlan struct {
	SteamID            string   `json:"steam_id"`
	SourceUID          string   `json:"source_uid"`
	TargetUID          string   `json:"target_uid"`
	Strategy           string   `json:"strategy"`
	CanExecute         bool     `json:"can_execute"`
	SourcePlayerFile   string   `json:"source_player_file"`
	SourceDPSExists    bool     `json:"source_dps_exists"`
	TargetPlayerExists bool     `json:"target_player_exists"`
	TargetDPSExists    bool     `json:"target_dps_exists"`
	Warnings           []string `json:"warnings"`
}

type hostMigrationExecution struct {
	Plan         hostMigrationPlan `json:"plan"`
	Verification any               `json:"verification"`
}

var (
	runHostMigrationHelper   = runHostMigrationCommand
	hostMigrationServerState = func(s Server, ctx context.Context) (string, error) {
		status, err := s.server.Status(ctx)
		if err != nil {
			return "", err
		}
		state := strings.ToLower(strings.TrimSpace(status.Container.Status))
		if state == "" && !status.Container.Exists {
			state = "not_found"
		}
		return state, nil
	}
	hostMigrationStopServer = func(s Server, ctx context.Context) error {
		return s.server.Stop(ctx)
	}
	hostMigrationStartServer = func(s Server, ctx context.Context) error {
		return s.server.Start(ctx)
	}
	hostMigrationCopyTree = copyHostMigrationTree
)

func (s Server) inspectSaveSourceImportDispatch(c *gin.Context) {
	input, claimed, err := readHostMigrationRequest(c)
	if err != nil {
		fail(c, http.StatusBadRequest, "host_migration_request_invalid", err.Error())
		return
	}
	if !claimed {
		s.inspectSaveSourceImport(c)
		return
	}
	s.inspectHostSaveMigration(c, input)
}

func (s Server) dispatchSaveSourceImport(c *gin.Context) {
	input, claimed, err := readHostMigrationRequest(c)
	if err != nil {
		fail(c, http.StatusBadRequest, "host_migration_request_invalid", err.Error())
		return
	}
	if !claimed {
		s.handleSaveSourceImport(c)
		return
	}
	s.executeHostSaveMigration(c, input)
}

func readHostMigrationRequest(c *gin.Context) (hostMigrationRequest, bool, error) {
	if c.ContentType() != "application/json" {
		return hostMigrationRequest{}, false, nil
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, hostMigrationRequestLimit+1))
	if err != nil {
		return hostMigrationRequest{}, false, fmt.Errorf("read request body: %w", err)
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if len(raw) > hostMigrationRequestLimit {
		return hostMigrationRequest{}, false, errors.New("request body is too large")
	}
	var input hostMigrationRequest
	if err := json.Unmarshal(raw, &input); err != nil {
		return hostMigrationRequest{}, false, nil
	}
	input.MigrationSourceID = strings.TrimSpace(input.MigrationSourceID)
	input.SteamID = strings.TrimSpace(input.SteamID)
	input.Name = strings.TrimSpace(input.Name)
	if input.MigrationSourceID == "" && input.SteamID == "" {
		return hostMigrationRequest{}, false, nil
	}
	if input.MigrationSourceID == "" || input.SteamID == "" {
		return hostMigrationRequest{}, true, errors.New("migration_source_id and steam_id are required")
	}
	return input, true, nil
}

func (s Server) inspectHostSaveMigration(c *gin.Context, input hostMigrationRequest) {
	source, err := s.hostMigrationSource(c.Request.Context(), input.MigrationSourceID)
	if err != nil {
		writeHostMigrationSourceError(c, err)
		return
	}
	plan, err := hostMigrationPlanFor(c.Request.Context(), source.Path, input.SteamID)
	if err != nil {
		fail(c, http.StatusBadGateway, "host_migration_plan_failed", err.Error())
		return
	}
	ok(c, plan)
}

func (s Server) executeHostSaveMigration(c *gin.Context, input hostMigrationRequest) {
	if !input.Confirm {
		fail(c, http.StatusBadRequest, "host_migration_confirmation_required", "confirm must be true")
		return
	}
	source, err := s.hostMigrationSource(c.Request.Context(), input.MigrationSourceID)
	if err != nil {
		writeHostMigrationSourceError(c, err)
		return
	}
	plan, err := hostMigrationPlanFor(c.Request.Context(), source.Path, input.SteamID)
	if err != nil {
		fail(c, http.StatusBadGateway, "host_migration_plan_failed", err.Error())
		return
	}
	if !plan.CanExecute {
		fail(c, http.StatusConflict, "host_migration_blocked", strings.Join(plan.Warnings, " "))
		return
	}
	name := input.Name
	if name == "" {
		name = source.Name + "（主机迁移）"
	}
	if len([]rune(name)) > 80 {
		fail(c, http.StatusBadRequest, "save_source_name_too_long", "save source name is too long")
		return
	}
	if err := os.MkdirAll(s.cfg.SaveSourcesDir, 0o750); err != nil {
		fail(c, http.StatusInternalServerError, "host_migration_output_failed", err.Error())
		return
	}
	newID := internalid.New("save")
	output := filepath.Join(s.cfg.SaveSourcesDir, newID)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Minute)
	defer cancel()
	raw, err := runHostMigrationHelper(ctx, "host-execute", "--input", source.Path, "--output", output, "--steam-id", input.SteamID)
	if err != nil {
		_ = os.RemoveAll(output)
		fail(c, http.StatusBadGateway, "host_migration_execute_failed", err.Error())
		return
	}
	var execution hostMigrationExecution
	if err := json.Unmarshal(raw, &execution); err != nil {
		_ = os.RemoveAll(output)
		fail(c, http.StatusBadGateway, "host_migration_response_invalid", err.Error())
		return
	}
	if _, err := os.Stat(filepath.Join(output, "Level.sav")); err != nil {
		_ = os.RemoveAll(output)
		fail(c, http.StatusBadGateway, "host_migration_output_invalid", "migration output does not contain Level.sav")
		return
	}

	activation, err := s.deployHostMigrationWorld(ctx, output, newID)
	if err != nil {
		_ = os.RemoveAll(output)
		fail(c, http.StatusConflict, "host_migration_activation_failed", err.Error())
		return
	}
	rollback := func() {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer rollbackCancel()
		_ = activation.rollback(s, rollbackCtx)
		_ = os.RemoveAll(output)
	}

	created := db.SaveSource{ID: newID, Name: name, Kind: "import", Path: output, Active: false}
	if err := s.store.UpsertSaveSource(ctx, created); err != nil {
		rollback()
		fail(c, http.StatusInternalServerError, "host_migration_register_failed", err.Error())
		return
	}
	if err := s.store.ActivateSaveSource(ctx, "server"); err != nil {
		_ = s.store.DeleteSaveSource(context.Background(), newID)
		rollback()
		fail(c, http.StatusInternalServerError, "host_migration_server_source_activate_failed", err.Error())
		return
	}
	activation.commit()
	s.saveIndex.SetSourcePath("")
	s.saveIndex.Invalidate()
	s.invalidateServerSaveIndex()
	s.invalidateServerCaches()
	stored, err := s.store.GetSaveSource(ctx, newID)
	if err == nil {
		created = stored
	}
	ok(c, gin.H{
		"source": created,
		"plan":   execution.Plan,
		"verification": gin.H{
			"uid_remap":                      execution.Verification,
			"dedicated_server_name":          activation.DedicatedServerName,
			"previous_dedicated_server_name": activation.PreviousDedicatedServerName,
			"server_save_path":               activation.ServerSavePath,
			"server_was_running":             activation.ServerWasRunning,
			"server_restarted":               activation.ServerRestarted,
		},
	})
}

type hostMigrationActivation struct {
	DedicatedServerName         string
	PreviousDedicatedServerName string
	ServerSavePath              string
	ServerWasRunning            bool
	ServerRestarted             bool
	settingsPath                string
	originalSettings            []byte
	serverStopped               bool
	settingsChanged             bool
	savePublished               bool
	rollbackPending             bool
}

func (s Server) deployHostMigrationWorld(ctx context.Context, sourcePath, migrationID string) (hostMigrationActivation, error) {
	settingsPath := filepath.Join(s.cfg.ServerDirectory(), "Pal", "Saved", "Config", "WindowsServer", "GameUserSettings.ini")
	if !pathWithin(s.cfg.ServerDirectory(), settingsPath) {
		return hostMigrationActivation{}, errors.New("GameUserSettings.ini is outside the managed server directory")
	}
	originalSettings, err := os.ReadFile(settingsPath)
	if err != nil {
		return hostMigrationActivation{}, fmt.Errorf("read GameUserSettings.ini: %w", err)
	}
	worldName := hostMigrationWorldName(migrationID)
	updatedSettings, previousName, err := replaceDedicatedServerName(originalSettings, worldName)
	if err != nil {
		return hostMigrationActivation{}, err
	}

	saveRoot := filepath.Join(s.cfg.ServerDirectory(), "Pal", "Saved", "SaveGames", "0")
	if !pathWithin(s.cfg.ServerDirectory(), saveRoot) {
		return hostMigrationActivation{}, errors.New("server save root is outside the managed server directory")
	}
	if err := os.MkdirAll(saveRoot, 0o750); err != nil {
		return hostMigrationActivation{}, fmt.Errorf("create server save root: %w", err)
	}
	serverSavePath := filepath.Join(saveRoot, worldName)
	if _, err := os.Lstat(serverSavePath); err == nil {
		return hostMigrationActivation{}, fmt.Errorf("target server save already exists: %s", worldName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return hostMigrationActivation{}, fmt.Errorf("inspect target server save: %w", err)
	}

	activation := hostMigrationActivation{
		DedicatedServerName:         worldName,
		PreviousDedicatedServerName: previousName,
		ServerSavePath:              serverSavePath,
		settingsPath:                settingsPath,
		originalSettings:            originalSettings,
		rollbackPending:             true,
	}
	failWithRollback := func(cause error) (hostMigrationActivation, error) {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if rollbackErr := activation.rollback(s, rollbackCtx); rollbackErr != nil {
			return hostMigrationActivation{}, fmt.Errorf("%v; rollback failed: %w", cause, rollbackErr)
		}
		return hostMigrationActivation{}, cause
	}

	state, err := hostMigrationServerState(s, ctx)
	if err != nil {
		return hostMigrationActivation{}, fmt.Errorf("read server state: %w", err)
	}
	switch state {
	case "running":
		activation.ServerWasRunning = true
	case "", "stopped", "exited", "created", "not_found", "not-found", "dead":
	default:
		return hostMigrationActivation{}, fmt.Errorf("server state %q is not safe for save switching", state)
	}

	// All writes under Pal/Saved happen only after the game server is stopped.
	if activation.ServerWasRunning {
		if err := hostMigrationStopServer(s, ctx); err != nil {
			return hostMigrationActivation{}, fmt.Errorf("stop server before save switch: %w", err)
		}
		activation.serverStopped = true
	}

	temporaryPath := serverSavePath + ".palpanel-deploy"
	_ = os.RemoveAll(temporaryPath)
	if err := hostMigrationCopyTree(sourcePath, temporaryPath); err != nil {
		_ = os.RemoveAll(temporaryPath)
		return failWithRollback(fmt.Errorf("copy migrated save into server: %w", err))
	}
	if err := os.Rename(temporaryPath, serverSavePath); err != nil {
		_ = os.RemoveAll(temporaryPath)
		return failWithRollback(fmt.Errorf("publish migrated server save: %w", err))
	}
	activation.savePublished = true

	if err := writeHostMigrationFileAtomic(settingsPath, updatedSettings); err != nil {
		return failWithRollback(fmt.Errorf("write DedicatedServerName: %w", err))
	}
	activation.settingsChanged = true

	if activation.ServerWasRunning {
		if err := hostMigrationStartServer(s, ctx); err != nil {
			return failWithRollback(fmt.Errorf("start server with migrated save: %w", err))
		}
		activation.serverStopped = false
		activation.ServerRestarted = true
	}
	return activation, nil
}

func (activation *hostMigrationActivation) commit() {
	activation.rollbackPending = false
	activation.originalSettings = nil
}

func (activation *hostMigrationActivation) rollback(s Server, ctx context.Context) error {
	if activation == nil || !activation.rollbackPending {
		return nil
	}
	activation.rollbackPending = false
	var failures []string
	if activation.ServerWasRunning && activation.ServerRestarted {
		if err := hostMigrationStopServer(s, ctx); err != nil {
			// Do not rewrite the selected world or delete files while the migrated
			// server may still be running. Leaving the new world intact is safer.
			return fmt.Errorf("stop migrated server before rollback: %w", err)
		}
		activation.serverStopped = true
	}
	if activation.settingsChanged && len(activation.originalSettings) > 0 {
		if err := writeHostMigrationFileAtomic(activation.settingsPath, activation.originalSettings); err != nil {
			if activation.ServerWasRunning && activation.serverStopped {
				if startErr := hostMigrationStartServer(s, ctx); startErr != nil {
					return fmt.Errorf("restore GameUserSettings.ini: %v; restart migrated server also failed: %w", err, startErr)
				}
				activation.serverStopped = false
			}
			return fmt.Errorf("restore GameUserSettings.ini: %w", err)
		}
	}
	if activation.savePublished && activation.ServerSavePath != "" {
		if err := os.RemoveAll(activation.ServerSavePath); err != nil {
			failures = append(failures, "remove migrated server save: "+err.Error())
		}
	}
	if activation.ServerWasRunning && activation.serverStopped {
		if err := hostMigrationStartServer(s, ctx); err != nil {
			failures = append(failures, "restart original server save: "+err.Error())
		} else {
			activation.serverStopped = false
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func hostMigrationWorldName(migrationID string) string {
	digest := sha256.Sum256([]byte("palpanel-host-migration:" + strings.TrimSpace(migrationID)))
	return strings.ToUpper(fmt.Sprintf("%x", digest[:16]))
}

func replaceDedicatedServerName(payload []byte, next string) ([]byte, string, error) {
	next = strings.TrimSpace(next)
	if len(next) != 32 {
		return nil, "", errors.New("generated DedicatedServerName must contain 32 characters")
	}
	newline := "\n"
	if bytes.Contains(payload, []byte("\r\n")) {
		newline = "\r\n"
	}
	text := strings.ReplaceAll(string(payload), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	matches := 0
	previous := ""
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, found := strings.Cut(trimmed, "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "DedicatedServerName") {
			continue
		}
		matches++
		previous = strings.TrimSpace(value)
		prefixLength := len(line) - len(strings.TrimLeft(line, " \t"))
		lines[index] = line[:prefixLength] + "DedicatedServerName=" + next
	}
	if matches == 0 {
		return nil, "", errors.New("GameUserSettings.ini does not contain DedicatedServerName")
	}
	if matches > 1 {
		return nil, "", errors.New("GameUserSettings.ini contains multiple DedicatedServerName entries")
	}
	if previous == "" {
		return nil, "", errors.New("GameUserSettings.ini DedicatedServerName is empty")
	}
	updated := strings.Join(lines, newline)
	return []byte(updated), previous, nil
}

func copyHostMigrationTree(source, destination string) error {
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !sourceInfo.IsDir() || sourceInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("migration source must be a regular directory")
	}
	if err := os.MkdirAll(destination, 0o750); err != nil {
		return err
	}
	return filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return errors.New("migration source contains an unsafe path")
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("migration source contains unsupported file type: %s", relative)
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeOutputErr := output.Close()
		closeInputErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutputErr != nil {
			return closeOutputErr
		}
		return closeInputErr
	})
}

func writeHostMigrationFileAtomic(path string, payload []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".game-user-settings-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	complete := false
	defer func() {
		_ = temporary.Close()
		if !complete {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	complete = true
	return nil
}

func (s Server) hostMigrationSource(ctx context.Context, sourceID string) (db.SaveSource, error) {
	source, err := s.store.GetSaveSource(ctx, sourceID)
	if err != nil {
		return db.SaveSource{}, err
	}
	if source.Kind != "import" || !pathWithin(s.cfg.SaveSourcesDir, source.Path) {
		return db.SaveSource{}, errHostMigrationSourceRejected
	}
	if _, err := os.Stat(filepath.Join(source.Path, "Level.sav")); err != nil {
		return db.SaveSource{}, fmt.Errorf("imported save is unavailable: %w", err)
	}
	if info, err := os.Stat(filepath.Join(source.Path, "Players")); err != nil || !info.IsDir() {
		return db.SaveSource{}, errors.New("imported save does not contain Players directory")
	}
	return source, nil
}

var errHostMigrationSourceRejected = errors.New("only managed imported save sources can be migrated")

func writeHostMigrationSourceError(c *gin.Context, err error) {
	status := http.StatusConflict
	code := "host_migration_source_rejected"
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, os.ErrNotExist) {
		code = "save_source_not_found"
		status = http.StatusNotFound
	}
	fail(c, status, code, err.Error())
}

func hostMigrationPlanFor(ctx context.Context, sourcePath, steamID string) (hostMigrationPlan, error) {
	raw, err := runHostMigrationHelper(ctx, "host-plan", "--input", sourcePath, "--steam-id", steamID)
	if err != nil {
		return hostMigrationPlan{}, err
	}
	var plan hostMigrationPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return hostMigrationPlan{}, fmt.Errorf("decode helper response: %w", err)
	}
	return plan, nil
}

func runHostMigrationCommand(ctx context.Context, args ...string) ([]byte, error) {
	binary, err := resolveHostMigrationBinary(ctx)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	if stdout.Len() > 8<<20 {
		return nil, errors.New("helper response is too large")
	}
	return stdout.Bytes(), nil
}

func resolveHostMigrationBinary(ctx context.Context) (string, error) {
	if configured := strings.TrimSpace(os.Getenv("PALPANEL_UID_REMAPPER_BIN")); configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured, nil
		}
		return "", fmt.Errorf("configured UID remapper is unavailable: %s", configured)
	}
	var bootstrapErr error
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "palworld-uid-remap")
		if hostMigrationHelperMatches(candidate) {
			return candidate, nil
		}
		bootstrapErr = bootstrapHostMigrationHelper(ctx, candidate)
		if bootstrapErr == nil {
			return candidate, nil
		}
	}
	if candidate, err := exec.LookPath("palworld-uid-remap"); err == nil {
		if hostMigrationHelperMatches(candidate) {
			return candidate, nil
		}
		return "", fmt.Errorf("palworld-uid-remap helper checksum mismatch: %s", candidate)
	}
	if bootstrapErr != nil {
		return "", fmt.Errorf("palworld-uid-remap helper bootstrap failed: %w", bootstrapErr)
	}
	return "", errors.New("palworld-uid-remap helper is unavailable")
}

func hostMigrationHelperMatches(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	expected := strings.ToLower(strings.TrimSpace(hostMigrationHelperSHA256))
	if len(expected) != 64 {
		return true
	}
	actual, err := hostMigrationFileSHA256(path)
	return err == nil && strings.EqualFold(actual, expected)
}

func bootstrapHostMigrationHelper(ctx context.Context, destination string) error {
	hostMigrationInstallMu.Lock()
	defer hostMigrationInstallMu.Unlock()
	if hostMigrationHelperMatches(destination) {
		return nil
	}
	expected := strings.ToLower(strings.TrimSpace(hostMigrationHelperSHA256))
	if len(expected) != 64 {
		return errors.New("UID remapper helper checksum was not embedded at build time")
	}
	packageURL := strings.TrimSpace(os.Getenv("PALPANEL_UID_REMAPPER_PACKAGE_URL"))
	if packageURL == "" {
		packageURL = fmt.Sprintf(
			"https://github.com/%s/releases/download/uitok-stable-%s-p%s/uitok-palworld-panel_stable-%s_patch-%s_linux-amd64.tar.gz",
			patchRepository, patchTargetVersion, patchVersion, patchTargetVersion, patchVersion,
		)
	}
	parsed, err := url.Parse(packageURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid UID remapper package URL: %s", packageURL)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, packageURL, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("download UID remapper package: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download UID remapper package: HTTP %d", response.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	archive, err := os.CreateTemp(filepath.Dir(destination), ".palworld-uid-remap-package-*.tar.gz")
	if err != nil {
		return err
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)
	written, copyErr := io.Copy(archive, io.LimitReader(response.Body, hostMigrationPackageLimit+1))
	closeErr := archive.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > hostMigrationPackageLimit {
		return errors.New("UID remapper package is too large")
	}
	temporary := destination + ".bootstrap"
	_ = os.Remove(temporary)
	if err := extractHostMigrationHelperArchive(archivePath, temporary); err != nil {
		return err
	}
	defer os.Remove(temporary)
	actual, err := hostMigrationFileSHA256(temporary)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("UID remapper helper checksum mismatch: got %s, expected %s", actual, expected)
	}
	if err := os.Chmod(temporary, 0o755); err != nil {
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return fmt.Errorf("install UID remapper helper: %w", err)
	}
	return nil
}

func extractHostMigrationHelperArchive(archivePath, destination string) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	found := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		clean := filepath.ToSlash(filepath.Clean(header.Name))
		if filepath.IsAbs(header.Name) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("unsafe archive path: %s", header.Name)
		}
		if !strings.HasSuffix(clean, "/overlay/bin/palworld-uid-remap") {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > 128<<20 {
			return errors.New("UID remapper helper archive entry is invalid")
		}
		found++
		if found > 1 {
			return errors.New("UID remapper package contains multiple helpers")
		}
		output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(output, reader, header.Size)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if found != 1 {
		return errors.New("UID remapper helper is missing from package")
	}
	return nil
}

func hostMigrationFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
