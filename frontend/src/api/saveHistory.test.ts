import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { saveHistoryApi } from './saveHistory';

describe('save history API', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('uses the active-world history endpoints', async () => {
    const get = vi.spyOn(apiClient, 'get')
      .mockResolvedValueOnce({ status: 200, data: { ok: true, data: { source: { id: 'server', name: 'Current', kind: 'server' }, retention: 24, max_total_bytes: 1, total_bytes: 0, items: [] } } })
      .mockResolvedValueOnce({ status: 200, data: { ok: true, data: { from: {}, to: {}, summary: {}, total: 0, limit: 100, offset: 0, items: [] } } });

    await saveHistoryApi.list();
    await saveHistoryApi.diff({ from: 'old', to: 'new', category: 'items', q: 'wood', limit: 100, offset: 0 });

    expect(get).toHaveBeenNthCalledWith(1, '/save/history');
    expect(get).toHaveBeenNthCalledWith(2, '/save/history/diff', {
      params: { from: 'old', to: 'new', category: 'items', q: 'wood', limit: 100, offset: 0 },
    });
  });
});
