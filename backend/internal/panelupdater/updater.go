//go:build linux

package panelupdater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"palpanel/internal/networkproxy"
)

const (
	OfficialRepository       = "ninhua/palworld-panel"
	maxChecksumsBytes  int64 = 8 << 20
	maxArchiveBytes    int64 = 512 << 20
	maxExtractedBytes  int64 = 1024 << 20
	maxArchiveFiles          = 4096
)

var requiredPackageFiles = []string{
	"bin/palpanel",
	"bin/palpanel-updater",
	"bin/sav-cli",
	"bin/palcalc-bridge",
	"palpanelctl",
	"systemd/palpanel.service",
	"systemd/palpanel-sav-cli.service",
	"systemd/palpanel-palcalc.service",
	"systemd/palpanel-update.service",
	"systemd/palpanel-update.path",
	"checksums.txt",
}

var managedUnits = []string{
	"palpanel.service",
	"palpanel-sav-cli.service",
	"palpanel-palcalc.service",
	"palpanel-update.service",
	"palpanel-update.path",
}

type Config struct {
	DataDir       string
	InstallRoot   string
	EtcDir        string
	SystemdDir    string
	LibexecDir    string
	ServiceUser   string
	SystemctlPath string
	HTTPClient    *http.Client
	ReleaseClient *http.Client
	HealthTimeout time.Duration
	PollInterval  time.Duration
	RunCommand    func(context.Context, string, ...string) ([]byte, error)
}

func DefaultConfig() Config {
	return Config{
		DataDir:       "/var/lib/palpanel",
		InstallRoot:   "/opt/palpanel",
		EtcDir:        "/etc/palpanel",
		SystemdDir:    "/etc/systemd/system",
		LibexecDir:    "/usr/libexec",
		ServiceUser:   "palpanel",
		SystemctlPath: "systemctl",
		HealthTimeout: 90 * time.Second,
		PollInterval:  time.Second,
	}
}

type unitBackup struct {
	Body   []byte
	Mode   os.FileMode
	Exists bool
}

type updateFailure struct {
	Code string
	Err  error
}

func (failure updateFailure) Error() string { return failure.Err.Error() }
func (failure updateFailure) Unwrap() error { return failure.Err }

func fail(code, format string, values ...any) error {
	return updateFailure{Code: code, Err: fmt.Errorf(format, values...)}
}

func Execute(ctx context.Context, requestPath string, config Config) Result {
	config = normalizeConfig(config)
	result := Result{SchemaVersion: SchemaVersion, Status: "failed"}
	stateDir := StateDir(config.DataDir)
	pending := RequestPath(config.DataDir)
	inProgress := InProgressPath(config.DataDir)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		result.Message, result.ErrorCode, result.Detail = "无法创建更新状态目录", "panel_update_state_failed", err.Error()
		return result
	}
	if filepath.Clean(requestPath) != filepath.Clean(pending) {
		result.Message, result.ErrorCode, result.Detail = "更新请求路径无效", "panel_update_request_path_invalid", requestPath
		return result
	}
	pendingExists, err := regularPathExists(pending)
	if err != nil {
		result.Message, result.ErrorCode, result.Detail = "无法检查更新请求", "panel_update_request_claim_failed", err.Error()
		return result
	}
	inProgressExists, err := regularPathExists(inProgress)
	if err != nil {
		result.Message, result.ErrorCode, result.Detail = "无法检查执行中的更新请求", "panel_update_request_claim_failed", err.Error()
		return result
	}
	if pendingExists && inProgressExists {
		result.Message, result.ErrorCode, result.Detail = "检测到冲突的更新请求", "panel_update_request_conflict", "request.json and request.in-progress.json both exist"
		return result
	}
	if !inProgressExists {
		if !pendingExists {
			result.Message, result.ErrorCode, result.Detail = "没有待执行的更新请求", "panel_update_request_missing", pending
			return result
		}
		if err := os.Rename(pending, inProgress); err != nil {
			result.Message, result.ErrorCode, result.Detail = "无法接管更新请求", "panel_update_request_claim_failed", err.Error()
			return result
		}
	}
	request, err := ReadRequest(inProgress, stateDir)
	if err != nil {
		result.Message, result.ErrorCode, result.Detail = "更新请求校验失败", "panel_update_request_invalid", err.Error()
		if writeErr := WriteResult(config.DataDir, result); writeErr == nil {
			_ = os.Remove(inProgress)
		}
		return result
	}
	result.JobID = request.JobID
	result.CurrentVersion = request.CurrentVersion
	result.TargetVersion = request.TargetVersion

	final, runErr := executeClaimed(ctx, request, config)
	if final.JobID != "" {
		result = final
	}
	if runErr != nil {
		var failure updateFailure
		if errors.As(runErr, &failure) {
			result.ErrorCode = failure.Code
		} else {
			result.ErrorCode = "panel_update_failed"
		}
		if result.Message == "" {
			result.Message = "PalPanel 更新失败"
		}
		if result.Detail == "" {
			result.Detail = runErr.Error()
		}
	} else {
		result = final
	}
	result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := WriteResult(config.DataDir, result); err != nil {
		result.Status = "failed"
		result.ErrorCode = "panel_update_result_write_failed"
		result.Message = "更新结果写入失败"
		result.Detail = err.Error()
		return result
	}
	_ = os.Remove(inProgress)
	if result.Status == "completed" || result.Status == "rolled_back" {
		_ = cleanupOperation(request.ArchivePath)
	}
	return result
}

func regularPathExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("%s is not a regular file", path)
	}
	return true, nil
}

func normalizeConfig(config Config) Config {
	defaults := DefaultConfig()
	if config.DataDir == "" {
		config.DataDir = defaults.DataDir
	}
	if config.InstallRoot == "" {
		config.InstallRoot = defaults.InstallRoot
	}
	if config.EtcDir == "" {
		config.EtcDir = defaults.EtcDir
	}
	if config.SystemdDir == "" {
		config.SystemdDir = defaults.SystemdDir
	}
	if config.LibexecDir == "" {
		config.LibexecDir = defaults.LibexecDir
	}
	if config.ServiceUser == "" {
		config.ServiceUser = defaults.ServiceUser
	}
	if config.SystemctlPath == "" {
		config.SystemctlPath = defaults.SystemctlPath
	}
	if config.HealthTimeout <= 0 {
		config.HealthTimeout = defaults.HealthTimeout
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaults.PollInterval
	}
	if config.RunCommand == nil {
		config.RunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		}
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	config.DataDir = filepath.Clean(config.DataDir)
	config.InstallRoot = filepath.Clean(config.InstallRoot)
	config.EtcDir = filepath.Clean(config.EtcDir)
	config.SystemdDir = filepath.Clean(config.SystemdDir)
	config.LibexecDir = filepath.Clean(config.LibexecDir)
	return config
}

func executeClaimed(ctx context.Context, request Request, config Config) (Result, error) {
	archiveName := "palpanel_" + request.TargetVersion + "_linux_amd64.tar.gz"
	if filepath.Base(request.ArchivePath) != archiveName {
		return Result{}, fail("panel_update_archive_name_invalid", "archive name %q does not match target version", filepath.Base(request.ArchivePath))
	}
	archive, err := openRegularNoFollow(request.ArchivePath)
	if err != nil {
		return Result{}, fail("panel_update_archive_open_failed", "open update archive: %v", err)
	}
	defer archive.Close()
	actualSHA, err := hashOpenFile(archive, maxArchiveBytes)
	if err != nil {
		return Result{}, fail("panel_update_archive_hash_failed", "hash update archive: %v", err)
	}
	if !strings.EqualFold(actualSHA, request.ArchiveSHA256) {
		return Result{}, fail("panel_update_archive_changed", "archive sha256 changed after panel validation")
	}
	releaseClient, err := releaseHTTPClient(config, request.ProxyURL)
	if err != nil {
		return Result{}, fail("panel_update_proxy_invalid", "configure release checksum proxy: %v", err)
	}
	officialSHA, err := fetchOfficialArchiveSHA(ctx, releaseClient, request.TargetVersion, archiveName)
	if err != nil {
		return Result{}, fail("panel_update_official_checksum_failed", "fetch official release checksum: %v", err)
	}
	if !strings.EqualFold(actualSHA, officialSHA) {
		return Result{}, fail("panel_update_archive_untrusted", "archive sha256 %s does not match official release %s", actualSHA, officialSHA)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return Result{}, fail("panel_update_archive_seek_failed", "rewind update archive: %v", err)
	}

	extractRoot, err := os.MkdirTemp(config.InstallRoot, ".panel-update-extract-")
	if err != nil {
		return Result{}, fail("panel_update_extract_root_failed", "create extraction directory: %v", err)
	}
	defer os.RemoveAll(extractRoot)
	packageRoot, err := extractPackage(archive, extractRoot, archiveName[:len(archiveName)-len(".tar.gz")])
	if err != nil {
		return Result{}, fail("panel_update_archive_invalid", "extract update package: %v", err)
	}
	if err := verifyPackageChecksums(packageRoot); err != nil {
		return Result{}, fail("panel_update_package_checksum_failed", "verify package checksums: %v", err)
	}
	if err := probeBinary(ctx, filepath.Join(packageRoot, "bin", "palpanel"), request.TargetVersion); err != nil {
		return Result{}, fail("panel_update_panel_probe_failed", "probe candidate panel: %v", err)
	}
	if err := probeBinary(ctx, filepath.Join(packageRoot, "bin", "palpanel-updater"), request.TargetVersion); err != nil {
		return Result{}, fail("panel_update_helper_probe_failed", "probe candidate updater: %v", err)
	}

	activeDir, err := currentVersionDirectory(config.InstallRoot)
	if err != nil {
		return Result{}, fail("panel_update_current_invalid", "resolve current installation: %v", err)
	}
	targetDir := filepath.Join(config.InstallRoot, request.TargetVersion)
	if !directChild(config.InstallRoot, targetDir) {
		return Result{}, fail("panel_update_target_invalid", "target directory is unsafe")
	}
	switch filepath.Base(activeDir) {
	case request.TargetVersion:
		previousDir := filepath.Join(config.InstallRoot, request.CurrentVersion)
		if !directChild(config.InstallRoot, previousDir) {
			return Result{}, fail("panel_update_previous_invalid", "previous version directory is unsafe")
		}
		if info, statErr := os.Stat(previousDir); statErr != nil || !info.IsDir() {
			return Result{}, fail("panel_update_previous_missing", "previous version directory is unavailable: %v", statErr)
		}
		return resumeActivatedUpdate(ctx, request, config, previousDir, targetDir)
	case request.CurrentVersion:
		// Normal update path.
	default:
		return Result{}, fail("panel_update_current_version_mismatch", "active version directory is %s, request claims %s", filepath.Base(activeDir), request.CurrentVersion)
	}
	previousDir := activeDir
	if err := os.RemoveAll(targetDir); err != nil {
		return Result{}, fail("panel_update_target_cleanup_failed", "remove stale target directory: %v", err)
	}
	if err := os.Rename(packageRoot, targetDir); err != nil {
		return Result{}, fail("panel_update_target_install_failed", "install target package: %v", err)
	}
	if err := hardenVersionTree(targetDir); err != nil {
		_ = os.RemoveAll(targetDir)
		return Result{}, fail("panel_update_permissions_failed", "harden target permissions: %v", err)
	}

	unitBackups, err := backupUnits(config.SystemdDir)
	if err != nil {
		_ = os.RemoveAll(targetDir)
		return Result{}, fail("panel_update_unit_backup_failed", "back up systemd units: %v", err)
	}
	helperPath := filepath.Join(config.LibexecDir, "palpanel-updater")
	helperBackup, helperExists, err := readOptionalFile(helperPath)
	if err != nil {
		_ = os.RemoveAll(targetDir)
		return Result{}, fail("panel_update_helper_backup_failed", "back up update helper: %v", err)
	}

	activated := false
	rollback := func(cause error) (bool, error) {
		if !activated {
			_ = os.RemoveAll(targetDir)
			return false, cause
		}
		_ = runSystemctl(ctx, config, "stop", "palpanel.service", "palpanel-sav-cli.service", "palpanel-palcalc.service")
		rollbackErr := switchCurrent(config.InstallRoot, previousDir)
		if restoreErr := restoreUnits(config.SystemdDir, unitBackups); rollbackErr == nil && restoreErr != nil {
			rollbackErr = restoreErr
		}
		if restoreErr := restoreOptionalFile(helperPath, helperBackup, helperExists, 0o755); rollbackErr == nil && restoreErr != nil {
			rollbackErr = restoreErr
		}
		if reloadErr := runSystemctl(ctx, config, "daemon-reload"); rollbackErr == nil && reloadErr != nil {
			rollbackErr = reloadErr
		}
		if restartErr := runSystemctl(ctx, config, "restart", "palpanel-sav-cli.service", "palpanel-palcalc.service", "palpanel.service"); rollbackErr == nil && restartErr != nil {
			rollbackErr = restartErr
		}
		if rollbackErr == nil {
			rollbackErr = waitForPanel(ctx, config, request.HealthPort, request.CurrentVersion)
		}
		if rollbackErr == nil {
			_ = os.RemoveAll(targetDir)
			return true, cause
		}
		return false, fmt.Errorf("%w; rollback failed: %v", cause, rollbackErr)
	}

	if err := runSystemctl(ctx, config, "stop", "palpanel.service", "palpanel-sav-cli.service", "palpanel-palcalc.service"); err != nil {
		_ = os.RemoveAll(targetDir)
		return Result{}, fail("panel_update_stop_failed", "stop PalPanel services: %v", err)
	}
	if err := switchCurrent(config.InstallRoot, targetDir); err != nil {
		_ = runSystemctl(ctx, config, "restart", "palpanel-sav-cli.service", "palpanel-palcalc.service", "palpanel.service")
		_ = os.RemoveAll(targetDir)
		return Result{}, fail("panel_update_switch_failed", "activate target version: %v", err)
	}
	activated = true
	if err := activateInstalledVersion(ctx, request, config, targetDir, request.TargetVersion); err != nil {
		succeeded, rollbackErr := rollback(err)
		return rolledBackResult(request, succeeded, rollbackErr), rollbackErr
	}
	return Result{
		SchemaVersion: SchemaVersion, JobID: request.JobID,
		CurrentVersion: request.CurrentVersion, TargetVersion: request.TargetVersion,
		Status: "completed", Message: "PalPanel " + request.TargetVersion + " 已完成完整包更新并通过健康检查",
	}, nil
}

func activateInstalledVersion(ctx context.Context, request Request, config Config, versionDir, expectedVersion string) error {
	if err := installUnits(versionDir, config); err != nil {
		return fail("panel_update_unit_install_failed", "install systemd units: %v", err)
	}
	if err := copyAtomic(filepath.Join(versionDir, "bin", "palpanel-updater"), filepath.Join(config.LibexecDir, "palpanel-updater"), 0o755); err != nil {
		return fail("panel_update_helper_install_failed", "install update helper: %v", err)
	}
	if err := runSystemctl(ctx, config, "daemon-reload"); err != nil {
		return fail("panel_update_daemon_reload_failed", "reload systemd: %v", err)
	}
	if err := runSystemctl(ctx, config, "enable", "palpanel-update.path"); err != nil {
		return fail("panel_update_path_enable_failed", "enable update trigger: %v", err)
	}
	if err := runSystemctl(ctx, config, "restart", "palpanel-sav-cli.service", "palpanel-palcalc.service", "palpanel.service"); err != nil {
		return fail("panel_update_restart_failed", "restart PalPanel services: %v", err)
	}
	if err := waitForPanel(ctx, config, request.HealthPort, expectedVersion); err != nil {
		return fail("panel_update_health_failed", "version %s health check failed: %v", expectedVersion, err)
	}
	return nil
}

func resumeActivatedUpdate(ctx context.Context, request Request, config Config, previousDir, targetDir string) (Result, error) {
	if err := verifyPackageChecksums(targetDir); err != nil {
		return Result{}, fail("panel_update_resume_target_invalid", "verify activated target package: %v", err)
	}
	if err := probeBinary(ctx, filepath.Join(targetDir, "bin", "palpanel"), request.TargetVersion); err != nil {
		return Result{}, fail("panel_update_resume_target_invalid", "probe activated target panel: %v", err)
	}
	if err := probeBinary(ctx, filepath.Join(targetDir, "bin", "palpanel-updater"), request.TargetVersion); err != nil {
		return Result{}, fail("panel_update_resume_target_invalid", "probe activated target updater: %v", err)
	}
	if err := activateInstalledVersion(ctx, request, config, targetDir, request.TargetVersion); err == nil {
		return Result{
			SchemaVersion: SchemaVersion, JobID: request.JobID,
			CurrentVersion: request.CurrentVersion, TargetVersion: request.TargetVersion,
			Status: "completed", Message: "PalPanel " + request.TargetVersion + " 已恢复中断的完整包更新并通过健康检查",
		}, nil
	} else {
		cause := err
		_ = runSystemctl(ctx, config, "stop", "palpanel.service", "palpanel-sav-cli.service", "palpanel-palcalc.service")
		rollbackErr := switchCurrent(config.InstallRoot, previousDir)
		if rollbackErr == nil {
			rollbackErr = activateInstalledVersion(ctx, request, config, previousDir, request.CurrentVersion)
		}
		if rollbackErr == nil {
			_ = os.RemoveAll(targetDir)
			return rolledBackResult(request, true, cause), cause
		}
		combined := fmt.Errorf("%w; rollback failed: %v", cause, rollbackErr)
		return rolledBackResult(request, false, combined), combined
	}
}

func rolledBackResult(request Request, succeeded bool, cause error) Result {
	status := "failed"
	message := "更新失败，旧版本恢复未通过验证"
	if succeeded {
		status = "rolled_back"
		message = "更新失败，已恢复并验证旧版本"
	}
	return Result{
		SchemaVersion: SchemaVersion, JobID: request.JobID,
		CurrentVersion: request.CurrentVersion, TargetVersion: request.TargetVersion,
		Status: status, Message: message, Detail: cause.Error(),
		RollbackAttempted: true, RollbackSucceeded: succeeded,
	}
}

func releaseHTTPClient(config Config, proxyRaw string) (*http.Client, error) {
	if config.ReleaseClient != nil {
		return config.ReleaseClient, nil
	}
	client := trustedHTTPClient(20 * time.Second)
	if strings.TrimSpace(proxyRaw) == "" {
		return client, nil
	}
	return networkproxy.HTTPClient(client, proxyRaw, 20*time.Second)
}

func trustedHTTPClient(timeout time.Duration) *http.Client {
	client := &http.Client{Timeout: timeout}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many redirects")
		}
		if request.URL.Scheme != "https" || !trustedReleaseHost(request.URL.Hostname()) {
			return fmt.Errorf("untrusted release redirect: %s", request.URL)
		}
		return nil
	}
	return client
}

func trustedReleaseHost(host string) bool {
	switch strings.ToLower(host) {
	case "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return true
	default:
		return false
	}
}

func fetchOfficialArchiveSHA(ctx context.Context, client *http.Client, version, archiveName string) (string, error) {
	endpoint := "https://github.com/" + OfficialRepository + "/releases/download/" + url.PathEscape(version) + "/SHA256SUMS"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "PalPanel-external-updater")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("official SHA256SUMS returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxChecksumsBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(body)) > maxChecksumsBytes {
		return "", errors.New("official SHA256SUMS exceeds size limit")
	}
	checksums, err := parseChecksums(body, false)
	if err != nil {
		return "", err
	}
	sha, ok := checksums[archiveName]
	if !ok {
		return "", fmt.Errorf("official SHA256SUMS has no entry for %s", archiveName)
	}
	return sha, nil
}

func openRegularNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxArchiveBytes {
		file.Close()
		return nil, errors.New("archive is not a bounded regular file")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink != 1 {
		file.Close()
		return nil, errors.New("archive must not have multiple hard links")
	}
	return file, nil
}

func hashOpenFile(file *os.File, limit int64) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, limit+1))
	if err != nil {
		return "", err
	}
	if written > limit {
		return "", errors.New("file exceeds size limit")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func extractPackage(archive *os.File, destination, expectedRoot string) (string, error) {
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return "", err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	files := 0
	var total int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		files++
		if files > maxArchiveFiles {
			return "", errors.New("archive contains too many entries")
		}
		name := filepath.ToSlash(filepath.Clean(header.Name))
		if filepath.IsAbs(header.Name) || name == "." || name == ".." || strings.HasPrefix(name, "../") {
			return "", fmt.Errorf("unsafe archive path: %s", header.Name)
		}
		parts := strings.Split(name, "/")
		if len(parts) == 0 || parts[0] != expectedRoot {
			return "", fmt.Errorf("archive entry is outside expected package root: %s", header.Name)
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if !pathWithin(destination, target) {
			return "", fmt.Errorf("archive path escapes extraction root: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxArchiveBytes {
				return "", fmt.Errorf("invalid archive entry size: %s", header.Name)
			}
			total += header.Size
			if total > maxExtractedBytes {
				return "", errors.New("archive exceeds extracted size limit")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			mode := os.FileMode(header.Mode).Perm() & 0o755
			if mode == 0 {
				mode = 0o644
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return "", err
			}
			_, copyErr := io.CopyN(output, reader, header.Size)
			syncErr := output.Sync()
			closeErr := output.Close()
			if copyErr != nil || syncErr != nil || closeErr != nil {
				if copyErr != nil {
					return "", copyErr
				}
				if syncErr != nil {
					return "", syncErr
				}
				return "", closeErr
			}
		default:
			return "", fmt.Errorf("unsupported archive entry type for %s", header.Name)
		}
	}
	root := filepath.Join(destination, expectedRoot)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", errors.New("archive package root is missing")
	}
	return root, nil
}

func verifyPackageChecksums(root string) error {
	for _, path := range requiredPackageFiles {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return fmt.Errorf("required package file missing: %s", path)
		}
		if path != "checksums.txt" && !info.Mode().IsRegular() {
			return fmt.Errorf("required package path is not a regular file: %s", path)
		}
	}
	body, err := os.ReadFile(filepath.Join(root, "checksums.txt"))
	if err != nil {
		return err
	}
	checksums, err := parseChecksums(body, true)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("package contains symbolic link: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("package contains non-regular file: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "checksums.txt" {
			return nil
		}
		expected, ok := checksums[relative]
		if !ok {
			return fmt.Errorf("package file is not covered by checksums.txt: %s", relative)
		}
		actual, err := hashFile(path, maxArchiveBytes)
		if err != nil {
			return err
		}
		if !strings.EqualFold(actual, expected) {
			return fmt.Errorf("package checksum mismatch for %s", relative)
		}
		seen[relative] = true
		return nil
	})
	if err != nil {
		return err
	}
	for path := range checksums {
		if !seen[path] {
			return fmt.Errorf("checksums.txt references missing file: %s", path)
		}
	}
	return nil
}

func parseChecksums(body []byte, allowPaths bool) (map[string]string, error) {
	out := map[string]string{}
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 || len(parts[0]) != 64 {
			return nil, fmt.Errorf("invalid checksum line: %s", raw)
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			return nil, fmt.Errorf("invalid checksum digest: %s", parts[0])
		}
		name := strings.TrimPrefix(strings.TrimPrefix(parts[1], "*"), "./")
		clean := filepath.ToSlash(filepath.Clean(name))
		if clean == "." || filepath.IsAbs(name) || strings.HasPrefix(clean, "../") || (!allowPaths && strings.Contains(clean, "/")) {
			return nil, fmt.Errorf("unsafe checksum path: %s", name)
		}
		if _, exists := out[clean]; exists {
			return nil, fmt.Errorf("duplicate checksum path: %s", clean)
		}
		out[clean] = strings.ToLower(parts[0])
	}
	return out, nil
}

func hashFile(path string, limit int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, limit+1))
	if err != nil {
		return "", err
	}
	if written > limit {
		return "", errors.New("file exceeds size limit")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func probeBinary(ctx context.Context, binary, version string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, binary, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	if !strings.Contains(string(output), version) {
		return fmt.Errorf("version output does not contain %s: %s", version, strings.TrimSpace(string(output)))
	}
	return nil
}

func currentVersionDirectory(installRoot string) (string, error) {
	current := filepath.Join(installRoot, "current")
	info, err := os.Lstat(current)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", errors.New("current installation is not a symbolic link")
	}
	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", err
	}
	if !directChild(installRoot, resolved) {
		return "", errors.New("current installation is outside install root")
	}
	return filepath.Clean(resolved), nil
}

func directChild(root, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(child))
	return err == nil && relative != "." && relative != ".." && !strings.Contains(relative, string(os.PathSeparator))
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func hardenVersionTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not allowed in installed package: %s", path)
		}
		if err := os.Chown(path, 0, 0); err != nil && os.Geteuid() == 0 {
			return err
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o755)
		}
		mode := info.Mode().Perm()
		if mode&0o111 != 0 {
			mode = 0o755
		} else {
			mode = 0o644
		}
		return os.Chmod(path, mode)
	})
}

func backupUnits(systemdDir string) (map[string]unitBackup, error) {
	out := map[string]unitBackup{}
	for _, name := range managedUnits {
		path := filepath.Join(systemdDir, name)
		body, exists, err := readOptionalFile(path)
		if err != nil {
			return nil, err
		}
		mode := os.FileMode(0o644)
		if exists {
			info, err := os.Stat(path)
			if err != nil {
				return nil, err
			}
			mode = info.Mode().Perm()
		}
		out[name] = unitBackup{Body: body, Mode: mode, Exists: exists}
	}
	return out, nil
}

func installUnits(versionDir string, config Config) error {
	if err := os.MkdirAll(config.SystemdDir, 0o755); err != nil {
		return err
	}
	for _, name := range managedUnits {
		source := filepath.Join(versionDir, "systemd", name)
		body, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		text := strings.ReplaceAll(string(body), "/opt/palpanel", config.InstallRoot)
		text = strings.ReplaceAll(text, "/etc/palpanel", config.EtcDir)
		text = strings.ReplaceAll(text, "/var/lib/palpanel", config.DataDir)
		text = strings.ReplaceAll(text, "/etc/systemd/system", config.SystemdDir)
		text = strings.ReplaceAll(text, "/usr/libexec", config.LibexecDir)
		text = strings.ReplaceAll(text, "User=palpanel", "User="+config.ServiceUser)
		text = strings.ReplaceAll(text, "Group=palpanel", "Group="+config.ServiceUser)
		if err := writeAtomic(filepath.Join(config.SystemdDir, name), []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func restoreUnits(systemdDir string, backups map[string]unitBackup) error {
	var names []string
	for name := range backups {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		backup := backups[name]
		path := filepath.Join(systemdDir, name)
		if !backup.Exists {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if err := writeAtomic(path, backup.Body, backup.Mode); err != nil {
			return err
		}
	}
	return nil
}

func switchCurrent(installRoot, target string) error {
	if !directChild(installRoot, target) {
		return errors.New("current target must be a direct install-root child")
	}
	temporary := filepath.Join(installRoot, ".current.new")
	_ = os.Remove(temporary)
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(installRoot, "current"))
}

func copyAtomic(source, destination string, mode os.FileMode) error {
	body, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return writeAtomic(destination, body, mode)
}

func writeAtomic(path string, body []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".palpanel-updater-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(body); err != nil {
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
	return os.Rename(temporary, path)
}

func readOptionalFile(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("path is not a regular file: %s", path)
	}
	body, err := os.ReadFile(path)
	return body, err == nil, err
}

func restoreOptionalFile(path string, body []byte, existed bool, mode os.FileMode) error {
	if !existed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeAtomic(path, body, mode)
}

func runSystemctl(ctx context.Context, config Config, args ...string) error {
	output, err := config.RunCommand(ctx, config.SystemctlPath, args...)
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func waitForPanel(ctx context.Context, config Config, port int, version string) error {
	deadline := time.Now().Add(config.HealthTimeout)
	readyURL := "http://127.0.0.1:" + strconv.Itoa(port) + "/api/ready"
	healthURL := "http://127.0.0.1:" + strconv.Itoa(port) + "/api/health"
	stable := 0
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ready := endpointOK(ctx, config.HTTPClient, readyURL, "ready", "")
		healthy := endpointOK(ctx, config.HTTPClient, healthURL, "ok", version)
		if ready && healthy {
			stable++
			if stable >= 3 {
				return nil
			}
		} else {
			stable = 0
		}
		time.Sleep(config.PollInterval)
	}
	return fmt.Errorf("panel did not report ready version %s before timeout", version)
}

func endpointOK(ctx context.Context, client *http.Client, endpoint, status, version string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	var envelope map[string]any
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&envelope); err != nil {
		return false
	}
	data := envelope
	if nested, ok := envelope["data"].(map[string]any); ok {
		data = nested
	}
	if fmt.Sprint(data["status"]) != status {
		return false
	}
	return version == "" || fmt.Sprint(data["version"]) == version
}

func cleanupOperation(archivePath string) error {
	operation := filepath.Dir(filepath.Clean(archivePath))
	if filepath.Base(filepath.Dir(operation)) != "operations" {
		return nil
	}
	return os.RemoveAll(operation)
}
