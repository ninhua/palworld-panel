import { describe, expect, it } from 'vitest';
import { mapMapEntitiesResponse, mapSaveIndexStatus } from './saveIndex';

describe('mapSaveIndexStatus', () => {
  it('preserves safe indexer diagnostics returned by the backend', () => {
    expect(mapSaveIndexStatus({
      enabled: true,
      state: 'error',
      error: 'save indexer failed (parser_incompatible)',
      error_code: 'parser_incompatible',
      error_detail: 'unknown property type',
      oodle_available: true,
    })).toMatchObject({
      error_code: 'parser_incompatible',
      error_detail: 'unknown property type',
      oodle_available: true,
    });
  });
});

describe('mapMapEntitiesResponse', () => {
  it('maps live coordinates and falls back to nested save coordinates', () => {
    const response = mapMapEntitiesResponse({
      entities: [
        { type: 'player', id: 'player-1', label: 'Builder', x: 10, y: 20, z: 30, is_online: true, live: true, source: 'live' },
        { type: 'base', id: 'base-1', raw_label: 'PalBoxV2', location: { x: 40, y: 50, z: 60 }, pals_count: 3 },
      ],
      live: { available: true, source: 'paldefender', online_players: 1, refreshed_at: '2026-07-16T00:00:00Z', pals: { available: true, real_time: false, source: 'save_snapshot', positions: 1, updated_at: '2026-07-16T00:00:00Z' } },
      summary: { total: 2, returned: 2, limit: 100, offset: 0, truncated: false },
    });

    expect(response.entities[0]).toMatchObject({ id: 'player-1', x: 10, y: 20, z: 30, is_online: true, source: 'live' });
    expect(response.entities[1]).toMatchObject({ id: 'base-1', label: 'PalBoxV2', x: 40, y: 50, z: 60, pals_count: 3, source: 'save' });
    expect(response.live).toEqual({ available: true, source: 'paldefender', online_players: 1, refreshed_at: '2026-07-16T00:00:00Z', pals: { available: true, real_time: false, source: 'save_snapshot', positions: 1, updated_at: '2026-07-16T00:00:00Z' } });
  });

  it('returns a safe empty response for invalid payloads', () => {
    const response = mapMapEntitiesResponse(null);
    expect(response.entities).toEqual([]);
    expect(response.live.available).toBe(false);
    expect(response.summary.total).toBe(0);
  });
});
