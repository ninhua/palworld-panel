import booleanContains from '@turf/boolean-contains';
import booleanOverlap from '@turf/boolean-overlap';
import { polygon } from '@turf/helpers';
import kinks from '@turf/kinks';

export type PalZonesRoleID = 'Player' | 'Otomo' | 'BaseCampPal' | 'PalMonster' | 'WildNPC';
export type PalZonesDamageTarget = PalZonesRoleID | 'Structure';
export type PalZonesWorldAction = 'Build' | 'Dismantle' | 'Ride' | 'Fly' | 'SignEdit' | 'PasswordEdit' | 'Deteriorate';
export type PalZonesMapID = 'world' | 'tree';

export interface PalZonesPoint {
  x: number;
  y: number;
}

export interface PalZonesMapPoint {
  lat: number;
  lng: number;
}

export interface PalZonesPermissionEntry {
  world?: PalZonesWorldAction[];
  damage?: Array<PalZonesDamageTarget | { DamageMultiplier: number }>;
}

export type PalZonesPermissions = Partial<Record<PalZonesRoleID, PalZonesPermissionEntry>>;

export interface PalZonesZone {
  id: string;
  name: string;
  points: PalZonesPoint[];
  permissions: PalZonesPermissions;
  levelRequirement: number;
}

export interface PalZonesDraft {
  globalPermissions: PalZonesPermissions;
  zones: PalZonesZone[];
}

export interface PalZonesValidationIssue {
  code: 'zone_name_required' | 'zone_name_duplicate' | 'zone_too_few_points' | 'zone_invalid_coordinate' | 'zone_level_invalid' | 'zone_out_of_bounds' | 'zone_self_intersection' | 'zone_overlap';
  path: string;
  message: string;
  zoneIndex: number;
}

export interface PalZonesMapDefinition {
  id: PalZonesMapID;
  label: string;
  image: string;
  worldBounds: { minimum: PalZonesPoint; maximum: PalZonesPoint };
}

export interface PermissionDefinition {
  id: PalZonesRoleID;
  label: string;
  damageLabel: string;
}

export const PERMISSION_DEFINITIONS: PermissionDefinition[] = [
  { id: 'Player', label: '玩家', damageLabel: '玩家' },
  { id: 'Otomo', label: '手持帕鲁', damageLabel: '玩家手持帕鲁' },
  { id: 'BaseCampPal', label: '基地帕鲁', damageLabel: '玩家基地帕鲁' },
  { id: 'PalMonster', label: '野生帕鲁', damageLabel: '野生帕鲁' },
  { id: 'WildNPC', label: 'NPC', damageLabel: 'NPC' },
];

export const DAMAGE_TARGETS: Array<{ id: PalZonesDamageTarget; label: string }> = [
  ...PERMISSION_DEFINITIONS.map(({ id, damageLabel }) => ({ id, label: damageLabel })),
  { id: 'Structure', label: '建筑' },
];

export const PLAYER_WORLD_ACTIONS: Array<{ id: PalZonesWorldAction; label: string; group: 'world' | 'effect'; description?: string }> = [
  { id: 'Build', label: '建造', group: 'world' },
  { id: 'Dismantle', label: '拆除', group: 'world' },
  { id: 'Ride', label: '骑乘地面坐骑', group: 'world' },
  { id: 'Fly', label: '骑乘飞行坐骑', group: 'world' },
  { id: 'SignEdit', label: '修改告示牌', group: 'effect', description: '允许修改区域内告示牌内容' },
  { id: 'PasswordEdit', label: '修改密码锁', group: 'effect', description: '允许修改区域内建筑密码' },
  { id: 'Deteriorate', label: '建筑自然劣化', group: 'effect', description: '启用游戏默认的建筑劣化规则' },
];

export const MAP_DEFINITIONS: PalZonesMapDefinition[] = [
  {
    id: 'world',
    label: '主世界',
    image: '/assets/maps/palworld-map.webp',
    worldBounds: { minimum: { x: -1099400, y: -724400 }, maximum: { x: 349400, y: 724400 } },
  },
  {
    id: 'tree',
    label: '世界树',
    image: '/assets/maps/palzones-world-tree.webp',
    worldBounds: { minimum: { x: 347351.5, y: -818197 }, maximum: { x: 689148.5, y: -476400 } },
  },
];

const roleIDs = new Set(PERMISSION_DEFINITIONS.map((role) => role.id));
const damageTargetIDs = new Set(DAMAGE_TARGETS.map((target) => target.id));
const playerActionIDs = new Set(PLAYER_WORLD_ACTIONS.map((action) => action.id));
const TILE_SIZE = 256;

const isRecord = (value: unknown): value is Record<string, unknown> => Boolean(value) && typeof value === 'object' && !Array.isArray(value);
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T;

export const createDefaultPermissions = (): PalZonesPermissions => Object.fromEntries(
  PERMISSION_DEFINITIONS.map((role) => [role.id, {
    ...(role.id === 'Player' ? { world: PLAYER_WORLD_ACTIONS.map((action) => action.id) } : {}),
    damage: [...DAMAGE_TARGETS.map((target) => target.id), { DamageMultiplier: 1 }],
  }]),
) as PalZonesPermissions;

export const createEmptyPalZonesDraft = (): PalZonesDraft => ({
  globalPermissions: createDefaultPermissions(),
  zones: [],
});

export const createPalZonesZone = (permissions: PalZonesPermissions, points: PalZonesPoint[], index = 0): PalZonesZone => ({
  id: `zone-${Date.now()}-${index}`,
  name: `区域 ${index + 1}`,
  points,
  permissions: clone(permissions),
  levelRequirement: 1,
});

const parseDamage = (raw: unknown, path: string): PalZonesPermissionEntry['damage'] => {
  if (!Array.isArray(raw)) throw new Error(`${path} 必须是数组`);
  const output: NonNullable<PalZonesPermissionEntry['damage']> = [];
  for (const [index, item] of raw.entries()) {
    if (typeof item === 'string') {
      if (!damageTargetIDs.has(item as PalZonesDamageTarget)) throw new Error(`${path}[${index}] 包含未知伤害目标 ${item}`);
      if (!output.includes(item as PalZonesDamageTarget)) output.push(item as PalZonesDamageTarget);
      continue;
    }
    if (!isRecord(item) || Object.keys(item).some((key) => key !== 'DamageMultiplier')) throw new Error(`${path}[${index}] 不是合法伤害倍率`);
    const multiplier = Number(item.DamageMultiplier);
    if (!Number.isFinite(multiplier) || multiplier < 0) throw new Error(`${path}[${index}].DamageMultiplier 必须是非负数`);
    if (!output.some((entry) => isRecord(entry))) output.push({ DamageMultiplier: multiplier });
  }
  return output;
};

const parsePermissions = (raw: unknown, path: string, includeDefaults: boolean): PalZonesPermissions => {
  if (raw == null && includeDefaults) return createDefaultPermissions();
  if (!isRecord(raw)) throw new Error(`${path} 必须是对象`);
  for (const role of Object.keys(raw)) {
    if (!roleIDs.has(role as PalZonesRoleID)) throw new Error(`${path}.${role} 是未知权限角色`);
  }
  const defaults = createDefaultPermissions();
  const output: PalZonesPermissions = {};
  for (const definition of PERMISSION_DEFINITIONS) {
    const supplied = raw[definition.id];
    if (supplied == null) {
      if (includeDefaults) output[definition.id] = clone(defaults[definition.id] ?? {});
      continue;
    }
    if (!isRecord(supplied)) throw new Error(`${path}.${definition.id} 必须是对象`);
    if (Object.keys(supplied).some((key) => key !== 'world' && key !== 'damage')) throw new Error(`${path}.${definition.id} 包含未知字段`);
    const entry: PalZonesPermissionEntry = {};
    if (supplied.world != null) {
      if (!Array.isArray(supplied.world)) throw new Error(`${path}.${definition.id}.world 必须是数组`);
      entry.world = supplied.world.map((action, index) => {
        if (typeof action !== 'string' || definition.id !== 'Player' || !playerActionIDs.has(action as PalZonesWorldAction)) {
          throw new Error(`${path}.${definition.id}.world[${index}] 包含未知动作`);
        }
        return action as PalZonesWorldAction;
      }).filter((action, index, actions) => actions.indexOf(action) === index);
    }
    if (supplied.damage != null) entry.damage = parseDamage(supplied.damage, `${path}.${definition.id}.damage`);
    output[definition.id] = entry;
  }
  return output;
};

export const parsePalZones = (content: string): PalZonesDraft => {
  let raw: unknown;
  try {
    raw = JSON.parse(content);
  } catch (error) {
    throw new Error(`zones.json 不是合法 JSON：${error instanceof Error ? error.message : String(error)}`);
  }
  if (!isRecord(raw)) throw new Error('zones.json 顶层必须是对象');
  if (!isRecord(raw.global)) throw new Error('global 必须是对象');
  if (!Array.isArray(raw.zones)) throw new Error('zones 必须是数组');
  const zones = raw.zones.map((value, zoneIndex): PalZonesZone => {
    const path = `zones[${zoneIndex}]`;
    if (!isRecord(value)) throw new Error(`${path} 必须是对象`);
    if (!Array.isArray(value.points)) throw new Error(`${path}.points 必须是数组`);
    const points = value.points.map((point, pointIndex) => {
      if (!isRecord(point)) throw new Error(`${path}.points[${pointIndex}] 必须是对象`);
      return { x: Number(point.x), y: Number(point.y) };
    });
    return {
      id: `zone-${zoneIndex + 1}`,
      name: typeof value.name === 'string' ? value.name : '',
      points,
      permissions: parsePermissions(value.permissions ?? {}, `${path}.permissions`, false),
      levelRequirement: Number(value.levelRequirement),
    };
  });
  return { globalPermissions: parsePermissions(raw.global.permissions, 'global.permissions', true), zones };
};

const serializePermissions = (permissions: PalZonesPermissions, includeAllRoles: boolean): PalZonesPermissions => {
  const result: PalZonesPermissions = {};
  for (const role of PERMISSION_DEFINITIONS) {
    const source = permissions[role.id];
    if (!source && !includeAllRoles) continue;
    const entry: PalZonesPermissionEntry = {};
    if (source?.world?.length) entry.world = [...source.world];
    if (source?.damage?.length) entry.damage = clone(source.damage);
    else if (includeAllRoles) entry.damage = [{ DamageMultiplier: 1 }];
    if (Object.keys(entry).length || includeAllRoles) result[role.id] = entry;
  }
  return result;
};

export const serializePalZones = (draft: PalZonesDraft): string => `${JSON.stringify({
  global: { permissions: serializePermissions(draft.globalPermissions, true) },
  zones: draft.zones.map((zone) => ({
    name: zone.name.trim() || 'PalZones',
    points: zone.points.map((point) => ({ x: point.x.toFixed(2), y: point.y.toFixed(2) })),
    permissions: serializePermissions(zone.permissions, false),
    levelRequirement: zone.levelRequirement,
  })),
}, null, 2)}\n`;

const mapDefinition = (mapID: PalZonesMapID): PalZonesMapDefinition => MAP_DEFINITIONS.find((item) => item.id === mapID) ?? MAP_DEFINITIONS[0];

export const worldToMapPoint = (point: PalZonesPoint, mapID: PalZonesMapID): PalZonesMapPoint => {
  const { minimum, maximum } = mapDefinition(mapID).worldBounds;
  return {
    lat: -TILE_SIZE + ((point.x - minimum.x) / (maximum.x - minimum.x)) * TILE_SIZE,
    lng: ((point.y - minimum.y) / (maximum.y - minimum.y)) * TILE_SIZE,
  };
};

export const mapToWorldPoint = (point: PalZonesMapPoint, mapID: PalZonesMapID): PalZonesPoint => {
  const { minimum, maximum } = mapDefinition(mapID).worldBounds;
  return {
    x: minimum.x + ((point.lat + TILE_SIZE) / TILE_SIZE) * (maximum.x - minimum.x),
    y: minimum.y + (point.lng / TILE_SIZE) * (maximum.y - minimum.y),
  };
};

export const mapContainsPoint = (mapID: PalZonesMapID, point: PalZonesPoint): boolean => {
  const { minimum, maximum } = mapDefinition(mapID).worldBounds;
  return point.x >= minimum.x && point.x <= maximum.x && point.y >= minimum.y && point.y <= maximum.y;
};

export const mapForZone = (zone: PalZonesZone): PalZonesMapID | null => MAP_DEFINITIONS.find((map) => zone.points.every((point) => mapContainsPoint(map.id, point)))?.id ?? null;

const polygonFeature = (zone: PalZonesZone) => polygon([[
  ...zone.points.map((point) => [point.x, point.y]),
  [zone.points[0].x, zone.points[0].y],
]]);

export const validatePalZonesDraft = (draft: PalZonesDraft): PalZonesValidationIssue[] => {
  const issues: PalZonesValidationIssue[] = [];
  const simple = new Map<number, ReturnType<typeof polygonFeature>>();
  const names = new Map<string, number>();
  draft.zones.forEach((zone, zoneIndex) => {
    const prefix = `zones[${zoneIndex}]`;
    const name = zone.name.trim();
    const duplicate = names.get(name.toLocaleLowerCase());
    if (!name) issues.push({ code: 'zone_name_required', path: `${prefix}.name`, message: `区域 ${zoneIndex + 1} 缺少名称`, zoneIndex });
    else if (duplicate != null) issues.push({ code: 'zone_name_duplicate', path: `${prefix}.name`, message: `区域名称与区域 ${duplicate + 1} 重复`, zoneIndex });
    else names.set(name.toLocaleLowerCase(), zoneIndex);
    if (zone.points.length < 3) issues.push({ code: 'zone_too_few_points', path: `${prefix}.points`, message: '区域至少需要 3 个坐标点', zoneIndex });
    if (zone.points.some((point) => !Number.isFinite(point.x) || !Number.isFinite(point.y))) issues.push({ code: 'zone_invalid_coordinate', path: `${prefix}.points`, message: '区域包含无效坐标', zoneIndex });
    if (!Number.isInteger(zone.levelRequirement) || zone.levelRequirement < 1 || zone.levelRequirement > 100) issues.push({ code: 'zone_level_invalid', path: `${prefix}.levelRequirement`, message: '进入等级必须是 1 至 100 的整数', zoneIndex });
    if (zone.points.length >= 3 && !mapForZone(zone)) issues.push({ code: 'zone_out_of_bounds', path: `${prefix}.points`, message: '区域坐标未完整落在主世界或世界树地图内', zoneIndex });
    if (zone.points.length >= 3 && zone.points.every((point) => Number.isFinite(point.x) && Number.isFinite(point.y))) {
      try {
        const feature = polygonFeature(zone);
        if (kinks(feature).features.length) issues.push({ code: 'zone_self_intersection', path: `${prefix}.points`, message: '区域边界存在自交', zoneIndex });
        else simple.set(zoneIndex, feature);
      } catch {
        issues.push({ code: 'zone_self_intersection', path: `${prefix}.points`, message: '区域边界无法构成有效多边形', zoneIndex });
      }
    }
  });
  const polygons = [...simple.entries()];
  for (let left = 0; left < polygons.length; left += 1) {
    for (let right = left + 1; right < polygons.length; right += 1) {
      const [leftIndex, leftFeature] = polygons[left];
      const [rightIndex, rightFeature] = polygons[right];
      if (booleanOverlap(leftFeature, rightFeature) || booleanContains(leftFeature, rightFeature) || booleanContains(rightFeature, leftFeature)) {
        issues.push({ code: 'zone_overlap', path: `zones[${rightIndex}].points`, message: `区域 ${rightIndex + 1} 与区域 ${leftIndex + 1} 重叠`, zoneIndex: rightIndex });
      }
    }
  }
  return issues;
};
