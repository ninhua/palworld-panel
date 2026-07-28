import { apiClient, handleRequest } from './client';
import { emptySummary, entityListQuery, mapSummary } from './entityList';
import { emptySaveIndexStatus, mapSaveIndexStatus } from './saveIndex';
import type {
  EntityListParams,
  EntityListResponse,
  Guild,
  GuildBaseDetail,
  GuildDetailResponse,
  GuildMemberDetail,
} from '../types';

const asRecord = (raw: unknown): Record<string, unknown> =>
  raw && typeof raw === 'object' ? (raw as Record<string, unknown>) : {};

const mapGuild = (raw: unknown): Guild => {
  const data = asRecord(raw);
  return {
    id: String(data.id || data.owner_player_uid || data.name || ''),
    name: String(data.name || 'Unknown Guild'),
    owner_player_uid: String(data.owner_player_uid || ''),
    members: Array.isArray(data.members)
      ? data.members.map((item) => {
          const member = asRecord(item);
          return {
            player_uid: String(member.player_uid || ''),
            nickname: String(member.nickname || member.player_name || ''),
            last_online_time: member.last_online_time ? String(member.last_online_time) : undefined,
          };
        })
      : [],
    base_ids: Array.isArray(data.base_ids) ? data.base_ids.map(String) : [],
    online_member_count: Number(data.online_member_count || 0),
  };
};

const mapGuilds = (raw: unknown): Guild[] => {
  const data = asRecord(raw);
  const list = Array.isArray(raw) ? raw : Array.isArray(data.guilds) ? data.guilds : [];
  return list.map(mapGuild);
};

const mapGuildsList = (raw: unknown): EntityListResponse<Guild> => {
  const data = asRecord(raw);
  const items = mapGuilds(raw);
  return {
    items,
    status: data.status ? mapSaveIndexStatus(data.status) : emptySaveIndexStatus,
    summary: data.summary ? mapSummary(data.summary) : { ...emptySummary, total: items.length, returned: items.length },
  };
};

const mapGuildMemberDetail = (raw: unknown): GuildMemberDetail => {
  const data = asRecord(raw);
  return {
    id: String(data.id || data.steam_id || data.player_uid || ''),
    player_uid: String(data.player_uid || ''),
    steam_id: String(data.steam_id || ''),
    nickname: String(data.nickname || data.player_uid || 'Unknown Player'),
    level: Number(data.level || 0),
    is_online: Boolean(data.is_online),
    last_online_time: String(data.last_online_time || ''),
    is_owner: Boolean(data.is_owner),
    note: String(data.note || ''),
    tags: Array.isArray(data.tags) ? data.tags.map(String) : [],
    has_annotation: Boolean(data.has_annotation),
  };
};

const mapGuildBaseDetail = (raw: unknown): GuildBaseDetail => {
  const data = asRecord(raw);
  const location = asRecord(data.location);
  return {
    id: String(data.id || ''),
    name: String(data.name || data.raw_name || data.id || 'Unknown Base'),
    raw_name: String(data.raw_name || ''),
    custom_name: String(data.custom_name || ''),
    has_custom_name: Boolean(data.has_custom_name),
    x: Number(data.x ?? location.x ?? 0),
    y: Number(data.y ?? location.y ?? 0),
    z: Number(data.z ?? location.z ?? 0),
    structures_count: Number(data.structures_count || 0),
    workers_count: Number(data.workers_count ?? data.pals_count ?? 0),
    status: String(data.status || ''),
  };
};

const emptyGuildDetail: GuildDetailResponse = {
  guild: mapGuild({}),
  members: [],
  bases: [],
  status: emptySaveIndexStatus,
  source_id: '',
};

const mapGuildDetail = (raw: unknown): GuildDetailResponse => {
  const data = asRecord(raw);
  return {
    guild: mapGuild(data.guild),
    members: Array.isArray(data.members) ? data.members.map(mapGuildMemberDetail) : [],
    bases: Array.isArray(data.bases) ? data.bases.map(mapGuildBaseDetail) : [],
    status: data.status ? mapSaveIndexStatus(data.status) : emptySaveIndexStatus,
    source_id: String(data.source_id || ''),
  };
};

export const guildsApi = {
  getGuildsList: (params: EntityListParams = {}) =>
    handleRequest<unknown, EntityListResponse<Guild>>(
      () => apiClient.get(`/guilds${entityListQuery(params)}`),
      { items: [], status: emptySaveIndexStatus, summary: emptySummary },
      {
        map: mapGuildsList,
        quiet: true,
      },
    ),

  getGuilds: () =>
    handleRequest<unknown, Guild[]>(() => apiClient.get('/guilds'), [], {
      map: mapGuilds,
      quiet: true,
    }),

  getGuild: (identifier: string) =>
    handleRequest<unknown, GuildDetailResponse>(
      () => apiClient.get(`/guilds/${encodeURIComponent(identifier)}`),
      emptyGuildDetail,
      { map: mapGuildDetail, quiet: true },
    ),
};
