import type { AxiosResponse } from 'axios';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { supportBundlesApi } from './supportBundles';

vi.mock('./client', async () => {
  const actual = await vi.importActual<typeof import('./client')>('./client');
  return {
    ...actual,
    apiClient: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
  };
});

describe('supportBundlesApi', () => {
  beforeEach(() => vi.clearAllMocks());

  it('creates bundles with explicit confirmation', async () => {
    vi.mocked(apiClient.post).mockResolvedValue({ data: { ok: true, data: { id: 'a'.repeat(32) } }, status: 200 } as AxiosResponse);
    await supportBundlesApi.create(true);
    expect(apiClient.post).toHaveBeenCalledWith(
      '/system/diagnostics/support-bundles',
      { include_logs: true, confirm: true },
      { timeout: 30_000 },
    );
  });

  it('encodes bundle ids for deletion', async () => {
    vi.mocked(apiClient.delete).mockResolvedValue({ data: { ok: true, data: { deleted: true, id: 'abc' } }, status: 200 } as AxiosResponse);
    await supportBundlesApi.remove('abc/def');
    expect(apiClient.delete).toHaveBeenCalledWith('/system/diagnostics/support-bundles/abc%2Fdef');
  });
});
