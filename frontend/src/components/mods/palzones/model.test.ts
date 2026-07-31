import { describe, expect, it } from 'vitest';
import {
  MAP_DEFINITIONS,
  mapToWorldPoint,
  parsePalZones,
  serializePalZones,
  validatePalZonesDraft,
  worldToMapPoint,
  type PalZonesDraft,
} from './model';

const validConfig = {
  global: {
    permissions: {
      Player: {
        world: ['Build', 'Dismantle', 'Ride', 'Fly'],
        damage: ['Player', 'Otomo', 'BaseCampPal', 'PalMonster', 'WildNPC', 'Structure', { DamageMultiplier: 1 }],
      },
    },
  },
  zones: [{
    name: '新手保护区',
    points: [
      { x: '-1000.125', y: '0' },
      { x: '1000', y: '0' },
      { x: '0', y: '1000.5' },
    ],
    permissions: { Player: { world: ['Build'], damage: ['Player', { DamageMultiplier: 0.5 }] } },
    levelRequirement: 12,
  }],
};

describe('PalZones data model', () => {
  it('parses upstream coordinate strings and exports two-decimal compatible JSON', () => {
    const draft = parsePalZones(JSON.stringify(validConfig));

    expect(draft.zones[0].points).toEqual([
      { x: -1000.125, y: 0 },
      { x: 1000, y: 0 },
      { x: 0, y: 1000.5 },
    ]);

    const output = JSON.parse(serializePalZones(draft));
    expect(output.zones[0].points).toEqual([
      { x: '-1000.13', y: '0.00' },
      { x: '1000.00', y: '0.00' },
      { x: '0.00', y: '1000.50' },
    ]);
    expect(output.zones[0].permissions.Player.damage.at(-1)).toEqual({ DamageMultiplier: 0.5 });
  });

  it('round-trips world coordinates through both upstream map definitions', () => {
    for (const definition of MAP_DEFINITIONS) {
      const point = {
        x: (definition.worldBounds.minimum.x + definition.worldBounds.maximum.x) / 2,
        y: (definition.worldBounds.minimum.y + definition.worldBounds.maximum.y) / 2,
      };
      const mapped = worldToMapPoint(point, definition.id);
      const restored = mapToWorldPoint(mapped, definition.id);
      expect(restored.x).toBeCloseTo(point.x, 5);
      expect(restored.y).toBeCloseTo(point.y, 5);
    }
  });

  it('blocks self-intersecting, overlapping, and out-of-bounds zones', () => {
    const draft: PalZonesDraft = parsePalZones(JSON.stringify(validConfig));
    draft.zones = [
      { ...draft.zones[0], id: 'self', name: '自交区域', points: [
        { x: 0, y: 0 }, { x: 100, y: 100 }, { x: 0, y: 100 }, { x: 100, y: 0 },
      ] },
      { ...draft.zones[0], id: 'one', name: '区域一', points: [
        { x: 1000, y: 1000 }, { x: 2000, y: 1000 }, { x: 2000, y: 2000 }, { x: 1000, y: 2000 },
      ] },
      { ...draft.zones[0], id: 'two', name: '区域二', points: [
        { x: 1500, y: 1500 }, { x: 2500, y: 1500 }, { x: 2500, y: 2500 }, { x: 1500, y: 2500 },
      ] },
      { ...draft.zones[0], id: 'outside', name: '越界区域', points: [
        { x: 900000, y: 900000 }, { x: 901000, y: 900000 }, { x: 900000, y: 901000 },
      ] },
    ];

    const codes = validatePalZonesDraft(draft).map((issue) => issue.code);
    expect(codes).toContain('zone_self_intersection');
    expect(codes).toContain('zone_overlap');
    expect(codes).toContain('zone_out_of_bounds');
  });
});
