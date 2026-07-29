import { apiClient, handleRequest } from './client';

export interface DiagnosticStatus {
  http_enabled: boolean;
  shell_enabled: boolean;
  timeout_ms: number;
  max_output: number;
  platform: string;
}

export interface DiagnosticHTTPResult {
  method: string;
  url: string;
  status: string;
  status_code: number;
  headers: Record<string, string[]>;
  body: string;
  truncated: boolean;
  duration_ms: number;
}

export interface DiagnosticShellResult {
  command: string;
  output: string;
  exit_code: number;
  success: boolean;
  timed_out: boolean;
  truncated: boolean;
  duration_ms: number;
  error: string;
}

const emptyStatus: DiagnosticStatus = {
  http_enabled: false,
  shell_enabled: false,
  timeout_ms: 15_000,
  max_output: 65_536,
  platform: '',
};

export const diagnosticsApi = {
  status: () =>
    handleRequest<DiagnosticStatus>(
      () => apiClient.get('/system/diagnostics'),
      emptyStatus,
      { quiet: true, fallbackOnError: false },
    ),

  http: (request: { method: string; url: string; headers: Record<string, string>; body: string }) =>
    handleRequest<DiagnosticHTTPResult>(
      () => apiClient.post('/system/diagnostics/http', request, { timeout: 20_000 }),
      {} as DiagnosticHTTPResult,
      { quiet: true, fallbackOnError: false },
    ),

  shell: (command: string, confirm: boolean) =>
    handleRequest<DiagnosticShellResult>(
      () => apiClient.post('/system/diagnostics/shell', { command, confirm }, { timeout: 20_000 }),
      {} as DiagnosticShellResult,
      { quiet: true, fallbackOnError: false },
    ),
};
