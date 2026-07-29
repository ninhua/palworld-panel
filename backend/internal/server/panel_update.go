package server

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"palpanel/internal/db"
	"palpanel/internal/jobs"
	"palpanel/internal/networkproxy"
	"palpanel/internal/panelupdater"
)

const (
	panelUpdateMaxMetadataBytes = 8 << 20
	panelUpdateMaxArchiveBytes  = 512 << 20
	panelUpdateRepository       = "ninhua/palworld-panel"

	panelUpdateModeAuto     = "auto"
	panelUpdateModeExternal = "external"
	panelUpdateModeExec     = "exec"

	panelExecManifestName     = "panel-update.json"
	panelExecSuccessThreshold = 3
)

var (
	panelRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	panelReleasePattern    = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)-custom\.(\d+)\.(\d+)\.(\d+)(?:[-+][A-Za-z0-9.-]+)?$`)
	panelGitHubAPIBaseURL  = "https://api.github.com"
	panelGitHubWebBaseURL  = "https://github.com"
)

type PanelUpdateRequest struct {
	CurrentVersion string
	Repository     string
}

type PanelUpdateStatus struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	ReleaseTag      string `json:"release_tag"`
	ReleaseURL      string `json:"release_url"`
	UpdateAvailable bool   `json:"update_available"`
	UpdateMode      string `json:"update_mode"`
	UpdateModeNote  string `json:"update_mode_note"`
	CheckedAt       string `json:"checked_at"`
	Message         string `json:"message"`
}

type panelGitHubRelease struct {
	TagName    string             `json:"tag_name"`
	HTMLURL    string             `json:"html_url"`
	Draft      bool               `json:"draft"`
	Prerelease bool               `json:"prerelease"`
	Assets     []panelGitHubAsset `json:"assets"`
}

type panelGitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type panelReleaseSelection struct {
	Release   panelGitHubRelease
	Archive   panelGitHubAsset
	Checksums panelGitHubAsset
}

type panelRestartMarker struct {
	SchemaVersion  int    `json:"schema_version,omitempty"`
	JobID          string `json:"job_id"`
	BinaryPath     string `json:"binary_path"`
	BackupPath     string `json:"backup_path"`
	PreviousSHA256 string `json:"previous_sha256,omitempty"`
	ExpectedSHA256 string `json:"expected_sha256"`
	CurrentVersion string `json:"current_version,omitempty"`
	TargetVersion  string `json:"target_version,omitempty"`
	HealthPort     int    `json:"health_port,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type panelExecPackageManifest struct {
	SchemaVersion int    `json:"schema_version"`
	Version       string `json:"version"`
	ExecHotUpdate struct {
		Supported        bool     `json:"supported"`
		RequiredFiles    []string `json:"required_files"`
		HealthPaths      []string `json:"health_paths"`
		SuccessThreshold int      `json:"success_threshold"`
	} `json:"exec_hot_update"`
}

func normalizePanelUpdateRequest(request PanelUpdateRequest) (PanelUpdateRequest, error) {
	request.CurrentVersion = strings.TrimSpace(request.CurrentVersion)
	request.Repository = strings.TrimSpace(request.Repository)
	if request.Repository == "" {
		request.Repository = panelUpdateRepository
	}
	if !panelReleasePattern.MatchString(request.CurrentVersion) {
		return request, fmt.Errorf("invalid current panel version: %s", request.CurrentVersion)
	}
	if !panelRepositoryPattern.MatchString(request.Repository) {
		return request, fmt.Errorf("invalid panel repository: %s", request.Repository)
	}
	parts := strings.Split(request.Repository, "/")
	if parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return request, fmt.Errorf("invalid panel repository: %s", request.Repository)
	}
	return request, nil
}

func (m Manager) PanelUpdateStatus(ctx context.Context, request PanelUpdateRequest) (PanelUpdateStatus, error) {
	request, err := normalizePanelUpdateRequest(request)
	if err != nil {
		return PanelUpdateStatus{}, err
	}
	selection, err := m.resolvePanelRelease(ctx, request)
	if err != nil {
		return PanelUpdateStatus{}, err
	}
	available := comparePanelVersions(selection.Release.TagName, request.CurrentVersion) > 0
	message := "当前面板已是最新版本 " + request.CurrentVersion
	if available {
		message = "发现面板新版本 " + selection.Release.TagName
	}
	mode, modeErr := resolvePanelUpdateMode(m.cfg)
	modeNote := ""
	if modeErr != nil {
		mode = "unavailable"
		modeNote = modeErr.Error()
	} else if mode == panelUpdateModeExternal {
		modeNote = "完整 Release 由外部 root 更新器切换并执行健康回滚"
	} else {
		modeNote = "主进程通过 syscall.Exec 原地热更新，PID 保持不变并执行启动健康回滚"
	}
	return PanelUpdateStatus{
		CurrentVersion: request.CurrentVersion, LatestVersion: selection.Release.TagName,
		ReleaseTag: selection.Release.TagName, ReleaseURL: selection.Release.HTMLURL,
		UpdateAvailable: available, UpdateMode: mode, UpdateModeNote: modeNote,
		CheckedAt: time.Now().UTC().Format(time.RFC3339Nano), Message: message,
	}, nil
}

func (m Manager) CheckPanelUpdate(ctx context.Context, request PanelUpdateRequest) (db.Job, error) {
	return m.jobs.Submit(ctx, jobs.ClassGeneral, "panel_update_check", "queued panel update check", func(jobCtx context.Context, jobID string) {
		_ = m.jobs.Update(jobID, "running", 20, "checking PalPanel releases", "")
		status, err := m.PanelUpdateStatus(jobCtx, request)
		if err != nil {
			_ = m.jobs.UpdateWithCode(jobID, "failed", 100, "panel update check failed", err.Error(), "panel_update_check_failed")
			return
		}
		_ = m.jobs.Update(jobID, "completed", 100, status.Message, "")
	})
}

func (m Manager) ApplyPanelUpdate(ctx context.Context, request PanelUpdateRequest) (db.Job, error) {
	request, err := normalizePanelUpdateRequest(request)
	if err != nil {
		return db.Job{}, err
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return db.Job{}, fmt.Errorf("panel self-update currently requires linux-amd64")
	}
	return m.jobs.Submit(ctx, jobs.ClassLifecycle, "panel_update", "queued panel update", func(jobCtx context.Context, jobID string) {
		m.runPanelUpdate(jobCtx, jobID, request)
	})
}

func (m Manager) runPanelUpdate(ctx context.Context, jobID string, request PanelUpdateRequest) {
	fail := func(progress int, code, message string, err error) {
		detail := message
		if err != nil {
			detail = err.Error()
		}
		_ = m.jobs.UpdateWithCode(jobID, "failed", progress, message, detail, code)
	}
	mode, err := resolvePanelUpdateMode(m.cfg)
	if err != nil {
		fail(5, "panel_update_mode_unavailable", "no usable panel update mode is available", err)
		return
	}
	modeLabel := "exec hot update"
	if mode == panelUpdateModeExternal {
		modeLabel = "external full-package update"
	}
	_ = m.jobs.Update(jobID, "running", 5, "checking PalPanel releases for "+modeLabel, "")
	selection, err := m.resolvePanelRelease(ctx, request)
	if err != nil {
		fail(15, "panel_release_lookup_failed", "panel release lookup failed", err)
		return
	}
	if comparePanelVersions(selection.Release.TagName, request.CurrentVersion) <= 0 {
		_ = m.jobs.Update(jobID, "completed", 100, "current panel is already up to date: "+request.CurrentVersion, "")
		return
	}

	operationDir := panelupdater.OperationDir(m.cfg.DataDir, jobID)
	if err := os.RemoveAll(operationDir); err != nil {
		fail(20, "panel_update_stage_cleanup_failed", "cannot clean panel update staging directory", err)
		return
	}
	if err := os.MkdirAll(operationDir, 0o700); err != nil {
		fail(20, "panel_update_stage_failed", "cannot create panel update staging directory", err)
		return
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(operationDir)
		}
	}()
	checksumsPath := filepath.Join(operationDir, "SHA256SUMS")
	archivePath := filepath.Join(operationDir, selection.Archive.Name)
	_ = m.jobs.Update(jobID, "running", 25, "downloading release checksums", "")
	if err := m.downloadPanelAsset(ctx, selection.Checksums.BrowserDownloadURL, checksumsPath, panelUpdateMaxMetadataBytes); err != nil {
		fail(30, "panel_checksums_download_failed", "cannot download release checksums", err)
		return
	}
	checksums, err := parsePanelChecksums(checksumsPath)
	if err != nil {
		fail(35, "panel_checksums_invalid", "release checksums are invalid", err)
		return
	}
	_ = m.jobs.Update(jobID, "running", 45, "downloading verified PalPanel package", "")
	if err := m.downloadPanelAsset(ctx, selection.Archive.BrowserDownloadURL, archivePath, panelUpdateMaxArchiveBytes); err != nil {
		fail(50, "panel_archive_download_failed", "cannot download panel package", err)
		return
	}
	if err := verifyNamedPanelFile(archivePath, selection.Archive.Name, checksums); err != nil {
		fail(55, "panel_archive_checksum_failed", "panel package checksum mismatch", err)
		return
	}
	archiveSHA, err := sha256PanelFile(archivePath)
	if err != nil {
		fail(60, "panel_archive_hash_failed", "cannot hash panel package", err)
		return
	}
	candidate := filepath.Join(operationDir, "candidate-palpanel")
	if mode == panelUpdateModeExec {
		if err := extractPanelExecCandidate(archivePath, candidate, selection.Release.TagName); err != nil {
			fail(60, "panel_exec_package_incompatible", "release package does not permit safe exec hot update", err)
			return
		}
	} else if err := extractPanelBinary(archivePath, candidate); err != nil {
		fail(60, "panel_archive_invalid", "cannot extract panel binary", err)
		return
	}
	if err := os.Chmod(candidate, 0o755); err != nil {
		fail(65, "panel_binary_mode_failed", "cannot mark panel binary executable", err)
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	output, checkErr := exec.CommandContext(checkCtx, candidate, "--version").CombinedOutput()
	cancel()
	if checkErr != nil || !strings.Contains(string(output), selection.Release.TagName) {
		if checkErr == nil {
			checkErr = fmt.Errorf("candidate version output does not contain %s: %s", selection.Release.TagName, strings.TrimSpace(string(output)))
		}
		fail(70, "panel_binary_probe_failed", "panel binary probe failed", checkErr)
		return
	}
	healthPort, err := panelupdater.ParseListenPort(m.cfg.ListenAddr)
	if err != nil {
		fail(75, "panel_health_port_invalid", "cannot determine panel health port", err)
		return
	}
	if mode == panelUpdateModeExec {
		_ = m.jobs.Update(jobID, "running", 85, "verified package permits PID-preserving exec hot update", "")
		if err := activatePanelExecUpdate(jobID, request.CurrentVersion, selection.Release.TagName, healthPort, candidate); err != nil {
			fail(95, "panel_exec_update_failed", "cannot activate exec hot update", err)
			return
		}
		return
	}

	proxyURL, err := networkproxy.New(m.cfg).InstallProxyURL()
	if err != nil {
		fail(75, "panel_update_proxy_invalid", "cannot read panel update proxy", err)
		return
	}
	updateRequest := panelupdater.Request{
		SchemaVersion:  panelupdater.SchemaVersion,
		JobID:          jobID,
		ArchivePath:    archivePath,
		ArchiveSHA256:  archiveSHA,
		CurrentVersion: request.CurrentVersion,
		TargetVersion:  selection.Release.TagName,
		HealthPort:     healthPort,
		ProxyURL:       proxyURL,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	_ = m.jobs.Update(jobID, "running", 90, "handing verified package to external updater", "")
	if err := panelupdater.WriteRequest(m.cfg.DataDir, updateRequest); err != nil {
		fail(90, "panel_update_handoff_failed", "cannot hand off package to external updater", err)
		return
	}
	cleanup = false
	startPanelUpdateResultWatcher(m.cfg, m.store)
	_ = m.jobs.Update(jobID, "running", 95, "PalPanel "+selection.Release.TagName+" package verified; external updater is switching the full release", "")
}

func activatePanelExecUpdate(jobID, currentVersion, targetVersion string, healthPort int, candidate string) error {
	executable, err := executablePath()
	if err != nil {
		return err
	}
	previousSHA, err := sha256PanelFile(executable)
	if err != nil {
		return fmt.Errorf("hash current panel binary: %w", err)
	}
	expectedSHA, err := sha256PanelFile(candidate)
	if err != nil {
		return fmt.Errorf("hash candidate panel binary: %w", err)
	}
	backupDir := filepath.Join(filepath.Dir(executable), ".palpanel-update-backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return fmt.Errorf("create panel update backup directory: %w", err)
	}
	backup := filepath.Join(backupDir, fmt.Sprintf("palpanel-%s-%s", currentVersion, time.Now().UTC().Format("20060102T150405.000000000Z")))
	if err := copyPanelFile(executable, backup, 0o755); err != nil {
		return fmt.Errorf("back up current panel binary: %w", err)
	}
	replacement := filepath.Join(filepath.Dir(executable), ".palpanel-update-replacement")
	_ = os.Remove(replacement)
	if err := copyPanelFile(candidate, replacement, 0o755); err != nil {
		return fmt.Errorf("prepare panel replacement: %w", err)
	}
	marker := panelRestartMarker{
		SchemaVersion:  2,
		JobID:          jobID,
		BinaryPath:     executable,
		BackupPath:     backup,
		PreviousSHA256: previousSHA,
		ExpectedSHA256: expectedSHA,
		CurrentVersion: currentVersion,
		TargetVersion:  targetVersion,
		HealthPort:     healthPort,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := writePanelRestartMarker(marker); err != nil {
		_ = os.Remove(replacement)
		return fmt.Errorf("write panel restart marker: %w", err)
	}
	if err := os.Rename(replacement, executable); err != nil {
		_ = removePanelRestartMarker(executable)
		return fmt.Errorf("activate panel replacement: %w", err)
	}
	activeSHA, err := sha256PanelFile(executable)
	if err != nil || !strings.EqualFold(activeSHA, expectedSHA) {
		_ = restorePanelBackup(executable, backup)
		_ = removePanelRestartMarker(executable)
		if err != nil {
			return fmt.Errorf("verify activated panel binary: %w", err)
		}
		return fmt.Errorf("activated panel sha256 = %s, expected %s", activeSHA, expectedSHA)
	}
	_ = os.RemoveAll(filepath.Dir(candidate))
	if err := replaceCurrentPanelProcess(executable); err != nil {
		_ = restorePanelBackup(executable, backup)
		_ = removePanelRestartMarker(executable)
		return fmt.Errorf("exec updated panel binary: %w", err)
	}
	return nil
}

func (m Manager) resolvePanelRelease(ctx context.Context, request PanelUpdateRequest) (panelReleaseSelection, error) {
	endpoint := strings.TrimRight(panelGitHubAPIBaseURL, "/") + "/repos/" + request.Repository + "/releases?per_page=100"
	var releases []panelGitHubRelease
	if err := m.getPanelJSON(ctx, endpoint, &releases); err != nil {
		fallback, fallbackErr := m.resolvePanelReleaseFromLatest(ctx, request)
		if fallbackErr == nil {
			return fallback, nil
		}
		return panelReleaseSelection{}, fmt.Errorf("%w; latest-release fallback failed: %v; GitHub is unreachable, configure 系统设置 → 网络代理 → 安装与下载代理 or set HTTPS_PROXY", err, fallbackErr)
	}
	var best panelReleaseSelection
	for _, release := range releases {
		if release.Draft || release.Prerelease || !panelReleasePattern.MatchString(release.TagName) {
			continue
		}
		archiveName := "palpanel_" + release.TagName + "_linux_amd64.tar.gz"
		candidate := panelReleaseSelection{Release: release}
		for _, asset := range release.Assets {
			switch asset.Name {
			case archiveName:
				candidate.Archive = asset
			case "SHA256SUMS":
				candidate.Checksums = asset
			}
		}
		if candidate.Archive.BrowserDownloadURL == "" || candidate.Checksums.BrowserDownloadURL == "" {
			continue
		}
		if best.Release.TagName == "" || comparePanelVersions(release.TagName, best.Release.TagName) > 0 {
			best = candidate
		}
	}
	if best.Release.TagName == "" {
		return panelReleaseSelection{}, fmt.Errorf("no stable PalPanel release with linux-amd64 package found")
	}
	return best, nil
}

func (m Manager) resolvePanelReleaseFromLatest(ctx context.Context, request PanelUpdateRequest) (panelReleaseSelection, error) {
	client, err := m.panelHTTPClient(2 * time.Minute)
	if err != nil {
		return panelReleaseSelection{}, err
	}
	endpoint := strings.TrimRight(panelGitHubWebBaseURL, "/") + "/" + request.Repository + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return panelReleaseSelection{}, err
	}
	setPanelRequestHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return panelReleaseSelection{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return panelReleaseSelection{}, fmt.Errorf("github latest release returned %s", resp.Status)
	}
	return panelReleaseFromLatestURL(request.Repository, resp.Request.URL)
}

func panelReleaseFromLatestURL(repository string, finalURL *url.URL) (panelReleaseSelection, error) {
	if finalURL == nil {
		return panelReleaseSelection{}, fmt.Errorf("github latest release did not return a final URL")
	}
	const marker = "/releases/tag/"
	position := strings.LastIndex(finalURL.Path, marker)
	if position < 0 {
		return panelReleaseSelection{}, fmt.Errorf("github latest release did not redirect to a tag")
	}
	tag, err := url.PathUnescape(strings.TrimSpace(finalURL.Path[position+len(marker):]))
	if err != nil || !panelReleasePattern.MatchString(tag) {
		return panelReleaseSelection{}, fmt.Errorf("github latest release returned invalid tag %q", tag)
	}
	webBase := strings.TrimRight(panelGitHubWebBaseURL, "/")
	downloadBase := webBase + "/" + repository + "/releases/download/" + url.PathEscape(tag) + "/"
	archiveName := "palpanel_" + tag + "_linux_amd64.tar.gz"
	return panelReleaseSelection{
		Release: panelGitHubRelease{
			TagName: tag,
			HTMLURL: webBase + "/" + repository + "/releases/tag/" + url.PathEscape(tag),
		},
		Archive:   panelGitHubAsset{Name: archiveName, BrowserDownloadURL: downloadBase + archiveName},
		Checksums: panelGitHubAsset{Name: "SHA256SUMS", BrowserDownloadURL: downloadBase + "SHA256SUMS"},
	}, nil
}

func (m Manager) getPanelJSON(ctx context.Context, endpoint string, destination any) error {
	client, err := m.panelHTTPClient(2 * time.Minute)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	setPanelRequestHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("github release request returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, panelUpdateMaxMetadataBytes)).Decode(destination)
}

func (m Manager) downloadPanelAsset(ctx context.Context, endpoint, destination string, maxBytes int64) error {
	client, err := m.panelHTTPClient(10 * time.Minute)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	setPanelRequestHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download returned %s", resp.Status)
	}
	if resp.ContentLength > maxBytes {
		return fmt.Errorf("download exceeds size limit")
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maxBytes {
		return fmt.Errorf("download exceeds size limit")
	}
	return nil
}

func (m Manager) panelHTTPClient(timeout time.Duration) (*http.Client, error) {
	base := m.downloadClient
	if base == nil {
		base = &http.Client{}
	}
	if strings.TrimSpace(m.cfg.DataDir) == "" {
		client := *base
		client.Timeout = timeout
		return &client, nil
	}
	proxyURL, err := networkproxy.New(m.cfg).InstallProxyURL()
	if err != nil {
		return nil, fmt.Errorf("cannot read install proxy for panel update: %w", err)
	}
	if proxyURL == "" {
		client := *base
		client.Timeout = timeout
		return &client, nil
	}
	client, err := networkproxy.HTTPClient(base, proxyURL, timeout)
	if err != nil {
		return nil, fmt.Errorf("cannot configure install proxy for panel update: %w", err)
	}
	return client, nil
}

func setPanelRequestHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PalPanel-self-update")
	if token := strings.TrimSpace(firstPanelValue(os.Getenv("PALPANEL_GITHUB_TOKEN"), os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN"))); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func firstPanelValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func comparePanelVersions(left, right string) int {
	parse := func(value string) []int {
		match := panelReleasePattern.FindStringSubmatch(value)
		if len(match) != 7 {
			return nil
		}
		out := make([]int, 6)
		for index := range out {
			out[index], _ = strconv.Atoi(match[index+1])
		}
		return out
	}
	leftParts, rightParts := parse(left), parse(right)
	if leftParts == nil || rightParts == nil {
		return strings.Compare(left, right)
	}
	for index := range leftParts {
		if leftParts[index] > rightParts[index] {
			return 1
		}
		if leftParts[index] < rightParts[index] {
			return -1
		}
	}
	return 0
}

func parsePanelChecksums(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 || len(parts[0]) != 64 {
			return nil, fmt.Errorf("invalid SHA256SUMS line: %s", raw)
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			return nil, fmt.Errorf("invalid SHA256SUMS digest: %s", parts[0])
		}
		name := strings.TrimPrefix(strings.TrimPrefix(parts[1], "*"), "./")
		clean := filepath.ToSlash(filepath.Clean(name))
		if clean == "." || strings.HasPrefix(clean, "../") || filepath.IsAbs(name) || strings.Contains(clean, "/") {
			return nil, fmt.Errorf("unsafe SHA256SUMS path: %s", name)
		}
		out[clean] = strings.ToLower(parts[0])
	}
	return out, nil
}

func verifyNamedPanelFile(path, name string, checksums map[string]string) error {
	expected, ok := checksums[name]
	if !ok {
		return fmt.Errorf("SHA256SUMS has no entry for %s", name)
	}
	actual, err := sha256PanelFile(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("%s sha256 = %s, expected %s", name, actual, expected)
	}
	return nil
}

func extractPanelBinary(archivePath, destination string) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader, found := tar.NewReader(gzipReader), 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Clean(header.Name))
		if filepath.IsAbs(header.Name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe archive path: %s", header.Name)
		}
		if name != "bin/palpanel" && !strings.HasSuffix(name, "/bin/palpanel") {
			continue
		}
		if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size <= 0 || header.Size > panelUpdateMaxArchiveBytes || found > 0 {
			return fmt.Errorf("invalid panel binary entry")
		}
		file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(file, reader, header.Size)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		found++
	}
	if found != 1 {
		return fmt.Errorf("archive does not contain exactly one bin/palpanel")
	}
	return nil
}

func extractPanelExecCandidate(archivePath, destination, targetVersion string) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	reader := tar.NewReader(gzipReader)
	var manifestBody, checksumsBody []byte
	binaryFound := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Clean(header.Name))
		if filepath.IsAbs(header.Name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe archive path: %s", header.Name)
		}
		regular := header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA
		switch {
		case name == "bin/palpanel" || strings.HasSuffix(name, "/bin/palpanel"):
			if !regular || header.Size <= 0 || header.Size > panelUpdateMaxArchiveBytes || binaryFound > 0 {
				return fmt.Errorf("invalid panel binary entry")
			}
			file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			binaryFound++
		case name == "checksums.txt" || strings.HasSuffix(name, "/checksums.txt"):
			if !regular || header.Size <= 0 || header.Size > panelUpdateMaxMetadataBytes || checksumsBody != nil {
				return fmt.Errorf("invalid package checksums entry")
			}
			checksumsBody, err = io.ReadAll(io.LimitReader(reader, panelUpdateMaxMetadataBytes+1))
			if err != nil {
				return fmt.Errorf("read package checksums: %w", err)
			}
			if int64(len(checksumsBody)) != header.Size {
				return fmt.Errorf("package checksums length mismatch")
			}
		case name == panelExecManifestName || strings.HasSuffix(name, "/"+panelExecManifestName):
			if !regular || header.Size <= 0 || header.Size > panelUpdateMaxMetadataBytes || manifestBody != nil {
				return fmt.Errorf("invalid exec update manifest entry")
			}
			manifestBody, err = io.ReadAll(io.LimitReader(reader, panelUpdateMaxMetadataBytes+1))
			if err != nil {
				return fmt.Errorf("read exec update manifest: %w", err)
			}
			if int64(len(manifestBody)) != header.Size {
				return fmt.Errorf("exec update manifest length mismatch")
			}
		}
	}
	if binaryFound != 1 {
		return fmt.Errorf("archive does not contain exactly one bin/palpanel")
	}
	if len(checksumsBody) == 0 {
		return fmt.Errorf("release package has no checksums.txt")
	}
	if len(manifestBody) == 0 {
		return fmt.Errorf("release package has no %s", panelExecManifestName)
	}
	checksums, err := parsePanelPackageChecksums(checksumsBody)
	if err != nil {
		return err
	}
	expectedSHA, ok := checksums["bin/palpanel"]
	if !ok {
		return fmt.Errorf("package checksums have no bin/palpanel entry")
	}
	actualSHA, err := sha256PanelFile(destination)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actualSHA, expectedSHA) {
		return fmt.Errorf("package panel sha256 = %s, expected %s", actualSHA, expectedSHA)
	}
	var manifest panelExecPackageManifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		return fmt.Errorf("decode %s: %w", panelExecManifestName, err)
	}
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported exec update manifest schema: %d", manifest.SchemaVersion)
	}
	if manifest.Version != targetVersion {
		return fmt.Errorf("exec update manifest version = %s, expected %s", manifest.Version, targetVersion)
	}
	if !manifest.ExecHotUpdate.Supported {
		return fmt.Errorf("release requires full-package update")
	}
	if len(manifest.ExecHotUpdate.RequiredFiles) != 1 || filepath.ToSlash(filepath.Clean(manifest.ExecHotUpdate.RequiredFiles[0])) != "bin/palpanel" {
		return fmt.Errorf("release requires files beyond bin/palpanel; use external full-package update")
	}
	if len(manifest.ExecHotUpdate.HealthPaths) != 2 || manifest.ExecHotUpdate.HealthPaths[0] != "/api/ready" || manifest.ExecHotUpdate.HealthPaths[1] != "/api/patch/info" {
		return fmt.Errorf("release declares unsupported exec health probes")
	}
	if manifest.ExecHotUpdate.SuccessThreshold != panelExecSuccessThreshold {
		return fmt.Errorf("release exec success threshold = %d, expected %d", manifest.ExecHotUpdate.SuccessThreshold, panelExecSuccessThreshold)
	}
	return nil
}

func parsePanelPackageChecksums(body []byte) (map[string]string, error) {
	out := map[string]string{}
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 || len(parts[0]) != 64 {
			return nil, fmt.Errorf("invalid package checksum line: %s", raw)
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			return nil, fmt.Errorf("invalid package checksum digest: %s", parts[0])
		}
		name := strings.TrimPrefix(strings.TrimPrefix(parts[1], "*"), "./")
		clean := filepath.ToSlash(filepath.Clean(name))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(name) {
			return nil, fmt.Errorf("unsafe package checksum path: %s", name)
		}
		out[clean] = strings.ToLower(parts[0])
	}
	return out, nil
}

func executablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
		path = resolved
	}
	return filepath.Clean(path), nil
}

func executableDirectory() string {
	path, err := executablePath()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Dir(path)
}

func sha256PanelFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func copyPanelFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary := destination + ".tmp"
	_ = os.Remove(temporary)
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(temporary)
		if copyErr != nil {
			return copyErr
		}
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
	if err := os.Chmod(temporary, mode); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Rename(temporary, destination)
}

func panelRestartMarkerPath(binary string) string {
	return filepath.Join(filepath.Dir(binary), ".palpanel-update-state.json")
}

func writePanelRestartMarker(marker panelRestartMarker) error {
	body, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	path := panelRestartMarkerPath(marker.BinaryPath)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(body, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func removePanelRestartMarker(binary string) error {
	err := os.Remove(panelRestartMarkerPath(binary))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func restorePanelBackup(binary, backup string) error {
	if backup == "" {
		return fmt.Errorf("panel backup path is empty")
	}
	return copyPanelFile(backup, binary, 0o755)
}
