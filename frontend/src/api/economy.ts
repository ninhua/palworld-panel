import { apiClient, handleRequest } from './client';

export interface EconomyConfig {
  command_prefix: string;
  daily_checkin_points: number;
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

interface EconomyAccountList {
  items: EconomyAccount[];
  count: number;
}

interface EconomyLedgerList {
  items: EconomyLedgerEntry[];
  count: number;
}

const configFallback: EconomyConfig = { command_prefix: '!', daily_checkin_points: 10, updated_at: '' };
const summaryFallback: EconomySummary = {
  accounts: 0,
  total_balance: 0,
  ledger_entries: 0,
  active_reservations: 0,
  reserved_points: 0,
  issued_last_24_hours: 0,
  spent_last_24_hours: 0,
  checkins_today: 0,
  checkin_points_today: 0,
  local_date: '',
};

export const economyApi = {
  config: () => handleRequest<unknown, EconomyConfig>(
    () => apiClient.get('/economy/config'),
    configFallback,
    { fallbackOnError: false },
  ),
  updateConfig: (input: Pick<EconomyConfig, 'command_prefix' | 'daily_checkin_points'>) =>
    handleRequest<unknown, EconomyConfig>(
      () => apiClient.put('/economy/config', input),
      configFallback,
      { fallbackOnError: false },
    ),
  summary: () => handleRequest<unknown, EconomySummary>(
    () => apiClient.get('/economy/summary'),
    summaryFallback,
    { fallbackOnError: false },
  ),
  accounts: (query = '') => handleRequest<unknown, EconomyAccountList>(
    () => apiClient.get('/economy/accounts', { params: { query, limit: 100 } }),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),
  ledger: (playerUID: string) => handleRequest<unknown, EconomyLedgerList>(
    () => apiClient.get(`/economy/accounts/${encodeURIComponent(playerUID)}/ledger`, { params: { limit: 100 } }),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),
  adjust: (playerUID: string, delta: number, reason: string) => handleRequest<unknown, { account: EconomyAccount }>(
    () => apiClient.post(`/economy/accounts/${encodeURIComponent(playerUID)}/adjust`, {
      delta,
      reason,
      reference_type: 'manual',
      reference_id: `panel-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`,
    }),
    { account: { player_uid: playerUID, status: 'active', balance: 0, created_at: '', updated_at: '' } },
    { fallbackOnError: false },
  ),
  inspectAstrBot: (file: File) => {
    const form = new FormData();
    form.append('database', file);
    return handleRequest<unknown, LegacyAstrBotPreview>(
      () => apiClient.post('/economy/imports/astrbot/inspect', form, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 30000 }),
      {
        source_sha256: '', accounts: 0, bound_accounts: 0, importable_accounts: 0, unbound_accounts: 0,
        zero_balance: 0, decreased_balance: 0, already_current: 0, source_points: 0, importable_points: 0,
        checkins: 0, candidates: [],
      },
      { fallbackOnError: false },
    );
  },
  importAstrBot: (file: File) => {
    const form = new FormData();
    form.append('database', file);
    return handleRequest<unknown, LegacyAstrBotImportResult>(
      () => apiClient.post('/economy/imports/astrbot', form, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 60000 }),
      {
        batch_id: '', source_sha256: '', imported_accounts: 0, imported_points: 0, imported_checkins: 0,
        unbound_accounts: 0, decreased_balance: 0, already_current: 0, zero_balance_accounts: 0,
      },
      { fallbackOnError: false },
    );
  },
};
