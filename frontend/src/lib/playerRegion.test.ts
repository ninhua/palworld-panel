import { describe, expect, it } from 'vitest';
import { estimatePlayerRegion, projectPlayerWorldToMap } from './playerRegion';

describe('player region projection', () => {
  it('uses the PalOps affine projection for save coordinates', () => {
    const point = projectPlayerWorldToMap(-83955, -161464);
    expect(point.x).toBeCloseTo(-696, 3);
    expect(point.y).toBeCloseTo(87, 3);
  });

  it('does not invent a region for an absent coordinate', () => {
    expect(estimatePlayerRegion(0, 0).name).toBe('位置未记录');
  });

  it('recognizes the World Tree map before estimating a broad region', () => {
    const match = estimatePlayerRegion(518300, -647300);
    expect(match.map_id).toBe('world-tree');
    expect(match.approximate).toBe(true);
  });

  it('always marks region labels as approximate', () => {
    expect(estimatePlayerRegion(-83955, -161464).approximate).toBe(true);
  });
});
