import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  mapPointToLngLat,
  palOpsMapLayers,
  type PalOpsMapLayerID,
  type PalOpsMapMarker,
} from '../../map/palopsMap';
import {
  loadMapLibreRuntime,
  type MapLibreMapInstance,
  type MapLibreRuntimeModule,
} from '../../map/maplibreRuntime';
import { PalOpsRasterFallback } from './PalOpsRasterFallback';

interface Props {
  layerID: PalOpsMapLayerID;
  markers: PalOpsMapMarker[];
  selectedKey: string | null;
  tilesAvailable: boolean;
  onSelect: (marker: PalOpsMapMarker) => void;
}

const markerSourceID = 'palops-markers';
const markerLayerID = 'palops-marker-circles';
const mapLibreMaxCanvasSize: [number, number] = [4096, 4096];

export const PalOpsMapViewport: React.FC<Props> = ({ layerID, markers, selectedKey, tilesAvailable, onSelect }) => {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const mapRef = useRef<MapLibreMapInstance | null>(null);
  const markersRef = useRef(markers);
  const onSelectRef = useRef(onSelect);
  const selectedKeyRef = useRef(selectedKey);
  const [compatibilityReason, setCompatibilityReason] = useState<string | null>(null);
  const layer = palOpsMapLayers[layerID];
  const markerData = useMemo(() => markerFeatureCollection(markers, selectedKey, layerID), [markers, selectedKey, layerID]);

  useEffect(() => { markersRef.current = markers; }, [markers]);
  useEffect(() => { onSelectRef.current = onSelect; }, [onSelect]);
  useEffect(() => { selectedKeyRef.current = selectedKey; }, [selectedKey]);

  useEffect(() => {
    const source = mapRef.current?.getSource(markerSourceID);
    source?.setData(markerData);
  }, [markerData]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    let cancelled = false;
    let observer: ResizeObserver | null = null;
    let effectMap: MapLibreMapInstance | null = null;
    setCompatibilityReason(null);

    void loadMapLibreRuntime().then((runtime) => {
      if (cancelled) return;
      const map = createMap(runtime, container, layerID, tilesAvailable, markerFeatureCollection(markersRef.current, selectedKeyRef.current, layerID));
      effectMap = map;
      mapRef.current = map;
      map.addControl(new runtime.NavigationControl({ showCompass: false, visualizePitch: false }), 'bottom-left');
      map.on('load', () => {
        map.getSource(markerSourceID)?.setData(
          markerFeatureCollection(markersRef.current, selectedKeyRef.current, layerID),
        );
      });
      map.on('click', markerLayerID, (event) => {
        const key = event.features?.[0]?.properties?.key;
        if (typeof key !== 'string') return;
        const marker = markersRef.current.find((item) => item.key === key);
        if (marker) onSelectRef.current(marker);
      });
      map.on('mouseenter', markerLayerID, () => { map.getCanvas().style.cursor = 'pointer'; });
      map.on('mouseleave', markerLayerID, () => { map.getCanvas().style.cursor = ''; });
      if (typeof ResizeObserver !== 'undefined') {
        observer = new ResizeObserver(() => map.resize());
        observer.observe(container);
      }
    }).catch((error: unknown) => {
      try { effectMap?.remove(); } catch { /* fall back even if partial MapLibre cleanup fails */ }
      if (mapRef.current === effectMap) mapRef.current = null;
      if (!cancelled) {
        setCompatibilityReason(error instanceof Error ? error.message : 'MapLibre runtime unavailable');
      }
    });

    return () => {
      cancelled = true;
      observer?.disconnect();
      try { effectMap?.remove(); } catch { /* ignore teardown errors from a partial runtime */ }
      if (mapRef.current === effectMap) mapRef.current = null;
    };
  }, [layerID, tilesAvailable]);

  return (
    <div className="relative h-full min-h-[560px] overflow-hidden bg-slate-950">
      <div ref={containerRef} className="absolute inset-0" aria-label={`${layer.displayName} MapLibre 离线地图`} />
      {!tilesAvailable && !compatibilityReason && (
        <div className="pointer-events-none absolute left-1/2 top-1/2 z-10 max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-slate-600 bg-slate-950/85 px-6 py-5 text-center shadow-xl backdrop-blur">
          <p className="text-sm font-bold text-slate-200">{layer.displayName}</p>
          <p className="mt-2 text-xs font-semibold leading-5 text-slate-400">MapLibre 与固定 POI 已加载，但资源快照缺少完整瓦片；请检查 palpanel-assets 构建输入。</p>
        </div>
      )}
      {compatibilityReason && (
        <PalOpsRasterFallback
          layerID={layerID}
          markers={markers}
          selectedKey={selectedKey}
          tilesAvailable={tilesAvailable}
          reason={compatibilityReason}
          onSelect={onSelect}
        />
      )}
      <div className="pointer-events-none absolute right-4 top-4 z-10 rounded-lg border border-white/10 bg-slate-950/80 px-3 py-2 text-right text-[10px] font-semibold text-slate-300 backdrop-blur">
        <p>{layer.displayName}</p>
        <p>{compatibilityReason ? '兼容瓦片模式 · MapLibre 回退' : tilesAvailable ? 'PalPanel 地图资源 · MapLibre' : '无底图 · MapLibre 标记层'}</p>
      </div>
    </div>
  );
};

const createMap = (
  runtime: MapLibreRuntimeModule,
  container: HTMLDivElement,
  layerID: PalOpsMapLayerID,
  tilesAvailable: boolean,
  data: Record<string, unknown>,
): MapLibreMapInstance => new runtime.Map(createPalOpsMapOptions(container, layerID, tilesAvailable, data));

export const createPalOpsMapOptions = (
  container: HTMLDivElement,
  layerID: PalOpsMapLayerID,
  tilesAvailable: boolean,
  data: Record<string, unknown>,
): Record<string, unknown> => {
  const layer = palOpsMapLayers[layerID];
  const style = createStyle(layerID, tilesAvailable, data);
  return {
    container,
    style,
    center: [0, 0],
    zoom: 0,
    minZoom: layer.minimumZoom,
    maxZoom: layer.maximumZoom,
    renderWorldCopies: false,
    maxBounds: [[-180, -85.0511287798066], [180, 85.0511287798066]],
    // Keep an explicit tuple because MapLibre reads both entries during its first resize.
    maxCanvasSize: [...mapLibreMaxCanvasSize],
    attributionControl: false,
    dragRotate: false,
    pitchWithRotate: false,
    touchPitch: false,
    cooperativeGestures: false,
  };
};

const createStyle = (layerID: PalOpsMapLayerID, tilesAvailable: boolean, data: Record<string, unknown>) => {
  const sources: Record<string, unknown> = {
    [markerSourceID]: { type: 'geojson', data },
  };
  const layers: Array<Record<string, unknown>> = [
    { id: 'palops-background', type: 'background', paint: { 'background-color': '#111827' } },
  ];
  if (tilesAvailable) {
    sources['palops-raster'] = {
      type: 'raster',
      tiles: [`/map/palops/tiles/${layerID}/{z}/{x}/{y}.webp`],
      tileSize: 512,
      minzoom: 0,
      maxzoom: 4,
    };
    layers.push({
      id: 'palops-raster-layer',
      type: 'raster',
      source: 'palops-raster',
      minzoom: 0,
      maxzoom: 5,
      paint: { 'raster-fade-duration': 0 },
    });
  }
  layers.push({
    id: markerLayerID,
    type: 'circle',
    source: markerSourceID,
    paint: {
      'circle-color': ['get', 'color'],
      'circle-radius': ['case', ['boolean', ['get', 'selected'], false], 10, ['boolean', ['get', 'online'], false], 8, 6],
      'circle-stroke-width': ['case', ['boolean', ['get', 'selected'], false], 4, 2],
      'circle-stroke-color': ['case', ['boolean', ['get', 'selected'], false], '#ffffff', '#0f172a'],
      'circle-opacity': 0.96,
    },
  });
  return { version: 8, sources, layers };
};

const markerFeatureCollection = (
  markers: PalOpsMapMarker[],
  selectedKey: string | null,
  layerID: PalOpsMapLayerID,
): Record<string, unknown> => {
  const layer = palOpsMapLayers[layerID];
  return {
    type: 'FeatureCollection',
    features: markers.map((marker) => ({
      type: 'Feature',
      id: marker.key,
      geometry: {
        type: 'Point',
        coordinates: mapPointToLngLat({ x: marker.mapX, y: marker.mapY }, layer),
      },
      properties: {
        key: marker.key,
        label: marker.label,
        color: marker.color,
        online: Boolean(marker.online),
        selected: marker.key === selectedKey,
        kind: marker.kind,
      },
    })),
  };
};

