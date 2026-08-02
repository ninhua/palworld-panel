import { apiClient, handleRequest } from './client';
import type { components } from './generated/contracts';

export type BossLocation = components['schemas']['BossLocation'];
export type BossRewardItem = components['schemas']['BossRewardItem'];
export type BossRewardInput = components['schemas']['BossRewardInput'];
export type BossReward = components['schemas']['BossReward'];
export type BossTemplateInput = components['schemas']['BossTemplateInput'];
export type BossTemplate = components['schemas']['BossTemplate'];
export type BossWaveInput = components['schemas']['BossWaveInput'];
export type BossWave = components['schemas']['BossWave'];
export type BossWaveSetRequest = components['schemas']['BossWaveSetRequest'];
export type BossWaveTransitionRequest = components['schemas']['BossWaveTransitionRequest'];
export type BossSummonWave = components['schemas']['BossSummonWave'];
export type BossWaveStatus = BossSummonWave['status'];
export type BossCreateSummonRequest = components['schemas']['BossCreateSummonRequest'];
export type BossTransitionRequest = components['schemas']['BossTransitionRequest'];
export type BossSummon = components['schemas']['BossSummon'];
export type BossSummonResult = components['schemas']['BossSummonResult'];
export type BossSummonEvent = components['schemas']['BossSummonEvent'];
export type BossSummary = components['schemas']['BossSummary'];
export type BossExecutionCapabilities = components['schemas']['BossExecutionCapabilities'];
export type BossExecutionStatus = components['schemas']['BossExecutionStatus'];
export type BossExecutionAttempt = components['schemas']['BossExecutionAttempt'];
export type BossSummonStatus = BossSummon['status'];
export type BossScheduleInput = components['schemas']['BossScheduleInput'];
export type BossSchedule = components['schemas']['BossSchedule'];
export type BossScheduleEvent = components['schemas']['BossScheduleEvent'];
export type BossRunDueResult = components['schemas']['BossRunDueResult'];
export type BossScheduleMode = BossSchedule['mode'];

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

const emptyExecutionStatus: BossExecutionStatus = {
  adapter: 'record_only',
  available: false,
  state: 'not_configured',
  message: '',
  capabilities: {
    fixed_coordinates: false,
    multiple_spawns: false,
    uncapturable: false,
    custom_pal_template: false,
    exact_multipliers: false,
  },
  limitations: [],
  busy: false,
  running_attempts: 0,
  uncertain_attempts: 0,
  active_waves: 0,
  reconciliation_required: false,
};

const emptyExecutionAttempt: BossExecutionAttempt = {
  id: '',
  summon_id: '',
  wave_position: 1,
  adapter: 'record_only',
  status: 'failed',
  command_count: 0,
  completed_commands: 0,
  commands: [],
  responses: [],
  details: {},
  failure: '',
  actor: '',
  started_at: '',
  completed_at: '',
  created_at: '',
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
  template_waves: 0,
  pending_waves: 0,
  active_waves: 0,
  completed_waves: 0,
  failed_waves: 0,
  skipped_waves: 0,
  schedules: 0,
  enabled_schedules: 0,
};

const emptySchedule: BossSchedule = {
  id: '',
  name: '',
  template_id: '',
  template_name: '',
  mode: 'daily',
  daily_time: '20:00',
  cron: '',
  timezone: 'Asia/Shanghai',
  warning_minutes: 30,
  warning_title: '',
  warning_message: '',
  enabled: true,
  metadata: {},
  created_at: '',
  updated_at: '',
};

export const bossApi = {
  executionStatus: () => handleRequest<unknown, BossExecutionStatus>(
    () => apiClient.get('/boss/execution/status'),
    emptyExecutionStatus,
    { fallbackOnError: false },
  ),

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

  templateWaves: (templateID: string) => handleRequest<unknown, BossListResult<BossWave>>(
    () => apiClient.get(`/boss/templates/${encodeURIComponent(templateID)}/waves`),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),

  replaceTemplateWaves: (templateID: string, input: BossWaveSetRequest) => handleRequest<unknown, BossListResult<BossWave>>(
    () => apiClient.put(`/boss/templates/${encodeURIComponent(templateID)}/waves`, input),
    { items: [], count: 0 },
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

  executeNextWave: (id: string) => handleRequest<unknown, BossExecutionAttempt>(
    () => apiClient.post(`/boss/summons/${encodeURIComponent(id)}/execute-next`),
    { ...emptyExecutionAttempt, summon_id: id },
    { fallbackOnError: false },
  ),

  executionAttempts: (id: string) => handleRequest<unknown, BossListResult<BossExecutionAttempt>>(
    () => apiClient.get(`/boss/summons/${encodeURIComponent(id)}/executions`, { params: { limit: 500 } }),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),

  transitionSummon: (id: string, input: BossTransitionRequest) => handleRequest<unknown, BossSummon>(
    () => apiClient.post(`/boss/summons/${encodeURIComponent(id)}/transition`, input),
    { ...emptySummon, id, status: input.status },
    { fallbackOnError: false },
  ),

  summonWaves: (id: string) => handleRequest<unknown, BossListResult<BossSummonWave>>(
    () => apiClient.get(`/boss/summons/${encodeURIComponent(id)}/waves`),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),

  transitionSummonWave: (id: string, position: number, input: BossWaveTransitionRequest) => handleRequest<unknown, BossSummonWave>(
    () => apiClient.post(`/boss/summons/${encodeURIComponent(id)}/waves/${position}/transition`, input),
    {
      id: 0,
      summon_id: id,
      source_wave_id: '',
      position,
      name: '',
      kind: 'main',
      pal_id: '',
      level: 1,
      count: 1,
      hp_multiplier: 1,
      attack_multiplier: 1,
      defense_multiplier: 1,
      spawn_radius: 0,
      delay_seconds: 0,
      capturable: false,
      status: input.status,
      actor: '',
      metadata: {},
      result: {},
      failure: '',
      created_at: '',
      updated_at: '',
    },
    { fallbackOnError: false },
  ),

  summonEvents: (id: string) => handleRequest<unknown, BossListResult<BossSummonEvent>>(
    () => apiClient.get(`/boss/summons/${encodeURIComponent(id)}/events`, { params: { limit: 500 } }),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),

  schedules: (includeArchived = false, enabled: boolean | undefined = undefined) => handleRequest<unknown, BossListResult<BossSchedule>>(
    () => apiClient.get('/boss/schedules', { params: { include_archived: includeArchived, enabled, limit: 500 } }),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),

  createSchedule: (input: BossScheduleInput) => handleRequest<unknown, BossSchedule>(
    () => apiClient.post('/boss/schedules', input),
    emptySchedule,
    { fallbackOnError: false },
  ),

  updateSchedule: (id: string, input: BossScheduleInput) => handleRequest<unknown, BossSchedule>(
    () => apiClient.put(`/boss/schedules/${encodeURIComponent(id)}`, input),
    { ...emptySchedule, id },
    { fallbackOnError: false },
  ),

  archiveSchedule: (id: string) => handleRequest<unknown, BossSchedule>(
    () => apiClient.delete(`/boss/schedules/${encodeURIComponent(id)}`),
    { ...emptySchedule, id, enabled: false },
    { fallbackOnError: false },
  ),

  runScheduleNow: (id: string) => handleRequest<unknown, BossSummonResult>(
    () => apiClient.post(`/boss/schedules/${encodeURIComponent(id)}/run-now`),
    { summon: emptySummon, duplicate: false },
    { fallbackOnError: false },
  ),

  testScheduleWarning: (id: string) => handleRequest<unknown, BossScheduleEvent>(
    () => apiClient.post(`/boss/schedules/${encodeURIComponent(id)}/test-warning`),
    {
      id: 0, schedule_id: id, event_type: 'warning', status: 'success', planned_for: '',
      summon_id: '', actor: '', message: '', details: {}, created_at: '',
    },
    { fallbackOnError: false },
  ),

  scheduleEvents: (scheduleID = '', eventType = '', status = '') => handleRequest<unknown, BossListResult<BossScheduleEvent>>(
    () => apiClient.get('/boss/schedule-events', { params: {
      schedule_id: scheduleID || undefined,
      event_type: eventType || undefined,
      status: status || undefined,
      limit: 500,
    } }),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),

  runDueSchedules: () => handleRequest<unknown, BossRunDueResult>(
    () => apiClient.post('/boss/maintenance/run-due'),
    { checked: 0, warnings: 0, summons: 0, failed: 0, skipped: 0 },
    { fallbackOnError: false },
  ),
};
