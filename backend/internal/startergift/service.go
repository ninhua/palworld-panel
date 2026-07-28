package startergift

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"palpanel/internal/db"
	"palpanel/internal/paldefender"
	"palpanel/internal/playerpresence"
)

const (
	ConfigKey          = "starter_gift:config"
	StateKey           = "starter_gift:state"
	ScopedConfigPrefix = "starter_gift:config:v2:"
	ScopedStatePrefix  = "starter_gift:state:v2:"
	Version            = 2
	MaxGrantEvents     = 80

	MaxItems        = 500
	MaxPalTemplates = 500

	starterGiftWorkerTimeout = 3 * time.Hour
)

var (
	ErrPlayerNotReady = errors.New("PalDefender has not registered the online player yet")

	stateMu       sync.Mutex
	workerMu      sync.Mutex
	workerRunning = map[string]bool{}
	itemIDPattern = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,128}$`)
)

type ItemGrant struct {
	ItemID string `json:"item_id"`
	Count  int64  `json:"count"`
}

type Config struct {
	Enabled           bool        `json:"enabled"`
	Items             []ItemGrant `json:"items"`
	PalTemplates      []string    `json:"pal_templates"`
	ItemBatchSize     int         `json:"item_batch_size"`
	TemplateBatchSize int         `json:"template_batch_size"`
	BatchDelayMS      int         `json:"batch_delay_ms"`
}

type GrantEvent struct {
	At           string `json:"at"`
	Phase        string `json:"phase"`
	Level        string `json:"level"`
	Message      string `json:"message"`
	ItemFrom     int    `json:"item_from,omitempty"`
	ItemTo       int    `json:"item_to,omitempty"`
	TemplateFrom int    `json:"template_from,omitempty"`
	TemplateTo   int    `json:"template_to,omitempty"`
}

type grantRecord struct {
	PlayerID         string       `json:"player_id"`
	PlayerUID        string       `json:"player_uid,omitempty"`
	SteamID          string       `json:"steam_id,omitempty"`
	Nickname         string       `json:"nickname,omitempty"`
	Status           string       `json:"status"`
	Phase            string       `json:"phase,omitempty"`
	DetectionSource  string       `json:"detection_source,omitempty"`
	DetectionReason  string       `json:"detection_reason,omitempty"`
	Manual           bool         `json:"manual,omitempty"`
	ResolvedPlayerID string       `json:"resolved_player_id,omitempty"`
	NextItem         int          `json:"next_item"`
	NextTemplate     int          `json:"next_template"`
	Attempts         int          `json:"attempts"`
	FirstSeenAt      string       `json:"first_seen_at"`
	UpdatedAt        string       `json:"updated_at"`
	CompletedAt      string       `json:"completed_at,omitempty"`
	LastError        string       `json:"last_error,omitempty"`
	PlanItems        []ItemGrant  `json:"plan_items,omitempty"`
	PlanTemplates    []string     `json:"plan_templates,omitempty"`
	Events           []GrantEvent `json:"events,omitempty"`
}

type Grant struct {
	PlayerID         string       `json:"player_id"`
	PlayerUID        string       `json:"player_uid,omitempty"`
	SteamID          string       `json:"steam_id,omitempty"`
	Nickname         string       `json:"nickname,omitempty"`
	Status           string       `json:"status"`
	Phase            string       `json:"phase,omitempty"`
	DetectionSource  string       `json:"detection_source,omitempty"`
	DetectionReason  string       `json:"detection_reason,omitempty"`
	Manual           bool         `json:"manual,omitempty"`
	ResolvedPlayerID string       `json:"resolved_player_id,omitempty"`
	NextItem         int          `json:"next_item"`
	NextTemplate     int          `json:"next_template"`
	ItemTotal        int          `json:"item_total"`
	TemplateTotal    int          `json:"template_total"`
	ProgressPercent  int          `json:"progress_percent"`
	Attempts         int          `json:"attempts"`
	FirstSeenAt      string       `json:"first_seen_at"`
	UpdatedAt        string       `json:"updated_at"`
	CompletedAt      string       `json:"completed_at,omitempty"`
	LastError        string       `json:"last_error,omitempty"`
	Events           []GrantEvent `json:"events,omitempty"`
}

type PlayerDecision struct {
	PlayerID     string   `json:"player_id"`
	PlayerUID    string   `json:"player_uid,omitempty"`
	SteamID      string   `json:"steam_id,omitempty"`
	Nickname     string   `json:"nickname,omitempty"`
	Online       bool     `json:"online"`
	Seen         bool     `json:"seen"`
	Rearmed      bool     `json:"rearmed"`
	IsNew        bool     `json:"is_new"`
	Eligible     bool     `json:"eligible"`
	Decision     string   `json:"decision"`
	Reason       string   `json:"reason"`
	GrantStatus  string   `json:"grant_status,omitempty"`
	GrantPhase   string   `json:"grant_phase,omitempty"`
	Evidence     []string `json:"evidence"`
	LastUpdateAt string   `json:"last_update_at,omitempty"`
}

type State struct {
	Version     int                    `json:"version"`
	ScopeID     string                 `json:"scope_id,omitempty"`
	Initialized bool                   `json:"initialized"`
	Seen        map[string]bool        `json:"seen"`
	Online      map[string]bool        `json:"online"`
	Rearm       map[string]bool        `json:"rearm"`
	Grants      map[string]grantRecord `json:"grants"`
}

type Snapshot struct {
	Scope         playerpresence.Scope `json:"scope"`
	Config        Config               `json:"config"`
	Grants        []Grant              `json:"grants"`
	WorkerRunning bool                 `json:"worker_running"`
}

type Dispatcher interface {
	ResolvePlayer(context.Context, []string) (string, error)
	GiveItems(context.Context, string, []ItemGrant) error
	GivePalTemplates(context.Context, string, []string) error
}

type palDefenderDispatcher struct{ manager paldefender.Manager }

func NewPalDefenderDispatcher(manager paldefender.Manager) Dispatcher {
	return palDefenderDispatcher{manager: manager}
}

func (d palDefenderDispatcher) ResolvePlayer(ctx context.Context, aliases []string) (string, error) {
	response, err := d.manager.RESTPlayers(ctx)
	if err != nil {
		return "", err
	}
	return resolvePalDefenderPlayer(response.Players, aliases)
}

func resolvePalDefenderPlayer(players []paldefender.RESTPlayer, aliases []string) (string, error) {
	wanted := map[string]bool{}
	for _, alias := range aliases {
		if key := identity(alias); key != "" {
			wanted[key] = true
		}
	}
	if len(wanted) == 0 {
		return "", errors.New("player identifier is unavailable")
	}
	for _, player := range players {
		// /v1/pdapi/players is an account catalog and may include offline records.
		// Its Status value is not a reliable online-presence flag. The caller has
		// already limited dispatch to the official Palworld REST online list, so
		// matching by UserID/PlayerUID is sufficient and avoids permanently
		// skipping valid players whose Status is empty or an account-state value.
		if !wanted[identity(player.UserID)] && !wanted[identity(player.PlayerUID)] {
			continue
		}
		if resolved := firstNonEmpty(player.UserID, player.PlayerUID); resolved != "" {
			return resolved, nil
		}
	}
	return "", ErrPlayerNotReady
}

func (d palDefenderDispatcher) GiveItems(ctx context.Context, player string, items []ItemGrant) error {
	request := paldefender.GiveItemsRequest{Items: make([]paldefender.ItemGrant, 0, len(items))}
	for _, item := range items {
		request.Items = append(request.Items, paldefender.ItemGrant{ItemID: item.ItemID, Count: item.Count})
	}
	_, err := d.manager.RESTGiveItems(ctx, player, request)
	return err
}

func (d palDefenderDispatcher) GivePalTemplates(ctx context.Context, player string, names []string) error {
	_, err := d.manager.RESTGivePalTemplates(ctx, player, paldefender.GivePalTemplatesRequest{PalTemplates: names})
	return err
}

func DefaultConfig() Config {
	return Config{ItemBatchSize: 20, TemplateBatchSize: 5, BatchDelayMS: 500}
}

func EmptyState() State {
	return State{
		Version: Version,
		Seen:    map[string]bool{},
		Online:  map[string]bool{},
		Rearm:   map[string]bool{},
		Grants:  map[string]grantRecord{},
	}
}

func LoadSnapshot(ctx context.Context, store *db.Store, scope playerpresence.Scope) (Snapshot, error) {
	config, err := loadConfig(ctx, store, scope)
	if err != nil {
		return Snapshot{}, err
	}
	state, err := loadState(ctx, store, scope)
	if err != nil {
		return Snapshot{}, err
	}
	grants := make([]Grant, 0, len(state.Grants))
	for _, record := range state.Grants {
		grants = append(grants, grantView(record))
	}
	sort.Slice(grants, func(i, j int) bool {
		if grants[i].FirstSeenAt != grants[j].FirstSeenAt {
			return grants[i].FirstSeenAt > grants[j].FirstSeenAt
		}
		return grants[i].PlayerID < grants[j].PlayerID
	})
	workerMu.Lock()
	running := workerRunning[scope.StorageKey()]
	workerMu.Unlock()
	return Snapshot{Scope: scope, Config: config, Grants: grants, WorkerRunning: running}, nil
}

func InspectPlayers(ctx context.Context, store *db.Store, scope playerpresence.Scope, players []playerpresence.OnlinePlayer) ([]PlayerDecision, error) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state, err := loadState(ctx, store, scope)
	if err != nil {
		return nil, err
	}
	result := make([]PlayerDecision, 0, len(players)+len(state.Grants))
	covered := map[string]bool{}
	appendPlayer := func(player playerpresence.OnlinePlayer) {
		aliases := playerAliases(player)
		key := canonicalPlayerKey(player)
		if key == "" {
			return
		}
		for _, alias := range aliases {
			covered[alias] = true
		}
		grantKey := findGrantKey(state, aliases)
		grant, hasGrant := state.Grants[grantKey]
		if hasGrant {
			for _, alias := range grantAliases(grant) {
				covered[alias] = true
			}
		}
		seen := anyMarked(state.Seen, aliases)
		online := anyMarked(state.Online, aliases)
		rearmed := anyMarked(state.Rearm, aliases)
		decision := PlayerDecision{
			PlayerID: firstNonEmpty(player.SteamID, player.PlayerUID), PlayerUID: strings.TrimSpace(player.PlayerUID),
			SteamID: strings.TrimSpace(player.SteamID), Nickname: strings.TrimSpace(player.Nickname), Online: online,
			Seen: seen, Rearmed: rearmed, Evidence: []string{
				fmt.Sprintf("seen=%t", seen), fmt.Sprintf("online=%t", online), fmt.Sprintf("rearmed=%t", rearmed), fmt.Sprintf("grant=%t", hasGrant),
			},
		}
		switch {
		case hasGrant:
			decision.IsNew = true
			decision.Eligible = grant.Status != "success"
			decision.Decision = "starter_gift_created"
			decision.Reason = firstNonEmpty(grant.DetectionReason, "已创建初始礼包任务。")
			decision.GrantStatus = grant.Status
			decision.GrantPhase = grant.Phase
			decision.LastUpdateAt = grant.UpdatedAt
			decision.Evidence = append(decision.Evidence, "grant_key="+grantKey, "detection_source="+firstNonEmpty(grant.DetectionSource, "legacy"))
		case rearmed:
			decision.IsNew = true
			decision.Eligible = !online
			decision.Decision = "marked_new_next_login"
			if online {
				decision.Reason = "已人工标记为新玩家，但当前仍在线；必须先离线，再次进入后才会建立发放任务。"
			} else {
				decision.Reason = "已人工标记为新玩家，等待下一次进入服务器。"
			}
		case !seen:
			decision.IsNew = true
			decision.Eligible = true
			decision.Decision = "unseen_candidate"
			decision.Reason = "该玩家身份不在当前 WorldID 的已见基线中；在线采样时应判定为新玩家。"
		default:
			decision.IsNew = false
			decision.Eligible = false
			decision.Decision = "known_existing"
			decision.Reason = "该玩家身份已经存在于当前 WorldID 的已见基线中，且没有人工重置标记。"
		}
		result = append(result, decision)
	}
	for _, player := range players {
		appendPlayer(player)
	}
	for key, grant := range state.Grants {
		aliases := uniqueStrings(append([]string{identity(key)}, grantAliases(grant)...))
		skip := false
		for _, alias := range aliases {
			if covered[alias] {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		appendPlayer(playerpresence.OnlinePlayer{PlayerUID: grant.PlayerUID, SteamID: grant.SteamID, Nickname: grant.Nickname})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Online != result[j].Online {
			return result[i].Online
		}
		if result[i].IsNew != result[j].IsNew {
			return result[i].IsNew
		}
		return firstNonEmpty(result[i].Nickname, result[i].PlayerID) < firstNonEmpty(result[j].Nickname, result[j].PlayerID)
	})
	return result, nil
}

// BaselineKnownPlayers marks players already present in the server save index
// and player-presence history as existing players. It is safe to call whenever
// the administrator enables or updates the feature.
func BaselineKnownPlayers(ctx context.Context, store *db.Store, scope playerpresence.Scope, players []playerpresence.OnlinePlayer) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	state, err := loadState(ctx, store, scope)
	if err != nil {
		return err
	}
	presence, err := playerpresence.LoadScoped(ctx, store, scope)
	if err != nil {
		return err
	}
	for _, record := range presence.Players {
		markSeen(&state, recordAliases(record))
	}
	for _, player := range players {
		markSeen(&state, playerAliases(player))
	}
	state.Initialized = true
	return saveState(ctx, store, scope, state)
}

func ValidateConfig(config Config) (Config, error) {
	return normalizeConfig(config)
}

func SaveConfig(ctx context.Context, store *db.Store, scope playerpresence.Scope, config Config) (Snapshot, error) {
	config, err := normalizeConfig(config)
	if err != nil {
		return Snapshot{}, err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return Snapshot{}, err
	}
	if err := store.SetKV(ctx, configStorageKey(scope), string(raw)); err != nil {
		return Snapshot{}, err
	}
	// Keep the latest saved configuration as the template inherited by worlds
	// that do not have their own scoped configuration yet. Existing worlds stay
	// independent because their scoped value always takes precedence.
	if err := store.SetKV(ctx, ConfigKey, string(raw)); err != nil {
		return Snapshot{}, err
	}
	return LoadSnapshot(ctx, store, scope)
}

func Retry(ctx context.Context, store *db.Store, scope playerpresence.Scope, playerID string) error {
	return ApplyAction(ctx, store, scope, playerpresence.OnlinePlayer{SteamID: playerID}, "retry")
}

func ApplyAction(ctx context.Context, store *db.Store, scope playerpresence.Scope, player playerpresence.OnlinePlayer, action string) error {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		action = "retry"
	}
	aliases := playerAliases(player)
	if len(aliases) == 0 {
		return errors.New("player id is required")
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	config, err := loadConfig(ctx, store, scope)
	if err != nil {
		return err
	}
	state, err := loadState(ctx, store, scope)
	if err != nil {
		return err
	}
	key := findGrantKey(state, aliases)
	grant, found := state.Grants[key]
	nowText := time.Now().UTC().Format(time.RFC3339Nano)
	switch action {
	case "retry", "supplement":
		if !found {
			return errors.New("starter gift grant was not found")
		}
		if grant.Status == "success" {
			return errors.New("completed starter gift requires full reissue")
		}
		grant.Status = "pending"
		grant.Phase = "queued"
		grant.LastError = ""
		grant.UpdatedAt = nowText
		appendGrantEvent(&grant, "queued", "info", "管理员已将未完成任务加入补发队列。", nowText)
		state.Grants[key] = grant
	case "reissue", "grant_now", "mark_new_now":
		if !config.Enabled {
			return errors.New("starter gift must be enabled before a full reissue")
		}
		key = canonicalPlayerKey(player)
		if key == "" {
			return errors.New("player identifier is unavailable")
		}
		grant = newGrantWithReason(player, config, nowText, "manual", "管理员人工标记为新玩家并要求全量重新发放。", true)
		state.Grants[key] = grant
		markSeen(&state, aliases)
		clearMarked(state.Rearm, aliases)
	case "next_login", "mark_new", "rearm":
		if found {
			aliases = uniqueStrings(append(aliases, grantAliases(grant)...))
			delete(state.Grants, key)
		}
		markSeen(&state, aliases)
		for _, alias := range aliases {
			state.Rearm[alias] = true
		}
	case "cancel_next_login", "cancel_mark_new", "cancel_rearm":
		if found {
			aliases = uniqueStrings(append(aliases, grantAliases(grant)...))
		}
		markSeen(&state, aliases)
		clearMarked(state.Rearm, aliases)
	default:
		return fmt.Errorf("unsupported starter gift action %q", action)
	}
	return saveState(ctx, store, scope, state)
}

func ReconcilePlayerAliases(ctx context.Context, store *db.Store, scope playerpresence.Scope, players []playerpresence.OnlinePlayer) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	state, err := loadState(ctx, store, scope)
	if err != nil {
		return err
	}
	changed := false
	for _, player := range players {
		aliases := playerAliases(player)
		if len(aliases) < 2 {
			continue
		}
		seen, online, rearmed := anyMarked(state.Seen, aliases), anyMarked(state.Online, aliases), anyMarked(state.Rearm, aliases)
		for _, alias := range aliases {
			if seen && !state.Seen[alias] {
				state.Seen[alias] = true
				changed = true
			}
			if online && !state.Online[alias] {
				state.Online[alias] = true
				changed = true
			}
			if rearmed && !state.Rearm[alias] {
				state.Rearm[alias] = true
				changed = true
			}
		}
		keys := matchingGrantKeys(state, aliases)
		if len(keys) < 2 {
			if len(keys) == 1 {
				record := state.Grants[keys[0]]
				if enrichGrantIdentity(&record, player) {
					state.Grants[keys[0]] = record
					changed = true
				}
			}
			continue
		}
		sort.SliceStable(keys, func(i, j int) bool {
			return state.Grants[keys[i]].UpdatedAt > state.Grants[keys[j]].UpdatedAt
		})
		keepKey := keys[0]
		merged := state.Grants[keepKey]
		for _, duplicateKey := range keys[1:] {
			duplicate := state.Grants[duplicateKey]
			mergeGrantRecord(&merged, duplicate)
			delete(state.Grants, duplicateKey)
		}
		enrichGrantIdentity(&merged, player)
		canonical := canonicalPlayerKey(player)
		if canonical == "" {
			canonical = keepKey
		}
		delete(state.Grants, keepKey)
		state.Grants[canonical] = merged
		changed = true
	}
	if !changed {
		return nil
	}
	return saveState(ctx, store, scope, state)
}

func matchingGrantKeys(state State, aliases []string) []string {
	keys := make([]string, 0)
	for key, grant := range state.Grants {
		if containsString(aliases, identity(key)) {
			keys = append(keys, key)
			continue
		}
		for _, alias := range grantAliases(grant) {
			if containsString(aliases, alias) {
				keys = append(keys, key)
				break
			}
		}
	}
	return keys
}

func enrichGrantIdentity(record *grantRecord, player playerpresence.OnlinePlayer) bool {
	changed := false
	if player.PlayerUID != "" && record.PlayerUID != player.PlayerUID {
		record.PlayerUID = player.PlayerUID
		changed = true
	}
	if player.SteamID != "" && record.SteamID != player.SteamID {
		record.SteamID = player.SteamID
		record.PlayerID = player.SteamID
		changed = true
	}
	if record.Nickname == "" && player.Nickname != "" {
		record.Nickname = player.Nickname
		changed = true
	}
	return changed
}

func mergeGrantRecord(target *grantRecord, source grantRecord) {
	if target.PlayerUID == "" {
		target.PlayerUID = source.PlayerUID
	}
	if target.SteamID == "" {
		target.SteamID = source.SteamID
	}
	if target.Nickname == "" {
		target.Nickname = source.Nickname
	}
	if target.FirstSeenAt == "" || source.FirstSeenAt != "" && source.FirstSeenAt < target.FirstSeenAt {
		target.FirstSeenAt = source.FirstSeenAt
	}
	target.Events = append(target.Events, source.Events...)
	sort.SliceStable(target.Events, func(i, j int) bool { return target.Events[i].At < target.Events[j].At })
	if len(target.Events) > MaxGrantEvents {
		target.Events = append([]GrantEvent(nil), target.Events[len(target.Events)-MaxGrantEvents:]...)
	}
}

// Forget removes the existing result and rearms the player for the next real
// offline-to-online transition. A player who is still online is not granted
// again on the next monitor tick.
func Forget(ctx context.Context, store *db.Store, scope playerpresence.Scope, playerID string) error {
	keyInput := identity(playerID)
	if keyInput == "" {
		return errors.New("player id is required")
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	state, err := loadState(ctx, store, scope)
	if err != nil {
		return err
	}
	key := findGrantKey(state, []string{keyInput})
	grant, found := state.Grants[key]
	if !found {
		return errors.New("starter gift grant was not found")
	}
	aliases := grantAliases(grant)
	for _, alias := range aliases {
		state.Seen[alias] = true
		state.Rearm[alias] = true
	}
	delete(state.Grants, key)
	return saveState(ctx, store, scope, state)
}

// Observe is called after playerpresence.Observe. On its first run it imports
// the complete presence-history player set as a baseline, including players
// who are currently offline, so enabling the feature does not backfill old
// players. Later unseen identities receive a frozen copy of the current plan.
func Observe(ctx context.Context, store *db.Store, scope playerpresence.Scope, now time.Time, online []playerpresence.OnlinePlayer, dispatcher Dispatcher) error {
	stateMu.Lock()
	config, err := loadConfig(ctx, store, scope)
	if err != nil {
		stateMu.Unlock()
		return err
	}
	state, err := loadState(ctx, store, scope)
	if err != nil {
		stateMu.Unlock()
		return err
	}
	nowText := now.UTC().Format(time.RFC3339Nano)

	if !state.Initialized {
		state.Online = onlineAliasSet(online)
		state.Initialized = true
		for _, player := range online {
			aliases := playerAliases(player)
			if len(aliases) == 0 {
				continue
			}
			if config.Enabled {
				key := canonicalPlayerKey(player)
				if key != "" {
					state.Grants[key] = newGrantWithReason(player, config, nowText, "automatic", "功能首次初始化时发现当前在线身份，已建立初始礼包任务。", false)
				}
			}
			markSeen(&state, aliases)
		}
		if err := saveState(ctx, store, scope, state); err != nil {
			stateMu.Unlock()
			return err
		}
		stateMu.Unlock()
		if config.Enabled && dispatcher != nil {
			startWorker(ctx, store, scope, dispatcher, online)
		}
		return nil
	}

	previousOnline := state.Online
	currentOnline := onlineAliasSet(online)
	for _, player := range online {
		aliases := playerAliases(player)
		if len(aliases) == 0 {
			continue
		}
		wasSeen := anyMarked(state.Seen, aliases)
		wasOnline := anyMarked(previousOnline, aliases)
		rearmed := anyMarked(state.Rearm, aliases)
		markSeen(&state, aliases)

		shouldGrant := false
		detectionReason := "该身份不在当前 WorldID 的已见基线中，首次在线采样判定为新玩家。"
		detectionSource := "automatic"
		manual := false
		if rearmed && !wasOnline {
			if config.Enabled {
				shouldGrant = true
				detectionReason = "管理员已标记该玩家为新玩家，并且检测到离线后再次进入。"
				detectionSource = "manual-next-login"
				manual = true
				clearMarked(state.Rearm, aliases)
			}
		} else if !wasSeen {
			shouldGrant = config.Enabled
		}
		if !shouldGrant {
			continue
		}

		key := canonicalPlayerKey(player)
		if key == "" {
			continue
		}
		if existing := findGrantKey(state, aliases); existing != "" {
			continue
		}
		state.Grants[key] = newGrantWithReason(player, config, nowText, detectionSource, detectionReason, manual)
	}
	state.Online = currentOnline
	if err := saveState(ctx, store, scope, state); err != nil {
		stateMu.Unlock()
		return err
	}
	stateMu.Unlock()

	if config.Enabled && dispatcher != nil {
		startWorker(ctx, store, scope, dispatcher, online)
	}
	return nil
}

func startWorker(ctx context.Context, store *db.Store, scope playerpresence.Scope, dispatcher Dispatcher, online []playerpresence.OnlinePlayer) {
	workerKey := scope.StorageKey()
	workerMu.Lock()
	if workerRunning[workerKey] {
		workerMu.Unlock()
		return
	}
	workerRunning[workerKey] = true
	workerMu.Unlock()

	players := append([]playerpresence.OnlinePlayer(nil), online...)
	go func() {
		defer func() {
			workerMu.Lock()
			delete(workerRunning, workerKey)
			workerMu.Unlock()
		}()
		workerCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), starterGiftWorkerTimeout)
		defer cancel()
		processOnline(workerCtx, store, scope, dispatcher, players)
	}()
}

func processOnline(ctx context.Context, store *db.Store, scope playerpresence.Scope, dispatcher Dispatcher, online []playerpresence.OnlinePlayer) {
	keys := make([]string, 0, len(online))
	stateMu.Lock()
	state, err := loadState(ctx, store, scope)
	if err == nil {
		for _, player := range online {
			if key := findGrantKey(state, playerAliases(player)); key != "" {
				keys = append(keys, key)
			}
		}
	}
	stateMu.Unlock()
	if err != nil {
		return
	}
	sort.Strings(keys)
	keys = uniqueStrings(keys)
	for _, key := range keys {
		if ctx.Err() != nil {
			return
		}
		_ = processOne(ctx, store, scope, dispatcher, key)
	}
}

func processOne(ctx context.Context, store *db.Store, scope playerpresence.Scope, dispatcher Dispatcher, key string) error {
	attemptStarted := false
	playerID := ""
	for {
		stateMu.Lock()
		config, err := loadConfig(ctx, store, scope)
		if err != nil {
			stateMu.Unlock()
			return err
		}
		state, err := loadState(ctx, store, scope)
		if err != nil {
			stateMu.Unlock()
			return err
		}
		grant, found := state.Grants[key]
		if !found || grant.Status == "success" {
			stateMu.Unlock()
			return nil
		}
		if grant.Status == "failed" && retryablePlayerReadinessError(grant.LastError) {
			grant.Status = "pending"
			grant.Phase = "queued"
			grant.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			appendGrantEvent(&grant, "queued", "info", "检测到旧版可重试的玩家解析错误，任务已自动重新排队。", grant.UpdatedAt)
			state.Grants[key] = grant
			if err := saveState(ctx, store, scope, state); err != nil {
				stateMu.Unlock()
				return err
			}
		}
		if !config.Enabled {
			if grant.Status == "running" {
				grant.Status = "pending"
				grant.Phase = "paused"
				grant.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
				appendGrantEvent(&grant, "paused", "warning", "初始礼包功能已停用，任务暂停。", grant.UpdatedAt)
				state.Grants[key] = grant
				_ = saveState(ctx, store, scope, state)
			}
			stateMu.Unlock()
			return nil
		}
		if grant.Status != "pending" && grant.Status != "running" {
			stateMu.Unlock()
			return nil
		}
		if playerID == "" {
			aliases := grantAliases(grant)
			nowText := time.Now().UTC().Format(time.RFC3339Nano)
			grant.Phase = "resolving_player"
			grant.UpdatedAt = nowText
			appendGrantEvent(&grant, "resolving_player", "info", "正在通过 PalDefender 账户目录解析实际发放目标。", nowText)
			state.Grants[key] = grant
			if err := saveState(ctx, store, scope, state); err != nil {
				stateMu.Unlock()
				return err
			}
			stateMu.Unlock()
			resolved, resolveErr := dispatcher.ResolvePlayer(ctx, aliases)
			if resolveErr != nil {
				_ = keepGrantPending(ctx, store, scope, key, resolveErr)
				if errors.Is(resolveErr, ErrPlayerNotReady) {
					return nil
				}
				return resolveErr
			}
			playerID = strings.TrimSpace(resolved)
			if playerID == "" {
				err := errors.New("resolved player identifier is empty")
				_ = keepGrantPending(ctx, store, scope, key, err)
				return err
			}
			stateMu.Lock()
			state, err = loadState(ctx, store, scope)
			if err != nil {
				stateMu.Unlock()
				return err
			}
			current, ok := state.Grants[key]
			if !ok {
				stateMu.Unlock()
				return nil
			}
			current.ResolvedPlayerID = playerID
			current.Phase = "ready"
			current.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			appendGrantEvent(&current, "ready", "success", "已解析发放目标："+playerID, current.UpdatedAt)
			state.Grants[key] = current
			err = saveState(ctx, store, scope, state)
			stateMu.Unlock()
			if err != nil {
				return err
			}
			continue
		}
		if !attemptStarted {
			grant.Attempts++
			attemptStarted = true
		}
		grant.Status = "running"
		grant.LastError = ""
		grant.ResolvedPlayerID = playerID
		grant.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)

		batchKind := ""
		batchStart, batchEnd := 0, 0
		if grant.NextItem < len(grant.PlanItems) {
			batchKind = "items"
			batchStart = grant.NextItem
			batchEnd = min(grant.NextItem+config.ItemBatchSize, len(grant.PlanItems))
			grant.Phase = "items"
			appendGrantBatchEvent(&grant, "items", "info", fmt.Sprintf("开始发放物品批次 %d–%d / %d。", batchStart+1, batchEnd, len(grant.PlanItems)), grant.UpdatedAt, batchStart, batchEnd, 0, 0)
		} else if grant.NextTemplate < len(grant.PlanTemplates) {
			batchKind = "templates"
			batchStart = grant.NextTemplate
			batchEnd = min(grant.NextTemplate+config.TemplateBatchSize, len(grant.PlanTemplates))
			grant.Phase = "templates"
			appendGrantBatchEvent(&grant, "templates", "info", fmt.Sprintf("开始发放帕鲁模板批次 %d–%d / %d。", batchStart+1, batchEnd, len(grant.PlanTemplates)), grant.UpdatedAt, 0, 0, batchStart, batchEnd)
		} else {
			grant.Status = "success"
			grant.Phase = "completed"
			grant.CompletedAt = grant.UpdatedAt
			appendGrantEvent(&grant, "completed", "success", "所有物品与帕鲁模板均已完成。", grant.UpdatedAt)
			state.Grants[key] = grant
			err := saveState(ctx, store, scope, state)
			stateMu.Unlock()
			return err
		}
		state.Grants[key] = grant
		if err := saveState(ctx, store, scope, state); err != nil {
			stateMu.Unlock()
			return err
		}
		stateMu.Unlock()

		var sendErr error
		if batchKind == "items" {
			sendErr = dispatcher.GiveItems(ctx, playerID, grant.PlanItems[batchStart:batchEnd])
			if sendErr == nil {
				grant.NextItem = batchEnd
			}
		} else {
			sendErr = dispatcher.GivePalTemplates(ctx, playerID, grant.PlanTemplates[batchStart:batchEnd])
			if sendErr == nil {
				grant.NextTemplate = batchEnd
			}
		}

		stateMu.Lock()
		state, loadErr := loadState(ctx, store, scope)
		if loadErr != nil {
			stateMu.Unlock()
			return loadErr
		}
		current, stillFound := state.Grants[key]
		if !stillFound {
			stateMu.Unlock()
			return nil
		}
		current.NextItem = grant.NextItem
		current.NextTemplate = grant.NextTemplate
		current.Attempts = grant.Attempts
		current.ResolvedPlayerID = playerID
		current.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if sendErr != nil {
			current.Status = "failed"
			current.Phase = "failed"
			current.LastError = boundedError(sendErr)
			appendGrantEvent(&current, "failed", "error", fmt.Sprintf("%s批次发放失败：%s", batchKind, current.LastError), current.UpdatedAt)
		} else if current.NextItem >= len(current.PlanItems) && current.NextTemplate >= len(current.PlanTemplates) {
			current.Status = "success"
			current.Phase = "completed"
			current.LastError = ""
			current.CompletedAt = current.UpdatedAt
			appendGrantEvent(&current, "completed", "success", "所有物品与帕鲁模板均已完成。", current.UpdatedAt)
		} else {
			current.Status = "running"
			current.Phase = batchKind
			current.LastError = ""
			if batchKind == "items" {
				appendGrantBatchEvent(&current, "items", "success", fmt.Sprintf("物品批次已完成，当前进度 %d / %d。", current.NextItem, len(current.PlanItems)), current.UpdatedAt, batchStart, batchEnd, 0, 0)
			} else {
				appendGrantBatchEvent(&current, "templates", "success", fmt.Sprintf("帕鲁模板批次已完成，当前进度 %d / %d。", current.NextTemplate, len(current.PlanTemplates)), current.UpdatedAt, 0, 0, batchStart, batchEnd)
			}
		}
		state.Grants[key] = current
		saveErr := saveState(ctx, store, scope, state)
		stateMu.Unlock()
		if saveErr != nil {
			return saveErr
		}
		if sendErr != nil || current.Status == "success" {
			return sendErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(config.BatchDelayMS) * time.Millisecond):
		}
	}
}

func keepGrantPending(ctx context.Context, store *db.Store, scope playerpresence.Scope, key string, reason error) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	state, err := loadState(ctx, store, scope)
	if err != nil {
		return err
	}
	grant, found := state.Grants[key]
	if !found || grant.Status == "success" {
		return nil
	}
	grant.Status = "pending"
	grant.Phase = "waiting_player"
	grant.LastError = boundedError(reason)
	grant.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	appendGrantEvent(&grant, "waiting_player", "warning", "暂未解析到 PalDefender 玩家账户，保持待处理并等待下一次在线采样："+grant.LastError, grant.UpdatedAt)
	state.Grants[key] = grant
	return saveState(ctx, store, scope, state)
}

func retryablePlayerReadinessError(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(message, "player_not_found") ||
		strings.Contains(message, "player was not found") ||
		strings.Contains(message, "has not registered the online player yet")
}

func normalizeConfig(config Config) (Config, error) {
	if config.ItemBatchSize == 0 {
		config.ItemBatchSize = 20
	}
	if config.TemplateBatchSize == 0 {
		config.TemplateBatchSize = 5
	}
	if config.BatchDelayMS == 0 {
		config.BatchDelayMS = 500
	}
	if config.ItemBatchSize < 1 || config.ItemBatchSize > 100 {
		return Config{}, errors.New("item_batch_size must be between 1 and 100")
	}
	if config.TemplateBatchSize < 1 || config.TemplateBatchSize > 20 {
		return Config{}, errors.New("template_batch_size must be between 1 and 20")
	}
	if config.BatchDelayMS < 100 || config.BatchDelayMS > 10000 {
		return Config{}, errors.New("batch_delay_ms must be between 100 and 10000")
	}
	if len(config.Items) > MaxItems {
		return Config{}, fmt.Errorf("items must contain at most %d entries", MaxItems)
	}
	if len(config.PalTemplates) > MaxPalTemplates {
		return Config{}, fmt.Errorf("pal_templates must contain at most %d entries", MaxPalTemplates)
	}

	seenItems := map[string]bool{}
	items := make([]ItemGrant, 0, len(config.Items))
	for _, item := range config.Items {
		item.ItemID = strings.TrimSpace(item.ItemID)
		if !itemIDPattern.MatchString(item.ItemID) {
			return Config{}, fmt.Errorf("invalid item_id %q", item.ItemID)
		}
		if item.Count < 1 || item.Count > 2_147_483_647 {
			return Config{}, fmt.Errorf("item %s count must be between 1 and 2147483647", item.ItemID)
		}
		if seenItems[item.ItemID] {
			return Config{}, fmt.Errorf("duplicate item_id %s", item.ItemID)
		}
		seenItems[item.ItemID] = true
		items = append(items, item)
	}

	seenTemplates := map[string]bool{}
	templates := make([]string, 0, len(config.PalTemplates))
	for _, name := range config.PalTemplates {
		name = strings.TrimSpace(name)
		if name == "" {
			return Config{}, errors.New("Pal template names must not be empty")
		}
		if len(name) > 255 || strings.ContainsRune(name, '\x00') {
			return Config{}, fmt.Errorf("invalid Pal template name %q", name)
		}
		if seenTemplates[name] {
			continue
		}
		seenTemplates[name] = true
		templates = append(templates, name)
	}
	if config.Enabled && len(items) == 0 && len(templates) == 0 {
		return Config{}, errors.New("enabled starter gift requires at least one item or Pal template")
	}
	config.Items = items
	config.PalTemplates = templates
	return config, nil
}

func configStorageKey(scope playerpresence.Scope) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(scope.ID)))
	return fmt.Sprintf("%s%x", ScopedConfigPrefix, digest[:16])
}

func loadConfig(ctx context.Context, store *db.Store, scope playerpresence.Scope) (Config, error) {
	config, found, err := loadConfigFromKey(ctx, store, configStorageKey(scope))
	if err != nil || found {
		return config, err
	}
	config, legacyFound, err := loadConfigFromKey(ctx, store, ConfigKey)
	if err != nil {
		return DefaultConfig(), err
	}
	if !legacyFound {
		return DefaultConfig(), nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return DefaultConfig(), err
	}
	if err := store.SetKV(ctx, configStorageKey(scope), string(raw)); err != nil {
		return DefaultConfig(), err
	}
	return config, nil
}

func loadConfigFromKey(ctx context.Context, store *db.Store, key string) (Config, bool, error) {
	config := DefaultConfig()
	raw, found, err := store.GetKV(ctx, key)
	if err != nil {
		return config, false, err
	}
	if !found || strings.TrimSpace(raw) == "" {
		return config, false, nil
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return DefaultConfig(), false, err
	}
	normalized, err := normalizeConfig(config)
	return normalized, true, err
}

func stateStorageKey(scope playerpresence.Scope) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(scope.ID)))
	return fmt.Sprintf("%s%x", ScopedStatePrefix, digest[:16])
}

func loadState(ctx context.Context, store *db.Store, scope playerpresence.Scope) (State, error) {
	state, found, err := loadStateKey(ctx, store, stateStorageKey(scope), scope.ID)
	if err != nil || found {
		return state, err
	}
	if scope.AllowLegacyMigration {
		legacy, legacyFound, legacyErr := loadStateKey(ctx, store, StateKey, scope.ID)
		if legacyErr != nil {
			return EmptyState(), legacyErr
		}
		if legacyFound {
			if err := saveState(ctx, store, scope, legacy); err != nil {
				return EmptyState(), err
			}
			return legacy, nil
		}
	}
	state = EmptyState()
	state.ScopeID = scope.ID
	return state, nil
}

func loadStateKey(ctx context.Context, store *db.Store, key, scopeID string) (State, bool, error) {
	state := EmptyState()
	state.ScopeID = scopeID
	raw, found, err := store.GetKV(ctx, key)
	if err != nil || !found || strings.TrimSpace(raw) == "" {
		return state, found, err
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return EmptyState(), false, err
	}
	if state.Seen == nil {
		state.Seen = map[string]bool{}
	}
	if state.Online == nil {
		state.Online = map[string]bool{}
	}
	if state.Rearm == nil {
		state.Rearm = map[string]bool{}
	}
	if state.Grants == nil {
		state.Grants = map[string]grantRecord{}
	}
	for key, grant := range state.Grants {
		if grant.Phase == "" {
			switch grant.Status {
			case "success":
				grant.Phase = "completed"
			case "failed":
				grant.Phase = "failed"
			case "running":
				grant.Phase = "running"
			default:
				grant.Phase = "queued"
			}
		}
		if grant.DetectionSource == "" {
			grant.DetectionSource = "legacy"
		}
		if grant.DetectionReason == "" {
			grant.DetectionReason = "旧版本已创建的初始礼包任务；缺少原始判定证据。"
		}
		state.Grants[key] = grant
	}
	state.Version = Version
	state.ScopeID = scopeID
	return state, true, nil
}

func saveState(ctx context.Context, store *db.Store, scope playerpresence.Scope, state State) error {
	state.Version = Version
	state.ScopeID = scope.ID
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return store.SetKV(ctx, stateStorageKey(scope), string(raw))
}

func newGrant(player playerpresence.OnlinePlayer, config Config, nowText string) grantRecord {
	return newGrantWithReason(player, config, nowText, "automatic", "首次在线采样判定为新玩家。", false)
}

func newGrantWithReason(player playerpresence.OnlinePlayer, config Config, nowText, source, reason string, manual bool) grantRecord {
	record := grantRecord{
		PlayerID: firstNonEmpty(player.SteamID, player.PlayerUID), PlayerUID: strings.TrimSpace(player.PlayerUID),
		SteamID: strings.TrimSpace(player.SteamID), Nickname: strings.TrimSpace(player.Nickname), Status: "pending", Phase: "queued",
		DetectionSource: source, DetectionReason: reason, Manual: manual, FirstSeenAt: nowText, UpdatedAt: nowText,
		PlanItems: append([]ItemGrant(nil), config.Items...), PlanTemplates: append([]string(nil), config.PalTemplates...),
	}
	appendGrantEvent(&record, "queued", "info", reason, nowText)
	return record
}

func grantView(record grantRecord) Grant {
	total := len(record.PlanItems) + len(record.PlanTemplates)
	done := record.NextItem + record.NextTemplate
	progress := 0
	if total > 0 {
		progress = min(100, done*100/total)
	}
	if record.Status == "success" {
		progress = 100
	}
	return Grant{
		PlayerID: record.PlayerID, PlayerUID: record.PlayerUID, SteamID: record.SteamID,
		Nickname: record.Nickname, Status: record.Status, Phase: record.Phase,
		DetectionSource: record.DetectionSource, DetectionReason: record.DetectionReason, Manual: record.Manual,
		ResolvedPlayerID: record.ResolvedPlayerID, NextItem: record.NextItem, NextTemplate: record.NextTemplate,
		ItemTotal: len(record.PlanItems), TemplateTotal: len(record.PlanTemplates), ProgressPercent: progress,
		Attempts: record.Attempts, FirstSeenAt: record.FirstSeenAt, UpdatedAt: record.UpdatedAt,
		CompletedAt: record.CompletedAt, LastError: record.LastError, Events: append([]GrantEvent(nil), record.Events...),
	}
}

func appendGrantEvent(record *grantRecord, phase, level, message, at string) {
	if record == nil {
		return
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if at == "" {
		at = time.Now().UTC().Format(time.RFC3339Nano)
	}
	record.Events = append(record.Events, GrantEvent{At: at, Phase: phase, Level: level, Message: message})
	if len(record.Events) > MaxGrantEvents {
		record.Events = append([]GrantEvent(nil), record.Events[len(record.Events)-MaxGrantEvents:]...)
	}
}

func appendGrantBatchEvent(record *grantRecord, phase, level, message, at string, itemFrom, itemTo, templateFrom, templateTo int) {
	appendGrantEvent(record, phase, level, message, at)
	if len(record.Events) == 0 {
		return
	}
	event := &record.Events[len(record.Events)-1]
	event.ItemFrom, event.ItemTo = itemFrom, itemTo
	event.TemplateFrom, event.TemplateTo = templateFrom, templateTo
}

func onlineAliasSet(players []playerpresence.OnlinePlayer) map[string]bool {
	result := map[string]bool{}
	for _, player := range players {
		for _, alias := range playerAliases(player) {
			result[alias] = true
		}
	}
	return result
}

func playerAliases(player playerpresence.OnlinePlayer) []string {
	return uniqueStrings([]string{identity(player.SteamID), identity(player.PlayerUID)})
}

func recordAliases(record playerpresence.Record) []string {
	return uniqueStrings([]string{identity(record.SteamID), identity(record.PlayerUID)})
}

func grantAliases(record grantRecord) []string {
	return uniqueStrings([]string{identity(record.PlayerID), identity(record.SteamID), identity(record.PlayerUID)})
}

func canonicalPlayerKey(player playerpresence.OnlinePlayer) string {
	return firstNonEmpty(identity(player.SteamID), identity(player.PlayerUID))
}

func findGrantKey(state State, aliases []string) string {
	for _, alias := range aliases {
		if _, found := state.Grants[alias]; found {
			return alias
		}
	}
	for key, grant := range state.Grants {
		for _, alias := range aliases {
			if alias != "" && containsString(grantAliases(grant), alias) {
				return key
			}
		}
	}
	return ""
}

func markSeen(state *State, aliases []string) {
	for _, alias := range aliases {
		if alias != "" {
			state.Seen[alias] = true
		}
	}
}

func anyMarked(values map[string]bool, aliases []string) bool {
	for _, alias := range aliases {
		if alias != "" && values[alias] {
			return true
		}
	}
	return false
}

func clearMarked(values map[string]bool, aliases []string) {
	for _, alias := range aliases {
		delete(values, alias)
	}
}

func identity(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	switch value {
	case "", "none", "null", "undefined":
		return ""
	}
	if strings.Trim(value, "0") == "" {
		return ""
	}
	return value
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func boundedError(err error) string {
	text := strings.TrimSpace(err.Error())
	if len(text) > 500 {
		text = text[:500]
	}
	return text
}
