import { describe, expect, it } from 'vitest';
import { validateMapLibreRuntime } from './maplibreRuntime';

describe('MapLibre browser runtime validation', () => {
  it('accepts the pinned v5 browser global', () => {
    const runtime = validateMapLibreRuntime({
      version: '5.24.0',
      Map: class {},
      NavigationControl: class {},
    });
    expect(runtime.version).toBe('5.24.0');
  });

  it('rejects stale v6 browser globals', () => {
    expect(() => validateMapLibreRuntime({
      version: '6.0.0',
      Map: class {},
      NavigationControl: class {},
    })).toThrow(/version mismatch/);
  });

  it('rejects incomplete globals', () => {
    expect(() => validateMapLibreRuntime(null)).toThrow(/did not initialize/);
    expect(() => validateMapLibreRuntime({ Map: class {} })).toThrow(/incomplete/);
  });
});
