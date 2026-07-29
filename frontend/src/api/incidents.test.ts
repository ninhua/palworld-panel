import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { incidentsApi } from './incidents';

describe('incidents API', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('encodes filters and incident identifiers', async () => {
    const get = vi.spyOn(apiClient, 'get')
      .mockResolvedValueOnce({ status: 200, data: { ok: true, data: { items: [], total: 0, limit: 25, offset: 5, summary: { open: 0, acknowledged: 0, resolved: 0 }, webhook: { enabled: false, signed: false, timeout_seconds: 10, max_attempts: 6 } } } })
      .mockResolvedValueOnce({ status: 200, data: { ok: true, data: { incident: {}, events: [], deliveries: [] } } });

    await incidentsApi.list({ status: 'open', severity: 'critical', source: 'crash-guard', q: 'OOM', limit: 25, offset: 5 });
    await incidentsApi.get('incident id/unsafe');

    expect(get).toHaveBeenNthCalledWith(1, '/incidents?status=open&severity=critical&source=crash-guard&q=OOM&limit=25&offset=5');
    expect(get).toHaveBeenNthCalledWith(2, '/incidents/incident%20id%2Funsafe');
  });

  it('requires explicit confirmation for state changes and webhook tests', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ status: 200, data: { ok: true, data: {} } });

    await incidentsApi.acknowledge('incident_1', 'investigating');
    await incidentsApi.resolve('incident_1', 'fixed');
    await incidentsApi.reopen('incident_1', 'failed again');
    await incidentsApi.testWebhook();

    expect(post).toHaveBeenNthCalledWith(1, '/incidents/incident_1/ack', { confirm: true, message: 'investigating' });
    expect(post).toHaveBeenNthCalledWith(2, '/incidents/incident_1/resolve', { confirm: true, message: 'fixed' });
    expect(post).toHaveBeenNthCalledWith(3, '/incidents/incident_1/reopen', { confirm: true, message: 'failed again' });
    expect(post).toHaveBeenNthCalledWith(4, '/system/incidents/webhook/test', { confirm: true });
  });
});
