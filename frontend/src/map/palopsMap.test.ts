import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  detectPalOpsLayer,
  isPalOpsPoi,
  lngLatToMapPoint,
  loadPalOpsMapManifest,
  loadPalOpsPois,
  mapPointToLngLat,
  mapPointToPixel,
  normalizePalOpsLocale,
  palOpsMapLayers,
  palOpsPoiGroup,
  worldToPalOpsMap,
} from './palopsMap';

describe('PalOps map projection', () => {
  it('matches the pinned PalOps affine calibration', () => {
    const point = worldToPalOpsMap(-83955, -161464);
    expect(point.x).toBeCloseTo(-696, 3);
    expect(point.y).toBeCloseTo(87, 3);
  });

  it('detects World Tree coordinates separately from Palpagos', () => {
    const transform = palOpsMapLayers['world-tree'].worldToMap;
    const mapX = -1800;
    const mapY = 1400;
    const determinant = transform.a * transform.e - transform.b * transform.d;
    const worldX = (transform.e * (mapX - transform.c) - transform.b * (mapY - transform.f)) / determinant;
    const worldY = (-transform.d * (mapX - transform.c) + transform.a * (mapY - transform.f)) / determinant;
    expect(detectPalOpsLayer(worldX, worldY)).toBe('world-tree');
  });

  it('maps custom PalOps coordinates through standard Web Mercator tiles', () => {
    const layer = palOpsMapLayers.palpagos;
    const northwest = mapPointToLngLat({ x: layer.bounds.minimumX, y: layer.bounds.maximumY }, layer);
    const southeast = mapPointToLngLat({ x: layer.bounds.maximumX, y: layer.bounds.minimumY }, layer);
    expect(northwest[0]).toBeCloseTo(-180, 6);
    expect(northwest[1]).toBeCloseTo(85.05112878, 6);
    expect(southeast[0]).toBeCloseTo(180, 6);
    expect(southeast[1]).toBeCloseTo(-85.05112878, 6);
    const roundtrip = lngLatToMapPoint(...mapPointToLngLat({ x: -696, y: 87 }, layer), layer);
    expect(roundtrip.x).toBeCloseTo(-696, 6);
    expect(roundtrip.y).toBeCloseTo(87, 6);
  });

  it('maps layer bounds to the tile-pyramid pixel bounds', () => {
    const layer = palOpsMapLayers.palpagos;
    expect(mapPointToPixel({ x: layer.bounds.minimumX, y: layer.bounds.maximumY }, layer, 2)).toEqual({ x: 0, y: 0 });
    expect(mapPointToPixel({ x: layer.bounds.maximumX, y: layer.bounds.minimumY }, layer, 2)).toEqual({ x: 2048, y: 2048 });
  });
});

describe('PalOps map data helpers', () => {
  it('normalizes the locales supported by PalPanel', () => {
    expect(normalizePalOpsLocale('en-GB')).toBe('en-US');
    expect(normalizePalOpsLocale('zh-CN')).toBe('zh-CN');
  });

  it('groups the fixed POI categories', () => {
    expect(palOpsPoiGroup('poi-location-fast-travel')).toBe('location');
    expect(palOpsPoiGroup('poi-resource-oil')).toBe('resource');
    expect(palOpsPoiGroup('unknown')).toBeNull();
  });
  it('rejects malformed localized POI arrays before spreading aliases', () => {
    expect(isPalOpsPoi({
      id: 'poi-1', type: 'location', category: 'poi-location-special', map: 'palpagos', name: '测试',
      aliases: 'not-an-array', keywords: [], mapX: 0, mapY: 0, worldX: 0, worldY: 0,
      source: 'fixture', license: 'CC-BY-SA-4.0', version: 'fixture', iconId: 'special',
    })).toBe(false);
  });

});


describe('PalPanel map asset loading', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('accepts the dedicated asset repository and uses the manifest POI count', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({
        schema_version: 1,
        source: {
          repository: 'ninhua/palpanel-assets',
          commit: '0123456789abcdef0123456789abcdef01234567',
          ref: 'main',
          version: 'fixture-assets-1',
        },
        dataset_version: 'fixture-dataset-1',
        maps: ['palpagos', 'world-tree'],
        locales: ['zh-CN', 'en-US', 'ja-JP'],
        poi_total: 1,
        category_counts: {},
        tiles_available: true,
        tile_policy: 'fixture',
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify([{
        id: 'poi-1', type: 'location', category: 'poi-location-special', map: 'palpagos', name: '测试',
        aliases: [], keywords: [], mapX: 0, mapY: 0, worldX: 0, worldY: 0,
        source: 'fixture', license: 'CC-BY-SA-4.0', version: 'fixture', iconId: 'special',
      }]), { status: 200 }));

    const manifest = await loadPalOpsMapManifest();
    const pois = await loadPalOpsPois('zh-CN', manifest.poi_total);

    expect(manifest.source.repository).toBe('ninhua/palpanel-assets');
    expect(pois).toHaveLength(1);
  });
});
