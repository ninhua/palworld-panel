import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Archive,
  CheckCircle2,
  CircleAlert,
  Crown,
  LoaderCircle,
  MapPin,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Search,
  ShieldAlert,
  X,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import {
  bossApi,
  type BossExecutionAttempt,
  type BossSummon,
  type BossTemplate,
  type BossTemplateInput,
} from '../api/boss';
import { starterGiftApi, type PalTemplateInfo } from '../api/starterGift';
import { PalIcon } from '../components/gm/PalIcon';
import { PalTemplateFilters } from '../components/gm/PalTemplateFilters';
import {
  createEmptyPalTemplateFilters,
  palTemplateMatchesFilters,
} from '../components/gm/palTemplateFilterModel';

interface Notice {
  type: 'success' | 'error';
  text: string;
}

interface BossDraft {
  name: string;
  description: string;
  palTemplateFile: string;
  count: number;
  spawnRadius: number;
  capturable: boolean;
  cooldownSeconds: number;
  location: { x: number; y: number; z: number; label: string };
  enabled: boolean;
}

const emptyDraft = (): BossDraft => ({
  name: '',
  description: '',
  palTemplateFile: '',
  count: 1,
  spawnRadius: 0,
  capturable: false,
  cooldownSeconds: 300,
  location: { x: 0, y: 0, z: 0, label: '' },
  enabled: true,
});

const number = new Intl.NumberFormat('zh-CN');
const asRecord = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};
const metadataString = (metadata: unknown, key: string) => {
  const value = asRecord(metadata)[key];
  return typeof value === 'string' ? value.trim() : '';
};
const isFixedBossMetadata = (metadata: unknown) => metadataString(metadata, 'activity_kind') === 'fixed_boss';
const templateDisplayName = (template: PalTemplateInfo) => template.pal_name?.trim() || template.nickname?.trim() || template.name;
const templateSearchText = (template: PalTemplateInfo) =>
  `${template.pal_name || ''} ${template.english_name || ''} ${template.pal_id || ''} ${template.nickname || ''} ${template.category || ''} ${template.usage_category || ''} ${template.overall_grade || ''} ${template.classification_tags.join(' ')} ${template.passive_names.join(' ')} ${template.index_names.join(' ')} ${template.name}`.toLowerCase();
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—';
const newRequestKey = () => {
  const uuid = globalThis.crypto?.randomUUID?.();
  return uuid ? `fixed-boss-${uuid}` : `fixed-boss-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
};

const attemptLabel: Record<BossExecutionAttempt['status'], string> = {
  running: '执行中',
  succeeded: '召唤成功',
  failed: '召唤失败',
  uncertain: '结果待核对',
};

const summonStatusLabel: Record<BossSummon['status'], string> = {
  pending: '等待执行',
  active: '进行中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
};

const statusClass = (status: BossSummon['status']) => ({
  pending: 'border-amber-200 bg-amber-50 text-amber-700',
  active: 'border-sky-200 bg-sky-50 text-sky-700',
  completed: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  failed: 'border-rose-200 bg-rose-50 text-rose-700',
  cancelled: 'border-slate-200 bg-slate-100 text-slate-600',
}[status]);

const FieldLabel: React.FC<React.PropsWithChildren> = ({ children }) => (
  <span className="mb-1.5 block text-xs font-bold text-slate-500">{children}</span>
);

const Metric: React.FC<{ label: string; value: React.ReactNode; detail: string; icon: React.ReactNode }> = ({ label, value, detail, icon }) => (
  <div className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm">
    <div className="flex items-start justify-between gap-3">
      <span className="text-[11px] font-black uppercase tracking-[0.16em] text-slate-400">{label}</span>
      <span className="text-rose-500">{icon}</span>
    </div>
    <strong className="mt-3 block truncate text-xl font-black text-slate-900">{value}</strong>
    <span className="mt-1 block text-[11px] font-semibold text-slate-500">{detail}</span>
  </div>
);

export const FixedBossOperations: React.FC = () => {
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<Notice | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingID, setEditingID] = useState('');
  const [draft, setDraft] = useState<BossDraft>(emptyDraft);
  const [templateSearch, setTemplateSearch] = useState('');
  const [templateFilters, setTemplateFilters] = useState(createEmptyPalTemplateFilters);
  const [definitionSearch, setDefinitionSearch] = useState('');
  const [notes, setNotes] = useState<Record<string, string>>({});

  const executionQuery = useQuery({
    queryKey: ['fixed-boss', 'execution'],
    queryFn: bossApi.executionStatus,
    refetchInterval: 15_000,
  });
  const definitionsQuery = useQuery({
    queryKey: ['fixed-boss', 'definitions'],
    queryFn: () => bossApi.templates(true),
  });
  const summonsQuery = useQuery({
    queryKey: ['fixed-boss', 'summons'],
    queryFn: () => bossApi.summons(''),
    refetchInterval: 10_000,
  });
  const catalogQuery = useQuery({
    queryKey: ['fixed-boss', 'pal-template-catalog'],
    queryFn: starterGiftApi.get,
    staleTime: 5 * 60 * 1000,
  });

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['fixed-boss'] }),
      queryClient.invalidateQueries({ queryKey: ['boss'] }),
    ]);
  };

  const allDefinitions = definitionsQuery.data?.items || [];
  const definitions = useMemo(
    () => allDefinitions.filter((item) => isFixedBossMetadata(item.metadata)),
    [allDefinitions],
  );
  const definitionIDs = useMemo(() => new Set(definitions.map((item) => item.id)), [definitions]);
  const summons = useMemo(
    () => (summonsQuery.data?.items || []).filter((item) => isFixedBossMetadata(item.metadata) || definitionIDs.has(item.template_id)),
    [summonsQuery.data, definitionIDs],
  );
  const catalog = useMemo(
    () => (catalogQuery.data?.templates || []).filter((item) => item.name && !item.parse_error && item.pal_id),
    [catalogQuery.data],
  );
  const catalogByName = useMemo(() => {
    const result = new Map<string, PalTemplateInfo>();
    for (const item of catalog) {
      result.set(item.name.toLowerCase(), item);
      result.set(item.name.toLowerCase().replace(/\.json$/, ''), item);
    }
    return result;
  }, [catalog]);
  const selectedTemplate = useMemo(() => {
    const key = draft.palTemplateFile.trim().toLowerCase();
    return catalogByName.get(key) || catalogByName.get(key.replace(/\.json$/, ''));
  }, [draft.palTemplateFile, catalogByName]);
  const visibleCatalog = useMemo(() => {
    const query = templateSearch.trim().toLowerCase();
    return catalog
      .filter((item) => palTemplateMatchesFilters(item, templateFilters))
      .filter((item) => !query || templateSearchText(item).includes(query))
      .sort((left, right) => templateDisplayName(left).localeCompare(templateDisplayName(right), 'zh-CN'))
      .slice(0, 300);
  }, [catalog, templateFilters, templateSearch]);
  const visibleDefinitions = useMemo(() => {
    const query = definitionSearch.trim().toLowerCase();
    return definitions
      .filter((item) => !query || `${item.name} ${item.description || ''} ${metadataString(item.metadata, 'pal_template_file')} ${item.location.label || ''}`.toLowerCase().includes(query))
      .sort((left, right) => right.updated_at.localeCompare(left.updated_at));
  }, [definitions, definitionSearch]);

  const saveMutation = useMutation({
    mutationFn: async () => {
      if (!selectedTemplate?.pal_id) throw new Error('必须从 PalTemplate 目录选择有效模板。');
      if (!draft.name.trim()) throw new Error('Boss 名称不能为空。');
      const input: BossTemplateInput = {
        name: draft.name.trim(),
        description: draft.description.trim(),
        pal_id: selectedTemplate.pal_id,
        level: Math.max(1, Math.trunc(selectedTemplate.level || 1)),
        count: Math.max(1, Math.min(50, Math.trunc(draft.count || 1))),
        hp_multiplier: 1,
        attack_multiplier: 1,
        defense_multiplier: 1,
        spawn_radius: Math.max(0, Math.min(10000, Number(draft.spawnRadius) || 0)),
        capturable: draft.capturable,
        cooldown_seconds: Math.max(0, Math.min(86400, Math.trunc(draft.cooldownSeconds || 0))),
        reward_id: '',
        location: {
          x: Number(draft.location.x) || 0,
          y: Number(draft.location.y) || 0,
          z: Number(draft.location.z) || 0,
          label: draft.location.label.trim(),
        },
        enabled: draft.enabled,
        metadata: {
          activity_kind: 'fixed_boss',
          fixed_coordinate: true,
          pal_template_file: selectedTemplate.name,
          pal_template_snapshot: {
            pal_id: selectedTemplate.pal_id,
            level: selectedTemplate.level || 1,
            nickname: selectedTemplate.nickname || '',
            pal_name: selectedTemplate.pal_name || '',
            modified_at: selectedTemplate.modified_at || '',
            size: selectedTemplate.size || 0,
          },
        },
      };
      return editingID ? bossApi.updateTemplate(editingID, input) : bossApi.createTemplate(input);
    },
    onSuccess: async (item) => {
      setNotice({ type: 'success', text: `Boss 定义“${item.name}”已${editingID ? '更新' : '创建'}。` });
      setEditorOpen(false);
      setEditingID('');
      setDraft(emptyDraft());
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const archiveMutation = useMutation({
    mutationFn: (item: BossTemplate) => bossApi.archiveTemplate(item.id),
    onSuccess: async (item) => {
      setNotice({ type: 'success', text: `Boss 定义“${item.name}”已归档。` });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const summonMutation = useMutation({
    mutationFn: async (item: BossTemplate) => {
      const palTemplateFile = metadataString(item.metadata, 'pal_template_file');
      if (!palTemplateFile || !catalogByName.has(palTemplateFile.toLowerCase()) && !catalogByName.has(palTemplateFile.toLowerCase().replace(/\.json$/, ''))) {
        throw new Error('该 Boss 使用的 PalTemplate 当前不存在，请编辑后重新选择。');
      }
      const result = await bossApi.createSummon({
        template_id: item.id,
        request_key: newRequestKey(),
        notes: (notes[item.id] || '').trim(),
        metadata: {
          activity_kind: 'fixed_boss',
          fixed_boss_definition_id: item.id,
          pal_template_file: palTemplateFile,
        },
      });
      const attempt = await bossApi.executeNextWave(result.summon.id);
      return { result, attempt };
    },
    onSuccess: async ({ result, attempt }) => {
      setNotes((current) => ({ ...current, [result.summon.template_id]: '' }));
      setNotice({
        type: attempt.status === 'succeeded' ? 'success' : 'error',
        text: attempt.status === 'succeeded'
          ? `Boss“${result.summon.template_name}”已在固定坐标召唤，成功执行 ${attempt.completed_commands}/${attempt.command_count} 条命令。`
          : `${attemptLabel[attempt.status]}：${attempt.failure || '请查看执行记录。'}`,
      });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: `Boss 召唤失败：${getErrorMessage(error)}` }),
  });

  const openCreate = () => {
    setEditingID('');
    setDraft(emptyDraft());
    setTemplateSearch('');
    setTemplateFilters(createEmptyPalTemplateFilters());
    setEditorOpen(true);
  };

  const openEdit = (item: BossTemplate) => {
    setEditingID(item.id);
    setDraft({
      name: item.name,
      description: item.description || '',
      palTemplateFile: metadataString(item.metadata, 'pal_template_file'),
      count: item.count,
      spawnRadius: item.spawn_radius,
      capturable: item.capturable,
      cooldownSeconds: item.cooldown_seconds,
      location: {
        x: item.location.x,
        y: item.location.y,
        z: item.location.z,
        label: item.location.label || '',
      },
      enabled: item.enabled,
    });
    setTemplateSearch('');
    setTemplateFilters(createEmptyPalTemplateFilters());
    setEditorOpen(true);
  };

  const chooseTemplate = (item: PalTemplateInfo) => {
    setDraft((current) => ({
      ...current,
      palTemplateFile: item.name,
      name: current.name.trim() ? current.name : templateDisplayName(item),
    }));
  };

  const activeSummons = summons.filter((item) => item.status === 'pending' || item.status === 'active').length;
  const execution = executionQuery.data;
  const loading = definitionsQuery.isLoading || catalogQuery.isLoading;

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <header className="flex flex-col gap-4 rounded-3xl border border-slate-200 bg-white p-5 shadow-sm lg:flex-row lg:items-center lg:justify-between">
        <div>
          <div className="flex items-center gap-2 text-xs font-black uppercase tracking-[0.18em] text-rose-500"><Crown size={16} />Fixed-coordinate Boss</div>
          <h1 className="mt-2 text-2xl font-black text-slate-900">Boss 管理</h1>
          <p className="mt-1 text-sm font-semibold text-slate-500">选择 PalTemplate，在指定世界坐标直接召唤。Boss 与据点袭击分开管理。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button type="button" onClick={() => void refresh()} className="inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-4 py-2.5 text-xs font-black text-slate-600 hover:bg-slate-50">
            <RefreshCw size={15} />刷新
          </button>
          <button type="button" onClick={openCreate} className="inline-flex items-center gap-2 rounded-xl bg-rose-600 px-4 py-2.5 text-xs font-black text-white hover:bg-rose-700">
            <Plus size={15} />新建 Boss
          </button>
        </div>
      </header>

      {notice && (
        <div className={`rounded-2xl border px-4 py-3 text-xs font-bold ${notice.type === 'success' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-rose-200 bg-rose-50 text-rose-700'}`}>
          {notice.type === 'success' ? <CheckCircle2 className="mr-2 inline" size={15} /> : <CircleAlert className="mr-2 inline" size={15} />}
          {notice.text}
        </div>
      )}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Metric label="Boss 定义" value={number.format(definitions.length)} detail="独立于袭击模板" icon={<Crown size={18} />} />
        <Metric label="可用定义" value={number.format(definitions.filter((item) => item.enabled && !item.archived_at).length)} detail="可立即召唤" icon={<CheckCircle2 size={18} />} />
        <Metric label="等待/进行中" value={number.format(activeSummons)} detail="复用全局执行互斥" icon={<LoaderCircle size={18} />} />
        <Metric label="执行器" value={execution?.available ? '可用' : '不可用'} detail={execution?.message || '正在读取状态'} icon={<ShieldAlert size={18} />} />
      </div>

      <section className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-lg font-black text-slate-900">Boss 定义</h2>
            <p className="mt-1 text-xs font-semibold text-slate-500">坐标、PalTemplate、数量和捕捉规则保存为可重复使用的定义。</p>
          </div>
          <label className="relative min-w-0 sm:w-80">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} />
            <input value={definitionSearch} onChange={(event) => setDefinitionSearch(event.target.value)} placeholder="搜索名称、模板或坐标标签" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold outline-none focus:border-rose-400" />
          </label>
        </div>

        {loading ? (
          <div className="py-16 text-center text-xs font-bold text-slate-400"><LoaderCircle className="mr-2 inline animate-spin" size={16} />正在读取 Boss 数据...</div>
        ) : visibleDefinitions.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-slate-300 py-16 text-center text-sm font-bold text-slate-400">暂无 Boss 定义</div>
        ) : (
          <div className="grid gap-4 xl:grid-cols-2">
            {visibleDefinitions.map((item) => {
              const file = metadataString(item.metadata, 'pal_template_file');
              const info = catalogByName.get(file.toLowerCase()) || catalogByName.get(file.toLowerCase().replace(/\.json$/, ''));
              const unavailable = !info;
              return (
                <article key={item.id} className="rounded-2xl border border-slate-200 p-4">
                  <div className="flex items-start gap-3">
                    <PalIcon characterID={info?.pal_id || item.pal_id} name={info ? templateDisplayName(info) : item.name} className="size-14 rounded-2xl border border-slate-200" />
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="truncate text-base font-black text-slate-900">{item.name}</h3>
                        {item.enabled && !item.archived_at
                          ? <span className="rounded-full bg-emerald-50 px-2 py-1 text-[10px] font-black text-emerald-700">已启用</span>
                          : <span className="rounded-full bg-slate-100 px-2 py-1 text-[10px] font-black text-slate-500">已停用</span>}
                        {unavailable && <span className="rounded-full bg-rose-50 px-2 py-1 text-[10px] font-black text-rose-700">模板缺失</span>}
                      </div>
                      <p className="mt-1 truncate text-xs font-semibold text-slate-500">{info ? templateDisplayName(info) : file || item.pal_id}</p>
                      <p className="mt-1 text-[11px] font-mono text-slate-400">{file || '未保存 PalTemplate 文件'}</p>
                    </div>
                  </div>
                  <div className="mt-4 grid grid-cols-2 gap-2 text-xs font-semibold text-slate-600 sm:grid-cols-4">
                    <span className="rounded-xl bg-slate-50 px-3 py-2">数量 {item.count}</span>
                    <span className="rounded-xl bg-slate-50 px-3 py-2">半径 {number.format(item.spawn_radius)}</span>
                    <span className="rounded-xl bg-slate-50 px-3 py-2">{item.capturable ? '允许捕捉' : '禁止捕捉'}</span>
                    <span className="rounded-xl bg-slate-50 px-3 py-2">冷却 {item.cooldown_seconds}s</span>
                  </div>
                  <div className="mt-3 flex items-start gap-2 rounded-xl border border-slate-100 bg-slate-50 px-3 py-2 text-xs font-semibold text-slate-600">
                    <MapPin className="mt-0.5 shrink-0 text-rose-500" size={14} />
                    <span>{item.location.label || '固定坐标'}：{item.location.x}, {item.location.y}, {item.location.z}</span>
                  </div>
                  <textarea value={notes[item.id] || ''} onChange={(event) => setNotes((current) => ({ ...current, [item.id]: event.target.value }))} placeholder="本次召唤备注（可选）" rows={2} className="mt-3 w-full resize-none rounded-xl border border-slate-200 px-3 py-2 text-xs font-semibold outline-none focus:border-rose-400" />
                  <div className="mt-3 flex flex-wrap justify-end gap-2">
                    <button type="button" onClick={() => openEdit(item)} className="inline-flex items-center gap-1.5 rounded-xl border border-slate-200 px-3 py-2 text-xs font-black text-slate-600 hover:bg-slate-50"><Pencil size={14} />编辑</button>
                    {!item.archived_at && <button type="button" onClick={() => archiveMutation.mutate(item)} disabled={archiveMutation.isPending} className="inline-flex items-center gap-1.5 rounded-xl border border-slate-200 px-3 py-2 text-xs font-black text-slate-600 hover:bg-slate-50 disabled:opacity-50"><Archive size={14} />归档</button>}
                    <button type="button" onClick={() => summonMutation.mutate(item)} disabled={!item.enabled || Boolean(item.archived_at) || unavailable || summonMutation.isPending || !execution?.available} className="inline-flex items-center gap-1.5 rounded-xl bg-rose-600 px-4 py-2 text-xs font-black text-white hover:bg-rose-700 disabled:cursor-not-allowed disabled:opacity-45">
                      {summonMutation.isPending ? <LoaderCircle className="animate-spin" size={14} /> : <Play size={14} />}立即召唤
                    </button>
                  </div>
                </article>
              );
            })}
          </div>
        )}
      </section>

      <section className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm">
        <h2 className="text-lg font-black text-slate-900">最近召唤记录</h2>
        <p className="mt-1 text-xs font-semibold text-slate-500">只显示固定坐标 Boss，不混入据点袭击记录。</p>
        <div className="mt-4 overflow-x-auto">
          <table className="min-w-full text-left text-xs">
            <thead><tr className="border-b border-slate-200 text-[11px] font-black uppercase tracking-wider text-slate-400"><th className="px-3 py-3">Boss</th><th className="px-3 py-3">坐标</th><th className="px-3 py-3">状态</th><th className="px-3 py-3">发起时间</th><th className="px-3 py-3">结果</th></tr></thead>
            <tbody>
              {summons.slice(0, 50).map((item) => (
                <tr key={item.id} className="border-b border-slate-100 last:border-0">
                  <td className="px-3 py-3"><strong className="block text-slate-800">{item.template_name}</strong><span className="text-[10px] font-mono text-slate-400">{item.id}</span></td>
                  <td className="px-3 py-3 font-mono text-slate-600">{item.location.x}, {item.location.y}, {item.location.z}</td>
                  <td className="px-3 py-3"><span className={`rounded-full border px-2 py-1 text-[10px] font-black ${statusClass(item.status)}`}>{summonStatusLabel[item.status]}</span></td>
                  <td className="px-3 py-3 text-slate-500">{formatTime(item.requested_at)}</td>
                  <td className="max-w-xs px-3 py-3 text-slate-500">{item.failure || (item.status === 'completed' ? '召唤命令已完成' : '—')}</td>
                </tr>
              ))}
              {summons.length === 0 && <tr><td colSpan={5} className="py-12 text-center text-xs font-bold text-slate-400">暂无 Boss 召唤记录</td></tr>}
            </tbody>
          </table>
        </div>
      </section>

      {editorOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/45 p-3 sm:p-6">
          <section className="flex max-h-[94vh] w-full max-w-6xl flex-col overflow-hidden rounded-3xl bg-white shadow-2xl">
            <header className="flex items-center justify-between border-b border-slate-200 px-5 py-4">
              <div><h2 className="text-lg font-black text-slate-900">{editingID ? '编辑 Boss' : '新建 Boss'}</h2><p className="mt-1 text-xs font-semibold text-slate-500">PalTemplate 必须从目录选择，不能手动输入文件名。</p></div>
              <button type="button" onClick={() => setEditorOpen(false)} className="rounded-xl border border-slate-200 p-2 text-slate-500 hover:bg-slate-50"><X size={17} /></button>
            </header>
            <div className="grid min-h-0 flex-1 overflow-hidden lg:grid-cols-[380px_minmax(0,1fr)]">
              <div className="overflow-y-auto border-b border-slate-200 p-5 lg:border-b-0 lg:border-r">
                <div className="grid gap-4">
                  <label><FieldLabel>Boss 名称</FieldLabel><input value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} maxLength={128} className="w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm font-semibold outline-none focus:border-rose-400" /></label>
                  <label><FieldLabel>说明</FieldLabel><textarea value={draft.description} onChange={(event) => setDraft((current) => ({ ...current, description: event.target.value }))} rows={3} maxLength={4096} className="w-full resize-none rounded-xl border border-slate-200 px-3 py-2.5 text-sm font-semibold outline-none focus:border-rose-400" /></label>
                  <div className="rounded-2xl border border-slate-200 bg-slate-50 p-3">
                    <FieldLabel>已选 PalTemplate</FieldLabel>
                    {selectedTemplate ? (
                      <div className="flex items-center gap-3"><PalIcon characterID={selectedTemplate.pal_id} name={templateDisplayName(selectedTemplate)} className="size-12 rounded-xl border border-slate-200" /><div className="min-w-0"><strong className="block truncate text-sm text-slate-900">{templateDisplayName(selectedTemplate)}</strong><span className="block truncate text-[10px] font-mono text-slate-500">{selectedTemplate.name}</span><span className="text-[10px] font-bold text-slate-400">{selectedTemplate.pal_id} · Lv.{selectedTemplate.level || 1}</span></div></div>
                    ) : <p className="text-xs font-bold text-rose-600">尚未选择有效模板</p>}
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <label><FieldLabel>召唤数量</FieldLabel><input type="number" min={1} max={50} value={draft.count} onChange={(event) => setDraft((current) => ({ ...current, count: Number(event.target.value) }))} className="w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm font-semibold outline-none focus:border-rose-400" /></label>
                    <label><FieldLabel>生成半径</FieldLabel><input type="number" min={0} max={10000} value={draft.spawnRadius} onChange={(event) => setDraft((current) => ({ ...current, spawnRadius: Number(event.target.value) }))} className="w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm font-semibold outline-none focus:border-rose-400" /></label>
                  </div>
                  <label><FieldLabel>冷却秒数</FieldLabel><input type="number" min={0} max={86400} value={draft.cooldownSeconds} onChange={(event) => setDraft((current) => ({ ...current, cooldownSeconds: Number(event.target.value) }))} className="w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm font-semibold outline-none focus:border-rose-400" /></label>
                  <div className="rounded-2xl border border-slate-200 p-3">
                    <FieldLabel>固定世界坐标</FieldLabel>
                    <div className="grid grid-cols-3 gap-2">
                      {(['x', 'y', 'z'] as const).map((key) => <label key={key}><span className="mb-1 block text-[10px] font-black uppercase text-slate-400">{key}</span><input type="number" value={draft.location[key]} onChange={(event) => setDraft((current) => ({ ...current, location: { ...current.location, [key]: Number(event.target.value) } }))} className="w-full rounded-xl border border-slate-200 px-2 py-2 text-xs font-semibold outline-none focus:border-rose-400" /></label>)}
                    </div>
                    <input value={draft.location.label} onChange={(event) => setDraft((current) => ({ ...current, location: { ...current.location, label: event.target.value } }))} placeholder="坐标标签，例如：雪山竞技场" maxLength={128} className="mt-2 w-full rounded-xl border border-slate-200 px-3 py-2 text-xs font-semibold outline-none focus:border-rose-400" />
                  </div>
                  <label className="flex items-center justify-between rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-black text-slate-600"><span>允许捕捉</span><input type="checkbox" checked={draft.capturable} onChange={(event) => setDraft((current) => ({ ...current, capturable: event.target.checked }))} className="size-4 rounded border-slate-300 text-rose-600 focus:ring-rose-500" /></label>
                  <label className="flex items-center justify-between rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-black text-slate-600"><span>启用定义</span><input type="checkbox" checked={draft.enabled} onChange={(event) => setDraft((current) => ({ ...current, enabled: event.target.checked }))} className="size-4 rounded border-slate-300 text-rose-600 focus:ring-rose-500" /></label>
                </div>
              </div>
              <div className="flex min-h-0 flex-col overflow-hidden p-5">
                <label className="relative mb-3"><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} /><input value={templateSearch} onChange={(event) => setTemplateSearch(event.target.value)} placeholder="搜索帕鲁名称、PalID、词条、分类或模板文件" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold outline-none focus:border-rose-400" /></label>
                <PalTemplateFilters templates={catalog} indexes={catalogQuery.data?.template_indexes || []} value={templateFilters} onChange={setTemplateFilters} />
                <div className="mt-3 min-h-0 flex-1 overflow-y-auto rounded-2xl border border-slate-200 p-2">
                  <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
                    {visibleCatalog.map((item) => {
                      const selected = draft.palTemplateFile === item.name;
                      return <button key={item.name} type="button" onClick={() => chooseTemplate(item)} className={`flex items-center gap-2 rounded-xl border p-2 text-left transition ${selected ? 'border-rose-400 bg-rose-50 ring-1 ring-rose-300' : 'border-slate-200 hover:bg-slate-50'}`}><PalIcon characterID={item.pal_id} name={templateDisplayName(item)} className="size-10 rounded-lg" /><span className="min-w-0"><strong className="block truncate text-xs text-slate-800">{templateDisplayName(item)}</strong><span className="block truncate text-[9px] font-mono text-slate-400">{item.name}</span><span className="block truncate text-[9px] font-bold text-slate-500">{item.category || '未分类'} · {item.overall_grade || '未分级'} · Lv.{item.level || 1}</span></span></button>;
                    })}
                  </div>
                  {visibleCatalog.length === 0 && <div className="py-12 text-center text-xs font-bold text-slate-400">没有符合条件的 PalTemplate</div>}
                </div>
              </div>
            </div>
            <footer className="flex items-center justify-end gap-2 border-t border-slate-200 px-5 py-4">
              <button type="button" onClick={() => setEditorOpen(false)} className="rounded-xl border border-slate-200 px-4 py-2.5 text-xs font-black text-slate-600">取消</button>
              <button type="button" onClick={() => saveMutation.mutate()} disabled={saveMutation.isPending || !selectedTemplate || !draft.name.trim()} className="inline-flex items-center gap-2 rounded-xl bg-rose-600 px-5 py-2.5 text-xs font-black text-white hover:bg-rose-700 disabled:opacity-45">
                {saveMutation.isPending && <LoaderCircle className="animate-spin" size={14} />}{editingID ? '保存修改' : '创建 Boss'}
              </button>
            </footer>
          </section>
        </div>
      )}
    </div>
  );
};
