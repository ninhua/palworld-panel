//go:build !linux

package server

import (
	"fmt"

	"palpanel/internal/db"
)

func replaceCurrentPanelProcess(string) error {
	return fmt.Errorf("panel self-update requires Linux")
}

func recoverPanelUpdateIfNeeded(*db.Store) {}
