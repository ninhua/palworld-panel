import { apiClient, handleRequest } from './client';
import { emptySummary, entityListQuery, mapSummary } from './entityList';
import { emptySaveIndexStatus, mapSaveIndexStatus } from './saveIndex';
import type {
  Base,
  BaseFeedBox,
  BaseFeedBoxesResponse,
  BaseFeedItemSummary,
  BaseStorageContainer,
  BaseStorageResponse,
  BaseWorker,
  BaseWorkersResponse,
  EntityListParams,
  EntityListResponse,
  UnsupportedActionResult,
} from '../types';

const mapBase = (raw: unknown): Base => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const location = data.location && typeof data.location === 'object' ? (data.location as Record<string, unknown>) : {};
  return {
    id: String(data.id || ''),
    name: String(data.name || 'Unknown Base'),
    raw_name: String(data.raw_name || ''),
    custom_name: String(data.custom_name || ''),
    has_custom_name: Boolean(data.has_custom_name),
    guild_id: String(data.guild_id || ''),
    guild_name: String(data.guild_name || data.guild || ''),
    x: Number(data.x ?? location.x ?? 0),
    y: Number(data.y ?? location.y ?? 0),
    z: Number(data.z ?? location.z ?? 0),
    structures_count: Number(data.structures_count || 0),
    pals_count: Number(data.pals_count || (Array.isArray(data.workers) ? data.workers.length : 0)),
    status: data.status === 'Raid' ? 'Raid' : 'Safe',
    online_members: Array.isArray(data.online_members) ? data.online_members.map(String) : [],
    workers: Array.isArray(data.workers) ? (data.workers as Base['workers']) : [],
    containers: Array.isArray(data.containers) ? data.containers.map(String) : [],
  };
};

const mapBaseStorageContainers = (raw: unknown): BaseStorageContainer[] => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const list = Array.isArray(raw) ? raw : Array.isArray(data.containers) ? data.containers : [];
  return list.flatMap((rawContainer) => {
    const container = (rawContainer && typeof rawContainer === 'object' ? rawContainer : {}) as Record<string, unknown>;
    const containerId = String(container.container_id || '').trim();
    if (!containerId) return [];
    const slots = Array.isArray(container.slots)
      ? container.slots.flatMap((rawSlot) => {
          const slot = (rawSlot && typeof rawSlot === 'object' ? rawSlot : {}) as Record<string, unknown>;
          const itemId = String(slot.item_id || '').trim();
          if (!itemId) return [];
          const durability = slot.durability == null ? undefined : Number(slot.durability);
          return [{
            slot: Number(slot.slot || 0),
            item_id: itemId,
            item_name: String(slot.item_name || itemId),
            item_icon: String(slot.item_icon || ''),
            count: Number(slot.count || 0),
            durability: durability !== undefined && Number.isFinite(durability) ? durability : undefined,
          }];
        })
      : [];
    return [{
      container_id: containerId,
      owner_type: String(container.owner_type || 'base'),
      owner_id: String(container.owner_id || ''),
      container_type: String(container.container_type || container.owner_type || ''),
      container_name: String(container.container_name || '存储容器'),
      slots,
    }];
  });
};


const mapFeedBox = (raw: unknown): BaseFeedBox => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const slots = mapBaseStorageContainers({
    containers: [{
      container_id: String(data.container_id || ''),
      container_type: String(data.container_type || ''),
      container_name: String(data.container_name || '饲料箱'),
      owner_type: 'map_object',
      owner_id: '',
      slots: Array.isArray(data.slots) ? data.slots : [],
    }],
  });
  return {
    container_id: String(data.container_id || ''),
    container_type: String(data.container_type || ''),
    container_name: String(data.container_name || '饲料箱'),
    slots: slots[0]?.slots ?? [],
  };
};

const mapFeedItemSummary = (raw: unknown): BaseFeedItemSummary => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return {
    item_id: String(data.item_id || ''),
    item_name: String(data.item_name || data.item_id || '未知物品'),
    item_icon: String(data.item_icon || ''),
    count: Number(data.count || 0),
    box_count: Number(data.box_count || 0),
  };
};

const mapBaseWorker = (raw: unknown): BaseWorker => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return {
    instance_id: String(data.instance_id || ''),
    character_id: String(data.character_id || ''),
    species_name: String(data.species_name || data.character_id || '未知帕鲁'),
    name: String(data.name || data.nickname || data.species_name || data.character_id || '未知帕鲁'),
    nickname: String(data.nickname || ''),
    level: Number(data.level || 0),
    gender: String(data.gender || ''),
    rank: Number(data.rank || 0),
    status: String(data.status || 'Unknown'),
    location_type: String(data.location_type || ''),
    passives: Array.isArray(data.passives) ? data.passives.map(String) : [],
    raw_passives: Array.isArray(data.raw_passives) ? data.raw_passives.map(String) : [],
    on_expedition: Boolean(data.on_expedition),
  };
};

const mapBases = (raw: unknown): Base[] => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const list = Array.isArray(raw) ? raw : Array.isArray(data.bases) ? data.bases : [];
  return list.map(mapBase);
};

const mapBasesList = (raw: unknown): EntityListResponse<Base> => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const items = mapBases(raw);
  return {
    items,
    status: data.status ? mapSaveIndexStatus(data.status) : emptySaveIndexStatus,
    summary: data.summary ? mapSummary(data.summary) : { ...emptySummary, total: items.length, returned: items.length },
  };
};

const unsupported = (message: string): Promise<UnsupportedActionResult> =>
  Promise.resolve({ ok: false, unsupported: true, message });

export const basesApi = {
  getBasesList: (params: EntityListParams = {}) =>
    handleRequest<unknown, EntityListResponse<Base>>(
      () => apiClient.get(`/bases${entityListQuery(params)}`),
      { items: [], status: emptySaveIndexStatus, summary: emptySummary },
      {
        map: mapBasesList,
        quiet: true,
      },
    ),

  getBases: () =>
    handleRequest<unknown, Base[]>(() => apiClient.get('/bases'), [], {
      map: mapBases,
      quiet: true,
    }),

  getWorkers: (baseId: string) =>
    handleRequest<unknown, BaseWorkersResponse>(
      () => apiClient.get(`/bases/${encodeURIComponent(baseId)}/workers`),
      {
        base: mapBase({}),
        workers: [],
        summary: { total: 0, average_level: 0, max_level: 0, named_count: 0, species_count: 0 },
        status: emptySaveIndexStatus,
        source_id: '',
      },
      {
        map: (raw) => {
          const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
          const summary = (data.summary && typeof data.summary === 'object' ? data.summary : {}) as Record<string, unknown>;
          return {
            base: mapBase(data.base),
            workers: Array.isArray(data.workers) ? data.workers.map(mapBaseWorker) : [],
            summary: {
              total: Number(summary.total || 0),
              average_level: Number(summary.average_level || 0),
              max_level: Number(summary.max_level || 0),
              named_count: Number(summary.named_count || 0),
              species_count: Number(summary.species_count || 0),
            },
            status: data.status ? mapSaveIndexStatus(data.status) : emptySaveIndexStatus,
            source_id: String(data.source_id || ''),
          };
        },
        quiet: true,
        fallbackOnError: false,
      },
    ),

  getFeedBoxes: (baseId: string) =>
    handleRequest<unknown, BaseFeedBoxesResponse>(
      () => apiClient.get(`/bases/${encodeURIComponent(baseId)}/feed-boxes`),
      {
        base: mapBase({}),
        feed_boxes: [],
        items: [],
        summary: { box_count: 0, empty_box_count: 0, occupied_slots: 0, total_items: 0, item_types: 0 },
        status: emptySaveIndexStatus,
        source_id: '',
      },
      {
        map: (raw) => {
          const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
          const summary = (data.summary && typeof data.summary === 'object' ? data.summary : {}) as Record<string, unknown>;
          return {
            base: mapBase(data.base),
            feed_boxes: Array.isArray(data.feed_boxes) ? data.feed_boxes.map(mapFeedBox).filter((box) => box.container_id) : [],
            items: Array.isArray(data.items) ? data.items.map(mapFeedItemSummary).filter((item) => item.item_id) : [],
            summary: {
              box_count: Number(summary.box_count || 0),
              empty_box_count: Number(summary.empty_box_count || 0),
              occupied_slots: Number(summary.occupied_slots || 0),
              total_items: Number(summary.total_items || 0),
              item_types: Number(summary.item_types || 0),
            },
            status: data.status ? mapSaveIndexStatus(data.status) : emptySaveIndexStatus,
            source_id: String(data.source_id || ''),
          };
        },
        quiet: true,
        fallbackOnError: false,
      },
    ),

  getStorage: (baseId: string) =>
    handleRequest<unknown, BaseStorageResponse>(
      () => apiClient.get(`/bases/${encodeURIComponent(baseId)}/storage`),
      { containers: [], status: emptySaveIndexStatus },
      {
        map: (raw) => {
          const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
          return {
            containers: mapBaseStorageContainers(raw),
            status: data.status ? mapSaveIndexStatus(data.status) : emptySaveIndexStatus,
          };
        },
        quiet: true,
        fallbackOnError: false,
      },
    ),

  updateCustomName: (baseId: string, name: string) =>
    handleRequest<unknown, Base>(
      () => apiClient.put(`/bases/${encodeURIComponent(baseId)}/name`, { name }),
      mapBase({}),
      {
        fallbackOnError: false,
        map: (raw) => {
          const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
          return mapBase(data.base ?? raw);
        },
      },
    ),

  clearCustomName: (baseId: string) =>
    handleRequest<unknown, Base>(
      () => apiClient.delete(`/bases/${encodeURIComponent(baseId)}/name`),
      mapBase({}),
      {
        fallbackOnError: false,
        map: (raw) => {
          const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
          return mapBase(data.base ?? raw);
        },
      },
    ),

  cleanBase: (baseId: string) =>
    handleRequest<unknown, { cleaned: boolean; saved: boolean; base: Base }>(
      () => apiClient.post(`/bases/${encodeURIComponent(baseId)}/clean`),
      { cleaned: false, saved: false, base: mapBase({}) },
      {
        map: (raw) => {
          const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
          return { cleaned: Boolean(data.cleaned), saved: Boolean(data.saved), base: mapBase(data.base) };
        },
        quiet: true,
        fallbackOnError: false,
      },
    ),
  backupBase: (_baseId: string) => unsupported('当前后端未提供单基地备份接口，请使用全服备份'),
};
