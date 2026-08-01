package mods

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"palpanel/internal/db"
)

const (
	gliderRestorationWorkshopID = "3625871847"
	palSchemaWorkshopID         = "3625280368"
	ue4ssWorkshopID             = "3625223587"
)

var palZonesReferenceURLs = map[string]string{
	"source": "https://github.com/xMathayus/PalZones-Map",
	"editor": "https://xmathayus.github.io/PalZones-Map/",
	"mod":    "https://www.curseforge.com/palworld/lua-code-mods/palzones",
}

var gliderReferenceURLs = map[string]string{
	"steam":     "https://steamcommunity.com/sharedfiles/filedetails/?id=3625871847",
	"nexus":     "https://www.nexusmods.com/palworld/mods/2454",
	"palschema": "https://github.com/Okaetsu/PalSchema/releases/latest",
}

func resolvePalZonesAdapter(m Manager, _ context.Context) ([]configTarget, error) {
	root, found, err := m.palZonesRoot()
	if err != nil || !found {
		return nil, err
	}
	configDirectory, found, err := findCaseInsensitiveChild(root, "Config", true)
	if err != nil || !found {
		return nil, err
	}
	path, found, err := findCaseInsensitiveChild(configDirectory, "zones.json", false)
	if err != nil || !found {
		return nil, err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || unsafeRelativePath(relative) {
		return nil, configFailure("unsafe_configuration_path", fmt.Errorf("PalZones configuration escapes Mod root"))
	}
	return []configTarget{{scope: "adapter:palzones", root: root, path: path, relative: filepath.ToSlash(relative)}}, nil
}

func resolveGliderRestorationAdapter(Manager, context.Context) ([]configTarget, error) {
	return nil, nil
}

func (m Manager) palZonesRoot() (string, bool, error) {
	current := m.cfg.Win64Dir()
	for _, component := range []string{"UE4SS", "Mods", "PalZones"} {
		path, found, err := findCaseInsensitiveChild(current, component, true)
		if err != nil || !found {
			return "", false, err
		}
		current = path
	}
	root, err := m.validateConfigRoot(current)
	if err != nil {
		return "", false, err
	}
	return root, true, nil
}

func findCaseInsensitiveChild(parent, name string, directory bool) (string, bool, error) {
	entries, err := os.ReadDir(parent)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	for _, entry := range entries {
		if !strings.EqualFold(entry.Name(), name) || entry.IsDir() != directory {
			continue
		}
		return filepath.Join(parent, entry.Name()), true, nil
	}
	return "", false, nil
}

func adapterReloadBehavior(adapterID string) string {
	for _, definition := range adapterRegistry() {
		if definition.ID == adapterID {
			return definition.ReloadBehavior
		}
	}
	return ""
}

func (m Manager) enrichConfigurationAdapter(ctx context.Context, adapter ConfigurationAdapter) (ConfigurationAdapter, error) {
	switch adapter.ID {
	case "palzones":
		return m.enrichPalZonesAdapter(ctx, adapter)
	case "glider-restoration":
		return m.enrichGliderAdapter(ctx, adapter)
	default:
		if adapter.Installed && configurationRestartPending(ctx, m) {
			adapter.Status = ConfigurationStatusRestartRequired
		}
		return adapter, nil
	}
}

func (m Manager) enrichPalZonesAdapter(ctx context.Context, adapter ConfigurationAdapter) (ConfigurationAdapter, error) {
	_, installed, err := m.palZonesRoot()
	if err != nil {
		return adapter, err
	}
	adapter.ReferenceURLs = palZonesReferenceURLs
	adapter.Installed = installed
	adapter.Available = installed
	adapter.Configured = installed && len(adapter.Files) > 0
	adapter.Enabled = installed
	if !installed {
		adapter.Status = ConfigurationStatusNotInstalled
		return adapter, nil
	}
	if !adapter.Configured {
		adapter.Status = ConfigurationStatusNotConfigured
		adapter.StatusDetail = "PalZones 已安装，但尚未创建 Config/zones.json。"
		adapter.Actions = []ConfigurationAction{{ID: ConfigurationActionInitialize, Available: true, RestartRequired: true}}
		return adapter, nil
	}
	if configurationRestartPending(ctx, m) {
		adapter.Status = ConfigurationStatusRestartRequired
		adapter.StatusDetail = "区域配置已变更，等待安全重启游戏服务。"
	} else {
		adapter.Status = ConfigurationStatusReady
	}
	return adapter, nil
}

func (m Manager) enrichGliderAdapter(ctx context.Context, adapter ConfigurationAdapter) (ConfigurationAdapter, error) {
	mods, err := m.store.ListMods(ctx)
	if err != nil {
		return adapter, err
	}
	byWorkshopID := installedWorkshopMods(mods)
	settings, err := ReadModSettings(m.cfg.PalModSettingsPath())
	if err != nil {
		return adapter, err
	}
	glider, gliderInstalled := byWorkshopID[gliderRestorationWorkshopID]
	palSchema, palSchemaInstalled := byWorkshopID[palSchemaWorkshopID]
	ue4ss, ue4ssInstalled := byWorkshopID[ue4ssWorkshopID]
	adapter.ReferenceURLs = gliderReferenceURLs
	adapter.Installed = gliderInstalled
	adapter.Available = gliderInstalled
	adapter.Configured = gliderInstalled
	adapter.Enabled = gliderInstalled && modEnabledInSettings(glider, settings) && modEnabledInSettings(palSchema, settings) && modEnabledInSettings(ue4ss, settings)
	adapter.Dependencies = []ConfigurationDependency{
		{ID: "palschema", Name: "PalSchema", WorkshopID: palSchemaWorkshopID, ModID: palSchema.ID, Installed: palSchemaInstalled, Enabled: palSchemaInstalled && modEnabledInSettings(palSchema, settings), Required: true},
		{ID: "ue4ss", Name: "UE4SS Experimental", WorkshopID: ue4ssWorkshopID, ModID: ue4ss.ID, Installed: ue4ssInstalled, Enabled: ue4ssInstalled && modEnabledInSettings(ue4ss, settings), Required: true},
	}
	adapter.Actions = []ConfigurationAction{{ID: ConfigurationActionEnable, Available: gliderInstalled && palSchemaInstalled && ue4ssInstalled && !adapter.Enabled, RestartRequired: true}}
	switch {
	case !gliderInstalled:
		adapter.Status = ConfigurationStatusNotInstalled
		adapter.StatusDetail = "尚未安装 Glider Restoration。"
	case !palSchemaInstalled || !ue4ssInstalled:
		adapter.Status = ConfigurationStatusDependencyMissing
		adapter.StatusDetail = "缺少 PalSchema 或 UE4SS Experimental 依赖。"
	case !adapter.Enabled:
		adapter.Status = ConfigurationStatusDisabled
		adapter.StatusDetail = "依赖已安装，但 Mod 或全局 Mod 开关尚未完整启用。"
	case configurationRestartPending(ctx, m):
		adapter.Status = ConfigurationStatusRestartRequired
		adapter.StatusDetail = "启用状态已写入，等待安全重启游戏服务。"
	default:
		adapter.Status = ConfigurationStatusReady
	}
	return adapter, nil
}

func installedWorkshopMods(mods []db.Mod) map[string]db.Mod {
	out := make(map[string]db.Mod, len(mods))
	for _, mod := range mods {
		workshopID := strings.TrimSpace(mod.WorkshopID)
		if workshopID == "" && strings.EqualFold(mod.Source, "workshop") {
			workshopID = strings.TrimSpace(mod.ID)
		}
		if workshopID == "" || strings.TrimSpace(mod.Path) == "" {
			continue
		}
		info, err := os.Stat(mod.Path)
		if err == nil && info.IsDir() {
			out[workshopID] = mod
		}
	}
	return out
}

func modEnabledInSettings(mod db.Mod, settings ModSettings) bool {
	return mod.ID != "" && mod.Enabled && settings.GlobalEnabled && containsFold(settings.ActiveMods, mod.PackageName)
}

func configurationRestartPending(ctx context.Context, m Manager) bool {
	value, configured, err := m.store.GetKV(ctx, "pending_restart")
	return err == nil && configured && strings.EqualFold(strings.TrimSpace(value), "true")
}

func (m Manager) RunConfigurationAction(ctx context.Context, adapterID string, request ConfigurationActionRequest) (ConfigurationActionResult, error) {
	switch strings.TrimSpace(adapterID) {
	case "palzones":
		if request.Action != ConfigurationActionInitialize {
			return ConfigurationActionResult{}, configFailure("configuration_action_not_supported", fmt.Errorf("PalZones only supports initialize"))
		}
		return m.initializePalZones(ctx)
	case "glider-restoration":
		if request.Action != ConfigurationActionEnable {
			return ConfigurationActionResult{}, configFailure("configuration_action_not_supported", fmt.Errorf("Glider Restoration only supports enable"))
		}
		return m.enableGliderRestoration(ctx)
	default:
		return ConfigurationActionResult{}, configFailure("configuration_adapter_not_found", os.ErrNotExist)
	}
}

func (m Manager) initializePalZones(ctx context.Context) (ConfigurationActionResult, error) {
	root, installed, err := m.palZonesRoot()
	if err != nil {
		return ConfigurationActionResult{}, err
	}
	if !installed {
		return ConfigurationActionResult{}, configFailure("configuration_unavailable", os.ErrNotExist)
	}
	if targets, resolveErr := resolvePalZonesAdapter(m, ctx); resolveErr != nil {
		return ConfigurationActionResult{}, resolveErr
	} else if len(targets) > 0 {
		return ConfigurationActionResult{}, configFailure("configuration_already_initialized", fmt.Errorf("Config/zones.json already exists"))
	}
	configDirectory := filepath.Join(root, "Config")
	if err := m.cfg.ValidateManagedPath(configDirectory, false); err != nil {
		return ConfigurationActionResult{}, configFailure("unsafe_configuration_path", err)
	}
	if err := os.MkdirAll(configDirectory, 0o755); err != nil {
		return ConfigurationActionResult{}, configFailure("configuration_write_failed", err)
	}
	if _, err := m.validateConfigRoot(configDirectory); err != nil {
		return ConfigurationActionResult{}, err
	}
	path := filepath.Join(configDirectory, "zones.json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return ConfigurationActionResult{}, configFailure("configuration_already_initialized", err)
	}
	if err != nil {
		return ConfigurationActionResult{}, configFailure("configuration_write_failed", err)
	}
	content := []byte("{\n  \"global\": {\n    \"permissions\": {}\n  },\n  \"zones\": []\n}\n")
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return ConfigurationActionResult{}, configFailure("configuration_write_failed", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return ConfigurationActionResult{}, configFailure("configuration_write_failed", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return ConfigurationActionResult{}, configFailure("configuration_write_failed", err)
	}
	target := configTarget{scope: "adapter:palzones", root: root, path: path, relative: "Config/zones.json"}
	document, err := m.readConfigTarget(target)
	if err != nil {
		return ConfigurationActionResult{}, err
	}
	if err := m.store.SetKV(ctx, "pending_restart", "true"); err != nil {
		_ = os.Remove(path)
		return ConfigurationActionResult{}, configFailure("configuration_write_failed", err)
	}
	adapter, err := m.configurationAdapterByID(ctx, "palzones")
	if err != nil {
		return ConfigurationActionResult{}, err
	}
	return ConfigurationActionResult{Adapter: adapter, Document: &document, Changed: []string{"Config/zones.json"}, RestartRequired: true}, nil
}

func (m Manager) enableGliderRestoration(ctx context.Context) (ConfigurationActionResult, error) {
	mods, err := m.store.ListMods(ctx)
	if err != nil {
		return ConfigurationActionResult{}, err
	}
	byWorkshopID := installedWorkshopMods(mods)
	requiredIDs := []string{gliderRestorationWorkshopID, palSchemaWorkshopID, ue4ssWorkshopID}
	for _, workshopID := range requiredIDs {
		if _, ok := byWorkshopID[workshopID]; !ok {
			return ConfigurationActionResult{}, configFailure("mod_dependency_missing", fmt.Errorf("required Workshop Mod %s is not installed", workshopID))
		}
	}
	settingsPath := m.cfg.PalModSettingsPath()
	settings, settingsReadErr := ReadModSettings(settingsPath)
	if settingsReadErr != nil {
		return ConfigurationActionResult{}, configFailure("mod_enable_failed", settingsReadErr)
	}
	originalSettings, settingsErr := os.ReadFile(settingsPath)
	settingsExisted := settingsErr == nil
	if settingsErr != nil && !os.IsNotExist(settingsErr) {
		return ConfigurationActionResult{}, configFailure("mod_enable_failed", settingsErr)
	}
	originalEnabled := make(map[string]bool, len(requiredIDs))
	changed := make([]string, 0, len(requiredIDs))
	for _, workshopID := range requiredIDs {
		record := byWorkshopID[workshopID]
		originalEnabled[record.ID] = record.Enabled
		if !modEnabledInSettings(record, settings) {
			changed = append(changed, record.ID)
		}
		if record.Enabled {
			continue
		}
		if err := m.store.SetModEnabled(ctx, record.ID, true); err != nil {
			m.rollbackGliderEnable(ctx, originalEnabled, settingsPath, originalSettings, settingsExisted)
			return ConfigurationActionResult{}, configFailure("mod_enable_failed", err)
		}
	}
	if len(changed) == 0 {
		adapter, adapterErr := m.configurationAdapterByID(ctx, "glider-restoration")
		return ConfigurationActionResult{Adapter: adapter, Changed: []string{}, RestartRequired: false}, adapterErr
	}
	if err := m.rewriteActiveMods(ctx); err != nil {
		m.rollbackGliderEnable(ctx, originalEnabled, settingsPath, originalSettings, settingsExisted)
		return ConfigurationActionResult{}, configFailure("mod_enable_failed", err)
	}
	if err := m.store.SetKV(ctx, "pending_restart", "true"); err != nil {
		m.rollbackGliderEnable(ctx, originalEnabled, settingsPath, originalSettings, settingsExisted)
		return ConfigurationActionResult{}, configFailure("mod_enable_failed", err)
	}
	adapter, err := m.configurationAdapterByID(ctx, "glider-restoration")
	if err != nil {
		return ConfigurationActionResult{}, err
	}
	return ConfigurationActionResult{Adapter: adapter, Changed: changed, RestartRequired: true}, nil
}

func (m Manager) rollbackGliderEnable(ctx context.Context, enabled map[string]bool, settingsPath string, content []byte, existed bool) {
	for modID, value := range enabled {
		_ = m.store.SetModEnabled(ctx, modID, value)
	}
	if existed {
		_ = os.MkdirAll(filepath.Dir(settingsPath), 0o755)
		_ = os.WriteFile(settingsPath, content, 0o644)
	} else {
		_ = os.Remove(settingsPath)
	}
}

func (m Manager) configurationAdapterByID(ctx context.Context, id string) (ConfigurationAdapter, error) {
	adapters, err := m.ListConfigurations(ctx)
	if err != nil {
		return ConfigurationAdapter{}, err
	}
	for _, adapter := range adapters {
		if adapter.ID == id {
			return adapter, nil
		}
	}
	return ConfigurationAdapter{}, configFailure("configuration_adapter_not_found", os.ErrNotExist)
}

func validatePalZonesContent(content []byte) error {
	if err := validateTextSafety(content); err != nil {
		return err
	}
	var root map[string]any
	if err := json.Unmarshal(content, &root); err != nil {
		return configFailure("configuration_validation_failed", fmt.Errorf("root: invalid JSON: %w", err))
	}
	global, ok := root["global"].(map[string]any)
	if !ok {
		return palZonesValidationError("global", "must be an object")
	}
	if err := validatePalZonesPermissions("global.permissions", global["permissions"]); err != nil {
		return err
	}
	zones, ok := root["zones"].([]any)
	if !ok {
		return palZonesValidationError("zones", "must be an array")
	}
	for index, rawZone := range zones {
		path := fmt.Sprintf("zones[%d]", index)
		zone, ok := rawZone.(map[string]any)
		if !ok {
			return palZonesValidationError(path, "must be an object")
		}
		name, ok := zone["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			return palZonesValidationError(path+".name", "must not be empty")
		}
		points, ok := zone["points"].([]any)
		if !ok || len(points) < 3 {
			return palZonesValidationError(path+".points", "must contain at least three points")
		}
		for pointIndex, rawPoint := range points {
			pointPath := fmt.Sprintf("%s.points[%d]", path, pointIndex)
			point, ok := rawPoint.(map[string]any)
			if !ok {
				return palZonesValidationError(pointPath, "must be an object")
			}
			x, ok := palZonesNumber(point["x"])
			if !ok || x < -1099400 || x > 689148.5 {
				return palZonesValidationError(pointPath+".x", "must be a finite world coordinate")
			}
			y, ok := palZonesNumber(point["y"])
			if !ok || y < -818197 || y > 724400 {
				return palZonesValidationError(pointPath+".y", "must be a finite world coordinate")
			}
		}
		level, ok := palZonesNumber(zone["levelRequirement"])
		if !ok || level != math.Trunc(level) || level < 1 || level > 100 {
			return palZonesValidationError(path+".levelRequirement", "must be an integer from 1 to 100")
		}
		if err := validatePalZonesPermissions(path+".permissions", zone["permissions"]); err != nil {
			return err
		}
	}
	return nil
}

func validatePalZonesPermissions(path string, raw any) error {
	permissions, ok := raw.(map[string]any)
	if !ok {
		return palZonesValidationError(path, "must be an object")
	}
	validRoles := map[string]bool{"Player": true, "Otomo": true, "BaseCampPal": true, "PalMonster": true, "WildNPC": true}
	validActions := map[string]bool{"Build": true, "Dismantle": true, "Ride": true, "Fly": true, "SignEdit": true, "PasswordEdit": true, "Deteriorate": true}
	validTargets := map[string]bool{"Player": true, "Otomo": true, "BaseCampPal": true, "PalMonster": true, "WildNPC": true, "Structure": true}
	for role, rawRules := range permissions {
		rolePath := path + "." + role
		if !validRoles[role] {
			return palZonesValidationError(rolePath, "is not a supported role")
		}
		rules, ok := rawRules.(map[string]any)
		if !ok {
			return palZonesValidationError(rolePath, "must be an object")
		}
		for ruleName, rawValues := range rules {
			values, ok := rawValues.([]any)
			if !ok {
				return palZonesValidationError(rolePath+"."+ruleName, "must be an array")
			}
			for index, rawValue := range values {
				valuePath := fmt.Sprintf("%s.%s[%d]", rolePath, ruleName, index)
				switch strings.ToLower(ruleName) {
				case "world":
					action, ok := rawValue.(string)
					if !ok || !validActions[action] {
						return palZonesValidationError(valuePath, "is not a supported action")
					}
				case "damage":
					if target, ok := rawValue.(string); ok {
						if !validTargets[target] {
							return palZonesValidationError(valuePath, "is not a supported damage target")
						}
						continue
					}
					multiplier, ok := rawValue.(map[string]any)
					value, valid := palZonesNumber(multiplier["DamageMultiplier"])
					if !ok || !valid || value < 0 {
						return palZonesValidationError(valuePath, "must contain a non-negative DamageMultiplier")
					}
				default:
					return palZonesValidationError(rolePath+"."+ruleName, "is not a supported permission group")
				}
			}
		}
	}
	return nil
}

func palZonesNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	default:
		return 0, false
	}
}

func palZonesValidationError(path, message string) error {
	return configFailure("configuration_validation_failed", fmt.Errorf("%s: %s", path, message))
}
