import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Security } from './Security';

const mocks = vi.hoisted(() => ({
  status: vi.fn(),
  releases: vi.fn(),
  getConfig: vi.fn(),
  runtimeCommands: vi.fn(),
  waitForJob: vi.fn(),
}));

vi.mock('../api/security', () => ({
  palDefenderPanelPermissions: [],
  securityApi: {
    status: mocks.status,
    releases: mocks.releases,
    getConfig: mocks.getConfig,
  },
}));

vi.mock('../api/palDefenderGM', () => ({
  palDefenderGMApi: { runtimeCommands: mocks.runtimeCommands },
}));

vi.mock('../api/tasks', () => ({
  tasksApi: { waitForJob: mocks.waitForJob },
}));

describe('Security', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('keeps local status and configuration available when GitHub releases fail', async () => {
    mocks.status.mockResolvedValue({
      installed: true,
      version: '1.8.3',
      release_source: 'github_latest',
      needs_first_start: false,
      files: {},
      paths: {},
      rest_api_enabled: true,
      warnings: [],
      load_verified: true,
      ue4ss: { state: 'installed', version: 'experimental', compatible: true, message: '' },
    });
    mocks.releases.mockRejectedValue(new Error('GitHub timeout'));
    mocks.getConfig.mockResolvedValue({ RESTAPI: { Enabled: true } });

    const { container } = render(<MemoryRouter><Security /></MemoryRouter>);

    expect(await screen.findByText('1.8.3')).toBeInTheDocument();
    expect(screen.getByText('未获取到 Release')).toBeInTheDocument();
    await waitFor(() => {
      const config = container.querySelector('textarea');
      expect(config).not.toBeNull();
      expect(config?.value).toContain('"RESTAPI"');
    });
  });
});
