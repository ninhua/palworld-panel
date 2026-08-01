import type { DiagnosticHTTPResult, DiagnosticShellResult } from '../api/diagnostics';

export type DiagnosticConsoleMode = 'http' | 'shell';
export type DiagnosticAuthorizationScheme = 'Bearer' | 'Basic' | 'Custom';

export interface DiagnosticHTTPRequestSnapshot {
  method: string;
  url: string;
  headers: Record<string, string>;
  body: string;
}

export interface DiagnosticShellRequestSnapshot {
  command: string;
}

export interface DiagnosticHeaderRow {
  id: string;
  name: string;
  value: string;
  authorization_scheme: DiagnosticAuthorizationScheme;
}

export interface DiagnosticCommonHeaderOption {
  name: string;
  label: string;
  description: string;
  default_value: string;
  authorization_scheme?: DiagnosticAuthorizationScheme;
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
  custom?: boolean;
  created_at?: string;
  method?: string;
  url?: string;
  headers?: Record<string, string>;
  body?: string;
  command?: string;
}

export const diagnosticHistoryStorageKey = 'palpanel:diagnostic-console-history:v1';
export const diagnosticTemplateStorageKey = 'palpanel:diagnostic-console-templates:v1';
export const maxDiagnosticHistoryEntries = 30;
export const maxDiagnosticCustomTemplates = 20;

export const diagnosticCommonHeaderOptions: DiagnosticCommonHeaderOption[] = [
  {
    name: 'Authorization',
    label: 'Authorization（Bearer Token）',
    description: '默认使用 Bearer。只需填写 Token；粘贴 Bearer 或 Basic 前缀时会自动识别。',
    default_value: '',
    authorization_scheme: 'Bearer',
  },
  {
    name: 'Accept',
    label: 'Accept',
    description: '声明期望接收的响应格式。',
    default_value: 'application/json',
  },
  {
    name: 'Content-Type',
    label: 'Content-Type',
    description: '声明请求体格式。',
    default_value: 'application/json',
  },
  {
    name: 'User-Agent',
    label: 'User-Agent',
    description: '标识诊断请求来源。',
    default_value: 'PalPanel-Diagnostics',
  },
  {
    name: 'X-API-Key',
    label: 'X-API-Key',
    description: '常见 API Key 请求头。',
    default_value: '',
  },
  {
    name: 'X-Auth-Token',
    label: 'X-Auth-Token',
    description: '常见自定义 Token 请求头。',
    default_value: '',
  },
  {
    name: 'Cookie',
    label: 'Cookie',
    description: '调试基于 Cookie 的私网接口。',
    default_value: '',
  },
];

const placeholderPattern = /<(?:填入|按实际|修改为|replace|token|password)[^>]*>/i;

const generatedID = (prefix: string) => {
  const uuid = globalThis.crypto?.randomUUID?.();
  if (uuid) return `${prefix}-${uuid}`;
  return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
};

const historyID = () => generatedID('diagnostic');
const headerRowID = () => generatedID('diagnostic-header');

const clip = (value: string, limit = 96) => {
  const normalized = value.replace(/\s+/g, ' ').trim();
  return normalized.length <= limit ? normalized : `${normalized.slice(0, limit - 1)}…`;
};

export const parseDiagnosticAuthorization = (value: string): { scheme: DiagnosticAuthorizationScheme; value: string } => {
  const trimmed = value.trim();
  const match = /^(Bearer|Basic)\s+(.+)$/i.exec(trimmed);
  if (match) {
    return {
      scheme: match[1].toLowerCase() === 'basic' ? 'Basic' : 'Bearer',
      value: match[2].trim(),
    };
  }
  if (/^[A-Za-z][A-Za-z0-9_-]*\s+.+$/.test(trimmed)) {
    return { scheme: 'Custom', value: trimmed };
  }
  return { scheme: 'Bearer', value: trimmed };
};

export const createDiagnosticHeaderRow = (
  name = '',
  value = '',
  authorizationScheme?: DiagnosticAuthorizationScheme,
): DiagnosticHeaderRow => {
  const normalizedName = name.trim();
  if (normalizedName.toLowerCase() === 'authorization') {
    const parsed = parseDiagnosticAuthorization(value);
    return {
      id: headerRowID(),
      name: 'Authorization',
      value: parsed.value,
      authorization_scheme: authorizationScheme || parsed.scheme,
    };
  }
  return {
    id: headerRowID(),
    name: normalizedName,
    value,
    authorization_scheme: authorizationScheme || 'Bearer',
  };
};

export const normalizeDiagnosticHeaderName = (name: string) => {
  const trimmed = name.trim();
  const known = diagnosticCommonHeaderOptions.find((item) => item.name.toLowerCase() === trimmed.toLowerCase());
  return known?.name || trimmed;
};

export const updateDiagnosticHeaderName = (row: DiagnosticHeaderRow, name: string): DiagnosticHeaderRow => {
  const normalized = normalizeDiagnosticHeaderName(name);
  if (normalized.toLowerCase() !== 'authorization') return { ...row, name: normalized };
  const parsed = parseDiagnosticAuthorization(row.value);
  return {
    ...row,
    name: 'Authorization',
    value: parsed.value,
    authorization_scheme: parsed.scheme,
  };
};

export const updateDiagnosticHeaderValue = (row: DiagnosticHeaderRow, value: string): DiagnosticHeaderRow => {
  if (row.name.trim().toLowerCase() !== 'authorization') return { ...row, value };
  const match = /^(Bearer|Basic)\s+(.+)$/i.exec(value.trim());
  if (!match) return { ...row, value };
  return {
    ...row,
    authorization_scheme: match[1].toLowerCase() === 'basic' ? 'Basic' : 'Bearer',
    value: match[2].trim(),
  };
};

export const diagnosticHeadersToRows = (headers: Record<string, string>): DiagnosticHeaderRow[] =>
  Object.entries(headers).map(([name, value]) => createDiagnosticHeaderRow(name, String(value)));

export const diagnosticHeaderJSONToRows = (source: string): DiagnosticHeaderRow[] => {
  const parsed = JSON.parse(source || '{}') as unknown;
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('请求头 JSON 的根节点必须是对象');
  }
  return Object.entries(parsed as Record<string, unknown>).map(([name, value]) => {
    if (value != null && typeof value === 'object') {
      throw new Error(`请求头 ${name} 的值必须是字符串、数字、布尔值或 null`);
    }
    return createDiagnosticHeaderRow(name, value == null ? '' : String(value));
  });
};

export const diagnosticHeaderRowsToRecord = (rows: DiagnosticHeaderRow[]): Record<string, string> => {
  if (rows.length > 32) throw new Error('请求头最多允许 32 项');
  const result: Record<string, string> = {};
  const seen = new Set<string>();

  for (const row of rows) {
    const name = normalizeDiagnosticHeaderName(row.name);
    const rawValue = row.value.trim();
    if (!name && !rawValue) continue;
    if (!name) throw new Error('请求头名称不能为空');
    if (/[:\r\n]/.test(name)) throw new Error(`请求头名称 ${name} 包含非法字符`);

    const lookup = name.toLowerCase();
    if (seen.has(lookup)) throw new Error(`请求头 ${name} 重复，请删除重复项`);
    seen.add(lookup);

    if (lookup === 'authorization') {
      const scheme = row.authorization_scheme || 'Bearer';
      if (!rawValue) throw new Error('Authorization 需要填写凭据');
      if (scheme === 'Custom') {
        result.Authorization = rawValue;
        continue;
      }
      const token = rawValue.replace(/^(?:Bearer|Basic)\s+/i, '').trim();
      if (!token) throw new Error(`Authorization ${scheme} 需要填写凭据`);
      result.Authorization = `${scheme} ${token}`;
      continue;
    }

    if (!rawValue) throw new Error(`请求头 ${name} 的值不能为空`);
    result[name] = rawValue;
  }

  return result;
};

export const diagnosticHeaderRowsJSON = (rows: DiagnosticHeaderRow[], compact = false) =>
  JSON.stringify(diagnosticHeaderRowsToRecord(rows), null, compact ? 0 : 2);

export const formatDiagnosticHeaderJSON = (source: string, compact = false) => {
  const rows = diagnosticHeaderJSONToRows(source);
  return diagnosticHeaderRowsJSON(rows, compact);
};

export const createHTTPHistoryEntry = (
  request: DiagnosticHTTPRequestSnapshot,
  result: DiagnosticHTTPResult,
): DiagnosticHistoryEntry => ({
  id: historyID(),
  mode: 'http',
  created_at: new Date().toISOString(),
  title: `${request.method.toUpperCase()} ${clip(request.url, 58)} · ${result.status || result.status_code}`,
  request: {
    method: request.method.toUpperCase(),
    url: request.url,
    headers: { ...request.headers },
    body: request.body,
  },
  result: {
    ...result,
    url: result.url || request.url,
    headers: Object.fromEntries(Object.entries(result.headers || {}).map(([name, values]) => [name, [...values]])),
    body: result.body || '',
  },
});

export const createShellHistoryEntry = (
  request: DiagnosticShellRequestSnapshot,
  result: DiagnosticShellResult,
): DiagnosticHistoryEntry => ({
  id: historyID(),
  mode: 'shell',
  created_at: new Date().toISOString(),
  title: `${clip(request.command, 70)} · exit=${result.exit_code}`,
  request: { command: request.command },
  result: {
    ...result,
    command: result.command || request.command,
    output: result.output || '',
    error: result.error || '',
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

const stableDiagnosticValue = (value: unknown): unknown => {
  if (Array.isArray(value)) return value.map(stableDiagnosticValue);
  if (!value || typeof value !== 'object') return value;
  return Object.fromEntries(Object.entries(value as Record<string, unknown>)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([name, item]) => [name, stableDiagnosticValue(item)]));
};

export const diagnosticHistorySignature = (entry: DiagnosticHistoryEntry) => JSON.stringify(stableDiagnosticValue({
  mode: entry.mode,
  request: entry.request,
}));

export const diagnosticHistoryEquivalent = (left: DiagnosticHistoryEntry, right: DiagnosticHistoryEntry) =>
  diagnosticHistorySignature(left) === diagnosticHistorySignature(right);

export const normalizeDiagnosticHistory = (value: unknown): DiagnosticHistoryEntry[] => {
  if (!Array.isArray(value)) return [];
  const seen = new Set<string>();
  const result: DiagnosticHistoryEntry[] = [];
  for (const entry of value.filter(isHistoryEntry)) {
    const signature = diagnosticHistorySignature(entry);
    if (seen.has(signature)) continue;
    seen.add(signature);
    result.push(entry);
    if (result.length >= maxDiagnosticHistoryEntries) break;
  }
  return result;
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
      // Storage failures must not block diagnostics execution.
    }
  }
  return normalized;
};

export const appendDiagnosticHistory = (
  entries: DiagnosticHistoryEntry[],
  entry: DiagnosticHistoryEntry,
) => saveDiagnosticHistory([entry, ...entries.filter((item) => !diagnosticHistoryEquivalent(item, entry))]);

const isDiagnosticTemplate = (value: unknown): value is DiagnosticTemplate => {
  if (!value || typeof value !== 'object') return false;
  const template = value as Partial<DiagnosticTemplate>;
  return typeof template.id === 'string'
    && (template.mode === 'http' || template.mode === 'shell')
    && typeof template.label === 'string'
    && typeof template.description === 'string';
};

export const normalizeDiagnosticTemplates = (value: unknown): DiagnosticTemplate[] => {
  if (!Array.isArray(value)) return [];
  const labels = new Set<string>();
  const result: DiagnosticTemplate[] = [];
  for (const template of value.filter(isDiagnosticTemplate)) {
    const label = template.label.trim().toLocaleLowerCase();
    if (!label || labels.has(label)) continue;
    labels.add(label);
    result.push({ ...template, custom: true });
    if (result.length >= maxDiagnosticCustomTemplates) break;
  }
  return result;
};

export const loadDiagnosticTemplates = (): DiagnosticTemplate[] => {
  if (typeof window === 'undefined') return [];
  try {
    return normalizeDiagnosticTemplates(JSON.parse(window.localStorage.getItem(diagnosticTemplateStorageKey) || '[]'));
  } catch {
    return [];
  }
};

export const saveDiagnosticTemplates = (templates: DiagnosticTemplate[]) => {
  const normalized = normalizeDiagnosticTemplates(templates);
  if (typeof window !== 'undefined') {
    try {
      window.localStorage.setItem(diagnosticTemplateStorageKey, JSON.stringify(normalized));
    } catch {
      // Storage failures must not block diagnostics execution.
    }
  }
  return normalized;
};

export const createCustomDiagnosticTemplate = (
  label: string,
  template: Omit<DiagnosticTemplate, 'id' | 'label' | 'description' | 'custom' | 'created_at'>,
): DiagnosticTemplate => {
  const normalizedLabel = label.trim();
  if (!normalizedLabel) throw new Error('模板名称不能为空');
  return {
    ...template,
    id: generatedID('diagnostic-template'),
    label: normalizedLabel,
    description: '保存在当前浏览器中的自定义诊断模板，包含当前请求的完整值。',
    custom: true,
    created_at: new Date().toISOString(),
    headers: template.headers ? { ...template.headers } : undefined,
  };
};

export const upsertDiagnosticTemplate = (
  templates: DiagnosticTemplate[],
  template: DiagnosticTemplate,
) => {
  const label = template.label.trim().toLocaleLowerCase();
  return saveDiagnosticTemplates([
    template,
    ...templates.filter((item) => item.id !== template.id && item.label.trim().toLocaleLowerCase() !== label),
  ]);
};

export const removeDiagnosticTemplate = (templates: DiagnosticTemplate[], id: string) =>
  saveDiagnosticTemplates(templates.filter((item) => item.id !== id));

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
      description: '调用 Palworld REST /v1/api/info。Authorization 会自动识别为 Basic。',
      method: 'GET',
      url: 'http://127.0.0.1:8212/v1/api/info',
      headers: { Authorization: 'Basic <按实际凭据修改>' },
      body: '',
    },
    {
      id: 'http-paldefender-version',
      mode: 'http',
      label: 'PalDefender REST 版本',
      description: '调用 PalDefender /v1/pdapi/version。Authorization 默认使用 Bearer。',
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
      command: 'uptime; free -h 2>/dev/null || true; df -hT; ps -eo pid,ppid,stat,%cpu,%mem,etime,cmd --sort=-%cpu | head -n 25',
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
