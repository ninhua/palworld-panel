package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"palpanel/internal/pallocalize"
	"palpanel/internal/playerpresence"
	"palpanel/internal/startergift"
)

func init() {
	patchFeatures = append(patchFeatures, "new-player-starter-gift")
}

const (
	maxStarterGiftTemplateJSONBytes      int64 = 1 << 20
	maxStarterGiftTemplateIndexJSONBytes int64 = 4 << 20
	maxStarterGiftTemplateIndexes              = 64
	maxStarterGiftTemplateIndexEntries         = 20000
)

type starterGiftTemplateInfo struct {
	Name                 string   `json:"name"`
	PalID                string   `json:"pal_id,omitempty"`
	PalName              string   `json:"pal_name,omitempty"`
	EnglishName          string   `json:"english_name,omitempty"`
	Category             string   `json:"category,omitempty"`
	UsageCategory        string   `json:"usage_category,omitempty"`
	OverallGrade         string   `json:"overall_grade,omitempty"`
	IndexNames           []string `json:"index_names,omitempty"`
	ClassificationTags   []string `json:"classification_tags,omitempty"`
	GraduationPal        bool     `json:"graduation_pal,omitempty"`
	GraduationUses       []string `json:"graduation_uses,omitempty"`
	CurrentGraduationUse bool     `json:"current_graduation_use,omitempty"`
	Transitional         bool     `json:"transitional,omitempty"`
	PassiveNames         []string `json:"passive_names,omitempty"`
	NightWorkMode        string   `json:"night_work_mode,omitempty"`
	Nickname             string   `json:"nickname,omitempty"`
	Level                int      `json:"level,omitempty"`
	ModifiedAt           string   `json:"modified_at,omitempty"`
	Size                 int64    `json:"size,omitempty"`
	ParseError           string   `json:"parse_error,omitempty"`
}

type starterGiftTemplateIndexInfo struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type starterGiftTemplateDocument struct {
	PalID    string `json:"PalID"`
	Nickname string `json:"Nickname"`
	Level    int    `json:"Level"`
}

type starterGiftTemplateIndexEntryDocument struct {
	CategoryCN             string   `json:"分类"`
	Category               string   `json:"category"`
	TemplateNameCN         string   `json:"模板名"`
	TemplateName           string   `json:"template_name"`
	FileNameCN             string   `json:"文件名"`
	FileName               string   `json:"file_name"`
	ChineseNameCN          string   `json:"中文名"`
	ChineseName            string   `json:"pal_name"`
	PalID                  string   `json:"PalID"`
	EnglishNameCN          string   `json:"英文名"`
	EnglishName            string   `json:"english_name"`
	UsageCategoryCN        string   `json:"用途分类"`
	UsageCategory          string   `json:"usage_category"`
	OverallGradeCN         string   `json:"综合分级"`
	OverallGrade           string   `json:"overall_grade"`
	ClassificationTagsCN   []string `json:"分类标签"`
	ClassificationTags     []string `json:"classification_tags"`
	GraduationPalCN        bool     `json:"毕业帕鲁"`
	GraduationPal          bool     `json:"graduation_pal"`
	GraduationUsesCN       []string `json:"毕业用途"`
	GraduationUses         []string `json:"graduation_uses"`
	CurrentGraduationUseCN bool     `json:"当前模板是否毕业用途"`
	CurrentGraduationUse   bool     `json:"current_graduation_use"`
	TransitionalCN         bool     `json:"低阶与过渡"`
	Transitional           bool     `json:"transitional"`
	PassiveNamesCN         []string `json:"词条中文"`
	PassiveNames           []string `json:"passive_names"`
	NightWorkModeCN        string   `json:"夜间工作方式"`
	NightWorkMode          string   `json:"night_work_mode"`
}

type starterGiftTemplateIndexEntry struct {
	Category             string
	TemplateName         string
	FileName             string
	ChineseName          string
	PalID                string
	EnglishName          string
	UsageCategory        string
	OverallGrade         string
	ClassificationTags   []string
	GraduationPal        bool
	GraduationUses       []string
	CurrentGraduationUse bool
	Transitional         bool
	PassiveNames         []string
	NightWorkMode        string
}

type starterGiftTemplateIndexMatch struct {
	IndexName string
	Entry     starterGiftTemplateIndexEntry
}

type starterGiftTemplateIndexes struct {
	Infos     []starterGiftTemplateIndexInfo
	FileNames map[string]bool
	Matches   map[string][]starterGiftTemplateIndexMatch
}

func starterGiftTemplateCatalog(serverDir string, names []string) ([]starterGiftTemplateInfo, []starterGiftTemplateIndexInfo) {
	indexes := readStarterGiftTemplateIndexes(serverDir)
	items := make([]starterGiftTemplateInfo, 0, len(names))
	for _, name := range names {
		fileName, err := starterGiftTemplateFileName(strings.TrimSpace(name))
		if err == nil && indexes.FileNames[starterGiftTemplateLookupKey(fileName)] {
			continue
		}
		items = append(items, readStarterGiftTemplateInfo(serverDir, name, indexes))
	}
	return items, indexes.Infos
}

func readStarterGiftTemplateIndexes(serverDir string) starterGiftTemplateIndexes {
	result := starterGiftTemplateIndexes{
		FileNames: map[string]bool{},
		Matches:   map[string][]starterGiftTemplateIndexMatch{},
	}
	templateDir := filepath.Join(serverDir, "Pal", "Binaries", "Win64", "PalDefender", "Pals", "Templates")
	entries, err := os.ReadDir(templateDir)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if len(result.Infos) >= maxStarterGiftTemplateIndexes || !starterGiftTemplateIndexCandidate(entry.Name()) {
			continue
		}
		path := filepath.Join(templateDir, entry.Name())
		raw, _, err := readStarterGiftRegularJSON(path, maxStarterGiftTemplateIndexJSONBytes)
		if err != nil {
			continue
		}
		label, indexed := parseStarterGiftTemplateIndex(raw)
		if len(indexed) == 0 {
			continue
		}
		if len(indexed) > maxStarterGiftTemplateIndexEntries {
			indexed = indexed[:maxStarterGiftTemplateIndexEntries]
		}
		if label == "" {
			label = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		}
		result.Infos = append(result.Infos, starterGiftTemplateIndexInfo{Name: entry.Name(), Label: label, Count: len(indexed)})
		result.FileNames[starterGiftTemplateLookupKey(entry.Name())] = true
		for _, indexedEntry := range indexed {
			match := starterGiftTemplateIndexMatch{IndexName: entry.Name(), Entry: indexedEntry}
			for _, value := range []string{indexedEntry.FileName, indexedEntry.TemplateName} {
				for _, key := range starterGiftTemplateLookupKeys(value) {
					result.Matches[key] = append(result.Matches[key], match)
				}
			}
		}
	}
	return result
}

func starterGiftTemplateIndexCandidate(name string) bool {
	if !strings.EqualFold(filepath.Ext(name), ".json") {
		return false
	}
	lower := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	return strings.Contains(lower, "index") || strings.Contains(lower, "索引")
}

func parseStarterGiftTemplateIndex(raw []byte) (string, []starterGiftTemplateIndexEntry) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err == nil && root != nil {
		label := firstStarterGiftIndexString(root, "名称", "标题", "版本", "label", "name")
		for _, key := range []string{"模板", "模板索引", "清单", "列表", "templates", "items"} {
			payload, found := root[key]
			if !found {
				continue
			}
			if entries := decodeStarterGiftTemplateIndexEntries(payload); len(entries) > 0 {
				return label, entries
			}
		}
	}
	return "", decodeStarterGiftTemplateIndexEntries(raw)
}

func firstStarterGiftIndexString(root map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var value string
		if raw, ok := root[key]; ok && json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func decodeStarterGiftTemplateIndexEntries(raw []byte) []starterGiftTemplateIndexEntry {
	var documents []starterGiftTemplateIndexEntryDocument
	if err := json.Unmarshal(raw, &documents); err != nil {
		return nil
	}
	entries := make([]starterGiftTemplateIndexEntry, 0, len(documents))
	for _, document := range documents {
		entry := starterGiftTemplateIndexEntry{
			Category:             starterGiftFirstNonEmpty(document.CategoryCN, document.Category),
			TemplateName:         starterGiftFirstNonEmpty(document.TemplateNameCN, document.TemplateName),
			FileName:             starterGiftFirstNonEmpty(document.FileNameCN, document.FileName),
			ChineseName:          starterGiftFirstNonEmpty(document.ChineseNameCN, document.ChineseName),
			PalID:                strings.TrimSpace(document.PalID),
			EnglishName:          starterGiftFirstNonEmpty(document.EnglishNameCN, document.EnglishName),
			UsageCategory:        starterGiftFirstNonEmpty(document.UsageCategoryCN, document.UsageCategory),
			OverallGrade:         starterGiftFirstNonEmpty(document.OverallGradeCN, document.OverallGrade),
			ClassificationTags:   starterGiftUniqueStrings(append(document.ClassificationTagsCN, document.ClassificationTags...)),
			GraduationPal:        document.GraduationPalCN || document.GraduationPal,
			GraduationUses:       starterGiftUniqueStrings(append(document.GraduationUsesCN, document.GraduationUses...)),
			CurrentGraduationUse: document.CurrentGraduationUseCN || document.CurrentGraduationUse,
			Transitional:         document.TransitionalCN || document.Transitional,
			PassiveNames:         starterGiftUniqueStrings(append(document.PassiveNamesCN, document.PassiveNames...)),
			NightWorkMode:        starterGiftFirstNonEmpty(document.NightWorkModeCN, document.NightWorkMode),
		}
		if entry.FileName == "" && entry.TemplateName == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func starterGiftFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func starterGiftUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func starterGiftTemplateLookupKey(value string) string {
	keys := starterGiftTemplateLookupKeys(value)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func starterGiftTemplateLookupKeys(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) {
		return nil
	}
	lower := strings.ToLower(value)
	withoutExtension := strings.TrimSuffix(lower, ".json")
	if lower == withoutExtension {
		return []string{lower, lower + ".json"}
	}
	return []string{lower, withoutExtension}
}

func readStarterGiftTemplateInfo(serverDir, name string, indexes starterGiftTemplateIndexes) starterGiftTemplateInfo {
	name = strings.TrimSpace(name)
	result := starterGiftTemplateInfo{Name: name}
	fileName, err := starterGiftTemplateFileName(name)
	if err != nil {
		result.ParseError = err.Error()
		return result
	}
	templateDir := filepath.Join(serverDir, "Pal", "Binaries", "Win64", "PalDefender", "Pals", "Templates")
	raw, info, err := readStarterGiftRegularJSON(filepath.Join(templateDir, fileName), maxStarterGiftTemplateJSONBytes)
	if err != nil {
		result.ParseError = err.Error()
		return result
	}
	result.ModifiedAt = info.ModTime().UTC().Format(time.RFC3339)
	result.Size = info.Size()
	var document starterGiftTemplateDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		result.ParseError = "invalid template JSON: " + err.Error()
		return result
	}
	result.PalID = strings.TrimSpace(document.PalID)
	result.Nickname = strings.TrimSpace(document.Nickname)
	result.Level = document.Level
	if result.PalID == "" {
		result.ParseError = "template JSON does not contain PalID"
		return result
	}
	seenIndexes := map[string]bool{}
	for _, key := range starterGiftTemplateLookupKeys(fileName) {
		for _, match := range indexes.Matches[key] {
			if seenIndexes[match.IndexName] {
				continue
			}
			seenIndexes[match.IndexName] = true
			result.IndexNames = append(result.IndexNames, match.IndexName)
			if result.PalName == "" {
				result.PalName = match.Entry.ChineseName
			}
			if result.EnglishName == "" {
				result.EnglishName = match.Entry.EnglishName
			}
			if result.Category == "" {
				result.Category = match.Entry.Category
			}
			if result.UsageCategory == "" {
				result.UsageCategory = match.Entry.UsageCategory
			}
			if result.OverallGrade == "" {
				result.OverallGrade = match.Entry.OverallGrade
			}
			result.ClassificationTags = starterGiftUniqueStrings(append(result.ClassificationTags, match.Entry.ClassificationTags...))
			result.GraduationPal = result.GraduationPal || match.Entry.GraduationPal
			result.GraduationUses = starterGiftUniqueStrings(append(result.GraduationUses, match.Entry.GraduationUses...))
			result.CurrentGraduationUse = result.CurrentGraduationUse || match.Entry.CurrentGraduationUse
			result.Transitional = result.Transitional || match.Entry.Transitional
			result.PassiveNames = starterGiftUniqueStrings(append(result.PassiveNames, match.Entry.PassiveNames...))
			if result.NightWorkMode == "" {
				result.NightWorkMode = match.Entry.NightWorkMode
			}
		}
	}
	return result
}

func readStarterGiftRegularJSON(path string, maxBytes int64) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("JSON file is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("JSON path is not a regular file")
	}
	if info.Size() > maxBytes {
		return nil, nil, fmt.Errorf("JSON exceeds %d bytes", maxBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("JSON file cannot be opened")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("JSON cannot be read")
	}
	if int64(len(raw)) > maxBytes {
		return nil, nil, fmt.Errorf("JSON exceeds %d bytes", maxBytes)
	}
	return raw, info, nil
}

func starterGiftTemplateFileName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("template name is empty")
	}
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("template name is invalid")
	}
	fileName := name
	if filepath.Ext(fileName) == "" {
		fileName += ".json"
	}
	if !strings.EqualFold(filepath.Ext(fileName), ".json") {
		return "", fmt.Errorf("template file must use the .json extension")
	}
	return fileName, nil
}

func (s Server) currentStarterGiftScope() (playerpresence.Scope, error) {
	return playerpresence.ResolveServerScope(s.cfg.ServerDirectory())
}

func (s Server) starterGiftConfig(c *gin.Context) {
	scope, err := s.currentStarterGiftScope()
	if err != nil {
		fail(c, http.StatusConflict, "starter_gift_world_unavailable", err.Error())
		return
	}
	snapshot, err := startergift.LoadSnapshot(c.Request.Context(), s.store, scope)
	if err != nil {
		fail(c, http.StatusInternalServerError, "starter_gift_read_failed", err.Error())
		return
	}
	ok(c, s.starterGiftResponse(c.Request.Context(), snapshot))
}

func (s Server) putStarterGiftConfig(c *gin.Context) {
	scope, err := s.currentStarterGiftScope()
	if err != nil {
		fail(c, http.StatusConflict, "starter_gift_world_unavailable", err.Error())
		return
	}
	var config startergift.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	validated, err := startergift.ValidateConfig(config)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid_starter_gift_config", err.Error())
		return
	}
	config = validated
	if !s.canonicalizeStarterGiftTemplates(c, &config) {
		return
	}
	current, err := startergift.LoadSnapshot(c.Request.Context(), s.store, scope)
	if err != nil {
		fail(c, http.StatusInternalServerError, "starter_gift_read_failed", err.Error())
		return
	}
	if config.Enabled && !current.Config.Enabled {
		index, status, indexErr := s.serverSaveIndex.Current(c.Request.Context())
		if indexErr == nil && status.State == "not_indexed" {
			index, status, indexErr = s.serverSaveIndex.Rebuild(c.Request.Context())
		}
		if indexErr != nil && status.State != "missing" {
			fail(c, http.StatusConflict, "starter_gift_baseline_unavailable", "Cannot safely enable starter gifts until the server save index is available: "+indexErr.Error())
			return
		}
		known := make([]playerpresence.OnlinePlayer, 0, len(index.Players))
		for _, player := range index.Players {
			known = append(known, playerpresence.OnlinePlayer{PlayerUID: player.PlayerUID, SteamID: player.SteamID, Nickname: player.Nickname})
		}
		if err := startergift.BaselineKnownPlayers(c.Request.Context(), s.store, scope, known); err != nil {
			fail(c, http.StatusInternalServerError, "starter_gift_baseline_failed", err.Error())
			return
		}
	}
	snapshot, err := startergift.SaveConfig(c.Request.Context(), s.store, scope, config)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid_starter_gift_config", err.Error())
		return
	}
	ok(c, s.starterGiftResponse(c.Request.Context(), snapshot))
}

func (s Server) canonicalizeStarterGiftTemplates(c *gin.Context, config *startergift.Config) bool {
	if len(config.PalTemplates) == 0 {
		return true
	}
	templates, err := s.defender.ListPalTemplates()
	if err != nil {
		fail(c, http.StatusConflict, "starter_gift_templates_unavailable", err.Error())
		return false
	}
	indexes := readStarterGiftTemplateIndexes(s.cfg.ServerDirectory())
	available := make(map[string]string, len(templates))
	for _, template := range templates {
		fileName, fileErr := starterGiftTemplateFileName(strings.TrimSpace(template.Name))
		if fileErr == nil && indexes.FileNames[starterGiftTemplateLookupKey(fileName)] {
			continue
		}
		key := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(template.Name), ".json"))
		if key != "" {
			available[key] = template.Name
		}
	}
	selected := make([]string, 0, len(config.PalTemplates))
	seen := map[string]bool{}
	for _, requested := range config.PalTemplates {
		key := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(requested), ".json"))
		actual, found := available[key]
		if !found {
			fail(c, http.StatusBadRequest, "starter_gift_template_not_found", "PalDefender template was not found: "+requested)
			return false
		}
		if !seen[actual] {
			seen[actual] = true
			selected = append(selected, actual)
		}
	}
	config.PalTemplates = selected
	return true
}

func (s Server) retryStarterGift(c *gin.Context) {
	scope, err := s.currentStarterGiftScope()
	if err != nil {
		fail(c, http.StatusConflict, "starter_gift_world_unavailable", err.Error())
		return
	}
	var input struct {
		Action string `json:"action"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&input); err != nil {
			fail(c, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
	}
	if strings.TrimSpace(input.Action) == "" {
		input.Action = c.DefaultQuery("action", "retry")
	}
	player := playerpresence.OnlinePlayer{SteamID: c.Param("id")}
	if index, _, indexErr := s.serverSaveIndex.Current(c.Request.Context()); indexErr == nil {
		requested := starterGiftPlayerIdentity(c.Param("id"))
		for _, indexed := range index.Players {
			if requested == starterGiftPlayerIdentity(indexed.SteamID) || requested == starterGiftPlayerIdentity(indexed.PlayerUID) {
				player = playerpresence.OnlinePlayer{PlayerUID: indexed.PlayerUID, SteamID: indexed.SteamID, Nickname: indexed.Nickname}
				break
			}
		}
	}
	if err := startergift.ApplyAction(c.Request.Context(), s.store, scope, player, input.Action); err != nil {
		fail(c, http.StatusBadRequest, "starter_gift_action_failed", err.Error())
		return
	}
	ok(c, gin.H{"queued": true, "action": input.Action, "message": "Starter gift state updated; online tasks run on the next player observation."})
}

func starterGiftPlayerIdentity(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	if value == "" || value == "none" || value == "null" || strings.Trim(value, "0") == "" {
		return ""
	}
	return value
}

func (s Server) forgetStarterGift(c *gin.Context) {
	scope, err := s.currentStarterGiftScope()
	if err != nil {
		fail(c, http.StatusConflict, "starter_gift_world_unavailable", err.Error())
		return
	}
	if err := startergift.Forget(c.Request.Context(), s.store, scope, c.Param("id")); err != nil {
		fail(c, http.StatusNotFound, "starter_gift_not_found", err.Error())
		return
	}
	ok(c, gin.H{"rearmed": true, "message": "The player will be eligible after leaving and entering again."})
}

func (s Server) starterGiftResponse(ctx context.Context, snapshot startergift.Snapshot) gin.H {
	templates, templateErr := s.defender.ListPalTemplates()
	templateNames := make([]string, 0, len(templates))
	for _, template := range templates {
		templateNames = append(templateNames, template.Name)
	}
	templateCatalog, templateIndexes := starterGiftTemplateCatalog(s.cfg.ServerDirectory(), templateNames)
	response := gin.H{
		"scope":            snapshot.Scope,
		"config":           snapshot.Config,
		"grants":           snapshot.Grants,
		"worker_running":   snapshot.WorkerRunning,
		"templates":        templateCatalog,
		"template_indexes": templateIndexes,
		"item_catalog":     pallocalize.SearchItems("", 5000),
	}
	if index, status, indexErr := s.serverSaveIndex.Current(ctx); indexErr == nil {
		known := make([]playerpresence.OnlinePlayer, 0, len(index.Players))
		for _, player := range index.Players {
			known = append(known, playerpresence.OnlinePlayer{PlayerUID: player.PlayerUID, SteamID: player.SteamID, Nickname: player.Nickname})
		}
		decisions, decisionErr := startergift.InspectPlayers(ctx, s.store, snapshot.Scope, known)
		if decisionErr == nil {
			response["players"] = decisions
		} else {
			response["players_error"] = decisionErr.Error()
		}
		response["save_index_state"] = status.State
	} else {
		response["players"] = []startergift.PlayerDecision{}
		response["players_error"] = indexErr.Error()
	}
	if templateErr != nil {
		response["template_error"] = templateErr.Error()
	}
	return response
}
