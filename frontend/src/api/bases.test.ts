import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { basesApi } from './bases';

describe('base feature api', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('updates a custom name and maps the returned base metadata', async () => {
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({
      status: 200,
      data: {
        ok: true,
        data: {
          base: {
            id: 'base / 1',
            name: '北境制造中心',
            raw_name: '未命名据点',
            custom_name: '北境制造中心',
            has_custom_name: true,
            guild_name: 'Guild',
          },
        },
      },
    });

    const base = await basesApi.updateCustomName('base / 1', '北境制造中心');

    expect(put).toHaveBeenCalledWith('/bases/base%20%2F%201/name', { name: '北境制造中心' });
    expect(base).toMatchObject({
      id: 'base / 1',
      name: '北境制造中心',
      raw_name: '未命名据点',
      custom_name: '北境制造中心',
      has_custom_name: true,
    });
  });

  it('clears a custom name through the dedicated endpoint', async () => {
    const remove = vi.spyOn(apiClient, 'delete').mockResolvedValue({
      status: 200,
      data: {
        ok: true,
        data: {
          base: {
            id: 'base-1',
            name: '未命名据点',
            raw_name: '未命名据点',
            custom_name: '',
            has_custom_name: false,
          },
          deleted: true,
        },
      },
    });

    const base = await basesApi.clearCustomName('base-1');

    expect(remove).toHaveBeenCalledWith('/bases/base-1/name');
    expect(base.has_custom_name).toBe(false);
    expect(base.custom_name).toBe('');
  });

  it('loads and maps base storage containers', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({
      status: 200,
      data: {
        ok: true,
        data: {
          containers: [
            {
              container_id: 'container / 1',
              owner_type: 'map_object',
              owner_id: 'map-object / 1',
              container_type: 'PalFoodBox',
              container_name: '饲料箱',
              slots: [
                { slot: 0, item_id: 'BerrySeeds', item_name: '野莓种子', item_icon: 'berryseeds', count: 42, durability: null },
              ],
            },
          ],
          status: {
            enabled: true,
            state: 'ready',
            stale: false,
            source_path: '/save',
            updated_at: '2026-07-23T00:00:00Z',
            duration_ms: 1,
            warnings: [],
            counts: { players: 1, guilds: 1, bases: 1, pals: 1, containers: 1, map_entities: 0 },
          },
        },
      },
    });

    const storage = await basesApi.getStorage('base / 1');

    expect(get).toHaveBeenCalledWith('/bases/base%20%2F%201/storage');
    expect(storage.containers).toHaveLength(1);
    expect(storage.containers[0]).toMatchObject({
      container_type: 'PalFoodBox',
      container_name: '饲料箱',
    });
    expect(storage.containers[0].slots[0]).toMatchObject({
      item_id: 'BerrySeeds',
      item_name: '野莓种子',
      item_icon: 'berryseeds',
      count: 42,
    });
    expect(storage.status.state).toBe('ready');
  });
  it('loads and maps base feed box summaries', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({
      status: 200,
      data: {
        ok: true,
        data: {
          base: { id: 'base-1', name: '北境制造中心', guild_name: 'Guild' },
          feed_boxes: [
            {
              container_id: 'feed-1',
              container_type: 'PalFoodBox',
              container_name: '饲料箱',
              slots: [{ slot: 0, item_id: 'Baked_Berries', item_name: '烤野莓', item_icon: 'baked_berries', count: 60, durability: null }],
            },
          ],
          items: [{ item_id: 'Baked_Berries', item_name: '烤野莓', item_icon: 'baked_berries', count: 60, box_count: 1 }],
          summary: { box_count: 1, empty_box_count: 0, occupied_slots: 1, total_items: 60, item_types: 1 },
          status: { enabled: true, state: 'ready', stale: false, source_path: '/save', updated_at: '', duration_ms: 1, warnings: [], counts: { players: 1, guilds: 1, bases: 1, pals: 1, containers: 1, map_entities: 1 } },
          source_id: 'server',
        },
      },
    });

    const result = await basesApi.getFeedBoxes('base / 1');

    expect(get).toHaveBeenCalledWith('/bases/base%20%2F%201/feed-boxes');
    expect(result.base.name).toBe('北境制造中心');
    expect(result.feed_boxes[0]).toMatchObject({ container_name: '饲料箱', container_type: 'PalFoodBox' });
    expect(result.items[0]).toMatchObject({ item_name: '烤野莓', count: 60, box_count: 1 });
    expect(result.summary).toMatchObject({ box_count: 1, total_items: 60, item_types: 1 });
  });

  it('loads and maps base workers with indexed pal details', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({
      status: 200,
      data: {
        ok: true,
        data: {
          base: { id: 'base-1', name: '北境制造中心', guild_name: 'Guild' },
          workers: [
            {
              instance_id: 'pal-1',
              character_id: 'Anubis',
              species_name: '阿努比斯',
              name: '矿工',
              nickname: '矿工',
              level: 50,
              gender: 'male',
              rank: 4,
              status: 'Working',
              location_type: 'base',
              passives: ['工作狂'],
              raw_passives: ['Workaholic'],
              on_expedition: false,
            },
          ],
          summary: { total: 1, average_level: 50, max_level: 50, named_count: 1, species_count: 1 },
          status: { enabled: true, state: 'ready', stale: false, source_path: '/save', updated_at: '', duration_ms: 1, warnings: [], counts: { players: 1, guilds: 1, bases: 1, pals: 1, containers: 0, map_entities: 0 } },
          source_id: 'server',
        },
      },
    });

    const result = await basesApi.getWorkers('base-1');

    expect(get).toHaveBeenCalledWith('/bases/base-1/workers');
    expect(result.base.name).toBe('北境制造中心');
    expect(result.workers[0]).toMatchObject({ name: '矿工', species_name: '阿努比斯', level: 50, rank: 4 });
    expect(result.summary).toMatchObject({ total: 1, average_level: 50, species_count: 1 });
  });

});
