import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Archive, Braces, CheckCircle2, CircleAlert, Clock3, Download, FileArchive,
  LoaderCircle, Network, Play, RefreshCw, ShieldCheck, SquareTerminal, Trash2,
} from 'lucide-react';
import { diagnosticsApi, type DiagnosticHTTPResult, type DiagnosticShellResult, type DiagnosticStatus } from '../api/diagnostics';
import { getErrorMessage } from '../api/client';
import { supportBundlesApi, type SupportBundleMetadata, type SupportBundleStatus } from '../api/supportBundles';

type ConsoleMode = 'http' | 'shell';

const formatBytes = (value: number) => {
  if (!Number.isFinite(value) || value <= 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB'];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
};

const formatHTTPResult = (result: DiagnosticHTTPResult | null) => {
  if (!result) return '';
  return [
    `${result.status} · ${result.duration_ms} ms${result.truncated ? ' · 输出已截断' : ''}`,
    '',
    ...Object.entries(result.headers || {}).map(([name, values]) => `${name}: ${values.join(', ')}`),
    '',
    result.body || '(空响应体)',
  ].join('\n');
};

const formatShellResult = (result: DiagnosticShellResult | null) => {
  if (!result) return '';
  return [
    `exit=${result.exit_code} · ${result.duration_ms} ms${result.timed_out ? ' · 已超时' : ''}${result.truncated ? ' · 输出已截断' : ''}`,
    result.error ? `error: ${result.error}` : '',
    '',
    result.output || '(无输出)',
  ].filter((line, index) => line || index > 1).join('\n');
};

export const Diagnostics: React.FC = () => {
  const [mode, setMode] = useState<ConsoleMode>('http');
  const [status, setStatus] = useState<DiagnosticStatus | null>(null);
  const [bundleStatus, setBundleStatus] = useState<SupportBundleStatus | null>(null);
  const [bundles, setBundles] = useState<SupportBundleMetadata[]>([]);
  const [includeLogs, setIncludeLogs] = useState(true);
  const [bundleBusy, setBundleBusy] = useState('');
  const [method, setMethod] = useState('GET');
  const [url, setURL] = useState('http://127.0.0.1:17993/');
  const [headersText, setHeadersText] = useState('{}');
  const [body, setBody] = useState('');
  const [command, setCommand] = useState('');
  const [confirmed, setConfirmed] = useState(false);
  const [httpResult, setHTTPResult] = useState<DiagnosticHTTPResult | null>(null);
  const [shellResult, setShellResult] = useState<DiagnosticShellResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [bundleError, setBundleError] = useState('');
  const result = useMemo(
    () => mode === 'http' ? formatHTTPResult(httpResult) : formatShellResult(shellResult),
    [httpResult, mode, shellResult],
  );

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

  const runHTTP = async () => {
    setBusy(true);
    setError('');
    try {
      const parsedHeaders = JSON.parse(headersText || '{}') as unknown;
      if (!parsedHeaders || Array.isArray(parsedHeaders) || typeof parsedHeaders !== 'object') throw new Error('请求头必须是 JSON 对象');
      const headers = Object.fromEntries(Object.entries(parsedHeaders as Record<string, unknown>).map(([name, value]) => [name, String(value)]));
      setHTTPResult(await diagnosticsApi.http({ method, url: url.trim(), headers, body }));
    } catch (runError) {
      setError(getErrorMessage(runError));
    } finally {
      setBusy(false);
    }
  };

  const runShell = async () => {
    setBusy(true);
    setError('');
    try {
      setShellResult(await diagnosticsApi.shell(command, confirmed));
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
          </div>
        </div>
      </section>

      <section className="overflow-hidden rounded-3xl border border-slate-100 bg-white shadow-sm">
        <div className="flex gap-2 border-b border-slate-100 p-3" role="group" aria-label="诊断类型">
          <button type="button" aria-pressed={mode === 'http'} onClick={() => { setMode('http'); setError(''); }} className={`pp-btn ${mode === 'http' ? 'pp-btn--primary' : ''}`}><Network size={15} /> 内网 HTTP</button>
          <button type="button" aria-pressed={mode === 'shell'} onClick={() => { setMode('shell'); setError(''); }} className={`pp-btn ${mode === 'shell' ? 'pp-btn--primary' : ''}`}><SquareTerminal size={15} /> 终端命令</button>
        </div>
        <div className="grid min-h-0 gap-0 lg:grid-cols-2">
          <div className="border-b border-slate-100 p-5 lg:border-b-0 lg:border-r">
            {mode === 'http' ? (
              <div className="space-y-4">
                <div className="grid gap-3 sm:grid-cols-[8rem_1fr]">
                  <label className="pp-field"><span className="pp-field__label">方法</span><select className="pp-input" value={method} onChange={(event) => setMethod(event.target.value)}>{['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE'].map((item) => <option key={item}>{item}</option>)}</select></label>
                  <label className="pp-field"><span className="pp-field__label">私网接口 URL</span><input className="pp-input font-mono" value={url} onChange={(event) => setURL(event.target.value)} placeholder="http://127.0.0.1:17993/" /></label>
                </div>
                <label className="pp-field"><span className="pp-field__label">请求头（JSON 对象）</span><textarea className="pp-input min-h-28 resize-y font-mono text-xs" value={headersText} onChange={(event) => setHeadersText(event.target.value)} spellCheck={false} /></label>
                <label className="pp-field"><span className="pp-field__label">请求体</span><textarea className="pp-input min-h-36 resize-y font-mono text-xs" value={body} onChange={(event) => setBody(event.target.value)} spellCheck={false} placeholder="GET/HEAD 可留空" /></label>
                <button type="button" onClick={() => void runHTTP()} disabled={busy || !url.trim()} className="pp-btn pp-btn--primary">{busy ? <LoaderCircle className="animate-spin" size={15} /> : <Play size={15} />} 执行请求</button>
              </div>
            ) : (
              <div className="space-y-4">
                {!status?.shell_enabled && <div className="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800"><p className="flex items-center gap-2 font-bold"><CircleAlert size={16} />终端命令默认关闭</p><p className="mt-2 text-pretty leading-6">设置 <code className="rounded bg-amber-100 px-1 font-mono">PALPANEL_DIAGNOSTIC_SHELL_ENABLED=true</code> 并重启后端后启用。</p></div>}
                <label className="pp-field"><span className="pp-field__label">终端命令</span><textarea className="pp-input min-h-52 resize-y font-mono text-xs" value={command} onChange={(event) => setCommand(event.target.value)} spellCheck={false} placeholder={status?.platform === 'windows' ? '例如：netstat -ano' : '例如：ss -lntp'} disabled={!status?.shell_enabled} /></label>
                <label className="flex items-start gap-3 rounded-2xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"><input type="checkbox" className="mt-0.5 size-4" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} disabled={!status?.shell_enabled} /><span className="text-pretty">我确认该命令会以 PalPanel 服务账号权限在主机执行，并可能修改或删除服务器文件。</span></label>
                <button type="button" onClick={() => void runShell()} disabled={busy || !status?.shell_enabled || !command.trim() || !confirmed} className="pp-btn pp-btn--danger">{busy ? <LoaderCircle className="animate-spin" size={15} /> : <Play size={15} />} 执行命令</button>
              </div>
            )}
            {error && <div role="alert" className="pp-note pp-note--danger mt-4">{error}</div>}
          </div>
          <div className="flex min-h-96 flex-col bg-slate-950 p-5 text-slate-200">
            <div className="mb-3 flex items-center justify-between gap-3"><h3 className="flex items-center gap-2 text-balance text-sm font-bold"><Braces size={15} className="text-sky-400" />执行结果</h3>{result && <span className="text-xs tabular-nums text-slate-400">最大 64 KiB</span>}</div>
            <pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-words rounded-2xl border border-slate-800 bg-slate-900 p-4 font-mono text-xs leading-6">{result || '执行后将在这里显示状态、响应头和输出。'}</pre>
          </div>
        </div>
      </section>
    </div>
  );
};
