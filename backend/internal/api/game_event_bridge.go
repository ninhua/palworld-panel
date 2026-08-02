package api

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"palpanel/internal/economy"
	"palpanel/internal/gameevents"
	"palpanel/internal/paldefender"
	"palpanel/internal/tasks"
)

const (
	gameEventBridgePollInterval = time.Second
	gameEventBridgeReadLimit    = 2 << 20
)

type gameEventBridgeStatus struct {
	Enabled            bool            `json:"enabled"`
	Running            bool            `json:"running"`
	LogDirectory       string          `json:"log_directory,omitempty"`
	ActiveFile         string          `json:"active_file,omitempty"`
	LastScanAt         string          `json:"last_scan_at,omitempty"`
	LastLineAt         string          `json:"last_line_at,omitempty"`
	LastEventAt        string          `json:"last_event_at,omitempty"`
	LastError          string          `json:"last_error,omitempty"`
	ParsedEvents       int64           `json:"parsed_events"`
	ProcessedEvents    int64           `json:"processed_events"`
	UnmatchedPlayers   int64           `json:"unmatched_players"`
	FailedEvents       int64           `json:"failed_events"`
	PendingDeadLetters int64           `json:"pending_dead_letters"`
	RotationResets     int64           `json:"rotation_resets"`
	CursorFiles        int             `json:"cursor_files"`
	Configuration      map[string]bool `json:"configuration"`
}

type gameEventBridgeRuntime struct {
	mu          sync.RWMutex
	status      gameEventBridgeStatus
	initialized bool
	players     []paldefender.RESTPlayer
	playersAt   time.Time
}

var gameEventBridges sync.Map

var (
	captureLogPattern = regexp.MustCompile(`(?i)(.+?)\s+has captured Pal\s+'([^']+)'\s+\(([^)]+)\)(?:\s+at\s+(-?[0-9.]+)[, ]+(-?[0-9.]+)[, ]+(-?[0-9.]+))?`)
	craftLogPattern   = regexp.MustCompile(`(?i)(.+?)\s+started crafting\s+'([^']+)'`)
	loginLogPattern   = regexp.MustCompile(`(?i)(.+?)\s+connected to the server\.?`)
	killLogPatterns   = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(.+?)\s+has killed\s+(?:Pal\s+)?'([^']+)'(?:\s+\(([^)]+)\))?`),
		regexp.MustCompile(`(?i)'([^']+)'\s+was (?:attacked|killed) by\s+'([^']+)'\s+and died`),
	}
	chatLogPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\[(?:global\s+|guild\s+)?chat\]\s*(?:<([^>]+)>|([^:：]+))[:：]\s*(.+)$`),
		regexp.MustCompile(`(?i)(?:global\s+|guild\s+)?chat\s*[:：-]\s*(?:<([^>]+)>|([^:：]+))[:：]\s*(.+)$`),
	}
	userIDPattern    = regexp.MustCompile(`(?i)\b(?:steam|gdk|ps5)_[A-Za-z0-9_-]+\b`)
	playerUIDPattern = regexp.MustCompile(`(?i)PlayerUID\s*[:=]\s*([A-Za-z0-9_-]{4,128})`)
	ipv4Pattern      = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
)

type parsedPalDefenderEvent struct {
	Type       string
	Nickname   string
	PlayerHint string
	SourceText string
	Payload    map[string]any
}

func (s Server) startGameEventBridge() {
	key := strings.TrimSpace(s.cfg.DBPath)
	if key == "" {
		return
	}
	runtime := &gameEventBridgeRuntime{status: gameEventBridgeStatus{Enabled: true, Configuration: map[string]bool{}}}
	actual, loaded := gameEventBridges.LoadOrStore(key, runtime)
	if loaded {
		_ = actual
		return
	}
	go s.runGameEventBridge(runtime)
}

func (s Server) runGameEventBridge(runtime *gameEventBridgeRuntime) {
	runtime.update(func(status *gameEventBridgeStatus) {
		status.Running = true
		status.Enabled = true
	})
	ticker := time.NewTicker(gameEventBridgePollInterval)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := s.scanGameEventBridge(ctx, runtime)
		cancel()
		runtime.update(func(status *gameEventBridgeStatus) {
			status.LastScanAt = time.Now().UTC().Format(time.RFC3339Nano)
			if err != nil {
				status.LastError = err.Error()
			} else {
				status.LastError = ""
			}
		})
		<-ticker.C
	}
}

func (s Server) scanGameEventBridge(ctx context.Context, runtime *gameEventBridgeRuntime) error {
	status, err := s.defender.Status(ctx)
	if err != nil {
		return fmt.Errorf("inspect PalDefender: %w", err)
	}
	palDir := strings.TrimSpace(status.Paths["paldefender"])
	if palDir == "" {
		return errors.New("PalDefender directory is unavailable")
	}
	logDir := filepath.Join(palDir, "Logs")
	configFlags := s.gameEventBridgeConfigFlags()
	runtime.update(func(item *gameEventBridgeStatus) {
		item.LogDirectory = logDir
		item.Configuration = configFlags
	})
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return fmt.Errorf("read PalDefender logs: %w", err)
	}
	type candidate struct {
		path    string
		modTime time.Time
	}
	files := make([]candidate, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".log") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, candidate{path: filepath.Join(logDir, entry.Name()), modTime: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	if len(files) == 0 {
		return errors.New("PalDefender has not created a .log file yet")
	}
	service, err := s.gameEventService()
	if err != nil {
		return err
	}
	pointService, err := s.economyService()
	if err != nil {
		return err
	}
	commandConfig, err := pointService.DetailedConfig(ctx)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := s.tailGameEventLog(ctx, runtime, service, file.path, commandConfig); err != nil {
			return err
		}
	}
	pending, pendingErr := service.CountBridgeDeadLetters(ctx, "pending")
	if pendingErr != nil {
		return pendingErr
	}
	offsets, offsetErr := service.ListBridgeOffsets(ctx)
	if offsetErr != nil {
		return offsetErr
	}
	runtime.update(func(item *gameEventBridgeStatus) {
		item.PendingDeadLetters = pending
		item.CursorFiles = len(offsets)
	})
	runtime.mu.Lock()
	runtime.initialized = true
	runtime.mu.Unlock()
	return nil
}

func (s Server) tailGameEventLog(ctx context.Context, runtime *gameEventBridgeRuntime, service *gameevents.Service, path string, commandConfig economy.DetailedConfig) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	prefixHash, err := bridgeFilePrefixHash(path)
	if err != nil {
		return err
	}
	offsetRecord, found, err := service.BridgeOffset(ctx, path)
	if err != nil {
		return err
	}
	runtime.mu.RLock()
	initialized := runtime.initialized
	runtime.mu.RUnlock()
	if !found && !initialized {
		return service.SaveBridgeOffsetState(ctx, gameevents.BridgeOffset{Path: path, Offset: info.Size(), FileSize: info.Size(), PrefixHash: prefixHash})
	}
	offset := offsetRecord.Offset
	resetReason := ""
	if found && offsetRecord.PrefixHash != "" && prefixHash != "" && offsetRecord.PrefixHash != prefixHash {
		offset = 0
		resetReason = "log file identity changed"
	} else if offset > info.Size() {
		offset = 0
		resetReason = "log file was truncated"
	} else if !found {
		offset = 0
	}
	if resetReason != "" {
		offsetRecord.ResetCount++
		offsetRecord.LastResetReason = resetReason
		runtime.update(func(item *gameEventBridgeStatus) { item.RotationResets++ })
		_ = service.AddBridgeObservation(ctx, gameevents.BridgeObservation{
			SourcePath: path, Offset: 0, Status: "cursor_reset", Reason: resetReason,
			Sample: filepath.Base(path),
		})
	}
	if offset == info.Size() {
		if found && (offsetRecord.PrefixHash != prefixHash || offsetRecord.FileSize != info.Size()) {
			offsetRecord.Path = path
			offsetRecord.Offset = offset
			offsetRecord.FileSize = info.Size()
			offsetRecord.PrefixHash = prefixHash
			return service.SaveBridgeOffsetState(ctx, offsetRecord)
		}
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(io.LimitReader(file, gameEventBridgeReadLimit), 64*1024)
	current := offset
	for {
		lineStart := current
		line, readErr := reader.ReadString('\n')
		if readErr != nil && len(line) == 0 {
			if readErr == io.EOF {
				break
			}
			return readErr
		}
		if readErr == io.EOF && !strings.HasSuffix(line, "\n") {
			break
		}
		current += int64(len(line))
		runtime.update(func(item *gameEventBridgeStatus) {
			item.ActiveFile = filepath.Base(path)
			item.LastLineAt = time.Now().UTC().Format(time.RFC3339Nano)
		})
		if parsed, ok := parsePalDefenderLogLine(line, commandConfig); ok {
			runtime.update(func(item *gameEventBridgeStatus) { item.ParsedEvents++ })
			if err := s.processBridgedPalDefenderEvent(ctx, runtime, service, path, lineStart, line, parsed); err != nil {
				runtime.update(func(item *gameEventBridgeStatus) { item.FailedEvents++ })
			}
		} else if looksLikeBridgeCandidate(line, commandConfig) {
			eventID := bridgeEventID(path, lineStart, line)
			_, _ = service.AddBridgeDeadLetter(ctx, gameevents.BridgeDeadLetter{
				EventID: eventID, SourcePath: path, Offset: lineStart, EventType: "PARSE_FAILED",
				RawLine: strings.TrimSpace(line), Sample: sanitizeBridgeSample(line),
				Reason: "log line looked relevant but did not match a supported PalDefender event format",
			})
			_ = service.AddBridgeObservation(ctx, gameevents.BridgeObservation{
				SourcePath: path, Offset: lineStart, EventType: "PARSE_FAILED", Status: "parse_failed",
				Reason: "saved to dead letters for parser upgrade or manual replay", Sample: sanitizeBridgeSample(line),
			})
			runtime.update(func(item *gameEventBridgeStatus) {
				item.FailedEvents++
				item.PendingDeadLetters++
			})
		}
		if readErr == io.EOF {
			break
		}
	}
	offsetRecord.Path = path
	offsetRecord.Offset = current
	offsetRecord.FileSize = info.Size()
	offsetRecord.PrefixHash = prefixHash
	return service.SaveBridgeOffsetState(ctx, offsetRecord)
}

func (s Server) processBridgedPalDefenderEvent(ctx context.Context, runtime *gameEventBridgeRuntime, service *gameevents.Service, path string, offset int64, raw string, parsed parsedPalDefenderEvent) error {
	eventID := bridgeEventID(path, offset, raw)
	deadLetter := gameevents.BridgeDeadLetter{
		EventID: eventID, SourcePath: path, Offset: offset, EventType: parsed.Type,
		PlayerHint: parsed.PlayerHint, Nickname: parsed.Nickname, Payload: parsed.Payload,
		RawLine: strings.TrimSpace(raw), Sample: sanitizeBridgeSample(raw),
	}
	playerUID, steamID, nickname, err := s.resolveBridgePlayer(ctx, runtime, parsed.PlayerHint, parsed.Nickname, parsed.SourceText)
	if err != nil {
		deadLetter.Reason = err.Error()
		_, _ = service.AddBridgeDeadLetter(ctx, deadLetter)
		_ = service.AddBridgeObservation(ctx, gameevents.BridgeObservation{SourcePath: path, Offset: offset, EventType: parsed.Type, Nickname: parsed.Nickname, Status: "error", Reason: err.Error(), Sample: sanitizeBridgeSample(raw)})
		runtime.update(func(item *gameEventBridgeStatus) { item.PendingDeadLetters++ })
		return err
	}
	if playerUID == "" {
		runtime.update(func(item *gameEventBridgeStatus) {
			item.UnmatchedPlayers++
			item.PendingDeadLetters++
		})
		deadLetter.Reason = "PalDefender player catalog did not match the log identity"
		_, _ = service.AddBridgeDeadLetter(ctx, deadLetter)
		_ = service.AddBridgeObservation(ctx, gameevents.BridgeObservation{SourcePath: path, Offset: offset, EventType: parsed.Type, Nickname: parsed.Nickname, Status: "unmatched_player", Reason: deadLetter.Reason, Sample: sanitizeBridgeSample(raw)})
		return nil
	}
	deadLetter.PlayerUID = playerUID
	deadLetter.SteamID = steamID
	deadLetter.Nickname = nickname
	event := gameevents.Event{
		EventID:    eventID,
		Type:       parsed.Type,
		PlayerUID:  playerUID,
		Nickname:   nickname,
		SteamID:    steamID,
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
		Payload:    parsed.Payload,
	}
	processingResult, err := s.processBridgeEvent(ctx, service, event)
	if err != nil {
		deadLetter.Reason = err.Error()
		_, _ = service.AddBridgeDeadLetter(ctx, deadLetter)
		_ = service.AddBridgeObservation(ctx, gameevents.BridgeObservation{SourcePath: path, Offset: offset, EventType: parsed.Type, PlayerUID: playerUID, Nickname: nickname, Status: "error", Reason: err.Error(), Sample: sanitizeBridgeSample(raw)})
		runtime.update(func(item *gameEventBridgeStatus) { item.PendingDeadLetters++ })
		return err
	}
	runtime.update(func(item *gameEventBridgeStatus) {
		item.ProcessedEvents++
		item.LastEventAt = time.Now().UTC().Format(time.RFC3339Nano)
	})
	_ = service.AddBridgeObservation(ctx, gameevents.BridgeObservation{SourcePath: path, Offset: offset, EventType: parsed.Type, PlayerUID: playerUID, Nickname: nickname, Status: "processed", Reason: bridgeProcessingSummary(processingResult), Sample: sanitizeBridgeSample(raw)})
	return nil
}

func (s Server) processBridgeEvent(ctx context.Context, service *gameevents.Service, event gameevents.Event) (map[string]any, error) {
	claim, err := service.Claim(ctx, event)
	if err != nil {
		return nil, err
	}
	if claim.Duplicate {
		return map[string]any{"duplicate": true}, nil
	}
	result := map[string]any{"accepted": true, "source": "paldefender_log_bridge", "retry": claim.Retry}
	var commandResult *economy.CommandResult
	if claim.Record.Type == "PLAYER_CHAT" {
		outcome, commandErr := s.executeGameChatCommand(ctx, claim.Record)
		if commandErr != nil {
			_, _ = service.Fail(ctx, claim.Record.EventID, commandErr)
			return nil, commandErr
		}
		if outcome.Economy != nil {
			result["command"] = outcome.Economy
			commandResult = outcome.Economy
		}
		if outcome.Shop != nil && outcome.Shop.Handled {
			result["shop_command"] = outcome.Shop
		}
		if outcome.Handled && outcome.Reply != "" && !outcome.Duplicate {
			delivery, deliveryErr := s.deliverGameEventReply(ctx, claim.Record, outcome.Reply)
			result["reply_delivery"] = delivery
			if deliveryErr != nil {
				result["reply_error"] = deliveryErr.Error()
			}
		}
	}
	taskService, err := s.taskService()
	if err != nil {
		_, _ = service.Fail(ctx, claim.Record.EventID, err)
		return nil, err
	}
	pointService, err := s.economyService()
	if err != nil {
		_, _ = service.Fail(ctx, claim.Record.EventID, err)
		return nil, err
	}
	taskEvents := gameTaskEvents(claim.Record, commandResult)
	updates := make([]tasks.ProgressUpdate, 0)
	for _, taskEvent := range taskEvents {
		eventUpdates, processErr := taskService.ProcessEvent(ctx, taskEvent, pointService)
		if processErr != nil {
			_, _ = service.Fail(ctx, claim.Record.EventID, processErr)
			return nil, processErr
		}
		updates = append(updates, eventUpdates...)
	}
	result["task_event_types"] = taskEventTypes(taskEvents)
	result["tasks"] = updates
	_, err = service.Complete(ctx, claim.Record.EventID, result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func bridgeProcessingSummary(result map[string]any) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if duplicate, _ := result["duplicate"].(bool); duplicate {
		parts = append(parts, "重复事件已跳过")
	}
	if types, ok := result["task_event_types"].([]string); ok && len(types) > 0 {
		parts = append(parts, "任务事件 "+strings.Join(types, ", "))
	}
	if updates, ok := result["tasks"].([]tasks.ProgressUpdate); ok {
		parts = append(parts, fmt.Sprintf("匹配任务 %d", len(updates)))
	}
	if delivery, ok := result["reply_delivery"].(string); ok && delivery != "" {
		parts = append(parts, "回复 "+delivery)
	}
	if replyError, ok := result["reply_error"].(string); ok && replyError != "" {
		parts = append(parts, "回复失败: "+replyError)
	}
	return strings.Join(parts, "；")
}

func (s Server) resolveBridgePlayer(ctx context.Context, runtime *gameEventBridgeRuntime, hint, nickname, sourceText string) (string, string, string, error) {
	hint = strings.TrimSpace(hint)
	nickname = strings.TrimSpace(nickname)
	sourceText = strings.TrimSpace(sourceText)
	runtime.mu.RLock()
	players := append([]paldefender.RESTPlayer(nil), runtime.players...)
	playersAt := runtime.playersAt
	runtime.mu.RUnlock()
	if len(players) == 0 || time.Since(playersAt) > 15*time.Second {
		response, err := s.defender.RESTPlayers(ctx)
		if err != nil {
			return "", "", nickname, err
		}
		players = response.Players
		runtime.mu.Lock()
		runtime.players = append([]paldefender.RESTPlayer(nil), players...)
		runtime.playersAt = time.Now()
		runtime.mu.Unlock()
	}
	identityText := strings.ToLower(strings.Join([]string{hint, nickname, sourceText}, " "))
	for _, player := range players {
		if bridgeIdentityEquals(hint, player.UserID) || bridgeIdentityEquals(hint, player.PlayerUID) ||
			bridgeIdentityEquals(nickname, player.Name) {
			return bridgeResolvedPlayer(player, nickname)
		}
	}
	// PalDefender log lines commonly include timestamps and log-level prefixes
	// before the player descriptor. Match stable IDs first, then choose the
	// longest nickname contained in the descriptor to avoid prefix collisions.
	for _, player := range players {
		if bridgeIdentityContained(identityText, player.UserID) || bridgeIdentityContained(identityText, player.PlayerUID) {
			return bridgeResolvedPlayer(player, nickname)
		}
	}
	sort.SliceStable(players, func(i, j int) bool { return len([]rune(players[i].Name)) > len([]rune(players[j].Name)) })
	for _, player := range players {
		name := strings.TrimSpace(player.Name)
		if name == "" {
			continue
		}
		if bridgeIdentityEquals(nickname, name) || (len([]rune(name)) >= 2 && strings.Contains(identityText, strings.ToLower(name))) {
			return bridgeResolvedPlayer(player, nickname)
		}
	}
	return "", "", nickname, nil
}

func bridgeIdentityEquals(left, right string) bool {
	return strings.TrimSpace(left) != "" && strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func bridgeIdentityContained(haystack, value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value != "" && strings.Contains(haystack, value)
}

func bridgeResolvedPlayer(player paldefender.RESTPlayer, fallbackName string) (string, string, string, error) {
	playerUID := strings.TrimSpace(player.PlayerUID)
	if playerUID == "" {
		playerUID = strings.TrimSpace(player.UserID)
	}
	return playerUID, strings.TrimSpace(player.UserID), firstBridgeNonEmpty(player.Name, fallbackName), nil
}

func parsePalDefenderLogLine(line string, commandConfig economy.DetailedConfig) (parsedPalDefenderEvent, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return parsedPalDefenderEvent{}, false
	}
	if match := captureLogPattern.FindStringSubmatch(line); len(match) > 0 {
		nickname, hint := parseBridgeIdentity(match[1])
		payload := map[string]any{"pal_name": strings.TrimSpace(match[2]), "pal_id": strings.TrimSpace(match[3]), "count": 1}
		if len(match) >= 7 && match[4] != "" {
			payload["x"], _ = strconv.ParseFloat(match[4], 64)
			payload["y"], _ = strconv.ParseFloat(match[5], 64)
			payload["z"], _ = strconv.ParseFloat(match[6], 64)
		}
		return parsedPalDefenderEvent{Type: "PAL_CAPTURED", Nickname: nickname, PlayerHint: hint, SourceText: line, Payload: payload}, true
	}
	if match := craftLogPattern.FindStringSubmatch(line); len(match) > 0 {
		nickname, hint := parseBridgeIdentity(match[1])
		return parsedPalDefenderEvent{Type: "ITEM_CRAFTED", Nickname: nickname, PlayerHint: hint, SourceText: line, Payload: map[string]any{"item_name": strings.TrimSpace(match[2]), "count": 1}}, true
	}
	if match := loginLogPattern.FindStringSubmatch(line); len(match) > 0 {
		nickname, hint := parseBridgeIdentity(match[1])
		return parsedPalDefenderEvent{Type: "PLAYER_LOGIN", Nickname: nickname, PlayerHint: hint, SourceText: line, Payload: map[string]any{"count": 1}}, true
	}
	for index, pattern := range killLogPatterns {
		if match := pattern.FindStringSubmatch(line); len(match) > 0 {
			playerDescriptor, targetName, targetID := match[1], match[2], ""
			if index == 1 {
				playerDescriptor, targetName = match[2], match[1]
			} else if len(match) > 3 {
				targetID = match[3]
			}
			nickname, hint := parseBridgeIdentity(playerDescriptor)
			return parsedPalDefenderEvent{Type: "PAL_KILLED", Nickname: nickname, PlayerHint: hint, SourceText: line, Payload: map[string]any{"target_name": strings.TrimSpace(targetName), "pal_id": strings.TrimSpace(targetID), "count": 1}}, true
		}
	}
	for _, pattern := range chatLogPatterns {
		if match := pattern.FindStringSubmatch(line); len(match) > 0 {
			descriptor := firstBridgeNonEmpty(match[1], match[2])
			nickname, hint := parseBridgeIdentity(descriptor)
			return parsedPalDefenderEvent{Type: "PLAYER_CHAT", Nickname: nickname, PlayerHint: hint, SourceText: line, Payload: map[string]any{"message": strings.TrimSpace(match[3])}}, true
		}
	}
	if descriptor, message, ok := configuredCommandFromLogLine(line, commandConfig); ok {
		nickname, hint := parseBridgeIdentity(descriptor)
		return parsedPalDefenderEvent{Type: "PLAYER_CHAT", Nickname: nickname, PlayerHint: hint, SourceText: line, Payload: map[string]any{"message": message}}, true
	}
	lower := strings.ToLower(line)
	if strings.Contains(lower, "chat") {
		if colon := strings.LastIndexAny(line, ":："); colon > 0 && colon < len(line)-1 {
			_, delimiterSize := utf8.DecodeRuneInString(line[colon:])
			left, message := line[:colon], strings.TrimSpace(line[colon+delimiterSize:])
			left = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(left, "[Chat]"), "[CHAT]"))
			nickname, hint := parseBridgeIdentity(left)
			if nickname != "" && message != "" {
				return parsedPalDefenderEvent{Type: "PLAYER_CHAT", Nickname: nickname, PlayerHint: hint, SourceText: line, Payload: map[string]any{"message": message}}, true
			}
		}
	}
	return parsedPalDefenderEvent{}, false
}

func configuredCommandFromLogLine(line string, config economy.DetailedConfig) (string, string, bool) {
	labels := make([]string, 0, len(config.CheckinAliases)+len(config.PointsAliases)+len(config.HelpAliases))
	aliases := append(append(append([]string{}, config.CheckinAliases...), config.PointsAliases...), config.HelpAliases...)
	aliases = append(aliases, "商城", "商店", "兑换", "购买", "我的订单", "订单")
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		if config.CommandPrefix != "" {
			labels = append(labels, config.CommandPrefix+alias)
		}
		if config.CommandPrefix == "" || config.AllowBareCommands {
			labels = append(labels, alias)
		}
	}
	sort.SliceStable(labels, func(i, j int) bool { return len([]rune(labels[i])) > len([]rune(labels[j])) })
	lowerLine := strings.ToLower(line)
	bestIndex := -1
	bestLabel := ""
	for _, label := range labels {
		lowerLabel := strings.ToLower(label)
		searchFrom := 0
		for searchFrom < len(lowerLine) {
			position := strings.Index(lowerLine[searchFrom:], lowerLabel)
			if position < 0 {
				break
			}
			position += searchFrom
			if bridgeCommandBoundary(line, position, position+len(label)) && position >= bestIndex {
				bestIndex = position
				bestLabel = line[position : position+len(label)]
			}
			searchFrom = position + len(lowerLabel)
		}
	}
	if bestIndex < 0 || bestLabel == "" {
		return "", "", false
	}
	descriptor := strings.TrimSpace(line[:bestIndex])
	// A command line must look like chat or carry a player/message delimiter.
	lowerDescriptor := strings.ToLower(descriptor)
	if !strings.Contains(lowerDescriptor, "chat") && !strings.ContainsAny(descriptor, ":：>]") {
		return "", "", false
	}
	messageTail := strings.TrimSpace(line[bestIndex+len(bestLabel):])
	messageTail = strings.TrimSpace(strings.TrimRight(messageTail, "'\"`])}。."))
	message := strings.TrimSpace(bestLabel)
	if messageTail != "" && !strings.HasPrefix(messageTail, "[") {
		message += " " + messageTail
	}
	return descriptor, message, true
}

func bridgeCommandBoundary(line string, start, end int) bool {
	if start < 0 || end > len(line) || start >= end {
		return false
	}
	if start > 0 {
		before, _ := utf8.DecodeLastRuneInString(line[:start])
		if !strings.ContainsRune(" \t\r\n:：>\"'`([{", before) {
			return false
		}
	}
	if end < len(line) {
		after, _ := utf8.DecodeRuneInString(line[end:])
		if !strings.ContainsRune(" \t\r\n。.!！?？,，;；:：<\"'`])}", after) {
			return false
		}
	}
	return true
}

func parseBridgeIdentity(value string) (string, string) {
	value = strings.TrimSpace(value)
	value = stripBridgeLogPrefix(value)
	hint := ""
	if match := userIDPattern.FindString(value); match != "" {
		hint = match
	} else if match := playerUIDPattern.FindStringSubmatch(value); len(match) > 1 {
		hint = match[1]
	}
	nickname := userIDPattern.ReplaceAllString(value, "")
	nickname = playerUIDPattern.ReplaceAllString(nickname, "")
	nickname = strings.Trim(nickname, " [](){}<>:-\t")
	if index := strings.LastIndex(nickname, "]["); index >= 0 {
		nickname = strings.TrimSpace(nickname[index+2:])
	}
	return nickname, hint
}

func stripBridgeLogPrefix(value string) string {
	value = strings.TrimSpace(value)
	for {
		if !strings.HasPrefix(value, "[") {
			break
		}
		end := strings.Index(value, "]")
		if end < 0 {
			break
		}
		prefix := strings.ToLower(value[1:end])
		if strings.Contains(prefix, "chat") || strings.Contains(prefix, "global") || strings.Contains(prefix, "guild") {
			value = strings.TrimSpace(value[end+1:])
			continue
		}
		if strings.ContainsAny(prefix, "0123456789") || prefix == "info" || prefix == "log" || prefix == "paldefender" || prefix == "server" {
			value = strings.TrimSpace(value[end+1:])
			continue
		}
		break
	}
	value = strings.TrimSpace(strings.TrimLeft(value, "-:| "))
	return value
}

func bridgeFilePrefixHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	buffer := make([]byte, 256)
	count, err := io.ReadFull(file, buffer)
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			// A short file is still being written. Do not assign an unstable
			// identity until the first 256 bytes are available.
			return "", nil
		}
		return "", err
	}
	hash := sha256.Sum256(buffer[:count])
	return hex.EncodeToString(hash[:16]), nil
}

func looksLikeBridgeCandidate(line string, config economy.DetailedConfig) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	for _, keyword := range []string{"captur", "craft", "connected to the server", "has killed", " and died", "[chat]", "global chat", "guild chat"} {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	aliases := append(append(append([]string{}, config.CheckinAliases...), config.PointsAliases...), config.HelpAliases...)
	aliases = append(aliases, "商城", "商店", "兑换", "购买", "我的订单", "订单")
	for _, alias := range aliases {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if alias != "" && strings.Contains(lower, alias) && strings.ContainsAny(line, ":：>]") {
			return true
		}
	}
	return false
}

func bridgeEventID(path string, offset int64, line string) string {
	hash := sha256.Sum256([]byte(path + "\x00" + strconv.FormatInt(offset, 10) + "\x00" + line))
	return "pdlog_" + hex.EncodeToString(hash[:16])
}

func sanitizeBridgeSample(value string) string {
	value = strings.TrimSpace(value)
	value = ipv4Pattern.ReplaceAllString(value, "<ip>")
	if len(value) > 512 {
		value = value[:512]
	}
	return value
}

func firstBridgeNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (runtime *gameEventBridgeRuntime) update(fn func(*gameEventBridgeStatus)) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	fn(&runtime.status)
}

func (runtime *gameEventBridgeRuntime) snapshot() gameEventBridgeStatus {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	result := runtime.status
	result.Configuration = map[string]bool{}
	for key, value := range runtime.status.Configuration {
		result.Configuration[key] = value
	}
	return result
}

func (s Server) gameEventBridgeConfigFlags() map[string]bool {
	config, err := s.defender.ReadConfig()
	keys := []string{"logChat", "logPlayerUID", "logPlayerCaptures", "logPlayerDeaths", "logPlayerLogins", "logCraftings"}
	result := map[string]bool{}
	for _, key := range keys {
		result[key] = false
	}
	if err != nil {
		return result
	}
	for _, key := range keys {
		value, ok := config[key].(bool)
		result[key] = ok && value
	}
	return result
}

func init() {
	patchFeatures = append(patchFeatures,
		"paldefender-log-event-bridge",
		"paldefender-chat-command-bridge",
		"paldefender-task-event-bridge",
		"game-event-bridge-diagnostics",
	)
}

func (s Server) gameEventBridgeStatusHandler(c *gin.Context) {
	s.startGameEventBridge()
	value, found := gameEventBridges.Load(strings.TrimSpace(s.cfg.DBPath))
	if !found {
		fail(c, http.StatusServiceUnavailable, "game_event_bridge_unavailable", "game event bridge could not be started")
		return
	}
	status := value.(*gameEventBridgeRuntime).snapshot()
	status.Configuration = s.gameEventBridgeConfigFlags()
	service, serviceErr := s.gameEventService()
	if serviceErr != nil {
		fail(c, http.StatusInternalServerError, "game_event_service_failed", serviceErr.Error())
		return
	}
	pending, pendingErr := service.CountBridgeDeadLetters(c.Request.Context(), "pending")
	if pendingErr != nil {
		fail(c, http.StatusInternalServerError, "game_event_bridge_dead_letter_count_failed", pendingErr.Error())
		return
	}
	offsets, offsetErr := service.ListBridgeOffsets(c.Request.Context())
	if offsetErr != nil {
		fail(c, http.StatusInternalServerError, "game_event_bridge_offsets_failed", offsetErr.Error())
		return
	}
	status.PendingDeadLetters = pending
	status.CursorFiles = len(offsets)
	okResponse := gin.H{"bridge": status, "offsets": offsets, "required_configuration": []string{"logChat", "logPlayerUID", "logPlayerCaptures", "logPlayerDeaths", "logPlayerLogins", "logCraftings"}}
	ok(c, okResponse)
}

func (s Server) repairGameEventBridge(c *gin.Context) {
	config, err := s.defender.ReadConfig()
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_bridge_config_read_failed", err.Error())
		return
	}
	if config == nil {
		config = map[string]any{}
	}
	for _, key := range []string{"logChat", "logPlayerUID", "logPlayerCaptures", "logPlayerDeaths", "logPlayerLogins", "logCraftings"} {
		config[key] = true
	}
	if _, err := s.defender.WriteConfig(config); err != nil {
		fail(c, http.StatusInternalServerError, "game_event_bridge_config_write_failed", err.Error())
		return
	}
	reloadError := s.defender.ReloadConfig(c.Request.Context())
	response := gin.H{"configuration": s.gameEventBridgeConfigFlags(), "reload_required": reloadError != nil}
	if reloadError != nil {
		response["reload_error"] = reloadError.Error()
	}
	ok(c, response)
}

func (s Server) listGameEventBridgeObservations(c *gin.Context) {
	service, err := s.gameEventService()
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_service_failed", err.Error())
		return
	}
	items, err := service.ListBridgeObservations(c.Request.Context(), c.Query("status"), economyQueryInt(c, "limit", 50), economyQueryInt(c, "offset", 0))
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_bridge_observations_failed", err.Error())
		return
	}
	ok(c, gin.H{"items": items, "count": len(items)})
}

type replayBridgeDeadLetterRequest struct {
	PlayerUID string `json:"player_uid"`
}

func (s Server) listGameEventBridgeDeadLetters(c *gin.Context) {
	service, err := s.gameEventService()
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_service_failed", err.Error())
		return
	}
	items, err := service.ListBridgeDeadLetters(c.Request.Context(), c.Query("status"), economyQueryInt(c, "limit", 50), economyQueryInt(c, "offset", 0))
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_bridge_dead_letters_failed", err.Error())
		return
	}
	pending, err := service.CountBridgeDeadLetters(c.Request.Context(), "pending")
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_bridge_dead_letter_count_failed", err.Error())
		return
	}
	ok(c, gin.H{"items": items, "count": len(items), "pending": pending})
}

func (s Server) replayGameEventBridgeDeadLetter(c *gin.Context) {
	id, err := gameevents.ParseBridgeDeadLetterID(c.Param("id"))
	if err != nil {
		fail(c, http.StatusBadRequest, "game_event_bridge_dead_letter_id_invalid", err.Error())
		return
	}
	var request replayBridgeDeadLetterRequest
	if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		fail(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	service, err := s.gameEventService()
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_service_failed", err.Error())
		return
	}
	item, err := service.GetBridgeDeadLetter(c.Request.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		fail(c, http.StatusNotFound, "game_event_bridge_dead_letter_not_found", "dead letter not found")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_bridge_dead_letter_read_failed", err.Error())
		return
	}
	if item.Status != "pending" {
		fail(c, http.StatusConflict, "game_event_bridge_dead_letter_not_pending", "only pending dead letters can be replayed")
		return
	}
	pointService, err := s.economyService()
	if err != nil {
		fail(c, http.StatusInternalServerError, "economy_service_failed", err.Error())
		return
	}
	commandConfig, err := pointService.DetailedConfig(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "economy_config_failed", err.Error())
		return
	}
	parsed := parsedPalDefenderEvent{
		Type: item.EventType, Nickname: item.Nickname, PlayerHint: item.PlayerHint,
		SourceText: item.RawLine, Payload: item.Payload,
	}
	if item.EventType == "PARSE_FAILED" || item.EventType == "UNKNOWN" {
		var ok bool
		parsed, ok = parsePalDefenderLogLine(item.RawLine, commandConfig)
		if !ok {
			message := "the current parser still does not recognize this log line"
			_ = service.MarkBridgeDeadLetterAttempt(c.Request.Context(), id, message)
			fail(c, http.StatusUnprocessableEntity, "game_event_bridge_dead_letter_parse_failed", message)
			return
		}
	}
	playerUID := strings.TrimSpace(request.PlayerUID)
	steamID := item.SteamID
	nickname := item.Nickname
	if playerUID == "" {
		playerUID = item.PlayerUID
	}
	if playerUID == "" {
		s.startGameEventBridge()
		value, found := gameEventBridges.Load(strings.TrimSpace(s.cfg.DBPath))
		if !found {
			fail(c, http.StatusServiceUnavailable, "game_event_bridge_unavailable", "game event bridge could not be started")
			return
		}
		playerUID, steamID, nickname, err = s.resolveBridgePlayer(c.Request.Context(), value.(*gameEventBridgeRuntime), parsed.PlayerHint, parsed.Nickname, parsed.SourceText)
		if err != nil {
			_ = service.MarkBridgeDeadLetterAttempt(c.Request.Context(), id, err.Error())
			fail(c, http.StatusBadGateway, "game_event_bridge_player_lookup_failed", err.Error())
			return
		}
	}
	if playerUID == "" {
		message := "player is still unmatched; provide player_uid when replaying"
		_ = service.MarkBridgeDeadLetterAttempt(c.Request.Context(), id, message)
		fail(c, http.StatusUnprocessableEntity, "game_event_bridge_player_unmatched", message)
		return
	}
	event := gameevents.Event{
		EventID: item.EventID, Type: parsed.Type, PlayerUID: playerUID, SteamID: steamID,
		Nickname: nickname, OccurredAt: time.Now().UTC().Format(time.RFC3339), Payload: parsed.Payload,
	}
	result, err := s.processBridgeEvent(c.Request.Context(), service, event)
	if err != nil {
		_ = service.MarkBridgeDeadLetterAttempt(c.Request.Context(), id, err.Error())
		fail(c, http.StatusInternalServerError, "game_event_bridge_dead_letter_replay_failed", err.Error())
		return
	}
	if err := service.CompleteBridgeDeadLetter(c.Request.Context(), id); err != nil {
		fail(c, http.StatusConflict, "game_event_bridge_dead_letter_complete_failed", err.Error())
		return
	}
	_ = service.AddBridgeObservation(c.Request.Context(), gameevents.BridgeObservation{
		SourcePath: item.SourcePath, Offset: item.Offset, EventType: parsed.Type,
		PlayerUID: playerUID, Nickname: nickname, Status: "replayed",
		Reason: bridgeProcessingSummary(result), Sample: item.Sample,
	})
	setAuditSuccess(c, true)
	ok(c, gin.H{"dead_letter_id": id, "event": event, "result": result})
}

func (s Server) dismissGameEventBridgeDeadLetter(c *gin.Context) {
	id, err := gameevents.ParseBridgeDeadLetterID(c.Param("id"))
	if err != nil {
		fail(c, http.StatusBadRequest, "game_event_bridge_dead_letter_id_invalid", err.Error())
		return
	}
	service, err := s.gameEventService()
	if err != nil {
		fail(c, http.StatusInternalServerError, "game_event_service_failed", err.Error())
		return
	}
	if err := service.DismissBridgeDeadLetter(c.Request.Context(), id); err != nil {
		fail(c, http.StatusConflict, "game_event_bridge_dead_letter_dismiss_failed", err.Error())
		return
	}
	setAuditSuccess(c, true)
	ok(c, gin.H{"dead_letter_id": id, "status": "dismissed"})
}
