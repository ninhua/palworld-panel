import React, { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  AlertCircle,
  Apple,
  Cat,
  Copy,
  MapPin,
  Package,
  PackageOpen,
  Pencil,
  RefreshCw,
  RotateCcw,
  Search,
  ShieldAlert,
  Sparkles,
  Users,
  X,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { basesApi } from '../api/bases';
import { saveIndexApi } from '../api/saveIndex';
import { useServerStore } from '../store/useServerStore';
import type { Base, BaseFeedBoxesResponse, BaseStorageResponse, BaseWorkersResponse } from '../types';
import { DataTable } from '../components/ui/DataTable';
import { StatusBadge } from '../components/ui/StatusBadge';
import { SaveIndexStatusBar } from '../components/ui/SaveIndexStatusBar';
import { SaveDataTabs } from '../components/ui/SaveDataTabs';
import { useDebouncedValue } from '../hooks/useDebouncedValue';
import { PalIcon } from '../components/gm/PalIcon';

const pageSize = 50;

export const Bases: React.FC = () => {
  const { refreshKey } = useServerStore();
  const queryClient = useQueryClient();
  const [searchText, setSearchText] = useState('');
  const [page, setPage] = useState(1);
  const [notice, setNotice] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [storageBase, setStorageBase] = useState<Base | null>(null);
  const [storageSearch, setStorageSearch] = useState('');
  const [workersBase, setWorkersBase] = useState<Base | null>(null);
  const [workerSearch, setWorkerSearch] = useState('');
  const [feedBase, setFeedBase] = useState<Base | null>(null);
  const [feedSearch, setFeedSearch] = useState('');
  const debouncedSearch = useDebouncedValue(searchText, 250);

  useEffect(() => {
    setPage(1);
  }, [debouncedSearch]);

  const basesQuery = useQuery({
    queryKey: ['bases', { page, q: debouncedSearch, refreshKey }],
    queryFn: () =>
      basesApi.getBasesList({
        limit: pageSize,
        offset: (page - 1) * pageSize,
        q: debouncedSearch,
      }),
    placeholderData: (previous) => previous,
  });

  const storageQuery = useQuery({
    queryKey: ['base-storage', storageBase?.id],
    queryFn: () => basesApi.getStorage(storageBase!.id),
    enabled: Boolean(storageBase?.id),
  });

  const workersQuery = useQuery({
    queryKey: ['base-workers', workersBase?.id],
    queryFn: () => basesApi.getWorkers(workersBase!.id),
    enabled: Boolean(workersBase?.id),
  });

  const feedQuery = useQuery({
    queryKey: ['base-feed-boxes', feedBase?.id],
    queryFn: () => basesApi.getFeedBoxes(feedBase!.id),
    enabled: Boolean(feedBase?.id),
  });

  useEffect(() => {
    setStorageSearch('');
  }, [storageBase?.id]);

  useEffect(() => {
    setWorkerSearch('');
  }, [workersBase?.id]);

  useEffect(() => {
    setFeedSearch('');
  }, [feedBase?.id]);

  const rebuildMutation = useMutation({
    mutationFn: saveIndexApi.rebuild,
    onSuccess: () => {
      setNotice('已触发存档索引重建');
      setActionError(null);
      void queryClient.invalidateQueries({ queryKey: ['bases'] });
    },
    onError: (rebuildError) => {
      setNotice(null);
      setActionError(getErrorMessage(rebuildError));
    },
  });


  const nameMutation = useMutation({
    mutationFn: ({ baseId, name }: { baseId: string; name: string | null }) =>
      name === null ? basesApi.clearCustomName(baseId) : basesApi.updateCustomName(baseId, name),
    onSuccess: (base) => {
      setNotice(base.has_custom_name ? `基地名称已更新为：${base.name}` : '已恢复基地原名');
      setActionError(null);
      void queryClient.invalidateQueries({ queryKey: ['bases'] });
    },
    onError: (mutationError) => {
      setNotice(null);
      setActionError(getErrorMessage(mutationError));
    },
  });

  const cleanMutation = useMutation({
    mutationFn: (base: Base) => basesApi.cleanBase(base.id),
    onSuccess: (result) => {
      setNotice(`已清理基地"${result.base.name}"，世界已保存，实时索引正在刷新`);
      setActionError(null);
      void queryClient.invalidateQueries({ queryKey: ['bases'] });
      void queryClient.invalidateQueries({ queryKey: ['base-detail', selectedBaseId] });
    },
    onError: (cleanError) => {
      setNotice(null);
      setActionError(getErrorMessage(cleanError));
    },
  });

  const bases = basesQuery.data?.items ?? [];
  const indexStatus = basesQuery.data?.status ?? null;
  const summary = basesQuery.data?.summary;
  const loading = basesQuery.isLoading;
  const error = actionError || (basesQuery.error ? getErrorMessage(basesQuery.error) : null);
  const totalItems = summary?.total ?? bases.length;
  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));

  const cleanBase = (base: Base) => {
    if (cleanMutation.isPending) return;
    const confirmed = window.confirm(
      `确认清理基地"${base.name}"吗？

将通过 PalDefender 摧毁坐标附近的整个基地，并请求保存世界。此操作不可撤销。`,
    );
    if (confirmed) cleanMutation.mutate(base);
  };

  const unsupported = async (promise: Promise<{ message: string }>) => {
    const result = await promise;
    setNotice(result.message);
  };

  const copyCoords = async (base: Base) => {
    const cmd = `/teleportto ${base.x.toFixed(0)} ${base.y.toFixed(0)} ${base.z.toFixed(0)}`;
    await navigator.clipboard?.writeText(cmd);
    setNotice(`已复制传送指令：${cmd}`);
  };


  const editBaseName = (base: Base) => {
    const value = window.prompt('请输入基地自定义名称（最多 64 个字符）', base.custom_name || base.name);
    if (value === null) return;
    const name = value.trim();
    if (!name) {
      setActionError('自定义名称不能为空；需要恢复原名时请使用“恢复原名”。');
      return;
    }
    if (name === base.custom_name) return;
    nameMutation.mutate({ baseId: base.id, name });
  };

  const clearBaseName = (base: Base) => {
    if (!base.has_custom_name) return;
    if (!window.confirm(`确认恢复“${base.name}”的原始名称？`)) return;
    nameMutation.mutate({ baseId: base.id, name: null });
  };

  const headers = [
    { key: 'name', label: '基地 / 公会' },
    { key: 'coordinates', label: '世界坐标' },
    { key: 'structures', label: '建筑数' },
    { key: 'pals', label: '工作帕鲁' },
    { key: 'members', label: '在线成员' },
    { key: 'status', label: '防御状态' },
    { key: 'actions', label: '操作', align: 'center' as const },
  ];

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <SaveDataTabs />
      {notice && (
        <div className="rounded-2xl border border-sky-100 bg-sky-50 px-5 py-3.5 text-xs font-semibold text-sky-700">
          <Sparkles size={16} className="mr-2 inline" />
          {notice}
        </div>
      )}
      {error && (
        <div className="rounded-2xl border border-rose-100 bg-rose-50 px-5 py-3.5 text-xs font-semibold text-rose-700">
          <AlertCircle size={16} className="mr-2 inline" />
          {error}
        </div>
      )}

      <div className="grid grid-cols-1 gap-5 md:grid-cols-3">
        <Summary icon={<MapPin size={18} />} label="活跃基地总数" value={`${bases.length} 个`} />
        <Summary icon={<ShieldAlert size={18} />} label="遭袭基地" value={`${bases.filter((base) => base.status === 'Raid').length} 个`} danger={bases.some((base) => base.status === 'Raid')} />
        <Summary icon={<Users size={18} />} label="总建筑数" value={`${bases.reduce((acc, base) => acc + base.structures_count, 0)} 件`} />
      </div>

      <SaveIndexStatusBar
        status={indexStatus}
        loading={basesQuery.isFetching}
        rebuilding={rebuildMutation.isPending}
        onRefresh={() => void basesQuery.refetch()}
        onRebuild={() => rebuildMutation.mutate()}
      />

      <section className="rounded-3xl border border-slate-100 bg-white p-4 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)] sm:p-6">
        {loading && bases.length === 0 ? (
          <div className="py-12 text-center text-xs font-semibold text-slate-400">
            <AlertCircle className="mr-2 inline text-sky-500" size={14} />
            正在获取基地数据...
          </div>
        ) : (
          <DataTable
            headers={headers}
            data={bases}
            searchText={searchText}
            onSearchChange={setSearchText}
            searchPlaceholder="搜索基地名称或所属公会"
            pagination={{
              currentPage: page,
              totalPages,
              totalItems,
              itemsPerPage: pageSize,
              onPageChange: setPage,
            }}
            virtualized
            emptyText={error ? '后端不可用或接口未实现' : '暂无基地'}
            renderCard={(base) => (
              <BaseCard
                key={base.id}
                base={base}
                onCopy={() => copyCoords(base)}
                onRename={() => editBaseName(base)}
                onResetName={() => clearBaseName(base)}
                onStorage={() => setStorageBase(base)}
                onWorkers={() => setWorkersBase(base)}
                onFeed={() => setFeedBase(base)}
                onClean={() => cleanBase(base)}
                onBackup={() => unsupported(basesApi.backupBase(base.id))}
              />
            )}
            renderRow={(base) => (
              <tr key={base.id} className="hover:bg-slate-50/50">
                <td className="px-6 py-4">
                  <div className="min-w-0">
                    <p className="truncate text-xs font-bold text-slate-700">{base.name}</p>
                    {base.has_custom_name && base.raw_name ? (
                      <p className="mt-0.5 truncate text-[10px] font-semibold text-slate-400">原名：{base.raw_name}</p>
                    ) : null}
                    <p className="mt-0.5 text-[10px] font-semibold text-slate-400">{base.guild_name}</p>
                  </div>
                </td>
                <td className="px-6 py-4 font-mono text-xs text-slate-500">
                  {base.x.toFixed(0)}, {base.y.toFixed(0)}, {base.z.toFixed(0)}
                </td>
                <td className="px-6 py-4 text-xs font-semibold text-slate-600">{base.structures_count} 件</td>
                <td className="px-6 py-4 text-xs font-semibold text-slate-600">{base.pals_count} / 20 只</td>
                <td className="px-6 py-4">
                  {base.online_members.length > 0 ? (
                    <div className="flex flex-wrap gap-1.5">
                      {base.online_members.map((member) => (
                        <span key={member} className="rounded-lg border border-sky-100 bg-sky-50 px-2 py-0.5 text-[10px] font-bold text-sky-700">
                          {member}
                        </span>
                      ))}
                    </div>
                  ) : (
                    <span className="text-[10px] font-semibold text-slate-400">无玩家在线</span>
                  )}
                </td>
                <td className="px-6 py-4">
                  <StatusBadge status={base.status} />
                </td>
                <td className="px-6 py-4">
                  <div className="flex justify-center gap-2">
                    <button type="button" onClick={() => copyCoords(base)} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50" aria-label="复制坐标">
                      <Copy size={14} />
                    </button>
                    <button type="button" onClick={() => editBaseName(base)} disabled={nameMutation.isPending} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50 disabled:opacity-50" aria-label="编辑基地名称">
                      <Pencil size={14} />
                    </button>
                    {base.has_custom_name ? (
                      <button type="button" onClick={() => clearBaseName(base)} disabled={nameMutation.isPending} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50 disabled:opacity-50" aria-label="恢复基地原名">
                        <RotateCcw size={14} />
                      </button>
                    ) : null}
                    <button type="button" onClick={() => setStorageBase(base)} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50" aria-label="查看基地仓库">
                      <PackageOpen size={14} />
                    </button>
                    <button type="button" onClick={() => setWorkersBase(base)} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50" aria-label="查看基地工作帕鲁">
                      <Cat size={14} />
                    </button>
                    <button type="button" onClick={() => setFeedBase(base)} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50" aria-label="查看基地饲料箱">
                      <Apple size={14} />
                    </button>
                    <button type="button" onClick={() => cleanBase(base)} className="rounded-lg border border-slate-200 px-3 py-2 text-[10px] font-bold text-slate-500 hover:bg-slate-50">
                      清理
                    </button>
                  </div>
                </td>
              </tr>
            )}
          />
        )}
      </section>
      {feedBase && (
        <BaseFeedBoxDrawer
          base={feedBase}
          data={feedQuery.data}
          loading={feedQuery.isLoading || feedQuery.isFetching}
          error={feedQuery.error ? getErrorMessage(feedQuery.error) : null}
          searchText={feedSearch}
          onSearchChange={setFeedSearch}
          onRetry={() => void feedQuery.refetch()}
          onClose={() => setFeedBase(null)}
        />
      )}
      {workersBase && (
        <BaseWorkerDrawer
          base={workersBase}
          data={workersQuery.data}
          loading={workersQuery.isLoading || workersQuery.isFetching}
          error={workersQuery.error ? getErrorMessage(workersQuery.error) : null}
          searchText={workerSearch}
          onSearchChange={setWorkerSearch}
          onRetry={() => void workersQuery.refetch()}
          onClose={() => setWorkersBase(null)}
        />
      )}
      {storageBase && (
        <BaseStorageDrawer
          base={storageBase}
          data={storageQuery.data}
          loading={storageQuery.isLoading || storageQuery.isFetching}
          error={storageQuery.error ? getErrorMessage(storageQuery.error) : null}
          searchText={storageSearch}
          onSearchChange={setStorageSearch}
          onRetry={() => void storageQuery.refetch()}
          onClose={() => setStorageBase(null)}
        />
      )}
    </div>
  );
};

const Summary: React.FC<{ icon: React.ReactNode; label: string; value: string; danger?: boolean }> = ({
  icon,
  label,
  value,
  danger = false,
}) => (
  <div className="flex items-center justify-between rounded-2xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
    <div>
      <p className="text-[11px] font-semibold text-slate-400">{label}</p>
      <p className={`mt-1 text-2xl font-bold ${danger ? 'text-rose-500' : 'text-slate-800'}`}>{value}</p>
    </div>
    <div className={`flex h-9 w-9 items-center justify-center rounded-xl border ${danger ? 'border-rose-100 bg-rose-50 text-rose-500' : 'border-slate-100 bg-slate-50 text-slate-500'}`}>
      {icon}
    </div>
  </div>
);

const BaseCard: React.FC<{
  base: Base;
  onCopy: () => void;
  onRename: () => void;
  onResetName: () => void;
  onStorage: () => void;
  onWorkers: () => void;
  onFeed: () => void;
  onClean: () => void;
  onBackup: () => void;
}> = ({ base, onCopy, onRename, onResetName, onStorage, onWorkers, onFeed, onClean, onBackup }) => (
  <div className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <p className="truncate text-sm font-bold text-slate-800">{base.name}</p>
        {base.has_custom_name && base.raw_name ? (
          <p className="mt-1 truncate text-[10px] font-semibold text-slate-400">原名：{base.raw_name}</p>
        ) : null}
        <p className="mt-1 truncate text-[11px] font-semibold text-slate-400">{base.guild_name}</p>
      </div>
      <StatusBadge status={base.status} />
    </div>
    <div className="mt-4 grid grid-cols-2 gap-2 text-[11px] font-semibold text-slate-500">
      <span>建筑: {base.structures_count}</span>
      <span>帕鲁: {base.pals_count} / 20</span>
      <span className="col-span-2 font-mono">
        坐标: {base.x.toFixed(0)}, {base.y.toFixed(0)}, {base.z.toFixed(0)}
      </span>
      <span className="col-span-2 truncate">在线成员: {base.online_members.join(', ') || '无'}</span>
    </div>
    <div className="mt-4 grid grid-cols-2 gap-2">
      <button type="button" onClick={onRename} className="rounded-xl border border-sky-200 bg-sky-50 py-2 text-xs font-bold text-sky-700">
        编辑名称
      </button>
      {base.has_custom_name ? (
        <button type="button" onClick={onResetName} className="rounded-xl border border-slate-200 py-2 text-xs font-bold text-slate-600">
          恢复原名
        </button>
      ) : (
        <button type="button" onClick={onCopy} className="rounded-xl border border-slate-200 py-2 text-xs font-bold text-slate-600">
          复制坐标
        </button>
      )}
      <button type="button" onClick={onStorage} className="rounded-xl border border-sky-200 bg-sky-50 py-2 text-xs font-bold text-sky-700">
        查看仓库
      </button>
      <button type="button" onClick={onWorkers} className="rounded-xl border border-sky-200 bg-sky-50 py-2 text-xs font-bold text-sky-700">
        工作帕鲁
      </button>
      <button type="button" onClick={onFeed} className="rounded-xl border border-amber-200 bg-amber-50 py-2 text-xs font-bold text-amber-700">
        饲料箱
      </button>
      <button type="button" onClick={onClean} className="rounded-xl border border-slate-200 py-2 text-xs font-bold text-slate-600">
        清理
      </button>
      <button type="button" onClick={onBackup} className="rounded-xl border border-slate-200 py-2 text-xs font-bold text-slate-600">
        备份
      </button>
    </div>
  </div>
);

const BaseFeedBoxDrawer: React.FC<{
  base: Base;
  data?: BaseFeedBoxesResponse;
  loading: boolean;
  error: string | null;
  searchText: string;
  onSearchChange: (value: string) => void;
  onRetry: () => void;
  onClose: () => void;
}> = ({ base, data, loading, error, searchText, onSearchChange, onRetry, onClose }) => {
  const boxes = data?.feed_boxes ?? [];
  const items = data?.items ?? [];
  const summary = data?.summary ?? { box_count: 0, empty_box_count: 0, occupied_slots: 0, total_items: 0, item_types: 0 };
  const query = searchText.trim().toLocaleLowerCase();
  const visibleItems = items.filter((item) => !query || [item.item_name, item.item_id].some((value) => value.toLocaleLowerCase().includes(query)));
  const visibleBoxes = boxes
    .map((box) => {
      const boxMatches = Boolean(query) && [box.container_name, box.container_type, box.container_id]
        .some((value) => value.toLocaleLowerCase().includes(query));
      return {
        ...box,
        slots: box.slots.filter((slot) => !query || boxMatches || [slot.item_name, slot.item_id]
          .some((value) => value.toLocaleLowerCase().includes(query))),
      };
    })
    .filter((box) => !query || box.slots.length > 0 || [box.container_name, box.container_type, box.container_id]
      .some((value) => value.toLocaleLowerCase().includes(query)));

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-slate-900/20 px-3 py-3 backdrop-blur-sm sm:px-6 sm:py-6">
      <aside className="flex h-full w-full max-w-3xl flex-col rounded-2xl border border-slate-100 bg-white shadow-2xl">
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-5 py-4">
          <div className="min-w-0">
            <p className="truncate text-sm font-bold text-slate-800">{data?.base.name || base.name} · 饲料箱摘要</p>
            <p className="mt-1 truncate font-mono text-[10px] text-slate-400">{base.id}</p>
          </div>
          <button type="button" onClick={onClose} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50" aria-label="关闭饲料箱摘要">
            <X size={14} />
          </button>
        </div>

        <div className="border-b border-slate-100 px-5 py-4">
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
            <StorageSummary label="饲料箱" value={summary.box_count} />
            <StorageSummary label="空箱" value={summary.empty_box_count} />
            <StorageSummary label="占用格" value={summary.occupied_slots} />
            <StorageSummary label="物品种类" value={summary.item_types} />
            <StorageSummary label="物品总数" value={summary.total_items} />
          </div>
          <label className="relative mt-4 block">
            <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
            <input
              type="search"
              value={searchText}
              onChange={(event) => onSearchChange(event.target.value)}
              placeholder="搜索物品、饲料箱名称或内部 ID"
              className="w-full rounded-xl border border-slate-200 bg-slate-50 py-2.5 pl-9 pr-3 text-xs font-semibold text-slate-700 outline-none focus:border-amber-300 focus:bg-white"
            />
          </label>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {data?.status.stale ? (
            <div className="mb-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs font-semibold text-amber-800">
              当前显示的是缓存存档索引，饲料箱内容可能稍有延迟。
            </div>
          ) : null}
          {error ? (
            <div className="rounded-xl border border-rose-100 bg-rose-50 px-4 py-4 text-xs font-semibold text-rose-700">
              <p>{error}</p>
              <button type="button" onClick={onRetry} className="mt-3 inline-flex items-center gap-2 rounded-lg border border-rose-200 bg-white px-3 py-2 text-[11px] font-bold">
                <RefreshCw size={13} />
                重试
              </button>
            </div>
          ) : loading && !data ? (
            <div className="py-16 text-center text-xs font-semibold text-slate-400">
              <RefreshCw size={15} className="mr-2 inline animate-spin text-amber-500" />
              正在读取基地饲料箱...
            </div>
          ) : boxes.length === 0 ? (
            <div className="py-16 text-center text-xs font-semibold text-slate-400">
              <Apple size={18} className="mx-auto mb-3 text-slate-300" />
              该基地没有可识别的饲料箱
            </div>
          ) : query && visibleItems.length === 0 && visibleBoxes.length === 0 ? (
            <div className="py-16 text-center text-xs font-semibold text-slate-400">没有匹配的物品或饲料箱</div>
          ) : (
            <div className="space-y-5">
              {visibleItems.length > 0 ? (
                <section>
                  <div className="mb-3 flex items-center gap-2">
                    <Apple size={15} className="text-amber-500" />
                    <h3 className="text-xs font-black text-slate-700">合并库存</h3>
                  </div>
                  <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                    {visibleItems.map((item) => (
                      <article key={item.item_id} className="rounded-xl border border-amber-100 bg-amber-50/40 px-3 py-3">
                        <div className="flex items-start justify-between gap-3">
                          <div className="flex min-w-0 items-center gap-3">
                            <StorageItemIcon icon={item.item_icon} name={item.item_name} />
                            <div className="min-w-0">
                              <p className="truncate text-xs font-bold text-slate-700">{item.item_name}</p>
                              <p className="mt-1 truncate font-mono text-[9px] text-slate-400" title={item.item_id}>{item.item_id}</p>
                              <p className="mt-1 text-[9px] font-semibold text-slate-400">分布于 {item.box_count} 个饲料箱</p>
                            </div>
                          </div>
                          <span className="shrink-0 rounded-lg border border-amber-200 bg-white px-2 py-1 text-xs font-black text-amber-700">×{item.count}</span>
                        </div>
                      </article>
                    ))}
                  </div>
                </section>
              ) : null}

              <section>
                <div className="mb-3 flex items-center gap-2">
                  <PackageOpen size={15} className="text-sky-500" />
                  <h3 className="text-xs font-black text-slate-700">按饲料箱查看</h3>
                </div>
                <div className="space-y-3">
                  {visibleBoxes.map((box, index) => (
                    <article key={box.container_id} className="rounded-2xl border border-slate-100 bg-slate-50/60 p-4">
                      <div className="flex items-start justify-between gap-3">
                        <div className="min-w-0">
                          <p className="truncate text-xs font-bold text-slate-700">{box.container_name || `饲料箱 ${index + 1}`}</p>
                          <p className="mt-1 truncate font-mono text-[9px] text-slate-400" title={box.container_type}>{box.container_type}</p>
                        </div>
                        <span className="rounded-lg border border-slate-100 bg-white px-2 py-1 text-[9px] font-bold text-slate-500">{box.slots.length} 格</span>
                      </div>
                      {box.slots.length === 0 ? (
                        <p className="mt-4 rounded-xl border border-dashed border-slate-200 bg-white px-3 py-4 text-center text-[10px] font-semibold text-slate-400">该饲料箱为空</p>
                      ) : (
                        <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2">
                          {box.slots.map((slot) => (
                            <div key={`${box.container_id}:${slot.slot}`} className="rounded-xl border border-slate-100 bg-white px-3 py-2.5">
                              <div className="flex items-start justify-between gap-3">
                                <div className="flex min-w-0 items-center gap-3">
                                  <StorageItemIcon icon={slot.item_icon} name={slot.item_name} />
                                  <div className="min-w-0">
                                    <p className="truncate text-xs font-bold text-slate-700">{slot.item_name}</p>
                                    <p className="mt-1 truncate font-mono text-[9px] text-slate-400" title={slot.item_id}>{slot.item_id}</p>
                                  </div>
                                </div>
                                <span className="shrink-0 rounded-lg border border-sky-100 bg-sky-50 px-2 py-1 text-xs font-black text-sky-700">×{slot.count}</span>
                              </div>
                              <p className="mt-2 text-[9px] font-semibold text-slate-400">槽位 {slot.slot}</p>
                            </div>
                          ))}
                        </div>
                      )}
                      <p className="mt-3 truncate font-mono text-[8px] text-slate-300" title={box.container_id}>{box.container_id}</p>
                    </article>
                  ))}
                </div>
              </section>
            </div>
          )}
        </div>
      </aside>
    </div>
  );
};

const BaseWorkerDrawer: React.FC<{
  base: Base;
  data?: BaseWorkersResponse;
  loading: boolean;
  error: string | null;
  searchText: string;
  onSearchChange: (value: string) => void;
  onRetry: () => void;
  onClose: () => void;
}> = ({ base, data, loading, error, searchText, onSearchChange, onRetry, onClose }) => {
  const workers = data?.workers ?? [];
  const summary = data?.summary ?? { total: 0, average_level: 0, max_level: 0, named_count: 0, species_count: 0 };
  const query = searchText.trim().toLocaleLowerCase();
  const visibleWorkers = workers.filter((worker) => {
    if (!query) return true;
    return [worker.name, worker.nickname, worker.species_name, worker.character_id, worker.instance_id, ...worker.passives]
      .some((value) => value.toLocaleLowerCase().includes(query));
  });

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-slate-900/20 px-3 py-3 backdrop-blur-sm sm:px-6 sm:py-6">
      <aside className="flex h-full w-full max-w-2xl flex-col rounded-2xl border border-slate-100 bg-white shadow-2xl">
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-5 py-4">
          <div className="min-w-0">
            <p className="truncate text-sm font-bold text-slate-800">{data?.base.name || base.name} · 工作帕鲁</p>
            <p className="mt-1 truncate font-mono text-[10px] text-slate-400">{base.id}</p>
          </div>
          <button type="button" onClick={onClose} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50" aria-label="关闭工作帕鲁列表">
            <X size={14} />
          </button>
        </div>

        <div className="border-b border-slate-100 px-5 py-4">
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <WorkerSummary label="总数" value={summary.total} />
            <WorkerSummary label="平均等级" value={Math.round(summary.average_level * 10) / 10} />
            <WorkerSummary label="最高等级" value={summary.max_level} />
            <WorkerSummary label="种类" value={summary.species_count} />
          </div>
          <label className="relative mt-4 block">
            <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
            <input
              type="search"
              value={searchText}
              onChange={(event) => onSearchChange(event.target.value)}
              placeholder="搜索昵称、种类、内部 ID 或被动词条"
              className="w-full rounded-xl border border-slate-200 bg-slate-50 py-2.5 pl-9 pr-3 text-xs font-semibold text-slate-700 outline-none focus:border-sky-300 focus:bg-white"
            />
          </label>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {data?.status.stale ? (
            <div className="mb-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs font-semibold text-amber-800">
              当前显示的是缓存存档索引，工作帕鲁列表可能稍有延迟。
            </div>
          ) : null}
          {error ? (
            <div className="rounded-xl border border-rose-100 bg-rose-50 px-4 py-4 text-xs font-semibold text-rose-700">
              <p>{error}</p>
              <button type="button" onClick={onRetry} className="mt-3 inline-flex items-center gap-2 rounded-lg border border-rose-200 bg-white px-3 py-2 text-[11px] font-bold">
                <RefreshCw size={13} />
                重试
              </button>
            </div>
          ) : loading && !data ? (
            <div className="py-16 text-center text-xs font-semibold text-slate-400">
              <RefreshCw size={15} className="mr-2 inline animate-spin text-sky-500" />
              正在读取基地工作帕鲁...
            </div>
          ) : visibleWorkers.length === 0 ? (
            <div className="py-16 text-center text-xs font-semibold text-slate-400">
              <Cat size={20} className="mx-auto mb-3 text-slate-300" />
              {query ? '没有匹配的工作帕鲁' : '该基地没有可读取的工作帕鲁'}
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              {visibleWorkers.map((worker) => (
                <article key={worker.instance_id} className="rounded-2xl border border-slate-100 bg-slate-50/60 p-4">
                  <div className="flex items-start gap-3">
                    <PalIcon characterID={worker.character_id} name={worker.name} className="h-12 w-12 rounded-xl border border-slate-100" />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0">
                          <p className="truncate text-xs font-bold text-slate-800">{worker.name}</p>
                          {worker.nickname && worker.nickname !== worker.species_name ? (
                            <p className="mt-0.5 truncate text-[10px] font-semibold text-slate-400">种类：{worker.species_name}</p>
                          ) : null}
                        </div>
                        <span className="shrink-0 rounded-lg border border-sky-100 bg-sky-50 px-2 py-1 text-[10px] font-black text-sky-700">Lv.{worker.level}</span>
                      </div>
                      <p className="mt-1 truncate font-mono text-[9px] text-slate-400" title={worker.character_id}>{worker.character_id}</p>
                    </div>
                  </div>
                  <div className="mt-3 flex flex-wrap gap-1.5 text-[9px] font-bold">
                    {worker.gender ? <span className="rounded-lg border border-slate-200 bg-white px-2 py-1 text-slate-500">{formatWorkerGender(worker.gender)}</span> : null}
                    <span className="rounded-lg border border-slate-200 bg-white px-2 py-1 text-slate-500">Rank {worker.rank}</span>
                    <span className="rounded-lg border border-slate-200 bg-white px-2 py-1 text-slate-500">{formatWorkerStatus(worker.status)}</span>
                    {worker.on_expedition ? <span className="rounded-lg border border-amber-200 bg-amber-50 px-2 py-1 text-amber-700">远征中</span> : null}
                  </div>
                  {worker.passives.length > 0 ? (
                    <div className="mt-3 flex flex-wrap gap-1.5">
                      {worker.passives.slice(0, 6).map((passive, index) => (
                        <span key={`${worker.instance_id}:${worker.raw_passives[index] || passive}`} className="rounded-lg border border-violet-100 bg-violet-50 px-2 py-1 text-[9px] font-bold text-violet-700">{passive}</span>
                      ))}
                    </div>
                  ) : (
                    <p className="mt-3 text-[9px] font-semibold text-slate-400">索引中没有被动词条</p>
                  )}
                  <p className="mt-3 truncate font-mono text-[8px] text-slate-300" title={worker.instance_id}>{worker.instance_id}</p>
                </article>
              ))}
            </div>
          )}
        </div>
      </aside>
    </div>
  );
};

const formatWorkerGender = (gender: string) => {
  const normalized = gender.trim().toLocaleLowerCase();
  if (normalized === 'male' || normalized === 'm') return '雄性';
  if (normalized === 'female' || normalized === 'f') return '雌性';
  return gender;
};

const formatWorkerStatus = (status: string) => {
  const labels: Record<string, string> = {
    healthy: '健康',
    working: '工作中',
    injured: '受伤',
    battling: '战斗中',
    dead: '死亡',
    unknown: '状态未知',
  };
  return labels[status.trim().toLocaleLowerCase()] || status || '状态未知';
};

const WorkerSummary: React.FC<{ label: string; value: number }> = ({ label, value }) => (
  <div className="rounded-xl border border-slate-100 bg-slate-50 px-3 py-2.5 text-center">
    <p className="text-[10px] font-semibold text-slate-400">{label}</p>
    <p className="mt-1 text-lg font-black text-slate-700">{value}</p>
  </div>
);

const BaseStorageDrawer: React.FC<{
  base: Base;
  data?: BaseStorageResponse;
  loading: boolean;
  error: string | null;
  searchText: string;
  onSearchChange: (value: string) => void;
  onRetry: () => void;
  onClose: () => void;
}> = ({ base, data, loading, error, searchText, onSearchChange, onRetry, onClose }) => {
  const containers = data?.containers ?? [];
  const occupiedSlots = containers.flatMap((container) => container.slots).filter((slot) => slot.count > 0);
  const totalItems = occupiedSlots.reduce((sum, slot) => sum + slot.count, 0);
  const query = searchText.trim().toLocaleLowerCase();
  const visibleContainers = containers
    .map((container) => {
      const containerMatches = Boolean(query) && (
        container.container_name.toLocaleLowerCase().includes(query)
        || container.container_type.toLocaleLowerCase().includes(query)
      );
      return {
        ...container,
        slots: container.slots.filter((slot) => {
          if (slot.count <= 0) return false;
          if (!query || containerMatches) return true;
          return slot.item_name.toLocaleLowerCase().includes(query) || slot.item_id.toLocaleLowerCase().includes(query);
        }),
      };
    })
    .filter((container) => container.slots.length > 0);

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-slate-900/20 px-3 py-3 backdrop-blur-sm sm:px-6 sm:py-6">
      <aside className="flex h-full w-full max-w-2xl flex-col rounded-2xl border border-slate-100 bg-white shadow-2xl">
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-5 py-4">
          <div className="min-w-0">
            <p className="truncate text-sm font-bold text-slate-800">{base.name} · 仓库</p>
            <p className="mt-1 truncate font-mono text-[10px] text-slate-400">{base.id}</p>
          </div>
          <button type="button" onClick={onClose} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50" aria-label="关闭仓库">
            <X size={14} />
          </button>
        </div>

        <div className="border-b border-slate-100 px-5 py-4">
          <div className="grid grid-cols-3 gap-3">
            <StorageSummary label="容器" value={containers.length} />
            <StorageSummary label="占用格" value={occupiedSlots.length} />
            <StorageSummary label="物品总数" value={totalItems} />
          </div>
          <label className="relative mt-4 block">
            <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
            <input
              type="search"
              value={searchText}
              onChange={(event) => onSearchChange(event.target.value)}
              placeholder="搜索容器或物品名称 / ID"
              className="w-full rounded-xl border border-slate-200 bg-slate-50 py-2.5 pl-9 pr-3 text-xs font-semibold text-slate-700 outline-none focus:border-sky-300 focus:bg-white"
            />
          </label>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {data?.status.stale ? (
            <div className="mb-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs font-semibold text-amber-800">
              当前显示的是缓存存档索引，仓库内容可能稍有延迟。
            </div>
          ) : null}
          {error ? (
            <div className="rounded-xl border border-rose-100 bg-rose-50 px-4 py-4 text-xs font-semibold text-rose-700">
              <p>{error}</p>
              <button type="button" onClick={onRetry} className="mt-3 inline-flex items-center gap-2 rounded-lg border border-rose-200 bg-white px-3 py-2 text-[11px] font-bold">
                <RefreshCw size={13} />
                重试
              </button>
            </div>
          ) : loading && !data ? (
            <div className="py-16 text-center text-xs font-semibold text-slate-400">
              <RefreshCw size={15} className="mr-2 inline animate-spin text-sky-500" />
              正在读取基地仓库...
            </div>
          ) : visibleContainers.length === 0 ? (
            <div className="py-16 text-center text-xs font-semibold text-slate-400">
              <PackageOpen size={18} className="mx-auto mb-3 text-slate-300" />
              {query ? '没有匹配的物品' : '该基地没有可读取的仓库物品'}
            </div>
          ) : (
            <div className="space-y-4">
              {visibleContainers.map((container, containerIndex) => (
                <section key={container.container_id} className="rounded-2xl border border-slate-100 bg-slate-50/60 p-4">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <PackageOpen size={15} className="shrink-0 text-sky-500" />
                        <p className="truncate text-xs font-bold text-slate-700">{container.container_name || `容器 ${containerIndex + 1}`}</p>
                      </div>
                      <p className="mt-1 truncate font-mono text-[9px] text-slate-400" title={container.container_type}>
                        {container.container_type || container.owner_type}
                      </p>
                    </div>
                    <p className="max-w-[52%] truncate font-mono text-[9px] text-slate-400" title={container.container_id}>{container.container_id}</p>
                  </div>
                  <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2">
                    {container.slots.map((slot) => (
                      <div key={`${container.container_id}:${slot.slot}`} className="rounded-xl border border-slate-100 bg-white px-3 py-2.5">
                        <div className="flex items-start justify-between gap-3">
                          <div className="flex min-w-0 items-center gap-3">
                            <StorageItemIcon icon={slot.item_icon} name={slot.item_name} />
                            <div className="min-w-0">
                              <p className="truncate text-xs font-bold text-slate-700">{slot.item_name}</p>
                              <p className="mt-1 truncate font-mono text-[9px] text-slate-400" title={slot.item_id}>{slot.item_id}</p>
                            </div>
                          </div>
                          <span className="shrink-0 rounded-lg border border-sky-100 bg-sky-50 px-2 py-1 text-xs font-black text-sky-700">×{slot.count}</span>
                        </div>
                        <p className="mt-2 text-[9px] font-semibold text-slate-400">
                          槽位 {slot.slot}{slot.durability == null ? '' : ` · 耐久 ${Math.round(slot.durability)}`}
                        </p>
                      </div>
                    ))}
                  </div>
                </section>
              ))}
            </div>
          )}
        </div>
      </aside>
    </div>
  );
};


const StorageItemIcon: React.FC<{ icon: string; name: string }> = ({ icon, name }) => {
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    setFailed(false);
  }, [icon]);

  if (!icon || failed) {
    return (
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-slate-100 bg-slate-50 text-slate-300" aria-hidden="true">
        <Package size={18} />
      </div>
    );
  }

  return (
    <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-slate-100 bg-slate-50 p-1">
      <img
        src={`/assets/items/${encodeURIComponent(icon)}.webp`}
        alt={`${name}图标`}
        loading="lazy"
        className="h-full w-full object-contain"
        onError={() => setFailed(true)}
      />
    </div>
  );
};

const StorageSummary: React.FC<{ label: string; value: number }> = ({ label, value }) => (
  <div className="rounded-xl border border-slate-100 bg-slate-50 px-3 py-2.5 text-center">
    <p className="text-[10px] font-semibold text-slate-400">{label}</p>
    <p className="mt-1 text-lg font-black text-slate-700">{value}</p>
  </div>
);
