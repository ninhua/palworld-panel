import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Activity, AlertCircle, Bug, Container, Cpu, FileText, HardDrive, MemoryStick, RefreshCw, RotateCcw, ShieldAlert, ShieldCheck, TriangleAlert } from 'lucide-react';
import { Link } from 'react-router-dom';
import { Area, AreaChart, CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { getErrorMessage } from '../api/client';
import { monitorApi } from '../api/monitor';
import { useServerStore } from '../store/useServerStore';
import type { CrashGuardStatus, DebugLogStatus, MonitorSample } from '../types';
import { StatCard } from '../components/ui/StatCard';
import { StatusBadge } from '../components/ui/StatusBadge';
import { chartTooltipFormatter, formatBytes, percent, toMonitorChartPoints } from '../utils/monitor';

export const Monitor: React.FC = () => {
  const { refreshKey, autoRefresh } = useServerStore();
  const [snapshot, setSnapshot] = useState<MonitorSample | null>(null);
  const [history, setHistory] = useState<MonitorSample[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [debugStatus, setDebugStatus] = useState<DebugLogStatus | null>(null);
  const [debugSaving, setDebugSaving] = useState(false);
  const [crashGuard, setCrashGuard] = useState<CrashGuardStatus | null>(null);
  const [recoveringGuard, setRecoveringGuard] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [nextSnapshot, nextHistory, nextDebugStatus, nextCrashGuard] = await Promise.all([
        monitorApi.snapshot(),
        monitorApi.history(120),
        monitorApi.debugStatus(),
        monitorApi.crashGuardStatus(),
      ]);
      setSnapshot(nextSnapshot.sample);
      setHistory(nextHistory);
      setDebugStatus(nextDebugStatus);
      setCrashGuard(nextCrashGuard);
      setError(null);
    } catch (loadError) {
      setSnapshot(null);
      setHistory([]);
      setCrashGuard(null);
      setError(getErrorMessage(loadError));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load, refreshKey]);

  useEffect(() => {
    if (!autoRefresh) return;
    const interval = window.setInterval(load, 15000);
    return () => window.clearInterval(interval);
  }, [autoRefresh, load]);

  const toggleDebug = async () => {
    if (!debugStatus || debugSaving) return;
    setDebugSaving(true);
    try {
      const next = await monitorApi.setDebug(!debugStatus.enabled);
      setDebugStatus(next);
      setError(null);
    } catch (toggleError) {
      setError(getErrorMessage(toggleError));
    } finally {
      setDebugSaving(false);
    }
  };

  const recoverCrashGuard = async (start: boolean) => {
    if (!crashGuard?.tripped || recoveringGuard) return;
    const action = start ? '解除熔断后重新启动 PalServer' : '仅解除熔断，暂不启动 PalServer';
    if (!window.confirm(`确认已完成故障检查，并${action}？`)) return;
    setRecoveringGuard(true);
    try {
      const next = await monitorApi.recoverCrashGuard(start);
      setCrashGuard(next);
      setError(null);
      await load();
    } catch (recoverError) {
      setError(getErrorMessage(recoverError, '解除崩溃熔断失败'));
    } finally {
      setRecoveringGuard(false);
    }
  };

  const chartData = useMemo(() => toMonitorChartPoints(history), [history]);

  const memoryPct = snapshot ? percent(snapshot.workload_memory_usage_bytes, snapshot.workload_memory_limit_bytes) : null;
  const hasMemoryPercent = chartData.some((point) => point.memoryPercent != null);
  const hasMemoryUsage = chartData.some((point) => point.memoryGiB != null);
  const memoryTrend =
    snapshot?.workload_memory_available && memoryPct != null
      ? `${memoryPct.toFixed(1)}% / ${formatBytes(snapshot.workload_memory_limit_bytes)}`
      : snapshot?.workload_memory_available
        ? '已采集，未提供内存上限'
        : snapshot?.unavailable_reason || '未采集';
  const diskUsedPct = snapshot
    ? percent(Math.max(0, snapshot.disk_total_bytes - snapshot.disk_free_bytes), snapshot.disk_total_bytes)
    : null;

  if (loading && !snapshot && history.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-12 text-xs font-semibold text-slate-400">
        <RefreshCw className="mr-2 animate-spin text-sky-500" size={16} />
        正在读取监控数据...
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      {error && (
        <div className="rounded-2xl border border-rose-100 bg-rose-50 px-5 py-3 text-xs font-semibold text-rose-700">
          <AlertCircle className="mr-2 inline" size={14} />
          {error}
        </div>
      )}

      <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-6">
        <StatCard
          title="在线玩家"
          value={snapshot ? `${snapshot.current_players} / ${snapshot.max_players || '-'}` : '不可用'}
          icon={<Activity size={16} />}
          trend="来自监控采样"
          trendType="info"
          color="sky"
        />
        <StatCard
          title="CPU"
          value={snapshot?.cpu_available ? `${snapshot.cpu_percent.toFixed(1)}%` : '不可用'}
          icon={<Cpu size={16} />}
          trend={snapshot?.cpu_available ? '已采集' : snapshot?.unavailable_reason || '未采集'}
          trendType={snapshot?.cpu_available ? 'neutral' : 'down'}
          color="blue"
        />
        <StatCard
          title="主机内存"
          value={snapshot?.host_memory_available ? formatBytes(Math.max(0, snapshot.host_memory_total_bytes - snapshot.host_memory_available_bytes)) : '不可用'}
          icon={<MemoryStick size={16} />}
          trend={snapshot?.host_memory_available ? `${formatBytes(snapshot.host_memory_available_bytes)} 可用 / ${formatBytes(snapshot.host_memory_total_bytes)}` : '未采集'}
          trendType={snapshot?.host_memory_available ? 'neutral' : 'down'}
          color="emerald"
        />
        <StatCard
          title="工作负载内存"
          value={snapshot?.workload_memory_available ? formatBytes(snapshot.workload_memory_usage_bytes) : '不可用'}
          icon={<Container size={16} />}
          trend={memoryTrend}
          trendType={snapshot?.workload_memory_available ? 'neutral' : 'down'}
          color="blue"
        />
        <StatCard
          title="交换空间"
          value={snapshot?.host_memory_available && snapshot.host_swap_total_bytes > 0 ? formatBytes(snapshot.host_swap_free_bytes) : '未配置'}
          icon={<RotateCcw size={16} />}
          trend={snapshot?.host_swap_total_bytes ? `${formatBytes(snapshot.host_swap_free_bytes)} 可用 / ${formatBytes(snapshot.host_swap_total_bytes)}` : '主机未配置 swap'}
          trendType={snapshot?.host_swap_total_bytes ? 'neutral' : 'info'}
          color="amber"
        />
        <StatCard
          title="磁盘"
          value={snapshot?.disk_available ? formatBytes(snapshot.disk_free_bytes) : '不可用'}
          icon={<HardDrive size={16} />}
          trend={diskUsedPct == null ? '未采集' : `已用 ${diskUsedPct.toFixed(1)}%`}
          trendType={snapshot?.disk_available ? 'neutral' : 'down'}
          color="amber"
        />
      </div>

      <section className="border-y border-slate-100 bg-white px-5 py-5">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <div className="flex items-center gap-2">
              <TriangleAlert size={16} className={snapshot?.oom_killed ? 'text-rose-500' : 'text-slate-400'} />
              <h3 className="text-[14px] font-bold text-slate-800">工作负载生命周期</h3>
            </div>
            <div className="mt-3 flex flex-wrap gap-x-5 gap-y-2 text-xs font-semibold text-slate-600">
              <span>{!snapshot?.lifecycle_available ? 'OOM 未采集' : snapshot.oom_killed ? 'OOM 已发生' : '未记录 OOM'}</span>
              <span>{!snapshot?.lifecycle_available ? '退出状态未采集' : snapshot.finished_at ? `退出码 ${snapshot.exit_code}` : '暂无退出记录'}</span>
              <span>{snapshot?.lifecycle_available ? `重启 ${snapshot.restart_count} 次` : '重启次数未采集'}</span>
              <span>启动：{snapshot?.started_at ? formatLifecycleTime(snapshot.started_at) : '未采集'}</span>
              <span>结束：{snapshot?.lifecycle_available ? formatLifecycleTime(snapshot.finished_at) : '未采集'}</span>
            </div>
          </div>
          <Link to="/dashboard" className="inline-flex shrink-0 items-center gap-2 text-xs font-bold text-sky-700 hover:text-sky-800">
            <RotateCcw size={14} />
            前往手动安全重启
          </Link>
        </div>
        {snapshot?.risk_reasons.length ? (
          <ul className="mt-4 grid gap-2 md:grid-cols-2" aria-label="风险原因">
            {snapshot.risk_reasons.map((reason) => (
              <li key={reason.code} className={`border-l-2 px-3 py-2 text-xs font-semibold ${reason.severity === 'critical' ? 'border-rose-400 bg-rose-50 text-rose-800' : 'border-amber-400 bg-amber-50 text-amber-800'}`}>
                <span className="mr-2 font-mono text-[10px]">{reason.code}</span>{reason.message}
              </li>
            ))}
          </ul>
        ) : (
          <p className="mt-4 text-xs font-semibold text-emerald-700">当前没有派生风险</p>
        )}
      </section>

      <section className={`border-y px-5 py-5 ${crashGuard?.tripped ? 'border-rose-200 bg-rose-50' : 'border-slate-100 bg-white'}`}>
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <div className="flex items-center gap-2">
              <ShieldAlert size={16} className={crashGuard?.tripped ? 'text-rose-600' : 'text-emerald-600'} />
              <h3 className="text-[14px] font-bold text-slate-800">崩溃守卫</h3>
              <StatusBadge status={crashGuard?.tripped ? 'error' : 'running'} customText={crashGuard?.tripped ? '已熔断' : '监控中'} />
            </div>
            <p className={`mt-2 text-xs font-semibold ${crashGuard?.tripped ? 'text-rose-800' : 'text-slate-600'}`}>
              {crashGuard?.tripped
                ? crashGuard.reason || '检测到崩溃循环，已暂停自动重启。'
                : `${Math.round((crashGuard?.window_seconds || 600) / 60)} 分钟内 ${crashGuard?.recent_crash_count || 0} / ${crashGuard?.threshold || 3} 次异常退出。`}
            </p>
            {crashGuard?.events.length ? (
              <div className="mt-3 space-y-1 text-[11px] font-medium text-slate-600">
                {crashGuard.events.slice(0, 3).map((event) => (
                  <p key={event.id}><span className="font-mono text-slate-400">{formatLifecycleTime(event.created_at)}</span> · {event.message}{event.occurrences > 1 ? ` ×${event.occurrences}` : ''}</p>
                ))}
              </div>
            ) : null}
          </div>
          <div className="flex shrink-0 flex-wrap gap-2">
            <Link to="/diagnostics" className="pp-button"><Bug size={14} />打开诊断控制台</Link>
            {crashGuard?.tripped && (
              <>
                <button type="button" disabled={recoveringGuard} onClick={() => recoverCrashGuard(false)} className="pp-button">
                  仅解除熔断
                </button>
                <button type="button" disabled={recoveringGuard} onClick={() => recoverCrashGuard(true)} className="pp-button border-rose-300 bg-rose-600 text-white hover:bg-rose-700">
                  <RotateCcw size={14} className={recoveringGuard ? 'animate-spin' : ''} />
                  {recoveringGuard ? '正在恢复' : '解除并启动'}
                </button>
              </>
            )}
          </div>
        </div>
      </section>

      <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
        <ChartCard title="在线人数历史">
          {chartData.length > 0 ? (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData} margin={{ top: 5, right: 10, left: -25, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#f1f5f9" />
                <XAxis dataKey="time" stroke="#94a3b8" fontSize={10} tickLine={false} />
                <YAxis stroke="#94a3b8" fontSize={10} tickLine={false} allowDecimals={false} />
                <Tooltip contentStyle={{ fontSize: '11px', borderRadius: '12px', border: '1px solid #f1f5f9' }} />
                <Line type="monotone" dataKey="players" name="玩家数" stroke="#0ea5e9" strokeWidth={2.5} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          ) : (
            <EmptyChart />
          )}
        </ChartCard>

        <ChartCard title="CPU / 工作负载内存历史">
          {chartData.length > 0 ? (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={{ top: 5, right: 10, left: -25, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#f1f5f9" />
                <XAxis dataKey="time" stroke="#94a3b8" fontSize={10} tickLine={false} />
                <YAxis yAxisId="percent" stroke="#94a3b8" fontSize={10} tickLine={false} domain={[0, 100]} tickFormatter={(value) => `${value}%`} />
                {!hasMemoryPercent && hasMemoryUsage && (
                  <YAxis yAxisId="memory" orientation="right" stroke="#94a3b8" fontSize={10} tickLine={false} width={36} tickFormatter={(value) => `${value}G`} />
                )}
                <Tooltip formatter={chartTooltipFormatter} contentStyle={{ fontSize: '11px', borderRadius: '12px', border: '1px solid #f1f5f9' }} />
                <Area yAxisId="percent" type="monotone" dataKey="cpu" name="CPU (%)" stroke="#2563eb" fill="#dbeafe" strokeWidth={1.5} connectNulls />
                {hasMemoryPercent ? (
                  <Area yAxisId="percent" type="monotone" dataKey="memoryPercent" name="工作负载内存 (%)" stroke="#4c7ea8" fill="#e7eef4" strokeWidth={1.5} connectNulls />
                ) : hasMemoryUsage ? (
                  <Area yAxisId="memory" type="monotone" dataKey="memoryGiB" name="工作负载内存用量 (GB)" stroke="#4c7ea8" fill="#e7eef4" strokeWidth={1.5} connectNulls />
                ) : null}
              </AreaChart>
            </ResponsiveContainer>
          ) : (
            <EmptyChart />
          )}
        </ChartCard>
      </div>

      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
        <div className="mb-4 flex items-center gap-2 border-b border-slate-100 pb-3">
          <ShieldCheck size={16} className="text-emerald-500" />
          <h3 className="text-[14px] font-bold text-slate-800">健康检查</h3>
        </div>
        <div className="grid grid-cols-1 gap-4 text-xs font-semibold text-slate-500 md:grid-cols-2 xl:grid-cols-4">
          <Health label="REST" healthy={Boolean(snapshot?.rest_healthy)} unavailableText={healthFailureLabel('REST', snapshot?.unavailable_reason)} />
          <Health label="RCON" healthy={Boolean(snapshot?.rcon_healthy)} unavailableText={healthFailureLabel('RCON', snapshot?.unavailable_reason)} />
          <Health label="Game Port" healthy={Boolean(snapshot?.game_port_healthy)} unavailableText="不可达" />
          <Health label="Query Port" healthy={Boolean(snapshot?.query_port_healthy)} unavailableText="不可达" />
        </div>
        {snapshot?.unavailable_reason && (
          <div className="mt-4 rounded-2xl border border-amber-100 bg-amber-50 p-4 text-xs font-semibold leading-6 text-amber-800">
            {snapshot.unavailable_reason}
          </div>
        )}
        {healthExplanation(snapshot) && (
          <div className="mt-3 rounded-2xl border border-sky-100 bg-sky-50 p-4 text-xs font-semibold leading-6 text-sky-800">
            {healthExplanation(snapshot)}
          </div>
        )}
        <p className="mt-4 text-[11px] font-medium text-slate-400">
          最近采样：{snapshot?.created_at ? new Date(snapshot.created_at).toLocaleString('zh-CN') : '未采集'}
        </p>
      </section>

      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <Bug size={16} className="text-violet-500" />
              <h3 className="text-[14px] font-bold text-slate-800">PalPanel 面板 Debug 日志</h3>
            </div>
            <p className="mt-2 text-xs font-semibold leading-5 text-slate-500">
              这是 PalPanel 面板自身的运行日志开关。开启后记录接口耗时、健康探测和连接结果，不会开启 Palworld 或 PalDefender 的 Debug，也不记录密码、Token 或请求正文。日志达到上限后自动轮转。
            </p>
          </div>
          <button
            type="button"
            onClick={() => void toggleDebug()}
            disabled={!debugStatus || debugSaving}
            className={`inline-flex shrink-0 items-center justify-center gap-2 rounded-xl px-4 py-2.5 text-xs font-bold text-white disabled:opacity-50 ${debugStatus?.enabled ? 'bg-rose-500 hover:bg-rose-600' : 'bg-violet-600 hover:bg-violet-700'}`}
          >
            <Bug size={14} />
            {debugSaving ? '正在保存' : debugStatus?.enabled ? '关闭面板 Debug' : '开启面板 Debug'}
          </button>
        </div>
        <div className="mt-4 flex min-w-0 items-start gap-3 rounded-2xl border border-slate-100 bg-slate-50 p-4">
          <FileText size={16} className="mt-0.5 shrink-0 text-slate-400" />
          <div className="min-w-0">
            <p className="text-[11px] font-bold text-slate-500">日志位置</p>
            <p className="mt-1 break-all font-mono text-[11px] font-semibold text-slate-700">{debugStatus?.path || '后端尚未返回日志路径'}</p>
            <p className="mt-1 text-[10px] font-medium text-slate-400">
              当前 {formatBytes(debugStatus?.size || 0)} · 单文件上限 {formatBytes(debugStatus?.max_bytes || 0)} · 保留 {debugStatus?.max_files || 0} 个轮转文件
            </p>
          </div>
        </div>
      </section>
    </div>
  );
};

const healthFailureLabel = (kind: 'REST' | 'RCON', reason?: string) => {
  const details = reason || '';
  if (kind === 'REST' && /authentication failed|status 401/i.test(details)) return '认证失败';
  if (/port .*match|port_mismatch|uses port .* maps/i.test(details)) return '端口不一致';
  if (new RegExp(`${kind}:.*disabled`, 'i').test(details)) return '未启用';
  if (new RegExp(`${kind}:.*connection refused`, 'i').test(details)) return '连接被拒绝';
  return kind === 'RCON' ? '未启用或不可达' : '不可达';
};

const formatLifecycleTime = (value?: string) => {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN');
};

const healthExplanation = (snapshot: MonitorSample | null) => {
  if (!snapshot || (snapshot.rest_healthy && snapshot.rcon_healthy)) return '';
  const reason = snapshot.unavailable_reason || '';
  if (/REST:.*authentication failed|REST:.*status 401/i.test(reason)) {
    return 'PalDefender GM 与游戏官方 REST 使用的是两套接口。GM 可用只说明 PalDefender 正常；这里的 401 表示官方 REST 已连通，但运行中的 Palworld 不接受当前管理密码。保存当前服务器配置后重启游戏服务端即可让密码重新加载。';
  }
  if (/RCON:.*uses port .*maps|RCON:.*does not match Linux container mapping/i.test(reason)) {
    return 'RCON 的游戏配置端口与 Linux 容器映射端口不同。把 PalWorldSettings.ini 的 RCONPort 和 PALPANEL_RCON_PORT 调成同一个值，然后重建或重启游戏容器。';
  }
  if (/RCON:.*127\.0\.0\.1.*connection refused/i.test(reason)) {
    return '当前地址没有进程监听 RCON。确认 RCONEnabled=True；如果面板本身运行在旧 Docker 容器中，还要把 PALPANEL_RCON_HOST 设置为游戏容器名或宿主机网关，不能继续指向面板容器自己的 127.0.0.1。';
  }
  return 'PalDefender GM、游戏官方 REST 和 RCON 是三条独立连接，健康状态不会互相替代。请按上方具体错误检查对应端口、密码和容器网络。';
};

const ChartCard: React.FC<{ title: string; children: React.ReactNode }> = ({ title, children }) => (
  <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
    <h3 className="mb-4 text-[14px] font-bold text-slate-800">{title}</h3>
    <div className="h-72 text-xs">{children}</div>
  </section>
);

const EmptyChart = () => (
  <div className="flex h-full items-center justify-center rounded-2xl border border-dashed border-slate-200 bg-slate-50 text-xs font-semibold text-slate-400">
    暂无历史采样
  </div>
);

const Health: React.FC<{ label: string; healthy: boolean; unavailableText: string }> = ({
  label,
  healthy,
  unavailableText,
}) => (
  <div className="flex items-center justify-between rounded-2xl border border-slate-100 bg-slate-50 p-4">
    <span className="text-slate-500">{label}</span>
    <StatusBadge status={healthy ? 'success' : 'failed'} customText={healthy ? '健康' : unavailableText} />
  </div>
);
