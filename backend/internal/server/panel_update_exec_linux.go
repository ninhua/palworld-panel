//go:build linux

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/panelupdater"
)

const (
	panelExecMarkerSchema       = 2
	panelExecHealthTimeout      = 75 * time.Second
	panelExecHealthPollInterval = time.Second
)

var (
	panelUpdateWatchers sync.Map
	panelExecRollbackMu sync.Mutex
)

type panelExecUpdateResult struct {
	SchemaVersion  int    `json:"schema_version"`
	JobID          string `json:"job_id"`
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	ErrorCode      string `json:"error_code,omitempty"`
	Detail         string `json:"detail,omitempty"`
	FinishedAt     string `json:"finished_at"`
}

func replaceCurrentPanelProcess(binary string) error {
	return syscall.Exec(binary, os.Args, os.Environ())
}

func resolvePanelUpdateMode(cfg appconfig.Config) (string, error) {
	requested := strings.ToLower(strings.TrimSpace(os.Getenv("PALPANEL_UPDATE_MODE")))
	if requested == "" {
		requested = panelUpdateModeAuto
	}
	switch requested {
	case panelUpdateModeAuto:
		externalErr := externalPanelUpdaterReady(cfg)
		if externalErr == nil {
			return panelUpdateModeExternal, nil
		}
		execErr := execPanelUpdaterReady()
		if execErr == nil {
			return panelUpdateModeExec, nil
		}
		return "", fmt.Errorf("external updater unavailable: %v; exec hot updater unavailable: %v", externalErr, execErr)
	case panelUpdateModeExternal, "systemd":
		if err := externalPanelUpdaterReady(cfg); err != nil {
			return "", err
		}
		return panelUpdateModeExternal, nil
	case panelUpdateModeExec, "hot", "hot-exec":
		if err := execPanelUpdaterReady(); err != nil {
			return "", err
		}
		return panelUpdateModeExec, nil
	default:
		return "", fmt.Errorf("PALPANEL_UPDATE_MODE must be auto, external, or exec")
	}
}

func externalPanelUpdaterReady(cfg appconfig.Config) error {
	binary, err := executablePath()
	if err != nil {
		return err
	}
	versionDir := filepath.Dir(filepath.Dir(binary))
	installRoot := filepath.Dir(versionDir)
	current := filepath.Join(installRoot, "current")
	resolved, err := filepath.EvalSymlinks(current)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(versionDir) {
		return fmt.Errorf("panel is not running from a versioned current installation")
	}
	for _, path := range []string{
		"/usr/libexec/palpanel-updater",
		"/etc/systemd/system/palpanel-update.service",
		"/etc/systemd/system/palpanel-update.path",
	} {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("required external updater component is missing: %s", path)
		}
	}
	stateDir := panelupdater.StateDir(cfg.DataDir)
	if err := os.MkdirAll(panelupdater.OperationsDir(cfg.DataDir), 0o700); err != nil {
		return fmt.Errorf("panel update state directory is unavailable: %w", err)
	}
	info, err := os.Stat(stateDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("panel update state directory is invalid: %s", stateDir)
	}
	return nil
}

func execPanelUpdaterReady() error {
	binary, err := executablePath()
	if err != nil {
		return err
	}
	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("current panel binary is not a regular file: %s", binary)
	}
	if _, err := os.Stat(panelRestartMarkerPath(binary)); err == nil {
		return fmt.Errorf("a previous exec panel update is still pending verification")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot inspect exec update marker: %w", err)
	}
	directory := filepath.Dir(binary)
	probe, err := os.CreateTemp(directory, ".palpanel-update-write-test-")
	if err != nil {
		return fmt.Errorf("panel binary directory is not writable: %w", err)
	}
	probePath := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(probePath)
	if closeErr != nil {
		return fmt.Errorf("panel binary directory write probe failed: %w", closeErr)
	}
	if removeErr != nil {
		return fmt.Errorf("panel binary directory cleanup failed: %w", removeErr)
	}
	backupDir := filepath.Join(directory, ".palpanel-update-backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return fmt.Errorf("cannot create exec update backup directory: %w", err)
	}
	return nil
}

func recoverPanelUpdateIfNeeded(cfg appconfig.Config, store *db.Store) {
	consumePanelUpdateResult(cfg, store)
	consumePanelExecUpdateResult(store)
	if _, err := os.Stat(panelupdater.InProgressPath(cfg.DataDir)); err == nil {
		startPanelUpdateResultWatcher(cfg, store)
	}
	preparePanelExecUpdateStartup(store)
}

func startPanelUpdateResultWatcher(cfg appconfig.Config, store *db.Store) {
	if store == nil || strings.TrimSpace(cfg.DataDir) == "" {
		return
	}
	key := filepath.Clean(cfg.DataDir)
	if _, loaded := panelUpdateWatchers.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	go func() {
		defer panelUpdateWatchers.Delete(key)
		deadline := time.Now().Add(10 * time.Minute)
		for time.Now().Before(deadline) {
			if consumePanelUpdateResult(cfg, store) {
				return
			}
			time.Sleep(time.Second)
		}
	}()
}

func consumePanelUpdateResult(cfg appconfig.Config, store *db.Store) bool {
	if store == nil {
		return false
	}
	result, ok, err := panelupdater.ConsumeResult(cfg.DataDir)
	if err != nil {
		log.Printf("panel update result read failed: %v", err)
		return false
	}
	if !ok {
		return false
	}
	status := "failed"
	progress := 100
	message := result.Message
	code := result.ErrorCode
	detail := result.Detail
	if result.Status == "completed" {
		status = "completed"
		code = ""
		if message == "" {
			message = "PalPanel " + result.TargetVersion + " update completed and passed health checks"
		}
	} else if result.RollbackSucceeded {
		if message == "" {
			message = "panel update failed; previous version restored and verified"
		}
		if code == "" {
			code = "panel_update_rolled_back"
		}
	} else {
		if message == "" {
			message = "panel update failed and requires manual recovery"
		}
		if code == "" {
			code = "panel_update_failed"
		}
	}
	if err := store.UpdateJobWithCode(context.Background(), result.JobID, status, progress, message, detail, code); err != nil {
		log.Printf("panel update job result persistence failed: %v", err)
	}
	return true
}

func panelExecResultPath(binary string) string {
	return filepath.Join(filepath.Dir(binary), ".palpanel-update-result.json")
}

func writePanelExecUpdateResult(binary string, result panelExecUpdateResult) error {
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	path := panelExecResultPath(binary)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(body, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func consumePanelExecUpdateResult(store *db.Store) bool {
	if store == nil {
		return false
	}
	binary, err := executablePath()
	if err != nil {
		return false
	}
	path := panelExecResultPath(binary)
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		log.Printf("exec panel update result read failed: %v", err)
		return false
	}
	var result panelExecUpdateResult
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("exec panel update result decode failed: %v", err)
		return false
	}
	status := result.Status
	if status != "completed" {
		status = "failed"
	}
	if err := store.UpdateJobWithCode(context.Background(), result.JobID, status, 100, result.Message, result.Detail, result.ErrorCode); err != nil {
		log.Printf("exec panel update result persistence failed: %v", err)
		return false
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("exec panel update result cleanup failed: %v", err)
	}
	return true
}

func readPanelExecMarker() (string, panelRestartMarker, bool, error) {
	binary, err := executablePath()
	if err != nil {
		return "", panelRestartMarker{}, false, err
	}
	body, err := os.ReadFile(panelRestartMarkerPath(binary))
	if os.IsNotExist(err) {
		return binary, panelRestartMarker{}, false, nil
	}
	if err != nil {
		return binary, panelRestartMarker{}, false, err
	}
	var marker panelRestartMarker
	if err := json.Unmarshal(body, &marker); err != nil {
		return binary, panelRestartMarker{}, false, err
	}
	return binary, marker, true, nil
}

func validatePanelExecMarker(binary string, marker panelRestartMarker) error {
	if marker.SchemaVersion != panelExecMarkerSchema {
		return fmt.Errorf("unsupported exec update marker schema: %d", marker.SchemaVersion)
	}
	if filepath.Clean(marker.BinaryPath) != binary {
		return fmt.Errorf("exec update marker binary path mismatch")
	}
	backupRoot := filepath.Join(filepath.Dir(binary), ".palpanel-update-backups")
	backupRelative, err := filepath.Rel(backupRoot, filepath.Clean(marker.BackupPath))
	if err != nil || backupRelative == ".." || strings.HasPrefix(backupRelative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("exec update marker contains unsafe backup path")
	}
	if len(marker.ExpectedSHA256) != 64 || len(marker.PreviousSHA256) != 64 {
		return fmt.Errorf("exec update marker contains invalid SHA-256")
	}
	if marker.JobID == "" || !panelReleasePattern.MatchString(marker.CurrentVersion) || !panelReleasePattern.MatchString(marker.TargetVersion) {
		return fmt.Errorf("exec update marker contains invalid version metadata")
	}
	if marker.HealthPort < 1 || marker.HealthPort > 65535 {
		return fmt.Errorf("exec update marker contains invalid health port")
	}
	return nil
}

func preparePanelExecUpdateStartup(store *db.Store) {
	binary, marker, ok, err := readPanelExecMarker()
	if err != nil {
		log.Printf("panel exec update marker read failed: %v", err)
		return
	}
	if !ok {
		return
	}
	if marker.SchemaVersion != panelExecMarkerSchema {
		recoverLegacyPanelUpdateIfNeeded(store)
		return
	}
	if err := validatePanelExecMarker(binary, marker); err != nil {
		log.Printf("panel exec update marker validation failed: %v", err)
		return
	}
	actualSHA, err := sha256PanelFile(binary)
	if err != nil {
		if rollbackErr := rollbackPanelExecUpdate(marker, fmt.Errorf("hash activated panel binary: %w", err), store); rollbackErr != nil {
			log.Printf("panel exec update rollback failed: %v", rollbackErr)
		}
		return
	}
	if strings.EqualFold(actualSHA, marker.ExpectedSHA256) {
		if store != nil {
			_ = store.UpdateJobWithCode(context.Background(), marker.JobID, "running", 96, "updated panel process started; verifying readiness and version", "", "")
		}
		return
	}
	if strings.EqualFold(actualSHA, marker.PreviousSHA256) {
		_ = removePanelRestartMarker(binary)
		result := panelExecUpdateResult{
			SchemaVersion: 1, JobID: marker.JobID, CurrentVersion: marker.CurrentVersion, TargetVersion: marker.TargetVersion,
			Status: "failed", Message: "exec panel update was interrupted before activation", ErrorCode: "panel_exec_activation_interrupted",
			Detail: "the previous binary remained active", FinishedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		_ = writePanelExecUpdateResult(binary, result)
		consumePanelExecUpdateResult(store)
		return
	}
	if rollbackErr := rollbackPanelExecUpdate(marker, fmt.Errorf("activated panel sha256 = %s, expected %s", actualSHA, marker.ExpectedSHA256), store); rollbackErr != nil {
		log.Printf("panel exec update rollback failed: %v", rollbackErr)
	}
}

// StartPanelUpdateStartupVerification must be called after the HTTP listener is started.
// It leaves the restart marker in place until readiness and version checks succeed.
func StartPanelUpdateStartupVerification(cfg appconfig.Config, store *db.Store) {
	binary, marker, ok, err := readPanelExecMarker()
	if err != nil || !ok || marker.SchemaVersion != panelExecMarkerSchema {
		return
	}
	if err := validatePanelExecMarker(binary, marker); err != nil {
		log.Printf("panel exec startup verification skipped: %v", err)
		return
	}
	go verifyPanelExecStartup(cfg, store, marker)
}

func verifyPanelExecStartup(_ appconfig.Config, store *db.Store, marker panelRestartMarker) {
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", marker.HealthPort)
	deadline := time.Now().Add(panelExecHealthTimeout)
	consecutive := 0
	var lastErr error
	for time.Now().Before(deadline) {
		if err := probePanelExecHealth(client, baseURL, marker.TargetVersion); err != nil {
			lastErr = err
			consecutive = 0
		} else {
			consecutive++
			if consecutive >= panelExecSuccessThreshold {
				if store != nil {
					_ = store.UpdateJobWithCode(context.Background(), marker.JobID, "completed", 100, "PalPanel "+marker.TargetVersion+" exec hot update completed and passed readiness checks", "", "")
				}
				if err := removePanelRestartMarker(marker.BinaryPath); err != nil {
					log.Printf("panel exec update marker cleanup failed: %v", err)
				}
				prunePanelExecBackups(filepath.Dir(marker.BinaryPath), 4)
				return
			}
		}
		time.Sleep(panelExecHealthPollInterval)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("readiness verification timed out")
	}
	if err := rollbackPanelExecUpdate(marker, lastErr, store); err != nil {
		log.Printf("panel exec health rollback failed: %v", err)
	}
}

func probePanelExecHealth(client *http.Client, baseURL, targetVersion string) error {
	readyRequest, _ := http.NewRequest(http.MethodGet, baseURL+"/api/ready", nil)
	readyResponse, err := client.Do(readyRequest)
	if err != nil {
		return fmt.Errorf("ready probe failed: %w", err)
	}
	readyBody, readErr := io.ReadAll(io.LimitReader(readyResponse.Body, 1<<20))
	readyResponse.Body.Close()
	if readErr != nil {
		return fmt.Errorf("ready probe response failed: %w", readErr)
	}
	var ready struct {
		OK   bool `json:"ok"`
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if readyResponse.StatusCode != http.StatusOK || json.Unmarshal(readyBody, &ready) != nil || !ready.OK || ready.Data.Status != "ready" {
		return fmt.Errorf("ready probe returned HTTP %d: %s", readyResponse.StatusCode, strings.TrimSpace(string(readyBody)))
	}

	versionRequest, _ := http.NewRequest(http.MethodGet, baseURL+"/api/patch/info", nil)
	versionResponse, err := client.Do(versionRequest)
	if err != nil {
		return fmt.Errorf("version probe failed: %w", err)
	}
	versionBody, readErr := io.ReadAll(io.LimitReader(versionResponse.Body, 1<<20))
	versionResponse.Body.Close()
	if readErr != nil {
		return fmt.Errorf("version probe response failed: %w", readErr)
	}
	var version struct {
		OK   bool `json:"ok"`
		Data struct {
			Build struct {
				Version string `json:"version"`
			} `json:"build"`
		} `json:"data"`
	}
	if versionResponse.StatusCode != http.StatusOK || json.Unmarshal(versionBody, &version) != nil || !version.OK {
		return fmt.Errorf("version probe returned HTTP %d: %s", versionResponse.StatusCode, strings.TrimSpace(string(versionBody)))
	}
	if version.Data.Build.Version != targetVersion {
		return fmt.Errorf("running panel version = %s, expected %s", version.Data.Build.Version, targetVersion)
	}
	return nil
}

// RollbackPanelUpdateOnStartupFailure is called by main when startup exits before
// the readiness verifier can confirm the new process.
func RollbackPanelUpdateOnStartupFailure(cause error) error {
	_, marker, ok, err := readPanelExecMarker()
	if err != nil || !ok || marker.SchemaVersion != panelExecMarkerSchema {
		return err
	}
	return rollbackPanelExecUpdate(marker, cause, nil)
}

func rollbackPanelExecUpdate(marker panelRestartMarker, cause error, store *db.Store) error {
	panelExecRollbackMu.Lock()
	defer panelExecRollbackMu.Unlock()

	binary, current, ok, err := readPanelExecMarker()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if current.JobID != marker.JobID {
		return fmt.Errorf("another panel update replaced the pending marker")
	}
	if err := validatePanelExecMarker(binary, current); err != nil {
		return err
	}
	if cause == nil {
		cause = fmt.Errorf("updated panel startup verification failed")
	}
	if store != nil {
		_ = store.UpdateJobWithCode(context.Background(), current.JobID, "failed", 100, "updated panel failed readiness checks; restoring previous binary", cause.Error(), "panel_exec_startup_verification_failed")
	}
	if err := restorePanelBackup(binary, current.BackupPath); err != nil {
		return fmt.Errorf("restore previous panel binary: %w (original error: %v)", err, cause)
	}
	restoredSHA, err := sha256PanelFile(binary)
	if err != nil || !strings.EqualFold(restoredSHA, current.PreviousSHA256) {
		if err != nil {
			return fmt.Errorf("verify restored panel binary: %w", err)
		}
		return fmt.Errorf("restored panel sha256 = %s, expected %s", restoredSHA, current.PreviousSHA256)
	}
	if err := removePanelRestartMarker(binary); err != nil {
		return fmt.Errorf("remove panel update marker: %w", err)
	}
	result := panelExecUpdateResult{
		SchemaVersion: 1, JobID: current.JobID, CurrentVersion: current.CurrentVersion, TargetVersion: current.TargetVersion,
		Status: "failed", Message: "PalPanel exec hot update failed; previous version restored",
		ErrorCode: "panel_exec_update_rolled_back", Detail: cause.Error(), FinishedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := writePanelExecUpdateResult(binary, result); err != nil {
		log.Printf("panel exec rollback result write failed: %v", err)
	}
	log.Printf("panel exec update failed; restored %s and restarting: %v", current.CurrentVersion, cause)
	if err := syscall.Exec(binary, os.Args, os.Environ()); err != nil {
		return fmt.Errorf("restart restored panel binary: %w", err)
	}
	return nil
}

func prunePanelExecBackups(binaryDir string, keep int) {
	backupDir := filepath.Join(binaryDir, ".palpanel-update-backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) <= keep {
		return
	}
	for index := 0; index < len(entries)-keep; index++ {
		if entries[index].Type().IsRegular() {
			_ = os.Remove(filepath.Join(backupDir, entries[index].Name()))
		}
	}
}

func recoverLegacyPanelUpdateIfNeeded(store *db.Store) {
	binary, err := executablePath()
	if err != nil {
		return
	}
	markerPath := panelRestartMarkerPath(binary)
	body, err := os.ReadFile(markerPath)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		log.Printf("panel update restart marker read failed: %v", err)
		return
	}
	var marker panelRestartMarker
	if err := json.Unmarshal(body, &marker); err != nil {
		log.Printf("panel update restart marker decode failed: %v", err)
		return
	}
	backupRoot := filepath.Join(filepath.Dir(binary), ".palpanel-update-backups")
	backupRelative, relativeErr := filepath.Rel(backupRoot, filepath.Clean(marker.BackupPath))
	if filepath.Clean(marker.BinaryPath) != binary || relativeErr != nil || backupRelative == ".." || strings.HasPrefix(backupRelative, ".."+string(os.PathSeparator)) {
		log.Printf("panel update restart marker contains unsafe paths")
		_ = os.Remove(markerPath)
		return
	}
	actualSHA, err := sha256PanelFile(binary)
	if err == nil && len(marker.ExpectedSHA256) == 64 && actualSHA == marker.ExpectedSHA256 {
		_ = os.Remove(markerPath)
		return
	}
	cause := fmt.Errorf("panel update startup verification failed: sha256=%s expected=%s", actualSHA, marker.ExpectedSHA256)
	if err != nil {
		cause = fmt.Errorf("panel update startup verification failed: %w", err)
	}
	if store != nil && marker.JobID != "" {
		_ = store.UpdateJobWithCode(context.Background(), marker.JobID, "failed", 100, "panel update startup verification failed; rolling back", cause.Error(), "panel_startup_verification_failed")
	}
	if restoreErr := restorePanelBackup(binary, marker.BackupPath); restoreErr != nil {
		log.Printf("panel update rollback failed: %v (original verification error: %v)", restoreErr, cause)
		return
	}
	_ = os.Remove(markerPath)
	log.Printf("panel update startup verification failed; restored previous binary and restarting: %v", cause)
	if execErr := syscall.Exec(binary, os.Args, os.Environ()); execErr != nil {
		log.Printf("panel update rollback restart failed: %v", execErr)
	}
}
