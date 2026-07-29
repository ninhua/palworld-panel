import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { diagnosticsApi } from './diagnostics';

describe('diagnostics API', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('uses the interactive diagnostic endpoints and bounded client timeout', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({
      status: 200,
      data: { ok: true, data: { http_enabled: true, shell_enabled: false, timeout_ms: 15000, max_output: 65536, platform: 'linux' } },
    });
    const post = vi.spyOn(apiClient, 'post')
      .mockResolvedValueOnce({ status: 200, data: { ok: true, data: { method: 'GET', url: 'http://127.0.0.1/', status: '200 OK', status_code: 200, headers: {}, body: 'ok', truncated: false, duration_ms: 1 } } })
      .mockResolvedValueOnce({ status: 200, data: { ok: true, data: { command: 'id', output: '', exit_code: 0, success: true, timed_out: false, truncated: false, duration_ms: 1, error: '' } } });

    await diagnosticsApi.status();
    await diagnosticsApi.http({ method: 'GET', url: 'http://127.0.0.1/', headers: {}, body: '' });
    await diagnosticsApi.shell('id', true);

    expect(get).toHaveBeenCalledWith('/system/diagnostics');
    expect(post).toHaveBeenNthCalledWith(1, '/system/diagnostics/http', { method: 'GET', url: 'http://127.0.0.1/', headers: {}, body: '' }, { timeout: 20_000 });
    expect(post).toHaveBeenNthCalledWith(2, '/system/diagnostics/shell', { command: 'id', confirm: true }, { timeout: 20_000 });
  });
});
