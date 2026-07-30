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
  palOpsPoiGroup,
  palOpsPoiGroups,
  palOpsPoiToMarker,
  PALOPS_DATASET_VERSION,
  PALOPS_SOURCE_REPOSITORY,
  PALOPS_SOURCE_VERSION,
  type PalOpsMapLayerID,
  type PalOpsMapMarker,
  type PalOpsPoiGroup,
} from '../map/palopsMap';
import type { MapEntityType } from '../types';

const dynamicFilters: Array<{ type: MapEntityType; label: string; color: string }> = [
  { type: 'player', label: '玩家', color: '#0ea5e9' },
  { type: 'base', label: '据点', color: '#f59e0b' },
  { type: 'pal', label: '帕鲁实体', color: '#84cc16' },
  { type: 'map_object', label: '地图对象', color: '#94a3b8' },
];

const defaultDynamicFilters: Record<string, boolean> = {
  player: true,
  base: true,
  pal: false,
  map_object: false,
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

export const LiveMap: React.FC = () => {
  const queryClient = useQueryClient();
  const { locale } = useI18n();
  const [layerID, setLayerID] = useState<PalOpsMapLayerID>('palpagos');
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [refreshSeconds, setRefreshSeconds] = useState(() => loadRefreshSeconds());
  const [dynamicEnabled, setDynamicEnabled] = useState(defaultDynamicFilters);
  const [poiEnabled, setPoiEnabled] = useState(defaultPoiFilters);
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
  const dynamicMarkers = useMemo(
    () => (mapQuery.data?.entities ?? [])
      .filter((entity) => dynamicEnabled[entity.type])
      .filter((entity) => Number.isFinite(entity.x) && Number.isFinite(entity.y))
      .filter((entity) => !(entity.x === 0 && entity.y === 0 && entity.z === 0))
      .filter((entity) => detectPalOpsLayer(entity.x, entity.y) === layerID)
      .map(mapEntityToMarker)
      .filter((marker) => mapPointInsideLayer({ x: marker.mapX, y: marker.mapY }, palOpsMapLayers[layerID])),
    [mapQuery.data?.entities, dynamicEnabled, layerID],
  );

  const poiMarkers = useMemo(
    () => (poisQuery.data ?? [])
      .filter((poi) => poi.map === layerID)
      .filter((poi) => {
        const group = palOpsPoiGroup(poi.category);
        return group ? poiEnabled[group] : false;
      })
      .filter((poi) => !onlyUndiscovered || !explored.has(poi.id))
      .map(palOpsPoiToMarker)
      .filter((marker) => mapPointInsideLayer({ x: marker.mapX, y: marker.mapY }, palOpsMapLayers[layerID])),
    [poisQuery.data, layerID, poiEnabled, onlyUndiscovered, explored],
  );

  const markers = useMemo(() => {
    const all = [...poiMarkers, ...dynamicMarkers];
    return normalizedSearch ? all.filter((marker) => markerSearchText(marker).includes(normalizedSearch)) : all;
  }, [poiMarkers, dynamicMarkers, normalizedSearch]);

  const selected = useMemo(
    () => [...poiMarkers, ...dynamicMarkers].find((marker) => marker.key === selectedKey) ?? null,
    [poiMarkers, dynamicMarkers, selectedKey],
  );

  const allEntities = mapQuery.data?.entities ?? [];
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

        <div className="flex flex-col gap-3 border-b border-slate-200 px-5 py-3">
          <div className="flex flex-wrap items-center gap-2">
            <span className="mr-1 inline-flex items-center gap-1 text-[10px] font-bold uppercase tracking-wider text-slate-400"><Layers3 size={12} />地图</span>
            {(Object.keys(palOpsMapLayers) as PalOpsMapLayerID[]).map((id) => (
              <button key={id} type="button" onClick={() => { setLayerID(id); setSelectedKey(null); }} className={`rounded-lg border px-3 py-1.5 text-[11px] font-bold ${layerID === id ? 'border-sky-300 bg-sky-50 text-sky-800' : 'border-slate-200 bg-white text-slate-500'}`}>
                {palOpsMapLayers[id].displayName}
              </button>
            ))}
            <span className="mx-1 h-5 w-px bg-slate-200" />
            {dynamicFilters.map((option) => (
              <FilterButton key={option.type} enabled={Boolean(dynamicEnabled[option.type])} color={option.color} label={option.label} onClick={() => setDynamicEnabled((current) => ({ ...current, [option.type]: !current[option.type] }))} />
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="mr-1 text-[10px] font-bold uppercase tracking-wider text-slate-400">固定图层</span>
            {palOpsPoiGroups.map((group) => (
              <FilterButton key={group.id} enabled={poiEnabled[group.id]} color={group.color} label={locale === 'en-US' ? group.en : group.zh} onClick={() => setPoiEnabled((current) => ({ ...current, [group.id]: !current[group.id] }))} />
            ))}
            <button type="button" onClick={() => setOnlyUndiscovered((value) => !value)} className={`inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-[11px] font-bold ${onlyUndiscovered ? 'border-emerald-200 bg-emerald-50 text-emerald-800' : 'border-slate-200 bg-white text-slate-500'}`}>
              <CheckCircle2 size={12} />仅未发现
            </button>
            <button type="button" onClick={() => setExplored(new Set())} className="inline-flex items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-[11px] font-bold text-slate-500">
              <Undo2 size={12} />清空探索记录
            </button>
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
                <MarkerDetails marker={selected} explored={Boolean(selected.poi && explored.has(selected.poi.id))} onToggleExplored={() => toggleExplored(selected)} />
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

const FilterButton: React.FC<{ enabled: boolean; color: string; label: string; onClick: () => void }> = ({ enabled, color, label, onClick }) => (
  <button type="button" onClick={onClick} className={`inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-[11px] font-bold ${enabled ? 'border-sky-200 bg-sky-50 text-sky-800' : 'border-slate-200 bg-white text-slate-500'}`}>
    <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: color }} />{label}
  </button>
);

const MarkerDetails: React.FC<{ marker: PalOpsMapMarker; explored: boolean; onToggleExplored: () => void }> = ({ marker, explored, onToggleExplored }) => (
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
      {marker.poi && <span className="col-span-2">类别：{marker.poi.category}</span>}
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
