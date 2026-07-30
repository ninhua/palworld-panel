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

export type PalOpsMarkerShape = 'circle' | 'diamond' | 'square' | 'triangle' | 'hexagon' | 'star' | 'pin' | 'cross';

export interface PalOpsMarkerVisual {
  icon: string;
  glyph: string;
  color: string;
  shape: PalOpsMarkerShape;
}

export interface PalOpsMapMarker {
  key: string;
  kind: 'poi' | 'entity';
  label: string;
  mapX: number;
  mapY: number;
  color: string;
  shape: PalOpsMarkerShape;
  icon: string;
  glyph: string;
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

export const palOpsPoiGroups: Array<{ id: PalOpsPoiGroup; zh: string; en: string; ja: string; color: string }> = [
  { id: 'location', zh: '地点', en: 'Locations', ja: '場所', color: '#38bdf8' },
  { id: 'enemy', zh: '敌人与首领', en: 'Enemies & Bosses', ja: '敵とボス', color: '#ef4444' },
  { id: 'resource', zh: '资源', en: 'Resources', ja: '資源', color: '#f59e0b' },
  { id: 'collectible', zh: '收集品', en: 'Collectibles', ja: '収集品', color: '#a855f7' },
  { id: 'npc', zh: 'NPC', en: 'NPCs', ja: 'NPC', color: '#14b8a6' },
  { id: 'pal', zh: '帕鲁', en: 'Pals', ja: 'パル', color: '#84cc16' },
];

const groupByPrefix: Array<[string, PalOpsPoiGroup]> = [
  ['poi-location-', 'location'],
  ['poi-enemy-', 'enemy'],
  ['poi-resource-', 'resource'],
  ['poi-collectible-', 'collectible'],
  ['poi-npc-', 'npc'],
  ['poi-pal-', 'pal'],
];

type PalOpsPoiCategoryLabels = { zh: string; en: string; ja: string };

const poiCategoryLabels: Record<string, PalOpsPoiCategoryLabels> = {
  'fast-travel': { zh: '快速传送', en: 'Fast Travel', ja: 'ファストトラベル' },
  waypoint: { zh: '快速传送', en: 'Fast Travel', ja: 'ファストトラベル' },
  dungeon: { zh: '地牢', en: 'Dungeons', ja: 'ダンジョン' },
  'region-name': { zh: '地区名称', en: 'Region Names', ja: '地域名' },
  region: { zh: '地区名称', en: 'Region Names', ja: '地域名' },
  tower: { zh: '高塔', en: 'Towers', ja: '塔' },
  special: { zh: '特殊地点', en: 'Special Locations', ja: '特殊地点' },
  'special-location': { zh: '特殊地点', en: 'Special Locations', ja: '特殊地点' },
  landmark: { zh: '地标', en: 'Landmarks', ja: 'ランドマーク' },
  'field-boss': { zh: '区域头目', en: 'Field Bosses', ja: 'フィールドボス' },
  'alpha-pal': { zh: '区域头目', en: 'Field Bosses', ja: 'フィールドボス' },
  boss: { zh: '区域头目', en: 'Bosses', ja: 'ボス' },
  'tower-boss': { zh: '高塔首领', en: 'Tower Bosses', ja: '塔ボス' },
  'enemy-camp': { zh: '敌人营地', en: 'Enemy Camps', ja: '敵のキャンプ' },
  camp: { zh: '敌人营地', en: 'Enemy Camps', ja: '敵のキャンプ' },
  encounter: { zh: '遭遇目标', en: 'Encounters', ja: 'エンカウント' },
  event: { zh: '事件地点', en: 'Events', ja: 'イベント地点' },
  raid: { zh: '突袭地点', en: 'Raids', ja: 'レイド地点' },
  ore: { zh: '矿石', en: 'Ore', ja: '鉱石' },
  mining: { zh: '矿点', en: 'Mining Nodes', ja: '採掘地点' },
  coal: { zh: '煤炭', en: 'Coal', ja: '石炭' },
  sulfur: { zh: '硫磺', en: 'Sulfur', ja: '硫黄' },
  quartz: { zh: '纯水晶', en: 'Pure Quartz', ja: 'ピュアクォーツ' },
  'pure-quartz': { zh: '纯水晶', en: 'Pure Quartz', ja: 'ピュアクォーツ' },
  oil: { zh: '原油', en: 'Crude Oil', ja: '原油' },
  'crude-oil': { zh: '原油', en: 'Crude Oil', ja: '原油' },
  meteorite: { zh: '陨石', en: 'Meteorites', ja: '隕石' },
  resource: { zh: '资源点', en: 'Resource Nodes', ja: '資源地点' },
  chest: { zh: '宝箱', en: 'Treasure Chests', ja: '宝箱' },
  'treasure-chest': { zh: '宝箱', en: 'Treasure Chests', ja: '宝箱' },
  effigy: { zh: '翠叶鼠雕像', en: 'Lifmunk Effigies', ja: 'クルリス像' },
  'lifmunk-effigy': { zh: '翠叶鼠雕像', en: 'Lifmunk Effigies', ja: 'クルリス像' },
  journal: { zh: '手记', en: 'Journals', ja: '手記' },
  memo: { zh: '手记', en: 'Journals', ja: '手記' },
  note: { zh: '手记', en: 'Notes', ja: 'メモ' },
  egg: { zh: '帕鲁蛋', en: 'Pal Eggs', ja: 'パルのタマゴ' },
  'pal-egg': { zh: '帕鲁蛋', en: 'Pal Eggs', ja: 'パルのタマゴ' },
  merchant: { zh: '商人', en: 'Merchants', ja: '商人' },
  vendor: { zh: '商人', en: 'Vendors', ja: '商人' },
  'wandering-merchant': { zh: '流浪商人', en: 'Wandering Merchants', ja: '放浪商人' },
  'black-marketeer': { zh: '黑市商人', en: 'Black Marketeers', ja: '闇商人' },
  'pal-merchant': { zh: '帕鲁商人', en: 'Pal Merchants', ja: 'パル商人' },
  npc: { zh: '其他 NPC', en: 'Other NPCs', ja: 'その他のNPC' },
  spawn: { zh: '帕鲁刷新点', en: 'Pal Spawns', ja: 'パル出現地点' },
  'pal-spawn': { zh: '帕鲁刷新点', en: 'Pal Spawns', ja: 'パル出現地点' },
  habitat: { zh: '帕鲁栖息地', en: 'Pal Habitats', ja: 'パル生息地' },
  'lucky-pal': { zh: '闪光帕鲁', en: 'Lucky Pals', ja: 'ラッキーパル' },
  pal: { zh: '其他帕鲁地点', en: 'Other Pal Locations', ja: 'その他のパル地点' },
};

const poiCategoryOrder: Record<PalOpsPoiGroup, string[]> = {
  location: ['fast-travel', 'waypoint', 'dungeon', 'region-name', 'region', 'tower', 'special-location', 'special', 'landmark'],
  enemy: ['field-boss', 'alpha-pal', 'boss', 'tower-boss', 'enemy-camp', 'camp', 'encounter', 'event', 'raid'],
  resource: ['ore', 'mining', 'coal', 'sulfur', 'pure-quartz', 'quartz', 'oil', 'crude-oil', 'meteorite', 'resource'],
  collectible: ['chest', 'treasure-chest', 'lifmunk-effigy', 'effigy', 'journal', 'memo', 'note', 'pal-egg', 'egg'],
  npc: ['wandering-merchant', 'black-marketeer', 'pal-merchant', 'merchant', 'vendor', 'npc'],
  pal: ['pal-spawn', 'spawn', 'habitat', 'lucky-pal', 'pal'],
};

const chineseCategoryTokens: Record<string, string> = {
  fast: '快速', travel: '传送', dungeon: '地牢', region: '地区', name: '名称', tower: '高塔',
  special: '特殊', location: '地点', landmark: '地标', field: '区域', boss: '头目', alpha: '头目',
  enemy: '敌人', camp: '营地', encounter: '遭遇', event: '事件', raid: '突袭', ore: '矿石',
  mining: '矿点', coal: '煤炭', sulfur: '硫磺', pure: '纯', quartz: '水晶', oil: '原油',
  crude: '原油', meteorite: '陨石', resource: '资源点', chest: '宝箱', treasure: '宝箱',
  effigy: '雕像', lifmunk: '翠叶鼠', journal: '手记', memo: '手记', note: '笔记', egg: '蛋',
  merchant: '商人', wandering: '流浪', black: '黑市', marketeer: '商人', vendor: '商人', npc: 'NPC',
  pal: '帕鲁', spawn: '刷新点', habitat: '栖息地', lucky: '闪光',
};

const palOpsPoiCategorySlug = (category: string, group: PalOpsPoiGroup | null): string => {
  if (!group) return category.trim().toLowerCase().replaceAll('_', '-').replaceAll(' ', '-');
  const prefix = `poi-${group}-`;
  return category.startsWith(prefix) ? category.slice(prefix.length) : category;
};

const titleCaseCategory = (slug: string): string => slug
  .split('-')
  .filter(Boolean)
  .map((part) => part.length > 0 ? `${part[0].toUpperCase()}${part.slice(1)}` : part)
  .join(' ');

const fallbackChineseCategory = (slug: string): string => slug
  .split('-')
  .filter(Boolean)
  .map((part) => chineseCategoryTokens[part] ?? part)
  .join('');

export const palOpsPoiGroupLabel = (group: PalOpsPoiGroup, locale: Locale | string): string => {
  const definition = palOpsPoiGroups.find((item) => item.id === group);
  if (!definition) return group;
  const normalized = normalizePalOpsLocale(locale);
  if (normalized === 'en-US') return definition.en;
  if (normalized === 'ja-JP') return definition.ja;
  return definition.zh;
};

export const palOpsPoiCategoryLabel = (category: string, locale: Locale | string): string => {
  const group = palOpsPoiGroup(category);
  const slug = palOpsPoiCategorySlug(category, group);
  const labels = poiCategoryLabels[slug];
  const normalized = normalizePalOpsLocale(locale);
  if (labels) {
    if (normalized === 'en-US') return labels.en;
    if (normalized === 'ja-JP') return labels.ja;
    return labels.zh;
  }
  if (normalized === 'en-US') return titleCaseCategory(slug);
  if (normalized === 'ja-JP') return titleCaseCategory(slug);
  return fallbackChineseCategory(slug);
};

export const palOpsPoiCategoryRank = (category: string): number => {
  const group = palOpsPoiGroup(category);
  if (!group) return Number.MAX_SAFE_INTEGER;
  const slug = palOpsPoiCategorySlug(category, group);
  const index = poiCategoryOrder[group].indexOf(slug);
  return index >= 0 ? index : poiCategoryOrder[group].length + 100;
};

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

export const palOpsPoiColor = (category: string): string => palOpsPoiVisual(category).color;

export const palOpsPoiVisual = (category: string): PalOpsMarkerVisual => {
  const group = palOpsPoiGroup(category);
  const slug = palOpsPoiCategorySlug(category, group);
  if (['fast-travel', 'waypoint'].includes(slug)) return { icon: 'poi-fast-travel', glyph: '↯', color: '#2563eb', shape: 'diamond' };
  if (slug === 'dungeon') return { icon: 'poi-dungeon', glyph: 'D', color: '#7c3aed', shape: 'square' };
  if (['tower', 'tower-boss'].includes(slug)) return { icon: 'poi-tower', glyph: 'T', color: '#dc2626', shape: 'triangle' };
  if (['field-boss', 'alpha-pal', 'boss'].includes(slug)) return { icon: 'poi-boss', glyph: '★', color: '#e11d48', shape: 'star' };
  if (['enemy-camp', 'camp'].includes(slug)) return { icon: 'poi-enemy-camp', glyph: 'C', color: '#ea580c', shape: 'triangle' };
  if (['raid', 'encounter', 'event'].includes(slug)) return { icon: 'poi-event', glyph: '!', color: '#f97316', shape: 'triangle' };
  if (['ore', 'mining', 'resource'].includes(slug)) return { icon: 'poi-resource-ore', glyph: 'O', color: '#78716c', shape: 'hexagon' };
  if (slug === 'coal') return { icon: 'poi-resource-coal', glyph: 'C', color: '#334155', shape: 'hexagon' };
  if (slug === 'sulfur') return { icon: 'poi-resource-sulfur', glyph: 'S', color: '#ca8a04', shape: 'hexagon' };
  if (['pure-quartz', 'quartz'].includes(slug)) return { icon: 'poi-resource-quartz', glyph: 'Q', color: '#0891b2', shape: 'diamond' };
  if (['oil', 'crude-oil'].includes(slug)) return { icon: 'poi-resource-oil', glyph: 'OIL', color: '#92400e', shape: 'pin' };
  if (slug === 'meteorite') return { icon: 'poi-resource-meteorite', glyph: 'M', color: '#7c3aed', shape: 'star' };
  if (group === 'resource') return { icon: 'poi-resource', glyph: '◆', color: '#d97706', shape: 'hexagon' };
  if (['chest', 'treasure-chest'].includes(slug)) return { icon: 'poi-collectible-chest', glyph: 'C', color: '#b45309', shape: 'square' };
  if (['effigy', 'lifmunk-effigy'].includes(slug)) return { icon: 'poi-collectible-effigy', glyph: 'E', color: '#16a34a', shape: 'star' };
  if (['journal', 'memo', 'note'].includes(slug)) return { icon: 'poi-collectible-note', glyph: 'N', color: '#2563eb', shape: 'square' };
  if (['egg', 'pal-egg'].includes(slug)) return { icon: 'poi-collectible-egg', glyph: 'E', color: '#db2777', shape: 'circle' };
  if (group === 'collectible') return { icon: 'poi-collectible', glyph: '✦', color: '#ca8a04', shape: 'star' };
  if (slug === 'black-marketeer') return { icon: 'poi-npc-black-market', glyph: 'B', color: '#1e293b', shape: 'pin' };
  if (slug === 'pal-merchant') return { icon: 'poi-npc-pal-merchant', glyph: 'P', color: '#059669', shape: 'pin' };
  if (['wandering-merchant', 'merchant', 'vendor'].includes(slug)) return { icon: 'poi-npc-merchant', glyph: '$', color: '#0d9488', shape: 'pin' };
  if (group === 'npc') return { icon: 'poi-npc', glyph: 'N', color: '#0f766e', shape: 'pin' };
  if (['pal-spawn', 'spawn'].includes(slug)) return { icon: 'poi-pal-spawn', glyph: 'P', color: '#65a30d', shape: 'hexagon' };
  if (slug === 'habitat') return { icon: 'poi-pal-habitat', glyph: 'H', color: '#15803d', shape: 'circle' };
  if (slug === 'lucky-pal') return { icon: 'poi-pal-lucky', glyph: '★', color: '#a3e635', shape: 'star' };
  if (group === 'pal') return { icon: 'poi-pal', glyph: 'P', color: '#65a30d', shape: 'hexagon' };
  if (['region-name', 'region'].includes(slug)) return { icon: 'poi-region', glyph: 'R', color: '#475569', shape: 'circle' };
  if (['special', 'special-location'].includes(slug)) return { icon: 'poi-special', glyph: '◎', color: '#0e7490', shape: 'diamond' };
  if (slug === 'landmark') return { icon: 'poi-landmark', glyph: 'L', color: '#0369a1', shape: 'pin' };
  if (group === 'location') return { icon: 'poi-location', glyph: '•', color: '#0284c7', shape: 'pin' };
  return { icon: 'poi-generic', glyph: '•', color: '#64748b', shape: 'circle' };
};

export const palOpsEntityVisual = (entity: MapEntity): PalOpsMarkerVisual => {
  if (entity.type === 'player' && entity.is_online) return { icon: 'entity-player-online', glyph: 'P', color: '#0ea5e9', shape: 'circle' };
  if (entity.type === 'player') return { icon: 'entity-player-offline', glyph: 'P', color: '#64748b', shape: 'circle' };
  if (entity.type === 'base') return { icon: 'entity-base', glyph: '⌂', color: '#f59e0b', shape: 'diamond' };
  if (entity.type === 'pal') return { icon: 'entity-pal', glyph: 'P', color: '#84cc16', shape: 'hexagon' };
  return { icon: 'entity-marker', glyph: '+', color: '#94a3b8', shape: 'square' };
};

const palOpsMarkerVisualList: PalOpsMarkerVisual[] = [
  { icon: 'poi-fast-travel', glyph: '↯', color: '#2563eb', shape: 'diamond' },
  { icon: 'poi-dungeon', glyph: 'D', color: '#7c3aed', shape: 'square' },
  { icon: 'poi-tower', glyph: 'T', color: '#dc2626', shape: 'triangle' },
  { icon: 'poi-boss', glyph: '★', color: '#e11d48', shape: 'star' },
  { icon: 'poi-enemy-camp', glyph: 'C', color: '#ea580c', shape: 'triangle' },
  { icon: 'poi-event', glyph: '!', color: '#f97316', shape: 'triangle' },
  { icon: 'poi-resource-ore', glyph: 'O', color: '#78716c', shape: 'hexagon' },
  { icon: 'poi-resource-coal', glyph: 'C', color: '#334155', shape: 'hexagon' },
  { icon: 'poi-resource-sulfur', glyph: 'S', color: '#ca8a04', shape: 'hexagon' },
  { icon: 'poi-resource-quartz', glyph: 'Q', color: '#0891b2', shape: 'diamond' },
  { icon: 'poi-resource-oil', glyph: 'OIL', color: '#92400e', shape: 'pin' },
  { icon: 'poi-resource-meteorite', glyph: 'M', color: '#7c3aed', shape: 'star' },
  { icon: 'poi-resource', glyph: '◆', color: '#d97706', shape: 'hexagon' },
  { icon: 'poi-collectible-chest', glyph: 'C', color: '#b45309', shape: 'square' },
  { icon: 'poi-collectible-effigy', glyph: 'E', color: '#16a34a', shape: 'star' },
  { icon: 'poi-collectible-note', glyph: 'N', color: '#2563eb', shape: 'square' },
  { icon: 'poi-collectible-egg', glyph: 'E', color: '#db2777', shape: 'circle' },
  { icon: 'poi-collectible', glyph: '✦', color: '#ca8a04', shape: 'star' },
  { icon: 'poi-npc-black-market', glyph: 'B', color: '#1e293b', shape: 'pin' },
  { icon: 'poi-npc-pal-merchant', glyph: 'P', color: '#059669', shape: 'pin' },
  { icon: 'poi-npc-merchant', glyph: '$', color: '#0d9488', shape: 'pin' },
  { icon: 'poi-npc', glyph: 'N', color: '#0f766e', shape: 'pin' },
  { icon: 'poi-pal-spawn', glyph: 'P', color: '#65a30d', shape: 'hexagon' },
  { icon: 'poi-pal-habitat', glyph: 'H', color: '#15803d', shape: 'circle' },
  { icon: 'poi-pal-lucky', glyph: '★', color: '#a3e635', shape: 'star' },
  { icon: 'poi-pal', glyph: 'P', color: '#65a30d', shape: 'hexagon' },
  { icon: 'poi-region', glyph: 'R', color: '#475569', shape: 'circle' },
  { icon: 'poi-special', glyph: '◎', color: '#0e7490', shape: 'diamond' },
  { icon: 'poi-landmark', glyph: 'L', color: '#0369a1', shape: 'pin' },
  { icon: 'poi-location', glyph: '•', color: '#0284c7', shape: 'pin' },
  { icon: 'poi-generic', glyph: '•', color: '#64748b', shape: 'circle' },
  { icon: 'entity-player-online', glyph: 'P', color: '#0ea5e9', shape: 'circle' },
  { icon: 'entity-player-offline', glyph: 'P', color: '#64748b', shape: 'circle' },
  { icon: 'entity-base', glyph: '⌂', color: '#f59e0b', shape: 'diamond' },
  { icon: 'entity-pal', glyph: 'P', color: '#84cc16', shape: 'hexagon' },
  { icon: 'entity-marker', glyph: '+', color: '#94a3b8', shape: 'square' },
];

export const palOpsMarkerVisuals: Record<string, PalOpsMarkerVisual> = Object.fromEntries(
  palOpsMarkerVisualList.map((visual) => [visual.icon, visual]),
);

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
  const visual = palOpsEntityVisual(entity);
  return {
    key: `entity:${entity.type}:${entity.id}`,
    kind: 'entity',
    label: entity.label,
    mapX: point.x,
    mapY: point.y,
    color: visual.color,
    shape: visual.shape,
    icon: visual.icon,
    glyph: visual.glyph,
    online: entity.is_online,
    entity,
  };
};

export const palOpsPoiToMarker = (poi: PalOpsPoi): PalOpsMapMarker => {
  const visual = palOpsPoiVisual(poi.category);
  return {
    key: `poi:${poi.id}`,
    kind: 'poi',
    label: poi.name,
    mapX: poi.mapX,
    mapY: poi.mapY,
    color: visual.color,
    shape: visual.shape,
    icon: visual.icon,
    glyph: visual.glyph,
    poi,
  };
};

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
