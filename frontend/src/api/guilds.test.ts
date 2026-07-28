import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AxiosResponse } from 'axios';
import { apiClient } from './client';
import { guildsApi } from './guilds';

afterEach(() => vi.restoreAllMocks());

describe('guild detail requests', () => {
  it('maps enriched members, annotations, bases, and encodes the guild id', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({
      data: {
        ok: true,
        data: {
          guild: { id: 'guild/1', name: 'Builders', owner_player_uid: 'uid-owner', members: [], base_ids: ['base-1'], online_member_count: 1 },
          members: [{
            id: 'steam-owner', player_uid: 'uid-owner', steam_id: 'steam-owner', nickname: 'Alice', level: 50,
            is_online: true, last_online_time: '2026-07-24T00:00:00Z', is_owner: true,
            note: '负责建筑', tags: ['建筑师'], has_annotation: true,
          }],
          bases: [{
            id: 'base-1', name: '北境制造中心', raw_name: 'raw', custom_name: '北境制造中心', has_custom_name: true,
            x: 10, y: 20, z: 30, structures_count: 12, workers_count: 8, status: 'Safe',
          }],
          status: { enabled: true, state: 'ready', stale: false, source_path: '', updated_at: '', duration_ms: 0, warnings: [], counts: { players: 1, guilds: 1, bases: 1, pals: 0, containers: 0, map_entities: 0 } },
          source_id: 'server',
        },
      },
      status: 200,
    } as AxiosResponse);

    const detail = await guildsApi.getGuild('guild/1');

    expect(get).toHaveBeenCalledWith('/guilds/guild%2F1');
    expect(detail.members[0]).toMatchObject({ nickname: 'Alice', is_owner: true, note: '负责建筑', tags: ['建筑师'] });
    expect(detail.bases[0]).toMatchObject({ name: '北境制造中心', workers_count: 8, x: 10 });
    expect(detail.source_id).toBe('server');
  });
});
