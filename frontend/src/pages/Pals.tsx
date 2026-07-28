import React, { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertCircle, Hammer, HeartPulse, MapPin, RefreshCw, SlidersHorizontal, Trash2 } from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { palsApi } from '../api/pals';
import { saveIndexApi } from '../api/saveIndex';
import { useServerStore } from '../store/useServerStore';
import type { Pal } from '../types';
import { DataTable } from '../components/ui/DataTable';
import { StatusBadge } from '../components/ui/StatusBadge';
import { SaveIndexStatusBar } from '../components/ui/SaveIndexStatusBar';
import { SaveDataTabs } from '../components/ui/SaveDataTabs';
import { useDebouncedValue } from '../hooks/useDebouncedValue';

const suitabilityText: Record<string, string> = {
  Handiwork: '手工',
  Transport: '搬运',
  Watering: '浇水',
  Planting: '播种',
  Generating: '发电',
  Gathering: '采集',
  Lumbering: '伐木',
  Mining: '采矿',
  Cooling: '冷却',
  Farming: '牧场',
  Medicine: '制药',
};

const pageSize = 50;
const rarityText: Record<Pal['rarity'], string> = {
  Common: '普通',
  Rare: '稀有',
  Boss: '首领',
};
const statusFilterByTab: Record<string, string | undefined> = {
  all: undefined,
  working: 'Working',
  battling: 'Battling',
  injured: 'Injured',
  dead: 'Dead',
};

export const Pals: React.FC = () => {
  const { refreshKey } = useServerStore();
  const queryClient = useQueryClient();
  const [searchText, setSearchText] = useState('');
  const [activeFilterTab, setActiveFilterTab] = useState('all');
  const [page, setPage] = useState(1);
  const [minLevel, setMinLevel] = useState(0);
  const [minStars, setMinStars] = useState(0);
  const [minIVAverage, setMinIVAverage] = useState(0);
  const [gender, setGender] = useState('');
  const [location, setLocation] = useState('');
  const [passive, setPassive] = useState('');
  const [sort, setSort] = useState<'level_desc' | 'iv_desc' | 'stars_desc' | 'name_asc'>('level_desc');
  const [notice, setNotice] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const debouncedSearch = useDebouncedValue(searchText, 250);
  const debouncedPassive = useDebouncedValue(passive, 250);
  const statusFilter = statusFilterByTab[activeFilterTab];

  useEffect(() => {
    setPage(1);
  }, [activeFilterTab, debouncedSearch, debouncedPassive, gender, location, minIVAverage, minLevel, minStars, sort]);

  const palsQuery = useQuery({
    queryKey: ['pals', { page, q: debouncedSearch, status: statusFilter, minLevel, minStars, minIVAverage, gender, location, passive: debouncedPassive, sort, refreshKey }],
    queryFn: () =>
      palsApi.getPalsList({
        limit: pageSize,
        offset: (page - 1) * pageSize,
        q: debouncedSearch,
        status: statusFilter,
        min_level: minLevel || undefined,
        min_stars: minStars || undefined,
        min_iv_average: minIVAverage || undefined,
        gender: gender || undefined,
        location: location || undefined,
        passive: debouncedPassive || undefined,
        sort,
      }),
    placeholderData: (previous) => previous,
  });

  const rebuildMutation = useMutation({
    mutationFn: saveIndexApi.rebuild,
    onSuccess: () => {
      setNotice('已触发存档索引重建');
      setActionError(null);
      void queryClient.invalidateQueries({ queryKey: ['pals'] });
    },
    onError: (rebuildError) => {
      setNotice(null);
      setActionError(getErrorMessage(rebuildError));
    },
  });

  const pals = palsQuery.data?.items ?? [];
  const indexStatus = palsQuery.data?.status ?? null;
  const summary = palsQuery.data?.summary;
  const loading = palsQuery.isLoading;
  const error = actionError || (palsQuery.error ? getErrorMessage(palsQuery.error) : null);
  const totalItems = summary?.total ?? pals.length;
  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));
  const filtersActive = minLevel > 0 || minStars > 0 || minIVAverage > 0 || gender !== '' || location !== '' || passive.trim() !== '' || sort !== 'level_desc';
  const resetFilters = () => {
    setMinLevel(0);
    setMinStars(0);
    setMinIVAverage(0);
    setGender('');
    setLocation('');
    setPassive('');
    setSort('level_desc');
  };

  const unsupported = async (promise: Promise<{ message: string }>) => {
    const result = await promise;
    setNotice(result.message);
  };

  const headers = [
    { key: 'name', label: '帕鲁 / 稀有度' },
    { key: 'level', label: '等级' },
    { key: 'quality', label: '星级 / 个体值' },
    { key: 'health', label: '生命值' },
    { key: 'suitability', label: '工作适应性' },
    { key: 'owner', label: '所属玩家' },
    { key: 'location', label: '终端位置' },
    { key: 'status', label: '状态' },
    { key: 'actions', label: '操作', align: 'center' as const },
  ];

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <SaveDataTabs />
      {notice && (
        <div className="rounded-2xl border border-amber-100 bg-amber-50 px-5 py-3 text-xs font-semibold text-amber-800">
          <AlertCircle className="mr-2 inline" size={14} />
          {notice}
        </div>
      )}
      {error && (
        <div className="rounded-2xl border border-rose-100 bg-rose-50 px-5 py-3 text-xs font-semibold text-rose-700">
          <AlertCircle className="mr-2 inline" size={14} />
          {error}
        </div>
      )}

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Summary label="全部帕鲁" value={pals.length} tone="emerald" />
        <Summary label="工作中" value={pals.filter((pal) => pal.status === 'Working').length} tone="sky" />
        <Summary label="受伤" value={pals.filter((pal) => pal.status === 'Injured').length} tone="amber" />
        <Summary label="死亡" value={pals.filter((pal) => pal.status === 'Dead').length} tone="rose" />
      </div>

      <SaveIndexStatusBar
        status={indexStatus}
        loading={palsQuery.isFetching}
        rebuilding={rebuildMutation.isPending}
        onRefresh={() => void palsQuery.refetch()}
        onRebuild={() => rebuildMutation.mutate()}
      />

      <section className="rounded-3xl border border-slate-100 bg-white p-4 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)] sm:p-5">
        <div className="mb-4 flex items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-xs font-bold text-slate-700">
            <SlidersHorizontal size={15} className="text-sky-500" />
            多项筛选
          </div>
          <button type="button" disabled={!filtersActive} onClick={resetFilters} className="text-[11px] font-bold text-sky-600 disabled:text-slate-300">
            重置筛选
          </button>
        </div>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-7">
          <FilterNumber label="最低等级" value={minLevel} max={65} onChange={setMinLevel} />
          <FilterNumber label="最低星级" value={minStars} max={4} onChange={setMinStars} />
          <FilterNumber label="最低平均 IV" value={minIVAverage} max={100} onChange={setMinIVAverage} />
          <label className="flex flex-col gap-1.5 text-[10px] font-bold text-slate-500">
            性别
            <select value={gender} onChange={(event) => setGender(event.target.value)} className="rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs text-slate-700">
              <option value="">全部</option>
              <option value="male">雄性</option>
              <option value="female">雌性</option>
              <option value="wildcard">通配</option>
            </select>
          </label>
          <label className="flex flex-col gap-1.5 text-[10px] font-bold text-slate-500">
            位置
            <select value={location} onChange={(event) => setLocation(event.target.value)} className="rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs text-slate-700">
              <option value="">全部</option>
              <option value="storage">帕鲁终端</option>
              <option value="party">队伍</option>
              <option value="base">据点工作</option>
              <option value="expedition">远征</option>
              <option value="unknown">未知</option>
            </select>
          </label>
          <label className="flex flex-col gap-1.5 text-[10px] font-bold text-slate-500">
            被动词条（逗号分隔）
            <input value={passive} onChange={(event) => setPassive(event.target.value)} placeholder="例如：工匠精神,认真" className="rounded-xl border border-slate-200 px-3 py-2 text-xs text-slate-700" />
          </label>
          <label className="flex flex-col gap-1.5 text-[10px] font-bold text-slate-500">
            排序
            <select value={sort} onChange={(event) => setSort(event.target.value as typeof sort)} className="rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs text-slate-700">
              <option value="level_desc">等级从高到低</option>
              <option value="iv_desc">平均 IV 从高到低</option>
              <option value="stars_desc">星级从高到低</option>
              <option value="name_asc">名称排序</option>
            </select>
          </label>
        </div>
      </section>

      <section className="rounded-3xl border border-slate-100 bg-white p-4 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)] sm:p-6">
        {loading && pals.length === 0 ? (
          <div className="py-12 text-center text-xs font-semibold text-slate-400">
            <RefreshCw className="mr-2 inline animate-spin text-sky-500" size={14} />
            正在获取帕鲁数据...
          </div>
        ) : (
          <DataTable
            headers={headers}
            data={pals}
            searchText={searchText}
            onSearchChange={setSearchText}
            searchPlaceholder="搜索帕鲁名称或所属玩家"
            tabs={[
              { id: 'all', label: '全部' },
              { id: 'working', label: '工作中' },
              { id: 'battling', label: '战斗中' },
              { id: 'injured', label: '受伤' },
              { id: 'dead', label: '死亡' },
            ]}
            activeTab={activeFilterTab}
            onTabChange={setActiveFilterTab}
            pagination={{
              currentPage: page,
              totalPages,
              totalItems,
              itemsPerPage: pageSize,
              onPageChange: setPage,
            }}
            virtualized
            emptyText={error ? '后端不可用或接口未实现' : '暂无帕鲁'}
            renderCard={(pal) => (
              <PalCard
                key={pal.id}
                pal={pal}
                onHeal={() => unsupported(palsApi.heal(pal.id))}
                onDelete={() => unsupported(palsApi.delete(pal.id))}
              />
            )}
            renderRow={(pal) => {
              const hpPercent = Math.min(100, Math.max(0, (pal.health / Math.max(1, pal.max_health)) * 100));
              return (
                <tr key={pal.id} className="hover:bg-slate-50/50">
                  <td className="px-6 py-4">
                    <PalIdentity pal={pal} />
                  </td>
                  <td className="px-6 py-4 text-xs font-bold text-slate-600">Lv.{pal.level}</td>
                  <td className="px-6 py-4">
                    <PalQuality pal={pal} />
                  </td>
                  <td className="px-6 py-4">
                    <HealthBar pal={pal} hpPercent={hpPercent} />
                  </td>
                  <td className="px-6 py-4">
                    <Suitability pal={pal} />
                  </td>
                  <td className="px-6 py-4">
                    <div className="min-w-0">
                      <p className="truncate text-xs font-bold text-slate-600">{pal.owner_nickname}</p>
                      <p className="truncate font-mono text-[9px] text-slate-400">{pal.owner_steam_id}</p>
                    </div>
                  </td>
                  <td className="px-6 py-4">
                    <PalLocation pal={pal} />
                  </td>
                  <td className="px-6 py-4">
                    <StatusBadge status={pal.status} />
                  </td>
                  <td className="px-6 py-4">
                    <div className="flex justify-center gap-2">
                      <button
                        type="button"
                        onClick={() => unsupported(palsApi.heal(pal.id))}
                        className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50"
                        aria-label="治疗帕鲁"
                      >
                        <HeartPulse size={14} />
                      </button>
                      <button
                        type="button"
                        onClick={() => unsupported(palsApi.delete(pal.id))}
                        className="rounded-lg border border-rose-200 p-2 text-rose-500 hover:bg-rose-50"
                        aria-label="释放帕鲁"
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  </td>
                </tr>
              );
            }}
          />
        )}
      </section>
    </div>
  );
};

const Summary: React.FC<{ label: string; value: number; tone: 'emerald' | 'sky' | 'amber' | 'rose' }> = ({
  label,
  value,
  tone,
}) => {
  const dot = {
    emerald: 'bg-emerald-500',
    sky: 'bg-sky-500',
    amber: 'bg-amber-500',
    rose: 'bg-rose-500',
  }[tone];
  return (
    <div className="flex items-center gap-3 rounded-2xl border border-slate-100 bg-white px-4 py-3 shadow-sm">
      <span className={`h-2.5 w-2.5 rounded-full ${dot}`} />
      <span className="text-xs font-semibold text-slate-500">
        {label}: {value}
      </span>
    </div>
  );
};

const PalIdentity: React.FC<{ pal: Pal }> = ({ pal }) => (
  <div className="flex min-w-0 items-center gap-3" title={pal.character_id}>
    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl border border-slate-200/50 bg-slate-100 text-xs font-semibold text-slate-600">
      {pal.name.charAt(0)}
    </div>
    <div className="min-w-0">
      <p className="truncate text-xs font-bold text-slate-700">{pal.name}</p>
      <span className="mt-1 inline-flex rounded-md border border-slate-200 bg-slate-50 px-1.5 py-0.5 text-[9px] font-bold text-slate-500">
        {pal.nickname && pal.species_name ? `${pal.species_name} · ` : ''}
        {pal.rarity_name || rarityText[pal.rarity]}
      </span>
    </div>
  </div>
);

const HealthBar: React.FC<{ pal: Pal; hpPercent: number }> = ({ pal, hpPercent }) => (
  <div className="flex w-32 max-w-full flex-col gap-1.5">
    <div className="flex justify-between text-[10px] font-bold text-slate-500">
      <span>
        {pal.health} / {pal.max_health}
      </span>
      <span>{hpPercent.toFixed(0)}%</span>
    </div>
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-100">
      <div
        style={{ width: `${hpPercent}%` }}
        className={`h-full rounded-full ${pal.status === 'Dead' ? 'bg-slate-300' : hpPercent < 30 ? 'bg-rose-500' : hpPercent < 60 ? 'bg-amber-500' : 'bg-emerald-500'}`}
      />
    </div>
  </div>
);

const Suitability: React.FC<{ pal: Pal }> = ({ pal }) => (
  <div className="flex max-w-[220px] flex-wrap gap-1">
    {pal.work_suitability.map((work) => (
      <span
        key={`${work.type}-${work.level}`}
        className="inline-flex items-center gap-0.5 rounded-lg border border-slate-200/50 bg-slate-50 px-1.5 py-0.5 text-[9px] font-semibold text-slate-500"
      >
        <Hammer size={8} />
        {suitabilityText[work.type] || work.type} Lv.{work.level}
      </span>
    ))}
  </div>
);

const FilterNumber: React.FC<{ label: string; value: number; max: number; onChange: (value: number) => void }> = ({ label, value, max, onChange }) => (
  <label className="flex flex-col gap-1.5 text-[10px] font-bold text-slate-500">
    {label}
    <input
      type="number"
      min={0}
      max={max}
      value={value}
      onChange={(event) => onChange(Math.max(0, Math.min(max, Number(event.target.value) || 0)))}
      className="rounded-xl border border-slate-200 px-3 py-2 text-xs text-slate-700"
    />
  </label>
);

const PalQuality: React.FC<{ pal: Pal }> = ({ pal }) => (
  <div className="space-y-1 text-[10px] font-semibold text-slate-500">
    <p>{'★'.repeat(pal.stars || 0) || '0 星'}</p>
    <p>IV {pal.iv_average ?? 0}</p>
    <p className="font-mono text-[9px] text-slate-400">{pal.iv_hp ?? 0}/{pal.iv_attack ?? 0}/{pal.iv_defense ?? 0}</p>
  </div>
);

const PalLocation: React.FC<{ pal: Pal }> = ({ pal }) => (
  <div className="flex max-w-[180px] items-start gap-1.5 text-[10px] font-semibold text-slate-500">
    <MapPin size={12} className="mt-0.5 shrink-0 text-sky-500" />
    <span>{pal.terminal_location || pal.location_type || '位置未知'}</span>
  </div>
);

const PalCard: React.FC<{ pal: Pal; onHeal: () => void; onDelete: () => void }> = ({ pal, onHeal, onDelete }) => {
  const hpPercent = Math.min(100, Math.max(0, (pal.health / Math.max(1, pal.max_health)) * 100));
  return (
    <div className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
      <div className="flex items-start justify-between gap-3">
        <PalIdentity pal={pal} />
        <StatusBadge status={pal.status} />
      </div>
      <div className="mt-4">
        <HealthBar pal={pal} hpPercent={hpPercent} />
      </div>
      <div className="mt-4">
        <Suitability pal={pal} />
      </div>
      <div className="mt-3 grid grid-cols-2 gap-3 rounded-xl bg-slate-50 p-3">
        <PalQuality pal={pal} />
        <PalLocation pal={pal} />
      </div>
      <p className="mt-3 truncate text-[11px] font-semibold text-slate-500">所属玩家: {pal.owner_nickname}</p>
      <div className="mt-4 grid grid-cols-2 gap-2">
        <button type="button" onClick={onHeal} className="rounded-xl border border-slate-200 py-2 text-xs font-bold text-slate-600">
          治疗
        </button>
        <button type="button" onClick={onDelete} className="rounded-xl border border-rose-200 py-2 text-xs font-bold text-rose-600">
          释放
        </button>
      </div>
    </div>
  );
};
