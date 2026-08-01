import { apiClient, handleRequest, isTemporaryBackendError } from './client';
import { emptySaveIndexStatus, mapSaveIndexStatus } from './saveIndex';
import type { Job, SaveIndexStatus, SaveSource } from '../types';
import { SAVE_ARCHIVE_IMPORT_TIMEOUT_MS, SAVE_INDEX_OPERATION_TIMEOUT_MS } from './requestTimeouts';
import { mapJob } from './tasks';

export interface RuntimeSave {
  available: boolean;
  server_active: boolean;
  source_id: string;
  source_name: string;
  world_id: string;
  state: string;
}

export interface SaveSourcesResponse {
  items: SaveSource[];
  active_status: SaveIndexStatus;
  runtime_save: RuntimeSave;
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

const mapHostMigrationPlan = (raw: unknown): HostMigrationPlan => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return { steam_id: String(data.steam_id || ''), source_uid: String(data.source_uid || ''), target_uid: String(data.target_uid || ''), strategy: String(data.strategy || ''), can_execute: Boolean(data.can_execute), source_player_file: String(data.source_player_file || ''), source_dps_exists: Boolean(data.source_dps_exists), target_player_exists: Boolean(data.target_player_exists), target_dps_exists: Boolean(data.target_dps_exists), warnings: Array.isArray(data.warnings) ? data.warnings.map(String) : [] };
};

export interface SaveMigrationPlayer {
  player_uid: string;
  steam_id?: string;
  nickname: string;
  level: number;
  guild_name?: string;
}

export interface SaveMigrationMappingInput {
  source_uid: string;
  steam_id: string;
}

export interface SaveMigrationRequest {
  source_id: string;
  target_mode: 'auto' | 'steam' | 'nosteam';
  mappings: SaveMigrationMappingInput[];
  expected_fingerprint?: string;
  manual_mode_confirmation?: string;
  confirmation?: string;
}

export interface SaveMigrationMappingPreview extends SaveMigrationMappingInput {
  nickname: string;
  level: number;
  steam_uid: string;
  nosteam_uid: string;
  target_uid?: string;
}

export interface SaveMigrationPreview {
  source: SaveSource;
  source_fingerprint: string;
  target_mode: 'unknown' | 'steam' | 'nosteam';
  mode_source: 'server_index' | 'unproven';
  mode_matched: number;
  mode_total: number;
  requires_manual_confirmation: boolean;
  mappings: SaveMigrationMappingPreview[];
  conflicts: string[];
  ready: boolean;
}

export interface SaveMigrationPlayersResponse {
  source: SaveSource;
  source_fingerprint: string;
  players: SaveMigrationPlayer[];
}

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

const emptyRuntimeSave: RuntimeSave = {
  available: false,
  server_active: false,
  source_id: 'server',
  source_name: '当前服务器存档',
  world_id: '',
  state: 'unknown',
};

const mapRuntimeSave = (raw: unknown): RuntimeSave => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return {
    available: Boolean(data.available),
    server_active: Boolean(data.server_active),
    source_id: String(data.source_id || emptyRuntimeSave.source_id),
    source_name: String(data.source_name || emptyRuntimeSave.source_name),
    world_id: String(data.world_id || ''),
    state: String(data.state || emptyRuntimeSave.state),
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

const mapMigrationPlayers = (raw: unknown): SaveMigrationPlayersResponse => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  return {
    source: mapSource(data.source),
    source_fingerprint: String(data.source_fingerprint || ''),
    players: (Array.isArray(data.players) ? data.players : []).map((entry) => {
      const player = (entry && typeof entry === 'object' ? entry : {}) as Record<string, unknown>;
      return {
        player_uid: String(player.player_uid || ''), steam_id: player.steam_id ? String(player.steam_id) : undefined,
        nickname: String(player.nickname || player.player_uid || '未命名玩家'), level: Number(player.level || 0),
        guild_name: player.guild_name ? String(player.guild_name) : undefined,
      };
    }),
  };
};

const mapMigrationPreview = (raw: unknown): SaveMigrationPreview => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const targetMode = String(data.target_mode || 'unknown');
  return {
    source: mapSource(data.source), source_fingerprint: String(data.source_fingerprint || ''),
    target_mode: targetMode === 'steam' || targetMode === 'nosteam' ? targetMode : 'unknown',
    mode_source: data.mode_source === 'server_index' ? 'server_index' : 'unproven',
    mode_matched: Number(data.mode_matched || 0), mode_total: Number(data.mode_total || 0),
    requires_manual_confirmation: Boolean(data.requires_manual_confirmation),
    mappings: (Array.isArray(data.mappings) ? data.mappings : []).map((entry) => {
      const mapping = (entry && typeof entry === 'object' ? entry : {}) as Record<string, unknown>;
      return {
        source_uid: String(mapping.source_uid || ''), steam_id: String(mapping.steam_id || ''), nickname: String(mapping.nickname || ''),
        level: Number(mapping.level || 0), steam_uid: String(mapping.steam_uid || ''), nosteam_uid: String(mapping.nosteam_uid || ''),
        target_uid: mapping.target_uid ? String(mapping.target_uid) : undefined,
      };
    }),
    conflicts: Array.isArray(data.conflicts) ? data.conflicts.map(String) : [], ready: Boolean(data.ready),
  };
};

export const saveSourcesApi = {
  list: () =>
    handleRequest<unknown, SaveSourcesResponse>(
      () => apiClient.get('/save-sources'),
      { items: [], active_status: emptySaveIndexStatus, runtime_save: emptyRuntimeSave },
      {
        fallbackOnError: false,
        map: (raw) => {
          const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
          return {
            items: Array.isArray(data.items) ? data.items.map(mapSource) : [],
            active_status: data.active_status ? mapSaveIndexStatus(data.active_status) : emptySaveIndexStatus,
            runtime_save: data.runtime_save ? mapRuntimeSave(data.runtime_save) : emptyRuntimeSave,
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
  executeHostMigration: (sourceID: string, steamID: string, name: string) => handleRequest<unknown, Job>(
    () => apiClient.post('/save-sources/import', { migration_source_id: sourceID, steam_id: steamID, name, confirm: true }, { timeout: SAVE_INDEX_OPERATION_TIMEOUT_MS }),
    mapJob({ type: 'host_save_migration', message: '正在提交主机迁移任务' }),
    { fallbackOnError: false, map: mapJob },
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
  migrationPlayers: (id: string) => handleRequest<unknown, SaveMigrationPlayersResponse>(
    () => apiClient.get(`/save-sources/${encodeURIComponent(id)}/migration/players`),
    { source: mapSource({}), source_fingerprint: '', players: [] },
    { fallbackOnError: false, map: mapMigrationPlayers },
  ),
  previewMigration: (request: SaveMigrationRequest) => handleRequest<unknown, SaveMigrationPreview>(
    () => apiClient.post('/save-migrations/preview', request),
    mapMigrationPreview({}),
    { fallbackOnError: false, map: mapMigrationPreview },
  ),
  startMigration: (request: SaveMigrationRequest) => handleRequest<unknown, Job>(
    () => apiClient.post('/save-migrations', request),
    mapJob({}),
    { fallbackOnError: false, map: mapJob },
  ),
};


const backendRecoveryDelayMs = 2_000;
const backendRecoveryAttempts = 30;

const sleep = (duration: number) => new Promise((resolve) => setTimeout(resolve, duration));

export const waitForSaveSourcesBackend = async (): Promise<SaveSourcesResponse | null> => {
  for (let attempt = 0; attempt < backendRecoveryAttempts; attempt += 1) {
    try {
      return await saveSourcesApi.list();
    } catch (error) {
      if (!isTemporaryBackendError(error)) throw error;
      if (attempt + 1 < backendRecoveryAttempts) await sleep(backendRecoveryDelayMs);
    }
  }
  return null;
};
