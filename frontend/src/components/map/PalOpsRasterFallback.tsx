import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  mapPointToPixel,
  palOpsMapLayers,
  palOpsTileURL,
  type PalOpsMapLayerID,
  type PalOpsMapMarker,
} from '../../map/palopsMap';

interface Props {
  layerID: PalOpsMapLayerID;
  markers: PalOpsMapMarker[];
  selectedKey: string | null;
  tilesAvailable: boolean;
  reason: string;
  onSelect: (marker: PalOpsMapMarker) => void;
}

const minimumZoom = 0;
const maximumZoom = 3;
const initialZoom = 1;

export const PalOpsRasterFallback: React.FC<Props> = ({
  layerID,
  markers,
  selectedKey,
  tilesAvailable,
  reason,
  onSelect,
}) => {
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const [zoom, setZoom] = useState(initialZoom);
  const layer = palOpsMapLayers[layerID];
  const tileCount = 2 ** zoom;
  const mapSize = layer.tileSize * tileCount;
  const tiles = useMemo(
    () => Array.from({ length: tileCount * tileCount }, (_, index) => ({
      x: index % tileCount,
      y: Math.floor(index / tileCount),
    })),
    [tileCount],
  );

  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const callback = () => {
      viewport.scrollLeft = Math.max(0, (viewport.scrollWidth - viewport.clientWidth) / 2);
      viewport.scrollTop = Math.max(0, (viewport.scrollHeight - viewport.clientHeight) / 2);
    };
    if (typeof window.requestAnimationFrame === 'function') {
      const frame = window.requestAnimationFrame(callback);
      return () => window.cancelAnimationFrame(frame);
    }
    const timer = window.setTimeout(callback, 0);
    return () => window.clearTimeout(timer);
  }, [layerID, zoom]);

  return (
    <div className="absolute inset-0 bg-slate-950">
      <div
        ref={viewportRef}
        className="absolute inset-0 overflow-auto overscroll-contain"
        aria-label={`${layer.displayName} 兼容瓦片地图`}
      >
        <div
          className="relative m-auto shrink-0 bg-slate-900"
          style={{ width: mapSize, height: mapSize }}
        >
          {tilesAvailable ? tiles.map((tile) => (
            <img
              key={`${zoom}-${tile.x}-${tile.y}`}
              src={palOpsTileURL(layerID, zoom, tile.x, tile.y)}
              alt=""
              draggable={false}
              className="absolute select-none"
              style={{
                left: tile.x * layer.tileSize,
                top: tile.y * layer.tileSize,
                width: layer.tileSize,
                height: layer.tileSize,
              }}
            />
          )) : (
            <div
              className="absolute inset-0 opacity-40"
              style={{
                backgroundImage: 'linear-gradient(rgba(148,163,184,.25) 1px, transparent 1px), linear-gradient(90deg, rgba(148,163,184,.25) 1px, transparent 1px)',
                backgroundSize: '64px 64px',
              }}
            />
          )}

          {markers.map((marker) => {
            const point = mapPointToPixel({ x: marker.mapX, y: marker.mapY }, layer, zoom);
            const selected = marker.key === selectedKey;
            return (
              <button
                key={marker.key}
                type="button"
                onClick={() => onSelect(marker)}
                title={marker.label}
                aria-label={marker.label}
                className={`absolute z-10 rounded-full border-2 border-slate-950 shadow-md transition-transform hover:scale-125 focus:outline-none focus:ring-2 focus:ring-white ${selected ? 'ring-4 ring-white/80' : ''}`}
                style={{
                  left: point.x,
                  top: point.y,
                  width: selected ? 18 : marker.online ? 15 : 12,
                  height: selected ? 18 : marker.online ? 15 : 12,
                  backgroundColor: marker.color,
                  transform: `translate(-50%, -50%) rotate(${marker.shape === 'diamond' ? '45deg' : '0deg'})`,
                  borderRadius: marker.shape === 'square' || marker.shape === 'diamond' ? 3 : undefined,
                }}
              />
            );
          })}
        </div>
      </div>

      <div className="pointer-events-none absolute left-4 top-4 z-20 max-w-md rounded-xl border border-amber-400/30 bg-slate-950/90 px-3 py-2 text-[10px] font-semibold leading-4 text-amber-100 shadow-lg backdrop-blur">
        <p>兼容瓦片模式 · MapLibre 回退</p>
        <p className="mt-1 text-slate-300">{reason}</p>
      </div>

      <div className="absolute bottom-4 left-4 z-20 flex overflow-hidden rounded-lg border border-white/15 bg-slate-950/90 shadow-lg">
        <button
          type="button"
          className="h-9 w-10 text-sm font-bold text-white disabled:text-slate-600"
          onClick={() => setZoom((value) => Math.max(minimumZoom, value - 1))}
          disabled={zoom <= minimumZoom}
          aria-label="缩小兼容地图"
        >
          −
        </button>
        <button
          type="button"
          className="h-9 min-w-12 border-x border-white/10 px-2 text-[11px] font-semibold text-slate-200"
          onClick={() => setZoom(initialZoom)}
          aria-label="重置兼容地图缩放"
        >
          Z{zoom}
        </button>
        <button
          type="button"
          className="h-9 w-10 text-sm font-bold text-white disabled:text-slate-600"
          onClick={() => setZoom((value) => Math.min(maximumZoom, value + 1))}
          disabled={zoom >= maximumZoom}
          aria-label="放大兼容地图"
        >
          +
        </button>
      </div>
    </div>
  );
};
