import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import {
  AlertTriangle, BookOpen, Boxes, CheckCircle2, Database, Gamepad2, Gift, LoaderCircle, MessageSquareText,
  MapPin, RefreshCw, Search, ShieldAlert, Sword, UserCog, UserRound, Wifi, WifiOff,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { palDefenderGMApi } from '../api/paldefenderGM';
import { palsApi } from '../api/pals';
import { playersApi } from '../api/players';
import { saveIndexApi } from '../api/saveIndex';
import { useServerStore } from '../store/useServerStore';
import type { Pal, PalDefenderMessageRequest, PalDefenderTeleportRequest, Player } from '../types';
import { StatusBadge } from '../components/ui/StatusBadge';
import { SaveDataTabs } from '../components/ui/SaveDataTabs';
import { AccessWorkspace } from '../components/gm/AccessWorkspace';
import { ItemWorkspace } from '../components/gm/ItemWorkspace';
import { MessageWorkspace, type MessageMode } from '../components/gm/MessageWorkspace';
import { ModerationWorkspace } from '../components/gm/ModerationWorkspace';
import { PalWorkspace } from '../components/gm/PalWorkspace';
import { PlayerOverview, type PlayerOverviewModel } from '../components/gm/PlayerOverview';
import { ProgressionWorkspace } from '../components/gm/ProgressionWorkspace';
import { SaveInventoryPanel } from '../components/gm/SaveInventoryPanel';
import { TeleportDialog } from '../components/gm/TeleportDialog';

type WorkspaceTab = 'profile' | 'items' | 'progression' | 'pals' | 'message' | 'access';
type PlayerFilter = 'all' | 'online' | 'offline';

const emptySavePlayers: Player[] = [];

export const PlayerCenter: React.FC = () => {
  const queryClient = useQueryClient();
  const { session } = useServerStore();
  const canWrite = Boolean(session?.permissions.includes('players:write'));
  const canSecurityWrite = Boolean(session?.permissions.includes('security:write'));
  const canRebuildSaveIndex = Boolean(session?.permissions.includes('server:control'));
  const [search, setSearch] = useState('');
  const [playerFilter, setPlayerFilter] = useState<PlayerFilter>('all');
  const [selectedKey, setSelectedKey] = useState('');
  const [activeTab, setActiveTab] = useState<WorkspaceTab>('profile');
  const [messageMode, setMessageMode] = useState<MessageMode>('player');
  const [messageType, setMessageType] = useState<NonNullable<PalDefenderMessageRequest['SendType']>>('PlayerLogImportant');
  const [message, setMessage] = useState('');
  const [reason, setReason] = useState('');
  const [banIP, setBanIP] = useState(false);
  const [pending, setPending] = useState('');
  const [notice, setNotice] = useState('');
  const [actionError, setActionError] = useState('');
  const [teleportOpen, setTeleportOpen] = useState(false);
  const actionInFlight = useRef(false);

  const statusQuery = useQuery({ queryKey: ['paldefender-gm', 'status'], queryFn: palDefenderGMApi.status });
  const status = statusQuery.data;
  const savePlayersQuery = useQuery({
    queryKey: ['player-center', 'save-players'],
    queryFn: () => playersApi.getPlayersList({ limit: 5000 }, { source: 'server' }),
  });
  const players = savePlayersQuery.data?.items ?? emptySavePlayers;
  const playerSourcesReady = !savePlayersQuery.isLoading;

  useEffect(() => {
    if (!playerSourcesReady) return;
    if (players.length === 0) {
      setSelectedKey('');
      return;
    }
    if (!players.some((player) => player.id === selectedKey)) setSelectedKey(players[0].id);
  }, [playerSourcesReady, players, selectedKey]);

  const filteredPlayers = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return players.filter((player) => {
      if (playerFilter === 'online' && !player.is_online) return false;
      if (playerFilter === 'offline' && player.is_online) return false;
      if (!needle) return true;
      return [player.nickname, player.steam_id, player.player_uid, player.guild_name, player.gm_user_id]
        .some((value) => String(value || '').toLowerCase().includes(needle));
    });
  }, [playerFilter, players, search]);

  const selected = players.find((player) => player.id === selectedKey) ?? null;
  const gmIdentifier = selected?.gm_user_id || selected?.steam_id || selected?.player_uid || '';
  const saveIdentifier = selected?.player_uid || selected?.steam_id || '';
  const gmAvailable = Boolean(status?.available && selected?.gm_user_id && gmIdentifier);
  const liveReady = Boolean(gmAvailable && selected?.is_online);

  const gmPlayerQuery = useQuery({
    queryKey: ['paldefender-gm', 'player', gmIdentifier],
    queryFn: () => palDefenderGMApi.player(gmIdentifier),
    enabled: gmAvailable,
  });
  const saveDetailQuery = useQuery({
    queryKey: ['player-center', 'save-player', saveIdentifier],
    queryFn: () => playersApi.getPlayer(saveIdentifier, 'server'),
    enabled: Boolean(saveIdentifier),
  });
  const saveInventoryQuery = useQuery({
    queryKey: ['player-center', 'save-inventory', saveIdentifier],
    queryFn: () => playersApi.getInventory(saveIdentifier, 'server'),
    enabled: Boolean(saveIdentifier),
  });
  const savePalsQuery = useQuery({
    queryKey: ['player-center', 'save-pals', selected?.player_uid],
    queryFn: () => palsApi.getPalsList({ owner_player_uid: selected?.player_uid, limit: 5000 }),
    enabled: Boolean(selected?.player_uid),
  });
  const catalogQuery = useQuery({
    queryKey: ['paldefender-gm', 'item-catalog'],
    queryFn: () => palDefenderGMApi.items('', 5000),
    staleTime: 30 * 60 * 1000,
  });
  const liveInventoryQuery = useQuery({
    queryKey: ['paldefender-gm', 'inventory', gmIdentifier],
    queryFn: () => palDefenderGMApi.inventory(gmIdentifier),
    enabled: Boolean(liveReady && activeTab === 'items'),
  });
  const progressionQuery = useQuery({
    queryKey: ['paldefender-gm', 'progression', gmIdentifier],
    queryFn: () => palDefenderGMApi.progression(gmIdentifier),
    enabled: Boolean(liveReady && (activeTab === 'profile' || activeTab === 'progression')),
  });
  const techsQuery = useQuery({
    queryKey: ['paldefender-gm', 'techs', gmIdentifier],
    queryFn: () => palDefenderGMApi.techs(gmIdentifier),
    enabled: Boolean(liveReady && activeTab === 'progression'),
  });
  const rebuildSaveIndex = useMutation({
    mutationFn: saveIndexApi.rebuild,
    onSuccess: async () => {
      setNotice('存档索引已构建');
      setActionError('');
      await queryClient.invalidateQueries({ queryKey: ['player-center'] });
    },
    onError: (error) => setActionError(getErrorMessage(error)),
  });
  const localTechnologyCatalogQuery = useQuery({
    queryKey: ['paldefender-gm', 'catalog', 'technologies'],
    queryFn: () => palDefenderGMApi.localTechnologyCatalog('', 5000),
    enabled: activeTab === 'progression',
    staleTime: 30 * 60 * 1000,
  });
  const runtimeTechnologyCatalogQuery = useQuery({
    queryKey: ['paldefender-gm', 'catalog', 'technology-runtime'],
    queryFn: palDefenderGMApi.technologyCatalog,
    enabled: Boolean(status?.available && activeTab === 'progression'),
    retry: false,
    staleTime: 5 * 60 * 1000,
  });
  const palCatalogQuery = useQuery({
    queryKey: ['paldefender-gm', 'catalog', 'pals'],
    queryFn: () => palDefenderGMApi.palCatalog('', 5000),
    enabled: activeTab === 'pals',
    staleTime: 30 * 60 * 1000,
  });
  const passiveCatalogQuery = useQuery({
    queryKey: ['paldefender-gm', 'catalog', 'passives'],
    queryFn: () => palDefenderGMApi.passiveCatalog('', 5000),
    enabled: activeTab === 'pals',
    staleTime: 30 * 60 * 1000,
  });

  const selectedGM = gmPlayerQuery.data;
  const selectedSave = saveDetailQuery.data?.player ?? selected;
  const selectedOnline = Boolean(selected?.is_online);
  const selectedName = selected?.nickname || selectedSave?.nickname || '未知玩家';
  const overview: PlayerOverviewModel | null = selected ? {
    name: selectedName,
    identifier: gmIdentifier,
    playerUID: selected.player_uid || selectedGM?.PlayerUID || '',
    guildName: selected.guild_name,
    level: selected.level,
    online: selectedOnline,
    x: selected.x,
    y: selected.y,
    z: selected.z,
    lastOnline: selected.last_online_time,
    hasSaveData: Boolean(selectedSave),
    hasLiveData: selected.online_source !== 'none',
  } : null;

  const runAction = async (key: string, action: () => Promise<unknown>, success: string): Promise<boolean> => {
    if (actionInFlight.current) return false;
    actionInFlight.current = true;
    setPending(key);
    setNotice('');
    setActionError('');
    try {
      await action();
      if (success) setNotice(success);
      return true;
    } catch (error) {
      setActionError(getErrorMessage(error));
      return false;
    } finally {
      actionInFlight.current = false;
      setPending('');
    }
  };

  const giveItems = async (items: Array<{ ItemID: string; Count: number }>) => {
    if (!selected || !selectedOnline) return;
    if (!window.confirm(`向 ${selectedName} 发放 ${items.length} 种物品？`)) return;
    await runAction('give-items', async () => {
      const result = await palDefenderGMApi.giveItems(gmIdentifier, items);
      await queryClient.invalidateQueries({ queryKey: ['paldefender-gm', 'inventory', gmIdentifier] });
      setNotice(`已向 ${selectedName} 发放 ${result.Granted.Items} 件物品`);
    }, '');
  };

  const adjustItemTotal = async (itemID: string, currentCount: number, targetCount: number) => {
    if (!selected || !selectedOnline || targetCount === currentCount) return targetCount === currentCount;
    const delta = targetCount - currentCount;
    const verb = delta > 0 ? '增加' : '移除';
    if (!window.confirm(`为 ${selectedName} 调整 ${itemID}：${currentCount} → ${targetCount}（${verb} ${Math.abs(delta)}）？`)) return false;
    return runAction('adjust-item', async () => {
      if (delta > 0) await palDefenderGMApi.giveItems(gmIdentifier, [{ ItemID: itemID, Count: delta }]);
      else await palDefenderGMApi.removeItems(gmIdentifier, { Items: [{ ItemID: itemID, Count: Math.abs(delta) }] });
      await queryClient.invalidateQueries({ queryKey: ['paldefender-gm', 'inventory', gmIdentifier] });
    }, `已将 ${itemID} 的目标总量调整为 ${targetCount}`);
  };

  const teleportPlayer = async (request: PalDefenderTeleportRequest) => {
    if (!selectedOnline || !liveReady) return false;
    const destination = request.Mode === 'player' ? `玩家 ${request.TargetPlayer}` : `坐标 ${request.X}, ${request.Y}${request.Z == null ? '' : `, ${request.Z}`}`;
    if (!window.confirm(`将 ${selectedName} 传送到${destination}？`)) return false;
    return runAction('teleport', async () => {
      await palDefenderGMApi.teleport(gmIdentifier, request);
      await Promise.all([savePlayersQuery.refetch(), gmPlayerQuery.refetch()]);
    }, `已发送 ${selectedName} 的传送命令`);
  };

  const releasePal = async (pal: Pal) => {
    if (!pal.character_id || !liveReady) return false;
    const palID = pal.character_id;
    return runAction('release-pal', async () => {
      await palDefenderGMApi.releasePal(gmIdentifier, {
        PalID: palID,
        ...(pal.level > 0 ? { Level: pal.level } : {}),
        ...(pal.gender === 'male' || pal.gender === 'female' ? { Gender: pal.gender } : {}),
        ...(pal.rank != null ? { Rank: pal.rank } : {}),
      });
      await queryClient.invalidateQueries({ queryKey: ['paldefender-gm', 'pals', gmIdentifier] });
    }, `已提交 ${pal.nickname || pal.name} 的放生命令；存档快照可能暂未更新`);
  };

  const submitMessage = async () => {
    const text = message.trim();
    if (!text || (messageMode === 'player' && !selectedOnline)) return;
    await runAction('message', () => messageMode === 'player'
      ? palDefenderGMApi.sendMessage(gmIdentifier, { SendType: messageType, Message: text })
      : palDefenderGMApi.broadcast(text, messageMode === 'alert'), '消息已发送');
  };

  const moderate = async (action: 'kick' | 'ban' | 'unban') => {
    if (!selected) return;
    const labels = { kick: '踢出', ban: '封禁', unban: '解除封禁' };
    if (!window.confirm(`${labels[action]} ${selectedName} (${gmIdentifier})？`)) return;
    await runAction(action, () => {
      const request = { Reason: reason.trim(), IP: action === 'ban' ? banIP : undefined };
      if (action === 'kick') return palDefenderGMApi.kick(gmIdentifier, request);
      if (action === 'ban') return palDefenderGMApi.ban(gmIdentifier, request);
      return palDefenderGMApi.unban(gmIdentifier, request);
    }, `${labels[action]}操作已完成：${selectedName}`);
  };

  const readinessText = palDefenderReadiness(status);
  const queryError = savePlayersQuery.error || gmPlayerQuery.error || saveDetailQuery.error || saveInventoryQuery.error || savePalsQuery.error || catalogQuery.error;
  const visibleError = actionError || (queryError ? getErrorMessage(queryError) : '');
  const busy = Boolean(pending);
  const onlineStatusStale = players.some((player) => player.online_stale)
    || Boolean(savePlayersQuery.data?.status?.warnings?.some((warning) => warning.includes('online player REST data is stale or unavailable')));
  const teleportOptions = players
    .filter((player) => player.is_online && player.gm_user_id && player.gm_user_id !== gmIdentifier)
    .map((player) => ({ id: player.gm_user_id!, name: player.nickname }));

  return (
    <div className="mx-auto flex w-full max-w-[1720px] flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <SaveDataTabs />
      <header className="flex flex-col gap-4 border-b border-slate-200 pb-5 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <div className="flex items-center gap-2"><UserCog size={21} className="text-sky-500" /><h1 className="text-xl font-black text-slate-900">玩家中心</h1></div>
          <p className="mt-2 max-w-3xl text-xs font-semibold leading-5 text-slate-500">存档索引负责离线档案、帕鲁和背包快照；PalDefender 负责在线读取、发放与管理。先在左侧选玩家，再在右侧执行所有操作。</p>
          <div className="mt-3 flex flex-wrap items-center gap-2"><StatusBadge status={status?.available ? 'Online' : 'Offline'} customText={status?.available ? 'PalDefender REST 已连接' : readinessText} />{savePlayersQuery.data?.status && <span className={`rounded-full border px-2.5 py-1 text-[10px] font-bold ${savePlayersQuery.data.status.state === 'ready' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-amber-200 bg-amber-50 text-amber-700'}`}>存档索引：{savePlayersQuery.data.status.state}</span>}<span className="text-[10px] font-bold text-slate-400">{players.filter((player) => player.is_online).length} / {players.length} 在线</span></div>
        </div>
        <div className="flex flex-wrap gap-2">{savePlayersQuery.data?.status?.state === 'not_indexed' && canRebuildSaveIndex && <button type="button" onClick={() => rebuildSaveIndex.mutate()} disabled={rebuildSaveIndex.isPending} className="inline-flex items-center gap-2 rounded-xl bg-violet-600 px-4 py-2.5 text-xs font-bold text-white hover:bg-violet-700 disabled:opacity-40"><Database size={14} className={rebuildSaveIndex.isPending ? 'animate-pulse' : ''} />{rebuildSaveIndex.isPending ? '正在构建索引...' : '构建存档索引'}</button>}<button type="button" onClick={() => { void statusQuery.refetch(); void savePlayersQuery.refetch(); }} className="inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-4 py-2.5 text-xs font-bold text-slate-600 hover:bg-slate-50"><RefreshCw size={14} className={statusQuery.isFetching || savePlayersQuery.isFetching ? 'animate-spin' : ''} />刷新玩家数据</button><Link to="/starter-gift" className="inline-flex items-center gap-2 rounded-xl border border-violet-200 bg-violet-50 px-4 py-2.5 text-xs font-bold text-violet-700 hover:bg-violet-100"><Gift size={14} />初始礼包</Link><Link to="/security" className="inline-flex items-center gap-2 rounded-xl bg-slate-900 px-4 py-2.5 text-xs font-bold text-white"><ShieldAlert size={14} />安全设置</Link></div>
      </header>

      {notice && <div role="status" className="flex items-center gap-2 rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-xs font-semibold text-emerald-800"><CheckCircle2 size={15} />{notice}</div>}
      {visibleError && <div role="alert" className="flex items-start gap-2 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-xs font-semibold text-rose-800"><AlertTriangle size={15} className="mt-0.5 shrink-0" /><span className="break-words">{visibleError}</span></div>}
      {onlineStatusStale && <div role="status" className="flex items-start gap-2 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs font-semibold leading-5 text-amber-800"><AlertTriangle size={15} className="mt-0.5 shrink-0" /><span>官方 REST 在线状态暂不可用；玩家身份与昵称已保留，未由 PalDefender 确认在线的玩家按离线显示。</span></div>}
      {!status?.available && <div className="flex flex-col gap-2 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs font-semibold leading-5 text-amber-800 sm:flex-row sm:items-center sm:justify-between"><span>PalDefender 当前不可操作：{readinessText}。存档玩家、帕鲁和背包快照仍可正常查看。</span><Link to="/security" className="shrink-0 font-bold underline">前往修复</Link></div>}

      <section className="grid min-h-[720px] overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm lg:grid-cols-[330px_minmax(0,1fr)]">
        <aside className="flex min-h-0 flex-col border-b border-slate-200 bg-slate-50/50 lg:border-b-0 lg:border-r">
          <div className="space-y-3 border-b border-slate-200 bg-white p-3">
            <label className="relative block"><span className="sr-only">搜索玩家</span><Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" /><input type="search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="昵称、UserId、公会" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none" /></label>
            <div className="flex rounded-xl bg-slate-100 p-1">{([['all', '全部'], ['online', '在线'], ['offline', '离线']] as const).map(([id, label]) => <button type="button" key={id} onClick={() => setPlayerFilter(id)} className={`flex-1 rounded-lg px-2 py-1.5 text-[10px] font-bold ${playerFilter === id ? 'bg-white text-slate-800 shadow-sm' : 'text-slate-500'}`}>{label}</button>)}</div>
          </div>
          <div className="max-h-80 min-h-0 flex-1 overflow-y-auto p-2 lg:max-h-none">
            {savePlayersQuery.isLoading && players.length === 0 ? <div className="flex items-center justify-center px-4 py-12 text-xs font-semibold text-slate-400"><LoaderCircle size={15} className="mr-2 animate-spin" />正在加载玩家数据...</div> : filteredPlayers.length === 0 ? <div className="px-4 py-12 text-center text-xs font-semibold text-slate-400">没有匹配的玩家</div> : filteredPlayers.map((player) => <button type="button" key={player.id} aria-label={`${player.nickname} ${player.is_online ? '在线' : '离线'}`} onClick={() => setSelectedKey(player.id)} className={`mb-1 flex w-full items-center gap-3 rounded-xl border px-3 py-3 text-left ${selectedKey === player.id ? 'border-sky-200 bg-sky-50 shadow-sm' : 'border-transparent hover:border-slate-200 hover:bg-white'}`}><span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-xl text-xs font-black ${player.is_online ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-200 text-slate-500'}`}>{player.nickname.slice(0, 2).toUpperCase()}</span><span className="min-w-0 flex-1"><span className="flex items-center gap-1.5"><span className="truncate text-xs font-bold text-slate-800">{player.nickname}</span><Database size={11} className="shrink-0 text-violet-400" />{player.online_source.includes('paldefender') && <Gamepad2 size={11} className="shrink-0 text-sky-500" />}</span><span className="mt-0.5 block truncate font-mono text-[9px] text-slate-400">{player.gm_user_id || player.steam_id || player.player_uid}</span><span className="mt-1 block truncate text-[9px] font-semibold text-slate-400">Lv.{player.level} · {player.guild_name || '无公会'}</span></span>{player.is_online ? <Wifi size={14} className="shrink-0 text-emerald-500" /> : <WifiOff size={14} className="shrink-0 text-slate-300" />}</button>)}
          </div>
        </aside>

        <main className="min-w-0">
          {selected && overview ? <>
            <div className="border-b border-slate-200 px-4 py-4 sm:px-5">
              <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
                <div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><h2 className="truncate text-lg font-black text-slate-900">{selectedName}</h2><StatusBadge status={selectedOnline ? 'Online' : 'Offline'} /><span className="rounded-lg bg-slate-100 px-2 py-1 text-[10px] font-bold text-slate-500">Lv.{overview.level}</span></div><p className="mt-1 truncate font-mono text-[10px] text-slate-400">{gmIdentifier || overview.playerUID} · {overview.playerUID}</p><p className="mt-1 text-[11px] font-semibold text-slate-500">{overview.guildName || '无公会'} · {overview.x.toFixed(0)}, {overview.y.toFixed(0)}, {overview.z.toFixed(0)}</p></div>
                <div className="flex items-center gap-2"><button type="button" onClick={() => setTeleportOpen(true)} disabled={!canWrite || !selectedOnline || !liveReady || busy} className="inline-flex shrink-0 items-center gap-1.5 rounded-xl border border-sky-200 bg-sky-50 px-3 py-2 text-xs font-bold text-sky-700 disabled:opacity-40"><MapPin size={13} />传送</button><div className="flex max-w-full gap-1 overflow-x-auto rounded-xl border border-slate-200 bg-slate-100 p-1" role="tablist" aria-label="玩家操作分类">
                  {([['profile', '档案', UserRound], ['items', '物品', Boxes], ['progression', '成长', BookOpen], ['pals', '帕鲁', Sword], ['message', '消息', MessageSquareText], ['access', '管理', ShieldAlert]] as const).map(([id, label, Icon]) => <button type="button" role="tab" aria-selected={activeTab === id} key={id} onClick={() => setActiveTab(id)} className={`inline-flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-2 text-xs font-bold ${activeTab === id ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500'}`}><Icon size={13} />{label}</button>)}
                </div></div>
              </div>
            </div>
            {activeTab === 'profile' && <PlayerOverview player={overview} progression={progressionQuery.data} savePals={savePalsQuery.data?.items ?? []} saveInventory={saveInventoryQuery.data?.containers ?? []} saveLoading={savePalsQuery.isLoading || saveInventoryQuery.isLoading} />}
            {activeTab === 'items' && <><ItemWorkspace catalog={catalogQuery.data?.items ?? []} inventory={liveInventoryQuery.data?.Inventory} inventoryLoading={liveInventoryQuery.isLoading || liveInventoryQuery.isFetching} canWrite={canWrite} online={selectedOnline && liveReady} busy={busy} pending={pending} onRefresh={() => void liveInventoryQuery.refetch()} onGive={giveItems} onAdjust={adjustItemTotal} /><SaveInventoryPanel containers={saveInventoryQuery.data?.containers ?? []} catalog={catalogQuery.data?.items ?? []} loading={saveInventoryQuery.isLoading} /></>}
            {activeTab === 'progression' && <ProgressionWorkspace identifier={gmIdentifier} canWrite={canWrite} available={liveReady} busy={busy} pending={pending} progression={progressionQuery.data} techs={techsQuery.data} catalog={localTechnologyCatalogQuery.data?.items ?? []} runtimeTechnologyIDs={runtimeTechnologyCatalogQuery.data?.catalog.entries ?? []} loading={progressionQuery.isFetching || techsQuery.isFetching} onRun={runAction} onRefresh={async () => { await Promise.all([progressionQuery.refetch(), techsQuery.refetch()]); }} />}
            {activeTab === 'pals' && <PalWorkspace identifier={gmIdentifier} playerName={selectedName} online={selectedOnline} canWrite={canWrite} canManageTemplates={canSecurityWrite} available={liveReady} busy={busy} pending={pending} savePals={savePalsQuery.data?.items ?? []} palCatalog={palCatalogQuery.data?.items ?? []} passiveCatalog={passiveCatalogQuery.data?.items ?? []} onRun={runAction} onRelease={releasePal} />}
            {activeTab === 'message' && <MessageWorkspace mode={messageMode} onModeChange={setMessageMode} messageType={messageType} onMessageTypeChange={setMessageType} message={message} onMessageChange={setMessage} canWrite={canWrite && liveReady} online={selectedOnline} busy={busy} onSubmit={() => void submitMessage()} />}
            {activeTab === 'access' && <><AccessWorkspace identifier={gmIdentifier} playerName={selectedName} canSecurityWrite={canSecurityWrite && gmAvailable} busy={busy} pending={pending} onRun={runAction} /><ModerationWorkspace reason={reason} onReasonChange={setReason} banIP={banIP} onBanIPChange={setBanIP} canWrite={canWrite && gmAvailable} busy={busy} online={selectedOnline} pending={pending} onAction={(action) => void moderate(action)} /></>}
          </> : <div className="flex min-h-[720px] flex-col items-center justify-center px-6 text-center text-xs font-semibold text-slate-400"><UserRound size={30} className="mb-3 text-slate-300" />请先从左侧选择玩家</div>}
        </main>
      </section>
      <TeleportDialog open={teleportOpen} playerName={selectedName} options={teleportOptions} pending={pending === 'teleport'} onClose={() => setTeleportOpen(false)} onSubmit={teleportPlayer} />
    </div>
  );
};

const palDefenderReadiness = (status: Awaited<ReturnType<typeof palDefenderGMApi.status>> | undefined) => {
  switch (status?.state) {
    case 'not_installed': return 'PalDefender 尚未安装';
    case 'not_loaded': return 'PalDefender 尚未确认加载';
    case 'rest_disabled': return 'PalDefender REST 未启用';
    case 'not_configured': return 'REST Token 未配置';
    case 'server_not_running': return '游戏服务未运行';
    default: return status?.error || 'PalDefender 暂不可用';
  }
};
