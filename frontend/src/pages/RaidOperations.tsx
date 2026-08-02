import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  AlertTriangle,
  Castle,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clock3,
  Layers3,
  LoaderCircle,
  MapPin,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Save,
  Search,
  ShieldAlert,
  Trash2,
  X,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import {
  bossApi,
  type BossSummon,
  type BossTemplate,
  type BossTemplateInput,
  type BossWaveInput,
} from '../api/boss';
import { basesApi } from '../api/bases';
import { starterGiftApi, type PalTemplateInfo } from '../api/starterGift';
import { PalTemplateFilters } from '../components/gm/PalTemplateFilters';
import {
  createEmptyPalTemplateFilters,
  palTemplateMatchesFilters,
} from '../components/gm/palTemplateFilterModel';
import { PalIcon } from '../components/gm/PalIcon';

interface Notice {
  type: 'success' | 'error';
  text: string;
}

interface RaidSpawnGroupDraft {
  id: string;
  name: string;
  palTemplateFile: string;
  palID: string;
  palName: string;
  level: number;
  count: number;
  spawnRadius: number;
  capturable: boolean;
}

interface RaidWaveDraft {
  id: string;
  name: string;
  delaySeconds: number;
  groups: RaidSpawnGroupDraft[];
}

interface RaidTemplateDraft {
  name: string;
  description: string;
  baseID: string;
  cooldownSeconds: number;
  rewardID: string;
  enabled: boolean;
  metadata: Record<string, unknown>;
  waves: RaidWaveDraft[];
}

const id = (prefix: string) => {
  const uuid = globalThis.crypto?.randomUUID?.();
  return uuid ? `${prefix}-${uuid}` : `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
};

const newWave = (position: number): RaidWaveDraft => ({
  id: id('wave'),
  name: `第${position}波`,
  delaySeconds: position === 1 ? 0 : 30,
  groups: [],
});

const emptyDraft = (): RaidTemplateDraft => ({
  name: '',
  description: '',
  baseID: '',
  cooldownSeconds: 3600,
  rewardID: '',
  enabled: true,
  metadata: {},
  waves: [newWave(1)],
});

const asRecord = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};

const metadataString = (metadata: Record<string, unknown> | undefined, key: string) => {
  const value = metadata?.[key];
  return typeof value === 'string' ? value.trim() : '';
};

const metadataGroups = (metadata: Record<string, unknown> | undefined): RaidSpawnGroupDraft[] => {
  const raw = metadata?.spawn_groups;
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((value, index) => {
    const group = asRecord(value);
    const template = String(group.pal_template_file || group.template_file || '').trim();
    const palID = String(group.pal_id || '').trim();
    const level = Number(group.level || 0);
    const count = Number(group.count || 0);
    if (!template || !palID || level < 1 || count < 1) return [];
    return [{
      id: id(`group-${index + 1}`),
      name: String(group.name || '').trim() || palID,
      palTemplateFile: template,
      palID,
      palName: String(group.pal_name || group.name || palID),
      level,
      count,
      spawnRadius: Math.max(0, Number(group.spawn_radius || 0)),
      capturable: Boolean(group.capturable),
    }];
  });
};

const templateDisplayName = (template: PalTemplateInfo) =>
  template.pal_name?.trim() || template.nickname?.trim() || template.name;

const templateSearchText = (template: PalTemplateInfo) =>
  `${template.pal_name || ''} ${template.english_name || ''} ${template.pal_id || ''} ${template.nickname || ''} ${template.category || ''} ${template.usage_category || ''} ${template.overall_grade || ''} ${template.classification_tags.join(' ')} ${template.passive_names.join(' ')} ${template.index_names.join(' ')} ${template.name}`.toLowerCase();

const statusLabel = (status: BossSummon['status']) => ({
  pending: '等待执行',
  active: '袭击进行中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
}[status] || status);

const statusClass = (status: BossSummon['status']) => ({
  pending: 'bg-amber-50 text-amber-700',
  active: 'bg-sky-50 text-sky-700',
  completed: 'bg-emerald-50 text-emerald-700',
  failed: 'bg-rose-50 text-rose-700',
  cancelled: 'bg-slate-100 text-slate-600',
}[status] || 'bg-slate-100 text-slate-600');

const formatTime = (value?: string) => value
  ? new Date(value).toLocaleString('zh-CN', { hour12: false })
  : '—';

export const RaidOperations: React.FC = () => {
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<Notice | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingID, setEditingID] = useState('');
  const [draft, setDraft] = useState<RaidTemplateDraft>(emptyDraft);
  const [activeWave, setActiveWave] = useState(0);
  const [baseSearch, setBaseSearch] = useState('');
  const [templateSearch, setTemplateSearch] = useState('');
  const [templateFilters, setTemplateFilters] = useState(createEmptyPalTemplateFilters);
  const [launchingTemplateID, setLaunchingTemplateID] = useState('');

  const templatesQuery = useQuery({
    queryKey: ['raid', 'templates'],
    queryFn: () => bossApi.templates(false),
  });
  const rewardsQuery = useQuery({
    queryKey: ['raid', 'rewards'],
    queryFn: () => bossApi.rewards(false),
  });
  const summonsQuery = useQuery({
    queryKey: ['raid', 'summons'],
    queryFn: () => bossApi.summons(),
    refetchInterval: 10_000,
  });
  const executionStatusQuery = useQuery({
    queryKey: ['raid', 'execution-status'],
    queryFn: bossApi.executionStatus,
    refetchInterval: 15_000,
  });
  const basesQuery = useQuery({
    queryKey: ['raid', 'bases'],
    queryFn: () => basesApi.getBasesList({ limit: 500, offset: 0 }),
  });
  const catalogQuery = useQuery({
    queryKey: ['raid', 'pal-template-catalog'],
    queryFn: starterGiftApi.get,
    staleTime: 5 * 60 * 1000,
  });

  const templates = templatesQuery.data?.items || [];
  const rewards = rewardsQuery.data?.items || [];
  const summons = summonsQuery.data?.items || [];
  const bases = basesQuery.data?.items || [];
  const palTemplates = catalogQuery.data?.templates || [];
  const templateIndexes = catalogQuery.data?.template_indexes || [];

  const baseByID = useMemo(() => new Map(bases.map((base) => [base.id, base])), [bases]);
  const visibleBases = useMemo(() => {
    const query = baseSearch.trim().toLowerCase();
    return bases.filter((base) => !query || `${base.name} ${base.guild_name} ${base.id}`.toLowerCase().includes(query));
  }, [bases, baseSearch]);
  const visiblePalTemplates = useMemo(() => {
    const query = templateSearch.trim().toLowerCase();
    return palTemplates
      .filter((template) => palTemplateMatchesFilters(template, templateFilters))
      .filter((template) => !template.parse_error && template.pal_id && (template.level || 0) > 0)
      .filter((template) => !query || templateSearchText(template).includes(query))
      .sort((left, right) => templateDisplayName(left).localeCompare(templateDisplayName(right), 'zh-CN'))
      .slice(0, 300);
  }, [palTemplates, templateFilters, templateSearch]);

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['raid'] }),
      queryClient.invalidateQueries({ queryKey: ['boss'] }),
    ]);
  };

  const openCreate = () => {
    setEditingID('');
    setDraft(emptyDraft());
    setActiveWave(0);
    setEditorOpen(true);
  };

  const openEdit = async (template: BossTemplate) => {
    setNotice(null);
    try {
      const waves = await bossApi.templateWaves(template.id);
      const parsedWaves = waves.items.map((wave, index) => {
        const groups = metadataGroups(wave.metadata);
        const legacyTemplate = metadataString(wave.metadata, 'pal_template_file');
        if (groups.length === 0 && legacyTemplate && wave.pal_id && wave.level > 0) {
          groups.push({
            id: id('legacy-group'),
            name: wave.name,
            palTemplateFile: legacyTemplate,
            palID: wave.pal_id,
            palName: wave.pal_id,
            level: wave.level,
            count: wave.count,
            spawnRadius: wave.spawn_radius,
            capturable: wave.capturable,
          });
        }
        return {
          id: id(`wave-${index + 1}`),
          name: wave.name,
          delaySeconds: wave.delay_seconds,
          groups,
        };
      });
      setEditingID(template.id);
      setDraft({
        name: template.name,
        description: template.description || '',
        baseID: metadataString(template.metadata, 'raid_base_id'),
        cooldownSeconds: template.cooldown_seconds,
        rewardID: template.reward_id || '',
        enabled: template.enabled,
        metadata: template.metadata || {},
        waves: parsedWaves.length > 0 ? parsedWaves : [newWave(1)],
      });
      setActiveWave(0);
      setEditorOpen(true);
    } catch (error) {
      setNotice({ type: 'error', text: getErrorMessage(error) });
    }
  };

  const addWave = () => {
    if (draft.waves.length >= 20) return;
    setDraft((current) => ({ ...current, waves: [...current.waves, newWave(current.waves.length + 1)] }));
    setActiveWave(draft.waves.length);
  };

  const updateWave = (index: number, patch: Partial<RaidWaveDraft>) => {
    setDraft((current) => ({
      ...current,
      waves: current.waves.map((wave, waveIndex) => waveIndex === index ? { ...wave, ...patch } : wave),
    }));
  };

  const removeWave = (index: number) => {
    if (draft.waves.length <= 1) return;
    setDraft((current) => ({ ...current, waves: current.waves.filter((_, waveIndex) => waveIndex !== index) }));
    setActiveWave((current) => Math.max(0, Math.min(current, draft.waves.length - 2)));
  };

  const moveWave = (index: number, direction: -1 | 1) => {
    const target = index + direction;
    if (target < 0 || target >= draft.waves.length) return;
    setDraft((current) => {
      const waves = [...current.waves];
      [waves[index], waves[target]] = [waves[target], waves[index]];
      return { ...current, waves };
    });
    setActiveWave(target);
  };

  const addGroup = (template: PalTemplateInfo) => {
    if (!template.pal_id || !template.level) return;
    const group: RaidSpawnGroupDraft = {
      id: id('group'),
      name: templateDisplayName(template),
      palTemplateFile: template.name,
      palID: template.pal_id,
      palName: templateDisplayName(template),
      level: template.level,
      count: 1,
      spawnRadius: 300,
      capturable: false,
    };
    setDraft((current) => ({
      ...current,
      waves: current.waves.map((wave, index) => index === activeWave
        ? { ...wave, groups: [...wave.groups, group] }
        : wave),
    }));
  };

  const updateGroup = (groupID: string, patch: Partial<RaidSpawnGroupDraft>) => {
    setDraft((current) => ({
      ...current,
      waves: current.waves.map((wave, index) => index === activeWave
        ? { ...wave, groups: wave.groups.map((group) => group.id === groupID ? { ...group, ...patch } : group) }
        : wave),
    }));
  };

  const removeGroup = (groupID: string) => {
    setDraft((current) => ({
      ...current,
      waves: current.waves.map((wave, index) => index === activeWave
        ? { ...wave, groups: wave.groups.filter((group) => group.id !== groupID) }
        : wave),
    }));
  };

  const saveMutation = useMutation({
    mutationFn: async () => {
      const base = baseByID.get(draft.baseID);
      if (!draft.name.trim()) throw new Error('请输入袭击模板名称。');
      if (!base) throw new Error('请选择有效据点。');
      if (draft.waves.length === 0 || draft.waves.some((wave) => wave.groups.length === 0)) {
        throw new Error('每个波次至少需要一个帕鲁模板生成组。');
      }
      for (const wave of draft.waves) {
        const total = wave.groups.reduce((sum, group) => sum + Math.max(0, Math.trunc(group.count)), 0);
        if (wave.groups.length > 20 || total > 200) throw new Error(`“${wave.name}”超过每波 20 个生成组或 200 只帕鲁限制。`);
      }
      const first = draft.waves[0].groups[0];
      const metadata: Record<string, unknown> = {
        ...draft.metadata,
        activity_type: 'raid',
        raid_base_id: base.id,
        raid_base_name: base.name,
        raid_guild_id: base.guild_id,
        raid_guild_name: base.guild_name,
      };
      const input: BossTemplateInput = {
        name: draft.name.trim(),
        description: draft.description.trim(),
        pal_id: first.palID,
        level: first.level,
        count: first.count,
        hp_multiplier: 1,
        attack_multiplier: 1,
        defense_multiplier: 1,
        spawn_radius: first.spawnRadius,
        capturable: first.capturable,
        cooldown_seconds: Math.max(0, Math.trunc(draft.cooldownSeconds || 0)),
        reward_id: draft.rewardID,
        location: { x: base.x, y: base.y, z: base.z, label: base.name },
        enabled: draft.enabled,
        metadata,
      };
      const saved = editingID
        ? await bossApi.updateTemplate(editingID, input)
        : await bossApi.createTemplate(input);
      const waves: BossWaveInput[] = draft.waves.map((wave, index) => {
        const primary = wave.groups[0];
        return {
          name: wave.name.trim() || `第${index + 1}波`,
          kind: index === 0 ? 'main' : 'reinforcement',
          pal_id: primary.palID,
          level: primary.level,
          count: primary.count,
          hp_multiplier: 1,
          attack_multiplier: 1,
          defense_multiplier: 1,
          spawn_radius: primary.spawnRadius,
          delay_seconds: index === 0 ? 0 : Math.max(0, Math.trunc(wave.delaySeconds || 0)),
          capturable: primary.capturable,
          metadata: {
            raid_base_id: base.id,
            raid_base_name: base.name,
            spawn_groups: wave.groups.map((group) => ({
              name: group.name,
              pal_template_file: group.palTemplateFile,
              pal_id: group.palID,
              pal_name: group.palName,
              level: Math.max(1, Math.trunc(group.level)),
              count: Math.max(1, Math.min(100, Math.trunc(group.count))),
              spawn_radius: Math.max(0, Number(group.spawnRadius) || 0),
              capturable: group.capturable,
            })),
          },
        };
      });
      await bossApi.replaceTemplateWaves(saved.id, { waves });
      return saved;
    },
    onSuccess: async (template) => {
      setNotice({ type: 'success', text: `袭击模板“${template.name}”已保存。` });
      setEditorOpen(false);
      setEditingID('');
      setDraft(emptyDraft());
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const launchMutation = useMutation({
    mutationFn: async (template: BossTemplate) => {
      const baseID = metadataString(template.metadata, 'raid_base_id');
      if (!baseID || !baseByID.has(baseID)) throw new Error('该袭击模板未绑定当前有效据点，请先编辑模板。');
      setLaunchingTemplateID(template.id);
      const created = await bossApi.createSummon({
        template_id: template.id,
        request_key: id('raid'),
        notes: '袭击管理手动启动',
        metadata: { activity_type: 'raid', raid_base_id: baseID, auto_execute: true },
      });
      return created;
    },
    onSuccess: async (created) => {
      setNotice({
        type: 'success',
        text: `袭击“${created.summon.template_name}”已进入自动执行队列，首波将在调度器下一轮扫描时启动。`,
      });
      setLaunchingTemplateID('');
      await refresh();
    },
    onError: (error) => {
      setLaunchingTemplateID('');
      setNotice({ type: 'error', text: getErrorMessage(error) });
    },
  });

  const executeMutation = useMutation({
    mutationFn: (summon: BossSummon) => bossApi.executeNextWave(summon.id),
    onSuccess: async (attempt) => {
      setNotice({
        type: attempt.status === 'succeeded' ? 'success' : 'error',
        text: attempt.status === 'succeeded'
          ? `第 ${attempt.wave_position} 波已执行，完成 ${attempt.completed_commands}/${attempt.command_count} 条命令。`
          : `波次执行状态：${attempt.status}。${attempt.failure || ''}`,
      });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const activeWaveDraft = draft.waves[activeWave];
  const configuredTemplates = templates.filter((template) => metadataString(template.metadata, 'raid_base_id'));
  const unboundTemplates = templates.length - configuredTemplates.length;
  const activeRuns = summons.filter((summon) => summon.status === 'pending' || summon.status === 'active');

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <header className="flex flex-col gap-4 rounded-3xl border border-slate-200 bg-white p-5 shadow-sm lg:flex-row lg:items-center lg:justify-between">
        <div>
          <div className="flex items-center gap-2 text-xs font-black uppercase tracking-[0.16em] text-rose-500"><ShieldAlert size={15} /> Raid Operations</div>
          <h1 className="mt-2 text-2xl font-black text-slate-900">袭击管理</h1>
          <p className="mt-1 text-sm font-medium text-slate-500">袭击绑定服务器据点；每个波次可以同时生成多种 PalTemplate。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <a href="/raid-legacy" className="rounded-xl border border-slate-200 bg-white px-4 py-2 text-xs font-black text-slate-600 hover:bg-slate-50">奖励与定时计划</a>
          <button type="button" onClick={() => void refresh()} className="flex items-center gap-2 rounded-xl border border-slate-200 px-4 py-2 text-xs font-black text-slate-600"><RefreshCw size={14} />刷新</button>
          <button type="button" onClick={openCreate} className="flex items-center gap-2 rounded-xl bg-rose-600 px-4 py-2 text-xs font-black text-white"><Plus size={14} />新建袭击</button>
        </div>
      </header>

      {notice && (
        <div className={`rounded-2xl border px-4 py-3 text-xs font-bold ${notice.type === 'success' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-rose-200 bg-rose-50 text-rose-700'}`}>
          {notice.type === 'success' ? <CheckCircle2 size={15} className="mr-2 inline" /> : <AlertTriangle size={15} className="mr-2 inline" />}
          {notice.text}
        </div>
      )}

      <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Metric icon={<Castle size={18} />} label="可用据点" value={bases.length} detail="来自当前存档索引" />
        <Metric icon={<Layers3 size={18} />} label="袭击模板" value={configuredTemplates.length} detail={unboundTemplates > 0 ? `${unboundTemplates} 个旧模板待绑定` : '全部已绑定据点'} />
        <Metric icon={<ShieldAlert size={18} />} label="活动袭击" value={activeRuns.length} detail="等待或执行中" />
        <Metric icon={<Clock3 size={18} />} label="执行器" value={executionStatusQuery.data?.available ? 1 : 0} detail={executionStatusQuery.data?.message || '正在读取状态'} />
      </section>

      <section className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="mb-4 flex items-center justify-between gap-3">
          <div><h2 className="text-lg font-black text-slate-900">袭击模板</h2><p className="text-xs font-semibold text-slate-500">旧 Boss 模板会显示在这里，但必须绑定据点并配置 PalTemplate 生成组后才能启动。</p></div>
        </div>
        {templatesQuery.isLoading ? (
          <div className="py-12 text-center text-sm font-semibold text-slate-400"><LoaderCircle size={18} className="mr-2 inline animate-spin" />正在加载</div>
        ) : templates.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-slate-300 py-12 text-center text-sm font-semibold text-slate-400">暂无袭击模板</div>
        ) : (
          <div className="grid gap-3 lg:grid-cols-2 2xl:grid-cols-3">
            {templates.map((template) => {
              const baseID = metadataString(template.metadata, 'raid_base_id');
              const base = baseByID.get(baseID);
              return (
                <article key={template.id} className="rounded-2xl border border-slate-200 p-4">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0"><h3 className="truncate text-sm font-black text-slate-900">{template.name}</h3><p className="mt-1 line-clamp-2 text-xs font-medium text-slate-500">{template.description || '未填写说明'}</p></div>
                    <span className={`shrink-0 rounded-full px-2.5 py-1 text-[10px] font-black ${template.enabled ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{template.enabled ? '启用' : '停用'}</span>
                  </div>
                  <div className={`mt-3 rounded-xl border px-3 py-2 text-xs font-bold ${base ? 'border-sky-100 bg-sky-50 text-sky-700' : 'border-amber-200 bg-amber-50 text-amber-700'}`}>
                    <MapPin size={13} className="mr-1.5 inline" />{base ? `${base.name} · ${base.guild_name || '无公会名'}` : '未绑定当前有效据点'}
                  </div>
                  <div className="mt-4 flex gap-2">
                    <button type="button" onClick={() => void openEdit(template)} className="flex flex-1 items-center justify-center gap-1.5 rounded-xl border border-slate-200 px-3 py-2 text-xs font-black text-slate-600"><Pencil size={13} />编辑</button>
                    <button type="button" disabled={!template.enabled || !base || launchMutation.isPending} onClick={() => launchMutation.mutate(template)} className="flex flex-1 items-center justify-center gap-1.5 rounded-xl bg-rose-600 px-3 py-2 text-xs font-black text-white disabled:cursor-not-allowed disabled:opacity-40">
                      {launchingTemplateID === template.id ? <LoaderCircle size={13} className="animate-spin" /> : <Play size={13} />}启动
                    </button>
                  </div>
                </article>
              );
            })}
          </div>
        )}
      </section>

      <section className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm">
        <h2 className="text-lg font-black text-slate-900">袭击运行记录</h2>
        <div className="mt-4 space-y-2">
          {summons.slice(0, 30).map((summon) => (
            <div key={summon.id} className="flex flex-col gap-3 rounded-2xl border border-slate-200 p-4 md:flex-row md:items-center md:justify-between">
              <div className="min-w-0"><div className="flex items-center gap-2"><strong className="truncate text-sm text-slate-900">{summon.template_name}</strong><span className={`rounded-full px-2 py-0.5 text-[10px] font-black ${statusClass(summon.status)}`}>{statusLabel(summon.status)}</span></div><p className="mt-1 text-[11px] font-semibold text-slate-400">{formatTime(summon.requested_at)} · {summon.id}</p></div>
              {(summon.status === 'pending' || summon.status === 'active') && (
                <button type="button" disabled={executeMutation.isPending} onClick={() => executeMutation.mutate(summon)} className="flex items-center justify-center gap-1.5 rounded-xl border border-rose-200 bg-rose-50 px-4 py-2 text-xs font-black text-rose-700 disabled:opacity-50"><Play size={13} />执行下一波</button>
              )}
            </div>
          ))}
          {summons.length === 0 && <div className="py-10 text-center text-xs font-semibold text-slate-400">暂无运行记录</div>}
        </div>
      </section>

      {editorOpen && (
        <div className="fixed inset-0 z-50 overflow-y-auto bg-slate-950/45 p-3 sm:p-6">
          <div className="mx-auto max-w-7xl rounded-3xl bg-white shadow-2xl">
            <div className="sticky top-0 z-10 flex items-center justify-between rounded-t-3xl border-b border-slate-200 bg-white px-5 py-4">
              <div><h2 className="text-lg font-black text-slate-900">{editingID ? '编辑袭击模板' : '新建袭击模板'}</h2><p className="text-xs font-semibold text-slate-500">据点为必选项；坐标只保存快照，执行时会重新解析据点。</p></div>
              <button type="button" onClick={() => setEditorOpen(false)} className="rounded-xl border border-slate-200 p-2 text-slate-500"><X size={17} /></button>
            </div>

            <div className="grid gap-6 p-5 xl:grid-cols-[360px_minmax(0,1fr)]">
              <aside className="space-y-5">
                <EditorField label="袭击名称"><input value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} className="w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm" maxLength={128} /></EditorField>
                <EditorField label="说明"><textarea value={draft.description} onChange={(event) => setDraft({ ...draft, description: event.target.value })} className="min-h-24 w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm" maxLength={4096} /></EditorField>
                <EditorField label="绑定据点">
                  <div className="rounded-2xl border border-slate-200 p-3">
                    <div className="relative"><Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" /><input value={baseSearch} onChange={(event) => setBaseSearch(event.target.value)} placeholder="搜索据点或公会" className="w-full rounded-xl border border-slate-200 py-2 pl-9 pr-3 text-xs" /></div>
                    <div className="mt-2 max-h-64 space-y-1 overflow-y-auto">
                      {visibleBases.map((base) => (
                        <button key={base.id} type="button" onClick={() => setDraft({ ...draft, baseID: base.id })} className={`w-full rounded-xl border px-3 py-2 text-left ${draft.baseID === base.id ? 'border-sky-400 bg-sky-50' : 'border-transparent hover:bg-slate-50'}`}>
                          <strong className="block truncate text-xs text-slate-800">{base.name}</strong><span className="mt-0.5 block truncate text-[10px] font-semibold text-slate-400">{base.guild_name || '无公会名'} · {base.id}</span>
                        </button>
                      ))}
                    </div>
                  </div>
                </EditorField>
                <EditorField label="奖励方案"><select value={draft.rewardID} onChange={(event) => setDraft({ ...draft, rewardID: event.target.value })} className="w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm"><option value="">不结算奖励</option>{rewards.filter((reward) => reward.enabled).map((reward) => <option key={reward.id} value={reward.id}>{reward.name}</option>)}</select></EditorField>
                <EditorField label="冷却秒数"><input type="number" min={0} value={draft.cooldownSeconds} onChange={(event) => setDraft({ ...draft, cooldownSeconds: Number(event.target.value) })} className="w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm" /></EditorField>
                <label className="flex items-center gap-2 text-xs font-bold text-slate-600"><input type="checkbox" checked={draft.enabled} onChange={(event) => setDraft({ ...draft, enabled: event.target.checked })} />启用此袭击模板</label>
              </aside>

              <main className="min-w-0 space-y-5">
                <section className="rounded-2xl border border-slate-200 p-4">
                  <div className="flex flex-wrap items-center gap-2">
                    {draft.waves.map((wave, index) => (
                      <button key={wave.id} type="button" onClick={() => setActiveWave(index)} className={`rounded-xl px-3 py-2 text-xs font-black ${activeWave === index ? 'bg-rose-600 text-white' : 'border border-slate-200 text-slate-600'}`}>第{index + 1}波 · {wave.groups.length}组</button>
                    ))}
                    <button type="button" onClick={addWave} disabled={draft.waves.length >= 20} className="rounded-xl border border-dashed border-slate-300 px-3 py-2 text-xs font-black text-slate-500 disabled:opacity-40"><Plus size={13} className="mr-1 inline" />加波次</button>
                  </div>
                  {activeWaveDraft && (
                    <div className="mt-4 grid gap-3 sm:grid-cols-[1fr_180px_auto]">
                      <EditorField label="波次名称"><input value={activeWaveDraft.name} onChange={(event) => updateWave(activeWave, { name: event.target.value })} className="w-full rounded-xl border border-slate-200 px-3 py-2 text-sm" /></EditorField>
                      <EditorField label="与上一波间隔（秒）"><input type="number" min={0} disabled={activeWave === 0} value={activeWaveDraft.delaySeconds} onChange={(event) => updateWave(activeWave, { delaySeconds: Number(event.target.value) })} className="w-full rounded-xl border border-slate-200 px-3 py-2 text-sm disabled:bg-slate-50" /></EditorField>
                      <div className="flex items-end gap-1"><button type="button" onClick={() => moveWave(activeWave, -1)} disabled={activeWave === 0} className="rounded-xl border border-slate-200 p-2.5 disabled:opacity-30"><ChevronUp size={15} /></button><button type="button" onClick={() => moveWave(activeWave, 1)} disabled={activeWave === draft.waves.length - 1} className="rounded-xl border border-slate-200 p-2.5 disabled:opacity-30"><ChevronDown size={15} /></button><button type="button" onClick={() => removeWave(activeWave)} disabled={draft.waves.length <= 1} className="rounded-xl border border-rose-200 p-2.5 text-rose-600 disabled:opacity-30"><Trash2 size={15} /></button></div>
                    </div>
                  )}
                </section>

                <section className="rounded-2xl border border-slate-200 p-4">
                  <div className="mb-3 flex items-center justify-between"><div><h3 className="text-sm font-black text-slate-900">当前波次生成组</h3><p className="text-[11px] font-semibold text-slate-500">同一波次中的所有组会依次生成，但属于同一袭击波次。</p></div><span className="text-xs font-black text-slate-400">{activeWaveDraft?.groups.reduce((sum, group) => sum + group.count, 0) || 0} 只</span></div>
                  <div className="space-y-2">
                    {activeWaveDraft?.groups.map((group) => (
                      <div key={group.id} className="grid gap-2 rounded-2xl border border-slate-200 p-3 lg:grid-cols-[minmax(180px,1fr)_90px_120px_130px_auto] lg:items-end">
                        <div className="min-w-0"><span className="mb-1 block text-[10px] font-black text-slate-400">PalTemplate</span><strong className="block truncate text-xs text-slate-800">{group.palName}</strong><span className="block truncate text-[10px] font-semibold text-slate-400">{group.palTemplateFile} · Lv.{group.level}</span></div>
                        <EditorField label="数量"><input type="number" min={1} max={100} value={group.count} onChange={(event) => updateGroup(group.id, { count: Math.max(1, Number(event.target.value)) })} className="w-full rounded-xl border border-slate-200 px-2 py-2 text-xs" /></EditorField>
                        <EditorField label="生成半径"><input type="number" min={0} value={group.spawnRadius} onChange={(event) => updateGroup(group.id, { spawnRadius: Math.max(0, Number(event.target.value)) })} className="w-full rounded-xl border border-slate-200 px-2 py-2 text-xs" /></EditorField>
                        <label className="flex h-9 items-center gap-2 text-xs font-bold text-slate-600"><input type="checkbox" checked={group.capturable} onChange={(event) => updateGroup(group.id, { capturable: event.target.checked })} />允许捕捉</label>
                        <button type="button" onClick={() => removeGroup(group.id)} className="rounded-xl border border-rose-200 p-2.5 text-rose-600"><Trash2 size={14} /></button>
                      </div>
                    ))}
                    {activeWaveDraft?.groups.length === 0 && <div className="rounded-2xl border border-dashed border-slate-300 py-8 text-center text-xs font-semibold text-slate-400">从下方模板目录添加本波次的帕鲁</div>}
                  </div>
                </section>

                <section className="rounded-2xl border border-slate-200 p-4">
                  <div className="relative mb-3"><Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" /><input value={templateSearch} onChange={(event) => setTemplateSearch(event.target.value)} placeholder="搜索中文名、PalID、模板名、用途或词条" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-xs" /></div>
                  <PalTemplateFilters templates={palTemplates} indexes={templateIndexes} value={templateFilters} onChange={setTemplateFilters} />
                  {catalogQuery.error && <div className="mt-3 rounded-xl border border-rose-200 bg-rose-50 px-3 py-2 text-xs font-bold text-rose-700">模板目录读取失败：{getErrorMessage(catalogQuery.error)}</div>}
                  <div className="mt-3 grid max-h-[520px] gap-2 overflow-y-auto sm:grid-cols-2 2xl:grid-cols-3">
                    {visiblePalTemplates.map((template) => (
                      <button key={template.name} type="button" onClick={() => addGroup(template)} className="flex min-w-0 items-center gap-3 rounded-2xl border border-slate-200 p-3 text-left hover:border-rose-300 hover:bg-rose-50/40">
                        <PalIcon characterID={template.pal_id || ''} name={templateDisplayName(template)} className="size-11 rounded-xl" />
                        <span className="min-w-0 flex-1"><strong className="block truncate text-xs text-slate-800">{templateDisplayName(template)}</strong><span className="mt-0.5 block truncate text-[10px] font-semibold text-slate-400">{template.name} · Lv.{template.level}</span><span className="mt-1 block truncate text-[10px] font-bold text-violet-600">{template.category || '未分类'} · {template.overall_grade || '未分级'}</span></span>
                        <Plus size={15} className="shrink-0 text-rose-500" />
                      </button>
                    ))}
                  </div>
                </section>
              </main>
            </div>

            <div className="sticky bottom-0 flex items-center justify-end gap-2 rounded-b-3xl border-t border-slate-200 bg-white px-5 py-4">
              <button type="button" onClick={() => setEditorOpen(false)} className="rounded-xl border border-slate-200 px-4 py-2.5 text-xs font-black text-slate-600">取消</button>
              <button type="button" disabled={saveMutation.isPending} onClick={() => saveMutation.mutate()} className="flex items-center gap-2 rounded-xl bg-rose-600 px-5 py-2.5 text-xs font-black text-white disabled:opacity-50">{saveMutation.isPending ? <LoaderCircle size={14} className="animate-spin" /> : <Save size={14} />}保存袭击</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

const EditorField: React.FC<React.PropsWithChildren<{ label: string }>> = ({ label, children }) => (
  <label className="block min-w-0"><span className="mb-1 block text-[10px] font-black text-slate-500">{label}</span>{children}</label>
);

const Metric: React.FC<{ icon: React.ReactNode; label: string; value: number; detail: string }> = ({ icon, label, value, detail }) => (
  <div className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm"><div className="flex items-center justify-between text-slate-400"><span className="text-[10px] font-black uppercase tracking-[0.14em]">{label}</span><span className="text-rose-500">{icon}</span></div><strong className="mt-3 block text-2xl font-black text-slate-900">{value}</strong><span className="mt-1 block truncate text-[11px] font-semibold text-slate-500">{detail}</span></div>
);
