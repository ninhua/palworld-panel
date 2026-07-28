package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"palpanel/internal/appconfig"
	"palpanel/internal/db"
	"palpanel/internal/docker"
	"palpanel/internal/id"
	"palpanel/internal/palconfig"
	"palpanel/internal/palrest"
	"palpanel/internal/server"
)

type Manager struct {
	cfg          appconfig.Config
	store        *db.Store
	server       Server
	palrest      palrest.Client
	run          func(context.Context, string, ...string) ([]byte, error)
	dial         func(string, string, time.Duration) (net.Conn, error)
	diskUsage    func(string) (int64, int64, error)
	processStats func(context.Context, int) (ProcessStats, error)
	hostMemory   HostMemoryCollector
	goos         string
	now          func() time.Time
}

type Server interface {
	Status(context.Context) (server.Status, error)
}

type Snapshot struct {
	Sample db.MonitorSample `json:"sample"`
}

type ProcessStats struct {
	CPUPercent       float64
	MemoryUsageBytes int64
	ProcessCount     int
}

func New(cfg appconfig.Config, store *db.Store, serverManager Server, restClient palrest.Client) Manager {
	return Manager{
		cfg: cfg, store: store, server: serverManager, palrest: restClient,
		run: runCommand, dial: net.DialTimeout, diskUsage: platformDiskUsage, processStats: platformProcessTreeStats,
		hostMemory: newPlatformHostMemoryCollector(),
		goos:       runtime.GOOS, now: time.Now,
	}
}

func (m Manager) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = m.Sample(ctx)
		_ = m.Prune(ctx)
		ticker := time.NewTicker(15 * time.Second)
		pruneTicker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		defer pruneTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = m.Sample(ctx)
			case <-pruneTicker.C:
				_ = m.Prune(ctx)
			}
		}
	}()
	return done
}

func (m Manager) Snapshot(ctx context.Context) (Snapshot, error) {
	samples, err := m.store.ListMonitorSamples(ctx, 1)
	if err != nil {
		return Snapshot{}, err
	}
	if len(samples) == 0 {
		sample, err := m.Sample(ctx)
		if err != nil {
			return Snapshot{}, err
		}
		return Snapshot{Sample: sample}, nil
	}
	return Snapshot{Sample: samples[len(samples)-1]}, nil
}

func (m Manager) History(ctx context.Context, limit int) ([]db.MonitorSample, error) {
	return m.store.ListMonitorSamples(ctx, limit)
}

func (m Manager) Sample(ctx context.Context) (db.MonitorSample, error) {
	sample := db.MonitorSample{ID: id.New("mon"), CreatedAt: m.currentTime().UTC().Format(time.RFC3339Nano)}
	m.fillHostMemory(ctx, &sample)
	runtimeMode := ""
	status, err := m.server.Status(ctx)
	if err != nil {
		sample.UnavailableReason = err.Error()
	} else {
		runtimeMode = status.RuntimeMode
		sample.GamePortHealthy = status.Container.Status == "running"
		sample.QueryPortHealthy = status.Container.Status == "running"
		if status.RuntimeMode == server.RuntimeWineDocker {
			m.fillDockerStats(ctx, &sample, status.Container)
		} else {
			m.fillWindowsProcessStats(ctx, &sample)
		}
	}
	m.fillDiskStats(ctx, &sample)
	m.fillRESTStats(ctx, &sample, runtimeMode)
	m.fillRCONHealth(ctx, &sample, runtimeMode)
	sample.RiskReasons = deriveRiskReasons(sample)
	if err := m.processRiskAlerts(ctx, sample.RiskReasons); err != nil {
		appendReason(&sample, "monitor alerts: "+err.Error())
	}
	if m.cfg.MonitorRetentionDays == 0 {
		return sample, nil
	}
	if err := m.store.InsertMonitorSample(ctx, sample); err != nil {
		return sample, err
	}
	return sample, nil
}

func (m Manager) Prune(ctx context.Context) error {
	if m.cfg.MonitorRetentionDays <= 0 {
		return nil
	}
	cutoff := m.currentTime().UTC().AddDate(0, 0, -m.cfg.MonitorRetentionDays)
	for {
		deleted, err := m.store.DeleteMonitorSamplesBefore(ctx, cutoff, 1000)
		if err != nil {
			return err
		}
		if deleted < 1000 {
			return nil
		}
	}
}

func (m Manager) fillRESTStats(ctx context.Context, sample *db.MonitorSample, runtimeMode string) {
	settings, settingsErr := palconfig.Read(m.cfg.PalWorldSettingsPath())
	if enabled := strings.TrimSpace(settings["RESTAPIEnabled"]); settingsErr == nil && enabled != "" && !strings.EqualFold(enabled, "True") {
		sample.RESTHealthy = false
		appendReason(sample, "REST: disabled in PalWorldSettings.ini (RESTAPIEnabled=False)")
		m.debugf("health rest state=disabled")
		return
	}
	if settingsErr == nil && runtimeMode == server.RuntimeWineDocker && isLoopbackURL(m.palrest.BaseURL) {
		configuredPort := strings.TrimSpace(settings["RESTAPIPort"])
		if configuredPort != "" && configuredPort != strconv.Itoa(m.cfg.RESTPort) {
			sample.RESTHealthy = false
			appendReason(sample, fmt.Sprintf("REST: PalWorldSettings.ini uses port %s but the Linux container maps host port %d; make RESTAPIPort and PALPANEL_REST_PORT match, then recreate the game container", configuredPort, m.cfg.RESTPort))
			m.debugf("health rest state=port_mismatch settings_port=%s mapped_port=%d", configuredPort, m.cfg.RESTPort)
			return
		}
	}
	client := m.palworldREST(settings, runtimeMode)
	resp, err := client.Do(ctx, "GET", "metrics", nil)
	if err != nil {
		sample.RESTHealthy = false
		if resp.Status == http.StatusUnauthorized {
			appendReason(sample, "REST: authentication failed (HTTP 401); the running server may still be using an older AdminPassword, save the current settings and restart Palworld")
			m.debugf("health rest endpoint=%s state=authentication_failed status=%d", client.BaseURL, resp.Status)
		} else {
			appendReason(sample, "REST: "+err.Error())
			m.debugf("health rest endpoint=%s state=failed status=%d error=%q", client.BaseURL, resp.Status, err.Error())
		}
		return
	}
	sample.RESTHealthy = resp.Status < 400
	m.debugf("health rest endpoint=%s state=healthy status=%d", client.BaseURL, resp.Status)
	data := mapFromAny(resp.Body)
	sample.CurrentPlayers = int(numberValue(data, "current_players", "currentPlayerNum", "currentplayernum", "players"))
	sample.MaxPlayers = int(numberValue(data, "max_players", "maxPlayerNum", "maxplayernum"))
	if err := m.observePlayerPresence(ctx, client); err != nil {
		m.debugf("player presence observation failed: %q", err.Error())
	}
}

func (m Manager) palworldREST(settings palconfig.Settings, runtimeMode string) palrest.Client {
	client := m.palrest
	if port := strings.TrimSpace(settings["RESTAPIPort"]); port != "" && (runtimeMode != server.RuntimeWineDocker || !isLoopbackURL(client.BaseURL)) {
		client.BaseURL = restBaseURLWithPort(client.BaseURL, port)
	}
	if password := strings.TrimSpace(settings["AdminPassword"]); password != "" {
		client.Password = password
	}
	return client
}

func (m Manager) fillRCONHealth(ctx context.Context, sample *db.MonitorSample, runtimeMode string) {
	settings, err := palconfig.Read(m.cfg.PalWorldSettingsPath())
	if err != nil {
		sample.RCONHealthy = false
		appendReason(sample, "RCON: cannot read PalWorldSettings.ini: "+err.Error())
		m.debugf("health rcon state=settings_unavailable error=%q", err.Error())
		return
	}
	if !strings.EqualFold(settings["RCONEnabled"], "True") {
		sample.RCONHealthy = false
		appendReason(sample, "RCON: disabled in PalWorldSettings.ini (RCONEnabled=False)")
		m.debugf("health rcon state=disabled")
		return
	}
	port := settings["RCONPort"]
	if strings.TrimSpace(port) == "" {
		port = "25575"
	}
	portNumber, parseErr := strconv.Atoi(port)
	if parseErr != nil || portNumber < 1 || portNumber > 65535 {
		sample.RCONHealthy = false
		appendReason(sample, "RCON: invalid RCONPort in PalWorldSettings.ini")
		m.debugf("health rcon state=invalid_port value=%q", port)
		return
	}
	host := m.cfg.EffectiveRCONHost()
	if runtimeMode == server.RuntimeWineDocker && isLoopbackHost(host) && portNumber != m.cfg.EffectiveRCONPort() {
		sample.RCONHealthy = false
		appendReason(sample, fmt.Sprintf("RCON: PalWorldSettings.ini uses port %d but the Linux container maps host port %d; make RCONPort and PALPANEL_RCON_PORT match, then recreate the game container", portNumber, m.cfg.EffectiveRCONPort()))
		m.debugf("health rcon state=port_mismatch settings_port=%d mapped_port=%d", portNumber, m.cfg.EffectiveRCONPort())
		return
	}
	target := net.JoinHostPort(host, strconv.Itoa(portNumber))
	dial := m.dial
	if dial == nil {
		dial = net.DialTimeout
	}
	conn, err := dial("tcp", target, 2*time.Second)
	if err != nil {
		sample.RCONHealthy = false
		appendReason(sample, "RCON: "+target+": "+err.Error())
		m.debugf("health rcon endpoint=%s state=failed error=%q", target, err.Error())
		return
	}
	_ = conn.Close()
	sample.RCONHealthy = true
	m.debugf("health rcon endpoint=%s state=healthy", target)
}

func (m Manager) debugf(format string, args ...any) {
	if m.cfg.DebugLogger != nil {
		m.cfg.DebugLogger.Printf(format, args...)
	}
}

func restBaseURLWithPort(baseURL string, port string) string {
	if _, err := strconv.Atoi(port); err != nil {
		return strings.TrimRight(baseURL, "/")
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" {
		return strings.TrimRight(baseURL, "/")
	}
	host := parsed.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	parsed.Host = net.JoinHostPort(host, port)
	return strings.TrimRight(parsed.String(), "/")
}

func isLoopbackURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && isLoopbackHost(parsed.Hostname())
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (m Manager) fillDockerStats(ctx context.Context, sample *db.MonitorSample, status docker.ContainerStatus) {
	sample.LifecycleAvailable = status.LifecycleAvailable
	if status.LifecycleAvailable {
		sample.OOMKilled = status.OOMKilled
		sample.ExitCode = status.ExitCode
		sample.RestartCount = status.RestartCount
		sample.StartedAt = status.StartedAt
		sample.FinishedAt = status.FinishedAt
	}
	out, err := m.command(ctx, m.cfg.DockerBinary, "stats", "--no-stream", "--format", "{{.CPUPerc}}|{{.MemUsage}}", m.cfg.DockerContainer)
	if err != nil {
		appendReason(sample, "docker stats: "+err.Error())
		return
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(parts) != 2 {
		appendReason(sample, "docker stats: unexpected output")
		return
	}
	cpu := strings.TrimSuffix(strings.TrimSpace(parts[0]), "%")
	if v, err := strconv.ParseFloat(cpu, 64); err == nil {
		sample.CPUAvailable = true
		sample.CPUPercent = v
	}
	memParts := strings.Split(parts[1], "/")
	if len(memParts) == 2 {
		if usage, ok := parseDockerBytes(strings.TrimSpace(memParts[0])); ok {
			sample.MemoryAvailable = true
			sample.MemoryUsageBytes = usage
			sample.WorkloadMemoryAvailable = true
			sample.WorkloadMemoryUsageBytes = usage
		}
		if limit, ok := parseDockerBytes(strings.TrimSpace(memParts[1])); ok {
			sample.MemoryLimitBytes = limit
			sample.WorkloadMemoryLimitBytes = limit
		}
	}
}

func (m Manager) fillWindowsProcessStats(ctx context.Context, sample *db.MonitorSample) {
	pidText, _, err := m.store.GetKV(ctx, "windows_pid")
	if err != nil || strings.TrimSpace(pidText) == "" {
		appendReason(sample, "windows process: pid unavailable")
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(pidText))
	if err != nil || pid <= 0 {
		appendReason(sample, "windows process: invalid pid")
		return
	}
	collector := m.processStats
	if collector == nil {
		collector = platformProcessTreeStats
	}
	stats, err := collector(ctx, pid)
	if err != nil {
		appendReason(sample, "windows process tree: "+err.Error())
		return
	}
	sample.CPUAvailable = true
	sample.CPUPercent = stats.CPUPercent
	sample.MemoryAvailable = true
	sample.MemoryUsageBytes = stats.MemoryUsageBytes
	sample.WorkloadMemoryAvailable = true
	sample.WorkloadMemoryUsageBytes = stats.MemoryUsageBytes
	if raw, ok, metadataErr := m.store.GetKV(ctx, "windows_process"); metadataErr != nil {
		appendReason(sample, "windows process metadata: "+metadataErr.Error())
	} else if ok && strings.TrimSpace(raw) != "" {
		var record struct {
			CreationTime uint64 `json:"creation_time"`
		}
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			appendReason(sample, "windows process metadata: "+err.Error())
		} else {
			sample.StartedAt = windowsFiletimeToRFC3339(record.CreationTime)
		}
	}
}

func windowsFiletimeToRFC3339(ticks uint64) string {
	const unixEpochFiletime = uint64(116444736000000000)
	if ticks < unixEpochFiletime {
		return ""
	}
	unixTicks := ticks - unixEpochFiletime
	seconds := int64(unixTicks / 10_000_000)
	nanoseconds := int64(unixTicks%10_000_000) * 100
	return time.Unix(seconds, nanoseconds).UTC().Format(time.RFC3339Nano)
}

func (m Manager) fillHostMemory(ctx context.Context, sample *db.MonitorSample) {
	collector := m.hostMemory
	if collector == nil {
		collector = newPlatformHostMemoryCollector()
	}
	stats, err := collector.Collect(ctx)
	if err != nil {
		appendReason(sample, "host memory: "+err.Error())
		return
	}
	if stats.TotalBytes <= 0 {
		appendReason(sample, "host memory: total memory unavailable")
		return
	}
	sample.HostMemoryAvailable = true
	sample.HostMemoryTotalBytes = stats.TotalBytes
	sample.HostMemoryAvailableBytes = stats.AvailableBytes
	sample.HostSwapTotalBytes = stats.SwapTotalBytes
	sample.HostSwapFreeBytes = stats.SwapFreeBytes
}

func (m Manager) fillDiskStats(ctx context.Context, sample *db.MonitorSample) {
	_ = ctx
	diskUsage := m.diskUsage
	if diskUsage == nil {
		diskUsage = platformDiskUsage
	}
	free, total, err := diskUsage(m.cfg.DataDir)
	if err != nil {
		appendReason(sample, "disk: "+err.Error())
		return
	}
	if total > 0 {
		sample.DiskAvailable = true
		sample.DiskFreeBytes = free
		sample.DiskTotalBytes = total
	}
}

func (m Manager) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.run != nil {
		return m.run(ctx, name, args...)
	}
	return runCommand(ctx, name, args...)
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (m Manager) runtimeOS() string {
	if m.goos != "" {
		return m.goos
	}
	return runtime.GOOS
}

func (m Manager) currentTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func appendReason(sample *db.MonitorSample, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	if sample.UnavailableReason != "" {
		sample.UnavailableReason += " | "
	}
	sample.UnavailableReason += reason
}

func nonEmptyLines(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func parseDockerBytes(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	units := []struct {
		suffix string
		mul    float64
	}{
		{"GiB", 1024 * 1024 * 1024},
		{"MiB", 1024 * 1024},
		{"KiB", 1024},
		{"GB", 1000 * 1000 * 1000},
		{"MB", 1000 * 1000},
		{"KB", 1000},
		{"B", 1},
	}
	for _, unit := range units {
		if strings.HasSuffix(raw, unit.suffix) {
			v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(raw, unit.suffix)), 64)
			if err != nil {
				return 0, false
			}
			return int64(v * unit.mul), true
		}
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	return v, err == nil
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func numberValue(data map[string]any, keys ...string) float64 {
	for _, key := range keys {
		value, ok := data[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case string:
			n, _ := strconv.ParseFloat(v, 64)
			return n
		}
	}
	return 0
}
