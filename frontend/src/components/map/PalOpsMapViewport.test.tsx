import { describe, expect, it, vi } from 'vitest';
import { createPalOpsMapOptions, mapLibreWebGL2CompatibilityIssue } from './PalOpsMapViewport';

describe('PalOpsMapViewport MapLibre options', () => {
  it('passes a concrete canvas limit instead of relying on the runtime default', () => {
    const container = document.createElement('div');
    const options = createPalOpsMapOptions(container, 'palpagos', false, {
      type: 'FeatureCollection',
      features: [],
    });

    expect(options.maxCanvasSize).toEqual([4096, 4096]);
    expect(options.maxCanvasSize).not.toBe(null);
  });

  it('rejects null WebGL capability arrays before MapLibre reads index zero', () => {
    const getParameter = vi.fn((parameter: number) => parameter === 1 ? null : 4096);
    const getContext = vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
      MAX_VIEWPORT_DIMS: 1,
      MAX_TEXTURE_SIZE: 2,
      getParameter,
    } as unknown as WebGL2RenderingContext);

    expect(mapLibreWebGL2CompatibilityIssue()).toContain('无效的 WebGL2 视口能力');
    getContext.mockRestore();
  });

});
