//go:build !linux

package server

import (
	"fmt"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
)

func replaceCurrentPanelProcess(string) error {
	return fmt.Errorf("panel self-update requires Linux")
}

func resolvePanelUpdateMode(appconfig.Config) (string, error) {
	return "", fmt.Errorf("panel self-update requires Linux")
}

func externalPanelUpdaterReady(appconfig.Config) error {
	return fmt.Errorf("external panel updater requires Linux")
}

func recoverPanelUpdateIfNeeded(appconfig.Config, *db.Store)    {}
func startPanelUpdateResultWatcher(appconfig.Config, *db.Store) {}
func StartPanelUpdateStartupVerification(appconfig.Config, *db.Store) {
}
func RollbackPanelUpdateOnStartupFailure(error) error { return nil }
