import React, { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Archive, CircleAlert, Gift, LoaderCircle, RefreshCw, Search } from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { starterGiftApi, type StarterGiftHistoryEntry } from '../api/starterGift';

const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—';
const statusLabel = (status: string) => ({ pending: '等待发放', running: '发放中', success: '发放成功', failed: '发放失败' }[status] || status || '未知');
const statusClass = (status: string) => ({
  pending: 'border-amber-200 bg-amber-50 text-amber-700',
  running: 'border-sky-200 bg-sky-50 text-sky-700',
  success: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  failed: 'border-rose-200 bg-rose-50 text-rose-700',
}[status] || 'border-slate-200 bg-slate-100 text-slate-600');

const playerName = (item: StarterGiftHistoryEntry) => item.nickname || item.steam_id || item.player_uid || item.player_id || '未知玩家';

export const StarterGiftHistory: React.FC = () => {
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState('all');
  const historyQuery = useQuery({
    queryKey: ['starter-gift', 'history'],
    queryFn: starterGiftApi.history,
  });
  const items = useMemo(() => {
    const query = search.trim().toLowerCase();
    return (historyQuery.data?.items || []).filter((item) => {
      if (status !== 'all' && item.status !== status) return false;
      return !query || `${playerName(item)} ${item.player_id} ${item.player_uid || ''} ${item.steam_id || ''} ${item.archive_reason}`.toLowerCase().includes(query);
    });
  }, [historyQuery.data, search, status]);

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <header className="flex flex-col gap-4 rounded-3xl border border-slate-200 bg-white p-5 shadow-sm lg:flex-row lg:items-center lg:justify-between">
        <div>
          <div className="flex items-center gap-2 text-xs font-black uppercase tracking-[0.18em] text-violet-500"><Archive size={16} />Starter gift archive</div>
          <h1 className="mt-2 text-2xl font-black text-slate-900">礼包发放历史</h1>
          <p className="mt-1 text-sm font-semibold text-slate-500">全量重新发放或安排下次登录重发前，旧发放周期会保留在这里。</p>
        </div>
        <button type="button" onClick={() => void historyQuery.refetch()} className="inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-4 py-2.5 text-xs font-black text-slate-600 hover:bg-slate-50">
          <RefreshCw className={historyQuery.isFetching ? 'animate-spin' : ''} size={15} />刷新
        </button>
      </header>

      {historyQuery.error && (
        <div className="rounded-2xl border border-rose-200 bg-rose-50 px-4 py-3 text-xs font-bold text-rose-700">
          <CircleAlert className="mr-2 inline" size={15} />{getErrorMessage(historyQuery.error)}
        </div>
      )}

      <section className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="mb-4 grid gap-3 sm:grid-cols-[minmax(0,1fr)_180px]">
          <label className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} />
            <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索玩家、SteamID、PlayerUID" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold outline-none focus:border-violet-400" />
          </label>
          <select value={status} onChange={(event) => setStatus(event.target.value)} className="rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-bold text-slate-600 outline-none focus:border-violet-400">
            <option value="all">全部状态</option><option value="success">发放成功</option><option value="failed">发放失败</option><option value="running">发放中</option><option value="pending">等待发放</option>
          </select>
        </div>

        {historyQuery.isLoading ? (
          <div className="py-16 text-center text-xs font-bold text-slate-400"><LoaderCircle className="mr-2 inline animate-spin" size={16} />正在读取历史记录...</div>
        ) : items.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-slate-300 py-16 text-center text-sm font-bold text-slate-400"><Gift className="mx-auto mb-3" size={24} />暂无已归档的发放周期</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full text-left text-xs">
              <thead><tr className="border-b border-slate-200 text-[11px] font-black uppercase tracking-wider text-slate-400"><th className="px-3 py-3">玩家</th><th className="px-3 py-3">原周期状态</th><th className="px-3 py-3">完成内容</th><th className="px-3 py-3">原周期时间</th><th className="px-3 py-3">归档原因</th><th className="px-3 py-3">归档时间</th></tr></thead>
              <tbody>{items.map((item) => (
                <tr key={item.id} className="border-b border-slate-100 align-top last:border-0">
                  <td className="px-3 py-3"><strong className="block text-slate-800">{playerName(item)}</strong><span className="block max-w-64 truncate font-mono text-[10px] text-slate-400">{item.steam_id || item.player_uid || item.player_id}</span></td>
                  <td className="px-3 py-3"><span className={`rounded-full border px-2 py-1 text-[10px] font-black ${statusClass(item.status)}`}>{statusLabel(item.status)}</span><span className="mt-2 block text-[10px] text-slate-400">尝试 {item.attempts} 次 · {item.progress_percent}%</span></td>
                  <td className="px-3 py-3 text-slate-600">物品 {item.next_item}/{item.item_total}<br />模板 {item.next_template}/{item.template_total}<br />科技 {item.technology_done ? '完成' : '未完成'}</td>
                  <td className="px-3 py-3 text-slate-500">开始：{formatTime(item.first_seen_at)}<br />完成：{formatTime(item.completed_at)}</td>
                  <td className="max-w-sm px-3 py-3 text-slate-600">{item.archive_reason || '发放周期被替换前归档'}</td>
                  <td className="px-3 py-3 text-slate-500">{formatTime(item.archived_at)}</td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
};
