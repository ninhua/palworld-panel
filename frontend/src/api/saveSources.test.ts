import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { SAVE_ARCHIVE_IMPORT_TIMEOUT_MS, SAVE_INDEX_OPERATION_TIMEOUT_MS } from './requestTimeouts';
import { saveSourcesApi } from './saveSources';

describe('save sources api timeouts', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
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
});
