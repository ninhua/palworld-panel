package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyHostMigrationEntryIsHardHidden(t *testing.T) {
	root := filepath.Join("..", "..", "..")

	page := readMigrationUIContractFile(t, filepath.Join(root, "frontend", "src", "pages", "SaveSources.tsx"))
	if !strings.Contains(page, "MigrationWizardButton") {
		t.Fatal("SaveSources no longer exposes the upstream player migration wizard")
	}
	if !strings.Contains(page, "data-legacy-host-migration-entry") {
		t.Fatal("legacy host migration entry lost its compatibility selector")
	}

	index := readMigrationUIContractFile(t, filepath.Join(root, "frontend", "src", "styles", "redesign", "index.css"))
	compatPosition := strings.Index(index, `@import "./compat.css";`)
	fixesPosition := strings.Index(index, `@import "./fixes.css";`)
	if compatPosition < 0 || fixesPosition <= compatPosition {
		t.Fatal("redesign fixes.css must be imported after compat.css")
	}

	fixes := readMigrationUIContractFile(t, filepath.Join(root, "frontend", "src", "styles", "redesign", "fixes.css"))
	if !strings.Contains(fixes, "button[data-legacy-host-migration-entry]") ||
		!strings.Contains(fixes, "display: none !important;") {
		t.Fatal("legacy host migration button is not protected by a final hard-hide rule")
	}
}

func readMigrationUIContractFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}
