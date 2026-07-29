import { apiClient, handleRequest } from './client';

export interface SupportBundleCheck {
  id: string;
  ok: boolean;
  required: boolean;
  message: string;
}

export interface SupportBundleStatus {
  schema_version: number;
  directory_ready: boolean;
  max_bundles: number;
  max_bundle_bytes: number;
  max_log_files: number;
  max_log_bytes_per_file: number;
  retention_days: number;
  checks: SupportBundleCheck[];
}

export interface SupportBundleMetadata {
  id: string;
  file_name: string;
  created_at: string;
  size_bytes: number;
  sha256: string;
  include_logs: boolean;
  entries: string[];
}

const emptyStatus: SupportBundleStatus = {
  schema_version: 1,
  directory_ready: false,
  max_bundles: 5,
  max_bundle_bytes: 50 * 1024 * 1024,
  max_log_files: 3,
  max_log_bytes_per_file: 128 * 1024,
  retention_days: 14,
  checks: [],
};

const fileNameFromDisposition = (value: unknown) => {
  if (typeof value !== 'string') return '';
  const encoded = value.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
  if (encoded) {
    try { return decodeURIComponent(encoded); } catch { return encoded; }
  }
  return value.match(/filename="?([^";]+)"?/i)?.[1] || '';
};

export const supportBundlesApi = {
  status: () => handleRequest<SupportBundleStatus>(
    () => apiClient.get('/system/diagnostics/support-bundles/status'),
    emptyStatus,
    { quiet: true, fallbackOnError: false },
  ),

  list: () => handleRequest<SupportBundleMetadata[]>(
    () => apiClient.get('/system/diagnostics/support-bundles'),
    [],
    { quiet: true, fallbackOnError: false },
  ),

  create: (includeLogs: boolean) => handleRequest<SupportBundleMetadata>(
    () => apiClient.post('/system/diagnostics/support-bundles', { include_logs: includeLogs, confirm: true }, { timeout: 30_000 }),
    {} as SupportBundleMetadata,
    { quiet: true, fallbackOnError: false },
  ),

  remove: (id: string) => handleRequest<{ deleted: boolean; id: string }>(
    () => apiClient.delete(`/system/diagnostics/support-bundles/${encodeURIComponent(id)}`),
    { deleted: false, id },
    { quiet: true, fallbackOnError: false },
  ),

  download: async (item: SupportBundleMetadata) => {
    const response = await apiClient.get(
      `/system/diagnostics/support-bundles/${encodeURIComponent(item.id)}/download`,
      { responseType: 'blob', timeout: 30_000 },
    );
    return {
      blob: response.data as Blob,
      fileName: fileNameFromDisposition(response.headers['content-disposition']) || item.file_name,
    };
  },
};
