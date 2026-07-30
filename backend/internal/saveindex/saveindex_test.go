package saveindex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"palpanel/internal/appconfig"
)

func TestRebuildWritesCacheAndCurrentReadsIt(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"version":      1,
				"source_path":  "ignored",
				"generated_at": "2026-07-09T00:00:00Z",
				"duration_ms":  7,
				"parser":       "test",
				"warnings":     []string{},
				"players":      []map[string]any{{"player_uid": "p1", "nickname": "Tester", "level": 2}},
				"guilds":       []map[string]any{},
				"bases":        []map[string]any{},
				"pals":         []map[string]any{},
				"containers":   []map[string]any{},
				"map_entities": []map[string]any{},
			},
		})
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL

	m := NewManager(cfg)
	index, status, err := m.Rebuild(t.Context())
	if err != nil {
		t.Fatalf("Rebuild returned error: %v", err)
	}
	if status.State != "ready" || status.Counts.Players != 1 || index.Players[0].Nickname != "Tester" {
		t.Fatalf("unexpected rebuild result: status=%#v index=%#v", status, index)
	}

	index, status, err = m.Current(t.Context())
	if err != nil {
		t.Fatalf("Current returned error: %v", err)
	}
	if status.State != "ready" || status.Stale || len(index.Players) != 1 {
		t.Fatalf("unexpected current result: status=%#v index=%#v", status, index)
	}
}

func TestRebuildRetriesWhenSaveChangesDuringIndexing(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	levelPath := filepath.Join(root, "server", "Pal", "Saved", "SaveGames", "0", "world", "Level.sav")
	initialMTime := time.Date(2026, time.July, 18, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(levelPath, initialMTime, initialMTime); err != nil {
		t.Fatalf("set initial Level.sav mtime: %v", err)
	}
	var calls int32
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			writeWorld(t, root, "level-two")
			changedMTime := initialMTime.Add(2 * time.Second)
			if err := os.Chtimes(levelPath, changedMTime, changedMTime); err != nil {
				t.Errorf("advance Level.sav mtime: %v", err)
				http.Error(w, "failed to update test save timestamp", http.StatusInternalServerError)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"version": 1, "generated_at": "2026-07-18T00:00:00Z", "parser": "test", "warnings": []string{},
				"players": []map[string]any{}, "guilds": []map[string]any{}, "bases": []map[string]any{},
				"pals": []map[string]any{}, "containers": []map[string]any{}, "map_entities": []map[string]any{},
			},
		})
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL

	m := NewManager(cfg)
	_, status, err := m.Rebuild(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	callCount := atomic.LoadInt32(&calls)
	if callCount != 2 || status.State != "ready" || status.Stale {
		t.Fatalf("rebuild did not settle after retry: calls=%d status=%#v", callCount, status)
	}
	_, currentStatus, err := m.Current(t.Context())
	if err != nil || currentStatus.State != "ready" {
		t.Fatalf("settled cache is not current: %#v, %v", currentStatus, err)
	}
}

func TestCurrentBeforeFirstIndexIsNotAnError(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	indexerCalled := false
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":   true,
				"data": map[string]any{"oodle": true},
			})
			return
		}
		indexerCalled = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL

	m := NewManager(cfg)
	index, status, err := m.Current(t.Context())
	if err != nil {
		t.Fatalf("Current returned an error before the first index: %v", err)
	}
	if status.State != "not_indexed" || status.Error != "" || len(index.Players) != 0 {
		t.Fatalf("unexpected current result: status=%#v index=%#v", status, index)
	}
	status = m.Status(t.Context())
	if status.State != "not_indexed" || status.Error != "" {
		t.Fatalf("unexpected status before the first index: %#v", status)
	}
	if indexerCalled {
		t.Fatal("Current should not call the sidecar indexer on cache miss")
	}
}

func TestFingerprintUsesMetadataOnly(t *testing.T) {
	root, _ := testConfig(t)
	writeWorld(t, root, "same-size")
	world := filepath.Join(root, "server", "Pal", "Saved", "SaveGames", "0", "world")
	level := filepath.Join(world, "Level.sav")
	st, err := os.Stat(level)
	if err != nil {
		t.Fatalf("stat Level.sav: %v", err)
	}
	before, err := fingerprintWorld(world)
	if err != nil {
		t.Fatalf("fingerprintWorld returned error: %v", err)
	}
	if err := os.WriteFile(level, []byte("diff-size"), 0o644); err != nil {
		t.Fatalf("rewrite Level.sav: %v", err)
	}
	if err := os.Chtimes(level, st.ModTime(), st.ModTime()); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}
	after, err := fingerprintWorld(world)
	if err != nil {
		t.Fatalf("fingerprintWorld returned error: %v", err)
	}
	if before != after {
		t.Fatalf("metadata-only fingerprint changed after same-size same-mtime content edit: before=%s after=%s", before, after)
	}
}

func TestFingerprintIgnoresUnusedLevelMetaChanges(t *testing.T) {
	root, _ := testConfig(t)
	writeWorld(t, root, "level")
	world := filepath.Join(root, "server", "Pal", "Saved", "SaveGames", "0", "world")
	before, err := fingerprintWorld(world)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(world, "LevelMeta.sav"), []byte("updated metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := fingerprintWorld(world)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("unused LevelMeta.sav changed the index fingerprint: %s -> %s", before, after)
	}
}

func TestRebuildFailureKeepsStaleCache(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	fail := false
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]any{"code": "parser_failed", "message": "boom"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"version":      1,
				"generated_at": "2026-07-09T00:00:00Z",
				"parser":       "test",
				"warnings":     []string{},
				"players":      []map[string]any{{"player_uid": "p1", "nickname": "Tester"}},
				"guilds":       []map[string]any{},
				"bases":        []map[string]any{},
				"pals":         []map[string]any{},
				"containers":   []map[string]any{},
				"map_entities": []map[string]any{},
			},
		})
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL

	m := NewManager(cfg)
	if _, _, err := m.Rebuild(t.Context()); err != nil {
		t.Fatalf("initial Rebuild returned error: %v", err)
	}
	writeWorld(t, root, "level-two")
	fail = true
	index, status, err := m.Rebuild(t.Context())
	if err == nil {
		t.Fatal("expected rebuild error")
	}
	if !status.Stale || status.State != "stale" || len(index.Players) != 1 {
		t.Fatalf("expected stale cached index after failure, got status=%#v index=%#v", status, index)
	}
	cached, readErr := m.loadCache()
	if readErr != nil {
		t.Fatalf("loadCache after failure: %v", readErr)
	}
	if cached.Status.State != "stale" || !cached.Status.Stale {
		t.Fatalf("cache was persisted with the wrong state: %#v", cached.Status)
	}
}

func TestRebuildFailureWithoutCacheReturnsErrorState(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]any{"code": "parser_failed", "message": "boom"}})
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL

	index, status, err := NewManager(cfg).Rebuild(t.Context())
	if err == nil {
		t.Fatal("expected rebuild error")
	}
	if status.State != "error" || status.Stale || len(index.Players) != 0 {
		t.Fatalf("expected hard error without cache, got status=%#v index=%#v", status, index)
	}
}

func TestStatusNormalizesLegacyErrorStaleCache(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	m := NewManager(cfg)
	worldDir, fp, err := m.fingerprint()
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	cached := cacheFile{
		Fingerprint: fp,
		SavedAt:     time.Now().UTC().Format(time.RFC3339),
		Index: Index{
			Version:     1,
			SourcePath:  worldDir,
			GeneratedAt: "2026-07-09T00:00:00Z",
			Parser:      "test",
			Warnings:    []string{"legacy warning"},
			Players:     []Player{{PlayerUID: "p1", Nickname: "Tester"}},
			Guilds:      []Guild{},
			Bases:       []Base{},
			Pals:        []Pal{},
			Containers:  []Container{},
			MapEntities: []MapEntity{},
		},
		Status: Status{Enabled: true, State: "error", Stale: true, SourcePath: worldDir, Warnings: []string{"legacy warning"}, CachePath: m.cachePath()},
	}
	b, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		t.Fatalf("marshal cache: %v", err)
	}
	if err := os.MkdirAll(cfg.SaveIndexCacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(m.cachePath(), b, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	index, status, err := m.Current(t.Context())
	if err != nil {
		t.Fatalf("Current returned error: %v", err)
	}
	if status.State != "stale" || !status.Stale || len(index.Players) != 1 {
		t.Fatalf("legacy cache was not normalized on read: status=%#v index=%#v", status, index)
	}
	if len(status.Warnings) == 0 || status.Warnings[0] != "legacy warning" {
		t.Fatalf("legacy warnings were not preserved: %#v", status.Warnings)
	}
}

func TestRebuildNeverReturnsRawBinarySidecarPayload(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	rawSave := append([]byte("GVAS"), bytes.Repeat([]byte{0x00, 0xff, 0x81, 0x10}, 256)...)
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(rawSave)
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL

	_, status, err := NewManager(cfg).Rebuild(t.Context())
	if err == nil {
		t.Fatal("expected a non-JSON sidecar response to fail")
	}
	for _, message := range []string{err.Error(), status.Error} {
		if strings.ContainsRune(message, '\x00') || strings.Contains(message, "GVAS") || strings.Contains(message, "\\x00") {
			t.Fatalf("binary save content leaked into diagnostic text: %q", message)
		}
		if !strings.Contains(message, "non-JSON response") {
			t.Fatalf("unexpected safe diagnostic: %q", message)
		}
	}
}

func TestRebuildNeverReturnsBinaryTextFromStructuredSidecarError(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": false,
			"error": map[string]any{
				"code":    "parse_failed",
				"message": "GVAS \\x00\\xff raw payload",
			},
		})
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL

	_, status, err := NewManager(cfg).Rebuild(t.Context())
	if err == nil {
		t.Fatal("expected structured sidecar error to fail")
	}
	for _, message := range []string{err.Error(), status.Error} {
		if strings.Contains(message, "GVAS") || strings.Contains(message, "\\x") || strings.ContainsRune(message, '\x00') {
			t.Fatalf("sidecar error payload leaked into diagnostic text: %q", message)
		}
		if !strings.Contains(message, "inspect the sav-cli text logs") {
			t.Fatalf("unexpected safe diagnostic: %q", message)
		}
	}
}

func TestRebuildSurfacesStructuredErrorCode(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	var logOutput bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logOutput)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":   true,
				"data": map[string]any{"oodle": true},
			})
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": false,
			"error": map[string]any{
				"code":    "parser_incompatible",
				"message": "PlM Oodle decompression failed: GVAS \\x00 raw",
			},
			"warnings": []string{"a warning"},
		})
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL

	_, status, err := NewManager(cfg).Rebuild(t.Context())
	if err == nil {
		t.Fatal("expected structured sidecar error to fail")
	}
	if status.ErrorCode != "parser_incompatible" {
		t.Fatalf("expected surfaced error code, got %q", status.ErrorCode)
	}
	if status.OodleAvailable == nil || !*status.OodleAvailable {
		t.Fatalf("expected Oodle capability to be surfaced, got %#v", status.OodleAvailable)
	}
	if status.ErrorDetail != "PlM Oodle decompression failed: GVAS [byte] raw" {
		t.Fatalf("unexpected sanitized error detail: %q", status.ErrorDetail)
	}
	if strings.Contains(status.ErrorDetail, "\\x") || strings.ContainsRune(status.ErrorDetail, '\x00') {
		t.Fatalf("unsafe sidecar bytes leaked into error detail: %q", status.ErrorDetail)
	}
	if !strings.Contains(logOutput.String(), status.ErrorDetail) {
		t.Fatalf("sanitized sidecar detail was not written to backend logs: %q", logOutput.String())
	}
	if !slices.Contains(status.Warnings, "a warning") {
		t.Fatalf("expected sidecar warning to be preserved, got %#v", status.Warnings)
	}
	// The safe code is surfaced, but the raw (potentially binary) message never is.
	for _, message := range []string{err.Error(), status.Error} {
		if strings.Contains(message, "GVAS") || strings.Contains(message, "\\x") || strings.ContainsRune(message, '\x00') {
			t.Fatalf("sidecar error payload leaked into diagnostic text: %q", message)
		}
	}
}

func TestRebuildClassifiesUnavailableIndexer(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	sidecar := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	cfg.SaveIndexerURL = sidecar.URL
	sidecar.Close()

	_, status, err := NewManager(cfg).Rebuild(t.Context())
	if err == nil {
		t.Fatal("expected unavailable sidecar to fail")
	}
	if status.ErrorCode != "save_indexer_unavailable" {
		t.Fatalf("unexpected unavailable error code: %#v", status)
	}
}

func TestRebuildClassifiesIndexerTimeout(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	_, status, err := NewManager(cfg).Rebuild(ctx)
	if err == nil {
		t.Fatal("expected timed out sidecar to fail")
	}
	if status.ErrorCode != "save_index_timeout" {
		t.Fatalf("unexpected timeout error code: %#v", status)
	}
}

func TestStatusSurfacesAndCachesOodleCapability(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	var healthCalls atomic.Int32
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		healthCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"data": map[string]any{"oodle": false},
		})
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL
	m := NewManager(cfg)

	for range 2 {
		status := m.Status(t.Context())
		if status.OodleAvailable == nil || *status.OodleAvailable {
			t.Fatalf("expected surfaced oodle_available=false, got %#v", status.OodleAvailable)
		}
		encoded, err := json.Marshal(status)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(encoded, []byte(`"oodle_available":false`)) {
			t.Fatalf("oodle_available=false was omitted from JSON: %s", encoded)
		}
	}
	if calls := healthCalls.Load(); calls != 1 {
		t.Fatalf("expected Oodle health result to be cached, got %d calls", calls)
	}
}

func TestStatusOodleProbeTimesOutAfterOneSecond(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		time.Sleep(1100 * time.Millisecond)
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL

	startedAt := time.Now()
	status := NewManager(cfg).Status(t.Context())
	elapsed := time.Since(startedAt)
	if elapsed < 800*time.Millisecond || elapsed > 1500*time.Millisecond {
		t.Fatalf("Oodle probe duration = %s, want approximately one second", elapsed)
	}
	if status.OodleAvailable != nil {
		t.Fatalf("timed out health probe should leave Oodle availability unknown: %#v", status.OodleAvailable)
	}
}

func TestRebuildFailureKeepsStructuredErrorOnStaleCache(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	fail := false
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":   true,
				"data": map[string]any{"oodle": false},
			})
			return
		}
		if fail {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": false,
				"error": map[string]any{
					"code":    "parser_incompatible",
					"message": "raw parser details must remain private",
				},
				"warnings": []string{"sidecar retry warning"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"version":      1,
				"generated_at": "2026-07-09T00:00:00Z",
				"parser":       "test",
			},
		})
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL
	m := NewManager(cfg)
	if _, _, err := m.Rebuild(t.Context()); err != nil {
		t.Fatalf("initial Rebuild: %v", err)
	}

	fail = true
	writeWorld(t, root, "level-two")
	_, status, err := m.Rebuild(t.Context())
	if err == nil {
		t.Fatal("expected rebuild failure")
	}
	if !status.Stale || status.ErrorCode != "parser_incompatible" {
		t.Fatalf("stale failure lost structured status: %#v", status)
	}
	if !strings.Contains(status.Error, "parser_incompatible") {
		t.Fatalf("stale failure lost safe diagnostic: %#v", status)
	}
	if !slices.Contains(status.Warnings, oodleUnavailableWarning) {
		t.Fatalf("stale failure lost Oodle warning: %#v", status.Warnings)
	}
	if !slices.Contains(status.Warnings, "sidecar retry warning") {
		t.Fatalf("stale failure lost sidecar warning: %#v", status.Warnings)
	}

	persisted := m.Status(t.Context())
	if persisted.ErrorCode != status.ErrorCode || persisted.Error != status.Error || !slices.Contains(persisted.Warnings, oodleUnavailableWarning) {
		t.Fatalf("cached status did not persist the failure diagnostics: %#v", persisted)
	}
}

func TestSaveCacheAtomicallyReplacesCacheFile(t *testing.T) {
	_, cfg := testConfig(t)
	m := NewManager(cfg)
	if err := m.saveCache("initial", EmptyIndex(), Status{State: "ready", Warnings: []string{}}); err != nil {
		t.Fatal(err)
	}
	path := m.cachePath()
	snapshotPath := filepath.Join(cfg.SaveIndexCacheDir, "initial-cache-link.json")
	if err := os.Link(path, snapshotPath); err != nil {
		t.Fatal(err)
	}
	if err := m.saveCache("replacement", EmptyIndex(), Status{State: "ready", Warnings: []string{}}); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{snapshotPath: "initial", path: "replacement"} {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var cached cacheFile
		if err := json.Unmarshal(body, &cached); err != nil {
			t.Fatalf("cache %s is not complete JSON: %v", file, err)
		}
		if cached.Fingerprint != want {
			t.Fatalf("cache %s fingerprint = %q, want %q", file, cached.Fingerprint, want)
		}
	}
}

func TestRebuildRejectsUnknownErrorCode(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": false,
			"error": map[string]any{
				"code":    "GVAS \\x00 injected",
				"message": "raw",
			},
		})
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL

	_, status, err := NewManager(cfg).Rebuild(t.Context())
	if err == nil {
		t.Fatal("expected structured sidecar error to fail")
	}
	// An unrecognized code must not be echoed back verbatim.
	if strings.Contains(status.ErrorCode, "GVAS") || strings.Contains(status.ErrorCode, "\\x") {
		t.Fatalf("unknown code leaked into ErrorCode: %q", status.ErrorCode)
	}
	if status.ErrorDetail != "" {
		t.Fatalf("unknown error code exposed sidecar detail: %q", status.ErrorDetail)
	}
}

func TestSafeIndexerDetailRedactsPrivatePaths(t *testing.T) {
	privateWorld := `C:\Users\Admin\PalPanel\world`
	privateCache := `C:\Users\Admin\PalPanel\cache`
	detail := safeIndexerDetail(
		`failed c:/users/admin/palpanel/world and C:\USERS\ADMIN\PALPANEL\CACHE\index.json via https://localhost:8090/index?token=secret raw \x00`,
		privateWorld,
		privateCache,
	)
	if strings.Contains(strings.ToLower(detail), "users/admin") || strings.Contains(strings.ToLower(detail), `users\admin`) {
		t.Fatalf("private path leaked from detail: %q", detail)
	}
	if strings.Contains(detail, "token=secret") || !strings.Contains(detail, "<redacted>") || strings.Contains(detail, "\\x00") {
		t.Fatalf("detail was not safely normalized: %q", detail)
	}
}

func TestEnsureFreshTriggersOneBackgroundRebuildWhenStale(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	var calls int32
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"version":      1,
				"generated_at": "2026-07-09T00:00:00Z",
				"parser":       "test",
				"warnings":     []string{},
				"players":      []map[string]any{{"player_uid": "p1", "nickname": "Tester"}},
				"guilds":       []map[string]any{},
				"bases":        []map[string]any{},
				"pals":         []map[string]any{},
				"containers":   []map[string]any{},
				"map_entities": []map[string]any{},
			},
		})
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL
	m := NewManager(cfg)

	if _, _, err := m.Rebuild(t.Context()); err != nil {
		t.Fatalf("initial Rebuild: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 indexer call after initial rebuild, got %d", got)
	}

	// Change the world so the cached fingerprint is stale.
	writeWorld(t, root, "level-two-changed")

	// Multiple concurrent EnsureFresh calls must coalesce into one rebuild.
	for i := 0; i < 5; i++ {
		m.EnsureFresh(t.Context())
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&calls) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected exactly one background rebuild (2 total calls), got %d", got)
	}

	// Debounce: immediate follow-up calls must not trigger another rebuild.
	for i := 0; i < 5; i++ {
		m.EnsureFresh(t.Context())
	}
	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("debounce failed: expected 2 total calls, got %d", got)
	}
}

func TestEnsureFreshDoesNothingWhenCacheIsFresh(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	var calls int32
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"data": map[string]any{"version": 1, "generated_at": "2026-07-09T00:00:00Z", "parser": "test"},
		})
	}))
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL
	m := NewManager(cfg)
	if _, _, err := m.Rebuild(t.Context()); err != nil {
		t.Fatalf("initial Rebuild: %v", err)
	}
	for i := 0; i < 5; i++ {
		m.EnsureFresh(t.Context())
	}
	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fresh cache must not rebuild: expected 1 call, got %d", got)
	}
}

func TestLoadCacheUsesMemoryUntilFileMTimeChanges(t *testing.T) {
	root, cfg := testConfig(t)
	writeWorld(t, root, "level-one")
	sidecar := successfulSidecar(t, "Tester")
	defer sidecar.Close()
	cfg.SaveIndexerURL = sidecar.URL
	m := NewManager(cfg)
	if _, _, err := m.Rebuild(t.Context()); err != nil {
		t.Fatalf("Rebuild returned error: %v", err)
	}
	cachePath := filepath.Join(cfg.SaveIndexCacheDir, "index-cache.json")
	st, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat cache: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("{bad json"), 0o644); err != nil {
		t.Fatalf("corrupt cache: %v", err)
	}
	if err := os.Chtimes(cachePath, st.ModTime(), st.ModTime()); err != nil {
		t.Fatalf("restore cache mtime: %v", err)
	}
	index, _, err := m.Current(t.Context())
	if err != nil || index.Players[0].Nickname != "Tester" {
		t.Fatalf("expected memory cache despite on-disk corruption, err=%v index=%#v", err, index)
	}
	later := st.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(cachePath, later, later); err != nil {
		t.Fatalf("advance cache mtime: %v", err)
	}
	_, _, err = m.Current(t.Context())
	if err == nil || errors.Is(err, ErrDisabled) {
		t.Fatalf("expected changed cache mtime to invalidate memory cache, got %v", err)
	}
}

func successfulSidecar(t *testing.T, nickname string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"version":      1,
				"generated_at": "2026-07-09T00:00:00Z",
				"parser":       "test",
				"warnings":     []string{},
				"players":      []map[string]any{{"player_uid": "p1", "nickname": nickname}},
				"guilds":       []map[string]any{},
				"bases":        []map[string]any{},
				"pals":         []map[string]any{},
				"containers":   []map[string]any{},
				"map_entities": []map[string]any{},
			},
		})
	}))
}

func testConfig(t *testing.T) (string, appconfig.Config) {
	t.Helper()
	root := t.TempDir()
	return root, appconfig.Config{
		ServerDir:               filepath.Join(root, "server"),
		SaveIndexerEnabled:      true,
		SaveIndexCacheDir:       filepath.Join(root, "cache"),
		SaveIndexTimeoutSeconds: 5,
	}
}

func writeWorld(t *testing.T, root, content string) {
	t.Helper()
	world := filepath.Join(root, "server", "Pal", "Saved", "SaveGames", "0", "world")
	if err := os.MkdirAll(world, 0o755); err != nil {
		t.Fatalf("MkdirAll world: %v", err)
	}
	if err := os.WriteFile(filepath.Join(world, "Level.sav"), []byte(content), 0o644); err != nil {
		t.Fatalf("write Level.sav: %v", err)
	}
	if err := os.WriteFile(filepath.Join(world, "LevelMeta.sav"), []byte("meta"), 0o644); err != nil {
		t.Fatalf("write LevelMeta.sav: %v", err)
	}
}
