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
});
