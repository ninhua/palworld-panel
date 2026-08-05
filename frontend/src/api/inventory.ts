import { apiClient, handleRequest } from './client';
import { emptySaveIndexStatus, mapSaveIndexStatus } from './saveIndex';
import type { SaveIndexStatus } from '../types';

export type InventoryOwnerType = 'all' | 'base' | 'player' | 'guild' | 'unknown';
export type InventorySort = 'count_desc' | 'name_asc' | 'name_desc';

export interface GlobalInventoryLocation {
  owner_type: Exclude<InventoryOwnerType, 'all'>;
  owner_id: string;
  owner_name: string;
  guild_name?: string;
  container_id: string;
  container_type: string;
  container_name: string;
  slot: number;
  count: number;
  trusted: boolean;
  scope_reason?: string;
}

export interface GlobalInventoryItem {
  item_id: string;
  item_name: string;
  item_icon: string;
  category: string;
  total_count: number;
  locations: GlobalInventoryLocation[];
}


export interface UnattendedInventoryItem {
  item_id: string;
  item_name: string;
  item_icon: string;
  category: string;
  quantity: number;
}

export interface UnattendedInventoryState {
  available: boolean;
  status: 'idle' | 'online' | 'waiting' | 'active' | 'completed' | 'source_mismatch' | 'world_unavailable' | 'state_unavailable' | 'unavailable' | string;
  world_id: string;
  started_at?: string;
  baseline_at?: string;
  eligible_at?: string;
  ended_at?: string;
  last_observed_at?: string;
  duration_seconds: number;
  eligible_remaining_seconds: number;
  qualified: boolean;
  current_online: boolean;
  presence_stale: boolean;
  snapshot_stale: boolean;
  additions: UnattendedInventoryItem[];
  total_added: number;
}

export interface GlobalInventorySummary {
  item_types: number;
  total_count: number;
  container_count: number;
  location_count: number;
  unresolved_containers: number;
  suppressed_containers: number;
  suppressed_locations: number;
  suppressed_item_types: number;
  suppressed_total_count: number;
  trusted_only: boolean;
  returned: number;
  offset: number;
  limit: number;
}

export interface GlobalInventoryResponse {
  items: GlobalInventoryItem[];
  summary: GlobalInventorySummary;
  filters: { categories: string[]; owner_types: InventoryOwnerType[] };
  status: SaveIndexStatus;
  source_id: string;
  unattended: UnattendedInventoryState;
}

export interface GlobalInventoryQuery {
  q?: string;
  category?: string;
  owner_type?: InventoryOwnerType;
  sort?: InventorySort;
  limit?: number;
  offset?: number;
}


const emptyUnattended: UnattendedInventoryState = {
  available: false,
  status: 'unavailable',
  world_id: '',
  duration_seconds: 0,
  eligible_remaining_seconds: 0,
  qualified: false,
  current_online: false,
  presence_stale: false,
  snapshot_stale: false,
  additions: [],
  total_added: 0,
};

const emptySummary: GlobalInventorySummary = {
  item_types: 0,
  total_count: 0,
  container_count: 0,
  location_count: 0,
  unresolved_containers: 0,
  suppressed_containers: 0,
  suppressed_locations: 0,
  suppressed_item_types: 0,
  suppressed_total_count: 0,
  trusted_only: true,
  returned: 0,
  offset: 0,
  limit: 200,
};

const mapLocation = (raw: unknown): GlobalInventoryLocation => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const ownerType = String(data.owner_type || 'unknown');
  return {
    owner_type: ownerType === 'base' || ownerType === 'player' || ownerType === 'guild' ? ownerType : 'unknown',
    owner_id: String(data.owner_id || ''),
    owner_name: String(data.owner_name || '未识别容器'),
    guild_name: data.guild_name ? String(data.guild_name) : undefined,
    container_id: String(data.container_id || ''),
    container_type: String(data.container_type || 'unknown'),
    container_name: String(data.container_name || '存储容器'),
    slot: Number(data.slot || 0),
    count: Number(data.count || 0),
    trusted: Boolean(data.trusted),
    scope_reason: data.scope_reason ? String(data.scope_reason) : undefined,
  };
};

const mapItem = (raw: unknown): GlobalInventoryItem => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return {
    item_id: String(data.item_id || ''),
    item_name: String(data.item_name || data.item_id || '未知物品'),
    item_icon: String(data.item_icon || ''),
    category: String(data.category || '其他'),
    total_count: Number(data.total_count || 0),
    locations: (Array.isArray(data.locations) ? data.locations : []).map(mapLocation),
  };
};


const mapUnattended = (raw: unknown): UnattendedInventoryState => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return {
    available: Boolean(data.available),
    status: String(data.status || 'unavailable'),
    world_id: String(data.world_id || ''),
    started_at: data.started_at ? String(data.started_at) : undefined,
    baseline_at: data.baseline_at ? String(data.baseline_at) : undefined,
    eligible_at: data.eligible_at ? String(data.eligible_at) : undefined,
    ended_at: data.ended_at ? String(data.ended_at) : undefined,
    last_observed_at: data.last_observed_at ? String(data.last_observed_at) : undefined,
    duration_seconds: Number(data.duration_seconds || 0),
    eligible_remaining_seconds: Number(data.eligible_remaining_seconds || 0),
    qualified: Boolean(data.qualified),
    current_online: Boolean(data.current_online),
    presence_stale: Boolean(data.presence_stale),
    snapshot_stale: Boolean(data.snapshot_stale),
    additions: (Array.isArray(data.additions) ? data.additions : []).map((item) => {
      const value = (item && typeof item === 'object' ? item : {}) as Record<string, unknown>;
      return {
        item_id: String(value.item_id || ''),
        item_name: String(value.item_name || value.item_id || '未知物品'),
        item_icon: String(value.item_icon || ''),
        category: String(value.category || '其他'),
        quantity: Number(value.quantity || 0),
      };
    }),
    total_added: Number(data.total_added || 0),
  };
};

export const mapGlobalInventoryResponse = (raw: unknown): GlobalInventoryResponse => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const rawSummary = (data.summary && typeof data.summary === 'object' ? data.summary : {}) as Record<string, unknown>;
  const rawFilters = (data.filters && typeof data.filters === 'object' ? data.filters : {}) as Record<string, unknown>;
  return {
    items: (Array.isArray(data.items) ? data.items : []).map(mapItem),
    summary: {
      item_types: Number(rawSummary.item_types || 0),
      total_count: Number(rawSummary.total_count || 0),
      container_count: Number(rawSummary.container_count || 0),
      location_count: Number(rawSummary.location_count || 0),
      unresolved_containers: Number(rawSummary.unresolved_containers || 0),
      suppressed_containers: Number(rawSummary.suppressed_containers || 0),
      suppressed_locations: Number(rawSummary.suppressed_locations || 0),
      suppressed_item_types: Number(rawSummary.suppressed_item_types || 0),
      suppressed_total_count: Number(rawSummary.suppressed_total_count || 0),
      trusted_only: rawSummary.trusted_only !== false,
      returned: Number(rawSummary.returned || 0),
      offset: Number(rawSummary.offset || 0),
      limit: Number(rawSummary.limit || 200),
    },
    filters: {
      categories: (Array.isArray(rawFilters.categories) ? rawFilters.categories : []).map(String),
      owner_types: (Array.isArray(rawFilters.owner_types) ? rawFilters.owner_types : ['all', 'base', 'player', 'guild', 'unknown']) as InventoryOwnerType[],
    },
    status: data.status ? mapSaveIndexStatus(data.status) : emptySaveIndexStatus,
    source_id: String(data.source_id || ''),
    unattended: data.unattended ? mapUnattended(data.unattended) : emptyUnattended,
  };
};

export const inventoryApi = {
  list: (query: GlobalInventoryQuery = {}) => handleRequest<unknown, GlobalInventoryResponse>(
    () => apiClient.get('/inventory', {
      params: {
        q: query.q?.trim() || undefined,
        category: query.category && query.category !== 'all' ? query.category : undefined,
        owner_type: query.owner_type || 'all',
        sort: query.sort || 'count_desc',
        limit: query.limit || 200,
        offset: query.offset || 0,
      },
    }),
    { items: [], summary: emptySummary, filters: { categories: [], owner_types: ['all', 'base', 'player', 'guild', 'unknown'] }, status: emptySaveIndexStatus, source_id: '', unattended: emptyUnattended },
    { fallbackOnError: false, map: mapGlobalInventoryResponse },
  ),
};
