package api

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func publishLevelFile(source, destination string) (string, error) {
	if filepath.Base(destination) != "Level.sav" || filepath.Dir(source) == filepath.Dir(destination) {
		return "", fmt.Errorf("invalid Level.sav publication paths")
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".Level.sav.palpanel-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	sourceFile, err := os.Open(source)
	if err != nil {
		temporary.Close()
		return "", err
	}
	_, copyErr := io.Copy(temporary, sourceFile)
	closeSourceErr := sourceFile.Close()
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	for _, candidate := range []error{copyErr, closeSourceErr, syncErr, closeErr} {
		if candidate != nil {
			return "", candidate
		}
	}
	if err := atomicReplaceLevelFile(temporaryPath, destination); err != nil {
		return "", err
	}
	return hostMigrationFileSHA256(destination)
}

func rollbackLevelFile(active, backup, expectedActiveHash string) error {
	actual, err := hostMigrationFileSHA256(active)
	if err != nil {
		return err
	}
	if actual != expectedActiveHash {
		return fmt.Errorf("active Level.sav changed after publication; refusing rollback")
	}
	_, err = publishLevelFile(backup, active)
	return err
}
