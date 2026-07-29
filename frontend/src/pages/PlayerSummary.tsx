import React, { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Activity, AlertTriangle, Clock3, Compass, Dna, MapPin, RefreshCw, Search, Users,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { palsApi } from '../api/pals';
import { playersApi } from '../api/players';
import { SaveDataTabs } from '../components/ui/SaveDataTabs';
import { estimatePlayerRegion } from '../lib/playerRegion';
import type { Pal, Player } from '../types';

const normalizeID = (value?: string | null): string => String(value || '').trim().toLowerCase();

const palIdentityKey = (pal: Pal): string => normalizeID(pal.instance_id || pal.id || pal.character_id);

type PlayerSnapshot = {
  player: Player;
  pals: Pal[];
  distinctSpecies: number;
  averagePalLevel: number;
  maxPalLevel: number;
  region: string;
  mapX: number;
  mapY: number;
};

const buildSnapshot = (player: Player, pals: Pal[]): PlayerSnapshot => {
  const location = estimatePlayerRegion(player.x, player.y);
  const levels = pals.map((pal) => Math.max(0, Number(pal.level) || 0));
  return {
    player,
    pals,
    distinctSpecies: new Set(pals.map((pal) => pal.character_id).filter(Boolean)).size,
    averagePalLevel: levels.length ? Math.round(levels.reduce((sum, level) => sum + level, 0) / levels.length) : 0,
    maxPalLevel: levels.length ? Math.max(...levels) : 0,
    region: location.name,
    mapX: location.map_x,
    mapY: location.map_y,
  };
};

export const PlayerSummary: React.FC = () => {
  const [search, setSearch] = useState('');
  const [onlyOnline, setOnlyOnline] = useState(false);
  const playersQuery = useQuery({
    queryKey: ['player-summary', 'players'],
    queryFn: () => playersApi.getPlayersList({ limit: 500, offset: 0 }),
    refetchInterval: 15000,
  });
  const palsQuery = useQuery({
    queryKey: ['player-summary', 'pals'],
    queryFn: () => palsApi.getPalsList({ limit: 500, offset: 0 }),
    refetchInterval: 60000,
  });

  const snapshots = useMemo(() => {
    const palsByOwner = new Map<string, Pal[]>();
    for (const pal of palsQuery.data?.items || []) {
      for (const owner of [pal.owner_player_uid, pal.owner_steam_id]) {
        const key = normalizeID(owner);
        if (!key) continue;
        const list = palsByOwner.get(key) || [];
        const palKey = palIdentityKey(pal);
        if (!palKey || !list.some((candidate) => palIdentityKey(candidate) === palKey)) list.push(pal);
        palsByOwner.set(key, list);
      }
    }
    return (playersQuery.data?.items || []).map((player) => {
      const owned = new Map<string, Pal>();
      for (const id of [player.player_uid, player.steam_id, player.id]) {
        for (const pal of palsByOwner.get(normalizeID(id)) || []) {
          const palKey = palIdentityKey(pal);
          if (palKey) owned.set(palKey, pal);
        }
      }
      return buildSnapshot(player, [...owned.values()]);
    });
  }, [playersQuery.data?.items, palsQuery.data?.items]);

  const visible = useMemo(() => {
    const query = search.trim().toLowerCase();
    return snapshots
      .filter((snapshot) => !onlyOnline || snapshot.player.is_online)
      .filter((snapshot) => !query || [
        snapshot.player.nickname,
        snapshot.player.player_uid,
        snapshot.player.steam_id,
        snapshot.player.guild_name,
        snapshot.region,
      ].some((value) => String(value || '').toLowerCase().includes(query)))
      .sort((left, right) => Number(right.player.is_online) - Number(left.player.is_online)
        || Number(right.player.level || 0) - Number(left.player.level || 0)
        || left.player.nickname.localeCompare(right.player.nickname, 'zh-CN'));
  }, [snapshots, search, onlyOnline]);

  const metrics = useMemo(() => ({
    players: snapshots.length,
    online: snapshots.filter((snapshot) => snapshot.player.is_online).length,
    pals: snapshots.reduce((sum, snapshot) => sum + snapshot.pals.length, 0),
    totalSeconds: snapshots.reduce((sum, snapshot) => sum + Math.max(0, Number(snapshot.player.total_seconds) || 0), 0),
  }), [snapshots]);

  const error = playersQuery.error || palsQuery.error;
  const truncated = (playersQuery.data?.summary.total || 0) > (playersQuery.data?.items.length || 0)
    || (palsQuery.data?.summary.total || 0) > (palsQuery.data?.items.length || 0);

  return (
    <div className="mx-auto flex w-full max-w-[1720px] flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <SaveDataTabs />
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <p className="text-[10px] font-black uppercase tracking-[0.18em] text-sky-600">Player archive summary</p>
          <h1 className="mt-1 text-2xl font-black text-slate-900">玩家存档概览</h1>
          <p className="mt-2 text-xs font-semibold text-slate-500">汇总等级、帕鲁持有情况、在线时长与中文区域定位；只读取现有存档索引。</p>
        </div>
        <button type="button" className="pp-button" disabled={playersQuery.isFetching || palsQuery.isFetching} onClick={() => { void playersQuery.refetch(); void palsQuery.refetch(); }}>
          <RefreshCw className={playersQuery.isFetching || palsQuery.isFetching ? 'animate-spin' : ''} size={15} />刷新
        </button>
      </div>

      <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Metric icon={<Users size={18} />} label="存档玩家" value={metrics.players} detail={`当前在线 ${metrics.online}`} />
        <Metric icon={<Dna size={18} />} label="已关联帕鲁" value={metrics.pals} detail="按玩家 UID / SteamID 关联" />
        <Metric icon={<Clock3 size={18} />} label="累计在线" value={formatDuration(metrics.totalSeconds)} detail="来自当前世界在线历史" />
        <Metric icon={<Compass size={18} />} label="区域覆盖" value={new Set(snapshots.map((snapshot) => snapshot.region)).size} detail="中文区域为坐标估算" />
      </section>

      {error && <div className="flex items-start gap-2 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-xs font-semibold text-rose-700"><AlertTriangle className="mt-0.5 shrink-0" size={15} />{getErrorMessage(error)}</div>}
      {truncated && <div className="flex items-start gap-2 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs font-semibold text-amber-800"><AlertTriangle className="mt-0.5 shrink-0" size={15} />当前索引返回数量超过 500，帕鲁汇总可能只覆盖前 500 条；玩家基础信息仍按已返回结果展示。</div>}
      {playersQuery.data?.status.warnings?.length ? <div className="flex items-start gap-2 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs font-semibold text-amber-800"><AlertTriangle className="mt-0.5 shrink-0" size={15} />{playersQuery.data.status.warnings.join('；')}</div> : null}

      <section className="rounded-3xl border border-slate-200 bg-white shadow-sm">
        <div className="flex flex-col gap-3 border-b border-slate-200 bg-slate-50/70 p-4 lg:flex-row lg:items-center lg:justify-between">
          <label className="relative min-w-0 flex-1">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} />
            <input className="pp-input w-full pl-9" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索玩家、UID、公会或区域" />
          </label>
          <button type="button" onClick={() => setOnlyOnline((value) => !value)} className={`pp-button ${onlyOnline ? 'accent' : ''}`}><Activity size={14} />{onlyOnline ? '只看在线' : '显示全部'}</button>
        </div>

        <div className="grid gap-3 p-4 md:grid-cols-2 2xl:grid-cols-3">
          {visible.map((snapshot) => <PlayerCard key={snapshot.player.player_uid || snapshot.player.steam_id || snapshot.player.id} snapshot={snapshot} />)}
          {visible.length === 0 && <div className="col-span-full flex min-h-64 flex-col items-center justify-center gap-2 text-center text-slate-400"><Users size={34} /><strong className="text-sm text-slate-600">没有匹配玩家</strong><span className="text-xs">调整搜索条件或取消“只看在线”</span></div>}
        </div>
      </section>

      <p className="text-[11px] font-semibold leading-5 text-slate-400">中文区域根据 PalOps 世界坐标投影后匹配最近的大区域，仅用于快速定位，不代表游戏内精确地标。科技、配方、图鉴和首领进度尚未由当前 sav-cli 索引输出，因此本页不伪造这些数据。</p>
    </div>
  );
};

const PlayerCard: React.FC<{ snapshot: PlayerSnapshot }> = ({ snapshot }) => {
  const { player } = snapshot;
  const locationAvailable = Number.isFinite(player.x) && Number.isFinite(player.y) && (player.x !== 0 || player.y !== 0);
  return <article className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm">
    <div className="flex items-start gap-3 border-b border-slate-100 p-4">
      <span className={`relative flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl text-sm font-black ${player.is_online ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>
        {player.nickname.slice(0, 2).toUpperCase()}
        <span className={`absolute -right-1 -top-1 h-3 w-3 rounded-full border-2 border-white ${player.is_online ? 'bg-emerald-500' : 'bg-slate-300'}`} />
      </span>
      <span className="min-w-0 flex-1"><strong className="block truncate text-sm font-black text-slate-800">{player.nickname}</strong><small className="mt-1 block truncate font-mono text-[9px] text-slate-400">{player.player_uid || player.steam_id || player.id}</small><span className="mt-2 inline-flex rounded-full bg-sky-50 px-2 py-1 text-[10px] font-bold text-sky-700">Lv. {player.level || 0}</span></span>
      <span className="text-right"><small className="block text-[9px] font-bold uppercase tracking-wider text-slate-400">公会</small><strong className="mt-1 block max-w-28 truncate text-[11px] text-slate-600" title={player.guild_name}>{player.guild_name || '未加入'}</strong></span>
    </div>
    <div className="grid grid-cols-2 gap-px bg-slate-100">
      <CardMetric label="持有帕鲁" value={`${snapshot.pals.length} 只`} detail={`${snapshot.distinctSpecies} 个种类`} />
      <CardMetric label="帕鲁等级" value={snapshot.pals.length ? `平均 ${snapshot.averagePalLevel}` : '—'} detail={snapshot.pals.length ? `最高 ${snapshot.maxPalLevel}` : '暂无关联'} />
      <CardMetric label={player.is_online ? '本次在线' : '上次在线'} value={player.presence_available ? formatDuration(player.session_seconds || 0) : '—'} detail={player.presence_stale ? '在线来源暂时中断' : '会话时长'} />
      <CardMetric label="累计在线" value={player.presence_available ? formatDuration(player.total_seconds || 0) : '—'} detail={player.last_online_at ? `上线 ${formatTime(player.last_online_at)}` : '暂无历史'} />
    </div>
    <div className="flex items-start gap-3 p-4">
      <MapPin className="mt-0.5 shrink-0 text-rose-500" size={16} />
      <span className="min-w-0"><strong className="block text-xs text-slate-700">{snapshot.region}</strong><small className="mt-1 block font-mono text-[9px] text-slate-400">{locationAvailable ? `${player.x.toFixed(0)}, ${player.y.toFixed(0)}, ${player.z.toFixed(0)}` : '存档未记录有效坐标'}</small></span>
    </div>
  </article>;
};

const Metric: React.FC<{ icon: React.ReactNode; label: string; value: React.ReactNode; detail: string }> = ({ icon, label, value, detail }) => (
  <div className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm"><div className="flex items-center justify-between text-sky-600"><span className="text-[10px] font-black uppercase tracking-wider text-slate-400">{label}</span>{icon}</div><strong className="mt-3 block text-xl font-black text-slate-900">{value}</strong><span className="mt-1 block text-[10px] font-semibold text-slate-500">{detail}</span></div>
);

const CardMetric: React.FC<{ label: string; value: string; detail: string }> = ({ label, value, detail }) => (
  <div className="bg-slate-50/80 p-3"><span className="block text-[9px] font-black uppercase tracking-wider text-slate-400">{label}</span><strong className="mt-1 block truncate text-xs text-slate-700">{value}</strong><small className="mt-1 block truncate text-[9px] text-slate-400">{detail}</small></div>
);

const formatDuration = (seconds: number) => {
  const value = Math.max(0, Math.floor(Number(seconds) || 0));
  const days = Math.floor(value / 86400);
  const hours = Math.floor((value % 86400) / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  if (days > 0) return `${days}天 ${hours}小时`;
  if (hours > 0) return `${hours}小时 ${minutes}分`;
  return `${minutes}分钟`;
};

const formatTime = (value?: string) => {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
};
