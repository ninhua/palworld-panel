import { describe, expect, it } from 'vitest';
import { estimatePlayerRegion, projectPlayerWorldToMap } from './playerRegion';

describe('player region projection', () => {
  it('returns finite map coordinates for save coordinates', () => {
    const point = projectPlayerWorldToMap(-122500, 158100);
    expect(Number.isFinite(point.x)).toBe(true);
    expect(Number.isFinite(point.y)).toBe(true);
  });

  it('does not invent a region for an absent coordinate', () => {
    expect(estimatePlayerRegion(0, 0).name).toBe('位置未记录');
  });

  it('always marks region labels as approximate', () => {
    expect(estimatePlayerRegion(-122500, 158100).approximate).toBe(true);
  });
});
