import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { CheckCircle2, ChevronDown, ChevronUp, History, RefreshCw, RotateCcw } from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { settingsApi } from '../api/settings';
import type { PalworldConfigDraft, PalworldConfigRevision, PalworldConfigRevisionDiff } from '../types';

const revisionLabel = (source: string) => source === 'baseline' ? '基线快照' : source === 'apply' ? '配置应用' : source;
const shortSHA = (value: string) => value ? value.slice(0, 12) : '-';

export const ConfigRevisionHistory: React.FC<{
  onDraftCreated?: (draft: PalworldConfigDraft) => void;
  onApplied?: () => void;
  canRestore?: boolean;
}> = ({ onDraftCreated, onApplied, canRestore = false }) => {
  const [items, setItems] = useState<PalworldConfigRevision[]>([]);
  const [retention, setRetention] = useState(50);
  const [selected, setSelected] = useState<string>('');
  const [diff, setDiff] = useState<PalworldConfigRevisionDiff | null>(null);
  const [draft, setDraft] = useState<PalworldConfigDraft | null>(null);
  const [draftRevisionID, setDraftRevisionID] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const diffRequest = useRef(0);
  const selectedRef = useRef(selected);

  useEffect(() => { selectedRef.current = selected; }, [selected]);

  const selectedRevision = useMemo(() => items.find((item) => item.id === selected), [items, selected]);

  const load = useCallback(async () => {
    setBusy(true);
    setMessage('');
    try {
      const result = await settingsApi.listRevisions();
      setItems(result.items);
      setRetention(result.retention);
      const currentSelected = selectedRef.current;
      if (currentSelected && !result.items.some((item) => item.id === currentSelected)) {
        setSelected('');
        setDiff(null);
      }
    } catch (error) {
      setMessage(getErrorMessage(error, '读取配置历史失败'));
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  const toggleDiff = async (revision: PalworldConfigRevision) => {
    const request = ++diffRequest.current;
    if (selected === revision.id) {
      setSelected('');
      setDiff(null);
      return;
    }
    setSelected(revision.id);
    setDiff(null);
    setDraft(null);
    setDraftRevisionID('');
    setMessage('');
    try {
      const result = await settingsApi.getRevisionDiff(revision.id);
      if (diffRequest.current === request) setDiff(result);
    } catch (error) {
      if (diffRequest.current === request) setMessage(getErrorMessage(error, '比较配置修订失败'));
    }
  };

  const createRestoreDraft = async () => {
    if (!selectedRevision || selectedRevision.current) return;
    setBusy(true);
    setMessage('');
    try {
      const restored = await settingsApi.restoreRevision(selectedRevision.id);
      if (!restored.draft) throw new Error('后端未返回回滚草稿');
      setDraft(restored.draft);
      setDraftRevisionID(selectedRevision.id);
      onDraftCreated?.(restored.draft);
      setMessage('回滚草稿已生成。应用前仍可检查上方字段差异。');
    } catch (error) {
      setMessage(getErrorMessage(error, '生成回滚草稿失败'));
    } finally {
      setBusy(false);
    }
  };

  const applyRestoreDraft = async () => {
    if (!draft) return;
    setBusy(true);
    setMessage('');
    try {
      const job = await settingsApi.applySettings(draft.id);
      setDraft({ ...draft, status: 'applying', applied_job_id: job.id });
      onDraftCreated?.({ ...draft, status: 'applying', applied_job_id: job.id });
      onApplied?.();
      setMessage(`回滚任务已提交：${job.id}`);
    } catch (error) {
      setMessage(getErrorMessage(error, '提交回滚任务失败'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="rounded-2xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)] sm:p-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex items-start gap-3">
          <div className="rounded-xl bg-indigo-50 p-2.5 text-indigo-600"><History size={19} /></div>
          <div>
            <h2 className="text-sm font-bold text-slate-800">配置修订历史</h2>
            <p className="mt-1 text-xs font-medium leading-relaxed text-slate-400">
              保留最近 {retention} 次私密快照。密码不会显示；恢复操作先生成草稿，再复用现有健康检查和失败自动恢复流程。
            </p>
          </div>
        </div>
        <button type="button" onClick={() => void load()} disabled={busy} className="inline-flex items-center justify-center gap-2 rounded-lg border border-slate-200 px-3 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:opacity-40">
          <RefreshCw size={14} className={busy ? 'animate-spin' : ''} />刷新
        </button>
      </div>

      {message && <div className="mt-4 rounded-xl bg-slate-50 px-3 py-2 text-[11px] font-semibold text-slate-600">{message}</div>}

      <div className="mt-5 grid gap-3">
        {items.map((revision) => (
          <div key={revision.id} className={`rounded-xl border ${revision.current ? 'border-emerald-200 bg-emerald-50/50' : 'border-slate-100 bg-slate-50/50'}`}>
            <button type="button" onClick={() => void toggleDiff(revision)} className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-xs font-bold text-slate-700">{revisionLabel(revision.source)}</span>
                  {revision.current && <span className="rounded-md bg-emerald-100 px-2 py-0.5 text-[9px] font-bold text-emerald-700">当前</span>}
                  <span className="font-mono text-[9px] font-semibold text-slate-400">{shortSHA(revision.revision_sha256)}</span>
                </div>
                <p className="mt-1 text-[10px] font-medium text-slate-400">
                  {revision.created_at ? new Date(revision.created_at).toLocaleString() : '-'}
                  {revision.changed_fields.length > 0 ? ` · ${revision.changed_fields.length} 个字段` : ''}
                </p>
              </div>
              {selected === revision.id ? <ChevronUp size={15} className="text-slate-400" /> : <ChevronDown size={15} className="text-slate-400" />}
            </button>

            {selected === revision.id && (
              <div className="border-t border-slate-100 px-4 py-4">
                {!diff ? (
                  <div className="flex items-center gap-2 text-xs font-semibold text-slate-400"><RefreshCw size={13} className="animate-spin" />正在比较...</div>
                ) : diff.changes.length === 0 ? (
                  <div className="flex items-center gap-2 text-xs font-semibold text-emerald-700"><CheckCircle2 size={14} />该修订与当前配置一致。</div>
                ) : (
                  <div className="grid gap-2">
                    {diff.changes.map((change) => (
                      <div key={change.field} className="grid gap-1 rounded-lg bg-white px-3 py-2 text-[10px] font-semibold sm:grid-cols-[minmax(140px,0.8fr)_1fr_1fr] sm:items-center">
                        <span className="font-mono text-slate-600">{change.field}</span>
                        <span className="break-all text-indigo-700">历史：{change.secret ? (change.revision_configured ? '已配置' : '未配置') : (change.revision_value || '空')}</span>
                        <span className="break-all text-slate-500">当前：{change.secret ? (change.current_configured ? '已配置' : '未配置') : (change.current_value || '空')}</span>
                      </div>
                    ))}
                  </div>
                )}
                {!revision.current && diff && diff.changes.length > 0 && canRestore && (
                  <div className="mt-4 flex flex-wrap justify-end gap-2">
                    <button type="button" onClick={() => void createRestoreDraft()} disabled={busy} className="inline-flex items-center gap-2 rounded-lg bg-indigo-600 px-4 py-2 text-xs font-bold text-white hover:bg-indigo-700 disabled:opacity-40">
                      <RotateCcw size={14} />生成回滚草稿
                    </button>
                    {draft && draftRevisionID === revision.id && draft.status === 'draft' && (
                      <button type="button" onClick={() => void applyRestoreDraft()} disabled={busy} className="inline-flex items-center gap-2 rounded-lg bg-emerald-600 px-4 py-2 text-xs font-bold text-white hover:bg-emerald-700 disabled:opacity-40">
                        <CheckCircle2 size={14} />应用回滚草稿
                      </button>
                    )}
                  </div>
                )}
              </div>
            )}
          </div>
        ))}
        {items.length === 0 && !busy && <div className="rounded-xl border border-dashed border-slate-200 px-4 py-8 text-center text-xs font-semibold text-slate-400">尚无配置修订</div>}
      </div>
    </section>
  );
};
