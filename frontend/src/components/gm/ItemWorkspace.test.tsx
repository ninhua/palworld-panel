import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ItemWorkspace } from './ItemWorkspace';

describe('ItemWorkspace collaboration filters', () => {
  it('shows Terraria and ULTRAKILL items through the collaboration filter', () => {
    render(<ItemWorkspace
      catalog={[
        { id: 'Stone', name: '石头', icon: 'stone' },
        { id: 'TerrariaSword', name: '泰拉刃', icon: 'terriasword', collaboration: 'terraria' },
        { id: 'Revolver', name: '神射左轮', icon: 'revolver', collaboration: 'ultrakill' },
      ] as never[]}
      inventoryLoading={false} canWrite={false} online={false} busy={false} pending=""
      onRefresh={vi.fn()} onGive={vi.fn()} onAdjust={vi.fn()}
    />);
    fireEvent.click(screen.getByRole('button', { name: '联动物品' }));
    expect(screen.queryByText('石头')).not.toBeInTheDocument();
    expect(screen.getByText('泰拉刃')).toBeInTheDocument();
    expect(screen.getByText('神射左轮')).toBeInTheDocument();
  });
});
