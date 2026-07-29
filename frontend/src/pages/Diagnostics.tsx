import React, { useEffect, useMemo, useState } from 'react';
import { Braces, CircleAlert, Clock3, LoaderCircle, Network, Play, SquareTerminal } from 'lucide-react';
import { diagnosticsApi, type DiagnosticHTTPResult, type DiagnosticShellResult, type DiagnosticStatus } from '../api/diagnostics';
import { getErrorMessage } from '../api/client';

type ConsoleMode = 'http' | 'shell';

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
  const result = useMemo(
    () => mode === 'http' ? formatHTTPResult(httpResult) : formatShellResult(shellResult),
    [httpResult, mode, shellResult],
  );

  useEffect(() => {
    void diagnosticsApi.status()
      .then(setStatus)
      .catch((loadError) => setError(getErrorMessage(loadError)));
  }, []);

  const runHTTP = async () => {
    setBusy(true);
    setError('');
    try {
      const parsedHeaders = JSON.parse(headersText || '{}') as unknown;
      if (!parsedHeaders || Array.isArray(parsedHeaders) || typeof parsedHeaders !== 'object') {
        throw new Error('请求头必须是 JSON 对象');
      }
      const headers = Object.fromEntries(
        Object.entries(parsedHeaders as Record<string, unknown>).map(([name, value]) => [name, String(value)]),
      );
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
              <SquareTerminal size={20} className="text-sky-500" />
              诊断控制台
            </h2>
            <p className="mt-2 max-w-3xl text-pretty text-sm leading-6 text-slate-500">
              从 PalPanel 后端测试回环或私网 HTTP 接口，并在显式启用后执行主机终端命令。所有执行均限制为 15 秒和 64 KiB 输出，并写入操作审计。
            </p>
          </div>
          <div className="flex flex-wrap gap-2 text-xs font-semibold text-slate-500">
            <span className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-3 py-1.5">
              <Clock3 size={13} /> {status ? `${status.timeout_ms / 1000} 秒超时` : '读取限制…'}
            </span>
            <span className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-3 py-1.5">
              平台：{status?.platform || '未知'}
            </span>
          </div>
        </div>
      </section>

      <section className="overflow-hidden rounded-3xl border border-slate-100 bg-white shadow-sm">
        <div className="flex gap-2 border-b border-slate-100 p-3" role="group" aria-label="诊断类型">
          <button
            type="button"
            aria-pressed={mode === 'http'}
            onClick={() => { setMode('http'); setError(''); }}
            className={`pp-btn ${mode === 'http' ? 'pp-btn--primary' : ''}`}
          >
            <Network size={15} /> 内网 HTTP
          </button>
          <button
            type="button"
            aria-pressed={mode === 'shell'}
            onClick={() => { setMode('shell'); setError(''); }}
            className={`pp-btn ${mode === 'shell' ? 'pp-btn--primary' : ''}`}
          >
            <SquareTerminal size={15} /> 终端命令
          </button>
        </div>

        <div className="grid min-h-0 gap-0 lg:grid-cols-2">
          <div className="border-b border-slate-100 p-5 lg:border-b-0 lg:border-r">
            {mode === 'http' ? (
              <div className="space-y-4">
                <div className="grid gap-3 sm:grid-cols-[8rem_1fr]">
                  <label className="pp-field">
                    <span className="pp-field__label">方法</span>
                    <select className="pp-input" value={method} onChange={(event) => setMethod(event.target.value)}>
                      {['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE'].map((item) => <option key={item}>{item}</option>)}
                    </select>
                  </label>
                  <label className="pp-field">
                    <span className="pp-field__label">私网接口 URL</span>
                    <input className="pp-input font-mono" value={url} onChange={(event) => setURL(event.target.value)} placeholder="http://127.0.0.1:17993/" />
                  </label>
                </div>
                <label className="pp-field">
                  <span className="pp-field__label">请求头（JSON 对象）</span>
                  <textarea className="pp-input min-h-28 resize-y font-mono text-xs" value={headersText} onChange={(event) => setHeadersText(event.target.value)} spellCheck={false} />
                </label>
                <label className="pp-field">
                  <span className="pp-field__label">请求体</span>
                  <textarea className="pp-input min-h-36 resize-y font-mono text-xs" value={body} onChange={(event) => setBody(event.target.value)} spellCheck={false} placeholder="GET/HEAD 可留空" />
                </label>
                <button type="button" onClick={() => void runHTTP()} disabled={busy || !url.trim()} className="pp-btn pp-btn--primary">
                  {busy ? <LoaderCircle className="animate-spin" size={15} /> : <Play size={15} />} 执行请求
                </button>
              </div>
            ) : (
              <div className="space-y-4">
                {!status?.shell_enabled && (
                  <div className="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800">
                    <p className="flex items-center gap-2 font-bold"><CircleAlert size={16} />终端命令默认关闭</p>
                    <p className="mt-2 text-pretty leading-6">
                      在 PalPanel 服务环境中设置 <code className="rounded bg-amber-100 px-1 font-mono">PALPANEL_DIAGNOSTIC_SHELL_ENABLED=true</code> 并重启后端后启用。
                    </p>
                  </div>
                )}
                <label className="pp-field">
                  <span className="pp-field__label">终端命令</span>
                  <textarea
                    className="pp-input min-h-52 resize-y font-mono text-xs"
                    value={command}
                    onChange={(event) => setCommand(event.target.value)}
                    spellCheck={false}
                    placeholder={status?.platform === 'windows' ? '例如：netstat -ano' : '例如：ss -lntp'}
                    disabled={!status?.shell_enabled}
                  />
                </label>
                <label className="flex items-start gap-3 rounded-2xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">
                  <input
                    type="checkbox"
                    className="mt-0.5 size-4"
                    checked={confirmed}
                    onChange={(event) => setConfirmed(event.target.checked)}
                    disabled={!status?.shell_enabled}
                  />
                  <span className="text-pretty">我确认该命令会以 PalPanel 服务账号权限在主机执行，并可能修改或删除服务器文件。</span>
                </label>
                <button
                  type="button"
                  onClick={() => void runShell()}
                  disabled={busy || !status?.shell_enabled || !command.trim() || !confirmed}
                  className="pp-btn pp-btn--danger"
                >
                  {busy ? <LoaderCircle className="animate-spin" size={15} /> : <Play size={15} />} 执行命令
                </button>
              </div>
            )}
            {error && <div role="alert" className="pp-note pp-note--danger mt-4">{error}</div>}
          </div>

          <div className="flex min-h-96 flex-col bg-slate-950 p-5 text-slate-200">
            <div className="mb-3 flex items-center justify-between gap-3">
              <h3 className="flex items-center gap-2 text-balance text-sm font-bold"><Braces size={15} className="text-sky-400" />执行结果</h3>
              {result && <span className="text-xs tabular-nums text-slate-400">最大 64 KiB</span>}
            </div>
            <pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-words rounded-2xl border border-slate-800 bg-slate-900 p-4 font-mono text-xs leading-6">
              {result || '执行后将在这里显示状态、响应头和输出。'}
            </pre>
          </div>
        </div>
      </section>
    </div>
  );
};
