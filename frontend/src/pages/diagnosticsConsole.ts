import type { DiagnosticHTTPResult, DiagnosticShellResult } from '../api/diagnostics';

export type DiagnosticConsoleMode = 'http' | 'shell';

export interface DiagnosticHTTPRequestSnapshot {
  method: string;
  url: string;
  headers: Record<string, string>;
  body: string;
}

export interface DiagnosticShellRequestSnapshot {
  command: string;
}

export type DiagnosticHistoryEntry =
  | {
      id: string;
      mode: 'http';
      created_at: string;
      title: string;
      request: DiagnosticHTTPRequestSnapshot;
      result: DiagnosticHTTPResult;
    }
  | {
      id: string;
      mode: 'shell';
      created_at: string;
      title: string;
      request: DiagnosticShellRequestSnapshot;
      result: DiagnosticShellResult;
    };

export interface DiagnosticTemplate {
  id: string;
  mode: DiagnosticConsoleMode;
  label: string;
  description: string;
  method?: string;
  url?: string;
  headers?: Record<string, string>;
  body?: string;
  command?: string;
}

export const diagnosticHistoryStorageKey = 'palpanel:diagnostic-console-history:v1';
export const maxDiagnosticHistoryEntries = 30;

const sensitiveNamePattern = /(authorization|cookie|token|api[-_]?key|secret|password|passwd|credential)/i;
const placeholderPattern = /<(?:填入|按实际|修改为|已隐藏|replace|token|password)[^>]*>/i;

const historyID = () => {
  const uuid = globalThis.crypto?.randomUUID?.();
  if (uuid) return `diagnostic-${uuid}`;
  return `diagnostic-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
};

const clip = (value: string, limit = 96) => {
  const normalized = value.replace(/\s+/g, ' ').trim();
  return normalized.length <= limit ? normalized : `${normalized.slice(0, limit - 1)}…`;
};

export const sanitizeDiagnosticHeaders = (headers: Record<string, string>) =>
  Object.fromEntries(Object.entries(headers).map(([name, value]) => [
    name,
    sensitiveNamePattern.test(name) ? '<已隐藏，不会保存到历史>' : value,
  ]));

export const sanitizeDiagnosticText = (value: string) => value
  .replace(/((?:Bearer|Basic)\s+)[A-Za-z0-9._~+/=-]+/gi, '$1<已隐藏>')
  .replace(/("(?:password|passwd|token|secret|api[_-]?key|authorization|cookie)"\s*:\s*)"(?:\\.|[^"\\])*"/gi, '$1"<已隐藏>"')
  .replace(/((?:password|passwd|token|secret|api[_-]?key)=)[^&\s]+/gi, '$1<已隐藏>');

export const sanitizeDiagnosticURL = (value: string) => {
  try {
    const parsed = new URL(value);
    const names: string[] = [];
    parsed.searchParams.forEach((_value, name) => names.push(name));
    for (const name of names) {
      if (sensitiveNamePattern.test(name)) parsed.searchParams.set(name, '<已隐藏>');
    }
    return parsed.toString();
  } catch {
    return sanitizeDiagnosticText(value);
  }
};

export const createHTTPHistoryEntry = (
  request: DiagnosticHTTPRequestSnapshot,
  result: DiagnosticHTTPResult,
): DiagnosticHistoryEntry => ({
  id: historyID(),
  mode: 'http',
  created_at: new Date().toISOString(),
  title: `${request.method.toUpperCase()} ${clip(sanitizeDiagnosticURL(request.url), 58)} · ${result.status || result.status_code}`,
  request: {
    method: request.method.toUpperCase(),
    url: sanitizeDiagnosticURL(request.url),
    headers: sanitizeDiagnosticHeaders(request.headers),
    body: sanitizeDiagnosticText(request.body),
  },
  result: {
    ...result,
    url: sanitizeDiagnosticURL(result.url || request.url),
    headers: Object.fromEntries(Object.entries(result.headers || {}).map(([name, values]) => [
      name,
      sensitiveNamePattern.test(name) ? ['<已隐藏>'] : values.map(sanitizeDiagnosticText),
    ])),
    body: sanitizeDiagnosticText(result.body || ''),
  },
});

export const createShellHistoryEntry = (
  request: DiagnosticShellRequestSnapshot,
  result: DiagnosticShellResult,
): DiagnosticHistoryEntry => ({
  id: historyID(),
  mode: 'shell',
  created_at: new Date().toISOString(),
  title: `${clip(sanitizeDiagnosticText(request.command), 70)} · exit=${result.exit_code}`,
  request: { command: sanitizeDiagnosticText(request.command) },
  result: {
    ...result,
    command: sanitizeDiagnosticText(result.command || request.command),
    output: sanitizeDiagnosticText(result.output || ''),
    error: sanitizeDiagnosticText(result.error || ''),
  },
});

const isHistoryEntry = (value: unknown): value is DiagnosticHistoryEntry => {
  if (!value || typeof value !== 'object') return false;
  const entry = value as Partial<DiagnosticHistoryEntry>;
  return typeof entry.id === 'string'
    && (entry.mode === 'http' || entry.mode === 'shell')
    && typeof entry.created_at === 'string'
    && typeof entry.title === 'string'
    && Boolean(entry.request)
    && Boolean(entry.result);
};

export const normalizeDiagnosticHistory = (value: unknown): DiagnosticHistoryEntry[] => {
  if (!Array.isArray(value)) return [];
  return value.filter(isHistoryEntry).slice(0, maxDiagnosticHistoryEntries);
};

export const loadDiagnosticHistory = (): DiagnosticHistoryEntry[] => {
  if (typeof window === 'undefined') return [];
  try {
    return normalizeDiagnosticHistory(JSON.parse(window.localStorage.getItem(diagnosticHistoryStorageKey) || '[]'));
  } catch {
    return [];
  }
};

export const saveDiagnosticHistory = (entries: DiagnosticHistoryEntry[]) => {
  const normalized = normalizeDiagnosticHistory(entries);
  if (typeof window !== 'undefined') {
    try {
      window.localStorage.setItem(diagnosticHistoryStorageKey, JSON.stringify(normalized));
    } catch {
      // History is a convenience feature. Quota or privacy-mode failures must not block diagnostics.
    }
  }
  return normalized;
};

export const appendDiagnosticHistory = (
  entries: DiagnosticHistoryEntry[],
  entry: DiagnosticHistoryEntry,
) => saveDiagnosticHistory([entry, ...entries.filter((item) => item.id !== entry.id)]);

export const formatHTTPResult = (result: DiagnosticHTTPResult | null) => {
  if (!result) return '';
  return [
    `${result.status} · ${result.duration_ms} ms${result.truncated ? ' · 输出已截断' : ''}`,
    '',
    ...Object.entries(result.headers || {}).map(([name, values]) => `${name}: ${values.join(', ')}`),
    '',
    result.body || '(空响应体)',
  ].join('\n');
};

export const formatShellResult = (result: DiagnosticShellResult | null) => {
  if (!result) return '';
  return [
    `exit=${result.exit_code} · ${result.duration_ms} ms${result.timed_out ? ' · 已超时' : ''}${result.truncated ? ' · 输出已截断' : ''}`,
    result.error ? `error: ${result.error}` : '',
    '',
    result.output || '(无输出)',
  ].filter((line, index) => line || index > 1).join('\n');
};

export const formatHTTPRequest = (request: DiagnosticHTTPRequestSnapshot) => [
  `${request.method.toUpperCase()} ${request.url}`,
  ...Object.entries(request.headers).map(([name, value]) => `${name}: ${value}`),
  request.body ? `\n${request.body}` : '',
].filter(Boolean).join('\n');

export const formatShellRequest = (request: DiagnosticShellRequestSnapshot) => request.command;

export const formatDiagnosticTranscript = (entry: DiagnosticHistoryEntry) => [
  `时间：${new Date(entry.created_at).toLocaleString()}`,
  `类型：${entry.mode === 'http' ? '内网 HTTP' : '终端命令'}`,
  '',
  '===== 请求 =====',
  entry.mode === 'http' ? formatHTTPRequest(entry.request) : formatShellRequest(entry.request),
  '',
  '===== 响应 =====',
  entry.mode === 'http' ? formatHTTPResult(entry.result) : formatShellResult(entry.result),
].join('\n');

export const hasUnresolvedDiagnosticPlaceholder = (values: Array<string | undefined>) =>
  values.some((value) => placeholderPattern.test(value || ''));

export const getDiagnosticTemplates = (platform = ''): DiagnosticTemplate[] => {
  const common: DiagnosticTemplate[] = [
    {
      id: 'http-palpanel-loopback',
      mode: 'http',
      label: 'PalPanel 回环连通性',
      description: '检查 PalPanel 默认回环端口是否能建立 HTTP 连接；端口不同请修改 URL。',
      method: 'GET',
      url: 'http://127.0.0.1:17993/',
      headers: {},
      body: '',
    },
    {
      id: 'http-palworld-rest-info',
      mode: 'http',
      label: 'Palworld REST 服务器信息',
      description: '调用 Palworld REST /v1/api/info。请按实际 REST 端口和 Basic 凭据修改。',
      method: 'GET',
      url: 'http://127.0.0.1:8212/v1/api/info',
      headers: { Authorization: 'Basic <按实际凭据修改>' },
      body: '',
    },
    {
      id: 'http-paldefender-version',
      mode: 'http',
      label: 'PalDefender REST 版本',
      description: '调用 PalDefender /v1/pdapi/version。请按实际端口填写 Bearer Token。',
      method: 'GET',
      url: 'http://127.0.0.1:8212/v1/pdapi/version',
      headers: { Authorization: 'Bearer <填入 PalDefender Token>' },
      body: '',
    },
  ];

  const windows: DiagnosticTemplate[] = [
    {
      id: 'shell-windows-ports',
      mode: 'shell',
      label: 'Windows 监听端口与 PID',
      description: '只读查看全部监听端口及关联 PID。',
      command: 'netstat -ano | findstr LISTENING',
    },
    {
      id: 'shell-windows-processes',
      mode: 'shell',
      label: 'Windows Palworld 相关进程',
      description: '只读筛选 PalServer、PalPanel 与 PalDefender 相关进程。',
      command: 'tasklist | findstr /I "PalServer Palworld PalPanel PalDefender wine"',
    },
    {
      id: 'shell-windows-system',
      mode: 'shell',
      label: 'Windows 系统与磁盘摘要',
      description: '只读输出系统信息和磁盘容量。',
      command: 'systeminfo & wmic logicaldisk get caption,freespace,size',
    },
  ];

  const unix: DiagnosticTemplate[] = [
    {
      id: 'shell-unix-ports',
      mode: 'shell',
      label: 'Linux 监听端口与进程',
      description: '只读查看 TCP 监听端口；缺少 ss 时回退到 netstat。',
      command: 'ss -lntp 2>/dev/null || netstat -lntp 2>/dev/null',
    },
    {
      id: 'shell-unix-processes',
      mode: 'shell',
      label: 'Linux Palworld 相关进程',
      description: '只读查看 PalServer、Wine、PalPanel 与 PalDefender 相关进程。',
      command: "ps -ef | grep -E '[P]alServer|[P]alworld|[P]alPanel|[P]alDefender|[w]ineserver'",
    },
    {
      id: 'shell-unix-resources',
      mode: 'shell',
      label: 'Linux CPU、内存与磁盘摘要',
      description: '只读输出负载、内存、磁盘和高 CPU 进程。',
      command: "uptime; free -h 2>/dev/null || true; df -hT; ps -eo pid,ppid,stat,%cpu,%mem,etime,cmd --sort=-%cpu | head -n 25",
    },
    {
      id: 'shell-unix-runtime-files',
      mode: 'shell',
      label: '运行目录文件摘要',
      description: '只读列出当前运行根目录两层内最近修改的文件，不输出文件内容。',
      command: "find . -maxdepth 2 -type f -printf '%TY-%Tm-%Td %TH:%TM %10s %p\\n' 2>/dev/null | sort -r | head -n 80",
    },
  ];

  return [...common, ...(platform.toLowerCase() === 'windows' ? windows : unix)];
};
