import React, { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  AlertTriangle, Boxes, ChevronDown, ChevronUp, Clock3, Database, MapPin, Package, PackageSearch,
  RefreshCw, Search, TrendingUp, UserRound, Users, Warehouse, WifiOff,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import {
  inventoryApi, type GlobalInventoryItem, type GlobalInventoryLocation,
  type InventoryOwnerType, type InventorySort, type UnattendedInventoryState,
} from '../api/inventory';

export const Inventory: React.FC = () => {
  const [query, setQuery] = useState('');
  const [deferredQuery, setDeferredQuery] = useState('');
  const [category, setCategory] = useState('all');
  const [ownerType, setOwnerType] = useState<InventoryOwnerType>('all');
  const [sort, setSort] = useState<InventorySort>('count_desc');
  const [expanded, setExpanded] = useState<string | null>(null);

  useEffect(() => {
    const timer = window.setTimeout(() => setDeferredQuery(query.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [query]);

  const inventory = useQuery({
    queryKey: ['global-inventory', deferredQuery, category, ownerType, sort],
    queryFn: () => inventoryApi.list({ q: deferredQuery, category, owner_type: ownerType, sort, limit: 500 }),
    refetchInterval: 15000,
  });
  const data = inventory.data;
  const items = data?.items || [];
  const summary = data?.summary;
  const categories = useMemo(() => data?.filters.categories || [], [data?.filters.categories]);

  useEffect(() => {
    if (category !== 'all' && !categories.includes(category)) setCategory('all');
  }, [categories, category]);

  return (
    <div className="page-shell">
      <div className="page-titlebar">
        <div>
          <p className="eyebrow">Global inventory</p>
          <h1>库存管理</h1>
          <p>默认聚合玩家、据点和公会的可信库存；野外宝箱、掉落物和未归属容器仅在诊断范围中显示。所有数据均为只读。</p>
        </div>
        <button type="button" className="pp-button" disabled={inventory.isFetching} onClick={() => void inventory.refetch()}>
          <RefreshCw className={inventory.isFetching ? 'animate-spin' : ''} size={15} />刷新
        </button>
      </div>

      <section className="status-strip compact-status">
        <InventoryMetric label="物品种类" value={summary?.item_types || 0} />
        <InventoryMetric label="物品总量" value={summary?.total_count || 0} />
        <InventoryMetric label="有效容器" value={summary?.container_count || 0} />
        <InventoryMetric label="库存位置" value={summary?.location_count || 0} />
        <InventoryMetric label="已隐藏容器" value={summary?.suppressed_containers || 0} warning={Boolean(summary?.suppressed_containers)} detail="全存档" />
      </section>

      {inventory.error && <div className="pp-notice pp-notice--danger"><AlertTriangle size={16} />{getErrorMessage(inventory.error)}</div>}
      {data?.status.warnings?.length ? <div className="pp-notice"><AlertTriangle size={16} />{data.status.warnings.join('；')}</div> : null}

      {Boolean(summary?.suppressed_containers) && summary && (
        <div className="pp-notice">
          <MapPin size={16} />
          {ownerType === 'unknown'
            ? `诊断视图正在显示 ${formatNumber(summary.suppressed_containers)} 个未归属或世界容器；这些数量不计入默认库存。`
            : `已从默认统计隐藏 ${formatNumber(summary.suppressed_containers)} 个世界或未归属容器、${formatNumber(summary.suppressed_item_types)} 种物品，共 ${formatNumber(summary.suppressed_total_count)} 件。`}
        </div>
      )}

      <UnattendedInventoryCard state={data?.unattended} loading={inventory.isLoading} />

      <section className="pp-card">
        <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_180px_180px_180px]">
          <label className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={16} />
            <input
              type="search"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              className="pp-input w-full pl-9"
              placeholder="搜索物品、玩家、据点或容器"
              aria-label="搜索全服库存"
            />
          </label>
          <select className="pp-input" value={ownerType} onChange={(event) => setOwnerType(event.target.value as InventoryOwnerType)} aria-label="库存归属">
            <option value="all">可信库存（默认）</option>
            <option value="base">据点仓储</option>
            <option value="player">玩家背包</option>
            <option value="guild">公会库存</option>
            <option value="unknown">诊断：隐藏容器</option>
          </select>
          <select className="pp-input" value={category} onChange={(event) => setCategory(event.target.value)} aria-label="物品分类">
            <option value="all">全部分类</option>
            {categories.map((value) => <option value={value} key={value}>{value}</option>)}
          </select>
          <select className="pp-input" value={sort} onChange={(event) => setSort(event.target.value as InventorySort)} aria-label="库存排序">
            <option value="count_desc">总量从高到低</option>
            <option value="name_asc">名称升序</option>
            <option value="name_desc">名称降序</option>
          </select>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-3 text-xs text-slate-500">
          <span><Database className="mr-1 inline" size={13} />数据源 {data?.source_id || '当前激活存档'}</span>
          <span>默认总量只包含可验证的玩家、据点和公会归属；分类依据物品内部 ID。</span>
        </div>
      </section>

      <section className="pp-card p-0 overflow-hidden">
        <div className="grid grid-cols-[minmax(0,1fr)_120px_120px_42px] gap-3 border-b border-slate-100 bg-slate-50 px-5 py-3 text-[11px] font-bold uppercase tracking-wider text-slate-400">
          <span>物品</span><span>分类</span><span className="text-right">总量</span><span />
        </div>
        {inventory.isLoading ? (
          <InventoryEmpty icon={<RefreshCw className="animate-spin" size={28} />} title="正在读取库存索引" detail="数据来自当前激活的只读存档源" />
        ) : items.length ? items.map((item) => {
          const open = expanded === item.item_id;
          return <InventoryRow key={item.item_id} item={item} open={open} onToggle={() => setExpanded(open ? null : item.item_id)} />;
        }) : (
          <InventoryEmpty icon={<PackageSearch size={30} />} title="没有匹配的库存" detail="调整搜索、归属或分类条件后重试" />
        )}
      </section>
    </div>
  );
};


const unattendedStatusCopy = (state?: UnattendedInventoryState) => {
  if (!state) return { title: '正在读取无人时段状态', detail: '状态随全服库存索引一同刷新。' };
  if (state.status === 'source_mismatch') return { title: '仅支持当前服务器存档', detail: '导入存档只用于浏览，不参与实时无人时段统计。' };
  if (state.status === 'world_unavailable') return { title: '当前世界不可用', detail: '无法解析正在运行的 WorldID，统计已暂停。' };
  if (state.status === 'state_unavailable' || state.status === 'unavailable') return { title: '统计状态不可用', detail: '暂时无法读取无人时段记录。' };
  if (state.status === 'waiting') return { title: '等待建立库存基线', detail: '当前没有玩家在线；存档索引可用后才开始计算有效时段。' };
  if (state.status === 'active') return {
    title: state.qualified ? '无人时段统计中' : '无人时段正在建立',
    detail: state.qualified ? '已达到 5 分钟门槛，后续库存净增加会持续更新。' : `还需 ${formatDuration(state.eligible_remaining_seconds)} 才保留本次记录。`,
  };
  if (state.status === 'completed') return { title: '最近一次完整无人时段', detail: '玩家重新上线后冻结结果，展示该时段的库存正向净变化。' };
  if (state.status === 'online') return { title: '当前有玩家在线', detail: state.additions.length ? '下方保留最近一次完整无人时段结果。' : '无人时段开始后达到 5 分钟才会保留记录。' };
  return { title: '等待无人时段', detail: '当服务器无人在线时自动建立全服库存基线。' };
};

const UnattendedInventoryCard: React.FC<{ state?: UnattendedInventoryState; loading: boolean }> = ({ state, loading }) => {
  const copy = unattendedStatusCopy(state);
  const additions = state?.additions?.slice(0, 8) || [];
  const active = state?.status === 'active' || state?.status === 'waiting';
  const completed = state?.status === 'completed' || (state?.status === 'online' && additions.length > 0);
  const unavailable = Boolean(state && !state.available && ['source_mismatch', 'world_unavailable', 'state_unavailable', 'unavailable'].includes(state.status));
  const Icon = unavailable ? WifiOff : active ? Clock3 : completed ? TrendingUp : Users;

  return <section className="pp-card overflow-hidden p-0">
    <div className="flex flex-col gap-4 border-b border-slate-100 bg-gradient-to-r from-sky-50/80 via-white to-emerald-50/60 px-5 py-5 lg:flex-row lg:items-center lg:justify-between">
      <div className="flex min-w-0 items-start gap-3">
        <span className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl border ${unavailable ? 'border-slate-200 bg-white text-slate-400' : 'border-sky-100 bg-white text-sky-600'}`}>
          {loading ? <RefreshCw className="animate-spin" size={20} /> : <Icon size={20} />}
        </span>
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-base font-bold text-slate-800">无人时段库存净增加</h2>
            {state?.qualified && <span className="rounded-full bg-emerald-100 px-2 py-1 text-[10px] font-bold text-emerald-700">已达到 5 分钟</span>}
            {state?.snapshot_stale && <span className="rounded-full bg-amber-100 px-2 py-1 text-[10px] font-bold text-amber-700">使用旧索引</span>}
            {state?.presence_stale && <span className="rounded-full bg-amber-100 px-2 py-1 text-[10px] font-bold text-amber-700">在线状态中断</span>}
          </div>
          <strong className="mt-1 block text-sm text-slate-700">{copy.title}</strong>
          <p className="mt-1 text-xs leading-5 text-slate-500">{copy.detail} 仅统计可信玩家、据点和公会容器；该数据表示库存正向净变化，不等同于据点生产量。</p>
        </div>
      </div>
      <div className="grid min-w-[280px] grid-cols-3 gap-2">
        <UnattendedMetric label="时长" value={formatDuration(state?.duration_seconds || 0)} />
        <UnattendedMetric label="净增加" value={`+${formatNumber(state?.total_added || 0)}`} />
        <UnattendedMetric label="世界" value={state?.world_id ? compactWorldID(state.world_id) : '—'} title={state?.world_id} />
      </div>
    </div>
    {additions.length ? <div className="grid gap-2 p-4 sm:grid-cols-2 xl:grid-cols-4">
      {additions.map((item) => <div key={item.item_id} className="flex items-center gap-3 rounded-xl border border-slate-100 bg-slate-50/60 px-3 py-3">
        <InventoryItemIcon icon={item.item_icon} name={item.item_name} />
        <span className="min-w-0 flex-1"><strong className="block truncate text-xs text-slate-700">{item.item_name}</strong><small className="block truncate text-[10px] text-slate-400">{item.category}</small></span>
        <b className="shrink-0 text-sm text-emerald-700">+{formatNumber(item.quantity)}</b>
      </div>)}
    </div> : <div className="flex items-center gap-2 px-5 py-4 text-xs text-slate-400">
      <Boxes size={16} />{active ? '库存基线建立后，这里会显示相对基线增加的物品。' : '暂无可显示的完整无人时段库存增量。'}
    </div>}
  </section>;
};

const UnattendedMetric: React.FC<{ label: string; value: string; title?: string }> = ({ label, value, title }) => (
  <div className="rounded-xl border border-white/80 bg-white/80 px-3 py-2 shadow-sm" title={title}>
    <span className="block text-[9px] font-bold uppercase tracking-wider text-slate-400">{label}</span>
    <strong className="mt-1 block truncate text-xs text-slate-700">{value}</strong>
  </div>
);

const InventoryRow: React.FC<{ item: GlobalInventoryItem; open: boolean; onToggle: () => void }> = ({ item, open, onToggle }) => (
  <article className="border-b border-slate-100 last:border-b-0">
    <button type="button" onClick={onToggle} aria-expanded={open} className="grid w-full grid-cols-[minmax(0,1fr)_120px_120px_42px] items-center gap-3 px-5 py-4 text-left hover:bg-slate-50">
      <span className="flex min-w-0 items-center gap-3">
        <InventoryItemIcon icon={item.item_icon} name={item.item_name} />
        <span className="min-w-0"><strong className="block truncate text-sm text-slate-800">{item.item_name}</strong><small className="block truncate font-mono text-[10px] text-slate-400">{item.item_id}</small></span>
      </span>
      <span><i className="rounded-full border border-slate-200 bg-slate-50 px-2 py-1 text-[10px] font-bold not-italic text-slate-600">{item.category}</i></span>
      <strong className="text-right text-sm text-sky-700">{formatNumber(item.total_count)}</strong>
      <span className="flex justify-end text-slate-400">{open ? <ChevronUp size={17} /> : <ChevronDown size={17} />}</span>
    </button>
    {open && <div className="border-t border-slate-100 bg-slate-50/70 px-5 py-4"><InventoryLocations locations={item.locations} /></div>}
  </article>
);

const InventoryLocations: React.FC<{ locations: GlobalInventoryLocation[] }> = ({ locations }) => (
  <div className="grid gap-2 lg:grid-cols-2">
    {locations.map((location) => <div key={`${location.container_id}:${location.slot}`} className="flex items-center gap-3 rounded-xl border border-slate-100 bg-white px-3 py-3">
      <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-slate-50 text-slate-500">
        {location.owner_type === 'base' ? <Warehouse size={17} /> : location.owner_type === 'player' ? <UserRound size={17} /> : location.owner_type === 'guild' ? <Users size={17} /> : <MapPin size={17} />}
      </span>
      <span className="min-w-0 flex-1">
        <strong className="block truncate text-xs text-slate-700">{location.owner_name}</strong>
        <small className="block truncate text-[10px] text-slate-400" title={location.container_id}>{location.container_name} · 槽位 {location.slot}{location.guild_name ? ` · ${location.guild_name}` : ''}</small>
      </span>
      <b className="shrink-0 text-xs text-sky-700">×{formatNumber(location.count)}</b>
    </div>)}
  </div>
);

const InventoryItemIcon: React.FC<{ icon: string; name: string }> = ({ icon, name }) => {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [icon]);
  if (!icon || failed) return <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-slate-100 bg-slate-50 text-slate-300"><Package size={18} /></span>;
  return <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-slate-100 bg-slate-50 p-1"><img src={`/assets/items/${encodeURIComponent(icon)}.webp`} alt={`${name}图标`} loading="lazy" className="h-full w-full object-contain" onError={() => setFailed(true)} /></span>;
};

const InventoryMetric: React.FC<{ label: string; value: number; warning?: boolean; detail?: string }> = ({ label, value, warning, detail = '当前筛选' }) => (
  <div className={warning ? 'metric text-amber-700' : 'metric'}><span className="eyebrow">{label}</span><strong>{formatNumber(value)}</strong><span>{detail}</span></div>
);

const InventoryEmpty: React.FC<{ icon: React.ReactNode; title: string; detail: string }> = ({ icon, title, detail }) => (
  <div className="flex min-h-56 flex-col items-center justify-center gap-2 p-8 text-center text-slate-400">{icon}<strong className="text-sm text-slate-600">{title}</strong><span className="text-xs">{detail}</span></div>
);

const formatNumber = (value: number) => new Intl.NumberFormat('zh-CN').format(Number(value) || 0);

const formatDuration = (seconds: number) => {
  const value = Math.max(0, Math.floor(Number(seconds) || 0));
  const hours = Math.floor(value / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  const remaining = value % 60;
  if (hours) return `${hours} 小时 ${minutes} 分`;
  if (minutes) return `${minutes} 分 ${remaining} 秒`;
  return `${remaining} 秒`;
};

const compactWorldID = (value: string) => value.length > 12 ? `${value.slice(0, 6)}…${value.slice(-4)}` : value;
