import { apiClient, handleRequest } from './client';
import { emptySummary, entityListQuery, mapSummary } from './entityList';
import { emptySaveIndexStatus, mapSaveIndexStatus } from './saveIndex';
import type { EntityListResponse, Pal, PalListParams, UnsupportedActionResult } from '../types';

type PalSource = 'server';
type PalSourceOptions = { source?: PalSource };

const withPalSource = (path: string, source?: PalSource) =>
  source ? `${path}${path.includes('?') ? '&' : '?'}source=${source}` : path;

export const mapPal = (raw: unknown): Pal => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const location = data.location && typeof data.location === 'object' ? (data.location as Record<string, unknown>) : {};
  return {
    id: String(data.id || data.instance_id || ''),
    instance_id: String(data.instance_id || data.id || ''),
    character_id: String(data.character_id || ''),
    species_name: data.species_name ? String(data.species_name) : undefined,
    name: String(data.name || data.character_id || 'Unknown Pal'),
    nickname: data.nickname ? String(data.nickname) : undefined,
    level: Number(data.level || 1),
    rarity: data.rarity === 'Boss' || data.rarity === 'Rare' ? data.rarity : 'Common',
    rarity_name: data.rarity_name ? String(data.rarity_name) : undefined,
    owner_player_uid: data.owner_player_uid ? String(data.owner_player_uid) : undefined,
    owner_nickname: String(data.owner_nickname || data.owner || ''),
    owner_steam_id: String(data.owner_steam_id || ''),
    guild_id: data.guild_id ? String(data.guild_id) : undefined,
    container_id: data.container_id ? String(data.container_id) : undefined,
    slot_index: data.slot_index == null ? undefined : Number(data.slot_index),
    location_type: data.location_type ? String(data.location_type) : undefined,
    location_kind: data.location_kind ? String(data.location_kind) : undefined,
    terminal_location: data.terminal_location ? String(data.terminal_location) : undefined,
    gender: data.gender ? String(data.gender) : undefined,
    rank: data.rank == null ? undefined : Number(data.rank),
    stars: data.stars == null ? undefined : Number(data.stars),
    iv_hp: data.iv_hp == null ? undefined : Number(data.iv_hp),
    iv_attack: data.iv_attack == null ? undefined : Number(data.iv_attack),
    iv_defense: data.iv_defense == null ? undefined : Number(data.iv_defense),
    iv_average: data.iv_average == null ? undefined : Number(data.iv_average),
    equipped_skills: Array.isArray(data.equipped_skills) ? data.equipped_skills.map(String) : [],
    old_owner_uids: Array.isArray(data.old_owner_uids) ? data.old_owner_uids.map(String) : [],
    on_expedition: Boolean(data.on_expedition),
    skills: Array.isArray(data.skills) ? (data.skills as Pal['skills']) : [],
    passives: Array.isArray(data.passives) ? data.passives.map(String) : [],
    raw_passives: Array.isArray(data.raw_passives) ? data.raw_passives.map(String) : [],
    raw_skills: Array.isArray(data.raw_skills) ? data.raw_skills.map(String) : [],
    work_suitability: Array.isArray(data.work_suitability) ? (data.work_suitability as Pal['work_suitability']) : [],
    health: Number(data.health || 0),
    max_health: Number(data.max_health || data.health || 0),
    status:
      data.status === 'Healthy' ||
      data.status === 'Injured' ||
      data.status === 'Working' ||
      data.status === 'Battling' ||
      data.status === 'Dead'
        ? data.status
        : 'Healthy',
    x: Number(data.x ?? location.x ?? 0),
    y: Number(data.y ?? location.y ?? 0),
    z: Number(data.z ?? location.z ?? 0),
  };
};

const mapPals = (raw: unknown): Pal[] => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const list = Array.isArray(raw) ? raw : Array.isArray(data.pals) ? data.pals : [];
  return list.map(mapPal);
};

const mapPalsList = (raw: unknown): EntityListResponse<Pal> => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const items = mapPals(raw);
  return {
    items,
    status: data.status ? mapSaveIndexStatus(data.status) : emptySaveIndexStatus,
    summary: data.summary ? mapSummary(data.summary) : { ...emptySummary, total: items.length, returned: items.length },
  };
};

const unsupported = (message: string): Promise<UnsupportedActionResult> =>
  Promise.resolve({ ok: false, unsupported: true, message });

export const palsApi = {
  getPalsList: (params: PalListParams = {}, options: PalSourceOptions = {}) =>
    handleRequest<unknown, EntityListResponse<Pal>>(
      () => apiClient.get(withPalSource(`/pals${entityListQuery(params)}`, options.source)),
      { items: [], status: emptySaveIndexStatus, summary: emptySummary },
      {
        map: mapPalsList,
        quiet: true,
      },
    ),

  getPals: () =>
    handleRequest<unknown, Pal[]>(() => apiClient.get('/pals'), [], {
      map: mapPals,
      quiet: true,
    }),

  updateLevel: (_palId: string, _level: number) => unsupported('当前后端未提供帕鲁等级修改接口'),
  heal: (_palId: string) => unsupported('当前后端未提供帕鲁治疗接口'),
  delete: (_palId: string) => unsupported('当前后端未提供帕鲁释放接口'),
};
