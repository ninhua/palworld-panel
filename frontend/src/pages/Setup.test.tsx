import React from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Setup } from './Setup';
import type { DockerInstallPlan, DockerMirrorPlan, HostCapabilities, Job, RuntimeMode, ServerStatus, ServerVersionInfo, StartupResponse } from '../types';

const setupApiMock = vi.hoisted(() => ({
  getPrerequisites: vi.fn(),
  getRuntime: vi.fn(),
  getStartup: vi.fn(),
  getHost: vi.fn(),
  getDockerPlan: vi.fn(),
  getDockerMirrorPlan: vi.fn(),
  installDocker: vi.fn(),
  configureDockerMirrors: vi.fn(),
  setRuntime: vi.fn(),
  importServerDirectory: vi.fn(),
  bootstrap: vi.fn(),
  initializeConfig: vi.fn(),
  setStartup: vi.fn(),
}));

const serverApiMock = vi.hoisted(() => ({
  getStatus: vi.fn(),
  getVersion: vi.fn(),
  checkVersion: vi.fn(),
  updateIfNeeded: vi.fn(),
  start: vi.fn(),
}));

const tasksApiMock = vi.hoisted(() => ({
  getJobs: vi.fn(),
  waitForJob: vi.fn(),
}));

vi.mock('../api/setup', () => ({ setupApi: setupApiMock }));
vi.mock('../api/server', () => ({ serverApi: serverApiMock }));
vi.mock('../api/tasks', () => ({
  tasksApi: tasksApiMock,
  isJobDone: (job: { status: string }) => job.status === 'success' || job.status === 'failed',
}));

afterEach(() => {
  cleanup();
});

describe('Setup', () => {
  beforeEach(() => {
    [...Object.values(setupApiMock), ...Object.values(serverApiMock), ...Object.values(tasksApiMock)].forEach((mock) => mock.mockReset());
    serverApiMock.getStatus.mockResolvedValue(baseStatus());
    serverApiMock.getVersion.mockResolvedValue(baseVersion());
    serverApiMock.start.mockResolvedValue({ status: 'started' });
    setupApiMock.getStartup.mockResolvedValue(baseStartup());
    setupApiMock.getRuntime.mockResolvedValue({ mode: 'wine_docker' as RuntimeMode });
    setupApiMock.getPrerequisites.mockResolvedValue([{ id: 'docker', label: 'Docker CLI', ok: false, required: true }]);
    setupApiMock.getHost.mockResolvedValue(linuxHost());
    setupApiMock.getDockerPlan.mockResolvedValue(linuxDockerPlan({ can_auto_install: false, requires_manual: true }));
    setupApiMock.getDockerMirrorPlan.mockResolvedValue(linuxDockerMirrorPlan());
    setupApiMock.importServerDirectory.mockResolvedValue({
      path: String.raw`D:\SteamLibrary\steamapps\common\PalServer`,
      manifest_path: String.raw`D:\SteamLibrary\steamapps\appmanifest_2394010.acf`,
      build_id: '123456',
      config_exists: true,
      already_bound: false,
      original_input: String.raw`D:\SteamLibrary\steamapps\common\PalServer`,
    });
    tasksApiMock.getJobs.mockResolvedValue([]);
  });

  it('shows a prominent manual Docker command on Linux without sudo', async () => {
    render(<Setup />);

    const checkButton = await screen.findByRole('button', { name: '检查 Docker 环境' });
    expect(checkButton).not.toBeDisabled();
    expect(setupApiMock.getDockerPlan).not.toHaveBeenCalled();
    fireEvent.click(checkButton);

    await waitFor(() => expect(setupApiMock.getDockerPlan).toHaveBeenCalled());
    expect(await screen.findByText('需要手动安装 Docker 环境')).toBeInTheDocument();
    expect(screen.getByText(/ADD_CURRENT_USER_TO_DOCKER_GROUP=1/)).toBeInTheDocument();
    expect(screen.getByText(/TARGET_DOCKER_USER='palpanel'/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '复制命令' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '复制完整脚本' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '下载脚本' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '我已执行，重新检查' })).toBeInTheDocument();
    expect(screen.queryByText(/apt-get install -y docker-ce/)).not.toBeInTheDocument();
  });

  it('shows the install and initialize server button when Docker is available on Linux', async () => {
    setupApiMock.getPrerequisites.mockResolvedValue([{ id: 'docker', label: 'Docker CLI', ok: true, required: true }]);
    setupApiMock.getHost.mockResolvedValue(linuxHost({ dockerReady: true }));
    setupApiMock.getDockerPlan.mockResolvedValue(linuxDockerPlan({ docker_ready: true, docker_installed: true, requires_manual: false }));

    render(<Setup />);

    const button = await screen.findByRole('button', { name: '安装并初始化服务端' });
    expect(button).not.toBeDisabled();
    expect(screen.queryByText('需要手动安装 Docker 环境')).not.toBeInTheDocument();
  });

  it('finishes the critical refresh in React StrictMode', async () => {
    setupApiMock.getPrerequisites.mockResolvedValue([{ id: 'docker', label: 'Docker CLI', ok: true, required: true }]);
    setupApiMock.getHost.mockResolvedValue(linuxHost({ dockerReady: true }));
    setupApiMock.getDockerPlan.mockResolvedValue(linuxDockerPlan({ docker_ready: true, docker_installed: true, requires_manual: false }));

    render(
      <React.StrictMode>
        <Setup />
      </React.StrictMode>,
    );

    expect(await screen.findByRole('button', { name: '安装并初始化服务端' })).not.toBeDisabled();
    expect(screen.queryByRole('button', { name: '正在检查环境' })).not.toBeInTheDocument();
  });

  it('shows running server state without waiting for Docker mirror plan', async () => {
    serverApiMock.getStatus.mockResolvedValue({
      ...baseStatus(),
      status: 'running',
      installed: true,
      config_exists: true,
      container: { exists: true, status: 'running' },
      setup_step: 'ready',
    });
    let finishMirrorPlan: (plan: DockerMirrorPlan) => void = () => {};
    setupApiMock.getDockerMirrorPlan.mockImplementation(() => new Promise<DockerMirrorPlan>((resolve) => {
      finishMirrorPlan = resolve;
    }));

    render(<Setup />);

    expect(await screen.findByRole('button', { name: '服务端运行中' })).toBeDisabled();
    expect(screen.queryByRole('button', { name: '正在检查环境' })).not.toBeInTheDocument();
    finishMirrorPlan(linuxDockerMirrorPlan());
  });

  it('does not stay loading when Docker plan fails', async () => {
    setupApiMock.getDockerPlan.mockRejectedValue(new Error('docker plan failed'));

    render(<Setup />);

    const checkButton = await screen.findByRole('button', { name: '检查 Docker 环境' });
    expect(checkButton).not.toBeDisabled();
    fireEvent.click(checkButton);

    expect(await screen.findByText('docker plan failed')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '正在检查环境' })).not.toBeInTheDocument();
  });

  it('retries the same-origin backend without asking for an address', async () => {
    serverApiMock.getStatus.mockRejectedValue(new Error('backend unavailable'));
    setupApiMock.getHost.mockRejectedValue(new Error('backend unavailable'));

    render(<Setup />);

    expect(await screen.findByText('检测失败')).toBeInTheDocument();
    expect(screen.queryByLabelText('后端地址')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '重新检测' }));
    await waitFor(() => expect(serverApiMock.getStatus).toHaveBeenCalledTimes(2));
  });

  it('restores a running setup job after returning to the wizard', async () => {
    const runningJob: Job = {
      id: 'job_running_bootstrap',
      type: 'bootstrap',
      status: 'running',
      progress: 20,
      message: 'building wine runner image',
      created_at: '2026-07-08T06:00:00Z',
    };
    setupApiMock.getPrerequisites.mockResolvedValue([{ id: 'docker', label: 'Docker CLI', ok: true, required: true }]);
    setupApiMock.getHost.mockResolvedValue(linuxHost({ dockerReady: true }));
    setupApiMock.getDockerPlan.mockResolvedValue(linuxDockerPlan({ docker_ready: true, docker_installed: true, requires_manual: false }));
    tasksApiMock.getJobs.mockResolvedValue([runningJob]);
    let finishJob: (job: Job) => void = () => {};
    tasksApiMock.waitForJob.mockImplementation((_id: string, onUpdate?: (job: Job) => void) => {
      onUpdate?.(runningJob);
      return new Promise<Job>((resolve) => {
        finishJob = resolve;
      });
    });

    render(<Setup />);

    expect(await screen.findByText('building wine runner image')).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: '任务执行中' })).toBeDisabled();
    expect(tasksApiMock.waitForJob).toHaveBeenCalledWith('job_running_bootstrap', expect.any(Function), expect.any(Function));
    finishJob({ ...runningJob, status: 'success', progress: 100, message: 'bootstrap completed' });
    await waitFor(() => expect(screen.getByText('bootstrap completed')).toBeInTheDocument());
  });

  it('automatically checks the latest Build when an installed server has no cached result', async () => {
    const versionCheck: Job = {
      id: 'job_version_check',
      type: 'version_check',
      status: 'waiting',
      progress: 0,
      message: 'queued version check',
      created_at: '2026-07-20T00:00:00Z',
    };
    serverApiMock.getStatus.mockResolvedValue({
      ...baseStatus(),
      installed: true,
      config_exists: true,
      setup_step: 'ready',
    });
    serverApiMock.getVersion
      .mockResolvedValueOnce({ ...baseVersion(), installed: true, current_build_id: '24088465' })
      .mockResolvedValue({ ...baseVersion(), installed: true, current_build_id: '24088465', latest_build_id: '24088465' });
    serverApiMock.checkVersion.mockResolvedValue(versionCheck);
    tasksApiMock.waitForJob.mockResolvedValue({ ...versionCheck, status: 'success', progress: 100 });

    render(<Setup />);

    await waitFor(() => expect(serverApiMock.checkVersion).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(tasksApiMock.waitForJob).toHaveBeenCalledWith('job_version_check', expect.any(Function), expect.any(Function)));
    await waitFor(() => expect(serverApiMock.getVersion).toHaveBeenCalledTimes(3));
  });

  it('offers the safe game update as the primary action when a newer Build is available', async () => {
    const updateJob: Job = {
      id: 'job_smart_update',
      type: 'smart_update',
      status: 'waiting',
      progress: 0,
      message: 'queued smart update',
      created_at: '2026-07-31T00:00:00Z',
    };
    serverApiMock.getStatus.mockResolvedValue({
      ...baseStatus(),
      status: 'running',
      installed: true,
      config_exists: true,
      container: { exists: true, status: 'running' },
      setup_step: 'ready',
    });
    serverApiMock.getVersion.mockResolvedValue({
      ...baseVersion(),
      installed: true,
      current_build_id: '24181105',
      latest_build_id: '24466863',
      update_available: true,
      compatibility_target: '1.x',
    });
    setupApiMock.getPrerequisites.mockResolvedValue([{ id: 'docker', label: 'Docker CLI', ok: true, required: true }]);
    setupApiMock.getHost.mockResolvedValue(linuxHost({ dockerReady: true }));
    setupApiMock.getDockerPlan.mockResolvedValue(linuxDockerPlan({ docker_ready: true, docker_installed: true, requires_manual: false }));
    serverApiMock.updateIfNeeded.mockResolvedValue(updateJob);
    tasksApiMock.waitForJob.mockResolvedValue({ ...updateJob, status: 'success', progress: 100, message: 'smart update completed' });

    render(<Setup />);

    const updateButton = await screen.findByRole('button', { name: '更新游戏服务端' });
    expect(updateButton).not.toBeDisabled();
    fireEvent.click(updateButton);
    await waitFor(() => expect(serverApiMock.updateIfNeeded).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(tasksApiMock.waitForJob).toHaveBeenCalledWith('job_smart_update', expect.any(Function), expect.any(Function)));
  });

  it('shows the SteamCMD primary action and hides the Docker install flow on Windows', async () => {
    setupApiMock.getRuntime.mockResolvedValue({ mode: 'windows_steamcmd' as RuntimeMode });
    setupApiMock.getPrerequisites.mockResolvedValue([
      { id: 'windows', label: 'Windows host', ok: true, required: true },
      { id: 'steamcmd', label: 'SteamCMD', ok: false, required: false },
    ]);
    setupApiMock.getHost.mockResolvedValue(windowsHost());
    setupApiMock.getDockerPlan.mockResolvedValue(windowsDockerPlan());
    setupApiMock.getDockerMirrorPlan.mockResolvedValue(windowsDockerMirrorPlan());

    render(<Setup />);

    expect(await screen.findByRole('button', { name: /^我已有服务器/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^帮我安装服务器/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '先选择开服方式' })).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: /^帮我安装服务器/ }));
    expect(await screen.findByRole('button', { name: '安装 SteamCMD 和帕鲁服务端' })).not.toBeDisabled();
    expect(screen.queryByRole('button', { name: '安装 Docker 环境' })).not.toBeInTheDocument();
    expect(screen.queryByText('需要手动安装 Docker 环境')).not.toBeInTheDocument();
  });

  it('imports an existing Windows server directory without copying files', async () => {
    setupApiMock.getRuntime.mockResolvedValue({ mode: 'windows_steamcmd' as RuntimeMode });
    setupApiMock.getPrerequisites.mockResolvedValue([
      { id: 'windows', label: 'Windows host', ok: true, required: true },
    ]);
    setupApiMock.getHost.mockResolvedValue(windowsHost());
    setupApiMock.getDockerPlan.mockResolvedValue(windowsDockerPlan());
    setupApiMock.getDockerMirrorPlan.mockResolvedValue(windowsDockerMirrorPlan());

    render(<Setup />);

    fireEvent.click(await screen.findByRole('button', { name: /^我已有服务器/ }));
    const path = String.raw`D:\SteamLibrary\steamapps\common\PalServer`;
    fireEvent.change(screen.getByLabelText('现有服务端目录'), { target: { value: path } });
    fireEvent.click(screen.getByRole('button', { name: '检查并接管' }));

    await waitFor(() => expect(setupApiMock.importServerDirectory).toHaveBeenCalledWith(path));
    expect(await screen.findByText(`已接管现有服务端：${path}`)).toBeInTheDocument();
    expect(screen.getByText(/接管不会复制游戏文件/)).toBeInTheDocument();
  });

  it('keeps advanced settings collapsed until opened', async () => {
    render(<Setup />);

    const advancedButton = await screen.findByRole('button', { name: /高级设置/ });
    expect(screen.queryByText('Docker 源')).not.toBeInTheDocument();
    expect(screen.queryByText('Docker Hub 镜像加速')).not.toBeInTheDocument();
    expect(screen.queryByText('Runtime 运行方式')).not.toBeInTheDocument();

    fireEvent.click(advancedButton);

    expect(screen.getByText('Docker 源')).toBeInTheDocument();
    expect(screen.getByText('Docker Hub 镜像加速')).toBeInTheDocument();
    expect(screen.getByText('Runtime 运行方式')).toBeInTheDocument();
  });

  it('shows a manual mirror command after automatic mirror configuration requires sudo', async () => {
    setupApiMock.getPrerequisites.mockResolvedValue([{ id: 'docker', label: 'Docker CLI', ok: true, required: true }]);
    setupApiMock.getHost.mockResolvedValue(linuxHost({ dockerReady: true }));
    setupApiMock.getDockerPlan.mockResolvedValue(linuxDockerPlan({ docker_ready: true, docker_installed: true, requires_manual: false }));
    setupApiMock.getDockerMirrorPlan.mockResolvedValue(linuxDockerMirrorPlan({ can_auto_configure: true, requires_manual: false }));
    setupApiMock.configureDockerMirrors.mockRejectedValue(Object.assign(new Error('sudo required'), { code: 'sudo_required' }));

    render(<Setup />);

    fireEvent.click(await screen.findByRole('button', { name: /高级设置/ }));
    await waitFor(() => expect(screen.getByRole('button', { name: '配置镜像加速' })).not.toBeDisabled());
    const configureButton = screen.getByRole('button', { name: '配置镜像加速' });
    fireEvent.click(configureButton);

    await waitFor(() => expect(setupApiMock.configureDockerMirrors).toHaveBeenCalled());
    await waitFor(() => expect(screen.getAllByText('需要手动配置 Docker 镜像加速').length).toBeGreaterThan(0));
    expect(screen.getByText(/sudo bash \/data\/tools\/configure-docker-mirrors.sh/)).toBeInTheDocument();
    expect(screen.queryByText(/registry-mirrors/)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '查看镜像加速脚本' }));
    expect(screen.getByText(/registry-mirrors/)).toBeInTheDocument();
  });
});

const baseStatus = (): ServerStatus => ({
  status: 'stopped',
  installed: false,
  pending_restart: false,
  runtime_mode: 'wine_docker',
  setup_step: 'prerequisites',
  config_exists: false,
  container: { exists: false, status: 'missing' },
  startup_args: [],
  ports: { game: 8211, query: 27015, rest: 8212 },
  warnings: [],
  paths: {},
  server_imported: false,
});

const baseVersion = (): ServerVersionInfo => ({
  installed: false,
  current_build_id: '',
  latest_build_id: '',
  update_available: false,
  last_checked_at: '',
  source: '',
  manifest_path: '',
  compatibility_warnings: [],
});

const baseStartup = (): StartupResponse => ({
  startup: {
    port: 8211,
    players: 32,
    public_lobby: false,
    public_ip: '',
    public_port: 8211,
    log_format: 'text',
    use_perf_threads: true,
    no_async_loading_thread: true,
    use_multithread_for_ds: true,
    number_of_worker_threads_server: 0,
    workshop_dir: '',
    no_mods: false,
  },
  args: [],
  issues: [],
});

const linuxHost = (options: { dockerReady?: boolean } = {}): HostCapabilities => ({
  os: 'linux',
  arch: 'amd64',
  distro_id: 'debian',
  distro_name: 'Debian',
  distro_version: '13',
  distro_codename: 'trixie',
  package_manager: 'apt',
  systemd: true,
  supported: true,
  recommended_runtime: 'wine_docker',
  docker: {
    cli_installed: Boolean(options.dockerReady),
    daemon_reachable: Boolean(options.dockerReady),
    version: options.dockerReady ? '27.0.0' : undefined,
    error: options.dockerReady ? undefined : 'Docker CLI not found',
  },
  sudo: {
    is_root: false,
    sudo_installed: true,
    passwordless: false,
    can_elevate: false,
    needs_password: true,
    message: 'sudo requires a password',
  },
  current_user: 'palpanel',
  current_user_in_docker_group: false,
  warnings: [],
});

const windowsHost = (): HostCapabilities => ({
  os: 'windows',
  arch: 'amd64',
  systemd: false,
  supported: true,
  recommended_runtime: 'windows_steamcmd',
  docker: {
    cli_installed: false,
    daemon_reachable: false,
  },
  sudo: {
    is_root: false,
    sudo_installed: false,
    passwordless: false,
    can_elevate: false,
    needs_password: false,
  },
  current_user_in_docker_group: false,
  warnings: [],
});

const linuxDockerPlan = (overrides: Partial<DockerInstallPlan> = {}): DockerInstallPlan => ({
  host: linuxHost(),
  source: 'official',
  source_url: 'https://download.docker.com/linux/debian',
  sources: [],
  supported: true,
  can_auto_install: false,
  requires_manual: true,
  docker_installed: false,
  docker_ready: false,
  manual_command: 'sudo bash /data/tools/install-docker.sh',
  script: '#!/usr/bin/env bash\napt-get install -y docker-ce docker-ce-cli containerd.io\n',
  script_path: '/data/tools/install-docker.sh',
  warnings: [],
  ...overrides,
});

const windowsDockerPlan = (): DockerInstallPlan => ({
  host: windowsHost(),
  source: '',
  sources: [],
  supported: false,
  can_auto_install: false,
  requires_manual: false,
  docker_installed: false,
  docker_ready: false,
  warnings: [],
});

const linuxDockerMirrorPlan = (overrides: Partial<DockerMirrorPlan> = {}): DockerMirrorPlan => ({
  host: linuxHost({ dockerReady: true }),
  mirror: 'auto',
  mirrors: [
    {
      id: 'one_ms',
      name: '1ms',
      url: 'https://docker.1ms.run',
      probe_url: 'https://docker.1ms.run/v2/',
      available: true,
      latency_ms: 20,
      selected: true,
    },
  ],
  selected_mirrors: ['https://docker.1ms.run'],
  existing_mirrors: [],
  config_path: '/etc/docker/daemon.json',
  supported: true,
  can_auto_configure: false,
  requires_manual: true,
  docker_installed: true,
  docker_ready: true,
  manual_command: 'sudo bash /data/tools/configure-docker-mirrors.sh',
  script: '#!/usr/bin/env bash\n# registry-mirrors\n',
  script_path: '/data/tools/configure-docker-mirrors.sh',
  warnings: [],
  ...overrides,
});

const windowsDockerMirrorPlan = (): DockerMirrorPlan => ({
  host: windowsHost(),
  mirror: 'auto',
  mirrors: [],
  selected_mirrors: [],
  existing_mirrors: [],
  config_path: '/etc/docker/daemon.json',
  supported: false,
  can_auto_configure: false,
  requires_manual: false,
  docker_installed: false,
  docker_ready: false,
  warnings: [],
});
