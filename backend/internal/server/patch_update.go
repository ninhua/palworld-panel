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
	"sort"
	"strconv"
	"strings"
	"time"

	"palpanel/internal/db"
	"palpanel/internal/jobs"
)

const (
	patchUpdateMaxMetadataBytes = 8 << 20
	patchUpdateMaxArchiveBytes  = 512 << 20
	patchUpdateRepository       = "ninhua/Palworld-Panel-Patches"
)

var patchRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type PatchUpdateRequest struct {
	TargetVersion       string
	CurrentPatchVersion string
	Repository          string
	RequiredFeature     string
}

type PatchUpdateStatus struct {
	TargetVersion       string `json:"target_version"`
	CurrentPatchVersion string `json:"current_patch_version"`
	LatestPatchVersion  string `json:"latest_patch_version"`
	ReleaseTag          string `json:"release_tag"`
	ReleaseURL          string `json:"release_url"`
	UpdateAvailable     bool   `json:"update_available"`
	CheckedAt           string `json:"checked_at"`
	Message             string `json:"message"`
}

type patchGitHubRelease struct {
	TagName    string             `json:"tag_name"`
	HTMLURL    string             `json:"html_url"`
	Draft      bool               `json:"draft"`
	Prerelease bool               `json:"prerelease"`
	Assets     []patchGitHubAsset `json:"assets"`
}

type patchGitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type patchReleaseSelection struct {
	Release   patchGitHubRelease
	Version   string
	Archive   patchGitHubAsset
	Manifest  patchGitHubAsset
	Checksums patchGitHubAsset
}

type patchPublishedWorkspace struct {
	SchemaVersion int    `json:"schema_version"`
	TargetVersion string `json:"target_version"`
	State         string `json:"state"`
	Verified      bool   `json:"verified"`
	ReleaseTag    string `json:"release_tag"`
}

type patchReleaseManifest struct {
	SchemaVersion int    `json:"schema_version"`
	Project       string `json:"project"`
	PatchVersion  string `json:"patch_version"`
	PatchType     string `json:"patch_type"`
	Upstream      struct {
		Repository string `json:"repository"`
		Version    string `json:"version"`
	} `json:"upstream"`
	Compatibility struct {
		Mode          string `json:"mode"`
		TargetVersion string `json:"target_version"`
		Verified      bool   `json:"verified"`
	} `json:"compatibility"`
	Platforms []string `json:"platforms"`
	Files     map[string]struct {
		OriginalSHA256 string `json:"original_sha256"`
		PatchedSHA256  string `json:"patched_sha256"`
	} `json:"files"`
	Features []string `json:"features"`
}

type patchRestartMarker struct {
	JobID          string `json:"job_id"`
	BinaryPath     string `json:"binary_path"`
	BackupPath     string `json:"backup_path"`
	ExpectedSHA256 string `json:"expected_sha256"`
	CreatedAt      string `json:"created_at"`
}

func normalizePatchUpdateRequest(request PatchUpdateRequest) (PatchUpdateRequest, error) {
	request.TargetVersion = strings.TrimSpace(request.TargetVersion)
	request.CurrentPatchVersion = strings.TrimSpace(request.CurrentPatchVersion)
	request.Repository = strings.TrimSpace(request.Repository)
	request.RequiredFeature = strings.TrimSpace(request.RequiredFeature)
	if request.Repository == "" {
		request.Repository = patchUpdateRepository
	}
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+$`).MatchString(request.TargetVersion) {
		return request, fmt.Errorf("invalid patch target version: %s", request.TargetVersion)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+(?:[-+][A-Za-z0-9.-]+)?$`).MatchString(request.CurrentPatchVersion) {
		return request, fmt.Errorf("invalid current patch version: %s", request.CurrentPatchVersion)
	}
	if !patchRepositoryPattern.MatchString(request.Repository) {
		return request, fmt.Errorf("invalid patch repository: %s", request.Repository)
	}
	return request, nil
}

func (m Manager) PatchUpdateStatus(ctx context.Context, request PatchUpdateRequest) (PatchUpdateStatus, error) {
	request, err := normalizePatchUpdateRequest(request)
	if err != nil {
		return PatchUpdateStatus{}, err
	}
	selection, err := m.resolvePatchRelease(ctx, request)
	if err != nil {
		return PatchUpdateStatus{}, err
	}
	available := comparePatchVersions(selection.Version, request.CurrentPatchVersion) > 0
	message := "当前已是最新补丁 " + request.CurrentPatchVersion
	if available {
		message = "发现补丁更新 " + selection.Version
	}
	return PatchUpdateStatus{
		TargetVersion:       request.TargetVersion,
		CurrentPatchVersion: request.CurrentPatchVersion,
		LatestPatchVersion:  selection.Version,
		ReleaseTag:          selection.Release.TagName,
		ReleaseURL:          selection.Release.HTMLURL,
		UpdateAvailable:     available,
		CheckedAt:           time.Now().UTC().Format(time.RFC3339Nano),
		Message:             message,
	}, nil
}

func (m Manager) CheckPatchUpdate(ctx context.Context, request PatchUpdateRequest) (db.Job, error) {
	return m.jobs.Submit(ctx, jobs.ClassGeneral, "patch_update_check", "queued panel patch update check", func(jobCtx context.Context, jobID string) {
		_ = m.jobs.Update(jobID, "running", 20, "checking stable panel patch releases", "")
		status, err := m.PatchUpdateStatus(jobCtx, request)
		if err != nil {
			_ = m.jobs.UpdateWithCode(jobID, "failed", 100, "panel patch update check failed", err.Error(), "patch_update_check_failed")
			return
		}
		_ = m.jobs.Update(jobID, "completed", 100, status.Message, "")
	})
}

func (m Manager) ApplyPatchUpdate(ctx context.Context, request PatchUpdateRequest) (db.Job, error) {
	request, err := normalizePatchUpdateRequest(request)
	if err != nil {
		return db.Job{}, err
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return db.Job{}, fmt.Errorf("panel patch hot update currently requires linux-amd64")
	}
	return m.jobs.Submit(ctx, jobs.ClassLifecycle, "patch_hot_update", "queued panel patch hot update", func(jobCtx context.Context, jobID string) {
		m.runPatchUpdate(jobCtx, jobID, request)
	})
}

func (m Manager) runPatchUpdate(ctx context.Context, jobID string, request PatchUpdateRequest) {
	fail := func(progress int, code, message string, err error) {
		detail := message
		if err != nil {
			detail = err.Error()
		}
		_ = m.jobs.UpdateWithCode(jobID, "failed", progress, message, detail, code)
	}

	_ = m.jobs.Update(jobID, "running", 5, "reading current panel patch metadata", "")
	selection, err := m.resolvePatchRelease(ctx, request)
	if err != nil {
		fail(15, "patch_release_lookup_failed", "panel patch release lookup failed", err)
		return
	}
	if comparePatchVersions(selection.Version, request.CurrentPatchVersion) <= 0 {
		_ = m.jobs.Update(jobID, "completed", 100, "current panel patch is already up to date: "+request.CurrentPatchVersion, "")
		return
	}

	_ = m.jobs.Update(jobID, "running", 25, "downloading patch release metadata", "")
	stage, err := os.MkdirTemp(executableDirectory(), ".palpanel-patch-update-")
	if err != nil {
		fail(25, "patch_stage_failed", "cannot create panel patch staging directory", err)
		return
	}
	defer os.RemoveAll(stage)

	checksumsPath := filepath.Join(stage, selection.Checksums.Name)
	manifestPath := filepath.Join(stage, selection.Manifest.Name)
	archivePath := filepath.Join(stage, selection.Archive.Name)
	if err := m.downloadPatchAsset(ctx, selection.Checksums.BrowserDownloadURL, checksumsPath, patchUpdateMaxMetadataBytes); err != nil {
		fail(30, "patch_checksums_download_failed", "cannot download panel patch checksums", err)
		return
	}
	if err := m.downloadPatchAsset(ctx, selection.Manifest.BrowserDownloadURL, manifestPath, patchUpdateMaxMetadataBytes); err != nil {
		fail(35, "patch_manifest_download_failed", "cannot download panel patch manifest", err)
		return
	}

	checksums, err := parsePatchChecksums(checksumsPath)
	if err != nil {
		fail(40, "patch_checksums_invalid", "panel patch checksums are invalid", err)
		return
	}
	if err := verifyNamedPatchFile(manifestPath, selection.Manifest.Name, checksums); err != nil {
		fail(45, "patch_manifest_checksum_failed", "panel patch manifest checksum mismatch", err)
		return
	}
	manifest, err := readPatchManifest(manifestPath)
	if err != nil {
		fail(45, "patch_manifest_invalid", "panel patch manifest is invalid", err)
		return
	}
	if err := validatePatchManifest(manifest, request, selection.Version); err != nil {
		fail(45, "patch_manifest_incompatible", "panel patch manifest is incompatible", err)
		return
	}

	_ = m.jobs.Update(jobID, "running", 55, "downloading verified panel patch binary", "")
	if err := m.downloadPatchAsset(ctx, selection.Archive.BrowserDownloadURL, archivePath, patchUpdateMaxArchiveBytes); err != nil {
		fail(55, "patch_archive_download_failed", "cannot download panel patch archive", err)
		return
	}
	if err := verifyNamedPatchFile(archivePath, selection.Archive.Name, checksums); err != nil {
		fail(60, "patch_archive_checksum_failed", "panel patch archive checksum mismatch", err)
		return
	}

	candidate := filepath.Join(stage, "palpanel")
	if err := extractPatchBinary(archivePath, candidate); err != nil {
		fail(65, "patch_archive_invalid", "cannot extract panel patch binary", err)
		return
	}
	binaryInfo, ok := manifest.Files["bin/palpanel"]
	if !ok {
		fail(65, "patch_binary_metadata_missing", "panel patch manifest has no bin/palpanel metadata", nil)
		return
	}
	candidateSHA, err := sha256PatchFile(candidate)
	if err != nil || !strings.EqualFold(candidateSHA, binaryInfo.PatchedSHA256) {
		if err == nil {
			err = fmt.Errorf("patched binary sha256 = %s, expected %s", candidateSHA, binaryInfo.PatchedSHA256)
		}
		fail(70, "patch_binary_checksum_failed", "panel patch binary checksum mismatch", err)
		return
	}
	if err := os.Chmod(candidate, 0o755); err != nil {
		fail(70, "patch_binary_mode_failed", "cannot mark panel patch binary executable", err)
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	output, checkErr := exec.CommandContext(checkCtx, candidate, "--version").CombinedOutput()
	cancel()
	if checkErr != nil || !strings.Contains(string(output), request.TargetVersion) {
		if checkErr == nil {
			checkErr = fmt.Errorf("candidate version output does not contain target %s: %s", request.TargetVersion, strings.TrimSpace(string(output)))
		}
		fail(70, "patch_binary_probe_failed", "panel patch binary probe failed", checkErr)
		return
	}

	_ = m.jobs.Update(jobID, "running", 75, "backing up current panel binary", "")
	executable, err := executablePath()
	if err != nil {
		fail(75, "patch_executable_path_failed", "cannot resolve current panel binary", err)
		return
	}
	backupDir := filepath.Join(filepath.Dir(executable), ".palpanel-patch-backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		fail(75, "patch_backup_directory_failed", "cannot create panel patch backup directory", err)
		return
	}
	backup := filepath.Join(backupDir, fmt.Sprintf("palpanel-%s-%s", request.CurrentPatchVersion, time.Now().UTC().Format("20060102T150405.000000000Z")))
	if err := copyPatchFile(executable, backup, 0o755); err != nil {
		fail(75, "patch_backup_failed", "cannot back up current panel binary", err)
		return
	}

	_ = m.jobs.Update(jobID, "running", 85, "atomically replacing panel binary", "")
	replacement := filepath.Join(filepath.Dir(executable), ".palpanel-patch-replacement")
	_ = os.Remove(replacement)
	if err := copyPatchFile(candidate, replacement, 0o755); err != nil {
		fail(85, "patch_replacement_prepare_failed", "cannot prepare panel patch replacement", err)
		return
	}
	marker := patchRestartMarker{
		JobID:          jobID,
		BinaryPath:     executable,
		BackupPath:     backup,
		ExpectedSHA256: binaryInfo.PatchedSHA256,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := writePatchRestartMarker(marker); err != nil {
		_ = os.Remove(replacement)
		fail(85, "patch_restart_marker_failed", "cannot write panel patch restart marker", err)
		return
	}
	if err := os.Rename(replacement, executable); err != nil {
		_ = removePatchRestartMarker(executable)
		fail(85, "patch_replacement_failed", "cannot activate panel patch binary", err)
		return
	}
	if activeSHA, hashErr := sha256PatchFile(executable); hashErr != nil || !strings.EqualFold(activeSHA, binaryInfo.PatchedSHA256) {
		_ = restorePatchBackup(executable, backup)
		_ = removePatchRestartMarker(executable)
		if hashErr == nil {
			hashErr = fmt.Errorf("active binary sha256 = %s, expected %s", activeSHA, binaryInfo.PatchedSHA256)
		}
		fail(90, "patch_activation_checksum_failed", "activated panel patch binary checksum mismatch", hashErr)
		return
	}

	_ = os.RemoveAll(stage)
	_ = m.jobs.Update(jobID, "completed", 100, "panel patch "+selection.Version+" installed; restarting PalPanel", "")
	if err := replaceCurrentPanelProcess(executable); err != nil {
		_ = restorePatchBackup(executable, backup)
		_ = removePatchRestartMarker(executable)
		fail(100, "patch_restart_failed", "panel patch installed but process restart failed; previous binary restored", err)
	}
}

func (m Manager) resolvePatchRelease(ctx context.Context, request PatchUpdateRequest) (patchReleaseSelection, error) {
	selection, workspaceErr := m.resolvePatchReleaseFromWorkspace(ctx, request)
	if workspaceErr == nil {
		return selection, nil
	}
	selection, apiErr := m.resolvePatchReleaseFromAPI(ctx, request)
	if apiErr == nil {
		return selection, nil
	}
	return patchReleaseSelection{}, fmt.Errorf(
		"patch release lookup failed (published workspace: %v; github api: %v)",
		workspaceErr,
		apiErr,
	)
}

func (m Manager) resolvePatchReleaseFromWorkspace(ctx context.Context, request PatchUpdateRequest) (patchReleaseSelection, error) {
	endpoint := "https://raw.githubusercontent.com/" + request.Repository + "/main/projects/uitok-palworld-panel/patches/stable-" + request.TargetVersion + "/workspace.json"
	if override := strings.TrimSpace(os.Getenv("PALPANEL_PATCH_RELEASE_WORKSPACE_URL")); override != "" {
		endpoint = override
	}
	var workspace patchPublishedWorkspace
	if err := m.getPatchJSON(ctx, endpoint, &workspace); err != nil {
		return patchReleaseSelection{}, err
	}
	return patchSelectionFromWorkspace(request, workspace)
}

func patchSelectionFromWorkspace(request PatchUpdateRequest, workspace patchPublishedWorkspace) (patchReleaseSelection, error) {
	if workspace.SchemaVersion != 2 || workspace.TargetVersion != request.TargetVersion || workspace.State != "released" || !workspace.Verified {
		return patchReleaseSelection{}, fmt.Errorf("published workspace is not released and verified for %s", request.TargetVersion)
	}
	prefix := "uitok-stable-" + request.TargetVersion + "-p"
	if !strings.HasPrefix(workspace.ReleaseTag, prefix) {
		return patchReleaseSelection{}, fmt.Errorf("published workspace release tag does not match %s", request.TargetVersion)
	}
	version := strings.TrimPrefix(workspace.ReleaseTag, prefix)
	if _, err := parsePatchVersion(version); err != nil {
		return patchReleaseSelection{}, err
	}
	archiveName := fmt.Sprintf("uitok-palworld-panel_stable-%s_patch-%s_linux-amd64.tar.gz", request.TargetVersion, version)
	releaseURL := "https://github.com/" + request.Repository + "/releases/tag/" + workspace.ReleaseTag
	downloadBase := "https://github.com/" + request.Repository + "/releases/download/" + workspace.ReleaseTag + "/"
	return patchReleaseSelection{
		Release: patchGitHubRelease{
			TagName: workspace.ReleaseTag,
			HTMLURL: releaseURL,
		},
		Version: version,
		Archive: patchGitHubAsset{
			Name:               archiveName,
			BrowserDownloadURL: downloadBase + archiveName,
		},
		Manifest: patchGitHubAsset{
			Name:               "manifest.json",
			BrowserDownloadURL: downloadBase + "manifest.json",
		},
		Checksums: patchGitHubAsset{
			Name:               "SHA256SUMS",
			BrowserDownloadURL: downloadBase + "SHA256SUMS",
		},
	}, nil
}

func (m Manager) resolvePatchReleaseFromAPI(ctx context.Context, request PatchUpdateRequest) (patchReleaseSelection, error) {
	endpoint := "https://api.github.com/repos/" + request.Repository + "/releases?per_page=100"
	var releases []patchGitHubRelease
	if err := m.getPatchJSON(ctx, endpoint, &releases); err != nil {
		return patchReleaseSelection{}, err
	}
	prefix := "uitok-stable-" + request.TargetVersion + "-p"
	var candidates []patchReleaseSelection
	for _, release := range releases {
		if release.Draft || release.Prerelease || !strings.HasPrefix(release.TagName, prefix) {
			continue
		}
		version := strings.TrimPrefix(release.TagName, prefix)
		if _, err := parsePatchVersion(version); err != nil {
			continue
		}
		archiveName := fmt.Sprintf("uitok-palworld-panel_stable-%s_patch-%s_linux-amd64.tar.gz", request.TargetVersion, version)
		selection := patchReleaseSelection{Release: release, Version: version}
		for _, asset := range release.Assets {
			switch asset.Name {
			case archiveName:
				selection.Archive = asset
			case "manifest.json":
				selection.Manifest = asset
			case "SHA256SUMS":
				selection.Checksums = asset
			}
		}
		if selection.Archive.BrowserDownloadURL == "" || selection.Manifest.BrowserDownloadURL == "" || selection.Checksums.BrowserDownloadURL == "" {
			continue
		}
		candidates = append(candidates, selection)
	}
	if len(candidates) == 0 {
		return patchReleaseSelection{}, fmt.Errorf("no verified stable patch release found for %s", request.TargetVersion)
	}
	sort.Slice(candidates, func(i, j int) bool { return comparePatchVersions(candidates[i].Version, candidates[j].Version) > 0 })
	return candidates[0], nil
}

func (m Manager) getPatchJSON(ctx context.Context, endpoint string, destination any) error {
	client := m.downloadClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	var lastErr error
	for _, candidate := range patchRequestURLs(endpoint) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate, nil)
		if err != nil {
			lastErr = err
			continue
		}
		setPatchRequestHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			lastErr = fmt.Errorf("github release request returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
			continue
		}
		decoder := json.NewDecoder(io.LimitReader(resp.Body, patchUpdateMaxMetadataBytes))
		err = decoder.Decode(destination)
		resp.Body.Close()
		if err == nil {
			return nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no patch release request URL available")
	}
	return lastErr
}

func (m Manager) downloadPatchAsset(ctx context.Context, endpoint, destination string, maxBytes int64) error {
	client := m.downloadClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	var lastErr error
	for _, candidate := range patchRequestURLs(endpoint) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate, nil)
		if err != nil {
			lastErr = err
			continue
		}
		setPatchRequestHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			lastErr = fmt.Errorf("download returned %s", resp.Status)
			continue
		}
		if resp.ContentLength > maxBytes {
			resp.Body.Close()
			lastErr = fmt.Errorf("download exceeds size limit")
			continue
		}
		temporary := destination + ".part"
		_ = os.Remove(temporary)
		file, err := os.OpenFile(temporary, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			resp.Body.Close()
			lastErr = err
			continue
		}
		written, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxBytes+1))
		closeErr := file.Close()
		resp.Body.Close()
		if copyErr != nil {
			_ = os.Remove(temporary)
			lastErr = copyErr
			continue
		}
		if closeErr != nil {
			_ = os.Remove(temporary)
			lastErr = closeErr
			continue
		}
		if written > maxBytes {
			_ = os.Remove(temporary)
			lastErr = fmt.Errorf("download exceeds size limit")
			continue
		}
		if err := os.Rename(temporary, destination); err != nil {
			_ = os.Remove(temporary)
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no patch asset download URL available")
	}
	return lastErr
}

func patchRequestURLs(endpoint string) []string {
	values := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, proxy := range []string{
		os.Getenv("PALPANEL_PATCH_GITHUB_PROXY"),
		os.Getenv("GH_PROXY_BASE"),
		os.Getenv("GH_PROXY_FALLBACK"),
	} {
		proxy = strings.TrimRight(strings.TrimSpace(proxy), "/")
		if proxy == "" || (!strings.HasPrefix(proxy, "https://") && !strings.HasPrefix(proxy, "http://")) {
			continue
		}
		candidate := proxy + "/" + endpoint
		if !seen[candidate] {
			seen[candidate] = true
			values = append(values, candidate)
		}
	}
	if !seen[endpoint] {
		values = append(values, endpoint)
	}
	return values
}

func setPatchRequestHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PalPanel-patch-hot-update")
	if token := strings.TrimSpace(firstPatchValue(
		os.Getenv("PALWORLD_LINUX_PANEL_PATCH_GITHUB_TOKEN"),
		os.Getenv("PALPANEL_PANEL_PATCH_GITHUB_TOKEN"),
		os.Getenv("PALPANEL_PATCH_GITHUB_TOKEN"),
		os.Getenv("GITHUB_TOKEN"),
		os.Getenv("GH_TOKEN"),
	)); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func firstPatchValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parsePatchVersion(value string) ([]int, error) {
	core := strings.SplitN(value, "-", 2)[0]
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid patch version: %s", value)
	}
	out := make([]int, 3)
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return nil, fmt.Errorf("invalid patch version: %s", value)
		}
		out[index] = number
	}
	return out, nil
}

func comparePatchVersions(left, right string) int {
	leftParts, leftErr := parsePatchVersion(left)
	rightParts, rightErr := parsePatchVersion(right)
	if leftErr != nil || rightErr != nil {
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

func parsePatchChecksums(path string) (map[string]string, error) {
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
		if _, exists := out[clean]; exists {
			return nil, fmt.Errorf("duplicate SHA256SUMS path: %s", clean)
		}
		out[clean] = strings.ToLower(parts[0])
	}
	return out, nil
}

func verifyNamedPatchFile(path, name string, checksums map[string]string) error {
	expected, ok := checksums[name]
	if !ok {
		return fmt.Errorf("SHA256SUMS has no entry for %s", name)
	}
	actual, err := sha256PatchFile(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("%s sha256 = %s, expected %s", name, actual, expected)
	}
	return nil
}

func readPatchManifest(path string) (patchReleaseManifest, error) {
	var manifest patchReleaseManifest
	body, err := os.ReadFile(path)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func validatePatchManifest(manifest patchReleaseManifest, request PatchUpdateRequest, expectedVersion string) error {
	if manifest.SchemaVersion != 1 || manifest.Project != "uitok-palworld-panel" || manifest.PatchType != "source-build" {
		return fmt.Errorf("unexpected patch manifest identity")
	}
	if manifest.PatchVersion != expectedVersion {
		return fmt.Errorf("manifest patch version = %s, expected %s", manifest.PatchVersion, expectedVersion)
	}
	if manifest.Upstream.Repository != "uitok/palworld-panel" || manifest.Upstream.Version != request.TargetVersion {
		return fmt.Errorf("manifest upstream does not match %s", request.TargetVersion)
	}
	if manifest.Compatibility.Mode != "exact" || manifest.Compatibility.TargetVersion != request.TargetVersion || !manifest.Compatibility.Verified {
		return fmt.Errorf("manifest compatibility is not exact and verified for %s", request.TargetVersion)
	}
	if !containsPatchValue(manifest.Platforms, "linux-amd64") {
		return fmt.Errorf("manifest does not contain linux-amd64")
	}
	if request.RequiredFeature != "" && !containsPatchValue(manifest.Features, request.RequiredFeature) {
		return fmt.Errorf("manifest does not contain required feature %s", request.RequiredFeature)
	}
	binary, ok := manifest.Files["bin/palpanel"]
	if !ok || !validPatchSHA256(binary.PatchedSHA256) {
		return fmt.Errorf("manifest patched binary sha256 is missing or invalid")
	}
	return nil
}

func containsPatchValue(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func validPatchSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func extractPatchBinary(archivePath, destination string) error {
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
	found := 0
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
		if !strings.HasSuffix(name, "/overlay/bin/palpanel") {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("patch binary is not a regular file")
		}
		if header.Size <= 0 || header.Size > patchUpdateMaxArchiveBytes {
			return fmt.Errorf("patch binary size is invalid")
		}
		if found > 0 {
			return fmt.Errorf("archive contains multiple patch binaries")
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
		return fmt.Errorf("archive does not contain exactly one overlay/bin/palpanel")
	}
	return nil
}

func executablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
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

func sha256PatchFile(path string) (string, error) {
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

func copyPatchFile(source, destination string, mode os.FileMode) error {
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
	if copyErr != nil {
		_ = os.Remove(temporary)
		return copyErr
	}
	if syncErr != nil {
		_ = os.Remove(temporary)
		return syncErr
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return closeErr
	}
	if err := os.Chmod(temporary, mode); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Rename(temporary, destination)
}

func patchRestartMarkerPath(binary string) string {
	return filepath.Join(filepath.Dir(binary), ".palpanel-patch-update-state.json")
}

func writePatchRestartMarker(marker patchRestartMarker) error {
	body, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	path := patchRestartMarkerPath(marker.BinaryPath)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(body, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func removePatchRestartMarker(binary string) error {
	err := os.Remove(patchRestartMarkerPath(binary))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func restorePatchBackup(binary, backup string) error {
	if backup == "" {
		return fmt.Errorf("patch backup path is empty")
	}
	return copyPatchFile(backup, binary, 0o755)
}
