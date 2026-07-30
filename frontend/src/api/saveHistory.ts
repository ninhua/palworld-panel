import { apiClient, handleRequest } from './client';

export interface SaveHistoryCounts {
  players: number;
  guilds: number;
  bases: number;
  pals: number;
  containers: number;
  map_entities: number;
}

export interface SaveHistorySnapshot {
  id: string;
  fingerprint: string;
  generated_at: string;
  captured_at: string;
  parser: string;
  counts: SaveHistoryCounts;
  size_bytes: number;
}

export interface SaveHistoryState {
  source: { id: string; name: string; kind: string };
  retention: number;
  max_total_bytes: number;
  total_bytes: number;
  items: SaveHistorySnapshot[];
}

export interface SaveHistoryFieldChange {
  field: string;
  before: string;
  after: string;
}

export interface SaveHistoryChange {
  category: 'players' | 'guilds' | 'bases' | 'pals' | 'containers' | 'items' | string;
  kind: 'added' | 'removed' | 'changed' | 'increased' | 'decreased' | string;
  id: string;
  label: string;
  delta?: number;
  fields: SaveHistoryFieldChange[];
}

export interface SaveHistoryDiffSummary {
  players_added: number;
  players_removed: number;
  players_changed: number;
  guilds_added: number;
  guilds_removed: number;
  guilds_changed: number;
  bases_added: number;
  bases_removed: number;
  bases_changed: number;
  pals_added: number;
  pals_removed: number;
  pals_changed: number;
  containers_added: number;
  containers_removed: number;
  containers_changed: number;
  items_increased: number;
  items_decreased: number;
}

export interface SaveHistoryDiff {
  from: SaveHistorySnapshot;
  to: SaveHistorySnapshot;
  summary: SaveHistoryDiffSummary;
  total: number;
  limit: number;
  offset: number;
  items: SaveHistoryChange[];
}

export interface SaveHistoryDiffParams {
  from: string;
  to: string;
  category?: string;
  q?: string;
  limit?: number;
  offset?: number;
}

const emptyCounts: SaveHistoryCounts = {
  players: 0, guilds: 0, bases: 0, pals: 0, containers: 0, map_entities: 0,
};

const emptySnapshot: SaveHistorySnapshot = {
  id: '', fingerprint: '', generated_at: '', captured_at: '', parser: '', counts: emptyCounts, size_bytes: 0,
};

const emptySummary: SaveHistoryDiffSummary = {
  players_added: 0, players_removed: 0, players_changed: 0,
  guilds_added: 0, guilds_removed: 0, guilds_changed: 0,
  bases_added: 0, bases_removed: 0, bases_changed: 0,
  pals_added: 0, pals_removed: 0, pals_changed: 0,
  containers_added: 0, containers_removed: 0, containers_changed: 0,
  items_increased: 0, items_decreased: 0,
};

export const emptySaveHistoryState: SaveHistoryState = {
  source: { id: '', name: '', kind: '' },
  retention: 24,
  max_total_bytes: 0,
  total_bytes: 0,
  items: [],
};

const record = (value: unknown): Record<string, unknown> => (
  value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
);

const number = (value: unknown, fallback = 0): number => {
  if (value == null || value === '') return fallback;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
};

const text = (value: unknown): string => typeof value === 'string' ? value : value == null ? '' : String(value);

export const mapSaveHistoryCounts = (value: unknown): SaveHistoryCounts => {
  const data = record(value);
  return {
    players: number(data.players),
    guilds: number(data.guilds),
    bases: number(data.bases),
    pals: number(data.pals),
    containers: number(data.containers),
    map_entities: number(data.map_entities),
  };
};

export const mapSaveHistorySnapshot = (value: unknown): SaveHistorySnapshot => {
  const data = record(value);
  return {
    id: text(data.id),
    fingerprint: text(data.fingerprint),
    generated_at: text(data.generated_at),
    captured_at: text(data.captured_at),
    parser: text(data.parser),
    counts: mapSaveHistoryCounts(data.counts),
    size_bytes: number(data.size_bytes),
  };
};

export const mapSaveHistoryState = (value: unknown): SaveHistoryState => {
  const data = record(value);
  const source = record(data.source);
  return {
    source: { id: text(source.id), name: text(source.name), kind: text(source.kind) },
    retention: number(data.retention, emptySaveHistoryState.retention),
    max_total_bytes: number(data.max_total_bytes),
    total_bytes: number(data.total_bytes),
    items: Array.isArray(data.items) ? data.items.map(mapSaveHistorySnapshot).filter((item) => item.id) : [],
  };
};

const mapSummary = (value: unknown): SaveHistoryDiffSummary => {
  const data = record(value);
  return Object.fromEntries(
    Object.keys(emptySummary).map((key) => [key, number(data[key])]),
  ) as unknown as SaveHistoryDiffSummary;
};

const mapChange = (value: unknown): SaveHistoryChange => {
  const data = record(value);
  return {
    category: text(data.category),
    kind: text(data.kind),
    id: text(data.id),
    label: text(data.label),
    ...(data.delta == null ? {} : { delta: number(data.delta) }),
    fields: Array.isArray(data.fields) ? data.fields.map((field) => {
      const item = record(field);
      return { field: text(item.field), before: text(item.before), after: text(item.after) };
    }).filter((field) => field.field) : [],
  };
};

export const mapSaveHistoryDiff = (value: unknown, fallbackLimit = 100, fallbackOffset = 0): SaveHistoryDiff => {
  const data = record(value);
  return {
    from: mapSaveHistorySnapshot(data.from),
    to: mapSaveHistorySnapshot(data.to),
    summary: mapSummary(data.summary),
    total: number(data.total),
    limit: number(data.limit, fallbackLimit),
    offset: number(data.offset, fallbackOffset),
    items: Array.isArray(data.items) ? data.items.map(mapChange).filter((item) => item.id) : [],
  };
};

export const saveHistoryApi = {
  list: () =>
    handleRequest<unknown, SaveHistoryState>(
      () => apiClient.get('/save/history'),
      emptySaveHistoryState,
      { quiet: true, fallbackOnError: false, map: mapSaveHistoryState },
    ),

  diff: (params: SaveHistoryDiffParams) =>
    handleRequest<unknown, SaveHistoryDiff>(
      () => apiClient.get('/save/history/diff', { params }),
      {
        from: emptySnapshot,
        to: emptySnapshot,
        summary: emptySummary,
        total: 0,
        limit: params.limit || 100,
        offset: params.offset || 0,
        items: [],
      },
      {
        quiet: true,
        fallbackOnError: false,
        map: (value) => mapSaveHistoryDiff(value, params.limit || 100, params.offset || 0),
      },
    ),
};
