//go:build linux

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

var panelUpdateWatchers sync.Map

func replaceCurrentPanelProcess(binary string) error {
	return syscall.Exec(binary, os.Args, os.Environ())
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

func recoverPanelUpdateIfNeeded(cfg appconfig.Config, store *db.Store) {
	consumePanelUpdateResult(cfg, store)
	if _, err := os.Stat(panelupdater.InProgressPath(cfg.DataDir)); err == nil {
		startPanelUpdateResultWatcher(cfg, store)
	}
	recoverLegacyPanelUpdateIfNeeded(store)
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
