import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AxiosResponse } from 'axios';
import { apiClient } from './client';
import { mapAccessEntries, mapPlayer, mapSaveInventoryContainers, playersApi } from './players';

afterEach(() => vi.restoreAllMocks());

describe('mapAccessEntries', () => {
  it('treats null and non-array payloads as empty arrays', () => {
    expect(mapAccessEntries(null)).toEqual([]);
    expect(mapAccessEntries({})).toEqual([]);
  });

  it('maps array and wrapped players payloads', () => {
    const entry = { steam_id: '76561198000000001', nickname: 'Tester', reason: 'manual' };

    expect(mapAccessEntries([entry])).toMatchObject([entry]);
    expect(mapAccessEntries({ players: [entry] })).toMatchObject([entry]);
  });
});

describe('save player detail mappers', () => {
  it('maps one save-index player without treating binary data as display text', () => {
    expect(mapPlayer({
      steam_id: 'steam_1',
      player_uid: 'uid_1',
      nickname: 'Builder',
      level: 45,
      location_x: 10,
      location_y: 20,
      location_z: 30,
    })).toMatchObject({
      steam_id: 'steam_1',
      player_uid: 'uid_1',
      nickname: 'Builder',
      level: 45,
      x: 10,
      y: 20,
      z: 30,
    });
    expect(mapPlayer({ raw: '\u0000\u0001' })).toBeNull();
  });

  it('defaults missing online state to offline and maps backend online metadata', () => {
    expect(mapPlayer({ player_uid: 'uid-offline' })).toMatchObject({
      is_online: false,
      online_source: 'none',
      online_stale: false,
    });
    expect(mapPlayer({
      player_uid: 'uid-live',
      is_online: true,
      online_source: 'rest+paldefender',
      online_stale: true,
      gm_user_id: 'steam_76561198000000001',
    })).toMatchObject({
      is_online: true,
      online_source: 'rest+paldefender',
      online_stale: true,
      gm_user_id: 'steam_76561198000000001',
    });
  });


  it('maps player notes and tags', () => {
    expect(mapPlayer({
      player_uid: 'uid-note',
      note: '负责建筑规划',
      tags: ['建筑师', '活跃'],
      has_annotation: true,
      annotation_updated_at: '2026-07-24T00:00:00Z',
    })).toMatchObject({
      note: '负责建筑规划',
      tags: ['建筑师', '活跃'],
      has_annotation: true,
      annotation_updated_at: '2026-07-24T00:00:00Z',
    });
  });

  it('maps player presence totals and completed sessions', () => {
    expect(mapPlayer({
      player_uid: 'uid-presence',
      presence_available: true,
      session_seconds: 90,
      total_seconds: 3600,
      last_online_at: '2026-07-24T10:00:00Z',
      last_offline_at: '2026-07-24T11:00:00Z',
      presence_sessions: [{
        started_at: '2026-07-24T10:00:00Z',
        ended_at: '2026-07-24T11:00:00Z',
        duration_seconds: 3600,
      }],
    })).toMatchObject({
      presence_available: true,
      session_seconds: 90,
      total_seconds: 3600,
      presence_sessions: [{ duration_seconds: 3600 }],
    });
  });

  it('maps parsed inventory slots and ignores entries without an ItemID', () => {
    expect(mapSaveInventoryContainers({
      containers: [{
        container_id: 'bag_1',
        container_type: 'items',
        container_name: '普通背包',
        owner_type: 'player',
        owner_id: 'uid_1',
        slots: [
          { slot: 0, item_id: 'Money', item_name: '金币', count: 25, durability: 0 },
          { slot: 1, item_id: '', count: 1 },
        ],
      }],
    })).toEqual([{ container_id: 'bag_1', container_type: 'items', container_name: '普通背包', owner_type: 'player', owner_id: 'uid_1', slots: [{ slot: 0, item_id: 'Money', item_name: '金币', count: 25, durability: 0 }] }]);
  });
});

describe('player source requests', () => {
  it('adds the server source to list, detail, inventory, and annotation requests', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({ data: { ok: true, data: {} }, status: 200 } as AxiosResponse);
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({ data: { ok: true, data: {} }, status: 200 } as AxiosResponse);
    const del = vi.spyOn(apiClient, 'delete').mockResolvedValue({ data: { ok: true, data: {} }, status: 200 } as AxiosResponse);

    await playersApi.getPlayersList({ limit: 50 }, { source: 'server' });
    await playersApi.getPlayer('uid/live', 'server');
    await playersApi.getInventory('uid/live', 'server');
    await playersApi.updateAnnotation('uid/live', 'note', ['tag'], 'server');
    await playersApi.clearAnnotation('uid/live', 'server');

    expect(get).toHaveBeenNthCalledWith(1, '/players?limit=50&source=server');
    expect(get).toHaveBeenNthCalledWith(2, '/players/uid%2Flive?source=server');
    expect(get).toHaveBeenNthCalledWith(3, '/players/uid%2Flive/inventory?source=server');
    expect(put).toHaveBeenCalledWith('/players/uid%2Flive/annotation?source=server', { note: 'note', tags: ['tag'] });
    expect(del).toHaveBeenCalledWith('/players/uid%2Flive/annotation?source=server');
  });

  it('keeps world archive requests on the active source by default', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({ data: { ok: true, data: {} }, status: 200 } as AxiosResponse);

    await playersApi.getPlayersList({ limit: 50 });

    expect(get).toHaveBeenCalledWith('/players?limit=50');
  });
});
