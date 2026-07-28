package playerpresence

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const scopedStoragePrefix = "player_presence:v2:"

type Scope struct {
	ID                   string `json:"id"`
	WorldID              string `json:"world_id"`
	WorldPath            string `json:"world_path,omitempty"`
	AllowLegacyMigration bool   `json:"-"`
}

func (scope Scope) StorageKey() string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(scope.ID)))
	return fmt.Sprintf("%s%x", scopedStoragePrefix, digest[:16])
}

func ResolveServerScope(serverDir string) (Scope, error) {
	serverDir = filepath.Clean(serverDir)
	root := filepath.Join(serverDir, "Pal", "Saved", "SaveGames", "0")
	entries, err := os.ReadDir(root)
	if err != nil {
		return Scope{}, fmt.Errorf("locate Palworld world saves: %w", err)
	}
	type candidate struct {
		id      string
		path    string
		modTime int64
	}
	candidates := make([]candidate, 0, len(entries))
	byID := make(map[string]candidate, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		worldPath := filepath.Join(root, entry.Name())
		info, statErr := os.Stat(filepath.Join(worldPath, "Level.sav"))
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		item := candidate{id: entry.Name(), path: worldPath, modTime: info.ModTime().UnixNano()}
		candidates = append(candidates, item)
		byID[strings.ToLower(item.id)] = item
	}
	if len(candidates) == 0 {
		return Scope{}, errors.New("no Palworld world containing Level.sav was found")
	}

	selected := candidate{}
	if configured := dedicatedServerName(serverDir); configured != "" {
		selected = byID[strings.ToLower(configured)]
	}
	if selected.id == "" {
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].modTime != candidates[j].modTime {
				return candidates[i].modTime > candidates[j].modTime
			}
			return candidates[i].id < candidates[j].id
		})
		selected = candidates[0]
	}
	return Scope{
		ID:                   "server-world:" + selected.id,
		WorldID:              selected.id,
		WorldPath:            selected.path,
		AllowLegacyMigration: len(candidates) == 1,
	}, nil
}

func dedicatedServerName(serverDir string) string {
	path := filepath.Join(serverDir, "Pal", "Saved", "Config", "WindowsServer", "GameUserSettings.ini")
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		key, value, _ := strings.Cut(line, "=")
		if strings.EqualFold(strings.TrimSpace(key), "DedicatedServerName") {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}
