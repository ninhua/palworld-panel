package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/networkproxy"
	"palpanel/internal/steamcmd"
)

type Runner struct {
	cfg     appconfig.Config
	runFunc func(context.Context, ...string) ([]byte, error)
}

type ContainerStatus struct {
	Exists             bool   `json:"exists"`
	Status             string `json:"status"`
	LifecycleAvailable bool   `json:"lifecycle_available"`
	OOMKilled          bool   `json:"oom_killed"`
	ExitCode           int    `json:"exit_code"`
	RestartCount       int    `json:"restart_count"`
	StartedAt          string `json:"started_at,omitempty"`
	FinishedAt         string `json:"finished_at,omitempty"`
}

type downloadProxySession struct {
	bridge      *networkproxy.Bridge
	proxyURL    string
	proxyScheme string
	environment []string
	hostNetwork bool
}

type ProxyContainerTestResult struct {
	OK            bool
	Latency       time.Duration
	FailureStage  string
	Diagnostic    string
	HostNetwork   bool
	BridgeEnabled bool
}

const (
	wineBaseImageBuildArg = "PALPANEL_WINE_BASE_IMAGE"
	wineDLLOverrides      = "dwmapi=n,b;d3d9=n,b"
	workshopSteamHomePath = "/root/Steam"
)

func NewRunner(cfg appconfig.Config) Runner {
	return Runner{cfg: cfg}
}

func (r Runner) BuildImage(ctx context.Context) error {
	candidates := runnerBaseImageCandidates(r.cfg.DockerRunnerBaseImage, r.cfg.DockerRunnerBaseImageMirrors)
	attemptErrors := make([]string, 0, len(candidates))
	for index, baseImage := range candidates {
		err := r.buildImageWithBase(ctx, baseImage)
		if err == nil {
			return nil
		}
		attemptErrors = append(attemptErrors, fmt.Sprintf("%s: %s", baseImage, compactDockerError(err.Error())))
		if index == 0 && !dockerBaseImagePullFailure(err) {
			return err
		}
	}
	return fmt.Errorf(
		"build wine runner image failed: unable to pull Docker base image after trying %d source(s). "+
			"Try configuring Docker Hub mirror acceleration in Setup > Advanced settings, or set PALPANEL_DOCKER_RUNNER_BASE_IMAGE_MIRRORS. Attempts: %s",
		len(attemptErrors),
		strings.Join(attemptErrors, " | "),
	)
}

func (r Runner) buildImageWithBase(ctx context.Context, baseImage string) error {
	session, err := r.startDownloadProxy()
	if err != nil {
		return fmt.Errorf("prepare Wine image download proxy: %w", err)
	}
	defer session.close()
	args := []string{"build", "-t", r.cfg.DockerImage, "-f", filepath.Join(r.cfg.RunnerDir, "Dockerfile")}
	args = append(args, session.buildArgs()...)
	args = append(args, "--build-arg", wineBaseImageBuildArg+"="+baseImage)
	args = append(args, r.cfg.RunnerDir)
	_, err = r.runWithEnvironment(ctx, session.environment, args...)
	if err != nil && session.proxyURL != "" && !dockerBaseImagePullFailure(err) {
		return session.wrapError("wine_image_build_download", err)
	}
	return err
}

func runnerBaseImageCandidates(baseImage string, mirrors []string) []string {
	baseImage = strings.TrimSpace(baseImage)
	if baseImage == "" {
		baseImage = appconfig.DefaultDockerRunnerBaseImage
	}
	out := []string{baseImage}
	dockerHubRef := dockerHubImageRef(baseImage)
	if dockerHubRef == "" {
		return dedupeStrings(out)
	}
	for _, mirror := range mirrors {
		mirror = normalizeImageMirrorPrefix(mirror)
		if mirror == "" {
			continue
		}
		if strings.Contains(mirror, dockerHubRef) {
			out = append(out, mirror)
			continue
		}
		out = append(out, mirror+"/"+dockerHubRef)
	}
	return dedupeStrings(out)
}

func dockerHubImageRef(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	parts := strings.Split(image, "/")
	if len(parts) == 1 {
		return "library/" + image
	}
	first := parts[0]
	if first == "docker.io" || first == "index.docker.io" || first == "registry-1.docker.io" {
		if len(parts) == 2 {
			return "library/" + parts[1]
		}
		return strings.Join(parts[1:], "/")
	}
	if strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost" {
		return ""
	}
	return image
}

func normalizeImageMirrorPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return ""
	}
	if parsed, err := url.Parse(prefix); err == nil && parsed.Host != "" {
		prefix = parsed.Host + strings.TrimRight(parsed.Path, "/")
	}
	prefix = strings.Trim(prefix, "/")
	return prefix
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func dockerBaseImagePullFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	needles := []string{
		"load metadata for",
		"failed to resolve source metadata",
		"failed to do request",
		"registry-1.docker.io",
		"docker.io",
		"deadlineexceeded",
		"context deadline exceeded",
		"i/o timeout",
		"tls handshake timeout",
		"client.timeout",
		"dial tcp",
		"no route to host",
		"connection refused",
		"connection reset",
		"temporary failure",
		"lookup ",
		"unexpected eof",
	}
	for _, needle := range needles {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func compactDockerError(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const limit = 900
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
}

func (r Runner) InstallOrUpdate(ctx context.Context) error {
	session, err := r.startDownloadProxy()
	if err != nil {
		return err
	}
	defer session.close()
	args := []string{
		"run", "--rm",
		"--add-host", "host.docker.internal:host-gateway",
		"-v", volume(r.cfg.ServerDirectory(), "/data/server"),
		"-v", volume(r.cfg.WinePrefixDir, "/data/wineprefix"),
	}
	args = append(args, session.containerArgs()...)
	args = append(args, hostOwnerEnvArgs()...)
	args = append(args, r.cfg.DockerImage, "install")
	_, err = r.runWithEnvironment(ctx, session.environment, args...)
	if err != nil {
		return session.wrapError("steamcmd_install_update", err)
	}
	return nil
}

func (r Runner) ImageExists(ctx context.Context) (bool, error) {
	_, err := r.run(ctx, "image", "inspect", r.cfg.DockerImage)
	if err == nil {
		return true, nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no such image") || strings.Contains(msg, "not found") {
		return false, nil
	}
	return false, err
}

func (r Runner) AppInfo(ctx context.Context) (string, error) {
	exists, err := r.ImageExists(ctx)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("wine runner image is not built; run install or bootstrap before checking remote version")
	}
	session, err := r.startDownloadProxy()
	if err != nil {
		return "", err
	}
	defer session.close()
	args := []string{
		"run", "--rm",
		"--add-host", "host.docker.internal:host-gateway",
	}
	args = append(args, session.containerArgs()...)
	args = append(args, r.cfg.DockerImage, "appinfo")
	out, err := r.runWithEnvironment(ctx, session.environment, args...)
	if err != nil {
		return "", session.wrapError("steamcmd_appinfo", err)
	}
	return string(out), nil
}

func (r Runner) DownloadWorkshop(ctx context.Context, itemID string) error {
	return r.DownloadWorkshopTo(ctx, itemID, r.cfg.WorkshopModsDir())
}

func (r Runner) DownloadWorkshopTo(ctx context.Context, itemID, destinationRoot string, accountNames ...string) error {
	session, err := r.startDownloadProxy()
	if err != nil {
		return err
	}
	defer session.close()
	args := []string{
		"run", "--rm",
		"--add-host", "host.docker.internal:host-gateway",
		"-e", "PALPANEL_WORKSHOP_APP_ID=" + r.cfg.WorkshopAppID,
		"-v", volume(destinationRoot, "/data/workshop"),
	}
	accountName := ""
	if len(accountNames) > 0 {
		accountName = strings.TrimSpace(accountNames[0])
	}
	if accountName != "" {
		if err := r.ensureWorkshopSteamCMDConfigDir(); err != nil {
			return err
		}
		args = append(args, "-v", volume(r.cfg.WorkshopSteamCMDConfigDir(), workshopSteamHomePath))
	}
	args = append(args, session.containerArgs()...)
	args = append(args, hostOwnerEnvArgs()...)
	args = append(args, r.cfg.DockerImage, "workshop", itemID)
	if accountName != "" {
		args = append(args, accountName)
	}
	out, err := r.runWithEnvironment(ctx, session.environment, args...)
	if err != nil {
		combined := append(append([]byte(nil), out...), []byte("\n"+err.Error())...)
		if authErr := steamcmd.CachedLoginFailure(combined); authErr != nil {
			return authErr
		}
	}
	if err != nil {
		return session.wrapError("steamcmd_workshop_download", err)
	}
	return nil
}

// AuthenticateWorkshop performs a non-interactive SteamCMD login using a
// private, short-lived runscript. Secrets are mounted into the container and
// never included in Docker process arguments.
func (r Runner) AuthenticateWorkshop(ctx context.Context, request steamcmd.LoginRequest) ([]byte, error) {
	exists, err := r.ImageExists(ctx)
	if err != nil {
		return nil, fmt.Errorf("check Docker/Wine SteamCMD runner: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("Docker/Wine SteamCMD runner image is not built")
	}
	if err := r.ensureWorkshopSteamCMDConfigDir(); err != nil {
		return nil, err
	}
	scriptPath, cleanup, err := r.writeWorkshopLoginScript(request)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	session, err := r.startDownloadProxy()
	if err != nil {
		return nil, err
	}
	defer session.close()
	const containerScript = "/run/palpanel/steam-login.txt"
	args := []string{
		"run", "--rm",
		"--add-host", "host.docker.internal:host-gateway",
		"-v", volume(r.cfg.WorkshopSteamCMDConfigDir(), workshopSteamHomePath),
		"-v", volume(scriptPath, containerScript) + ":ro",
	}
	args = append(args, session.containerArgs()...)
	args = append(args, hostOwnerEnvArgs()...)
	args = append(args, r.cfg.DockerImage, "steam-auth-runscript", containerScript)
	out, err := r.runSensitiveWithEnvironment(ctx, session.environment, args...)
	if err != nil {
		return out, session.wrapError("steamcmd_login", err)
	}
	return out, nil
}

func (r Runner) writeWorkshopLoginScript(request steamcmd.LoginRequest) (string, func(), error) {
	directory := filepath.Dir(r.cfg.SteamWorkshopCredentialsPath())
	if r.cfg.RuntimeRoot != "" {
		if err := r.cfg.ValidateManagedPath(directory, false); err != nil {
			return "", func() {}, fmt.Errorf("validate Steam credential directory: %w", err)
		}
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", func() {}, fmt.Errorf("create Steam credential directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", func() {}, fmt.Errorf("secure Steam credential directory: %w", err)
	}
	script, err := os.CreateTemp(directory, "steamcmd-docker-login-*.txt")
	if err != nil {
		return "", func() {}, fmt.Errorf("create Docker SteamCMD login script: %w", err)
	}
	path := script.Name()
	cleanup := func() {
		_ = script.Close()
		_ = os.Remove(path)
	}
	if err := script.Chmod(0o600); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("secure Docker SteamCMD login script: %w", err)
	}
	login := "login " + steamScriptArgument(request.AccountName) + " " + steamScriptArgument(request.Password)
	if request.SteamGuardCode != "" {
		login += " " + steamScriptArgument(request.SteamGuardCode)
	}
	body := "@ShutdownOnFailedCommand 1\n@NoPromptForPassword 1\n" + login + "\nquit\n"
	if _, err := script.WriteString(body); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("write Docker SteamCMD login script: %w", err)
	}
	if err := script.Sync(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("sync Docker SteamCMD login script: %w", err)
	}
	if err := script.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close Docker SteamCMD login script: %w", err)
	}
	return path, cleanup, nil
}

func steamScriptArgument(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

// VerifyWorkshopLogin probes the persisted SteamCMD cache without ever
// prompting for, accepting, or logging a password or Steam Guard code.
func (r Runner) VerifyWorkshopLogin(ctx context.Context, accountName string) (bool, error) {
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return false, nil
	}
	if err := r.ensureWorkshopSteamCMDConfigDir(); err != nil {
		return false, err
	}
	session, err := r.startDownloadProxy()
	if err != nil {
		return false, err
	}
	defer session.close()
	args := []string{
		"run", "--rm",
		"--add-host", "host.docker.internal:host-gateway",
		"-v", volume(r.cfg.WorkshopSteamCMDConfigDir(), workshopSteamHomePath),
	}
	args = append(args, session.containerArgs()...)
	args = append(args, hostOwnerEnvArgs()...)
	args = append(args, r.cfg.DockerImage, "steam-auth-verify", accountName)
	_, err = r.runWithEnvironment(ctx, session.environment, args...)
	if err == nil {
		return true, nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "steam authentication cache is missing or expired") ||
		strings.Contains(message, "no cached credentials") ||
		strings.Contains(message, "password required") ||
		strings.Contains(message, "account logon denied") {
		return false, nil
	}
	return false, session.wrapError("steamcmd_login_verify", err)
}

func (r Runner) WorkshopCredentialsSecure() bool {
	info, err := os.Stat(r.cfg.WorkshopSteamCMDConfigDir())
	return err == nil && info.IsDir() && info.Mode().Perm()&0o077 == 0
}

func (r Runner) ensureWorkshopSteamCMDConfigDir() error {
	path := r.cfg.WorkshopSteamCMDConfigDir()
	if strings.TrimSpace(r.cfg.DataDir) == "" {
		return fmt.Errorf("SteamCMD Workshop cache requires PALPANEL_DATA_DIR")
	}
	if r.cfg.RuntimeRoot != "" {
		if err := r.cfg.ValidateManagedPath(path, false); err != nil {
			return fmt.Errorf("validate SteamCMD Workshop cache: %w", err)
		}
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create SteamCMD Workshop cache: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure SteamCMD Workshop cache: %w", err)
	}
	return nil
}

func (r Runner) Start(ctx context.Context) error {
	return r.StartWithArgs(ctx, nil)
}

func (r Runner) StartWithArgs(ctx context.Context, serverArgs []string) error {
	status, err := r.Status(ctx)
	if err != nil {
		return err
	}
	if status.Exists {
		if status.Status == "running" {
			return nil
		}
		if err := r.removeContainer(ctx); err != nil {
			return err
		}
	}

	gamePort := palServerPort(serverArgs, r.cfg.GamePort)
	args := []string{
		"run", "-d",
		"--name", r.cfg.DockerContainer,
		"--restart", "unless-stopped",
		"--log-opt", "max-size=20m",
		"--log-opt", "max-file=5",
		"-e", "WINEDLLOVERRIDES=" + wineDLLOverrides,
		"-v", volume(r.cfg.ServerDirectory(), "/data/server"),
		"-v", volume(r.cfg.WinePrefixDir, "/data/wineprefix"),
		"-v", volume(r.cfg.LogsDir, "/data/logs"),
		"-p", fmt.Sprintf("%d:%d/udp", gamePort, gamePort),
		"-p", fmt.Sprintf("%d:27015/udp", r.cfg.QueryPort),
		"-p", fmt.Sprintf("127.0.0.1:%d:%d/tcp", r.cfg.RESTPort, r.cfg.RESTPort),
		"-p", fmt.Sprintf("127.0.0.1:%d:%d/tcp", r.cfg.EffectiveRCONPort(), r.cfg.EffectiveRCONPort()),
		"-p", fmt.Sprintf("127.0.0.1:%d:%d/tcp", r.cfg.EffectivePalDefenderRESTPort(), r.cfg.EffectivePalDefenderRESTPort()),
	}
	args = append(args, hostUserRunArgs()...)
	args = append(args, r.cfg.DockerImage, "start")
	args = append(args, serverArgs...)
	_, err = r.run(ctx, args...)
	return err
}

func (r Runner) Stop(ctx context.Context) error {
	status, err := r.Status(ctx)
	if err != nil {
		return err
	}
	if !status.Exists {
		return nil
	}
	_, err = r.run(ctx, "stop", r.cfg.DockerContainer)
	return err
}

func (r Runner) Restart(ctx context.Context) error {
	return r.RestartWithArgs(ctx, nil)
}

func (r Runner) RestartWithArgs(ctx context.Context, serverArgs []string) error {
	status, err := r.Status(ctx)
	if err != nil {
		return err
	}
	if !status.Exists {
		return r.StartWithArgs(ctx, serverArgs)
	}
	if status.Status == "running" {
		if _, err := r.run(ctx, "stop", r.cfg.DockerContainer); err != nil {
			return err
		}
	}
	if err := r.removeContainer(ctx); err != nil {
		return err
	}
	return r.StartWithArgs(ctx, serverArgs)
}

func (r Runner) Logs(ctx context.Context, tail int) (string, error) {
	if tail <= 0 || tail > 5000 {
		tail = 200
	}
	out, err := r.run(ctx, "logs", "--tail", fmt.Sprintf("%d", tail), r.cfg.DockerContainer)
	return string(out), err
}

func (r Runner) Status(ctx context.Context) (ContainerStatus, error) {
	inspectContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := r.run(inspectContext, "inspect", r.cfg.DockerContainer)
	if err != nil {
		msg := strings.ToLower(err.Error() + string(out))
		if strings.Contains(msg, "no such object") {
			return ContainerStatus{Exists: false, Status: "missing"}, nil
		}
		return ContainerStatus{}, err
	}
	return parseContainerStatus(out)
}

func parseContainerStatus(raw []byte) (ContainerStatus, error) {
	var payload []struct {
		RestartCount int `json:"RestartCount"`
		State        *struct {
			Status     string `json:"Status"`
			OOMKilled  bool   `json:"OOMKilled"`
			ExitCode   int    `json:"ExitCode"`
			StartedAt  string `json:"StartedAt"`
			FinishedAt string `json:"FinishedAt"`
		} `json:"State"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ContainerStatus{}, fmt.Errorf("decode docker inspect JSON: %w", err)
	}
	if len(payload) != 1 {
		return ContainerStatus{}, fmt.Errorf("docker inspect returned %d objects", len(payload))
	}
	result := ContainerStatus{Exists: true, Status: "unknown"}
	if payload[0].State == nil {
		return result, nil
	}
	state := payload[0].State
	result.Status = strings.TrimSpace(state.Status)
	if result.Status == "" {
		result.Status = "unknown"
	}
	result.LifecycleAvailable = true
	result.OOMKilled = state.OOMKilled
	result.ExitCode = state.ExitCode
	result.RestartCount = payload[0].RestartCount
	result.StartedAt = normalizeContainerTimestamp(state.StartedAt)
	result.FinishedAt = normalizeContainerTimestamp(state.FinishedAt)
	return result, nil
}

func normalizeContainerTimestamp(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Year() <= 1 {
		return ""
	}
	return value
}

func (r Runner) run(ctx context.Context, args ...string) ([]byte, error) {
	if r.runFunc != nil {
		return r.runFunc(ctx, args...)
	}
	return r.runWithEnvironment(ctx, os.Environ(), args...)
}

func (r Runner) runWithEnvironment(ctx context.Context, environment []string, args ...string) ([]byte, error) {
	return runCommandWithEnvironment(ctx, r.cfg.DockerBinary, environment, args...)
}

func (r Runner) runSensitive(ctx context.Context, args ...string) ([]byte, error) {
	return r.runSensitiveWithEnvironment(ctx, r.commandEnvironment(), args...)
}

func (r Runner) runSensitiveWithEnvironment(ctx context.Context, environment []string, args ...string) ([]byte, error) {
	binary := dockerBinary(r.cfg.DockerBinary)
	out, err := runDockerCommand(ctx, binary, args, false, environment)
	if err == nil {
		return out, nil
	}
	if dockerPermissionDenied(err, out) && canUseDockerGroupShell() {
		out, err = runDockerCommand(ctx, binary, args, true, environment)
		if err == nil {
			return out, nil
		}
	}
	return out, fmt.Errorf("Docker credential command failed: %w", err)
}

func (r Runner) TestInstallProxy(ctx context.Context, target string) (ProxyContainerTestResult, error) {
	session, err := r.startDownloadProxy()
	if err != nil {
		return ProxyContainerTestResult{FailureStage: "bridge_start"}, err
	}
	defer session.close()
	result := ProxyContainerTestResult{
		HostNetwork: session.hostNetwork, BridgeEnabled: session.bridge != nil,
		Diagnostic: session.diagnostic("docker_container_test"),
	}
	if session.bridge == nil {
		result.FailureStage = "bridge_disabled"
		return result, fmt.Errorf("install proxy bridge is not enabled")
	}
	exists, err := r.ImageExists(ctx)
	if err != nil || !exists {
		result.FailureStage = "runner_image_unavailable"
		return result, fmt.Errorf("Docker/Wine runner image is unavailable; build it after configuring the Docker daemon mirror or proxy")
	}
	args := []string{"run", "--rm"}
	args = append(args, session.containerArgs()...)
	args = append(args, r.cfg.DockerImage, "proxy-test", target)
	started := time.Now()
	_, err = r.runWithEnvironment(ctx, session.environment, args...)
	result.Latency = time.Since(started)
	if err != nil {
		result.FailureStage = "docker_container_proxy"
		return result, session.wrapError(result.FailureStage, err)
	}
	result.OK = true
	return result, nil
}

func RunCommand(ctx context.Context, binary string, args ...string) ([]byte, error) {
	return runCommandWithEnvironment(ctx, binary, os.Environ(), args...)
}

func runCommandWithEnvironment(ctx context.Context, binary string, environment []string, args ...string) ([]byte, error) {
	binary = dockerBinary(binary)
	out, err := runDockerCommand(ctx, binary, args, false, environment)
	if err == nil {
		return out, nil
	}
	if dockerPermissionDenied(err, out) && canUseDockerGroupShell() {
		sgOut, sgErr := runDockerCommand(ctx, binary, args, true, environment)
		if sgErr == nil {
			return sgOut, nil
		}
		return sgOut, fmt.Errorf("docker %s via sg docker failed after direct Docker socket permission was denied: %w: %s", strings.Join(args, " "), sgErr, compactDockerError(strings.TrimSpace(string(sgOut))))
	}
	return out, fmt.Errorf("docker %s failed: %w: %s", strings.Join(args, " "), err, compactDockerError(strings.TrimSpace(string(out))))
}

func runDockerCommand(ctx context.Context, binary string, args []string, useDockerGroup bool, environment []string) ([]byte, error) {
	command := binary
	commandArgs := args
	if useDockerGroup {
		command = "sg"
		commandArgs = []string{"docker", "-c", dockerShellCommand(binary, args)}
	}
	cmd := exec.CommandContext(ctx, command, commandArgs...)
	cmd.Env = dockerEnv(environment)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.Bytes(), err
}

func dockerPermissionDenied(err error, out []byte) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error() + " " + string(out))
	return strings.Contains(msg, "permission denied") &&
		(strings.Contains(msg, "docker.sock") || strings.Contains(msg, "docker api") || strings.Contains(msg, "var/run/docker"))
}

func canUseDockerGroupShell() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if _, err := exec.LookPath("sg"); err != nil {
		return false
	}
	current, err := user.Current()
	if err != nil {
		return false
	}
	group, err := user.LookupGroup("docker")
	if err != nil {
		return false
	}
	ids, err := current.GroupIds()
	if err != nil {
		return false
	}
	for _, id := range ids {
		if id == group.Gid {
			return true
		}
	}
	return false
}

func dockerShellCommand(binary string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(binary))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (r Runner) removeContainer(ctx context.Context) error {
	_, err := r.run(ctx, "rm", r.cfg.DockerContainer)
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no such container") || strings.Contains(msg, "no such object") {
		return nil
	}
	return err
}

func dockerEnv(base []string) []string {
	pathExtra := `C:\Program Files\Docker\Docker\resources\bin`
	foundPath := false
	for i, item := range base {
		if strings.HasPrefix(strings.ToUpper(item), "PATH=") {
			foundPath = true
			if !strings.Contains(strings.ToLower(item), strings.ToLower(pathExtra)) {
				base[i] = item + ";" + pathExtra
			}
		}
	}
	if !foundPath {
		base = append(base, "PATH="+pathExtra)
	}
	return base
}

func volume(host, container string) string {
	return filepath.Clean(host) + ":" + container
}

func dockerBinary(configured string) string {
	if configured != "" {
		if resolved, err := exec.LookPath(configured); err == nil {
			return resolved
		}
		if strings.ContainsAny(configured, `\/`) {
			return configured
		}
	}
	defaultPath := `C:\Program Files\Docker\Docker\resources\bin\docker.exe`
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath
	}
	return configured
}

func containerProxyEnvArgs() []string {
	var args []string
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		if v := os.Getenv(key); v != "" {
			args = append(args, "-e", key)
		}
	}
	return args
}

func (r Runner) containerProxyEnvArgs() []string {
	var args []string
	for _, key := range configuredProxyNames(r.commandEnvironment()) {
		args = append(args, "-e", key)
	}
	return args
}

func (r Runner) commandEnvironment() []string {
	environment := append([]string(nil), os.Environ()...)
	rawProxy, err := networkproxy.New(r.cfg).InstallProxyURL()
	if err != nil || rawProxy == "" {
		return environment
	}
	proxyValue := proxyForContainer(rawProxy)
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		environment = setEnvironmentValue(environment, key, proxyValue)
	}
	return environment
}

func (r Runner) startDownloadProxy() (*downloadProxySession, error) {
	session := &downloadProxySession{environment: r.commandEnvironment()}
	rawProxy, err := networkproxy.New(r.cfg).InstallProxyURL()
	if err != nil {
		return nil, err
	}
	if rawProxy == "" || runtime.GOOS != "linux" {
		return session, nil
	}
	bridge, err := networkproxy.StartBridge(rawProxy)
	if err != nil {
		return nil, err
	}
	parsed, _ := url.Parse(rawProxy)
	session.bridge = bridge
	session.proxyURL = "http://" + bridge.Address()
	session.proxyScheme = strings.ToLower(parsed.Scheme)
	session.environment = removeProxyEnvironment(os.Environ())
	session.hostNetwork = true
	return session, nil
}

func (s *downloadProxySession) close() {
	if s != nil && s.bridge != nil {
		_ = s.bridge.Close()
	}
}

func (s *downloadProxySession) containerArgs() []string {
	if s == nil {
		return nil
	}
	if s.proxyURL == "" {
		var args []string
		for _, key := range configuredProxyNames(s.environment) {
			args = append(args, "-e", key)
		}
		return args
	}
	args := []string{"--network", "host"}
	for _, key := range proxyEnvironmentNames() {
		args = append(args, "-e", key+"="+s.proxyURL)
	}
	return args
}

func (s *downloadProxySession) buildArgs() []string {
	if s == nil {
		return nil
	}
	if s.proxyURL == "" {
		var args []string
		for _, key := range configuredProxyNames(s.environment) {
			args = append(args, "--build-arg", key)
		}
		return args
	}
	args := []string{"--network", "host"}
	for _, key := range proxyEnvironmentNames() {
		args = append(args, "--build-arg", key+"="+s.proxyURL)
	}
	return args
}

func (s *downloadProxySession) diagnostic(stage string) string {
	if s == nil || s.bridge == nil {
		return "proxy=disabled host_network=false bridge=false stage=" + stage
	}
	return fmt.Sprintf("proxy=%s host_network=%t bridge=true stage=%s", s.proxyScheme, s.hostNetwork, stage)
}

func (s *downloadProxySession) wrapError(stage string, err error) error {
	if err == nil || s == nil || s.bridge == nil {
		return err
	}
	return fmt.Errorf("%w (%s)", err, s.diagnostic(stage))
}

func proxyEnvironmentNames() []string {
	return []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"}
}

func removeProxyEnvironment(environment []string) []string {
	blocked := map[string]bool{}
	for _, name := range proxyEnvironmentNames() {
		blocked[name] = true
	}
	out := make([]string, 0, len(environment))
	for _, item := range environment {
		name, _, ok := strings.Cut(item, "=")
		if ok && blocked[name] {
			continue
		}
		out = append(out, item)
	}
	return out
}

func configuredProxyNames(environment []string) []string {
	values := environmentMap(environment)
	var names []string
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		if values[key] != "" {
			names = append(names, key)
		}
	}
	return names
}

func environmentMap(environment []string) map[string]string {
	values := make(map[string]string, len(environment))
	for _, item := range environment {
		name, value, ok := strings.Cut(item, "=")
		if ok {
			values[name] = value
		}
	}
	return values
}

func setEnvironmentValue(environment []string, name, value string) []string {
	prefix := name + "="
	for index, item := range environment {
		if strings.HasPrefix(item, prefix) {
			environment[index] = prefix + value
			return environment
		}
	}
	return append(environment, prefix+value)
}

func hostOwnerEnvArgs() []string {
	if runtime.GOOS == "windows" {
		return nil
	}
	uid := os.Getuid()
	gid := os.Getgid()
	if uid < 0 || gid < 0 {
		return nil
	}
	return []string{
		"-e", fmt.Sprintf("PALPANEL_HOST_UID=%d", uid),
		"-e", fmt.Sprintf("PALPANEL_HOST_GID=%d", gid),
	}
}

func hostUserRunArgs() []string {
	if runtime.GOOS == "windows" {
		return nil
	}
	uid := os.Getuid()
	gid := os.Getgid()
	if uid < 0 || gid < 0 {
		return nil
	}
	return []string{
		"--user", fmt.Sprintf("%d:%d", uid, gid),
		"-e", "HOME=/data/wineprefix",
	}
}

func proxyForContainer(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		host = u.Hostname()
		port = u.Port()
	}
	if host == "127.0.0.1" || host == "localhost" || host == "::1" {
		if u.Scheme == "socks5" || u.Scheme == "socks5h" {
			u.Scheme = "socks5h"
		}
		if port != "" {
			u.Host = net.JoinHostPort("host.docker.internal", port)
		} else {
			u.Host = "host.docker.internal"
		}
	}
	return u.String()
}

func palServerPort(serverArgs []string, fallback int) int {
	for _, arg := range serverArgs {
		value, ok := strings.CutPrefix(arg, "-port=")
		if !ok {
			continue
		}
		var port int
		if _, err := fmt.Sscanf(value, "%d", &port); err == nil && port > 0 && port <= 65535 {
			return port
		}
	}
	return fallback
}

// PauseForCrashLoop disables Docker's restart policy before stopping the
// container. The ordering is critical: stopping first can race with
// unless-stopped and immediately spawn another PalServer process.
func (r Runner) PauseForCrashLoop(ctx context.Context) error {
	status, err := r.Status(ctx)
	if err != nil {
		return err
	}
	if !status.Exists {
		return nil
	}
	if _, err := r.run(ctx, "update", "--restart=no", r.cfg.DockerContainer); err != nil {
		return fmt.Errorf("disable Docker restart policy: %w", err)
	}
	if status.Status == "running" || status.Status == "restarting" || status.Status == "starting" {
		if _, err := r.run(ctx, "stop", "--time", "15", r.cfg.DockerContainer); err != nil {
			return fmt.Errorf("stop crash-looping container: %w", err)
		}
	}
	return nil
}
