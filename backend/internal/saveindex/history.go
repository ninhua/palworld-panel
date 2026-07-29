package saveindex

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	historySchemaVersion    = 1
	historyRetention        = 24
	historyMaxSnapshotBytes = 128 << 20
	historyMaxDecodedBytes  = 512 << 20
	historyMaxTotalBytes    = 512 << 20
)

var ErrHistoryCorrupt = errors.New("save history snapshot integrity check failed")

var (
	historySnapshotIDPattern  = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{32}$`)
	historyFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	historySHA256Pattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type HistorySnapshot struct {
	ID            string `json:"id"`
	Fingerprint   string `json:"fingerprint"`
	GeneratedAt   string `json:"generated_at"`
	CapturedAt    string `json:"captured_at"`
	Parser        string `json:"parser"`
	Counts        Counts `json:"counts"`
	SizeBytes     int64  `json:"size_bytes"`
	ArchiveSHA256 string `json:"archive_sha256"`
}

type HistoryState struct {
	Retention     int               `json:"retention"`
	MaxTotalBytes int64             `json:"max_total_bytes"`
	TotalBytes    int64             `json:"total_bytes"`
	Items         []HistorySnapshot `json:"items"`
}

type HistoryDiffOptions struct {
	Category string
	Query    string
	Limit    int
	Offset   int
}

type HistoryDiffSummary struct {
	PlayersAdded      int `json:"players_added"`
	PlayersRemoved    int `json:"players_removed"`
	PlayersChanged    int `json:"players_changed"`
	GuildsAdded       int `json:"guilds_added"`
	GuildsRemoved     int `json:"guilds_removed"`
	GuildsChanged     int `json:"guilds_changed"`
	BasesAdded        int `json:"bases_added"`
	BasesRemoved      int `json:"bases_removed"`
	BasesChanged      int `json:"bases_changed"`
	PalsAdded         int `json:"pals_added"`
	PalsRemoved       int `json:"pals_removed"`
	PalsChanged       int `json:"pals_changed"`
	ContainersAdded   int `json:"containers_added"`
	ContainersRemoved int `json:"containers_removed"`
	ContainersChanged int `json:"containers_changed"`
	ItemsIncreased    int `json:"items_increased"`
	ItemsDecreased    int `json:"items_decreased"`
}

type HistoryFieldChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type HistoryChange struct {
	Category string               `json:"category"`
	Kind     string               `json:"kind"`
	ID       string               `json:"id"`
	Label    string               `json:"label"`
	Delta    int                  `json:"delta,omitempty"`
	Fields   []HistoryFieldChange `json:"fields"`
}

type HistoryDiff struct {
	From    HistorySnapshot    `json:"from"`
	To      HistorySnapshot    `json:"to"`
	Summary HistoryDiffSummary `json:"summary"`
	Total   int                `json:"total"`
	Limit   int                `json:"limit"`
	Offset  int                `json:"offset"`
	Items   []HistoryChange    `json:"items"`
}

type historyManifest struct {
	SchemaVersion int               `json:"schema_version"`
	WorldKey      string            `json:"world_key"`
	Items         []HistorySnapshot `json:"items"`
}

type historyDiskSnapshot struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	WorldKey      string `json:"world_key"`
	Fingerprint   string `json:"fingerprint"`
	GeneratedAt   string `json:"generated_at"`
	CapturedAt    string `json:"captured_at"`
	Parser        string `json:"parser"`
	Counts        Counts `json:"counts"`
	Index         Index  `json:"index"`
}

func (m *Manager) EnsureHistorySnapshot() error {
	if !m.cfg.SaveIndexerEnabled {
		return ErrDisabled
	}
	worldDir, err := m.FindWorldDir()
	if err != nil {
		return err
	}
	cached, err := m.loadCache()
	if err != nil {
		return err
	}
	return m.captureHistorySnapshot(worldDir, cached.Fingerprint, cached.Index)
}

func (m *Manager) RemoveHistoryForWorld(worldDir string) error {
	worldDir = strings.TrimSpace(worldDir)
	if worldDir == "" {
		return errors.New("save history world directory is required")
	}
	m.historyMu.Lock()
	defer m.historyMu.Unlock()
	return os.RemoveAll(m.historyWorldDirectory(historyWorldKey(worldDir)))
}

func (m *Manager) History() (HistoryState, error) {
	worldDir, err := m.FindWorldDir()
	if err != nil {
		return HistoryState{}, err
	}
	m.historyMu.Lock()
	defer m.historyMu.Unlock()
	manifest, err := m.loadHistoryManifest(historyWorldKey(worldDir))
	if err != nil {
		return HistoryState{}, err
	}
	state := HistoryState{
		Retention: historyRetention, MaxTotalBytes: historyMaxTotalBytes,
		Items: append([]HistorySnapshot(nil), manifest.Items...),
	}
	for _, item := range state.Items {
		state.TotalBytes += item.SizeBytes
	}
	return state, nil
}

func (m *Manager) HistoryDiff(fromID, toID string, options HistoryDiffOptions) (HistoryDiff, error) {
	worldDir, err := m.FindWorldDir()
	if err != nil {
		return HistoryDiff{}, err
	}
	var fromMeta, toMeta HistorySnapshot
	var fromIndex, toIndex Index
	err = func() error {
		m.historyMu.Lock()
		defer m.historyMu.Unlock()
		worldKey := historyWorldKey(worldDir)
		manifest, err := m.loadHistoryManifest(worldKey)
		if err != nil {
			return err
		}
		var found bool
		fromMeta, found = historyMetadata(manifest.Items, fromID)
		if !found {
			return os.ErrNotExist
		}
		toMeta, found = historyMetadata(manifest.Items, toID)
		if !found {
			return os.ErrNotExist
		}
		from, err := m.readHistorySnapshot(worldKey, fromMeta)
		if err != nil {
			return err
		}
		to, err := m.readHistorySnapshot(worldKey, toMeta)
		if err != nil {
			return err
		}
		fromIndex, toIndex = from.Index, to.Index
		return nil
	}()
	if err != nil {
		return HistoryDiff{}, err
	}
	return buildHistoryDiff(fromMeta, fromIndex, toMeta, toIndex, options), nil
}

func (m *Manager) captureHistorySnapshot(worldDir, fingerprint string, index Index) error {
	fingerprint = strings.ToLower(strings.TrimSpace(fingerprint))
	if !historyFingerprintPattern.MatchString(fingerprint) {
		return errors.New("invalid save history fingerprint")
	}
	m.historyMu.Lock()
	defer m.historyMu.Unlock()
	worldKey := historyWorldKey(worldDir)
	manifest, err := m.loadHistoryManifest(worldKey)
	if err != nil {
		return err
	}
	if len(manifest.Items) > 0 && manifest.Items[0].Fingerprint == fingerprint {
		return nil
	}
	capturedAt := time.Now().UTC()
	generatedAt := strings.TrimSpace(index.GeneratedAt)
	if _, err := time.Parse(time.RFC3339, generatedAt); err != nil {
		generatedAt = capturedAt.Format(time.RFC3339)
	}
	id := capturedAt.Format("20060102T150405Z") + "-" + fingerprint
	for historyMetadataExists(manifest.Items, id) {
		capturedAt = capturedAt.Add(time.Second)
		id = capturedAt.Format("20060102T150405Z") + "-" + fingerprint
	}

	index, err = sanitizeHistoryIndex(index)
	if err != nil {
		return err
	}
	index.Snapshot.Fingerprint = fingerprint
	index.Counts = countsFor(index)

	disk := historyDiskSnapshot{
		SchemaVersion: historySchemaVersion,
		ID:            id, WorldKey: worldKey, Fingerprint: fingerprint,
		GeneratedAt: generatedAt, CapturedAt: capturedAt.Format(time.RFC3339),
		Parser: index.Parser, Counts: countsFor(index), Index: index,
	}
	archive, err := encodeHistorySnapshot(disk)
	if err != nil {
		return err
	}
	if len(archive) > historyMaxSnapshotBytes {
		return fmt.Errorf("save history snapshot exceeds %d bytes", historyMaxSnapshotBytes)
	}
	directory := m.historyWorldDirectory(worldKey)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	archiveHash := sha256.Sum256(archive)
	metadata := HistorySnapshot{
		ID: id, Fingerprint: fingerprint, GeneratedAt: generatedAt,
		CapturedAt: disk.CapturedAt, Parser: disk.Parser, Counts: disk.Counts,
		SizeBytes: int64(len(archive)), ArchiveSHA256: hex.EncodeToString(archiveHash[:]),
	}
	path, err := m.historySnapshotPath(worldKey, id)
	if err != nil {
		return err
	}
	if err := writeHistoryFile(path, archive); err != nil {
		return err
	}
	manifest.Items = append([]HistorySnapshot{metadata}, manifest.Items...)
	removed := pruneHistoryItems(&manifest.Items)
	if err := m.writeHistoryManifest(manifest); err != nil {
		_ = os.Remove(path)
		return err
	}
	for _, item := range removed {
		if oldPath, err := m.historySnapshotPath(worldKey, item.ID); err == nil {
			_ = os.Remove(oldPath)
		}
	}
	return nil
}

func (m *Manager) historyRoot() string {
	return filepath.Join(m.cfg.SaveIndexCacheDir, "history")
}

func (m *Manager) historyWorldDirectory(worldKey string) string {
	return filepath.Join(m.historyRoot(), worldKey)
}

func (m *Manager) historyManifestPath(worldKey string) string {
	return filepath.Join(m.historyWorldDirectory(worldKey), "manifest.json")
}

func (m *Manager) historySnapshotPath(worldKey, id string) (string, error) {
	if !historySnapshotIDPattern.MatchString(id) {
		return "", errors.New("invalid save history snapshot id")
	}
	return filepath.Join(m.historyWorldDirectory(worldKey), id+".json.gz"), nil
}

func (m *Manager) loadHistoryManifest(worldKey string) (historyManifest, error) {
	manifest := historyManifest{
		SchemaVersion: historySchemaVersion,
		WorldKey:      worldKey,
		Items:         []HistorySnapshot{},
	}
	body, err := os.ReadFile(m.historyManifestPath(worldKey))
	if errors.Is(err, os.ErrNotExist) {
		return manifest, nil
	}
	if err != nil {
		return historyManifest{}, err
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return historyManifest{}, fmt.Errorf("%w: invalid manifest JSON", ErrHistoryCorrupt)
	}
	if manifest.SchemaVersion != historySchemaVersion || manifest.WorldKey != worldKey {
		return historyManifest{}, fmt.Errorf("%w: manifest world mismatch", ErrHistoryCorrupt)
	}
	if manifest.Items == nil {
		manifest.Items = []HistorySnapshot{}
	}
	seen := make(map[string]struct{}, len(manifest.Items))
	for _, item := range manifest.Items {
		if !historySnapshotIDPattern.MatchString(item.ID) ||
			!historyFingerprintPattern.MatchString(item.Fingerprint) ||
			!historySHA256Pattern.MatchString(item.ArchiveSHA256) ||
			item.SizeBytes < 1 || item.SizeBytes > historyMaxSnapshotBytes {
			return historyManifest{}, fmt.Errorf("%w: invalid manifest entry", ErrHistoryCorrupt)
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return historyManifest{}, fmt.Errorf("%w: duplicate manifest entry", ErrHistoryCorrupt)
		}
		seen[item.ID] = struct{}{}
	}
	return manifest, nil
}

func (m *Manager) writeHistoryManifest(manifest historyManifest) error {
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.historyWorldDirectory(manifest.WorldKey), 0o700); err != nil {
		return err
	}
	return writeHistoryFile(m.historyManifestPath(manifest.WorldKey), body)
}

func (m *Manager) readHistorySnapshot(worldKey string, metadata HistorySnapshot) (historyDiskSnapshot, error) {
	path, err := m.historySnapshotPath(worldKey, metadata.ID)
	if err != nil {
		return historyDiskSnapshot{}, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return historyDiskSnapshot{}, fmt.Errorf("%w: archive is missing", ErrHistoryCorrupt)
	}
	if err != nil {
		return historyDiskSnapshot{}, err
	}
	defer file.Close()

	hasher := sha256.New()
	archive, err := io.ReadAll(io.LimitReader(io.TeeReader(file, hasher), historyMaxSnapshotBytes+1))
	if err != nil {
		return historyDiskSnapshot{}, err
	}
	if len(archive) > historyMaxSnapshotBytes {
		return historyDiskSnapshot{}, fmt.Errorf("%w: compressed size limit exceeded", ErrHistoryCorrupt)
	}
	if int64(len(archive)) != metadata.SizeBytes {
		return historyDiskSnapshot{}, fmt.Errorf("%w: compressed size mismatch", ErrHistoryCorrupt)
	}
	if hex.EncodeToString(hasher.Sum(nil)) != metadata.ArchiveSHA256 {
		return historyDiskSnapshot{}, fmt.Errorf("%w: checksum mismatch", ErrHistoryCorrupt)
	}
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return historyDiskSnapshot{}, fmt.Errorf("%w: invalid gzip archive", ErrHistoryCorrupt)
	}
	defer reader.Close()
	decoded, err := io.ReadAll(io.LimitReader(reader, historyMaxDecodedBytes+1))
	if err != nil {
		return historyDiskSnapshot{}, err
	}
	if len(decoded) > historyMaxDecodedBytes {
		return historyDiskSnapshot{}, fmt.Errorf("%w: decoded size limit exceeded", ErrHistoryCorrupt)
	}
	var snapshot historyDiskSnapshot
	if err := json.Unmarshal(decoded, &snapshot); err != nil {
		return historyDiskSnapshot{}, fmt.Errorf("%w: invalid snapshot JSON", ErrHistoryCorrupt)
	}
	if snapshot.SchemaVersion != historySchemaVersion ||
		snapshot.WorldKey != worldKey ||
		snapshot.ID != metadata.ID ||
		snapshot.Fingerprint != metadata.Fingerprint ||
		snapshot.GeneratedAt != metadata.GeneratedAt ||
		snapshot.CapturedAt != metadata.CapturedAt ||
		snapshot.Parser != metadata.Parser ||
		snapshot.Counts != metadata.Counts {
		return historyDiskSnapshot{}, fmt.Errorf("%w: metadata mismatch", ErrHistoryCorrupt)
	}
	ensureSlices(&snapshot.Index)
	if snapshot.Counts != countsFor(snapshot.Index) ||
		snapshot.Index.Counts != snapshot.Counts ||
		snapshot.Index.Snapshot.Fingerprint != snapshot.Fingerprint ||
		snapshot.Parser != snapshot.Index.Parser ||
		!historyIndexIsSanitized(snapshot.Index) {
		return historyDiskSnapshot{}, fmt.Errorf("%w: snapshot index validation failed", ErrHistoryCorrupt)
	}
	return snapshot, nil
}

func historyIndexIsSanitized(index Index) bool {
	if index.SourcePath != "" || len(index.Snapshot.Files) != 0 || len(index.Warnings) != 0 {
		return false
	}
	for _, player := range index.Players {
		if player.IP != "" || player.Ping != nil || len(player.InventorySummary) != 0 {
			return false
		}
	}
	return true
}

func encodeHistorySnapshot(snapshot historyDiskSnapshot) ([]byte, error) {
	var output bytes.Buffer
	writer, err := gzip.NewWriterLevel(&output, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(snapshot); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeHistoryFile(path string, body []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".save-history-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
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
	return os.Chmod(path, 0o600)
}

func historyWorldKey(worldDir string) string {
	cleaned, err := filepath.Abs(filepath.Clean(worldDir))
	if err != nil {
		cleaned = filepath.Clean(worldDir)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(cleaned); resolveErr == nil {
		cleaned = resolved
	}
	if runtime.GOOS == "windows" {
		cleaned = strings.ToLower(cleaned)
	}
	sum := sha256.Sum256([]byte(cleaned))
	return hex.EncodeToString(sum[:16])
}

func sanitizeHistoryIndex(index Index) (Index, error) {
	body, err := json.Marshal(index)
	if err != nil {
		return Index{}, err
	}
	var sanitized Index
	if err := json.Unmarshal(body, &sanitized); err != nil {
		return Index{}, err
	}
	ensureSlices(&sanitized)
	sanitized.SourcePath = ""
	sanitized.Warnings = nil
	sanitized.Snapshot.Files = nil
	for i := range sanitized.Players {
		sanitized.Players[i].IP = ""
		sanitized.Players[i].Ping = nil
		sanitized.Players[i].InventorySummary = nil
	}
	return sanitized, nil
}

func historyMetadata(items []HistorySnapshot, id string) (HistorySnapshot, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return HistorySnapshot{}, false
}

func historyMetadataExists(items []HistorySnapshot, id string) bool {
	_, ok := historyMetadata(items, id)
	return ok
}

func pruneHistoryItems(items *[]HistorySnapshot) []HistorySnapshot {
	kept := *items
	var total int64
	cut := len(kept)
	for index, item := range kept {
		if index >= historyRetention || total+item.SizeBytes > historyMaxTotalBytes {
			cut = index
			break
		}
		total += item.SizeBytes
	}
	removed := append([]HistorySnapshot(nil), kept[cut:]...)
	*items = append([]HistorySnapshot(nil), kept[:cut]...)
	return removed
}

func buildHistoryDiff(fromMeta HistorySnapshot, from Index, toMeta HistorySnapshot, to Index, options HistoryDiffOptions) HistoryDiff {
	changes := make([]HistoryChange, 0)
	summary := HistoryDiffSummary{}
	changes = append(changes, diffPlayers(from.Players, to.Players, &summary)...)
	changes = append(changes, diffGuilds(from.Guilds, to.Guilds, &summary)...)
	changes = append(changes, diffBases(from.Bases, to.Bases, &summary)...)
	changes = append(changes, diffPals(from.Pals, to.Pals, &summary)...)
	changes = append(changes, diffContainers(from.Containers, to.Containers, &summary)...)
	changes = append(changes, diffItems(from.Containers, to.Containers, &summary)...)

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Category != changes[j].Category {
			return changes[i].Category < changes[j].Category
		}
		if changes[i].Kind != changes[j].Kind {
			return changes[i].Kind < changes[j].Kind
		}
		return changes[i].ID < changes[j].ID
	})
	category := strings.ToLower(strings.TrimSpace(options.Category))
	query := strings.ToLower(strings.TrimSpace(options.Query))
	filtered := make([]HistoryChange, 0, len(changes))
	for _, change := range changes {
		if category != "" && category != "all" && change.Category != category {
			continue
		}
		if query != "" && !historyChangeMatches(change, query) {
			continue
		}
		filtered = append(filtered, change)
	}
	limit := options.Limit
	if limit < 1 || limit > 500 {
		limit = 100
	}
	offset := options.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(filtered) {
		offset = len(filtered)
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return HistoryDiff{
		From: fromMeta, To: toMeta, Summary: summary,
		Total: len(filtered), Limit: limit, Offset: offset,
		Items: append([]HistoryChange(nil), filtered[offset:end]...),
	}
}

func historyChangeMatches(change HistoryChange, query string) bool {
	if strings.Contains(strings.ToLower(change.ID+" "+change.Label), query) {
		return true
	}
	for _, field := range change.Fields {
		if strings.Contains(strings.ToLower(field.Field+" "+field.Before+" "+field.After), query) {
			return true
		}
	}
	return false
}

func diffPlayers(before, after []Player, summary *HistoryDiffSummary) []HistoryChange {
	left := make(map[string]Player, len(before))
	right := make(map[string]Player, len(after))
	for _, item := range before {
		if key := historyPlayerKey(item); key != "" {
			left[key] = item
		}
	}
	for _, item := range after {
		if key := historyPlayerKey(item); key != "" {
			right[key] = item
		}
	}
	return diffEntityMaps("players", left, right,
		func(item Player) string { return firstHistoryValue(item.Nickname, item.SteamID, item.PlayerUID) },
		func(a, b Player) []HistoryFieldChange {
			return compactHistoryFields(
				historyField("nickname", a.Nickname, b.Nickname),
				historyField("level", strconv.Itoa(a.Level), strconv.Itoa(b.Level)),
				historyField("guild", firstHistoryValue(a.GuildName, a.GuildID), firstHistoryValue(b.GuildName, b.GuildID)),
				historyField("last_online", a.LastOnlineTime, b.LastOnlineTime),
			)
		},
		func(kind string) {
			switch kind {
			case "added":
				summary.PlayersAdded++
			case "removed":
				summary.PlayersRemoved++
			case "changed":
				summary.PlayersChanged++
			}
		},
	)
}

func diffGuilds(before, after []Guild, summary *HistoryDiffSummary) []HistoryChange {
	left := mapHistoryBy(before, func(item Guild) string { return item.ID })
	right := mapHistoryBy(after, func(item Guild) string { return item.ID })
	return diffEntityMaps("guilds", left, right,
		func(item Guild) string { return firstHistoryValue(item.Name, item.ID) },
		func(a, b Guild) []HistoryFieldChange {
			return compactHistoryFields(
				historyField("name", a.Name, b.Name),
				historyField("owner", a.OwnerPlayerUID, b.OwnerPlayerUID),
				historyField("members", historyGuildMembers(a.Members), historyGuildMembers(b.Members)),
				historyField("bases", historySortedStrings(a.BaseIDs), historySortedStrings(b.BaseIDs)),
			)
		},
		func(kind string) {
			switch kind {
			case "added":
				summary.GuildsAdded++
			case "removed":
				summary.GuildsRemoved++
			case "changed":
				summary.GuildsChanged++
			}
		},
	)
}

func diffBases(before, after []Base, summary *HistoryDiffSummary) []HistoryChange {
	left := mapHistoryBy(before, func(item Base) string { return item.ID })
	right := mapHistoryBy(after, func(item Base) string { return item.ID })
	return diffEntityMaps("bases", left, right,
		func(item Base) string { return firstHistoryValue(item.Name, item.ID) },
		func(a, b Base) []HistoryFieldChange {
			return compactHistoryFields(
				historyField("name", a.Name, b.Name),
				historyField("guild", firstHistoryValue(a.GuildName, a.GuildID), firstHistoryValue(b.GuildName, b.GuildID)),
				historyField("location", historyCoordinates(a.Location), historyCoordinates(b.Location)),
				historyField("structures", strconv.Itoa(a.StructuresCount), strconv.Itoa(b.StructuresCount)),
				historyField("workers", historyWorkers(a.Workers), historyWorkers(b.Workers)),
				historyField("containers", historySortedStrings(a.Containers), historySortedStrings(b.Containers)),
				historyField("status", a.Status, b.Status),
			)
		},
		func(kind string) {
			switch kind {
			case "added":
				summary.BasesAdded++
			case "removed":
				summary.BasesRemoved++
			case "changed":
				summary.BasesChanged++
			}
		},
	)
}

func diffPals(before, after []Pal, summary *HistoryDiffSummary) []HistoryChange {
	left := mapHistoryBy(before, func(item Pal) string { return item.InstanceID })
	right := mapHistoryBy(after, func(item Pal) string { return item.InstanceID })
	return diffEntityMaps("pals", left, right,
		func(item Pal) string { return firstHistoryValue(item.Nickname, item.CharacterID, item.InstanceID) },
		func(a, b Pal) []HistoryFieldChange {
			return compactHistoryFields(
				historyField("character", a.CharacterID, b.CharacterID),
				historyField("nickname", a.Nickname, b.Nickname),
				historyField("level", strconv.Itoa(a.Level), strconv.Itoa(b.Level)),
				historyField("owner", a.OwnerPlayerUID, b.OwnerPlayerUID),
				historyField("guild", a.GuildID, b.GuildID),
				historyField("container", a.ContainerID, b.ContainerID),
				historyField("slot", strconv.Itoa(a.SlotIndex), strconv.Itoa(b.SlotIndex)),
				historyField("location_type", a.LocationType, b.LocationType),
				historyField("rank", strconv.Itoa(a.Rank), strconv.Itoa(b.Rank)),
				historyField("ivs", fmt.Sprintf("%d/%d/%d", a.IVHP, a.IVAttack, a.IVDefense), fmt.Sprintf("%d/%d/%d", b.IVHP, b.IVAttack, b.IVDefense)),
				historyField("passives", historySortedStrings(a.Passives), historySortedStrings(b.Passives)),
				historyField("status", a.Status, b.Status),
			)
		},
		func(kind string) {
			switch kind {
			case "added":
				summary.PalsAdded++
			case "removed":
				summary.PalsRemoved++
			case "changed":
				summary.PalsChanged++
			}
		},
	)
}

func diffContainers(before, after []Container, summary *HistoryDiffSummary) []HistoryChange {
	left := mapHistoryBy(before, func(item Container) string { return item.ContainerID })
	right := mapHistoryBy(after, func(item Container) string { return item.ContainerID })
	return diffEntityMaps("containers", left, right,
		func(item Container) string { return firstHistoryValue(item.OwnerID, item.ContainerID) },
		func(a, b Container) []HistoryFieldChange {
			return compactHistoryFields(
				historyField("owner_type", a.OwnerType, b.OwnerType),
				historyField("owner", a.OwnerID, b.OwnerID),
				historyField("contents", historyContainerContents(a.Slots), historyContainerContents(b.Slots)),
			)
		},
		func(kind string) {
			switch kind {
			case "added":
				summary.ContainersAdded++
			case "removed":
				summary.ContainersRemoved++
			case "changed":
				summary.ContainersChanged++
			}
		},
	)
}

func diffItems(before, after []Container, summary *HistoryDiffSummary) []HistoryChange {
	left := historyItemCounts(before)
	right := historyItemCounts(after)
	keys := unionHistoryKeys(left, right)
	changes := make([]HistoryChange, 0)
	for _, key := range keys {
		delta := right[key] - left[key]
		if delta == 0 {
			continue
		}
		kind := "increased"
		if delta < 0 {
			kind = "decreased"
			summary.ItemsDecreased++
		} else {
			summary.ItemsIncreased++
		}
		changes = append(changes, HistoryChange{
			Category: "items", Kind: kind, ID: key, Label: key, Delta: delta,
			Fields: []HistoryFieldChange{{Field: "count", Before: strconv.Itoa(left[key]), After: strconv.Itoa(right[key])}},
		})
	}
	return changes
}

func diffEntityMaps[T any](category string, before, after map[string]T, label func(T) string, fields func(T, T) []HistoryFieldChange, count func(string)) []HistoryChange {
	keys := unionHistoryKeys(before, after)
	changes := make([]HistoryChange, 0)
	for _, key := range keys {
		left, leftFound := before[key]
		right, rightFound := after[key]
		switch {
		case !leftFound && rightFound:
			count("added")
			changes = append(changes, HistoryChange{Category: category, Kind: "added", ID: key, Label: label(right), Fields: []HistoryFieldChange{}})
		case leftFound && !rightFound:
			count("removed")
			changes = append(changes, HistoryChange{Category: category, Kind: "removed", ID: key, Label: label(left), Fields: []HistoryFieldChange{}})
		default:
			changed := fields(left, right)
			if len(changed) > 0 {
				count("changed")
				changes = append(changes, HistoryChange{Category: category, Kind: "changed", ID: key, Label: firstHistoryValue(label(right), label(left), key), Fields: changed})
			}
		}
	}
	return changes
}

func mapHistoryBy[T any](items []T, key func(T) string) map[string]T {
	output := make(map[string]T, len(items))
	for _, item := range items {
		identifier := strings.TrimSpace(key(item))
		if identifier != "" {
			output[identifier] = item
		}
	}
	return output
}

func unionHistoryKeys[T any](left, right map[string]T) []string {
	keys := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		keys[key] = struct{}{}
	}
	for key := range right {
		keys[key] = struct{}{}
	}
	output := make([]string, 0, len(keys))
	for key := range keys {
		output = append(output, key)
	}
	sort.Strings(output)
	return output
}

func historyPlayerKey(player Player) string {
	if value := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(player.PlayerUID), "-", "")); value != "" {
		return "uid:" + value
	}
	if value := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(player.SteamID)), "steam_"); value != "" {
		return "steam:" + value
	}
	if value := strings.ToLower(strings.TrimSpace(player.Nickname)); value != "" {
		return "name:" + value
	}
	return ""
}

func historyField(name, before, after string) HistoryFieldChange {
	return HistoryFieldChange{Field: name, Before: before, After: after}
}

func compactHistoryFields(fields ...HistoryFieldChange) []HistoryFieldChange {
	output := make([]HistoryFieldChange, 0, len(fields))
	for _, field := range fields {
		if field.Before != field.After {
			output = append(output, field)
		}
	}
	return output
}

func firstHistoryValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func historyCoordinates(value Coordinates) string {
	return fmt.Sprintf("%.2f,%.2f,%.2f", value.X, value.Y, value.Z)
}

func historySortedStrings(values []string) string {
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	return strings.Join(copyValues, ",")
}

func historyWorkers(values []Worker) string {
	ids := make([]string, 0, len(values))
	for _, item := range values {
		ids = append(ids, firstHistoryValue(item.InstanceID, item.CharacterID, item.Nickname))
	}
	return historySortedStrings(ids)
}

func historyGuildMembers(values []GuildMember) string {
	ids := make([]string, 0, len(values))
	for _, item := range values {
		ids = append(ids, firstHistoryValue(item.PlayerUID, item.Nickname))
	}
	return historySortedStrings(ids)
}

func historyContainerContents(slots []Slot) string {
	counts := make(map[string]int)
	for _, slot := range slots {
		if slot.ItemID != "" && slot.Count != 0 {
			counts[slot.ItemID] += slot.Count
		}
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	const displayLimit = 20
	parts := make([]string, 0, min(len(keys), displayLimit)+1)
	canonical := strings.Builder{}
	for index, key := range keys {
		entry := key + "×" + strconv.Itoa(counts[key])
		canonical.WriteString(entry)
		canonical.WriteByte('\n')
		if index < displayLimit {
			parts = append(parts, entry)
		}
	}
	if len(keys) > displayLimit {
		digest := sha256.Sum256([]byte(canonical.String()))
		parts = append(parts, fmt.Sprintf("+%d more [sha256:%x]", len(keys)-displayLimit, digest[:6]))
	}
	return strings.Join(parts, ", ")
}

func historyItemCounts(containers []Container) map[string]int {
	counts := make(map[string]int)
	for _, container := range containers {
		for _, slot := range container.Slots {
			if slot.ItemID != "" {
				counts[slot.ItemID] += slot.Count
			}
		}
	}
	return counts
}
