import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  CheckCircle2, Layers3, Map as MapIcon, Radio, RefreshCw, Search, Undo2,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { saveIndexApi } from '../api/saveIndex';
import { PalOpsMapViewport } from '../components/map/PalOpsMapViewport';
import { SaveDataTabs } from '../components/ui/SaveDataTabs';
import { SaveIndexStatusBar } from '../components/ui/SaveIndexStatusBar';
import { useI18n } from '../i18n';
import {
  detectPalOpsLayer,
  loadPalOpsMapManifest,
  loadPalOpsPois,
  mapEntityToMarker,
  mapPointInsideLayer,
  markerSearchText,
  palOpsMapLayers,
  palOpsPoiCategoryLabel,
  palOpsPoiCategoryRank,
  palOpsPoiGroup,
  palOpsPoiGroupLabel,
  palOpsPoiGroups,
  palOpsPoiToMarker,
  PALOPS_DATASET_VERSION,
  PALOPS_SOURCE_REPOSITORY,
  PALOPS_SOURCE_VERSION,
  type PalOpsMapLayerID,
  type PalOpsMapMarker,
  type PalOpsPoiGroup,
} from '../map/palopsMap';
import type { MapEntity } from '../types';

type DynamicFilterID = 'guild-base' | 'custom-marker' | 'offline-player' | 'online-player' | 'pal';

type LocalizedLabel = { zh: string; en: string; ja: string };

interface PoiCategorySummary {
  category: string;
  group: PalOpsPoiGroup;
  count: number;
  label: string;
  color: string;
}

interface MapFilterItem {
  id: string;
  label: string;
  count: number;
  color: string;
  enabled: boolean;
  onToggle: () => void;
}

const dynamicFilters: Array<{
  id: DynamicFilterID;
  label: LocalizedLabel;
  color: string;
}> = [
  { id: 'guild-base', label: { zh: '公会据点', en: 'Guild Bases', ja: 'ギルド拠点' }, color: '#f59e0b' },
  { id: 'custom-marker', label: { zh: '自定义标记', en: 'Custom Markers', ja: 'カスタムマーカー' }, color: '#94a3b8' },
  { id: 'offline-player', label: { zh: '离线玩家', en: 'Offline Players', ja: 'オフラインプレイヤー' }, color: '#64748b' },
  { id: 'online-player', label: { zh: '在线玩家', en: 'Online Players', ja: 'オンラインプレイヤー' }, color: '#10b981' },
  { id: 'pal', label: { zh: '帕鲁实体', en: 'Pal Entities', ja: 'パル実体' }, color: '#84cc16' },
];

const defaultDynamicFilters: Record<DynamicFilterID, boolean> = {
  'guild-base': true,
  'custom-marker': true,
  'offline-player': true,
  'online-player': true,
  pal: false,
};

const defaultPoiFilters: Record<PalOpsPoiGroup, boolean> = {
  location: true,
  enemy: false,
  resource: false,
  collectible: false,
  npc: false,
  pal: false,
};

const refreshOptions = [1, 2, 3, 5, 10, 15, 30];
const refreshStorageKey = 'palpanel-live-map-refresh-seconds';
const defaultExploredStorageKey = `palpanel-palops-explored:${PALOPS_DATASET_VERSION}`;

const localizedLabel = (label: LocalizedLabel, locale: string): string => {
  const normalized = locale.trim().toLowerCase();
  if (normalized.startsWith('en')) return label.en;
  if (normalized.startsWith('ja')) return label.ja;
  return label.zh;
};

const dynamicFilterIDForEntity = (entity: MapEntity): DynamicFilterID | null => {
  if (entity.type === 'player') return entity.is_online ? 'online-player' : 'offline-player';
  if (entity.type === 'base') return 'guild-base';
  if (entity.type === 'map_object') return 'custom-marker';
  if (entity.type === 'pal') return 'pal';
  return null;
};

export const LiveMap: React.FC = () => {
  const queryClient = useQueryClient();
  const { locale } = useI18n();
  const [layerID, setLayerID] = useState<PalOpsMapLayerID>('palpagos');
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [refreshSeconds, setRefreshSeconds] = useState(() => loadRefreshSeconds());
  const [dynamicEnabled, setDynamicEnabled] = useState(defaultDynamicFilters);
  const [poiEnabled, setPoiEnabled] = useState(defaultPoiFilters);
  const [poiCategoryEnabled, setPoiCategoryEnabled] = useState<Record<string, boolean>>({});
  const [search, setSearch] = useState('');
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [onlyUndiscovered, setOnlyUndiscovered] = useState(false);

  const mapQuery = useQuery({
    queryKey: ['live-map'],
    queryFn: saveIndexApi.getMapEntities,
    refetchInterval: autoRefresh ? refreshSeconds * 1000 : false,
  });
  const manifestQuery = useQuery({
    queryKey: ['palops-map-manifest'],
    queryFn: loadPalOpsMapManifest,
    staleTime: 60 * 60 * 1000,
    retry: false,
  });
  const exploredStorageKey = useMemo(
    () => `palpanel-palops-explored:${manifestQuery.data?.dataset_version?.trim() || PALOPS_DATASET_VERSION}`,
    [manifestQuery.data?.dataset_version],
  );
  const activeExploredStorageKey = useRef(defaultExploredStorageKey);
  const skipExploredPersistKey = useRef<string | null>(null);
  const [explored, setExplored] = useState<Set<string>>(() => loadExplored(defaultExploredStorageKey));
  const poisQuery = useQuery({
    queryKey: ['palops-map-pois', locale, manifestQuery.data?.poi_total],
    queryFn: () => loadPalOpsPois(locale, manifestQuery.data!.poi_total),
    enabled: Boolean(manifestQuery.data?.poi_total),
    staleTime: Infinity,
    retry: false,
  });
  const rebuildMutation = useMutation({
    mutationFn: saveIndexApi.rebuild,
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['live-map'] }),
  });

  useEffect(() => {
    localStorage.setItem(refreshStorageKey, String(refreshSeconds));
  }, [refreshSeconds]);

  useEffect(() => {
    if (activeExploredStorageKey.current === exploredStorageKey) return;
    activeExploredStorageKey.current = exploredStorageKey;
    skipExploredPersistKey.current = exploredStorageKey;
    setExplored(loadExplored(exploredStorageKey));
  }, [exploredStorageKey]);

  useEffect(() => {
    if (skipExploredPersistKey.current === exploredStorageKey) {
      skipExploredPersistKey.current = null;
      return;
    }
    localStorage.setItem(exploredStorageKey, JSON.stringify([...explored].sort()));
  }, [explored, exploredStorageKey]);

  const normalizedSearch = search.trim().toLowerCase();
  const allEntities = mapQuery.data?.entities ?? [];
  const layerEntities = useMemo(
    () => allEntities
      .filter((entity) => Number.isFinite(entity.x) && Number.isFinite(entity.y))
      .filter((entity) => !(entity.x === 0 && entity.y === 0 && entity.z === 0))
      .filter((entity) => detectPalOpsLayer(entity.x, entity.y) === layerID),
    [allEntities, layerID],
  );
  const dynamicFilterCounts = useMemo(() => {
    const counts = Object.fromEntries(dynamicFilters.map((option) => [option.id, 0])) as Record<DynamicFilterID, number>;
    for (const entity of layerEntities) {
      const filterID = dynamicFilterIDForEntity(entity);
      if (filterID) counts[filterID] += 1;
    }
    return counts;
  }, [layerEntities]);
  const dynamicMarkers = useMemo(
    () => layerEntities
      .filter((entity) => {
        const filterID = dynamicFilterIDForEntity(entity);
        return filterID ? dynamicEnabled[filterID] : false;
      })
      .map(mapEntityToMarker)
      .filter((marker) => mapPointInsideLayer({ x: marker.mapX, y: marker.mapY }, palOpsMapLayers[layerID])),
    [layerEntities, dynamicEnabled, layerID],
  );

  const poiCategorySummaries = useMemo<PoiCategorySummary[]>(() => {
    const counts = new Map<string, number>();
    for (const poi of poisQuery.data ?? []) {
      if (poi.map !== layerID) continue;
      counts.set(poi.category, (counts.get(poi.category) ?? 0) + 1);
    }
    return [...counts.entries()]
      .map(([category, count]) => {
        const group = palOpsPoiGroup(category);
        if (!group) return null;
        return {
          category,
          group,
          count,
          label: palOpsPoiCategoryLabel(category, locale),
          color: palOpsPoiGroups.find((item) => item.id === group)?.color ?? '#94a3b8',
        };
      })
      .filter((item): item is PoiCategorySummary => Boolean(item))
      .sort((left, right) => {
        const leftGroup = palOpsPoiGroups.findIndex((item) => item.id === left.group);
        const rightGroup = palOpsPoiGroups.findIndex((item) => item.id === right.group);
        if (leftGroup !== rightGroup) return leftGroup - rightGroup;
        const rank = palOpsPoiCategoryRank(left.category) - palOpsPoiCategoryRank(right.category);
        return rank || left.label.localeCompare(right.label, locale);
      });
  }, [poisQuery.data, layerID, locale]);
  const poiCategoriesByGroup = useMemo(() => {
    const grouped = Object.fromEntries(palOpsPoiGroups.map((group) => [group.id, []])) as Record<PalOpsPoiGroup, PoiCategorySummary[]>;
    for (const category of poiCategorySummaries) grouped[category.group].push(category);
    return grouped;
  }, [poiCategorySummaries]);

  const poiMarkers = useMemo(
    () => (poisQuery.data ?? [])
      .filter((poi) => poi.map === layerID)
      .filter((poi) => {
        const group = palOpsPoiGroup(poi.category);
        return group ? (poiCategoryEnabled[poi.category] ?? poiEnabled[group]) : false;
      })
      .filter((poi) => !onlyUndiscovered || !explored.has(poi.id))
      .map(palOpsPoiToMarker)
      .filter((marker) => mapPointInsideLayer({ x: marker.mapX, y: marker.mapY }, palOpsMapLayers[layerID])),
    [poisQuery.data, layerID, poiEnabled, poiCategoryEnabled, onlyUndiscovered, explored],
  );

  const markers = useMemo(() => {
    const all = [...poiMarkers, ...dynamicMarkers];
    return normalizedSearch ? all.filter((marker) => markerSearchText(marker).includes(normalizedSearch)) : all;
  }, [poiMarkers, dynamicMarkers, normalizedSearch]);

  const selected = useMemo(
    () => [...poiMarkers, ...dynamicMarkers].find((marker) => marker.key === selectedKey) ?? null,
    [poiMarkers, dynamicMarkers, selectedKey],
  );

  const onlinePlayers = allEntities.filter((entity) => entity.type === 'player' && entity.is_online);
  const mapError = mapQuery.error
    ? getErrorMessage(mapQuery.error)
    : rebuildMutation.error
      ? getErrorMessage(rebuildMutation.error)
      : null;
  const assetError = manifestQuery.error || poisQuery.error;
  const tilesAvailable = Boolean(manifestQuery.data?.tiles_available);
  const currentLayerPoiTotal = (poisQuery.data ?? []).filter((poi) => poi.map === layerID).length;
  const currentLayerExplored = (poisQuery.data ?? []).filter((poi) => poi.map === layerID && explored.has(poi.id)).length;

  const toggleDynamicGroup = () => {
    const next = !dynamicFilters.every((option) => dynamicEnabled[option.id]);
    setDynamicEnabled(Object.fromEntries(dynamicFilters.map((option) => [option.id, next])) as Record<DynamicFilterID, boolean>);
  };

  const togglePoiGroup = (group: PalOpsPoiGroup) => {
    const categories = poiCategoriesByGroup[group];
    const allEnabled = categories.length > 0
      ? categories.every((category) => poiCategoryEnabled[category.category] ?? poiEnabled[group])
      : poiEnabled[group];
    const next = !allEnabled;
    setPoiEnabled((current) => ({ ...current, [group]: next }));
    setPoiCategoryEnabled((current) => {
      const updated = { ...current };
      for (const category of categories) updated[category.category] = next;
      return updated;
    });
  };

  const toggleExplored = (marker: PalOpsMapMarker) => {
    if (!marker.poi) return;
    setExplored((current) => {
      const next = new Set(current);
      if (next.has(marker.poi!.id)) next.delete(marker.poi!.id);
      else next.add(marker.poi!.id);
      return next;
    });
  };

  return (
    <div className="mx-auto flex w-full max-w-[1840px] flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <SaveDataTabs />

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <Metric label="在线玩家" value={mapQuery.data?.live.online_players ?? onlinePlayers.length} tone="blue" />
        <Metric label="服务器标记" value={dynamicMarkers.length} tone="sky" />
        <Metric label="固定 POI" value={currentLayerPoiTotal} tone="terracotta" />
        <Metric label="探索进度" value={`${currentLayerExplored}/${currentLayerPoiTotal}`} tone="green" />
        <Metric label="地图数据" value={manifestQuery.data?.source.version ?? PALOPS_SOURCE_VERSION} tone="amber" />
      </div>

      {mapError && <div className="rounded-2xl border border-rose-100 bg-rose-50 px-5 py-3 text-xs font-semibold text-rose-700">{mapError}</div>}
      {assetError && (
        <div className="rounded-2xl border border-amber-200 bg-amber-50 px-5 py-3 text-xs font-semibold text-amber-800">
          地图资源尚未同步。发布构建需要从 <code>{PALOPS_SOURCE_REPOSITORY}</code> 导入瓦片、POI 与许可证；当前仅显示服务器动态图层。
        </div>
      )}
      {!assetError && !tilesAvailable && (
        <div className="rounded-2xl border border-sky-200 bg-sky-50 px-5 py-3 text-xs font-semibold leading-5 text-sky-800">
          固定 POI 已加载，但地图瓦片未打包。请检查地图资源仓库的 <code>tiles/palpagos</code> 与 <code>tiles/world-tree</code> 是否包含完整 0–4 级金字塔。
        </div>
      )}

      <SaveIndexStatusBar
        status={mapQuery.data?.status ?? null}
        loading={mapQuery.isFetching}
        rebuilding={rebuildMutation.isPending}
        onRefresh={() => void mapQuery.refetch()}
        onRebuild={() => rebuildMutation.mutate()}
      />

      <section className="overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-sm shadow-slate-200/60">
        <header className="flex flex-col gap-4 border-b border-slate-200 bg-slate-50/70 px-5 py-4 xl:flex-row xl:items-center xl:justify-between">
          <div>
            <h3 className="flex items-center gap-2 text-sm font-bold text-slate-800">
              <MapIcon size={17} className="text-sky-600" />PalOps MapLibre 离线世界地图
            </h3>
            <p className="mt-1 text-[11px] font-medium text-slate-500">
              Palpagos / World Tree · MapLibre · 瓦片和固定 POI 来自 PalPanel 地图资源仓库 · 玩家和据点来自实时与存档索引。
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <label className="relative min-w-56 flex-1 xl:flex-none">
              <Search size={13} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
              <input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder="搜索玩家、据点、地点或内部 ID"
                className="w-full rounded-xl border border-slate-200 bg-white py-2 pl-8 pr-3 text-xs font-semibold text-slate-700 outline-none placeholder:text-slate-400 focus:border-sky-500"
              />
            </label>
            <select
              value={refreshSeconds}
              onChange={(event) => setRefreshSeconds(Number(event.target.value))}
              className="rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs font-bold text-slate-600"
              aria-label="玩家位置刷新间隔"
            >
              {refreshOptions.map((seconds) => <option key={seconds} value={seconds}>{seconds} 秒</option>)}
            </select>
            <button
              type="button"
              onClick={() => setAutoRefresh((value) => !value)}
              className={`inline-flex items-center gap-2 rounded-xl border px-3 py-2 text-xs font-bold ${autoRefresh ? 'border-sky-200 bg-sky-50 text-sky-700' : 'border-slate-200 bg-white text-slate-500'}`}
            >
              <Radio size={13} className={autoRefresh ? 'animate-pulse' : ''} />{autoRefresh ? '实时刷新' : '已暂停'}
            </button>
            <button type="button" onClick={() => void mapQuery.refetch()} className="rounded-xl border border-slate-200 bg-white p-2 text-slate-600 hover:bg-slate-50" aria-label="刷新地图数据">
              <RefreshCw size={14} className={mapQuery.isFetching ? 'animate-spin' : ''} />
            </button>
          </div>
        </header>

        <div className="flex flex-col gap-4 border-b border-slate-200 px-5 py-4">
          <div className="flex flex-wrap items-center gap-2">
            <span className="mr-1 inline-flex items-center gap-1 text-[10px] font-bold uppercase tracking-wider text-slate-400"><Layers3 size={12} />地图</span>
            {(Object.keys(palOpsMapLayers) as PalOpsMapLayerID[]).map((id) => (
              <button key={id} type="button" onClick={() => { setLayerID(id); setSelectedKey(null); }} className={`rounded-lg border px-3 py-1.5 text-[11px] font-bold ${layerID === id ? 'border-sky-300 bg-sky-50 text-sky-800' : 'border-slate-200 bg-white text-slate-500'}`}>
                {palOpsMapLayers[id].displayName}
              </button>
            ))}
            <span className="mx-1 h-5 w-px bg-slate-200" />
            <button type="button" onClick={() => setOnlyUndiscovered((value) => !value)} className={`inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-[11px] font-bold ${onlyUndiscovered ? 'border-emerald-200 bg-emerald-50 text-emerald-800' : 'border-slate-200 bg-white text-slate-500'}`}>
              <CheckCircle2 size={12} />仅未发现
            </button>
            <button type="button" onClick={() => setExplored(new Set())} className="inline-flex items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-[11px] font-bold text-slate-500">
              <Undo2 size={12} />清空探索记录
            </button>
          </div>

          <div className="grid gap-3 md:grid-cols-2 2xl:grid-cols-3">
            <FilterGroupCard
              label={localizedLabel({ zh: '服务器数据', en: 'Server Data', ja: 'サーバーデータ' }, locale)}
              color="#2563eb"
              total={dynamicFilters.reduce((sum, option) => sum + dynamicFilterCounts[option.id], 0)}
              enabledCount={dynamicFilters.filter((option) => dynamicEnabled[option.id]).length}
              onToggleAll={toggleDynamicGroup}
              items={dynamicFilters.map((option) => ({
                id: option.id,
                label: localizedLabel(option.label, locale),
                count: dynamicFilterCounts[option.id],
                color: option.color,
                enabled: dynamicEnabled[option.id],
                onToggle: () => setDynamicEnabled((current) => ({ ...current, [option.id]: !current[option.id] })),
              }))}
            />
            {palOpsPoiGroups.map((group) => {
              const categories = poiCategoriesByGroup[group.id];
              const enabledCount = categories.filter((category) => poiCategoryEnabled[category.category] ?? poiEnabled[group.id]).length;
              return (
                <FilterGroupCard
                  key={group.id}
                  label={palOpsPoiGroupLabel(group.id, locale)}
                  color={group.color}
                  total={categories.reduce((sum, category) => sum + category.count, 0)}
                  enabledCount={enabledCount}
                  onToggleAll={() => togglePoiGroup(group.id)}
                  items={categories.map((category) => ({
                    id: category.category,
                    label: category.label,
                    count: category.count,
                    color: category.color,
                    enabled: poiCategoryEnabled[category.category] ?? poiEnabled[group.id],
                    onToggle: () => setPoiCategoryEnabled((current) => ({
                      ...current,
                      [category.category]: !(current[category.category] ?? poiEnabled[group.id]),
                    })),
                  }))}
                />
              );
            })}
          </div>
        </div>

        <div className="grid min-h-[680px] xl:grid-cols-[minmax(0,1fr)_320px]">
          <div className="min-h-[580px] border-b border-slate-200 xl:border-b-0 xl:border-r">
            <PalOpsMapViewport
              layerID={layerID}
              markers={markers}
              selectedKey={selectedKey}
              tilesAvailable={tilesAvailable}
              onSelect={(marker) => setSelectedKey(marker.key)}
            />
          </div>

          <aside className="flex min-h-0 flex-col bg-slate-50/70 p-4">
            <div className="rounded-2xl border border-slate-200 bg-white p-4">
              <p className="text-[10px] font-bold uppercase tracking-wider text-slate-400">选中标记</p>
              {selected ? (
                <MarkerDetails marker={selected} locale={locale} explored={Boolean(selected.poi && explored.has(selected.poi.id))} onToggleExplored={() => toggleExplored(selected)} />
              ) : <p className="mt-3 text-xs font-semibold leading-5 text-slate-500">点击玩家、据点或固定 POI 查看坐标和来源。</p>}
            </div>

            <div className="mt-4 rounded-2xl border border-slate-200 bg-white p-4 text-[10px] font-semibold leading-5 text-slate-500">
              <p className="font-bold text-slate-700">地图来源</p>
              <p className="mt-2">{manifestQuery.data?.source.repository ?? PALOPS_SOURCE_REPOSITORY}</p>
              <p className="break-all font-mono">{manifestQuery.data?.source.commit ?? '未同步'}</p>
              <p className="mt-2">资源版本：{manifestQuery.data?.source.version ?? PALOPS_SOURCE_VERSION}</p>
              <p>数据集：{manifestQuery.data?.dataset_version ?? PALOPS_DATASET_VERSION}</p>
              <p>固定 POI：{manifestQuery.data?.poi_total ?? poisQuery.data?.length ?? 0}</p>
              <p>动态图层：PalPanel `/api/map/entities`</p>
            </div>

            <div className="mt-4 min-h-0 flex-1">
              <div className="mb-2 flex items-center justify-between">
                <p className="text-xs font-bold text-slate-700">当前显示</p>
                <span className="text-[10px] font-bold text-sky-700">{markers.length}</span>
              </div>
              <div className="flex max-h-[360px] flex-col gap-2 overflow-y-auto pr-1">
                {markers.slice(0, 100).map((marker) => (
                  <button key={marker.key} type="button" onClick={() => setSelectedKey(marker.key)} className="flex items-center gap-3 rounded-xl border border-slate-200 bg-white px-3 py-2 text-left hover:bg-sky-50">
                    <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: marker.color }} />
                    <span className="min-w-0"><span className="block truncate text-xs font-bold text-slate-700">{marker.label}</span><span className="mt-0.5 block truncate font-mono text-[9px] text-slate-500">{formatMarkerCoordinates(marker)}</span></span>
                  </button>
                ))}
                {markers.length > 100 && <p className="text-center text-[10px] font-semibold text-slate-400">侧栏仅显示前 100 项，地图仍显示全部筛选结果。</p>}
                {markers.length === 0 && <p className="rounded-xl border border-dashed border-slate-200 bg-white/70 px-3 py-5 text-center text-[11px] font-semibold text-slate-500">当前筛选条件下没有标记</p>}
              </div>
            </div>
          </aside>
        </div>
      </section>
    </div>
  );
};

const FilterGroupCard: React.FC<{
  label: string;
  color: string;
  total: number;
  enabledCount: number;
  items: MapFilterItem[];
  onToggleAll: () => void;
}> = ({ label, color, total, enabledCount, items, onToggleAll }) => {
  const allEnabled = items.length > 0 && enabledCount === items.length;
  const partiallyEnabled = enabledCount > 0 && !allEnabled;
  return (
    <section className="rounded-2xl border border-slate-200 bg-slate-50/70 p-3">
      <button
        type="button"
        aria-label={label}
        aria-pressed={partiallyEnabled ? 'mixed' : allEnabled}
        disabled={items.length === 0}
        onClick={onToggleAll}
        className={`flex w-full items-center gap-2 rounded-xl border px-3 py-2 text-left text-xs font-black transition ${allEnabled || partiallyEnabled ? 'border-sky-200 bg-white text-slate-800' : 'border-slate-200 bg-white/70 text-slate-500'} disabled:cursor-not-allowed disabled:opacity-50`}
      >
        <span className="h-3 w-3 shrink-0 rounded" style={{ backgroundColor: color, opacity: allEnabled ? 1 : partiallyEnabled ? 0.55 : 0.22 }} />
        <span className="min-w-0 flex-1 truncate">{label}</span>
        <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-bold text-slate-500">{total}</span>
      </button>
      <div className="mt-2 flex flex-wrap gap-1.5">
        {items.map((item) => (
          <button
            key={item.id}
            type="button"
            aria-label={item.label}
            aria-pressed={item.enabled}
            onClick={item.onToggle}
            className={`inline-flex min-w-0 items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-[10px] font-bold transition ${item.enabled ? 'border-sky-200 bg-white text-slate-700 shadow-sm' : 'border-slate-200 bg-white/60 text-slate-400'}`}
          >
            <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: item.color, opacity: item.enabled ? 1 : 0.28 }} />
            <span className="max-w-32 truncate">{item.label}</span>
            <span className="text-[9px] tabular-nums opacity-60">{item.count}</span>
          </button>
        ))}
        {items.length === 0 && <span className="px-2 py-1 text-[10px] font-semibold text-slate-400">当前地图无此类标记</span>}
      </div>
    </section>
  );
};

const MarkerDetails: React.FC<{ marker: PalOpsMapMarker; locale: string; explored: boolean; onToggleExplored: () => void }> = ({ marker, locale, explored, onToggleExplored }) => (
  <div className="mt-3">
    <div className="flex items-center gap-2"><span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: marker.color }} /><p className="truncate text-sm font-bold text-slate-800">{marker.label}</p></div>
    <p className="mt-2 break-all font-mono text-[9px] text-slate-500">{marker.poi?.id ?? marker.entity?.id}</p>
    <div className="mt-3 grid grid-cols-2 gap-2 text-[10px] font-semibold text-slate-500">
      <span>来源：{marker.kind === 'poi' ? 'PalOps 固定数据' : marker.entity?.live ? '实时' : '存档'}</span>
      <span>图层：{marker.poi?.map ?? (marker.entity ? detectPalOpsLayer(marker.entity.x, marker.entity.y) : '')}</span>
      <span className="col-span-2 font-mono">地图：{marker.mapX.toFixed(1)}, {marker.mapY.toFixed(1)}</span>
      {marker.poi && <span className="col-span-2 font-mono">世界：{marker.poi.worldX.toFixed(0)}, {marker.poi.worldY.toFixed(0)}</span>}
      {marker.entity && <span className="col-span-2 font-mono">世界：{marker.entity.x.toFixed(0)}, {marker.entity.y.toFixed(0)}, {marker.entity.z.toFixed(0)}</span>}
      {marker.entity?.guild_name && <span className="col-span-2 truncate">公会：{marker.entity.guild_name}</span>}
      {marker.poi && <span className="col-span-2">类别：{palOpsPoiGroupLabel(palOpsPoiGroup(marker.poi.category) ?? 'location', locale)} / {palOpsPoiCategoryLabel(marker.poi.category, locale)}</span>}
      {marker.poi && <span className="col-span-2">许可：{marker.poi.license}</span>}
    </div>
    {marker.poi && (
      <button type="button" onClick={onToggleExplored} className={`mt-4 w-full rounded-xl border px-3 py-2 text-xs font-bold ${explored ? 'border-slate-200 bg-slate-50 text-slate-600' : 'border-emerald-200 bg-emerald-50 text-emerald-800'}`}>
        {explored ? '标记为未发现' : '标记为已发现'}
      </button>
    )}
  </div>
);

const Metric: React.FC<{ label: string; value: string | number; tone: 'blue' | 'sky' | 'terracotta' | 'amber' | 'green' }> = ({ label, value, tone }) => {
  const colors = { blue: 'bg-blue-500', sky: 'bg-sky-500', terracotta: 'bg-rose-500', amber: 'bg-amber-500', green: 'bg-emerald-500' };
  return <div className="rounded-2xl border border-slate-100 bg-white px-4 py-3 shadow-sm"><p className="text-[10px] font-bold uppercase tracking-wider text-slate-400">{label}</p><div className="mt-1 flex items-center gap-2"><span className={`h-2 w-2 rounded-full ${colors[tone]}`} /><p className="truncate text-sm font-bold text-slate-800">{value}</p></div></div>;
};

const formatMarkerCoordinates = (marker: PalOpsMapMarker) => marker.poi
  ? `${marker.poi.map} · ${marker.poi.mapX.toFixed(0)}, ${marker.poi.mapY.toFixed(0)}`
  : `${marker.entity?.x.toFixed(0)}, ${marker.entity?.y.toFixed(0)}, ${marker.entity?.z.toFixed(0)}`;

const loadRefreshSeconds = (): number => {
  const stored = Number(localStorage.getItem(refreshStorageKey));
  return refreshOptions.includes(stored) ? stored : 3;
};

const loadExplored = (storageKey: string): Set<string> => {
  try {
    const value = JSON.parse(localStorage.getItem(storageKey) || '[]');
    return new Set(Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []);
  } catch {
    return new Set();
  }
};
