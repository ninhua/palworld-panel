import { apiClient, handleRequest } from './client';

export interface EconomyConfig {
  command_prefix: string;
  allow_bare_commands: boolean;
  daily_checkin_points: number;
  checkin_streak_enabled: boolean;
  checkin_streak_bonus_per_day: number;
  checkin_streak_max_days: number;
  checkin_cycle_days: number;
  checkin_cycle_bonus: number;
  checkin_aliases: string[];
  points_aliases: string[];
  help_aliases: string[];
  updated_at: string;
}

export interface EconomySummary {
  accounts: number;
  total_balance: number;
  ledger_entries: number;
  active_reservations: number;
  reserved_points: number;
  issued_last_24_hours: number;
  spent_last_24_hours: number;
  checkins_today: number;
  checkin_points_today: number;
  local_date: string;
}

export interface EconomyAccount {
  player_uid: string;
  nickname?: string;
  steam_id?: string;
  status: string;
  balance: number;
  created_at: string;
  updated_at: string;
}

export interface EconomyLedgerEntry {
  id: string;
  player_uid: string;
  delta: number;
  balance_after: number;
  reason: string;
  reference_type?: string;
  reference_id?: string;
  actor?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

export interface EconomyCheckinHistoryEntry {
  local_date: string;
  points: number;
  base_points: number;
  streak_bonus: number;
  cycle_bonus: number;
  streak_day: number;
  created_at: string;
}

export interface GameEventBridgeStatus {
  enabled: boolean;
  running: boolean;
  log_directory?: string;
  active_file?: string;
  last_scan_at?: string;
  last_line_at?: string;
  last_event_at?: string;
  last_error?: string;
  parsed_events: number;
  processed_events: number;
  unmatched_players: number;
  failed_events: number;
  pending_dead_letters: number;
  rotation_resets: number;
  cursor_files: number;
  online_players: number;
  tracked_online_players: number;
  online_minutes_emitted: number;
  last_online_sample_at?: string;
  last_online_error?: string;
  configuration: Record<string, boolean>;
}

export interface OnlineTaskTrackingRecord {
  player_uid: string;
  nickname?: string;
  steam_id?: string;
  active: boolean;
  online_since?: string;
  last_seen_at?: string;
  pending_seconds: number;
  total_emitted_minutes: number;
  updated_at: string;
}

export interface GameEventBridgeOffset {
  path: string;
  offset: number;
  file_size: number;
  prefix_hash?: string;
  reset_count: number;
  last_reset_reason?: string;
  updated_at: string;
}

export interface GameEventBridgeDeadLetter {
  id: number;
  event_id: string;
  source_path: string;
  offset: number;
  event_type?: string;
  player_hint?: string;
  player_uid?: string;
  steam_id?: string;
  nickname?: string;
  payload: Record<string, unknown>;
  raw_line?: string;
  sample?: string;
  reason: string;
  status: 'pending' | 'replayed' | 'dismissed';
  attempts: number;
  last_error?: string;
  replayed_at?: string;
  dismissed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface GameEventBridgeObservation {
  id: number;
  source_path: string;
  offset: number;
  event_type?: string;
  player_uid?: string;
  nickname?: string;
  status: 'processed' | 'unmatched_player' | 'error' | string;
  reason?: string;
  sample?: string;
  created_at: string;
}

export interface LegacyAstrBotCandidate {
  qq_id: string;
  player_uid?: string;
  nickname?: string;
  binding_status?: string;
  source_balance: number;
  previously_imported: number;
  import_delta: number;
  status: 'ready' | 'unbound' | 'zero_balance' | 'source_balance_decreased' | 'already_current';
}

export interface LegacyAstrBotPreview {
  source_sha256: string;
  accounts: number;
  bound_accounts: number;
  importable_accounts: number;
  unbound_accounts: number;
  zero_balance: number;
  decreased_balance: number;
  already_current: number;
  source_points: number;
  importable_points: number;
  checkins: number;
  candidates: LegacyAstrBotCandidate[];
}

export interface LegacyAstrBotImportResult {
  batch_id: string;
  source_sha256: string;
  imported_accounts: number;
  imported_points: number;
  imported_checkins: number;
  unbound_accounts: number;
  decreased_balance: number;
  already_current: number;
  zero_balance_accounts: number;
}

interface EconomyAccountList { items: EconomyAccount[]; count: number }
interface EconomyLedgerList { items: EconomyLedgerEntry[]; count: number }
interface EconomyCheckinList { items: EconomyCheckinHistoryEntry[]; count: number }
interface BridgeObservationList { items: GameEventBridgeObservation[]; count: number }
interface BridgeDeadLetterList { items: GameEventBridgeDeadLetter[]; count: number; pending: number }

const configFallback: EconomyConfig = {
  command_prefix: '!',
  allow_bare_commands: true,
  daily_checkin_points: 10,
  checkin_streak_enabled: true,
  checkin_streak_bonus_per_day: 2,
  checkin_streak_max_days: 7,
  checkin_cycle_days: 7,
  checkin_cycle_bonus: 10,
  checkin_aliases: ['签到', 'qd', 'checkin'],
  points_aliases: ['积分', 'jf', 'points'],
  help_aliases: ['帮助', '菜单', 'help'],
  updated_at: '',
};

const summaryFallback: EconomySummary = {
  accounts: 0, total_balance: 0, ledger_entries: 0, active_reservations: 0, reserved_points: 0,
  issued_last_24_hours: 0, spent_last_24_hours: 0, checkins_today: 0, checkin_points_today: 0, local_date: '',
};

const bridgeFallback: GameEventBridgeStatus = {
  enabled: true, running: false, parsed_events: 0, processed_events: 0, unmatched_players: 0, failed_events: 0, pending_dead_letters: 0, rotation_resets: 0, cursor_files: 0, online_players: 0, tracked_online_players: 0, online_minutes_emitted: 0, configuration: {},
};

export const economyApi = {
  config: () => handleRequest<unknown, EconomyConfig>(
    () => apiClient.get('/economy/config'), configFallback, { fallbackOnError: false },
  ),
  updateConfig: (input: Omit<EconomyConfig, 'updated_at'>) => handleRequest<unknown, EconomyConfig>(
    () => apiClient.put('/economy/config', input), configFallback, { fallbackOnError: false },
  ),
  summary: () => handleRequest<unknown, EconomySummary>(
    () => apiClient.get('/economy/summary'), summaryFallback, { fallbackOnError: false },
  ),
  accounts: (query = '') => handleRequest<unknown, EconomyAccountList>(
    () => apiClient.get('/economy/accounts', { params: { query, limit: 100 } }), { items: [], count: 0 }, { fallbackOnError: false },
  ),
  ledger: (playerUID: string) => handleRequest<unknown, EconomyLedgerList>(
    () => apiClient.get(`/economy/accounts/${encodeURIComponent(playerUID)}/ledger`, { params: { limit: 100 } }), { items: [], count: 0 }, { fallbackOnError: false },
  ),
  checkins: (playerUID: string) => handleRequest<unknown, EconomyCheckinList>(
    () => apiClient.get(`/economy/accounts/${encodeURIComponent(playerUID)}/checkins`, { params: { limit: 60 } }), { items: [], count: 0 }, { fallbackOnError: false },
  ),
  adjust: (playerUID: string, delta: number, reason: string) => handleRequest<unknown, { account: EconomyAccount }>(
    () => apiClient.post(`/economy/accounts/${encodeURIComponent(playerUID)}/adjust`, {
      delta, reason, reference_type: 'manual', reference_id: `panel-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`,
    }),
    { account: { player_uid: playerUID, status: 'active', balance: 0, created_at: '', updated_at: '' } },
    { fallbackOnError: false },
  ),
  bridgeStatus: () => handleRequest<unknown, { bridge: GameEventBridgeStatus; offsets: GameEventBridgeOffset[]; online_tracking: OnlineTaskTrackingRecord[]; required_configuration: string[] }>(
    () => apiClient.get('/game-events/bridge/status'), { bridge: bridgeFallback, offsets: [], online_tracking: [], required_configuration: [] }, { fallbackOnError: false },
  ),
  repairBridge: () => handleRequest<unknown, { configuration: Record<string, boolean>; reload_required: boolean; reload_error?: string }>(
    () => apiClient.post('/game-events/bridge/repair'), { configuration: {}, reload_required: false }, { fallbackOnError: false },
  ),
  bridgeObservations: () => handleRequest<unknown, BridgeObservationList>(
    () => apiClient.get('/game-events/bridge/observations', { params: { limit: 30 } }), { items: [], count: 0 }, { fallbackOnError: false },
  ),
  bridgeDeadLetters: (status = 'pending') => handleRequest<unknown, BridgeDeadLetterList>(
    () => apiClient.get('/game-events/bridge/dead-letters', { params: { status, limit: 50 } }), { items: [], count: 0, pending: 0 }, { fallbackOnError: false },
  ),
  replayBridgeDeadLetter: (id: number, playerUID = '') => handleRequest<unknown, { dead_letter_id: number; result: Record<string, unknown> }>(
    () => apiClient.post(`/game-events/bridge/dead-letters/${id}/replay`, playerUID.trim() ? { player_uid: playerUID.trim() } : {}),
    { dead_letter_id: id, result: {} }, { fallbackOnError: false },
  ),
  dismissBridgeDeadLetter: (id: number) => handleRequest<unknown, { dead_letter_id: number; status: string }>(
    () => apiClient.post(`/game-events/bridge/dead-letters/${id}/dismiss`, {}),
    { dead_letter_id: id, status: 'dismissed' }, { fallbackOnError: false },
  ),
  inspectAstrBot: (file: File) => {
    const form = new FormData(); form.append('database', file);
    return handleRequest<unknown, LegacyAstrBotPreview>(
      () => apiClient.post('/economy/imports/astrbot/inspect', form, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 30000 }),
      { source_sha256: '', accounts: 0, bound_accounts: 0, importable_accounts: 0, unbound_accounts: 0, zero_balance: 0, decreased_balance: 0, already_current: 0, source_points: 0, importable_points: 0, checkins: 0, candidates: [] },
      { fallbackOnError: false },
    );
  },
  importAstrBot: (file: File) => {
    const form = new FormData(); form.append('database', file);
    return handleRequest<unknown, LegacyAstrBotImportResult>(
      () => apiClient.post('/economy/imports/astrbot', form, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 60000 }),
      { batch_id: '', source_sha256: '', imported_accounts: 0, imported_points: 0, imported_checkins: 0, unbound_accounts: 0, decreased_balance: 0, already_current: 0, zero_balance_accounts: 0 },
      { fallbackOnError: false },
    );
  },
};
