import type { AxiosResponse } from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { monitorApi } from './monitor';

describe('monitor API', () => {
  afterEach(() => vi.restoreAllMocks());

  it('maps and updates runtime debug logging status', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({
      data: { ok: true, data: { enabled: true, path: '/var/lib/palpanel/logs/palpanel-debug.log', size: 128, max_bytes: 1024, max_files: 3 } },
      status: 200,
    } as AxiosResponse);
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({
      data: { ok: true, data: { enabled: false, path: '/var/lib/palpanel/logs/palpanel-debug.log', size: 256, max_bytes: 1024, max_files: 3 } },
      status: 200,
    } as AxiosResponse);

    await expect(monitorApi.debugStatus()).resolves.toMatchObject({ enabled: true, size: 128 });
    await expect(monitorApi.setDebug(false)).resolves.toMatchObject({ enabled: false, size: 256 });
    expect(get).toHaveBeenCalledWith('/system/debug');
    expect(put).toHaveBeenCalledWith('/system/debug', { enabled: false });
  });

  it('maps separate host and workload memory with lifecycle diagnostics', async () => {
	vi.spyOn(apiClient, 'get').mockResolvedValue({
	  data: { ok: true, data: { sample: {
		id: 'sample-1', created_at: '2026-07-22T01:00:00Z',
		host_memory_available: true, host_memory_total_bytes: 17179869184, host_memory_available_bytes: 4294967296,
		host_swap_total_bytes: 4294967296, host_swap_free_bytes: 1073741824,
		workload_memory_available: true, workload_memory_usage_bytes: 6442450944, workload_memory_limit_bytes: 8589934592,
		lifecycle_available: true, oom_killed: true, exit_code: 137, restart_count: 3,
		started_at: '2026-07-22T00:00:00Z', finished_at: '2026-07-22T00:59:00Z',
		risk_reasons: [{ code: 'oom_killed', message: '工作负载被 OOM 终止', severity: 'critical' }],
	  } } }, status: 200,
	} as AxiosResponse);

	await expect(monitorApi.snapshot()).resolves.toMatchObject({ sample: {
	  host_memory_total_bytes: 17179869184,
	  host_memory_available_bytes: 4294967296,
	  workload_memory_usage_bytes: 6442450944,
	  workload_memory_limit_bytes: 8589934592,
	  oom_killed: true,
	  lifecycle_available: true,
	  exit_code: 137,
	  restart_count: 3,
	  risk_reasons: [{ code: 'oom_killed', severity: 'critical' }],
	} });
  });
});

describe('crash guard API', () => {
  afterEach(() => vi.restoreAllMocks());

  it('maps crash-loop state and submits explicit recovery confirmation', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({
      data: { ok: true, data: {
        enabled: true, tripped: true, reason: 'three crashes', expected_operation: false,
        recent_crash_count: 3, threshold: 3, window_seconds: 600, updated_at: '2026-07-29T12:00:00Z',
        events: [{ id: 'crash_1', kind: 'oom_kill', runtime_mode: 'wine_docker', occurrences: 1,
          exit_code: 137, oom_killed: true, restart_count: 3, message: 'OOM', created_at: '2026-07-29T12:00:00Z' }],
      } }, status: 200,
    } as AxiosResponse);
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({
      data: { ok: true, data: { enabled: true, tripped: false, expected_operation: false,
        recent_crash_count: 3, threshold: 3, window_seconds: 600, updated_at: '2026-07-29T12:01:00Z', events: [] } }, status: 200,
    } as AxiosResponse);

    await expect(monitorApi.crashGuardStatus()).resolves.toMatchObject({ tripped: true, events: [{ kind: 'oom_kill' }] });
    await expect(monitorApi.recoverCrashGuard(true)).resolves.toMatchObject({ tripped: false });
    expect(get).toHaveBeenCalledWith('/server/crash-guard?limit=20');
    expect(post).toHaveBeenCalledWith('/server/crash-guard/recover', { confirm: true, start: true });
  });
});
