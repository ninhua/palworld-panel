import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { PalOpsRasterFallback } from './PalOpsRasterFallback';

const marker = {
  key: 'poi:test',
  kind: 'poi' as const,
  label: '测试地点',
  mapX: 0,
  mapY: 0,
  color: '#ffffff',
  shape: 'circle' as const,
  icon: 'poi-generic',
  glyph: '•',
};

describe('PalOpsRasterFallback', () => {
  it('renders self-hosted tiles and keeps markers selectable without WebGL', () => {
    const onSelect = vi.fn();
    render(
      <PalOpsRasterFallback
        layerID="palpagos"
        markers={[marker]}
        selectedKey={null}
        tilesAvailable
        reason="MapLibre initialization failed"
        onSelect={onSelect}
      />,
    );

    expect(screen.getByText(/兼容瓦片模式/)).toBeInTheDocument();
    expect(document.querySelector('img[src="/map/palops/tiles/palpagos/1/0/0.webp"]')).not.toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '测试地点' }));
    expect(onSelect).toHaveBeenCalledWith(marker);
  });
  it('supports pointer dragging to pan the compatibility map', () => {
    render(
      <PalOpsRasterFallback
        layerID="palpagos"
        markers={[]}
        selectedKey={null}
        tilesAvailable
        reason="MapLibre initialization failed"
        onSelect={vi.fn()}
      />,
    );

    const viewport = screen.getByLabelText('Palpagos 兼容瓦片地图');
    viewport.scrollLeft = 120;
    viewport.scrollTop = 80;
    fireEvent.pointerDown(viewport, { pointerId: 7, button: 0, clientX: 100, clientY: 100 });
    fireEvent.pointerMove(viewport, { pointerId: 7, clientX: 60, clientY: 70 });
    expect(viewport.scrollLeft).toBe(160);
    expect(viewport.scrollTop).toBe(110);
    fireEvent.pointerUp(viewport, { pointerId: 7, clientX: 60, clientY: 70 });
  });

});
