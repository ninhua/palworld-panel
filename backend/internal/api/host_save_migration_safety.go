package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"palpanel/internal/db"
	internalid "palpanel/internal/id"
)

var (
	hostMigrationBackupCopyTree = copyHostMigrationTree
	hostMigrationBackupPending  sync.Map
	hostMigrationBackupMu       sync.Mutex
)

func init() {
	installHostMigrationSafetyHooks()
	patchFeatures = append(patchFeatures,
		"host-save-migration-pre-switch-backup",
		"host-save-migration-server-name-sync",
	)
}

func installHostMigrationSafetyHooks() {
	readState := hostMigrationServerState
	stopServer := hostMigrationStopServer
	startServer := hostMigrationStartServer

	hostMigrationServerState = func(s Server, ctx context.Context) (string, error) {
		state, err := readState(s, ctx)
		if err != nil {
			return state, err
		}
		state = strings.ToLower(strings.TrimSpace(state))
		key := hostMigrationBackupKey(s)
		switch state {
		case "running":
			worldID, readErr := readHostMigrationCurrentWorldID(s)
			if readErr != nil {
				return "", fmt.Errorf("read current world before migration backup: %w", readErr)
			}
			if _, loaded := hostMigrationBackupPending.LoadOrStore(key, worldID); loaded {
				return "", errors.New("another host migration is already preparing a pre-switch backup")
			}
		case "", "stopped", "exited", "created", "not_found", "not-found", "dead":
			if _, _, backupErr := createHostMigrationPreSwitchBackup(s, ctx, state, ""); backupErr != nil {
				return "", backupErr
			}
		}
		return state, nil
	}

	hostMigrationStopServer = func(s Server, ctx context.Context) error {
		key := hostMigrationBackupKey(s)
		pendingWorld, backupRequired := hostMigrationBackupPending.LoadAndDelete(key)
		if err := stopServer(s, ctx); err != nil {
			return err
		}
		if !backupRequired {
			return nil
		}
		expectedWorld, _ := pendingWorld.(string)
		if _, _, err := createHostMigrationPreSwitchBackup(s, ctx, "stopped", expectedWorld); err != nil {
			restartCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if restartErr := startServer(s, restartCtx); restartErr != nil {
				return fmt.Errorf("create pre-switch save backup: %v; restart original server also failed: %w", err, restartErr)
			}
			return fmt.Errorf("create pre-switch save backup: %w", err)
		}
		return nil
	}
}

func hostMigrationBackupKey(s Server) string {
	if value := strings.TrimSpace(s.cfg.DBPath); value != "" {
		return value
	}
	return filepath.Clean(s.cfg.ServerDirectory())
}

func createHostMigrationPreSwitchBackup(s Server, ctx context.Context, state, expectedWorldID string) (db.SaveSource, bool, error) {
	if s.store == nil {
		return db.SaveSource{}, false, errors.New("save source database is unavailable for pre-switch backup")
	}
	worldID, err := readHostMigrationCurrentWorldID(s)
	if err != nil {
		if hostMigrationStateMayHaveNoWorld(state) && errors.Is(err, os.ErrNotExist) {
			return db.SaveSource{}, false, nil
		}
		return db.SaveSource{}, false, fmt.Errorf("read current server world: %w", err)
	}
	if expectedWorldID != "" && !strings.EqualFold(strings.TrimSpace(expectedWorldID), worldID) {
		return db.SaveSource{}, false, fmt.Errorf("current server world changed from %s to %s before backup", expectedWorldID, worldID)
	}

	serverDir := s.cfg.ServerDirectory()
	saveRoot := filepath.Join(serverDir, "Pal", "Saved", "SaveGames", "0")
	worldPath := filepath.Join(saveRoot, worldID)
	if !pathWithin(saveRoot, worldPath) {
		return db.SaveSource{}, false, errors.New("current server world is outside the managed save root")
	}
	levelPath := filepath.Join(worldPath, "Level.sav")
	levelInfo, err := os.Stat(levelPath)
	if err != nil {
		if hostMigrationStateMayHaveNoWorld(state) && errors.Is(err, os.ErrNotExist) {
			return db.SaveSource{}, false, nil
		}
		return db.SaveSource{}, false, fmt.Errorf("current server world cannot be backed up: %w", err)
	}
	if !levelInfo.Mode().IsRegular() {
		return db.SaveSource{}, false, errors.New("current server Level.sav is not a regular file")
	}

	hostMigrationBackupMu.Lock()
	defer hostMigrationBackupMu.Unlock()

	if strings.TrimSpace(s.cfg.SaveSourcesDir) == "" {
		return db.SaveSource{}, false, errors.New("save source directory is unavailable for migration backup")
	}
	if err := os.MkdirAll(s.cfg.SaveSourcesDir, 0o750); err != nil {
		return db.SaveSource{}, false, fmt.Errorf("create save source directory for migration backup: %w", err)
	}
	backupID := internalid.New("save")
	backupPath := filepath.Join(s.cfg.SaveSourcesDir, backupID)
	if !pathWithin(s.cfg.SaveSourcesDir, backupPath) {
		return db.SaveSource{}, false, errors.New("migration backup path is outside the managed save source directory")
	}
	temporaryPath := backupPath + ".palpanel-host-migration-backup"
	_ = os.RemoveAll(temporaryPath)
	if err := hostMigrationBackupCopyTree(worldPath, temporaryPath); err != nil {
		_ = os.RemoveAll(temporaryPath)
		return db.SaveSource{}, false, fmt.Errorf("copy current server world into migration backup: %w", err)
	}
	if info, err := os.Stat(filepath.Join(temporaryPath, "Level.sav")); err != nil || !info.Mode().IsRegular() {
		_ = os.RemoveAll(temporaryPath)
		if err == nil {
			err = errors.New("Level.sav is not a regular file")
		}
		return db.SaveSource{}, false, fmt.Errorf("verify migration backup: %w", err)
	}
	if err := os.Rename(temporaryPath, backupPath); err != nil {
		_ = os.RemoveAll(temporaryPath)
		return db.SaveSource{}, false, fmt.Errorf("publish migration backup: %w", err)
	}

	source := db.SaveSource{
		ID:     backupID,
		Name:   hostMigrationBackupName(resolveRuntimeSaveSourceName(ctx, s.store, nil, worldID), worldID),
		Kind:   "import",
		Path:   backupPath,
		Active: false,
	}
	if err := s.store.UpsertSaveSource(ctx, source); err != nil {
		_ = os.RemoveAll(backupPath)
		return db.SaveSource{}, false, fmt.Errorf("register migration backup: %w", err)
	}
	return source, true, nil
}

func readHostMigrationCurrentWorldID(s Server) (string, error) {
	settingsPath := filepath.Join(s.cfg.ServerDirectory(), "Pal", "Saved", "Config", "WindowsServer", "GameUserSettings.ini")
	if !pathWithin(s.cfg.ServerDirectory(), settingsPath) {
		return "", errors.New("GameUserSettings.ini is outside the managed server directory")
	}
	payload, err := os.ReadFile(settingsPath)
	if err != nil {
		return "", err
	}
	_, worldID, err := replaceDedicatedServerName(payload, strings.Repeat("0", 32))
	if err != nil {
		return "", err
	}
	worldID = strings.TrimSpace(worldID)
	if worldID == "" || worldID == "." || worldID == ".." || filepath.Base(worldID) != worldID || strings.ContainsAny(worldID, `/\`) {
		return "", errors.New("DedicatedServerName is not a safe world directory name")
	}
	return worldID, nil
}

func hostMigrationStateMayHaveNoWorld(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "created", "not_found", "not-found":
		return true
	default:
		return false
	}
}

func hostMigrationBackupName(sourceName, worldID string) string {
	sourceName = strings.TrimSpace(sourceName)
	if sourceName == "" || sourceName == "当前服务器存档" {
		sourceName = strings.TrimSpace(worldID)
	}
	if sourceName == "" {
		sourceName = "服务器存档"
	}
	const suffix = "（主机迁移前备份）"
	limit := 80 - len([]rune(suffix))
	runes := []rune(sourceName)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes) + suffix
}

func reconcileRuntimeSaveSourceName(ctx context.Context, store *db.Store, items []db.SaveSource, worldID string) string {
	name := resolveRuntimeSaveSourceName(ctx, store, items, worldID)
	if name == "" {
		name = "当前服务器存档"
	}
	for index := range items {
		if items[index].ID != "server" {
			continue
		}
		if items[index].Name == name {
			return name
		}
		items[index].Name = name
		if store != nil {
			_ = store.UpsertSaveSource(ctx, items[index])
		}
		return name
	}
	return name
}

func resolveRuntimeSaveSourceName(ctx context.Context, store *db.Store, items []db.SaveSource, worldID string) string {
	serverName := "当前服务器存档"
	if len(items) == 0 && store != nil {
		stored, err := store.ListSaveSources(ctx)
		if err == nil {
			items = stored
		}
	}
	for _, item := range items {
		if item.ID == "server" && strings.TrimSpace(item.Name) != "" {
			serverName = strings.TrimSpace(item.Name)
			break
		}
	}
	worldID = strings.TrimSpace(worldID)
	if worldID == "" {
		return serverName
	}
	for _, item := range items {
		if item.Kind != "import" || strings.TrimSpace(item.Name) == "" {
			continue
		}
		if strings.EqualFold(hostMigrationWorldName(item.ID), worldID) {
			return strings.TrimSpace(item.Name)
		}
	}
	return serverName
}
