package mods

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/docker"
)

func TestModConfigurationFilesWriteBackupRestoreAndRevision(t *testing.T) {
	manager, store, root := newConfigurationTestManager(t)
	modRoot := filepath.Join(root, "server", "Mods", "Workshop", "mod_one")
	writeConfigurationTestFile(t, filepath.Join(modRoot, "Config.json"), `{"Enabled":true,"Limit":10}`)
	writeConfigurationTestFile(t, filepath.Join(modRoot, "Info.json"), `{}`)
	writeConfigurationTestFile(t, filepath.Join(modRoot, "payload.dll"), "binary")
	if err := store.UpsertMod(t.Context(), db.Mod{ID: "mod_one", Name: "One", PackageName: "One", Path: modRoot}); err != nil {
		t.Fatal(err)
	}

	files, err := manager.ListModConfigFiles(t.Context(), "mod_one")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "Config.json" || len(files[0].ID) != 32 {
		t.Fatalf("unexpected editable files: %#v", files)
	}
	document, err := manager.GetModConfigFile(t.Context(), "mod_one", files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Fields) != 2 {
		t.Fatalf("expected JSON fields, got %#v", document.Fields)
	}
	written, err := manager.WriteModConfigFile(t.Context(), "mod_one", files[0].ID, ConfigWriteRequest{Content: `{"Enabled":false,"Limit":20}`, Revision: document.File.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if written.File.Revision == document.File.Revision {
		t.Fatal("revision did not change")
	}
	for _, pattern := range []string{".palpanel-config-*", "Config.json.palpanel-old-*"} {
		matches, globErr := filepath.Glob(filepath.Join(modRoot, pattern))
		if globErr != nil || len(matches) != 0 {
			t.Fatalf("temporary files for %q = %#v, %v", pattern, matches, globErr)
		}
	}
	if _, err := manager.WriteModConfigFile(t.Context(), "mod_one", files[0].ID, ConfigWriteRequest{Content: `{}`, Revision: document.File.Revision}); configurationErrorCode(err) != "configuration_revision_conflict" {
		t.Fatalf("stale write error = %v", err)
	}
	backups, err := manager.ListModConfigBackups(t.Context(), "mod_one", files[0].ID)
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %#v, %v", backups, err)
	}
	restored, err := manager.RestoreModConfigBackup(t.Context(), "mod_one", files[0].ID, backups[0].ID, written.File.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Content != document.Content {
		t.Fatalf("restored content = %q, want %q", restored.Content, document.Content)
	}
}

func TestConfigurationFieldLabelsUseChineseNamesAndKeepPaths(t *testing.T) {
	fields := configFields(".json", []byte(`{"preventAdminPasswordInChat":true,"baseRange":{"multiplier":2},"UnknownOption":3}`))
	labels := map[string]string{}
	for _, field := range fields {
		labels[field.Path] = field.Label
	}
	for path, want := range map[string]string{
		"preventAdminPasswordInChat": "防止管理员密码出现在聊天中",
		"baseRange.multiplier":       "基地范围倍率",
		"UnknownOption":              "自定义配置项",
	} {
		if labels[path] != want {
			t.Errorf("label for %s = %q, want %q", path, labels[path], want)
		}
	}
}

func TestModConfigurationConcurrentRevisionAllowsOnlyOneWriter(t *testing.T) {
	manager, store, root := newConfigurationTestManager(t)
	modRoot := filepath.Join(root, "server", "Mods", "Workshop", "mod_race")
	writeConfigurationTestFile(t, filepath.Join(modRoot, "Config.json"), `{"Value":0}`)
	if err := store.UpsertMod(t.Context(), db.Mod{ID: "mod_race", Name: "Race", PackageName: "Race", Path: modRoot}); err != nil {
		t.Fatal(err)
	}
	files, err := manager.ListModConfigFiles(t.Context(), "mod_race")
	if err != nil || len(files) != 1 {
		t.Fatalf("files = %#v, %v", files, err)
	}
	document, err := manager.GetModConfigFile(t.Context(), "mod_race", files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, content := range []string{`{"Value":1}`, `{"Value":2}`} {
		go func(content string) {
			ready.Done()
			<-start
			_, writeErr := manager.WriteModConfigFile(t.Context(), "mod_race", files[0].ID, ConfigWriteRequest{Content: content, Revision: document.File.Revision})
			results <- writeErr
		}(content)
	}
	ready.Wait()
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		if writeErr := <-results; writeErr == nil {
			successes++
		} else if configurationErrorCode(writeErr) == "configuration_revision_conflict" {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent write error: %v", writeErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes = %d, conflicts = %d", successes, conflicts)
	}
	backups, err := manager.ListModConfigBackups(t.Context(), "mod_race", files[0].ID)
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %#v, %v", backups, err)
	}
}

func TestModConfigurationRejectsLuaWithoutConfirmationAndInvalidText(t *testing.T) {
	manager, store, root := newConfigurationTestManager(t)
	modRoot := filepath.Join(root, "server", "Mods", "Workshop", "mod_lua")
	writeConfigurationTestFile(t, filepath.Join(modRoot, "Scripts", "main.lua"), "Range = 100\n")
	writeConfigurationTestFile(t, filepath.Join(modRoot, "bad.txt"), string([]byte{'a', 0, 'b'}))
	if err := store.UpsertMod(t.Context(), db.Mod{ID: "mod_lua", Name: "Lua", PackageName: "Lua", Path: modRoot}); err != nil {
		t.Fatal(err)
	}
	files, err := manager.ListModConfigFiles(t.Context(), "mod_lua")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !files[0].Executable {
		t.Fatalf("files = %#v", files)
	}
	document, err := manager.GetModConfigFile(t.Context(), "mod_lua", files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.WriteModConfigFile(t.Context(), "mod_lua", files[0].ID, ConfigWriteRequest{Content: "Range = 200\n", Revision: document.File.Revision})
	if configurationErrorCode(err) != "executable_confirmation_required" {
		t.Fatalf("Lua write error = %v", err)
	}
	_, err = manager.WriteModConfigFile(t.Context(), "mod_lua", files[0].ID, ConfigWriteRequest{Content: "Range = 200\n", Revision: document.File.Revision, ConfirmExecutable: true})
	if err != nil {
		t.Fatal(err)
	}
}

func TestModConfigurationRejectsOversizedInvalidJSONAndLinks(t *testing.T) {
	manager, store, root := newConfigurationTestManager(t)
	modRoot := filepath.Join(root, "server", "Mods", "Workshop", "mod_safe")
	writeConfigurationTestFile(t, filepath.Join(modRoot, "Config.json"), `{}`)
	writeConfigurationTestFile(t, filepath.Join(modRoot, "large.ini"), strings.Repeat("x", int(MaxConfigFileBytes)+1))
	outside := filepath.Join(root, "outside.json")
	writeConfigurationTestFile(t, outside, `{"outside":true}`)
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(modRoot, "linked.json")); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpsertMod(t.Context(), db.Mod{ID: "mod_safe", Name: "Safe", PackageName: "Safe", Path: modRoot}); err != nil {
		t.Fatal(err)
	}
	files, err := manager.ListModConfigFiles(t.Context(), "mod_safe")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "Config.json" {
		t.Fatalf("unsafe files were exposed: %#v", files)
	}
	document, err := manager.GetModConfigFile(t.Context(), "mod_safe", files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.WriteModConfigFile(t.Context(), "mod_safe", files[0].ID, ConfigWriteRequest{Content: `{`, Revision: document.File.Revision})
	if configurationErrorCode(err) != "configuration_parse_failed" {
		t.Fatalf("invalid JSON error = %v", err)
	}
}

func TestModConfigurationValidatesTOMLINIAndCFG(t *testing.T) {
	for _, test := range []struct {
		name, filename, initial, invalid string
	}{
		{name: "toml", filename: "settings.toml", initial: "enabled = true\n", invalid: "enabled = [\n"},
		{name: "ini", filename: "settings.ini", initial: "[General]\nenabled=true\n", invalid: "[General\nenabled=true\n"},
		{name: "cfg", filename: "settings.cfg", initial: "enabled: true\n", invalid: "missing assignment\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager, store, root := newConfigurationTestManager(t)
			modID := "mod_" + test.name
			modRoot := filepath.Join(root, "server", "Mods", "Workshop", modID)
			writeConfigurationTestFile(t, filepath.Join(modRoot, test.filename), test.initial)
			if err := store.UpsertMod(t.Context(), db.Mod{ID: modID, Name: test.name, PackageName: test.name, Path: modRoot}); err != nil {
				t.Fatal(err)
			}
			files, err := manager.ListModConfigFiles(t.Context(), modID)
			if err != nil || len(files) != 1 {
				t.Fatalf("files = %#v, %v", files, err)
			}
			_, err = manager.WriteModConfigFile(t.Context(), modID, files[0].ID, ConfigWriteRequest{Content: test.invalid, Revision: files[0].Revision})
			if configurationErrorCode(err) != "configuration_parse_failed" {
				t.Fatalf("invalid %s error = %v", test.name, err)
			}
		})
	}
}

func TestModConfigurationRejectsDatabasePathOutsideManagedWorkshopMod(t *testing.T) {
	manager, store, root := newConfigurationTestManager(t)
	secretRoot := filepath.Join(root, "data", "secrets")
	writeConfigurationTestFile(t, filepath.Join(secretRoot, "token.json"), `{"token":"secret"}`)
	if err := store.UpsertMod(t.Context(), db.Mod{ID: "mod_untrusted", Name: "Untrusted", PackageName: "Untrusted", Path: secretRoot}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ListModConfigFiles(t.Context(), "mod_untrusted"); configurationErrorCode(err) != "unsafe_configuration_path" {
		t.Fatalf("untrusted database path error = %v", err)
	}
}

func TestModConfigurationRejectsLinkedBackupDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows junction coverage is in configuration_windows_test.go")
	}
	manager, store, root := newConfigurationTestManager(t)
	modRoot := filepath.Join(root, "server", "Mods", "Workshop", "mod_backup_link")
	writeConfigurationTestFile(t, filepath.Join(modRoot, "Config.json"), `{}`)
	if err := store.UpsertMod(t.Context(), db.Mod{ID: "mod_backup_link", Name: "Backup", PackageName: "Backup", Path: modRoot}); err != nil {
		t.Fatal(err)
	}
	files, err := manager.ListModConfigFiles(t.Context(), "mod_backup_link")
	if err != nil || len(files) != 1 {
		t.Fatalf("files = %#v, %v", files, err)
	}
	target, err := manager.resolveModFileTarget(t.Context(), "mod_backup_link", files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	backupDir := manager.backupDirectory(target)
	outside := filepath.Join(root, "outside-backups")
	if err := os.MkdirAll(filepath.Dir(backupDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, backupDir); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ListModConfigBackups(t.Context(), "mod_backup_link", files[0].ID); configurationErrorCode(err) != "configuration_backup_read_failed" {
		t.Fatalf("linked backup directory error = %v", err)
	}
}

func TestKnownModIdentityRequiresExactNormalizedMatch(t *testing.T) {
	if !knownModIdentityMatches("Extended Base Range", "", "extended base range") {
		t.Fatal("expected canonical identity match")
	}
	if knownModIdentityMatches("Extended Base Range Evil", "", "extended base range") || knownModIdentityMatches("NotPalSchema", "", "palschema") {
		t.Fatal("substring identity was accepted")
	}
}

func TestDedicatedConfigurationAdapters(t *testing.T) {
	manager, store, root := newConfigurationTestManager(t)
	writeConfigurationTestFile(t, filepath.Join(manager.cfg.PalDefenderDir(), "Config.json"), `{"BanListEnabled":true}`)
	writeConfigurationTestFile(t, filepath.Join(manager.cfg.Win64Dir(), "UE4SS-settings.ini"), "[General]\nbUseUObjectArrayCache=true\n")
	writeConfigurationTestFile(t, filepath.Join(manager.cfg.Win64Dir(), "Mods", "mods.txt"), "PalSchema : 1\n")
	palschemaRoot := filepath.Join(root, "server", "Mods", "Workshop", "palschema")
	extendedRoot := filepath.Join(root, "server", "Mods", "Workshop", "extended")
	qualityOfLifeRoot := filepath.Join(root, "server", "Mods", "Workshop", "quality-of-life")
	writeConfigurationTestFile(t, filepath.Join(palschemaRoot, "settings.json"), `{"Enabled":true}`)
	writeConfigurationTestFile(t, filepath.Join(extendedRoot, "Scripts", "main.lua"), "BaseRange = 1200\n")
	writeConfigurationTestFile(t, filepath.Join(qualityOfLifeRoot, "Mods", "NativeMods", "UE4SS", "Mods", "QualityOfLife", "qualityoflifeCONFIG.JSON"), `{"baseRange":{"multiplier":1.5},"workEfficiency":{"multiplier":4}}`)
	writeConfigurationTestFile(t, filepath.Join(qualityOfLifeRoot, "Mods", "NativeMods", "UE4SS", "Mods", "QualityOfLife", "generated.json"), `{"ignored":true}`)
	for _, record := range []db.Mod{
		{ID: "palschema", Name: "PalSchema", PackageName: "PalSchema", WorkshopID: "3625280368", Path: palschemaRoot},
		{ID: "extended", Name: "Extended Base Range", PackageName: "ExtendedBaseRange", WorkshopID: "3625907101", Path: extendedRoot},
		{ID: "quality-of-life", Name: "QualityOfLife", PackageName: "QualityOfLife", WorkshopID: "3761921027", Path: qualityOfLifeRoot},
	} {
		if err := store.UpsertMod(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	adapters, err := manager.ListConfigurations(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(adapters) != 7 {
		t.Fatalf("adapters = %#v", adapters)
	}
	for _, adapter := range adapters[:5] {
		if !adapter.Available || len(adapter.Files) == 0 {
			t.Fatalf("adapter unavailable: %#v", adapter)
		}
	}
	extended := adapters[3]
	document, err := manager.GetConfiguration(t.Context(), extended.ID, extended.Files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Fields) != 1 || document.Fields[0].Path != "BaseRange" {
		t.Fatalf("extended fields = %#v", document.Fields)
	}
	qualityOfLife := adapters[4]
	if len(qualityOfLife.Files) != 1 || !strings.EqualFold(qualityOfLife.Files[0].Name, "QualityOfLifeConfig.json") {
		t.Fatalf("quality-of-life files = %#v", qualityOfLife.Files)
	}
	qualityDocument, err := manager.GetConfiguration(t.Context(), qualityOfLife.ID, qualityOfLife.Files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(qualityDocument.Fields) != 2 || qualityDocument.Fields[0].Path != "baseRange.multiplier" || qualityDocument.Fields[1].Path != "workEfficiency.multiplier" {
		t.Fatalf("quality-of-life fields = %#v", qualityDocument.Fields)
	}
	if _, err := manager.WriteConfiguration(t.Context(), qualityOfLife.ID, qualityOfLife.Files[0].ID, ConfigWriteRequest{
		Content:  `{"baseRange":`,
		Revision: qualityDocument.File.Revision,
	}); configurationErrorCode(err) != "configuration_parse_failed" {
		t.Fatalf("invalid quality-of-life JSON error = %v", err)
	}
}

func TestPalZonesAdapterInitializesAndValidatesConfiguration(t *testing.T) {
	manager, _, _ := newConfigurationTestManager(t)
	palZonesRoot := filepath.Join(manager.cfg.Win64Dir(), "UE4SS", "Mods", "PalZones")
	if err := os.MkdirAll(palZonesRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := findConfigurationAdapter(t, manager, "palzones")
	if !adapter.Installed || adapter.Configured || adapter.Status != ConfigurationStatusNotConfigured {
		t.Fatalf("unexpected PalZones status: %#v", adapter)
	}
	if len(adapter.Actions) != 1 || adapter.Actions[0].ID != ConfigurationActionInitialize || !adapter.Actions[0].Available {
		t.Fatalf("unexpected PalZones actions: %#v", adapter.Actions)
	}

	result, err := manager.RunConfigurationAction(t.Context(), "palzones", ConfigurationActionRequest{Action: ConfigurationActionInitialize})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document == nil || result.Document.File.Path != "Config/zones.json" || !result.RestartRequired {
		t.Fatalf("unexpected initialize result: %#v", result)
	}
	if result.Adapter.Status != ConfigurationStatusRestartRequired || !result.Adapter.Configured {
		t.Fatalf("unexpected initialized adapter: %#v", result.Adapter)
	}
	if _, err := manager.RunConfigurationAction(t.Context(), "palzones", ConfigurationActionRequest{Action: ConfigurationActionInitialize}); configurationErrorCode(err) != "configuration_already_initialized" {
		t.Fatalf("repeated initialize error = %v", err)
	}

	valid := `{"global":{"permissions":{"Player":{"world":["Build"],"damage":["Player",{"DamageMultiplier":1}]}}},"zones":[{"name":"出生点","points":[{"x":"1.00","y":"2.00"},{"x":"3.00","y":"4.00"},{"x":"5.00","y":"6.00"}],"permissions":{},"levelRequirement":1}]}`
	document := result.Document
	written, err := manager.WriteConfiguration(t.Context(), "palzones", document.File.ID, ConfigWriteRequest{Content: valid, Revision: document.File.Revision})
	if err != nil || !strings.Contains(written.Content, "出生点") {
		t.Fatalf("valid PalZones write = %#v, %v", written, err)
	}

	invalidCases := []struct {
		name, content, path string
	}{
		{name: "empty name", content: `{"global":{"permissions":{}},"zones":[{"name":"","points":[{"x":1,"y":2},{"x":3,"y":4},{"x":5,"y":6}],"permissions":{},"levelRequirement":1}]}`, path: "zones[0].name"},
		{name: "too few points", content: `{"global":{"permissions":{}},"zones":[{"name":"A","points":[{"x":1,"y":2},{"x":3,"y":4}],"permissions":{},"levelRequirement":1}]}`, path: "zones[0].points"},
		{name: "level", content: `{"global":{"permissions":{}},"zones":[{"name":"A","points":[{"x":1,"y":2},{"x":3,"y":4},{"x":5,"y":6}],"permissions":{},"levelRequirement":101}]}`, path: "zones[0].levelRequirement"},
		{name: "permission", content: `{"global":{"permissions":{}},"zones":[{"name":"A","points":[{"x":1,"y":2},{"x":3,"y":4},{"x":5,"y":6}],"permissions":{"Player":{"world":["Exploit"]}},"levelRequirement":1}]}`, path: "zones[0].permissions.Player.world[0]"},
	}
	for _, test := range invalidCases {
		t.Run(test.name, func(t *testing.T) {
			_, err := manager.WriteConfiguration(t.Context(), "palzones", written.File.ID, ConfigWriteRequest{Content: test.content, Revision: written.File.Revision})
			if configurationErrorCode(err) != "configuration_validation_failed" || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("validation error = %v, want path %s", err, test.path)
			}
		})
	}
}

func TestGliderRestorationAdapterDiagnosesDependenciesAndEnablesTogether(t *testing.T) {
	manager, store, root := newConfigurationTestManager(t)
	records := []db.Mod{
		{ID: "glider", Name: "Glider Restoration", PackageName: "GliderRestore", WorkshopID: "3625871847", Path: filepath.Join(root, "server", "Mods", "Workshop", "glider")},
		{ID: "palschema", Name: "PalSchema", PackageName: "PalSchema", WorkshopID: "3625280368", Path: filepath.Join(root, "server", "Mods", "Workshop", "palschema")},
		{ID: "ue4ss", Name: "UE4SS Experimental", PackageName: "UE4SS", WorkshopID: "3625223587", Path: filepath.Join(root, "server", "Mods", "Workshop", "ue4ss")},
	}
	for _, record := range records {
		if err := os.MkdirAll(record.Path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertMod(t.Context(), record); err != nil {
			t.Fatal(err)
		}
	}

	adapter := findConfigurationAdapter(t, manager, "glider-restoration")
	if adapter.Status != ConfigurationStatusDisabled || len(adapter.Dependencies) != 2 {
		t.Fatalf("unexpected Glider status: %#v", adapter)
	}
	result, err := manager.RunConfigurationAction(t.Context(), "glider-restoration", ConfigurationActionRequest{Action: ConfigurationActionEnable})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RestartRequired || result.Adapter.Status != ConfigurationStatusRestartRequired || len(result.Changed) != 3 {
		t.Fatalf("unexpected enable result: %#v", result)
	}
	settings, err := ReadModSettings(manager.cfg.PalModSettingsPath())
	if err != nil || !settings.GlobalEnabled {
		t.Fatalf("settings = %#v, %v", settings, err)
	}
	for _, record := range records {
		stored, getErr := store.GetMod(t.Context(), record.ID)
		if getErr != nil || !stored.Enabled || !containsFold(settings.ActiveMods, record.PackageName) {
			t.Fatalf("mod %s not enabled: %#v, settings=%#v, err=%v", record.ID, stored, settings, getErr)
		}
	}
}

func TestGliderRestorationEnableRejectsMissingDependency(t *testing.T) {
	manager, store, root := newConfigurationTestManager(t)
	path := filepath.Join(root, "server", "Mods", "Workshop", "glider")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertMod(t.Context(), db.Mod{ID: "glider", Name: "Glider Restoration", PackageName: "GliderRestore", WorkshopID: "3625871847", Path: path}); err != nil {
		t.Fatal(err)
	}
	adapter := findConfigurationAdapter(t, manager, "glider-restoration")
	if adapter.Status != ConfigurationStatusDependencyMissing {
		t.Fatalf("unexpected status: %#v", adapter)
	}
	if _, err := manager.RunConfigurationAction(t.Context(), "glider-restoration", ConfigurationActionRequest{Action: ConfigurationActionEnable}); configurationErrorCode(err) != "mod_dependency_missing" {
		t.Fatalf("enable error = %v", err)
	}
}

func TestConfigurationFieldMetadataUsesChineseFallback(t *testing.T) {
	fields := configFields(".json", []byte(`{"UnknownOption":3,"Enabled":true}`))
	byPath := map[string]ConfigurationField{}
	for _, field := range fields {
		byPath[field.Path] = field
	}
	if byPath["Enabled"].Label != "启用" || byPath["Enabled"].Group == "" {
		t.Fatalf("known metadata = %#v", byPath["Enabled"])
	}
	if byPath["UnknownOption"].Label != "自定义配置项" || byPath["UnknownOption"].Description == "" {
		t.Fatalf("fallback metadata = %#v", byPath["UnknownOption"])
	}
}

func findConfigurationAdapter(t *testing.T, manager Manager, id string) ConfigurationAdapter {
	t.Helper()
	adapters, err := manager.ListConfigurations(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, adapter := range adapters {
		if adapter.ID == id {
			return adapter
		}
	}
	t.Fatalf("adapter %s not found in %#v", id, adapters)
	return ConfigurationAdapter{}
}

func newConfigurationTestManager(t *testing.T) (Manager, *db.Store, string) {
	t.Helper()
	root := t.TempDir()
	cfg := appconfig.Config{
		RuntimeRoot: root, DataDir: filepath.Join(root, "data"), ServerDir: filepath.Join(root, "server"),
		UploadsDir: filepath.Join(root, "uploads"), BackupsDir: filepath.Join(root, "backups"),
		UE4SSDir: filepath.Join(root, "ue4ss"), DBPath: filepath.Join(root, "test.db"),
	}
	for _, path := range []string{cfg.DataDir, cfg.ServerDir, cfg.UploadsDir, cfg.BackupsDir, cfg.UE4SSDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewManager(cfg, store, docker.NewRunner(cfg)), store, root
}

func writeConfigurationTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func configurationErrorCode(err error) string {
	var target ConfigurationError
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}
