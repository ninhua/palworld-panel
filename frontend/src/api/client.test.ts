import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, apiClient, currentApiBaseUrl, getErrorMessage, handleRequest, unwrapApiData } from './client';

describe('api client response handling', () => {
  beforeEach(() => {
    localStorage.clear();
    window.history.replaceState({}, '', 'http://localhost:3000/');
  });

  it('unwraps backend { ok, data } envelopes', () => {
    const result = unwrapApiData({ data: { ok: true, data: [{ id: 'job_1' }] }, status: 200 }, []);
    expect(result).toEqual([{ id: 'job_1' }]);
  });

  it('converts null array payloads to an empty array', () => {
    const result = unwrapApiData({ data: { ok: true, data: null }, status: 200 }, [{ id: 'old' }]);
    expect(result).toEqual([]);
  });

  it('throws by default when the backend request fails', async () => {
    await expect(
      handleRequest(() => Promise.reject({ response: { status: 502 } }), { logs: '暂无日志' }, { quiet: true }),
    ).rejects.toMatchObject({
      status: 502,
      code: 'backend_temporarily_unavailable',
      message: expect.stringContaining('暂时无法连接 PalPanel 后端'),
    });
  });

  it('returns the fallback only when fallbackOnError is explicitly enabled', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const result = await handleRequest(
      () => Promise.reject({ response: { status: 502 } }),
      { logs: '暂无日志' },
      { quiet: true, fallbackOnError: true },
    );

    expect(result).toEqual({ logs: '暂无日志' });
    expect(warnSpy).not.toHaveBeenCalled();
    warnSpy.mockRestore();
  });

  it('throws backend error envelopes when fallback is disabled', async () => {
    await expect(
      handleRequest(() => Promise.resolve({ data: { ok: false, error: { code: 'unsupported', message: '接口未实现' } }, status: 501 }), {}),
    ).rejects.toMatchObject({ message: '接口未实现', code: 'unsupported' });
  });

  it('uses a contextual fallback only when no structured error message exists', () => {
    expect(getErrorMessage(undefined, '读取配置历史失败')).toBe('读取配置历史失败');
    expect(getErrorMessage(new ApiError('后端拒绝请求'), '读取配置历史失败')).toBe('后端拒绝请求');
  });

  it('always uses the same-origin API and ignores old backend URL storage', async () => {
    localStorage.setItem('palpanel_backend_url', 'http://198.51.100.48:64217');
    localStorage.setItem('palsphere_backend_url', 'http://127.0.0.1:65002');
    let baseUrl = '';

    await apiClient.get('/health', {
      adapter: async (config) => {
        baseUrl = String(config.baseURL);
        return {
          data: { ok: true, data: { status: 'ok' } },
          status: 200,
          statusText: 'OK',
          headers: {},
          config,
        };
      },
    });

    expect(currentApiBaseUrl()).toBe('/api');
    expect(baseUrl).toBe('/api');
  });

  it('publishes an authentication event for HTTP 401 responses', async () => {
	const listener = vi.fn();
	window.addEventListener('palpanel:auth-error', listener);
	await expect(
	  handleRequest(
		() => Promise.reject({ response: { status: 401 } }),
		{},
		{ quiet: true, fallbackOnError: false },
	  ),
	).rejects.toMatchObject({ status: 401, code: 'unauthorized' });
	expect(listener).toHaveBeenCalledTimes(1);
	window.removeEventListener('palpanel:auth-error', listener);
  });

  it('keeps Steam login failures scoped to the Workshop gate', async () => {
    const listener = vi.fn();
    window.addEventListener('palpanel:auth-error', listener);

    await expect(
      handleRequest(
        () => Promise.reject({
          response: {
            status: 401,
            data: { ok: false, error: { code: 'steam_login_required', message: 'Steam cache expired' } },
          },
        }),
        {},
        { quiet: true, fallbackOnError: false },
      ),
    ).rejects.toMatchObject({ status: 401, code: 'steam_login_required', message: 'Steam cache expired' });

    await expect(
      handleRequest(
        () => Promise.reject({
          response: {
            status: 401,
            data: { ok: false, error: { code: 'invalid_steam_credentials', message: 'invalid Steam credentials' } },
          },
        }),
        {},
        { quiet: true, fallbackOnError: false },
      ),
    ).rejects.toMatchObject({ status: 401, code: 'invalid_steam_credentials', message: 'invalid Steam credentials' });
    expect(listener).not.toHaveBeenCalled();

    window.removeEventListener('palpanel:auth-error', listener);
  });
});
