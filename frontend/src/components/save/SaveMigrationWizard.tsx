import React, { useMemo, useState } from 'react';
import { AlertTriangle, ArrowRightLeft, CheckCircle2, LoaderCircle, Play, ShieldCheck, Users, X } from 'lucide-react';
import { getErrorMessage } from '../../api/client';
import { saveSourcesApi, type SaveMigrationPlayer, type SaveMigrationPreview } from '../../api/saveSources';
import type { SaveSource } from '../../types';

interface MappingDraft {
  player: SaveMigrationPlayer;
  steamID: string;
}

export const SaveMigrationWizard: React.FC<{ sources: SaveSource[]; activeSourceID?: string; onClose: () => void }> = ({ sources, activeSourceID, onClose }) => {
  const migrationSources = sources.filter((source) => source.kind === 'import' || source.kind === 'server');
  const [sourceID, setSourceID] = useState(activeSourceID || migrationSources[0]?.id || '');
  const [players, setPlayers] = useState<SaveMigrationPlayer[]>([]);
  const [fingerprint, setFingerprint] = useState('');
  const [selected, setSelected] = useState<Record<string, MappingDraft>>({});
  const [preview, setPreview] = useState<SaveMigrationPreview | null>(null);
  const [manualMode, setManualMode] = useState<'steam' | 'nosteam' | ''>('');
  const [riskConfirmation, setRiskConfirmation] = useState('');
  const [pending, setPending] = useState<'players' | 'preview' | 'start' | ''>('');
  const [error, setError] = useState('');
  const [jobID, setJobID] = useState('');

  const drafts = useMemo(() => Object.values(selected), [selected]);
  const effectiveMode = preview?.target_mode === 'steam' || preview?.target_mode === 'nosteam' ? preview.target_mode : manualMode;
  const expectedRisk = effectiveMode === 'steam' ? 'USE STEAM UID' : 'USE NOSTEAM UID';
  const manualConfirmed = !preview?.requires_manual_confirmation || (Boolean(effectiveMode) && riskConfirmation === expectedRisk);
  const readyToStart = Boolean(preview?.ready && effectiveMode && manualConfirmed && preview.conflicts.length === 0 && !pending && !jobID);

  const loadPlayers = async () => {
    setPending('players'); setError(''); setPreview(null); setSelected({}); setJobID('');
    try {
      const response = await saveSourcesApi.migrationPlayers(sourceID);
      setPlayers(response.players); setFingerprint(response.source_fingerprint);
    } catch (nextError) { setError(getErrorMessage(nextError)); }
    finally { setPending(''); }
  };

  const togglePlayer = (player: SaveMigrationPlayer) => {
    setPreview(null); setJobID('');
    setSelected((current) => {
      const next = { ...current };
      if (next[player.player_uid]) delete next[player.player_uid];
      else next[player.player_uid] = { player, steamID: player.steam_id || '' };
      return next;
    });
  };

  const migrationRequest = (targetMode: 'auto' | 'steam' | 'nosteam') => ({
    source_id: sourceID, target_mode: targetMode, expected_fingerprint: fingerprint,
    mappings: drafts.map((draft) => ({ source_uid: draft.player.player_uid, steam_id: draft.steamID.trim() })),
  });

  const runPreview = async () => {
    setPending('preview'); setError(''); setJobID(''); setManualMode(''); setRiskConfirmation('');
    try { setPreview(await saveSourcesApi.previewMigration(migrationRequest('auto'))); }
    catch (nextError) { setError(getErrorMessage(nextError)); }
    finally { setPending(''); }
  };

  const startMigration = async () => {
    if (!readyToStart || !effectiveMode) return;
    setPending('start'); setError('');
    try {
      const job = await saveSourcesApi.startMigration({
        ...migrationRequest(effectiveMode), confirmation: 'MIGRATE PLAYERS',
        ...(preview?.requires_manual_confirmation ? { manual_mode_confirmation: riskConfirmation } : {}),
      });
      setJobID(job.id);
    } catch (nextError) { setError(getErrorMessage(nextError)); }
    finally { setPending(''); }
  };

  return (
    <section className="pp-card border-sky-200" aria-label="玩家迁移向导">
      <div className="pp-card-head">
        <div><p className="eyebrow">Player migration</p><h2>旧存档玩家迁移</h2><p>离线计算目标 UID，并通过停服、完整备份、验证、原子切换和失败回滚完成迁移。</p></div>
        <button type="button" className="icon-button" aria-label="关闭迁移向导" onClick={onClose}><X size={16} /></button>
      </div>

      <div className="mt-4 grid gap-3 lg:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
        <div className="space-y-3">
          <label className="field-label">1. 旧存档源
            <select value={sourceID} onChange={(event) => { setSourceID(event.target.value); setPlayers([]); setSelected({}); setPreview(null); }}>
              {migrationSources.map((source) => <option key={source.id} value={source.id}>{source.name} · {source.kind === 'server' ? '当前服务器' : '导入存档'}</option>)}
            </select>
          </label>
          <button type="button" className="pp-button wide" disabled={!sourceID || Boolean(pending)} onClick={() => void loadPlayers()}>
            {pending === 'players' ? <LoaderCircle size={15} className="animate-spin" /> : <Users size={15} />}载入旧玩家
          </button>
          <div className="rounded-lg border border-slate-200 bg-slate-50 px-3 py-2.5 text-[11px] font-semibold leading-5 text-slate-600">
            <strong className="block text-slate-800">迁移会自动停服并重启</strong>
            玩家无需先进入目标服务器。成功后仍需安排一名玩家首登验收，确认角色、背包、帕鲁、公会和基地归属。
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between gap-3"><div><h3 className="text-sm font-black text-slate-800">2. 选择旧玩家并填写新 SteamID64</h3><p className="mt-1 text-[11px] font-semibold text-slate-500">可一次迁移多人；每个目标 SteamID 必须唯一。</p></div><span className="state-pill">已选 {drafts.length}</span></div>
          <div className="mt-3 max-h-64 space-y-2 overflow-auto pr-1">
            {players.length === 0 && <div className="rounded-lg border border-dashed border-slate-200 px-4 py-8 text-center text-xs font-semibold text-slate-400">先载入一个存档源的玩家索引</div>}
            {players.map((player) => {
              const draft = selected[player.player_uid];
              return <div key={player.player_uid} className={`rounded-lg border p-3 ${draft ? 'border-sky-300 bg-sky-50/70' : 'border-slate-200 bg-white'}`}>
                <label className="flex cursor-pointer items-center gap-3">
                  <input type="checkbox" aria-label={`选择旧玩家 ${player.nickname}`} checked={Boolean(draft)} onChange={() => togglePlayer(player)} />
                  <span className="min-w-0 flex-1"><strong className="block truncate text-xs text-slate-800">{player.nickname}</strong><span className="block truncate font-mono text-[10px] text-slate-400">{player.player_uid}</span></span>
                  <span className="text-[10px] font-bold text-slate-500">Lv.{player.level}</span>
                </label>
                {draft && <label className="mt-3 block text-[11px] font-bold text-slate-600">{player.nickname} 的新 SteamID64
                  <input aria-label={`${player.nickname}的新 SteamID64`} inputMode="numeric" maxLength={17} value={draft.steamID} onChange={(event) => { const value = event.target.value.replace(/\D/g, ''); setSelected((current) => ({ ...current, [player.player_uid]: { ...current[player.player_uid], steamID: value } })); setPreview(null); }} className="mt-1 w-full rounded-lg border border-slate-200 bg-white px-3 py-2 font-mono text-xs" placeholder="7656119xxxxxxxxxx" />
                </label>}
              </div>;
            })}
          </div>
        </div>
      </div>

      <div className="mt-4 border-t border-slate-100 pt-4">
        <div className="flex flex-wrap items-center justify-between gap-3"><div><h3 className="text-sm font-black text-slate-800">3. 模式、冲突与备份预检</h3><p className="mt-1 text-[11px] font-semibold text-slate-500">目标服务器模式能被现有身份精确证明时自动选择，否则必须人工确认。</p></div><button type="button" className="pp-button accent" disabled={drafts.length === 0 || drafts.some((draft) => !/^\d{17}$/.test(draft.steamID)) || Boolean(pending)} onClick={() => void runPreview()}>{pending === 'preview' ? <LoaderCircle size={15} className="animate-spin" /> : <ShieldCheck size={15} />}运行迁移预检</button></div>

        {preview && <div className="mt-3 grid gap-3 lg:grid-cols-2">
          <div className={`rounded-lg border p-3 ${preview.requires_manual_confirmation ? 'border-amber-200 bg-amber-50' : 'border-emerald-200 bg-emerald-50'}`}>
            <strong className="flex items-center gap-2 text-xs text-slate-800">{preview.requires_manual_confirmation ? <AlertTriangle size={15} className="text-amber-700" /> : <CheckCircle2 size={15} className="text-emerald-700" />}{preview.requires_manual_confirmation ? '无法自动证明目标 UID 模式' : `已确认 ${preview.target_mode === 'steam' ? 'Steam' : 'NoSteam'} UID 模式`}</strong>
            <p className="mt-2 text-[11px] font-semibold leading-5 text-slate-600">{preview.requires_manual_confirmation ? '当前服务器索引没有足够的 SteamID/PlayerUID 对照。请根据实际认证运行方式手动选择，并输入固定确认短语。' : `服务器索引 ${preview.mode_matched}/${preview.mode_total} 条身份全部匹配。`}</p>
            {preview.requires_manual_confirmation && <div className="mt-3 space-y-2">
              <label className="flex items-center gap-2 text-xs font-bold text-slate-700"><input type="radio" aria-label="手动选择 Steam UID" checked={manualMode === 'steam'} onChange={() => { setManualMode('steam'); setRiskConfirmation(''); }} />Steam UID</label>
              <label className="flex items-center gap-2 text-xs font-bold text-slate-700"><input type="radio" aria-label="手动选择 NoSteam UID" checked={manualMode === 'nosteam'} onChange={() => { setManualMode('nosteam'); setRiskConfirmation(''); }} />NoSteam UID</label>
              <label className="block text-[11px] font-bold text-amber-900">高风险确认
                <input aria-label="高风险确认" value={riskConfirmation} onChange={(event) => setRiskConfirmation(event.target.value)} className="mt-1 w-full rounded-lg border border-amber-300 bg-white px-3 py-2 font-mono text-xs" placeholder={effectiveMode ? expectedRisk : '先选择 UID 模式'} />
              </label>
            </div>}
          </div>
          <div className="rounded-lg border border-slate-200 bg-slate-50 p-3">
            <strong className="text-xs text-slate-800">映射预览</strong>
            <div className="mt-2 space-y-2">{preview.mappings.map((mapping) => <div key={mapping.source_uid} className="text-[10px] font-semibold text-slate-600"><span className="block text-xs font-bold text-slate-800">{mapping.nickname} · Lv.{mapping.level}</span><span className="block font-mono">{mapping.source_uid}</span><span className="block font-mono text-sky-700">→ {effectiveMode === 'steam' ? mapping.steam_uid : effectiveMode === 'nosteam' ? mapping.nosteam_uid : '请选择目标模式'}</span></div>)}</div>
            {preview.conflicts.map((conflict) => <p key={conflict} className="mt-2 text-[11px] font-bold text-rose-700">{conflict}</p>)}
          </div>
        </div>}
      </div>

      {error && <div role="alert" className="pp-notice mt-3">{error}</div>}
      {jobID && <div role="status" className="mt-3 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-3 text-xs font-bold text-emerald-800">迁移任务已启动：{jobID}。可在任务中心查看停服、备份、转换、验证、切换与重启进度。</div>}
      <div className="mt-4 flex flex-wrap justify-end gap-2">
        <button type="button" className="pp-button" onClick={onClose}>稍后处理</button>
        <button type="button" className="pp-button accent" disabled={!readyToStart} onClick={() => void startMigration()}>{pending === 'start' ? <LoaderCircle size={15} className="animate-spin" /> : <Play size={15} />}开始自动迁移</button>
      </div>
    </section>
  );
};

export const MigrationWizardButton: React.FC<{ onClick: () => void }> = ({ onClick }) => <button type="button" className="pp-button accent" onClick={onClick}><ArrowRightLeft size={15} />玩家迁移向导</button>;
