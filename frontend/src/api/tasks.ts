import { apiClient, handleRequest } from './client';
import type { Job } from '../types';

export const mapJobStatus = (status: unknown): Job['status'] => {
  switch (String(status || '').toLowerCase()) {
    case 'queued':
    case 'waiting':
      return 'waiting';
    case 'completed':
    case 'complete':
    case 'success':
    case 'succeeded':
      return 'success';
    case 'running':
    case 'processing':
    case 'paused':
      return 'running';
    case 'canceled':
    case 'cancelled':
    case 'failed':
    case 'error':
      return 'failed';
    default:
      return 'waiting';
  }
};

export const mapJob = (raw: unknown): Job => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const status = mapJobStatus(data.status);
  const progress = Number(data.progress);

  return {
    id: String(data.id || data.job_id || ''),
    type: String(data.type || data.kind || 'job'),
    status,
    progress: Number.isFinite(progress) ? progress : status === 'success' ? 100 : 0,
    message: data.message ? String(data.message) : undefined,
    error: data.error ? String(data.error) : undefined,
    error_code: data.error_code ? String(data.error_code) : undefined,
    created_at: String(data.created_at || data.createdAt || ''),
    updated_at: data.updated_at ? String(data.updated_at) : data.updatedAt ? String(data.updatedAt) : undefined,
    finished_at:
      data.finished_at || data.finishedAt
        ? String(data.finished_at || data.finishedAt)
        : status === 'success' || status === 'failed'
          ? String(data.updated_at || data.updatedAt || '')
          : undefined,
  };
};

export const mapJobs = (raw: unknown): Job[] => {
  if (!Array.isArray(raw)) {
    return [];
  }
  return raw.map(mapJob);
};

export const isJobDone = (job: Job) => job.status === 'success' || job.status === 'failed';

const jobPollIntervalMs = 1000;
const maxJobPollAttempts = 60 * 30;

export const createFallbackJob = (type: string, message?: string, id = `local_${Date.now()}`): Job => ({
  id,
  type,
  status: 'waiting',
  progress: 0,
  message,
  created_at: new Date().toISOString(),
});

export const tasksApi = {
  getJobs: () =>
    handleRequest<unknown, Job[]>(() => apiClient.get('/jobs'), [], {
      map: mapJobs,
      quiet: true,
    }),

  getJobById: (id: string) =>
    handleRequest<unknown, Job>(
      () => apiClient.get(`/jobs/${id}`),
      createFallbackJob('job', undefined, id),
      { map: mapJob, quiet: true },
    ),

  waitForJob: async (id: string, onUpdate?: (job: Job) => void, shouldContinue: () => boolean = () => true) => {
    let current = await tasksApi.getJobById(id);
    if (!shouldContinue()) {
      return current;
    }
    if (id.startsWith('local_') && current.status === 'waiting') {
      current = {
        ...current,
        status: 'failed',
        error: current.error || '后端暂不可用或接口未返回任务状态',
        updated_at: new Date().toISOString(),
      };
    }
    onUpdate?.(current);

    for (let attempt = 0; attempt < maxJobPollAttempts && shouldContinue() && !isJobDone(current); attempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, jobPollIntervalMs));
      if (!shouldContinue()) {
        break;
      }
      current = await tasksApi.getJobById(id);
      if (!shouldContinue()) {
        break;
      }
      onUpdate?.(current);
    }
    return current;
  },

  createBackupJob: () =>
    handleRequest<unknown, Job>(
      () => apiClient.post('/server/backup'),
      createFallbackJob('backup', '已提交备份任务'),
      { map: mapJob, quiet: true, fallbackOnError: false },
    ),

  createUpdateJob: () =>
    handleRequest<unknown, Job>(
      () => apiClient.post('/server/update'),
      createFallbackJob('update', '已提交更新任务'),
      { map: mapJob, quiet: true, fallbackOnError: false },
    ),

  createPatchUpdateJob: () =>
    handleRequest<unknown, Job>(
      () => apiClient.post('/patch/update'),
      createFallbackJob('patch_hot_update', '已提交面板补丁热更新任务'),
      { map: mapJob, quiet: true, fallbackOnError: false },
    ),
};
