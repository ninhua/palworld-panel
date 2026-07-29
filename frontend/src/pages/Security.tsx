import React, { useEffect, useState } from 'react';
import { Gamepad2, KeyRound, RefreshCw, RotateCcw, Save, ShieldCheck, Sparkles, TerminalSquare, Wand2 } from 'lucide-react';
import { Link } from 'react-router-dom';
import { getErrorMessage } from '../api/client';
import { palDefenderGMApi } from '../api/paldefenderGM';
import { palDefenderPanelPermissions, securityApi } from '../api/security';
import { tasksApi } from '../api/tasks';
import type { Job, PalDefenderRCONResult, PalDefenderRelease, PalDefenderStatus, TokenResult } from '../types';
import { StatusBadge } from '../components/ui/StatusBadge';

export const Security: React.FC = () => {
  const [status, setStatus] = useState<PalDefenderStatus | null>(null);
  const [releases, setReleases] = useState<PalDefenderRelease[]>([]);
  const [configText, setConfigText] = useState('{}');
  const [tokenName, setTokenName] = useState('AdminPanel');
  const [tokenResult, setTokenResult] = useState<TokenResult | null>(null);
  const [activeJob, setActiveJob] = useState<Job | null>(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState<string | null>(null);
  const [runtimeCommands, setRuntimeCommands] = useState<PalDefenderRCONResult | null>(null);
  const [runtimeCommandsError, setRuntimeCommandsError] = useState('');
  const [runtimeCommandsLoading, setRuntimeCommandsLoading] = useState(false);

  const load = async () => {
    setLoading(true);
    const [statusResult, releasesResult] = await Promise.allSettled([securityApi.status(), securityApi.releases()]);
    if (releasesResult.status === 'fulfilled') {
      setReleases(releasesResult.value);
    } else {
      setReleases([]);
    }
    if (statusResult.status === 'rejected') {
      setStatus(null);
      setMessage(getErrorMessage(statusResult.reason));
      setLoading(false);
      return;
    }
    const nextStatus = statusResult.value;
    setStatus(nextStatus);
    try {
      const config =
        nextStatus.installed && !nextStatus.needs_first_start
          ? await securityApi.getConfig()
          : {};
      setConfigText(JSON.stringify(config, null, 2));
    } catch (error) {
      setMessage(`PalDefender 本地配置读取失败：${getErrorMessage(error)}`);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const trackJob = async (job: Job) => {
    setActiveJob(job);
    const done = await tasksApi.waitForJob(job.id, setActiveJob);
    setMessage(done.status === 'success' ? 'PalDefender 任务已完成' : done.error || 'PalDefender 任务失败');
    await load();
  };

  const runJob = async (start: () => Promise<Job>) => {
    try {
      const job = await start();
      await trackJob(job);
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const rollback = async () => {
    if (!window.confirm('回滚到最近一次 PalDefender 备份？')) return;
    try {
      const result = await securityApi.rollback();
      setStatus(result);
      setMessage('已回滚到最近备份');
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const applyPreset = async () => {
    try {
      const cfg = await securityApi.applyPreset('balanced');
      setConfigText(JSON.stringify(cfg, null, 2));
      setMessage('已应用 balanced 推荐配置');
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const saveConfig = async () => {
    try {
      const parsed = JSON.parse(configText) as Record<string, unknown>;
      const saved = await securityApi.putConfig(parsed);
      setConfigText(JSON.stringify(saved, null, 2));
      setMessage('PalDefender 配置已保存');
    } catch (error) {
      setMessage(error instanceof SyntaxError ? 'Config.json 不是合法 JSON' : getErrorMessage(error));
    }
  };

  const createToken = async () => {
    try {
      const token = await securityApi.createToken(tokenName, [...palDefenderPanelPermissions]);
      setTokenResult(token);
      setMessage('已生成面板专用 REST Token，请妥善保存');
      await load();
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const reloadConfig = async () => {
    try {
      const result = await securityApi.reloadConfig();
      setMessage(result.reloaded ? 'PalDefender 已重新加载配置' : 'ReloadConfig 请求未确认，可能服务未启动');
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const inspectRuntimeCommands = async () => {
    setRuntimeCommandsLoading(true);
    setRuntimeCommandsError('');
    try {
      setRuntimeCommands(await palDefenderGMApi.runtimeCommands());
    } catch (error) {
      setRuntimeCommands(null);
      setRuntimeCommandsError(getErrorMessage(error));
    } finally {
      setRuntimeCommandsLoading(false);
    }
  };

  const latest = releases[0];

  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-6 p-4 sm:p-6 lg:p-8">
      {message && <div className="rounded-2xl border border-sky-100 bg-sky-50 px-5 py-3 text-xs font-semibold text-sky-700">{message}</div>}

      <div className="grid grid-cols-1 gap-6 xl:grid-cols-3">
        <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
          <h3 className="mb-4 flex items-center gap-2 text-[15px] font-bold text-slate-800">
            <ShieldCheck size={18} className="text-emerald-500" />
            PalDefender 状态
          </h3>
          {loading || !status ? (
            <p className="text-xs font-semibold text-slate-400">正在读取状态...</p>
          ) : (
            <div className="flex flex-col gap-3">
              <InfoRow label="安装状态" value={status.installed ? '已安装' : '未安装'} ok={status.installed} />
              <InfoRow label="版本" value={status.version || '未知'} ok={Boolean(status.version)} />
              <InfoRow label="更新来源" value={status.release_source === 'github_latest' ? 'GitHub 最新稳定版' : status.release_source} ok={status.release_source === 'github_latest'} />
              <InfoRow label="REST API" value={status.rest_api_enabled ? '已启用' : '未启用'} ok={status.rest_api_enabled} />
              <InfoRow label="PalDefender 加载" value={status.load_verified ? '启动日志已确认' : '尚未确认'} ok={status.load_verified} />
              <InfoRow
                label="UE4SS"
                value={status.ue4ss.version ? `${status.ue4ss.state} (${status.ue4ss.version})` : status.ue4ss.state}
                ok={status.ue4ss.compatible}
              />
              <InfoRow label="UE4SS 加载" value={status.ue4ss.load_verified ? '启动日志已确认' : '尚未确认'} ok={status.ue4ss.load_verified} />
              <InfoRow label="首次启动" value={status.needs_first_start ? '需要启动生成配置' : '已就绪'} ok={!status.needs_first_start} />
              {status.ue4ss.message && (
                <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-[11px] font-semibold text-slate-700">
                  {status.ue4ss.message}
                  {status.ue4ss.error && <p className="mt-1 text-rose-600">{status.ue4ss.error}</p>}
                </div>
              )}
              {status.warnings.length > 0 && (
                <div className="rounded-2xl border border-amber-100 bg-amber-50 p-3 text-[11px] font-semibold text-amber-800">
                  {status.warnings.join(' / ')}
                </div>
              )}
              <div className="rounded-2xl border border-slate-200 bg-slate-50 p-3">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <p className="flex items-center gap-1.5 text-[11px] font-bold text-slate-700"><TerminalSquare size={13} />实际 RCON 命令</p>
                    <p className="mt-1 text-[10px] font-semibold leading-4 text-slate-500">手动执行 /getrconcmds，原样检查当前游戏进程实际暴露的命令；不会改变 GM 操作。</p>
                  </div>
                  <button type="button" onClick={() => void inspectRuntimeCommands()} disabled={runtimeCommandsLoading || !status.load_verified} className="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-2.5 py-2 text-[10px] font-bold text-slate-600 disabled:opacity-40">
                    <RefreshCw size={12} className={runtimeCommandsLoading ? 'animate-spin' : ''} />
                    {runtimeCommandsLoading ? '检查中' : '检查'}
                  </button>
                </div>
                {runtimeCommandsError && <p role="alert" className="mt-3 break-words rounded-lg border border-rose-200 bg-rose-50 p-2 text-[10px] font-semibold text-rose-700">{runtimeCommandsError}</p>}
                {runtimeCommands && (
                  <div className="mt-3">
                    <p className="mb-1 font-mono text-[9px] font-semibold text-slate-400">已执行：{runtimeCommands.command || '/getrconcmds'}</p>
                    <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-slate-950 p-3 font-mono text-[10px] leading-5 text-slate-100">{runtimeCommands.output || '命令执行成功，但没有返回内容。'}</pre>
                  </div>
                )}
              </div>
            </div>
          )}
        </section>

        <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
          <h3 className="mb-4 flex items-center gap-2 text-[15px] font-bold text-slate-800">
            <Sparkles size={18} className="text-sky-500" />
            GitHub 最新版
          </h3>
          <div className="rounded-2xl border border-slate-100 bg-slate-50 p-4">
            <p className="text-[11px] font-semibold text-slate-400">Latest</p>
            <p className="mt-1 text-lg font-bold text-slate-800">{latest?.tag_name || '未获取到 Release'}</p>
            <p className="mt-1 text-[11px] font-medium text-slate-400">
              {latest?.published_at || 'GitHub API 不可达时显示为空状态'}
            </p>
            <p className="mt-3 text-[11px] font-semibold text-slate-500">
              {(latest?.assets || []).map((asset) => asset.name).join(' / ') || '暂无资产信息'}
            </p>
            <p className="mt-3 text-[10px] font-medium leading-4 text-slate-500">安装和更新都会读取官方最新稳定版，并校验 GitHub Release 公布的 SHA-256。</p>
          </div>
          {activeJob && (
            <div className="mt-4 rounded-2xl border border-slate-100 bg-slate-50 p-3">
              <div className="flex items-center justify-between gap-3 text-xs font-bold text-slate-700">
                <span>{activeJob.message || activeJob.type}</span>
                <StatusBadge status={activeJob.status === 'running' ? 'running_job' : activeJob.status} />
              </div>
              <div className="mt-3 h-2 rounded-full bg-white">
                <div className="h-full rounded-full bg-sky-500" style={{ width: `${activeJob.progress}%` }} />
              </div>
            </div>
          )}
          <div className="mt-4 grid grid-cols-2 gap-2">
            <button type="button" onClick={() => runJob(securityApi.install)} className="rounded-xl bg-sky-500 px-4 py-2 text-xs font-bold text-white hover:bg-sky-600">
              安装
            </button>
            <button type="button" onClick={() => runJob(securityApi.update)} className="rounded-xl border border-slate-200 px-4 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50">
              更新
            </button>
            <button type="button" onClick={rollback} className="col-span-2 flex items-center justify-center gap-2 rounded-xl border border-rose-200 px-4 py-2 text-xs font-bold text-rose-600 hover:bg-rose-50">
              <RotateCcw size={14} />
              回滚最近备份
            </button>
          </div>
          <Link to="/gm" className="mt-3 flex items-center justify-center gap-2 rounded-lg border border-sky-200 px-4 py-2.5 text-xs font-bold text-sky-700 hover:bg-sky-50">
            <Gamepad2 size={14} /> 打开 GM 工具
          </Link>
        </section>

        <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
          <h3 className="mb-4 flex items-center gap-2 text-[15px] font-bold text-slate-800">
            <KeyRound size={18} className="text-indigo-500" />
            REST Token
          </h3>
          <div className="flex flex-col gap-3">
            <input
              type="text"
              value={tokenName}
              onChange={(event) => setTokenName(event.target.value)}
              className="rounded-xl border border-slate-200 p-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
            />
            <button type="button" onClick={createToken} className="rounded-xl bg-slate-900 px-4 py-2 text-xs font-bold text-white hover:bg-slate-800">
              生成 GM Token
            </button>
            {tokenResult && (
              <div className="rounded-2xl border border-emerald-100 bg-emerald-50 p-3">
                <p className="text-[11px] font-bold text-emerald-800">Token 仅显示一次</p>
                <p className="mt-2 break-all font-mono text-[11px] font-semibold text-emerald-700">{tokenResult.token}</p>
              </div>
            )}
          </div>
        </section>
      </div>

      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
        <div className="mb-4 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h3 className="text-[15px] font-bold text-slate-800">Config.json</h3>
            <p className="mt-1 text-xs font-medium text-slate-400">
              如果 PalDefender 目录尚未生成，可先应用 balanced preset，首次启动后再 reload。
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <button type="button" onClick={applyPreset} className="flex items-center gap-2 rounded-xl border border-slate-200 px-4 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50">
              <Wand2 size={14} />
              Balanced
            </button>
            <button type="button" onClick={saveConfig} className="flex items-center gap-2 rounded-xl bg-sky-500 px-4 py-2 text-xs font-bold text-white hover:bg-sky-600">
              <Save size={14} />
              保存配置
            </button>
            <button type="button" onClick={reloadConfig} className="flex items-center gap-2 rounded-xl border border-slate-200 px-4 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50">
              <RefreshCw size={14} />
              Reload
            </button>
          </div>
        </div>
        <textarea
          value={configText}
          onChange={(event) => setConfigText(event.target.value)}
          rows={18}
          className="w-full resize-y rounded-2xl border border-slate-200 bg-slate-950 p-4 font-mono text-xs font-semibold leading-relaxed text-emerald-300 focus:border-sky-500 focus:outline-none"
        />
      </section>
    </div>
  );
};

const InfoRow: React.FC<{ label: string; value: string; ok: boolean }> = ({ label, value, ok }) => (
  <div className="flex items-center justify-between rounded-2xl border border-slate-100 bg-slate-50/70 p-3">
    <span className="text-xs font-semibold text-slate-500">{label}</span>
    <StatusBadge status={ok ? 'success' : 'missing'} customText={value} />
  </div>
);
