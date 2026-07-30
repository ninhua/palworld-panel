import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { SAVE_ARCHIVE_IMPORT_TIMEOUT_MS, SAVE_INDEX_OPERATION_TIMEOUT_MS } from './requestTimeouts';
import { saveSourcesApi } from './saveSources';

describe('save sources api timeouts', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });


  it('maps the actual server world separately from the active analysis source', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({
      status: 200,
      data: {
        ok: true,
        data: {
          items: [{ id: 'import-1', name: '分析副本', kind: 'import', active: true }],
          active_status: { state: 'ready', counts: {} },
          runtime_save: {
            available: true, server_active: true, source_id: 'server', source_name: '当前服务器存档',
            world_id: 'WORLD-RUNNING', state: 'running',
          },
        },
      },
    });

    const result = await saveSourcesApi.list();

    expect(get).toHaveBeenCalledWith('/save-sources');
    expect(result.items[0]).toMatchObject({ id: 'import-1', active: true });
    expect(result.runtime_save).toEqual({
      available: true, server_active: true, source_id: 'server', source_name: '当前服务器存档',
      world_id: 'WORLD-RUNNING', state: 'running',
    });
  });

  it('does not apply the global eight-second timeout to archive imports', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ status: 200, data: { ok: true, data: { id: 'save-1', name: 'Imported', kind: 'import' } } });
    const file = new File(['archive'], 'world.tar.gz', { type: 'application/gzip' });

    await saveSourcesApi.importArchive(file, 'Imported');

    expect(post).toHaveBeenCalledWith('/save-sources/import', expect.any(FormData), {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: SAVE_ARCHIVE_IMPORT_TIMEOUT_MS,
    });
    expect(SAVE_ARCHIVE_IMPORT_TIMEOUT_MS).toBe(0);
  });

  it('inspects, selects, and imports an archive candidate through the staged API', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ status: 200, data: { ok: true, data: {} } });
    const file = new File(['archive'], 'world.zip', { type: 'application/zip' });

    await saveSourcesApi.inspectArchive(file, 'Imported');
    await saveSourcesApi.selectImportCandidate('inspect with spaces', 'candidate/world');
    await saveSourcesApi.importInspected('inspect with spaces', 'Imported');

    expect(post).toHaveBeenNthCalledWith(1, '/save-sources/import/inspect', expect.any(FormData), {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: SAVE_ARCHIVE_IMPORT_TIMEOUT_MS,
    });
    expect(post).toHaveBeenNthCalledWith(2, '/save-sources/import/inspect/inspect%20with%20spaces/select', {
      candidate_id: 'candidate/world',
    });
    expect(post).toHaveBeenNthCalledWith(3, '/save-sources/import', {
      inspection_id: 'inspect with spaces',
      name: 'Imported',
    }, { timeout: SAVE_ARCHIVE_IMPORT_TIMEOUT_MS });
  });

  it('allows activation and rebuild to wait for save indexing', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ status: 200, data: { ok: true, data: {} } });

    await saveSourcesApi.activate('save with spaces');
    await saveSourcesApi.rebuild('save with spaces');

    expect(post).toHaveBeenNthCalledWith(1, '/save-sources/save%20with%20spaces/activate', undefined, { timeout: SAVE_INDEX_OPERATION_TIMEOUT_MS });
    expect(post).toHaveBeenNthCalledWith(2, '/save-sources/save%20with%20spaces/rebuild', undefined, { timeout: SAVE_INDEX_OPERATION_TIMEOUT_MS });
    expect(SAVE_INDEX_OPERATION_TIMEOUT_MS).toBe(180_000);
  });
  it('submits host migration as an asynchronous lifecycle job', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({
      status: 202,
      data: { ok: true, data: { id: 'job-migrate', type: 'host_save_migration', status: 'queued', progress: 0, message: 'queued', created_at: '2026-07-30T00:00:00Z', updated_at: '2026-07-30T00:00:00Z' } },
    });

    const job = await saveSourcesApi.executeHostMigration('save-1', '76561198000000000', 'Migrated world');

    expect(post).toHaveBeenCalledWith('/save-sources/import', {
      migration_source_id: 'save-1',
      steam_id: '76561198000000000',
      name: 'Migrated world',
      confirm: true,
    }, { timeout: SAVE_INDEX_OPERATION_TIMEOUT_MS });
    expect(job).toMatchObject({ id: 'job-migrate', type: 'host_save_migration', status: 'waiting' });
  });

});
