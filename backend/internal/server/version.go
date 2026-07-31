package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"palpanel/internal/db"
	"palpanel/internal/id"
)

const (
	palworldServerAppID = "2394010"
	// CompatibilityTarget controls the semantic compatibility indicator only.
	// Steam Build IDs independently determine whether an update is available.
	CompatibilityTarget = "1.x"

	kvLatestBuildID      = "server_version_latest_build_id"
	kvVersionLastChecked = "server_version_last_checked_at"
	kvVersionSource      = "server_version_source"
	kvVersionLastError   = "server_version_error"
	versionAlertSource   = "server_version"
	versionCheckJobType  = "version_check"
	smartUpdateJobType   = "smart_update"
)

var appManifestBuildPattern = regexp.MustCompile(`(?i)"buildid"\s+"([^"]+)"`)

type VersionInfo struct {
	Installed             bool     `json:"installed"`
	CurrentBuildID        string   `json:"current_build_id"`
	LatestBuildID         string   `json:"latest_build_id"`
	UpdateAvailable       bool     `json:"update_available"`
	LastCheckedAt         string   `json:"last_checked_at"`
	Source                string   `json:"source"`
	ManifestPath          string   `json:"manifest_path"`
	GameVersion           string   `json:"game_version,omitempty"`
	CompatibilityTarget   string   `json:"compatibility_target,omitempty"`
	Compatible            *bool    `json:"compatible,omitempty"`
	CompatibilityWarnings []string `json:"compatibility_warnings,omitempty"`
	Error                 string   `json:"error,omitempty"`
}

func (m Manager) VersionInfo(ctx context.Context) (VersionInfo, error) {
	info := VersionInfo{
		Installed:           m.isInstalled(ctx),
		ManifestPath:        m.appManifestPath(),
		CompatibilityTarget: CompatibilityTarget,
	}
	if !info.Installed {
		info.Error = "server is not installed"
	}
	if buildID, err := readAppManifestBuildID(info.ManifestPath); err == nil {
		info.CurrentBuildID = buildID
	} else if info.Error == "" {
		info.Error = err.Error()
	}
	var err error
	info.LatestBuildID, _, err = m.store.GetKV(ctx, kvLatestBuildID)
	if err != nil {
		return VersionInfo{}, err
	}
	info.LastCheckedAt, _, err = m.store.GetKV(ctx, kvVersionLastChecked)
	if err != nil {
		return VersionInfo{}, err
	}
	info.Source, _, err = m.store.GetKV(ctx, kvVersionSource)
	if err != nil {
		return VersionInfo{}, err
	}
	lastError, _, err := m.store.GetKV(ctx, kvVersionLastError)
	if err != nil {
		return VersionInfo{}, err
	}
	if info.Error == "" {
		info.Error = lastError
	}
	info.UpdateAvailable = buildIDsDiffer(info.CurrentBuildID, info.LatestBuildID)
	info.CompatibilityWarnings = m.compatibilityWarnings(ctx)
	return info, nil
}

// WithGameVersion adds the semantic version reported by the running official
// /info endpoint. Build IDs remain authoritative while the server is offline.
func (m Manager) WithGameVersion(ctx context.Context, info VersionInfo, gameVersion string) VersionInfo {
	gameVersion = strings.TrimSpace(gameVersion)
	if gameVersion == "" {
		return info
	}
	info.GameVersion = gameVersion
	compatible := semanticVersionMatches(gameVersion, CompatibilityTarget)
	info.Compatible = &compatible
	if !compatible {
		info.CompatibilityWarnings = appendUniqueString(
			info.CompatibilityWarnings,
			fmt.Sprintf("运行中的服务端版本 %s 与面板兼容目标 %s 不一致", gameVersion, CompatibilityTarget),
		)
	}
	return info
}

func (m Manager) compatibilityWarnings(ctx context.Context) []string {
	warnings := []string{}
	if installedMods, err := m.store.ListMods(ctx); err == nil {
		enabledWorkshop := 0
		for _, mod := range installedMods {
			if mod.Enabled && (mod.Source == "workshop" || strings.TrimSpace(mod.WorkshopID) != "") {
				enabledWorkshop++
			}
		}
		if enabledWorkshop > 0 {
			warnings = append(warnings, fmt.Sprintf("已启用 %d 个 Workshop Mod；Build 变化后请确认作者已适配", enabledWorkshop))
		}
	}
	if fileExists(filepath.Join(m.cfg.Win64Dir(), "PalDefender.dll")) || dirExists(m.cfg.PalDefenderDir()) {
		warnings = append(warnings, "已安装 PalDefender；Build 变化后请确认其版本兼容")
	}
	if hasLevelSave(filepath.Join(m.cfg.ServerDirectory(), "Pal", "Saved", "SaveGames")) {
		warnings = append(warnings, "存档解析器兼容范围为 1.x；更新后请重建索引确认存档结构")
	}
	return warnings
}

func semanticVersionMatches(version, target string) bool {
	normalize := func(value string) []string {
		value = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "v"))
		if index := strings.IndexAny(value, "+-"); index >= 0 {
			value = value[:index]
		}
		return strings.Split(value, ".")
	}
	actualParts := normalize(version)
	targetParts := normalize(target)
	if len(actualParts) < 3 || len(targetParts) == 0 || len(actualParts) < len(targetParts) {
		return false
	}
	for index, targetPart := range targetParts {
		if targetPart == "x" || targetPart == "*" {
			return index == len(targetParts)-1
		}
		if actualParts[index] != targetPart {
			return false
		}
	}
	return len(targetParts) >= 3
}

func hasLevelSave(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() && strings.EqualFold(entry.Name(), "Level.sav") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func appendUniqueString(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func (m Manager) CheckVersion(ctx context.Context) (db.Job, error) {
	return m.startJob(ctx, versionCheckJobType, "queued version check", func(jobCtx context.Context, jobID string) {
		m.update(jobID, "running", 10, "checking local and latest build ids", "")
		info, err := m.refreshVersion(jobCtx, true)
		if err != nil {
			m.update(jobID, "failed", 20, "version check failed", err.Error())
			return
		}
		if info.UpdateAvailable {
			m.update(jobID, "completed", 100, fmt.Sprintf("update available: %s -> %s", info.CurrentBuildID, info.LatestBuildID), "")
			return
		}
		m.update(jobID, "completed", 100, "server is already on the latest build", "")
	})
}

func (m Manager) UpdateIfNeeded(ctx context.Context, preUpdate func(context.Context) error) (db.Job, error) {
	return m.startLifecycleJob(ctx, smartUpdateJobType, "queued smart update", func(jobCtx context.Context, jobID string) {
		m.update(jobID, "running", 5, "checking whether update is needed", "")
		info, err := m.refreshVersion(jobCtx, false)
		if err != nil {
			m.update(jobID, "failed", 10, "version check failed", err.Error())
			return
		}
		if !info.UpdateAvailable {
			m.update(jobID, "completed", 100, "server is already on the latest build", "")
			return
		}
		m.update(jobID, "running", 20, fmt.Sprintf("update available: %s -> %s", info.CurrentBuildID, info.LatestBuildID), "")
		if !m.runInstallOrUpdateJob(jobCtx, jobID, true, true, preUpdate) {
			return
		}
		refreshed, err := m.VersionInfo(jobCtx)
		if err != nil {
			m.update(jobID, "completed", 100, "smart update completed; version read failed: "+err.Error(), "")
			return
		}
		m.update(jobID, "completed", 100, "smart update completed; current build "+refreshed.CurrentBuildID, "")
	})
}

func (m Manager) refreshVersion(ctx context.Context, createAlert bool) (VersionInfo, error) {
	info, err := m.VersionInfo(ctx)
	if err != nil {
		return VersionInfo{}, err
	}
	if !info.Installed {
		_ = m.store.SetKV(ctx, kvVersionLastError, info.Error)
		return info, errors.New(info.Error)
	}
	if info.CurrentBuildID == "" {
		if info.Error == "" {
			info.Error = "local build id is unavailable"
		}
		_ = m.store.SetKV(ctx, kvVersionLastError, info.Error)
		return info, errors.New(info.Error)
	}
	previousLatest := info.LatestBuildID
	latest, source, err := m.remoteBuildID(ctx)
	if err != nil {
		info.Error = err.Error()
		_ = m.store.SetKV(ctx, kvVersionLastError, info.Error)
		return info, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := m.store.SetKV(ctx, kvLatestBuildID, latest); err != nil {
		return VersionInfo{}, err
	}
	if err := m.store.SetKV(ctx, kvVersionLastChecked, now); err != nil {
		return VersionInfo{}, err
	}
	if err := m.store.SetKV(ctx, kvVersionSource, source); err != nil {
		return VersionInfo{}, err
	}
	if err := m.store.SetKV(ctx, kvVersionLastError, ""); err != nil {
		return VersionInfo{}, err
	}
	info.LatestBuildID = latest
	info.LastCheckedAt = now
	info.Source = source
	info.Error = ""
	info.UpdateAvailable = buildIDsDiffer(info.CurrentBuildID, info.LatestBuildID)
	if createAlert && info.UpdateAvailable && previousLatest != latest {
		_ = m.createVersionAlert(ctx, info)
	}
	return info, nil
}

func (m Manager) remoteBuildID(ctx context.Context) (string, string, error) {
	if m.remoteBuildIDFunc != nil {
		return m.remoteBuildIDFunc(ctx)
	}
	mode, err := m.RuntimeMode(ctx)
	if err != nil {
		return "", "", err
	}
	var raw string
	source := "steamcmd_windows"
	if mode == RuntimeWineDocker {
		source = "steamcmd_wine_runner"
		raw, err = m.runner.AppInfo(ctx)
	} else {
		raw, err = m.windowsSteamAppInfo(ctx)
	}
	if err != nil {
		return "", source, err
	}
	buildID, err := parseSteamAppInfoPublicBuildID(raw)
	if err != nil {
		return "", source, err
	}
	return buildID, source, nil
}

func (m Manager) windowsSteamAppInfo(ctx context.Context) (string, error) {
	return m.nativeSteamCMD().AppInfo(ctx, palworldServerAppID)
}

func (m Manager) createVersionAlert(ctx context.Context, info VersionInfo) error {
	return m.store.CreateAlert(ctx, db.Alert{
		ID:       id.New("alert"),
		Severity: "warning",
		Title:    "发现 Palworld 服务端新版本",
		Message:  fmt.Sprintf("当前 Build %s，最新 Build %s。请确认备份后执行检查后更新。", info.CurrentBuildID, info.LatestBuildID),
		Source:   versionAlertSource,
		Status:   "open",
	})
}

func (m Manager) isInstalled(ctx context.Context) bool {
	if value, ok, err := m.store.GetKV(ctx, kvInstalled); err == nil && ok && value == "true" {
		return true
	}
	return fileExists(m.cfg.PalServerExePath())
}

func (m Manager) appManifestPath() string {
	return appManifestPathForRoot(m.cfg.ServerDirectory())
}

func appManifestPathForRoot(serverRoot string) string {
	candidates := appManifestCandidates(serverRoot)
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate
		}
	}
	return candidates[0]
}

func appManifestCandidates(serverRoot string) []string {
	name := "appmanifest_" + palworldServerAppID + ".acf"
	candidates := []string{filepath.Join(serverRoot, "steamapps", name)}
	parent := filepath.Dir(serverRoot)
	if strings.EqualFold(filepath.Base(parent), "common") {
		candidates = append(candidates, filepath.Join(filepath.Dir(parent), name))
	}
	candidates = append(candidates, filepath.Join(parent, name))

	seen := make(map[string]bool, len(candidates))
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		key := strings.ToLower(filepath.Clean(candidate))
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, filepath.Clean(candidate))
	}
	return result
}

func readAppManifestBuildID(path string) (string, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("local Steam appmanifest not found at %s", path)
	}
	if err != nil {
		return "", err
	}
	matches := appManifestBuildPattern.FindSubmatch(body)
	if len(matches) < 2 || strings.TrimSpace(string(matches[1])) == "" {
		return "", fmt.Errorf("local Steam appmanifest does not contain buildid")
	}
	return strings.TrimSpace(string(matches[1])), nil
}

func parseSteamAppInfoPublicBuildID(raw string) (string, error) {
	tokens := vdfTokens(raw)
	branchesStart, branchesEnd, ok := findVDFBlock(tokens, "branches", 0, len(tokens))
	if !ok {
		return "", fmt.Errorf("steam app info does not contain branches")
	}
	publicStart, publicEnd, ok := findVDFBlock(tokens, "public", branchesStart, branchesEnd)
	if !ok {
		return "", fmt.Errorf("steam app info does not contain public branch")
	}
	for i := publicStart; i+1 < publicEnd; i++ {
		if strings.EqualFold(tokens[i], "buildid") {
			buildID := strings.TrimSpace(tokens[i+1])
			if buildID == "" || buildID == "{" || buildID == "}" {
				return "", fmt.Errorf("steam app info public buildid is empty")
			}
			return buildID, nil
		}
	}
	return "", fmt.Errorf("steam app info public branch does not contain buildid")
}

func findVDFBlock(tokens []string, key string, start, end int) (int, int, bool) {
	for i := start; i+1 < end; i++ {
		if !strings.EqualFold(tokens[i], key) || tokens[i+1] != "{" {
			continue
		}
		depth := 1
		for j := i + 2; j < end; j++ {
			switch tokens[j] {
			case "{":
				depth++
			case "}":
				depth--
				if depth == 0 {
					return i + 2, j, true
				}
			}
		}
	}
	return 0, 0, false
}

func vdfTokens(raw string) []string {
	var tokens []string
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '{', '}':
			tokens = append(tokens, string(raw[i]))
		case '"':
			start := i
			i++
			escaped := false
			for ; i < len(raw); i++ {
				if escaped {
					escaped = false
					continue
				}
				if raw[i] == '\\' {
					escaped = true
					continue
				}
				if raw[i] == '"' {
					break
				}
			}
			if i < len(raw) {
				token, err := strconv.Unquote(raw[start : i+1])
				if err == nil {
					tokens = append(tokens, token)
				}
			}
		}
	}
	return tokens
}

func buildIDsDiffer(current, latest string) bool {
	return strings.TrimSpace(current) != "" &&
		strings.TrimSpace(latest) != "" &&
		strings.TrimSpace(current) != strings.TrimSpace(latest)
}
