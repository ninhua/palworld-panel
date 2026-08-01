import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Archive, BookOpen, Braces, CheckCircle2, CircleAlert, Clock3, Copy, Download, FileArchive,
  History, LoaderCircle, Network, Play, RefreshCw, RotateCcw, Save, ShieldCheck, SquareTerminal, Trash2, X,
} from 'lucide-react';
import { diagnosticsApi, type DiagnosticHTTPResult, type DiagnosticShellResult, type DiagnosticStatus } from '../api/diagnostics';
import { getErrorMessage } from '../api/client';
import { supportBundlesApi, type SupportBundleMetadata, type SupportBundleStatus } from '../api/supportBundles';
import {
  appendDiagnosticHistory,
  createCustomDiagnosticTemplate,
  createHTTPHistoryEntry,
  createShellHistoryEntry,
  diagnosticHeaderRowsJSON,
  diagnosticHeaderRowsToRecord,
  diagnosticHeadersToRows,
  diagnosticHistoryStorageKey,
  diagnosticTemplateStorageKey,
  formatHTTPResult,
  formatShellResult,
  getDiagnosticTemplates,
  hasUnresolvedDiagnosticPlaceholder,
  loadDiagnosticHistory,
  loadDiagnosticTemplates,
  maxDiagnosticCustomTemplates,
  maxDiagnosticHistoryEntries,
  removeDiagnosticTemplate,
  saveDiagnosticHistory,
  upsertDiagnosticTemplate,
  type DiagnosticConsoleMode,
  type DiagnosticHeaderRow,
  type DiagnosticHistoryEntry,
  type DiagnosticTemplate,
} from './diagnosticsConsole';
import { DiagnosticHeaderEditor } from '../components/diagnostics/DiagnosticHeaderEditor';

const formatBytes = (value: number) => {
  if (!Number.isFinite(value) || value <= 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB'];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
};

const formatHistoryTime = (value: string) => {
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) ? new Date(timestamp).toLocaleString() : value;
};

const copyToClipboard = async (value: string) => {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const textarea = document.createElement('textarea');
  textarea.value = value;
  textarea.setAttribute('readonly', '');
  textarea.style.position = 'fixed';
  textarea.style.opacity = '0';
  document.body.appendChild(textarea);
  textarea.select();
  const copied = document.execCommand('copy');
  textarea.remove();
  if (!copied) throw new Error('浏览器不允许复制到剪贴板');
};

export const Diagnostics: React.FC = () => {
  const [mode, setMode] = useState<DiagnosticConsoleMode>('http');
  const [status, setStatus] = useState<DiagnosticStatus | null>(null);
  const [bundleStatus, setBundleStatus] = useState<SupportBundleStatus | null>(null);
  const [bundles, setBundles] = useState<SupportBundleMetadata[]>([]);
  const [includeLogs, setIncludeLogs] = useState(true);
  const [bundleBusy, setBundleBusy] = useState('');
  const [method, setMethod] = useState('GET');
  const [url, setURL] = useState('http://127.0.0.1:17993/');
  const [headerRows, setHeaderRows] = useState<DiagnosticHeaderRow[]>([]);
  const [body, setBody] = useState('');
  const [command, setCommand] = useState('');
  const [confirmed, setConfirmed] = useState(false);
  const [httpResult, setHTTPResult] = useState<DiagnosticHTTPResult | null>(null);
  const [shellResult, setShellResult] = useState<DiagnosticShellResult | null>(null);
  const [historyEntries, setHistoryEntries] = useState<DiagnosticHistoryEntry[]>(loadDiagnosticHistory);
  const [customTemplates, setCustomTemplates] = useState<DiagnosticTemplate[]>(loadDiagnosticTemplates);
  const [selectedHistoryID, setSelectedHistoryID] = useState('');
  const [selectedTemplateID, setSelectedTemplateID] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [bundleError, setBundleError] = useState('');

  const templates = useMemo(() => [...getDiagnosticTemplates(status?.platform || ''), ...customTemplates], [customTemplates, status?.platform]);
  const selectedHistory = useMemo(
    () => historyEntries.find((entry) => entry.id === selectedHistoryID) || null,
    [historyEntries, selectedHistoryID],
  );
  const result = useMemo(
    () => mode === 'http' ? formatHTTPResult(httpResult) : formatShellResult(shellResult),
    [httpResult, mode, shellResult],
  );
  const headersPreview = useMemo(() => {
    try {
      return diagnosticHeaderRowsJSON(headerRows);
    } catch {
      const preview = Object.fromEntries(headerRows
        .filter((row) => row.name.trim())
        .map((row) => {
          const name = row.name.trim();
          if (name.toLowerCase() !== 'authorization' || row.authorization_scheme === 'Custom') {
            return [name, row.value];
          }
          const token = row.value.replace(/^(?:Bearer|Basic)\s+/i, '').trim();
          return [name, `${row.authorization_scheme} ${token}`.trim()];
        }));
      return JSON.stringify(preview, null, 2);
    }
  }, [headerRows]);
  const requestText = useMemo(() => mode === 'http'
    ? [
        `${method} ${url.trim() || '(未填写 URL)'}`,
        '',
        '请求头 JSON：',
        headersPreview,
        '',
        '请求体：',
        body || '(空)',
      ].join('\n')
    : command || '(未填写命令)', [body, command, headersPreview, method, mode, url]);
  const transcript = useMemo(() => [
    `时间：${selectedHistory ? formatHistoryTime(selectedHistory.created_at) : new Date().toLocaleString()}`,
    `类型：${mode === 'http' ? '内网 HTTP' : '终端命令'}`,
    '',
    '===== 请求 =====',
    requestText,
    '',
    '===== 响应 =====',
    result || '(尚未执行)',
  ].join('\n'), [mode, requestText, result, selectedHistory]);
  const unresolvedPlaceholder = mode === 'http' && hasUnresolvedDiagnosticPlaceholder([url, headersPreview, body]);

  const loadBundles = useCallback(async () => {
    setBundleError('');
    try {
      const [nextStatus, nextBundles] = await Promise.all([supportBundlesApi.status(), supportBundlesApi.list()]);
      setBundleStatus(nextStatus);
      setBundles(nextBundles);
    } catch (loadError) {
      setBundleError(getErrorMessage(loadError));
    }
  }, []);

  useEffect(() => {
    void diagnosticsApi.status().then(setStatus).catch((loadError) => setError(getErrorMessage(loadError)));
    void loadBundles();
  }, [loadBundles]);

  const syncHistory = (entries: DiagnosticHistoryEntry[]) => {
    const next = saveDiagnosticHistory(entries);
    setHistoryEntries(next);
    return next;
  };

  const createBundle = async () => {
    setBundleBusy('create');
    setBundleError('');
    try {
      await supportBundlesApi.create(includeLogs);
      await loadBundles();
    } catch (createError) {
      setBundleError(getErrorMessage(createError));
    } finally {
      setBundleBusy('');
    }
  };

  const downloadBundle = async (item: SupportBundleMetadata) => {
    setBundleBusy(`download:${item.id}`);
    setBundleError('');
    try {
      const { blob, fileName } = await supportBundlesApi.download(item);
      const objectURL = window.URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = objectURL;
      anchor.download = fileName;
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      window.URL.revokeObjectURL(objectURL);
    } catch (downloadError) {
      setBundleError(getErrorMessage(downloadError));
    } finally {
      setBundleBusy('');
    }
  };

  const removeBundle = async (item: SupportBundleMetadata) => {
    if (!window.confirm(`删除支持包 ${item.file_name}？`)) return;
    setBundleBusy(`delete:${item.id}`);
    setBundleError('');
    try {
      await supportBundlesApi.remove(item.id);
      await loadBundles();
    } catch (removeError) {
      setBundleError(getErrorMessage(removeError));
    } finally {
      setBundleBusy('');
    }
  };

  const selectMode = (nextMode: DiagnosticConsoleMode) => {
    setMode(nextMode);
    setError('');
    setNotice('');
    setSelectedHistoryID('');
  };

  const applyTemplate = (template: DiagnosticTemplate) => {
    setMode(template.mode);
    setSelectedTemplateID(template.id);
    setSelectedHistoryID('');
    setError('');
    setHTTPResult(null);
    setShellResult(null);
    setConfirmed(false);
    if (template.mode === 'http') {
      setMethod(template.method || 'GET');
      setURL(template.url || '');
      setHeaderRows(diagnosticHeadersToRows(template.headers || {}));
      setBody(template.body || '');
    } else {
      setCommand(template.command || '');
    }
    setNotice(`已载入诊断模板：${template.label}`);
  };

  const loadHistoryEntry = (entry: DiagnosticHistoryEntry) => {
    setMode(entry.mode);
    setSelectedHistoryID(entry.id);
    setSelectedTemplateID('');
    setError('');
    setNotice(`已载入 ${formatHistoryTime(entry.created_at)} 的历史记录`);
    setConfirmed(false);
    if (entry.mode === 'http') {
      setMethod(entry.request.method);
      setURL(entry.request.url);
      setHeaderRows(diagnosticHeadersToRows(entry.request.headers || {}));
      setBody(entry.request.body || '');
      setHTTPResult(entry.result);
      setShellResult(null);
    } else {
      setCommand(entry.request.command);
      setShellResult(entry.result);
      setHTTPResult(null);
    }
  };

  const removeSelectedHistory = () => {
    if (!selectedHistory) return;
    const next = syncHistory(historyEntries.filter((entry) => entry.id !== selectedHistory.id));
    setSelectedHistoryID('');
    setNotice(`已删除历史记录，当前剩余 ${next.length} 条`);
  };

  const clearHistory = () => {
    if (historyEntries.length === 0) return;
    if (!window.confirm(`清空当前浏览器保存的 ${historyEntries.length} 条诊断历史？`)) return;
    syncHistory([]);
    setSelectedHistoryID('');
    setNotice('诊断历史已清空');
  };

  const clearResult = () => {
    setHTTPResult(null);
    setShellResult(null);
    setSelectedHistoryID('');
    setError('');
    setNotice('执行结果已清空，请求内容保留');
  };

  const copyText = async (value: string, label: string) => {
    try {
      await copyToClipboard(value);
      setNotice(`${label}已复制到剪贴板`);
      setError('');
    } catch (copyError) {
      setError(getErrorMessage(copyError, `${label}复制失败`));
    }
  };

  const saveCurrentTemplate = () => {
    const label = window.prompt('请输入模板名称');
    if (label == null) return;
    try {
      const template = createCustomDiagnosticTemplate(label, mode === 'http'
        ? { mode: 'http', method, url: url.trim(), headers: diagnosticHeaderRowsToRecord(headerRows), body }
        : { mode: 'shell', command });
      const next = upsertDiagnosticTemplate(customTemplates, template);
      setCustomTemplates(next);
      setSelectedTemplateID(template.id);
      setNotice(`已保存自定义模板：${template.label}（${next.length}/${maxDiagnosticCustomTemplates}）`);
      setError('');
    } catch (templateError) {
      setError(getErrorMessage(templateError));
    }
  };

  const removeSelectedTemplate = () => {
    const template = customTemplates.find((item) => item.id === selectedTemplateID);
    if (!template) return;
    if (!window.confirm(`删除自定义模板“${template.label}”？`)) return;
    const next = removeDiagnosticTemplate(customTemplates, template.id);
    setCustomTemplates(next);
    setSelectedTemplateID('');
    setNotice(`已删除自定义模板：${template.label}`);
  };

  const runHTTP = async () => {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      if (unresolvedPlaceholder) throw new Error('模板中仍有未替换的占位内容，请先填写实际凭据或删除该请求头');
      const headers = diagnosticHeaderRowsToRecord(headerRows);
      const request = { method, url: url.trim(), headers, body };
      const nextResult = await diagnosticsApi.http(request);
      setHTTPResult(nextResult);
      setShellResult(null);
      const entry = createHTTPHistoryEntry(request, nextResult);
      const nextHistory = appendDiagnosticHistory(historyEntries, entry);
      setHistoryEntries(nextHistory);
      setSelectedHistoryID(entry.id);
      setNotice(`请求完成，已更新当前浏览器历史（相同请求不重复，${nextHistory.length}/${maxDiagnosticHistoryEntries}）`);
    } catch (runError) {
      setError(getErrorMessage(runError));
    } finally {
      setBusy(false);
    }
  };

  const runShell = async () => {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const nextResult = await diagnosticsApi.shell(command, confirmed);
      setShellResult(nextResult);
      setHTTPResult(null);
      const entry = createShellHistoryEntry({ command }, nextResult);
      const nextHistory = appendDiagnosticHistory(historyEntries, entry);
      setHistoryEntries(nextHistory);
      setSelectedHistoryID(entry.id);
      setNotice(`命令完成，已更新当前浏览器历史（相同命令不重复，${nextHistory.length}/${maxDiagnosticHistoryEntries}）`);
      setConfirmed(false);
    } catch (runError) {
      setError(getErrorMessage(runError));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-sm">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <h2 className="flex items-center gap-2 text-balance text-lg font-bold text-slate-800">
              <ShieldCheck size={20} className="text-emerald-500" />诊断与支持包
            </h2>
            <p className="mt-2 max-w-3xl text-pretty text-sm leading-6 text-slate-500">
              执行固定白名单体检并生成脱敏 ZIP。支持包不包含环境变量、数据库、原始存档、密码、Token、完整路径、IP 或玩家标识。
            </p>
          </div>
          <button type="button" className="pp-btn" onClick={() => void loadBundles()} disabled={Boolean(bundleBusy)}>
            <RefreshCw size={15} className={bundleBusy ? 'animate-spin' : ''} />刷新
          </button>
        </div>
        <div className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {(bundleStatus?.checks || []).map((check) => (
            <div key={check.id} className={`rounded-2xl border p-3 text-sm ${check.ok ? 'border-emerald-100 bg-emerald-50' : check.required ? 'border-rose-200 bg-rose-50' : 'border-amber-200 bg-amber-50'}`}>
              <p className="flex items-center gap-2 font-bold text-slate-700">
                {check.ok ? <CheckCircle2 size={15} className="text-emerald-600" /> : <CircleAlert size={15} className={check.required ? 'text-rose-600' : 'text-amber-600'} />}
                {check.id}
              </p>
              <p className="mt-1 text-xs leading-5 text-slate-600">{check.message}</p>
            </div>
          ))}
        </div>
        <div className="mt-5 flex flex-col gap-3 rounded-2xl border border-slate-100 bg-slate-50 p-4 sm:flex-row sm:items-center sm:justify-between">
          <label className="flex items-start gap-3 text-sm text-slate-700">
            <input type="checkbox" className="mt-0.5 size-4" checked={includeLogs} onChange={(event) => setIncludeLogs(event.target.checked)} />
            <span>附带最近 {bundleStatus?.max_log_files || 3} 个日志尾部；每个最多 {formatBytes(bundleStatus?.max_log_bytes_per_file || 131072)}，生成前自动脱敏。</span>
          </label>
          <button type="button" className="pp-btn pp-btn--primary shrink-0" disabled={bundleBusy === 'create' || bundleStatus?.directory_ready === false} onClick={() => void createBundle()}>
            {bundleBusy === 'create' ? <LoaderCircle size={15} className="animate-spin" /> : <Archive size={15} />}生成支持包
          </button>
        </div>
        {bundleError && <div role="alert" className="pp-note pp-note--danger mt-4">{bundleError}</div>}
      </section>

      <section className="overflow-hidden rounded-3xl border border-slate-100 bg-white shadow-sm">
        <div className="flex items-center justify-between border-b border-slate-100 p-4">
          <h3 className="flex items-center gap-2 font-bold text-slate-800"><FileArchive size={17} className="text-sky-500" />已生成支持包</h3>
          <span className="text-xs text-slate-500">最多保留 {bundleStatus?.max_bundles || 5} 份 / {bundleStatus?.retention_days || 14} 天</span>
        </div>
        {bundles.length === 0 ? (
          <p className="p-6 text-sm text-slate-500">尚未生成支持包。</p>
        ) : (
          <div className="divide-y divide-slate-100">
            {bundles.map((item) => (
              <div key={item.id} className="flex flex-col gap-3 p-4 lg:flex-row lg:items-center lg:justify-between">
                <div className="min-w-0">
                  <p className="truncate font-mono text-sm font-semibold text-slate-700">{item.file_name}</p>
                  <p className="mt-1 text-xs text-slate-500">{new Date(item.created_at).toLocaleString()} · {formatBytes(item.size_bytes)} · {item.entries.length} 个条目 · 日志：{item.include_logs ? '包含' : '不包含'}</p>
                  <p className="mt-1 truncate font-mono text-[11px] text-slate-400">SHA-256 {item.sha256}</p>
                </div>
                <div className="flex shrink-0 gap-2">
                  <button type="button" className="pp-btn" disabled={Boolean(bundleBusy)} onClick={() => void downloadBundle(item)}>
                    {bundleBusy === `download:${item.id}` ? <LoaderCircle size={15} className="animate-spin" /> : <Download size={15} />}下载
                  </button>
                  <button type="button" className="pp-btn pp-btn--danger" disabled={Boolean(bundleBusy)} onClick={() => void removeBundle(item)}>
                    {bundleBusy === `delete:${item.id}` ? <LoaderCircle size={15} className="animate-spin" /> : <Trash2 size={15} />}删除
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-sm">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <h2 className="flex items-center gap-2 text-balance text-lg font-bold text-slate-800"><SquareTerminal size={20} className="text-sky-500" />诊断控制台</h2>
            <p className="mt-2 max-w-3xl text-pretty text-sm leading-6 text-slate-500">从 PalPanel 后端测试回环或私网 HTTP 接口，并在显式启用后执行主机终端命令。所有执行均限制为 15 秒和 64 KiB 输出，并写入操作审计。</p>
          </div>
          <div className="flex flex-wrap gap-2 text-xs font-semibold text-slate-500">
            <span className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-3 py-1.5"><Clock3 size={13} /> {status ? `${status.timeout_ms / 1000} 秒超时` : '读取限制…'}</span>
            <span className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-3 py-1.5">平台：{status?.platform || '未知'}</span>
            <span className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-3 py-1.5"><History size={13} /> 历史 {historyEntries.length}/{maxDiagnosticHistoryEntries}</span>
          </div>
        </div>
      </section>

      <section className="overflow-hidden rounded-3xl border border-slate-100 bg-white shadow-sm">
        <div className="flex flex-wrap gap-2 border-b border-slate-100 p-3" role="group" aria-label="诊断类型">
          <button type="button" aria-pressed={mode === 'http'} onClick={() => selectMode('http')} className={`pp-btn ${mode === 'http' ? 'pp-btn--primary' : ''}`}><Network size={15} /> 内网 HTTP</button>
          <button type="button" aria-pressed={mode === 'shell'} onClick={() => selectMode('shell')} className={`pp-btn ${mode === 'shell' ? 'pp-btn--primary' : ''}`}><SquareTerminal size={15} /> 终端命令</button>
        </div>

        <div className="grid gap-4 border-b border-slate-100 bg-slate-50/70 p-4 xl:grid-cols-2">
          <div className="rounded-2xl border border-slate-200 bg-white p-4">
            <div className="mb-3 flex items-center gap-2 text-sm font-bold text-slate-700"><BookOpen size={16} className="text-violet-500" />诊断模板</div>
            <div className="flex flex-col gap-2 sm:flex-row">
              <select
                className="pp-input min-w-0 flex-1"
                value={selectedTemplateID}
                onChange={(event) => {
                  const template = templates.find((item) => item.id === event.target.value);
                  if (template) applyTemplate(template);
                }}
              >
                <option value="">选择常用模板…</option>
                {templates.map((template) => <option key={template.id} value={template.id}>{template.custom ? '自定义' : template.mode === 'http' ? 'HTTP' : '终端'} · {template.label}</option>)}
              </select>
              {selectedTemplateID && <button type="button" className="pp-btn" onClick={() => {
                const template = templates.find((item) => item.id === selectedTemplateID);
                if (template) applyTemplate(template);
              }}><RotateCcw size={14} />重新载入</button>}
              <button type="button" className="pp-btn pp-btn--primary" onClick={saveCurrentTemplate}><Save size={14} />保存当前</button>
              <button type="button" className="pp-btn pp-btn--danger" disabled={!customTemplates.some((item) => item.id === selectedTemplateID)} onClick={removeSelectedTemplate}><Trash2 size={14} />删除模板</button>
            </div>
            <p className="mt-2 text-xs leading-5 text-slate-500">{templates.find((item) => item.id === selectedTemplateID)?.description || '模板只填充请求，不会自动执行。可把当前请求完整保存为浏览器自定义模板，包含 Authorization 等字段。'}</p>
          </div>

          <div className="rounded-2xl border border-slate-200 bg-white p-4">
            <div className="mb-3 flex items-center justify-between gap-2">
              <span className="flex items-center gap-2 text-sm font-bold text-slate-700"><History size={16} className="text-sky-500" />执行历史</span>
              <span className="text-[11px] text-slate-400">当前浏览器 · 最近 {maxDiagnosticHistoryEntries} 条</span>
            </div>
            <div className="flex flex-col gap-2 sm:flex-row">
              <select
                className="pp-input min-w-0 flex-1"
                value={selectedHistoryID}
                onChange={(event) => {
                  const entry = historyEntries.find((item) => item.id === event.target.value);
                  if (entry) loadHistoryEntry(entry);
                  else setSelectedHistoryID('');
                }}
              >
                <option value="">选择历史记录…</option>
                {historyEntries.map((entry) => <option key={entry.id} value={entry.id}>{formatHistoryTime(entry.created_at)} · {entry.mode === 'http' ? 'HTTP' : '终端'} · {entry.title}</option>)}
              </select>
              <button type="button" className="pp-btn pp-btn--danger" disabled={!selectedHistory} onClick={removeSelectedHistory}><Trash2 size={14} />删除</button>
              <button type="button" className="pp-btn" disabled={historyEntries.length === 0} onClick={clearHistory}><X size={14} />清空</button>
            </div>
            <p className="mt-2 text-xs leading-5 text-slate-500">历史保存在本机浏览器的 <code className="font-mono">localStorage</code>，会保留 Authorization、Cookie、Token 和请求体原值。相同请求只保留最新一次响应，不重复新增。历史键：<code className="font-mono">{diagnosticHistoryStorageKey}</code>；模板键：<code className="font-mono">{diagnosticTemplateStorageKey}</code></p>
          </div>
        </div>

        <div className="grid min-h-0 gap-0 lg:grid-cols-2">
          <div className="border-b border-slate-100 p-5 lg:border-b-0 lg:border-r">
            {mode === 'http' ? (
              <div className="space-y-4">
                <div className="grid gap-3 sm:grid-cols-[8rem_1fr]">
                  <label className="pp-field"><span className="pp-field__label">方法</span><select className="pp-input" value={method} onChange={(event) => { setMethod(event.target.value); setSelectedHistoryID(''); }}>{['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE'].map((item) => <option key={item}>{item}</option>)}</select></label>
                  <label className="pp-field"><span className="pp-field__label">私网接口 URL</span><input className="pp-input font-mono" value={url} onChange={(event) => { setURL(event.target.value); setSelectedHistoryID(''); }} placeholder="http://127.0.0.1:17993/" /></label>
                </div>
                <div>
                  <div className="mb-2 flex items-center justify-between gap-3">
                    <div>
                      <p className="text-sm font-bold text-slate-700">请求头 JSON</p>
                      <p className="mt-1 text-xs leading-5 text-slate-500">左侧可直接编辑 JSON；右侧通过“＋/－”增删字段。两侧解析后同步，Authorization 会自动识别 Bearer、Basic 或自定义值。</p>
                    </div>
                  </div>
                  <DiagnosticHeaderEditor
                    rows={headerRows}
                    onChange={(next) => { setHeaderRows(next); setSelectedHistoryID(''); setSelectedTemplateID(''); }}
                    onNotice={(message) => { setNotice(message); setError(''); }}
                  />
                </div>
                <label className="pp-field"><span className="pp-field__label">请求体</span><textarea className="pp-input min-h-36 resize-y font-mono text-xs" value={body} onChange={(event) => { setBody(event.target.value); setSelectedHistoryID(''); }} spellCheck={false} placeholder="GET/HEAD 可留空" /></label>
                {unresolvedPlaceholder && <div className="rounded-2xl border border-amber-200 bg-amber-50 p-3 text-xs leading-5 text-amber-800">模板中仍包含凭据占位符。请替换为实际值，或删除不需要的请求头。</div>}
                <button type="button" onClick={() => void runHTTP()} disabled={busy || !url.trim() || unresolvedPlaceholder} className="pp-btn pp-btn--primary">{busy ? <LoaderCircle className="animate-spin" size={15} /> : <Play size={15} />} 执行请求</button>
              </div>
            ) : (
              <div className="space-y-4">
                {!status?.shell_enabled && <div className="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800"><p className="flex items-center gap-2 font-bold"><CircleAlert size={16} />终端命令默认关闭</p><p className="mt-2 text-pretty leading-6">设置 <code className="rounded bg-amber-100 px-1 font-mono">PALPANEL_DIAGNOSTIC_SHELL_ENABLED=true</code> 并重启后端后启用。</p></div>}
                <label className="pp-field"><span className="pp-field__label">终端命令</span><textarea className="pp-input min-h-52 resize-y font-mono text-xs" value={command} onChange={(event) => { setCommand(event.target.value); setSelectedHistoryID(''); }} spellCheck={false} placeholder={status?.platform === 'windows' ? '例如：netstat -ano' : '例如：ss -lntp'} disabled={!status?.shell_enabled} /></label>
                <label className="flex items-start gap-3 rounded-2xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"><input type="checkbox" className="mt-0.5 size-4" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} disabled={!status?.shell_enabled} /><span className="text-pretty">我确认该命令会以 PalPanel 服务账号权限在主机执行，并可能修改或删除服务器文件。内置模板均为只读命令，但执行前仍应检查内容。</span></label>
                <button type="button" onClick={() => void runShell()} disabled={busy || !status?.shell_enabled || !command.trim() || !confirmed} className="pp-btn pp-btn--danger">{busy ? <LoaderCircle className="animate-spin" size={15} /> : <Play size={15} />} 执行命令</button>
              </div>
            )}
            {error && <div role="alert" className="pp-note pp-note--danger mt-4">{error}</div>}
            {notice && <div role="status" className="pp-note mt-4">{notice}</div>}
          </div>

          <div className="flex min-h-96 flex-col bg-slate-950 p-5 text-slate-200">
            <div className="mb-3 flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
              <div>
                <h3 className="flex items-center gap-2 text-balance text-sm font-bold"><Braces size={15} className="text-sky-400" />执行结果</h3>
                {selectedHistory && <p className="mt-1 text-[11px] text-slate-400">历史记录：{formatHistoryTime(selectedHistory.created_at)}</p>}
              </div>
              <div className="flex flex-wrap gap-2">
                <button type="button" className="rounded-lg border border-slate-700 px-2.5 py-1.5 text-xs font-semibold text-slate-300 hover:bg-slate-800 disabled:opacity-40" disabled={!result} onClick={() => void copyText(result, '响应')}><Copy size={13} className="mr-1 inline" />复制响应</button>
                <button type="button" className="rounded-lg border border-slate-700 px-2.5 py-1.5 text-xs font-semibold text-slate-300 hover:bg-slate-800" onClick={() => void copyText(requestText, '请求')}><Copy size={13} className="mr-1 inline" />复制请求</button>
                <button type="button" className="rounded-lg border border-slate-700 px-2.5 py-1.5 text-xs font-semibold text-slate-300 hover:bg-slate-800" onClick={() => void copyText(transcript, '完整记录')}><Copy size={13} className="mr-1 inline" />复制完整记录</button>
                <button type="button" className="rounded-lg border border-slate-700 px-2.5 py-1.5 text-xs font-semibold text-slate-300 hover:bg-slate-800 disabled:opacity-40" disabled={!result} onClick={clearResult}><X size={13} className="mr-1 inline" />清空结果</button>
              </div>
            </div>
            <pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-words rounded-2xl border border-slate-800 bg-slate-900 p-4 font-mono text-xs leading-6">{result || '执行后将在这里显示状态、响应头和输出。可从上方选择历史记录恢复请求与响应。'}</pre>
            <p className="mt-3 text-[11px] leading-5 text-slate-500">实时结果最大 {formatBytes(status?.max_output || 65536)}。历史和自定义模板会在当前浏览器中原样保存请求数据，请仅在受信任设备使用。</p>
          </div>
        </div>
      </section>
    </div>
  );
};
