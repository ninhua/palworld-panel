import { apiClient, handleRequest } from './client';

export interface BossRegistrationLocation {
  x: number;
  y: number;
  z: number;
  label?: string;
}

export interface BossRegistrationPolicy {
  summon_id: string;
  enabled: boolean;
  open: boolean;
  allow_active: boolean;
  max_players: number;
  registered: number;
  checked_in: number;
  outside: number;
  available: number;
  radius: number;
  use_z: boolean;
  area_required: boolean;
  center: BossRegistrationLocation;
  summon_status: string;
  configuration: string;
}

export interface BossParticipant {
  id: number;
  summon_id: string;
  player_uid: string;
  nickname?: string;
  steam_id?: string;
  status: 'registered' | 'checked_in' | 'cancelled';
  area_status: 'unknown' | 'eligible' | 'outside' | 'disabled';
  eligible: boolean;
  location?: BossRegistrationLocation;
  distance: number;
  actor?: string;
  metadata?: Record<string, unknown>;
  registered_at: string;
  checked_at?: string;
  cancelled_at?: string;
  updated_at: string;
}

export interface BossParticipantEvent {
  id: number;
  summon_id: string;
  player_uid: string;
  event_type: string;
  actor?: string;
  details?: Record<string, unknown>;
  created_at: string;
}

export interface BossRegistrationSnapshot {
  policy: BossRegistrationPolicy;
  participants: BossParticipant[];
  events: BossParticipantEvent[];
  count: number;
  event_count: number;
}

export interface BossLocationLookup {
  found: boolean;
  source: string;
  matched_by?: string;
  player_uid?: string;
  account_name?: string;
  identifier?: string;
  location?: BossRegistrationLocation;
  observed_at?: string;
  error?: string;
}

export interface BossParticipantResult {
  participant: BossParticipant;
  policy: BossRegistrationPolicy;
  duplicate: boolean;
  location_lookup?: BossLocationLookup;
}

export interface BossRegistrationRefreshResult {
  summon_id: string;
  online_players: number;
  participants: number;
  updated: number;
  unavailable: number;
  failed: number;
  lookups: BossLocationLookup[];
  snapshot: BossRegistrationSnapshot;
}


export type BossTransportState = 'prepared' | 'teleported' | 'teleport_failed' | 'returned' | 'return_failed';

export interface BossParticipantTransport {
  id: number;
  summon_id: string;
  player_uid: string;
  nickname?: string;
  identifier: string;
  state: BossTransportState;
  origin: BossRegistrationLocation;
  destination: BossRegistrationLocation;
  teleport_attempts: number;
  return_attempts: number;
  last_error?: string;
  actor?: string;
  metadata?: Record<string, unknown>;
  prepared_at: string;
  teleported_at?: string;
  returned_at?: string;
  updated_at: string;
}

export interface BossTransportSnapshot {
  summon_id: string;
  items: BossParticipantTransport[];
  count: number;
  prepared: number;
  teleported: number;
  teleport_failed: number;
  returned: number;
  return_failed: number;
}

export interface BossTransportItemResult {
  player_uid: string;
  nickname?: string;
  status: string;
  error?: string;
  location_lookup?: BossLocationLookup;
  transport?: BossParticipantTransport;
}

export interface BossTransportBatchResult {
  summon_id: string;
  action: 'teleport' | 'return';
  requested: number;
  selected: number;
  succeeded: number;
  failed: number;
  skipped: number;
  items: BossTransportItemResult[];
  snapshot: BossTransportSnapshot;
}

export interface BossTransportBatchInput {
  player_uids?: string[];
  spread_radius?: number;
}

export interface BossRegistrationPolicyInput {
  enabled?: boolean;
  max_players?: number;
  radius?: number;
  use_z?: boolean;
  allow_active?: boolean;
}

export interface BossParticipantInput {
  player_uid: string;
  nickname?: string;
  steam_id?: string;
  metadata?: Record<string, unknown>;
}

const emptyPolicy: BossRegistrationPolicy = {
  summon_id: '', enabled: false, open: false, allow_active: false,
  max_players: 0, registered: 0, checked_in: 0, outside: 0, available: -1,
  radius: 0, use_z: false, area_required: false,
  center: { x: 0, y: 0, z: 0 }, summon_status: '', configuration: 'disabled',
};

const emptySnapshot: BossRegistrationSnapshot = {
  policy: emptyPolicy,
  participants: [],
  events: [],
  count: 0,
  event_count: 0,
};


const emptyTransportSnapshot: BossTransportSnapshot = {
  summon_id: '', items: [], count: 0, prepared: 0, teleported: 0,
  teleport_failed: 0, returned: 0, return_failed: 0,
};

const control = <T>(summonID: string, status: string, result: Record<string, unknown>, fallback: T) =>
  handleRequest<unknown, T>(
    () => apiClient.post(`/boss/summons/${encodeURIComponent(summonID)}/transition`, {
      status,
      message: '',
      result,
    }),
    fallback,
    { fallbackOnError: false },
  );

export const bossRegistrationApi = {
  snapshot: (summonID: string, includeCancelled = false) =>
    control(summonID, 'registration_snapshot', { include_cancelled: includeCancelled }, emptySnapshot),

  configure: (summonID: string, input: BossRegistrationPolicyInput) =>
    control(summonID, 'registration_configure', input as unknown as Record<string, unknown>, emptySnapshot),

  registerOnline: (summonID: string, input: BossParticipantInput) =>
    control<BossParticipantResult>(summonID, 'participant_register_online', input as unknown as Record<string, unknown>, {
      participant: {
        id: 0, summon_id: summonID, player_uid: input.player_uid, status: 'registered', area_status: 'unknown',
        eligible: false, distance: 0, registered_at: '', updated_at: '',
      },
      policy: { ...emptyPolicy, summon_id: summonID },
      duplicate: false,
    }),

  checkOnline: (summonID: string, input: BossParticipantInput) =>
    control<BossParticipantResult>(summonID, 'participant_check_area_online', input as unknown as Record<string, unknown>, {
      participant: {
        id: 0, summon_id: summonID, player_uid: input.player_uid, status: 'registered', area_status: 'unknown',
        eligible: false, distance: 0, registered_at: '', updated_at: '',
      },
      policy: { ...emptyPolicy, summon_id: summonID },
      duplicate: false,
    }),

  cancel: (summonID: string, playerUID: string, reason = '') =>
    control<BossParticipantResult>(summonID, 'participant_cancel', { player_uid: playerUID, reason }, {
      participant: {
        id: 0, summon_id: summonID, player_uid: playerUID, status: 'cancelled', area_status: 'unknown',
        eligible: false, distance: 0, registered_at: '', updated_at: '',
      },
      policy: { ...emptyPolicy, summon_id: summonID },
      duplicate: false,
    }),

  refreshOnline: (summonID: string) =>
    control<BossRegistrationRefreshResult>(summonID, 'registration_refresh_online', {}, {
      summon_id: summonID,
      online_players: 0,
      participants: 0,
      updated: 0,
      unavailable: 0,
      failed: 0,
      lookups: [],
      snapshot: { ...emptySnapshot, policy: { ...emptyPolicy, summon_id: summonID } },
    }),


  transportSnapshot: (summonID: string) =>
    control<BossTransportSnapshot>(summonID, 'transport_snapshot', {}, { ...emptyTransportSnapshot, summon_id: summonID }),

  teleportParticipants: (summonID: string, input: BossTransportBatchInput = {}) =>
    control<BossTransportBatchResult>(summonID, 'participants_teleport', input as unknown as Record<string, unknown>, {
      summon_id: summonID, action: 'teleport', requested: 0, selected: 0,
      succeeded: 0, failed: 0, skipped: 0, items: [],
      snapshot: { ...emptyTransportSnapshot, summon_id: summonID },
    }),

  returnParticipants: (summonID: string, input: BossTransportBatchInput = {}) =>
    control<BossTransportBatchResult>(summonID, 'participants_return', input as unknown as Record<string, unknown>, {
      summon_id: summonID, action: 'return', requested: 0, selected: 0,
      succeeded: 0, failed: 0, skipped: 0, items: [],
      snapshot: { ...emptyTransportSnapshot, summon_id: summonID },
    }),
};
