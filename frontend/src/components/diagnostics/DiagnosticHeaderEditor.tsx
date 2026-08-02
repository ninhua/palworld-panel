import React, { useEffect, useMemo, useState } from 'react';
import { Braces, Check, Code2, ListTree, Minus, Plus } from 'lucide-react';
import {
  createDiagnosticHeaderRow,
  diagnosticCommonHeaderOptions,
  diagnosticHeaderJSONToRows,
  diagnosticHeaderRowsJSON,
  normalizeDiagnosticHeaderName,
  updateDiagnosticHeaderName,
  updateDiagnosticHeaderValue,
  type DiagnosticHeaderRow,
} from '../../pages/diagnosticsConsole';

interface DiagnosticHeaderEditorProps {
  rows: DiagnosticHeaderRow[];
  onChange: (rows: DiagnosticHeaderRow[]) => void;
  onNotice?: (message: string) => void;
}

type HeaderEditorView = 'fields' | 'json';

const fallbackJSON = (rows: DiagnosticHeaderRow[], compact = false) => {
  const value = Object.fromEntries(rows.filter((row) => row.name.trim()).map((row) => {
    const name = normalizeDiagnosticHeaderName(row.name);
    if (name.toLowerCase() !== 'authorization' || row.authorization_scheme === 'Custom') return [name, row.value];
    const credential = row.value.replace(/^(?:Bearer|Basic)\s+/i, '').trim();
    return [name, `${row.authorization_scheme} ${credential}`.trim()];
  }));
  return JSON.stringify(value, null, compact ? 0 : 2);
};

export const DiagnosticHeaderEditor: React.FC<DiagnosticHeaderEditorProps> = ({ rows, onChange, onNotice }) => {
  const [raw, setRaw] = useState(() => fallbackJSON(rows));
  const [rawError, setRawError] = useState('');
  const [selectedPreset, setSelectedPreset] = useState('Authorization');
  const [view, setView] = useState<HeaderEditorView>('fields');
  const [compact, setCompact] = useState(false);

  const serialized = useMemo(() => {
    try {
      return diagnosticHeaderRowsJSON(rows, compact);
    } catch {
      return fallbackJSON(rows, compact);
    }
  }, [compact, rows]);

  useEffect(() => {
    setRaw(serialized);
  }, [serialized]);

  const commitRows = (next: DiagnosticHeaderRow[], message = '') => {
    onChange(next);
    setRaw(fallbackJSON(next, compact));
    setRawError('');
    if (message) onNotice?.(message);
  };

  const parseRaw = (nextCompact = false) => {
    try {
      const next = diagnosticHeaderJSONToRows(raw);
      setCompact(nextCompact);
      commitRows(next, `已从 JSON 解析 ${next.length} 个请求头`);
      setRaw(diagnosticHeaderRowsJSON(next, nextCompact));
    } catch (error) {
      setRawError(error instanceof Error ? error.message : '请求头 JSON 解析失败');
    }
  };

  const addRow = () => {
    if (rows.length >= 32) {
      setRawError('请求头最多允许 32 项');
      return;
    }
    const preset = diagnosticCommonHeaderOptions.find((item) => item.name === selectedPreset);
    const name = selectedPreset === '__custom__' ? '' : preset?.name || '';
    if (name && rows.some((row) => row.name.trim().toLowerCase() === name.toLowerCase())) {
      onNotice?.(`请求头 ${name} 已存在，可直接修改当前项`);
      return;
    }
    commitRows([
      ...rows,
      createDiagnosticHeaderRow(name, preset?.default_value || '', preset?.authorization_scheme),
    ], name === 'Authorization' ? '已添加 Authorization，默认 Bearer，只需填写 Token' : `已添加${name ? ` ${name}` : '自定义请求头'}`);
    setView('fields');
  };

  return (
    <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm">
      <div className="flex flex-col gap-3 border-b border-slate-200 bg-slate-50/80 px-3 py-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="inline-flex w-fit rounded-xl border border-slate-200 bg-white p-1 shadow-sm" role="group" aria-label="请求头编辑视图">
          <button
            type="button"
            aria-pressed={view === 'fields'}
            onClick={() => setView('fields')}
            className={`inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-semibold transition ${view === 'fields' ? 'bg-slate-900 text-white shadow-sm' : 'text-slate-500 hover:bg-slate-100 hover:text-slate-700'}`}
          >
            <ListTree size={14} />字段编辑
          </button>
          <button
            type="button"
            aria-pressed={view === 'json'}
            onClick={() => setView('json')}
            className={`inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-semibold transition ${view === 'json' ? 'bg-slate-900 text-white shadow-sm' : 'text-slate-500 hover:bg-slate-100 hover:text-slate-700'}`}
          >
            <Code2 size={14} />JSON
          </button>
        </div>

        <div className="flex min-w-0 items-center gap-2">
          <span className="hidden shrink-0 text-[11px] font-medium text-slate-400 sm:inline">{rows.length}/32 项</span>
          <select
            className="pp-input min-w-0 flex-1 sm:w-56 sm:flex-none"
            value={selectedPreset}
            onChange={(event) => setSelectedPreset(event.target.value)}
            aria-label="常用请求头"
          >
            {diagnosticCommonHeaderOptions.map((item) => <option key={item.name} value={item.name}>{item.label}</option>)}
            <option value="__custom__">自定义请求头</option>
          </select>
          <button type="button" className="pp-btn pp-btn--primary shrink-0 px-3" onClick={addRow} aria-label="添加请求头">
            <Plus size={15} />添加
          </button>
        </div>
      </div>

      {view === 'fields' ? (
        <div className="p-3 sm:p-4">
          {rows.length === 0 ? (
            <button
              type="button"
              onClick={addRow}
              className="flex min-h-28 w-full flex-col items-center justify-center gap-2 rounded-2xl border border-dashed border-slate-300 bg-slate-50/60 px-5 text-center text-xs text-slate-500 transition hover:border-sky-300 hover:bg-sky-50/50 hover:text-sky-700"
            >
              <span className="grid size-9 place-items-center rounded-full border border-slate-200 bg-white shadow-sm"><Plus size={16} /></span>
              添加第一个请求头
            </button>
          ) : (
            <div className="space-y-2">
              <div className="hidden grid-cols-[minmax(9rem,0.85fr)_7rem_minmax(12rem,1.4fr)_2.5rem] gap-2 px-2 text-[11px] font-semibold uppercase tracking-wide text-slate-400 xl:grid">
                <span>字段名</span><span>认证方式</span><span>字段值</span><span />
              </div>
              {rows.map((row, index) => {
                const isAuthorization = row.name.trim().toLowerCase() === 'authorization';
                return (
                  <div key={row.id} className="rounded-xl border border-slate-200 bg-white p-2.5 transition focus-within:border-sky-300 focus-within:ring-2 focus-within:ring-sky-100">
                    <div className="grid gap-2 xl:grid-cols-[minmax(9rem,0.85fr)_7rem_minmax(12rem,1.4fr)_2.5rem] xl:items-center">
                      <input
                        className="pp-input font-mono text-xs"
                        value={row.name}
                        onChange={(event) => commitRows(rows.map((item) => item.id === row.id ? updateDiagnosticHeaderName(item, event.target.value) : item))}
                        list="diagnostic-header-name-options"
                        placeholder={`字段名 ${index + 1}`}
                        spellCheck={false}
                        aria-label={`请求头 ${index + 1} 名称`}
                      />
                      {isAuthorization ? (
                        <select
                          className="pp-input"
                          value={row.authorization_scheme}
                          onChange={(event) => commitRows(rows.map((item) => item.id === row.id ? { ...item, authorization_scheme: event.target.value as DiagnosticHeaderRow['authorization_scheme'] } : item))}
                          aria-label="Authorization 认证方案"
                        >
                          <option value="Bearer">Bearer</option>
                          <option value="Basic">Basic</option>
                          <option value="Custom">自定义</option>
                        </select>
                      ) : (
                        <span className="hidden rounded-lg border border-slate-200 bg-slate-50 px-2 py-2 text-center font-mono text-[11px] text-slate-400 xl:block">string</span>
                      )}
                      <input
                        className="pp-input font-mono text-xs"
                        value={row.value}
                        onChange={(event) => commitRows(rows.map((item) => item.id === row.id ? updateDiagnosticHeaderValue(item, event.target.value) : item))}
                        placeholder={isAuthorization && row.authorization_scheme !== 'Custom' ? '只填写 Token，前缀自动补全' : '字段值'}
                        autoComplete="off"
                        spellCheck={false}
                        aria-label={`${row.name || `请求头 ${index + 1}`}值`}
                      />
                      <button
                        type="button"
                        className="inline-flex size-10 items-center justify-center rounded-lg border border-rose-200 text-rose-600 transition hover:bg-rose-50"
                        onClick={() => commitRows(rows.filter((item) => item.id !== row.id), '请求头已移除')}
                        aria-label={`删除请求头 ${row.name || index + 1}`}
                      >
                        <Minus size={15} />
                      </button>
                    </div>
                    {isAuthorization && row.authorization_scheme !== 'Custom' && (
                      <p className="mt-2 px-1 text-[11px] leading-5 text-slate-500">
                        实际发送：<code className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-sky-700">Authorization: {row.authorization_scheme} &lt;凭据&gt;</code>
                      </p>
                    )}
                  </div>
                );
              })}
            </div>
          )}
          <datalist id="diagnostic-header-name-options">
            {diagnosticCommonHeaderOptions.map((item) => <option key={item.name} value={item.name} />)}
          </datalist>
        </div>
      ) : (
        <div>
          <div className="flex flex-wrap items-center gap-2 border-b border-slate-200 px-3 py-2">
            <button type="button" className="pp-btn" onClick={() => parseRaw(false)}><Braces size={14} />格式化</button>
            <button type="button" className="pp-btn" onClick={() => parseRaw(true)}>压缩</button>
            <button type="button" className="pp-btn pp-btn--primary" onClick={() => parseRaw(false)}><Check size={14} />应用 JSON</button>
            <span className="ml-auto text-[11px] text-slate-400">对象 · {rows.length} 项</span>
          </div>
          <textarea
            className="min-h-56 w-full resize-y border-0 bg-slate-950 p-4 font-mono text-xs leading-6 text-slate-200 outline-none"
            value={raw}
            onChange={(event) => {
              setRaw(event.target.value);
              setRawError('');
            }}
            spellCheck={false}
            aria-label="请求头原始 JSON"
          />
          {rawError && <div role="alert" className="border-t border-rose-200 bg-rose-50 px-4 py-2 text-xs text-rose-700">{rawError}</div>}
        </div>
      )}
    </div>
  );
};
