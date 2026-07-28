import React, { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertCircle, Ban as BanIcon, Eye, FileText, LogOut, RefreshCw, Save, Tag, Trash2, X } from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { playersApi } from '../api/players';
import { saveIndexApi } from '../api/saveIndex';
import { useServerStore } from '../store/useServerStore';
import type { Player } from '../types';
import { DataTable } from '../components/ui/DataTable';
import { StatusBadge } from '../components/ui/StatusBadge';
import { SaveIndexStatusBar } from '../components/ui/SaveIndexStatusBar';
import { SaveDataTabs } from '../components/ui/SaveDataTabs';
import { useDebouncedValue } from '../hooks/useDebouncedValue';
import { appConfig } from '../config/defaults';

const pageSize = 50;

const formatPresenceDuration = (seconds?: number) => {
  const value = Math.max(0, Math.floor(Number(seconds) || 0));
  const days = Math.floor(value / 86400);
  const hours = Math.floor((value % 86400) / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  if (days > 0) return `${days} 天 ${hours} 小时`;
  if (hours > 0) return `${hours} 小时 ${minutes} 分`;
  return `${minutes} 分钟`;
};

const formatPresenceTime = (value?: string) => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', { hour12: false });
};

export const Players: React.FC = () => {
  const { refreshKey, session } = useServerStore();
  const canWriteAnnotations = Boolean(session?.permissions.includes('players:write'));
  const queryClient = useQueryClient();
  const [searchText, setSearchText] = useState('');
  const [activeTab, setActiveTab] = useState('all');
  const [page, setPage] = useState(1);
  const [notice, setNotice] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const [selectedPlayer, setSelectedPlayer] = useState<Player | null>(null);
  const debouncedSearch = useDebouncedValue(searchText, 250);
  const onlineFilter = activeTab === 'online' ? true : activeTab === 'offline' ? false : undefined;

  useEffect(() => {
    setPage(1);
  }, [activeTab, debouncedSearch]);

  const playersQuery = useQuery({
    queryKey: ['players', { page, q: debouncedSearch, online: onlineFilter, refreshKey }],
    queryFn: () =>
      playersApi.getPlayersList({
        limit: pageSize,
        offset: (page - 1) * pageSize,
        q: debouncedSearch,
        online: onlineFilter,
      }),
    placeholderData: (previous) => previous,
  });

  const annotationMutation = useMutation({
    mutationFn: ({ player, note, tags }: { player: Player; note: string; tags: string[] }) =>
      playersApi.updateAnnotation(player.player_uid || player.steam_id, note, tags),
    onSuccess: async (result) => {
      setSelectedPlayer((current) => current ? {
        ...current,
        note: result.player.note,
        tags: result.player.tags,
        has_annotation: result.player.has_annotation,
        annotation_updated_at: result.player.annotation_updated_at,
      } : result.player);
      setNotice('玩家备注已保存');
      setActionError(null);
      await queryClient.invalidateQueries({ queryKey: ['players'] });
    },
    onError: (annotationError) => {
      setNotice(null);
      setActionError(getErrorMessage(annotationError));
    },
  });

  const clearAnnotationMutation = useMutation({
    mutationFn: (player: Player) => playersApi.clearAnnotation(player.player_uid || player.steam_id),
    onSuccess: async (result) => {
      setSelectedPlayer((current) => current ? {
        ...current,
        note: '',
        tags: [],
        has_annotation: false,
        annotation_updated_at: '',
      } : result.player);
      setNotice('玩家备注与标签已清除');
      setActionError(null);
      await queryClient.invalidateQueries({ queryKey: ['players'] });
    },
    onError: (annotationError) => {
      setNotice(null);
      setActionError(getErrorMessage(annotationError));
    },
  });

  const rebuildMutation = useMutation({
    mutationFn: saveIndexApi.rebuild,
    onSuccess: () => {
      setNotice('已触发存档索引重建');
      setActionError(null);
      void queryClient.invalidateQueries({ queryKey: ['players'] });
    },
    onError: (rebuildError) => {
      setNotice(null);
      setActionError(getErrorMessage(rebuildError));
    },
  });

  const players = playersQuery.data?.items ?? [];
  const indexStatus = playersQuery.data?.status ?? null;
  const historicalView = playersQuery.data?.view?.online_overlay === false;
  const onlineStatusStale = !historicalView && (players.some((player) => player.online_stale)
    || Boolean(indexStatus?.warnings.some((warning) => warning.includes('online player REST data is stale or unavailable'))));
  const summary = playersQuery.data?.summary;
  const loading = playersQuery.isLoading;
  const error = actionError || (playersQuery.error ? getErrorMessage(playersQuery.error) : null);
  const totalItems = summary?.total ?? players.length;
  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));

  const runPlayerAction = async (actionKey: string, action: () => Promise<unknown>, successMessage: string) => {
    setPendingAction(actionKey);
    try {
      await action();
      setNotice(successMessage);
      setActionError(null);
      await queryClient.invalidateQueries({ queryKey: ['players'] });
    } catch (actionError) {
      setNotice(null);
      setActionError(getErrorMessage(actionError));
    } finally {
      setPendingAction(null);
    }
  };

  const kick = async (player: Player) => {
    if (!window.confirm(`踢出玩家 ${player.nickname} (${player.steam_id})？`)) return;
    const reason = `Kicked from ${appConfig.brand} panel`;
    await runPlayerAction(
      `kick:${player.steam_id}`,
      () => playersApi.kickPlayer(player.steam_id, reason),
      `已向官方 REST 提交踢出请求：${player.nickname}`,
    );
  };

  const ban = async (player: Player) => {
    const defaultReason = `Banned from ${appConfig.brand} panel`;
    const reason = window.prompt(`请输入封禁 ${player.nickname} 的原因`, defaultReason);
    if (reason === null) return;
    if (!window.confirm(`确认封禁玩家 ${player.nickname} (${player.steam_id})？`)) return;
    await runPlayerAction(
      `ban:${player.steam_id}`,
      () => playersApi.banPlayer(player.steam_id, player.nickname, reason.trim() || defaultReason),
      `已向官方 REST 提交封禁请求，并写入本地封禁列表：${player.nickname}`,
    );
  };

  const headers = [
    { key: 'nickname', label: '玩家 / SteamID' },
    { key: 'level', label: '等级' },
    { key: 'guild_name', label: '公会' },
    { key: 'coordinates', label: '坐标' },
    { key: 'status', label: '状态' },
    { key: 'last_seen', label: '最后在线' },
    { key: 'actions', label: '操作', align: 'center' as const },
  ];

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <SaveDataTabs />
      {error && (
        <div className="rounded-2xl border border-rose-100 bg-rose-50 px-5 py-3 text-xs font-semibold text-rose-700">
          <AlertCircle className="mr-2 inline" size={14} />
          {error}
        </div>
      )}
      {notice && (
        <div className="rounded-2xl border border-sky-100 bg-sky-50 px-5 py-3 text-xs font-semibold text-sky-700">
          {notice}
        </div>
      )}
      {historicalView && (
        <div role="status" className="rounded-2xl border border-sky-200 bg-sky-50 px-5 py-3 text-xs font-semibold leading-5 text-sky-800">
          历史存档视图，不叠加实时在线状态
        </div>
      )}
      {onlineStatusStale && (
        <div role="status" className="rounded-2xl border border-amber-200 bg-amber-50 px-5 py-3 text-xs font-semibold leading-5 text-amber-800">
          官方 REST 在线状态暂不可用；玩家身份与昵称已保留，未由 PalDefender 确认在线的玩家按离线显示。
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Summary label="在线玩家" value={players.filter((player) => player.is_online).length} color="emerald" />
        <Summary label="离线玩家" value={players.filter((player) => !player.is_online).length} color="slate" />
        <Summary label="匹配玩家" value={totalItems} color="sky" />
      </div>

      <SaveIndexStatusBar
        status={indexStatus}
        loading={playersQuery.isFetching}
        rebuilding={rebuildMutation.isPending}
        onRefresh={() => void playersQuery.refetch()}
        onRebuild={() => rebuildMutation.mutate()}
      />

      <section className="rounded-3xl border border-slate-100 bg-white p-4 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)] sm:p-6">
        {loading && players.length === 0 ? (
          <div className="py-12 text-center text-xs font-semibold text-slate-400">
            <RefreshCw className="mr-2 inline animate-spin text-sky-500" size={14} />
            正在获取玩家数据...
          </div>
        ) : (
          <DataTable
            headers={headers}
            data={players}
            searchText={searchText}
            onSearchChange={setSearchText}
            searchPlaceholder="搜索玩家昵称、SteamID、备注或标签"
            tabs={[
              { id: 'all', label: '全部' },
              { id: 'online', label: '在线' },
              { id: 'offline', label: '离线' },
            ]}
            activeTab={activeTab}
            onTabChange={setActiveTab}
            headerActions={
              <button
                type="button"
                onClick={() => void playersQuery.refetch()}
                disabled={playersQuery.isFetching}
                className="inline-flex items-center justify-center gap-2 rounded-xl border border-slate-200 px-3 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:opacity-40"
              >
                <RefreshCw size={13} className={playersQuery.isFetching ? 'animate-spin' : ''} />
                刷新
              </button>
            }
            pagination={{
              currentPage: page,
              totalPages,
              totalItems,
              itemsPerPage: pageSize,
              onPageChange: setPage,
            }}
            virtualized
            emptyText={error ? '后端不可用或接口未实现' : '暂无玩家'}
            renderCard={(player) => (
              <PlayerCard
                key={player.steam_id}
                player={player}
                pendingAction={pendingAction}
                onDetail={() => setSelectedPlayer(player)}
                onKick={() => kick(player)}
                onBan={() => ban(player)}
              />
            )}
            renderRow={(player) => (
              <tr key={player.steam_id} className="hover:bg-slate-50/50">
                <td className="px-6 py-4">
                  <PlayerIdentity player={player} />
                </td>
                <td className="px-6 py-4 text-xs font-bold text-slate-600">Lv.{player.level}</td>
                <td className="px-6 py-4 text-xs font-semibold text-slate-500">{player.guild_name || '-'}</td>
                <td className="px-6 py-4 font-mono text-xs text-slate-500">
                  {player.x.toFixed(0)}, {player.y.toFixed(0)}, {player.z.toFixed(0)}
                </td>
                <td className="px-6 py-4">
                  <StatusBadge status={player.is_online ? 'Online' : 'Offline'} />
                </td>
                <td className="px-6 py-4 text-xs font-medium text-slate-400">{player.last_online_time}</td>
                <td className="px-6 py-4 text-center">
                  <PlayerActions
                    player={player}
                    pendingAction={pendingAction}
                    onDetail={() => setSelectedPlayer(player)}
                    onKick={() => kick(player)}
                    onBan={() => ban(player)}
                  />
                </td>
              </tr>
            )}
          />
        )}
      </section>
      {selectedPlayer && (
        <PlayerDetail
          player={selectedPlayer}
          canWrite={canWriteAnnotations}
          saving={annotationMutation.isPending || clearAnnotationMutation.isPending}
          onSave={(note, tags) => annotationMutation.mutate({ player: selectedPlayer, note, tags })}
          onClear={() => clearAnnotationMutation.mutate(selectedPlayer)}
          onClose={() => setSelectedPlayer(null)}
        />
      )}
    </div>
  );
};

const Summary: React.FC<{ label: string; value: number; color: 'emerald' | 'slate' | 'sky' }> = ({ label, value, color }) => {
  const dot = color === 'emerald' ? 'bg-emerald-500' : color === 'sky' ? 'bg-sky-500' : 'bg-slate-400';
  return (
    <div className="flex items-center gap-3 rounded-2xl border border-slate-100 bg-white px-5 py-3 shadow-sm">
      <span className={`h-2.5 w-2.5 rounded-full ${dot}`} />
      <span className="text-xs font-semibold text-slate-500">
        {label}: {value}
      </span>
    </div>
  );
};

const PlayerIdentity: React.FC<{ player: Player }> = ({ player }) => (
  <div className="flex min-w-0 items-center gap-3">
    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-sky-50 text-xs font-bold text-sky-600">
      {player.nickname.substring(0, 2).toUpperCase()}
    </div>
    <div className="min-w-0">
      <p className="truncate text-xs font-bold text-slate-700">{player.nickname}</p>
      <p className="truncate font-mono text-[10px] text-slate-400">{player.steam_id}</p>
      {(player.tags?.length ?? 0) > 0 && (
        <div className="mt-1 flex max-w-[220px] flex-wrap gap-1">
          {player.tags!.slice(0, 3).map((tag) => (
            <span key={tag} className="rounded-full bg-violet-50 px-1.5 py-0.5 text-[9px] font-bold text-violet-600">{tag}</span>
          ))}
          {(player.tags?.length ?? 0) > 3 && <span className="text-[9px] font-bold text-slate-400">+{player.tags!.length - 3}</span>}
        </div>
      )}
      {player.presence_available && (
        <p className="mt-1 truncate text-[9px] font-semibold text-sky-600">
          {player.is_online ? '本次在线' : '上次在线'} {formatPresenceDuration(player.session_seconds)} · 累计 {formatPresenceDuration(player.total_seconds)}
        </p>
      )}
    </div>
  </div>
);

const PlayerActions: React.FC<{
  player: Player;
  pendingAction: string | null;
  onDetail: () => void;
  onKick: () => void;
  onBan: () => void;
}> = ({ player, pendingAction, onDetail, onKick, onBan }) => {
  const kickPending = pendingAction === `kick:${player.steam_id}`;
  const banPending = pendingAction === `ban:${player.steam_id}`;
  return (
    <div className="flex justify-center gap-2">
      <button
        type="button"
        title="查看详情"
        onClick={onDetail}
        className="inline-flex items-center gap-1.5 rounded-lg border border-sky-200 px-3 py-2 text-[10px] font-bold text-sky-600 hover:bg-sky-50"
      >
        <Eye size={12} />
        详情
      </button>
      <button
        type="button"
        title="踢出玩家"
        onClick={onKick}
        disabled={!player.is_online || kickPending || Boolean(pendingAction)}
        className="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 px-3 py-2 text-[10px] font-bold text-slate-500 hover:bg-slate-50 disabled:opacity-40"
      >
        <LogOut size={12} />
        {kickPending ? '提交中' : '踢出'}
      </button>
      <button
        type="button"
        title="封禁玩家"
        onClick={onBan}
        disabled={banPending || Boolean(pendingAction)}
        className="inline-flex items-center gap-1.5 rounded-lg border border-rose-200 px-3 py-2 text-[10px] font-bold text-rose-600 hover:bg-rose-50 disabled:opacity-40"
      >
        <BanIcon size={12} />
        {banPending ? '提交中' : '封禁'}
      </button>
    </div>
  );
};

const PlayerCard: React.FC<{
  player: Player;
  pendingAction: string | null;
  onDetail: () => void;
  onKick: () => void;
  onBan: () => void;
}> = ({ player, pendingAction, onDetail, onKick, onBan }) => (
  <div className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
    <div className="flex items-start justify-between gap-3">
      <PlayerIdentity player={player} />
      <StatusBadge status={player.is_online ? 'Online' : 'Offline'} />
    </div>
    <div className="mt-4 grid grid-cols-2 gap-2 text-[11px] font-semibold text-slate-500">
      <span>等级: Lv.{player.level}</span>
      <span className="truncate">公会: {player.guild_name || '-'}</span>
      <span className="col-span-2 font-mono">
        坐标: {player.x.toFixed(0)}, {player.y.toFixed(0)}, {player.z.toFixed(0)}
      </span>
      <span className="col-span-2 text-slate-400">最后在线: {player.last_online_time}</span>
      {player.note && <span className="col-span-2 line-clamp-2 rounded-lg bg-amber-50 px-2 py-1.5 text-amber-700">备注: {player.note}</span>}
    </div>
    <div className="mt-4">
      <PlayerActions player={player} pendingAction={pendingAction} onDetail={onDetail} onKick={onKick} onBan={onBan} />
    </div>
  </div>
);

const PlayerDetail: React.FC<{
  player: Player;
  canWrite: boolean;
  saving: boolean;
  onSave: (note: string, tags: string[]) => void;
  onClear: () => void;
  onClose: () => void;
}> = ({ player, canWrite, saving, onSave, onClear, onClose }) => {
  const inventoryEntries = Object.entries(player.inventory_summary || {});
  const [note, setNote] = useState(player.note || '');
  const [tagText, setTagText] = useState((player.tags || []).join('，'));

  useEffect(() => {
    setNote(player.note || '');
    setTagText((player.tags || []).join('，'));
  }, [player.annotation_updated_at, player.note, player.tags]);

  const normalizedTags = () => Array.from(new Set(
    tagText
      .split(/[,，\n]/)
      .map((tag) => tag.trim())
      .filter(Boolean),
  )).slice(0, 8);

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-slate-900/20 px-3 py-3 backdrop-blur-sm sm:px-6 sm:py-6">
      <aside className="flex h-full w-full max-w-md flex-col rounded-2xl border border-slate-100 bg-white shadow-2xl">
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-5 py-4">
          <div className="min-w-0">
            <p className="truncate text-sm font-bold text-slate-800">{player.nickname}</p>
            <p className="truncate font-mono text-[10px] text-slate-400">{player.steam_id || player.player_uid || '-'}</p>
          </div>
          <button type="button" onClick={onClose} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50" aria-label="关闭详情">
            <X size={14} />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          <div className="grid grid-cols-2 gap-3 text-xs">
            <Detail label="等级" value={`Lv.${player.level}`} />
            <Detail label="状态" value={player.is_online ? '在线' : '离线'} />
            <Detail label="公会" value={player.guild_name || '-'} />
            <Detail label="最后在线" value={player.last_online_time || '-'} />
            <Detail label="玩家 UID" value={player.player_uid || '-'} mono />
            <Detail label="Steam ID" value={player.steam_id || '-'} mono />
            <Detail label="坐标" value={`${player.x.toFixed(0)}, ${player.y.toFixed(0)}, ${player.z.toFixed(0)}`} mono />
            <Detail label="Ping" value={player.ping == null ? '-' : `${player.ping} ms`} />
            <Detail label={player.is_online ? '本次在线' : '上次在线'} value={player.presence_available ? formatPresenceDuration(player.session_seconds) : '统计暂不可用'} />
            <Detail label="累计在线" value={player.presence_available ? formatPresenceDuration(player.total_seconds) : '统计暂不可用'} />
            <Detail label="最近上线" value={formatPresenceTime(player.last_online_at)} />
            <Detail label="最近下线" value={formatPresenceTime(player.last_offline_at)} />
          </div>

          {(player.presence_sessions?.length ?? 0) > 0 && (
            <section className="mt-5 rounded-2xl border border-sky-100 bg-sky-50/40 p-4">
              <h3 className="text-xs font-bold text-slate-700">最近在线记录</h3>
              <div className="mt-3 space-y-2">
                {[...player.presence_sessions!].slice(-5).reverse().map((session) => (
                  <div key={`${session.started_at}-${session.ended_at}`} className="rounded-xl border border-sky-100 bg-white px-3 py-2">
                    <div className="flex items-center justify-between gap-3 text-[10px]">
                      <span className="font-semibold text-slate-500">{formatPresenceTime(session.started_at)}</span>
                      <span className="font-bold text-sky-600">{formatPresenceDuration(session.duration_seconds)}</span>
                    </div>
                    <p className="mt-1 text-[9px] text-slate-400">下线：{formatPresenceTime(session.ended_at)}</p>
                  </div>
                ))}
              </div>
            </section>
          )}

          <section className="mt-5 rounded-2xl border border-violet-100 bg-violet-50/40 p-4">
            <div className="flex items-center justify-between gap-3">
              <div>
                <h3 className="flex items-center gap-2 text-xs font-bold text-slate-700"><FileText size={14} className="text-violet-500" />玩家备注</h3>
                <p className="mt-1 text-[10px] font-semibold text-slate-400">仅保存在面板数据库中，不修改玩家存档。</p>
              </div>
              {player.annotation_updated_at && <span className="text-[9px] font-semibold text-slate-400">已记录</span>}
            </div>
            <textarea
              value={note}
              onChange={(event) => setNote(event.target.value)}
              disabled={!canWrite || saving}
              maxLength={500}
              rows={5}
              placeholder="记录玩家行为、职责或管理注意事项"
              className="mt-3 w-full resize-y rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs text-slate-700 outline-none focus:border-violet-300 disabled:bg-slate-50"
            />
            <label className="mt-3 block text-[10px] font-bold text-slate-500">
              <span className="mb-1.5 flex items-center gap-1"><Tag size={11} />标签，最多 8 个</span>
              <input
                value={tagText}
                onChange={(event) => setTagText(event.target.value)}
                disabled={!canWrite || saving}
                placeholder="活跃，建筑师，重点观察"
                className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs text-slate-700 outline-none focus:border-violet-300 disabled:bg-slate-50"
              />
            </label>
            <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
              <span className="text-[10px] font-semibold text-slate-400">{note.length}/500 字符</span>
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={onClear}
                  disabled={!canWrite || saving || !player.has_annotation}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-rose-200 px-3 py-2 text-[10px] font-bold text-rose-600 hover:bg-rose-50 disabled:opacity-40"
                >
                  <Trash2 size={12} />清除
                </button>
                <button
                  type="button"
                  onClick={() => onSave(note, normalizedTags())}
                  disabled={!canWrite || saving}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-violet-600 px-3 py-2 text-[10px] font-bold text-white hover:bg-violet-700 disabled:opacity-40"
                >
                  <Save size={12} />{saving ? '保存中' : '保存备注'}
                </button>
              </div>
            </div>
            {!canWrite && <p className="mt-2 text-[10px] font-semibold text-amber-600">当前账号缺少 players:write 权限，只能查看。</p>}
          </section>

          <div className="mt-5">
            <p className="text-[11px] font-bold uppercase text-slate-400">背包摘要</p>
            {inventoryEntries.length > 0 ? (
              <div className="mt-2 grid grid-cols-2 gap-2">
                {inventoryEntries.map(([key, value]) => (
                  <div key={key} className="rounded-xl border border-slate-100 bg-slate-50 px-3 py-2">
                    <p className="truncate text-[10px] font-semibold text-slate-400">{key}</p>
                    <p className="mt-1 truncate text-xs font-bold text-slate-700">{String(value)}</p>
                  </div>
                ))}
              </div>
            ) : (
              <p className="mt-2 text-xs font-semibold text-slate-400">暂无背包摘要</p>
            )}
          </div>
        </div>
      </aside>
    </div>
  );
};

const Detail: React.FC<{ label: string; value: string; mono?: boolean }> = ({ label, value, mono = false }) => (
  <div className="rounded-xl border border-slate-100 bg-slate-50 px-3 py-2">
    <p className="text-[10px] font-semibold text-slate-400">{label}</p>
    <p className={`mt-1 truncate text-xs font-bold text-slate-700 ${mono ? 'font-mono' : ''}`}>{value}</p>
  </div>
);
