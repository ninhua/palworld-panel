import React, { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { ClipboardList, Copy, RefreshCw, X } from 'lucide-react';
import { auditApi } from '../api/audit';
import type { AuditLog } from '../types';
import { DataTable } from '../components/ui/DataTable';
import { StatusBadge } from '../components/ui/StatusBadge';

const formatResponseDetail = (message?: string) => {
  const value = message?.trim();
  if (!value) return '该审计记录没有响应详情。';
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
};

export const AuditLogs: React.FC = () => {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<AuditLog | null>(null);
  const [copied, setCopied] = useState(false);
  const selectedResponse = formatResponseDetail(selected?.message);

  const load = async () => {
    setLoading(true);
    const data = await auditApi.list();
    setLogs(data);
    setLoading(false);
  };

  const openDetails = (item: AuditLog) => {
    setCopied(false);
    setSelected(item);
  };

  const closeDetails = () => {
    setSelected(null);
    setCopied(false);
  };

  const copyResponse = async () => {
    if (!selected?.message) return;
    try {
      if (window.isSecureContext && navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(selectedResponse);
      } else {
        const textarea = document.createElement('textarea');
        textarea.value = selectedResponse;
        textarea.style.position = 'fixed';
        textarea.style.opacity = '0';
        document.body.appendChild(textarea);
        try {
          textarea.focus();
          textarea.select();
          if (!document.execCommand('copy')) throw new Error('copy failed');
        } finally {
          document.body.removeChild(textarea);
        }
      }
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  useEffect(() => {
    if (!selected) return;
    const previousOverflow = document.body.style.overflow;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setSelected(null);
        setCopied(false);
      }
    };
    document.body.style.overflow = 'hidden';
    window.addEventListener('keydown', onKeyDown);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener('keydown', onKeyDown);
    };
  }, [selected]);

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h3 className="flex items-center gap-2 text-[15px] font-bold text-slate-800">
              <ClipboardList size={18} className="text-sky-500" />
              操作审计
            </h3>
            <p className="mt-1 text-xs font-medium text-slate-400">记录所有写操作的操作者、角色、来源 IP、结果、响应和时间；点击记录查看完整响应。</p>
          </div>
          <button type="button" onClick={load} className="flex items-center gap-2 rounded-xl border border-slate-200 px-4 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50">
            <RefreshCw size={14} />
            刷新
          </button>
        </div>
      </section>

      <section className="rounded-3xl border border-slate-100 bg-white p-4 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)] sm:p-6">
        {loading ? (
          <div className="py-12 text-center text-xs font-semibold text-slate-400">正在读取审计日志...</div>
        ) : (
          <DataTable
            title={`最近操作（${logs.length}）`}
            headers={[
              { key: 'time', label: '时间' },
              { key: 'actor', label: '操作者' },
              { key: 'action', label: '动作' },
              { key: 'target', label: '对象' },
              { key: 'status', label: '结果' },
              { key: 'response', label: '响应' },
              { key: 'ip', label: '来源 IP' },
            ]}
            data={logs}
            emptyText="暂无审计记录"
            renderCard={(item) => (
              <div
                key={item.id}
                role="button"
                tabIndex={0}
                aria-label={`查看操作审计 ${item.action} 的响应详情`}
                onClick={() => openDetails(item)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    openDetails(item);
                  }
                }}
                className="cursor-pointer rounded-2xl border border-slate-100 bg-white p-4 shadow-sm transition hover:border-sky-200 hover:shadow-md focus:outline-none focus:ring-2 focus:ring-sky-200"
              >
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <p className="text-xs font-bold text-slate-800">{item.action}</p>
                    <p className="mt-1 text-[11px] text-slate-400">{item.actor} / {item.role}</p>
                  </div>
                  <StatusBadge status={item.status === 'success' ? 'success' : 'failed'} />
                </div>
                <div className="mt-3 rounded-xl border border-slate-100 bg-slate-50 px-3 py-2">
                  <p className="text-[10px] font-bold uppercase tracking-wide text-slate-400">响应</p>
                  <p className={`mt-1 line-clamp-3 break-words text-[11px] leading-5 ${item.status === 'success' ? 'text-slate-600' : 'text-rose-700'}`}>
                    {item.message || '无响应摘要'}
                  </p>
                  <p className="mt-1 text-[10px] font-semibold text-sky-600">点击查看完整响应</p>
                </div>
                <p className="mt-3 font-mono text-[10px] text-slate-400">{item.created_at}</p>
              </div>
            )}
            renderRow={(item) => (
              <tr
                key={item.id}
                role="button"
                tabIndex={0}
                aria-label={`查看操作审计 ${item.action} 的响应详情`}
                onClick={() => openDetails(item)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    openDetails(item);
                  }
                }}
                className="cursor-pointer hover:bg-sky-50/60 focus:bg-sky-50/60 focus:outline-none"
              >
                <td className="px-6 py-4 text-xs font-medium text-slate-400">{item.created_at}</td>
                <td className="px-6 py-4 text-xs font-bold text-slate-700">{item.actor} / {item.role}</td>
                <td className="px-6 py-4 font-mono text-[11px] text-slate-600">{item.action}</td>
                <td className="px-6 py-4 text-xs text-slate-500">{item.target || '-'}</td>
                <td className="px-6 py-4">
                  <StatusBadge status={item.status === 'success' ? 'success' : 'failed'} />
                </td>
                <td className={`max-w-md px-6 py-4 text-xs leading-5 ${item.status === 'success' ? 'text-slate-500' : 'font-semibold text-rose-700'}`}>
                  <span className="line-clamp-3 break-words" title={item.message || ''}>{item.message || '-'}</span>
                  <span className="mt-1 block text-[10px] font-semibold text-sky-600">查看详情</span>
                </td>
                <td className="px-6 py-4 font-mono text-[11px] text-slate-400">{item.ip || '-'}</td>
              </tr>
            )}
          />
        )}
      </section>

      {selected && createPortal(
        <div className="pp-dialog-backdrop fixed inset-0 z-[90] flex items-center justify-center p-4" onMouseDown={closeDetails}>
          <section
            role="dialog"
            aria-modal="true"
            aria-labelledby="audit-response-title"
            onMouseDown={(event) => event.stopPropagation()}
            className="pp-dialog-panel flex min-h-0 max-h-[88dvh] w-full max-w-3xl flex-col overflow-hidden rounded-3xl"
          >
            <header className="flex items-start justify-between gap-4 border-b border-slate-100 px-5 py-4 sm:px-6">
              <div>
                <p className="text-[10px] font-bold uppercase tracking-[0.18em] text-sky-500">Audit response</p>
                <h3 id="audit-response-title" className="mt-1 text-base font-bold text-slate-800">操作响应详情</h3>
                <p className="mt-1 font-mono text-[10px] text-slate-400">记录 ID：{selected.id || '-'}</p>
              </div>
              <button type="button" onClick={closeDetails} className="rounded-xl border border-slate-200 p-2 text-slate-500 hover:bg-slate-50" aria-label="关闭响应详情">
                <X size={17} />
              </button>
            </header>

            <div className="min-h-0 overflow-y-auto px-5 py-5 sm:px-6">
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {[
                  ['时间', selected.created_at || '-'],
                  ['操作者', `${selected.actor || '-'} / ${selected.role || '-'}`],
                  ['动作', selected.action || '-'],
                  ['对象', selected.target || '-'],
                  ['来源 IP', selected.ip || '-'],
                ].map(([label, value]) => (
                  <div key={label} className="rounded-2xl border border-slate-100 bg-slate-50 px-4 py-3">
                    <p className="text-[10px] font-bold uppercase tracking-wide text-slate-400">{label}</p>
                    <p className="mt-1 break-words text-xs font-semibold text-slate-700">{value}</p>
                  </div>
                ))}
                <div className="rounded-2xl border border-slate-100 bg-slate-50 px-4 py-3">
                  <p className="text-[10px] font-bold uppercase tracking-wide text-slate-400">结果</p>
                  <div className="mt-2"><StatusBadge status={selected.status === 'success' ? 'success' : 'failed'} /></div>
                </div>
              </div>

              <div className="mt-5 rounded-2xl border border-slate-200 bg-slate-950 p-4 text-slate-100">
                <div className="flex items-center justify-between gap-3">
                  <p className="text-[10px] font-bold uppercase tracking-[0.16em] text-slate-400">完整响应</p>
                  <button
                    type="button"
                    onClick={copyResponse}
                    disabled={!selected.message}
                    className="flex items-center gap-1.5 rounded-lg border border-slate-700 px-2.5 py-1.5 text-[10px] font-bold text-slate-300 hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    <Copy size={12} />
                    {copied ? '已复制' : '复制响应'}
                  </button>
                </div>
                <pre className={`mt-3 min-h-20 max-h-[42dvh] overflow-auto whitespace-pre-wrap break-words font-mono text-xs leading-6 ${selected.status === 'success' ? 'text-emerald-200' : 'text-rose-200'}`}>
                  {selectedResponse}
                </pre>
              </div>

              <p className="mt-3 text-[10px] leading-5 text-slate-400">此处展示的是后端已脱敏并写入审计记录的响应信息，不包含未过滤的完整 HTTP 响应体。</p>
            </div>
          </section>
        </div>,
        document.body,
      )}
    </div>
  );
};
