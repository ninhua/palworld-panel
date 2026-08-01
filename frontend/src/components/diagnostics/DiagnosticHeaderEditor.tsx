import React, { useEffect, useMemo, useState } from 'react';
import { Braces, Check, ChevronDown, ChevronRight, Minus, Plus } from 'lucide-react';
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
  const [expanded, setExpanded] = useState(true);
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
  };

  return (
    <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white">
      <div className="flex flex-wrap items-center gap-2 border-b border-slate-200 bg-slate-50 px-3 py-2">
        <button type="button" className="pp-btn" onClick={() => parseRaw(false)}><Braces size={14} />格式化 JSON</button>
        <button type="button" className="pp-btn" onClick={() => parseRaw(true)}>压缩 JSON</button>
        <button type="button" className="pp-btn pp-btn--primary" onClick={() => parseRaw(false)}><Check size={14} />解析 JSON</button>
        <span className="ml-auto text-[11px] text-slate-500">对象 · {rows.length} 项 · 最多 32 项</span>
      </div>

      <div className="grid min-h-72 lg:grid-cols-2">
        <div className="border-b border-slate-200 lg:border-b-0 lg:border-r">
          <textarea
            className="min-h-72 w-full resize-y border-0 bg-white p-4 font-mono text-xs leading-6 text-slate-700 outline-none"
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

        <div className="min-w-0 bg-slate-50/40 p-3">
          <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
            <button type="button" className="flex items-center gap-1 text-xs font-bold text-slate-700" onClick={() => setExpanded((value) => !value)}>
              {expanded ? <ChevronDown size={15} /> : <ChevronRight size={15} />} object <span className="font-mono text-slate-400">{'{'}{rows.length}{'}'}</span>
            </button>
            <div className="ml-auto flex min-w-0 flex-1 gap-2 sm:max-w-md">
              <select className="pp-input min-w-0 flex-1" value={selectedPreset} onChange={(event) => setSelectedPreset(event.target.value)}>
                {diagnosticCommonHeaderOptions.map((item) => <option key={item.name} value={item.name}>{item.label}</option>)}
                <option value="__custom__">自定义请求头</option>
              </select>
              <button type="button" className="pp-btn pp-btn--primary shrink-0 px-3" onClick={addRow} aria-label="添加请求头"><Plus size={15} /></button>
            </div>
          </div>

          {expanded && (
            <div className="space-y-2 pl-4">
              {rows.length === 0 && <div className="rounded-xl border border-dashed border-slate-300 bg-white p-5 text-center text-xs text-slate-500">点击右上角“＋”添加请求头。</div>}
              {rows.map((row, index) => {
                const isAuthorization = row.name.trim().toLowerCase() === 'authorization';
                return (
                  <div key={row.id} className="rounded-xl border border-slate-200 bg-white p-3 shadow-sm">
                    <div className="grid gap-2 xl:grid-cols-[minmax(9rem,0.85fr)_7rem_minmax(10rem,1.35fr)_auto] xl:items-center">
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
                      ) : <span className="hidden rounded-lg bg-slate-100 px-2 py-2 text-center text-[11px] text-slate-400 xl:block">string</span>}
                      <input
                        className="pp-input font-mono text-xs"
                        value={row.value}
                        onChange={(event) => commitRows(rows.map((item) => item.id === row.id ? updateDiagnosticHeaderValue(item, event.target.value) : item))}
                        placeholder={isAuthorization && row.authorization_scheme !== 'Custom' ? '只填写 Token；前缀自动添加' : '字段值'}
                        autoComplete="off"
                        spellCheck={false}
                        aria-label={`${row.name || `请求头 ${index + 1}`}值`}
                      />
                      <button type="button" className="pp-btn pp-btn--danger px-3" onClick={() => commitRows(rows.filter((item) => item.id !== row.id), '请求头已移除')} aria-label={`删除请求头 ${row.name || index + 1}`}><Minus size={15} /></button>
                    </div>
                    {isAuthorization && row.authorization_scheme !== 'Custom' && (
                      <p className="mt-2 text-[11px] leading-5 text-sky-700">实际发送：<code className="font-mono">Authorization: {row.authorization_scheme} &lt;凭据&gt;</code>。粘贴带 Bearer/Basic 前缀的值会自动识别并去除重复前缀。</p>
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
      </div>
    </div>
  );
};
