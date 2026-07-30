package saveindex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"palpanel/internal/appconfig"
)

var ErrDisabled = errors.New("save indexer is disabled")

const (
	staleAfterRebuildFailureWarning = "returning stale save index after rebuild failed"
	oodleHealthTimeout              = time.Second
	oodleHealthCacheTTL             = 5 * time.Second
)

var (
	escapedBytePattern         = regexp.MustCompile(`\\[xX][0-9A-Fa-f]{2}`)
	windowsAbsolutePathPattern = regexp.MustCompile(`(?i)(?:[a-z]:[\\/]|\\\\)[^\s"'<>|]+`)
	httpURLPattern             = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)
)

type Manager struct {
	cfg                appconfig.Config
	client             *http.Client
	mu                 sync.Mutex
	sourceMu           sync.RWMutex
	sourcePath         string
	cacheMu            sync.Mutex
	cache              *cacheFile
	cacheMTime         time.Time
	cacheLoaded        bool
	oodleMu            sync.Mutex
	oodleAt            time.Time
	oodle              *bool
	historyMu          sync.Mutex
	historyMinInterval time.Duration

	autoMu          sync.Mutex
	rebuildInFlight bool
	lastAutoRebuild time.Time
}

// autoRebuildDebounce throttles access-driven rebuilds so a rapidly autosaving
// server cannot trigger a parse storm.
const autoRebuildDebounce = 10 * time.Second

func NewManager(cfg appconfig.Config) *Manager {
	timeout := time.Duration(cfg.SaveIndexTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	historyMinutes := cfg.SaveHistoryIntervalMinutes
	if historyMinutes <= 0 {
		historyMinutes = 15
	}
	if historyMinutes > 24*60 {
		historyMinutes = 24 * 60
	}
	return &Manager{
		cfg:                cfg,
		client:             &http.Client{Timeout: timeout},
		historyMinInterval: time.Duration(historyMinutes) * time.Minute,
	}
}

type Coordinates struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type Player struct {
	PlayerUID        string         `json:"player_uid"`
	SteamID          string         `json:"steam_id"`
	Nickname         string         `json:"nickname"`
	Level            int            `json:"level"`
	GuildID          string         `json:"guild_id"`
	GuildName        string         `json:"guild_name"`
	IsOnline         bool           `json:"is_online"`
	LastOnlineTime   string         `json:"last_online_time"`
	Location         Coordinates    `json:"location"`
	InventorySummary map[string]any `json:"inventory_summary,omitempty"`
	Ping             *float64       `json:"ping,omitempty"`
	IP               string         `json:"ip,omitempty"`
	Raw              any            `json:"-"`
}

type GuildMember struct {
	PlayerUID      string `json:"player_uid"`
	Nickname       string `json:"nickname"`
	LastOnlineTime string `json:"last_online_time,omitempty"`
}

type Guild struct {
	ID                string        `json:"id"`
	Name              string        `json:"name"`
	OwnerPlayerUID    string        `json:"owner_player_uid"`
	Members           []GuildMember `json:"members"`
	BaseIDs           []string      `json:"base_ids"`
	OnlineMemberCount int           `json:"online_member_count"`
	Raw               any           `json:"-"`
}

type Worker struct {
	InstanceID  string `json:"instance_id"`
	CharacterID string `json:"character_id"`
	Nickname    string `json:"nickname,omitempty"`
	Level       int    `json:"level,omitempty"`
}

type Base struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	GuildID         string      `json:"guild_id"`
	GuildName       string      `json:"guild_name"`
	Location        Coordinates `json:"location"`
	StructuresCount int         `json:"structures_count"`
	Workers         []Worker    `json:"workers"`
	Containers      []string    `json:"containers"`
	Status          string      `json:"status"`
	Raw             any         `json:"-"`
}

type WorkSuitability struct {
	Type  string `json:"type"`
	Level int    `json:"level"`
}

type Pal struct {
	InstanceID      string            `json:"instance_id"`
	CharacterID     string            `json:"character_id"`
	Nickname        string            `json:"nickname"`
	Level           int               `json:"level"`
	OwnerPlayerUID  string            `json:"owner_player_uid"`
	OldOwnerUIDs    []string          `json:"old_owner_uids"`
	GuildID         string            `json:"guild_id"`
	ContainerID     string            `json:"container_id"`
	SlotIndex       int               `json:"slot_index"`
	LocationType    string            `json:"location_type"`
	Location        Coordinates       `json:"location"`
	Gender          string            `json:"gender"`
	Rank            int               `json:"rank"`
	IVHP            int               `json:"iv_hp"`
	IVAttack        int               `json:"iv_attack"`
	IVDefense       int               `json:"iv_defense"`
	Skills          []string          `json:"skills"`
	EquippedSkills  []string          `json:"equipped_skills"`
	Passives        []string          `json:"passives"`
	WorkSuitability []WorkSuitability `json:"work_suitability"`
	Health          *int64            `json:"health,omitempty"`
	Sanity          *float64          `json:"sanity,omitempty"`
	FullStomach     *float64          `json:"full_stomach,omitempty"`
	IsSick          bool              `json:"is_sick"`
	OnExpedition    bool              `json:"on_expedition"`
	Status          string            `json:"status"`
	Raw             any               `json:"-"`
}

type Slot struct {
	Slot       int      `json:"slot"`
	ItemID     string   `json:"item_id"`
	Count      int      `json:"count"`
	Durability *float64 `json:"durability,omitempty"`
}

type Container struct {
	ContainerID   string `json:"container_id"`
	ContainerType string `json:"container_type,omitempty"`
	OwnerType     string `json:"owner_type"`
	OwnerID       string `json:"owner_id"`
	Slots         []Slot `json:"slots"`
}

type MapEntity struct {
	Type     string      `json:"type"`
	ID       string      `json:"id"`
	Label    string      `json:"label"`
	Location Coordinates `json:"location"`
}

type Counts struct {
	Players     int `json:"players"`
	Guilds      int `json:"guilds"`
	Bases       int `json:"bases"`
	Pals        int `json:"pals"`
	Containers  int `json:"containers"`
	MapEntities int `json:"map_entities"`
}

type SnapshotFile struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
}

type Snapshot struct {
	Fingerprint string         `json:"fingerprint"`
	Files       []SnapshotFile `json:"files,omitempty"`
}

type Index struct {
	Version     int         `json:"version"`
	SourcePath  string      `json:"source_path"`
	GeneratedAt string      `json:"generated_at"`
	DurationMS  int         `json:"duration_ms"`
	Parser      string      `json:"parser"`
	Warnings    []string    `json:"warnings"`
	Players     []Player    `json:"players"`
	Guilds      []Guild     `json:"guilds"`
	Bases       []Base      `json:"bases"`
	Pals        []Pal       `json:"pals"`
	Containers  []Container `json:"containers"`
	MapEntities []MapEntity `json:"map_entities"`
	Snapshot    Snapshot    `json:"snapshot"`
	Counts      Counts      `json:"counts"`
}

type Status struct {
	Enabled        bool     `json:"enabled"`
	State          string   `json:"state"`
	Stale          bool     `json:"stale"`
	SourcePath     string   `json:"source_path"`
	UpdatedAt      string   `json:"updated_at"`
	DurationMS     int      `json:"duration_ms"`
	Error          string   `json:"error,omitempty"`
	ErrorCode      string   `json:"error_code,omitempty"`
	ErrorDetail    string   `json:"error_detail,omitempty"`
	OodleAvailable *bool    `json:"oodle_available,omitempty"`
	Warnings       []string `json:"warnings"`
	Counts         Counts   `json:"counts"`
	Parser         string   `json:"parser,omitempty"`
	CachePath      string   `json:"cache_path,omitempty"`
}

type cacheFile struct {
	Fingerprint string `json:"fingerprint"`
	SavedAt     string `json:"saved_at"`
	Index       Index  `json:"index"`
	Status      Status `json:"status"`
}

type sidecarEnvelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *sidecarError   `json:"error"`
	Warnings []string        `json:"warnings"`
	Trace    string          `json:"trace"`
}

type sidecarError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// IndexerError wraps a failure reported by the sav-cli sidecar. Only an
// enumerated Code and bounded, redacted Detail may be exposed to API clients.
type IndexerError struct {
	Code     string
	Detail   string
	Summary  string
	Warnings []string
}

func (e *IndexerError) Error() string {
	if e != nil && e.Summary != "" {
		return e.Summary
	}
	if e == nil || e.Code == "" {
		return "save indexer failed; inspect the sav-cli text logs"
	}
	return "save indexer failed (" + e.Code + "); inspect the sav-cli text logs"
}

// knownIndexerCodes is the allowlist of sidecar error codes that are safe to
// surface to the panel. Codes outside this set are discarded so an untrusted
// or corrupted sidecar cannot inject arbitrary text into diagnostics.
var knownIndexerCodes = map[string]bool{
	"save_indexer_unavailable": true,
	"save_index_timeout":       true,
	"parser_incompatible":      true,
	"save_path_not_found":      true,
	"level_sav_not_found":      true,
	"index_failed":             true,
	"save_path_required":       true,
	"bad_request":              true,
	"method_not_allowed":       true,
	"json_encode_failed":       true,
}

func safeIndexerCode(code string) string {
	code = strings.TrimSpace(code)
	if knownIndexerCodes[code] {
		return code
	}
	return ""
}

func safeIndexerDetail(detail string, privatePaths ...string) string {
	detail = strings.ToValidUTF8(detail, "")
	detail = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, detail)
	detail = escapedBytePattern.ReplaceAllString(detail, "[byte]")
	for _, privatePath := range privatePaths {
		privatePath = strings.TrimSpace(privatePath)
		if privatePath == "" {
			continue
		}
		variants := map[string]bool{
			privatePath:                               true,
			filepath.Clean(privatePath):               true,
			strings.ReplaceAll(privatePath, `\`, "/"): true,
			strings.ReplaceAll(privatePath, "/", `\`): true,
		}
		for variant := range variants {
			detail = replaceFold(detail, variant, "<redacted>")
		}
	}
	detail = windowsAbsolutePathPattern.ReplaceAllString(detail, "<redacted>")
	detail = httpURLPattern.ReplaceAllString(detail, "<redacted>")
	detail = strings.Join(strings.Fields(detail), " ")
	runes := []rune(detail)
	if len(runes) > 512 {
		detail = string(runes[:512]) + "…"
	}
	return detail
}

func replaceFold(value, target, replacement string) string {
	if target == "" {
		return value
	}
	pattern, err := regexp.Compile(`(?i:` + regexp.QuoteMeta(target) + `)`)
	if err != nil {
		return value
	}
	return pattern.ReplaceAllString(value, replacement)
}

func classifyIndexerRequestError(err error) *IndexerError {
	code := "save_indexer_unavailable"
	var timeout interface{ Timeout() bool }
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		code = "save_index_timeout"
	}
	return &IndexerError{Code: code}
}

func EmptyIndex() Index {
	return Index{
		Version:     1,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Warnings:    []string{},
		Players:     []Player{},
		Guilds:      []Guild{},
		Bases:       []Base{},
		Pals:        []Pal{},
		Containers:  []Container{},
		MapEntities: []MapEntity{},
	}
}

func (m *Manager) Status(ctx context.Context) (status Status) {
	defer func() {
		status = m.withOodleStatus(ctx, status)
	}()
	if !m.cfg.SaveIndexerEnabled {
		return Status{Enabled: false, State: "disabled", Warnings: []string{}}
	}
	worldDir, fp, err := m.fingerprint()
	if err != nil {
		return Status{Enabled: true, State: "missing", Error: err.Error(), Warnings: []string{}, CachePath: m.cachePath()}
	}
	cached, err := m.loadCache()
	if err != nil {
		status := Status{Enabled: true, State: "not_indexed", SourcePath: worldDir, Warnings: []string{}, CachePath: m.cachePath()}
		if errors.Is(err, os.ErrNotExist) {
			return status
		}
		status.Error = err.Error()
		return status
	}
	status = normalizeCachedStatus(cached.Status)
	status.Enabled = true
	status.SourcePath = worldDir
	status.CachePath = m.cachePath()
	if cached.Fingerprint != fp {
		status.State = "stale"
		status.Stale = true
	}
	return status
}

func (m *Manager) Current(ctx context.Context) (Index, Status, error) {
	if !m.cfg.SaveIndexerEnabled {
		index := EmptyIndex()
		return index, Status{Enabled: false, State: "disabled", Warnings: []string{}}, ErrDisabled
	}
	worldDir, fp, err := m.fingerprint()
	if err != nil {
		index := EmptyIndex()
		return index, Status{Enabled: true, State: "missing", Error: err.Error(), Warnings: []string{}, CachePath: m.cachePath()}, err
	}
	cached, err := m.loadCache()
	if err != nil {
		index := EmptyIndex()
		status := Status{Enabled: true, State: "not_indexed", SourcePath: worldDir, Warnings: []string{}, CachePath: m.cachePath()}
		if errors.Is(err, os.ErrNotExist) {
			return index, status, nil
		}
		status.Error = err.Error()
		return index, status, err
	}
	status := normalizeCachedStatus(cached.Status)
	status.Enabled = true
	status.SourcePath = worldDir
	status.CachePath = m.cachePath()
	if status.Counts == (Counts{}) {
		status.Counts = cached.Index.Counts
	}
	if cached.Fingerprint != fp {
		status.State = "stale"
		status.Stale = true
		cached.Index.Warnings = appendUnique(cached.Index.Warnings, "save files changed after the last successful index")
	}
	return cached.Index, status, nil
}

func (m *Manager) Rebuild(ctx context.Context) (Index, Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.cfg.SaveIndexerEnabled {
		index := EmptyIndex()
		return index, Status{Enabled: false, State: "disabled", Warnings: []string{}}, ErrDisabled
	}
	worldDir, fp, err := m.fingerprint()
	if err != nil {
		index := EmptyIndex()
		return index, Status{Enabled: true, State: "missing", Error: err.Error(), Warnings: []string{}, CachePath: m.cachePath()}, err
	}
	index, err := m.callIndexer(ctx, worldDir)
	if err != nil {
		status := Status{Enabled: true, State: "error", Stale: false, SourcePath: worldDir, Error: err.Error(), Warnings: []string{}, CachePath: m.cachePath()}
		var indexerErr *IndexerError
		if errors.As(err, &indexerErr) {
			status.ErrorCode = indexerErr.Code
			status.ErrorDetail = indexerErr.Detail
			for _, warning := range indexerErr.Warnings {
				status.Warnings = appendUnique(status.Warnings, warning)
			}
		}
		if status.ErrorCode == "parser_incompatible" {
			status = m.withOodleStatus(ctx, status)
			if status.OodleAvailable != nil && !*status.OodleAvailable {
				status.Warnings = appendUnique(status.Warnings, oodleUnavailableWarning)
			}
		}
		if cached, cacheErr := m.loadCache(); cacheErr == nil {
			cached.Status = normalizeCachedStatus(cached.Status)
			cached.Status.State = "stale"
			cached.Status.Stale = true
			cached.Status.Error = status.Error
			cached.Status.ErrorCode = status.ErrorCode
			cached.Status.ErrorDetail = status.ErrorDetail
			cached.Status.OodleAvailable = status.OodleAvailable
			cached.Status.CachePath = m.cachePath()
			for _, warning := range status.Warnings {
				cached.Status.Warnings = appendUnique(cached.Status.Warnings, warning)
			}
			cached.Status.Warnings = appendUnique(cached.Status.Warnings, staleAfterRebuildFailureWarning)
			cached.Index.Warnings = appendUnique(cached.Index.Warnings, staleAfterRebuildFailureWarning)
			_ = m.saveCache(cached.Fingerprint, cached.Index, cached.Status)
			return cached.Index, cached.Status, err
		}
		return EmptyIndex(), status, err
	}
	unsettled := false
	if latestFingerprint, fingerprintErr := fingerprintWorld(worldDir); fingerprintErr == nil && latestFingerprint != fp {
		// An autosave can land while the sidecar is parsing its snapshot. Retry
		// once against the newer files so a manual rebuild does not immediately
		// return as stale.
		retryStart := latestFingerprint
		if retryIndex, retryErr := m.callIndexer(ctx, worldDir); retryErr == nil {
			index = retryIndex
			if retryEnd, retryFingerprintErr := fingerprintWorld(worldDir); retryFingerprintErr == nil && retryEnd == retryStart {
				fp = retryEnd
			} else {
				fp = retryStart
				unsettled = true
			}
		} else {
			unsettled = true
			index.Warnings = appendUnique(index.Warnings, "save files changed during indexing and the automatic retry failed")
		}
	}
	index.SourcePath = worldDir
	index.Counts = countsFor(index)
	if unsettled {
		index.Warnings = appendUnique(index.Warnings, "save files are still changing; pause writes briefly and rebuild again for a current snapshot")
	}
	status := Status{
		Enabled:    true,
		State:      "ready",
		SourcePath: worldDir,
		UpdatedAt:  index.GeneratedAt,
		DurationMS: index.DurationMS,
		Warnings:   index.Warnings,
		Counts:     index.Counts,
		Parser:     index.Parser,
		CachePath:  m.cachePath(),
	}
	if unsettled {
		status.State = "stale"
		status.Stale = true
	}
	if historyErr := m.captureHistorySnapshotIfDue(worldDir, fp, index); historyErr != nil {
		index.Warnings = appendUnique(index.Warnings, "save index history snapshot could not be stored")
		status.Warnings = appendUnique(status.Warnings, "save index history snapshot could not be stored")
	}
	if err := m.saveCache(fp, index, status); err != nil {
		status.State = "error"
		status.Error = err.Error()
		return index, status, err
	}
	return index, status, nil
}

// EnsureFresh triggers a single background rebuild when the cached index is
// stale relative to the current save files. It never blocks the caller: the
// current (possibly stale) cache is served immediately and the next request
// picks up the refreshed index. Concurrent calls coalesce, and a debounce
// window prevents a rapidly autosaving server from causing a parse storm.
func (m *Manager) EnsureFresh(ctx context.Context) {
	if !m.cfg.SaveIndexerEnabled {
		return
	}
	_, fp, err := m.fingerprint()
	if err != nil {
		return
	}
	cached, err := m.loadCache()
	if err != nil || cached.Fingerprint == fp {
		// No cache yet (leave the first build to an explicit action) or already
		// current — nothing to do.
		return
	}

	m.autoMu.Lock()
	if m.rebuildInFlight || time.Since(m.lastAutoRebuild) < autoRebuildDebounce {
		m.autoMu.Unlock()
		return
	}
	m.rebuildInFlight = true
	m.lastAutoRebuild = time.Now()
	m.autoMu.Unlock()

	go func() {
		defer func() {
			m.autoMu.Lock()
			m.rebuildInFlight = false
			m.autoMu.Unlock()
		}()
		// Detach from the request context so a returning client does not cancel
		// the rebuild mid-parse.
		rebuildCtx, cancel := context.WithTimeout(context.Background(), time.Duration(m.cfg.SaveIndexTimeoutSeconds+30)*time.Second)
		defer cancel()
		_, _, _ = m.Rebuild(rebuildCtx)
	}()
}

func (m *Manager) FindWorldDir() (string, error) {
	m.sourceMu.RLock()
	override := m.sourcePath
	m.sourceMu.RUnlock()
	if strings.TrimSpace(override) != "" {
		return findWorldDir(override)
	}
	worldDir, err := findWorldDir(filepath.Join(m.cfg.ServerDirectory(), "Pal", "Saved", "SaveGames"))
	if err != nil {
		return "", err
	}
	return worldDir, nil
}

// SetSourcePath changes the active save source. An empty path restores the
// managed server save. Switching sources invalidates the single active cache.
func (m *Manager) SetSourcePath(path string) {
	m.sourceMu.Lock()
	changed := filepath.Clean(m.sourcePath) != filepath.Clean(path)
	m.sourcePath = strings.TrimSpace(path)
	m.sourceMu.Unlock()
	if changed {
		m.Invalidate()
	}
}

func (m *Manager) Invalidate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(m.cfg.SaveIndexCacheDir) != "" {
		_ = os.Remove(m.cachePath())
	}
	m.cacheMu.Lock()
	m.cache = nil
	m.cacheLoaded = false
	m.cacheMTime = time.Time{}
	m.cacheMu.Unlock()
}

func (m *Manager) fingerprint() (string, string, error) {
	worldDir, err := m.FindWorldDir()
	if err != nil {
		return "", "", err
	}
	fp, err := fingerprintWorld(worldDir)
	return worldDir, fp, err
}

func (m *Manager) callIndexer(ctx context.Context, worldDir string) (Index, error) {
	url := strings.TrimRight(m.cfg.SaveIndexerURL, "/") + "/index"
	payload := map[string]any{
		"save_dir":         worldDir,
		"timeout_seconds":  m.cfg.SaveIndexTimeoutSeconds,
		"cache_dir":        m.cfg.SaveIndexCacheDir,
		"requested_by":     "palpanel",
		"requested_at_utc": time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Index{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Index{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return Index{}, classifyIndexerRequestError(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Index{}, err
	}
	var envelope sidecarEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Index{}, &IndexerError{
			Code:    "parser_incompatible",
			Summary: "save indexer returned a non-JSON response; verify the sav-cli version and inspect its text logs",
		}
	}
	if resp.StatusCode >= 400 || !envelope.OK {
		if envelope.Error != nil {
			code := safeIndexerCode(envelope.Error.Code)
			privatePaths := []string{worldDir, m.cfg.SaveIndexCacheDir, os.TempDir()}
			if homeDir, homeErr := os.UserHomeDir(); homeErr == nil {
				privatePaths = append(privatePaths, homeDir)
			}
			detail := safeIndexerDetail(envelope.Error.Message, privatePaths...)
			if detail != "" {
				log.Printf("save indexer sidecar error code=%s detail=%q", code, detail)
			}
			exposedDetail := ""
			if code != "" {
				exposedDetail = detail
			}
			return Index{}, &IndexerError{Code: code, Detail: exposedDetail, Warnings: append([]string(nil), envelope.Warnings...)}
		}
		return Index{}, &IndexerError{Code: "save_indexer_unavailable"}
	}
	var index Index
	if err := json.Unmarshal(envelope.Data, &index); err != nil {
		return Index{}, &IndexerError{
			Code:    "parser_incompatible",
			Summary: "save indexer returned incompatible index data; verify the sav-cli version and inspect its text logs",
		}
	}
	ensureSlices(&index)
	return index, nil
}

func (m *Manager) withOodleStatus(ctx context.Context, status Status) Status {
	if status.Enabled {
		status.OodleAvailable = m.probeOodleAvailability(ctx)
	}
	return status
}

// probeOodleAvailability reads the sav-cli capability contract with a short,
// independent timeout. Results, including an unknown result, are cached briefly
// so frequently-polled status endpoints do not create a health-check storm.
func (m *Manager) probeOodleAvailability(ctx context.Context) *bool {
	m.oodleMu.Lock()
	defer m.oodleMu.Unlock()

	if !m.oodleAt.IsZero() && time.Since(m.oodleAt) < oodleHealthCacheTTL {
		return copyBool(m.oodle)
	}

	probeCtx, cancel := context.WithTimeout(ctx, oodleHealthTimeout)
	defer cancel()
	url := strings.TrimRight(m.cfg.SaveIndexerURL, "/") + "/health"
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		m.oodleAt = time.Now()
		m.oodle = nil
		return nil
	}
	resp, err := m.client.Do(req)
	if err != nil {
		m.oodleAt = time.Now()
		m.oodle = nil
		return nil
	}
	defer resp.Body.Close()
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Oodle *bool `json:"oodle"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		m.oodleAt = time.Now()
		m.oodle = nil
		return nil
	}
	m.oodleAt = time.Now()
	if resp.StatusCode >= http.StatusBadRequest || !envelope.OK || envelope.Data.Oodle == nil {
		m.oodle = nil
		return nil
	}
	m.oodle = copyBool(envelope.Data.Oodle)
	return copyBool(m.oodle)
}

func copyBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

const oodleUnavailableWarning = "sav-cli was built without cgo/Oodle support; Oodle-compressed (PlM) saves cannot be parsed. Use a release build with cgo enabled."

func (m *Manager) cachePath() string {
	return filepath.Join(m.cfg.SaveIndexCacheDir, "index-cache.json")
}

func (m *Manager) loadCache() (cacheFile, error) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()

	path := m.cachePath()
	st, err := os.Stat(path)
	if err != nil {
		m.cache = nil
		m.cacheLoaded = false
		m.cacheMTime = time.Time{}
		return cacheFile{}, err
	}

	if m.cacheLoaded && m.cache != nil && m.cacheMTime.Equal(st.ModTime()) {
		return *m.cache, nil
	}

	var cached cacheFile
	b, err := os.ReadFile(path)
	if err != nil {
		return cached, err
	}
	if err := json.Unmarshal(b, &cached); err != nil {
		return cached, err
	}
	ensureSlices(&cached.Index)
	m.cache = &cached
	m.cacheMTime = st.ModTime()
	m.cacheLoaded = true
	return cached, nil
}

func (m *Manager) saveCache(fingerprint string, index Index, status Status) error {
	if err := os.MkdirAll(m.cfg.SaveIndexCacheDir, 0o755); err != nil {
		return err
	}
	cached := cacheFile{
		Fingerprint: fingerprint,
		SavedAt:     time.Now().UTC().Format(time.RFC3339),
		Index:       index,
		Status:      status,
	}
	b, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()

	path := m.cachePath()
	temporary, err := os.CreateTemp(m.cfg.SaveIndexCacheDir, ".index-cache-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(b); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	st, err := os.Stat(path)
	if err == nil {
		copy := cached
		m.cache = &copy
		m.cacheMTime = st.ModTime()
		m.cacheLoaded = true
	}
	return nil
}

func findWorldDir(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("save root is empty")
	}
	if st, err := os.Stat(root); err == nil && !st.IsDir() && filepath.Base(root) == "Level.sav" {
		return filepath.Dir(root), nil
	}
	if _, err := os.Stat(filepath.Join(root, "Level.sav")); err == nil {
		return filepath.Abs(root)
	}
	var candidates []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := strings.ToLower(d.Name())
			if name == "backup" || name == "backups" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "Level.sav" {
			candidates = append(candidates, filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("Level.sav not found under %s", root)
	}
	sort.Slice(candidates, func(i, j int) bool {
		ii, _ := os.Stat(filepath.Join(candidates[i], "Level.sav"))
		jj, _ := os.Stat(filepath.Join(candidates[j], "Level.sav"))
		if ii == nil || jj == nil {
			return candidates[i] < candidates[j]
		}
		return ii.ModTime().After(jj.ModTime())
	})
	return filepath.Abs(candidates[0])
}

func fingerprintWorld(worldDir string) (string, error) {
	var files []string
	// LevelMeta.sav is not consumed by the indexer and is rewritten by the
	// server independently, so including it makes an otherwise current cache
	// appear stale. Only fingerprint files that affect indexed entities.
	for _, name := range []string{"Level.sav"} {
		path := filepath.Join(worldDir, name)
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
		}
	}
	playerFiles, _ := filepath.Glob(filepath.Join(worldDir, "Players", "*.sav"))
	files = append(files, playerFiles...)
	if len(files) == 0 {
		return "", fmt.Errorf("no .sav files found in %s", worldDir)
	}
	sort.Strings(files)
	h := fnv.New128a()
	// Salt the fingerprint with the index schema contract. Parser-only changes
	// may not modify any save file, so without this salt an older on-disk cache
	// would remain "fresh" and continue serving incomplete entities after an
	// upgrade. Bump this value whenever the sidecar index schema changes.
	_, _ = h.Write([]byte("palpanel-save-index-schema:v2\n"))
	for _, path := range files {
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(worldDir, path)
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte(fmt.Sprintf("%d:%d:", st.Size(), st.ModTime().UnixNano())))
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func ensureSlices(index *Index) {
	if index.Warnings == nil {
		index.Warnings = []string{}
	}
	if index.Players == nil {
		index.Players = []Player{}
	}
	if index.Guilds == nil {
		index.Guilds = []Guild{}
	}
	if index.Bases == nil {
		index.Bases = []Base{}
	}
	if index.Pals == nil {
		index.Pals = []Pal{}
	}
	if index.Containers == nil {
		index.Containers = []Container{}
	}
	if index.MapEntities == nil {
		index.MapEntities = []MapEntity{}
	}
	if index.Counts == (Counts{}) {
		index.Counts = countsFor(*index)
	}
}

func countsFor(index Index) Counts {
	return Counts{
		Players:     len(index.Players),
		Guilds:      len(index.Guilds),
		Bases:       len(index.Bases),
		Pals:        len(index.Pals),
		Containers:  len(index.Containers),
		MapEntities: len(index.MapEntities),
	}
}

func normalizeCachedStatus(status Status) Status {
	if status.State == "error" && status.Stale {
		status.State = "stale"
		status.Error = ""
		status.Warnings = appendUnique(status.Warnings, staleAfterRebuildFailureWarning)
	}
	return status
}

func appendUnique(items []string, values ...string) []string {
	for _, value := range values {
		found := false
		for _, item := range items {
			if item == value {
				found = true
				break
			}
		}
		if !found {
			items = append(items, value)
		}
	}
	return items
}
