import { apiClient, handleRequest } from './client';
import type { CrashGuardEvent, CrashGuardStatus, DebugLogStatus, MonitorRiskReason, MonitorSample, MonitorSnapshot } from '../types';

const emptyCrashGuardStatus: CrashGuardStatus = {
  enabled: true,
  tripped: false,
  expected_operation: false,
  recent_crash_count: 0,
  threshold: 3,
  window_seconds: 600,
  updated_at: '',
  events: [],
};

const mapCrashGuardEvent = (raw: unknown): CrashGuardEvent => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const kind: CrashGuardEvent['kind'] = data.kind === 'oom_kill' || data.kind === 'container_restart' ? data.kind : 'unexpected_exit';
  return {
    id: String(data.id || ''), kind, runtime_mode: String(data.runtime_mode || ''),
    occurrences: Number(data.occurrences || 1), exit_code: Number(data.exit_code || 0),
    oom_killed: Boolean(data.oom_killed), restart_count: Number(data.restart_count || 0),
    started_at: data.started_at ? String(data.started_at) : undefined,
    finished_at: data.finished_at ? String(data.finished_at) : undefined,
    message: String(data.message || ''), created_at: String(data.created_at || ''),
  };
};

const mapCrashGuardStatus = (raw: unknown): CrashGuardStatus => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return {
    enabled: data.enabled === undefined ? true : Boolean(data.enabled),
    tripped: Boolean(data.tripped), tripped_at: data.tripped_at ? String(data.tripped_at) : undefined,
    reason: data.reason ? String(data.reason) : undefined, expected_operation: Boolean(data.expected_operation),
    recent_crash_count: Number(data.recent_crash_count || 0), threshold: Number(data.threshold || 3),
    window_seconds: Number(data.window_seconds || 600),
    last_observed_status: data.last_observed_status ? String(data.last_observed_status) : undefined,
    last_observed_runtime: data.last_observed_runtime ? String(data.last_observed_runtime) : undefined,
    updated_at: String(data.updated_at || ''),
    events: Array.isArray(data.events) ? data.events.map(mapCrashGuardEvent) : [],
  };
};

const emptyDebugStatus: DebugLogStatus = {
  enabled: false,
  path: '',
  size: 0,
  max_bytes: 20 * 1024 * 1024,
  max_files: 5,
};

const emptySample: MonitorSample = {
  id: '',
  created_at: '',
  cpu_available: false,
  cpu_percent: 0,
  memory_available: false,
  memory_usage_bytes: 0,
  memory_limit_bytes: 0,
  host_memory_available: false,
  host_memory_total_bytes: 0,
  host_memory_available_bytes: 0,
  host_swap_total_bytes: 0,
  host_swap_free_bytes: 0,
  workload_memory_available: false,
  workload_memory_usage_bytes: 0,
  workload_memory_limit_bytes: 0,
  oom_killed: false,
  lifecycle_available: false,
  exit_code: 0,
  restart_count: 0,
  risk_reasons: [],
  disk_available: false,
  disk_free_bytes: 0,
  disk_total_bytes: 0,
  current_players: 0,
  max_players: 0,
  rest_healthy: false,
  rcon_healthy: false,
  game_port_healthy: false,
  query_port_healthy: false,
};

const mapSample = (raw: unknown): MonitorSample => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const riskReasons = Array.isArray(data.risk_reasons)
    ? data.risk_reasons.flatMap((rawReason) => {
        if (!rawReason || typeof rawReason !== 'object') return [];
        const reason = rawReason as Record<string, unknown>;
        const severity: MonitorRiskReason['severity'] = reason.severity === 'critical' ? 'critical' : 'warning';
        return [{ code: String(reason.code || ''), message: String(reason.message || ''), severity }];
      })
    : [];
  return {
    id: String(data.id || ''),
    created_at: String(data.created_at || ''),
    cpu_available: Boolean(data.cpu_available),
    cpu_percent: Number(data.cpu_percent || 0),
    memory_available: Boolean(data.memory_available),
    memory_usage_bytes: Number(data.memory_usage_bytes || 0),
    memory_limit_bytes: Number(data.memory_limit_bytes || 0),
    host_memory_available: Boolean(data.host_memory_available),
    host_memory_total_bytes: Number(data.host_memory_total_bytes || 0),
    host_memory_available_bytes: Number(data.host_memory_available_bytes || 0),
    host_swap_total_bytes: Number(data.host_swap_total_bytes || 0),
    host_swap_free_bytes: Number(data.host_swap_free_bytes || 0),
    workload_memory_available: data.workload_memory_available === undefined ? Boolean(data.memory_available) : Boolean(data.workload_memory_available),
    workload_memory_usage_bytes: Number(data.workload_memory_usage_bytes ?? data.memory_usage_bytes ?? 0),
    workload_memory_limit_bytes: Number(data.workload_memory_limit_bytes ?? data.memory_limit_bytes ?? 0),
    oom_killed: Boolean(data.oom_killed),
    lifecycle_available: Boolean(data.lifecycle_available),
    exit_code: Number(data.exit_code || 0),
    restart_count: Number(data.restart_count || 0),
    started_at: data.started_at ? String(data.started_at) : undefined,
    finished_at: data.finished_at ? String(data.finished_at) : undefined,
    risk_reasons: riskReasons,
    disk_available: Boolean(data.disk_available),
    disk_free_bytes: Number(data.disk_free_bytes || 0),
    disk_total_bytes: Number(data.disk_total_bytes || 0),
    current_players: Number(data.current_players || 0),
    max_players: Number(data.max_players || 0),
    rest_healthy: Boolean(data.rest_healthy),
    rcon_healthy: Boolean(data.rcon_healthy),
    game_port_healthy: Boolean(data.game_port_healthy),
    query_port_healthy: Boolean(data.query_port_healthy),
    unavailable_reason: data.unavailable_reason ? String(data.unavailable_reason) : undefined,
  };
};

const mapSnapshot = (raw: unknown): MonitorSnapshot => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return { sample: mapSample(data.sample) };
};

const mapHistory = (raw: unknown): MonitorSample[] => {
  if (!Array.isArray(raw)) return [];
  return raw.map(mapSample);
};

const mapDebugStatus = (raw: unknown): DebugLogStatus => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return {
    enabled: Boolean(data.enabled),
    path: String(data.path || ''),
    size: Number(data.size || 0),
    max_bytes: Number(data.max_bytes || 20 * 1024 * 1024),
    max_files: Number(data.max_files || 5),
  };
};

export const monitorApi = {
  crashGuardStatus: () =>
    handleRequest<unknown, CrashGuardStatus>(
      () => apiClient.get('/server/crash-guard?limit=20'),
      emptyCrashGuardStatus,
      { map: mapCrashGuardStatus, quiet: true, fallbackOnError: true },
    ),

  recoverCrashGuard: (start = true) =>
    handleRequest<unknown, CrashGuardStatus>(
      () => apiClient.post('/server/crash-guard/recover', { confirm: true, start }),
      emptyCrashGuardStatus,
      { map: mapCrashGuardStatus, quiet: true, fallbackOnError: false },
    ),

  snapshot: () =>
    handleRequest<unknown, MonitorSnapshot>(
      () => apiClient.get('/monitor/snapshot'),
      { sample: emptySample },
      { map: mapSnapshot, quiet: true },
    ),

  history: (limit = 120) =>
    handleRequest<unknown, MonitorSample[]>(
      () => apiClient.get(`/monitor/history?limit=${limit}`),
      [],
      { map: mapHistory, quiet: true },
    ),

  debugStatus: () =>
    handleRequest<unknown, DebugLogStatus>(
      () => apiClient.get('/system/debug'),
      emptyDebugStatus,
      { map: mapDebugStatus, quiet: true, fallbackOnError: true },
    ),

  setDebug: (enabled: boolean) =>
    handleRequest<unknown, DebugLogStatus>(
      () => apiClient.put('/system/debug', { enabled }),
      emptyDebugStatus,
      { map: mapDebugStatus, quiet: true, fallbackOnError: false },
    ),
};
