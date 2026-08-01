import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { PalWorkspace } from './PalWorkspace';

describe('PalWorkspace catalog modes', () => {
  it('hides advanced entities until the administrator enables advanced content', () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><PalWorkspace
      identifier="player" playerName="玩家" online={false} canWrite={false} canManageTemplates={false} available={false} busy={false} pending=""
      savePals={[]} passiveCatalog={[]} onRun={vi.fn()} onRelease={vi.fn()}
      palCatalog={[
        { id: 'SleeveRabbit', name: '兔绣袖', kind: 'standard', icon_url: '/assets/pals/sleeverabbit.png' },
        { id: 'PREDATOR_Garm_Quest', name: '狂暴化的猎狼', kind: 'advanced', icon_url: '/assets/pals/predator_garm_quest.png' },
      ] as never[]}
    /></QueryClientProvider>);

    expect(screen.getByText('兔绣袖')).toBeInTheDocument();
    expect(screen.queryByText('狂暴化的猎狼')).not.toBeInTheDocument();
    fireEvent.click(screen.getByLabelText('显示高级帕鲁内容'));
    expect(screen.getByText('狂暴化的猎狼')).toBeInTheDocument();
  });
});
