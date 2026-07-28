import React, { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  AlertCircle,
  Building2,
  Crown,
  Eye,
  FileText,
  MapPin,
  RefreshCw,
  Tag,
  Users,
  X,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { guildsApi } from '../api/guilds';
import { saveIndexApi } from '../api/saveIndex';
import { useServerStore } from '../store/useServerStore';
import type { Guild, GuildDetailResponse, GuildMemberDetail } from '../types';
import { DataTable } from '../components/ui/DataTable';
import { SaveIndexStatusBar } from '../components/ui/SaveIndexStatusBar';
import { SaveDataTabs } from '../components/ui/SaveDataTabs';
import { useDebouncedValue } from '../hooks/useDebouncedValue';

const pageSize = 50;

export const Guilds: React.FC = () => {
  const { refreshKey } = useServerStore();
  const queryClient = useQueryClient();
  const [searchText, setSearchText] = useState('');
  const [page, setPage] = useState(1);
  const [selectedGuildID, setSelectedGuildID] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const debouncedSearch = useDebouncedValue(searchText, 250);

  useEffect(() => {
    setPage(1);
  }, [debouncedSearch]);

  const guildsQuery = useQuery({
    queryKey: ['guilds', { page, q: debouncedSearch, refreshKey }],
    queryFn: () =>
      guildsApi.getGuildsList({
        limit: pageSize,
        offset: (page - 1) * pageSize,
        q: debouncedSearch,
      }),
    placeholderData: (previous) => previous,
  });

  const detailQuery = useQuery({
    queryKey: ['guild-detail', selectedGuildID, refreshKey],
    queryFn: () => guildsApi.getGuild(selectedGuildID || ''),
    enabled: Boolean(selectedGuildID),
  });

  const rebuildMutation = useMutation({
    mutationFn: saveIndexApi.rebuild,
    onSuccess: () => {
      setActionError(null);
      void queryClient.invalidateQueries({ queryKey: ['guilds'] });
      void queryClient.invalidateQueries({ queryKey: ['guild-detail'] });
    },
    onError: (rebuildError) => {
      setActionError(getErrorMessage(rebuildError));
    },
  });

  const guilds = guildsQuery.data?.items ?? [];
  const indexStatus = guildsQuery.data?.status ?? null;
  const summary = guildsQuery.data?.summary;
  const loading = guildsQuery.isLoading;
  const error = actionError || (guildsQuery.error ? getErrorMessage(guildsQuery.error) : null);
  const totalItems = summary?.total ?? guilds.length;
  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <SaveDataTabs />
      {error && (
        <div className="rounded-2xl border border-rose-100 bg-rose-50 px-5 py-3 text-xs font-semibold text-rose-700">
          <AlertCircle className="mr-2 inline" size={14} />
          {error}
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Summary label="匹配公会" value={totalItems} />
        <Summary label="成员总数" value={guilds.reduce((sum, guild) => sum + guild.members.length, 0)} />
        <Summary label="在线成员" value={guilds.reduce((sum, guild) => sum + guild.online_member_count, 0)} />
      </div>

      <SaveIndexStatusBar
        status={indexStatus}
        loading={guildsQuery.isFetching}
        rebuilding={rebuildMutation.isPending}
        onRefresh={() => void guildsQuery.refetch()}
        onRebuild={() => rebuildMutation.mutate()}
      />

      <section className="rounded-3xl border border-slate-100 bg-white p-4 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)] sm:p-6">
        {loading && guilds.length === 0 ? (
          <div className="py-12 text-center text-xs font-semibold text-slate-400">
            <RefreshCw className="mr-2 inline animate-spin text-sky-500" size={14} />
            正在获取公会数据...
          </div>
        ) : (
          <DataTable
            headers={[
              { key: 'name', label: '公会' },
              { key: 'members', label: '成员' },
              { key: 'online', label: '在线' },
              { key: 'bases', label: '基地' },
              { key: 'owner', label: '会长 UID' },
              { key: 'actions', label: '详情', align: 'right' },
            ]}
            data={guilds}
            searchText={searchText}
            onSearchChange={setSearchText}
            searchPlaceholder="搜索公会或会长 UID"
            pagination={{
              currentPage: page,
              totalPages,
              totalItems,
              itemsPerPage: pageSize,
              onPageChange: setPage,
            }}
            virtualized
            emptyText={error ? '存档索引不可用' : '暂无公会'}
            renderCard={(guild) => <GuildCard key={guild.id} guild={guild} onDetail={() => setSelectedGuildID(guild.id)} />}
            renderRow={(guild) => (
              <tr key={guild.id} className="hover:bg-slate-50/50">
                <td className="px-6 py-4 text-xs font-bold text-slate-700">{guild.name}</td>
                <td className="px-6 py-4 text-xs font-semibold text-slate-600">{guild.members.length} 人</td>
                <td className="px-6 py-4 text-xs font-semibold text-emerald-600">{guild.online_member_count} 人</td>
                <td className="px-6 py-4 text-xs font-semibold text-slate-600">{guild.base_ids.length} 个</td>
                <td className="px-6 py-4 font-mono text-[10px] text-slate-400">{guild.owner_player_uid || '-'}</td>
                <td className="px-6 py-4 text-right">
                  <button
                    type="button"
                    onClick={() => setSelectedGuildID(guild.id)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 px-3 py-2 text-[10px] font-bold text-slate-600 hover:border-sky-200 hover:bg-sky-50 hover:text-sky-700"
                  >
                    <Eye size={12} />查看
                  </button>
                </td>
              </tr>
            )}
          />
        )}
      </section>

      {selectedGuildID && (
        <GuildDetailDrawer
          data={detailQuery.data}
          loading={detailQuery.isLoading || detailQuery.isFetching}
          error={detailQuery.error ? getErrorMessage(detailQuery.error) : null}
          onRetry={() => void detailQuery.refetch()}
          onClose={() => setSelectedGuildID(null)}
        />
      )}
    </div>
  );
};

const Summary: React.FC<{ label: string; value: number }> = ({ label, value }) => (
  <div className="flex items-center gap-3 rounded-2xl border border-slate-100 bg-white px-5 py-3 shadow-sm">
    <Users size={15} className="text-sky-500" />
    <span className="text-xs font-semibold text-slate-500">
      {label}: {value}
    </span>
  </div>
);

const GuildCard: React.FC<{ guild: Guild; onDetail: () => void }> = ({ guild, onDetail }) => (
  <div className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
    <p className="truncate text-sm font-bold text-slate-800">{guild.name}</p>
    <div className="mt-3 grid grid-cols-2 gap-2 text-[11px] font-semibold text-slate-500">
      <span>成员: {guild.members.length}</span>
      <span>在线: {guild.online_member_count}</span>
      <span>基地: {guild.base_ids.length}</span>
      <span className="truncate font-mono">会长: {guild.owner_player_uid || '-'}</span>
    </div>
    <button type="button" onClick={onDetail} className="mt-4 inline-flex w-full items-center justify-center gap-2 rounded-xl border border-sky-200 bg-sky-50 py-2 text-xs font-bold text-sky-700">
      <Eye size={13} />查看公会详情
    </button>
  </div>
);

const GuildDetailDrawer: React.FC<{
  data?: GuildDetailResponse;
  loading: boolean;
  error: string | null;
  onRetry: () => void;
  onClose: () => void;
}> = ({ data, loading, error, onRetry, onClose }) => {
  const guild = data?.guild;
  const members = data?.members ?? [];
  const bases = data?.bases ?? [];
  const owner = members.find((member) => member.is_owner);

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-slate-900/20 px-3 py-3 backdrop-blur-sm sm:px-6 sm:py-6">
      <aside className="flex h-full w-full max-w-2xl flex-col rounded-2xl border border-slate-100 bg-white shadow-2xl">
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-5 py-4">
          <div className="min-w-0">
            <p className="truncate text-sm font-bold text-slate-800">{guild?.name || '公会详情'}</p>
            <p className="mt-1 truncate font-mono text-[10px] text-slate-400">{guild?.id || '-'}</p>
          </div>
          <button type="button" onClick={onClose} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50" aria-label="关闭公会详情">
            <X size={14} />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {data?.status.stale && (
            <div className="mb-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs font-semibold text-amber-800">
              当前显示的是缓存存档索引，成员与基地信息可能稍有延迟。
            </div>
          )}
          {error ? (
            <div className="rounded-xl border border-rose-100 bg-rose-50 px-4 py-4 text-xs font-semibold text-rose-700">
              <p>{error}</p>
              <button type="button" onClick={onRetry} className="mt-3 inline-flex items-center gap-2 rounded-lg border border-rose-200 bg-white px-3 py-2 text-[11px] font-bold">
                <RefreshCw size={13} />重试
              </button>
            </div>
          ) : loading && !data ? (
            <div className="py-16 text-center text-xs font-semibold text-slate-400">
              <RefreshCw size={15} className="mr-2 inline animate-spin text-sky-500" />正在读取公会详情...
            </div>
          ) : (
            <>
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                <DetailSummary label="成员" value={members.length} />
                <DetailSummary label="在线" value={members.filter((member) => member.is_online).length} />
                <DetailSummary label="基地" value={bases.length} />
                <DetailSummary label="有备注" value={members.filter((member) => member.has_annotation).length} />
              </div>

              <section className="mt-5 rounded-2xl border border-amber-100 bg-amber-50/50 p-4">
                <p className="flex items-center gap-2 text-xs font-bold text-slate-700"><Crown size={14} className="text-amber-500" />公会会长</p>
                <p className="mt-2 text-sm font-black text-slate-800">{owner?.nickname || guild?.owner_player_uid || '未知'}</p>
                <p className="mt-1 break-all font-mono text-[10px] text-slate-400">{owner?.steam_id || owner?.player_uid || guild?.owner_player_uid || '-'}</p>
              </section>

              <section className="mt-5">
                <h3 className="flex items-center gap-2 text-xs font-bold text-slate-700"><Users size={14} className="text-sky-500" />成员列表</h3>
                <div className="mt-3 space-y-2">
                  {members.length === 0 ? (
                    <EmptyText>该公会没有可读取的成员详情</EmptyText>
                  ) : members.map((member) => <GuildMemberCard key={member.id || member.player_uid} member={member} />)}
                </div>
              </section>

              <section className="mt-6">
                <h3 className="flex items-center gap-2 text-xs font-bold text-slate-700"><Building2 size={14} className="text-violet-500" />公会基地</h3>
                <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
                  {bases.length === 0 ? (
                    <div className="sm:col-span-2"><EmptyText>该公会没有可读取的基地详情</EmptyText></div>
                  ) : bases.map((base) => (
                    <article key={base.id} className="rounded-2xl border border-slate-100 bg-slate-50/60 p-4">
                      <p className="truncate text-xs font-bold text-slate-700">{base.name}</p>
                      {base.has_custom_name && <p className="mt-1 truncate text-[9px] font-semibold text-violet-500">面板自定义名称</p>}
                      <p className="mt-3 flex items-center gap-1.5 font-mono text-[10px] text-slate-500"><MapPin size={11} />{base.x.toFixed(0)}, {base.y.toFixed(0)}, {base.z.toFixed(0)}</p>
                      <div className="mt-3 grid grid-cols-2 gap-2 text-[10px] font-semibold text-slate-500">
                        <span>建筑: {base.structures_count}</span>
                        <span>工作帕鲁: {base.workers_count}</span>
                      </div>
                    </article>
                  ))}
                </div>
              </section>
            </>
          )}
        </div>
      </aside>
    </div>
  );
};

const GuildMemberCard: React.FC<{ member: GuildMemberDetail }> = ({ member }) => (
  <article className="rounded-2xl border border-slate-100 bg-slate-50/60 p-4">
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className={`h-2 w-2 shrink-0 rounded-full ${member.is_online ? 'bg-emerald-500' : 'bg-slate-300'}`} />
          <p className="truncate text-xs font-bold text-slate-700">{member.nickname}</p>
          {member.is_owner && <span className="rounded-full bg-amber-100 px-2 py-0.5 text-[9px] font-black text-amber-700">会长</span>}
        </div>
        <p className="mt-1 truncate font-mono text-[9px] text-slate-400">{member.steam_id || member.player_uid || '-'}</p>
      </div>
      <span className="shrink-0 text-[10px] font-bold text-slate-500">Lv.{member.level}</span>
    </div>
    {member.tags.length > 0 && (
      <div className="mt-3 flex flex-wrap gap-1.5">
        {member.tags.map((tag) => <span key={tag} className="inline-flex items-center gap-1 rounded-full bg-violet-100 px-2 py-1 text-[9px] font-bold text-violet-700"><Tag size={9} />{tag}</span>)}
      </div>
    )}
    {member.note && <p className="mt-3 flex gap-2 rounded-xl border border-amber-100 bg-amber-50 px-3 py-2 text-[10px] font-semibold leading-5 text-amber-800"><FileText size={12} className="mt-1 shrink-0" />{member.note}</p>}
    <p className="mt-3 text-[9px] font-semibold text-slate-400">最后在线: {member.last_online_time || '-'}</p>
  </article>
);

const DetailSummary: React.FC<{ label: string; value: number }> = ({ label, value }) => (
  <div className="rounded-xl border border-slate-100 bg-slate-50 px-3 py-3 text-center">
    <p className="text-lg font-black text-slate-800">{value}</p>
    <p className="mt-1 text-[9px] font-bold text-slate-400">{label}</p>
  </div>
);

const EmptyText: React.FC<React.PropsWithChildren> = ({ children }) => (
  <div className="rounded-xl border border-dashed border-slate-200 px-4 py-8 text-center text-xs font-semibold text-slate-400">{children}</div>
);
