import React, { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  AlertTriangle, ArrowRight, Boxes, Building2, ChevronDown, Clock3, Download, FileDiff,
  PackageMinus, PackagePlus, PawPrint, RefreshCw, Search, ShieldCheck, TrendingDown, TrendingUp,
  UserRound, UsersRound,
} from 'lucide-react';
import {
  saveHistoryApi,
  type SaveHistoryChange,
  type SaveHistoryDiff,
  type SaveHistoryDiffSummary,
  type SaveHistoryEvent,
  type SaveHistorySnapshot,
} from '../api/saveHistory';
import { getErrorMessage } from '../api/client';

const categories = [
  { value: 'all', label: '全部变化', icon: <FileDiff size={14} /> },
  { value: 'players', label: '玩家', icon: <UserRound size={14} /> },
  { value: 'guilds', label: '公会', icon: <UsersRound size={14} /> },
  { value: 'bases', label: '据点', icon: <Building2 size={14} /> },
  { value: 'pals', label: '帕鲁', icon: <PawPrint size={14} /> },
  { value: 'items', label: '物品', icon: <PackagePlus size={14} /> },
  { value: 'containers', label: '容器', icon: <Boxes size={14} /> },
];

const categoryLabels: Record<string, string> = Object.fromEntries(categories.map((item) => [item.value, item.label]));
const kindLabels: Record<string, string> = {
  added: '新增', removed: '移除', changed: '修改', increased: '增加', decreased: '减少',
};
const fieldLabels: Record<string, string> = {
  nickname: '名称', level: '等级', guild: '公会', last_online: '最后在线', owner: '归属', members: '成员',
  bases: '据点', location: '位置', structures: '建筑数', workers: '工作帕鲁', containers: '容器', status: '状态',
  character: '帕鲁种类', container: '容器', slot: '槽位', location_type: '位置类型', rank: '星级', ivs: '个体值',
  passives: '被动词条', owner_type: '归属类型', contents: '内容摘要', count: '数量',
};

export const SaveHistory: React.FC = () => {
  const history = useQuery({ queryKey: ['save-history'], queryFn: saveHistoryApi.list });
  const [fromID, setFromID] = useState('');
  const [toID, setToID] = useState('');
  const [category, setCategory] = useState('all');
  const [query, setQuery] = useState('');
  const [selectionSourceID, setSelectionSourceID] = useState('');
  const sourceID = history.data?.source.id || '';
  const minimumIntervalSeconds = history.data?.minimum_interval_seconds || 15 * 60;

  useEffect(() => {
    const items = history.data?.items || [];
    if (sourceID !== selectionSourceID) {
      const newestID = items[0]?.id || '';
      setSelectionSourceID(sourceID);
      setToID(newestID);
      setFromID(selectMeaningfulBaseline(items, minimumIntervalSeconds, newestID)?.id || '');
      return;
    }
    const ids = new Set(items.map((item) => item.id));
    const nextTo = toID && ids.has(toID) ? toID : items[0]?.id || '';
    const nextFrom = fromID && ids.has(fromID) && fromID !== nextTo
      ? fromID
      : selectMeaningfulBaseline(items, minimumIntervalSeconds, nextTo)?.id || '';
    if (nextTo !== toID) setToID(nextTo);
    if (nextFrom !== fromID) setFromID(nextFrom);
  }, [fromID, history.data?.items, minimumIntervalSeconds, selectionSourceID, sourceID, toID]);

  const diff = useQuery({
    queryKey: ['save-history-diff', sourceID, fromID, toID, category, query],
    queryFn: () => saveHistoryApi.diff({ from: fromID, to: toID, category, q: query.trim(), limit: 200, offset: 0 }),
    enabled: Boolean(fromID && toID && fromID !== toID),
  });

  const snapshots = history.data?.items || [];
  const events = diff.data?.events ?? [];
  const summaryCards = useMemo(() => summarize(diff.data?.summary), [diff.data?.summary]);
  const attentionEvents = useMemo(() => selectAttentionEvents(events), [events]);
  const notice = history.error ? getErrorMessage(history.error) : diff.error ? getErrorMessage(diff.error) : '';

  const compareLatest = () => {
    const newestID = snapshots[0]?.id || '';
    setToID(newestID);
    setFromID(selectMeaningfulBaseline(snapshots, minimumIntervalSeconds, newestID)?.id || '');
  };

  return (
    <div className="page-shell">
      <div className="page-titlebar">
        <div>
          <p className="eyebrow">Save intelligence</p>
          <h1>存档差异与事件追踪</h1>
          <p>先看总体变化，再按玩家、物品、帕鲁、公会和据点审计具体事件；原始字段保留在末尾供排查。</p>
        </div>
        <button type="button" className="pp-button" onClick={() => void history.refetch()} disabled={history.isFetching}>
          <RefreshCw className={history.isFetching ? 'animate-spin' : ''} size={15} />刷新
        </button>
      </div>

      {notice && <div className="pp-notice pp-notice--danger"><AlertTriangle size={16} />{notice}</div>}

      <section className="pp-card overflow-hidden p-0">
        <div className="flex flex-col gap-4 border-b border-slate-100 bg-slate-50/70 px-5 py-5 lg:flex-row lg:items-center lg:justify-between">
          <div className="min-w-0">
            <span className="eyebrow">当前存档源</span>
            <div className="mt-1 flex flex-wrap items-center gap-2">
              <h2 className="truncate text-base font-black text-slate-900">{history.data?.source.name || '等待索引'}</h2>
              <span className={`pp-badge ${snapshots.length >= 2 ? 'pp-badge--ok' : 'pp-badge--warn'}`}>
                {snapshots.length >= 2 ? '可比较' : '需要两份快照'}
              </span>
            </div>
            <p className="mt-1 text-xs leading-5 text-slate-500">
              {snapshots.length} 份快照 · 最小采样间隔 {formatDuration(minimumIntervalSeconds)} · 占用 {formatBytes(history.data?.total_bytes || 0)} / {formatBytes(history.data?.max_total_bytes || 0)}
            </p>
          </div>
          <button type="button" className="pp-button" onClick={compareLatest} disabled={snapshots.length < 2}>
            <Clock3 size={15} />比较最近有效快照
          </button>
        </div>

        <div className="grid gap-4 p-5 xl:grid-cols-[minmax(0,1fr)_36px_minmax(0,1fr)]">
          <SnapshotSelect label="较早快照" value={fromID} snapshots={snapshots} onChange={setFromID} />
          <div className="hidden items-end justify-center pb-3 text-slate-300 xl:flex"><ArrowRight size={20} /></div>
          <SnapshotSelect label="较新快照" value={toID} snapshots={snapshots} onChange={setToID} />
        </div>
      </section>

      {!history.isLoading && snapshots.length < 2 && (
        <div className="pp-notice">自动索引会按采样间隔保留快照，避免几十秒内产生大量无意义记录。也可以在存档中心手动重建分析索引。</div>
      )}
      {fromID && toID && fromID === toID && <div className="pp-notice">请选择两份不同的快照。</div>}
      {fromID && toID && fromID !== toID && diff.isLoading && <div className="pp-notice">正在分析存档事件……</div>}

      {diff.data && fromID !== toID && (
        <>
          <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-6">
            {summaryCards.map((item) => <SummaryTile key={item.label} {...item} />)}
          </section>

          <section className="pp-card p-0 overflow-hidden">
            <div className="flex flex-col gap-3 border-b border-slate-100 px-5 py-4 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <h2 className="text-base font-black text-slate-900">变化明细</h2>
                <p className="mt-1 text-xs text-slate-500">
                  {snapshotShort(diff.data.from)} <ArrowRight className="inline" size={12} /> {snapshotShort(diff.data.to)} · 推断 {diff.data.event_total} 项事件
                </p>
              </div>
              <ExportButtons diff={diff.data} />
            </div>

            <div className="border-b border-slate-100 px-4 py-3">
              <div className="flex max-w-full gap-1 overflow-x-auto rounded-xl border border-slate-200 bg-slate-100 p-1" role="tablist" aria-label="存档变化分类">
                {categories.map((item) => (
                  <button
                    key={item.value}
                    type="button"
                    role="tab"
                    aria-selected={category === item.value}
                    onClick={() => setCategory(item.value)}
                    className={`inline-flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-2 text-xs font-bold transition ${category === item.value ? 'bg-white text-sky-700 shadow-sm' : 'text-slate-500 hover:text-slate-800'}`}
                  >
                    {item.icon}{item.label}
                  </button>
                ))}
              </div>
              <label className="relative mt-3 block">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={16} />
                <input
                  type="search"
                  aria-label="搜索差异"
                  value={query}
                  onChange={(event: React.ChangeEvent<HTMLInputElement>) => setQuery(event.target.value)}
                  className="pp-input w-full pl-9"
                  placeholder="搜索玩家、装备、帕鲁、据点、名称或 ID"
                />
              </label>
            </div>

            {diff.isFetching && <div className="pp-notice m-4">正在刷新变化明细……</div>}
            {!diff.isFetching && events.length === 0 && <div className="empty-state">所选范围没有匹配的可读事件。</div>}
            {events.length > 0 && <EventLedger events={events} />}
          </section>

          {attentionEvents.length > 0 && (
            <section className="pp-card">
              <div className="pp-card-head">
                <div><h2>重点变化</h2><p>优先列出大额增减、实体消失、等级回退和据点拆除等需要复核的事件。</p></div>
                <ShieldCheck size={18} />
              </div>
              <div className="grid gap-2 lg:grid-cols-2">
                {attentionEvents.map((event) => <AttentionRow key={event.id} event={event} />)}
              </div>
            </section>
          )}

          <div className="rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">
            <div className="flex items-start gap-3"><ShieldCheck className="mt-0.5 shrink-0" size={18} /><div><strong>事件由两个快照推断</strong><p className="mt-1 text-xs leading-5 text-amber-800">可以确认前后状态，但无法恢复快照之间每一步的精确时间和原因。例如“获得帕鲁”可能来自捕获、孵化、交易或存档导入。</p></div></div>
          </div>

          <details className="pp-card group">
            <summary className="flex cursor-pointer list-none items-center justify-between gap-3">
              <div><h2 className="text-base font-black text-slate-900">原始字段差异</h2><p className="mt-1 text-xs text-slate-500">用于审计和排查；共 {diff.data.total} 项，最多显示 200 项。</p></div>
              <ChevronDown className="transition-transform group-open:rotate-180" size={18} />
            </summary>
            <div className="source-list mt-4 max-h-[34rem] overflow-y-auto">
              {(diff.data.items ?? []).map((change, index) => <ChangeRow key={`${change.category}-${change.kind}-${change.id}-${index}`} change={change} />)}
            </div>
          </details>
        </>
      )}
    </div>
  );
};

const SnapshotSelect: React.FC<{
  label: string;
  value: string;
  snapshots: SaveHistorySnapshot[];
  onChange: (value: string) => void;
}> = ({ label, value, snapshots, onChange }) => (
  <label className="field-label">{label}
    <select aria-label={label} value={value} onChange={(event: React.ChangeEvent<HTMLSelectElement>) => onChange(event.target.value)}>
      <option value="">选择快照</option>
      {snapshots.map((item) => <option key={item.id} value={item.id}>{snapshotLabel(item)}</option>)}
    </select>
  </label>
);

const SummaryTile: React.FC<{ label: string; value: number; detail: string; tone: string; icon: React.ReactNode }> = ({ label, value, detail, tone, icon }) => (
  <article className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm">
    <div className="flex items-center justify-between gap-3">
      <span className="text-[10px] font-black uppercase tracking-wider text-slate-400">{label}</span>
      <span className={`grid h-8 w-8 place-items-center rounded-xl ${tone}`}>{icon}</span>
    </div>
    <strong className="mt-3 block text-2xl font-black text-slate-900">{value}</strong>
    <span className="mt-1 block text-[10px] font-semibold text-slate-500">{detail}</span>
  </article>
);

const EventLedger: React.FC<{ events: SaveHistoryEvent[] }> = ({ events }) => (
  <div className="max-h-[48rem] overflow-auto">
    <div className="hidden min-w-[920px] md:block">
      <table className="pp-table">
        <thead className="sticky top-0 z-10">
          <tr><th>类型</th><th>主体</th><th>具体变化</th><th>归属 / 目标</th><th>证据</th></tr>
        </thead>
        <tbody>{events.map((event) => <EventTableRow key={event.id} event={event} />)}</tbody>
      </table>
    </div>
    <div className="divide-y divide-slate-100 md:hidden">
      {events.map((event) => <EventMobileRow key={event.id} event={event} />)}
    </div>
  </div>
);

const EventTableRow: React.FC<{ event: SaveHistoryEvent }> = ({ event }) => {
  const presentation = describeEvent(event);
  return (
    <tr>
      <td><EventBadge event={event} /></td>
      <td>
        <div className="pp-cellmain max-w-[220px]"><strong className="truncate" title={presentation.title}>{presentation.title}</strong><span className="truncate" title={event.actor_id || event.subject_id}>{compactID(event.actor_id || event.subject_id)}</span></div>
      </td>
      <td><p className="max-w-[360px] text-xs leading-5 text-slate-700">{presentation.description}</p></td>
      <td><EntityTarget event={event} /></td>
      <td><EventEvidence event={event} /></td>
    </tr>
  );
};

const EventMobileRow: React.FC<{ event: SaveHistoryEvent }> = ({ event }) => {
  const presentation = describeEvent(event);
  return (
    <article className="p-4">
      <div className="flex items-start gap-3">
        <span className={`grid h-9 w-9 shrink-0 place-items-center rounded-xl ${presentation.iconTone}`}>{presentation.icon}</span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-start justify-between gap-2"><strong className="text-sm font-black text-slate-900">{presentation.title}</strong><EventBadge event={event} /></div>
          <p className="mt-1 text-xs leading-5 text-slate-600">{presentation.description}</p>
          <div className="mt-2"><EntityTarget event={event} /></div>
          {(event.details ?? []).length > 0 && <div className="mt-2"><EventEvidence event={event} /></div>}
        </div>
      </div>
    </article>
  );
};

const EventBadge: React.FC<{ event: SaveHistoryEvent }> = ({ event }) => {
  const tone = event.kind.includes('removed') || event.kind.includes('lost') || event.kind.includes('down')
    ? 'pp-badge--danger'
    : event.kind.includes('added') || event.kind.includes('gained') || event.kind.includes('up') || event.kind.includes('acquired')
      ? 'pp-badge--ok'
      : 'pp-badge--info';
  return <span className={`pp-badge ${tone}`}>{categoryLabels[event.category] || event.category}</span>;
};

const EntityTarget: React.FC<{ event: SaveHistoryEvent }> = ({ event }) => {
  const target = event.target_label || event.target_id;
  const actor = event.actor_label || event.actor_id;
  if (!target && !actor) return <span className="text-xs text-slate-400">—</span>;
  return <div className="pp-cellmain max-w-[220px]"><strong className="truncate">{target || actor}</strong><span className="truncate" title={event.target_id || event.actor_id}>{compactID(event.target_id || event.actor_id)}</span></div>;
};

const EventEvidence: React.FC<{ event: SaveHistoryEvent }> = ({ event }) => {
  const details = event.details ?? [];
  if (details.length === 0) return <span className="text-xs text-slate-400">状态推断</span>;
  return (
    <details className="group/evidence">
      <summary className="cursor-pointer list-none text-xs font-bold text-sky-700">{details.length} 个字段 <ChevronDown className="inline transition-transform group-open/evidence:rotate-180" size={12} /></summary>
      <div className="mt-2 grid min-w-[220px] gap-1.5">
        {details.map((detail) => (
          <div key={`${event.id}-${detail.field}`} className="rounded-lg border border-slate-100 bg-slate-50 px-2 py-1.5 text-[10px] text-slate-600">
            <b>{fieldLabels[detail.field] || detail.field}</b>：<s className="text-rose-600">{detail.before || '—'}</s> <ArrowRight className="inline" size={10} /> <span className="font-bold text-emerald-700">{detail.after || '—'}</span>
          </div>
        ))}
      </div>
    </details>
  );
};

const AttentionRow: React.FC<{ event: SaveHistoryEvent }> = ({ event }) => {
  const presentation = describeEvent(event);
  return (
    <article className="flex items-start gap-3 rounded-xl border border-amber-100 bg-amber-50/60 px-3 py-3">
      <span className="mt-0.5 text-amber-700"><AlertTriangle size={16} /></span>
      <div className="min-w-0"><strong className="block truncate text-xs text-slate-800">{presentation.title}</strong><p className="mt-1 text-[11px] leading-5 text-slate-600">{presentation.description}</p></div>
    </article>
  );
};

const ExportButtons: React.FC<{ diff: SaveHistoryDiff }> = ({ diff }) => (
  <div className="flex gap-2 overflow-x-auto">
    <button type="button" className="pp-button" onClick={() => exportDiff(diff, 'json')}><Download size={14} />JSON</button>
    <button type="button" className="pp-button" onClick={() => exportDiff(diff, 'csv')}><Download size={14} />CSV</button>
    <button type="button" className="pp-button" onClick={() => exportDiff(diff, 'md')}><Download size={14} />Markdown</button>
  </div>
);

const describeEvent = (event: SaveHistoryEvent): { title: string; description: string; icon: React.ReactNode; iconTone: string } => {
  const actor = event.actor_label || event.actor_id || '未知对象';
  const subject = event.subject_label || event.subject_id || '未知对象';
  const target = event.target_label || event.target_id || '未知目标';
  const quantity = Math.abs(event.delta || 0);
  const level = event.metadata?.level ? `Lv.${event.metadata.level}` : '';
  switch (event.kind) {
    case 'player_joined': return { title: `${actor} 首次出现在存档`, description: `角色已进入当前世界${event.details[0]?.after ? `，等级 ${event.details[0].after}` : ''}。`, icon: <UserRound size={17} />, iconTone: 'bg-sky-50 text-sky-700' };
    case 'player_missing': return { title: `${actor} 不再出现在新快照`, description: '角色可能被删除、迁移或当前解析结果缺失。', icon: <UserRound size={17} />, iconTone: 'bg-rose-50 text-rose-700' };
    case 'player_level_up': return { title: `${actor} 升级了`, description: `等级 ${event.before || '—'} → ${event.after || '—'}${event.delta ? `（+${event.delta}）` : ''}。`, icon: <TrendingUp size={17} />, iconTone: 'bg-emerald-50 text-emerald-700' };
    case 'player_level_down': return { title: `${actor} 等级下降`, description: `等级 ${event.before || '—'} → ${event.after || '—'}。可能由回档、迁移或管理操作造成。`, icon: <TrendingDown size={17} />, iconTone: 'bg-amber-50 text-amber-700' };
    case 'player_joined_guild': return { title: `${actor} 加入公会`, description: `加入 ${event.after || subject}。`, icon: <UsersRound size={17} />, iconTone: 'bg-violet-50 text-violet-700' };
    case 'player_left_guild': return { title: `${actor} 离开公会`, description: `离开 ${event.before || '原公会'}。`, icon: <UsersRound size={17} />, iconTone: 'bg-amber-50 text-amber-700' };
    case 'player_guild_changed': return { title: `${actor} 更换公会`, description: `${event.before || '无'} → ${event.after || '无'}。`, icon: <UsersRound size={17} />, iconTone: 'bg-violet-50 text-violet-700' };
    case 'player_renamed': return { title: '玩家改名', description: `${event.before || '—'} → ${event.after || actor}。`, icon: <UserRound size={17} />, iconTone: 'bg-sky-50 text-sky-700' };
    case 'item_gained': return { title: `${actor} ${event.metadata?.equipment === 'true' ? '获得装备' : '获得物品'}`, description: `${subject} ×${quantity}，库存 ${event.before || '0'} → ${event.after || '0'}。`, icon: <PackagePlus size={17} />, iconTone: 'bg-emerald-50 text-emerald-700' };
    case 'item_lost': return { title: `${actor} ${event.metadata?.equipment === 'true' ? '失去装备' : '消耗或转出物品'}`, description: `${subject} ×${quantity}，库存 ${event.before || '0'} → ${event.after || '0'}。`, icon: <PackageMinus size={17} />, iconTone: 'bg-rose-50 text-rose-700' };
    case 'pal_acquired': return { title: `${actor} 获得帕鲁`, description: `${level ? `${level} ` : ''}${subject}。可能由捕获、孵化、交易或导入产生。`, icon: <PawPrint size={17} />, iconTone: 'bg-lime-50 text-lime-700' };
    case 'pal_lost': return { title: `${actor} 失去帕鲁`, description: `${level ? `${level} ` : ''}${subject} 不再出现在新快照，可能被放生、交易、迁移或回档。`, icon: <PawPrint size={17} />, iconTone: 'bg-rose-50 text-rose-700' };
    case 'pal_transferred': return { title: `${subject} 更换归属`, description: `${actor} → ${target}。`, icon: <PawPrint size={17} />, iconTone: 'bg-violet-50 text-violet-700' };
    case 'pal_progressed': return { title: `${subject} 成长了`, description: `${actor ? `${actor} 的 ` : ''}${subject}：等级 ${event.before || '—'} → ${event.after || '—'}。`, icon: <TrendingUp size={17} />, iconTone: 'bg-lime-50 text-lime-700' };
    case 'pal_assignment_changed': return { title: `${subject} 的位置或任务变化`, description: `${actor ? `${actor} · ` : ''}${event.before || '未知位置'} → ${event.after || '未知位置'}。`, icon: <PawPrint size={17} />, iconTone: 'bg-cyan-50 text-cyan-700' };
    case 'pal_passives_changed': return { title: `${subject} 的被动词条变化`, description: `${event.before || '无'} → ${event.after || '无'}。`, icon: <PawPrint size={17} />, iconTone: 'bg-cyan-50 text-cyan-700' };
    case 'base_created': return { title: `新建据点：${actor}`, description: `初始建筑 ${event.details.find((item) => item.field === 'structures')?.after || '0'}，工作帕鲁 ${event.details.find((item) => item.field === 'workers')?.after || '0'}。`, icon: <Building2 size={17} />, iconTone: 'bg-amber-50 text-amber-700' };
    case 'base_removed': return { title: `据点消失：${actor}`, description: '据点可能被拆除、迁移、回档或从当前存档源移除。', icon: <Building2 size={17} />, iconTone: 'bg-rose-50 text-rose-700' };
    case 'base_structures_added': return { title: `${actor} 扩建`, description: `新增 ${quantity} 个建筑，建筑总数 ${event.before || '0'} → ${event.after || '0'}。`, icon: <Building2 size={17} />, iconTone: 'bg-emerald-50 text-emerald-700' };
    case 'base_structures_removed': return { title: `${actor} 建筑减少`, description: `减少 ${quantity} 个建筑，建筑总数 ${event.before || '0'} → ${event.after || '0'}。`, icon: <Building2 size={17} />, iconTone: 'bg-rose-50 text-rose-700' };
    case 'base_worker_assigned': return { title: `${subject} 被分配到 ${actor}`, description: `${level ? `${level} · ` : ''}新增为据点工作帕鲁。`, icon: <PawPrint size={17} />, iconTone: 'bg-cyan-50 text-cyan-700' };
    case 'base_worker_removed': return { title: `${subject} 离开 ${actor}`, description: '不再属于该据点的工作帕鲁列表。', icon: <PawPrint size={17} />, iconTone: 'bg-amber-50 text-amber-700' };
    case 'base_storage_changed': return { title: `${actor} 的存储结构变化`, description: `容器数量 ${event.before || '0'} → ${event.after || '0'}。`, icon: <Boxes size={17} />, iconTone: 'bg-sky-50 text-sky-700' };
    case 'base_status_changed': return { title: `${actor} 状态变化`, description: `${event.before || '—'} → ${event.after || '—'}。`, icon: <Building2 size={17} />, iconTone: 'bg-amber-50 text-amber-700' };
    case 'base_moved': return { title: `${actor} 的坐标变化`, description: `${event.before || '—'} → ${event.after || '—'}。`, icon: <Building2 size={17} />, iconTone: 'bg-violet-50 text-violet-700' };
    case 'guild_created': return { title: `新公会：${actor}`, description: '该公会首次出现在新快照。', icon: <UsersRound size={17} />, iconTone: 'bg-violet-50 text-violet-700' };
    case 'guild_removed': return { title: `公会消失：${actor}`, description: '该公会不再出现在新快照。', icon: <UsersRound size={17} />, iconTone: 'bg-rose-50 text-rose-700' };
    case 'guild_member_joined': return { title: `${subject} 加入 ${actor}`, description: '公会成员列表新增该玩家。', icon: <UsersRound size={17} />, iconTone: 'bg-violet-50 text-violet-700' };
    case 'guild_member_left': return { title: `${subject} 离开 ${actor}`, description: '公会成员列表不再包含该玩家。', icon: <UsersRound size={17} />, iconTone: 'bg-amber-50 text-amber-700' };
    case 'guild_base_added': return { title: `${actor} 新增据点`, description: `据点 ID：${subject}。`, icon: <Building2 size={17} />, iconTone: 'bg-amber-50 text-amber-700' };
    case 'guild_base_removed': return { title: `${actor} 失去据点`, description: `据点 ID：${subject}。`, icon: <Building2 size={17} />, iconTone: 'bg-rose-50 text-rose-700' };
    case 'guild_owner_changed': return { title: `${actor} 更换会长`, description: `${event.before || '—'} → ${event.after || subject || '—'}。`, icon: <UsersRound size={17} />, iconTone: 'bg-violet-50 text-violet-700' };
    default: return { title: `${actor}：${event.kind}`, description: [subject, event.before && `${event.before} → ${event.after}`].filter(Boolean).join(' · '), icon: <FileDiff size={17} />, iconTone: 'bg-slate-100 text-slate-600' };
  }
};

const ChangeRow: React.FC<{ change: SaveHistoryChange }> = ({ change }) => (
  <article className="source-row">
    <span className={`state-pill ${change.kind === 'added' || change.kind === 'increased' ? 'ok' : change.kind === 'removed' || change.kind === 'decreased' ? 'warn' : ''}`}>
      {kindLabels[change.kind] || change.kind}
    </span>
    <div className="source-copy">
      <strong>{change.label || compactID(change.id)}</strong>
      <span>{categoryLabels[change.category] || change.category} · {compactID(change.id)}{typeof change.delta === 'number' ? ` · Δ ${change.delta > 0 ? '+' : ''}${change.delta}` : ''}</span>
      {(change.fields ?? []).map((field) => (
        <span key={`${change.id}-${field.field}`}><b>{fieldLabels[field.field] || field.field}</b>：{field.before || '—'} <ArrowRight size={12} /> {field.after || '—'}</span>
      ))}
    </div>
  </article>
);

const summarize = (summary?: SaveHistoryDiffSummary) => {
  if (!summary) return [];
  return [
    { label: '玩家', value: summary.players_added + summary.players_removed + summary.players_changed, detail: `+${summary.players_added} / -${summary.players_removed} / 变更 ${summary.players_changed}`, tone: 'bg-sky-50 text-sky-700', icon: <UserRound size={16} /> },
    { label: '公会', value: summary.guilds_added + summary.guilds_removed + summary.guilds_changed, detail: `+${summary.guilds_added} / -${summary.guilds_removed} / 变更 ${summary.guilds_changed}`, tone: 'bg-violet-50 text-violet-700', icon: <UsersRound size={16} /> },
    { label: '据点', value: summary.bases_added + summary.bases_removed + summary.bases_changed, detail: `+${summary.bases_added} / -${summary.bases_removed} / 变更 ${summary.bases_changed}`, tone: 'bg-amber-50 text-amber-700', icon: <Building2 size={16} /> },
    { label: '帕鲁', value: summary.pals_added + summary.pals_removed + summary.pals_changed, detail: `+${summary.pals_added} / -${summary.pals_removed} / 变更 ${summary.pals_changed}`, tone: 'bg-lime-50 text-lime-700', icon: <PawPrint size={16} /> },
    { label: '容器', value: summary.containers_added + summary.containers_removed + summary.containers_changed, detail: `+${summary.containers_added} / -${summary.containers_removed} / 变更 ${summary.containers_changed}`, tone: 'bg-cyan-50 text-cyan-700', icon: <Boxes size={16} /> },
    { label: '物品', value: summary.items_increased + summary.items_decreased, detail: `增加 ${summary.items_increased} / 减少 ${summary.items_decreased}`, tone: 'bg-emerald-50 text-emerald-700', icon: <PackagePlus size={16} /> },
  ];
};

const selectAttentionEvents = (events: SaveHistoryEvent[]) => events
  .filter((event) => {
    const delta = Math.abs(event.delta || 0);
    return delta >= 20
      || event.kind.includes('removed')
      || event.kind.includes('missing')
      || event.kind.includes('lost')
      || event.kind.includes('down');
  })
  .sort((left, right) => Math.abs(right.delta || 0) - Math.abs(left.delta || 0))
  .slice(0, 6);

const snapshotTimestamp = (item: SaveHistorySnapshot): number | null => {
  for (const value of [item.captured_at, item.generated_at]) {
    const parsed = value ? new Date(value).getTime() : Number.NaN;
    if (Number.isFinite(parsed)) return parsed;
  }
  return null;
};

const selectMeaningfulBaseline = (
  items: SaveHistorySnapshot[],
  minimumIntervalSeconds: number,
  newestID = items[0]?.id || '',
): SaveHistorySnapshot | undefined => {
  const newest = items.find((item) => item.id === newestID) || items[0];
  if (!newest) return undefined;
  const candidates = items.filter((item) => item.id !== newest.id);
  if (candidates.length === 0) return undefined;
  const newestAt = snapshotTimestamp(newest);
  const minimumGap = Math.max(0, minimumIntervalSeconds) * 1000;
  if (newestAt != null && minimumGap > 0) {
    const spaced = candidates.find((item) => {
      const itemAt = snapshotTimestamp(item);
      return itemAt != null && newestAt - itemAt >= minimumGap;
    });
    if (spaced) return spaced;
  }
  return candidates[candidates.length - 1] || candidates[0];
};

const snapshotTime = (item: SaveHistorySnapshot) => {
  const value = item.generated_at || item.captured_at;
  const parsed = value ? new Date(value) : null;
  return parsed && Number.isFinite(parsed.getTime()) ? parsed.toLocaleString('zh-CN') : '时间未知';
};
const snapshotLabel = (item: SaveHistorySnapshot) => `${snapshotTime(item)} · ${item.counts?.players ?? 0} 玩家 · ${(item.fingerprint || item.id || 'unknown').slice(0, 8)}`;
const snapshotShort = snapshotTime;
const formatBytes = (value: number) => value >= 1024 * 1024 ? `${(value / 1024 / 1024).toFixed(1)} MiB` : value >= 1024 ? `${(value / 1024).toFixed(1)} KiB` : `${value} B`;
const formatDuration = (seconds: number) => seconds >= 3600 ? `${Math.round(seconds / 3600)} 小时` : `${Math.max(1, Math.round(seconds / 60))} 分钟`;
const compactID = (value: string) => value.length > 26 ? `${value.slice(0, 12)}…${value.slice(-8)}` : value || '—';

const exportDiff = (diff: SaveHistoryDiff, format: 'json' | 'csv' | 'md') => {
  const rows = diff.events.map((event) => {
    const presentation = describeEvent(event);
    return { category: categoryLabels[event.category] || event.category, type: event.kind, title: presentation.title, description: presentation.description, actor: event.actor_label || event.actor_id, subject: event.subject_label || event.subject_id, target: event.target_label || event.target_id };
  });
  const baseName = `save-diff-${diff.from.id.slice(0, 15)}-${diff.to.id.slice(0, 15)}`;
  if (format === 'json') {
    downloadText(`${baseName}.json`, JSON.stringify(diff, null, 2), 'application/json;charset=utf-8');
    return;
  }
  if (format === 'csv') {
    const header = ['分类', '事件类型', '标题', '说明', '主体', '对象', '目标'];
    const body = rows.map((row) => [row.category, row.type, row.title, row.description, row.actor, row.subject, row.target].map(csvCell).join(','));
    downloadText(`${baseName}.csv`, `\uFEFF${[header.join(','), ...body].join('\n')}`, 'text/csv;charset=utf-8');
    return;
  }
  const markdown = [
    '# 存档差异报告',
    '',
    `- 较早快照：${snapshotShort(diff.from)}`,
    `- 较新快照：${snapshotShort(diff.to)}`,
    `- 推断事件：${diff.event_total}`,
    '',
    '| 分类 | 事件 | 说明 |',
    '| --- | --- | --- |',
    ...rows.map((row) => `| ${markdownCell(row.category)} | ${markdownCell(row.title)} | ${markdownCell(row.description)} |`),
  ].join('\n');
  downloadText(`${baseName}.md`, markdown, 'text/markdown;charset=utf-8');
};

const csvCell = (value: string) => `"${value.replaceAll('"', '""')}"`;
const markdownCell = (value: string) => value.replaceAll('|', '\\|').replaceAll('\n', ' ');
const downloadText = (filename: string, content: string, type: string) => {
  const url = URL.createObjectURL(new Blob([content], { type }));
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
};
