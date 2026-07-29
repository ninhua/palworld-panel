import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Monitor } from './Monitor';

const mocks = vi.hoisted(() => ({
  snapshot: vi.fn(),
  history: vi.fn(),
  debugStatus: vi.fn(),
  setDebug: vi.fn(),
  crashGuardStatus: vi.fn(),
  recoverCrashGuard: vi.fn(),
}));

vi.mock('../api/monitor', () => ({ monitorApi: mocks }));
vi.mock('../store/useServerStore', () => ({
  useServerStore: () => ({ refreshKey: 0, autoRefresh: false }),
}));

describe('Monitor diagnostics', () => {
  beforeEach(() => {
	vi.clearAllMocks();
	const sample = {
	  id: 'sample-1', created_at: '2026-07-22T01:00:00Z', cpu_available: true, cpu_percent: 12,
	  memory_available: true, memory_usage_bytes: 6442450944, memory_limit_bytes: 8589934592,
	  host_memory_available: true, host_memory_total_bytes: 17179869184, host_memory_available_bytes: 4294967296,
	  host_swap_total_bytes: 4294967296, host_swap_free_bytes: 1073741824,
	  workload_memory_available: true, workload_memory_usage_bytes: 6442450944, workload_memory_limit_bytes: 8589934592,
	  lifecycle_available: true, oom_killed: true, exit_code: 137, restart_count: 3,
	  started_at: '2026-07-22T00:00:00Z', finished_at: '2026-07-22T00:59:00Z',
	  risk_reasons: [{ code: 'oom_killed', message: '工作负载被 OOM 终止', severity: 'critical' }],
	  disk_available: true, disk_free_bytes: 1, disk_total_bytes: 2, current_players: 2, max_players: 32,
	  rest_healthy: true, rcon_healthy: true, game_port_healthy: false, query_port_healthy: false,
	};
	mocks.snapshot.mockResolvedValue({ sample });
	mocks.history.mockResolvedValue([sample]);
	mocks.debugStatus.mockResolvedValue({ enabled: false, path: '', size: 0, max_bytes: 1024, max_files: 2 });
	mocks.crashGuardStatus.mockResolvedValue({ tripped: false, reason: '', threshold: 3, recent_crash_count: 0, window_seconds: 600, events: [] });
  });

  it('renders host, workload, swap, OOM and exit details separately', async () => {
	render(<MemoryRouter><Monitor /></MemoryRouter>);

	expect(await screen.findByText('主机内存')).toBeInTheDocument();
	expect(screen.getByText('工作负载内存')).toBeInTheDocument();
	expect(screen.getByText('交换空间')).toBeInTheDocument();
	expect(screen.getByText('OOM 已发生')).toBeInTheDocument();
	expect(screen.getByText('退出码 137')).toBeInTheDocument();
	expect(screen.getByText('重启 3 次')).toBeInTheDocument();
	expect(screen.getByText('工作负载被 OOM 终止')).toBeInTheDocument();
	expect(screen.getByText('CPU / 工作负载内存历史')).toBeInTheDocument();
	expect(screen.getByRole('link', { name: '前往手动安全重启' })).toHaveAttribute('href', '/dashboard');
  });

  it('renders unknown lifecycle facts as not collected', async () => {
	const startedAt = '2026-07-22T00:00:00Z';
	const unavailable = { ...(await mocks.snapshot()).sample, lifecycle_available: false, oom_killed: false, restart_count: 0, started_at: startedAt, finished_at: undefined };
	mocks.snapshot.mockResolvedValue({ sample: unavailable });
	mocks.history.mockResolvedValue([unavailable]);
	render(<MemoryRouter><Monitor /></MemoryRouter>);
	expect(await screen.findByText('OOM 未采集')).toBeInTheDocument();
	expect(screen.getByText(`启动：${new Date(startedAt).toLocaleString('zh-CN')}`)).toBeInTheDocument();
	expect(screen.getByText('退出状态未采集')).toBeInTheDocument();
	expect(screen.getByText('重启次数未采集')).toBeInTheDocument();
  });
});
