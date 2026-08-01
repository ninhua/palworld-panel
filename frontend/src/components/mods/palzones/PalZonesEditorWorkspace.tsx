import React, { useEffect, useMemo, useRef, useState } from 'react';
import { AlertTriangle, Download, FileJson, Save, Settings2, Trash2, Upload } from 'lucide-react';
import type { ModConfigDocument } from '../../../types';
import { PalZonesMapCanvas } from './PalZonesMapCanvas';
import { PermissionEditor } from './PermissionEditor';
import {
  MAP_DEFINITIONS,
  createPalZonesZone,
  mapForZone,
  parsePalZones,
  serializePalZones,
  validatePalZonesDraft,
  type PalZonesDraft,
  type PalZonesMapID,
  type PalZonesPoint,
} from './model';

interface Props {
  document: ModConfigDocument;
  canWrite: boolean;
  saving: boolean;
  onSave: (content: string) => Promise<void>;
}

const readDraft = (content: string) => {
  const draft = parsePalZones(content);
  return { draft, baseline: serializePalZones(draft) };
};

export const PalZonesEditorWorkspace: React.FC<Props> = ({ document, canWrite, saving, onSave }) => {
  const initial = useMemo(() => readDraft(document.content), [document.content]);
  const [draft, setDraft] = useState<PalZonesDraft>(initial.draft);
  const [baseline, setBaseline] = useState(initial.baseline);
  const [mapID, setMapID] = useState<PalZonesMapID>('world');
  const [selectedZoneID, setSelectedZoneID] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const importRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const next = readDraft(document.content);
    setDraft(next.draft);
    setBaseline(next.baseline);
    setSelectedZoneID(null);
  }, [document.content, document.file.revision]);

  const serialized = useMemo(() => serializePalZones(draft), [draft]);
  const issues = useMemo(() => validatePalZonesDraft(draft), [draft]);
  const invalidZoneIDs = useMemo(() => new Set(issues.map((issue) => draft.zones[issue.zoneIndex]?.id).filter(Boolean)), [draft.zones, issues]);
  const selectedZone = draft.zones.find((zone) => zone.id === selectedZoneID);
  const visibleZones = draft.zones.filter((zone) => {
    const zoneMap = mapForZone(zone);
    return (zoneMap === mapID || zoneMap == null) && zone.name.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase());
  });

  const updateZone = (zoneID: string, update: (zone: PalZonesDraft['zones'][number]) => PalZonesDraft['zones'][number]) => {
    setDraft((current) => ({ ...current, zones: current.zones.map((zone) => zone.id === zoneID ? update(zone) : zone) }));
  };

  const createZone = (points: PalZonesPoint[]) => {
    const zone = createPalZonesZone(draft.globalPermissions, points, draft.zones.length);
    setDraft((current) => ({ ...current, zones: [...current.zones, zone] }));
    setSelectedZoneID(zone.id);
    setNotice('区域已创建，请设置名称、等级与权限。');
  };

  const removeZone = (zoneID: string) => {
    setDraft((current) => ({ ...current, zones: current.zones.filter((zone) => zone.id !== zoneID) }));
    setSelectedZoneID(null);
  };

  const save = async () => {
    if (!canWrite || issues.length || serialized === baseline) return;
    setError(null);
    await onSave(serialized);
    setBaseline(serialized);
    setNotice('区域配置已保存，安全重启游戏服务后生效。');
  };

  const exportDraft = () => {
    const url = URL.createObjectURL(new Blob([serialized], { type: 'application/json' }));
    const link = window.document.createElement('a');
    link.href = url;
    link.download = 'zones.json';
    link.click();
    URL.revokeObjectURL(url);
  };

  const importDraft = async (file?: File) => {
    if (!file) return;
    try {
      const next = parsePalZones(await file.text());
      setDraft(next);
      setSelectedZoneID(next.zones[0]?.id ?? null);
      setNotice('已导入 ' + next.zones.length + ' 个区域，请检查警告后再保存。');
      setError(null);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      if (importRef.current) importRef.current.value = '';
    }
  };

  return (
    <div className={'space-y-4'}>
      <div className={'flex flex-wrap items-center justify-between gap-3'}>
        <div>
          <h2 className={'flex items-center gap-2 text-base font-black text-slate-900'}><FileJson size={18} className={'text-sky-600'} />PalZones 区域编辑器</h2>
          <p className={'mt-1 text-xs text-slate-500'}>{draft.zones.length} 个区域 · {issues.length ? issues.length + ' 个问题待修复' : '几何与权限检查通过'}</p>
        </div>
        <div className={'flex flex-wrap gap-2'}>
          <input ref={importRef} type={'file'} accept={'.json,application/json'} className={'hidden'} onChange={(event) => void importDraft(event.target.files?.[0])} />
          <button type={'button'} onClick={() => importRef.current?.click()} className={'inline-flex items-center gap-1.5 border border-slate-200 bg-white px-3 py-2 text-xs font-bold text-slate-700'}><Upload size={14} />导入</button>
          <button type={'button'} onClick={exportDraft} className={'inline-flex items-center gap-1.5 border border-slate-200 bg-white px-3 py-2 text-xs font-bold text-slate-700'}><Download size={14} />导出</button>
          <button type={'button'} disabled={!canWrite || saving || issues.length > 0 || serialized === baseline} onClick={() => void save()} className={'inline-flex items-center gap-1.5 bg-sky-600 px-4 py-2 text-xs font-black text-white disabled:cursor-not-allowed disabled:opacity-40'}><Save size={14} />保存区域配置</button>
        </div>
      </div>

      {error && <div className={'border border-rose-200 bg-rose-50 px-4 py-3 text-xs font-semibold text-rose-700'}>{error}</div>}
      {notice && <div className={'border border-emerald-200 bg-emerald-50 px-4 py-3 text-xs font-semibold text-emerald-700'}>{notice}</div>}
      {issues.length > 0 && <div className={'flex gap-2 border border-amber-200 bg-amber-50 px-4 py-3 text-xs text-amber-800'}><AlertTriangle size={16} className={'shrink-0'} /><div>{issues.slice(0, 4).map((issue) => <p key={issue.path + issue.code}>{issue.path}：{issue.message}</p>)}</div></div>}

      <div className={'flex flex-wrap items-center justify-between gap-3 border-y border-slate-200 py-3'}>
        <div className={'flex items-center border border-slate-200 bg-slate-100 p-1'}>
          {MAP_DEFINITIONS.map((map) => <button key={map.id} type={'button'} onClick={() => setMapID(map.id)} className={'px-3 py-1.5 text-xs font-bold ' + (mapID === map.id ? 'bg-white text-sky-700 shadow-sm' : 'text-slate-500')}>{map.label}</button>)}
        </div>
        <button type={'button'} className={'inline-flex items-center gap-1.5 text-xs font-bold text-slate-600'}><Settings2 size={14} />全局权限</button>
      </div>

      <div className={'grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1fr)_310px]'}>
        <PalZonesMapCanvas
          mapID={mapID}
          zones={visibleZones}
          selectedZoneID={selectedZoneID}
          invalidZoneIDs={invalidZoneIDs}
          onCreateZone={createZone}
          onSelectZone={setSelectedZoneID}
          onMovePoint={(zoneID, pointIndex, point) => updateZone(zoneID, (zone) => ({ ...zone, points: zone.points.map((current, index) => index === pointIndex ? point : current) }))}
        />
        <aside className={'min-w-0 border border-slate-200 bg-slate-50 p-3'}>
          <input value={search} onChange={(event) => setSearch(event.target.value)} aria-label={'搜索区域'} placeholder={'搜索区域'} className={'w-full border border-slate-200 bg-white px-3 py-2 text-xs'} />
          <div className={'mt-3 max-h-48 space-y-1 overflow-y-auto'}>{draft.zones.map((zone) => <button key={zone.id} type={'button'} onClick={() => { setSelectedZoneID(zone.id); const targetMap = mapForZone(zone); if (targetMap) setMapID(targetMap); }} className={'flex w-full items-center justify-between px-2.5 py-2 text-left text-xs font-bold ' + (selectedZoneID === zone.id ? 'bg-slate-900 text-white' : 'bg-white text-slate-700')}><span className={'truncate'}>{zone.name || '未命名区域'}</span>{invalidZoneIDs.has(zone.id) && <AlertTriangle size={13} className={'text-amber-400'} />}</button>)}</div>
          {selectedZone ? <div className={'mt-4 space-y-3 border-t border-slate-200 pt-4'}>
            <label className={'block text-[11px] font-bold text-slate-600'}>区域名称<input aria-label={'区域名称'} value={selectedZone.name} disabled={!canWrite} onChange={(event) => updateZone(selectedZone.id, (zone) => ({ ...zone, name: event.target.value }))} className={'mt-1 w-full border border-slate-200 bg-white px-3 py-2 text-xs'} /></label>
            <label className={'block text-[11px] font-bold text-slate-600'}>进入等级<input aria-label={'进入等级'} type={'number'} min={1} max={100} value={selectedZone.levelRequirement} disabled={!canWrite} onChange={(event) => updateZone(selectedZone.id, (zone) => ({ ...zone, levelRequirement: Number(event.target.value) }))} className={'mt-1 w-full border border-slate-200 bg-white px-3 py-2 text-xs'} /></label>
            <PermissionEditor permissions={selectedZone.permissions} disabled={!canWrite} onChange={(permissions) => updateZone(selectedZone.id, (zone) => ({ ...zone, permissions }))} />
            <button type={'button'} disabled={!canWrite} onClick={() => removeZone(selectedZone.id)} className={'inline-flex items-center gap-1.5 text-xs font-bold text-rose-700 disabled:opacity-40'}><Trash2 size={14} />删除区域</button>
          </div> : <p className={'mt-6 text-center text-xs text-slate-400'}>选择区域后编辑属性与权限</p>}
        </aside>
      </div>

      <details className={'border-t border-slate-200 pt-3'}><summary className={'cursor-pointer text-xs font-bold text-slate-600'}>高级：预览原始 zones.json</summary><textarea aria-label={'PalZones 原始 JSON'} readOnly value={serialized} className={'mt-2 min-h-64 w-full border border-slate-800 bg-slate-950 p-3 font-mono text-xs text-slate-100'} /></details>
    </div>
  );
};

export default PalZonesEditorWorkspace;
