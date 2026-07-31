import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { SaveHistory } from './SaveHistory';

const mocks = vi.hoisted(() => ({ list: vi.fn(), diff: vi.fn() }));
vi.mock('../api/saveHistory', async () => {
  const actual = await vi.importActual<typeof import('../api/saveHistory')>('../api/saveHistory');
  return { ...actual, saveHistoryApi: mocks };
});

const counts = { players: 1, guilds: 0, bases: 0, pals: 0, containers: 1, map_entities: 0 };
const older = { id: '20260729T100000Z-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', fingerprint: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', generated_at: '2026-07-29T10:00:00Z', captured_at: '2026-07-29T10:00:01Z', parser: 'test', counts, size_bytes: 100 };
const newer = { id: '20260729T100500Z-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', fingerprint: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', generated_at: '2026-07-29T10:05:00Z', captured_at: '2026-07-29T10:05:01Z', parser: 'test', counts, size_bytes: 120 };

const renderPage = () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><SaveHistory /></QueryClientProvider>);
};

describe('SaveHistory page', () => {
  afterEach(() => cleanup());

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.list.mockResolvedValue({ source: { id: 'server', name: '当前服务器存档', kind: 'server' }, retention: 24, minimum_interval_seconds: 900, max_total_bytes: 536870912, total_bytes: 220, items: [newer, older] });
    mocks.diff.mockResolvedValue({
      from: older, to: newer,
      summary: {
        players_added: 0, players_removed: 0, players_changed: 1,
        guilds_added: 0, guilds_removed: 0, guilds_changed: 0,
        bases_added: 0, bases_removed: 0, bases_changed: 0,
        pals_added: 0, pals_removed: 0, pals_changed: 0,
        containers_added: 0, containers_removed: 0, containers_changed: 1,
        items_increased: 1, items_decreased: 0,
      },
      total: 1, limit: 200, offset: 0,
      event_total: 1,
      events: [{
        id: 'player:uid:Stone', category: 'items', kind: 'item_gained',
        actor_type: 'player', actor_id: 'uid', actor_label: 'Alice',
        subject_type: 'item', subject_id: 'Stone', subject_label: '石头',
        target_type: '', target_id: '', target_label: '', delta: 4,
        before: '0', after: '4', details: [], metadata: { item_id: 'Stone', equipment: 'false' }, inferred: true,
      }],
      items: [{ category: 'items', kind: 'increased', id: 'Stone', label: 'Stone', delta: 4, fields: [{ field: 'count', before: '0', after: '4' }] }],
    });
  });


  it('selects a meaningful baseline instead of the immediately adjacent autosave', async () => {
    const adjacent = { ...newer, id: '20260729T102900Z-cccccccccccccccccccccccccccccccc', fingerprint: 'cccccccccccccccccccccccccccccccc', generated_at: '2026-07-29T10:29:00Z', captured_at: '2026-07-29T10:29:01Z' };
    const latest = { ...newer, id: '20260729T103000Z-dddddddddddddddddddddddddddddddd', fingerprint: 'dddddddddddddddddddddddddddddddd', generated_at: '2026-07-29T10:30:00Z', captured_at: '2026-07-29T10:30:01Z' };
    mocks.list.mockResolvedValue({
      source: { id: 'server', name: '当前服务器存档', kind: 'server' },
      retention: 24, minimum_interval_seconds: 900, max_total_bytes: 536870912, total_bytes: 340,
      items: [latest, adjacent, older],
    });

    renderPage();

    await waitFor(() => expect(mocks.diff).toHaveBeenCalledWith(expect.objectContaining({
      from: older.id,
      to: latest.id,
    })));
    expect(mocks.diff).not.toHaveBeenCalledWith(expect.objectContaining({ from: adjacent.id, to: latest.id }));
  });

  it('selects the two latest snapshots and renders bounded item changes', async () => {
    renderPage();
    expect(await screen.findByText('当前服务器存档')).toBeInTheDocument();
    await waitFor(() => expect(mocks.diff).toHaveBeenCalledWith(expect.objectContaining({ from: older.id, to: newer.id, limit: 200, offset: 0 })));
    expect((await screen.findAllByText('Alice 获得物品')).length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText(/石头 ×4/).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('原始字段差异')).toBeInTheDocument();
  });
});
