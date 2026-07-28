import { apiClient, handleRequest } from './client';
import { emptySaveIndexStatus, mapSaveIndexStatus } from './saveIndex';
import type { SaveIndexStatus, SaveSource } from '../types';
import { SAVE_ARCHIVE_IMPORT_TIMEOUT_MS, SAVE_INDEX_OPERATION_TIMEOUT_MS } from './requestTimeouts';

export interface SaveSourcesResponse {
  items: SaveSource[];
  active_status: SaveIndexStatus;
}

export interface SaveImportCandidate {
  id: string;
  relative_path: string;
  world_id?: string;
  player_count: number;
  level_sha256: string;
  level_size: number;
  valid: boolean;
  warnings: string[];
  errors: string[];
}

export interface SaveImportInspection {
  id: string;
  file_name: string;
  name?: string;
  candidates: SaveImportCandidate[];
  selected_candidate_id: string;
  requires_selection: boolean;
  expires_at: string;
}

export interface HostMigrationPlan {
  steam_id: string;
  source_uid: string;
  target_uid: string;
  strategy: 'direct' | 'target_exists' | 'already_migrated' | string;
  can_execute: boolean;
  source_player_file: string;
  source_dps_exists: boolean;
  target_player_exists: boolean;
  target_dps_exists: boolean;
  warnings: string[];
}

export interface HostMigrationResult {
  source: SaveSource;
  plan: HostMigrationPlan;
  verification: Record<string, unknown>;
}

const mapHostMigrationPlan = (raw: unknown): HostMigrationPlan => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return { steam_id: String(data.steam_id || ''), source_uid: String(data.source_uid || ''), target_uid: String(data.target_uid || ''), strategy: String(data.strategy || ''), can_execute: Boolean(data.can_execute), source_player_file: String(data.source_player_file || ''), source_dps_exists: Boolean(data.source_dps_exists), target_player_exists: Boolean(data.target_player_exists), target_dps_exists: Boolean(data.target_dps_exists), warnings: Array.isArray(data.warnings) ? data.warnings.map(String) : [] };
};

const mapSource = (raw: unknown): SaveSource => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return {
    id: String(data.id || ''),
    name: String(data.name || data.id || '未命名存档'),
    kind: String(data.kind || 'import'),
    path: data.path ? String(data.path) : undefined,
    active: Boolean(data.active),
    fingerprint: data.fingerprint ? String(data.fingerprint) : undefined,
    parser_version: data.parser_version ? String(data.parser_version) : undefined,
    warnings: Array.isArray(data.warnings) ? data.warnings.map(String) : [],
    indexed_at: data.indexed_at ? String(data.indexed_at) : undefined,
    created_at: String(data.created_at || ''),
    updated_at: String(data.updated_at || ''),
  };
};

const mapImportInspection = (raw: unknown): SaveImportInspection => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return {
    id: String(data.id || ''),
    file_name: String(data.file_name || ''),
    name: data.name ? String(data.name) : undefined,
    selected_candidate_id: String(data.selected_candidate_id || ''),
    requires_selection: Boolean(data.requires_selection),
    expires_at: String(data.expires_at || ''),
    candidates: (Array.isArray(data.candidates) ? data.candidates : []).map((rawCandidate) => {
      const candidate = (rawCandidate && typeof rawCandidate === 'object' ? rawCandidate : {}) as Record<string, unknown>;
      return {
        id: String(candidate.id || ''),
        relative_path: String(candidate.relative_path || ''),
        world_id: candidate.world_id ? String(candidate.world_id) : undefined,
        player_count: Number(candidate.player_count || 0),
        level_sha256: String(candidate.level_sha256 || ''),
        level_size: Number(candidate.level_size || 0),
        valid: Boolean(candidate.valid),
        warnings: Array.isArray(candidate.warnings) ? candidate.warnings.map(String) : [],
        errors: Array.isArray(candidate.errors) ? candidate.errors.map(String) : [],
      };
    }),
  };
};

export const saveSourcesApi = {
  list: () =>
    handleRequest<unknown, SaveSourcesResponse>(
      () => apiClient.get('/save-sources'),
      { items: [], active_status: emptySaveIndexStatus },
      {
        fallbackOnError: false,
        map: (raw) => {
          const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
          return {
            items: Array.isArray(data.items) ? data.items.map(mapSource) : [],
            active_status: data.active_status ? mapSaveIndexStatus(data.active_status) : emptySaveIndexStatus,
          };
        },
      },
    ),
  importArchive: (file: File, name: string) => {
    const body = new FormData();
    body.append('file', file);
    if (name.trim()) body.append('name', name.trim());
    return handleRequest<unknown, SaveSource>(() => apiClient.post('/save-sources/import', body, {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: SAVE_ARCHIVE_IMPORT_TIMEOUT_MS,
    }), mapSource({}), {
      fallbackOnError: false,
      map: mapSource,
    });
  },
  inspectArchive: (file: File, name: string) => {
    const body = new FormData();
    body.append('file', file);
    if (name.trim()) body.append('name', name.trim());
    return handleRequest<unknown, SaveImportInspection>(() => apiClient.post('/save-sources/import/inspect', body, {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: SAVE_ARCHIVE_IMPORT_TIMEOUT_MS,
    }), mapImportInspection({}), { fallbackOnError: false, map: mapImportInspection });
  },
  selectImportCandidate: (inspectionID: string, candidateID: string) =>
    handleRequest<unknown, SaveImportInspection>(
      () => apiClient.post(`/save-sources/import/inspect/${encodeURIComponent(inspectionID)}/select`, { candidate_id: candidateID }),
      mapImportInspection({}),
      { fallbackOnError: false, map: mapImportInspection },
    ),
  importInspected: (inspectionID: string, name: string) =>
    handleRequest<unknown, SaveSource>(
      () => apiClient.post('/save-sources/import', {
        inspection_id: inspectionID,
        ...(name.trim() ? { name: name.trim() } : {}),
      }, { timeout: SAVE_ARCHIVE_IMPORT_TIMEOUT_MS }),
      mapSource({}),
      { fallbackOnError: false, map: mapSource },
    ),
  planHostMigration: (sourceID: string, steamID: string) => handleRequest<unknown, HostMigrationPlan>(
    () => apiClient.post('/save-sources/import/inspect', { migration_source_id: sourceID, steam_id: steamID }, { timeout: SAVE_INDEX_OPERATION_TIMEOUT_MS }),
    mapHostMigrationPlan({}),
    { fallbackOnError: false, map: mapHostMigrationPlan },
  ),
  executeHostMigration: (sourceID: string, steamID: string, name: string) => handleRequest<unknown, HostMigrationResult>(
    () => apiClient.post('/save-sources/import', { migration_source_id: sourceID, steam_id: steamID, name, confirm: true }, { timeout: SAVE_ARCHIVE_IMPORT_TIMEOUT_MS }),
    { source: mapSource({}), plan: mapHostMigrationPlan({}), verification: {} },
    {
      fallbackOnError: false,
      map: (raw) => {
        const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
        return { source: mapSource(data.source), plan: mapHostMigrationPlan(data.plan), verification: data.verification && typeof data.verification === 'object' ? data.verification as Record<string, unknown> : {} };
      },
    },
  ),
  activate: (id: string) => handleRequest(
    () => apiClient.post(`/save-sources/${encodeURIComponent(id)}/activate`, undefined, { timeout: SAVE_INDEX_OPERATION_TIMEOUT_MS }),
    {},
    { fallbackOnError: false },
  ),
  rebuild: (id: string) => handleRequest(
    () => apiClient.post(`/save-sources/${encodeURIComponent(id)}/rebuild`, undefined, { timeout: SAVE_INDEX_OPERATION_TIMEOUT_MS }),
    {},
    { fallbackOnError: false },
  ),
  rename: (id: string, name: string) => handleRequest<unknown, SaveSource>(() => apiClient.patch(`/save-sources/${encodeURIComponent(id)}`, { name }), mapSource({}), { fallbackOnError: false, map: mapSource }),
  remove: (id: string) => handleRequest(() => apiClient.delete(`/save-sources/${encodeURIComponent(id)}`), {}, { fallbackOnError: false }),
};
