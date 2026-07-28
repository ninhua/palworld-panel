import { describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { mapNetworkProxyConfig, networkProxyApi } from './networkProxy';

describe('networkProxyApi', () => {
  it('maps public endpoints without inventing credentials', () => {
    expect(mapNetworkProxyConfig({
      install: { enabled: true, configured: true, url: 'http://127.0.0.1:7890', scheme: 'http', authentication_configured: true, source: 'managed' },
      community: { enabled: false, configured: false, url: '', source: 'environment' },
    })).toEqual({
      install: { enabled: true, configured: true, url: 'http://127.0.0.1:7890', scheme: 'http', authentication_configured: true, source: 'managed', requires_restart: false, effective_for_next_task: true },
      community: { enabled: false, configured: false, url: '', authentication_configured: false, source: 'environment', requires_restart: false, effective_for_next_task: true },
    });
  });

  it('sends proxy URLs only in the write request', async () => {
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({ status: 200, data: { ok: true, data: {
      install: { enabled: true, configured: true, url: 'http://127.0.0.1:7890', scheme: 'http', authentication_configured: true, source: 'managed' },
      community: { enabled: false, configured: false, url: '', authentication_configured: false, source: 'managed' },
    } } });
    const update = { install_enabled: true, install_proxy_url: 'http://user:secret@127.0.0.1:7890' };

    const result = await networkProxyApi.updateConfig(update);

    expect(put).toHaveBeenCalledWith('/settings/network-proxy', update);
    expect(JSON.stringify(result)).not.toContain('secret');
    put.mockRestore();
  });
});
