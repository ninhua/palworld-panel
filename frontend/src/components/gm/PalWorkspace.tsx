import React, { useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertTriangle, Download, ExternalLink, FileJson, LoaderCircle, RefreshCw, Save, Search, Send, Sparkles, Sword, Trash2, Upload, X } from 'lucide-react';
import { palDefenderGMApi } from '../../api/paldefenderGM';
import { starterGiftApi, type PalTemplateInfo } from '../../api/starterGift';
import type { Pal, PalDefenderPalCatalogEntry, PalDefenderPalTemplate } from '../../types';
import { PalIcon } from './PalIcon';
import { PalTemplateFilters } from './PalTemplateFilters';
import { createEmptyPalTemplateFilters, palTemplateMatchesFilters } from './palTemplateFilterModel';

type ActionRunner = (key: string, action: () => Promise<unknown>, success: string) => Promise<boolean>;

export const PalWorkspace: React.FC<{
  identifier: string;
  playerName: string;
  online: boolean;
  canWrite: boolean;
  canManageTemplates: boolean;
  available: boolean;
  busy: boolean;
  pending: string;
  savePals: Pal[];
  palCatalog: PalDefenderPalCatalogEntry[];
  passiveCatalog: PalDefenderPalCatalogEntry[];
  onRun: ActionRunner;
  onRelease: (pal: Pal) => Promise<boolean>;
}> = ({ identifier, playerName, online, canWrite, canManageTemplates, available, busy, pending, savePals, palCatalog, passiveCatalog, onRun, onRelease }) => {
  const queryClient = useQueryClient();
  const [palID, setPalID] = useState('');
  const [palLevel, setPalLevel] = useState('1');
  const [palCount, setPalCount] = useState('1');
  const [selectedTemplate, setSelectedTemplate] = useState('');
  const [templateSearch, setTemplateSearch] = useState('');
  const [templateFilters, setTemplateFilters] = useState(createEmptyPalTemplateFilters);
  const [templateCount, setTemplateCount] = useState('1');
  const [selectedExport, setSelectedExport] = useState('');
  const [editor, setEditor] = useState<TemplateEditor>(() => emptyTemplateEditor());
  const [templateBase, setTemplateBase] = useState<PalDefenderPalTemplate | null>(null);
  const [templateImportMessage, setTemplateImportMessage] = useState('');
  const [templateImportError, setTemplateImportError] = useState('');
  const [customCount, setCustomCount] = useState('1');
  const [grantMessage, setGrantMessage] = useState('');
  const [palSearch, setPalSearch] = useState('');
  const [passiveSearch, setPassiveSearch] = useState('');
  const [releaseTarget, setReleaseTarget] = useState<Pal | null>(null);
  const [releaseConfirmation, setReleaseConfirmation] = useState('');
  const editorSectionRef = useRef<HTMLElement>(null);

  const showEditor = () => requestAnimationFrame(() => {
    const section = editorSectionRef.current;
    if (section && typeof section.scrollIntoView === 'function') section.scrollIntoView({ behavior: 'smooth', block: 'start' });
  });

  const filteredPalCatalog = palCatalog.filter((pal) => {
    const needle = palSearch.trim().toLowerCase();
    return !needle || pal.id.toLowerCase().includes(needle) || pal.name.toLowerCase().includes(needle);
  }).slice(0, 80);
  const selectedPassiveIDs = textList(editor.passives);
  const passiveByID = new Map(passiveCatalog.map((passive) => [passive.id.toLowerCase(), passive]));
  const filteredPassiveCatalog = passiveCatalog.filter((passive) => {
    const needle = passiveSearch.trim().toLowerCase();
    return !selectedPassiveIDs.some((id) => id.toLowerCase() === passive.id.toLowerCase())
      && (!needle || passive.id.toLowerCase().includes(needle) || passive.name.toLowerCase().includes(needle));
  }).slice(0, 60);

  const palsQuery = useQuery({
    queryKey: ['paldefender-gm', 'pals', identifier],
    queryFn: () => palDefenderGMApi.pals(identifier),
    enabled: Boolean(available && identifier),
  });
  const templatesQuery = useQuery({
    queryKey: ['paldefender-gm', 'templates'],
    queryFn: palDefenderGMApi.templates,
    enabled: available,
  });
  const indexedTemplateQuery = useQuery({
    queryKey: ['paldefender-gm', 'template-index-catalog'],
    queryFn: starterGiftApi.get,
    enabled: available,
    staleTime: 5 * 60 * 1000,
  });
  const exportedQuery = useQuery({
    queryKey: ['paldefender-gm', 'exported-templates', identifier],
    queryFn: () => palDefenderGMApi.exportedPalTemplates(identifier),
    enabled: Boolean(identifier),
  });

  const indexedTemplateByName = useMemo(() => new Map(
    (indexedTemplateQuery.data?.templates ?? []).map((template) => [template.name.toLowerCase(), template] as const),
  ), [indexedTemplateQuery.data]);
  const templateIndexLabels = useMemo(() => new Map(
    (indexedTemplateQuery.data?.template_indexes ?? []).map((index) => [index.name, index.label || index.name] as const),
  ), [indexedTemplateQuery.data]);
  const visibleManagedTemplates = useMemo(() => {
    const needle = templateSearch.trim().toLowerCase();
    return (templatesQuery.data?.templates ?? []).map((template) => {
      const info = indexedTemplateByName.get(template.name.toLowerCase()) as PalTemplateInfo | undefined;
      return { template, info };
    }).filter(({ template, info }) => {
      if (!palTemplateMatchesFilters(info, templateFilters)) return false;
      const text = `${template.name} ${info?.pal_id || ''} ${info?.pal_name || ''} ${info?.english_name || ''} ${info?.category || ''} ${info?.usage_category || ''} ${info?.overall_grade || ''} ${(info?.classification_tags ?? []).join(' ')}`.toLowerCase();
      return !needle || text.includes(needle);
    }).slice(0, 120);
  }, [indexedTemplateByName, templateFilters, templateSearch, templatesQuery.data]);

  const directGrant = async () => {
    const level = Number(palLevel);
    const count = Number(palCount);
    if (!Number.isInteger(count) || count < 1 || count > 100) {
      setGrantMessage('普通帕鲁发放数量必须是 1–100 的整数。');
      return;
    }
    if (!palID.trim() || !Number.isInteger(level) || level < 1 || level > 255) return;
    if (!window.confirm(`向 ${playerName} 发放 ${palID.trim()} Lv.${level}，共 ${count} 只？`)) return;
    await onRun('give-pal', async () => {
      const result = await palDefenderGMApi.givePals(identifier, { Pals: Array.from({ length: count }, () => ({ PalID: palID.trim(), Level: level })) });
      setGrantMessage(`普通发放：请求 ${count} 只，实际发放 ${result.Granted.Pals} 只。`);
      await queryClient.invalidateQueries({ queryKey: ['paldefender-gm', 'pals', identifier] });
    }, `已向 ${playerName} 提交 ${count} 只 ${palID.trim()} Lv.${level}`);
  };

  const giveTemplate = async () => {
    const count = Number(templateCount);
    if (!Number.isInteger(count) || count < 1 || count > 20) {
      setGrantMessage('模板发放数量必须是 1–20 的整数。');
      return;
    }
    if (!selectedTemplate) return;
    if (!window.confirm(`向 ${playerName} 发放模板 ${selectedTemplate}，共 ${count} 只？`)) return;
    await onRun('give-template', async () => {
      const result = await palDefenderGMApi.givePalTemplates(identifier, { PalTemplates: Array.from({ length: count }, () => selectedTemplate) });
      setGrantMessage(`模板发放：请求 ${count} 只，实际发放 ${result.Granted.PalTemplates} 只。`);
      await queryClient.invalidateQueries({ queryKey: ['paldefender-gm', 'pals', identifier] });
    }, `已向 ${playerName} 提交模板 ${selectedTemplate} × ${count}`);
  };

  const exportPals = async () => {
    await onRun('export-pals', async () => {
      const exported = await palDefenderGMApi.exportPals(identifier);
      const latest = exported.template_info;
      queryClient.setQueryData(['paldefender-gm', 'exported-templates', identifier], (current: { player_id: string; templates: typeof exported.templates; reference_url: string } | undefined) => ({
        player_id: exported.player_id,
        templates: [...exported.templates, ...(current?.templates ?? []).filter((item) => !exported.templates.some((fresh) => fresh.name.toLowerCase() === item.name.toLowerCase()))],
        reference_url: current?.reference_url ?? '',
      }));
      setSelectedExport(latest.name);
      const defaultName = latest.name.replace(/\.json$/i, '').replace(/[^A-Za-z0-9_-]+/g, '_').slice(0, 64);
      setTemplateBase(exported.template);
      setEditor(editorFromTemplate(defaultName || 'exported_pal', exported.template));
      setTemplateImportError('');
      setTemplateImportMessage(`已导出玩家帕鲁并自动载入 ${latest.name}；可直接编辑或另存模板`);
      showEditor();
    }, `已导出 ${playerName} 的帕鲁并载入模板编辑器`);
  };

  const loadManagedTemplate = async () => {
    if (!selectedTemplate) return;
    const template = await palDefenderGMApi.template(selectedTemplate);
    setTemplateBase(template);
    setEditor(editorFromTemplate(selectedTemplate, template));
  };

  const loadExportedTemplate = async () => {
    if (!selectedExport) return;
    const template = await palDefenderGMApi.exportedPalTemplate(identifier, selectedExport);
    const defaultName = selectedExport.replace(/\.json$/i, '').replace(/[^A-Za-z0-9_-]+/g, '_').slice(0, 64);
    setTemplateBase(template);
    setEditor(editorFromTemplate(defaultName || 'exported_pal', template));
  };

  const importTemplateFile = async (file?: File) => {
    setTemplateImportMessage('');
    setTemplateImportError('');
    if (!file) return;
    if (file.size > 1024 * 1024) {
      setTemplateImportError('模板文件不能超过 1 MB');
      return;
    }
    try {
      const raw = JSON.parse(await file.text()) as unknown;
      if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('模板必须是单个 JSON 对象');
      const template = raw as PalDefenderPalTemplate;
      if (typeof template.PalID !== 'string' || !template.PalID.trim()) throw new Error('模板缺少 PalID');
      const defaultName = file.name.replace(/\.json$/i, '').replace(/[^A-Za-z0-9_-]+/g, '_').slice(0, 64);
      setTemplateBase(template);
      setEditor(editorFromTemplate(defaultName || 'imported_pal', template));
      setTemplateImportMessage(`已导入 ${file.name}，可继续编辑、直接发放或另存模板`);
    } catch (error) {
      setTemplateImportError(error instanceof Error ? error.message : '模板解析失败');
    }
  };

  const createTemplate = async () => {
    setTemplateImportMessage('');
    setTemplateImportError('');
    showEditor();
    if (!available) {
      setTemplateImportError('PalDefender 当前不可用，无法创建模板文件');
      return;
    }
    if (!canManageTemplates) {
      setTemplateImportError('当前管理员缺少 security:write 权限，无法创建模板文件');
      return;
    }
    const templatePalID = editor.palID.trim() || palID.trim();
    if (!templatePalID) {
      setTemplateImportError('新建模板前请先在上方选择帕鲁，或在编辑器中填写 PalID');
      return;
    }
    const safePalID = templatePalID.replace(/[^A-Za-z0-9_-]+/g, '_').replace(/^_+/, '').slice(0, 36) || 'pal';
    const name = `pal_${safePalID}_${Date.now()}`.slice(0, 64);
    const template: PalDefenderPalTemplate = { PalID: templatePalID, Level: 1 };
    await onRun('create-template', async () => {
      const result = await palDefenderGMApi.putTemplate(name, template);
      const savedName = result.template?.name || `${name}.json`;
      setTemplateBase(template);
      setEditor(editorFromTemplate(savedName, template));
      setSelectedTemplate(savedName);
      setTemplateImportMessage(`模板文件 ${savedName} 已创建；可继续编辑并点击“保存模板”更新文件`);
      await queryClient.invalidateQueries({ queryKey: ['paldefender-gm', 'templates'] });
    }, `模板 ${name}.json 已创建`);
  };

  const saveTemplate = async () => {
    const name = editor.name.trim();
    const template = templateFromEditor(editor, templateBase);
    if (!canManageTemplates || !name || !template.PalID) return;
    await onRun('save-template', async () => {
      await palDefenderGMApi.putTemplate(name, template);
      setSelectedTemplate(name.endsWith('.json') ? name : `${name}.json`);
      await queryClient.invalidateQueries({ queryKey: ['paldefender-gm', 'templates'] });
    }, `模板 ${name} 已保存`);
  };

  const giveCustomPal = async () => {
    const template = templateFromEditor(editor, templateBase);
    const count = Number(customCount);
    if (!Number.isInteger(count) || count < 1 || count > 20) {
      setGrantMessage('自定义发放数量必须是 1–20 的整数。');
      return;
    }
    if (!template.PalID) return;
    if (!window.confirm(`向 ${playerName} 发放 ${template.PalID} Lv.${template.Level ?? 1}，完全相同配置共 ${count} 只？`)) return;
    await onRun('give-custom-pal', async () => {
      const result = await palDefenderGMApi.giveCustomPals(identifier, { Template: template, Count: count });
      setGrantMessage(`自定义发放：请求 ${count} 只，实际发放 ${result.Granted.PalTemplates} 只。`);
      await queryClient.invalidateQueries({ queryKey: ['paldefender-gm', 'pals', identifier] });
    }, `已向 ${playerName} 提交自定义 ${template.PalID} × ${count}${template.Passives?.length ? `（${template.Passives.length} 个指定词条）` : ''}`);
  };

  const applyLegalMaximums = () => setEditor({
    ...editor,
    partnerSkillLevel: '5', condensedPals: '116',
    ivHealth: '100', ivAttackMelee: '100', ivAttackShot: '100', ivDefense: '100',
    soulHealth: '20', soulAttack: '20', soulDefense: '20', soulCraftSpeed: '20',
  });

  const addPassive = (id: string) => {
    setEditor({ ...editor, passives: [...selectedPassiveIDs, id].join(', ') });
    setPassiveSearch('');
  };

  const removePassive = (id: string) => setEditor({
    ...editor,
    passives: selectedPassiveIDs.filter((entry) => entry.toLowerCase() !== id.toLowerCase()).join(', '),
  });

  const disabled = !canWrite || !available || busy;
  return (
    <div className="space-y-5 p-4 sm:p-5">
      <div className="grid gap-5 xl:grid-cols-2">
        <section className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
          <div><h3 className="flex items-center gap-2 text-sm font-bold text-slate-800"><Sword size={16} className="text-sky-500" />按 ID 发放帕鲁</h3><p className="mt-1 text-[11px] font-semibold text-slate-400">适合普通帕鲁；需要指定词条、IV、魂强化或工作适性时使用下方自定义编辑器。</p></div>
		  <div className="mt-4 grid gap-3 sm:grid-cols-[minmax(0,1fr)_110px_110px]">
            <label className="text-xs font-bold text-slate-600">PalID<input aria-label="帕鲁 ID" value={palID} onChange={(event) => setPalID(event.target.value)} placeholder="例如 Anubis" className="mt-1.5 w-full rounded-xl border border-slate-200 px-3 py-2.5 font-mono text-xs text-slate-700 focus:border-sky-500 focus:outline-none" /></label>
            <label className="text-xs font-bold text-slate-600">等级<input aria-label="帕鲁等级" type="number" min={1} max={255} value={palLevel} onChange={(event) => setPalLevel(event.target.value)} className="mt-1.5 w-full rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none" /></label>
			<label className="text-xs font-bold text-slate-600">数量<input aria-label="普通帕鲁发放数量" type="number" min={1} max={100} value={palCount} onChange={(event) => setPalCount(event.target.value)} className="mt-1.5 w-full rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none" /></label>
          </div>
          <label className="relative mt-3 block"><Search size={13} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" /><input aria-label="搜索帕鲁目录" value={palSearch} onChange={(event) => setPalSearch(event.target.value)} placeholder="搜索中文名或 PalID" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none" /></label>
          <div className="mt-3 grid max-h-56 grid-cols-2 gap-2 overflow-y-auto pr-1 sm:grid-cols-3">
            {filteredPalCatalog.map((pal) => <button type="button" key={pal.id} onClick={() => setPalID(pal.id)} aria-pressed={palID.toLowerCase() === pal.id.toLowerCase()} className={`flex min-w-0 items-center gap-2 rounded-xl border p-2 text-left ${palID.toLowerCase() === pal.id.toLowerCase() ? 'border-sky-300 bg-sky-50' : 'border-slate-100 bg-slate-50/70'}`}><PalIcon characterID={pal.id} name={pal.name} className="h-9 w-9 rounded-lg" /><span className="min-w-0 flex-1"><span className="block truncate text-[11px] font-bold text-slate-700">{pal.name}</span><span className="mt-1 block truncate font-mono text-[9px] text-slate-400">{pal.id}</span></span></button>)}
          </div>
          <div className="mt-4 flex flex-wrap items-center gap-2">
            <button type="button" onClick={() => void directGrant()} disabled={disabled || !palID.trim()} className="inline-flex items-center gap-2 rounded-xl bg-slate-900 px-4 py-2.5 text-xs font-bold text-white disabled:opacity-40">{pending === 'give-pal' ? <LoaderCircle size={14} className="animate-spin" /> : <Send size={14} />}发放帕鲁</button>
            <a href="https://paldeck.cc/pals" target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 text-xs font-bold text-sky-600">查询 PalID <ExternalLink size={12} /></a>
          </div>
          <div className="mt-5 grid grid-cols-3 gap-2 border-t border-slate-100 pt-4">
            <SmallCount label="队伍" value={palsQuery.data?.Meta.TeamCount ?? 0} />
            <SmallCount label="帕鲁终端" value={palsQuery.data?.Meta.PalboxCount ?? 0} />
            <SmallCount label="基地" value={palsQuery.data?.Meta.BaseCampCount ?? 0} />
          </div>
        </section>

        <section className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
          <div><h3 className="flex items-center gap-2 text-sm font-bold text-slate-800"><FileJson size={16} className="text-violet-500" />模板发放与导出</h3><p className="mt-1 text-[11px] font-semibold text-slate-400">导出的现有帕鲁先读取到编辑器，再另存为可发放模板。</p></div>
		  <div className="mt-4 grid gap-3 sm:grid-cols-3">
            <label className="text-xs font-bold text-slate-600">已保存模板<select aria-label="已保存模板" value={selectedTemplate} onChange={(event) => setSelectedTemplate(event.target.value)} className="mt-1.5 w-full rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-xs font-semibold text-slate-700"><option value="">请选择</option>{(templatesQuery.data?.templates ?? []).map((template) => <option key={template.name} value={template.name}>{template.name}</option>)}</select></label>
            <label className="text-xs font-bold text-slate-600">导出文件<select aria-label="导出帕鲁模板" value={selectedExport} onChange={(event) => setSelectedExport(event.target.value)} className="mt-1.5 w-full rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-xs font-semibold text-slate-700"><option value="">请选择</option>{(exportedQuery.data?.templates ?? []).map((template) => <option key={template.name} value={template.name}>{template.name}</option>)}</select></label>
			<label className="text-xs font-bold text-slate-600">发放数量<input aria-label="模板发放数量" type="number" min={1} max={20} value={templateCount} onChange={(event) => setTemplateCount(event.target.value)} className="mt-1.5 w-full rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-violet-500 focus:outline-none" /></label>
          </div>
          <div className="mt-3">
            <label className="relative block"><Search size={13} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" /><input aria-label="搜索已保存帕鲁模板" value={templateSearch} onChange={(event) => setTemplateSearch(event.target.value)} placeholder="搜索中文名、PalID、用途或分类" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold text-slate-700" /></label>
          </div>
          <div className="mt-3"><PalTemplateFilters templates={indexedTemplateQuery.data?.templates ?? []} indexes={indexedTemplateQuery.data?.template_indexes ?? []} value={templateFilters} onChange={setTemplateFilters} /></div>
          <div className="mt-3 grid max-h-56 gap-2 overflow-y-auto pr-1 sm:grid-cols-2">
            {visibleManagedTemplates.map(({ template, info }) => <button type="button" key={template.name} onClick={() => setSelectedTemplate(template.name)} aria-pressed={selectedTemplate === template.name} className={`flex min-w-0 items-center gap-2 rounded-xl border p-2 text-left ${selectedTemplate === template.name ? 'border-violet-300 bg-violet-50' : 'border-slate-100 bg-slate-50/70'}`}>
              <PalIcon characterID={info?.pal_id || ''} name={info?.pal_name || template.name} className="h-10 w-10 rounded-lg" />
              <span className="min-w-0 flex-1"><span className="block truncate text-[11px] font-bold text-slate-700">{info?.category ? `${info.category} · ` : ''}{info?.pal_name || template.name}</span><span className="mt-1 block truncate font-mono text-[9px] text-slate-400">{info?.pal_id || template.name}</span><span className="mt-1 flex flex-wrap gap-1">{info?.overall_grade && <span className="rounded-full bg-amber-100 px-1.5 py-0.5 text-[9px] font-bold text-amber-800">{info.overall_grade}</span>}{info?.usage_category && <span className="max-w-32 truncate rounded-full bg-emerald-100 px-1.5 py-0.5 text-[9px] font-bold text-emerald-700">{info.usage_category}</span>}{info?.index_names.slice(0, 1).map((name) => <span key={name} className="max-w-32 truncate rounded-full bg-sky-100 px-1.5 py-0.5 text-[9px] font-bold text-sky-700">{templateIndexLabels.get(name) || name}</span>)}</span></span>
            </button>)}
            {visibleManagedTemplates.length === 0 && <p className="col-span-full rounded-xl bg-slate-50 px-3 py-6 text-center text-[11px] font-semibold text-slate-400">没有匹配的模板；索引 JSON 与模板 JSON 应一起放在 Templates 目录。</p>}
          </div>
          <div className="mt-4 flex flex-wrap gap-2">
            <button type="button" onClick={() => void giveTemplate()} disabled={disabled || !selectedTemplate} className="inline-flex items-center gap-2 rounded-xl bg-violet-600 px-4 py-2.5 text-xs font-bold text-white disabled:opacity-40">{pending === 'give-template' ? <LoaderCircle size={14} className="animate-spin" /> : <Upload size={14} />}发放模板</button>
            <button type="button" onClick={() => void loadManagedTemplate()} disabled={!selectedTemplate} className="inline-flex items-center gap-2 rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-bold text-slate-600 disabled:opacity-40"><FileJson size={14} />编辑模板</button>
            <button type="button" onClick={() => void exportPals()} disabled={!canWrite || busy || !identifier || !online} title={online ? '通过 PalDefender 导出玩家当前帕鲁' : 'PalDefender /exportpals 需要玩家在线并完成角色加载'} className="inline-flex items-center gap-2 rounded-xl border border-sky-200 bg-sky-50 px-3 py-2.5 text-xs font-bold text-sky-700 disabled:opacity-40">{pending === 'export-pals' ? <LoaderCircle size={14} className="animate-spin" /> : <Download size={14} />}导出玩家帕鲁</button>
            <button type="button" onClick={() => void loadExportedTemplate()} disabled={!selectedExport} className="inline-flex items-center gap-2 rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-bold text-slate-600 disabled:opacity-40"><RefreshCw size={14} />载入导出文件</button>
          </div>
          {!online && <p className="mt-3 rounded-xl bg-amber-50 px-3 py-2 text-[11px] font-semibold leading-5 text-amber-800">玩家离线时 PalDefender 不会加载用于导出的帕鲁容器。请让玩家进入服务器并完成角色加载后再导出；以前已经导出的文件仍可在上方选择和编辑。</p>}
        </section>
      </div>

      <section ref={editorSectionRef} className="scroll-mt-6 rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between"><div><h3 className="flex items-center gap-2 text-sm font-bold text-slate-800"><Save size={16} className="text-emerald-500" />PalTemplate 属性编辑器</h3><p className="mt-1 text-[11px] font-semibold text-slate-400">可直接发放当前配置；持久保存模板需要 security:write 权限，且不会覆盖玩家已有帕鲁。</p></div><a href="https://paldeck.cc/creator" target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 text-xs font-bold text-sky-600">打开模板生成器 <ExternalLink size={12} /></a></div>
        <div className="mt-4 flex flex-wrap items-center gap-2 rounded-2xl border border-sky-100 bg-sky-50/50 p-3">
          <label className="inline-flex cursor-pointer items-center gap-2 rounded-xl bg-sky-600 px-3 py-2.5 text-xs font-bold text-white"><Upload size={14} />导入 JSON 模板<input aria-label="导入帕鲁模板文件" type="file" accept=".json,application/json" className="sr-only" onChange={(event) => { const file = event.target.files?.[0]; void importTemplateFile(file); event.target.value = ''; }} /></label>
          <button type="button" onClick={() => void createTemplate()} disabled={busy} title="立即创建一个最小可用的 PalTemplate JSON 文件" className="inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-xs font-bold text-slate-600 disabled:opacity-40">{pending === 'create-template' ? <LoaderCircle size={14} className="animate-spin" /> : <FileJson size={14} />}新建模板文件</button>
          <span className="text-[10px] font-semibold text-slate-400">推荐：</span>
          <a href="https://paldeck.cc/creator" target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-[11px] font-bold text-sky-700">Paldeck 模板生成器 <ExternalLink size={11} /></a>
          <a href="https://paldeck.cc/passives" target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-[11px] font-bold text-violet-700">词条表 <ExternalLink size={11} /></a>
          <a href="https://paldeck.cc/skills" target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-[11px] font-bold text-emerald-700">技能表 <ExternalLink size={11} /></a>
        </div>
        {templateImportMessage && <p role="status" className="mt-2 rounded-xl bg-emerald-50 px-3 py-2 text-[11px] font-semibold text-emerald-700">{templateImportMessage}</p>}
        {templateImportError && <p role="alert" className="mt-2 rounded-xl bg-rose-50 px-3 py-2 text-[11px] font-semibold text-rose-700">导入失败：{templateImportError}</p>}
		{grantMessage && <p role="status" className="mt-2 rounded-xl bg-sky-50 px-3 py-2 text-[11px] font-semibold text-sky-700">{grantMessage}</p>}
		<div className="mt-3 flex flex-wrap items-center gap-2">
		  <button type="button" onClick={applyLegalMaximums} className="rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-bold text-amber-800">一键合法满值</button>
		  <span className="text-[10px] font-semibold text-slate-400">IV 100 · 魂强化 20 · 伙伴技能 5 · 浓缩 116</span>
		</div>
        <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <TextField label="模板名称" value={editor.name} onChange={(value) => setEditor({ ...editor, name: value })} placeholder="reward_anubis" />
          <TextField label="PalID" value={editor.palID} onChange={(value) => setEditor({ ...editor, palID: value })} placeholder="Anubis" />
          <TextField label="昵称" value={editor.nickname} onChange={(value) => setEditor({ ...editor, nickname: value })} placeholder="可选" />
          <TextField label="皮肤 ID" value={editor.skinID} onChange={(value) => setEditor({ ...editor, skinID: value })} placeholder="可选" />
          <label className="text-xs font-bold text-slate-600">性别<select aria-label="性别" value={editor.gender} onChange={(event) => setEditor({ ...editor, gender: event.target.value })} className="mt-1.5 w-full rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-xs font-semibold text-slate-700"><option value="">默认</option><option value="Male">Male</option><option value="Female">Female</option><option value="None">None</option></select></label>
		  <NumberEditor label="等级" min={1} max={255} value={editor.level} onChange={(value) => setEditor({ ...editor, level: value })} />
		  <NumberEditor label="伙伴技能等级" min={1} max={5} value={editor.partnerSkillLevel} onChange={(value) => setEditor({ ...editor, partnerSkillLevel: value })} />
		  <NumberEditor label="浓缩数量 / 星级进度" max={116} value={editor.condensedPals} onChange={(value) => setEditor({ ...editor, condensedPals: value })} />
		  <NumberEditor label="IV 生命" max={100} value={editor.ivHealth} onChange={(value) => setEditor({ ...editor, ivHealth: value })} />
		  <NumberEditor label="IV 近战攻击" max={100} value={editor.ivAttackMelee} onChange={(value) => setEditor({ ...editor, ivAttackMelee: value })} />
		  <NumberEditor label="IV 远程攻击" max={100} value={editor.ivAttackShot} onChange={(value) => setEditor({ ...editor, ivAttackShot: value })} />
		  <NumberEditor label="IV 防御" max={100} value={editor.ivDefense} onChange={(value) => setEditor({ ...editor, ivDefense: value })} />
		  <NumberEditor label="魂强化 生命" max={20} value={editor.soulHealth} onChange={(value) => setEditor({ ...editor, soulHealth: value })} />
		  <NumberEditor label="魂强化 攻击" max={20} value={editor.soulAttack} onChange={(value) => setEditor({ ...editor, soulAttack: value })} />
		  <NumberEditor label="魂强化 防御" max={20} value={editor.soulDefense} onChange={(value) => setEditor({ ...editor, soulDefense: value })} />
		  <NumberEditor label="魂强化 作业速度" max={20} value={editor.soulCraftSpeed} onChange={(value) => setEditor({ ...editor, soulCraftSpeed: value })} />
          <CheckboxEditor label="稀有 / 闪光" checked={editor.shiny} onChange={(checked) => setEditor({ ...editor, shiny: checked })} />
          <CheckboxEditor label="觉醒个体" checked={editor.isAwakening} onChange={(checked) => setEditor({ ...editor, isAwakening: checked })} />
        </div>
        <section className="mt-4 rounded-2xl border border-violet-100 bg-violet-50/40 p-3">
          <div className="flex flex-wrap items-center justify-between gap-2"><div><h4 className="flex items-center gap-2 text-xs font-black text-violet-800"><Sparkles size={14} />指定被动词条</h4><p className="mt-1 text-[10px] font-semibold text-violet-500">支持中文名或内部 ID 搜索；提交给 PalDefender 时自动使用准确的内部 ID。</p></div><span className="rounded-full bg-white px-2 py-1 text-[10px] font-black text-violet-600">已选 {selectedPassiveIDs.length}</span></div>
          <label className="relative mt-3 block"><Search size={13} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" /><input aria-label="搜索被动词条" value={passiveSearch} onChange={(event) => setPassiveSearch(event.target.value)} placeholder="例如 传说、卓绝技艺、Legend" className="w-full rounded-xl border border-violet-100 bg-white py-2.5 pl-9 pr-3 text-xs font-semibold text-slate-700 focus:border-violet-400 focus:outline-none" /></label>
          {selectedPassiveIDs.length > 0 && <div className="mt-3 flex flex-wrap gap-2">{selectedPassiveIDs.map((id) => { const entry = passiveByID.get(id.toLowerCase()); return <button type="button" key={id} onClick={() => removePassive(id)} aria-label={`移除词条 ${entry?.name || id}`} className="inline-flex items-center gap-1.5 rounded-full border border-violet-200 bg-white px-2.5 py-1.5 text-[10px] font-bold text-violet-700"><span>{entry?.name || id}</span>{entry && <span className="font-mono text-[9px] text-violet-400">{id}</span>}<X size={11} /></button>; })}</div>}
          <div className="mt-3 grid max-h-44 gap-2 overflow-y-auto pr-1 sm:grid-cols-2 lg:grid-cols-3">{filteredPassiveCatalog.map((passive) => <button type="button" key={passive.id} onClick={() => addPassive(passive.id)} className="rounded-xl border border-violet-100 bg-white p-2.5 text-left hover:border-violet-300"><span className="block truncate text-[11px] font-bold text-slate-700">{passive.name}</span><span className="mt-1 block truncate font-mono text-[9px] text-slate-400">{passive.id}</span></button>)}</div>
        </section>
        <div className="mt-3 grid gap-3 lg:grid-cols-3">
          <TextArea label="被动词条 ID（高级编辑）" value={editor.passives} onChange={(value) => setEditor({ ...editor, passives: value })} placeholder="Legend, CraftSpeed_up3" />
          <TextArea label="主动技能（最多 3 个）" value={editor.activeSkills} onChange={(value) => setEditor({ ...editor, activeSkills: value })} placeholder="SandTornado, RockLance" />
          <TextArea label="已学习技能" value={editor.learntSkills} onChange={(value) => setEditor({ ...editor, learntSkills: value })} placeholder="技能内部 ID，逗号分隔" />
        </div>
        <section className="mt-4 rounded-2xl border border-slate-100 bg-slate-50/60 p-3"><h4 className="text-xs font-black text-slate-700">额外工作适性</h4><p className="mt-1 text-[10px] font-semibold text-slate-400">仅填写需要额外覆盖的等级；留空不会修改。</p><div className="mt-3 grid gap-2 sm:grid-cols-3 lg:grid-cols-5">{WORK_SUITABILITY_FIELDS.map(([key, label]) => <NumberEditor key={key} label={label} value={editor.workSuitabilities[key] || ''} onChange={(value) => setEditor({ ...editor, workSuitabilities: { ...editor.workSuitabilities, [key]: value } })} />)}</div></section>
        <div className="mt-4 flex flex-wrap gap-2">
		  <label className="text-xs font-bold text-slate-600">发放数量<input aria-label="自定义发放数量" type="number" min={1} max={20} value={customCount} onChange={(event) => setCustomCount(event.target.value)} className="ml-2 w-20 rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700" /></label>
          <button type="button" onClick={() => void giveCustomPal()} disabled={disabled || !editor.palID.trim()} className="inline-flex items-center gap-2 rounded-xl bg-violet-600 px-4 py-2.5 text-xs font-bold text-white disabled:opacity-40">{pending === 'give-custom-pal' ? <LoaderCircle size={14} className="animate-spin" /> : <Sparkles size={14} />}直接发放当前配置</button>
          <button type="button" onClick={() => void saveTemplate()} disabled={busy || !available || !canManageTemplates || !editor.name.trim() || !editor.palID.trim()} title={canManageTemplates ? '保存为持久模板' : '需要 security:write 权限'} className="inline-flex items-center gap-2 rounded-xl bg-emerald-600 px-4 py-2.5 text-xs font-bold text-white disabled:opacity-40">{pending === 'save-template' ? <LoaderCircle size={14} className="animate-spin" /> : <Save size={14} />}保存模板</button>
        </div>
      </section>

      <section className="overflow-hidden rounded-2xl border border-slate-100 bg-white">
        <div className="flex flex-col gap-2 border-b border-slate-100 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
          <div><h3 className="text-sm font-bold text-slate-800">存档解析中的玩家帕鲁</h3><p className="mt-1 text-[11px] font-semibold text-slate-400">独立滚动区域；状态、被动词条和位置来自最新存档索引。</p></div>
          <span className="text-[11px] font-bold text-slate-400">{savePals.length} 只</span>
        </div>
        {savePals.length === 0 ? <div className="px-4 py-10 text-center text-xs font-semibold text-slate-400">没有匹配的存档帕鲁</div> : <div className="grid max-h-[36rem] gap-2 overflow-y-auto overscroll-contain p-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">{savePals.map((pal) => <div key={pal.id} className="flex min-w-0 items-start gap-3 rounded-xl border border-slate-100 bg-slate-50/70 p-3"><button type="button" onClick={() => setPalID(pal.character_id || '')} className="flex min-w-0 flex-1 items-start gap-3 text-left"><PalIcon characterID={pal.character_id} name={pal.name} className="h-12 w-12 rounded-xl" /><span className="min-w-0 flex-1"><span className="block truncate text-xs font-bold text-slate-700">{pal.nickname || pal.name}</span><span className="block truncate font-mono text-[10px] text-slate-400">{pal.character_id || pal.id}</span><span className="mt-1 block text-[10px] font-bold text-slate-500">Lv.{pal.level} · {pal.gender || '未知性别'} · Rank {pal.rank ?? 0}</span><span className="mt-1 flex flex-wrap gap-1"><span className={`rounded-full px-1.5 py-0.5 text-[9px] font-bold ${pal.status === 'Dead' ? 'bg-rose-100 text-rose-700' : pal.status === 'Injured' ? 'bg-amber-100 text-amber-700' : pal.status === 'Working' ? 'bg-sky-100 text-sky-700' : 'bg-emerald-100 text-emerald-700'}`}>{pal.status === 'Dead' ? '死亡' : pal.status === 'Injured' ? '异常' : pal.status === 'Working' ? '工作中' : '正常'}</span>{(pal.passives ?? []).slice(0, 3).map((passive) => <span key={passive} className="max-w-28 truncate rounded-full bg-violet-100 px-1.5 py-0.5 text-[9px] font-bold text-violet-700" title={passive}>{passive}</span>)}{(pal.passives?.length ?? 0) > 3 && <span className="text-[9px] font-bold text-slate-400">+{(pal.passives?.length ?? 0) - 3}</span>}</span><span className="mt-1 block truncate text-[9px] font-semibold text-slate-400">{pal.terminal_location || pal.location_type || '位置未知'}</span></span></button><button type="button" aria-label={`放生 ${pal.nickname || pal.name}`} title="谨慎放生" onClick={() => { setReleaseTarget(pal); setReleaseConfirmation(''); }} disabled={!canWrite || !available || busy || !pal.character_id} className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-rose-200 bg-rose-50 text-rose-600 disabled:opacity-35"><Trash2 size={13} /></button></div>)}</div>}
      </section>
      {releaseTarget && <div className="fixed inset-0 z-[80] flex items-center justify-center bg-slate-100/85 p-4 backdrop-blur-sm" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) setReleaseTarget(null); }}><section role="dialog" aria-modal="true" aria-labelledby="release-pal-title" className="w-full max-w-md rounded-2xl border border-slate-200 bg-white p-5 shadow-xl ring-1 ring-slate-900/5"><h3 id="release-pal-title" className="flex items-center gap-2 text-base font-black text-rose-700"><AlertTriangle size={17} />确认放生帕鲁</h3><p className="mt-3 text-xs font-semibold leading-5 text-slate-600">将通过 PalDefender 删除最多一只匹配帕鲁：<strong>{releaseTarget.nickname || releaseTarget.name}</strong>，{releaseTarget.character_id}，Lv.{releaseTarget.level}{releaseTarget.gender ? `，${releaseTarget.gender}` : ''}{releaseTarget.rank != null ? `，Rank ${releaseTarget.rank}` : ''}。</p><p className="mt-2 rounded-xl bg-amber-50 px-3 py-2.5 text-[11px] font-semibold leading-5 text-amber-800">PalDefender 按属性筛选而不是实例 ID；存在完全相同帕鲁时，可能删除其中任意一只。存档快照也可能暂时滞后。</p><label className="mt-4 block text-xs font-bold text-slate-600">输入玩家名称“{playerName}”确认<input aria-label="放生确认玩家名称" value={releaseConfirmation} onChange={(event) => setReleaseConfirmation(event.target.value)} className="mt-1.5 w-full rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-semibold" /></label><div className="mt-5 flex justify-end gap-2"><button type="button" onClick={() => setReleaseTarget(null)} disabled={busy} className="rounded-xl border border-slate-200 px-4 py-2.5 text-xs font-bold text-slate-600 disabled:opacity-40">取消</button><button type="button" onClick={async () => { if (releaseTarget && await onRelease(releaseTarget)) setReleaseTarget(null); }} disabled={busy || releaseConfirmation !== playerName} className="inline-flex items-center gap-2 rounded-xl bg-rose-600 px-4 py-2.5 text-xs font-bold text-white disabled:opacity-40">{pending === 'release-pal' && <LoaderCircle size={14} className="animate-spin" />}确认放生</button></div></section></div>}
    </div>
  );
};

interface TemplateEditor {
  name: string; palID: string; nickname: string; skinID: string; gender: string; level: string; partnerSkillLevel: string; condensedPals: string;
  shiny: boolean; isAwakening: boolean;
  ivHealth: string; ivAttackMelee: string; ivAttackShot: string; ivDefense: string;
  soulHealth: string; soulAttack: string; soulDefense: string; soulCraftSpeed: string;
  passives: string; activeSkills: string; learntSkills: string;
  workSuitabilities: Record<string, string>;
}

const WORK_SUITABILITY_FIELDS = [
  ['BaseCampBattle', '基地战斗'], ['EmitFlame', '生火'], ['Watering', '浇水'], ['Seeding', '播种'], ['GenerateElectricity', '发电'],
  ['Handcraft', '手工作业'], ['Collection', '采集'], ['Deforest', '伐木'], ['Mining', '采矿'], ['OilExtraction', '采油'],
  ['ProductMedicine', '制药'], ['Cool', '冷却'], ['Transport', '搬运'], ['MonsterFarm', '牧场'], ['Anyone', '任意工作'],
] as const;

const emptyTemplateEditor = (): TemplateEditor => ({
  name: '', palID: '', nickname: '', skinID: '', gender: '', level: '1', partnerSkillLevel: '1', condensedPals: '', shiny: false, isAwakening: false,
  ivHealth: '', ivAttackMelee: '', ivAttackShot: '', ivDefense: '', soulHealth: '', soulAttack: '', soulDefense: '', soulCraftSpeed: '',
  passives: '', activeSkills: '', learntSkills: '', workSuitabilities: {},
});
const textList = (value: string) => [...new Set(value.split(/[\n,，;；]+/).map((item) => item.trim()).filter(Boolean))];
const optionalNumber = (value: string) => value.trim() === '' ? undefined : Number(value);
const compactMap = (values: Record<string, string>) => Object.fromEntries(Object.entries(values).flatMap(([key, value]) => value.trim() === '' ? [] : [[key, Number(value)]]));

const templateFromEditor = (editor: TemplateEditor, base: PalDefenderPalTemplate | null = null): PalDefenderPalTemplate => {
  const ivs = compactMap({ Health: editor.ivHealth, AttackMelee: editor.ivAttackMelee, AttackShot: editor.ivAttackShot, Defense: editor.ivDefense });
  const souls = compactMap({ Health: editor.soulHealth, Attack: editor.soulAttack, Defense: editor.soulDefense, CraftSpeed: editor.soulCraftSpeed });
  const workSuitabilities = compactMap(editor.workSuitabilities);
  const template: PalDefenderPalTemplate = { ...(base ?? {}), PalID: editor.palID.trim() };
  delete template.Nickname;
  delete template.SkinId;
  delete template.Gender;
  delete template.Level;
  delete template.PartnerSkillLevel;
  delete template.CondensedPals;
  delete template.Shiny;
  delete template.IsAwakening;
  delete template.IVs;
  delete template.PalSouls;
  delete template.Passives;
  delete template.ActiveSkills;
  delete template.LearntSkills;
  delete template.ExtraWorkSuitabilities;
  if (editor.nickname.trim()) template.Nickname = editor.nickname.trim();
  if (editor.skinID.trim()) template.SkinId = editor.skinID.trim();
  if (editor.gender) template.Gender = editor.gender as 'Male' | 'Female' | 'None';
  if (optionalNumber(editor.level) !== undefined) template.Level = optionalNumber(editor.level);
  if (optionalNumber(editor.partnerSkillLevel) !== undefined) template.PartnerSkillLevel = optionalNumber(editor.partnerSkillLevel);
  if (optionalNumber(editor.condensedPals) !== undefined) template.CondensedPals = optionalNumber(editor.condensedPals);
  if (editor.shiny) template.Shiny = true;
  if (editor.isAwakening) template.IsAwakening = true;
  if (Object.keys(ivs).length > 0) template.IVs = ivs;
  if (Object.keys(souls).length > 0) template.PalSouls = souls;
  if (textList(editor.passives).length > 0) template.Passives = textList(editor.passives);
  if (textList(editor.activeSkills).length > 0) template.ActiveSkills = textList(editor.activeSkills).slice(0, 3);
  if (textList(editor.learntSkills).length > 0) template.LearntSkills = textList(editor.learntSkills);
  if (Object.keys(workSuitabilities).length > 0) template.ExtraWorkSuitabilities = workSuitabilities;
  return template;
};

const editorFromTemplate = (name: string, template: PalDefenderPalTemplate): TemplateEditor => ({
  name: name.replace(/\.json$/i, ''), palID: template.PalID || '', nickname: template.Nickname || '', skinID: template.SkinId || '', gender: template.Gender || '',
  level: template.Level == null ? '' : String(template.Level), partnerSkillLevel: template.PartnerSkillLevel == null ? '' : String(template.PartnerSkillLevel), condensedPals: template.CondensedPals == null ? '' : String(template.CondensedPals),
  shiny: Boolean(template.Shiny), isAwakening: Boolean(template.IsAwakening),
  ivHealth: template.IVs?.Health == null ? '' : String(template.IVs.Health), ivAttackMelee: template.IVs?.AttackMelee == null ? '' : String(template.IVs.AttackMelee), ivAttackShot: template.IVs?.AttackShot == null ? '' : String(template.IVs.AttackShot), ivDefense: template.IVs?.Defense == null ? '' : String(template.IVs.Defense),
  soulHealth: template.PalSouls?.Health == null ? '' : String(template.PalSouls.Health), soulAttack: template.PalSouls?.Attack == null ? '' : String(template.PalSouls.Attack), soulDefense: template.PalSouls?.Defense == null ? '' : String(template.PalSouls.Defense), soulCraftSpeed: template.PalSouls?.CraftSpeed == null ? '' : String(template.PalSouls.CraftSpeed),
  passives: (template.Passives ?? []).join(', '), activeSkills: (template.ActiveSkills ?? []).join(', '), learntSkills: (template.LearntSkills ?? []).join(', '),
  workSuitabilities: Object.fromEntries(Object.entries(template.ExtraWorkSuitabilities ?? {}).map(([key, value]) => [key, String(value)])),
});

const SmallCount: React.FC<{ label: string; value: number }> = ({ label, value }) => <div className="rounded-xl bg-slate-50 px-3 py-2.5 text-center"><p className="text-[9px] font-bold text-slate-400">{label}</p><p className="mt-1 text-sm font-black text-slate-700">{value}</p></div>;
const TextField: React.FC<{ label: string; value: string; onChange: (value: string) => void; placeholder: string }> = ({ label, value, onChange, placeholder }) => <label className="text-xs font-bold text-slate-600">{label}<input aria-label={label} value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} className="mt-1.5 w-full rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none" /></label>;
const NumberEditor: React.FC<{ label: string; value: string; onChange: (value: string) => void; min?: number; max?: number }> = ({ label, value, onChange, min = 0, max = 255 }) => <label className="text-xs font-bold text-slate-600">{label}<input aria-label={label} type="number" min={min} max={max} value={value} onChange={(event) => onChange(event.target.value)} className="mt-1.5 w-full rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none" /></label>;
const CheckboxEditor: React.FC<{ label: string; checked: boolean; onChange: (checked: boolean) => void }> = ({ label, checked, onChange }) => <label className="flex min-h-[62px] items-center justify-between rounded-xl border border-slate-200 px-3 text-xs font-bold text-slate-600"><span>{label}</span><input aria-label={label} type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} className="h-4 w-4 rounded border-slate-300 text-violet-600" /></label>;
const TextArea: React.FC<{ label: string; value: string; onChange: (value: string) => void; placeholder: string }> = ({ label, value, onChange, placeholder }) => <label className="text-xs font-bold text-slate-600">{label}<textarea aria-label={label} rows={3} value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} className="mt-1.5 w-full resize-y rounded-xl border border-slate-200 p-3 font-mono text-xs text-slate-700 focus:border-sky-500 focus:outline-none" /></label>;
