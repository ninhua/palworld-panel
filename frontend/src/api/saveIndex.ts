import { apiClient, handleRequest } from './client';
import { emptySummary, mapSummary } from './entityList';
import type { MapEntitiesResponse, MapEntity, SaveIndexStatus } from '../types';

export const emptySaveIndexStatus: SaveIndexStatus = {
  enabled: false,
  state: 'disabled',
  stale: false,
  source_path: '',
  updated_at: '',
  duration_ms: 0,
  warnings: [],
  counts: {
    players: 0,
    guilds: 0,
    bases: 0,
    pals: 0,
    containers: 0,
    map_entities: 0,
  },
};

export const mapSaveIndexStatus = (raw: unknown): SaveIndexStatus => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const counts = data.counts && typeof data.counts === 'object' ? (data.counts as Record<string, unknown>) : {};
  return {
    enabled: Boolean(data.enabled),
    state: String(data.state || emptySaveIndexStatus.state),
    stale: Boolean(data.stale),
    source_path: String(data.source_path || ''),
    updated_at: String(data.updated_at || ''),
    duration_ms: Number(data.duration_ms || 0),
    error: data.error ? String(data.error) : undefined,
    error_code: data.error_code ? String(data.error_code) : undefined,
    error_detail: data.error_detail ? String(data.error_detail) : undefined,
    oodle_available: data.oodle_available == null ? undefined : Boolean(data.oodle_available),
    warnings: Array.isArray(data.warnings) ? data.warnings.map(String) : [],
    counts: {
      players: Number(counts.players || 0),
      guilds: Number(counts.guilds || 0),
      bases: Number(counts.bases || 0),
      pals: Number(counts.pals || 0),
      containers: Number(counts.containers || 0),
      map_entities: Number(counts.map_entities || 0),
    },
    parser: data.parser ? String(data.parser) : undefined,
    cache_path: data.cache_path ? String(data.cache_path) : undefined,
  };
};

export const mapMapEntity = (raw: unknown): MapEntity => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const location = data.location && typeof data.location === 'object' ? (data.location as Record<string, unknown>) : {};
  return {
    type: String(data.type || 'map_object'),
    id: String(data.id || ''),
    label: String(data.label || data.raw_label || data.id || '未知对象'),
    raw_label: data.raw_label ? String(data.raw_label) : undefined,
    x: Number(data.x ?? location.x ?? 0),
    y: Number(data.y ?? location.y ?? 0),
    z: Number(data.z ?? location.z ?? 0),
    is_online: data.is_online == null ? undefined : Boolean(data.is_online),
    live: data.live == null ? undefined : Boolean(data.live),
    source: String(data.source || 'save'),
    guild_id: data.guild_id ? String(data.guild_id) : undefined,
    guild_name: data.guild_name ? String(data.guild_name) : undefined,
    level: data.level == null ? undefined : Number(data.level),
    ping: data.ping == null ? undefined : Number(data.ping),
    owner_id: data.owner_id ? String(data.owner_id) : undefined,
    pals_count: data.pals_count == null ? undefined : Number(data.pals_count),
    location_type: data.location_type ? String(data.location_type) : undefined,
    status: data.status ? String(data.status) : undefined,
  };
};

export const mapMapEntitiesResponse = (raw: unknown): MapEntitiesResponse => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const live = data.live && typeof data.live === 'object' ? (data.live as Record<string, unknown>) : {};
  const entities = Array.isArray(data.entities) ? data.entities.map(mapMapEntity) : [];
  return {
    entities,
    status: data.status ? mapSaveIndexStatus(data.status) : emptySaveIndexStatus,
    summary: data.summary ? mapSummary(data.summary) : { ...emptySummary, total: entities.length, returned: entities.length },
    live: {
      available: Boolean(live.available),
      source: String(live.source || ''),
      online_players: Number(live.online_players || 0),
      refreshed_at: String(live.refreshed_at || ''),
      pals: (() => {
        const pals = live.pals && typeof live.pals === 'object' ? live.pals as Record<string, unknown> : {};
        return {
          available: Boolean(pals.available),
          real_time: Boolean(pals.real_time),
          source: String(pals.source || 'save_snapshot'),
          positions: Number(pals.positions || 0),
          updated_at: String(pals.updated_at || ''),
        };
      })(),
    },
  };
};

export const saveIndexApi = {
  getStatus: () =>
    handleRequest<unknown, SaveIndexStatus>(
      () => apiClient.get('/save/index/status'),
      emptySaveIndexStatus,
      { map: mapSaveIndexStatus, quiet: true },
    ),

  rebuild: () =>
    handleRequest<unknown, { status: SaveIndexStatus }>(
      () => apiClient.post('/save/index/rebuild'),
      { status: emptySaveIndexStatus },
      { quiet: true, fallbackOnError: false },
    ),

  getMapEntities: () =>
    handleRequest<unknown, MapEntitiesResponse>(
      () => apiClient.get('/map/entities'),
      {
        entities: [],
        status: emptySaveIndexStatus,
        summary: emptySummary,
        live: { available: false, source: '', online_players: 0, refreshed_at: '', pals: { available: false, real_time: false, source: 'save_snapshot', positions: 0, updated_at: '' } },
      },
      { map: mapMapEntitiesResponse, quiet: true },
    ),
};
