import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { modConfigurationsApi } from './modConfigurations';

describe('mod configuration api', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('maps adapters and requests opaque file ids', async () => {
    vi.spyOn(apiClient, 'get').mockResolvedValueOnce({
	  status: 200,
      data: {
        ok: true,
        data: [{
          id: 'paldefender', name: 'PalDefender', description: 'security', available: true,
          installed: true, configured: true, enabled: true, status: 'ready',
          dependencies: [], actions: [], reload_behavior: 'online_reload', files: [{ id: 'opaque', name: 'Config.json', path: 'Config.json', extension: '.json', size: 12, modified_at: '2026-07-18T00:00:00Z', revision: 'rev', executable: false }],
        }],
      },
    });
    const adapters = await modConfigurationsApi.listAdapters();
    expect(adapters[0]).toMatchObject({ id: 'paldefender', installed: true, configured: true, enabled: true, status: 'ready', files: [{ id: 'opaque' }] });

    const get = vi.spyOn(apiClient, 'get').mockResolvedValueOnce({
	  status: 200,
      data: { ok: true, data: { file: adapters[0].files[0], content: '{}', format: 'json', fields: [] } },
    });
    await modConfigurationsApi.getAdapter('paldefender', 'opaque');
    expect(get).toHaveBeenCalledWith('/mods/configurations/paldefender', { params: { file: 'opaque' } });
  });

  it('sends revision and executable confirmation when saving Lua', async () => {
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({
	  status: 200,
      data: { ok: true, data: { file: { id: 'lua', executable: true }, content: 'Range = 2', format: 'lua' } },
    });
    await modConfigurationsApi.saveFile('local/mod', 'lua', 'Range = 2', 'revision-1', true);
    expect(put).toHaveBeenCalledWith('/mods/local%2Fmod/files/lua', {
      content: 'Range = 2', revision: 'revision-1', confirm_executable: true,
    });
  });

  it('runs a dedicated adapter action and maps the refreshed adapter', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({
      status: 200,
      data: {
        ok: true,
        data: {
          adapter: {
            id: 'palzones', name: 'PalZones', description: '区域配置', available: true,
            installed: true, configured: true, enabled: true, status: 'restart_required',
            status_detail: '等待重启', dependencies: [], actions: [], reference_urls: { source: 'https://example.test/source' },
            reload_behavior: 'restart_required', files: [],
          },
          changed: ['Config/zones.json'],
          restart_required: true,
        },
      },
    });

    const result = await modConfigurationsApi.runAction('palzones', 'initialize');

    expect(post).toHaveBeenCalledWith('/mods/configurations/palzones/actions', { action: 'initialize' });
    expect(result).toMatchObject({
      adapter: { id: 'palzones', status: 'restart_required', reference_urls: { source: 'https://example.test/source' } },
      changed: ['Config/zones.json'],
      restart_required: true,
    });
  });
});
