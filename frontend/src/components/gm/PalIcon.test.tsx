import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { PalIcon } from './PalIcon';

describe('PalIcon', () => {
  it('uses packaged assets instead of a remote GitHub URL', () => {
    render(<PalIcon characterID="WingGolem_Fire" name="丹烽" />);
    const image = screen.getByRole('img', { name: '丹烽图标' });
    expect(image).toHaveAttribute('src', '/assets/pals/winggolem_fire.png');
    expect(image.getAttribute('src')).not.toContain('githubusercontent.com');
  });
});
