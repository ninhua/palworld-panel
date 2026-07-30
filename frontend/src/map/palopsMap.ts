import type { Locale } from '../i18n';
import type { MapEntity } from '../types';

export type PalOpsMapLayerID = 'palpagos' | 'world-tree';
export type PalOpsPoiGroup = 'location' | 'enemy' | 'resource' | 'collectible' | 'npc' | 'pal';

export interface PalOpsMapPoint {
  x: number;
  y: number;
}

export interface PalOpsMapBounds {
  minimumX: number;
  maximumX: number;
  minimumY: number;
  maximumY: number;
}

export interface PalOpsAffineTransform {
  a: number;
  b: number;
  c: number;
  d: number;
  e: number;
  f: number;
}

export interface PalOpsMapLayer {
  id: PalOpsMapLayerID;
  displayName: string;
  tileSize: number;
  minimumZoom: number;
  maximumZoom: number;
  bounds: PalOpsMapBounds;
  worldToMap: PalOpsAffineTransform;
}

export interface PalOpsMapAssetsManifest {
  schema_version: number;
  source: {
    repository: string;
    commit: string;
    ref?: string;
    version: string;
  };
  dataset_version?: string;
  maps: PalOpsMapLayerID[];
  locales: string[];
  poi_total: number;
  category_counts: Record<string, number>;
  tiles_available: boolean;
  tile_policy: string;
}

export interface PalOpsPoi {
  id: string;
  type: string;
  category: string;
  map: PalOpsMapLayerID;
  name: string;
  aliases: string[];
  keywords: string[];
  mapX: number;
  mapY: number;
  worldX: number;
  worldY: number;
  source: string;
  license: string;
  version: string;
  iconId: string;
}

export interface PalOpsMapMarker {
  key: string;
  kind: 'poi' | 'entity';
  label: string;
  mapX: number;
  mapY: number;
  color: string;
  shape: 'circle' | 'diamond' | 'square';
  online?: boolean;
  entity?: MapEntity;
  poi?: PalOpsPoi;
}

export const PALOPS_SOURCE_REPOSITORY = 'ninhua/palpanel-assets';
export const PALOPS_SOURCE_VERSION = 'palpanel-assets';
export const PALOPS_DATASET_VERSION = 'palpanel-assets-v1';
export const PALOPS_POI_TOTAL = 1251;

export const palOpsMapLayers: Record<PalOpsMapLayerID, PalOpsMapLayer> = {
  palpagos: {
    id: 'palpagos',
    displayName: 'Palpagos',
    tileSize: 512,
    minimumZoom: 0,
    maximumZoom: 4,
    bounds: {
      minimumX: -1922.43790849673,
      maximumX: 1233.98474945534,
      minimumY: -2125.2962962963,
      maximumY: 1031.12636165577,
    },
    worldToMap: {
      a: 0,
      b: 0.00217864923747277,
      c: -344.226579520697,
      d: 0.00217864923747277,
      e: 0,
      f: 269.908496732026,
    },
  },
  'world-tree': {
    id: 'world-tree',
    displayName: 'World Tree',
    tileSize: 512,
    minimumZoom: 0,
    maximumZoom: 4,
    bounds: {
      minimumX: -2126.78867102396,
      maximumX: -1382.13725490196,
      minimumY: 1026.66775599129,
      maximumY: 1771.31917211329,
    },
    worldToMap: {
      a: 0,
      b: 0.00217864923747277,
      c: -344.226579520697,
      d: 0.00217864923747277,
      e: 0,
      f: 269.908496732026,
    },
  },
};

export const palOpsPoiGroups: Array<{ id: PalOpsPoiGroup; zh: string; en: string; color: string }> = [
  { id: 'location', zh: '地点', en: 'Locations', color: '#38bdf8' },
  { id: 'enemy', zh: '敌人与首领', en: 'Enemies', color: '#ef4444' },
  { id: 'resource', zh: '资源', en: 'Resources', color: '#f59e0b' },
  { id: 'collectible', zh: '收集', en: 'Collectibles', color: '#a855f7' },
  { id: 'npc', zh: 'NPC', en: 'NPCs', color: '#14b8a6' },
  { id: 'pal', zh: '帕鲁', en: 'Pals', color: '#84cc16' },
];

const groupByPrefix: Array<[string, PalOpsPoiGroup]> = [
  ['poi-location-', 'location'],
  ['poi-enemy-', 'enemy'],
  ['poi-resource-', 'resource'],
  ['poi-collectible-', 'collectible'],
  ['poi-npc-', 'npc'],
  ['poi-pal-', 'pal'],
];

const manifestPath = '/map/palops/palpanel-map-assets.json';

export const normalizePalOpsLocale = (locale: Locale | string): 'zh-CN' | 'en-US' | 'ja-JP' => {
  const normalized = locale.trim().toLowerCase();
  if (normalized.startsWith('en')) return 'en-US';
  if (normalized.startsWith('ja')) return 'ja-JP';
  return 'zh-CN';
};

export const loadPalOpsMapManifest = async (): Promise<PalOpsMapAssetsManifest> => {
  const response = await fetch(manifestPath, { cache: 'no-cache', credentials: 'same-origin' });
  if (!response.ok) throw new Error(`PalOps map manifest unavailable (${response.status})`);
  const value = await response.json() as PalOpsMapAssetsManifest;
  if (
    value.schema_version !== 1
    || typeof value.source?.repository !== 'string'
    || value.source.repository.length === 0
    || typeof value.source?.commit !== 'string'
    || value.source.commit.length === 0
    || typeof value.source?.version !== 'string'
    || value.source.version.length === 0
    || !Number.isInteger(value.poi_total)
    || value.poi_total <= 0
    || !Array.isArray(value.maps)
    || value.maps.length !== 2
    || !value.maps.includes('palpagos')
    || !value.maps.includes('world-tree')
    || !Array.isArray(value.locales)
    || !['zh-CN', 'en-US', 'ja-JP'].every((locale) => value.locales.includes(locale))
  ) {
    throw new Error('PalPanel map asset manifest is invalid');
  }
  return value;
};

export const loadPalOpsPois = async (locale: Locale | string, expectedTotal = PALOPS_POI_TOTAL): Promise<PalOpsPoi[]> => {
  const normalized = normalizePalOpsLocale(locale);
  const response = await fetch(`/map/palops/data/default-pois.${normalized}.json`, {
    cache: 'force-cache',
    credentials: 'same-origin',
  });
  if (!response.ok) throw new Error(`PalOps POI data unavailable (${response.status})`);
  const value = await response.json();
  if (!Array.isArray(value) || value.length !== expectedTotal || !value.every(isPalOpsPoi)) {
    throw new Error('PalPanel POI data failed manifest validation');
  }
  return value;
};

export const isPalOpsPoi = (value: unknown): value is PalOpsPoi => {
  if (!value || typeof value !== 'object') return false;
  const record = value as Record<string, unknown>;
  return typeof record.id === 'string'
    && record.id.length > 0
    && typeof record.type === 'string'
    && typeof record.name === 'string'
    && typeof record.category === 'string'
    && (record.map === 'palpagos' || record.map === 'world-tree')
    && Array.isArray(record.aliases)
    && record.aliases.every((item) => typeof item === 'string')
    && Array.isArray(record.keywords)
    && record.keywords.every((item) => typeof item === 'string')
    && typeof record.mapX === 'number'
    && Number.isFinite(record.mapX)
    && typeof record.mapY === 'number'
    && Number.isFinite(record.mapY)
    && typeof record.worldX === 'number'
    && Number.isFinite(record.worldX)
    && typeof record.worldY === 'number'
    && Number.isFinite(record.worldY)
    && typeof record.source === 'string'
    && typeof record.license === 'string'
    && typeof record.version === 'string'
    && typeof record.iconId === 'string';
};

export const palOpsPoiGroup = (category: string): PalOpsPoiGroup | null => {
  for (const [prefix, group] of groupByPrefix) {
    if (category.startsWith(prefix)) return group;
  }
  return null;
};

export const palOpsPoiColor = (category: string): string => {
  const group = palOpsPoiGroup(category);
  return palOpsPoiGroups.find((item) => item.id === group)?.color ?? '#94a3b8';
};

export const worldToPalOpsMap = (worldX: number, worldY: number): PalOpsMapPoint => {
  const transform = palOpsMapLayers.palpagos.worldToMap;
  return {
    x: transform.a * worldX + transform.b * worldY + transform.c,
    y: transform.d * worldX + transform.e * worldY + transform.f,
  };
};

export const mapPointInsideLayer = (point: PalOpsMapPoint, layer: PalOpsMapLayer): boolean => (
  point.x >= layer.bounds.minimumX
  && point.x <= layer.bounds.maximumX
  && point.y >= layer.bounds.minimumY
  && point.y <= layer.bounds.maximumY
);

export const detectPalOpsLayer = (worldX: number, worldY: number): PalOpsMapLayerID => {
  const point = worldToPalOpsMap(worldX, worldY);
  return mapPointInsideLayer(point, palOpsMapLayers['world-tree']) ? 'world-tree' : 'palpagos';
};

const maximumMercatorLatitude = 85.0511287798066;

export const mapPointToLngLat = (point: PalOpsMapPoint, layer: PalOpsMapLayer): [number, number] => {
  const normalizedX = (point.x - layer.bounds.minimumX) / (layer.bounds.maximumX - layer.bounds.minimumX);
  const normalizedY = (layer.bounds.maximumY - point.y) / (layer.bounds.maximumY - layer.bounds.minimumY);
  const longitude = normalizedX * 360 - 180;
  const latitude = Math.atan(Math.sinh(Math.PI * (1 - 2 * normalizedY))) * 180 / Math.PI;
  return [longitude, Math.min(maximumMercatorLatitude, Math.max(-maximumMercatorLatitude, latitude))];
};

export const lngLatToMapPoint = (longitude: number, latitude: number, layer: PalOpsMapLayer): PalOpsMapPoint => {
  const clampedLatitude = Math.min(maximumMercatorLatitude, Math.max(-maximumMercatorLatitude, latitude));
  const normalizedX = (longitude + 180) / 360;
  const latitudeRadians = clampedLatitude * Math.PI / 180;
  const normalizedY = (1 - Math.asinh(Math.tan(latitudeRadians)) / Math.PI) / 2;
  return {
    x: layer.bounds.minimumX + normalizedX * (layer.bounds.maximumX - layer.bounds.minimumX),
    y: layer.bounds.maximumY - normalizedY * (layer.bounds.maximumY - layer.bounds.minimumY),
  };
};

export const mapPointToPixel = (
  point: PalOpsMapPoint,
  layer: PalOpsMapLayer,
  zoom: number,
): PalOpsMapPoint => {
  const scale = layer.tileSize * (2 ** zoom);
  const normalizedX = (point.x - layer.bounds.minimumX) / (layer.bounds.maximumX - layer.bounds.minimumX);
  const normalizedY = (layer.bounds.maximumY - point.y) / (layer.bounds.maximumY - layer.bounds.minimumY);
  return { x: normalizedX * scale, y: normalizedY * scale };
};

export const mapEntityToMarker = (entity: MapEntity): PalOpsMapMarker => {
  const point = worldToPalOpsMap(entity.x, entity.y);
  const color = entity.type === 'player'
    ? (entity.is_online ? '#0ea5e9' : '#64748b')
    : entity.type === 'base'
      ? '#f59e0b'
      : entity.type === 'pal'
        ? '#84cc16'
        : '#94a3b8';
  return {
    key: `entity:${entity.type}:${entity.id}`,
    kind: 'entity',
    label: entity.label,
    mapX: point.x,
    mapY: point.y,
    color,
    shape: entity.type === 'base' ? 'diamond' : entity.type === 'map_object' ? 'square' : 'circle',
    online: entity.is_online,
    entity,
  };
};

export const palOpsPoiToMarker = (poi: PalOpsPoi): PalOpsMapMarker => ({
  key: `poi:${poi.id}`,
  kind: 'poi',
  label: poi.name,
  mapX: poi.mapX,
  mapY: poi.mapY,
  color: palOpsPoiColor(poi.category),
  shape: poi.category === 'poi-location-fast-travel' ? 'diamond' : 'circle',
  poi,
});

export const palOpsTileURL = (layer: PalOpsMapLayerID, zoom: number, x: number, y: number): string => (
  `/map/palops/tiles/${layer}/${zoom}/${x}/${y}.webp`
);

export const markerSearchText = (marker: PalOpsMapMarker): string => {
  if (marker.poi) {
    return [marker.poi.name, marker.poi.id, marker.poi.category, ...marker.poi.aliases, ...marker.poi.keywords]
      .join(' ')
      .toLowerCase();
  }
  const entity = marker.entity;
  return [marker.label, entity?.id, entity?.guild_name, entity?.owner_id].filter(Boolean).join(' ').toLowerCase();
};
