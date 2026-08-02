import { apiClient, handleRequest } from './client';

export type OperationsTaskCycle = 'once' | 'daily' | 'weekly';

export interface OperationsTaskInput {
  name: string;
  description?: string;
  event_type: string;
  target_amount: number;
  reward_points: number;
  cycle: OperationsTaskCycle;
  amount_field?: string;
  filters?: Record<string, unknown>;
  enabled: boolean;
}

export interface OperationsTaskDefinition extends OperationsTaskInput {
  id: string;
  created_at: string;
  updated_at: string;
  archived_at?: string;
}

export interface OperationsTaskProgress {
  task_id: string;
  task_name: string;
  description?: string;
  event_type: string;
  cycle: OperationsTaskCycle;
  cycle_key: string;
  target_amount: number;
  reward_points: number;
  progress: number;
  completed: boolean;
  completed_at?: string;
  reward_status: string;
  reward_error?: string;
  reward_granted: boolean;
  updated_at?: string;
  filters?: Record<string, unknown>;
  amount_field?: string;
}

export interface OperationsTaskRetrySummary {
  selected: number;
  granted: number;
  failed: number;
}

export interface OperationsTaskEvent {
  event_id?: string;
  type: string;
  player_uid: string;
  nickname?: string;
  steam_id?: string;
  occurred_at?: string;
  payload?: Record<string, unknown>;
}

export interface OperationsTaskDiagnosticMatch {
  task_id: string;
  task_name: string;
  task_event_type: string;
  enabled: boolean;
  archived: boolean;
  cycle: OperationsTaskCycle;
  cycle_key?: string;
  amount_field?: string;
  filters?: Record<string, unknown>;
  target_amount: number;
  current_progress: number;
  event_amount: number;
  would_add: number;
  would_progress: number;
  status: string;
  reason: string;
  field?: string;
  expected?: unknown;
  actual?: unknown;
}

export interface OperationsTaskDiagnosticReport {
  event: OperationsTaskEvent;
  matched: number;
  would_apply: number;
  results: OperationsTaskDiagnosticMatch[];
}

export interface OperationsTaskReplayResult {
  event: OperationsTaskEvent;
  diagnostic: OperationsTaskDiagnosticReport;
  updates: Array<{
    task_id: string;
    task_name: string;
    cycle_key: string;
    added: number;
    progress: number;
    target_amount: number;
    completed: boolean;
    just_completed: boolean;
    reward_points: number;
    reward_status: string;
    reward_granted: boolean;
    duplicate: boolean;
  }>;
  count: number;
}

interface DefinitionList {
  items: OperationsTaskDefinition[];
  count: number;
}

interface ProgressList {
  items: OperationsTaskProgress[];
  count: number;
}

const emptyDefinition: OperationsTaskDefinition = {
  id: '',
  name: '',
  description: '',
  event_type: 'PLAYER_CHAT',
  target_amount: 1,
  reward_points: 0,
  cycle: 'daily',
  amount_field: '',
  filters: {},
  enabled: true,
  created_at: '',
  updated_at: '',
};

export const operationsTasksApi = {
  list: (includeArchived = false) => handleRequest<unknown, DefinitionList>(
    () => apiClient.get('/tasks', { params: { include_archived: includeArchived } }),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),
  create: (input: OperationsTaskInput) => handleRequest<unknown, OperationsTaskDefinition>(
    () => apiClient.post('/tasks', input),
    emptyDefinition,
    { fallbackOnError: false },
  ),
  update: (id: string, input: OperationsTaskInput) => handleRequest<unknown, OperationsTaskDefinition>(
    () => apiClient.put(`/tasks/${encodeURIComponent(id)}`, input),
    { ...emptyDefinition, id },
    { fallbackOnError: false },
  ),
  archive: (id: string) => handleRequest<unknown, OperationsTaskDefinition>(
    () => apiClient.delete(`/tasks/${encodeURIComponent(id)}`),
    { ...emptyDefinition, id },
    { fallbackOnError: false },
  ),
  progress: (playerUID: string) => handleRequest<unknown, ProgressList>(
    () => apiClient.get(`/tasks/progress/${encodeURIComponent(playerUID)}`),
    { items: [], count: 0 },
    { fallbackOnError: false },
  ),
  evaluateEvent: (event: OperationsTaskEvent) => handleRequest<unknown, OperationsTaskDiagnosticReport>(
    () => apiClient.post('/tasks/diagnostics/evaluate', event),
    { event, matched: 0, would_apply: 0, results: [] },
    { fallbackOnError: false },
  ),
  replayEvent: (event: OperationsTaskEvent) => handleRequest<unknown, OperationsTaskReplayResult>(
    () => apiClient.post('/tasks/diagnostics/replay', event),
    { event, diagnostic: { event, matched: 0, would_apply: 0, results: [] }, updates: [], count: 0 },
    { fallbackOnError: false },
  ),
  retryRewards: (limit = 100) => handleRequest<unknown, OperationsTaskRetrySummary>(
    () => apiClient.post('/tasks/maintenance/retry-rewards', undefined, { params: { limit } }),
    { selected: 0, granted: 0, failed: 0 },
    { fallbackOnError: false },
  ),
};
