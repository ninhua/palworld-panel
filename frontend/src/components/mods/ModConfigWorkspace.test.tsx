import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ModConfigWorkspace } from './ModConfigWorkspace';

const mocks = vi.hoisted(() => ({
  listAdapters: vi.fn(), getAdapter: vi.fn(), saveAdapter: vi.fn(),
  listAdapterBackups: vi.fn(), restoreAdapter: vi.fn(), listFiles: vi.fn(),
  getFile: vi.fn(), saveFile: vi.fn(), listFileBackups: vi.fn(), restoreFile: vi.fn(),
  runAction: vi.fn(),
  reloadConfig: vi.fn(),
}));

vi.mock('../../api/modConfigurations', () => ({ modConfigurationsApi: mocks }));
vi.mock('../../api/security', () => ({ securityApi: { reloadConfig: mocks.reloadConfig } }));
vi.mock('./palzones/PalZonesEditorWorkspace', () => ({ default: () => <div>PalZones 专用编辑器已加载</div> }));

const file = {
  id: 'opaque-lua', name: 'main.lua', path: 'Scripts/main.lua', extension: '.lua', size: 20,
  modified_at: '2026-07-18T08:00:00Z', revision: 'revision-1', executable: true,
  risk: 'Lua 会由 UE4SS 执行。',
};
const document = {
  file,
  content: 'local BaseRange = 2\n',
  format: 'lua',
  fields: [{ path: 'BaseRange', label: '基地范围倍率', type: 'number' as const, value: 2, min: 1, max: 10 }],
};

describe('ModConfigWorkspace', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    mocks.listAdapters.mockResolvedValue([{
      id: 'extended-base-range', name: 'Extended Base Range', description: '范围配置', workshop_id: '3625907101',
      available: true, reload_behavior: 'restart_required', files: [file],
    }]);
    mocks.getAdapter.mockResolvedValue(document);
    mocks.listAdapterBackups.mockResolvedValue([{ id: 'backup-1', revision: 'old-revision', size: 18, created_at: '2026-07-17T08:00:00Z' }]);
    mocks.saveAdapter.mockResolvedValue({ ...document, file: { ...file, revision: 'revision-2' }, content: 'local BaseRange = 3\n' });
    mocks.restoreAdapter.mockResolvedValue({ ...document, file: { ...file, revision: 'revision-3' }, content: 'local BaseRange = 1\n' });
  });

  it('edits a typed popular Mod field and requires Lua confirmation before saving', async () => {
    render(<ModConfigWorkspace mods={[]} localFindings={[]} canWrite canReloadPalDefender={false} />);

    expect(await screen.findByText('Extended Base Range')).toBeInTheDocument();
    expect(await screen.findByText('Lua 可执行代码')).toBeInTheDocument();
    fireEvent.change(screen.getByRole('spinbutton'), { target: { value: '3' } });
    expect(screen.getByText('修改差异')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }));

    await waitFor(() => expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('Lua 是可执行代码')));
    expect(mocks.saveAdapter).toHaveBeenCalledWith('extended-base-range', 'opaque-lua', 'local BaseRange = 3\n', 'revision-1', true);
    expect(await screen.findByText('配置已保存，需要安全重启服务器后生效。')).toBeInTheDocument();
  });

  it('keeps the editor read-only without mods:write and disables restore', async () => {
    render(<ModConfigWorkspace mods={[]} localFindings={[]} canWrite={false} canReloadPalDefender={false} />);

    const editor = await screen.findByLabelText('Mod 配置原始文件');
    expect(editor).toHaveAttribute('readonly');
    expect(screen.getByRole('button', { name: '保存修改' })).toBeDisabled();
    expect(await screen.findByRole('button', { name: '恢复' })).toBeDisabled();
    expect(mocks.saveAdapter).not.toHaveBeenCalled();
  });

  it('restores a selected backup with the current revision', async () => {
    render(<ModConfigWorkspace mods={[]} localFindings={[]} canWrite canReloadPalDefender={false} />);

    fireEvent.click(await screen.findByRole('button', { name: '恢复' }));
    await waitFor(() => expect(mocks.restoreAdapter).toHaveBeenCalledWith('extended-base-range', 'opaque-lua', 'backup-1', 'revision-1'));
    expect(await screen.findByText('历史版本已恢复。')).toBeInTheDocument();
  });

  it('initializes an installed PalZones adapter only after an explicit click', async () => {
    const zonesFile = { ...file, id: 'zones', name: 'zones.json', path: 'Config/zones.json', extension: '.json', executable: false };
    const configured = {
      id: 'palzones', name: 'PalZones', description: '区域权限配置', available: true,
      installed: true, configured: true, enabled: true, status: 'restart_required' as const,
      status_detail: '等待安全重启', dependencies: [], actions: [],
      reload_behavior: 'restart_required', files: [zonesFile],
    };
    mocks.listAdapters.mockResolvedValue([{
      ...configured, configured: false, status: 'not_configured', files: [],
      actions: [{ id: 'initialize', available: true, restart_required: true }],
    }]);
    mocks.runAction.mockResolvedValue({
      adapter: configured,
      document: { file: zonesFile, content: '{\n  "global": {"permissions": {}},\n  "zones": []\n}\n', format: 'json', fields: [] },
      changed: ['Config/zones.json'], restart_required: true,
    });

    render(<ModConfigWorkspace mods={[]} localFindings={[]} canWrite canReloadPalDefender={false} />);

    expect(await screen.findByText('尚未创建区域配置')).toBeInTheDocument();
    expect(mocks.runAction).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: '创建默认配置' }));
    await waitFor(() => expect(mocks.runAction).toHaveBeenCalledWith('palzones', 'initialize'));
    expect(await screen.findByText('PalZones 专用编辑器已加载')).toBeInTheDocument();
  });
});
