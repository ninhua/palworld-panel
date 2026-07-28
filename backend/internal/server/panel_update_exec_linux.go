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
	"syscall"

	"palpanel/internal/db"
)

func replaceCurrentPanelProcess(binary string) error {
	return syscall.Exec(binary, os.Args, os.Environ())
}

func recoverPanelUpdateIfNeeded(store *db.Store) {
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
