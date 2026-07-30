import { apiClient, handleRequest } from './client';
import { emptySummary, entityListQuery, mapSummary } from './entityList';
import { emptySaveIndexStatus, mapSaveIndexStatus } from './saveIndex';
import { palDefenderGMApi } from './paldefenderGM';
import type {
  EntityListParams,
  Player,
  PlayerAccessEntry,
  PlayerDataView,
  PlayerListResponse,
  SaveInventoryContainer,
  SavePlayerDetail,
  SavePlayerInventory,
  UnsupportedActionResult,
} from '../types';

type PlayerSource = 'server';
type PlayerSourceOptions = { source?: PlayerSource };

const defaultPlayerView = (source?: PlayerSource): PlayerDataView => ({
  scope: source === 'server' ? 'server' : 'active',
  source_id: source === 'server' ? 'server' : '',
  source_kind: 'server',
  source_name: '',
  online_overlay: true,
});

const mapPlayerView = (raw: unknown, source?: PlayerSource): PlayerDataView => {
  const view = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const fallback = defaultPlayerView(source);
  return {
    scope: view.scope === 'server' ? 'server' : 'active',
    source_id: String(view.source_id || fallback.source_id),
    source_kind: view.source_kind === 'import' ? 'import' : 'server',
    source_name: String(view.source_name || ''),
    online_overlay: typeof view.online_overlay === 'boolean' ? view.online_overlay : fallback.online_overlay,
  };
};

const withPlayerSource = (path: string, source?: PlayerSource) => source ? `${path}${path.includes('?') ? '&' : '?'}source=${source}` : path;

export const mapPlayer = (raw: unknown): Player | null => {
  const player = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const steamId = String(player.userId || player.steam_id || player.playerId || player.player_uid || '').trim();
  const playerUid = String(player.player_uid || '').trim();
  if (!steamId && !playerUid) return null;
  return {
    id: steamId || playerUid,
    steam_id: steamId,
    player_uid: playerUid,
    nickname: String(player.name || player.nickname || player.playerName || '未知玩家'),
    level: Number(player.level || 0),
    guild_id: String(player.guild_id || ''),
    guild_name: String(player.guild_name || player.guild || '未知公会'),
    is_online: Boolean(player.is_online),
    online_source: player.online_source === 'rest'
      || player.online_source === 'paldefender'
      || player.online_source === 'rest+paldefender'
      ? player.online_source
      : 'none',
    online_stale: Boolean(player.online_stale),
    gm_user_id: player.gm_user_id ? String(player.gm_user_id) : undefined,
    last_online_time: String(player.last_online_time || ''),
    x: Number(player.location_x || player.x || 0),
    y: Number(player.location_y || player.y || 0),
    z: Number(player.location_z || player.z || 0),
    ping: player.ping ? Number(player.ping) : undefined,
    ip: player.ip ? String(player.ip) : undefined,
    inventory_summary:
      player.inventory_summary && typeof player.inventory_summary === 'object'
        ? (player.inventory_summary as Record<string, unknown>)
        : undefined,
    note: String(player.note || ''),
    tags: Array.isArray(player.tags) ? player.tags.map((tag) => String(tag)).filter(Boolean) : [],
    has_annotation: Boolean(player.has_annotation),
    annotation_updated_at: String(player.annotation_updated_at || ''),
    presence_available: Boolean(player.presence_available),
    presence_stale: Boolean(player.presence_stale),
    presence_observed_at: String(player.presence_observed_at || ''),
    presence_online: Boolean(player.presence_online),
    session_seconds: Math.max(0, Number(player.session_seconds || 0)),
    total_seconds: Math.max(0, Number(player.total_seconds || 0)),
    session_started_at: String(player.session_started_at || ''),
    last_seen_at: String(player.last_seen_at || ''),
    last_online_at: String(player.last_online_at || ''),
    last_offline_at: String(player.last_offline_at || ''),
    presence_sessions: Array.isArray(player.presence_sessions)
      ? player.presence_sessions.map((rawSession) => {
          const session = (rawSession && typeof rawSession === 'object' ? rawSession : {}) as Record<string, unknown>;
          return {
            started_at: String(session.started_at || ''),
            ended_at: String(session.ended_at || ''),
            duration_seconds: Math.max(0, Number(session.duration_seconds || 0)),
          };
        })
      : [],
  };
};

const mapPlayers = (raw: unknown): Player[] => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const body = data.body && typeof data.body === 'object' ? (data.body as Record<string, unknown>) : data;
  const list = Array.isArray(body.players) ? body.players : Array.isArray(raw) ? raw : [];

  return list.flatMap((item) => {
    const player = mapPlayer(item);
    return player ? [player] : [];
  });
};

export const mapSaveInventoryContainers = (raw: unknown): SaveInventoryContainer[] => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const containers = Array.isArray(raw) ? raw : Array.isArray(data.containers) ? data.containers : [];
  return containers.flatMap((rawContainer) => {
    const container = (rawContainer && typeof rawContainer === 'object' ? rawContainer : {}) as Record<string, unknown>;
    const containerId = String(container.container_id || '').trim();
    if (!containerId) return [];
    const slots = Array.isArray(container.slots)
      ? container.slots.flatMap((rawSlot) => {
          const slot = (rawSlot && typeof rawSlot === 'object' ? rawSlot : {}) as Record<string, unknown>;
          const itemId = String(slot.item_id || '').trim();
          if (!itemId) return [];
          return [{
            slot: Number(slot.slot || 0),
            item_id: itemId,
            item_name: String(slot.item_name || itemId),
            count: Number(slot.count || 0),
            durability: Number(slot.durability || 0),
          }];
        })
      : [];
    return [{
      container_id: containerId,
      container_type: String(container.container_type || 'player'),
      container_name: String(container.container_name || '玩家背包'),
      owner_type: String(container.owner_type || 'player'),
      owner_id: String(container.owner_id || ''),
      slots,
    }];
  });
};

const mapPlayersList = (raw: unknown, source?: PlayerSource): PlayerListResponse => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const items = mapPlayers(raw);
  return {
    items,
    status: data.status ? mapSaveIndexStatus(data.status) : emptySaveIndexStatus,
    summary: data.summary ? mapSummary(data.summary) : { ...emptySummary, total: items.length, returned: items.length },
    view: mapPlayerView(data.view, source),
  };
};

export const mapAccessEntries = (raw: unknown): PlayerAccessEntry[] => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const list = Array.isArray(raw) ? raw : Array.isArray(data.players) ? data.players : [];
  return list.flatMap((item) => {
    const data = (item && typeof item === 'object' ? item : {}) as Record<string, unknown>;
    const steamId = String(data.steam_id || '').trim();
    if (!steamId) return [];
    return [
      {
        steam_id: steamId,
        nickname: data.nickname ? String(data.nickname) : undefined,
        reason: data.reason ? String(data.reason) : undefined,
        created_at: data.created_at ? String(data.created_at) : undefined,
        updated_at: data.updated_at ? String(data.updated_at) : undefined,
      },
    ];
  });
};

export const playersApi = {
  getPlayersList: (params: EntityListParams = {}, options: PlayerSourceOptions = {}) =>
    handleRequest<unknown, PlayerListResponse>(
      () => apiClient.get(withPlayerSource(`/players${entityListQuery(params)}`, options.source)),
      { items: [], status: emptySaveIndexStatus, summary: emptySummary, view: defaultPlayerView(options.source) },
      {
        map: (raw) => mapPlayersList(raw, options.source),
        quiet: true,
      },
    ),

  getPlayers: () =>
    handleRequest<unknown, Player[]>(() => apiClient.get('/players'), [], {
      map: mapPlayers,
      quiet: true,
    }),

  getPlayer: (identifier: string, source?: PlayerSource) =>
    handleRequest<unknown, SavePlayerDetail>(
      () => apiClient.get(withPlayerSource(`/players/${encodeURIComponent(identifier)}`, source)),
      { player: mapPlayer({ player_uid: identifier })!, status: emptySaveIndexStatus, view: defaultPlayerView(source) },
      {
        map: (raw) => {
          const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
          return {
            player: mapPlayer(data.player) || mapPlayer({ player_uid: identifier })!,
            status: data.status ? mapSaveIndexStatus(data.status) : emptySaveIndexStatus,
            view: mapPlayerView(data.view, source),
          };
        },
        quiet: true,
        fallbackOnError: false,
      },
    ),

  updateAnnotation: (identifier: string, note: string, tags: string[], source?: PlayerSource) =>
    handleRequest<unknown, { player: Player; source_id: string }>(
      () => apiClient.put(withPlayerSource(`/players/${encodeURIComponent(identifier)}/annotation`, source), { note, tags }),
      { player: mapPlayer({ player_uid: identifier, note, tags, has_annotation: Boolean(note.trim() || tags.length) })!, source_id: source === 'server' ? 'server' : '' },
      {
        map: (raw) => {
          const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
          return {
            player: mapPlayer(data.player) || mapPlayer({ player_uid: identifier, note, tags })!,
            source_id: String(data.source_id || ''),
          };
        },
        quiet: true,
        fallbackOnError: false,
      },
    ),

  clearAnnotation: (identifier: string, source?: PlayerSource) =>
    handleRequest<unknown, { player: Player; source_id: string; deleted: boolean }>(
      () => apiClient.delete(withPlayerSource(`/players/${encodeURIComponent(identifier)}/annotation`, source)),
      { player: mapPlayer({ player_uid: identifier, note: '', tags: [], has_annotation: false })!, source_id: source === 'server' ? 'server' : '', deleted: true },
      {
        map: (raw) => {
          const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
          return {
            player: mapPlayer(data.player) || mapPlayer({ player_uid: identifier })!,
            source_id: String(data.source_id || ''),
            deleted: Boolean(data.deleted),
          };
        },
        quiet: true,
        fallbackOnError: false,
      },
    ),

  getInventory: (identifier: string, source?: PlayerSource) =>
    handleRequest<unknown, SavePlayerInventory>(
      () => apiClient.get(withPlayerSource(`/players/${encodeURIComponent(identifier)}/inventory`, source)),
      { containers: [], status: emptySaveIndexStatus, view: defaultPlayerView(source) },
      {
        map: (raw) => {
          const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
          return {
            containers: mapSaveInventoryContainers(raw),
            status: data.status ? mapSaveIndexStatus(data.status) : emptySaveIndexStatus,
            view: mapPlayerView(data.view, source),
          };
        },
        quiet: true,
        fallbackOnError: false,
      },
    ),

  getBanList: () =>
    handleRequest<unknown, PlayerAccessEntry[]>(() => apiClient.get('/players/bans'), [], {
      map: mapAccessEntries,
      quiet: true,
    }),

  getWhitelist: () =>
    handleRequest<unknown, PlayerAccessEntry[]>(() => apiClient.get('/players/whitelist'), [], {
      map: mapAccessEntries,
      quiet: true,
    }),

  addLocalBan: (entry: PlayerAccessEntry) =>
    handleRequest<unknown, PlayerAccessEntry>(
      () => apiClient.post('/players/bans', entry),
      entry,
      { quiet: true, fallbackOnError: false },
    ),

  deleteLocalBan: (steam_id: string) =>
    handleRequest<unknown, { deleted: boolean }>(
      () => apiClient.delete(`/players/bans/${encodeURIComponent(steam_id)}`),
      { deleted: true },
      { quiet: true, fallbackOnError: false },
    ),

  banPlayer: (steam_id: string, nickname = '', reason = '') =>
    handleRequest<unknown, { player_id: string; ban?: PlayerAccessEntry }>(
      () => apiClient.post(`/players/${encodeURIComponent(steam_id)}/ban`, { userid: steam_id, nickname, reason }),
      { player_id: steam_id, ban: { steam_id, nickname, reason } },
      { quiet: true, fallbackOnError: false },
    ),

  unbanPlayer: (steam_id: string) =>
    handleRequest<unknown, { deleted: boolean }>(
      () => apiClient.post(`/players/${encodeURIComponent(steam_id)}/unban`, { userid: steam_id }),
      { deleted: true },
      { quiet: true, fallbackOnError: false },
    ),

  setWhitelist: (players: PlayerAccessEntry[]) =>
    handleRequest<unknown, { players: PlayerAccessEntry[] }>(
      () => apiClient.put('/players/whitelist', { players }),
      { players },
      { quiet: true, fallbackOnError: false },
    ),

  kickPlayer: (steam_id: string, message = '') =>
    handleRequest<unknown, { player_id: string }>(
      () => apiClient.post(`/players/${encodeURIComponent(steam_id)}/kick`, { userid: steam_id, message }),
      { player_id: steam_id },
      { quiet: true, fallbackOnError: false },
    ),

  teleportPlayer: async (): Promise<UnsupportedActionResult> => ({
    ok: false,
    unsupported: true,
    message: '当前后端未提供玩家传送接口',
  }),
  giveItem: (playerId: string, itemId: string, count: number) =>
    palDefenderGMApi.giveItems(playerId, [{ ItemID: itemId, Count: count }]),
};
