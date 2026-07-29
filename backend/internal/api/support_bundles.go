package api

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"palpanel/internal/buildinfo"
	"palpanel/internal/db"
)

const (
	supportBundleSchemaVersion  = 1
	supportBundleRetentionCount = 5
	supportBundleRetentionAge   = 14 * 24 * time.Hour
	supportBundleMaxBytes       = 50 * 1024 * 1024
	supportBundleMaxLogFiles    = 3
	supportBundleMaxLogBytes    = 128 * 1024
	supportBundleMaxLogsTotal   = 384 * 1024
	supportBundleTimeout        = 20 * time.Second
)

var (
	supportBundleMu         sync.RWMutex
	supportBundleIDPattern  = regexp.MustCompile(`^[a-f0-9]{32}$`)
	sensitiveKeyPattern     = regexp.MustCompile(`(?i)(password|passwd|secret|token|cookie|authorization|api[_-]?key|credential|private[_-]?key)`)
	pathKeyPattern          = regexp.MustCompile(`(?i)(^|_)(path|dir|directory|root|executable|working[_-]?directory)($|_)`)
	identityKeyPattern      = regexp.MustCompile(`(?i)(steam[_-]?id|player[_-]?id|player[_-]?name|uid|guid|ip|address|actor|username|nickname|display[_-]?name)`)
	bearerPattern           = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{8,}`)
	assignmentSecretPattern = regexp.MustCompile(`(?i)"?(password|passwd|secret|token|api[_-]?key|cookie|authorization)"?\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	windowsPathPattern      = regexp.MustCompile(`(?i)\b[A-Z]:\\(?:[^\s"'<>|]+\\)*[^\s"'<>|]*`)
	unixPathPattern         = regexp.MustCompile(`(?:^|[\s=:"'])/(?:home|root|var|opt|srv|mnt|media|tmp|etc|usr|run|data)(?:/[^\s"'<>]*)?`)
	ipv4Pattern             = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	guidPattern             = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	longHexPattern          = regexp.MustCompile(`(?i)\b[0-9a-f]{24,64}\b`)
	longNumericPattern      = regexp.MustCompile(`\b\d{15,20}\b`)
)

type supportBundleCreateRequest struct {
	IncludeLogs bool `json:"include_logs"`
	Confirm     bool `json:"confirm"`
}

type supportBundleMetadata struct {
	ID          string   `json:"id"`
	FileName    string   `json:"file_name"`
	CreatedAt   string   `json:"created_at"`
	SizeBytes   int64    `json:"size_bytes"`
	SHA256      string   `json:"sha256"`
	IncludeLogs bool     `json:"include_logs"`
	Entries     []string `json:"entries"`
}

type supportBundleEntry struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

type supportBundleCheck struct {
	ID       string `json:"id"`
	OK       bool   `json:"ok"`
	Required bool   `json:"required"`
	Message  string `json:"message"`
}

func (s Server) supportBundleStatus(c *gin.Context) {
	checks := s.collectSupportBundleChecks(c.Request.Context())
	ok(c, gin.H{
		"schema_version":         supportBundleSchemaVersion,
		"directory_ready":        checksReady(checks),
		"max_bundles":            supportBundleRetentionCount,
		"max_bundle_bytes":       supportBundleMaxBytes,
		"max_log_files":          supportBundleMaxLogFiles,
		"max_log_bytes_per_file": supportBundleMaxLogBytes,
		"retention_days":         int(supportBundleRetentionAge / (24 * time.Hour)),
		"checks":                 checks,
	})
}

func (s Server) listSupportBundles(c *gin.Context) {
	supportBundleMu.RLock()
	defer supportBundleMu.RUnlock()
	items, err := s.readSupportBundleMetadata()
	if err != nil {
		fail(c, http.StatusInternalServerError, "support_bundle_list_failed", sanitizeSupportText(err.Error()))
		return
	}
	ok(c, items)
}

func (s Server) createSupportBundle(c *gin.Context) {
	var request supportBundleCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil || !request.Confirm {
		fail(c, http.StatusBadRequest, "support_bundle_confirmation_required", "confirm must be true")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), supportBundleTimeout)
	defer cancel()
	supportBundleMu.Lock()
	defer supportBundleMu.Unlock()
	metadata, err := s.generateSupportBundle(ctx, request.IncludeLogs)
	if err != nil {
		status := http.StatusInternalServerError
		code := "support_bundle_create_failed"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			code = "support_bundle_timeout"
		}
		fail(c, status, code, sanitizeSupportText(err.Error()))
		return
	}
	created(c, metadata)
}

func (s Server) downloadSupportBundle(c *gin.Context) {
	supportBundleMu.RLock()
	defer supportBundleMu.RUnlock()
	id := strings.TrimSpace(c.Param("id"))
	if !supportBundleIDPattern.MatchString(id) {
		fail(c, http.StatusBadRequest, "support_bundle_id_invalid", "support bundle id is invalid")
		return
	}
	metadata, err := s.readSupportBundleMetadataFile(id)
	if errors.Is(err, os.ErrNotExist) {
		fail(c, http.StatusNotFound, "support_bundle_not_found", "support bundle not found")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "support_bundle_read_failed", sanitizeSupportText(err.Error()))
		return
	}
	path := filepath.Join(s.supportBundleDir(), id+".zip")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		fail(c, http.StatusNotFound, "support_bundle_not_found", "support bundle not found")
		return
	}
	if info.Size() != metadata.SizeBytes {
		fail(c, http.StatusConflict, "support_bundle_integrity_failed", "support bundle size does not match metadata")
		return
	}
	digest, err := hashRegularFile(path)
	if err != nil || digest != metadata.SHA256 {
		fail(c, http.StatusConflict, "support_bundle_integrity_failed", "support bundle checksum does not match metadata")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.FileAttachment(path, supportBundleDownloadName(metadata))
}

func (s Server) deleteSupportBundle(c *gin.Context) {
	supportBundleMu.Lock()
	defer supportBundleMu.Unlock()
	id := strings.TrimSpace(c.Param("id"))
	if !supportBundleIDPattern.MatchString(id) {
		fail(c, http.StatusBadRequest, "support_bundle_id_invalid", "support bundle id is invalid")
		return
	}
	found := false
	for _, suffix := range []string{".zip", ".json"} {
		path := filepath.Join(s.supportBundleDir(), id+suffix)
		if err := os.Remove(path); err == nil {
			found = true
		} else if !errors.Is(err, os.ErrNotExist) {
			fail(c, http.StatusInternalServerError, "support_bundle_delete_failed", sanitizeSupportText(err.Error()))
			return
		}
	}
	if !found {
		fail(c, http.StatusNotFound, "support_bundle_not_found", "support bundle not found")
		return
	}
	ok(c, gin.H{"deleted": true, "id": id})
}

func (s Server) generateSupportBundle(ctx context.Context, includeLogs bool) (supportBundleMetadata, error) {
	if err := ctx.Err(); err != nil {
		return supportBundleMetadata{}, err
	}
	dir := s.supportBundleDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return supportBundleMetadata{}, fmt.Errorf("create support bundle directory: %w", err)
	}
	if err := rejectSupportBundleSymlinks(dir); err != nil {
		return supportBundleMetadata{}, err
	}
	id, err := randomSupportBundleID()
	if err != nil {
		return supportBundleMetadata{}, err
	}
	createdAt := time.Now().UTC()
	entries, err := s.collectSupportBundleEntries(ctx, includeLogs)
	if err != nil {
		return supportBundleMetadata{}, err
	}
	entryMetadata := make([]supportBundleEntry, 0, len(entries))
	entryNames := make([]string, 0, len(entries)+1)
	for name, data := range entries {
		digest := sha256.Sum256(data)
		entryMetadata = append(entryMetadata, supportBundleEntry{Name: name, Size: len(data), SHA256: hex.EncodeToString(digest[:])})
		entryNames = append(entryNames, name)
	}
	sort.Slice(entryMetadata, func(i, j int) bool { return entryMetadata[i].Name < entryMetadata[j].Name })
	sort.Strings(entryNames)
	manifest := map[string]any{
		"schema_version": supportBundleSchemaVersion,
		"bundle_id":      id,
		"created_at":     createdAt.Format(time.RFC3339),
		"include_logs":   includeLogs,
		"entries":        entryMetadata,
		"redaction": map[string]any{
			"secrets": true, "absolute_paths": true, "network_addresses": true,
			"player_identifiers": true, "environment": "excluded", "save_files": "excluded", "database": "excluded",
		},
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return supportBundleMetadata{}, err
	}
	entries["manifest.json"] = append(manifestBytes, '\n')
	entryNames = append(entryNames, "manifest.json")
	sort.Strings(entryNames)

	zipPath := filepath.Join(dir, id+".zip")
	tempPath := zipPath + ".tmp"
	if err := writeSupportBundleArchive(tempPath, entries); err != nil {
		_ = os.Remove(tempPath)
		return supportBundleMetadata{}, err
	}
	info, err := os.Stat(tempPath)
	if err != nil {
		_ = os.Remove(tempPath)
		return supportBundleMetadata{}, err
	}
	if info.Size() > supportBundleMaxBytes {
		_ = os.Remove(tempPath)
		return supportBundleMetadata{}, fmt.Errorf("support bundle exceeds %d bytes", supportBundleMaxBytes)
	}
	digest, err := hashRegularFile(tempPath)
	if err != nil {
		_ = os.Remove(tempPath)
		return supportBundleMetadata{}, err
	}
	if err := os.Rename(tempPath, zipPath); err != nil {
		_ = os.Remove(tempPath)
		return supportBundleMetadata{}, fmt.Errorf("publish support bundle: %w", err)
	}
	if err := os.Chmod(zipPath, 0o600); err != nil {
		_ = os.Remove(zipPath)
		return supportBundleMetadata{}, err
	}
	metadata := supportBundleMetadata{
		ID: id, FileName: "palpanel-support-" + createdAt.Format("20060102T150405Z") + "-" + id[:8] + ".zip",
		CreatedAt: createdAt.Format(time.RFC3339), SizeBytes: info.Size(), SHA256: digest, IncludeLogs: includeLogs, Entries: entryNames,
	}
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		_ = os.Remove(zipPath)
		return supportBundleMetadata{}, err
	}
	if err := writeFileAtomic(filepath.Join(dir, id+".json"), append(metadataBytes, '\n'), 0o600); err != nil {
		_ = os.Remove(zipPath)
		return supportBundleMetadata{}, err
	}
	if err := s.cleanupSupportBundles(createdAt); err != nil {
		_ = os.Remove(zipPath)
		_ = os.Remove(filepath.Join(dir, id+".json"))
		return supportBundleMetadata{}, err
	}
	return metadata, nil
}

func (s Server) collectSupportBundleEntries(ctx context.Context, includeLogs bool) (map[string][]byte, error) {
	checks := s.collectSupportBundleChecks(ctx)
	status, statusErr := s.server.Status(ctx)
	prerequisites, prereqErr := s.server.Prerequisites(ctx)
	runtimeMode, runtimeErr := s.server.RuntimeMode(ctx)
	host := s.server.HostCapabilities(ctx)
	jobs, jobsErr := s.store.ListJobs(ctx, 50)
	audit, auditErr := s.store.ListAuditLogs(ctx, 100)
	incidents, incidentTotal, incidentsErr := s.store.ListIncidents(ctx, db.IncidentListFilter{Limit: 20})
	incidentSummary, incidentSummaryErr := s.store.IncidentSummary(ctx)
	schemaVersion, schemaErr := s.store.SchemaVersion(ctx)
	build := buildinfo.Current()

	entries := map[string][]byte{}
	values := map[string]any{
		"system/build.json":             map[string]any{"version": build.Version, "commit": build.Commit, "build_time": build.BuildTime, "patch_version": patchVersion},
		"system/runtime.json":           map[string]any{"goos": runtime.GOOS, "goarch": runtime.GOARCH, "runtime_mode": valueOrError(runtimeMode, runtimeErr), "schema_version": valueOrError(schemaVersion, schemaErr)},
		"diagnostics/checks.json":       checks,
		"server/status.json":            valueOrError(status, statusErr),
		"server/prerequisites.json":     valueOrError(prerequisites, prereqErr),
		"server/host-capabilities.json": host,
		"activity/jobs.json":            valueOrError(jobs, jobsErr),
		"activity/audit.json":           valueOrError(audit, auditErr),
		"incidents/recent.json":         map[string]any{"items": valueOrError(incidents, incidentsErr), "total": incidentTotal, "summary": valueOrError(incidentSummary, incidentSummaryErr)},
		"configuration/safe-summary.json": map[string]any{
			"require_auth":              s.cfg.RequireAuth,
			"diagnostic_shell_enabled":  s.cfg.DiagnosticShellEnabled,
			"save_indexer_enabled":      s.cfg.SaveIndexerEnabled,
			"community_servers_enabled": s.cfg.CommunityServersEnabled,
			"incident_webhook_enabled":  strings.TrimSpace(s.cfg.IncidentWebhookURL) != "",
			"runtime_root_configured":   strings.TrimSpace(s.cfg.RuntimeRoot) != "",
			"server_directory_imported": s.cfg.ServerDirectoryImported(),
			"log_level":                 s.cfg.LogLevel,
		},
	}
	for name, value := range values {
		data, err := marshalSanitizedJSON(value)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", name, err)
		}
		entries[name] = data
	}
	if includeLogs {
		for name, data := range collectSanitizedLogTails(s.cfg.LogsDir) {
			entries[name] = data
		}
	}
	return entries, nil
}

func (s Server) collectSupportBundleChecks(ctx context.Context) []supportBundleCheck {
	checks := []supportBundleCheck{
		checkDirectory("data-directory", s.cfg.DataDir, true, true),
		checkDirectory("server-directory", s.cfg.ServerDirectory(), true, false),
		checkDirectory("logs-directory", s.cfg.LogsDir, false, false),
		checkDirectory("backups-directory", s.cfg.BackupsDir, false, false),
	}
	if err := s.store.Ping(ctx); err != nil {
		checks = append(checks, supportBundleCheck{ID: "database", Required: true, Message: sanitizeSupportText(err.Error())})
	} else {
		checks = append(checks, supportBundleCheck{ID: "database", OK: true, Required: true, Message: "database is reachable"})
	}
	if _, err := s.server.Status(ctx); err != nil {
		checks = append(checks, supportBundleCheck{ID: "server-status", Required: false, Message: sanitizeSupportText(err.Error())})
	} else {
		checks = append(checks, supportBundleCheck{ID: "server-status", OK: true, Required: false, Message: "server status is readable"})
	}
	return checks
}

func checkDirectory(id, path string, required, writable bool) supportBundleCheck {
	check := supportBundleCheck{ID: id, Required: required}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		check.Message = "directory is unavailable"
		return check
	}
	if writable {
		probe, err := os.CreateTemp(path, ".support-probe-*")
		if err != nil {
			check.Message = "directory is not writable"
			return check
		}
		name := probe.Name()
		_ = probe.Close()
		_ = os.Remove(name)
	}
	check.OK = true
	check.Message = "directory is available"
	return check
}

func checksReady(checks []supportBundleCheck) bool {
	for _, check := range checks {
		if check.Required && !check.OK {
			return false
		}
	}
	return true
}

func valueOrError(value any, err error) any {
	if err == nil {
		return value
	}
	return map[string]any{"available": false, "error": sanitizeSupportText(err.Error())}
}

func marshalSanitizedJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	sanitized := sanitizeSupportValue(decoded, "")
	out, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func sanitizeSupportValue(value any, key string) any {
	if sensitiveKeyPattern.MatchString(key) {
		return "<redacted>"
	}
	if pathKeyPattern.MatchString(key) {
		return "<path>"
	}
	if identityKeyPattern.MatchString(key) {
		return "<redacted-id>"
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			out[childKey] = sanitizeSupportValue(childValue, childKey)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, child := range typed {
			out[index] = sanitizeSupportValue(child, key)
		}
		return out
	case string:
		return sanitizeSupportText(typed)
	default:
		return value
	}
}

func sanitizeSupportText(value string) string {
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "�")
	}
	value = bearerPattern.ReplaceAllString(value, "Bearer <redacted>")
	value = assignmentSecretPattern.ReplaceAllString(value, "$1=<redacted>")
	value = windowsPathPattern.ReplaceAllString(value, "<path>")
	value = unixPathPattern.ReplaceAllStringFunc(value, func(match string) string {
		prefix := ""
		if len(match) > 0 && match[0] != '/' {
			prefix = match[:1]
		}
		return prefix + "<path>"
	})
	value = ipv4Pattern.ReplaceAllString(value, "<ip>")
	value = guidPattern.ReplaceAllString(value, "<guid>")
	value = longHexPattern.ReplaceAllString(value, "<hex-id>")
	value = longNumericPattern.ReplaceAllString(value, "<numeric-id>")
	return value
}

func collectSanitizedLogTails(logsDir string) map[string][]byte {
	result := map[string][]byte{}
	directoryInfo, err := os.Lstat(logsDir)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 {
		return result
	}
	items, err := os.ReadDir(logsDir)
	if err != nil {
		return result
	}
	type candidate struct {
		name, path string
		modified   time.Time
		size       int64
	}
	candidates := make([]candidate, 0)
	for _, item := range items {
		if item.Type()&os.ModeSymlink != 0 || !item.Type().IsRegular() {
			continue
		}
		name := item.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".log" && ext != ".txt" {
			continue
		}
		info, err := item.Info()
		if err != nil || info.Size() <= 0 {
			continue
		}
		candidates = append(candidates, candidate{name: name, path: filepath.Join(logsDir, name), modified: info.ModTime(), size: info.Size()})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].modified.After(candidates[j].modified) })
	total := 0
	for _, item := range candidates {
		if len(result) >= supportBundleMaxLogFiles || total >= supportBundleMaxLogsTotal {
			break
		}
		limit := min(supportBundleMaxLogBytes, supportBundleMaxLogsTotal-total)
		data, err := readFileTail(item.path, limit)
		if err != nil {
			continue
		}
		sanitized := []byte(sanitizeSupportText(string(data)))
		entryName := fmt.Sprintf("logs/recent-%d.tail.txt", len(result)+1)
		result[entryName] = sanitized
		total += len(sanitized)
	}
	return result
}

func readFileTail(path string, limit int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("log is not a regular file")
	}
	if info.Size() > int64(limit) {
		if _, err := file.Seek(-int64(limit), io.SeekEnd); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(io.LimitReader(file, int64(limit)))
}

func writeSupportBundleArchive(path string, entries map[string][]byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(file)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.HasPrefix(name, "/") || strings.Contains(name, "..") || strings.ContainsRune(name, '\x00') {
			_ = archive.Close()
			_ = file.Close()
			return fmt.Errorf("unsafe support bundle entry %q", name)
		}
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o600)
		header.Modified = time.Unix(0, 0).UTC()
		writer, err := archive.CreateHeader(header)
		if err != nil {
			_ = archive.Close()
			_ = file.Close()
			return err
		}
		if _, err := writer.Write(entries[name]); err != nil {
			_ = archive.Close()
			_ = file.Close()
			return err
		}
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (s Server) supportBundleDir() string { return filepath.Join(s.cfg.DataDir, "support-bundles") }

func supportBundleDownloadName(metadata supportBundleMetadata) string {
	createdAt, err := time.Parse(time.RFC3339, metadata.CreatedAt)
	if err != nil {
		return "palpanel-support-" + metadata.ID[:8] + ".zip"
	}
	return "palpanel-support-" + createdAt.UTC().Format("20060102T150405Z") + "-" + metadata.ID[:8] + ".zip"
}

func randomSupportBundleID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func rejectSupportBundleSymlinks(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("support bundle directory must be a real directory")
	}
	return os.Chmod(dir, 0o700)
}

func hashRegularFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("support bundle is not a regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(temp)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func (s Server) readSupportBundleMetadata() ([]supportBundleMetadata, error) {
	dir := s.supportBundleDir()
	items, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []supportBundleMetadata{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]supportBundleMetadata, 0)
	for _, item := range items {
		if item.Type()&os.ModeSymlink != 0 || !item.Type().IsRegular() || filepath.Ext(item.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(item.Name(), ".json")
		if !supportBundleIDPattern.MatchString(id) {
			continue
		}
		metadata, err := s.readSupportBundleMetadataFile(id)
		if err == nil {
			result = append(result, metadata)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result, nil
}

func (s Server) readSupportBundleMetadataFile(id string) (supportBundleMetadata, error) {
	path := filepath.Join(s.supportBundleDir(), id+".json")
	info, err := os.Lstat(path)
	if err != nil {
		return supportBundleMetadata{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 64*1024 {
		return supportBundleMetadata{}, errors.New("invalid support bundle metadata")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return supportBundleMetadata{}, err
	}
	var metadata supportBundleMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return supportBundleMetadata{}, err
	}
	if metadata.ID != id || !supportBundleIDPattern.MatchString(metadata.ID) {
		return supportBundleMetadata{}, errors.New("support bundle metadata id mismatch")
	}
	return metadata, nil
}

func (s Server) cleanupSupportBundles(now time.Time) error {
	items, err := s.readSupportBundleMetadata()
	if err != nil {
		return err
	}
	for index, item := range items {
		createdAt, _ := time.Parse(time.RFC3339, item.CreatedAt)
		if index < supportBundleRetentionCount && (createdAt.IsZero() || now.Sub(createdAt) <= supportBundleRetentionAge) {
			continue
		}
		for _, suffix := range []string{".zip", ".json"} {
			_ = os.Remove(filepath.Join(s.supportBundleDir(), item.ID+suffix))
		}
	}
	return nil
}
