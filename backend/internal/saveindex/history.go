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
	Retention          int               `json:"retention"`
	MinimumIntervalSec int               `json:"minimum_interval_seconds"`
	MaxTotalBytes      int64             `json:"max_total_bytes"`
	TotalBytes         int64             `json:"total_bytes"`
	Items              []HistorySnapshot `json:"items"`
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

// HistoryEvent is a human-oriented inference derived from two save snapshots.
// It does not claim to be a chronological game event log: multiple actions may
// have happened between snapshots, so each event is explicitly marked inferred.
type HistoryEvent struct {
	ID           string               `json:"id"`
	Category     string               `json:"category"`
	Kind         string               `json:"kind"`
	ActorType    string               `json:"actor_type"`
	ActorID      string               `json:"actor_id"`
	ActorLabel   string               `json:"actor_label"`
	SubjectType  string               `json:"subject_type"`
	SubjectID    string               `json:"subject_id"`
	SubjectLabel string               `json:"subject_label"`
	TargetType   string               `json:"target_type"`
	TargetID     string               `json:"target_id"`
	TargetLabel  string               `json:"target_label"`
	Delta        int                  `json:"delta,omitempty"`
	Before       string               `json:"before"`
	After        string               `json:"after"`
	Details      []HistoryFieldChange `json:"details"`
	Metadata     map[string]string    `json:"metadata"`
	Inferred     bool                 `json:"inferred"`
}

type HistoryDiff struct {
	From       HistorySnapshot    `json:"from"`
	To         HistorySnapshot    `json:"to"`
	Summary    HistoryDiffSummary `json:"summary"`
	Total      int                `json:"total"`
	Limit      int                `json:"limit"`
	Offset     int                `json:"offset"`
	Items      []HistoryChange    `json:"items"`
	EventTotal int                `json:"event_total"`
	Events     []HistoryEvent     `json:"events"`
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
	return m.captureCachedHistorySnapshot(false)
}

// ForceHistorySnapshot stores the current cached index even when the automatic
// semantic sampling interval has not elapsed. It is intended for explicit
// baselines and deterministic API tests; normal rebuilds must continue to use
// EnsureHistorySnapshot or captureHistorySnapshotIfDue.
func (m *Manager) ForceHistorySnapshot() error {
	return m.captureCachedHistorySnapshot(true)
}

func (m *Manager) captureCachedHistorySnapshot(force bool) error {
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
	if force {
		return m.captureHistorySnapshot(worldDir, cached.Fingerprint, cached.Index)
	}
	return m.captureHistorySnapshotIfDue(worldDir, cached.Fingerprint, cached.Index)
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
	worldKey := historyWorldKey(worldDir)
	manifest, err := m.loadHistoryManifest(worldKey)
	if err != nil {
		return HistoryState{}, err
	}
	removed := compactHistoryItems(&manifest.Items, m.historyMinInterval)
	if len(removed) > 0 {
		if err := m.writeHistoryManifest(manifest); err != nil {
			return HistoryState{}, err
		}
		removeHistoryArchives(m, worldKey, removed)
	}
	state := HistoryState{
		Retention: historyRetention, MinimumIntervalSec: int(m.historyMinInterval / time.Second),
		MaxTotalBytes: historyMaxTotalBytes, Items: append([]HistorySnapshot(nil), manifest.Items...),
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
	return m.captureHistorySnapshotWithPolicy(worldDir, fingerprint, index, true)
}

func (m *Manager) captureHistorySnapshotIfDue(worldDir, fingerprint string, index Index) error {
	return m.captureHistorySnapshotWithPolicy(worldDir, fingerprint, index, false)
}

func (m *Manager) captureHistorySnapshotWithPolicy(worldDir, fingerprint string, index Index, force bool) error {
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
	compacted := compactHistoryItems(&manifest.Items, m.historyMinInterval)
	if len(manifest.Items) > 0 && manifest.Items[0].Fingerprint == fingerprint {
		return m.persistHistoryCompaction(worldKey, manifest, compacted)
	}
	capturedAt := time.Now().UTC()
	if !force && m.historyMinInterval > 0 && len(manifest.Items) > 0 {
		if latestAt, ok := historySnapshotTime(manifest.Items[0]); ok && capturedAt.Sub(latestAt) < m.historyMinInterval {
			return m.persistHistoryCompaction(worldKey, manifest, compacted)
		}
	}
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
	removed := append(compacted, pruneHistoryItems(&manifest.Items)...)
	if err := m.writeHistoryManifest(manifest); err != nil {
		_ = os.Remove(path)
		return err
	}
	removeHistoryArchives(m, worldKey, removed)
	return nil
}

func (m *Manager) persistHistoryCompaction(worldKey string, manifest historyManifest, removed []HistorySnapshot) error {
	if len(removed) == 0 {
		return nil
	}
	if err := m.writeHistoryManifest(manifest); err != nil {
		return err
	}
	removeHistoryArchives(m, worldKey, removed)
	return nil
}

func removeHistoryArchives(m *Manager, worldKey string, items []HistorySnapshot) {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, exists := seen[item.ID]; exists {
			continue
		}
		seen[item.ID] = struct{}{}
		if oldPath, err := m.historySnapshotPath(worldKey, item.ID); err == nil {
			_ = os.Remove(oldPath)
		}
	}
}

// compactHistoryItems keeps snapshots far enough apart for meaningful semantic
// comparisons. The newest snapshot is retained, and a dense history keeps its
// oldest entry as a baseline when no item reaches the configured interval.
func compactHistoryItems(items *[]HistorySnapshot, minimumInterval time.Duration) []HistorySnapshot {
	if items == nil || len(*items) < 2 || minimumInterval <= 0 {
		return nil
	}
	original := *items
	kept := make([]HistorySnapshot, 0, len(original))
	removed := make([]HistorySnapshot, 0, len(original))
	kept = append(kept, original[0])
	lastAt, lastOK := historySnapshotTime(original[0])
	for index := 1; index < len(original); index++ {
		item := original[index]
		itemAt, itemOK := historySnapshotTime(item)
		if !lastOK || !itemOK || lastAt.Sub(itemAt) >= minimumInterval {
			kept = append(kept, item)
			lastAt, lastOK = itemAt, itemOK
			continue
		}
		removed = append(removed, item)
	}
	if len(kept) == 1 && len(original) > 1 {
		oldest := original[len(original)-1]
		kept = append(kept, oldest)
		for index, item := range removed {
			if item.ID == oldest.ID {
				removed = append(removed[:index], removed[index+1:]...)
				break
			}
		}
	}
	*items = kept
	return removed
}

func historySnapshotTime(item HistorySnapshot) (time.Time, bool) {
	for _, value := range []string{item.CapturedAt, item.GeneratedAt} {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
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
	events := buildHistoryEvents(from, to)

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
	filteredEvents := make([]HistoryEvent, 0, len(events))
	for _, event := range events {
		if category != "" && category != "all" && event.Category != category {
			continue
		}
		if query != "" && !historyEventMatches(event, query) {
			continue
		}
		filteredEvents = append(filteredEvents, event)
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
	eventOffset := offset
	if eventOffset > len(filteredEvents) {
		eventOffset = len(filteredEvents)
	}
	eventEnd := eventOffset + limit
	if eventEnd > len(filteredEvents) {
		eventEnd = len(filteredEvents)
	}
	return HistoryDiff{
		From: fromMeta, To: toMeta, Summary: summary,
		Total: len(filtered), Limit: limit, Offset: offset,
		Items:      append([]HistoryChange(nil), filtered[offset:end]...),
		EventTotal: len(filteredEvents),
		Events:     append([]HistoryEvent(nil), filteredEvents[eventOffset:eventEnd]...),
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

type historyEventOwner struct {
	Type  string
	ID    string
	Label string
}

func buildHistoryEvents(before, after Index) []HistoryEvent {
	events := make([]HistoryEvent, 0)
	events = append(events, historyPlayerEvents(before, after)...)
	events = append(events, historyGuildEvents(before, after)...)
	events = append(events, historyBaseEvents(before, after)...)
	events = append(events, historyPalEvents(before, after)...)
	events = append(events, historyItemEvents(before, after)...)
	sort.SliceStable(events, func(i, j int) bool {
		leftRank, rightRank := historyEventCategoryRank(events[i].Category), historyEventCategoryRank(events[j].Category)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if events[i].ActorLabel != events[j].ActorLabel {
			return events[i].ActorLabel < events[j].ActorLabel
		}
		if events[i].Kind != events[j].Kind {
			return events[i].Kind < events[j].Kind
		}
		return events[i].ID < events[j].ID
	})
	return events
}

func historyPlayerEvents(before, after Index) []HistoryEvent {
	left := historyPlayersByKey(before.Players)
	right := historyPlayersByKey(after.Players)
	keys := unionHistoryKeys(left, right)
	events := make([]HistoryEvent, 0)
	for _, key := range keys {
		oldPlayer, oldFound := left[key]
		newPlayer, newFound := right[key]
		switch {
		case !oldFound && newFound:
			events = append(events, newHistoryEvent("players", "player_joined", key,
				historyPlayerOwner(newPlayer), historyEventOwner{}, historyEventOwner{}, 0, "", "",
				[]HistoryFieldChange{{Field: "level", After: strconv.Itoa(newPlayer.Level)}},
				map[string]string{"player_uid": newPlayer.PlayerUID, "steam_id": newPlayer.SteamID}))
		case oldFound && !newFound:
			events = append(events, newHistoryEvent("players", "player_missing", key,
				historyPlayerOwner(oldPlayer), historyEventOwner{}, historyEventOwner{}, 0, "", "", nil,
				map[string]string{"player_uid": oldPlayer.PlayerUID, "steam_id": oldPlayer.SteamID}))
		case oldFound && newFound:
			actor := historyPlayerOwner(newPlayer)
			if newPlayer.Level != oldPlayer.Level {
				kind := "player_level_up"
				if newPlayer.Level < oldPlayer.Level {
					kind = "player_level_down"
				}
				events = append(events, newHistoryEvent("players", kind, key+":level", actor,
					historyEventOwner{Type: "level", ID: "level", Label: "level"}, historyEventOwner{},
					newPlayer.Level-oldPlayer.Level, strconv.Itoa(oldPlayer.Level), strconv.Itoa(newPlayer.Level), nil, nil))
			}
			oldGuild := firstHistoryValue(oldPlayer.GuildName, oldPlayer.GuildID)
			newGuild := firstHistoryValue(newPlayer.GuildName, newPlayer.GuildID)
			if oldGuild != newGuild {
				kind := "player_guild_changed"
				if oldGuild == "" {
					kind = "player_joined_guild"
				} else if newGuild == "" {
					kind = "player_left_guild"
				}
				events = append(events, newHistoryEvent("players", kind, key+":guild", actor,
					historyEventOwner{Type: "guild", ID: newPlayer.GuildID, Label: newGuild}, historyEventOwner{},
					0, oldGuild, newGuild, nil, nil))
			}
			if strings.TrimSpace(oldPlayer.Nickname) != strings.TrimSpace(newPlayer.Nickname) {
				events = append(events, newHistoryEvent("players", "player_renamed", key+":nickname", actor,
					historyEventOwner{Type: "player", ID: actor.ID, Label: actor.Label}, historyEventOwner{}, 0,
					oldPlayer.Nickname, newPlayer.Nickname, nil, nil))
			}
		}
	}
	return events
}

func historyGuildEvents(before, after Index) []HistoryEvent {
	left := mapHistoryBy(before.Guilds, func(item Guild) string { return item.ID })
	right := mapHistoryBy(after.Guilds, func(item Guild) string { return item.ID })
	players := historyPlayerLabels(append(append([]Player(nil), before.Players...), after.Players...))
	keys := unionHistoryKeys(left, right)
	events := make([]HistoryEvent, 0)
	for _, key := range keys {
		oldGuild, oldFound := left[key]
		newGuild, newFound := right[key]
		switch {
		case !oldFound && newFound:
			events = append(events, newHistoryEvent("guilds", "guild_created", key,
				historyEventOwner{Type: "guild", ID: key, Label: firstHistoryValue(newGuild.Name, key)}, historyEventOwner{}, historyEventOwner{},
				0, "", "", nil, nil))
		case oldFound && !newFound:
			events = append(events, newHistoryEvent("guilds", "guild_removed", key,
				historyEventOwner{Type: "guild", ID: key, Label: firstHistoryValue(oldGuild.Name, key)}, historyEventOwner{}, historyEventOwner{},
				0, "", "", nil, nil))
		case oldFound && newFound:
			actor := historyEventOwner{Type: "guild", ID: key, Label: firstHistoryValue(newGuild.Name, oldGuild.Name, key)}
			oldMembers := historyGuildMemberMap(oldGuild.Members)
			newMembers := historyGuildMemberMap(newGuild.Members)
			for _, memberKey := range unionHistoryKeys(oldMembers, newMembers) {
				oldMember, wasMember := oldMembers[memberKey]
				newMember, isMember := newMembers[memberKey]
				if wasMember == isMember {
					continue
				}
				member := newMember
				kind := "guild_member_joined"
				if !isMember {
					member = oldMember
					kind = "guild_member_left"
				}
				label := firstHistoryValue(member.Nickname, players[historyCanonicalID(member.PlayerUID)], member.PlayerUID)
				events = append(events, newHistoryEvent("guilds", kind, key+":member:"+memberKey, actor,
					historyEventOwner{Type: "player", ID: member.PlayerUID, Label: label}, historyEventOwner{}, 0, "", "", nil, nil))
			}
			oldBases := historyStringSet(oldGuild.BaseIDs)
			newBases := historyStringSet(newGuild.BaseIDs)
			for _, baseID := range unionHistoryKeys(oldBases, newBases) {
				_, hadBase := oldBases[baseID]
				_, hasBase := newBases[baseID]
				if hadBase == hasBase {
					continue
				}
				kind := "guild_base_added"
				if !hasBase {
					kind = "guild_base_removed"
				}
				events = append(events, newHistoryEvent("guilds", kind, key+":base:"+baseID, actor,
					historyEventOwner{Type: "base", ID: baseID, Label: baseID}, historyEventOwner{}, 0, "", "", nil, nil))
			}
			if oldGuild.OwnerPlayerUID != newGuild.OwnerPlayerUID {
				events = append(events, newHistoryEvent("guilds", "guild_owner_changed", key+":owner", actor,
					historyEventOwner{Type: "player", ID: newGuild.OwnerPlayerUID, Label: players[historyCanonicalID(newGuild.OwnerPlayerUID)]}, historyEventOwner{},
					0, players[historyCanonicalID(oldGuild.OwnerPlayerUID)], players[historyCanonicalID(newGuild.OwnerPlayerUID)], nil, nil))
			}
		}
	}
	return events
}

func historyBaseEvents(before, after Index) []HistoryEvent {
	left := mapHistoryBy(before.Bases, func(item Base) string { return item.ID })
	right := mapHistoryBy(after.Bases, func(item Base) string { return item.ID })
	palLabels := historyPalLabels(before.Pals, after.Pals)
	keys := unionHistoryKeys(left, right)
	events := make([]HistoryEvent, 0)
	for _, key := range keys {
		oldBase, oldFound := left[key]
		newBase, newFound := right[key]
		switch {
		case !oldFound && newFound:
			events = append(events, newHistoryEvent("bases", "base_created", key, historyBaseOwner(newBase), historyEventOwner{}, historyEventOwner{},
				0, "", "", []HistoryFieldChange{{Field: "structures", After: strconv.Itoa(newBase.StructuresCount)}, {Field: "workers", After: strconv.Itoa(len(newBase.Workers))}}, nil))
		case oldFound && !newFound:
			events = append(events, newHistoryEvent("bases", "base_removed", key, historyBaseOwner(oldBase), historyEventOwner{}, historyEventOwner{},
				0, "", "", []HistoryFieldChange{{Field: "structures", Before: strconv.Itoa(oldBase.StructuresCount)}, {Field: "workers", Before: strconv.Itoa(len(oldBase.Workers))}}, nil))
		case oldFound && newFound:
			actor := historyBaseOwner(newBase)
			if oldBase.StructuresCount != newBase.StructuresCount {
				kind := "base_structures_added"
				if newBase.StructuresCount < oldBase.StructuresCount {
					kind = "base_structures_removed"
				}
				events = append(events, newHistoryEvent("bases", kind, key+":structures", actor,
					historyEventOwner{Type: "structure", ID: "structures", Label: "structures"}, historyEventOwner{},
					newBase.StructuresCount-oldBase.StructuresCount, strconv.Itoa(oldBase.StructuresCount), strconv.Itoa(newBase.StructuresCount), nil, nil))
			}
			oldWorkers := historyWorkerMap(oldBase.Workers)
			newWorkers := historyWorkerMap(newBase.Workers)
			for _, workerKey := range unionHistoryKeys(oldWorkers, newWorkers) {
				oldWorker, hadWorker := oldWorkers[workerKey]
				newWorker, hasWorker := newWorkers[workerKey]
				if hadWorker == hasWorker {
					continue
				}
				worker := newWorker
				kind := "base_worker_assigned"
				if !hasWorker {
					worker = oldWorker
					kind = "base_worker_removed"
				}
				label := firstHistoryValue(worker.Nickname, palLabels[historyCanonicalID(worker.InstanceID)], worker.CharacterID, worker.InstanceID)
				events = append(events, newHistoryEvent("bases", kind, key+":worker:"+workerKey, actor,
					historyEventOwner{Type: "pal", ID: worker.InstanceID, Label: label}, historyEventOwner{}, 0, "", "", nil,
					map[string]string{"character_id": worker.CharacterID, "level": strconv.Itoa(worker.Level)}))
			}
			if len(oldBase.Containers) != len(newBase.Containers) {
				events = append(events, newHistoryEvent("bases", "base_storage_changed", key+":containers", actor,
					historyEventOwner{Type: "container", ID: "containers", Label: "containers"}, historyEventOwner{},
					len(newBase.Containers)-len(oldBase.Containers), strconv.Itoa(len(oldBase.Containers)), strconv.Itoa(len(newBase.Containers)), nil, nil))
			}
			if oldBase.Status != newBase.Status {
				events = append(events, newHistoryEvent("bases", "base_status_changed", key+":status", actor,
					historyEventOwner{Type: "status", ID: "status", Label: "status"}, historyEventOwner{}, 0, oldBase.Status, newBase.Status, nil, nil))
			}
			if historyCoordinates(oldBase.Location) != historyCoordinates(newBase.Location) {
				events = append(events, newHistoryEvent("bases", "base_moved", key+":location", actor,
					historyEventOwner{Type: "location", ID: "location", Label: "location"}, historyEventOwner{}, 0,
					historyCoordinates(oldBase.Location), historyCoordinates(newBase.Location), nil, nil))
			}
		}
	}
	return events
}

func historyPalEvents(before, after Index) []HistoryEvent {
	left := mapHistoryBy(before.Pals, func(item Pal) string { return item.InstanceID })
	right := mapHistoryBy(after.Pals, func(item Pal) string { return item.InstanceID })
	players := historyPlayerLabels(append(append([]Player(nil), before.Players...), after.Players...))
	bases := historyBaseLabels(before.Bases, after.Bases)
	keys := unionHistoryKeys(left, right)
	events := make([]HistoryEvent, 0)
	for _, key := range keys {
		oldPal, oldFound := left[key]
		newPal, newFound := right[key]
		switch {
		case !oldFound && newFound:
			actor := historyPalOwner(newPal, players, bases)
			subject := historyPalSubject(newPal)
			events = append(events, newHistoryEvent("pals", "pal_acquired", key, actor, subject, historyEventOwner{}, 1, "", "", nil,
				historyPalMetadata(newPal)))
		case oldFound && !newFound:
			actor := historyPalOwner(oldPal, players, bases)
			subject := historyPalSubject(oldPal)
			events = append(events, newHistoryEvent("pals", "pal_lost", key, actor, subject, historyEventOwner{}, -1, "", "", nil,
				historyPalMetadata(oldPal)))
		case oldFound && newFound:
			oldOwner := historyPalOwner(oldPal, players, bases)
			newOwner := historyPalOwner(newPal, players, bases)
			subject := historyPalSubject(newPal)
			if historyOwnerKey(oldOwner) != historyOwnerKey(newOwner) {
				events = append(events, newHistoryEvent("pals", "pal_transferred", key+":owner", oldOwner, subject, newOwner, 0,
					oldOwner.Label, newOwner.Label, nil, historyPalMetadata(newPal)))
			}
			if oldPal.Level != newPal.Level || oldPal.Rank != newPal.Rank {
				details := compactHistoryFields(
					historyField("level", strconv.Itoa(oldPal.Level), strconv.Itoa(newPal.Level)),
					historyField("rank", strconv.Itoa(oldPal.Rank), strconv.Itoa(newPal.Rank)),
				)
				events = append(events, newHistoryEvent("pals", "pal_progressed", key+":progress", newOwner, subject, historyEventOwner{},
					newPal.Level-oldPal.Level, strconv.Itoa(oldPal.Level), strconv.Itoa(newPal.Level), details, historyPalMetadata(newPal)))
			}
			if oldPal.ContainerID != newPal.ContainerID || oldPal.LocationType != newPal.LocationType || oldPal.Status != newPal.Status {
				details := compactHistoryFields(
					historyField("container", oldPal.ContainerID, newPal.ContainerID),
					historyField("location_type", oldPal.LocationType, newPal.LocationType),
					historyField("status", oldPal.Status, newPal.Status),
				)
				events = append(events, newHistoryEvent("pals", "pal_assignment_changed", key+":assignment", newOwner, subject, historyEventOwner{}, 0,
					oldPal.LocationType, newPal.LocationType, details, historyPalMetadata(newPal)))
			}
			if historySortedStrings(oldPal.Passives) != historySortedStrings(newPal.Passives) {
				events = append(events, newHistoryEvent("pals", "pal_passives_changed", key+":passives", newOwner, subject, historyEventOwner{}, 0,
					historySortedStrings(oldPal.Passives), historySortedStrings(newPal.Passives), nil, historyPalMetadata(newPal)))
			}
		}
	}
	return events
}

func historyItemEvents(before, after Index) []HistoryEvent {
	left := historyOwnedItemCounts(before)
	right := historyOwnedItemCounts(after)
	owners := make(map[string]historyEventOwner, len(left.Owners)+len(right.Owners))
	for key, owner := range left.Owners {
		owners[key] = owner
	}
	for key, owner := range right.Owners {
		owners[key] = owner
	}
	ownerKeys := unionHistoryKeys(left.Items, right.Items)
	events := make([]HistoryEvent, 0)
	for _, ownerKey := range ownerKeys {
		oldItems := left.Items[ownerKey]
		newItems := right.Items[ownerKey]
		for _, itemID := range unionHistoryKeys(oldItems, newItems) {
			delta := newItems[itemID] - oldItems[itemID]
			if delta == 0 {
				continue
			}
			kind := "item_gained"
			if delta < 0 {
				kind = "item_lost"
			}
			owner := owners[ownerKey]
			events = append(events, newHistoryEvent("items", kind, ownerKey+":"+itemID, owner,
				historyEventOwner{Type: "item", ID: itemID, Label: itemID}, historyEventOwner{}, delta,
				strconv.Itoa(oldItems[itemID]), strconv.Itoa(newItems[itemID]), nil,
				map[string]string{"item_id": itemID, "owner_type": owner.Type, "equipment": strconv.FormatBool(historyLikelyEquipment(itemID))}))
		}
	}
	return events
}

type historyOwnedItems struct {
	Owners map[string]historyEventOwner
	Items  map[string]map[string]int
}

func historyOwnedItemCounts(index Index) historyOwnedItems {
	containerOwners := historyContainerOwners(index)
	result := historyOwnedItems{Owners: map[string]historyEventOwner{}, Items: map[string]map[string]int{}}
	for _, container := range index.Containers {
		owner, found := containerOwners[historyCanonicalID(container.ContainerID)]
		if !found {
			owner = historyEventOwner{Type: "unknown", ID: "unknown", Label: "未归属容器"}
		}
		ownerKey := historyOwnerKey(owner)
		if ownerKey == "" {
			ownerKey = "unknown:unknown"
		}
		result.Owners[ownerKey] = owner
		counts := result.Items[ownerKey]
		if counts == nil {
			counts = map[string]int{}
			result.Items[ownerKey] = counts
		}
		for _, slot := range container.Slots {
			if itemID := strings.TrimSpace(slot.ItemID); itemID != "" {
				counts[itemID] += slot.Count
			}
		}
	}
	return result
}

func historyContainerOwners(index Index) map[string]historyEventOwner {
	owners := make(map[string]historyEventOwner, len(index.Containers))
	players := historyPlayerLabels(index.Players)
	bases := historyBaseLabels(index.Bases)
	for _, base := range index.Bases {
		owner := historyBaseOwner(base)
		for _, containerID := range base.Containers {
			if key := historyCanonicalID(containerID); key != "" {
				owners[key] = owner
			}
		}
	}
	for _, container := range index.Containers {
		key := historyCanonicalID(container.ContainerID)
		if key == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(container.OwnerType)) {
		case "player":
			label := players[historyCanonicalID(container.OwnerID)]
			owners[key] = historyEventOwner{Type: "player", ID: container.OwnerID, Label: firstHistoryValue(label, container.OwnerID, "未知玩家")}
		case "base":
			label := bases[historyCanonicalID(container.OwnerID)]
			owners[key] = historyEventOwner{Type: "base", ID: container.OwnerID, Label: firstHistoryValue(label, container.OwnerID, "未知据点")}
		case "guild":
			owners[key] = historyEventOwner{Type: "guild", ID: container.OwnerID, Label: firstHistoryValue(container.OwnerID, "未知公会")}
		default:
			if _, linked := owners[key]; !linked && strings.TrimSpace(container.OwnerID) != "" {
				owners[key] = historyEventOwner{Type: firstHistoryValue(container.OwnerType, "unknown"), ID: container.OwnerID, Label: container.OwnerID}
			}
		}
	}
	return owners
}

func newHistoryEvent(category, kind, id string, actor, subject, target historyEventOwner, delta int, before, after string, details []HistoryFieldChange, metadata map[string]string) HistoryEvent {
	if details == nil {
		details = []HistoryFieldChange{}
	}
	return HistoryEvent{
		ID: id, Category: category, Kind: kind,
		ActorType: actor.Type, ActorID: actor.ID, ActorLabel: actor.Label,
		SubjectType: subject.Type, SubjectID: subject.ID, SubjectLabel: subject.Label,
		TargetType: target.Type, TargetID: target.ID, TargetLabel: target.Label,
		Delta: delta, Before: before, After: after, Details: details, Metadata: metadata, Inferred: true,
	}
}

func historyPlayerOwner(player Player) historyEventOwner {
	return historyEventOwner{Type: "player", ID: firstHistoryValue(player.PlayerUID, player.SteamID), Label: firstHistoryValue(player.Nickname, player.SteamID, player.PlayerUID, "未知玩家")}
}

func historyBaseOwner(base Base) historyEventOwner {
	return historyEventOwner{Type: "base", ID: base.ID, Label: firstHistoryValue(base.Name, base.ID, "未知据点")}
}

func historyPalSubject(pal Pal) historyEventOwner {
	return historyEventOwner{Type: "pal", ID: pal.InstanceID, Label: firstHistoryValue(pal.Nickname, pal.CharacterID, pal.InstanceID, "未知帕鲁")}
}

func historyPalOwner(pal Pal, players, bases map[string]string) historyEventOwner {
	if playerID := strings.TrimSpace(pal.OwnerPlayerUID); playerID != "" {
		return historyEventOwner{Type: "player", ID: playerID, Label: firstHistoryValue(players[historyCanonicalID(playerID)], playerID, "未知玩家")}
	}
	if strings.EqualFold(strings.TrimSpace(pal.LocationType), "base") || strings.Contains(strings.ToLower(pal.LocationType), "base") {
		if label := bases[historyCanonicalID(pal.ContainerID)]; label != "" {
			return historyEventOwner{Type: "base", ID: pal.ContainerID, Label: label}
		}
	}
	if guildID := strings.TrimSpace(pal.GuildID); guildID != "" {
		return historyEventOwner{Type: "guild", ID: guildID, Label: guildID}
	}
	return historyEventOwner{Type: "world", ID: "world", Label: "世界"}
}

func historyPalMetadata(pal Pal) map[string]string {
	return map[string]string{
		"character_id":  pal.CharacterID,
		"nickname":      pal.Nickname,
		"level":         strconv.Itoa(pal.Level),
		"rank":          strconv.Itoa(pal.Rank),
		"location_type": pal.LocationType,
		"status":        pal.Status,
	}
}

func historyPlayersByKey(players []Player) map[string]Player {
	output := make(map[string]Player, len(players))
	for _, player := range players {
		if key := historyPlayerKey(player); key != "" {
			output[key] = player
		}
	}
	return output
}

func historyPlayerLabels(players []Player) map[string]string {
	labels := make(map[string]string, len(players)*2)
	for _, player := range players {
		label := firstHistoryValue(player.Nickname, player.SteamID, player.PlayerUID)
		for _, value := range []string{player.PlayerUID, player.SteamID} {
			if key := historyCanonicalID(value); key != "" {
				labels[key] = label
			}
		}
	}
	return labels
}

func historyBaseLabels(groups ...[]Base) map[string]string {
	labels := map[string]string{}
	for _, bases := range groups {
		for _, base := range bases {
			label := firstHistoryValue(base.Name, base.ID)
			for _, value := range append([]string{base.ID}, base.Containers...) {
				if key := historyCanonicalID(value); key != "" {
					labels[key] = label
				}
			}
		}
	}
	return labels
}

func historyPalLabels(groups ...[]Pal) map[string]string {
	labels := map[string]string{}
	for _, pals := range groups {
		for _, pal := range pals {
			if key := historyCanonicalID(pal.InstanceID); key != "" {
				labels[key] = firstHistoryValue(pal.Nickname, pal.CharacterID, pal.InstanceID)
			}
		}
	}
	return labels
}

func historyGuildMemberMap(members []GuildMember) map[string]GuildMember {
	output := make(map[string]GuildMember, len(members))
	for _, member := range members {
		key := historyCanonicalID(firstHistoryValue(member.PlayerUID, member.Nickname))
		if key != "" {
			output[key] = member
		}
	}
	return output
}

func historyWorkerMap(workers []Worker) map[string]Worker {
	output := make(map[string]Worker, len(workers))
	for _, worker := range workers {
		key := historyCanonicalID(firstHistoryValue(worker.InstanceID, worker.CharacterID, worker.Nickname))
		if key != "" {
			output[key] = worker
		}
	}
	return output
}

func historyStringSet(values []string) map[string]struct{} {
	output := make(map[string]struct{}, len(values))
	for _, value := range values {
		if key := strings.TrimSpace(value); key != "" {
			output[key] = struct{}{}
		}
	}
	return output
}

func historyOwnerKey(owner historyEventOwner) string {
	if strings.TrimSpace(owner.Type) == "" && strings.TrimSpace(owner.ID) == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(owner.Type)) + ":" + historyCanonicalID(firstHistoryValue(owner.ID, owner.Label))
}

func historyCanonicalID(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
}

func historyLikelyEquipment(itemID string) bool {
	value := strings.ToLower(strings.TrimSpace(itemID))
	for _, token := range []string{"weapon", "armor", "armour", "helmet", "shield", "accessory", "glider", "spear", "sword", "rifle", "pistol", "shotgun", "launcher", "bow", "bat", "axe", "pickaxe"} {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

func historyEventCategoryRank(category string) int {
	switch category {
	case "players":
		return 0
	case "items":
		return 1
	case "pals":
		return 2
	case "bases":
		return 3
	case "guilds":
		return 4
	default:
		return 5
	}
}

func historyEventMatches(event HistoryEvent, query string) bool {
	parts := []string{
		event.ID, event.Category, event.Kind, event.ActorType, event.ActorID, event.ActorLabel,
		event.SubjectType, event.SubjectID, event.SubjectLabel, event.TargetType, event.TargetID, event.TargetLabel,
		event.Before, event.After,
	}
	for _, detail := range event.Details {
		parts = append(parts, detail.Field, detail.Before, detail.After)
	}
	for key, value := range event.Metadata {
		parts = append(parts, key, value)
	}
	return strings.Contains(strings.ToLower(strings.Join(parts, " ")), query)
}
