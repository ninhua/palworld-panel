package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"palpanel/internal/boss"
	"palpanel/internal/gameevents"
	"palpanel/internal/playeridentity"
)

var (
	errBossBridgeUnavailable      = errors.New("PalPanelBridge player location is unavailable")
	errBossBridgePlayerNotFound   = errors.New("PalPanelBridge did not return the requested online player")
	errBossBridgeLocationNotFound = errors.New("PalPanelBridge did not return a valid player location")
	bossBridgeJobIDPattern        = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

const (
	bossBridgeDefaultURL       = "http://127.0.0.1:18083"
	bossBridgeRequestTimeout   = 4 * time.Second
	bossBridgePollInterval     = 100 * time.Millisecond
	bossBridgeMaximumBody      = 2 << 20
	bossRegistrationChatLimit  = 500
	bossRegistrationEventLimit = 5
)

type bossBridgeConnection struct {
	BaseURL    string
	Token      string
	ConfigPath string
}

type bossBridgeLocationLookup struct {
	Found       bool           `json:"found"`
	Source      string         `json:"source"`
	MatchedBy   string         `json:"matched_by,omitempty"`
	PlayerUID   string         `json:"player_uid,omitempty"`
	AccountName string         `json:"account_name,omitempty"`
	Identifier  string         `json:"identifier,omitempty"`
	Location    *boss.Location `json:"location,omitempty"`
	ObservedAt  string         `json:"observed_at,omitempty"`
	Error       string         `json:"error,omitempty"`
}

type bossBridgePlayer struct {
	PlayerUID           string         `json:"player_uid"`
	SteamID             string         `json:"steam_id"`
	UserID              string         `json:"user_id"`
	AccountName         string         `json:"account_name"`
	CachedLocationFound bool           `json:"cached_location_found"`
	CachedLocation      *boss.Location `json:"cached_location"`
	CachedLocationError string         `json:"cached_location_error"`
}

type bossBridgeJob struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Error   string `json:"error"`
	Failure string `json:"failure"`
	Result  struct {
		Players []bossBridgePlayer `json:"players"`
	} `json:"result"`
}

type bossBridgeJobEnvelope struct {
	Job    *bossBridgeJob `json:"job"`
	ID     string         `json:"id"`
	Status string         `json:"status"`
	Error  string         `json:"error"`
	Result struct {
		Players []bossBridgePlayer `json:"players"`
	} `json:"result"`
}

type bossOnlineParticipantResult struct {
	boss.ParticipantResult
	LocationLookup bossBridgeLocationLookup `json:"location_lookup"`
}

type bossOnlineRefreshResult struct {
	SummonID      string                     `json:"summon_id"`
	OnlinePlayers int                        `json:"online_players"`
	Participants  int                        `json:"participants"`
	Updated       int                        `json:"updated"`
	Unavailable   int                        `json:"unavailable"`
	Failed        int                        `json:"failed"`
	Lookups       []bossBridgeLocationLookup `json:"lookups"`
	Snapshot      boss.RegistrationSnapshot  `json:"snapshot"`
}

type bossRegistrationChatResult struct {
	Handled      bool                      `json:"handled"`
	Command      string                    `json:"command,omitempty"`
	Selector     string                    `json:"selector,omitempty"`
	SummonID     string                    `json:"summon_id,omitempty"`
	TemplateName string                    `json:"template_name,omitempty"`
	Reply        string                    `json:"reply,omitempty"`
	Participant  *boss.Participant         `json:"participant,omitempty"`
	Policy       *boss.RegistrationPolicy  `json:"policy,omitempty"`
	Location     *bossBridgeLocationLookup `json:"location_lookup,omitempty"`
}

type bossRegistrationCandidate struct {
	Summon   boss.Summon
	Snapshot boss.RegistrationSnapshot
}

func init() {
	patchFeatures = append(patchFeatures,
		"boss-game-chat-registration",
		"boss-game-chat-check-in",
		"boss-palpanel-bridge-location-lookup",
		"boss-registration-online-refresh",
		"boss-registration-management-ui",
	)
}

func isBossRegistrationOnlineControl(action string) bool {
	switch normalizeBossRegistrationAction(action) {
	case "participant_register_online", "participant_check_area_online", "registration_refresh_online":
		return true
	default:
		return false
	}
}

func (s Server) registerBossParticipantOnline(ctx context.Context, service *boss.Service, summonID string, input boss.ParticipantInput, actor string) (bossOnlineParticipantResult, error) {
	snapshot, err := service.RegistrationSnapshot(ctx, summonID, false)
	if err != nil {
		return bossOnlineParticipantResult{}, err
	}
	lookup := bossBridgeLocationLookup{Source: "registration_policy"}
	if snapshot.Policy.AreaRequired {
		var lookupErr error
		lookup, lookupErr = s.lookupBossPlayerLocation(ctx, input.PlayerUID, input.SteamID, input.Nickname)
		if lookupErr == nil && lookup.Location != nil {
			input.Location = lookup.Location
		}
	}
	input.Metadata = mergeBossRegistrationMetadata(input.Metadata, lookup)
	result, err := service.RegisterParticipant(ctx, summonID, input, actor)
	if err != nil {
		return bossOnlineParticipantResult{}, err
	}
	return bossOnlineParticipantResult{ParticipantResult: result, LocationLookup: lookup}, nil
}

func (s Server) checkBossParticipantOnline(ctx context.Context, service *boss.Service, summonID string, input boss.ParticipantInput, actor string) (bossOnlineParticipantResult, error) {
	snapshot, err := service.RegistrationSnapshot(ctx, summonID, false)
	if err != nil {
		return bossOnlineParticipantResult{}, err
	}
	if !snapshot.Policy.AreaRequired {
		participant, participantErr := service.GetParticipant(ctx, summonID, input.PlayerUID)
		if participantErr != nil {
			return bossOnlineParticipantResult{}, participantErr
		}
		return bossOnlineParticipantResult{
			ParticipantResult: boss.ParticipantResult{Participant: participant, Policy: snapshot.Policy, Duplicate: true},
			LocationLookup:    bossBridgeLocationLookup{Source: "registration_policy"},
		}, nil
	}
	lookup, err := s.lookupBossPlayerLocation(ctx, input.PlayerUID, input.SteamID, input.Nickname)
	if err != nil || lookup.Location == nil {
		if err == nil {
			err = errBossBridgeLocationNotFound
		}
		return bossOnlineParticipantResult{LocationLookup: lookup}, err
	}
	result, err := service.CheckParticipantArea(ctx, summonID, boss.ParticipantAreaInput{
		PlayerUID: input.PlayerUID,
		Location:  lookup.Location,
		Metadata:  mergeBossRegistrationMetadata(input.Metadata, lookup),
	}, actor)
	if err != nil {
		return bossOnlineParticipantResult{LocationLookup: lookup}, err
	}
	return bossOnlineParticipantResult{ParticipantResult: result, LocationLookup: lookup}, nil
}

func (s Server) refreshBossRegistrationOnline(ctx context.Context, service *boss.Service, summonID, actor string) (bossOnlineRefreshResult, error) {
	snapshot, err := service.RegistrationSnapshot(ctx, summonID, false)
	if err != nil {
		return bossOnlineRefreshResult{}, err
	}
	if !snapshot.Policy.AreaRequired {
		return bossOnlineRefreshResult{
			SummonID: summonID, Participants: len(snapshot.Participants), Snapshot: snapshot,
		}, nil
	}
	players, observedAt, err := s.queryBossBridgeOnlinePlayers(ctx)
	if err != nil {
		return bossOnlineRefreshResult{}, err
	}
	result := bossOnlineRefreshResult{
		SummonID:      summonID,
		OnlinePlayers: len(players),
		Participants:  len(snapshot.Participants),
		Lookups:       make([]bossBridgeLocationLookup, 0, len(snapshot.Participants)),
	}
	for _, participant := range snapshot.Participants {
		lookup, lookupErr := matchBossBridgePlayerLocation(players, observedAt, participant.PlayerUID, participant.SteamID, participant.Nickname)
		if lookupErr != nil || lookup.Location == nil {
			result.Unavailable++
			result.Lookups = append(result.Lookups, lookup)
			continue
		}
		_, checkErr := service.CheckParticipantArea(ctx, summonID, boss.ParticipantAreaInput{
			PlayerUID: participant.PlayerUID,
			Location:  lookup.Location,
			Metadata:  mergeBossRegistrationMetadata(participant.Metadata, lookup),
		}, actor)
		if checkErr != nil {
			lookup.Error = checkErr.Error()
			result.Failed++
		} else {
			result.Updated++
		}
		result.Lookups = append(result.Lookups, lookup)
	}
	result.Snapshot, err = service.RegistrationSnapshot(ctx, summonID, false)
	if err != nil {
		return bossOnlineRefreshResult{}, err
	}
	return result, nil
}

func mergeBossRegistrationMetadata(metadata map[string]any, lookup bossBridgeLocationLookup) map[string]any {
	result := map[string]any{}
	for key, value := range metadata {
		result[key] = value
	}
	result["registration_source"] = "palpanel"
	result["location_source"] = lookup.Source
	result["location_found"] = lookup.Found
	if lookup.ObservedAt != "" {
		result["location_observed_at"] = lookup.ObservedAt
	}
	if lookup.MatchedBy != "" {
		result["location_matched_by"] = lookup.MatchedBy
	}
	if lookup.Error != "" {
		result["location_error"] = lookup.Error
	} else {
		delete(result, "location_error")
	}
	return result
}

func (s Server) lookupBossPlayerLocation(ctx context.Context, playerUID, steamID, nickname string) (bossBridgeLocationLookup, error) {
	players, observedAt, err := s.queryBossBridgeOnlinePlayers(ctx)
	if err != nil {
		return bossBridgeLocationLookup{Source: "palpanel_bridge", ObservedAt: observedAt, Error: err.Error()}, err
	}
	return matchBossBridgePlayerLocation(players, observedAt, playerUID, steamID, nickname)
}

func matchBossBridgePlayerLocation(players []bossBridgePlayer, observedAt, playerUID, steamID, nickname string) (bossBridgeLocationLookup, error) {
	targetUID := playeridentity.Normalize(playerUID)
	targetSteam := playeridentity.NormalizeSteamID(steamID)
	targetName := strings.TrimSpace(nickname)
	lookup := bossBridgeLocationLookup{Source: "palpanel_bridge", ObservedAt: observedAt}

	matchIndex := -1
	matchedBy := ""
	for index, player := range players {
		candidateUID := playeridentity.Normalize(player.PlayerUID)
		candidateSteam := playeridentity.NormalizeSteamID(firstBossNonEmpty(player.SteamID, player.UserID))
		if targetUID != "" && candidateUID == targetUID {
			matchIndex, matchedBy = index, "player_uid"
			break
		}
		if targetSteam != "" && candidateSteam == targetSteam {
			matchIndex, matchedBy = index, "steam_id"
			break
		}
	}
	if matchIndex < 0 && targetName != "" {
		matches := make([]int, 0, 2)
		for index, player := range players {
			if strings.EqualFold(strings.TrimSpace(player.AccountName), targetName) {
				matches = append(matches, index)
			}
		}
		if len(matches) == 1 {
			matchIndex, matchedBy = matches[0], "account_name"
		}
	}
	if matchIndex < 0 {
		lookup.Error = errBossBridgePlayerNotFound.Error()
		return lookup, errBossBridgePlayerNotFound
	}
	player := players[matchIndex]
	lookup.MatchedBy = matchedBy
	lookup.PlayerUID = playeridentity.Normalize(player.PlayerUID)
	lookup.AccountName = strings.TrimSpace(player.AccountName)
	lookup.Identifier = firstBossNonEmpty(strings.TrimSpace(player.UserID), strings.TrimSpace(player.SteamID), lookup.PlayerUID)
	if !player.CachedLocationFound || player.CachedLocation == nil {
		lookup.Error = firstBossNonEmpty(strings.TrimSpace(player.CachedLocationError), errBossBridgeLocationNotFound.Error())
		return lookup, errBossBridgeLocationNotFound
	}
	if !validBossLocation(*player.CachedLocation) {
		lookup.Error = "PalPanelBridge returned an invalid player location"
		return lookup, errBossBridgeLocationNotFound
	}
	location := *player.CachedLocation
	lookup.Found = true
	lookup.Location = &location
	return lookup, nil
}

func validBossLocation(location boss.Location) bool {
	for _, value := range []float64{location.X, location.Y, location.Z} {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 10_000_000 {
			return false
		}
	}
	return true
}

func (s Server) queryBossBridgeOnlinePlayers(ctx context.Context) ([]bossBridgePlayer, string, error) {
	connection, err := s.bossBridgeConnection()
	if err != nil {
		return nil, time.Now().UTC().Format(time.RFC3339Nano), err
	}
	requestCtx, cancel := context.WithTimeout(ctx, bossBridgeRequestTimeout)
	defer cancel()
	client := &http.Client{Timeout: bossBridgeRequestTimeout}
	var submission struct {
		JobID string `json:"job_id"`
	}
	if err := bossBridgeJSONRequest(requestCtx, client, connection, http.MethodPost, "/v1/players/online", []byte("{}"), &submission); err != nil {
		return nil, time.Now().UTC().Format(time.RFC3339Nano), err
	}
	if !bossBridgeJobIDPattern.MatchString(submission.JobID) {
		return nil, time.Now().UTC().Format(time.RFC3339Nano), fmt.Errorf("%w: invalid job id", errBossBridgeUnavailable)
	}
	observedAt := time.Now().UTC().Format(time.RFC3339Nano)
	ticker := time.NewTicker(bossBridgePollInterval)
	defer ticker.Stop()
	for {
		var envelope bossBridgeJobEnvelope
		if err := bossBridgeJSONRequest(requestCtx, client, connection, http.MethodGet, "/v1/jobs/"+url.PathEscape(submission.JobID), nil, &envelope); err != nil {
			return nil, observedAt, err
		}
		job := envelope.Job
		if job == nil {
			job = &bossBridgeJob{ID: envelope.ID, Status: envelope.Status, Error: envelope.Error, Result: envelope.Result}
		}
		switch strings.ToLower(strings.TrimSpace(job.Status)) {
		case "completed":
			return job.Result.Players, observedAt, nil
		case "failed":
			message := firstBossNonEmpty(job.Error, job.Failure, "online player query failed")
			return nil, observedAt, fmt.Errorf("%w: %s", errBossBridgeUnavailable, message)
		case "queued", "running", "":
			select {
			case <-requestCtx.Done():
				return nil, observedAt, fmt.Errorf("%w: %v", errBossBridgeUnavailable, requestCtx.Err())
			case <-ticker.C:
			}
		default:
			return nil, observedAt, fmt.Errorf("%w: unexpected job status %q", errBossBridgeUnavailable, job.Status)
		}
	}
}

func bossBridgeJSONRequest(ctx context.Context, client *http.Client, connection bossBridgeConnection, method, requestPath string, body []byte, target any) error {
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(connection.BaseURL, "/")+requestPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", errBossBridgeUnavailable, err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, bossBridgeMaximumBody+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("%w: read response: %v", errBossBridgeUnavailable, err)
	}
	if len(payload) > bossBridgeMaximumBody {
		return fmt.Errorf("%w: response exceeds %d bytes", errBossBridgeUnavailable, bossBridgeMaximumBody)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: HTTP %d", errBossBridgeUnavailable, response.StatusCode)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("%w: invalid JSON response: %v", errBossBridgeUnavailable, err)
	}
	return nil
}

func (s Server) bossBridgeConnection() (bossBridgeConnection, error) {
	baseURL := strings.TrimSpace(os.Getenv("PALPANEL_BRIDGE_URL"))
	token := strings.TrimSpace(os.Getenv("PALPANEL_BRIDGE_TOKEN"))
	configPath := strings.TrimSpace(os.Getenv("PALPANEL_BRIDGE_CONFIG"))
	if configPath == "" {
		serverDirectory := strings.TrimSpace(s.cfg.ServerDirectory())
		candidates := []string{
			filepath.Join(serverDirectory, "Pal", "Binaries", "Win64", "ue4ss", "Mods", "PalPanelBridge", "config.ini"),
			filepath.Join(serverDirectory, "PalServer", "Pal", "Binaries", "Win64", "ue4ss", "Mods", "PalPanelBridge", "config.ini"),
			filepath.Join(serverDirectory, "server", "Pal", "Binaries", "Win64", "ue4ss", "Mods", "PalPanelBridge", "config.ini"),
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				configPath = candidate
				break
			}
		}
	}
	values := map[string]string{}
	if configPath != "" {
		if payload, err := os.ReadFile(configPath); err == nil {
			values = parseBossBridgeConfig(payload)
		} else if token == "" {
			return bossBridgeConnection{}, fmt.Errorf("%w: read %s: %v", errBossBridgeUnavailable, configPath, err)
		}
	}
	if token == "" {
		token = strings.TrimSpace(values["token"])
	}
	if baseURL == "" {
		listen := strings.TrimSpace(values["listen"])
		if listen == "" || listen == "0.0.0.0" || listen == "::" || listen == "[::]" {
			listen = "127.0.0.1"
		}
		port := strings.TrimSpace(values["port"])
		if port == "" {
			port = "18083"
		}
		baseURL = "http://" + net.JoinHostPort(strings.Trim(listen, "[]"), port)
	}
	if token == "" || strings.EqualFold(token, "REPLACE_WITH_A_RANDOM_TOKEN") {
		return bossBridgeConnection{}, fmt.Errorf("%w: bridge token is not configured", errBossBridgeUnavailable)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() == "" || !isBossBridgeLoopback(parsed.Hostname()) {
		return bossBridgeConnection{}, fmt.Errorf("%w: bridge URL must use loopback HTTP", errBossBridgeUnavailable)
	}
	if _, err := strconv.Atoi(parsed.Port()); parsed.Port() != "" && err != nil {
		return bossBridgeConnection{}, fmt.Errorf("%w: invalid bridge port", errBossBridgeUnavailable)
	}
	return bossBridgeConnection{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, ConfigPath: configPath}, nil
}

func parseBossBridgeConfig(payload []byte) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(string(payload), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "[") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			result[key] = value
		}
	}
	return result
}

func isBossBridgeLoopback(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func (s Server) executeBossRegistrationChatCommand(ctx context.Context, record gameevents.Record, message, prefix string, allowBare bool) (bossRegistrationChatResult, error) {
	command, selector, handled := parseBossRegistrationChatCommand(message, prefix, allowBare)
	result := bossRegistrationChatResult{Handled: handled, Command: command, Selector: selector}
	if !handled {
		return result, nil
	}
	service, err := s.bossService()
	if err != nil {
		return result, err
	}
	candidates, err := s.listBossRegistrationCandidates(ctx, service)
	if err != nil {
		return result, err
	}
	if command == "list" {
		result.Reply = formatBossRegistrationCandidateList(candidates)
		return result, nil
	}
	candidate, found, ambiguous := selectBossRegistrationCandidate(candidates, selector, record.PlayerUID, service, ctx, command)
	if ambiguous {
		result.Reply = "存在多个可用 Boss 场次，请输入 Boss列表，再使用 Boss报名 <场次编号>。"
		return result, nil
	}
	if !found {
		if selector != "" {
			result.Reply = "没有找到该 Boss 报名场次。输入 Boss列表 查看可用场次。"
		} else {
			result.Reply = "当前没有开放报名的 Boss 场次。"
		}
		return result, nil
	}
	result.SummonID = candidate.Summon.ID
	result.TemplateName = candidate.Summon.TemplateName
	actor := "game-chat:" + playeridentity.Normalize(record.PlayerUID)

	switch command {
	case "register":
		operation, operationErr := s.registerBossParticipantOnline(ctx, service, candidate.Summon.ID, boss.ParticipantInput{
			PlayerUID: record.PlayerUID,
			Nickname:  record.Nickname,
			SteamID:   record.SteamID,
			Metadata:  map[string]any{"chat_event_id": record.EventID},
		}, actor)
		if operationErr != nil {
			result.Reply = bossRegistrationPlayerError(operationErr)
			return result, nil
		}
		result.Participant = &operation.Participant
		result.Policy = &operation.Policy
		result.Location = &operation.LocationLookup
		result.Reply = formatBossRegistrationSuccess(candidate.Summon, operation)
	case "check":
		operation, operationErr := s.checkBossParticipantOnline(ctx, service, candidate.Summon.ID, boss.ParticipantInput{
			PlayerUID: record.PlayerUID,
			Nickname:  record.Nickname,
			SteamID:   record.SteamID,
			Metadata:  map[string]any{"chat_event_id": record.EventID},
		}, actor)
		if operationErr != nil {
			result.Reply = bossRegistrationPlayerError(operationErr)
			return result, nil
		}
		result.Participant = &operation.Participant
		result.Policy = &operation.Policy
		result.Location = &operation.LocationLookup
		result.Reply = formatBossRegistrationCheck(candidate.Summon, operation)
	case "cancel":
		operation, operationErr := service.CancelParticipant(ctx, candidate.Summon.ID, boss.ParticipantCancelInput{
			PlayerUID: record.PlayerUID,
			Reason:    "player cancelled through game chat",
		}, actor)
		if operationErr != nil {
			result.Reply = bossRegistrationPlayerError(operationErr)
			return result, nil
		}
		result.Participant = &operation.Participant
		result.Policy = &operation.Policy
		result.Reply = fmt.Sprintf("已取消 Boss 场次“%s”的报名，名额已释放。", candidate.Summon.TemplateName)
	case "status":
		participant, participantErr := service.GetParticipant(ctx, candidate.Summon.ID, record.PlayerUID)
		if participantErr != nil {
			result.Reply = bossRegistrationPlayerError(participantErr)
			return result, nil
		}
		policy := candidate.Snapshot.Policy
		result.Participant = &participant
		result.Policy = &policy
		result.Reply = formatBossRegistrationStatus(candidate.Summon, participant, policy)
	}
	return result, nil
}

func parseBossRegistrationChatCommand(message, prefix string, allowBare bool) (string, string, bool) {
	message = strings.TrimSpace(message)
	prefix = strings.TrimSpace(prefix)
	if message == "" {
		return "", "", false
	}
	if prefix != "" && strings.HasPrefix(message, prefix) {
		message = strings.TrimSpace(strings.TrimPrefix(message, prefix))
	} else if prefix != "" && !allowBare {
		return "", "", false
	}
	aliases := []struct {
		command string
		labels  []string
	}{
		{command: "status", labels: []string{"Boss报名状态", "Boss状态", "boss报名状态", "boss状态"}},
		{command: "cancel", labels: []string{"取消Boss报名", "Boss取消", "boss取消"}},
		{command: "register", labels: []string{"报名Boss", "Boss报名", "boss报名"}},
		{command: "check", labels: []string{"Boss签到", "Boss到场", "Boss核验", "boss签到", "boss到场", "boss核验"}},
		{command: "list", labels: []string{"Boss列表", "Boss活动", "boss列表", "boss活动"}},
	}
	for _, item := range aliases {
		for _, label := range item.labels {
			if strings.EqualFold(message, label) {
				return item.command, "", true
			}
			if len(message) > len(label) && strings.EqualFold(message[:len(label)], label) {
				remainder := message[len(label):]
				if strings.TrimSpace(remainder) != remainder {
					return item.command, strings.TrimSpace(remainder), true
				}
			}
		}
	}
	return "", "", false
}

func (s Server) listBossRegistrationCandidates(ctx context.Context, service *boss.Service) ([]bossRegistrationCandidate, error) {
	all := make([]boss.Summon, 0, 20)
	for _, status := range []string{boss.SummonStatusActive, boss.SummonStatusPending} {
		items, err := service.ListSummons(ctx, boss.SummonFilter{Status: status, Limit: bossRegistrationChatLimit})
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
	}
	candidates := make([]bossRegistrationCandidate, 0, len(all))
	for _, summon := range all {
		if !strings.EqualFold(bossMetadataString(summon.Metadata, "activity_kind"), "fixed_boss") {
			continue
		}
		snapshot, err := service.RegistrationSnapshot(ctx, summon.ID, false)
		if err != nil {
			if errors.Is(err, boss.ErrSummonNotFound) {
				continue
			}
			return nil, err
		}
		candidates = append(candidates, bossRegistrationCandidate{Summon: summon, Snapshot: snapshot})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		leftActive := candidates[i].Summon.Status == boss.SummonStatusActive
		rightActive := candidates[j].Summon.Status == boss.SummonStatusActive
		if leftActive != rightActive {
			return leftActive
		}
		return candidates[i].Summon.RequestedAt > candidates[j].Summon.RequestedAt
	})
	return candidates, nil
}

func selectBossRegistrationCandidate(candidates []bossRegistrationCandidate, selector, playerUID string, service *boss.Service, ctx context.Context, command string) (bossRegistrationCandidate, bool, bool) {
	selector = strings.TrimSpace(selector)
	if selector != "" {
		matches := make([]bossRegistrationCandidate, 0, 2)
		for _, candidate := range candidates {
			shortID := shortBossRegistrationID(candidate.Summon.ID)
			if strings.EqualFold(selector, candidate.Summon.ID) || strings.EqualFold(selector, shortID) ||
				strings.EqualFold(selector, candidate.Summon.TemplateName) || strings.HasPrefix(strings.ToLower(candidate.Summon.ID), strings.ToLower(selector)) {
				matches = append(matches, candidate)
			}
		}
		if len(matches) == 1 {
			return matches[0], true, false
		}
		return bossRegistrationCandidate{}, false, len(matches) > 1
	}
	if command == "cancel" || command == "status" || command == "check" {
		matches := make([]bossRegistrationCandidate, 0, 2)
		for _, candidate := range candidates {
			participant, err := service.GetParticipant(ctx, candidate.Summon.ID, playerUID)
			if err == nil && participant.Status != boss.ParticipantStatusCancelled {
				matches = append(matches, candidate)
			}
		}
		if len(matches) == 1 {
			return matches[0], true, false
		}
		if len(matches) > 1 {
			return bossRegistrationCandidate{}, false, true
		}
	}
	open := make([]bossRegistrationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Snapshot.Policy.Open {
			open = append(open, candidate)
		}
	}
	if len(open) == 1 {
		return open[0], true, false
	}
	if len(open) > 1 {
		return bossRegistrationCandidate{}, false, true
	}
	return bossRegistrationCandidate{}, false, false
}

func formatBossRegistrationCandidateList(candidates []bossRegistrationCandidate) string {
	open := make([]bossRegistrationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Snapshot.Policy.Open {
			open = append(open, candidate)
		}
	}
	if len(open) == 0 {
		return "当前没有开放报名的 Boss 场次。"
	}
	lines := []string{"开放的 Boss 场次："}
	for index, candidate := range open {
		if index >= bossRegistrationEventLimit {
			lines = append(lines, fmt.Sprintf("另有 %d 场，请在面板查看。", len(open)-index))
			break
		}
		policy := candidate.Snapshot.Policy
		slots := "不限人数"
		if policy.MaxPlayers > 0 {
			slots = fmt.Sprintf("%d/%d", policy.Registered, policy.MaxPlayers)
		}
		lines = append(lines, fmt.Sprintf("%s %s（%s）", shortBossRegistrationID(candidate.Summon.ID), candidate.Summon.TemplateName, slots))
	}
	lines = append(lines, "用法：Boss报名 <场次编号>")
	return strings.Join(lines, "\n")
}

func formatBossRegistrationSuccess(summon boss.Summon, result bossOnlineParticipantResult) string {
	prefix := "Boss报名成功"
	if result.Duplicate {
		prefix = "已报名该 Boss 场次"
	}
	parts := []string{fmt.Sprintf("%s：“%s”。", prefix, summon.TemplateName), formatBossRegistrationSlots(result.Policy)}
	switch result.Participant.AreaStatus {
	case boss.ParticipantAreaEligible:
		parts = append(parts, fmt.Sprintf("区域核验通过，距离 %.0f。", result.Participant.Distance))
	case boss.ParticipantAreaDisabled:
		parts = append(parts, "该场次无需区域核验。")
	case boss.ParticipantAreaOutside:
		parts = append(parts, fmt.Sprintf("当前距离 %.0f，需进入 %.0f 范围后输入 Boss签到。", result.Participant.Distance, result.Policy.Radius))
	default:
		parts = append(parts, "已占用名额，但未取得当前位置；靠近活动点后输入 Boss签到。")
	}
	return strings.Join(parts, " ")
}

func formatBossRegistrationCheck(summon boss.Summon, result bossOnlineParticipantResult) string {
	switch result.Participant.AreaStatus {
	case boss.ParticipantAreaEligible, boss.ParticipantAreaDisabled:
		return fmt.Sprintf("Boss场次“%s”到场核验通过，距离 %.0f。%s", summon.TemplateName, result.Participant.Distance, formatBossRegistrationSlots(result.Policy))
	case boss.ParticipantAreaOutside:
		return fmt.Sprintf("尚未进入 Boss 区域：距离 %.0f，要求不超过 %.0f。", result.Participant.Distance, result.Policy.Radius)
	default:
		return "未取得当前位置，无法完成 Boss 到场核验。"
	}
}

func formatBossRegistrationStatus(summon boss.Summon, participant boss.Participant, policy boss.RegistrationPolicy) string {
	status := "已报名"
	if participant.Status == boss.ParticipantStatusCheckedIn {
		status = "已到场"
	} else if participant.Status == boss.ParticipantStatusCancelled {
		status = "已取消"
	}
	area := "待区域核验"
	switch participant.AreaStatus {
	case boss.ParticipantAreaEligible:
		area = fmt.Sprintf("区域内，距离 %.0f", participant.Distance)
	case boss.ParticipantAreaOutside:
		area = fmt.Sprintf("区域外，距离 %.0f / %.0f", participant.Distance, policy.Radius)
	case boss.ParticipantAreaDisabled:
		area = "无需区域核验"
	}
	return fmt.Sprintf("Boss场次“%s”：%s；%s；%s", summon.TemplateName, status, area, formatBossRegistrationSlots(policy))
}

func formatBossRegistrationSlots(policy boss.RegistrationPolicy) string {
	if policy.MaxPlayers <= 0 {
		return fmt.Sprintf("当前报名 %d 人，不限人数。", policy.Registered)
	}
	return fmt.Sprintf("名额 %d/%d，剩余 %d。", policy.Registered, policy.MaxPlayers, policy.Available)
}

func bossRegistrationPlayerError(err error) string {
	switch {
	case errors.Is(err, boss.ErrRegistrationFull):
		return "该 Boss 场次报名人数已满。"
	case errors.Is(err, boss.ErrRegistrationClosed):
		return "该 Boss 场次已经关闭报名。"
	case errors.Is(err, boss.ErrParticipantNotFound):
		return "你尚未报名该 Boss 场次。"
	case errors.Is(err, boss.ErrParticipantStateConflict):
		return "当前报名状态不允许执行该操作。"
	case errors.Is(err, errBossBridgePlayerNotFound):
		return "PalPanelBridge 未找到你的在线玩家对象，请稍后重试。"
	case errors.Is(err, errBossBridgeLocationNotFound):
		return "PalPanelBridge 暂未取得你的位置，请移动后再次输入 Boss签到。"
	case errors.Is(err, errBossBridgeUnavailable):
		return "玩家位置服务暂不可用，请稍后重试或联系管理员检查 PalPanelBridge。"
	case errors.Is(err, boss.ErrRegistrationBusy):
		return "报名正在处理其他请求，请稍后重试。"
	default:
		return "Boss报名操作失败，请管理员在面板查看报名审计。"
	}
}

func bossMetadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func shortBossRegistrationID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		return value
	}
	return value[len(value)-8:]
}

func firstBossNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
