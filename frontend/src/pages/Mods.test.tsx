import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../api/client';
import { ServerStoreProvider } from '../store/ServerStoreProvider';
import { Mods } from './Mods';

const mocks = vi.hoisted(() => ({
  modsApi: {
    list: vi.fn(),
    workshopStatus: vi.fn(),
    workshopAuthStatus: vi.fn(),
    startWorkshopAuth: vi.fn(),
    verifyWorkshopAuth: vi.fn(),
    clearWorkshopAuth: vi.fn(),
    searchWorkshop: vi.fn(),
    getWorkshopItem: vi.fn(),
    translateWorkshop: vi.fn(),
    scanLocal: vi.fn(),
    actOnLocalFinding: vi.fn(),
    downloadWorkshop: vi.fn(),
    upload: vi.fn(),
    inspectImport: vi.fn(),
    selectImportCandidate: vi.fn(),
    importInspected: vi.fn(),
    setEnabled: vi.fn(),
    delete: vi.fn(),
  },
  serverApi: {
    getStatus: vi.fn(),
  },
  securityApi: {
    status: vi.fn(),
  },
  tasksApi: {
    waitForJob: vi.fn(),
  },
  authApi: {
    status: vi.fn(),
    me: vi.fn(),
  },
}));

vi.mock('../api/mods', () => ({
  modsApi: mocks.modsApi,
}));

vi.mock('../api/server', () => ({
  serverApi: mocks.serverApi,
}));

vi.mock('../api/security', () => ({
  securityApi: mocks.securityApi,
}));

vi.mock('../api/tasks', () => ({
  tasksApi: mocks.tasksApi,
}));

vi.mock('../api/auth', () => ({
  authApi: mocks.authApi,
}));

const renderMods = () =>
  render(
    <ServerStoreProvider>
      <Mods />
    </ServerStoreProvider>,
  );

describe('Mods Workshop store', () => {
  afterEach(() => {
    cleanup();
  });

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.authApi.status.mockResolvedValue({ initialized: true, authenticated: true, user: { name: 'operator', role: 'operator', permissions: ['read', 'mods:write'] } });
    mocks.modsApi.list.mockResolvedValue([]);
    mocks.modsApi.workshopStatus.mockResolvedValue({ configured: true, key_source: 'environment', app_id: '1623730' });
    mocks.modsApi.workshopAuthStatus.mockResolvedValue({
      supported: true, steamcmd_installed: true, credentials_secure: true, login_in_progress: false,
      logged_in: true, verification_required: false, password_configured: true, steam_guard_required: false, account_name: 'steam_account',
    });
    mocks.modsApi.startWorkshopAuth.mockResolvedValue({
      supported: true, steamcmd_installed: true, credentials_secure: true, login_in_progress: true,
      logged_in: true, verification_required: false, password_configured: true, steam_guard_required: false, account_name: 'steam_account', message: 'configured',
    });
    mocks.modsApi.verifyWorkshopAuth.mockResolvedValue({
      supported: true, steamcmd_installed: true, credentials_secure: true, login_in_progress: false,
      logged_in: true, verification_required: false, password_configured: true, steam_guard_required: false, account_name: 'steam_account', last_verified_at: '2026-07-15T12:00:00Z',
    });
    mocks.modsApi.clearWorkshopAuth.mockResolvedValue({
      supported: true, steamcmd_installed: true, credentials_secure: false, login_in_progress: false,
      logged_in: false, verification_required: true, password_configured: false, steam_guard_required: false,
    });
    mocks.modsApi.searchWorkshop.mockResolvedValue({ items: [], total: 0, page_size: 24 });
    mocks.modsApi.getWorkshopItem.mockResolvedValue({
      id: '123456789', title: 'Test Mod', summary: 'Original description', steam_url: 'https://steamcommunity.com/sharedfiles/filedetails/?id=123456789',
      tags: [], installed: false, enabled: false, update_available: false,
    });
    mocks.modsApi.translateWorkshop.mockResolvedValue({
      text: '中文译文', target_language: 'zh-CN', model: 'translate-model', generated_at: '2026-07-10T00:00:00Z', cached: false,
    });
    mocks.modsApi.scanLocal.mockResolvedValue({
      server_dir: 'D:\\Pal Server',
      scanned_at: '2026-07-15T10:00:00Z',
      findings: [{
        id: 'localmod_manual', revision: 'revision-1', ignored: false,
        ownership: 'manual', state: 'present', source: 'legacy_pak', confidence: 'high', name: '手动中文 Mod',
        package_name: 'ManualPackage', version: '1.0', enabled: true, duplicate: false,
        paths: ['D:\\Pal Server\\Pal\\Content\\Paks\\~mods\\Manual.pak'],
        classifications: ['manual', 'present'], issues: [],
        actions: [
          { action: 'import', available: false, confirmation_required: false, reason: 'Pak Mods cannot be imported safely by the current Info.json Workshop installer.' },
          { action: 'repair', available: false, confirmation_required: false, reason: 'Only Workshop Mods can be repaired.' },
          { action: 'ignore', available: true, confirmation_required: false },
          { action: 'unignore', available: false, confirmation_required: false },
          { action: 'delete', available: false, confirmation_required: true },
        ],
      }],
      skipped_paths: ['D:\\Pal Server\\Pal\\Binaries\\Win64\\Mods\\linked'],
      warnings: ['已跳过目录链接'],
    });
    mocks.modsApi.downloadWorkshop.mockResolvedValue({
      id: 'job_1',
      type: 'workshop_download',
      status: 'waiting',
      progress: 0,
      message: 'queued',
      created_at: new Date(0).toISOString(),
    });
    mocks.modsApi.actOnLocalFinding.mockImplementation(async (finding, action) => ({
      action,
      finding_id: finding.id,
      message: action === 'ignore' ? 'Local Mod finding is now ignored.' : 'done',
      scan: {
        server_dir: 'D:\\Pal Server', scanned_at: '2026-07-15T10:01:00Z', skipped_paths: [], warnings: [],
        findings: [{
          id: finding.id, revision: 'revision-2', ignored: action === 'ignore',
          ownership: 'manual', state: 'present', source: 'legacy_pak', confidence: 'high', name: '手动中文 Mod',
          package_name: 'ManualPackage', version: '1.0', enabled: true, duplicate: false,
          paths: ['D:\\Pal Server\\Pal\\Content\\Paks\\~mods\\Manual.pak'], classifications: ['manual', 'present'], issues: [],
          actions: [{ action: action === 'ignore' ? 'unignore' : 'ignore', available: true, confirmation_required: false }],
        }],
      },
    }));
    mocks.modsApi.inspectImport.mockResolvedValue({
      id: 'inspection_1', source_type: 'workshop', source: '123456789', expires_at: '2026-07-14T01:00:00Z',
      selected_candidate_id: 'candidate_1',
      candidates: [{ id: 'candidate_1', source_type: 'workshop', file_name: '123456789', action: 'unknown', ready: true, warnings: [] }],
    });
    mocks.modsApi.importInspected.mockResolvedValue({
      id: 'job_import', type: 'mod_import', status: 'waiting', progress: 0, message: 'queued', created_at: new Date(0).toISOString(),
    });
    mocks.serverApi.getStatus.mockResolvedValue({ pending_restart: false });
    mocks.securityApi.status.mockResolvedValue({
      installed: true,
      version: '1.8.3',
      release_source: 'github_latest',
      needs_first_start: false,
      files: { 'PalDefender.dll': true },
      paths: {},
      rest_api_enabled: false,
      warnings: [],
      load_verified: true,
      ue4ss: {
        state: 'installed',
        installed: true,
        version: 'v3.0.1',
        compatible: true,
        files: { 'UE4SS.dll': true },
        path: 'D:\\Pal Server\\Pal\\Binaries\\Win64',
        message: 'UE4SS v3.0.1 is installed and compatible.',
        load_verified: true,
      },
    });
    mocks.authApi.me.mockResolvedValue({ name: 'operator', role: 'operator', permissions: ['read', 'mods:write'] });
    mocks.tasksApi.waitForJob.mockResolvedValue({
      id: 'job_1',
      type: 'workshop_download',
      status: 'success',
      progress: 100,
      message: 'done',
      created_at: new Date(0).toISOString(),
    });
  });

  it('loads Workshop while configured explicit credentials are ready for downloads', async () => {
    renderMods();

    await waitFor(() => expect(mocks.modsApi.workshopAuthStatus).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(mocks.modsApi.searchWorkshop).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole('dialog', { name: '登录 Steam 以使用 Workshop' })).not.toBeInTheDocument();
  });

  it('loads Workshop without login and lets an administrator configure explicit credentials', async () => {
    mocks.authApi.status.mockResolvedValue({ initialized: true, authenticated: true, user: { name: 'admin', role: 'admin', permissions: ['read', 'mods:write', 'security:write'] } });
    mocks.authApi.me.mockResolvedValue({ name: 'admin', role: 'admin', permissions: ['read', 'mods:write', 'security:write'] });
    mocks.modsApi.workshopAuthStatus.mockResolvedValueOnce({
      supported: true, steamcmd_installed: true, credentials_secure: true, login_in_progress: false,
      logged_in: false, verification_required: true, password_configured: false, steam_guard_required: false,
    });
    renderMods();

    await waitFor(() => expect(mocks.modsApi.searchWorkshop).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: '配置 Steam 登录' }));
    const dialog = await screen.findByRole('dialog', { name: '登录 Steam 以使用 Workshop' });
    expect(dialog.querySelector('input[type="password"]')).not.toBeNull();

    fireEvent.change(screen.getByLabelText('Steam 账户名'), { target: { value: 'steam_account' } });
    fireEvent.change(screen.getByLabelText('Steam 密码'), { target: { value: 'fixture password' } });
    fireEvent.click(screen.getByRole('button', { name: '保存并验证登录' }));
    await waitFor(() => expect(mocks.modsApi.startWorkshopAuth).toHaveBeenCalledWith({ accountName: 'steam_account', password: 'fixture password', steamGuardCode: '' }));
    expect(screen.queryByRole('dialog', { name: '登录 Steam 以使用 Workshop' })).not.toBeInTheDocument();
  });

  it('prompts for a transient Steam Guard code without clearing the entered password', async () => {
    mocks.authApi.status.mockResolvedValue({ initialized: true, authenticated: true, user: { name: 'admin', role: 'admin', permissions: ['read', 'mods:write', 'security:write'] } });
    mocks.authApi.me.mockResolvedValue({ name: 'admin', role: 'admin', permissions: ['read', 'mods:write', 'security:write'] });
    mocks.modsApi.workshopAuthStatus.mockResolvedValue({
      supported: true, steamcmd_installed: true, credentials_secure: false, login_in_progress: false,
      logged_in: false, verification_required: true, password_configured: false, steam_guard_required: false,
    });
    mocks.modsApi.startWorkshopAuth
      .mockRejectedValueOnce(new ApiError('Steam Guard verification code is required', 409, 'steam_guard_required'))
      .mockResolvedValueOnce({
        supported: true, steamcmd_installed: true, credentials_secure: true, login_in_progress: false,
        logged_in: true, verification_required: false, password_configured: true, steam_guard_required: false, account_name: 'steam_account',
      });

    renderMods();
    fireEvent.click(await screen.findByRole('button', { name: '配置 Steam 登录' }));
    fireEvent.change(screen.getByLabelText('Steam 账户名'), { target: { value: 'steam_account' } });
    fireEvent.change(screen.getByLabelText('Steam 密码'), { target: { value: 'fixture password' } });
    fireEvent.click(screen.getByRole('button', { name: '保存并验证登录' }));

    expect(await screen.findByText('Steam 要求新的 Steam Guard 验证码，请输入后重新验证。')).toBeInTheDocument();
    expect(screen.getByLabelText('Steam 密码')).toHaveValue('fixture password');
    fireEvent.change(screen.getByLabelText('Steam Guard 验证码（需要时填写）'), { target: { value: '123456' } });
    fireEvent.click(screen.getByRole('button', { name: '保存并验证登录' }));
    await waitFor(() => expect(mocks.modsApi.startWorkshopAuth).toHaveBeenLastCalledWith({ accountName: 'steam_account', password: 'fixture password', steamGuardCode: '123456' }));
  });

  it('lets mods:write users use an already verified session without granting Steam login controls', async () => {
    mocks.authApi.status.mockResolvedValue({ initialized: true, authenticated: true, user: { name: 'operator', role: 'operator', permissions: ['read', 'mods:write'] } });
    mocks.authApi.me.mockResolvedValue({ name: 'operator', role: 'operator', permissions: ['read', 'mods:write'] });

    renderMods();

    await waitFor(() => expect(mocks.modsApi.searchWorkshop).toHaveBeenCalledTimes(1));
    expect(screen.queryByText(/本机管理员/)).not.toBeInTheDocument();
  });

  it('hides Steam credential controls from users without security permission', async () => {
    mocks.modsApi.workshopAuthStatus.mockResolvedValue({
      supported: true, steamcmd_installed: true, credentials_secure: true, login_in_progress: false,
      logged_in: false, verification_required: true, password_configured: false, steam_guard_required: false,
    });

    renderMods();

    await waitFor(() => expect(mocks.modsApi.searchWorkshop).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole('button', { name: '配置 Steam 登录' })).not.toBeInTheDocument();
  });

  it('keeps GitHub and HTTP(S) imports available while blocking Workshop imports behind Steam login', async () => {
    mocks.modsApi.workshopAuthStatus.mockResolvedValue({
      supported: true, steamcmd_installed: true, credentials_secure: true, login_in_progress: false,
      logged_in: false, verification_required: true, password_configured: false, steam_guard_required: false,
    });
    mocks.modsApi.inspectImport.mockResolvedValue({
      id: 'inspection_github', source_type: 'github_release', source: 'https://github.com/example/mod/releases/latest',
      expires_at: '2026-07-14T01:00:00Z', candidates: [],
    });

    renderMods();
    await waitFor(() => expect(mocks.modsApi.searchWorkshop).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: '导入 Mod' }));
    fireEvent.change(screen.getByLabelText('导入来源'), { target: { value: 'https://github.com/example/mod/releases/latest' } });
    fireEvent.click(screen.getByRole('button', { name: '检查' }));
    await waitFor(() => expect(mocks.modsApi.inspectImport).toHaveBeenCalledWith({ source: 'https://github.com/example/mod/releases/latest', file: undefined }));

    fireEvent.click(screen.getByRole('button', { name: '重新选择' }));
    fireEvent.change(screen.getByLabelText('导入来源'), { target: { value: '123456789' } });
    fireEvent.click(screen.getByRole('button', { name: '检查' }));
    expect(await screen.findByRole('dialog', { name: '登录 Steam 以使用 Workshop' })).toBeInTheDocument();
    expect(mocks.modsApi.inspectImport).toHaveBeenCalledTimes(1);
  });

  it('returns to the Steam gate when a verified Workshop session expires', async () => {
    mocks.modsApi.searchWorkshop.mockRejectedValue(new ApiError('Steam cache expired', 401, 'steam_login_required'));

    renderMods();

    const dialog = await screen.findByRole('dialog', { name: '登录 Steam 以使用 Workshop' });
    expect(within(dialog).getByText('Steam cache expired')).toBeInTheDocument();
    expect(mocks.modsApi.searchWorkshop).toHaveBeenCalledTimes(1);
  });

  it('shows read-only local findings and can rescan with viewer permission', async () => {
    mocks.authApi.status.mockResolvedValue({ initialized: true, authenticated: true, user: { name: 'viewer', role: 'viewer', permissions: ['read'] } });
    mocks.authApi.me.mockResolvedValue({ name: 'viewer', role: 'viewer', permissions: ['read'] });

    renderMods();
    fireEvent.click(screen.getByRole('button', { name: '本地检测' }));

    await waitFor(() => expect(mocks.modsApi.scanLocal).toHaveBeenCalledTimes(1));
    expect((await screen.findAllByText('手动中文 Mod')).length).toBeGreaterThan(0);
    expect(screen.getAllByText('Pak / LogicMods').length).toBeGreaterThan(0);
    expect(screen.getAllByText('高').length).toBeGreaterThan(0);
    expect(screen.getAllByText('文件存在').length).toBeGreaterThan(0);
    expect(screen.getAllByText('D:\\Pal Server\\Pal\\Content\\Paks\\~mods\\Manual.pak').length).toBeGreaterThan(0);
    expect(screen.getByText(/每次操作都会重新扫描并校验结果修订/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /删除 Mod/ })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '重新扫描' }));
    await waitFor(() => expect(mocks.modsApi.scanLocal).toHaveBeenCalledTimes(2));
  });

  it('shows detected UE4SS and PalDefender components in the installed view', async () => {
    renderMods();
    fireEvent.click(screen.getByRole('button', { name: '已安装' }));

    expect(await screen.findByText('运行组件')).toBeInTheDocument();
    expect(screen.getByText('UE4SS')).toBeInTheDocument();
    expect(screen.getByText('v3.0.1')).toBeInTheDocument();
    expect(screen.getByText('PalDefender')).toBeInTheDocument();
    expect(screen.getByText('1.8.3')).toBeInTheDocument();
    expect(screen.getAllByText('启动日志已确认')).toHaveLength(2);
  });

  it('persists a local finding ignore action through the revision-checked API', async () => {
    renderMods();
    fireEvent.click(screen.getByRole('button', { name: '本地检测' }));

    await screen.findAllByText('手动中文 Mod');
    fireEvent.click(screen.getAllByRole('button', { name: '忽略' })[0]);

    await waitFor(() => expect(mocks.modsApi.actOnLocalFinding).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'localmod_manual', revision: 'revision-1' }),
      'ignore',
      false,
    ));
    expect((await screen.findAllByText('已忽略')).length).toBeGreaterThan(0);
  });

  it('shows Steam API errors without replacing them with key setup text', async () => {
    mocks.modsApi.searchWorkshop.mockRejectedValue(new Error('Steam API returned HTTP 403: forbidden'));

    renderMods();

    expect(await screen.findByText('Steam API returned HTTP 403: forbidden')).toBeInTheDocument();
    expect(screen.queryByText(/STEAM_WEB_API_KEY/)).not.toBeInTheDocument();
  });

  it('keeps unified Workshop import available after store search fails', async () => {
    mocks.modsApi.searchWorkshop.mockRejectedValue(new Error('Steam API request timed out'));

    renderMods();

    expect(await screen.findByText('Steam API request timed out')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '导入 Mod' }));
    fireEvent.change(screen.getByLabelText('导入来源'), { target: { value: '123456789' } });
    fireEvent.click(screen.getByRole('button', { name: '检查' }));
    fireEvent.click(await screen.findByRole('button', { name: '确认导入' }));

    await waitFor(() => expect(mocks.modsApi.inspectImport).toHaveBeenCalledWith({ source: '123456789', file: undefined }));
    expect(mocks.modsApi.importInspected).toHaveBeenCalledWith('inspection_1', 'candidate_1');
  });

  it('downloads and inspects a selected GitHub ZIP candidate before import', async () => {
    mocks.modsApi.inspectImport.mockResolvedValue({
      id: 'inspection_github',
      source_type: 'github_release',
      source: 'https://github.com/example/mod/releases/latest',
      expires_at: '2026-07-14T01:00:00Z',
      candidates: [
        { id: 'candidate_one', source_type: 'github_asset', file_name: 'client.zip', action: 'unknown', ready: false },
        { id: 'candidate_two', source_type: 'github_asset', file_name: 'server.zip', action: 'unknown', ready: false },
      ],
    });
    mocks.modsApi.selectImportCandidate.mockResolvedValue({
      id: 'inspection_github',
      source_type: 'github_release',
      source: 'https://github.com/example/mod/releases/latest',
      expires_at: '2026-07-14T01:00:00Z',
      selected_candidate_id: 'candidate_two',
      candidates: [
        { id: 'candidate_one', source_type: 'github_asset', file_name: 'client.zip', action: 'unknown', ready: false },
        {
          id: 'candidate_two', source_type: 'github_asset', file_name: 'server.zip', name: 'Server Mod',
          package_name: 'ServerMod', version: '2.0', action: 'update', existing_mod_id: 'mod_existing', ready: true,
          warnings: ['The existing enabled state and record identity will be preserved.'],
        },
      ],
    });

    renderMods();
    fireEvent.click(await screen.findByRole('button', { name: '导入 Mod' }));
    fireEvent.change(screen.getByLabelText('导入来源'), { target: { value: 'https://github.com/example/mod/releases/latest' } });
    fireEvent.click(screen.getByRole('button', { name: '检查' }));
    fireEvent.change(await screen.findByLabelText('候选 ZIP'), { target: { value: 'candidate_two' } });

    await waitFor(() => expect(mocks.modsApi.selectImportCandidate).toHaveBeenCalledWith('inspection_github', 'candidate_two'));
    expect(await screen.findByText('更新现有 Mod')).toBeInTheDocument();
    expect(screen.getByText('ServerMod')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '确认导入' }));
    await waitFor(() => expect(mocks.modsApi.importInspected).toHaveBeenCalledWith('inspection_github', 'candidate_two'));
  });

  it('translates authoritative Workshop details and switches to Chinese', async () => {
    mocks.modsApi.searchWorkshop.mockResolvedValue({
      items: [{
        id: '123456789', title: 'Test Mod', summary: 'Short', steam_url: 'https://steamcommunity.com/sharedfiles/filedetails/?id=123456789',
        tags: [], installed: false, enabled: false, update_available: false,
      }],
      total: 1,
      page_size: 24,
    });
    renderMods();

    fireEvent.click(await screen.findByRole('button', { name: '查看详情' }));
    fireEvent.click(await screen.findByRole('button', { name: '翻译为中文' }));

    await waitFor(() => expect(mocks.modsApi.translateWorkshop).toHaveBeenCalledWith('123456789', false));
    expect(await screen.findByText('中文译文')).toBeInTheDocument();
    expect(screen.getByText(/translate-model/)).toBeInTheDocument();
  });
});
