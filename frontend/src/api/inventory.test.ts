import { describe, expect, it } from 'vitest';
import { inventoryApi, mapGlobalInventoryResponse } from './inventory';

describe('inventoryApi', () => {
  it('exposes the global inventory list operation', () => {
    expect(typeof inventoryApi.list).toBe('function');
  });

  it('maps world-scoped unattended inventory state', () => {
    const result = mapGlobalInventoryResponse({
      unattended: {
        available: true,
        status: 'completed',
        world_id: 'WORLD-A',
        duration_seconds: 900,
        qualified: true,
        total_added: 35,
        additions: [{ item_id: 'Wood', item_name: '木材', item_icon: 'Wood', category: '材料', quantity: 35 }],
      },
    });
    expect(result.unattended.world_id).toBe('WORLD-A');
    expect(result.unattended.duration_seconds).toBe(900);
    expect(result.unattended.additions).toEqual([
      { item_id: 'Wood', item_name: '木材', item_icon: 'Wood', category: '材料', quantity: 35 },
    ]);
  });

  it('uses an unavailable empty state when the server omits the field', () => {
    const result = mapGlobalInventoryResponse({});
    expect(result.unattended).toMatchObject({
      available: false,
      status: 'unavailable',
      additions: [],
      total_added: 0,
    });
  });
});
