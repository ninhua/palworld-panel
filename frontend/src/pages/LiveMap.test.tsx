import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { LiveMap } from './LiveMap';

const mocks = vi.hoisted(() => ({
  getMapEntities: vi.fn(),
  rebuild: vi.fn(),
  loadManifest: vi.fn(),
  loadPois: vi.fn(),
}));

vi.mock('../api/saveIndex', () => ({ saveIndexApi: mocks }));
vi.mock('../i18n', () => ({ useI18n: () => ({ locale: 'zh-CN', t: (key: string) => key }) }));
vi.mock('../map/palopsMap', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../map/palopsMap')>();
  return {
    ...actual,
    loadPalOpsMapManifest: mocks.loadManifest,
    loadPalOpsPois: mocks.loadPois,
  };
});
vi.mock('../components/map/PalOpsMapViewport', () => ({
  PalOpsMapViewport: ({ markers, onSelect }: { markers: Array<{ key: string; label: string }>; onSelect: (marker: unknown) => void }) => (
    <div aria-label="PalOps map fixture">
      {markers.map((marker) => <button key={marker.key} type="button" onClick={() => onSelect(marker)}>{marker.label} 地图标记</button>)}
    </div>
  ),
}));

const renderPage = () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter initialEntries={['/map']}>
      <QueryClientProvider client={client}>
        <LiveMap />
      </QueryClientProvider>
    </MemoryRouter>,
  );
};

describe('LiveMap', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.rebuild.mockResolvedValue({ status: 'waiting' });
    mocks.loadManifest.mockResolvedValue({
      schema_version: 1,
      source: { repository: 'CoderYiXin/PalOpsWeb', commit: 'dc2ec173c77e759482e59d9b63d228c88132061c', version: '1.3.2' },
      maps: ['palpagos', 'world-tree'],
      locales: ['zh-CN', 'en-US', 'ja-JP'],
      poi_total: 2,
      category_counts: { 'poi-location-fast-travel': 1, 'poi-resource-oil': 1 },
      tiles_available: true,
      tile_policy: 'fixture',
    });
    mocks.loadPois.mockResolvedValue([
      {
        id: 'poi-fast-travel-1', type: 'fast-travel', category: 'poi-location-fast-travel', map: 'palpagos',
        name: '樱花岛快速传送', aliases: ['Sakurajima'], keywords: ['fast travel'], mapX: -696, mapY: 87,
        worldX: -83955, worldY: -161464, source: 'fixture', license: 'CC-BY-SA-4.0', version: 'fixture', iconId: 'fast-travel',
      },
      {
        id: 'poi-oil-1', type: 'oil', category: 'poi-resource-oil', map: 'palpagos',
        name: '原油节点', aliases: [], keywords: ['oil'], mapX: -500, mapY: 100,
        worldX: -78000, worldY: -71000, source: 'fixture', license: 'CC-BY-SA-4.0', version: 'fixture', iconId: 'oil',
      },
    ]);
    mocks.getMapEntities.mockResolvedValue({
      entities: [
        { type: 'player', id: 'player-1', label: 'Builder', x: -83955, y: -161464, z: 30, is_online: true, live: true, source: 'live', guild_name: 'Builders' },
        { type: 'base', id: 'base-1', label: '主基地', x: -80000, y: -150000, z: 20, source: 'save', pals_count: 12 },
        { type: 'pal', id: 'pal-1', label: '捣蛋猫', x: -70000, y: -140000, z: 0, source: 'save', level: 8 },
      ],
      status: { enabled: true, available: true, stale: false, building: false, parser_available: true, counts: {}, warnings: [] },
      summary: { total: 3, returned: 3, limit: 100, offset: 0, truncated: false },
      live: { available: true, source: 'paldefender', online_players: 1, refreshed_at: '2026-07-16T00:00:00Z' },
    });
  });

  afterEach(() => cleanup());

  it('combines PalOps fixed POIs with PalPanel runtime entities', async () => {
    renderPage();

    expect(await screen.findByText('PalOps MapLibre 离线世界地图')).toBeInTheDocument();
    expect(await screen.findByText('樱花岛快速传送 地图标记')).toBeInTheDocument();
    expect(await screen.findByText('Builder 地图标记')).toBeInTheDocument();
    expect(screen.queryByText('原油节点 地图标记')).not.toBeInTheDocument();
    expect(screen.queryByText('捣蛋猫 地图标记')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '资源' }));
    expect(screen.getByText('原油节点 地图标记')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '帕鲁实体' }));
    expect(screen.getByText('捣蛋猫 地图标记')).toBeInTheDocument();
  });

  it('stores fixed POI exploration state in the browser', async () => {
    renderPage();
    fireEvent.click(await screen.findByText('樱花岛快速传送 地图标记'));
    fireEvent.click(screen.getByRole('button', { name: '标记为已发现' }));
    expect(JSON.parse(localStorage.getItem('palpanel-palops-explored:2026.07.5-extended') || '[]')).toContain('poi-fast-travel-1');
  });
});
