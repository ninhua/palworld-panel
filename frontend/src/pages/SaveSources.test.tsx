import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { SaveSources } from './SaveSources';

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  inspectArchive: vi.fn(),
  selectImportCandidate: vi.fn(),
  importInspected: vi.fn(),
  activate: vi.fn(),
  rebuild: vi.fn(),
  rename: vi.fn(),
  remove: vi.fn(),
  planHostMigration: vi.fn(),
  executeHostMigration: vi.fn(),
  waitForSaveSourcesBackend: vi.fn(),
}));

const taskMocks = vi.hoisted(() => ({
  waitForJob: vi.fn(),
}));

vi.mock('../api/saveSources', () => ({ saveSourcesApi: mocks, waitForSaveSourcesBackend: mocks.waitForSaveSourcesBackend }));
vi.mock('../api/tasks', () => ({ tasksApi: taskMocks }));

const renderSaveSources = () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={client}><SaveSources /></QueryClientProvider>);
};

const status = {
  state: 'ready', parser: 'test', warnings: [], error: '', updated_at: '',
  counts: { players: 0, guilds: 0, bases: 0, pals: 0, containers: 0, map_entities: 0 },
};

const runtimeSave = {
  available: true, server_active: true, source_id: 'server', source_name: '当前服务器存档',
  world_id: 'RUNNINGWORLD0123456789ABCDEF0123', state: 'running',
};

describe('SaveSources archive inspection', () => {
  afterEach(() => cleanup());

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.list.mockResolvedValue({ items: [], active_status: status, runtime_save: runtimeSave });
    mocks.waitForSaveSourcesBackend.mockResolvedValue(null);
    mocks.selectImportCandidate.mockImplementation(async (inspectionID: string, candidateID: string) => ({
      id: inspectionID, file_name: 'world.zip', candidates: [], selected_candidate_id: candidateID,
      requires_selection: false, expires_at: '2026-07-22T13:00:00Z',
    }));
    mocks.importInspected.mockResolvedValue({ id: 'save-1', name: 'Imported', kind: 'import' });
    mocks.planHostMigration.mockResolvedValue({
      steam_id: '76561198000000000', source_uid: 'source', target_uid: 'target', strategy: 'direct',
      can_execute: true, source_player_file: 'Players/host.sav', source_dps_exists: false,
      target_player_exists: false, target_dps_exists: false, warnings: [],
    });
    mocks.executeHostMigration.mockResolvedValue({ id: 'job-migrate', type: 'host_save_migration', status: 'waiting', progress: 0, message: 'queued', created_at: '' });
    taskMocks.waitForJob.mockImplementation(async (_id: string, onUpdate?: (job: unknown) => void) => {
      onUpdate?.({ id: 'job-migrate', type: 'host_save_migration', status: 'running', progress: 55, message: 'publishing world', created_at: '' });
      return { id: 'job-migrate', type: 'host_save_migration', status: 'success', progress: 100, message: 'completed', created_at: '' };
    });
  });

  it('shows the running world separately from the active parsing source', async () => {
    mocks.list.mockResolvedValue({
      items: [{ id: 'import-active', name: '分析副本', kind: 'import', active: true, created_at: '', updated_at: '' }],
      active_status: status,
      runtime_save: runtimeSave,
    });
    renderSaveSources();

    expect(await screen.findByText('RUNNINGWORLD0123456789ABCDEF0123')).toBeInTheDocument();
    expect(screen.getByText('服务器实际运行世界')).toBeInTheDocument();
    expect(screen.getByText('PalPanel 当前分析源')).toBeInTheDocument();
    expect(screen.getAllByText('分析副本').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText(/服务器实际运行的是/)).toBeInTheDocument();
    expect(screen.getByText(/“用于分析”不会部署或切换运行世界/)).toBeInTheDocument();
  });

  it('requires an explicit valid world selection before importing a multi-world archive', async () => {
    mocks.inspectArchive.mockResolvedValue({
      id: 'inspect-1', file_name: 'world.zip', selected_candidate_id: '', requires_selection: true,
      expires_at: '2026-07-22T13:00:00Z',
      candidates: [
        { id: 'candidate-a', relative_path: 'world-a', world_id: 'world-a', player_count: 3, level_sha256: 'a', level_size: 10, valid: true, warnings: [], errors: [] },
        { id: 'candidate-b', relative_path: 'world-b', world_id: 'world-b', player_count: 5, level_sha256: 'b', level_size: 20, valid: true, warnings: [], errors: [] },
        { id: 'candidate-bad', relative_path: 'broken', world_id: 'broken', player_count: 0, level_sha256: 'c', level_size: 4, valid: false, warnings: [], errors: ['parse failed'] },
      ],
    });
    renderSaveSources();

    fireEvent.change(screen.getByLabelText('存档归档文件'), { target: { files: [new File(['archive'], 'world.zip')] } });
    fireEvent.click(screen.getByRole('button', { name: '检查存档' }));

    expect(await screen.findByText('选择要导入的世界')).toBeInTheDocument();
    expect(screen.getByLabelText('世界 world-b')).toBeEnabled();
    expect(screen.getByLabelText('世界 broken')).toBeDisabled();
    fireEvent.click(screen.getByLabelText('世界 world-b'));
    fireEvent.click(screen.getByRole('button', { name: '导入所选世界' }));

    await waitFor(() => expect(mocks.selectImportCandidate).toHaveBeenCalledWith('inspect-1', 'candidate-b'));
    await waitFor(() => expect(mocks.importInspected).toHaveBeenCalledWith('inspect-1', ''));
  });

  it('imports the server-selected candidate immediately when exactly one world is valid', async () => {
    mocks.inspectArchive.mockResolvedValue({
      id: 'inspect-2', file_name: 'single.zip', selected_candidate_id: 'candidate-only', requires_selection: false,
      expires_at: '2026-07-22T13:00:00Z',
      candidates: [{ id: 'candidate-only', relative_path: 'world', world_id: 'world', player_count: 1, level_sha256: 'a', level_size: 10, valid: true, warnings: [], errors: [] }],
    });
    renderSaveSources();

    fireEvent.change(screen.getByLabelText('存档归档文件'), { target: { files: [new File(['archive'], 'single.zip')] } });
    fireEvent.click(screen.getByRole('button', { name: '检查存档' }));

    await waitFor(() => expect(mocks.importInspected).toHaveBeenCalledWith('inspect-2', ''));
    expect(mocks.selectImportCandidate).not.toHaveBeenCalled();
  });

  it('keeps invalid inspection results actionable instead of hiding the upload controls', async () => {
    mocks.inspectArchive.mockResolvedValue({
      id: 'inspect-invalid', file_name: 'broken.zip', selected_candidate_id: '', requires_selection: false,
      expires_at: '2026-07-22T13:00:00Z',
      candidates: [{ id: 'broken', relative_path: 'world', world_id: 'world', player_count: 0, level_sha256: 'a', level_size: 10, valid: false, warnings: [], errors: ['无法解析 Level.sav'] }],
    });
    renderSaveSources();

    fireEvent.change(screen.getByLabelText('存档归档文件'), { target: { files: [new File(['archive'], 'broken.zip')] } });
    fireEvent.click(screen.getByRole('button', { name: '检查存档' }));

    expect(await screen.findByText('没有可导入的世界')).toBeInTheDocument();
    expect(screen.getByText('无法解析 Level.sav')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '重新选择归档' })).toBeEnabled();
    expect(screen.getByRole('button', { name: '导入所选世界' })).toBeDisabled();
  });

  it('shows warnings for a single valid world before importing it', async () => {
    mocks.inspectArchive.mockResolvedValue({
      id: 'inspect-warning', file_name: 'warning.zip', selected_candidate_id: 'candidate-warning', requires_selection: false,
      expires_at: '2026-07-22T13:00:00Z',
      candidates: [{
        id: 'candidate-warning', relative_path: 'world', world_id: 'world', player_count: 1,
        level_sha256: 'a', level_size: 10, valid: true,
        warnings: ['parser_incompatible'], errors: [],
      }],
    });
    renderSaveSources();

    fireEvent.change(screen.getByLabelText('存档归档文件'), { target: { files: [new File(['archive'], 'warning.zip')] } });
    fireEvent.click(screen.getByRole('button', { name: '检查存档' }));

    expect(await screen.findByText(/存档格式不兼容/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '导入所选世界' })).toBeEnabled();
    expect(mocks.importInspected).not.toHaveBeenCalled();
  });

  it('returns to archive inspection after an automatically selected world fails to import', async () => {
    mocks.importInspected.mockRejectedValueOnce(new Error('导入失败'));
    mocks.inspectArchive.mockResolvedValue({
      id: 'inspect-retry', file_name: 'single.zip', selected_candidate_id: 'only', requires_selection: false,
      expires_at: '2026-07-22T13:00:00Z',
      candidates: [{ id: 'only', relative_path: 'world', world_id: 'world', player_count: 1, level_sha256: 'a', level_size: 10, valid: true, warnings: [], errors: [] }],
    });
    renderSaveSources();

    fireEvent.change(screen.getByLabelText('存档归档文件'), { target: { files: [new File(['archive'], 'single.zip')] } });
    fireEvent.click(screen.getByRole('button', { name: '检查存档' }));

    expect(await screen.findByText('导入失败')).toBeInTheDocument();
    expect(screen.getByLabelText('存档归档文件')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '检查存档' })).toBeEnabled();
  });
});


describe('SaveSources host migration recovery', () => {
  afterEach(() => cleanup());

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.waitForSaveSourcesBackend.mockResolvedValue(null);
    mocks.planHostMigration.mockResolvedValue({
      steam_id: '76561198000000000', source_uid: 'source', target_uid: 'target', strategy: 'direct',
      can_execute: true, source_player_file: 'Players/host.sav', source_dps_exists: false,
      target_player_exists: false, target_dps_exists: false, warnings: [],
    });
    mocks.executeHostMigration.mockResolvedValue({ id: 'job-migrate', type: 'host_save_migration', status: 'waiting', progress: 0, message: 'queued', created_at: '' });
    taskMocks.waitForJob.mockImplementation(async (_id: string, onUpdate?: (job: unknown) => void) => {
      onUpdate?.({ id: 'job-migrate', type: 'host_save_migration', status: 'running', progress: 55, message: 'publishing world', created_at: '' });
      return { id: 'job-migrate', type: 'host_save_migration', status: 'success', progress: 100, message: 'completed', created_at: '' };
    });
  });

  it('recovers after an uncertain migration submission response and confirms the migrated source', async () => {
    const source = { id: 'import-1', name: '单人世界', kind: 'import', active: false, created_at: '', updated_at: '', warnings: [] };
    mocks.list.mockResolvedValue({ items: [source], active_status: status, runtime_save: runtimeSave });
    mocks.planHostMigration.mockResolvedValue({
      steam_id: '76561198000000000', source_uid: 'source', target_uid: 'target', strategy: 'direct', can_execute: true,
      source_player_file: 'Players/source.sav', source_dps_exists: false, target_player_exists: false, target_dps_exists: false, warnings: [],
    });
    const temporaryError = Object.assign(new Error('HTTP 502'), { name: 'ApiError', status: 502 });
    Object.setPrototypeOf(temporaryError, (await import('../api/client')).ApiError.prototype);
    mocks.executeHostMigration.mockRejectedValue(temporaryError);
    mocks.waitForSaveSourcesBackend.mockResolvedValue({
      items: [{ ...source, id: 'migrated-1', name: '单人世界（主机迁移）' }], active_status: status, runtime_save: runtimeSave,
    });
    vi.spyOn(window, 'prompt').mockReturnValue('76561198000000000');
    vi.spyOn(window, 'confirm').mockReturnValue(true);

    renderSaveSources();
    expect(await screen.findByText('单人世界')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '主机迁移' }));

    expect(await screen.findByText(/网关连接曾短暂中断/)).toBeInTheDocument();
    expect(mocks.waitForSaveSourcesBackend).toHaveBeenCalledTimes(1);
  });

  it('submits host migration as a background job and follows its progress', async () => {
    mocks.list.mockResolvedValue({
      items: [{ id: 'save-imported', name: 'Imported world', kind: 'import', active: false, created_at: '', updated_at: '' }],
      active_status: status,
      runtime_save: runtimeSave,
    });
    vi.spyOn(window, 'prompt').mockReturnValue('76561198000000000');
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    renderSaveSources();

    fireEvent.click(await screen.findByRole('button', { name: '主机迁移' }));

    await waitFor(() => expect(mocks.executeHostMigration).toHaveBeenCalledWith(
      'save-imported', '76561198000000000', 'Imported world（主机迁移）',
    ));
    await waitFor(() => expect(taskMocks.waitForJob).toHaveBeenCalledWith('job-migrate', expect.any(Function)));
    expect(await screen.findByText(/迁移完成：Imported world/)).toBeInTheDocument();
  });

});
