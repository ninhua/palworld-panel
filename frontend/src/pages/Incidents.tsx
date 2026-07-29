import React, { useEffect, useMemo, useState } from 'react';
import { BellRing, CheckCircle2, CircleDot, RefreshCw, RotateCcw, Search, ShieldAlert, Webhook, X } from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { incidentsApi } from '../api/incidents';
import type { Incident, IncidentDetail, IncidentListResponse, IncidentSeverity, IncidentStatus } from '../types';

const severityLabel: Record<IncidentSeverity, string> = {
  info: '信息', warning: '警告', error: '错误', critical: '严重',
};

const statusLabel: Record<IncidentStatus, string> = {
  open: '待处理', acknowledged: '已确认', resolved: '已解决',
};

const severityClass: Record<IncidentSeverity, string> = {
  info: 'border-sky-200 bg-sky-50 text-sky-700',
  warning: 'border-amber-200 bg-amber-50 text-amber-700',
  error: 'border-rose-200 bg-rose-50 text-rose-700',
  critical: 'border-red-300 bg-red-50 text-red-800',
};

const formatTime = (value?: string) => value ? new Date(value).toLocaleString() : '—';

const emptyResponse: IncidentListResponse = {
  items: [], total: 0, limit: 50, offset: 0,
  summary: { open: 0, acknowledged: 0, resolved: 0 },
  webhook: { enabled: false, signed: false, timeout_seconds: 10, max_attempts: 6 },
};

export const Incidents: React.FC = () => {
  const [data, setData] = useState<IncidentListResponse>(emptyResponse);
  const [selected, setSelected] = useState<IncidentDetail | null>(null);
  const [status, setStatus] = useState('');
  const [severity, setSeverity] = useState('');
  const [source, setSource] = useState('');
  const [query, setQuery] = useState('');
  const [loading, setLoading] = useState(true);
  const [acting, setActing] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = async () => {
    setLoading(true);
    try {
      const next = await incidentsApi.list({ status, severity, source, q: query.trim(), limit: 100 });
      setData(next);
      setError(null);
      if (selected) {
        const stillExists = next.items.some((item) => item.id === selected.incident.id);
        if (!stillExists) setSelected(null);
      }
    } catch (loadError) {
      setError(getErrorMessage(loadError, '读取事件中心失败'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, [status, severity, source]);

  const openDetail = async (item: Incident) => {
    try {
      setSelected(await incidentsApi.get(item.id));
      setError(null);
    } catch (loadError) {
      setError(getErrorMessage(loadError, '读取事件详情失败'));
    }
  };

  const action = async (kind: 'ack' | 'resolve' | 'reopen') => {
    if (!selected) return;
    setActing(true);
    try {
      if (kind === 'ack') await incidentsApi.acknowledge(selected.incident.id, '管理员已确认');
      if (kind === 'resolve') await incidentsApi.resolve(selected.incident.id, '管理员确认问题已解决');
      if (kind === 'reopen') await incidentsApi.reopen(selected.incident.id, '管理员重新打开事件');
      setSelected(await incidentsApi.get(selected.incident.id));
      await load();
      setMessage(kind === 'ack' ? '事件已确认' : kind === 'resolve' ? '事件已解决' : '事件已重新打开');
    } catch (actionError) {
      setError(getErrorMessage(actionError, '事件状态更新失败'));
    } finally {
      setActing(false);
    }
  };

  const testWebhook = async () => {
    setActing(true);
    try {
      const result = await incidentsApi.testWebhook();
      setMessage(result.delivered ? `测试消息已发送到 ${result.target_host || 'Webhook'}` : 'Webhook 测试未完成');
      setError(null);
    } catch (testError) {
      setError(getErrorMessage(testError, 'Webhook 测试失败'));
    } finally {
      setActing(false);
    }
  };

  const sources = useMemo(() => Array.from(new Set(data.items.map((item) => item.source))).sort(), [data.items]);

  return (
    <div className="space-y-5">
      <header className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <p className="text-[10px] font-bold uppercase tracking-[0.2em] text-sky-500">Incident center</p>
          <h1 className="mt-1 text-2xl font-bold text-slate-900">通知与事件中心</h1>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-500">统一归并崩溃熔断、监控告警和后台任务失败。相同根因只累计次数，不重复刷屏。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          {data.webhook.enabled && (
            <button type="button" disabled={acting} onClick={testWebhook} className="inline-flex items-center gap-2 rounded-xl border border-sky-200 bg-sky-50 px-4 py-2 text-xs font-bold text-sky-700 hover:bg-sky-100 disabled:opacity-50">
              <Webhook size={15} /> 测试 Webhook
            </button>
          )}
          <button type="button" disabled={loading} onClick={load} className="inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-4 py-2 text-xs font-bold text-slate-700 hover:bg-slate-50 disabled:opacity-50">
            <RefreshCw size={15} className={loading ? 'animate-spin' : ''} /> 刷新
          </button>
        </div>
      </header>

      {(message || error) && (
        <div className={`rounded-2xl border px-4 py-3 text-sm ${error ? 'border-rose-200 bg-rose-50 text-rose-700' : 'border-emerald-200 bg-emerald-50 text-emerald-700'}`}>
          {error || message}
        </div>
      )}

      <section className="grid gap-3 sm:grid-cols-3">
        {([
          ['open', '待处理', data.summary.open, ShieldAlert],
          ['acknowledged', '已确认', data.summary.acknowledged, CircleDot],
          ['resolved', '已解决', data.summary.resolved, CheckCircle2],
        ] as const).map(([key, label, count, Icon]) => (
          <button key={key} type="button" onClick={() => setStatus(status === key ? '' : key)} className={`rounded-2xl border p-4 text-left transition ${status === key ? 'border-sky-300 bg-sky-50' : 'border-slate-200 bg-white hover:border-slate-300'}`}>
            <div className="flex items-center justify-between"><span className="text-xs font-bold text-slate-500">{label}</span><Icon size={17} className="text-slate-400" /></div>
            <p className="mt-2 text-2xl font-bold text-slate-900">{count}</p>
          </button>
        ))}
      </section>

      <section className="rounded-3xl border border-slate-200 bg-white p-4 shadow-sm">
        <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_180px_180px_auto]">
          <label className="relative">
            <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
            <input value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void load(); }} placeholder="搜索标题、来源或摘要" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-sm outline-none focus:border-sky-400" />
          </label>
          <select value={severity} onChange={(event) => setSeverity(event.target.value)} className="rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-sm text-slate-700">
            <option value="">全部级别</option><option value="critical">严重</option><option value="error">错误</option><option value="warning">警告</option><option value="info">信息</option>
          </select>
          <select value={source} onChange={(event) => setSource(event.target.value)} className="rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-sm text-slate-700">
            <option value="">全部来源</option>{sources.map((itemSource) => <option key={itemSource} value={itemSource}>{itemSource}</option>)}
          </select>
          <button type="button" onClick={load} className="rounded-xl bg-slate-900 px-5 py-2.5 text-xs font-bold text-white hover:bg-slate-800">查询</button>
        </div>
      </section>

      <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
        <div className="space-y-3">
          {loading && <div className="rounded-2xl border border-slate-200 bg-white p-8 text-center text-sm text-slate-400">正在读取事件…</div>}
          {!loading && data.items.length === 0 && <div className="rounded-2xl border border-dashed border-slate-300 bg-white p-10 text-center text-sm text-slate-500">没有符合条件的事件。</div>}
          {!loading && data.items.map((item) => (
            <button key={item.id} type="button" onClick={() => openDetail(item)} className={`w-full rounded-2xl border bg-white p-4 text-left shadow-sm transition hover:border-sky-300 ${selected?.incident.id === item.id ? 'border-sky-400 ring-2 ring-sky-100' : 'border-slate-200'}`}>
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className={`rounded-full border px-2 py-0.5 text-[10px] font-bold ${severityClass[item.severity]}`}>{severityLabel[item.severity]}</span>
                    <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-bold text-slate-600">{statusLabel[item.status]}</span>
                    {item.occurrences > 1 && <span className="text-[10px] font-bold text-slate-400">累计 {item.occurrences} 次</span>}
                  </div>
                  <h2 className="mt-2 truncate text-sm font-bold text-slate-900">{item.title}</h2>
                  <p className="mt-1 line-clamp-2 text-xs leading-5 text-slate-500">{item.summary || '无摘要'}</p>
                </div>
                <div className="text-right text-[10px] text-slate-400"><p>{item.source}</p><p className="mt-1">{formatTime(item.last_seen_at)}</p></div>
              </div>
            </button>
          ))}
        </div>

        <aside className="min-h-[320px] rounded-3xl border border-slate-200 bg-white p-5 shadow-sm">
          {!selected ? (
            <div className="flex h-full min-h-[280px] flex-col items-center justify-center text-center text-slate-400"><BellRing size={30} /><p className="mt-3 text-sm font-semibold">选择一个事件查看时间线</p></div>
          ) : (
            <div className="space-y-5">
              <div className="flex items-start justify-between gap-3">
                <div><p className="text-[10px] font-bold uppercase tracking-wide text-slate-400">{selected.incident.kind}</p><h3 className="mt-1 text-lg font-bold text-slate-900">{selected.incident.title}</h3></div>
                <button type="button" onClick={() => setSelected(null)} className="rounded-lg border border-slate-200 p-1.5 text-slate-400 hover:bg-slate-50"><X size={15} /></button>
              </div>
              <div className="grid grid-cols-2 gap-2 text-xs">
                <div className="rounded-xl bg-slate-50 p-3"><p className="text-slate-400">首次出现</p><p className="mt-1 font-semibold text-slate-700">{formatTime(selected.incident.first_seen_at)}</p></div>
                <div className="rounded-xl bg-slate-50 p-3"><p className="text-slate-400">最近出现</p><p className="mt-1 font-semibold text-slate-700">{formatTime(selected.incident.last_seen_at)}</p></div>
              </div>
              <p className="rounded-xl border border-slate-100 bg-slate-50 p-3 text-xs leading-5 text-slate-600">{selected.incident.summary || '无摘要'}</p>
              <div className="flex flex-wrap gap-2">
                {selected.incident.status === 'open' && <button disabled={acting} onClick={() => action('ack')} className="rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-bold text-amber-700">确认</button>}
                {selected.incident.status !== 'resolved' && <button disabled={acting} onClick={() => action('resolve')} className="rounded-xl border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs font-bold text-emerald-700">解决</button>}
                {selected.incident.status === 'resolved' && <button disabled={acting} onClick={() => action('reopen')} className="inline-flex items-center gap-1 rounded-xl border border-sky-200 bg-sky-50 px-3 py-2 text-xs font-bold text-sky-700"><RotateCcw size={13} /> 重新打开</button>}
              </div>
              <div>
                <h4 className="text-xs font-bold text-slate-700">事件时间线</h4>
                <div className="mt-3 space-y-3">
                  {selected.events.map((event) => <div key={event.id} className="border-l-2 border-slate-200 pl-3"><div className="flex justify-between gap-2"><span className="text-xs font-bold text-slate-700">{event.type}</span><span className="text-[10px] text-slate-400">{formatTime(event.created_at)}</span></div><p className="mt-1 text-[11px] leading-4 text-slate-500">{event.message || event.actor || '系统事件'}</p></div>)}
                </div>
              </div>
              {selected.deliveries.length > 0 && <div><h4 className="text-xs font-bold text-slate-700">Webhook 投递</h4><div className="mt-2 space-y-2">{selected.deliveries.map((delivery) => <div key={delivery.id} className="flex items-center justify-between rounded-xl bg-slate-50 px-3 py-2 text-[11px]"><span className="font-semibold text-slate-600">{delivery.status}</span><span className="text-slate-400">尝试 {delivery.attempts} 次</span></div>)}</div></div>}
            </div>
          )}
        </aside>
      </section>
    </div>
  );
};
