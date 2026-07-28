import React, { useEffect, useMemo, useState } from 'react';
import { AlertTriangle, Check, DownloadCloud, FolderDown, PackageCheck, Play, RefreshCw, Trash2 } from 'lucide-react';
import { useSearchParams } from 'react-router-dom';
import { getErrorMessage } from '../api/client';
import { schedulesApi } from '../api/schedules';
import { tasksApi } from '../api/tasks';
import { useServerStore } from '../store/useServerStore';
import type { Alert, Job, Schedule, ScheduleType } from '../types';
import { DataTable } from '../components/ui/DataTable';
import { StatusBadge } from '../components/ui/StatusBadge';

const scheduleTypes: Array<{ value: ScheduleType; label: string }> = [
  { value: 'save', label: '保存世界' },
  { value: 'backup', label: '创建备份' },
  { value: 'safe_restart', label: '定时安全重启' },
  { value: 'update', label: '更新服务端' },
  { value: 'version_check', label: '检查更新' },
];

const jobTypeLabel = (type: string) => {
  switch (type) {
    case 'backup':
      return '全服备份';
    case 'update':
      return '服务端更新';
    case 'version_check':
      return '检查更新';
    case 'smart_update':
      return '检查后更新';
    case 'install':
      return '服务端安装';
    case 'patch_hot_update':
      return '补丁热更新';
    case 'bootstrap':
      return '开服初始化';
    case 'docker_install':
      return 'Docker 安装';
    case 'workshop_download':
      return 'Workshop 下载';
    case 'paldefender_install':
      return 'PalDefender 安装';
    case 'paldefender_update':
      return 'PalDefender 更新';
    case 'restart':
      return '服务器重启';
    case 'broadcast':
      return '系统广播';
    case 'save':
      return '保存世界';
    case 'shutdown':
      return '关闭服务器';
    case 'safe_restart':
      return '安全重启';
    case 'safe_stop':
      return '安全关服';
    case 'restore':
      return '备份恢复';
    default:
      return type;
  }
};

const scheduleTypeLabel = (type: string) => scheduleTypes.find((item) => item.value === type)?.label || type;

const browserTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';

export const TaskQueue: React.FC = () => {
  const { refreshKey } = useServerStore();
  const [searchParams, setSearchParams] = useSearchParams();
  const [jobs, setJobs] = useState<Job[]>([]);
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [loading, setLoading] = useState(true);
  const [scheduleLoading, setScheduleLoading] = useState(true);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const requestedTab = searchParams.get('tab');
  const initialTab = requestedTab === 'schedules' || requestedTab === 'alerts' ? requestedTab : 'jobs';
  const [activeTab, setActiveTabState] = useState<'jobs' | 'schedules' | 'alerts'>(initialTab);
  const [draft, setDraft] = useState({
    type: 'backup' as ScheduleType,
    mode: 'interval' as 'interval' | 'daily',
    interval_minutes: 60,
    time_of_day: '04:00',
    timezone: browserTimezone,
    waittime: 60,
    message: '服务器即将保存并进行例行重启，请暂时停止操作。',
  });

  const fetchJobs = async () => {
    setLoading(true);
    try {
      const data = await tasksApi.getJobs();
      setJobs(Array.isArray(data) ? data : []);
      setError(null);
    } catch (loadError) {
      setJobs([]);
      setError(getErrorMessage(loadError));
    } finally {
      setLoading(false);
    }
  };

  const fetchSchedulesAndAlerts = async () => {
    setScheduleLoading(true);
    try {
      const [nextSchedules, nextAlerts] = await Promise.all([schedulesApi.list(), schedulesApi.alerts()]);
      setSchedules(nextSchedules);
      setAlerts(nextAlerts);
    } catch (loadError) {
      setMessage(getErrorMessage(loadError));
    } finally {
      setScheduleLoading(false);
    }
  };

  useEffect(() => {
    fetchJobs();
    fetchSchedulesAndAlerts();
  }, [refreshKey]);

  useEffect(() => {
    if (requestedTab === 'jobs' || requestedTab === 'schedules' || requestedTab === 'alerts') {
      setActiveTabState(requestedTab);
    }
  }, [requestedTab]);

  const setActiveTab = (tab: 'jobs' | 'schedules' | 'alerts') => {
    setActiveTabState(tab);
    setSearchParams(tab === 'jobs' ? {} : { tab });
  };

  useEffect(() => {
    const hasRunning = jobs.some((job) => job.status === 'running');
    if (!hasRunning) return;
    const interval = window.setInterval(fetchJobs, 1000);
    return () => window.clearInterval(interval);
  }, [jobs]);

  const openAlerts = useMemo(() => alerts.filter((alert) => alert.status !== 'acked'), [alerts]);
  const patchUpdateRunning = jobs.some((job) => job.type === 'patch_hot_update' && (job.status === 'waiting' || job.status === 'running'));

  const createBackup = async () => {
    try {
      await tasksApi.createBackupJob();
      await fetchJobs();
      setMessage('备份任务已提交');
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    }
  };

  const createUpdate = async () => {
    try {
      await tasksApi.createUpdateJob();
      await fetchJobs();
      setMessage('更新任务已提交');
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    }
  };

  const createPatchUpdate = async () => {
    try {
      await tasksApi.createPatchUpdateJob();
      await fetchJobs();
      setMessage('补丁热更新任务已提交');
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    }
  };

  const createSchedule = async () => {
    try {
      await schedulesApi.create({
        type: draft.type,
        enabled: true,
        interval_minutes: draft.mode === 'interval' ? draft.interval_minutes : undefined,
        time_of_day: draft.mode === 'daily' ? draft.time_of_day : undefined,
        timezone: draft.mode === 'daily' ? draft.timezone : undefined,
        waittime: draft.type === 'safe_restart' ? draft.waittime : undefined,
        message: draft.type === 'safe_restart' ? draft.message : undefined,
      });
      setMessage('计划任务已创建');
      await fetchSchedulesAndAlerts();
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    }
  };

  const toggleSchedule = async (schedule: Schedule) => {
    try {
      await schedulesApi.update(schedule.id, { ...schedule, enabled: !schedule.enabled });
      await fetchSchedulesAndAlerts();
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    }
  };

  const runSchedule = async (schedule: Schedule) => {
    try {
      await schedulesApi.run(schedule.id);
      setMessage(`已立即运行计划任务：${scheduleTypeLabel(schedule.type)}`);
      await Promise.all([fetchJobs(), fetchSchedulesAndAlerts()]);
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    }
  };

  const deleteSchedule = async (schedule: Schedule) => {
    if (!window.confirm(`删除计划任务 ${scheduleTypeLabel(schedule.type)}？`)) return;
    try {
      await schedulesApi.delete(schedule.id);
      await fetchSchedulesAndAlerts();
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    }
  };

  const ackAlert = async (alert: Alert) => {
    try {
      await schedulesApi.ackAlert(alert.id);
      await fetchSchedulesAndAlerts();
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    }
  };

  const jobHeaders = [
    { key: 'id', label: '任务 ID' },
    { key: 'type', label: '类型' },
    { key: 'status', label: '状态' },
    { key: 'progress', label: '进度' },
    { key: 'message', label: '消息' },
    { key: 'created_at', label: '创建时间' },
    { key: 'updated_at', label: '最后活动' },
  ];

  const scheduleHeaders = [
    { key: 'type', label: '类型' },
    { key: 'rule', label: '规则' },
    { key: 'enabled', label: '状态' },
    { key: 'last', label: '上次运行' },
    { key: 'next', label: '下次运行' },
    { key: 'actions', label: '操作', align: 'center' as const },
  ];

  const alertHeaders = [
    { key: 'severity', label: '级别' },
    { key: 'title', label: '标题' },
    { key: 'message', label: '消息' },
    { key: 'source', label: '来源' },
    { key: 'created_at', label: '创建时间' },
    { key: 'actions', label: '操作', align: 'center' as const },
  ];

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      {error && <div className="rounded-2xl border border-rose-100 bg-rose-50 px-5 py-3 text-xs font-semibold text-rose-700">{error}</div>}
      {message && <div className="rounded-2xl border border-sky-100 bg-sky-50 px-5 py-3 text-xs font-semibold text-sky-700">{message}</div>}

      <div className="flex flex-col gap-3 sm:flex-row">
        <button
          type="button"
          onClick={createBackup}
          className="flex items-center justify-center gap-2 rounded-2xl bg-sky-500 px-5 py-3 text-xs font-semibold text-white hover:bg-sky-600"
        >
          <FolderDown size={14} />
          立即执行备份
        </button>

        <button
          type="button"
          onClick={createUpdate}
          className="flex items-center justify-center gap-2 rounded-2xl border border-slate-200 bg-white px-5 py-3 text-xs font-semibold text-slate-600 hover:bg-slate-50"
        >
          <DownloadCloud size={14} />
          检查并更新服务端
        </button>

        <button
          type="button"
          onClick={createPatchUpdate}
          disabled={patchUpdateRunning}
          className="flex items-center justify-center gap-2 rounded-2xl border border-emerald-200 bg-emerald-50 px-5 py-3 text-xs font-semibold text-emerald-700 hover:bg-emerald-100 disabled:cursor-not-allowed disabled:opacity-45"
        >
          <PackageCheck size={14} />
          {patchUpdateRunning ? '补丁更新中' : '补丁热更新'}
        </button>
      </div>

      <section className="rounded-3xl border border-slate-100 bg-white p-4 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)] sm:p-6">
        <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center overflow-x-auto rounded-xl border border-slate-100/70 bg-slate-100/60 p-0.5">
            {[
              ['jobs', `任务队列 (${jobs.length})`],
              ['schedules', `计划任务 (${schedules.length})`],
              ['alerts', `告警 (${openAlerts.length})`],
            ].map(([id, label]) => (
              <button
                key={id}
                type="button"
                onClick={() => setActiveTab(id as 'jobs' | 'schedules' | 'alerts')}
                className={`shrink-0 rounded-lg px-3 py-1.5 text-xs font-semibold ${
                  activeTab === id ? 'border border-slate-100/70 bg-white text-slate-900 shadow-sm' : 'text-slate-500 hover:text-slate-800'
                }`}
              >
                {label}
              </button>
            ))}
          </div>
          <button
            type="button"
            onClick={() => {
              fetchJobs();
              fetchSchedulesAndAlerts();
            }}
            className="flex items-center gap-2 rounded-xl border border-slate-200 px-4 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50"
          >
            <RefreshCw size={14} />
            刷新
          </button>
        </div>

        {activeTab === 'jobs' &&
          (loading && jobs.length === 0 ? (
            <div className="py-12 text-center text-xs font-semibold text-slate-400">
              <RefreshCw className="mr-2 inline animate-spin text-sky-500" size={14} />
              正在获取任务队列...
            </div>
          ) : (
            <DataTable
              headers={jobHeaders}
              data={jobs}
              title="最近任务运行记录"
              emptyText={error ? '后端不可用或接口未实现' : '暂无任务'}
              renderCard={(job) => <JobCard key={job.id} job={job} />}
              renderRow={(job) => (
                <tr key={job.id} className="hover:bg-slate-50/50">
                  <td className="px-6 py-4 font-mono text-xs font-bold text-slate-600">#{job.id}</td>
                  <td className="px-6 py-4 text-xs font-bold text-slate-700">{jobTypeLabel(job.type)}</td>
                  <td className="px-6 py-4">
                    <StatusBadge status={job.status === 'running' ? 'running_job' : job.status} />
                  </td>
                  <td className="px-6 py-4">
                    <Progress job={job} />
                  </td>
                  <td className="max-w-[320px] px-6 py-4 text-xs font-medium text-slate-500">
                    <p>{job.error || job.message || '-'}</p>
                    {isSteamCMDInstallStage(job) && (
                      <p className="mt-1.5 text-[10px] font-semibold leading-4 text-sky-700">
                        SteamCMD 下载、解压和校验都处于 60% 阶段；最后活动时间持续更新即表示任务仍正常运行，请勿重启 PalPanel。
                      </p>
                    )}
                  </td>
                  <td className="px-6 py-4 text-xs font-medium text-slate-400">{formatJobTime(job.created_at)}</td>
                  <td className="px-6 py-4 text-xs font-medium text-slate-400" title={job.updated_at || job.created_at}>
                    {jobActivityText(job.updated_at || job.created_at)}
                  </td>
                </tr>
              )}
            />
          ))}

        {activeTab === 'schedules' && (
          <div className="flex flex-col gap-5">
            <ScheduleForm draft={draft} setDraft={setDraft} onSubmit={createSchedule} />
            {scheduleLoading && schedules.length === 0 ? (
              <div className="py-12 text-center text-xs font-semibold text-slate-400">正在读取计划任务...</div>
            ) : (
              <DataTable
                headers={scheduleHeaders}
                data={schedules}
                title="计划任务"
                emptyText="暂无计划任务"
                renderCard={(schedule) => (
                  <ScheduleCard
                    key={schedule.id}
                    schedule={schedule}
                    onToggle={() => toggleSchedule(schedule)}
                    onRun={() => runSchedule(schedule)}
                    onDelete={() => deleteSchedule(schedule)}
                  />
                )}
                renderRow={(schedule) => (
                  <tr key={schedule.id} className="hover:bg-slate-50/50">
                    <td className="px-6 py-4 text-xs font-bold text-slate-700">{scheduleTypeLabel(schedule.type)}</td>
                    <td className="px-6 py-4 text-xs font-semibold text-slate-500">{scheduleRule(schedule)}</td>
                    <td className="px-6 py-4">
                      <StatusBadge status={schedule.enabled ? 'enabled' : 'disabled'} />
                    </td>
                    <td className="px-6 py-4 text-xs font-medium text-slate-400">{schedule.last_run_at || '-'}</td>
                    <td className="px-6 py-4 text-xs font-medium text-slate-400">{schedule.next_run_at || '-'}</td>
                    <td className="px-6 py-4 text-center">
                      <ScheduleActions
                        enabled={schedule.enabled}
                        onToggle={() => toggleSchedule(schedule)}
                        onRun={() => runSchedule(schedule)}
                        onDelete={() => deleteSchedule(schedule)}
                      />
                    </td>
                  </tr>
                )}
              />
            )}
          </div>
        )}

        {activeTab === 'alerts' && (
          <DataTable
            headers={alertHeaders}
            data={alerts}
            title="失败告警"
            emptyText="暂无告警"
            renderCard={(alert) => (
              <AlertCard key={alert.id} alert={alert} onAck={() => ackAlert(alert)} />
            )}
            renderRow={(alert) => (
              <tr key={alert.id} className="hover:bg-slate-50/50">
                <td className="px-6 py-4">
                  <AlertSeverity alert={alert} />
                </td>
                <td className="px-6 py-4 text-xs font-bold text-slate-700">{alert.title}</td>
                <td className="max-w-[360px] px-6 py-4 text-xs font-medium text-slate-500">{alert.message}</td>
                <td className="px-6 py-4 font-mono text-[11px] text-slate-400">{alert.source || '-'}</td>
                <td className="px-6 py-4 text-xs font-medium text-slate-400">{alert.created_at}</td>
                <td className="px-6 py-4 text-center">
                  <button
                    type="button"
                    onClick={() => ackAlert(alert)}
                    disabled={alert.status === 'acked'}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 px-3 py-2 text-[10px] font-bold text-slate-500 hover:bg-slate-50 disabled:opacity-40"
                  >
                    <Check size={12} />
                    确认
                  </button>
                </td>
              </tr>
            )}
          />
        )}
      </section>
    </div>
  );
};

const scheduleRule = (schedule: Schedule) => {
  if (schedule.interval_minutes) return `每 ${schedule.interval_minutes} 分钟`;
  if (schedule.time_of_day) return `每日 ${schedule.time_of_day} (${schedule.timezone || 'UTC'})`;
  return '-';
};

const ScheduleForm: React.FC<{
  draft: {
    type: ScheduleType;
    mode: 'interval' | 'daily';
    interval_minutes: number;
    time_of_day: string;
    timezone: string;
    waittime: number;
    message: string;
  };
  setDraft: React.Dispatch<
    React.SetStateAction<{
      type: ScheduleType;
      mode: 'interval' | 'daily';
      interval_minutes: number;
      time_of_day: string;
      timezone: string;
      waittime: number;
      message: string;
    }>
  >;
  onSubmit: () => void;
}> = ({ draft, setDraft, onSubmit }) => (
  <div className="rounded-2xl border border-slate-100 bg-slate-50 p-4">
    <p className="mb-4 text-[11px] font-semibold leading-5 text-slate-500">
      选择“定时安全重启”可在指定时间先保存世界、向在线玩家发送公告，再按倒计时重启；自动备份可在“备份与恢复”中继续配置 WebDAV 上传。
    </p>
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-6">
      <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
        类型
        <select
          value={draft.type}
          onChange={(event) => setDraft((prev) => ({ ...prev, type: event.target.value as ScheduleType }))}
          className="rounded-xl border border-slate-200 bg-white p-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
        >
          {scheduleTypes.map((item) => (
            <option key={item.value} value={item.value}>
              {item.label}
            </option>
          ))}
        </select>
      </label>
      <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
        规则
        <select
          value={draft.mode}
          onChange={(event) => setDraft((prev) => ({ ...prev, mode: event.target.value as 'interval' | 'daily' }))}
          className="rounded-xl border border-slate-200 bg-white p-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
        >
          <option value="interval">每 N 分钟</option>
          <option value="daily">每日 HH:mm</option>
        </select>
      </label>
      {draft.mode === 'interval' ? (
        <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
          间隔分钟
          <input
            type="number"
            min={1}
            value={draft.interval_minutes}
            onChange={(event) => setDraft((prev) => ({ ...prev, interval_minutes: Number(event.target.value) }))}
            className="rounded-xl border border-slate-200 bg-white p-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
          />
        </label>
      ) : (
        <>
          <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
            每日时间
            <input
              type="time"
              value={draft.time_of_day}
              onChange={(event) => setDraft((prev) => ({ ...prev, time_of_day: event.target.value }))}
              className="rounded-xl border border-slate-200 bg-white p-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
            时区
            <input
              type="text"
              value={draft.timezone}
              onChange={(event) => setDraft((prev) => ({ ...prev, timezone: event.target.value }))}
              className="rounded-xl border border-slate-200 bg-white p-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
            />
          </label>
        </>
      )}
      <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
        重启倒计时（秒）
        <input
          type="number"
          min={5}
          value={draft.waittime}
          disabled={draft.type !== 'safe_restart'}
          onChange={(event) => setDraft((prev) => ({ ...prev, waittime: Number(event.target.value) }))}
          className="rounded-xl border border-slate-200 bg-white p-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none disabled:bg-slate-100 disabled:text-slate-400"
        />
      </label>
      <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500 xl:col-span-2">
        消息
        <input
          type="text"
          value={draft.message}
          disabled={draft.type !== 'safe_restart'}
          onChange={(event) => setDraft((prev) => ({ ...prev, message: event.target.value }))}
          className="rounded-xl border border-slate-200 bg-white p-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none disabled:bg-slate-100 disabled:text-slate-400"
        />
      </label>
    </div>
    <button type="button" onClick={onSubmit} className="mt-4 rounded-xl bg-sky-600 px-4 py-2 text-xs font-bold text-white hover:bg-sky-700">
      新增计划任务
    </button>
  </div>
);

const ScheduleActions: React.FC<{ enabled: boolean; onToggle: () => void; onRun: () => void; onDelete: () => void }> = ({
  enabled,
  onToggle,
  onRun,
  onDelete,
}) => (
  <div className="flex justify-center gap-2">
    <button type="button" onClick={onToggle} className="rounded-lg border border-slate-200 px-3 py-2 text-[10px] font-bold text-slate-500 hover:bg-slate-50">
      {enabled ? '停用' : '启用'}
    </button>
    <button type="button" onClick={onRun} className="rounded-lg border border-sky-200 p-2 text-sky-600 hover:bg-sky-50" title="立即运行">
      <Play size={14} />
    </button>
    <button type="button" onClick={onDelete} className="rounded-lg border border-rose-200 p-2 text-rose-500 hover:bg-rose-50" title="删除">
      <Trash2 size={14} />
    </button>
  </div>
);

const ScheduleCard: React.FC<{ schedule: Schedule; onToggle: () => void; onRun: () => void; onDelete: () => void }> = ({
  schedule,
  onToggle,
  onRun,
  onDelete,
}) => (
  <div className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
    <div className="flex items-start justify-between gap-3">
      <div>
        <p className="text-sm font-bold text-slate-800">{scheduleTypeLabel(schedule.type)}</p>
        <p className="mt-1 text-[11px] font-semibold text-slate-400">{scheduleRule(schedule)}</p>
      </div>
      <StatusBadge status={schedule.enabled ? 'enabled' : 'disabled'} />
    </div>
    <p className="mt-3 text-[11px] font-medium text-slate-500">下次运行：{schedule.next_run_at || '-'}</p>
    <div className="mt-4">
      <ScheduleActions enabled={schedule.enabled} onToggle={onToggle} onRun={onRun} onDelete={onDelete} />
    </div>
  </div>
);

const AlertSeverity: React.FC<{ alert: Alert }> = ({ alert }) => {
  const status = alert.status === 'acked' ? 'disabled' : alert.severity === 'error' ? 'failed' : alert.severity === 'warning' ? 'Warning' : 'success';
  return <StatusBadge status={status} customText={alert.status === 'acked' ? '已确认' : alert.severity} />;
};

const AlertCard: React.FC<{ alert: Alert; onAck: () => void }> = ({ alert, onAck }) => (
  <div className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
    <div className="flex items-start justify-between gap-3">
      <div>
        <p className="flex items-center gap-1.5 text-sm font-bold text-slate-800">
          <AlertTriangle size={14} className={alert.severity === 'error' ? 'text-rose-500' : 'text-amber-500'} />
          {alert.title}
        </p>
        <p className="mt-2 text-[11px] font-medium text-slate-500">{alert.message}</p>
      </div>
      <AlertSeverity alert={alert} />
    </div>
    <div className="mt-4 flex items-center justify-between gap-3">
      <span className="font-mono text-[10px] text-slate-400">{alert.created_at}</span>
      <button type="button" onClick={onAck} disabled={alert.status === 'acked'} className="rounded-lg border border-slate-200 px-3 py-2 text-[10px] font-bold text-slate-500 hover:bg-slate-50 disabled:opacity-40">
        确认
      </button>
    </div>
  </div>
);

const progressText = (status: Job['status']) => {
  if (status === 'running') return '阶段进度';
  if (status === 'success') return '已完成';
  if (status === 'failed') return '中断于';
  return '等待中';
};

const isSteamCMDInstallStage = (job: Job) => (
  job.status === 'running'
  && job.progress === 60
  && ['bootstrap', 'install', 'update', 'smart_update'].includes(job.type)
);

const formatJobTime = (value?: string) => {
  if (!value) return '-';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString('zh-CN', { hour12: false });
};

const jobActivityText = (value?: string) => {
  if (!value) return '-';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  const seconds = Math.max(0, Math.floor((Date.now() - parsed.getTime()) / 1000));
  if (seconds < 60) return `${seconds} 秒前`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
  return formatJobTime(value);
};

const Progress: React.FC<{ job: Job }> = ({ job }) => (
  <div className="flex w-36 max-w-full flex-col gap-1.5">
    <div className="flex justify-between text-[10px] font-bold text-slate-400">
      <span>{progressText(job.status)}</span>
      <span>{job.progress || 0}%</span>
    </div>
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-100">
      <div
        style={{ width: `${job.progress || 0}%` }}
        className={`h-full rounded-full transition-all duration-300 ${
          job.status === 'failed' ? 'bg-rose-500' : job.status === 'success' ? 'bg-emerald-500' : 'animate-pulse bg-sky-500'
        }`}
      />
    </div>
  </div>
);

const JobCard: React.FC<{ job: Job }> = ({ job }) => (
  <div className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <p className="text-sm font-bold text-slate-800">{jobTypeLabel(job.type)}</p>
        <p className="mt-1 truncate font-mono text-[10px] text-slate-400">#{job.id}</p>
      </div>
      <StatusBadge status={job.status === 'running' ? 'running_job' : job.status} />
    </div>
    <div className="mt-4">
      <Progress job={job} />
    </div>
    <p className="mt-3 text-[11px] font-medium text-slate-500">{job.error || job.message || '无消息'}</p>
    {isSteamCMDInstallStage(job) && (
      <p className="mt-2 text-[10px] font-semibold leading-4 text-sky-700">60% 是 SteamCMD 下载、解压和校验阶段；最后活动时间持续更新即正常，请勿重启 PalPanel。</p>
    )}
    <p className="mt-2 text-[10px] text-slate-400">最后活动：{jobActivityText(job.updated_at || job.created_at)}</p>
  </div>
);
