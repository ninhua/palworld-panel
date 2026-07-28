export type PlayerRegionMatch = {
  name: string;
  map_x: number;
  map_y: number;
  distance: number;
  approximate: boolean;
};

type Region = { name: string; x: number; y: number };

const MAP_SIZE = 2048;
const PLAYER_REGIONS: Region[] = [
  { name: '世界树外缘', x: 150, y: 150 },
  { name: '西北雪山', x: 590, y: 390 },
  { name: '樱花岛', x: 430, y: 730 },
  { name: '东北沙丘', x: 1580, y: 520 },
  { name: '中央群岛', x: 1080, y: 1010 },
  { name: '西南火山岛', x: 520, y: 1530 },
  { name: '天坠之地', x: 210, y: 1710 },
  { name: '东南起始群岛', x: 1390, y: 1470 },
];

// Matches the verified projection used by LiveMap. The labels are broad Chinese
// regions, not exact in-game POIs; callers must present them as estimates.
export const projectPlayerWorldToMap = (worldX: number, worldY: number) => {
  const ratio = 458.355;
  const mapRatio = 7.8;
  const leafletSize = 256;
  const adjustedX = worldX + 122500;
  const adjustedY = worldY - 158100;
  const gameX = adjustedX / ratio + (adjustedX > 0 ? 0 : 1);
  const gameY = adjustedY / ratio + (adjustedY > 0 ? 0 : 1);
  const markerLatitude = (gameX - (gameX > 0 ? 0 : 1)) / mapRatio - leafletSize / 2;
  const markerLongitude = (gameY - (gameY > 0 ? 0 : 1)) / mapRatio + leafletSize / 2;
  return {
    x: (markerLongitude / leafletSize) * MAP_SIZE,
    y: (-markerLatitude / leafletSize) * MAP_SIZE,
  };
};

export const estimatePlayerRegion = (worldX: number, worldY: number): PlayerRegionMatch => {
  if (!Number.isFinite(worldX) || !Number.isFinite(worldY) || (worldX === 0 && worldY === 0)) {
    return { name: '位置未记录', map_x: 0, map_y: 0, distance: 0, approximate: true };
  }
  const point = projectPlayerWorldToMap(worldX, worldY);
  let nearest = PLAYER_REGIONS[0];
  let distance = Number.POSITIVE_INFINITY;
  for (const region of PLAYER_REGIONS) {
    const next = Math.hypot(point.x - region.x, point.y - region.y);
    if (next < distance) {
      nearest = region;
      distance = next;
    }
  }
  return {
    name: distance > 720 ? '帕洛斯群岛边缘' : nearest.name,
    map_x: point.x,
    map_y: point.y,
    distance,
    approximate: true,
  };
};
