import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { settingsApi } from '../api/settings';
import { serverApi } from '../api/server';
import { ServerStoreProvider } from '../store/ServerStoreProvider';
import { Settings } from './Settings';

const auxiliaryMocks = vi.hoisted(() => ({
	authStatus: vi.fn(),
  authMe: vi.fn(),
	listKeys: vi.fn(),
	createKey: vi.fn(),
	revokeKey: vi.fn(),
	changePassword: vi.fn(),
	clipboardWrite: vi.fn(),
  getAIConfig: vi.fn(),
  updateAIConfig: vi.fn(),
  testAIConfig: vi.fn(),
  getNetworkConfig: vi.fn(),
  updateNetworkConfig: vi.fn(),
  testNetworkProxy: vi.fn(),
}));

vi.mock('../api/auth', () => ({ authApi: {
	status: auxiliaryMocks.authStatus,
	me: auxiliaryMocks.authMe,
	listKeys: auxiliaryMocks.listKeys,
	createKey: auxiliaryMocks.createKey,
	revokeKey: auxiliaryMocks.revokeKey,
	changePassword: auxiliaryMocks.changePassword,
} }));
vi.mock('../api/aiTranslation', () => ({
  aiTranslationApi: {
    getConfig: auxiliaryMocks.getAIConfig,
    updateConfig: auxiliaryMocks.updateAIConfig,
    testConfig: auxiliaryMocks.testAIConfig,
  },
}));
vi.mock('../api/networkProxy', () => ({
  networkProxyApi: {
    getConfig: auxiliaryMocks.getNetworkConfig,
    updateConfig: auxiliaryMocks.updateNetworkConfig,
    test: auxiliaryMocks.testNetworkProxy,
  },
}));

vi.mock('../api/settings', () => ({
  settingsApi: {
    getSchema: vi.fn(),
    getSettings: vi.fn(),
    validateSettings: vi.fn(),
    updateSettings: vi.fn(),
    applySettings: vi.fn(),
    listRevisions: vi.fn(),
    getRevisionDiff: vi.fn(),
    restoreRevision: vi.fn(),
  },
}));

vi.mock('../api/server', () => ({
  serverApi: {
    getStatus: vi.fn(),
    getVersion: vi.fn(),
  },
}));

const renderSettings = () =>
  render(
    <ServerStoreProvider>
      <Settings />
    </ServerStoreProvider>,
  );

describe('Settings page', () => {
  afterEach(() => cleanup());

  beforeEach(() => {
    vi.clearAllMocks();
	Object.defineProperty(navigator, 'clipboard', {
	  configurable: true,
	  value: { writeText: auxiliaryMocks.clipboardWrite },
	});
	auxiliaryMocks.authStatus.mockResolvedValue({ initialized: true, authenticated: true, user: { name: 'admin', role: 'admin', permissions: ['read', 'config:write', 'ai:config', 'security:write'] } });
    auxiliaryMocks.authMe.mockResolvedValue({ name: 'admin', role: 'admin', permissions: ['read', 'config:write', 'ai:config', 'security:write'] });
	auxiliaryMocks.listKeys.mockResolvedValue([]);
	auxiliaryMocks.createKey.mockResolvedValue({ id: 'key_1', name: '本机自动化', prefix: 'ppk_example', token: 'ppk_full-once', created_at: '2026-07-14T00:00:00Z' });
	auxiliaryMocks.revokeKey.mockResolvedValue({ revoked: true });
	auxiliaryMocks.changePassword.mockResolvedValue({ password_changed: true, sessions_revoked: true, api_keys_revoked: true });
    auxiliaryMocks.getAIConfig.mockResolvedValue({ configured: false, base_url: '', model: '', api_key_present: false, timeout_seconds: 90, proxy_configured: false, proxy_url: '', custom_header_names: [] });
    auxiliaryMocks.updateAIConfig.mockResolvedValue({ configured: true, base_url: 'https://ai.example/v1', model: 'translate-model', api_key_present: true, timeout_seconds: 90, proxy_configured: false, proxy_url: '', custom_header_names: [] });
    auxiliaryMocks.testAIConfig.mockResolvedValue({ ok: true, base_url: 'https://ai.example/v1', model: 'translate-model', message: 'ok', timeout_seconds: 90, proxy_configured: false, custom_header_names: [] });
    auxiliaryMocks.getNetworkConfig.mockResolvedValue({
      install: { enabled: false, configured: false, url: '', authentication_configured: false, source: 'managed', requires_restart: false, effective_for_next_task: true },
      community: { enabled: false, configured: false, url: '', authentication_configured: false, source: 'managed', requires_restart: false, effective_for_next_task: true },
    });
    auxiliaryMocks.updateNetworkConfig.mockResolvedValue({
      install: { enabled: true, configured: true, url: 'http://127.0.0.1:7890', scheme: 'http', authentication_configured: true, source: 'managed', requires_restart: false, effective_for_next_task: true },
      community: { enabled: true, configured: true, url: 'socks5://127.0.0.1:10808', scheme: 'socks5', authentication_configured: false, source: 'managed', requires_restart: false, effective_for_next_task: true },
    });
    auxiliaryMocks.testNetworkProxy.mockResolvedValue({ ok: true, scope: 'install', target: 'https://steamcdn-a.akamaihd.net', latency_ms: 88, http_status: 206, proxy_scheme: 'http', proxy_enabled: true, message: 'ok' });

    vi.mocked(serverApi.getStatus).mockResolvedValue({
      status: 'stopped',
      installed: true,
      pending_restart: false,
      runtime_mode: 'wine_docker',
      setup_step: 'configured',
      config_exists: true,
      container: { exists: false, status: 'missing' },
      startup_args: [],
      ports: { game: 8211, rest: 8212 },
      warnings: [],
      paths: {},
      server_imported: false,
      settings_path: '/srv/PalWorldSettings.ini',
    });
    vi.mocked(serverApi.getVersion).mockResolvedValue({
      installed: true,
      current_build_id: '24088465',
      latest_build_id: '24088465',
      update_available: false,
      last_checked_at: '2026-07-10T00:00:00Z',
      source: 'test',
      manifest_path: '/srv/appmanifest_2394010.acf',
      game_version: 'v1.0.1.81201',
      compatibility_target: '1.0.1',
      compatible: true,
      compatibility_warnings: [],
    });

    vi.mocked(settingsApi.getSchema).mockResolvedValue({
      version: '1.0.0',
      fields: [
        {
          key: 'ServerName',
          label: '服务器名称',
          group: 'server_management',
          type: 'string',
          default: 'Palworld Server',
          requires_restart: true,
          description: '服务器名称。',
        },
        {
          key: 'DeathPenalty',
          label: '死亡惩罚',
          group: 'server_management',
          type: 'enum',
          default: 'All',
          enum: ['None', 'All'],
          enum_labels: {
            None: '不掉落',
            All: '全部掉落（物品、装备和队伍帕鲁）',
          },
          requires_restart: true,
          description: '死亡惩罚。',
        },
        {
          key: 'bEnableVoiceChat',
          label: '启用语音聊天',
          group: 'server_management',
          type: 'bool',
          default: 'False',
          requires_restart: true,
          description: '是否启用游戏内语音聊天。',
        },
      ],
    });

    vi.mocked(settingsApi.getSettings).mockResolvedValue({
      settings: { ServerName: '测试服', DeathPenalty: 'All' },
      path: '/srv/PalWorldSettings.ini',
      pending_restart: false,
      issues: [],
    });
    vi.mocked(settingsApi.validateSettings).mockResolvedValue({ valid: true, issues: [] });
    vi.mocked(settingsApi.updateSettings).mockResolvedValue({
      settings: { ServerName: '测试服', DeathPenalty: 'None' },
      path: '/srv/PalWorldSettings.ini',
      revision_sha256: 'abc123',
      secret_state: { admin_password: { configured: true }, server_password: { configured: true } },
      draft: { id: 'cfg_test', revision_sha256: 'abc123', status: 'draft', created_at: '2026-07-22T00:00:00Z', updated_at: '2026-07-22T00:00:00Z' },
      pending_restart: false,
      issues: [],
    });
    vi.mocked(settingsApi.applySettings).mockResolvedValue({
      id: 'job_config', type: 'palworld_config_apply', status: 'waiting', progress: 0,
      created_at: '2026-07-22T00:00:00Z', message: 'queued',
    });
    vi.mocked(settingsApi.listRevisions).mockResolvedValue({ current_revision_sha256: 'abc123', retention: 50, items: [] });
    vi.mocked(settingsApi.getRevisionDiff).mockResolvedValue({ revision_id: 'rev_1', revision_sha256: 'old', current_sha256: 'abc123', changes: [] });
    vi.mocked(settingsApi.restoreRevision).mockResolvedValue({
      settings: { ServerName: '历史服' }, path: '/srv/PalWorldSettings.ini', pending_restart: false,
      revision_sha256: 'abc123', issues: [],
      draft: { id: 'cfg_restore', revision_sha256: 'abc123', status: 'draft', created_at: '2026-07-29T00:00:00Z', updated_at: '2026-07-29T00:00:00Z' },
    });
  });

  it('shows localized field and enum labels while saving raw keys and enum values', async () => {
    renderSettings();

    expect(await screen.findByText('服务器名称')).toBeInTheDocument();
    expect(screen.getByText('配置规范 1.0.0')).toBeInTheDocument();
    expect(screen.getByText('v1.0.1.81201')).toBeInTheDocument();
    expect(screen.getByText(/当前未设置/)).toBeInTheDocument();
    expect(screen.getByText('ServerName')).toBeInTheDocument();
    expect(screen.getByText('全部掉落（物品、装备和队伍帕鲁）')).toBeInTheDocument();

    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'None' } });
    fireEvent.click(screen.getByRole('button', { name: /^保存$/ }));

    await waitFor(() => {
      expect(settingsApi.updateSettings).toHaveBeenCalledWith(
        expect.objectContaining({
          ServerName: '测试服',
          DeathPenalty: 'None',
        }),
      );
      expect(settingsApi.updateSettings).not.toHaveBeenCalledWith(
        expect.objectContaining({ bEnableVoiceChat: expect.anything() }),
      );
    });
    expect(await screen.findByRole('button', { name: '应用草稿' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '应用草稿' }));
    await waitFor(() => expect(settingsApi.applySettings).toHaveBeenCalledWith('cfg_test'));
  });

  it('shows a redacted warning for legacy unquoted password values', async () => {
    vi.mocked(settingsApi.getSettings).mockResolvedValue({
      settings: { ServerName: 'Visible' }, path: '/srv/PalWorldSettings.ini', pending_restart: false,
      revision_sha256: 'abc123',
      secret_state: { admin_password: { configured: true }, server_password: { configured: true } },
      format_issues: [{ field: 'ServerPassword', code: 'string_not_quoted', severity: 'warning', message: 'redacted' }],
      issues: [],
    });
    renderSettings();
    expect(await screen.findByText('密码字符串格式不正确，将在应用草稿时自动修复。')).toBeInTheDocument();
    expect(screen.queryByText(/admin-secret|join-secret/)).not.toBeInTheDocument();
  });

  it('uses password inputs and requires explicit secret clearing', async () => {
    vi.mocked(settingsApi.getSchema).mockResolvedValue({
      version: '1.0.0',
      fields: [{
        key: 'ServerPassword', label: '加入密码', group: 'server_management', type: 'string',
        default: '', requires_restart: true, description: '服务器加入密码',
      }],
    });
    vi.mocked(settingsApi.getSettings).mockResolvedValue({
      settings: {}, path: '/srv/PalWorldSettings.ini', pending_restart: false,
      secret_state: { admin_password: { configured: false }, server_password: { configured: true } }, issues: [],
    });
    renderSettings();
    const password = await screen.findByLabelText('加入密码');
    expect(password).toHaveAttribute('type', 'password');
    expect(password).toHaveAttribute('placeholder', '留空以保留现有密码');
    fireEvent.click(screen.getByLabelText('清除加入密码'));
    fireEvent.click(screen.getByRole('button', { name: /^保存$/ }));
    await waitFor(() => expect(settingsApi.updateSettings).toHaveBeenCalledWith(
      expect.any(Object), ['ServerPassword'],
    ));
  });

  it('saves AI configuration without requiring an API key replacement', async () => {
    auxiliaryMocks.getAIConfig.mockResolvedValue({ configured: true, base_url: 'https://ai.example/v1', model: 'old-model', api_key_present: true, timeout_seconds: 90, proxy_configured: false, proxy_url: '', custom_header_names: [] });
    renderSettings();

    const modelInput = await screen.findByLabelText('Model');
    const saveButton = screen.getByRole('button', { name: '保存 AI 配置' });

    fireEvent.change(modelInput, { target: { value: 'translate-model' } });
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(auxiliaryMocks.updateAIConfig).toHaveBeenCalledWith({
        base_url: 'https://ai.example/v1',
        model: 'translate-model',
        timeout_seconds: 90,
      });
    });
  });

  it('saves separate installation and community proxy credentials without displaying passwords', async () => {
    renderSettings();

    const proxyInputs = await screen.findAllByLabelText('代理 URL');
    fireEvent.change(proxyInputs[0], { target: { value: 'http://proxy-user:install-secret@127.0.0.1:7890' } });
    fireEvent.change(proxyInputs[1], { target: { value: 'socks5://community-user:community-secret@127.0.0.1:10808' } });
    const enableBoxes = screen.getAllByLabelText('启用');
    fireEvent.click(enableBoxes[0]);
    fireEvent.click(enableBoxes[1]);
    fireEvent.click(screen.getByRole('button', { name: '保存代理设置' }));

    await waitFor(() => {
      expect(auxiliaryMocks.updateNetworkConfig).toHaveBeenCalledWith({
        install_enabled: true,
        community_enabled: true,
        install_proxy_url: 'http://proxy-user:install-secret@127.0.0.1:7890',
        community_proxy_url: 'socks5://community-user:community-secret@127.0.0.1:10808',
      });
    });
    expect(screen.queryByText(/install-secret|community-secret/)).not.toBeInTheDocument();
    expect(await screen.findByText(/已保存：http:\/\/127\.0\.0\.1:7890/)).toBeInTheDocument();
  });

  it('saves AI timeout, proxy, and private custom headers without replacing existing secrets', async () => {
    auxiliaryMocks.getAIConfig.mockResolvedValue({
      configured: true,
      base_url: 'https://ai.example/v1',
      model: 'old-model',
      api_key_present: true,
      timeout_seconds: 45,
      proxy_configured: true,
      proxy_url: 'socks5://127.0.0.1:10808',
      custom_header_names: ['X-Tenant-ID'],
    });
    renderSettings();

    const timeoutInput = await screen.findByLabelText('请求超时（秒）');
    const proxyInput = await screen.findByLabelText(/Proxy URL/);
    const headersInput = await screen.findByLabelText(/自定义请求头/);
    const saveButton = screen.getByRole('button', { name: '保存 AI 配置' });

    fireEvent.change(timeoutInput, { target: { value: '120' } });
    fireEvent.change(proxyInput, { target: { value: 'socks5://proxy-user:proxy-pass@127.0.0.1:10808' } });
    fireEvent.change(headersInput, { target: { value: '{"X-Tenant-ID":"tenant-b"}' } });
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(auxiliaryMocks.updateAIConfig).toHaveBeenCalledWith({
        base_url: 'https://ai.example/v1',
        model: 'old-model',
        timeout_seconds: 120,
        proxy_url: 'socks5://proxy-user:proxy-pass@127.0.0.1:10808',
        custom_headers: { 'X-Tenant-ID': 'tenant-b' },
      });
    });
  });

  it('shows a development key once, copies it, and revokes it', async () => {
	renderSettings();

	await screen.findByText('开发密钥');
	fireEvent.click(screen.getByRole('button', { name: '创建' }));
	expect(await screen.findByText('ppk_full-once')).toBeInTheDocument();
	fireEvent.click(screen.getByRole('button', { name: '复制开发密钥' }));
	expect(auxiliaryMocks.clipboardWrite).toHaveBeenCalledWith('ppk_full-once');
	fireEvent.click(screen.getByRole('button', { name: '撤销 本机自动化' }));

	await waitFor(() => expect(auxiliaryMocks.revokeKey).toHaveBeenCalledWith('key_1'));
	expect((await screen.findAllByText(/已撤销/)).length).toBeGreaterThan(0);
  });

  it('changes the administrator password after confirmation', async () => {
	renderSettings();

	await screen.findByText('修改管理员密码');
	fireEvent.change(screen.getByLabelText('当前管理员密码'), { target: { value: 'strong-password-123' } });
	fireEvent.change(screen.getByLabelText('新管理员密码'), { target: { value: 'replacement-password-123' } });
	fireEvent.change(screen.getByLabelText('确认新管理员密码'), { target: { value: 'replacement-password-123' } });
	fireEvent.click(screen.getByRole('button', { name: '修改密码并重新登录' }));

	await waitFor(() => expect(auxiliaryMocks.changePassword).toHaveBeenCalledWith('strong-password-123', 'replacement-password-123'));
  });

  it('rejects mismatched password confirmation without calling the API', async () => {
	renderSettings();

	await screen.findByText('修改管理员密码');
	fireEvent.change(screen.getByLabelText('当前管理员密码'), { target: { value: 'strong-password-123' } });
	fireEvent.change(screen.getByLabelText('新管理员密码'), { target: { value: 'replacement-password-123' } });
	fireEvent.change(screen.getByLabelText('确认新管理员密码'), { target: { value: 'another-password-123' } });
	fireEvent.click(screen.getByRole('button', { name: '修改密码并重新登录' }));

	expect(await screen.findByText('两次输入的新密码不一致')).toBeInTheDocument();
	expect(auxiliaryMocks.changePassword).not.toHaveBeenCalled();
  });
});
