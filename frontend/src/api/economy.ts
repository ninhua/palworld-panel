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
};
