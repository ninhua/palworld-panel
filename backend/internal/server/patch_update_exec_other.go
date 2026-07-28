//go:build !linux

package server

import (
	"fmt"

	"palpanel/internal/db"
)

func replaceCurrentPanelProcess(string) error {
	return fmt.Errorf("panel patch hot update requires Linux")
}

func recoverPatchUpdateIfNeeded(*db.Store) {}
