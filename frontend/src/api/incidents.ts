import { apiClient, handleRequest } from './client';
import type { IncidentDetail, IncidentListResponse } from '../types';

const emptyList: IncidentListResponse = {
  items: [],
  total: 0,
  limit: 50,
  offset: 0,
  summary: { open: 0, acknowledged: 0, resolved: 0 },
  webhook: { enabled: false, signed: false, timeout_seconds: 10, max_attempts: 6 },
};

const emptyDetail: IncidentDetail = {
  incident: {
    id: '', kind: '', severity: 'info', source: '', title: '', summary: '', status: 'open',
    occurrences: 0, first_seen_at: '', last_seen_at: '', updated_at: '',
  },
  events: [],
  deliveries: [],
};

export interface IncidentListQuery {
  status?: string;
  severity?: string;
  source?: string;
  q?: string;
  limit?: number;
  offset?: number;
}

export const incidentsApi = {
  list: (query: IncidentListQuery = {}) => {
    const params = new URLSearchParams();
    if (query.status) params.set('status', query.status);
    if (query.severity) params.set('severity', query.severity);
    if (query.source) params.set('source', query.source);
    if (query.q) params.set('q', query.q);
    params.set('limit', String(query.limit || 50));
    params.set('offset', String(query.offset || 0));
    return handleRequest<IncidentListResponse>(
      () => apiClient.get(`/incidents?${params.toString()}`),
      emptyList,
      { fallbackOnError: false },
    );
  },

  get: (id: string) => handleRequest<IncidentDetail>(
    () => apiClient.get(`/incidents/${encodeURIComponent(id)}`),
    emptyDetail,
    { fallbackOnError: false },
  ),

  acknowledge: (id: string, message = '') => handleRequest(
    () => apiClient.post(`/incidents/${encodeURIComponent(id)}/ack`, { confirm: true, message }),
    {},
    { fallbackOnError: false },
  ),

  resolve: (id: string, message = '') => handleRequest(
    () => apiClient.post(`/incidents/${encodeURIComponent(id)}/resolve`, { confirm: true, message }),
    {},
    { fallbackOnError: false },
  ),

  reopen: (id: string, message = '') => handleRequest(
    () => apiClient.post(`/incidents/${encodeURIComponent(id)}/reopen`, { confirm: true, message }),
    {},
    { fallbackOnError: false },
  ),

  testWebhook: () => handleRequest<{ delivered: boolean; target_host?: string }>(
    () => apiClient.post('/system/incidents/webhook/test', { confirm: true }),
    { delivered: false },
    { fallbackOnError: false },
  ),
};
