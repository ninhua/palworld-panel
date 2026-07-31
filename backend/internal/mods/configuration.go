package mods

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	"github.com/pelletier/go-toml/v2"

	"palpanel/internal/id"
)

const MaxConfigFileBytes int64 = 1 << 20

var editableConfigExtensions = map[string]bool{
	".json": true, ".ini": true, ".cfg": true, ".toml": true,
	".yaml": true, ".yml": true, ".txt": true, ".lua": true,
}

var forbiddenConfigNames = map[string]bool{
	"info.json": true, "installmanifest.json": true, "palmodsettings.ini": true,
}

var configPathLocks sync.Map

type ConfigurationError struct {
	Code string
	Err  error
}

func (e ConfigurationError) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Err.Error()
}

func (e ConfigurationError) Unwrap() error { return e.Err }

type ConfigurationAdapter struct {
	ID             string                    `json:"id"`
	Name           string                    `json:"name"`
	Description    string                    `json:"description"`
	WorkshopID     string                    `json:"workshop_id,omitempty"`
	Available      bool                      `json:"available"`
	Installed      bool                      `json:"installed"`
	Configured     bool                      `json:"configured"`
	Enabled        bool                      `json:"enabled"`
	Status         ConfigurationStatus       `json:"status"`
	StatusDetail   string                    `json:"status_detail,omitempty"`
	ReloadBehavior string                    `json:"reload_behavior"`
	Dependencies   []ConfigurationDependency `json:"dependencies"`
	Actions        []ConfigurationAction     `json:"actions"`
	ReferenceURLs  map[string]string         `json:"reference_urls,omitempty"`
	Files          []ConfigFile              `json:"files"`
}

type ConfigurationStatus string

const (
	ConfigurationStatusNotInstalled      ConfigurationStatus = "not_installed"
	ConfigurationStatusDependencyMissing ConfigurationStatus = "dependency_missing"
	ConfigurationStatusNotConfigured     ConfigurationStatus = "not_configured"
	ConfigurationStatusDisabled          ConfigurationStatus = "disabled"
	ConfigurationStatusRestartRequired   ConfigurationStatus = "restart_required"
	ConfigurationStatusReady             ConfigurationStatus = "ready"
)

const (
	ConfigurationActionInitialize = "initialize"
	ConfigurationActionEnable     = "enable"
)

type ConfigurationDependency struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	WorkshopID string `json:"workshop_id,omitempty"`
	ModID      string `json:"mod_id,omitempty"`
	Installed  bool   `json:"installed"`
	Enabled    bool   `json:"enabled"`
	Required   bool   `json:"required"`
}

type ConfigurationAction struct {
	ID              string `json:"id"`
	Available       bool   `json:"available"`
	RestartRequired bool   `json:"restart_required"`
}

type ConfigurationActionRequest struct {
	Action string `json:"action"`
}

type ConfigurationActionResult struct {
	Adapter         ConfigurationAdapter `json:"adapter"`
	Document        *ConfigDocument      `json:"document,omitempty"`
	Changed         []string             `json:"changed"`
	RestartRequired bool                 `json:"restart_required"`
}

type ConfigFile struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Extension  string    `json:"extension"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
	Revision   string    `json:"revision"`
	Executable bool      `json:"executable"`
	Risk       string    `json:"risk,omitempty"`
}

type ConfigurationField struct {
	Path        string                     `json:"path"`
	Label       string                     `json:"label"`
	Description string                     `json:"description,omitempty"`
	Group       string                     `json:"group,omitempty"`
	Type        string                     `json:"type"`
	Value       any                        `json:"value"`
	Options     []ConfigurationFieldOption `json:"options,omitempty"`
	Unit        string                     `json:"unit,omitempty"`
	Min         *int64                     `json:"min,omitempty"`
	Max         *int64                     `json:"max,omitempty"`
}

type ConfigurationFieldOption struct {
	Value any    `json:"value"`
	Label string `json:"label"`
}

type ConfigDocument struct {
	File    ConfigFile           `json:"file"`
	Content string               `json:"content"`
	Format  string               `json:"format"`
	Fields  []ConfigurationField `json:"fields,omitempty"`
}

type ConfigWriteRequest struct {
	Content           string `json:"content"`
	Revision          string `json:"revision"`
	ConfirmExecutable bool   `json:"confirm_executable,omitempty"`
}

type ConfigBackup struct {
	ID        string    `json:"id"`
	Revision  string    `json:"revision"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

type configTarget struct {
	scope    string
	root     string
	path     string
	relative string
}

type adapterDefinition struct {
	ID, Name, Description, WorkshopID, ReloadBehavior string
	resolve                                           func(Manager, context.Context) ([]configTarget, error)
}

func adapterRegistry() []adapterDefinition {
	return []adapterDefinition{
		{ID: "paldefender", Name: "PalDefender", Description: "安全、防作弊与服务器管理配置", ReloadBehavior: "online_reload", resolve: resolvePalDefenderAdapter},
		{ID: "ue4ss", Name: "UE4SS Experimental", Description: "UE4SS 运行参数和模块启停列表", WorkshopID: "3625223587", ReloadBehavior: "restart_required", resolve: resolveUE4SSAdapter},
		{ID: "palschema", Name: "PalSchema", Description: "PalSchema 运行设置与模块 JSON 配置", WorkshopID: "3625280368", ReloadBehavior: "restart_required", resolve: resolvePalSchemaAdapter},
		{ID: "extended-base-range", Name: "Extended Base Range", Description: "基地范围 Lua 数值参数", WorkshopID: "3625907101", ReloadBehavior: "restart_required", resolve: resolveExtendedBaseRangeAdapter},
		{ID: "quality-of-life", Name: "QualityOfLife", Description: "多人服务器专用 JSON 文件（字段随 Mod 版本）", WorkshopID: "3761921027", ReloadBehavior: "restart_required", resolve: resolveQualityOfLifeAdapter},
		{ID: "palzones", Name: "PalZones", Description: "区域权限、等级限制与伤害规则配置", ReloadBehavior: "restart_required", resolve: resolvePalZonesAdapter},
		{ID: "glider-restoration", Name: "滑翔帕鲁外观恢复", Description: "恢复滑翔帕鲁外观，并检查 PalSchema 与 UE4SS 依赖", WorkshopID: gliderRestorationWorkshopID, ReloadBehavior: "restart_required", resolve: resolveGliderRestorationAdapter},
	}
}

func (m Manager) ListConfigurations(ctx context.Context) ([]ConfigurationAdapter, error) {
	out := make([]ConfigurationAdapter, 0, len(adapterRegistry()))
	for _, definition := range adapterRegistry() {
		targets, err := definition.resolve(m, ctx)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		files := make([]ConfigFile, 0, len(targets))
		for _, target := range targets {
			file, fileErr := m.describeConfigTarget(target)
			if fileErr == nil {
				files = append(files, file)
			} else if !errors.Is(fileErr, fs.ErrNotExist) {
				return nil, fileErr
			}
		}
		adapter := ConfigurationAdapter{
			ID: definition.ID, Name: definition.Name, Description: definition.Description,
			WorkshopID: definition.WorkshopID, Available: len(files) > 0, Installed: len(files) > 0,
			Configured: len(files) > 0, Enabled: len(files) > 0, Status: ConfigurationStatusNotInstalled,
			ReloadBehavior: definition.ReloadBehavior, Dependencies: []ConfigurationDependency{},
			Actions: []ConfigurationAction{}, Files: files,
		}
		if adapter.Available {
			adapter.Status = ConfigurationStatusReady
		}
		adapter, err = m.enrichConfigurationAdapter(ctx, adapter)
		if err != nil {
			return nil, err
		}
		out = append(out, adapter)
	}
	return out, nil
}

func (m Manager) GetConfiguration(ctx context.Context, adapterID, fileID string) (ConfigDocument, error) {
	target, err := m.resolveAdapterTarget(ctx, adapterID, fileID)
	if err != nil {
		return ConfigDocument{}, err
	}
	return m.readConfigTarget(target)
}

func (m Manager) WriteConfiguration(ctx context.Context, adapterID, fileID string, request ConfigWriteRequest) (ConfigDocument, error) {
	target, err := m.resolveAdapterTarget(ctx, adapterID, fileID)
	if err != nil {
		return ConfigDocument{}, err
	}
	if adapterID == "palzones" {
		if err := validatePalZonesContent([]byte(request.Content)); err != nil {
			return ConfigDocument{}, err
		}
	}
	document, err := m.writeConfigTarget(target, request)
	if err == nil && adapterReloadBehavior(adapterID) == "restart_required" {
		_ = m.store.SetKV(ctx, "pending_restart", "true")
	}
	return document, err
}

func (m Manager) ListConfigurationBackups(ctx context.Context, adapterID, fileID string) ([]ConfigBackup, error) {
	target, err := m.resolveAdapterTarget(ctx, adapterID, fileID)
	if err != nil {
		return nil, err
	}
	return m.listBackups(target)
}

func (m Manager) RestoreConfigurationBackup(ctx context.Context, adapterID, fileID, backupID, revision string) (ConfigDocument, error) {
	target, err := m.resolveAdapterTarget(ctx, adapterID, fileID)
	if err != nil {
		return ConfigDocument{}, err
	}
	return m.restoreBackup(target, backupID, revision)
}

func (m Manager) ListModConfigFiles(ctx context.Context, modID string) ([]ConfigFile, error) {
	root, err := m.resolveModConfigRoot(ctx, modID)
	if err != nil {
		return nil, err
	}
	return m.listConfigTargets(configTarget{scope: "mod:" + modID, root: root, path: root})
}

func (m Manager) GetModConfigFile(ctx context.Context, modID, fileID string) (ConfigDocument, error) {
	target, err := m.resolveModFileTarget(ctx, modID, fileID)
	if err != nil {
		return ConfigDocument{}, err
	}
	return m.readConfigTarget(target)
}

func (m Manager) WriteModConfigFile(ctx context.Context, modID, fileID string, request ConfigWriteRequest) (ConfigDocument, error) {
	target, err := m.resolveModFileTarget(ctx, modID, fileID)
	if err != nil {
		return ConfigDocument{}, err
	}
	return m.writeConfigTarget(target, request)
}

func (m Manager) ListModConfigBackups(ctx context.Context, modID, fileID string) ([]ConfigBackup, error) {
	target, err := m.resolveModFileTarget(ctx, modID, fileID)
	if err != nil {
		return nil, err
	}
	return m.listBackups(target)
}

func (m Manager) RestoreModConfigBackup(ctx context.Context, modID, fileID, backupID, revision string) (ConfigDocument, error) {
	target, err := m.resolveModFileTarget(ctx, modID, fileID)
	if err != nil {
		return ConfigDocument{}, err
	}
	return m.restoreBackup(target, backupID, revision)
}

func (m Manager) resolveAdapterTarget(ctx context.Context, adapterID, fileID string) (configTarget, error) {
	for _, definition := range adapterRegistry() {
		if definition.ID != adapterID {
			continue
		}
		targets, err := definition.resolve(m, ctx)
		if err != nil {
			return configTarget{}, configFailure("configuration_unavailable", err)
		}
		if len(targets) == 0 {
			return configTarget{}, configFailure("configuration_unavailable", fs.ErrNotExist)
		}
		if fileID == "" && len(targets) == 1 {
			return targets[0], nil
		}
		for _, target := range targets {
			if configFileID(target.scope, target.relative) == fileID {
				return target, nil
			}
		}
		return configTarget{}, configFailure("configuration_file_not_found", fs.ErrNotExist)
	}
	return configTarget{}, configFailure("configuration_adapter_not_found", fs.ErrNotExist)
}

func (m Manager) resolveModFileTarget(ctx context.Context, modID, fileID string) (configTarget, error) {
	root, err := m.resolveModConfigRoot(ctx, modID)
	if err != nil {
		return configTarget{}, err
	}
	targets, err := m.scanConfigTargets(configTarget{scope: "mod:" + modID, root: root, path: root})
	if err != nil {
		return configTarget{}, err
	}
	for _, target := range targets {
		if configFileID(target.scope, target.relative) == fileID {
			return target, nil
		}
	}
	return configTarget{}, configFailure("configuration_file_not_found", fs.ErrNotExist)
}

func (m Manager) resolveModConfigRoot(ctx context.Context, modID string) (string, error) {
	if strings.TrimSpace(modID) == "" {
		return "", configFailure("mod_not_found", fs.ErrNotExist)
	}
	if record, err := m.store.GetMod(ctx, modID); err == nil {
		root, targetErr := palPanelOwnedModTarget(m.cfg.WorkshopModsDir(), record.ID, record.Path)
		if targetErr != nil {
			return "", configFailure("unsafe_configuration_path", targetErr)
		}
		return m.validateConfigRoot(root)
	}
	scan, err := m.ScanLocal(ctx)
	if err != nil {
		return "", configFailure("mod_scan_failed", err)
	}
	for _, finding := range scan.Findings {
		if finding.ID != modID {
			continue
		}
		if finding.Source == LocalModSourceDatabase {
			return "", configFailure("mod_has_no_config_root", fs.ErrNotExist)
		}
		root := commonConfigRoot(finding.Paths)
		if root == "" {
			return "", configFailure("mod_has_no_config_root", fs.ErrNotExist)
		}
		if err := m.validateSupportedLocalModRoot(finding.Source, root); err != nil {
			return "", err
		}
		return m.validateConfigRoot(root)
	}
	return "", configFailure("mod_not_found", fs.ErrNotExist)
}

func (m Manager) validateSupportedLocalModRoot(source LocalModSource, root string) error {
	switch source {
	case LocalModSourceWorkshop:
		if isDirectChildOfServerLayout(m.cfg.ServerDirectory(), root, "Mods", "Workshop") {
			return nil
		}
	case LocalModSourceLegacyPak:
		if isDirectChildOfServerLayout(m.cfg.ServerDirectory(), root, "Pal", "Content", "Paks", "LogicMods", "Mods") {
			return nil
		}
	case LocalModSourceUE4SS:
		for _, components := range [][]string{
			{"Pal", "Binaries", "Win64", "Mods"},
			{"Pal", "Binaries", "Win64", "UE4SS", "Mods"},
			{"Pal", "Binaries", "Win64", "RE-UE4SS", "Mods"},
		} {
			if isDirectChildOfServerLayout(m.cfg.ServerDirectory(), root, components...) {
				return nil
			}
		}
	default:
		return configFailure("mod_has_no_config_root", fs.ErrNotExist)
	}
	return configFailure("unsafe_configuration_path", fmt.Errorf("configuration root is not a direct child of a supported Mod directory"))
}

func isDirectChildOfServerLayout(serverRoot, target string, parentComponents ...string) bool {
	serverAbsolute, serverErr := filepath.Abs(filepath.Clean(serverRoot))
	targetAbsolute, targetErr := filepath.Abs(filepath.Clean(target))
	if serverErr != nil || targetErr != nil {
		return false
	}
	relative, err := filepath.Rel(serverAbsolute, targetAbsolute)
	if err != nil || unsafeRelativePath(relative) {
		return false
	}
	components := strings.Split(filepath.ToSlash(relative), "/")
	if len(components) != len(parentComponents)+1 {
		return false
	}
	for index, expected := range parentComponents {
		if !strings.EqualFold(components[index], expected) {
			return false
		}
	}
	return strings.TrimSpace(components[len(components)-1]) != ""
}

func (m Manager) validateConfigRoot(root string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(strings.TrimSpace(root)))
	if err != nil {
		return "", configFailure("unsafe_configuration_path", err)
	}
	if err := m.cfg.ValidateManagedPath(absolute, false); err != nil {
		return "", configFailure("unsafe_configuration_path", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", configFailure("configuration_root_not_found", err)
	}
	linked, linkErr := localScanPathIsLink(absolute, info)
	if linkErr != nil || linked {
		return "", configFailure("unsafe_configuration_path", fmt.Errorf("configuration root is a link or reparse point"))
	}
	if !info.IsDir() {
		absolute = filepath.Dir(absolute)
	}
	return absolute, nil
}

func commonConfigRoot(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	root := filepath.Clean(paths[0])
	if info, err := os.Lstat(root); err == nil && !info.IsDir() {
		root = filepath.Dir(root)
	}
	for _, path := range paths[1:] {
		candidate := filepath.Clean(path)
		if info, err := os.Lstat(candidate); err == nil && !info.IsDir() {
			candidate = filepath.Dir(candidate)
		}
		for !scanPathWithin(root, candidate) {
			parent := filepath.Dir(root)
			if parent == root {
				return ""
			}
			root = parent
		}
	}
	return root
}

func (m Manager) listConfigTargets(base configTarget) ([]ConfigFile, error) {
	targets, err := m.scanConfigTargets(base)
	if err != nil {
		return nil, err
	}
	out := make([]ConfigFile, 0, len(targets))
	for _, target := range targets {
		file, err := m.describeConfigTarget(target)
		if err != nil {
			var configErr ConfigurationError
			if errors.As(err, &configErr) && (configErr.Code == "configuration_not_utf8" || configErr.Code == "configuration_file_too_large") {
				continue
			}
			return nil, err
		}
		out = append(out, file)
	}
	return out, nil
}

func (m Manager) scanConfigTargets(base configTarget) ([]configTarget, error) {
	root, err := m.validateConfigRoot(base.root)
	if err != nil {
		return nil, err
	}
	var targets []configTarget
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if len(targets) >= 256 {
			return fs.SkipAll
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		linked, err := localScanPathIsLink(path, info)
		if err != nil {
			return err
		}
		if linked {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			relative, _ := filepath.Rel(root, path)
			if relative != "." && len(strings.Split(filepath.ToSlash(relative), "/")) > 12 {
				return fs.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() || !isEditableConfigName(entry.Name()) || info.Size() > MaxConfigFileBytes {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || unsafeRelativePath(relative) {
			return nil
		}
		targets = append(targets, configTarget{scope: base.scope, root: root, path: path, relative: filepath.ToSlash(relative)})
		return nil
	})
	if err != nil {
		return nil, configFailure("configuration_scan_failed", err)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].relative < targets[j].relative })
	return targets, nil
}

func (m Manager) describeConfigTarget(target configTarget) (ConfigFile, error) {
	content, info, err := m.secureReadTarget(target)
	if err != nil {
		return ConfigFile{}, err
	}
	extension := strings.ToLower(filepath.Ext(target.path))
	executable := extension == ".lua"
	risk := ""
	if executable {
		risk = "Lua 是可执行代码；保存前必须确认，并在重启服务器前审查差异。"
	}
	return ConfigFile{
		ID: configFileID(target.scope, target.relative), Name: filepath.Base(target.path), Path: target.relative,
		Extension: extension, Size: info.Size(), ModifiedAt: info.ModTime().UTC(), Revision: configRevision(content),
		Executable: executable, Risk: risk,
	}, nil
}

func (m Manager) readConfigTarget(target configTarget) (ConfigDocument, error) {
	content, _, err := m.secureReadTarget(target)
	if err != nil {
		return ConfigDocument{}, err
	}
	file, err := m.describeConfigTarget(target)
	if err != nil {
		return ConfigDocument{}, err
	}
	return ConfigDocument{File: file, Content: string(content), Format: strings.TrimPrefix(file.Extension, "."), Fields: configFields(file.Extension, content)}, nil
}

func (m Manager) secureReadTarget(target configTarget) ([]byte, fs.FileInfo, error) {
	if err := m.validateTarget(target); err != nil {
		return nil, nil, err
	}
	info, err := os.Lstat(target.path)
	if err != nil {
		return nil, nil, configFailure("configuration_file_not_found", err)
	}
	linked, linkErr := localScanPathIsLink(target.path, info)
	if linkErr != nil || linked || !info.Mode().IsRegular() {
		return nil, nil, configFailure("unsafe_configuration_path", fmt.Errorf("configuration file is not a regular non-linked file"))
	}
	if info.Size() > MaxConfigFileBytes {
		return nil, nil, configFailure("configuration_file_too_large", fmt.Errorf("configuration file exceeds 1 MiB"))
	}
	content, err := os.ReadFile(target.path)
	if err != nil {
		return nil, nil, configFailure("configuration_read_failed", err)
	}
	if err := validateTextSafety(content); err != nil {
		return nil, nil, err
	}
	return content, info, nil
}

func (m Manager) validateTarget(target configTarget) error {
	if unsafeRelativePath(target.relative) || !isEditableConfigName(filepath.Base(target.path)) {
		return configFailure("configuration_file_forbidden", fmt.Errorf("file is not editable"))
	}
	root, err := m.validateConfigRoot(target.root)
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(filepath.Clean(target.path))
	if err != nil || !scanPathWithin(root, absolute) {
		return configFailure("unsafe_configuration_path", fmt.Errorf("configuration path escapes Mod root"))
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || filepath.ToSlash(relative) != filepath.ToSlash(target.relative) {
		return configFailure("unsafe_configuration_path", fmt.Errorf("configuration file identity changed"))
	}
	for current := absolute; !sameScanPath(current, root); current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return configFailure("configuration_file_not_found", statErr)
		}
		linked, linkErr := localScanPathIsLink(current, info)
		if linkErr != nil || linked {
			return configFailure("unsafe_configuration_path", fmt.Errorf("configuration path contains a link or reparse point"))
		}
	}
	return nil
}

func (m Manager) writeConfigTarget(target configTarget, request ConfigWriteRequest) (ConfigDocument, error) {
	content := []byte(request.Content)
	if int64(len(content)) > MaxConfigFileBytes {
		return ConfigDocument{}, configFailure("configuration_file_too_large", fmt.Errorf("configuration file exceeds 1 MiB"))
	}
	extension := strings.ToLower(filepath.Ext(target.path))
	if extension == ".lua" && !request.ConfirmExecutable {
		return ConfigDocument{}, configFailure("executable_confirmation_required", fmt.Errorf("saving Lua requires explicit confirmation"))
	}
	if err := validateTextContent(extension, content); err != nil {
		return ConfigDocument{}, err
	}
	lock := configPathMutex(target.path)
	lock.Lock()
	defer lock.Unlock()
	current, info, err := m.secureReadTarget(target)
	if err != nil {
		return ConfigDocument{}, err
	}
	if strings.TrimSpace(request.Revision) == "" || request.Revision != configRevision(current) {
		return ConfigDocument{}, configFailure("configuration_revision_conflict", fmt.Errorf("configuration changed since it was opened"))
	}
	if string(current) == request.Content {
		return m.readConfigTarget(target)
	}
	if _, err := m.createBackup(target, current); err != nil {
		return ConfigDocument{}, err
	}
	if err := m.atomicReplace(target, content, info.Mode().Perm()); err != nil {
		return ConfigDocument{}, err
	}
	return m.readConfigTarget(target)
}

func (m Manager) atomicReplace(target configTarget, content []byte, mode fs.FileMode) error {
	if err := m.validateTarget(target); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target.path), ".palpanel-config-*.tmp")
	if err != nil {
		return configFailure("configuration_write_failed", err)
	}
	temporaryPath := temporary.Name()
	complete := false
	defer func() {
		_ = temporary.Close()
		if !complete {
			_ = os.Remove(temporaryPath)
		}
	}()
	if mode == 0 {
		mode = 0o600
	}
	if err := temporary.Chmod(mode); err != nil {
		return configFailure("configuration_write_failed", err)
	}
	if _, err := temporary.Write(content); err != nil {
		return configFailure("configuration_write_failed", err)
	}
	if err := temporary.Sync(); err != nil {
		return configFailure("configuration_write_failed", err)
	}
	if err := temporary.Close(); err != nil {
		return configFailure("configuration_write_failed", err)
	}
	if err := replaceConfigFile(temporaryPath, target.path); err != nil {
		return configFailure("configuration_write_failed", err)
	}
	complete = true
	return nil
}

func validateTextContent(extension string, content []byte) error {
	if err := validateTextSafety(content); err != nil {
		return err
	}
	switch strings.ToLower(extension) {
	case ".json":
		var value any
		if err := json.Unmarshal(content, &value); err != nil {
			return configFailure("configuration_parse_failed", fmt.Errorf("invalid JSON: %w", err))
		}
	case ".yaml", ".yml":
		var value any
		if err := yaml.Unmarshal(content, &value); err != nil {
			return configFailure("configuration_parse_failed", fmt.Errorf("invalid YAML: %w", err))
		}
	case ".toml":
		var value any
		if err := toml.Unmarshal(content, &value); err != nil {
			return configFailure("configuration_parse_failed", fmt.Errorf("invalid TOML: %w", err))
		}
	case ".ini", ".cfg":
		if err := validateINIContent(content); err != nil {
			return configFailure("configuration_parse_failed", err)
		}
	}
	return nil
}

func validateINIContent(content []byte) error {
	for index, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") || strings.TrimSpace(line[1:len(line)-1]) == "" {
				return fmt.Errorf("invalid INI section on line %d", index+1)
			}
			continue
		}
		separator := strings.IndexByte(line, '=')
		if separator < 0 {
			separator = strings.IndexByte(line, ':')
		}
		if separator <= 0 || strings.TrimSpace(line[:separator]) == "" {
			return fmt.Errorf("invalid INI assignment on line %d", index+1)
		}
	}
	return nil
}

func validateTextSafety(content []byte) error {
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		return configFailure("configuration_not_utf8", fmt.Errorf("configuration must be UTF-8 text without NUL bytes"))
	}
	return nil
}

func (m Manager) createBackup(target configTarget, content []byte) (ConfigBackup, error) {
	directory := m.backupDirectory(target)
	if err := m.validateBackupPath(directory, true); err != nil {
		return ConfigBackup{}, configFailure("configuration_backup_failed", err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ConfigBackup{}, configFailure("configuration_backup_failed", err)
	}
	if err := m.validateBackupPath(directory, false); err != nil {
		return ConfigBackup{}, configFailure("configuration_backup_failed", err)
	}
	backupID := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + id.New("backup")
	path := filepath.Join(directory, backupID+".bak")
	if err := m.validateBackupPath(path, true); err != nil {
		return ConfigBackup{}, configFailure("configuration_backup_failed", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return ConfigBackup{}, configFailure("configuration_backup_failed", err)
	}
	if err := m.validateBackupPath(path, false); err != nil {
		return ConfigBackup{}, configFailure("configuration_backup_failed", err)
	}
	return ConfigBackup{ID: backupID, Revision: configRevision(content), Size: int64(len(content)), CreatedAt: time.Now().UTC()}, nil
}

func (m Manager) listBackups(target configTarget) ([]ConfigBackup, error) {
	directory := m.backupDirectory(target)
	if err := m.validateBackupPath(directory, false); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []ConfigBackup{}, nil
		}
		return nil, configFailure("configuration_backup_read_failed", err)
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return []ConfigBackup{}, nil
	}
	if err != nil {
		return nil, configFailure("configuration_backup_read_failed", err)
	}
	out := make([]ConfigBackup, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".bak") {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		linked, linkErr := localScanPathIsLink(filepath.Join(directory, entry.Name()), info)
		if linkErr != nil || linked || !info.Mode().IsRegular() || info.Size() > MaxConfigFileBytes {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(directory, entry.Name()))
		if readErr != nil {
			continue
		}
		out = append(out, ConfigBackup{ID: strings.TrimSuffix(entry.Name(), ".bak"), Revision: configRevision(body), Size: info.Size(), CreatedAt: info.ModTime().UTC()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m Manager) restoreBackup(target configTarget, backupID, revision string) (ConfigDocument, error) {
	if backupID == "" || strings.ContainsAny(backupID, `/\\`) || strings.Contains(backupID, "..") {
		return ConfigDocument{}, configFailure("configuration_backup_not_found", fs.ErrNotExist)
	}
	current, info, err := m.secureReadTarget(target)
	if err != nil {
		return ConfigDocument{}, err
	}
	if revision == "" || revision != configRevision(current) {
		return ConfigDocument{}, configFailure("configuration_revision_conflict", fmt.Errorf("configuration changed since it was opened"))
	}
	backupPath := filepath.Join(m.backupDirectory(target), backupID+".bak")
	if err := m.validateBackupPath(backupPath, false); err != nil {
		return ConfigDocument{}, configFailure("configuration_backup_not_found", fs.ErrNotExist)
	}
	backupInfo, err := os.Lstat(backupPath)
	if err != nil {
		return ConfigDocument{}, configFailure("configuration_backup_not_found", fs.ErrNotExist)
	}
	linked, linkErr := localScanPathIsLink(backupPath, backupInfo)
	if linkErr != nil || linked || !backupInfo.Mode().IsRegular() || backupInfo.Size() > MaxConfigFileBytes {
		return ConfigDocument{}, configFailure("configuration_backup_not_found", fs.ErrNotExist)
	}
	body, err := os.ReadFile(backupPath)
	if err != nil {
		return ConfigDocument{}, configFailure("configuration_backup_read_failed", err)
	}
	if err := validateTextContent(filepath.Ext(target.path), body); err != nil {
		return ConfigDocument{}, err
	}
	lock := configPathMutex(target.path)
	lock.Lock()
	defer lock.Unlock()
	current, info, err = m.secureReadTarget(target)
	if err != nil {
		return ConfigDocument{}, err
	}
	if revision == "" || revision != configRevision(current) {
		return ConfigDocument{}, configFailure("configuration_revision_conflict", fmt.Errorf("configuration changed since it was opened"))
	}
	if _, err := m.createBackup(target, current); err != nil {
		return ConfigDocument{}, err
	}
	if err := m.atomicReplace(target, body, info.Mode().Perm()); err != nil {
		return ConfigDocument{}, err
	}
	return m.readConfigTarget(target)
}

func (m Manager) validateBackupPath(path string, allowMissing bool) error {
	if err := m.cfg.ValidateManagedPath(path, false); err != nil {
		return err
	}
	root, err := filepath.Abs(filepath.Clean(m.cfg.BackupsDir))
	if err != nil {
		return err
	}
	target, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	if !scanPathWithin(root, target) || sameScanPath(root, target) {
		return fmt.Errorf("backup path escapes the configured backup root")
	}
	for current := target; ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if !allowMissing || !errors.Is(statErr, fs.ErrNotExist) {
				return statErr
			}
		} else {
			linked, linkErr := localScanPathIsLink(current, info)
			if linkErr != nil {
				return linkErr
			}
			if linked {
				return fmt.Errorf("backup path contains a link or reparse point")
			}
		}
		if sameScanPath(current, root) {
			break
		}
		parent := filepath.Dir(current)
		if sameScanPath(parent, current) {
			return fmt.Errorf("backup path escapes the configured backup root")
		}
	}
	return nil
}

func (m Manager) backupDirectory(target configTarget) string {
	hash := sha256.Sum256([]byte(target.scope + "\x00" + target.relative))
	return filepath.Join(m.cfg.BackupsDir, "mod-config", hex.EncodeToString(hash[:16]))
}

func resolvePalDefenderAdapter(m Manager, _ context.Context) ([]configTarget, error) {
	root := m.cfg.PalDefenderDir()
	return existingTargets("adapter:paldefender", root, []string{"Config.json"})
}

func resolveUE4SSAdapter(m Manager, _ context.Context) ([]configTarget, error) {
	root := m.cfg.Win64Dir()
	return existingTargets("adapter:ue4ss", root, []string{"UE4SS-settings.ini", filepath.Join("Mods", "mods.txt")})
}

func resolvePalSchemaAdapter(m Manager, ctx context.Context) ([]configTarget, error) {
	root, err := m.findKnownModRoot(ctx, "3625280368", "palschema")
	if err != nil {
		return nil, err
	}
	targets, err := m.scanConfigTargets(configTarget{scope: "adapter:palschema", root: root, path: root})
	if err != nil {
		return nil, err
	}
	jsonTargets := targets[:0]
	for _, target := range targets {
		if strings.EqualFold(filepath.Ext(target.path), ".json") {
			jsonTargets = append(jsonTargets, target)
		}
	}
	return jsonTargets, nil
}

func resolveExtendedBaseRangeAdapter(m Manager, ctx context.Context) ([]configTarget, error) {
	root, err := m.findKnownModRoot(ctx, "3625907101", "extended base range")
	if err != nil {
		return nil, err
	}
	var targets []configTarget
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || len(targets) > 0 {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(entry.Name(), "main.lua") {
			relative, _ := filepath.Rel(root, path)
			targets = append(targets, configTarget{scope: "adapter:extended-base-range", root: root, path: path, relative: filepath.ToSlash(relative)})
		}
		return nil
	})
	return targets, nil
}

func resolveQualityOfLifeAdapter(m Manager, ctx context.Context) ([]configTarget, error) {
	root, err := m.findKnownModRoot(ctx, "3761921027", "qualityoflife")
	if err != nil {
		return nil, err
	}
	targets, err := m.scanConfigTargets(configTarget{scope: "adapter:quality-of-life", root: root, path: root})
	if err != nil {
		return nil, err
	}
	matched := targets[:0]
	for _, target := range targets {
		if strings.EqualFold(filepath.Base(target.path), "QualityOfLifeConfig.json") {
			matched = append(matched, target)
		}
	}
	return matched, nil
}

func (m Manager) findKnownModRoot(ctx context.Context, workshopID, name string) (string, error) {
	mods, err := m.store.ListMods(ctx)
	if err != nil {
		return "", err
	}
	for _, mod := range mods {
		if mod.WorkshopID == workshopID {
			root, targetErr := palPanelOwnedModTarget(m.cfg.WorkshopModsDir(), mod.ID, mod.Path)
			if targetErr != nil {
				return "", configFailure("unsafe_configuration_path", targetErr)
			}
			return m.validateConfigRoot(root)
		}
	}
	scan, err := m.ScanLocal(ctx)
	if err != nil {
		return "", err
	}
	for _, finding := range scan.Findings {
		if knownModIdentityMatches(finding.Name, finding.PackageName, name) {
			root := commonConfigRoot(finding.Paths)
			if err := m.validateSupportedLocalModRoot(finding.Source, root); err != nil {
				return "", err
			}
			return m.validateConfigRoot(root)
		}
	}
	return "", fs.ErrNotExist
}

func knownModIdentityMatches(modName, packageName, expected string) bool {
	want := normalizeModIdentity(expected)
	return want != "" && (normalizeModIdentity(modName) == want || normalizeModIdentity(packageName) == want)
}

func normalizeModIdentity(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func existingTargets(scope, root string, relatives []string) ([]configTarget, error) {
	out := make([]configTarget, 0, len(relatives))
	for _, relative := range relatives {
		path := filepath.Join(root, relative)
		if _, err := os.Lstat(path); err == nil {
			out = append(out, configTarget{scope: scope, root: root, path: path, relative: filepath.ToSlash(relative)})
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return out, nil
}

func configFields(extension string, content []byte) []ConfigurationField {
	switch strings.ToLower(extension) {
	case ".json":
		var value any
		if json.Unmarshal(content, &value) == nil {
			var fields []ConfigurationField
			flattenJSONFields("", value, &fields)
			if len(fields) > 160 {
				fields = fields[:160]
			}
			return fields
		}
	case ".ini", ".cfg", ".toml":
		return keyValueFields(string(content))
	case ".lua":
		return luaNumericFields(string(content))
	}
	return nil
}

func flattenJSONFields(path string, value any, out *[]ConfigurationField) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			next := key
			if path != "" {
				next = path + "." + key
			}
			flattenJSONFields(next, typed[key], out)
		}
	case bool:
		*out = append(*out, decorateConfigurationField(ConfigurationField{Path: path, Type: "boolean", Value: typed}))
	case float64:
		fieldType := "number"
		if typed == float64(int64(typed)) {
			fieldType = "integer"
		}
		*out = append(*out, decorateConfigurationField(ConfigurationField{Path: path, Type: fieldType, Value: typed}))
	case string:
		*out = append(*out, decorateConfigurationField(ConfigurationField{Path: path, Type: "string", Value: typed}))
	}
}

func keyValueFields(content string) []ConfigurationField {
	section := ""
	var out []ConfigurationField
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, raw, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		path := key
		if section != "" {
			path = section + "." + key
		}
		value, fieldType := parseScalar(strings.TrimSpace(raw))
		out = append(out, decorateConfigurationField(ConfigurationField{Path: path, Type: fieldType, Value: value}))
		if len(out) >= 160 {
			break
		}
	}
	return out
}

func luaNumericFields(content string) []ConfigurationField {
	var out []ConfigurationField
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		left, right, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(left), "local "))
		if name == "" || strings.ContainsAny(name, " [](){}") {
			continue
		}
		raw := strings.TrimSpace(strings.SplitN(right, "--", 2)[0])
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			continue
		}
		min, max := int64(0), int64(1000000)
		out = append(out, decorateConfigurationField(ConfigurationField{Path: name, Type: "number", Value: value, Min: &min, Max: &max}))
		if len(out) >= 80 {
			break
		}
	}
	return out
}

func parseScalar(raw string) (any, string) {
	if value, err := strconv.ParseBool(strings.ToLower(raw)); err == nil {
		return value, "boolean"
	}
	if value, err := strconv.ParseFloat(raw, 64); err == nil {
		return value, "number"
	}
	return strings.Trim(raw, `"'`), "string"
}

func fieldLabel(path string) string {
	parts := strings.Split(path, ".")
	leaf := parts[len(parts)-1]
	for _, candidate := range []string{path, leaf} {
		if label := chineseConfigFieldLabels[normalizeConfigFieldKey(candidate)]; label != "" {
			return label
		}
	}
	words := splitConfigFieldWords(leaf)
	translated := false
	localized := make([]string, 0, len(words))
	for _, word := range words {
		if label, ok := chineseConfigFieldWords[strings.ToLower(word)]; ok {
			translated = true
			if label != "" {
				localized = append(localized, label)
			}
			continue
		}
		localized = append(localized, word)
	}
	if translated {
		return strings.Join(localized, " ")
	}
	return leaf
}

func decorateConfigurationField(field ConfigurationField) ConfigurationField {
	field.Label = fieldLabel(field.Path)
	field.Group = "常规设置"
	field.Description = "配置路径：" + field.Path
	leaf := field.Path
	if separator := strings.LastIndex(field.Path, "."); separator >= 0 {
		leaf = field.Path[separator+1:]
	}
	if field.Label == leaf {
		field.Label = "自定义配置项"
		field.Group = "自定义设置"
		field.Description = "此字段尚未收录中文说明，请结合原始配置路径确认含义。"
	}
	return field
}

var chineseConfigFieldLabels = map[string]string{
	"version":                      "配置版本",
	"motd":                         "欢迎消息（MOTD）",
	"exitserveronstartupfailure":   "启动失败时退出服务器",
	"preventadminpasswordinchat":   "防止管理员密码出现在聊天中",
	"shouldwarncheaters":           "警告作弊玩家",
	"shouldwarncheatersreason":     "显示作弊警告原因",
	"shouldkickcheaters":           "踢出作弊玩家",
	"shouldbancheaters":            "封禁作弊玩家",
	"shouldipbancheaters":          "同时封禁作弊玩家 IP",
	"logchat":                      "记录聊天",
	"logrcon":                      "记录 RCON 操作",
	"logplayeruid":                 "记录玩家 UID",
	"logplayerip":                  "记录玩家 IP",
	"logplayerdeaths":              "记录玩家死亡",
	"logplayerlogins":              "记录玩家登录",
	"logplayerbuildings":           "记录玩家建造",
	"logplayersummons":             "记录玩家召唤",
	"logplayercaptures":            "记录玩家捕获",
	"logcraftings":                 "记录制作",
	"logtechunlocks":               "记录科技解锁",
	"useadminwhitelist":            "启用管理员白名单",
	"adminautologin":               "管理员自动登录",
	"adminips":                     "管理员 IP 列表",
	"bannedchatwords":              "聊天违禁词",
	"bannednames":                  "禁用名称",
	"announceconnections":          "播报玩家连接",
	"dontannounceadminconnections": "不播报管理员连接",
	"announcepunishments":          "播报处罚",
	"usewhitelist":                 "启用玩家白名单",
	"whitelistmessage":             "白名单提示消息",
	"steamidprotection":            "Steam ID 保护",
	"blocktowerbosscapture":        "阻止捕获高塔头目",
	"disableillegalitemprotection": "关闭非法物品保护",
	"disablebutchering":            "禁止肢解帕鲁",
	"disablerenaming":              "禁止重命名",
	"disablepalrenaming":           "禁止帕鲁重命名",
	"doactionuponillegalpalstats":  "发现非法帕鲁属性时执行操作",
	"palstatsmaxrank":              "帕鲁属性允许的最高 Rank",
	"bannedtechnologies":           "禁用科技列表",
	"baserange":                    "基地范围",
	"baserangemultiplier":          "基地范围倍率",
	"workefficiencymultiplier":     "工作效率倍率",
	"basecampworkermaxnum":         "基地最大工作帕鲁数",
	"buseuobjectarraycache":        "使用 UObject 数组缓存",
	"guiconsoleenabled":            "启用图形控制台",
	"consoleenabled":               "启用控制台",
	"graphicsapi":                  "图形 API",
	"loglevel":                     "日志级别",
	"serverpassword":               "服务器密码",
	"adminpassword":                "管理员密码",
	"enabled":                      "启用",
	"disabled":                     "禁用",
	"multiplier":                   "倍率",
	"interval":                     "间隔",
	"timeout":                      "超时",
	"port":                         "端口",
}

var chineseConfigFieldWords = map[string]string{
	"b": "", "use": "使用", "enabled": "启用", "disabled": "禁用", "enable": "启用", "disable": "禁用",
	"server": "服务器", "admin": "管理员", "player": "玩家", "pal": "帕鲁", "base": "基地", "camp": "营地",
	"max": "最大", "min": "最小", "count": "数量", "num": "数量", "range": "范围", "limit": "限制",
	"rate": "速率", "multiplier": "倍率", "interval": "间隔", "timeout": "超时", "port": "端口",
	"log": "日志", "level": "等级", "password": "密码", "message": "消息", "name": "名称",
	"cache": "缓存", "array": "数组", "console": "控制台", "graphics": "图形", "api": "API",
	"worker": "工作帕鲁", "work": "工作", "efficiency": "效率", "protection": "保护", "announce": "播报",
}

func normalizeConfigFieldKey(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(value) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func splitConfigFieldWords(value string) []string {
	runes := []rune(strings.TrimSpace(value))
	words := make([]string, 0, 6)
	start := -1
	flush := func(end int) {
		if start >= 0 && end > start {
			words = append(words, string(runes[start:end]))
		}
		start = -1
	}
	for index, character := range runes {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			flush(index)
			continue
		}
		if start < 0 {
			start = index
			continue
		}
		previous := runes[index-1]
		if unicode.IsUpper(character) && (unicode.IsLower(previous) || unicode.IsDigit(previous)) {
			flush(index)
			start = index
		}
	}
	flush(len(runes))
	return words
}

func isEditableConfigName(name string) bool {
	if forbiddenConfigNames[strings.ToLower(strings.TrimSpace(name))] {
		return false
	}
	return editableConfigExtensions[strings.ToLower(filepath.Ext(name))]
}

func unsafeRelativePath(relative string) bool {
	clean := filepath.Clean(strings.TrimSpace(relative))
	return clean == "" || clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator))
}

func configRevision(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func configFileID(scope, relative string) string {
	hash := sha256.Sum256([]byte(scope + "\x00" + filepath.ToSlash(relative)))
	return hex.EncodeToString(hash[:16])
}

func configPathMutex(path string) *sync.Mutex {
	key := scanPathKey(filepath.Clean(path))
	value, _ := configPathLocks.LoadOrStore(key, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func configFailure(code string, err error) error { return ConfigurationError{Code: code, Err: err} }
