import React from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { settingsApi } from '../api/settings';
import type { PalworldConfigRevisionDiff } from '../types';
import { ConfigRevisionHistory } from './ConfigRevisionHistory';

const revisions = [
  {
    id: 'rev_a', revision_sha256: 'a'.repeat(64), source: 'baseline', changed_fields: ['ServerName'],
    created_at: '2026-07-29T00:00:00Z', current: false,
  },
  {
    id: 'rev_b', revision_sha256: 'b'.repeat(64), parent_sha256: 'a'.repeat(64), source: 'apply',
    changed_fields: ['AdminPassword'], created_at: '2026-07-29T01:00:00Z', current: false,
  },
];

const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
};

describe('ConfigRevisionHistory', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(settingsApi, 'listRevisions').mockResolvedValue({
      current_revision_sha256: 'c'.repeat(64), retention: 50, items: revisions,
    });
  });

  afterEach(() => cleanup());

  it('ignores a stale diff response after another revision is selected', async () => {
    const first = deferred<PalworldConfigRevisionDiff>();
    const second = deferred<PalworldConfigRevisionDiff>();
    vi.spyOn(settingsApi, 'getRevisionDiff').mockImplementation((id) => id === 'rev_a' ? first.promise : second.promise);

    render(<ConfigRevisionHistory />);
    const baseline = await screen.findByText('基线快照');
    const applied = screen.getByText('配置应用');
    fireEvent.click(baseline.closest('button')!);
    fireEvent.click(applied.closest('button')!);

    await act(async () => {
      second.resolve({
        revision_id: 'rev_b', revision_sha256: 'b'.repeat(64), current_sha256: 'c'.repeat(64),
        changes: [{ field: 'ServerName', secret: false, revision_value: 'B-history', current_value: 'B-current', revision_configured: true, current_configured: true }],
      });
    });
    expect(await screen.findByText('当前：B-current')).toBeInTheDocument();

    await act(async () => {
      first.resolve({
        revision_id: 'rev_a', revision_sha256: 'a'.repeat(64), current_sha256: 'c'.repeat(64),
        changes: [{ field: 'ServerName', secret: false, revision_value: 'A-history', current_value: 'A-current', revision_configured: true, current_configured: true }],
      });
    });
    await waitFor(() => expect(screen.queryByText('当前：A-current')).not.toBeInTheDocument());
    expect(screen.getByText('当前：B-current')).toBeInTheDocument();
  });

  it('renders secret changes as configured state only', async () => {
    vi.spyOn(settingsApi, 'getRevisionDiff').mockResolvedValue({
      revision_id: 'rev_b', revision_sha256: 'b'.repeat(64), current_sha256: 'c'.repeat(64),
      changes: [{ field: 'AdminPassword', secret: true, revision_configured: true, current_configured: false }],
    });

    render(<ConfigRevisionHistory />);
    const applied = await screen.findByText('配置应用');
    fireEvent.click(applied.closest('button')!);
    expect(await screen.findByText('历史：已配置')).toBeInTheDocument();
    expect(screen.getByText('当前：未配置')).toBeInTheDocument();
  });
});
