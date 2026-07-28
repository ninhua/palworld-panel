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
)

const (
	panelUpdateMaxMetadataBytes = 8 << 20
	panelUpdateMaxArchiveBytes  = 512 << 20
	panelUpdateRepository       = "ninhua/palworld-panel"
)

var (
	panelRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	panelReleasePattern    = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)-custom\.(\d+)\.(\d+)\.(\d+)(?:[-+][A-Za-z0-9.-]+)?$`)
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
	JobID          string `json:"job_id"`
	BinaryPath     string `json:"binary_path"`
	BackupPath     string `json:"backup_path"`
	ExpectedSHA256 string `json:"expected_sha256"`
	CreatedAt      string `json:"created_at"`
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
	return PanelUpdateStatus{
		CurrentVersion: request.CurrentVersion, LatestVersion: selection.Release.TagName,
		ReleaseTag: selection.Release.TagName, ReleaseURL: selection.Release.HTMLURL,
		UpdateAvailable: available, CheckedAt: time.Now().UTC().Format(time.RFC3339Nano), Message: message,
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
	_ = m.jobs.Update(jobID, "running", 5, "checking PalPanel releases", "")
	selection, err := m.resolvePanelRelease(ctx, request)
	if err != nil {
		fail(15, "panel_release_lookup_failed", "panel release lookup failed", err)
		return
	}
	if comparePanelVersions(selection.Release.TagName, request.CurrentVersion) <= 0 {
		_ = m.jobs.Update(jobID, "completed", 100, "current panel is already up to date: "+request.CurrentVersion, "")
		return
	}

	stage, err := os.MkdirTemp(executableDirectory(), ".palpanel-update-")
	if err != nil {
		fail(20, "panel_update_stage_failed", "cannot create panel update staging directory", err)
		return
	}
	defer os.RemoveAll(stage)
	checksumsPath := filepath.Join(stage, "SHA256SUMS")
	archivePath := filepath.Join(stage, selection.Archive.Name)
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
	candidate := filepath.Join(stage, "palpanel")
	if err := extractPanelBinary(archivePath, candidate); err != nil {
		fail(60, "panel_archive_invalid", "cannot extract panel binary", err)
		return
	}
	candidateSHA, err := sha256PanelFile(candidate)
	if err != nil {
		fail(65, "panel_binary_checksum_failed", "cannot hash panel binary", err)
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

	executable, err := executablePath()
	if err != nil {
		fail(75, "panel_executable_path_failed", "cannot resolve current panel binary", err)
		return
	}
	backupDir := filepath.Join(filepath.Dir(executable), ".palpanel-update-backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		fail(75, "panel_backup_directory_failed", "cannot create panel backup directory", err)
		return
	}
	backup := filepath.Join(backupDir, fmt.Sprintf("palpanel-%s-%s", request.CurrentVersion, time.Now().UTC().Format("20060102T150405.000000000Z")))
	if err := copyPanelFile(executable, backup, 0o755); err != nil {
		fail(75, "panel_backup_failed", "cannot back up current panel binary", err)
		return
	}
	replacement := filepath.Join(filepath.Dir(executable), ".palpanel-update-replacement")
	_ = os.Remove(replacement)
	if err := copyPanelFile(candidate, replacement, 0o755); err != nil {
		fail(85, "panel_replacement_prepare_failed", "cannot prepare panel replacement", err)
		return
	}
	marker := panelRestartMarker{JobID: jobID, BinaryPath: executable, BackupPath: backup, ExpectedSHA256: candidateSHA, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := writePanelRestartMarker(marker); err != nil {
		_ = os.Remove(replacement)
		fail(85, "panel_restart_marker_failed", "cannot write panel restart marker", err)
		return
	}
	if err := os.Rename(replacement, executable); err != nil {
		_ = removePanelRestartMarker(executable)
		fail(85, "panel_replacement_failed", "cannot activate panel binary", err)
		return
	}
	if activeSHA, hashErr := sha256PanelFile(executable); hashErr != nil || !strings.EqualFold(activeSHA, candidateSHA) {
		_ = restorePanelBackup(executable, backup)
		_ = removePanelRestartMarker(executable)
		if hashErr == nil {
			hashErr = fmt.Errorf("active binary sha256 = %s, expected %s", activeSHA, candidateSHA)
		}
		fail(90, "panel_activation_checksum_failed", "activated panel binary checksum mismatch", hashErr)
		return
	}
	_ = m.jobs.Update(jobID, "completed", 100, "PalPanel "+selection.Release.TagName+" installed; restarting", "")
	if err := replaceCurrentPanelProcess(executable); err != nil {
		_ = restorePanelBackup(executable, backup)
		_ = removePanelRestartMarker(executable)
		fail(100, "panel_restart_failed", "panel installed but restart failed; previous binary restored", err)
	}
}

func (m Manager) resolvePanelRelease(ctx context.Context, request PanelUpdateRequest) (panelReleaseSelection, error) {
	endpoint := "https://api.github.com/repos/" + request.Repository + "/releases?per_page=100"
	var releases []panelGitHubRelease
	if err := m.getPanelJSON(ctx, endpoint, &releases); err != nil {
		return panelReleaseSelection{}, err
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

func (m Manager) getPanelJSON(ctx context.Context, endpoint string, destination any) error {
	client := m.downloadClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
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
	client := m.downloadClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
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
