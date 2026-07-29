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

export const emptySaveHistoryState: SaveHistoryState = {
  source: { id: '', name: '', kind: '' },
  retention: 24,
  max_total_bytes: 0,
  total_bytes: 0,
  items: [],
};

export const saveHistoryApi = {
  list: () =>
    handleRequest<SaveHistoryState>(
      () => apiClient.get('/save/history'),
      emptySaveHistoryState,
      { quiet: true, fallbackOnError: false },
    ),

  diff: (params: SaveHistoryDiffParams) =>
    handleRequest<SaveHistoryDiff>(
      () => apiClient.get('/save/history/diff', { params }),
      {
        from: { id: '', fingerprint: '', generated_at: '', captured_at: '', parser: '', counts: emptyCounts, size_bytes: 0 },
        to: { id: '', fingerprint: '', generated_at: '', captured_at: '', parser: '', counts: emptyCounts, size_bytes: 0 },
        summary: {
          players_added: 0, players_removed: 0, players_changed: 0,
          guilds_added: 0, guilds_removed: 0, guilds_changed: 0,
          bases_added: 0, bases_removed: 0, bases_changed: 0,
          pals_added: 0, pals_removed: 0, pals_changed: 0,
          containers_added: 0, containers_removed: 0, containers_changed: 0,
          items_increased: 0, items_decreased: 0,
        },
        total: 0, limit: params.limit || 100, offset: params.offset || 0, items: [],
      },
      { quiet: true, fallbackOnError: false },
    ),
};
