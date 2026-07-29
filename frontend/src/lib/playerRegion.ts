import {
  detectPalOpsLayer,
  palOpsMapLayers,
  worldToPalOpsMap,
  type PalOpsMapLayerID,
} from '../map/palopsMap';

export type PlayerRegionMatch = {
  name: string;
  map_id: PalOpsMapLayerID;
  map_x: number;
  map_y: number;
  distance: number;
  approximate: boolean;
};

type Region = { name: string; map: PalOpsMapLayerID; x: number; y: number };

// Broad operational labels only. The coordinates use the same PalOps affine
// projection as the world map; they are not exact game POIs.
const PLAYER_REGIONS: Region[] = [
  { name: '世界树区域', map: 'world-tree', x: -1754.463, y: 1398.993 },
  { name: '西北雪山', map: 'palpagos', x: -1013.117, y: 430.050 },
  { name: '樱花岛', map: 'palpagos', x: -1259.712, y: -93.966 },
  { name: '东北沙丘', map: 'palpagos', x: 512.693, y: 229.691 },
  { name: '中央群岛', map: 'palpagos', x: -257.918, y: -525.508 },
  { name: '西南火山岛', map: 'palpagos', x: -1121.002, y: -1326.943 },
  { name: '天坠之地', map: 'palpagos', x: -1598.781, y: -1604.363 },
  { name: '东南起始群岛', map: 'palpagos', x: 219.861, y: -1234.470 },
];

export const projectPlayerWorldToMap = (worldX: number, worldY: number) => worldToPalOpsMap(worldX, worldY);

export const estimatePlayerRegion = (worldX: number, worldY: number): PlayerRegionMatch => {
  if (!Number.isFinite(worldX) || !Number.isFinite(worldY) || (worldX === 0 && worldY === 0)) {
    return { name: '位置未记录', map_id: 'palpagos', map_x: 0, map_y: 0, distance: 0, approximate: true };
  }
  const point = projectPlayerWorldToMap(worldX, worldY);
  const mapID = detectPalOpsLayer(worldX, worldY);
  const candidates = PLAYER_REGIONS.filter((region) => region.map === mapID);
  let nearest = candidates[0];
  let distance = Number.POSITIVE_INFINITY;
  for (const region of candidates) {
    const next = Math.hypot(point.x - region.x, point.y - region.y);
    if (next < distance) {
      nearest = region;
      distance = next;
    }
  }
  const layer = palOpsMapLayers[mapID];
  const diagonal = Math.hypot(
    layer.bounds.maximumX - layer.bounds.minimumX,
    layer.bounds.maximumY - layer.bounds.minimumY,
  );
  return {
    name: distance > diagonal * 0.30 ? (mapID === 'world-tree' ? '世界树边缘' : '帕洛斯群岛边缘') : nearest.name,
    map_id: mapID,
    map_x: point.x,
    map_y: point.y,
    distance,
    approximate: true,
  };
};
