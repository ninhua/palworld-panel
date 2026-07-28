import React, { useEffect, useMemo, useState } from 'react';
import { Archive, CloudUpload, Download, FolderDown, Link2, RefreshCw, RotateCcw, Save, ShieldCheck, Trash2 } from 'lucide-react';
import { Link } from 'react-router-dom';
import { getErrorMessage } from '../api/client';
import { backupsApi } from '../api/backups';
import { tasksApi } from '../api/tasks';
import type { BackupInfo, BackupVerifyResult, Job, WebDAVConfig } from '../types';
import { DataTable } from '../components/ui/DataTable';
import { StatusBadge } from '../components/ui/StatusBadge';
import { formatBytes } from '../utils/monitor';

export const Backups: React.FC = () => {
  const [backups, setBackups] = useState<BackupInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [activeJob, setActiveJob] = useState<Job | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [verifyResults, setVerifyResults] = useState<Record<string, BackupVerifyResult>>({});
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const [webDAVConfig, setWebDAVConfig] = useState<WebDAVConfig>({
    enabled: false,
    base_url: '',
    username: '',
    remote_path: 'PalPanel',
    upload_after_backup: false,
    password_configured: false,
  });
  const [webDAVPassword, setWebDAVPassword] = useState('');
  const [clearWebDAVPassword, setClearWebDAVPassword] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      const list = await backupsApi.list();
      setBackups(Array.isArray(list) ? list : []);
      try {
        const config = await backupsApi.getWebDAVConfig();
        setWebDAVConfig(config);
      } catch (configError) {
        setMessage(`WebDAV 配置读取失败：${getErrorMessage(configError)}`);
      }
      setError(null);
    } catch (loadError) {
      setBackups([]);
      setError(getErrorMessage(loadError));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const totalSize = useMemo(() => backups.reduce((sum, item) => sum + item.size_bytes, 0), [backups]);

  const createBackup = async () => {
    setPendingAction('create');
    try {
      const job = await backupsApi.create();
      setActiveJob(job);
      const done = await tasksApi.waitForJob(job.id, setActiveJob);
      setMessage(done.status === 'success' ? '备份任务已完成' : done.error || '备份任务失败');
      await load();
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    } finally {
      setPendingAction(null);
    }
  };

  const restoreBackup = async (backup: BackupInfo) => {
    if (!window.confirm(`恢复备份 "${backup.name}"？当前服务器会先停止，并自动创建 pre-restore 备份。`)) return;
    if (!window.confirm('这是高风险操作。请再次确认已经通知在线玩家，并理解恢复会覆盖当前存档。')) return;
    setPendingAction(`restore:${backup.name}`);
    try {
      const job = await backupsApi.restore(backup.name);
      setActiveJob(job);
      const done = await tasksApi.waitForJob(job.id, setActiveJob);
      setMessage(done.status === 'success' ? '备份恢复任务已完成，请核验文件后再启动服务器' : done.error || '备份恢复失败');
      await load();
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    } finally {
      setPendingAction(null);
    }
  };

  const verifyBackup = async (backup: BackupInfo) => {
    setPendingAction(`verify:${backup.name}`);
    try {
      const result = await backupsApi.verify(backup.name);
      setVerifyResults((prev) => ({ ...prev, [backup.name]: result }));
      setMessage(result.valid ? `校验通过：${backup.name}` : `校验失败：${result.errors.join(' / ') || backup.name}`);
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    } finally {
      setPendingAction(null);
    }
  };

  const deleteBackup = async (backup: BackupInfo) => {
    if (!window.confirm(`删除备份 "${backup.name}"？`)) return;
    if (!window.confirm('删除后无法从面板恢复，请再次确认。')) return;
    setPendingAction(`delete:${backup.name}`);
    try {
      await backupsApi.delete(backup.name);
      setVerifyResults((prev) => {
        const next = { ...prev };
        delete next[backup.name];
        return next;
      });
      setMessage(`已删除备份：${backup.name}`);
      await load();
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    } finally {
      setPendingAction(null);
    }
  };

  const webDAVUpdate = () => ({
    enabled: webDAVConfig.enabled,
    base_url: webDAVConfig.base_url,
    username: webDAVConfig.username,
    remote_path: webDAVConfig.remote_path,
    upload_after_backup: webDAVConfig.upload_after_backup,
    password: webDAVPassword || undefined,
    clear_password: clearWebDAVPassword || undefined,
  });

  const saveWebDAVConfig = async () => {
    setPendingAction('webdav-save');
    try {
      const saved = await backupsApi.updateWebDAVConfig(webDAVUpdate());
      setWebDAVConfig(saved);
      setWebDAVPassword('');
      setClearWebDAVPassword(false);
      setMessage('WebDAV 备份策略已保存');
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    } finally {
      setPendingAction(null);
    }
  };

  const testWebDAVConfig = async () => {
    setPendingAction('webdav-test');
    try {
      await backupsApi.testWebDAVConfig(webDAVUpdate());
      setMessage('WebDAV 连接测试成功');
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    } finally {
      setPendingAction(null);
    }
  };

  const uploadBackup = async (backup: BackupInfo) => {
    setPendingAction(`webdav:${backup.name}`);
    try {
      const job = await backupsApi.uploadWebDAV(backup.name);
      setActiveJob(job);
      const done = await tasksApi.waitForJob(job.id, setActiveJob);
      setMessage(done.status === 'success' ? `已上传到 WebDAV：${backup.name}` : done.error || 'WebDAV 上传失败');
    } catch (actionError) {
      setMessage(getErrorMessage(actionError));
    } finally {
      setPendingAction(null);
    }
  };

  const headers = [
    { key: 'name', label: '备份名称' },
    { key: 'size', label: '大小' },
    { key: 'created', label: '创建时间' },
    { key: 'reason', label: '原因' },
    { key: 'status', label: '校验' },
    { key: 'path', label: '路径' },
    { key: 'actions', label: '操作', align: 'center' as const },
  ];

  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-6 p-4 sm:p-6 lg:p-8">
      {error && <div className="rounded-2xl border border-rose-100 bg-rose-50 px-5 py-3 text-xs font-semibold text-rose-700">{error}</div>}
      {message && <div className="rounded-2xl border border-sky-100 bg-sky-50 px-5 py-3 text-xs font-semibold text-sky-700">{message}</div>}

      <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-4">
        <SummaryCard label="备份数量" value={`${backups.length} 个`} />
        <SummaryCard label="总容量" value={formatBytes(totalSize)} />
        <SummaryCard label="存储位置" value="data/backups" />
        <SummaryCard label="WebDAV" value={webDAVConfig.enabled ? '已启用' : '未启用'} />
      </div>

      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <h3 className="flex items-center gap-2 text-[15px] font-bold text-slate-800">
              <CloudUpload size={18} className="text-sky-600" />
              WebDAV 自动归档
            </h3>
            <p className="mt-1 max-w-2xl text-xs font-medium leading-5 text-slate-500">
              备份完成后可自动上传到 NAS 或云盘。地址支持 HTTP 和 HTTPS；使用 HTTP 时凭据与备份内容不会被加密，密码只保存在面板数据目录且不会由 API 回传。
            </p>
          </div>
          <Link to="/tasks?tab=schedules" className="inline-flex shrink-0 items-center gap-2 rounded-xl border border-sky-200 bg-sky-50 px-4 py-2 text-xs font-bold text-sky-700 hover:bg-sky-100">
            <Link2 size={14} />
            设置定时备份与重启
          </Link>
        </div>

        <div className="mt-5 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
          <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500 xl:col-span-2">
            WebDAV 地址
            <input
              type="url"
              value={webDAVConfig.base_url}
              onChange={(event) => setWebDAVConfig((current) => ({ ...current, base_url: event.target.value }))}
              placeholder="http://nas.local/dav 或 https://dav.example.com/dav"
              className="rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-xs font-semibold text-slate-700 outline-none focus:border-sky-500"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
            用户名
            <input
              value={webDAVConfig.username}
              onChange={(event) => setWebDAVConfig((current) => ({ ...current, username: event.target.value }))}
              className="rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-xs font-semibold text-slate-700 outline-none focus:border-sky-500"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500">
            远程目录
            <input
              value={webDAVConfig.remote_path}
              onChange={(event) => setWebDAVConfig((current) => ({ ...current, remote_path: event.target.value }))}
              placeholder="PalPanel/server-01"
              className="rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-xs font-semibold text-slate-700 outline-none focus:border-sky-500"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs font-semibold text-slate-500 xl:col-span-2">
            密码或应用专用密码
            <input
              type="password"
              value={webDAVPassword}
              onChange={(event) => setWebDAVPassword(event.target.value)}
              placeholder={webDAVConfig.password_configured ? '已保存；留空表示保持不变' : '输入 WebDAV 密码'}
              className="rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-xs font-semibold text-slate-700 outline-none focus:border-sky-500"
            />
          </label>
          <div className="grid gap-2 rounded-2xl border border-slate-100 bg-slate-50/70 p-3 md:col-span-2 xl:col-span-2">
            <label className="flex items-center gap-2 text-xs font-semibold text-slate-600">
              <input type="checkbox" checked={webDAVConfig.enabled} onChange={(event) => setWebDAVConfig((current) => ({ ...current, enabled: event.target.checked }))} />
              启用 WebDAV 上传
            </label>
            <label className="flex items-center gap-2 text-xs font-semibold text-slate-600">
              <input type="checkbox" checked={webDAVConfig.upload_after_backup} onChange={(event) => setWebDAVConfig((current) => ({ ...current, upload_after_backup: event.target.checked }))} />
              每次备份成功后自动上传
            </label>
            <label className="flex items-center gap-2 text-xs font-semibold text-slate-600">
              <input type="checkbox" checked={clearWebDAVPassword} onChange={(event) => setClearWebDAVPassword(event.target.checked)} />
              删除已保存的密码
            </label>
          </div>
        </div>

        <div className="mt-4 flex flex-wrap gap-2">
          <button type="button" onClick={testWebDAVConfig} disabled={Boolean(pendingAction)} className="inline-flex items-center gap-2 rounded-xl border border-sky-200 bg-sky-50 px-4 py-2.5 text-xs font-bold text-sky-700 hover:bg-sky-100 disabled:opacity-50">
            <Link2 size={14} />
            {pendingAction === 'webdav-test' ? '测试中' : '连接测试'}
          </button>
          <button type="button" onClick={saveWebDAVConfig} disabled={Boolean(pendingAction)} className="inline-flex items-center gap-2 rounded-xl bg-sky-600 px-4 py-2.5 text-xs font-bold text-white hover:bg-sky-700 disabled:opacity-50">
            <Save size={14} />
            {pendingAction === 'webdav-save' ? '保存中' : '保存策略'}
          </button>
        </div>
      </section>

      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h3 className="flex items-center gap-2 text-[15px] font-bold text-slate-800">
              <Archive size={18} className="text-sky-500" />
              备份管理
            </h3>
            <p className="mt-1 text-xs font-medium text-slate-400">
              新备份会写入 manifest 和 sha256；旧备份仍可做 zip 结构与路径安全校验。
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={load}
              className="flex items-center gap-2 rounded-xl border border-slate-200 px-4 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50"
            >
              <RefreshCw size={14} />
              刷新
            </button>
            <button
              type="button"
              onClick={createBackup}
              disabled={pendingAction === 'create'}
              className="flex items-center gap-2 rounded-xl bg-sky-500 px-4 py-2 text-xs font-bold text-white hover:bg-sky-600 disabled:opacity-50"
            >
              <FolderDown size={14} />
              {pendingAction === 'create' ? '提交中' : '立即备份'}
            </button>
          </div>
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
      </section>

      <section className="rounded-3xl border border-slate-100 bg-white p-4 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)] sm:p-6">
        {loading ? (
          <div className="py-12 text-center text-xs font-semibold text-slate-400">正在读取备份列表...</div>
        ) : (
          <DataTable
            title="备份文件"
            headers={headers}
            data={backups}
            emptyText={error ? '后端不可用或接口未实现' : '暂无备份'}
            renderCard={(backup) => (
              <BackupCard
                key={backup.name}
                backup={backup}
                pendingAction={pendingAction}
                verifyResult={verifyResults[backup.name]}
                onVerify={() => verifyBackup(backup)}
                downloadUrl={backupsApi.downloadUrl(backup.name)}
                onUpload={() => uploadBackup(backup)}
                onRestore={() => restoreBackup(backup)}
                onDelete={() => deleteBackup(backup)}
                webDAVEnabled={webDAVConfig.enabled}
              />
            )}
            renderRow={(backup) => (
              <tr key={backup.name} className="hover:bg-slate-50/50">
                <td className="px-6 py-4 text-xs font-bold text-slate-700">{backup.name}</td>
                <td className="px-6 py-4 text-xs font-semibold text-slate-600">{formatBytes(backup.size_bytes)}</td>
                <td className="px-6 py-4 text-xs font-medium text-slate-400">{backup.created_at}</td>
                <td className="px-6 py-4 text-xs font-semibold text-slate-500">{backup.reason || 'manual'}</td>
                <td className="px-6 py-4">
                  <VerifyBadge result={verifyResults[backup.name]} fallback={backup.status || '未校验'} />
                </td>
                <td className="max-w-[320px] truncate px-6 py-4 font-mono text-[10px] text-slate-400">{backup.path}</td>
                <td className="px-6 py-4 text-center">
                  <BackupActions
                    name={backup.name}
                    pendingAction={pendingAction}
                    onVerify={() => verifyBackup(backup)}
                    downloadUrl={backupsApi.downloadUrl(backup.name)}
                    onUpload={() => uploadBackup(backup)}
                    onRestore={() => restoreBackup(backup)}
                    onDelete={() => deleteBackup(backup)}
                    webDAVEnabled={webDAVConfig.enabled}
                  />
                </td>
              </tr>
            )}
          />
        )}
      </section>
    </div>
  );
};

const VerifyBadge: React.FC<{ result?: BackupVerifyResult; fallback: string }> = ({ result, fallback }) => {
  if (!result) return <StatusBadge status="waiting" customText={fallback} />;
  if (result.valid) {
    return <StatusBadge status="success" customText={result.format === 'manifest_v1' ? '有效' : '旧格式有效'} />;
  }
  return <StatusBadge status="failed" customText="损坏" />;
};

const BackupActions: React.FC<{
  name: string;
  pendingAction: string | null;
  onVerify: () => void;
  downloadUrl: string;
  onUpload: () => void;
  onRestore: () => void;
  onDelete: () => void;
  webDAVEnabled: boolean;
}> = ({ name, pendingAction, onVerify, downloadUrl, onUpload, onRestore, onDelete, webDAVEnabled }) => {
  const busy = Boolean(pendingAction);
  const current = (prefix: string) => pendingAction === `${prefix}:${name}`;
  return (
    <div className="flex flex-wrap justify-center gap-2">
      <button type="button" title="校验" onClick={onVerify} disabled={busy} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50 disabled:opacity-40">
        <ShieldCheck size={14} />
      </button>
      <a href={downloadUrl} download={name} title="下载" aria-disabled={busy} onClick={(event) => { if (busy) event.preventDefault(); }} className={`rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50 ${busy ? 'pointer-events-none opacity-40' : ''}`}>
        <Download size={14} />
      </a>
      <button type="button" title="上传到 WebDAV" onClick={onUpload} disabled={busy || !webDAVEnabled} className="rounded-lg border border-sky-200 p-2 text-sky-600 hover:bg-sky-50 disabled:opacity-40">
        {current('webdav') ? <RefreshCw size={14} className="animate-spin" /> : <CloudUpload size={14} />}
      </button>
      <button type="button" title="恢复" onClick={onRestore} disabled={busy} className="rounded-lg border border-amber-200 p-2 text-amber-600 hover:bg-amber-50 disabled:opacity-40">
        <RotateCcw size={14} />
      </button>
      <button type="button" title="删除" onClick={onDelete} disabled={busy} className="rounded-lg border border-rose-200 p-2 text-rose-500 hover:bg-rose-50 disabled:opacity-40">
        {current('delete') ? <RefreshCw size={14} className="animate-spin" /> : <Trash2 size={14} />}
      </button>
    </div>
  );
};

const BackupCard: React.FC<{
  backup: BackupInfo;
  verifyResult?: BackupVerifyResult;
  pendingAction: string | null;
  onVerify: () => void;
  downloadUrl: string;
  onUpload: () => void;
  onRestore: () => void;
  onDelete: () => void;
  webDAVEnabled: boolean;
}> = ({ backup, verifyResult, pendingAction, onVerify, downloadUrl, onUpload, onRestore, onDelete, webDAVEnabled }) => (
  <div className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
    <div className="flex items-start justify-between gap-3">
      <p className="min-w-0 break-all text-sm font-bold text-slate-800">{backup.name}</p>
      <VerifyBadge result={verifyResult} fallback={backup.status || '未校验'} />
    </div>
    <div className="mt-3 grid grid-cols-2 gap-2 text-[11px] font-semibold text-slate-500">
      <span>大小: {formatBytes(backup.size_bytes)}</span>
      <span>原因: {backup.reason || 'manual'}</span>
      <span className="col-span-2">时间: {backup.created_at}</span>
    </div>
    {verifyResult && verifyResult.errors.length > 0 && (
      <p className="mt-3 rounded-xl bg-rose-50 p-2 text-[10px] font-semibold text-rose-700">{verifyResult.errors.join(' / ')}</p>
    )}
    <p className="mt-3 break-all rounded-xl bg-slate-50 p-2 font-mono text-[10px] text-slate-400">{backup.path}</p>
    <div className="mt-3">
      <BackupActions
        name={backup.name}
        pendingAction={pendingAction}
        onVerify={onVerify}
        downloadUrl={downloadUrl}
        onUpload={onUpload}
        onRestore={onRestore}
        onDelete={onDelete}
        webDAVEnabled={webDAVEnabled}
      />
    </div>
  </div>
);

const SummaryCard: React.FC<{ label: string; value: string }> = ({ label, value }) => (
  <div className="rounded-2xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
    <p className="text-[11px] font-semibold text-slate-400">{label}</p>
    <p className="mt-1 text-2xl font-bold text-slate-800">{value}</p>
  </div>
);
