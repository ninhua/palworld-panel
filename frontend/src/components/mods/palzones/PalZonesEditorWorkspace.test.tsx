import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { PalZonesEditorWorkspace } from './PalZonesEditorWorkspace';

vi.mock('./PalZonesMapCanvas', () => ({
  PalZonesMapCanvas: ({ onCreateZone }: { onCreateZone: (points: Array<{ x: number; y: number }>) => void }) => (
    <button type={'button'} onClick={() => onCreateZone([{ x: 0, y: 0 }, { x: 1000, y: 0 }, { x: 0, y: 1000 }])}>模拟绘制</button>
  ),
}));

const file = {
  id: 'zones', name: 'zones.json', path: 'Config/zones.json', extension: '.json', size: 64,
  modified_at: '2026-07-24T00:00:00Z', revision: 'revision-1', executable: false,
};

describe('PalZonesEditorWorkspace', () => {
  it('draws, edits, validates, and saves a Chinese zone draft', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(<PalZonesEditorWorkspace
      document={{ file, content: JSON.stringify({ global: { permissions: {} }, zones: [] }), format: 'json', fields: [] }}
      canWrite
      saving={false}
      onSave={onSave}
    />);

    fireEvent.click(screen.getByRole('button', { name: '模拟绘制' }));
    fireEvent.change(screen.getByLabelText('区域名称'), { target: { value: '新手保护区' } });
    fireEvent.click(screen.getByRole('switch', { name: '允许玩家建造' }));
    fireEvent.click(screen.getByRole('button', { name: '世界树' }));
    fireEvent.click(screen.getByRole('button', { name: '主世界' }));
    fireEvent.click(screen.getByRole('button', { name: '保存区域配置' }));

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    const output = JSON.parse(onSave.mock.calls[0][0]);
    expect(output.zones[0]).toMatchObject({ name: '新手保护区', levelRequirement: 1 });
    expect(output.zones[0].permissions.Player.world).not.toContain('Build');
    expect(output.zones[0].points).toEqual([
      { x: '0.00', y: '0.00' }, { x: '1000.00', y: '0.00' }, { x: '0.00', y: '1000.00' },
    ]);
  });
});
