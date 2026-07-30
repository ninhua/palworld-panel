import { describe, expect, it } from 'vitest';
import { createPalOpsMapOptions } from './PalOpsMapViewport';

describe('PalOpsMapViewport MapLibre options', () => {
  it('passes a concrete canvas limit instead of relying on the runtime default', () => {
    const container = document.createElement('div');
    const options = createPalOpsMapOptions(container, 'palpagos', false, {
      type: 'FeatureCollection',
      features: [],
    });

    expect(options.maxCanvasSize).toEqual([4096, 4096]);
    expect(options.maxCanvasSize).not.toBe(null);
    expect(options.dragPan).toBe(true);
    expect(options.scrollZoom).toBe(true);
    expect(options.touchZoomRotate).toBe(true);
    const style = options.style as { layers: Array<{ id: string; type: string }> };
    expect(style.layers).toEqual(expect.arrayContaining([expect.objectContaining({ id: 'palops-marker-icons', type: 'symbol' })]));
  });


});
