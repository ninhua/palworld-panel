import React, { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArchiveRestore, ArrowRightLeft, CheckCircle2, Database, FileArchive, Pencil, RefreshCw, Server, Trash2, Upload } from 'lucide-react';
import { getErrorMessage, isTemporaryBackendError } from '../api/client';
import { saveSourcesApi, waitForSaveSourcesBackend, type SaveImportInspection } from '../api/saveSources';
import { tasksApi } from '../api/tasks';
import type { SaveSource } from '../types';

export const SaveSources: React.FC = () => {
  const queryClient = useQueryClient();
  const [file, setFile] = useState<File | null>(null);
  const [name, setName] = useState('');
  const [notice, setNotice] = useState('');
  const [inspection, setInspection] = useState<SaveImportInspection | null>(null);
  const [candidateID, setCandidateID] = useState('');
  const [renamingID, setRenamingID] = useState('');
  const [renameValue, setRenameValue] = useState('');
  const [migrationID, setMigrationID] = useState('');
  const sources = useQuery({ queryKey: ['save-sources'], queryFn: saveSourcesApi.list });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['save-sources'] });
  const importMutation = useMutation({
    mutationFn: async ({ current, selected }: { current: SaveImportInspection; selected: string }) => {
      if (!selected) throw new Error('请选择一个可导入的世界');
      if (current.selected_candidate_id !== selected) {
        await saveSourcesApi.selectImportCandidate(current.id, selected);
      }
      return saveSourcesApi.importInspected(current.id, name);
    },
    onSuccess: () => {
      setFile(null);
      setName('');
      setInspection(null);
      setCandidateID('');
      setNotice('存档已导入，并已移除导入世界根目录中的 WorldOption.sav，避免沿用旧世界覆盖配置。选择“用于分析”只会切换面板解析源，不会切换服务器当前运行世界。');
      void refresh();
    },
    onError: (error) => {
      setInspection(null);
      setCandidateID('');
      setNotice(getErrorMessage(error));
    },
  });
  const inspectMutation = useMutation({
    mutationFn: () => {
      if (!file) throw new Error('请选择包含 Level.sav 的 ZIP 或 TAR 文件');
      return saveSourcesApi.inspectArchive(file, name);
    },
    onSuccess: (next) => {
      setInspection(next);
      setCandidateID(next.selected_candidate_id);
      setNotice('');
      const selectedCandidate = next.candidates.find((candidate) => candidate.id === next.selected_candidate_id);
      if (!next.requires_selection && next.selected_candidate_id && selectedCandidate?.warnings.length === 0) {
        importMutation.mutate({ current: next, selected: next.selected_candidate_id });
      }
    },
    onError: (error) => setNotice(getErrorMessage(error)),
  });
  const action = useMutation({
    mutationFn: async ({ type, id, value }: { type: 'activate' | 'rebuild' | 'remove' | 'rename'; id: string; value?: string }) => {
      if (type === 'activate') return saveSourcesApi.activate(id);
      if (type === 'rebuild') return saveSourcesApi.rebuild(id);
      if (type === 'rename') return saveSourcesApi.rename(id, value || '');
      return saveSourcesApi.remove(id);
    },
    onSuccess: () => { setRenamingID(''); setRenameValue(''); void refresh(); },
    onError: (error) => setNotice(getErrorMessage(error)),
  });

  const migrateHost = async (source: SaveSource) => {
    const steamID = window.prompt(`输入联机主机的 SteamID64\n源存档：${source.name}`, '');
    if (!steamID?.trim()) return;
    setMigrationID(source.id);
    setNotice('正在只读预检主机角色迁移……');
    try {
      const plan = await saveSourcesApi.planHostMigration(source.id, steamID.trim());
      if (!plan.can_execute) {
        setNotice(plan.warnings.join(' ') || `当前迁移策略不可执行：${plan.strategy}`);
        return;
      }
      const confirmed = window.confirm(
        `只读预检通过。\n\n源 UID：${plan.source_uid}\n目标 UID：${plan.target_uid}\n\n将生成迁移存档，并自动停止服务器、切换 DedicatedServerName；若服务器原来运行，将自动重新启动。继续吗？`,
      );
      if (!confirmed) { setNotice('已取消主机角色迁移。'); return; }
      const job = await saveSourcesApi.executeHostMigration(source.id, steamID.trim(), `${source.name}（主机迁移）`);
      setNotice(`迁移任务已提交（${job.id}），页面会持续读取后端任务状态。关闭页面不会中止迁移。`);
      const completed = await tasksApi.waitForJob(job.id, (current) => {
        const progress = Number.isFinite(current.progress) ? ` ${current.progress}%` : '';
        setNotice(`主机迁移${progress}：${current.message || '正在处理'}`);
      });
      if (completed.status !== 'success') {
        throw new Error(completed.error || completed.message || '主机迁移任务未成功完成');
      }
      setNotice(`迁移完成：${source.name}（主机迁移）。已自动切换 GameUserSettings.ini 的 DedicatedServerName；服务器原为运行状态时已重新启动。地图探索 LocalData.sav 不在服务端迁移范围内。`);
      await refresh();
    } catch (error) {
      if (isTemporaryBackendError(error)) {
        setNotice('主机迁移的提交响应被网关中断，任务可能已经创建。正在等待 PalPanel 后端恢复并核对结果……');
        try {
          const recovered = await waitForSaveSourcesBackend();
          if (recovered) {
            queryClient.setQueryData(['save-sources'], recovered);
            const expectedName = `${source.name}（主机迁移）`;
            const migrated = recovered.items.find((item) => item.name === expectedName);
            setNotice(migrated
              ? `迁移完成：${migrated.name}。网关连接曾短暂中断，现已恢复并确认迁移存档存在。`
              : 'PalPanel 后端已经恢复，但无法确认迁移任务是否创建。请先检查后台任务和可用存档，不要立即重复迁移。');
          } else {
            setNotice('PalPanel 后端在 60 秒内仍未恢复，无法确认迁移任务是否创建。请检查服务日志和后台任务，不要立即重复迁移。');
          }
        } catch (recoveryError) {
          setNotice(`无法核对迁移任务状态：${getErrorMessage(recoveryError)}。请检查服务日志、后台任务和可用存档，不要立即重复迁移。`);
        }
      } else {
        setNotice(getErrorMessage(error));
      }
    } finally {
      setMigrationID('');
    }
  };

  const submitRename = (id: string, current: string) => {
    const next = renameValue.trim();
    if (!next || next === current) { setRenamingID(''); return; }
    action.mutate({ type: 'rename', id, value: next });
  };

  const active = sources.data?.items.find((item) => item.active);
  const status = sources.data?.active_status;
  const runtime = sources.data?.runtime_save;
  const analysisDiffersFromRuntime = Boolean(
    active && runtime?.available && active.id !== runtime.source_id,
  );
  return (
    <div className="page-shell">
      <div className="page-titlebar">
        <div><p className="eyebrow">Save workspace</p><h1>存档中心</h1><p>分别显示服务器实际运行世界和 PalPanel 当前分析源；“用于分析”不会切换服务器存档。</p></div>
        <button type="button" className="pp-button" onClick={() => void sources.refetch()}><RefreshCw size={15} />刷新</button>
      </div>

      <div className="content-grid two-column">
        <section className="pp-card">
          <div className="pp-card-head">
            <div><span className="eyebrow">服务器实际运行世界</span><h2>{runtime?.world_id || '尚未检测到世界'}</h2><p>{runtime?.available ? `服务器存档源 · ${runtime.source_name || '当前服务器存档'}` : '未在服务器存档目录中找到可运行的 Level.sav'}</p></div>
            <span className={`state-pill ${runtime?.server_active ? 'ok' : 'warn'}`}>{runtimeStateLabel(runtime?.state, runtime?.server_active)}</span>
          </div>
          <p className="text-sm text-slate-600">此处由 GameUserSettings.ini 的 DedicatedServerName 和服务器存档目录解析，表示 PalServer 实际读取的世界。</p>
        </section>
        <section className="pp-card">
          <div className="pp-card-head">
            <div><span className="eyebrow">PalPanel 当前分析源</span><h2>{active?.name || '尚未选择'}</h2><p>{status?.updated_at ? `分析索引更新于 ${new Date(status.updated_at).toLocaleString('zh-CN')}` : '等待首次分析索引'}</p></div>
            <span className={`state-pill ${status?.state === 'ready' ? 'ok' : 'warn'}`}>{status?.state || 'unknown'}</span>
          </div>
          <div className="grid grid-cols-3 gap-3">
            <Metric label="玩家" value={status?.counts.players || 0} />
            <Metric label="帕鲁" value={status?.counts.pals || 0} />
            <Metric label="基地" value={status?.counts.bases || 0} />
          </div>
        </section>
      </div>

      {analysisDiffersFromRuntime && <div className="pp-notice">服务器实际运行的是 <strong>{runtime?.world_id}</strong>，面板当前分析的是 <strong>{active?.name}</strong>。两者不同是允许的，但“用于分析”不会部署或切换运行世界。</div>}
      {notice && <div className="pp-notice">{notice}</div>}

      <div className="content-grid two-column">
        <section className="pp-card">
          <div className="pp-card-head"><div><h2>可用分析源</h2><p>服务器源跟踪实际运行世界；导入源用于离线查看和差异分析。选择“用于分析”只改变解析对象。</p></div><Database size={18} /></div>
          <div className="source-list">
            {(sources.data?.items || []).map((source) => (
              <article key={source.id} className={`source-row ${source.active ? 'active' : ''}`}>
                <span className="source-icon">{source.kind === 'server' ? <Server size={18} /> : <FileArchive size={18} />}</span>
                {renamingID === source.id ? (
                  <div className="source-copy">
                    <input
                      autoFocus
                      aria-label="存档名称"
                      value={renameValue}
                      maxLength={80}
                      onChange={(event) => setRenameValue(event.target.value)}
                      onKeyDown={(event) => { if (event.key === 'Enter') submitRename(source.id, source.name); if (event.key === 'Escape') setRenamingID(''); }}
                    />
                  </div>
                ) : (
                  <div className="source-copy"><strong>{source.name}</strong><span>{source.kind === 'server' ? `服务器存档源${runtime?.world_id ? ` · 世界 ${runtime.world_id}` : ''}` : '本地归档导入'} · {source.active ? '当前分析源' : '未用于分析'}</span></div>
                )}
                <div className="source-actions">
                  {renamingID === source.id ? <>
                    <button type="button" className="pp-button accent" disabled={action.isPending} onClick={() => submitRename(source.id, source.name)}><CheckCircle2 size={14} />保存</button>
                    <button type="button" className="pp-button" onClick={() => setRenamingID('')}>取消</button>
                  </> : <>
                    <button type="button" className="pp-button" onClick={() => { setRenamingID(source.id); setRenameValue(source.name); }}><Pencil size={14} />重命名</button>
                    {source.kind !== 'server' && <button type="button" className="pp-button" disabled={migrationID === source.id} onClick={() => void migrateHost(source)}>{migrationID === source.id ? <RefreshCw className="animate-spin" size={14} /> : <ArrowRightLeft size={14} />}主机迁移</button>}
                    {!source.active && <button type="button" className="pp-button accent" onClick={() => action.mutate({ type: 'activate', id: source.id })}><CheckCircle2 size={14} />用于分析</button>}
                    {source.active && <button type="button" className="pp-button" onClick={() => action.mutate({ type: 'rebuild', id: source.id })}><RefreshCw size={14} />重建分析索引</button>}
                    {source.kind !== 'server' && !source.active && <button type="button" className="icon-danger" aria-label="删除存档" onClick={() => window.confirm('删除这个导入存档？') && action.mutate({ type: 'remove', id: source.id })}><Trash2 size={15} /></button>}
                  </>}
                </div>
              </article>
            ))}
          </div>
        </section>

        <section className="pp-card upload-card">
          <div className="pp-card-head"><div><h2>导入本地存档</h2><p>上传标准 Steam 或服务端世界 ZIP/TAR。导入后会删除世界根目录中的 WorldOption.sav，避免旧世界选项覆盖当前服务器配置。</p></div><ArchiveRestore size={18} /></div>
          <label className="field-label">显示名称<input value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：单人世界 2026-07-16" /></label>
          {!inspection && <>
            <label className="file-drop"><Upload size={24} /><strong>{file?.name || '选择 ZIP 或 TAR 存档'}</strong><span>支持 .zip、.tar、.tar.gz、.tgz，归档中必须包含 Level.sav</span><input aria-label="存档归档文件" type="file" accept=".zip,.tar,.tar.gz,.tgz,application/zip,application/x-tar,application/gzip" onChange={(event) => { setFile(event.target.files?.[0] || null); setInspection(null); setCandidateID(''); }} /></label>
            <button type="button" disabled={!file || inspectMutation.isPending || importMutation.isPending} className="pp-button accent wide" onClick={() => inspectMutation.mutate()}>{inspectMutation.isPending || importMutation.isPending ? <RefreshCw className="animate-spin" size={15} /> : <Upload size={15} />}检查存档</button>
          </>}
          {inspection && <div className="save-candidate-picker">
            <div className="pp-card-head"><div><h3>{inspection.requires_selection ? '选择要导入的世界' : inspection.selected_candidate_id ? '已识别到一个世界' : '没有可导入的世界'}</h3><p>{inspection.requires_selection ? '检测到多个有效世界，必须明确选择一个。' : inspection.selected_candidate_id ? '检查已完成，可重试导入或重新选择归档。' : '请查看候选错误后重新选择归档。'}</p></div><FileArchive size={17} /></div>
            <div className="source-list">
              {inspection.candidates.map((candidate) => {
                const label = candidate.world_id || candidate.relative_path;
                return <label key={candidate.id} className={`source-row ${candidateID === candidate.id ? 'active' : ''} ${candidate.valid ? '' : 'disabled'}`}>
                  <input type="radio" name="save-world-candidate" aria-label={`世界 ${label}`} value={candidate.id} disabled={!candidate.valid || importMutation.isPending} checked={candidateID === candidate.id} onChange={() => setCandidateID(candidate.id)} />
                  <div className="source-copy">
                    <strong>{label}</strong>
                    <span>{candidate.relative_path} · {candidate.player_count} 名玩家 · {formatBytes(candidate.level_size)}</span>
                    {candidate.warnings.map((warning) => <span key={`warning-${warning}`} className="candidate-warning">警告：{readableCandidateError(warning)}</span>)}
                    {candidate.errors.map((error) => <span key={`error-${error}`} className="candidate-error">{readableCandidateError(error)}</span>)}
                  </div>
                </label>;
              })}
            </div>
            <div className="candidate-actions">
              <button type="button" className="pp-button" disabled={importMutation.isPending} onClick={() => { setInspection(null); setCandidateID(''); }}>重新选择归档</button>
              <button type="button" className="pp-button accent" disabled={!candidateID || importMutation.isPending} onClick={() => importMutation.mutate({ current: inspection, selected: candidateID })}>{importMutation.isPending ? <RefreshCw className="animate-spin" size={15} /> : <Upload size={15} />}导入所选世界</button>
            </div>
          </div>}
        </section>
      </div>
    </div>
  );
};

const runtimeStateLabel = (state?: string, serverActive?: boolean) => {
  if (serverActive) return state === 'starting' || state === 'restarting' ? '启动中' : '运行中';
  switch (state) {
    case 'stopped':
    case 'exited': return '已停止';
    case 'not_found': return '未安装';
    case 'created': return '未启动';
    default: return state || '状态未知';
  }
};

const Metric: React.FC<{ label: string; value: number }> = ({ label, value }) => <div className="metric"><span className="eyebrow">{label}</span><strong>{value}</strong><span>当前索引</span></div>;

const candidateErrorHints: Record<string, string> = {
  parser_incompatible: '存档格式不兼容（可能是 sav-cli 缺少 Oodle 解压，需使用带 cgo 的发布包）',
  level_sav_not_found: '未找到 Level.sav',
  save_path_not_found: '存档路径不存在',
  index_failed: '索引失败，请查看 sav-cli 日志',
};

const readableCandidateError = (error: string): string => {
  for (const [code, hint] of Object.entries(candidateErrorHints)) {
    if (error.includes(code)) return `${error} — ${hint}`;
  }
  return error;
};

const formatBytes = (value: number) => {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
  return `${(value / 1024 / 1024).toFixed(1)} MiB`;
};
