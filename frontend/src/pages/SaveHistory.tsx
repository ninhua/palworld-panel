import React, { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  ArrowRight, Boxes, Building2, ChevronDown, FileDiff, History, PackageMinus, PackagePlus,
  PawPrint, RefreshCw, Search, ShieldCheck, TrendingDown, TrendingUp, UserRound, UsersRound,
} from 'lucide-react';
import {
  saveHistoryApi,
  type SaveHistoryChange,
  type SaveHistoryDiffSummary,
  type SaveHistoryEvent,
  type SaveHistorySnapshot,
} from '../api/saveHistory';
import { getErrorMessage } from '../api/client';

const categories = [
  { value: 'all', label: '全部事件' },
  { value: 'players', label: '玩家' },
  { value: 'guilds', label: '公会' },
  { value: 'bases', label: '据点' },
  { value: 'pals', label: '帕鲁' },
  { value: 'items', label: '装备与物品' },
  { value: 'containers', label: '容器原始变化' },
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

const eventGroupOrder = ['players', 'items', 'pals', 'bases', 'guilds', 'containers'];

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
  const diffItems = diff.data?.items ?? [];
  const events = diff.data?.events ?? [];
  const eventGroups = useMemo(() => groupEvents(events), [events]);
  const summaryCards = useMemo(() => summarize(diff.data?.summary), [diff.data?.summary]);
  const notice = history.error ? getErrorMessage(history.error) : diff.error ? getErrorMessage(diff.error) : '';

  return (
    <div className="page-shell">
      <div className="page-titlebar">
        <div>
          <p className="eyebrow">Save activity</p>
          <h1>存档事件与差异</h1>
          <p>按有意义的时间间隔采样存档，并把前后状态解释成玩家、装备、帕鲁、公会和据点事件。</p>
        </div>
        <button type="button" className="pp-button" onClick={() => void history.refetch()} disabled={history.isFetching}>
          <RefreshCw className={history.isFetching ? 'animate-spin' : ''} size={15} />刷新
        </button>
      </div>

      <section className="status-strip compact-status">
        <div className="server-hero">
          <div>
            <span className="eyebrow">当前存档源</span>
            <h2>{history.data?.source.name || '等待索引'}</h2>
            <p>{snapshots.length} 份快照 · {formatBytes(history.data?.total_bytes || 0)} / {formatBytes(history.data?.max_total_bytes || 0)}</p>
          </div>
          <span className={`state-pill ${snapshots.length >= 2 ? 'ok' : 'warn'}`}>{snapshots.length >= 2 ? '可比较' : '需要两份快照'}</span>
        </div>
        <Metric label="采样间隔" value={Math.max(1, Math.round(minimumIntervalSeconds / 60))} suffix="分钟" />
        <Metric label="当前快照" value={snapshots.length} suffix="份" />
        <Metric label="推断事件" value={diff.data?.event_total || 0} suffix="项" />
      </section>

      {notice && <div className="pp-notice">{notice}</div>}
      {!history.isLoading && snapshots.length < 2 && (
        <div className="pp-notice">首次打开已记录当前成功索引。自动索引会按采样间隔保存快照，避免几十秒内生成大量无意义记录；也可以在存档中心手动重建分析索引。</div>
      )}

      <section className="pp-card">
        <div className="pp-card-head"><div><h2>比较范围</h2><p>默认选择与最新快照至少相隔一个采样周期的基线；旧的过密快照会自动去重压缩。搜索会同时匹配玩家、物品、帕鲁、据点和原始字段。</p></div><History size={18} /></div>
        <div className="content-grid two-column">
          <label className="field-label">较早快照
            <select aria-label="较早快照" value={fromID} onChange={(event) => setFromID(event.target.value)}>
              <option value="">选择快照</option>
              {snapshots.map((item) => <option key={item.id} value={item.id}>{snapshotLabel(item)}</option>)}
            </select>
          </label>
          <label className="field-label">较新快照
            <select aria-label="较新快照" value={toID} onChange={(event) => setToID(event.target.value)}>
              <option value="">选择快照</option>
              {snapshots.map((item) => <option key={item.id} value={item.id}>{snapshotLabel(item)}</option>)}
            </select>
          </label>
          <label className="field-label">事件类别
            <select aria-label="变化类别" value={category} onChange={(event) => setCategory(event.target.value)}>
              {categories.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
            </select>
          </label>
          <label className="field-label">搜索
            <span className="input-with-icon"><Search size={15} /><input aria-label="搜索差异" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="玩家、装备、帕鲁、据点或 ID" /></span>
          </label>
        </div>
      </section>

      {fromID && toID && fromID === toID && <div className="pp-notice">请选择两份不同的快照。</div>}
      {fromID && toID && fromID !== toID && diff.isLoading && <div className="pp-notice">正在分析存档事件……</div>}

      {diff.data && fromID !== toID && (
        <>
          <section className="status-strip compact-status">
            {summaryCards.map((item) => <Metric key={item.label} label={item.label} value={item.value} suffix="项" />)}
          </section>

          <div className="rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">
            <div className="flex items-start gap-3"><ShieldCheck className="mt-0.5 shrink-0" size={18} /><div><strong>事件由两个快照推断</strong><p className="mt-1 text-xs leading-5 text-amber-800">可以确定前后状态，但无法恢复快照之间每一步的精确时间和原因。例如“获得帕鲁”可能来自捕获、孵化、交易或存档导入。</p></div></div>
          </div>

          <section className="pp-card">
            <div className="pp-card-head">
              <div><h2>发生了什么</h2><p>{snapshotShort(diff.data.from)} <ArrowRight size={13} /> {snapshotShort(diff.data.to)} · 共推断 {diff.data.event_total} 项事件，当前显示 {events.length} 项。</p></div>
              <FileDiff size={18} />
            </div>
            {diff.isFetching && <div className="pp-notice">正在分析存档事件……</div>}
            {!diff.isFetching && events.length === 0 && <div className="empty-state">所选范围没有匹配的可读事件。</div>}
            <div className="space-y-5">
              {eventGroups.map((group) => (
                <section key={group.category}>
                  <div className="mb-2 flex items-center justify-between gap-3">
                    <h3 className="flex items-center gap-2 text-sm font-black text-slate-800">{eventGroupIcon(group.category)}{categoryLabels[group.category] || group.category}</h3>
                    <span className="state-pill">{group.events.length} 项</span>
                  </div>
                  <div className="grid gap-3 xl:grid-cols-2">
                    {group.events.map((event) => <EventCard key={event.id} event={event} />)}
                  </div>
                </section>
              ))}
            </div>
          </section>

          <details className="pp-card group">
            <summary className="flex cursor-pointer list-none items-center justify-between gap-3">
              <div><h2 className="text-base font-black text-slate-900">原始字段差异</h2><p className="mt-1 text-xs text-slate-500">用于审计和排查；共 {diff.data.total} 项，最多显示 200 项。</p></div>
              <ChevronDown className="transition-transform group-open:rotate-180" size={18} />
            </summary>
            <div className="source-list mt-4">
              {diffItems.map((change, index) => <ChangeRow key={`${change.category}-${change.kind}-${change.id}-${index}`} change={change} />)}
            </div>
          </details>
        </>
      )}
    </div>
  );
};

const EventCard: React.FC<{ event: SaveHistoryEvent }> = ({ event }) => {
  const presentation = describeEvent(event);
  return (
    <article className={`rounded-2xl border p-4 ${presentation.tone}`}>
      <div className="flex items-start gap-3">
        <span className="mt-0.5 grid h-9 w-9 shrink-0 place-items-center rounded-xl border border-current/15 bg-white/70">{presentation.icon}</span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <strong className="text-sm font-black text-slate-900">{presentation.title}</strong>
            <span className="rounded-full border border-current/15 bg-white/70 px-2 py-0.5 text-[10px] font-black uppercase tracking-wide">推断</span>
          </div>
          <p className="mt-1 text-sm leading-6 text-slate-700">{presentation.description}</p>
          {(event.details ?? []).length > 0 && (
            <div className="mt-3 grid gap-2 sm:grid-cols-2">
              {event.details.map((detail) => (
                <div key={`${event.id}-${detail.field}`} className="rounded-xl border border-slate-200 bg-white/75 px-3 py-2 text-xs text-slate-700">
                  <b>{fieldLabels[detail.field] || detail.field}</b><span className="mt-1 flex items-center gap-1.5"><s className="text-rose-600">{detail.before || '—'}</s><ArrowRight size={11} /><span className="font-bold text-emerald-700">{detail.after || '—'}</span></span>
                </div>
              ))}
            </div>
          )}
          <p className="mt-3 truncate text-[10px] font-semibold text-slate-400" title={event.id}>{event.id}</p>
        </div>
      </div>
    </article>
  );
};

const describeEvent = (event: SaveHistoryEvent): { title: string; description: string; tone: string; icon: React.ReactNode } => {
  const actor = event.actor_label || event.actor_id || '未知对象';
  const subject = event.subject_label || event.subject_id || '未知对象';
  const target = event.target_label || event.target_id || '未知目标';
  const quantity = Math.abs(event.delta || 0);
  const level = event.metadata?.level ? `Lv.${event.metadata.level}` : '';
  switch (event.kind) {
    case 'player_joined': return { title: `${actor} 首次出现在存档`, description: `角色已进入当前世界${event.details[0]?.after ? `，等级 ${event.details[0].after}` : ''}。`, tone: 'border-sky-200 bg-sky-50', icon: <UserRound size={18} /> };
    case 'player_missing': return { title: `${actor} 不再出现在新快照`, description: '角色可能被删除、迁移或当前解析结果缺失。', tone: 'border-rose-200 bg-rose-50', icon: <UserRound size={18} /> };
    case 'player_level_up': return { title: `${actor} 升级了`, description: `等级 ${event.before || '—'} → ${event.after || '—'}${event.delta ? `（+${event.delta}）` : ''}。`, tone: 'border-emerald-200 bg-emerald-50', icon: <TrendingUp size={18} /> };
    case 'player_level_down': return { title: `${actor} 等级下降`, description: `等级 ${event.before || '—'} → ${event.after || '—'}。可能由回档、迁移或管理操作造成。`, tone: 'border-amber-200 bg-amber-50', icon: <TrendingDown size={18} /> };
    case 'player_joined_guild': return { title: `${actor} 加入公会`, description: `加入 ${event.after || subject}。`, tone: 'border-violet-200 bg-violet-50', icon: <UsersRound size={18} /> };
    case 'player_left_guild': return { title: `${actor} 离开公会`, description: `离开 ${event.before || '原公会'}。`, tone: 'border-amber-200 bg-amber-50', icon: <UsersRound size={18} /> };
    case 'player_guild_changed': return { title: `${actor} 更换公会`, description: `${event.before || '无'} → ${event.after || '无'}。`, tone: 'border-violet-200 bg-violet-50', icon: <UsersRound size={18} /> };
    case 'player_renamed': return { title: `玩家改名`, description: `${event.before || '—'} → ${event.after || actor}。`, tone: 'border-sky-200 bg-sky-50', icon: <UserRound size={18} /> };
    case 'item_gained': return { title: `${actor} ${event.metadata?.equipment === 'true' ? '获得装备' : '获得物品'}`, description: `${subject} ×${quantity}，库存 ${event.before || '0'} → ${event.after || '0'}。`, tone: 'border-emerald-200 bg-emerald-50', icon: <PackagePlus size={18} /> };
    case 'item_lost': return { title: `${actor} ${event.metadata?.equipment === 'true' ? '失去装备' : '消耗或转出物品'}`, description: `${subject} ×${quantity}，库存 ${event.before || '0'} → ${event.after || '0'}。`, tone: 'border-rose-200 bg-rose-50', icon: <PackageMinus size={18} /> };
    case 'pal_acquired': return { title: `${actor} 获得帕鲁`, description: `${level ? `${level} ` : ''}${subject}。可能由捕获、孵化、交易或导入产生。`, tone: 'border-lime-200 bg-lime-50', icon: <PawPrint size={18} /> };
    case 'pal_lost': return { title: `${actor} 失去帕鲁`, description: `${level ? `${level} ` : ''}${subject} 不再出现在新快照，可能被放生、交易、迁移或回档。`, tone: 'border-rose-200 bg-rose-50', icon: <PawPrint size={18} /> };
    case 'pal_transferred': return { title: `${subject} 更换归属`, description: `${actor} → ${target}。`, tone: 'border-violet-200 bg-violet-50', icon: <PawPrint size={18} /> };
    case 'pal_progressed': return { title: `${subject} 成长了`, description: `${actor ? `${actor} 的 ` : ''}${subject}：等级 ${event.before || '—'} → ${event.after || '—'}。`, tone: 'border-lime-200 bg-lime-50', icon: <TrendingUp size={18} /> };
    case 'pal_assignment_changed': return { title: `${subject} 的位置或任务变化`, description: `${actor ? `${actor} · ` : ''}${event.before || '未知位置'} → ${event.after || '未知位置'}。`, tone: 'border-cyan-200 bg-cyan-50', icon: <PawPrint size={18} /> };
    case 'pal_passives_changed': return { title: `${subject} 的被动词条变化`, description: `${event.before || '无'} → ${event.after || '无'}。`, tone: 'border-cyan-200 bg-cyan-50', icon: <PawPrint size={18} /> };
    case 'base_created': return { title: `新建据点：${actor}`, description: `初始建筑 ${event.details.find((item) => item.field === 'structures')?.after || '0'}，工作帕鲁 ${event.details.find((item) => item.field === 'workers')?.after || '0'}。`, tone: 'border-amber-200 bg-amber-50', icon: <Building2 size={18} /> };
    case 'base_removed': return { title: `据点消失：${actor}`, description: '据点可能被拆除、迁移、回档或从当前存档源移除。', tone: 'border-rose-200 bg-rose-50', icon: <Building2 size={18} /> };
    case 'base_structures_added': return { title: `${actor} 扩建`, description: `新增 ${quantity} 个建筑，建筑总数 ${event.before || '0'} → ${event.after || '0'}。`, tone: 'border-emerald-200 bg-emerald-50', icon: <Building2 size={18} /> };
    case 'base_structures_removed': return { title: `${actor} 建筑减少`, description: `减少 ${quantity} 个建筑，建筑总数 ${event.before || '0'} → ${event.after || '0'}。`, tone: 'border-rose-200 bg-rose-50', icon: <Building2 size={18} /> };
    case 'base_worker_assigned': return { title: `${subject} 被分配到 ${actor}`, description: `${level ? `${level} · ` : ''}新增为据点工作帕鲁。`, tone: 'border-cyan-200 bg-cyan-50', icon: <PawPrint size={18} /> };
    case 'base_worker_removed': return { title: `${subject} 离开 ${actor}`, description: '不再属于该据点的工作帕鲁列表。', tone: 'border-amber-200 bg-amber-50', icon: <PawPrint size={18} /> };
    case 'base_storage_changed': return { title: `${actor} 的存储结构变化`, description: `容器数量 ${event.before || '0'} → ${event.after || '0'}。`, tone: 'border-sky-200 bg-sky-50', icon: <Boxes size={18} /> };
    case 'base_status_changed': return { title: `${actor} 状态变化`, description: `${event.before || '—'} → ${event.after || '—'}。`, tone: 'border-amber-200 bg-amber-50', icon: <Building2 size={18} /> };
    case 'base_moved': return { title: `${actor} 的坐标变化`, description: `${event.before || '—'} → ${event.after || '—'}。`, tone: 'border-violet-200 bg-violet-50', icon: <Building2 size={18} /> };
    case 'guild_created': return { title: `新公会：${actor}`, description: '该公会首次出现在新快照。', tone: 'border-violet-200 bg-violet-50', icon: <UsersRound size={18} /> };
    case 'guild_removed': return { title: `公会消失：${actor}`, description: '该公会不再出现在新快照。', tone: 'border-rose-200 bg-rose-50', icon: <UsersRound size={18} /> };
    case 'guild_member_joined': return { title: `${subject} 加入 ${actor}`, description: '公会成员列表新增该玩家。', tone: 'border-violet-200 bg-violet-50', icon: <UsersRound size={18} /> };
    case 'guild_member_left': return { title: `${subject} 离开 ${actor}`, description: '公会成员列表不再包含该玩家。', tone: 'border-amber-200 bg-amber-50', icon: <UsersRound size={18} /> };
    case 'guild_base_added': return { title: `${actor} 新增据点`, description: `据点 ID：${subject}。`, tone: 'border-amber-200 bg-amber-50', icon: <Building2 size={18} /> };
    case 'guild_base_removed': return { title: `${actor} 失去据点`, description: `据点 ID：${subject}。`, tone: 'border-rose-200 bg-rose-50', icon: <Building2 size={18} /> };
    case 'guild_owner_changed': return { title: `${actor} 更换会长`, description: `${event.before || '—'} → ${event.after || subject || '—'}。`, tone: 'border-violet-200 bg-violet-50', icon: <UsersRound size={18} /> };
    default: return { title: `${actor}：${event.kind}`, description: [subject, event.before && `${event.before} → ${event.after}`].filter(Boolean).join(' · '), tone: 'border-slate-200 bg-slate-50', icon: <FileDiff size={18} /> };
  }
};

const ChangeRow: React.FC<{ change: SaveHistoryChange }> = ({ change }) => (
  <article className="source-row">
    <span className={`state-pill ${change.kind === 'added' || change.kind === 'increased' ? 'ok' : change.kind === 'removed' || change.kind === 'decreased' ? 'warn' : ''}`}>
      {kindLabels[change.kind] || change.kind}
    </span>
    <div className="source-copy">
      <strong>{change.label || change.id}</strong>
      <span>{categoryLabels[change.category] || change.category} · {change.id}{typeof change.delta === 'number' ? ` · Δ ${change.delta > 0 ? '+' : ''}${change.delta}` : ''}</span>
      {(change.fields ?? []).map((field) => (
        <span key={`${change.id}-${field.field}`}><b>{fieldLabels[field.field] || field.field}</b>：{field.before || '—'} <ArrowRight size={12} /> {field.after || '—'}</span>
      ))}
    </div>
  </article>
);

const Metric: React.FC<{ label: string; value: number; suffix: string }> = ({ label, value, suffix }) => (
  <div className="metric"><span className="eyebrow">{label}</span><strong>{value}</strong><span>{suffix}</span></div>
);

const groupEvents = (events: SaveHistoryEvent[]) => eventGroupOrder
  .map((category) => ({ category, events: events.filter((event) => event.category === category) }))
  .filter((group) => group.events.length > 0);

const eventGroupIcon = (category: string) => {
  switch (category) {
    case 'players': return <UserRound size={16} />;
    case 'items': return <PackagePlus size={16} />;
    case 'pals': return <PawPrint size={16} />;
    case 'bases': return <Building2 size={16} />;
    case 'guilds': return <UsersRound size={16} />;
    default: return <FileDiff size={16} />;
  }
};

const summarize = (summary?: SaveHistoryDiffSummary) => {
  if (!summary) return [];
  return [
    { label: '玩家', value: summary.players_added + summary.players_removed + summary.players_changed },
    { label: '公会', value: summary.guilds_added + summary.guilds_removed + summary.guilds_changed },
    { label: '据点', value: summary.bases_added + summary.bases_removed + summary.bases_changed },
    { label: '帕鲁', value: summary.pals_added + summary.pals_removed + summary.pals_changed },
    { label: '容器', value: summary.containers_added + summary.containers_removed + summary.containers_changed },
    { label: '物品', value: summary.items_increased + summary.items_decreased },
  ];
};

const snapshotTimestamp = (item: SaveHistorySnapshot): number | null => {
  for (const value of [item.captured_at, item.generated_at]) {
    const parsed = value ? new Date(value).getTime() : Number.NaN;
    if (Number.isFinite(parsed)) return parsed;
  }
  return null;
};

export const selectMeaningfulBaseline = (
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
