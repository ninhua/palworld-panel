import type { AxiosResponse } from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { mapSchema, settingsApi } from './settings';

describe('settings dto mapping', () => {
  it('preserves localized labels and enum labels', () => {
    const schema = mapSchema({
      version: '1.0.0',
      fields: [
        {
          key: 'DeathPenalty',
          label: '死亡惩罚',
          group: 'game_balance',
          type: 'enum',
          default: 'All',
          enum: ['None', 'All'],
          enum_labels: {
            None: '不掉落',
            All: '全部掉落（物品、装备和队伍帕鲁）',
          },
          requires_restart: true,
          description: '死亡惩罚。',
        },
      ],
    });

    expect(schema.fields[0]).toMatchObject({
      key: 'DeathPenalty',
      label: '死亡惩罚',
      enum: ['None', 'All'],
      enum_labels: {
        None: '不掉落',
        All: '全部掉落（物品、装备和队伍帕鲁）',
      },
    });
  });

  it('preserves an explicitly unset schema default', () => {
    const schema = mapSchema({
      version: '1.0.0',
      fields: [{ key: 'FutureField', group: 'features', type: 'bool', default: null }],
    });
    expect(schema.fields[0].default).toBeUndefined();
  });
});


describe('settings config revision API', () => {
  afterEach(() => vi.restoreAllMocks());

  it('maps revision metadata and keeps secret diffs redacted', async () => {
    const get = vi.spyOn(apiClient, 'get')
      .mockResolvedValueOnce({
        status: 200,
        data: { ok: true, data: { current_revision_sha256: 'current', retention: 50, items: [{ id: 'rev_1', revision_sha256: 'old', source: 'apply', changed_fields: ['AdminPassword'], created_at: '2026-07-29T00:00:00Z', current: false }] } },
      } as AxiosResponse)
      .mockResolvedValueOnce({
        status: 200,
        data: { ok: true, data: { revision_id: 'rev_1', revision_sha256: 'old', current_sha256: 'current', changes: [{ field: 'AdminPassword', secret: true, revision_configured: true, current_configured: true }] } },
      } as AxiosResponse);

    const revisions = await settingsApi.listRevisions();
    const diff = await settingsApi.getRevisionDiff('rev_1');

    expect(get).toHaveBeenNthCalledWith(1, '/config/palworld/revisions', { params: { limit: 50 } });
    expect(get).toHaveBeenNthCalledWith(2, '/config/palworld/revisions/rev_1/diff');
    expect(revisions.items[0].changed_fields).toEqual(['AdminPassword']);
    expect(diff.changes[0]).toEqual({
      field: 'AdminPassword', secret: true,
      revision_value: undefined, current_value: undefined,
      revision_configured: true, current_configured: true,
    });
    expect(JSON.stringify(diff)).not.toContain('password-value');
  });

  it('requires explicit confirmation when creating a restore draft', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({
      status: 200,
      data: { ok: true, data: { settings: { ServerName: '历史服' }, path: '/srv/PalWorldSettings.ini', pending_restart: false, issues: [], draft: { id: 'cfg_restore', revision_sha256: 'current', status: 'draft', created_at: '2026-07-29T00:00:00Z', updated_at: '2026-07-29T00:00:00Z' } } },
    } as AxiosResponse);

    const restored = await settingsApi.restoreRevision('rev/1');

    expect(post).toHaveBeenCalledWith('/config/palworld/revisions/rev%2F1/restore', { confirm: true });
    expect(restored.draft?.id).toBe('cfg_restore');
  });
});
