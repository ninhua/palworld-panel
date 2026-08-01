package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
)

func TestCreateHostMigrationPreSwitchBackupCopiesAndRegistersCurrentWorld(t *testing.T) {
	server, store, worldPath := newHostMigrationSafetyTestServer(t, "旧服务器世界")
	if err := os.WriteFile(filepath.Join(worldPath, "Level.sav"), []byte("old-level"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(worldPath, "Players"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldPath, "Players", "old.sav"), []byte("old-player"), 0o640); err != nil {
		t.Fatal(err)
	}

	backup, created, err := createHostMigrationPreSwitchBackup(server, context.Background(), "stopped", "ORIGINALWORLD")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected a pre-switch backup")
	}
	if backup.Name != "旧服务器世界（主机迁移前备份）" {
		t.Fatalf("backup name = %q", backup.Name)
	}
	if backup.Active || backup.Kind != "import" || !pathWithin(server.cfg.SaveSourcesDir, backup.Path) {
		t.Fatalf("backup source = %#v", backup)
	}
	for _, relative := range []string{"Level.sav", filepath.Join("Players", "old.sav")} {
		body, readErr := os.ReadFile(filepath.Join(backup.Path, relative))
		if readErr != nil || !strings.HasPrefix(string(body), "old-") {
			t.Fatalf("backup %s = %q, %v", relative, body, readErr)
		}
	}
	stored, err := store.GetSaveSource(context.Background(), backup.ID)
	if err != nil || stored.Name != backup.Name || stored.Path != backup.Path {
		t.Fatalf("stored backup = %#v, %v", stored, err)
	}
}

func TestCreateHostMigrationPreSwitchBackupFailsClosedOnCopyError(t *testing.T) {
	server, store, worldPath := newHostMigrationSafetyTestServer(t, "需要保护的世界")
	if err := os.WriteFile(filepath.Join(worldPath, "Level.sav"), []byte("old-level"), 0o640); err != nil {
		t.Fatal(err)
	}

	previousCopy := hostMigrationBackupCopyTree
	hostMigrationBackupCopyTree = func(string, string) error { return errors.New("copy failed") }
	t.Cleanup(func() { hostMigrationBackupCopyTree = previousCopy })

	if _, created, err := createHostMigrationPreSwitchBackup(server, context.Background(), "stopped", "ORIGINALWORLD"); err == nil || created {
		t.Fatalf("created=%t err=%v", created, err)
	}
	items, err := store.ListSaveSources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "server" {
		t.Fatalf("failed backup registered a source: %#v", items)
	}
	entries, err := os.ReadDir(server.cfg.SaveSourcesDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed backup left files behind: %#v", entries)
	}
}

func TestReconcileRuntimeSaveSourceNamePersistsMigratedName(t *testing.T) {
	root := t.TempDir()
	store, err := db.Open(filepath.Join(root, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	serverSource := db.SaveSource{ID: "server", Name: "当前服务器存档", Kind: "server", Active: true}
	migratedSource := db.SaveSource{ID: "save-migrated", Name: "观瀑局迁移世界", Kind: "import", Path: filepath.Join(root, "save-migrated")}
	if err := store.UpsertSaveSource(context.Background(), serverSource); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSaveSource(context.Background(), migratedSource); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateSaveSource(context.Background(), "server"); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListSaveSources(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	name := reconcileRuntimeSaveSourceName(context.Background(), store, items, hostMigrationWorldName(migratedSource.ID))
	if name != migratedSource.Name {
		t.Fatalf("runtime name = %q", name)
	}
	stored, err := store.GetSaveSource(context.Background(), "server")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != migratedSource.Name || !stored.Active {
		t.Fatalf("server source after reconciliation = %#v", stored)
	}
	for _, item := range items {
		if item.ID == "server" && item.Name != migratedSource.Name {
			t.Fatalf("response item was not reconciled: %#v", item)
		}
	}
}

func newHostMigrationSafetyTestServer(t *testing.T, serverSourceName string) (Server, *db.Store, string) {
	t.Helper()
	root := t.TempDir()
	serverDir := filepath.Join(root, "server")
	saveSourcesDir := filepath.Join(root, "save-sources")
	settingsPath := filepath.Join(serverDir, "Pal", "Saved", "Config", "WindowsServer", "GameUserSettings.ini")
	worldPath := filepath.Join(serverDir, "Pal", "Saved", "SaveGames", "0", "ORIGINALWORLD")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worldPath, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(saveSourcesDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte("DedicatedServerName=ORIGINALWORLD\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(filepath.Join(root, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertSaveSource(context.Background(), db.SaveSource{
		ID: "server", Name: serverSourceName, Kind: "server", Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateSaveSource(context.Background(), "server"); err != nil {
		t.Fatal(err)
	}

	cfg := appconfig.Config{
		ServerDir:      serverDir,
		SaveSourcesDir: saveSourcesDir,
		DBPath:         filepath.Join(root, "panel.db"),
	}.WithServerDirectoryState()
	cfg.SaveSourcesDir = saveSourcesDir
	cfg.DBPath = filepath.Join(root, "panel.db")
	return Server{cfg: cfg, store: store}, store, worldPath
}
