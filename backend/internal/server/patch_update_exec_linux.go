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

func recoverPatchUpdateIfNeeded(store *db.Store) {
	binary, err := executablePath()
	if err != nil {
		return
	}
	markerPath := patchRestartMarkerPath(binary)
	body, err := os.ReadFile(markerPath)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		log.Printf("panel patch restart marker read failed: %v", err)
		return
	}
	var marker patchRestartMarker
	if err := json.Unmarshal(body, &marker); err != nil {
		log.Printf("panel patch restart marker decode failed: %v", err)
		return
	}
	backupRoot := filepath.Join(filepath.Dir(binary), ".palpanel-patch-backups")
	backupRelative, relativeErr := filepath.Rel(backupRoot, filepath.Clean(marker.BackupPath))
	if filepath.Clean(marker.BinaryPath) != binary || relativeErr != nil || backupRelative == ".." || strings.HasPrefix(backupRelative, ".."+string(os.PathSeparator)) {
		log.Printf("panel patch restart marker contains unsafe paths")
		_ = os.Remove(markerPath)
		return
	}
	actualSHA, err := sha256PatchFile(binary)
	if err == nil && validPatchSHA256(marker.ExpectedSHA256) && actualSHA == marker.ExpectedSHA256 {
		_ = os.Remove(markerPath)
		return
	}
	cause := fmt.Errorf("panel patch startup verification failed: sha256=%s expected=%s", actualSHA, marker.ExpectedSHA256)
	if err != nil {
		cause = fmt.Errorf("panel patch startup verification failed: %w", err)
	}
	if store != nil && marker.JobID != "" {
		_ = store.UpdateJobWithCode(context.Background(), marker.JobID, "failed", 100, "panel patch startup verification failed; rolling back", cause.Error(), "patch_startup_verification_failed")
	}
	if restoreErr := restorePatchBackup(binary, marker.BackupPath); restoreErr != nil {
		log.Printf("panel patch rollback failed: %v (original verification error: %v)", restoreErr, cause)
		return
	}
	_ = os.Remove(markerPath)
	log.Printf("panel patch startup verification failed; restored previous binary and restarting: %v", cause)
	if execErr := syscall.Exec(binary, os.Args, os.Environ()); execErr != nil {
		log.Printf("panel patch rollback restart failed: %v", execErr)
	}
}
