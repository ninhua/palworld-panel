import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';
import { SaveDataTabs } from './SaveDataTabs';
import { navGroups } from '../../routes';

describe('SaveDataTabs', () => {
  afterEach(() => cleanup());

  it('keeps one sidebar entry and exposes all save and player-management views as page tabs', () => {
    const worldLabels = navGroups.find((group) => group.id === 'world')?.items.map((item) => item.navLabel) ?? [];
    expect(worldLabels).toEqual(expect.arrayContaining(['玩家中心', '存档中心', '世界档案', '帕鲁仓库', '配种实验室', '世界地图']));
    expect(worldLabels).not.toContain('玩家管理');
    expect(worldLabels).not.toContain('公会列表');
    expect(worldLabels).not.toContain('基地列表');
    expect(worldLabels).not.toContain('帕鲁管理');

    render(
      <MemoryRouter initialEntries={['/bases']}>
        <SaveDataTabs />
      </MemoryRouter>,
    );

    expect(screen.getByRole('link', { name: '玩家中心' })).toHaveAttribute('href', '/player-center');
    expect(screen.getByRole('link', { name: '玩家档案' })).toHaveAttribute('href', '/world');
    expect(screen.getByRole('link', { name: '公会' })).toHaveAttribute('href', '/guilds');
    expect(screen.getByRole('link', { name: '基地' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('link', { name: '帕鲁仓库' })).toHaveAttribute('href', '/pal-inventory');
    expect(screen.getByRole('link', { name: '配种实验室' })).toHaveAttribute('href', '/breeding');
    expect(screen.getByRole('link', { name: '世界地图' })).toHaveAttribute('href', '/map');
  });
});
