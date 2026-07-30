import { describe, expect, it } from 'vitest';
import { mapPatchInfo } from './patchInfo';

describe('patch info API', () => {
  it('normalizes the visible panel version', () => {
    expect(mapPatchInfo({ patch: { version: '0.8.44', features: null } })).toMatchObject({
      patch: { version: '0.8.44', features: [] },
    });
  });
});
