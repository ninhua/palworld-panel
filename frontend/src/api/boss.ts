import { apiClient, handleRequest } from './client';
import type { components } from './generated/contracts';

export type BossLocation = components['schemas']['BossLocation'];
export type BossRewardItem = components['schemas']['BossRewardItem'];
export type BossRewardInput = components['schemas']['BossRewardInput'];
export type BossReward = components['schemas']['BossReward'];
export type BossTemplateInput = components['schemas']['BossTemplateInput'];
export type BossTemplate = components['schemas']['BossTemplate'];
export type BossCreateSummonRequest = components['schemas']['BossCreateSummonRequest'];
export type BossTransitionRequest = components['schemas']['BossTransitionRequest'];
export type BossSummon = components['schemas']['BossSummon'];
export type BossSummonResult = components['schemas']['BossSummonResult'];
export type BossSummonEvent = components['schemas']['BossSummonEvent'];
export type BossSummary = components['schemas']['BossSummary'];
export type BossSummonStatus = BossSummon['status'];

interface BossListResult<T> {
  items: T[];
  count: number;
}

const emptyLocation: BossLocation = { x: 0, y: 0, z: 0 };

const emptyReward: BossReward = {
  id: '',
  name: '',
  description: '',
  points: 0,
  items: [],
  pal_templates: [],
  enabled: true,
  metadata: {},
  created_at: '',
  updated_at: '',
};

const emptyTemplate: BossTemplate = {
  id: '',
  name: '',
  description: '',
  pal_id: '',
  level: 1,
  count: 1,
  hp_multiplier: 1,
  attack_multiplier: 1,
  defense_multiplier: 1,
  spawn_radius: 0,
  capturable: false,
  cooldown_seconds: 0,
  reward_id: '',
  location: emptyLocation,
  enabled: true,
  metadata: {},
  created_at: '',
  updated_at: '',
};

const emptySummon: BossSummon = {
  id: '',
  request_key: '',
  template_id: '',
  template_name: '',
  reward_id: '',
  status: 'pending',
  execution_mode: 'record_only',
  actor: '',
  pal_id: '',
  level: 1,
  count: 1,
  hp_multiplier: 1,
  attack_multiplier: 1,
  defense_multiplier: 1,
  spawn_radius: 0,
  capturable: false,
  location: emptyLocation,
  notes: '',
  metadata: {},
  result: {},
  failure: '',
  requested_at: '',
  updated_at: '',
};

const emptySummary: BossSummary = {
  rewards: 0,
  enabled_rewards: 0,
  templates: 0,
  enabled_templates: 0,
  pending_summons: 0,
  active_summons: 0,
  completed_summons: 0,
  failed_summons: 0,
  cancelled_summons: 0,
};

export const bossApi = {
  summary: () => handleRequest<unknown, BossSummary>(
    () => apiClient.get('/boss/summary'),
    emptySummary,
    { fallbackOnError: false },
  ),

  rewards: (includeArchived = false) => handleRequest<unknown, BossListResult<BossReward>>(
    () => apiClient.get('/boss/rewards', { params: { include_archived: includeArchived, limit: 500 } }),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),

  createReward: (input: BossRewardInput) => handleRequest<unknown, BossReward>(
    () => apiClient.post('/boss/rewards', input),
    emptyReward,
    { fallbackOnError: false },
  ),

  updateReward: (id: string, input: BossRewardInput) => handleRequest<unknown, BossReward>(
    () => apiClient.put(`/boss/rewards/${encodeURIComponent(id)}`, input),
    { ...emptyReward, id },
    { fallbackOnError: false },
  ),

  archiveReward: (id: string) => handleRequest<unknown, BossReward>(
    () => apiClient.delete(`/boss/rewards/${encodeURIComponent(id)}`),
    { ...emptyReward, id, enabled: false },
    { fallbackOnError: false },
  ),

  templates: (includeArchived = false) => handleRequest<unknown, BossListResult<BossTemplate>>(
    () => apiClient.get('/boss/templates', { params: { include_archived: includeArchived, limit: 500 } }),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),

  createTemplate: (input: BossTemplateInput) => handleRequest<unknown, BossTemplate>(
    () => apiClient.post('/boss/templates', input),
    emptyTemplate,
    { fallbackOnError: false },
  ),

  updateTemplate: (id: string, input: BossTemplateInput) => handleRequest<unknown, BossTemplate>(
    () => apiClient.put(`/boss/templates/${encodeURIComponent(id)}`, input),
    { ...emptyTemplate, id },
    { fallbackOnError: false },
  ),

  archiveTemplate: (id: string) => handleRequest<unknown, BossTemplate>(
    () => apiClient.delete(`/boss/templates/${encodeURIComponent(id)}`),
    { ...emptyTemplate, id, enabled: false },
    { fallbackOnError: false },
  ),

  summons: (status: BossSummonStatus | '' = '', templateID = '') => handleRequest<unknown, BossListResult<BossSummon>>(
    () => apiClient.get('/boss/summons', { params: { status: status || undefined, template_id: templateID || undefined, limit: 500 } }),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),

  createSummon: (input: BossCreateSummonRequest) => handleRequest<unknown, BossSummonResult>(
    () => apiClient.post('/boss/summons', input),
    { summon: emptySummon, duplicate: false },
    { fallbackOnError: false },
  ),

  transitionSummon: (id: string, input: BossTransitionRequest) => handleRequest<unknown, BossSummon>(
    () => apiClient.post(`/boss/summons/${encodeURIComponent(id)}/transition`, input),
    { ...emptySummon, id, status: input.status },
    { fallbackOnError: false },
  ),

  summonEvents: (id: string) => handleRequest<unknown, BossListResult<BossSummonEvent>>(
    () => apiClient.get(`/boss/summons/${encodeURIComponent(id)}/events`, { params: { limit: 500 } }),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),
};
