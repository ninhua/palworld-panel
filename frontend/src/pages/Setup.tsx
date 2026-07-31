import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  Copy,
  Download,
  FileDown,
  FileText,
  FolderOpen,
  Gauge,
  HardDriveDownload,
  Play,
  RefreshCw,
  Save,
  Server,
  Settings2,
  Terminal,
  Wand2,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { setupApi } from '../api/setup';
import { serverApi } from '../api/server';
import { isJobDone, tasksApi } from '../api/tasks';
import type {
  DockerInstallPlan,
  DockerMirrorID,
  DockerMirrorPlan,
  DockerSourceID,
  HostCapabilities,
  Job,
  Prerequisite,
  RuntimeMode,
  ServerStatus,
  ServerVersionInfo,
  StartupConfig,
  StartupResponse,
} from '../types';
import { StatusBadge } from '../components/ui/StatusBadge';

const runtimeLabels: Record<RuntimeMode, string> = {
  windows_steamcmd: 'Windows SteamCMD（推荐正式服）',
  wine_docker: 'Docker + Wine（兼容 Windows Mod）',
};

const dockerMirrorOptions: { value: DockerMirrorID; label: string }[] = [
  { value: 'auto', label: 'auto' },
  { value: 'daocloud', label: 'DaoCloud' },
  { value: 'one_ms', label: '1ms' },
  { value: 'registry_cyou', label: 'registry.cyou' },
  { value: 'dockerproxy_net', label: 'dockerproxy.net' },
  { value: 'dockerproxy_link', label: 'dockerproxy.link' },
  { value: 'docker_jiaxin', label: 'docker.jiaxin.site' },
  { value: 'docker_xuanyuan', label: 'docker.xuanyuan.me' },
  { value: 'free_hubfast', label: 'free.hubfast.cn' },
];

type NextActionKind =
  | 'check_docker'
  | 'install_docker'
  | 'bootstrap'
  | 'initialize_config'
  | 'update_game'
  | 'start_server'
  | 'running'
  | 'blocked'
  | 'loading';

type WindowsServerSource = 'existing' | 'install' | null;

const recoverableSetupJobTypes = new Set([
  'bootstrap',
  'install',
  'docker_install',
  'docker_mirror_configure',
  'update',
  'smart_update',
  'version_check',
]);

const isRecoverableSetupJob = (job: Job) => recoverableSetupJobTypes.has(job.type) && !isJobDone(job);

const settledValue = <T,>(result: PromiseSettledResult<T>): T | null => (
  result.status === 'fulfilled' ? result.value : null
);

const settledErrorMessage = (results: PromiseSettledResult<unknown>[]) => {
  const rejected = results.find((result) => result.status === 'rejected');
  return rejected && rejected.status === 'rejected' ? getErrorMessage(rejected.reason) : null;
};

interface NextAction {
  kind: NextActionKind;
  label: string;
  description: string;
  disabled: boolean;
  disabledReason?: string;
}

interface SimpleStatus {
  label: string;
  value: string;
  description: string;
  state: 'ok' | 'pending' | 'warning' | 'error';
}

export const Setup: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [status, setStatus] = useState<ServerStatus | null>(null);
  const [prerequisites, setPrerequisites] = useState<Prerequisite[]>([]);
  const [runtime, setRuntime] = useState<RuntimeMode>('wine_docker');
  const [startup, setStartup] = useState<StartupResponse | null>(null);
  const [versionInfo, setVersionInfo] = useState<ServerVersionInfo | null>(null);
  const [host, setHost] = useState<HostCapabilities | null>(null);
  const [dockerPlan, setDockerPlan] = useState<DockerInstallPlan | null>(null);
  const [dockerSource, setDockerSource] = useState<DockerSourceID>('auto');
  const [dockerMirrorPlan, setDockerMirrorPlan] = useState<DockerMirrorPlan | null>(null);
  const [dockerMirror, setDockerMirror] = useState<DockerMirrorID>('auto');
  const [addDockerGroup, setAddDockerGroup] = useState(true);
  const [showDockerManualPanel, setShowDockerManualPanel] = useState(false);
  const [showMirrorManualPanel, setShowMirrorManualPanel] = useState(false);
  const [showManualScript, setShowManualScript] = useState(false);
  const [showMirrorScript, setShowMirrorScript] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [windowsServerSource, setWindowsServerSource] = useState<WindowsServerSource>(null);
  const [existingServerPath, setExistingServerPath] = useState('');
  const [importingServer, setImportingServer] = useState(false);
  const [activeJob, setActiveJob] = useState<Job | null>(null);
  const [jobRecoveryComplete, setJobRecoveryComplete] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const mountedRef = useRef(true);
  const trackedJobIdRef = useRef<string | null>(null);
  const automaticVersionCheckAttemptedRef = useRef(false);

  const refreshAdvanced = useCallback(async () => {
    const results = await Promise.allSettled([
      serverApi.getVersion(),
      setupApi.getDockerPlan(dockerSource),
      setupApi.getDockerMirrorPlan(dockerMirror),
    ]);
    if (!mountedRef.current) return;

    const [versionRes, dockerPlanRes, dockerMirrorPlanRes] = results;
    const nextVersionInfo = settledValue(versionRes);
    const nextDockerPlan = settledValue(dockerPlanRes);
    const nextDockerMirrorPlan = settledValue(dockerMirrorPlanRes);

    if (nextVersionInfo) setVersionInfo(nextVersionInfo);
    if (nextDockerPlan) setDockerPlan(nextDockerPlan);
    if (nextDockerMirrorPlan) setDockerMirrorPlan(nextDockerMirrorPlan);

    const advancedError = settledErrorMessage(results);
    if (advancedError) {
      setMessage((current) => current || advancedError);
    }
  }, [dockerSource, dockerMirror]);

  const refresh = useCallback(async () => {
    if (mountedRef.current) {
      setLoading(true);
    }
    const results = await Promise.allSettled([
      serverApi.getStatus(),
      setupApi.getPrerequisites(),
      setupApi.getRuntime(),
      setupApi.getStartup(),
      setupApi.getHost(),
      serverApi.getVersion(),
    ]);
    if (!mountedRef.current) return;

    const [statusRes, checksRes, runtimeRes, startupRes, hostRes, versionRes] = results;
    const nextStatus = settledValue(statusRes);
    const checks = settledValue(checksRes);
    const nextRuntime = settledValue(runtimeRes);
    const startupResValue = settledValue(startupRes);
    const hostResValue = settledValue(hostRes);
    const versionResValue = settledValue(versionRes);

    if (nextStatus) setStatus(nextStatus);
    if (checks) setPrerequisites(checks);
    if (nextRuntime) setRuntime(nextRuntime.mode);
    if (startupResValue) setStartup(startupResValue);
    if (hostResValue) setHost(hostResValue);
    if (versionResValue) setVersionInfo(versionResValue);

    setMessage(settledErrorMessage(results));
    setLoading(false);
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    if (loading || (host && status)) return;
    const timer = window.setTimeout(() => void refresh(), 5000);
    return () => window.clearTimeout(timer);
  }, [host, loading, refresh, status]);

  useEffect(() => {
    if (host?.os !== 'windows' || status?.installed) return;
    if (status?.server_imported) {
      setWindowsServerSource('existing');
      setExistingServerPath((current) => current || status.paths.server || '');
    }
  }, [host, status]);

  const trackJob = useCallback(async (job: Job) => {
    if (!job.id) return;
    if (trackedJobIdRef.current === job.id) {
      setActiveJob(job);
      return;
    }
    trackedJobIdRef.current = job.id;
    setActiveJob(job);
    const shouldContinue = () => mountedRef.current && trackedJobIdRef.current === job.id;
    const done = await tasksApi.waitForJob(job.id, (nextJob) => {
      if (shouldContinue()) {
        setActiveJob(nextJob);
      }
    }, shouldContinue);
    if (!mountedRef.current || trackedJobIdRef.current !== job.id) {
      return;
    }
    setActiveJob(done);
    trackedJobIdRef.current = null;
    setMessage(done.status === 'success' ? '任务已完成' : done.error || '任务执行失败');
    await refresh();
    if (done.type === 'update' || done.type === 'smart_update' || done.type === 'version_check') {
      const refreshedVersion = await serverApi.getVersion();
      if (mountedRef.current) setVersionInfo(refreshedVersion);
    }
  }, [refresh]);

  useEffect(() => {
    let cancelled = false;

    const recoverActiveJob = async () => {
      try {
        const jobs = await tasksApi.getJobs();
        const job = jobs.find(isRecoverableSetupJob);
        if (cancelled || !job || trackedJobIdRef.current === job.id) {
          return;
        }
        await trackJob(job);
      } catch {
        // Job recovery is opportunistic; the regular status refresh still drives the setup page.
      } finally {
        if (!cancelled && mountedRef.current) setJobRecoveryComplete(true);
      }
    };

    void recoverActiveJob();
    return () => {
      cancelled = true;
    };
  }, [trackJob]);

  useEffect(() => {
    if (
      !jobRecoveryComplete
      || loading
      || !status?.installed
      || !versionInfo?.current_build_id
      || versionInfo.latest_build_id
      || (activeJob && !isJobDone(activeJob))
      || automaticVersionCheckAttemptedRef.current
    ) return;

    automaticVersionCheckAttemptedRef.current = true;
    void (async () => {
      try {
        const job = await serverApi.checkVersion();
        await trackJob(job);
      } catch (error) {
        if (mountedRef.current) setMessage(getErrorMessage(error));
      }
    })();
  }, [activeJob, jobRecoveryComplete, loading, status?.installed, trackJob, versionInfo?.current_build_id, versionInfo?.latest_build_id]);

  const runJob = async (start: () => Promise<Job>) => {
    try {
      const job = await start();
      await trackJob(job);
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const setRuntimeMode = async (mode: RuntimeMode) => {
    try {
      setRuntime(mode);
      await setupApi.setRuntime(mode);
      await refresh();
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const updateStartup = (key: keyof StartupConfig, value: string | number | boolean) => {
    if (!startup) return;
    setStartup({ ...startup, startup: { ...startup.startup, [key]: value } });
  };

  const saveStartup = async () => {
    if (!startup) return;
    try {
      const saved = await setupApi.setStartup(startup.startup);
      setStartup(saved);
      setMessage('启动参数已保存');
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const initializeConfig = async () => {
    try {
      const result = await setupApi.initializeConfig();
      setMessage(result.path ? `配置已初始化：${result.path}` : '配置已初始化');
      await refresh();
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const startServer = async () => {
    try {
      await serverApi.start();
      setMessage('服务端启动命令已发送');
      await refresh();
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const importExistingServer = async () => {
    const path = existingServerPath.trim();
    if (!path) {
      setMessage('请填写现有服务端目录');
      return;
    }
    setImportingServer(true);
    try {
      const imported = await setupApi.importServerDirectory(path);
      setRuntime('windows_steamcmd');
      setExistingServerPath(imported.path);
      await refresh();
      setMessage(
        imported.config_exists
          ? `已接管现有服务端：${imported.path}`
          : `已接管现有服务端：${imported.path}。下一步初始化配置文件。`,
      );
    } catch (error) {
      setMessage(getErrorMessage(error));
    } finally {
      setImportingServer(false);
    }
  };

  const changeDockerSource = async (source: DockerSourceID) => {
    setDockerSource(source);
    try {
      const plan = await setupApi.getDockerPlan(source);
      setDockerPlan(plan);
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const changeDockerMirror = async (mirror: DockerMirrorID) => {
    setDockerMirror(mirror);
    try {
      const plan = await setupApi.getDockerMirrorPlan(mirror);
      setDockerMirrorPlan(plan);
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const installDocker = async () => {
    try {
      const job = await setupApi.installDocker({
        source: dockerSource,
        add_current_user_to_docker_group: addDockerGroup,
      });
      await trackJob(job);
    } catch (error) {
      const code = (error as { code?: string }).code;
      if (code === 'sudo_required') {
        setShowDockerManualPanel(true);
        setMessage('需要手动安装 Docker 环境');
      } else {
        setMessage(getErrorMessage(error));
      }
      try {
        const plan = await setupApi.getDockerPlan(dockerSource);
        setDockerPlan(plan);
      } catch {
        // Keep the current plan visible when the refresh fails after an action error.
      }
    }
  };

  const configureDockerMirrors = async () => {
    try {
      const job = await setupApi.configureDockerMirrors({ mirror: dockerMirror });
      await trackJob(job);
    } catch (error) {
      const code = (error as { code?: string }).code;
      if (code === 'sudo_required') {
        setShowMirrorManualPanel(true);
        setMessage('需要手动配置 Docker 镜像加速');
      } else {
        setMessage(getErrorMessage(error));
      }
      try {
        const plan = await setupApi.getDockerMirrorPlan(dockerMirror);
        setDockerMirrorPlan(plan);
      } catch {
        // Keep the current mirror plan visible when refresh fails after an action error.
      }
    }
  };

  const copyText = async (value: string, successMessage: string) => {
    if (!value || typeof navigator === 'undefined' || !navigator.clipboard) return;
    await navigator.clipboard.writeText(value);
    setMessage(successMessage);
  };

  const downloadScript = (filename: string, script?: string) => {
    if (!script || typeof window === 'undefined') return;
    const blob = new Blob([script], { type: 'text/x-shellscript' });
    const url = window.URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = filename;
    anchor.click();
    window.URL.revokeObjectURL(url);
  };

  const requiredSystemMissing = prerequisites.filter((item) => item.required && !item.ok && !isDockerPrerequisite(item));
  const isLinuxHost = host?.os === 'linux';
  const isWindowsHost = host?.os === 'windows';
  const isJobRunning = Boolean(activeJob && !isJobDone(activeJob));
  const dockerReady = Boolean(dockerPlan?.docker_ready || host?.docker.daemon_reachable);
  const needsDockerForRuntime = Boolean(isLinuxHost && runtime === 'wine_docker');
  const dockerManualVisible = Boolean(
    needsDockerForRuntime && !dockerReady && dockerPlan?.script && (dockerPlan.requires_manual || showDockerManualPanel),
  );
  const mirrorManualVisible = Boolean(isLinuxHost && showMirrorManualPanel && dockerMirrorPlan?.script);
  const platformText = host ? platformLabel(host) : '检测中';
  const dockerManualCommand = dockerPlan ? dockerManualCommandFor(dockerPlan, addDockerGroup) : '';
  const dockerMirrorManualCommand = dockerMirrorPlan ? dockerMirrorManualCommandFor(dockerMirrorPlan) : '';
  const detectedNextAction = getNextAction({
    loading,
    status,
    host,
    runtime,
    dockerReady,
    dockerPlan,
    requiredSystemMissing,
    isJobRunning,
    versionInfo,
  });
  const needsWindowsSource = Boolean(isWindowsHost && status && !status.installed);
  const nextAction: NextAction = needsWindowsSource && windowsServerSource !== 'install'
    ? {
        kind: 'blocked',
        label: windowsServerSource === 'existing' ? '等待导入现有目录' : '先选择开服方式',
        description: windowsServerSource === 'existing'
          ? '填写现有 Palworld 服务端目录并完成接管。'
          : '先选择接管已有服务器，或由 PalPanel 自动安装。',
        disabled: true,
      }
    : detectedNextAction;
  const simpleStatuses = getSimpleStatuses({
    loading,
    host,
    runtime,
    dockerReady,
    dockerPlan,
    status,
    requiredSystemMissing,
    dockerManualVisible,
  });

  const handlePrimaryAction = async () => {
    switch (nextAction.kind) {
      case 'check_docker':
        setShowAdvanced(true);
        await refreshAdvanced();
        break;
      case 'install_docker':
        await installDocker();
        break;
      case 'bootstrap':
        await runJob(setupApi.bootstrap);
        break;
      case 'initialize_config':
        await initializeConfig();
        break;
      case 'update_game':
        await runJob(serverApi.updateIfNeeded);
        break;
      case 'start_server':
        await startServer();
        break;
      default:
        break;
    }
  };

  const copyManualCommand = () => copyText(dockerManualCommand, 'Docker 安装命令已复制');
  const copyManualScript = () => copyText(dockerPlan?.script || '', 'Docker 安装脚本已复制');
  const downloadManualScript = () => downloadScript('install-docker.sh', dockerPlan?.script);
  const copyMirrorCommand = () => copyText(dockerMirrorManualCommand, 'Docker 镜像加速命令已复制');
  const copyMirrorScript = () => copyText(dockerMirrorPlan?.script || '', 'Docker 镜像加速脚本已复制');
  const downloadMirrorScript = () => downloadScript('configure-docker-mirrors.sh', dockerMirrorPlan?.script);
  const criticalStatusMissing = !loading && (!host || !status);

  return (
    <div className="mx-auto flex min-w-0 w-full max-w-7xl flex-col gap-6 overflow-x-clip p-4 sm:p-6 lg:p-8">
      {needsWindowsSource && (
        <WindowsServerSourcePanel
          source={windowsServerSource}
          path={existingServerPath}
          importing={importingServer}
          onSourceChange={setWindowsServerSource}
          onPathChange={setExistingServerPath}
          onImport={importExistingServer}
        />
      )}

      <SetupHero
        action={nextAction}
        platformText={platformText}
        message={message}
        activeJob={activeJob}
        onPrimaryAction={handlePrimaryAction}
        onRefresh={refresh}
      />

      <SimpleStatusStrip items={simpleStatuses} />

      {status?.installed && versionInfo && (
        <section className="grid grid-cols-2 gap-3 border-y border-slate-100 bg-white px-4 py-4 sm:grid-cols-4">
          <StatusItem label="游戏版本" value={versionInfo.game_version || '离线未知'} ok={versionInfo.compatible === true} />
          <StatusItem label="当前 Build" value={versionInfo.current_build_id || '未知'} ok={Boolean(versionInfo.current_build_id)} />
          <StatusItem label="最新 Build" value={versionInfo.latest_build_id || '未检查'} ok={Boolean(versionInfo.latest_build_id)} />
          <StatusItem
            label={`兼容范围 ${versionInfo.compatibility_target || '1.x'}`}
            value={versionInfo.compatible === true ? '兼容' : versionInfo.compatible === false ? '不匹配' : '待运行确认'}
            ok={versionInfo.compatible === true}
          />
          {versionInfo.compatibility_warnings.length > 0 && (
            <p className="col-span-2 text-[11px] font-semibold leading-relaxed text-amber-700 sm:col-span-4">
              {versionInfo.compatibility_warnings.join(' / ')}
            </p>
          )}
        </section>
      )}

      {criticalStatusMissing && (
        <ConnectionIssuePanel
          message={message}
          onRetry={refresh}
        />
      )}

      {dockerManualVisible && dockerPlan && (
        <ManualCommandPanel
          title="需要手动安装 Docker 环境"
          description={dockerPlan.message || host?.sudo.message || '当前账号不能自动执行管理员命令。请复制下面命令到服务器终端执行。'}
          command={dockerManualCommand}
          warnings={dockerPlan.warnings}
          tone="amber"
          onCopyCommand={copyManualCommand}
          onCopyScript={copyManualScript}
          onDownloadScript={downloadManualScript}
          onRefresh={refresh}
        />
      )}

      {mirrorManualVisible && dockerMirrorPlan && (
        <ManualCommandPanel
          title="需要手动配置 Docker 镜像加速"
          description={dockerMirrorPlan.message || '自动配置需要管理员权限。请复制下面命令到服务器终端执行。'}
          command={dockerMirrorManualCommand}
          warnings={dockerMirrorPlan.warnings}
          tone="rose"
          onCopyCommand={copyMirrorCommand}
          onCopyScript={copyMirrorScript}
          onDownloadScript={downloadMirrorScript}
          onRefresh={refresh}
        />
      )}

      <AdvancedSetupPanel
        open={showAdvanced}
        onToggle={() => {
          const nextOpen = !showAdvanced;
          setShowAdvanced(nextOpen);
          if (nextOpen) void refreshAdvanced();
        }}
        host={host}
        runtime={runtime}
        status={status}
        versionInfo={versionInfo}
        dockerPlan={dockerPlan}
        dockerMirrorPlan={dockerMirrorPlan}
        dockerSource={dockerSource}
        dockerMirror={dockerMirror}
        dockerReady={dockerReady}
        isLinuxHost={Boolean(isLinuxHost)}
        isWindowsHost={Boolean(isWindowsHost)}
        isJobRunning={isJobRunning}
        addDockerGroup={addDockerGroup}
        showManualScript={showManualScript}
        showMirrorScript={showMirrorScript}
        onRuntimeChange={setRuntimeMode}
        onDockerSourceChange={changeDockerSource}
        onDockerMirrorChange={changeDockerMirror}
        onAddDockerGroupChange={setAddDockerGroup}
        onInstallDocker={installDocker}
        onConfigureMirrors={configureDockerMirrors}
        onToggleManualScript={() => setShowManualScript((value) => !value)}
        onToggleMirrorScript={() => {
          setShowMirrorManualPanel(true);
          setShowMirrorScript((value) => !value);
        }}
        onCheckVersion={() => runJob(serverApi.checkVersion)}
        onUpdateIfNeeded={() => runJob(serverApi.updateIfNeeded)}
      />

      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
        <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h4 className="text-sm font-bold text-slate-800">启动参数</h4>
            <p className="mt-1 text-xs font-medium text-slate-400">
              这些参数会参与 PalServer.exe 启动命令，保存后下次启动生效。
            </p>
          </div>
          <button
            type="button"
            onClick={saveStartup}
            className="flex items-center justify-center gap-2 rounded-xl bg-sky-600 px-4 py-2 text-xs font-bold text-white hover:bg-sky-700"
          >
            <Save size={14} />
            保存启动参数
          </button>
        </div>

        {startup && (
          <>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
              <NumberField label="监听端口" value={startup.startup.port} onChange={(value) => updateStartup('port', value)} />
              <NumberField label="最大玩家" value={startup.startup.players} onChange={(value) => updateStartup('players', value)} />
              <NumberField label="公网端口" value={startup.startup.public_port || 8211} onChange={(value) => updateStartup('public_port', value)} />
              <NumberField label="工作线程" value={startup.startup.number_of_worker_threads_server || 0} onChange={(value) => updateStartup('number_of_worker_threads_server', value)} />
              <TextField label="公网 IP" value={startup.startup.public_ip || ''} onChange={(value) => updateStartup('public_ip', value)} />
              <TextField label="Workshop 目录" value={startup.startup.workshop_dir || ''} onChange={(value) => updateStartup('workshop_dir', value)} />
              <SelectField label="日志格式" value={startup.startup.log_format} onChange={(value) => updateStartup('log_format', value)} />
              <div className="grid grid-cols-1 gap-2 rounded-2xl border border-slate-100 bg-slate-50/70 p-3">
                {[
                  ['public_lobby', '公开大厅'],
                  ['use_perf_threads', '性能线程'],
                  ['no_async_loading_thread', '禁用异步加载线程'],
                  ['use_multithread_for_ds', 'DS 多线程'],
                  ['no_mods', '禁用 Mod'],
                ].map(([key, label]) => (
                  <label key={key} className="flex items-center gap-2 text-xs font-semibold text-slate-600">
                    <input
                      type="checkbox"
                      checked={Boolean(startup.startup[key as keyof StartupConfig])}
                      onChange={(event) => updateStartup(key as keyof StartupConfig, event.target.checked)}
                      className="h-4 w-4 rounded border-slate-300 text-sky-500 focus:ring-sky-500"
                    />
                    {label}
                  </label>
                ))}
              </div>
            </div>

            {startup.issues.length > 0 && (
              <div className="mt-4 rounded-2xl border border-amber-100 bg-amber-50 p-3">
                {startup.issues.map((issue, index) => (
                  <p key={index} className="text-[11px] font-semibold text-amber-800">
                    {issue.field ? `${issue.field}: ` : ''}
                    {issue.message}
                  </p>
                ))}
              </div>
            )}

            <div className="mt-4 rounded-2xl bg-slate-950 p-4 text-[11px] font-semibold text-emerald-300">
              <pre className="overflow-x-auto whitespace-pre-wrap">{startup.args.join(' ') || '保存后生成启动参数'}</pre>
            </div>
          </>
        )}
      </section>
    </div>
  );
};

const SetupHero: React.FC<{
  action: NextAction;
  platformText: string;
  message: string | null;
  activeJob: Job | null;
  onPrimaryAction: () => void;
  onRefresh: () => void;
}> = ({ action, platformText, message, activeJob, onPrimaryAction, onRefresh }) => (
  <section className="min-w-0 overflow-hidden rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)] sm:p-6">
    <div className="flex min-w-0 flex-col gap-5 xl:flex-row xl:items-center xl:justify-between">
      <div className="min-w-0">
        <h3 className="flex items-center gap-2 text-[18px] font-bold text-slate-900">
          <Wand2 size={20} className="text-sky-500" />
          一键开服
        </h3>
        <p className="mt-2 max-w-2xl text-sm font-medium leading-6 text-slate-500">
          当前环境：{platformText}。{action.description}
        </p>
        {action.disabledReason && <p className="mt-2 text-xs font-semibold text-amber-700">{action.disabledReason}</p>}
      </div>
      <div className="grid w-full min-w-0 grid-cols-1 gap-2 sm:grid-cols-2 xl:w-72 xl:grid-cols-1">
        <button
          type="button"
          onClick={onPrimaryAction}
          disabled={action.disabled}
          className="flex w-full items-center justify-center gap-2 rounded-2xl bg-sky-600 px-6 py-4 text-sm font-bold text-white shadow-sm shadow-sky-600/15 hover:bg-sky-700 disabled:cursor-not-allowed disabled:opacity-45"
        >
          {primaryActionIcon(action.kind)}
          {action.label}
        </button>
        <button
          type="button"
          onClick={onRefresh}
          className="flex w-full items-center justify-center gap-2 rounded-xl border border-slate-200 px-4 py-2.5 text-xs font-semibold text-slate-600 hover:bg-slate-50"
        >
          <RefreshCw size={14} />
          重新检查
        </button>
      </div>
    </div>

    {message && (
      <div className="mt-5 rounded-2xl border border-sky-100 bg-sky-50 px-4 py-3 text-xs font-semibold text-sky-700">
        {message}
      </div>
    )}

    {activeJob && <JobProgress job={activeJob} />}
  </section>
);

const WindowsServerSourcePanel: React.FC<{
  source: WindowsServerSource;
  path: string;
  importing: boolean;
  onSourceChange: (source: Exclude<WindowsServerSource, null>) => void;
  onPathChange: (path: string) => void;
  onImport: () => void;
}> = ({ source, path, importing, onSourceChange, onPathChange, onImport }) => {
  const step = source ? 2 : 1;
  return (
    <section className="overflow-hidden rounded-3xl border border-slate-200 bg-white text-slate-900 shadow-sm shadow-slate-200/50">
      <div className="flex flex-col gap-5 border-b border-slate-200 bg-slate-50/70 px-5 py-5 sm:flex-row sm:items-center sm:justify-between sm:px-7">
        <div>
          <p className="text-xs font-bold uppercase tracking-[0.18em] text-slate-700">Windows 开服向导</p>
          <h3 className="mt-2 text-xl font-bold">选择服务端来源</h3>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-slate-500">
            已经装过服务端就直接接管原目录；还没有的话，让 PalPanel 通过 SteamCMD 安装。
          </p>
        </div>
        <div className="flex items-center gap-2 text-[11px] font-bold text-slate-500" aria-label={`当前第 ${step} 步，共 3 步`}>
          {['选择方式', '准备服务端', '完成配置'].map((label, index) => {
            const number = index + 1;
            const active = number <= step;
            return (
              <React.Fragment key={label}>
                {index > 0 && <span className={`h-px w-5 ${active ? 'bg-sky-500' : 'bg-slate-200'}`} />}
                <span className="flex flex-col items-center gap-1">
                  <span className={`flex h-7 w-7 items-center justify-center rounded-full ${active ? 'bg-sky-600 text-white' : 'bg-slate-100 text-slate-400'}`}>
                    {number}
                  </span>
                  <span className="hidden sm:block">{label}</span>
                </span>
              </React.Fragment>
            );
          })}
        </div>
      </div>

      <div className="grid gap-4 p-5 sm:grid-cols-2 sm:p-7">
        <button
          type="button"
          aria-label="我已有服务器"
          onClick={() => onSourceChange('existing')}
          className={`group rounded-2xl border p-6 text-left transition ${
            source === 'existing'
              ? 'border-sky-300 bg-sky-50 ring-2 ring-sky-500/15'
              : 'border-slate-200 bg-white hover:border-sky-300 hover:bg-sky-50/60'
          }`}
        >
          <span className="flex h-12 w-12 items-center justify-center rounded-2xl bg-white text-sky-600 shadow-sm">
            <FolderOpen size={24} />
          </span>
          <span className="mt-5 block text-base font-bold">我已有服务器</span>
          <span className="mt-2 block text-sm leading-6 text-slate-500">
            指定 PalServer.exe 所在目录，继续使用原来的配置、存档和 Mod。
          </span>
        </button>

        <button
          type="button"
          aria-label="帮我安装服务器"
          onClick={() => onSourceChange('install')}
          className={`group rounded-2xl border p-6 text-left transition ${
            source === 'install'
              ? 'border-sky-300 bg-sky-50 ring-2 ring-sky-500/15'
              : 'border-slate-200 bg-white hover:border-sky-300 hover:bg-sky-50/60'
          }`}
        >
          <span className="flex h-12 w-12 items-center justify-center rounded-2xl bg-white text-sky-600 shadow-sm">
            <HardDriveDownload size={24} />
          </span>
          <span className="mt-5 block text-base font-bold">帮我安装服务器</span>
          <span className="mt-2 block text-sm leading-6 text-slate-500">
            下载 SteamCMD，并把服务端安装到 PalPanel 自己的数据目录。
          </span>
        </button>
      </div>

      {source === 'existing' && (
        <div className="border-t border-slate-200 bg-slate-50/70 px-5 py-5 sm:px-7">
          <label htmlFor="existing-palserver-path" className="text-sm font-bold text-slate-800">
            现有服务端目录
          </label>
          <div className="mt-3 flex flex-col gap-3 lg:flex-row">
            <input
              id="existing-palserver-path"
              type="text"
              value={path}
              onChange={(event) => onPathChange(event.target.value)}
              placeholder={String.raw`D:\SteamLibrary\steamapps\common\PalServer`}
              className="min-w-0 flex-1 rounded-xl border border-slate-200 bg-white px-4 py-3 text-sm font-semibold text-slate-800 outline-none placeholder:text-slate-400 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20"
            />
            <button
              type="button"
              onClick={onImport}
              disabled={importing || !path.trim()}
              className="flex items-center justify-center gap-2 rounded-xl bg-sky-500 px-6 py-3 text-sm font-bold text-white hover:bg-sky-400 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {importing ? <RefreshCw size={16} className="animate-spin" /> : <CheckCircle2 size={16} />}
              {importing ? '正在检查' : '检查并接管'}
            </button>
          </div>
          <p className="mt-3 text-xs leading-5 text-slate-500">
            常见路径是 <code className="font-semibold text-slate-800">D:\SteamLibrary\steamapps\common\PalServer</code>。
            也可以填写 Steam 库目录，面板会自动查找。接管不会复制游戏文件，之后的更新、备份和 Mod 操作会直接作用于这个目录。
          </p>
        </div>
      )}
    </section>
  );
};

const SimpleStatusStrip: React.FC<{ items: SimpleStatus[] }> = ({ items }) => (
  <section className="grid grid-cols-1 gap-3 md:grid-cols-3">
    {items.map((item) => (
      <div key={item.label} className="rounded-2xl border border-slate-100 bg-white p-4 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
        <div className="flex items-center justify-between gap-3">
          <span className="text-xs font-bold text-slate-500">{item.label}</span>
          <span className={`h-2.5 w-2.5 rounded-full ${statusDotClass(item.state)}`} />
        </div>
        <p className="mt-2 text-sm font-bold text-slate-900">{item.value}</p>
        <p className="mt-1 min-h-8 text-xs font-medium leading-4 text-slate-400">{item.description}</p>
      </div>
    ))}
  </section>
);

const ConnectionIssuePanel: React.FC<{
  message: string | null;
  onRetry: () => void;
}> = ({ message, onRetry }) => (
  <section className="rounded-3xl border border-rose-100 bg-rose-50 p-5 text-rose-900 shadow-sm">
    <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
      <div className="min-w-0 flex-1">
        <h4 className="flex items-center gap-2 text-sm font-bold">
          <AlertTriangle size={17} />
          检测失败
        </h4>
        <p className="mt-2 text-xs font-semibold leading-5 opacity-85">
          无法读取当前面板后端的关键状态，页面会自动重新检测。安装或更新任务不会因切换页面而取消，但重启 PalPanel 会中断正在运行的任务。
        </p>
        {message && <p className="mt-2 break-words text-[11px] font-semibold opacity-80">{message}</p>}
      </div>
      <button
        type="button"
        onClick={onRetry}
        className="flex shrink-0 items-center justify-center gap-2 rounded-xl border border-rose-200 bg-white px-4 py-3 text-xs font-bold text-rose-700 hover:bg-rose-50"
      >
        <RefreshCw size={14} />
        重新检测
      </button>
    </div>
  </section>
);

const ManualCommandPanel: React.FC<{
  title: string;
  description: string;
  command: string;
  warnings?: string[];
  tone: 'amber' | 'rose';
  onCopyCommand: () => void;
  onCopyScript: () => void;
  onDownloadScript: () => void;
  onRefresh: () => void;
}> = ({
  title,
  description,
  command,
  warnings,
  tone,
  onCopyCommand,
  onCopyScript,
  onDownloadScript,
  onRefresh,
}) => {
  const toneClass =
    tone === 'rose'
      ? 'border-rose-200 bg-rose-50 text-rose-900'
      : 'border-amber-200 bg-amber-50 text-amber-900';
  const buttonClass =
    tone === 'rose'
      ? 'border-rose-200 bg-white/75 text-rose-800 hover:bg-white'
      : 'border-amber-200 bg-white/75 text-amber-800 hover:bg-white';

  return (
    <section className={`rounded-3xl border p-5 shadow-sm ${toneClass}`}>
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <h4 className="flex items-center gap-2 text-sm font-bold">
            <AlertTriangle size={17} />
            {title}
          </h4>
          <p className="mt-2 text-xs font-semibold leading-5 opacity-85">{description}</p>
        </div>
        <button
          type="button"
          onClick={onRefresh}
          className={`flex shrink-0 items-center justify-center gap-2 rounded-xl border px-3 py-2 text-xs font-bold ${buttonClass}`}
        >
          <RefreshCw size={13} />
          我已执行，重新检查
        </button>
      </div>

      {command && (
        <div className="mt-4 rounded-2xl bg-slate-950 p-4 text-[12px] font-semibold leading-5 text-emerald-300">
          <code className="break-all">{command}</code>
        </div>
      )}

      <div className="mt-4 flex flex-wrap gap-2">
        <button type="button" onClick={onCopyCommand} className={`flex items-center gap-2 rounded-xl border px-3 py-2 text-xs font-bold ${buttonClass}`}>
          <Copy size={13} />
          复制命令
        </button>
        <button type="button" onClick={onCopyScript} className={`flex items-center gap-2 rounded-xl border px-3 py-2 text-xs font-bold ${buttonClass}`}>
          <FileText size={13} />
          复制完整脚本
        </button>
        <button type="button" onClick={onDownloadScript} className={`flex items-center gap-2 rounded-xl border px-3 py-2 text-xs font-bold ${buttonClass}`}>
          <FileDown size={13} />
          下载脚本
        </button>
      </div>

      {warnings && warnings.length > 0 && (
        <div className="mt-4 rounded-2xl border border-white/70 bg-white/60 p-3 text-[11px] font-semibold leading-5">
          {warnings.join(' / ')}
        </div>
      )}
    </section>
  );
};

const AdvancedSetupPanel: React.FC<{
  open: boolean;
  onToggle: () => void;
  host: HostCapabilities | null;
  runtime: RuntimeMode;
  status: ServerStatus | null;
  versionInfo: ServerVersionInfo | null;
  dockerPlan: DockerInstallPlan | null;
  dockerMirrorPlan: DockerMirrorPlan | null;
  dockerSource: DockerSourceID;
  dockerMirror: DockerMirrorID;
  dockerReady: boolean;
  isLinuxHost: boolean;
  isWindowsHost: boolean;
  isJobRunning: boolean;
  addDockerGroup: boolean;
  showManualScript: boolean;
  showMirrorScript: boolean;
  onRuntimeChange: (mode: RuntimeMode) => void;
  onDockerSourceChange: (source: DockerSourceID) => void;
  onDockerMirrorChange: (mirror: DockerMirrorID) => void;
  onAddDockerGroupChange: (value: boolean) => void;
  onInstallDocker: () => void;
  onConfigureMirrors: () => void;
  onToggleManualScript: () => void;
  onToggleMirrorScript: () => void;
  onCheckVersion: () => void;
  onUpdateIfNeeded: () => void;
}> = ({
  open,
  onToggle,
  host,
  runtime,
  status,
  versionInfo,
  dockerPlan,
  dockerMirrorPlan,
  dockerSource,
  dockerMirror,
  dockerReady,
  isLinuxHost,
  isWindowsHost,
  isJobRunning,
  addDockerGroup,
  showManualScript,
  showMirrorScript,
  onRuntimeChange,
  onDockerSourceChange,
  onDockerMirrorChange,
  onAddDockerGroupChange,
  onInstallDocker,
  onConfigureMirrors,
  onToggleManualScript,
  onToggleMirrorScript,
  onCheckVersion,
  onUpdateIfNeeded,
}) => {
  const canInstallDocker = Boolean(!dockerReady && !isJobRunning && dockerPlan?.supported && dockerPlan.can_auto_install);
  const canConfigureMirrors = Boolean(
    !isJobRunning &&
      dockerMirrorPlan?.supported &&
      dockerMirrorPlan.can_auto_configure &&
      dockerMirrorPlan.selected_mirrors.length > 0,
  );

  return (
    <section className="rounded-3xl border border-slate-100 bg-white shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="flex w-full items-center justify-between gap-3 p-5 text-left sm:p-6"
      >
        <span>
          <span className="flex items-center gap-2 text-sm font-bold text-slate-800">
            <Settings2 size={16} className="text-indigo-500" />
            高级设置
          </span>
          <span className="mt-1 block text-xs font-medium text-slate-400">
            运行方式、网络和维护选项。
          </span>
        </span>
        <ChevronDown size={18} className={`shrink-0 text-slate-400 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      {open && (
        <div className="grid grid-cols-1 gap-5 border-t border-slate-100 p-5 sm:p-6 xl:grid-cols-3">
          <div>
            <h4 className="mb-3 flex items-center gap-2 text-sm font-bold text-slate-800">
              <Server size={16} className="text-sky-500" />
              Runtime 运行方式
            </h4>
            <div className="grid grid-cols-1 gap-3">
              {(['windows_steamcmd', 'wine_docker'] as RuntimeMode[]).map((mode) => (
                <button
                  type="button"
                  key={mode}
                  onClick={() => onRuntimeChange(mode)}
                  className={`rounded-2xl border p-4 text-left transition-all ${
                    runtime === mode
                      ? 'border-sky-200 bg-sky-50 text-sky-800'
                      : 'border-slate-100 bg-slate-50/70 text-slate-600 hover:border-slate-200'
                  }`}
                >
                  <span className="text-xs font-bold">{runtimeLabels[mode]}</span>
                  <p className="mt-1 text-[11px] font-medium opacity-75">
                    {mode === 'windows_steamcmd'
                      ? '使用本机 SteamCMD 管理 Windows 版服务端。'
                      : '使用 Docker + Wine 运行 Windows 版服务端和 Mod。'}
                  </p>
                </button>
              ))}
            </div>
          </div>

          {isLinuxHost && (
            <div>
              <h4 className="mb-3 flex items-center gap-2 text-sm font-bold text-slate-800">
                <Terminal size={16} className="text-slate-700" />
                Docker 环境
              </h4>
              <div className="grid grid-cols-2 gap-3 text-xs font-semibold">
                <StatusItem label="安装状态" value={dockerReady ? '已就绪' : '未就绪'} ok={dockerReady} />
                <StatusItem label="sudo" value={host?.sudo.can_elevate ? '可自动执行' : '需手动'} ok={Boolean(host?.sudo.can_elevate)} />
              </div>

              <label className="mt-4 flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
                Docker 源
                <select
                  value={dockerSource}
                  onChange={(event) => onDockerSourceChange(event.target.value as DockerSourceID)}
                  className="rounded-xl border border-slate-200 bg-white p-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
                >
                  <option value="auto">auto</option>
                  <option value="official">official</option>
                  <option value="aliyun">aliyun</option>
                  <option value="azurecn">azurecn</option>
                </select>
              </label>

              {dockerPlan?.source_url && (
                <p className="mt-2 break-all text-[11px] font-medium text-slate-400">
                  {dockerPlan.source} / {dockerPlan.source_url}
                </p>
              )}

              <label className="mt-4 flex items-center gap-2 text-xs font-semibold text-slate-600">
                <input
                  type="checkbox"
                  checked={addDockerGroup}
                  onChange={(event) => onAddDockerGroupChange(event.target.checked)}
                  className="h-4 w-4 rounded border-slate-300 text-sky-500 focus:ring-sky-500"
                />
                安装后把当前用户加入 docker 组
              </label>

              <div className="mt-4 grid grid-cols-1 gap-2">
                <button
                  type="button"
                  onClick={onInstallDocker}
                  disabled={!canInstallDocker}
                  className="flex items-center justify-center gap-2 rounded-xl bg-sky-600 px-3 py-2 text-xs font-bold text-white hover:bg-sky-700 disabled:opacity-40"
                >
                  <Download size={13} />
                  安装 Docker 环境
                </button>
                {dockerPlan?.script && (
                  <button
                    type="button"
                    onClick={onToggleManualScript}
                    className="flex items-center justify-center gap-2 rounded-xl border border-slate-200 px-3 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50"
                  >
                    <FileText size={13} />
                    {showManualScript ? '隐藏 Docker 安装脚本' : '查看 Docker 安装脚本'}
                  </button>
                )}
              </div>

              {dockerPlan?.message && <p className="mt-3 text-[11px] font-semibold text-slate-500">{dockerPlan.message}</p>}
              {showManualScript && dockerPlan?.script && (
                <pre className="mt-4 max-h-[320px] overflow-auto rounded-2xl bg-slate-950 p-4 text-[11px] font-semibold leading-5 text-emerald-300">{dockerPlan.script}</pre>
              )}
            </div>
          )}

          <div>
            <h4 className="mb-3 flex items-center gap-2 text-sm font-bold text-slate-800">
              <Gauge size={16} className="text-emerald-600" />
              镜像加速与维护
            </h4>

            {isLinuxHost && (
              <>
                <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
                  Docker Hub 镜像加速
                  <select
                    value={dockerMirror}
                    onChange={(event) => onDockerMirrorChange(event.target.value as DockerMirrorID)}
                    className="rounded-xl border border-slate-200 bg-white p-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
                  >
                    {dockerMirrorOptions.map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                </label>
                <p className="mt-2 text-[11px] font-medium text-slate-400">拉取镜像慢或超时时使用。</p>

                {dockerMirrorPlan?.selected_mirrors && dockerMirrorPlan.selected_mirrors.length > 0 && (
                  <div className="mt-3 rounded-2xl border border-emerald-100 bg-emerald-50 p-3 text-[11px] font-semibold text-emerald-700">
                    <p>已选择：{dockerMirrorPlan.selected_mirrors.join(' / ')}</p>
                    {dockerMirrorPlan.existing_mirrors && dockerMirrorPlan.existing_mirrors.length > 0 && (
                      <p className="mt-1 break-all text-emerald-600">原配置：{dockerMirrorPlan.existing_mirrors.join(' / ')}</p>
                    )}
                  </div>
                )}

                {dockerMirrorPlan?.message && <p className="mt-3 text-[11px] font-semibold text-slate-500">{dockerMirrorPlan.message}</p>}

                <div className="mt-4 grid grid-cols-1 gap-2">
                  <button
                    type="button"
                    onClick={onConfigureMirrors}
                    disabled={!canConfigureMirrors}
                    className="flex items-center justify-center gap-2 rounded-xl bg-emerald-500 px-3 py-2 text-xs font-bold text-white hover:bg-emerald-600 disabled:opacity-40"
                  >
                    <Gauge size={13} />
                    配置镜像加速
                  </button>
                  {dockerMirrorPlan?.script && (
                    <button
                      type="button"
                      onClick={onToggleMirrorScript}
                      className="flex items-center justify-center gap-2 rounded-xl border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs font-bold text-emerald-700 hover:bg-emerald-100"
                    >
                      <FileText size={13} />
                      {showMirrorScript ? '隐藏镜像加速脚本' : '查看镜像加速脚本'}
                    </button>
                  )}
                </div>

                {showMirrorScript && dockerMirrorPlan?.script && (
                  <pre className="mt-4 max-h-[320px] overflow-auto rounded-2xl bg-slate-950 p-4 text-[11px] font-semibold leading-5 text-emerald-300">{dockerMirrorPlan.script}</pre>
                )}
              </>
            )}

            {isWindowsHost && (
              <div className="rounded-2xl border border-slate-100 bg-slate-50/70 p-3 text-xs font-semibold text-slate-500">
                Windows 模式不需要 Docker 镜像加速。
              </div>
            )}

            <div className="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-1 2xl:grid-cols-2">
              <button type="button" onClick={onCheckVersion} className="rounded-xl border border-slate-200 px-3 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50">
                检查更新
              </button>
              <button type="button" onClick={onUpdateIfNeeded} className="rounded-xl border border-sky-200 bg-sky-50 px-3 py-2 text-xs font-bold text-sky-700 hover:bg-sky-100">
                检查后更新
              </button>
            </div>

            <div className="mt-4 grid grid-cols-2 gap-3 text-xs font-semibold">
              <StatusItem label="进程" value={status?.status || 'stopped'} ok={status?.status === 'running'} />
              <StatusItem label="配置状态" value={status?.pending_restart ? '待重启' : '已生效'} ok={!status?.pending_restart} />
              <StatusItem label="游戏版本" value={versionInfo?.game_version || '离线未知'} ok={versionInfo?.compatible === true} />
              <StatusItem label="当前 Build" value={versionInfo?.current_build_id || '未知'} ok={Boolean(versionInfo?.current_build_id)} />
              <StatusItem label="最新 Build" value={versionInfo?.latest_build_id || '未检查'} ok={Boolean(versionInfo?.latest_build_id)} />
              <StatusItem
                label={`兼容范围 ${versionInfo?.compatibility_target || '1.x'}`}
                value={versionInfo?.compatible === true ? '兼容' : versionInfo?.compatible === false ? '不匹配' : '待运行确认'}
                ok={versionInfo?.compatible === true}
              />
            </div>

            {versionInfo?.error && (
              <div className="mt-3 rounded-2xl border border-amber-100 bg-amber-50 p-3 text-[11px] font-medium text-amber-800">
                {versionInfo.error}
              </div>
            )}
            {versionInfo?.compatibility_warnings && versionInfo.compatibility_warnings.length > 0 && (
              <div className="mt-3 rounded-2xl border border-amber-100 bg-amber-50 p-3 text-[11px] font-medium text-amber-800">
                {versionInfo.compatibility_warnings.join(' / ')}
              </div>
            )}
            {status?.warnings && status.warnings.length > 0 && (
              <div className="mt-3 rounded-2xl border border-amber-100 bg-amber-50 p-3 text-[11px] font-medium text-amber-800">
                {status.warnings.join(' / ')}
              </div>
            )}
          </div>
        </div>
      )}
    </section>
  );
};

const JobProgress: React.FC<{ job: Job }> = ({ job }) => (
  <div className="mt-5 rounded-2xl border border-slate-100 bg-slate-50 p-4">
    <div className="flex items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="truncate text-xs font-bold text-slate-700">{job.message || job.type}</p>
        {job.error && <p className="mt-1 text-[11px] font-medium text-rose-600">{job.error}</p>}
      </div>
      <StatusBadge status={job.status === 'running' ? 'running_job' : job.status} />
    </div>
    <div className="mt-3 h-2 overflow-hidden rounded-full bg-white">
      <div className="h-full rounded-full bg-sky-500 transition-all" style={{ width: `${job.progress}%` }} />
    </div>
  </div>
);

const getNextAction = ({
  loading,
  status,
  host,
  runtime,
  dockerReady,
  dockerPlan,
  requiredSystemMissing,
  isJobRunning,
  versionInfo,
}: {
  loading: boolean;
  status: ServerStatus | null;
  host: HostCapabilities | null;
  runtime: RuntimeMode;
  dockerReady: boolean;
  dockerPlan: DockerInstallPlan | null;
  requiredSystemMissing: Prerequisite[];
  isJobRunning: boolean;
  versionInfo: ServerVersionInfo | null;
}): NextAction => {
  if (loading) {
    return {
      kind: 'loading',
      label: '正在检查环境',
      description: '正在读取系统环境和服务端状态。',
      disabled: true,
    };
  }

  if (!host || !status) {
    return {
      kind: 'blocked',
      label: '重新检查',
      description: '无法读取服务端关键状态，请检查后端连接后重新检查。',
      disabled: true,
      disabledReason: '关键状态读取失败',
    };
  }

  const needsDocker = host.os === 'linux' && runtime === 'wine_docker';
  const blockedReason = host.supported
    ? requiredSystemMissing[0]?.message || requiredSystemMissing[0]?.label
    : host.unsupported_reason || '当前系统暂不支持自动开服。';

  if (!host.supported || requiredSystemMissing.length > 0) {
    return {
      kind: 'blocked',
      label: status.installed ? '启动服务端' : '安装并初始化服务端',
      description: blockedReason || '系统环境还没有准备好。',
      disabled: true,
      disabledReason: blockedReason,
    };
  }

  if (isJobRunning) {
    return {
      kind: 'blocked',
      label: '任务执行中',
      description: '正在执行安装或维护任务，请等待任务完成。',
      disabled: true,
    };
  }

  if (status.installed && versionInfo?.update_available) {
    return {
      kind: 'update_game',
      label: '更新游戏服务端',
      description: `检测到新 Build ${versionInfo.current_build_id || '未知'} -> ${versionInfo.latest_build_id || '未知'}。更新前会自动备份，完成后恢复原运行状态。`,
      disabled: false,
    };
  }

  if (status.status === 'running') {
    return {
      kind: 'running',
      label: '服务端运行中',
      description: '服务端已经启动，可以进入仪表盘查看运行状态。',
      disabled: true,
    };
  }

  if (needsDocker && !dockerReady) {
    if (!dockerPlan) {
      return {
        kind: 'check_docker',
        label: '检查 Docker 环境',
        description: 'Docker 安装方案会在需要时单独检测，不阻塞开服向导首屏。',
        disabled: false,
      };
    }
    const canAutoInstall = Boolean(dockerPlan?.supported && dockerPlan.can_auto_install);
    return {
      kind: 'install_docker',
      label: '安装 Docker 环境',
      description: canAutoInstall
        ? '先准备 Docker 环境，然后继续安装游戏服务端。'
        : '需要先在服务器终端执行下方命令安装 Docker。',
      disabled: !canAutoInstall,
      disabledReason: canAutoInstall ? undefined : dockerPlan?.message || '当前账号不能自动安装 Docker，请按下方命令手动执行。',
    };
  }

  if (!status.installed) {
    return {
      kind: 'bootstrap',
      label: host.os === 'windows' ? '安装 SteamCMD 和帕鲁服务端' : '安装并初始化服务端',
      description: '下一步会安装游戏服务端并生成默认配置。',
      disabled: false,
    };
  }

  if (!status.config_exists) {
    return {
      kind: 'initialize_config',
      label: '初始化配置',
      description: '服务端已安装，下一步生成默认配置文件。',
      disabled: false,
    };
  }

  return {
    kind: 'start_server',
    label: '启动服务端',
    description: '环境和配置已经就绪，可以启动服务端。',
    disabled: false,
  };
};

const getSimpleStatuses = ({
  loading,
  host,
  runtime,
  dockerReady,
  dockerPlan,
  status,
  requiredSystemMissing,
  dockerManualVisible,
}: {
  loading: boolean;
  host: HostCapabilities | null;
  runtime: RuntimeMode;
  dockerReady: boolean;
  dockerPlan: DockerInstallPlan | null;
  status: ServerStatus | null;
  requiredSystemMissing: Prerequisite[];
  dockerManualVisible: boolean;
}): SimpleStatus[] => {
  const systemOk = Boolean(host?.supported && requiredSystemMissing.length === 0);
  const needsDocker = host?.os === 'linux' && runtime === 'wine_docker';
  const systemReason = host?.unsupported_reason || requiredSystemMissing[0]?.message || requiredSystemMissing[0]?.label || '系统可以继续开服流程。';

  return [
    {
      label: '系统环境',
      value: loading ? '检查中' : systemOk ? '可用' : '需处理',
      description: loading ? '正在检查系统支持情况。' : systemReason,
      state: loading ? 'pending' : systemOk ? 'ok' : 'error',
    },
    {
      label: 'Docker 环境',
      value: !needsDocker ? '不需要' : dockerReady ? '已就绪' : dockerManualVisible ? '需手动安装' : '待安装',
      description: !needsDocker
        ? '当前安装方式不依赖 Docker。'
        : dockerReady
          ? 'Docker 可以正常连接。'
          : dockerPlan?.message || '先完成 Docker 环境，再安装服务端。',
      state: !needsDocker || dockerReady ? 'ok' : dockerManualVisible ? 'warning' : 'pending',
    },
    {
      label: '游戏服务端',
      value: status?.status === 'running' ? '运行中' : status?.installed ? (status.config_exists ? '可启动' : '待初始化') : '待安装',
      description: status?.status === 'running'
        ? '服务端正在运行。'
        : status?.installed
          ? status.config_exists
            ? '配置已存在，下一步启动服务端。'
            : '服务端已安装，下一步初始化配置。'
          : '下一步安装游戏服务端。',
      state: status?.status === 'running' ? 'ok' : status?.installed ? 'pending' : 'warning',
    },
  ];
};

const primaryActionIcon = (kind: NextActionKind) => {
  switch (kind) {
    case 'start_server':
      return <Play size={18} />;
    case 'running':
      return <CheckCircle2 size={18} />;
    case 'check_docker':
    case 'install_docker':
    case 'bootstrap':
    case 'initialize_config':
    case 'update_game':
      return <Download size={18} />;
    default:
      return <RefreshCw size={18} />;
  }
};

const statusDotClass = (state: SimpleStatus['state']) => {
  switch (state) {
    case 'ok':
      return 'bg-emerald-500';
    case 'warning':
      return 'bg-amber-500';
    case 'error':
      return 'bg-rose-500';
    case 'pending':
    default:
      return 'bg-sky-500';
  }
};

const platformLabel = (host: HostCapabilities) => {
  const distro = [host.distro_name, host.distro_version].filter(Boolean).join(' ');
  const base = distro || host.os || '未知';
  return host.arch ? `${base} / ${host.arch}` : base;
};

const isDockerPrerequisite = (item: Prerequisite) => {
  const key = `${item.id} ${item.label}`.toLowerCase();
  return key.includes('docker');
};

const dockerManualCommandFor = (plan: DockerInstallPlan, addDockerGroup: boolean) => {
  if (!plan.script_path && !plan.manual_command) return '';
  if (!addDockerGroup) return plan.manual_command || `sudo bash ${shellQuote(plan.script_path || 'install-docker.sh')}`;
  const targetUser = plan.host.current_user || 'palpanel';
  const scriptPath = plan.script_path || 'install-docker.sh';
  return `sudo env ADD_CURRENT_USER_TO_DOCKER_GROUP=1 TARGET_DOCKER_USER=${shellQuote(targetUser)} bash ${shellQuote(scriptPath)}`;
};

const dockerMirrorManualCommandFor = (plan: DockerMirrorPlan) => {
  if (!plan.script_path && !plan.manual_command) return '';
  return plan.manual_command || `sudo bash ${shellQuote(plan.script_path || 'configure-docker-mirrors.sh')}`;
};

const shellQuote = (value: string) => {
  if (!value) return "''";
  return `'${value.replaceAll("'", `'"'"'`)}'`;
};

const StatusItem: React.FC<{ label: string; value: string; ok: boolean }> = ({ label, value, ok }) => (
  <div className="rounded-2xl border border-slate-100 bg-slate-50/70 p-3">
    <span className="text-[11px] text-slate-400">{label}</span>
    <p className={`mt-1 break-words font-bold ${ok ? 'text-emerald-600' : 'text-slate-700'}`}>{value}</p>
  </div>
);

const NumberField: React.FC<{ label: string; value: number; onChange: (value: number) => void }> = ({ label, value, onChange }) => (
  <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
    {label}
    <input
      type="number"
      value={value}
      onChange={(event) => onChange(Number(event.target.value))}
      className="rounded-xl border border-slate-200 p-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
    />
  </label>
);

const TextField: React.FC<{ label: string; value: string; onChange: (value: string) => void }> = ({ label, value, onChange }) => (
  <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
    {label}
    <input
      type="text"
      value={value}
      onChange={(event) => onChange(event.target.value)}
      className="rounded-xl border border-slate-200 p-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
    />
  </label>
);

const SelectField: React.FC<{ label: string; value: string; onChange: (value: string) => void }> = ({ label, value, onChange }) => (
  <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
    {label}
    <select
      value={value}
      onChange={(event) => onChange(event.target.value)}
      className="rounded-xl border border-slate-200 bg-white p-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
    >
      <option value="text">text</option>
      <option value="json">json</option>
    </select>
  </label>
);
