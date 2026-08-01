import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Archive,
  ArrowDown,
  ArrowUp,
  BellRing,
  CalendarClock,
  CheckCircle2,
  CircleAlert,
  Clock3,
  Crown,
  Gift,
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
  Skull,
  Sword,
  X,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import {
  bossApi,
  type BossReward,
  type BossRewardInput,
  type BossRewardItem,
  type BossRunDueResult,
  type BossSchedule,
  type BossScheduleEvent,
  type BossScheduleInput,
  type BossSummon,
  type BossSummonEvent,
  type BossSummonStatus,
  type BossTemplate,
  type BossTemplateInput,
  type BossWaveInput,
  type BossWaveStatus,
  type BossSummonWave,
} from '../api/boss';
import { palDefenderGMApi } from '../api/paldefenderGM';

interface Notice {
  type: 'success' | 'error';
  text: string;
}

type BossTab = 'templates' | 'rewards' | 'schedules' | 'summons';
type BossWaveActionStatus = Exclude<BossWaveStatus, 'pending'>;

const number = new Intl.NumberFormat('zh-CN');

const summonStatusLabel: Record<BossSummonStatus, string> = {
  pending: '等待处理',
  active: '进行中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
};

const summonStatusClass: Record<BossSummonStatus, string> = {
  pending: 'bg-amber-50 text-amber-700',
  active: 'bg-sky-50 text-sky-700',
  completed: 'bg-emerald-50 text-emerald-700',
  failed: 'bg-rose-50 text-rose-700',
  cancelled: 'bg-slate-100 text-slate-600',
};

const waveKindLabel: Record<BossWaveInput['kind'], string> = {
  main: '主Boss',
  minion: '护卫怪',
  reinforcement: '增援',
};

const waveStatusLabel: Record<BossWaveStatus, string> = {
  pending: '等待执行',
  active: '进行中',
  completed: '已完成',
  failed: '失败',
  skipped: '已跳过',
};

const waveStatusClass: Record<BossWaveStatus, string> = {
  pending: 'bg-amber-50 text-amber-700',
  active: 'bg-sky-50 text-sky-700',
  completed: 'bg-emerald-50 text-emerald-700',
  failed: 'bg-rose-50 text-rose-700',
  skipped: 'bg-slate-100 text-slate-600',
};

const scheduleEventTypeLabel: Record<BossScheduleEvent['event_type'], string> = {
  warning: '提前预警',
  summon: '创建召唤',
};

const scheduleEventStatusLabel: Record<BossScheduleEvent['status'], string> = {
  success: '成功',
  failed: '失败',
  skipped: '已跳过',
};

const emptyRewardInput = (): BossRewardInput => ({
  name: '',
  description: '',
  points: 0,
  items: [],
  pal_templates: [],
  enabled: true,
  metadata: {},
});

const emptyScheduleInput = (): BossScheduleInput => ({
  name: '',
  template_id: '',
  mode: 'daily',
  daily_time: '20:00',
  cron: '0 20 * * 6',
  timezone: 'Asia/Shanghai',
  warning_minutes: 30,
  warning_title: 'Boss活动即将开始',
  warning_message: '{{boss}}将在{{minutes}}分钟后开始，请提前前往活动区域。',
  enabled: true,
  metadata: {},
});

const emptyTemplateInput = (): BossTemplateInput => ({
  name: '',
  description: '',
  pal_id: '',
  level: 50,
  count: 1,
  hp_multiplier: 1,
  attack_multiplier: 1,
  defense_multiplier: 1,
  spawn_radius: 500,
  capturable: false,
  cooldown_seconds: 3600,
  reward_id: '',
  location: { x: 0, y: 0, z: 0, label: '' },
  enabled: true,
  metadata: {},
});

const waveFromTemplate = (template: BossTemplate, position: number): BossWaveInput => ({
  name: position === 1 ? template.name : `第${position}波`,
  kind: position === 1 ? 'main' : 'reinforcement',
  pal_id: template.pal_id,
  level: template.level,
  count: template.count,
  hp_multiplier: template.hp_multiplier,
  attack_multiplier: template.attack_multiplier,
  defense_multiplier: template.defense_multiplier,
  spawn_radius: template.spawn_radius,
  delay_seconds: position === 1 ? 0 : 30,
  capturable: template.capturable,
  metadata: {},
});

const newRequestKey = () => {
  const uuid = globalThis.crypto?.randomUUID?.();
  return uuid ? `boss-${uuid}` : `boss-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
};

const FieldLabel: React.FC<React.PropsWithChildren> = ({ children }) => (
  <span className="mb-1.5 block text-xs font-bold text-slate-500">{children}</span>
);

const Metric: React.FC<{ label: string; value: number; icon: React.ReactNode; suffix?: string }> = ({ label, value, icon, suffix }) => (
  <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
    <div className="mb-4 flex items-center justify-between text-slate-400">
      <span className="text-xs font-bold uppercase tracking-wider">{label}</span>
      <span className="rounded-lg bg-rose-50 p-2 text-rose-500">{icon}</span>
    </div>
    <div className="text-2xl font-black tracking-tight text-slate-900">
      {number.format(value)}{suffix && <span className="ml-1 text-sm text-slate-400">{suffix}</span>}
    </div>
  </div>
);

const EnabledBadge: React.FC<{ enabled: boolean; archived?: string }> = ({ enabled, archived }) => {
  if (archived) return <span className="rounded-full bg-slate-100 px-2.5 py-1 text-[11px] font-bold text-slate-500">已归档</span>;
  return enabled
    ? <span className="rounded-full bg-emerald-50 px-2.5 py-1 text-[11px] font-bold text-emerald-700">已启用</span>
    : <span className="rounded-full bg-amber-50 px-2.5 py-1 text-[11px] font-bold text-amber-700">已停用</span>;
};

export const BossOperations: React.FC = () => {
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<BossTab>('templates');
  const [includeArchived, setIncludeArchived] = useState(false);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [rewardEditorOpen, setRewardEditorOpen] = useState(false);
  const [rewardEditingID, setRewardEditingID] = useState('');
  const [rewardDraft, setRewardDraft] = useState<BossRewardInput>(emptyRewardInput());
  const [templateEditorOpen, setTemplateEditorOpen] = useState(false);
  const [templateEditingID, setTemplateEditingID] = useState('');
  const [templateDraft, setTemplateDraft] = useState<BossTemplateInput>(emptyTemplateInput());
  const [summonTemplateID, setSummonTemplateID] = useState('');
  const [summonNotes, setSummonNotes] = useState('');
  const [overrideLocation, setOverrideLocation] = useState(false);
  const [summonLocation, setSummonLocation] = useState({ x: 0, y: 0, z: 0, label: '' });
  const [summonStatus, setSummonStatus] = useState<BossSummonStatus | ''>('');
  const [selectedSummonID, setSelectedSummonID] = useState('');
  const [itemSearch, setItemSearch] = useState('');
  const [palSearch, setPalSearch] = useState('');
  const [templateSearch, setTemplateSearch] = useState('');
  const [waveEditorTemplate, setWaveEditorTemplate] = useState<BossTemplate | null>(null);
  const [waveDraft, setWaveDraft] = useState<BossWaveInput[]>([]);
  const [waveLoading, setWaveLoading] = useState(false);
  const [wavePalSearch, setWavePalSearch] = useState('');
  const [scheduleEditingID, setScheduleEditingID] = useState('');
  const [scheduleDraft, setScheduleDraft] = useState<BossScheduleInput>(emptyScheduleInput());
  const [selectedScheduleID, setSelectedScheduleID] = useState('');

  const summaryQuery = useQuery({ queryKey: ['boss', 'summary'], queryFn: bossApi.summary });
  const rewardsQuery = useQuery({
    queryKey: ['boss', 'rewards', includeArchived],
    queryFn: () => bossApi.rewards(includeArchived),
  });
  const templatesQuery = useQuery({
    queryKey: ['boss', 'templates', includeArchived],
    queryFn: () => bossApi.templates(includeArchived),
  });
  const summonsQuery = useQuery({
    queryKey: ['boss', 'summons', summonStatus],
    queryFn: () => bossApi.summons(summonStatus),
  });
  const schedulesQuery = useQuery({
    queryKey: ['boss', 'schedules', includeArchived],
    queryFn: () => bossApi.schedules(includeArchived),
  });
  const scheduleEventsQuery = useQuery({
    queryKey: ['boss', 'schedule-events', selectedScheduleID],
    queryFn: () => bossApi.scheduleEvents(selectedScheduleID),
  });
  const summonEventsQuery = useQuery({
    queryKey: ['boss', 'summon-events', selectedSummonID],
    queryFn: () => bossApi.summonEvents(selectedSummonID),
    enabled: Boolean(selectedSummonID),
  });

  const summonWavesQuery = useQuery({
    queryKey: ['boss', 'summon-waves', selectedSummonID],
    queryFn: () => bossApi.summonWaves(selectedSummonID),
    enabled: Boolean(selectedSummonID),
  });

  const itemCatalogQuery = useQuery({
    queryKey: ['boss', 'catalog', 'items'],
    queryFn: () => palDefenderGMApi.items('', 5000),
    staleTime: 30 * 60 * 1000,
  });
  const palCatalogQuery = useQuery({
    queryKey: ['boss', 'catalog', 'pals'],
    queryFn: () => palDefenderGMApi.palCatalog('', 5000),
    staleTime: 30 * 60 * 1000,
  });
  const palTemplatesQuery = useQuery({
    queryKey: ['boss', 'catalog', 'pal-templates'],
    queryFn: palDefenderGMApi.templates,
    staleTime: 5 * 60 * 1000,
  });

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['boss'] });
  };

  const saveRewardMutation = useMutation({
    mutationFn: () => rewardEditingID ? bossApi.updateReward(rewardEditingID, rewardDraft) : bossApi.createReward(rewardDraft),
    onSuccess: async (reward) => {
      setNotice({ type: 'success', text: `奖励“${reward.name}”已${rewardEditingID ? '更新' : '创建'}。` });
      setRewardEditorOpen(false);
      setRewardEditingID('');
      setRewardDraft(emptyRewardInput());
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const archiveRewardMutation = useMutation({
    mutationFn: (reward: BossReward) => bossApi.archiveReward(reward.id),
    onSuccess: async (reward) => {
      setNotice({ type: 'success', text: `奖励“${reward.name}”已归档。` });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const saveTemplateMutation = useMutation({
    mutationFn: () => templateEditingID ? bossApi.updateTemplate(templateEditingID, templateDraft) : bossApi.createTemplate(templateDraft),
    onSuccess: async (template) => {
      setNotice({ type: 'success', text: `Boss模板“${template.name}”已${templateEditingID ? '更新' : '创建'}。` });
      setTemplateEditorOpen(false);
      setTemplateEditingID('');
      setTemplateDraft(emptyTemplateInput());
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const archiveTemplateMutation = useMutation({
    mutationFn: (template: BossTemplate) => bossApi.archiveTemplate(template.id),
    onSuccess: async (template) => {
      setNotice({ type: 'success', text: `Boss模板“${template.name}”已归档。` });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const saveScheduleMutation = useMutation({
    mutationFn: () => scheduleEditingID
      ? bossApi.updateSchedule(scheduleEditingID, scheduleDraft)
      : bossApi.createSchedule(scheduleDraft),
    onSuccess: async (schedule) => {
      setNotice({ type: 'success', text: `定时计划“${schedule.name}”已${scheduleEditingID ? '更新' : '创建'}。` });
      setScheduleEditingID('');
      setScheduleDraft(emptyScheduleInput());
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const archiveScheduleMutation = useMutation({
    mutationFn: (schedule: BossSchedule) => bossApi.archiveSchedule(schedule.id),
    onSuccess: async (schedule) => {
      setNotice({ type: 'success', text: `定时计划“${schedule.name}”已归档。` });
      if (selectedScheduleID === schedule.id) setSelectedScheduleID('');
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const runScheduleMutation = useMutation({
    mutationFn: (schedule: BossSchedule) => bossApi.runScheduleNow(schedule.id),
    onSuccess: async (result) => {
      setNotice({ type: 'success', text: `已从计划“${result.summon.template_name}”创建立即召唤记录。` });
      await refresh();
      await scheduleEventsQuery.refetch();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const runDueMutation = useMutation<BossRunDueResult>({
    mutationFn: bossApi.runDueSchedules,
    onSuccess: async (result) => {
      setNotice({ type: result.failed > 0 ? 'error' : 'success', text: `定时扫描完成：检查 ${result.checked}，预警 ${result.warnings}，召唤 ${result.summons}，失败 ${result.failed}。` });
      await refresh();
      await scheduleEventsQuery.refetch();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const saveWavesMutation = useMutation({
    mutationFn: () => {
      if (!waveEditorTemplate) throw new Error('未选择Boss模板');
      return bossApi.replaceTemplateWaves(waveEditorTemplate.id, { waves: waveDraft });
    },
    onSuccess: async (result) => {
      setNotice({ type: 'success', text: `已保存 ${result.count} 个波次。新召唤记录会保存当前波次快照。` });
      setWaveEditorTemplate(null);
      setWaveDraft([]);
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const createSummonMutation = useMutation({
    mutationFn: () => bossApi.createSummon({
      template_id: summonTemplateID,
      request_key: newRequestKey(),
      notes: summonNotes.trim(),
      metadata: {},
      ...(overrideLocation ? { location_override: summonLocation } : {}),
    }),
    onSuccess: async (result) => {
      setNotice({
        type: 'success',
        text: result.duplicate ? `检测到重复请求，已返回原召唤记录。` : `已创建“${result.summon.template_name}”手动召唤记录。`,
      });
      setSummonNotes('');
      setOverrideLocation(false);
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const transitionMutation = useMutation({
    mutationFn: ({ summon, status }: { summon: BossSummon; status: BossSummonStatus }) => bossApi.transitionSummon(summon.id, {
      status,
      message: `面板手动更新为：${summonStatusLabel[status]}`,
      result: {},
    }),
    onSuccess: async (summon) => {
      setNotice({ type: 'success', text: `召唤记录已更新为“${summonStatusLabel[summon.status]}”。` });
      await refresh();
      if (selectedSummonID === summon.id) await summonEventsQuery.refetch();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const transitionWaveMutation = useMutation({
    mutationFn: ({ wave, status }: { wave: BossSummonWave; status: BossWaveActionStatus }) => bossApi.transitionSummonWave(wave.summon_id, wave.position, {
      status,
      message: `第${wave.position}波“${wave.name}”更新为：${waveStatusLabel[status]}`,
      result: {},
    }),
    onSuccess: async (wave) => {
      setNotice({ type: 'success', text: `第${wave.position}波已更新为“${waveStatusLabel[wave.status]}”。` });
      await refresh();
      await Promise.all([summonWavesQuery.refetch(), summonEventsQuery.refetch()]);
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const rewards = rewardsQuery.data?.items || [];
  const templates = templatesQuery.data?.items || [];
  const summons = summonsQuery.data?.items || [];
  const schedules = schedulesQuery.data?.items || [];
  const scheduleEvents = scheduleEventsQuery.data?.items || [];
  const summary = summaryQuery.data;

  const rewardByID = useMemo(() => new Map(rewards.map((reward) => [reward.id, reward])), [rewards]);
  const activeTemplates = useMemo(() => templates.filter((template) => template.enabled && !template.archived_at), [templates]);

  const filteredItems = useMemo(() => {
    const needle = itemSearch.trim().toLowerCase();
    return (itemCatalogQuery.data?.items || []).filter((item) => !needle || `${item.name} ${item.id}`.toLowerCase().includes(needle)).slice(0, 80);
  }, [itemCatalogQuery.data, itemSearch]);

  const filteredPals = useMemo(() => {
    const needle = palSearch.trim().toLowerCase();
    return (palCatalogQuery.data?.items || []).filter((pal) => !needle || `${pal.name} ${pal.id}`.toLowerCase().includes(needle)).slice(0, 100);
  }, [palCatalogQuery.data, palSearch]);

  const filteredPalTemplates = useMemo(() => {
    const needle = templateSearch.trim().toLowerCase();
    return (palTemplatesQuery.data?.templates || []).filter((template) => !needle || template.name.toLowerCase().includes(needle)).slice(0, 100);
  }, [palTemplatesQuery.data, templateSearch]);

  const filteredWavePals = useMemo(() => {
    const needle = wavePalSearch.trim().toLowerCase();
    return (palCatalogQuery.data?.items || [])
      .filter((pal) => !needle || `${pal.name} ${pal.id}`.toLowerCase().includes(needle))
      .slice(0, 300);
  }, [palCatalogQuery.data, wavePalSearch]);

  const openRewardCreate = () => {
    setRewardEditingID('');
    setRewardDraft(emptyRewardInput());
    setRewardEditorOpen(true);
  };

  const openRewardEdit = (reward: BossReward) => {
    setRewardEditingID(reward.id);
    setRewardDraft({
      name: reward.name,
      description: reward.description || '',
      points: reward.points,
      items: reward.items || [],
      pal_templates: reward.pal_templates || [],
      enabled: reward.enabled,
      metadata: reward.metadata || {},
    });
    setRewardEditorOpen(true);
  };

  const openTemplateCreate = () => {
    setTemplateEditingID('');
    setTemplateDraft(emptyTemplateInput());
    setTemplateEditorOpen(true);
  };

  const openTemplateEdit = (template: BossTemplate) => {
    setTemplateEditingID(template.id);
    setTemplateDraft({
      name: template.name,
      description: template.description || '',
      pal_id: template.pal_id,
      level: template.level,
      count: template.count,
      hp_multiplier: template.hp_multiplier,
      attack_multiplier: template.attack_multiplier,
      defense_multiplier: template.defense_multiplier,
      spawn_radius: template.spawn_radius,
      capturable: template.capturable,
      cooldown_seconds: template.cooldown_seconds,
      reward_id: template.reward_id || '',
      location: template.location,
      enabled: template.enabled,
      metadata: template.metadata || {},
    });
    setTemplateEditorOpen(true);
  };

  const addRewardItem = (itemID: string) => {
    setRewardDraft((current) => {
      const items = [...current.items];
      const existing = items.find((item) => item.item_id === itemID);
      if (existing) existing.count += 1;
      else items.push({ item_id: itemID, count: 1 });
      return { ...current, items };
    });
  };

  const setRewardItemCount = (itemID: string, count: number) => {
    setRewardDraft((current) => ({
      ...current,
      items: current.items.map((item) => item.item_id === itemID ? { ...item, count: Math.max(1, Math.trunc(count || 1)) } : item),
    }));
  };

  const removeRewardItem = (itemID: string) => {
    setRewardDraft((current) => ({ ...current, items: current.items.filter((item) => item.item_id !== itemID) }));
  };

  const addRewardPalTemplate = (name: string) => {
    setRewardDraft((current) => current.pal_templates.includes(name) ? current : { ...current, pal_templates: [...current.pal_templates, name] });
  };

  const openWaveEditor = async (template: BossTemplate) => {
    setWaveEditorTemplate(template);
    setWavePalSearch('');
    setWaveLoading(true);
    try {
      const result = await bossApi.templateWaves(template.id);
      setWaveDraft(result.items.map((wave) => ({
        name: wave.name,
        kind: wave.kind,
        pal_id: wave.pal_id,
        level: wave.level,
        count: wave.count,
        hp_multiplier: wave.hp_multiplier,
        attack_multiplier: wave.attack_multiplier,
        defense_multiplier: wave.defense_multiplier,
        spawn_radius: wave.spawn_radius,
        delay_seconds: wave.delay_seconds,
        capturable: wave.capturable,
        metadata: wave.metadata || {},
      })));
    } catch (error) {
      setNotice({ type: 'error', text: getErrorMessage(error) });
      setWaveEditorTemplate(null);
    } finally {
      setWaveLoading(false);
    }
  };

  const addWave = () => {
    if (!waveEditorTemplate || waveDraft.length >= 20) return;
    setWaveDraft((current) => [...current, waveFromTemplate(waveEditorTemplate, current.length + 1)]);
  };

  const updateWave = (index: number, patch: Partial<BossWaveInput>) => {
    setWaveDraft((current) => current.map((wave, waveIndex) => waveIndex === index ? { ...wave, ...patch } : wave));
  };

  const moveWave = (index: number, direction: -1 | 1) => {
    setWaveDraft((current) => {
      const target = index + direction;
      if (target < 0 || target >= current.length) return current;
      const next = [...current];
      [next[index], next[target]] = [next[target], next[index]];
      return next;
    });
  };

  const openScheduleCreate = () => {
    setScheduleEditingID('');
    setScheduleDraft(emptyScheduleInput());
  };

  const openScheduleEdit = (schedule: BossSchedule) => {
    setScheduleEditingID(schedule.id);
    setScheduleDraft({
      name: schedule.name,
      template_id: schedule.template_id,
      mode: schedule.mode,
      daily_time: schedule.daily_time || '20:00',
      cron: schedule.cron || '0 20 * * 6',
      timezone: schedule.timezone,
      warning_minutes: schedule.warning_minutes,
      warning_title: schedule.warning_title || '',
      warning_message: schedule.warning_message || '',
      ...(schedule.location_override ? { location_override: schedule.location_override } : {}),
      enabled: schedule.enabled,
      metadata: schedule.metadata || {},
    });
  };

  const scheduleRule = (schedule: BossSchedule) => schedule.mode === 'daily'
    ? `每天 ${schedule.daily_time} · ${schedule.timezone}`
    : `${schedule.cron} · ${schedule.timezone}`;

  const possibleWaveTransitions = (wave: BossSummonWave): BossWaveActionStatus[] => {
    if (wave.status === 'pending') return ['active', 'failed', 'skipped'];
    if (wave.status === 'active') return ['completed', 'failed', 'skipped'];
    return [];
  };

  const possibleTransitions = (summon: BossSummon): BossSummonStatus[] => {
    if (summon.status === 'pending') return ['active', 'failed', 'cancelled'];
    if (summon.status === 'active') return ['completed', 'failed', 'cancelled'];
    return [];
  };

  const tabButton = (id: BossTab, label: string, icon: React.ReactNode) => (
    <button
      type="button"
      onClick={() => setActiveTab(id)}
      className={`flex items-center gap-2 rounded-xl px-4 py-2 text-xs font-bold transition ${activeTab === id ? 'bg-slate-900 text-white shadow-sm' : 'text-slate-500 hover:bg-slate-100 hover:text-slate-800'}`}
    >
      {icon}{label}
    </button>
  );

  return (
    <div className="mx-auto flex w-full max-w-[1550px] flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <div className="mb-2 flex items-center gap-2 text-xs font-bold uppercase tracking-[0.18em] text-rose-600"><Crown size={15} />Boss活动</div>
            <h1 className="text-2xl font-black tracking-tight text-slate-900">Boss管理</h1>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-500">配置Boss模板、奖励方案、波次编排、定时计划和召唤记录。定时器会自动创建“仅记录”召唤，仍不会调用PalDefender实际生成Boss。</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <label className="flex items-center gap-2 rounded-xl border border-slate-200 px-3 py-2 text-xs font-bold text-slate-500">
              <input type="checkbox" checked={includeArchived} onChange={(event) => setIncludeArchived(event.target.checked)} className="h-4 w-4 rounded border-slate-300" />显示归档
            </label>
            <button type="button" onClick={() => void refresh()} className="pp-button"><RefreshCw size={14} />刷新</button>
          </div>
        </div>
        {notice && <div className={`mt-4 rounded-xl border px-4 py-3 text-sm font-semibold ${notice.type === 'success' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-rose-200 bg-rose-50 text-rose-700'}`}>{notice.text}</div>}
      </section>

      <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-7">
        <Metric label="奖励方案" value={summary?.rewards || 0} icon={<Gift size={18} />} />
        <Metric label="Boss模板" value={summary?.templates || 0} icon={<Skull size={18} />} />
        <Metric label="已配置波次" value={summary?.template_waves || 0} icon={<Layers3 size={18} />} />
        <Metric label="启用计划" value={summary?.enabled_schedules || 0} icon={<CalendarClock size={18} />} />
        <Metric label="等待处理" value={summary?.pending_summons || 0} icon={<Clock3 size={18} />} />
        <Metric label="进行中" value={summary?.active_summons || 0} icon={<Sword size={18} />} />
        <Metric label="已完成" value={summary?.completed_summons || 0} icon={<CheckCircle2 size={18} />} />
      </section>

      <section className="rounded-2xl border border-slate-200 bg-white p-3 shadow-sm">
        <div className="flex flex-wrap gap-1 rounded-xl bg-slate-50 p-1">
          {tabButton('templates', `Boss模板 (${templates.length})`, <Skull size={14} />)}
          {tabButton('rewards', `奖励方案 (${rewards.length})`, <Gift size={14} />)}
          {tabButton('schedules', `定时计划 (${schedules.length})`, <CalendarClock size={14} />)}
          {tabButton('summons', `召唤记录 (${summons.length})`, <Sword size={14} />)}
        </div>
      </section>

      {activeTab === 'templates' && <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="mb-4 flex items-start justify-between gap-3">
          <div><h2 className="font-black text-slate-900">Boss模板</h2><p className="mt-1 text-xs text-slate-400">模板保存默认Boss参数；波次编辑器可以编排主Boss、护卫怪和增援。召唤记录创建时会保存完整波次快照。</p></div>
          <button type="button" onClick={openTemplateCreate} className="pp-btn pp-btn--primary"><Plus size={14} />新建模板</button>
        </div>
        {templatesQuery.error && <ErrorBox error={templatesQuery.error} />}
        <div className="grid gap-3 lg:grid-cols-2">
          {templates.map((template) => <article key={template.id} className="rounded-xl border border-slate-200 p-4">
            <div className="flex items-start justify-between gap-3">
              <div><h3 className="font-black text-slate-800">{template.name}</h3><p className="mt-1 text-xs text-slate-400">{template.description || template.id}</p></div>
              <EnabledBadge enabled={template.enabled} archived={template.archived_at} />
            </div>
            <div className="mt-4 grid grid-cols-2 gap-3 text-xs sm:grid-cols-4">
              <Data label="帕鲁" value={template.pal_id} mono />
              <Data label="等级 / 数量" value={`${template.level} / ${template.count}`} />
              <Data label="生命倍率" value={`${template.hp_multiplier}×`} />
              <Data label="攻击 / 防御" value={`${template.attack_multiplier}× / ${template.defense_multiplier}×`} />
            </div>
            <div className="mt-3 flex flex-wrap gap-2 text-[11px] font-bold text-slate-500">
              <span className="rounded-full bg-slate-100 px-2.5 py-1">{template.capturable ? '允许捕获' : '禁止捕获'}</span>
              <span className="rounded-full bg-slate-100 px-2.5 py-1">半径 {number.format(template.spawn_radius)}</span>
              <span className="rounded-full bg-slate-100 px-2.5 py-1">冷却 {formatDuration(template.cooldown_seconds)}</span>
              <span className="rounded-full bg-slate-100 px-2.5 py-1">奖励 {rewardByID.get(template.reward_id || '')?.name || '无'}</span>
            </div>
            <div className="mt-4 flex items-center justify-between border-t border-slate-100 pt-3">
              <span className="flex min-w-0 items-center gap-1.5 truncate text-xs text-slate-400"><MapPin size={13} />{template.location.label || `${template.location.x}, ${template.location.y}, ${template.location.z}`}</span>
              <div className="flex gap-1.5">
                {!template.archived_at && <IconButton title="配置波次" icon={<Layers3 size={14} />} onClick={() => void openWaveEditor(template)} />}
                {!template.archived_at && <IconButton title="编辑模板" icon={<Pencil size={14} />} onClick={() => openTemplateEdit(template)} />}
                {!template.archived_at && <IconButton danger title="归档模板" icon={<Archive size={14} />} onClick={() => {
                  if (window.confirm(`归档Boss模板“${template.name}”？历史召唤记录不会删除。`)) archiveTemplateMutation.mutate(template);
                }} />}
              </div>
            </div>
          </article>)}
          {!templatesQuery.isLoading && templates.length === 0 && <Empty text="暂无Boss模板。先创建奖励方案，再建立Boss模板。" />}
        </div>
      </section>}

      {activeTab === 'rewards' && <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="mb-4 flex items-start justify-between gap-3">
          <div><h2 className="font-black text-slate-900">奖励方案</h2><p className="mt-1 text-xs text-slate-400">奖励可组合积分、PalDefender物品和帕鲁模板；本版只负责配置，尚未自动结算参与者奖励。</p></div>
          <button type="button" onClick={openRewardCreate} className="pp-btn pp-btn--primary"><Plus size={14} />新建奖励</button>
        </div>
        {rewardsQuery.error && <ErrorBox error={rewardsQuery.error} />}
        <div className="grid gap-3 lg:grid-cols-2 xl:grid-cols-3">
          {rewards.map((reward) => <article key={reward.id} className="rounded-xl border border-slate-200 p-4">
            <div className="flex items-start justify-between gap-3"><div><h3 className="font-black text-slate-800">{reward.name}</h3><p className="mt-1 text-xs leading-5 text-slate-400">{reward.description || reward.id}</p></div><EnabledBadge enabled={reward.enabled} archived={reward.archived_at} /></div>
            <div className="mt-4 grid grid-cols-3 gap-2 text-center">
              <Data label="积分" value={number.format(reward.points)} />
              <Data label="物品种类" value={number.format(reward.items.length)} />
              <Data label="帕鲁模板" value={number.format(reward.pal_templates.length)} />
            </div>
            {(reward.items.length > 0 || reward.pal_templates.length > 0) && <div className="mt-3 rounded-lg bg-slate-50 p-3 text-[11px] leading-5 text-slate-500">
              {reward.items.slice(0, 4).map((item) => <div key={item.item_id}>{item.item_id} × {number.format(item.count)}</div>)}
              {reward.items.length > 4 && <div>另有 {reward.items.length - 4} 种物品</div>}
              {reward.pal_templates.slice(0, 3).map((name) => <div key={name}>模板：{name}</div>)}
              {reward.pal_templates.length > 3 && <div>另有 {reward.pal_templates.length - 3} 个模板</div>}
            </div>}
            <div className="mt-4 flex justify-end gap-1.5 border-t border-slate-100 pt-3">
              {!reward.archived_at && <IconButton title="编辑奖励" icon={<Pencil size={14} />} onClick={() => openRewardEdit(reward)} />}
              {!reward.archived_at && <IconButton danger title="归档奖励" icon={<Archive size={14} />} onClick={() => {
                if (window.confirm(`归档奖励方案“${reward.name}”？已关联模板仍保留奖励ID。`)) archiveRewardMutation.mutate(reward);
              }} />}
            </div>
          </article>)}
          {!rewardsQuery.isLoading && rewards.length === 0 && <Empty text="暂无奖励方案。" />}
        </div>
      </section>}

      {activeTab === 'schedules' && <section className="grid gap-5 xl:grid-cols-[420px_minmax(0,1fr)]">
        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <div className="flex items-start justify-between gap-3"><div><h2 className="font-black text-slate-900">{scheduleEditingID ? '编辑定时计划' : '新建定时计划'}</h2><p className="mt-1 text-xs text-slate-400">后台每30秒检查一次。到点后创建record_only召唤记录；预警先写入审计，不会自动发送游戏广播。</p></div>{scheduleEditingID && <button type="button" onClick={openScheduleCreate} className="pp-button">新建</button>}</div>
          <div className="mt-4 grid gap-3 sm:grid-cols-2">
            <label className="sm:col-span-2"><FieldLabel>计划名称</FieldLabel><input value={scheduleDraft.name} onChange={(event) => setScheduleDraft((current) => ({ ...current, name: event.target.value }))} className="pp-input w-full" placeholder="例如：周六晚间Boss" /></label>
            <label className="sm:col-span-2"><FieldLabel>Boss模板</FieldLabel><select value={scheduleDraft.template_id} onChange={(event) => setScheduleDraft((current) => ({ ...current, template_id: event.target.value }))} className="pp-input w-full"><option value="">请选择模板</option>{activeTemplates.map((template) => <option key={template.id} value={template.id}>{template.name} · {template.pal_id}</option>)}</select></label>
            <label><FieldLabel>计划类型</FieldLabel><select value={scheduleDraft.mode} onChange={(event) => setScheduleDraft((current) => ({ ...current, mode: event.target.value as BossScheduleInput['mode'] }))} className="pp-input w-full"><option value="daily">每天固定时间</option><option value="cron">Cron表达式</option></select></label>
            <label><FieldLabel>时区</FieldLabel><input value={scheduleDraft.timezone} onChange={(event) => setScheduleDraft((current) => ({ ...current, timezone: event.target.value }))} className="pp-input w-full" placeholder="Asia/Shanghai" /></label>
            {scheduleDraft.mode === 'daily' ? <label className="sm:col-span-2"><FieldLabel>每天执行时间</FieldLabel><input type="time" value={scheduleDraft.daily_time} onChange={(event) => setScheduleDraft((current) => ({ ...current, daily_time: event.target.value }))} className="pp-input w-full" /></label> : <label className="sm:col-span-2"><FieldLabel>Cron（分 时 日 月 周）</FieldLabel><input value={scheduleDraft.cron} onChange={(event) => setScheduleDraft((current) => ({ ...current, cron: event.target.value }))} className="pp-input w-full font-mono" placeholder="0 20 * * 6" /><span className="mt-1 block text-[11px] text-slate-400">支持 *、逗号、范围和步长，例如 */15 * * * *。</span></label>}
            <label><FieldLabel>提前预警（分钟）</FieldLabel><input type="number" min={0} max={10080} value={scheduleDraft.warning_minutes} onChange={(event) => setScheduleDraft((current) => ({ ...current, warning_minutes: Math.max(0, Math.trunc(Number(event.target.value) || 0)) }))} className="pp-input w-full" /></label>
            <label className="flex items-center gap-3 rounded-xl border border-slate-200 p-3"><input type="checkbox" checked={scheduleDraft.enabled} onChange={(event) => setScheduleDraft((current) => ({ ...current, enabled: event.target.checked }))} className="h-4 w-4 rounded border-slate-300" /><span className="text-sm font-black text-slate-800">启用计划</span></label>
            <label className="sm:col-span-2"><FieldLabel>预警标题</FieldLabel><input value={scheduleDraft.warning_title} onChange={(event) => setScheduleDraft((current) => ({ ...current, warning_title: event.target.value }))} className="pp-input w-full" /></label>
            <label className="sm:col-span-2"><FieldLabel>预警消息</FieldLabel><textarea value={scheduleDraft.warning_message} onChange={(event) => setScheduleDraft((current) => ({ ...current, warning_message: event.target.value }))} rows={3} className="pp-input w-full" /><span className="mt-1 block text-[11px] text-slate-400">变量：{'{{minutes}}'}、{'{{schedule}}'}、{'{{boss}}'}。当前仅写入预警审计。</span></label>
            <label className="sm:col-span-2 flex items-center gap-3 rounded-xl border border-slate-200 p-3"><input type="checkbox" checked={Boolean(scheduleDraft.location_override)} onChange={(event) => setScheduleDraft((current) => event.target.checked ? ({ ...current, location_override: { x: 0, y: 0, z: 0, label: '' } }) : ({ ...current, location_override: undefined }))} className="h-4 w-4 rounded border-slate-300" /><span><span className="block text-sm font-black text-slate-800">覆盖模板坐标</span><span className="mt-1 block text-xs text-slate-400">每次自动创建召唤记录时使用该坐标。</span></span></label>
            {scheduleDraft.location_override && <div className="sm:col-span-2 grid grid-cols-3 gap-2"><Coordinate label="X" value={scheduleDraft.location_override.x} onChange={(value) => setScheduleDraft((current) => ({ ...current, location_override: { ...(current.location_override || { x: 0, y: 0, z: 0 }), x: value } }))} /><Coordinate label="Y" value={scheduleDraft.location_override.y} onChange={(value) => setScheduleDraft((current) => ({ ...current, location_override: { ...(current.location_override || { x: 0, y: 0, z: 0 }), y: value } }))} /><Coordinate label="Z" value={scheduleDraft.location_override.z} onChange={(value) => setScheduleDraft((current) => ({ ...current, location_override: { ...(current.location_override || { x: 0, y: 0, z: 0 }), z: value } }))} /></div>}
          </div>
          <button type="button" disabled={saveScheduleMutation.isPending || !scheduleDraft.name.trim() || !scheduleDraft.template_id} onClick={() => saveScheduleMutation.mutate()} className="pp-btn pp-btn--primary mt-4 w-full">{saveScheduleMutation.isPending ? <LoaderCircle className="animate-spin" size={15} /> : <Save size={15} />}{scheduleEditingID ? '保存计划' : '创建计划'}</button>
        </div>
        <div className="space-y-5">
          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
            <div className="mb-4 flex flex-wrap items-start justify-between gap-3"><div><h2 className="font-black text-slate-900">定时计划</h2><p className="mt-1 text-xs text-slate-400">计划到点后自动创建召唤及波次快照。实际Boss生成仍需后续执行器。</p></div><button type="button" disabled={runDueMutation.isPending} onClick={() => runDueMutation.mutate()} className="pp-button">{runDueMutation.isPending ? <LoaderCircle className="animate-spin" size={14} /> : <RefreshCw size={14} />}立即扫描到期计划</button></div>
            {schedulesQuery.error && <ErrorBox error={schedulesQuery.error} />}
            <div className="space-y-3">{schedules.map((schedule) => <article key={schedule.id} className={`rounded-xl border p-4 ${selectedScheduleID === schedule.id ? 'border-rose-300 bg-rose-50/30' : 'border-slate-200'}`}>
              <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between"><div><div className="flex flex-wrap items-center gap-2"><h3 className="font-black text-slate-800">{schedule.name}</h3><EnabledBadge enabled={schedule.enabled} archived={schedule.archived_at} /></div><p className="mt-1 text-xs text-slate-500">{schedule.template_name || schedule.template_id}</p></div><div className="flex flex-wrap gap-1.5"><IconButton title="查看计划审计" icon={<BellRing size={14} />} onClick={() => setSelectedScheduleID(selectedScheduleID === schedule.id ? '' : schedule.id)} />{!schedule.archived_at && <IconButton title="立即创建召唤记录" icon={<Play size={14} />} onClick={() => runScheduleMutation.mutate(schedule)} />}{!schedule.archived_at && <IconButton title="编辑计划" icon={<Pencil size={14} />} onClick={() => openScheduleEdit(schedule)} />}{!schedule.archived_at && <IconButton danger title="归档计划" icon={<Archive size={14} />} onClick={() => { if (window.confirm(`归档定时计划“${schedule.name}”？`)) archiveScheduleMutation.mutate(schedule); }} />}</div></div>
              <div className="mt-3 grid gap-2 text-xs sm:grid-cols-3"><Data label="规则" value={scheduleRule(schedule)} /><Data label="下次运行" value={formatDate(schedule.next_run_at)} /><Data label="提前预警" value={schedule.warning_minutes > 0 ? `${schedule.warning_minutes}分钟` : '关闭'} /></div>
              {schedule.last_run_at && <div className="mt-2 text-[11px] text-slate-400">上次计划时间：{formatDate(schedule.last_run_at)}</div>}
            </article>)}{!schedulesQuery.isLoading && schedules.length === 0 && <Empty text="暂无Boss定时计划。" />}</div>
          </div>
          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm"><div className="mb-3"><h2 className="font-black text-slate-900">计划审计</h2><p className="mt-1 text-xs text-slate-400">{selectedScheduleID ? '当前仅显示所选计划。' : '显示全部计划最近事件。'}</p></div>{scheduleEventsQuery.error && <ErrorBox error={scheduleEventsQuery.error} />}<div className="space-y-2">{scheduleEvents.map((event) => <div key={event.id} className="rounded-xl bg-slate-50 p-3 text-xs"><div className="flex flex-wrap items-center gap-2"><span className="font-black text-slate-700">{scheduleEventTypeLabel[event.event_type]}</span><span className={`rounded-full px-2 py-0.5 text-[10px] font-bold ${event.status === 'success' ? 'bg-emerald-50 text-emerald-700' : event.status === 'failed' ? 'bg-rose-50 text-rose-700' : 'bg-slate-100 text-slate-600'}`}>{scheduleEventStatusLabel[event.status]}</span><span className="text-slate-400">计划时间 {formatDate(event.planned_for)}</span></div><div className="mt-1 text-slate-600">{event.message || '—'}</div><div className="mt-1 text-[10px] text-slate-400">{event.actor || '系统'} · {formatDate(event.created_at)}{event.summon_id ? ` · 召唤 ${event.summon_id}` : ''}</div></div>)}{!scheduleEventsQuery.isLoading && scheduleEvents.length === 0 && <Empty text="暂无计划审计事件。" />}</div></div>
        </div>
      </section>}

      {activeTab === 'summons' && <section className="grid gap-5 xl:grid-cols-[360px_minmax(0,1fr)]">
        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <div className="flex items-center gap-2 text-sm font-black text-slate-900"><Play size={16} />创建手动召唤记录</div>
          <div className="mt-3 rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs leading-5 text-amber-800"><ShieldAlert className="mr-1 inline" size={14} />当前仅创建审计记录，不会在游戏内生成Boss。完成实际操作后再更新记录状态。</div>
          <label className="mt-4 block"><FieldLabel>Boss模板</FieldLabel><select value={summonTemplateID} onChange={(event) => setSummonTemplateID(event.target.value)} className="pp-input w-full"><option value="">请选择模板</option>{activeTemplates.map((template) => <option key={template.id} value={template.id}>{template.name} · Lv.{template.level} · {template.pal_id}</option>)}</select></label>
          <label className="mt-4 block"><FieldLabel>操作备注</FieldLabel><textarea value={summonNotes} onChange={(event) => setSummonNotes(event.target.value)} rows={3} maxLength={4096} className="pp-input w-full resize-y" placeholder="例如：周末活动场次、人工RCON命令记录" /></label>
          <label className="mt-4 flex items-center gap-3 rounded-xl border border-slate-200 p-3"><input type="checkbox" checked={overrideLocation} onChange={(event) => setOverrideLocation(event.target.checked)} className="h-4 w-4 rounded border-slate-300" /><span><span className="block text-sm font-black text-slate-800">覆盖模板坐标</span><span className="mt-1 block text-xs text-slate-400">仅影响本次记录快照。</span></span></label>
          {overrideLocation && <div className="mt-3 grid grid-cols-3 gap-2"><Coordinate label="X" value={summonLocation.x} onChange={(value) => setSummonLocation((current) => ({ ...current, x: value }))} /><Coordinate label="Y" value={summonLocation.y} onChange={(value) => setSummonLocation((current) => ({ ...current, y: value }))} /><Coordinate label="Z" value={summonLocation.z} onChange={(value) => setSummonLocation((current) => ({ ...current, z: value }))} /><label className="col-span-3"><FieldLabel>地点名称</FieldLabel><input value={summonLocation.label} onChange={(event) => setSummonLocation((current) => ({ ...current, label: event.target.value }))} className="pp-input w-full" /></label></div>}
          <button type="button" disabled={!summonTemplateID || createSummonMutation.isPending} onClick={() => createSummonMutation.mutate()} className="pp-btn pp-btn--primary mt-4 w-full justify-center">{createSummonMutation.isPending ? <LoaderCircle className="animate-spin" size={15} /> : <Plus size={15} />}创建召唤记录</button>
        </div>

        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div><h2 className="font-black text-slate-900">召唤记录</h2><p className="mt-1 text-xs text-slate-400">状态和事件名称均以中文显示，内部仍保存稳定状态代码。</p></div>
            <select value={summonStatus} onChange={(event) => setSummonStatus(event.target.value as BossSummonStatus | '')} className="pp-input min-w-40"><option value="">全部状态</option>{Object.entries(summonStatusLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>
          </div>
          {summonsQuery.error && <ErrorBox error={summonsQuery.error} />}
          <div className="space-y-3">
            {summons.map((summon) => <article key={summon.id} className="rounded-xl border border-slate-200 p-4">
              <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                <div><div className="flex flex-wrap items-center gap-2"><h3 className="font-black text-slate-800">{summon.template_name}</h3><span className={`rounded-full px-2.5 py-1 text-[11px] font-bold ${summonStatusClass[summon.status]}`}>{summonStatusLabel[summon.status]}</span><span className="rounded-full bg-violet-50 px-2.5 py-1 text-[11px] font-bold text-violet-700">仅记录模式</span></div><div className="mt-1 font-mono text-[11px] text-slate-400">{summon.id}</div></div>
                <div className="flex flex-wrap gap-1.5">
                  <button type="button" onClick={() => setSelectedSummonID(selectedSummonID === summon.id ? '' : summon.id)} className="pp-button">波次 / 审计</button>
                  {possibleTransitions(summon).map((status) => <button key={status} type="button" disabled={transitionMutation.isPending} onClick={() => transitionMutation.mutate({ summon, status })} className="pp-button">{summonStatusLabel[status]}</button>)}
                </div>
              </div>
              <div className="mt-4 grid gap-3 text-xs sm:grid-cols-2 lg:grid-cols-5"><Data label="帕鲁" value={summon.pal_id} mono /><Data label="等级 / 数量" value={`${summon.level} / ${summon.count}`} /><Data label="生命倍率" value={`${summon.hp_multiplier}×`} /><Data label="坐标" value={`${summon.location.x}, ${summon.location.y}, ${summon.location.z}`} /><Data label="申请时间" value={formatDate(summon.requested_at)} /></div>
              {summon.notes && <div className="mt-3 rounded-lg bg-slate-50 px-3 py-2 text-xs leading-5 text-slate-500">{summon.notes}</div>}
              {summon.failure && <div className="mt-3 flex gap-2 rounded-lg bg-rose-50 p-3 text-xs text-rose-700"><CircleAlert className="shrink-0" size={14} />{summon.failure}</div>}
              {selectedSummonID === summon.id && <>
                <SummonWaveList
                  loading={summonWavesQuery.isLoading}
                  error={summonWavesQuery.error}
                  waves={summonWavesQuery.data?.items || []}
                  pending={transitionWaveMutation.isPending}
                  transitions={possibleWaveTransitions}
                  onTransition={(wave, status) => transitionWaveMutation.mutate({ wave, status })}
                />
                <SummonEvents loading={summonEventsQuery.isLoading} error={summonEventsQuery.error} events={summonEventsQuery.data?.items || []} />
              </>}
            </article>)}
            {!summonsQuery.isLoading && summons.length === 0 && <Empty text="暂无召唤记录。" />}
          </div>
        </div>
      </section>}

      {rewardEditorOpen && <Modal title={rewardEditingID ? '编辑奖励方案' : '新建奖励方案'} subtitle={rewardEditingID || '保存后生成奖励ID'} onClose={() => setRewardEditorOpen(false)} footer={<><button type="button" onClick={() => setRewardEditorOpen(false)} className="pp-button">取消</button><button type="button" disabled={saveRewardMutation.isPending || !rewardDraft.name.trim()} onClick={() => saveRewardMutation.mutate()} className="pp-btn pp-btn--primary">{saveRewardMutation.isPending ? <LoaderCircle className="animate-spin" size={15} /> : <Save size={15} />}保存奖励</button></>}>
        <div className="grid gap-4 sm:grid-cols-2">
          <label className="sm:col-span-2"><FieldLabel>奖励名称</FieldLabel><input value={rewardDraft.name} onChange={(event) => setRewardDraft((current) => ({ ...current, name: event.target.value }))} maxLength={128} className="pp-input w-full" /></label>
          <label className="sm:col-span-2"><FieldLabel>说明</FieldLabel><textarea value={rewardDraft.description || ''} onChange={(event) => setRewardDraft((current) => ({ ...current, description: event.target.value }))} rows={2} className="pp-input w-full resize-y" /></label>
          <label><FieldLabel>积分奖励</FieldLabel><input type="number" min={0} value={rewardDraft.points} onChange={(event) => setRewardDraft((current) => ({ ...current, points: Math.max(0, Math.trunc(Number(event.target.value) || 0)) }))} className="pp-input w-full" /></label>
          <label className="flex items-center gap-3 rounded-xl border border-slate-200 p-3"><input type="checkbox" checked={rewardDraft.enabled} onChange={(event) => setRewardDraft((current) => ({ ...current, enabled: event.target.checked }))} className="h-4 w-4 rounded border-slate-300" /><span className="text-sm font-black text-slate-800">启用奖励方案</span></label>
        </div>
        <CatalogSection title="PalDefender物品" description="搜索中文名称或内部物品ID，点击后加入奖励列表。" search={itemSearch} onSearch={setItemSearch} loading={itemCatalogQuery.isLoading} error={itemCatalogQuery.error}>
          <div className="max-h-44 overflow-y-auto rounded-xl border border-slate-200"><div className="divide-y divide-slate-100">{filteredItems.map((item) => <button key={item.id} type="button" onClick={() => addRewardItem(item.id)} className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-xs hover:bg-slate-50"><span className="font-bold text-slate-700">{item.name}</span><code className="text-[10px] text-slate-400">{item.id}</code></button>)}</div></div>
          <div className="mt-3 space-y-2">{rewardDraft.items.map((item) => <RewardItemEditor key={item.item_id} item={item} onCount={(count) => setRewardItemCount(item.item_id, count)} onRemove={() => removeRewardItem(item.item_id)} />)}{rewardDraft.items.length === 0 && <div className="rounded-lg bg-slate-50 py-4 text-center text-xs text-slate-400">未选择物品</div>}</div>
        </CatalogSection>
        <CatalogSection title="帕鲁模板" description="从PalDefender已保存模板中选择。" search={templateSearch} onSearch={setTemplateSearch} loading={palTemplatesQuery.isLoading} error={palTemplatesQuery.error}>
          <div className="max-h-36 overflow-y-auto rounded-xl border border-slate-200"><div className="divide-y divide-slate-100">{filteredPalTemplates.map((template) => <button key={template.name} type="button" onClick={() => addRewardPalTemplate(template.name)} className="flex w-full items-center justify-between px-3 py-2 text-left text-xs hover:bg-slate-50"><span className="font-bold text-slate-700">{template.name}</span><span className="text-slate-400">{number.format(template.size)} B</span></button>)}</div></div>
          <div className="mt-3 flex flex-wrap gap-2">{rewardDraft.pal_templates.map((name) => <span key={name} className="inline-flex items-center gap-1.5 rounded-full bg-violet-50 px-2.5 py-1 text-[11px] font-bold text-violet-700">{name}<button type="button" onClick={() => setRewardDraft((current) => ({ ...current, pal_templates: current.pal_templates.filter((item) => item !== name) }))}><X size={12} /></button></span>)}{rewardDraft.pal_templates.length === 0 && <span className="text-xs text-slate-400">未选择帕鲁模板</span>}</div>
        </CatalogSection>
      </Modal>}

      {waveEditorTemplate && <Modal
        title={`配置波次：${waveEditorTemplate.name}`}
        subtitle="最多20波；保存后只影响后续新建召唤记录"
        onClose={() => { setWaveEditorTemplate(null); setWaveDraft([]); }}
        footer={<>
          <button type="button" onClick={() => { setWaveEditorTemplate(null); setWaveDraft([]); }} className="pp-button">取消</button>
          <button type="button" disabled={waveLoading || saveWavesMutation.isPending} onClick={() => saveWavesMutation.mutate()} className="pp-btn pp-btn--primary">
            {saveWavesMutation.isPending ? <LoaderCircle className="animate-spin" size={15} /> : <Save size={15} />}保存波次
          </button>
        </>}
      >
        <div className="rounded-xl border border-sky-200 bg-sky-50 p-3 text-xs leading-5 text-sky-800">
          波次按列表顺序执行。延迟秒数表示上一波结束后等待多久。清空全部波次后，系统会在创建召唤记录时使用Boss模板本身生成一个隐式主Boss波次。
        </div>
        <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <label className="min-w-0 flex-1"><FieldLabel>帕鲁下拉筛选</FieldLabel><div className="relative"><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={14} /><input value={wavePalSearch} onChange={(event) => setWavePalSearch(event.target.value)} className="pp-input w-full pl-9" placeholder="输入中文名或Pal ID，下面每个波次下拉仅显示匹配项" /></div></label>
          <div className="flex items-center justify-between gap-3 sm:justify-end"><div className="text-xs font-bold text-slate-500">当前 {waveDraft.length} / 20 波</div><button type="button" disabled={waveDraft.length >= 20} onClick={addWave} className="pp-button"><Plus size={14} />添加波次</button></div>
        </div>
        {waveLoading && <div className="py-12 text-center text-sm text-slate-400"><LoaderCircle className="mr-2 inline animate-spin" size={16} />正在加载波次</div>}
        {!waveLoading && <div className="mt-3 space-y-3">
          {waveDraft.map((wave, index) => <article key={`${index}-${wave.pal_id}`} className="rounded-xl border border-slate-200 p-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center gap-2"><span className="rounded-full bg-slate-900 px-2.5 py-1 text-[11px] font-black text-white">第 {index + 1} 波</span><span className="text-xs font-bold text-slate-500">{waveKindLabel[wave.kind]}</span></div>
              <div className="flex gap-1">
                <IconButton title="上移" icon={<ArrowUp size={14} />} onClick={() => moveWave(index, -1)} />
                <IconButton title="下移" icon={<ArrowDown size={14} />} onClick={() => moveWave(index, 1)} />
                <IconButton danger title="删除波次" icon={<X size={14} />} onClick={() => setWaveDraft((current) => current.filter((_, waveIndex) => waveIndex !== index))} />
              </div>
            </div>
            <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <label className="sm:col-span-2"><FieldLabel>波次名称</FieldLabel><input value={wave.name} onChange={(event) => updateWave(index, { name: event.target.value })} maxLength={128} className="pp-input w-full" /></label>
              <label><FieldLabel>波次类型</FieldLabel><select value={wave.kind} onChange={(event) => updateWave(index, { kind: event.target.value as BossWaveInput['kind'] })} className="pp-input w-full"><option value="main">主Boss</option><option value="minion">护卫怪</option><option value="reinforcement">增援</option></select></label>
              <label><FieldLabel>帕鲁</FieldLabel><select value={wave.pal_id} onChange={(event) => updateWave(index, { pal_id: event.target.value })} className="pp-input w-full"><option value={wave.pal_id}>{wave.pal_id || '请选择'}</option>{filteredWavePals.filter((pal) => pal.id !== wave.pal_id).map((pal) => <option key={pal.id} value={pal.id}>{pal.name} · {pal.id}</option>)}</select></label>
              <NumberField label="等级" min={1} max={100} value={wave.level} onChange={(value) => updateWave(index, { level: Math.trunc(value) })} />
              <NumberField label="数量" min={1} max={100} value={wave.count} onChange={(value) => updateWave(index, { count: Math.trunc(value) })} />
              <NumberField label="生命倍率" min={0.1} max={100} step={0.1} value={wave.hp_multiplier} onChange={(value) => updateWave(index, { hp_multiplier: value })} />
              <NumberField label="攻击倍率" min={0.1} max={100} step={0.1} value={wave.attack_multiplier} onChange={(value) => updateWave(index, { attack_multiplier: value })} />
              <NumberField label="防御倍率" min={0.1} max={100} step={0.1} value={wave.defense_multiplier} onChange={(value) => updateWave(index, { defense_multiplier: value })} />
              <NumberField label="生成半径" min={0} max={100000} value={wave.spawn_radius} onChange={(value) => updateWave(index, { spawn_radius: value })} />
              <NumberField label="延迟秒数" min={0} max={86400} value={wave.delay_seconds} onChange={(value) => updateWave(index, { delay_seconds: Math.trunc(value) })} />
              <label className="flex items-center gap-3 rounded-xl border border-slate-200 p-3"><input type="checkbox" checked={wave.capturable} onChange={(event) => updateWave(index, { capturable: event.target.checked })} className="h-4 w-4 rounded border-slate-300" /><span className="text-sm font-black text-slate-800">允许捕获</span></label>
            </div>
          </article>)}
          {waveDraft.length === 0 && <Empty text="尚未配置波次。保存空列表后，新召唤会使用模板本身作为隐式主Boss波次。" />}
        </div>}
      </Modal>}

      {templateEditorOpen && <Modal title={templateEditingID ? '编辑Boss模板' : '新建Boss模板'} subtitle={templateEditingID || '保存后生成模板ID'} onClose={() => setTemplateEditorOpen(false)} footer={<><button type="button" onClick={() => setTemplateEditorOpen(false)} className="pp-button">取消</button><button type="button" disabled={saveTemplateMutation.isPending || !templateDraft.name.trim() || !templateDraft.pal_id} onClick={() => saveTemplateMutation.mutate()} className="pp-btn pp-btn--primary">{saveTemplateMutation.isPending ? <LoaderCircle className="animate-spin" size={15} /> : <Save size={15} />}保存模板</button></>}>
        <div className="grid gap-4 sm:grid-cols-2">
          <label className="sm:col-span-2"><FieldLabel>模板名称</FieldLabel><input value={templateDraft.name} onChange={(event) => setTemplateDraft((current) => ({ ...current, name: event.target.value }))} maxLength={128} className="pp-input w-full" /></label>
          <label className="sm:col-span-2"><FieldLabel>说明</FieldLabel><textarea value={templateDraft.description || ''} onChange={(event) => setTemplateDraft((current) => ({ ...current, description: event.target.value }))} rows={2} className="pp-input w-full resize-y" /></label>
          <label className="sm:col-span-2"><FieldLabel>选择帕鲁</FieldLabel><div className="relative"><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={14} /><input value={palSearch} onChange={(event) => setPalSearch(event.target.value)} className="pp-input w-full pl-9" placeholder="搜索中文名或Pal ID" /></div><div className="mt-2 max-h-40 overflow-y-auto rounded-xl border border-slate-200"><div className="divide-y divide-slate-100">{filteredPals.map((pal) => <button key={pal.id} type="button" onClick={() => { setTemplateDraft((current) => ({ ...current, pal_id: pal.id })); setPalSearch(`${pal.name} · ${pal.id}`); }} className={`flex w-full items-center justify-between px-3 py-2 text-left text-xs hover:bg-slate-50 ${templateDraft.pal_id === pal.id ? 'bg-rose-50' : ''}`}><span className="font-bold text-slate-700">{pal.name}</span><code className="text-[10px] text-slate-400">{pal.id}</code></button>)}</div></div><span className="mt-1.5 block text-xs text-slate-400">已选择：{templateDraft.pal_id || '无'}</span></label>
          <NumberField label="等级" min={1} max={100} value={templateDraft.level} onChange={(value) => setTemplateDraft((current) => ({ ...current, level: value }))} />
          <NumberField label="生成数量" min={1} max={100} value={templateDraft.count} onChange={(value) => setTemplateDraft((current) => ({ ...current, count: value }))} />
          <NumberField label="生命倍率" min={0.1} max={100} step={0.1} value={templateDraft.hp_multiplier} onChange={(value) => setTemplateDraft((current) => ({ ...current, hp_multiplier: value }))} />
          <NumberField label="攻击倍率" min={0.1} max={100} step={0.1} value={templateDraft.attack_multiplier} onChange={(value) => setTemplateDraft((current) => ({ ...current, attack_multiplier: value }))} />
          <NumberField label="防御倍率" min={0.1} max={100} step={0.1} value={templateDraft.defense_multiplier} onChange={(value) => setTemplateDraft((current) => ({ ...current, defense_multiplier: value }))} />
          <NumberField label="生成半径" min={0} max={100000} value={templateDraft.spawn_radius} onChange={(value) => setTemplateDraft((current) => ({ ...current, spawn_radius: value }))} />
          <NumberField label="冷却秒数" min={0} max={604800} value={templateDraft.cooldown_seconds} onChange={(value) => setTemplateDraft((current) => ({ ...current, cooldown_seconds: Math.trunc(value) }))} />
          <label><FieldLabel>关联奖励</FieldLabel><select value={templateDraft.reward_id || ''} onChange={(event) => setTemplateDraft((current) => ({ ...current, reward_id: event.target.value }))} className="pp-input w-full"><option value="">无奖励</option>{rewards.filter((reward) => !reward.archived_at).map((reward) => <option key={reward.id} value={reward.id}>{reward.name}</option>)}</select></label>
          <div className="sm:col-span-2"><FieldLabel>默认生成坐标</FieldLabel><div className="grid grid-cols-3 gap-2"><Coordinate label="X" value={templateDraft.location.x} onChange={(value) => setTemplateDraft((current) => ({ ...current, location: { ...current.location, x: value } }))} /><Coordinate label="Y" value={templateDraft.location.y} onChange={(value) => setTemplateDraft((current) => ({ ...current, location: { ...current.location, y: value } }))} /><Coordinate label="Z" value={templateDraft.location.z} onChange={(value) => setTemplateDraft((current) => ({ ...current, location: { ...current.location, z: value } }))} /></div><input value={templateDraft.location.label || ''} onChange={(event) => setTemplateDraft((current) => ({ ...current, location: { ...current.location, label: event.target.value } }))} className="pp-input mt-2 w-full" placeholder="地点名称，例如：火山竞技场" /></div>
          <label className="flex items-center gap-3 rounded-xl border border-slate-200 p-3"><input type="checkbox" checked={templateDraft.capturable} onChange={(event) => setTemplateDraft((current) => ({ ...current, capturable: event.target.checked }))} className="h-4 w-4 rounded border-slate-300" /><span className="text-sm font-black text-slate-800">允许捕获</span></label>
          <label className="flex items-center gap-3 rounded-xl border border-slate-200 p-3"><input type="checkbox" checked={templateDraft.enabled} onChange={(event) => setTemplateDraft((current) => ({ ...current, enabled: event.target.checked }))} className="h-4 w-4 rounded border-slate-300" /><span className="text-sm font-black text-slate-800">启用模板</span></label>
        </div>
      </Modal>}
    </div>
  );
};

const Data: React.FC<{ label: string; value: React.ReactNode; mono?: boolean }> = ({ label, value, mono = false }) => (
  <div className="min-w-0 rounded-lg bg-slate-50 px-3 py-2"><div className="text-[10px] font-bold uppercase tracking-wider text-slate-400">{label}</div><div className={`mt-1 truncate font-bold text-slate-700 ${mono ? 'font-mono text-[11px]' : 'text-xs'}`}>{value}</div></div>
);

const IconButton: React.FC<{ title: string; icon: React.ReactNode; onClick: () => void; danger?: boolean }> = ({ title, icon, onClick, danger = false }) => (
  <button type="button" title={title} aria-label={title} onClick={onClick} className={`rounded-lg border p-2 transition ${danger ? 'border-rose-100 text-rose-500 hover:bg-rose-50' : 'border-slate-200 text-slate-500 hover:bg-slate-50 hover:text-slate-800'}`}>{icon}</button>
);

const ErrorBox: React.FC<{ error: unknown }> = ({ error }) => <div className="mb-4 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm font-semibold text-rose-700">{getErrorMessage(error)}</div>;

const Empty: React.FC<{ text: string }> = ({ text }) => <div className="col-span-full rounded-xl bg-slate-50 py-12 text-center text-sm text-slate-400">{text}</div>;

const Coordinate: React.FC<{ label: string; value: number; onChange: (value: number) => void }> = ({ label, value, onChange }) => <label><FieldLabel>{label}</FieldLabel><input type="number" value={value} onChange={(event) => onChange(Number(event.target.value) || 0)} className="pp-input w-full" /></label>;

const NumberField: React.FC<{ label: string; value: number; min: number; max: number; step?: number; onChange: (value: number) => void }> = ({ label, value, min, max, step = 1, onChange }) => <label><FieldLabel>{label}</FieldLabel><input type="number" min={min} max={max} step={step} value={value} onChange={(event) => onChange(Number(event.target.value) || min)} className="pp-input w-full" /></label>;

const RewardItemEditor: React.FC<{ item: BossRewardItem; onCount: (count: number) => void; onRemove: () => void }> = ({ item, onCount, onRemove }) => <div className="flex items-center gap-2 rounded-lg border border-slate-200 p-2"><code className="min-w-0 flex-1 truncate text-[11px] font-bold text-slate-600">{item.item_id}</code><input type="number" min={1} value={item.count} onChange={(event) => onCount(Number(event.target.value))} className="pp-input w-28" /><button type="button" onClick={onRemove} className="rounded-lg p-2 text-rose-500 hover:bg-rose-50"><X size={14} /></button></div>;

const CatalogSection: React.FC<React.PropsWithChildren<{ title: string; description: string; search: string; onSearch: (value: string) => void; loading: boolean; error: unknown }>> = ({ title, description, search, onSearch, loading, error, children }) => <section className="mt-5 rounded-xl border border-slate-200 p-4"><div className="flex items-start justify-between gap-3"><div><h3 className="font-black text-slate-800">{title}</h3><p className="mt-1 text-xs text-slate-400">{description}</p></div>{loading && <LoaderCircle className="animate-spin text-slate-400" size={16} />}</div><label className="relative mt-3 block"><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={14} /><input value={search} onChange={(event) => onSearch(event.target.value)} className="pp-input w-full pl-9" placeholder="搜索" /></label>{error && <div className="mt-2 text-xs font-semibold text-rose-600">{getErrorMessage(error)}</div>}<div className="mt-3">{children}</div></section>;

const Modal: React.FC<React.PropsWithChildren<{ title: string; subtitle: string; onClose: () => void; footer: React.ReactNode }>> = ({ title, subtitle, onClose, footer, children }) => <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/40 p-4" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}><div className="max-h-[92vh] w-full max-w-4xl overflow-y-auto rounded-2xl border border-slate-200 bg-white shadow-2xl"><div className="sticky top-0 z-10 flex items-center justify-between border-b border-slate-100 bg-white px-5 py-4"><div><h2 className="font-black text-slate-900">{title}</h2><p className="mt-1 font-mono text-[11px] text-slate-400">{subtitle}</p></div><button type="button" onClick={onClose} className="rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700"><X size={18} /></button></div><div className="p-5">{children}</div><div className="flex justify-end gap-2 border-t border-slate-100 px-5 py-4">{footer}</div></div></div>;

const SummonWaveList: React.FC<{
  loading: boolean;
  error: unknown;
  waves: BossSummonWave[];
  pending: boolean;
  transitions: (wave: BossSummonWave) => BossWaveActionStatus[];
  onTransition: (wave: BossSummonWave, status: BossWaveActionStatus) => void;
}> = ({ loading, error, waves, pending, transitions, onTransition }) => (
  <div className="mt-4 rounded-xl border border-slate-200 bg-slate-50 p-3">
    <div className="mb-2 flex items-center gap-2 text-xs font-black text-slate-700"><Layers3 size={14} />波次快照</div>
    {loading && <div className="py-4 text-center text-xs text-slate-400"><LoaderCircle className="mr-2 inline animate-spin" size={13} />加载中</div>}
    {error && <div className="text-xs font-semibold text-rose-600">{getErrorMessage(error)}</div>}
    <div className="space-y-2">
      {waves.map((wave) => <div key={wave.id} className="rounded-lg bg-white p-3">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2"><span className="font-black text-slate-800">第 {wave.position} 波 · {wave.name}</span><span className={`rounded-full px-2 py-0.5 text-[10px] font-bold ${waveStatusClass[wave.status]}`}>{waveStatusLabel[wave.status]}</span><span className="rounded-full bg-violet-50 px-2 py-0.5 text-[10px] font-bold text-violet-700">{waveKindLabel[wave.kind]}</span></div>
            <div className="mt-1 text-[11px] text-slate-500">{wave.pal_id} · Lv.{wave.level} × {wave.count} · HP {wave.hp_multiplier}× · 延迟 {formatDuration(wave.delay_seconds)}</div>
            {wave.failure && <div className="mt-2 text-xs font-semibold text-rose-600">{wave.failure}</div>}
          </div>
          <div className="flex flex-wrap gap-1.5">
            {transitions(wave).map((status) => <button key={status} type="button" disabled={pending} onClick={() => onTransition(wave, status)} className="pp-button">{waveStatusLabel[status]}</button>)}
          </div>
        </div>
      </div>)}
      {!loading && waves.length === 0 && <div className="py-3 text-center text-xs text-slate-400">暂无波次快照</div>}
    </div>
  </div>
);

const SummonEvents: React.FC<{ loading: boolean; error: unknown; events: BossSummonEvent[] }> = ({ loading, error, events }) => <div className="mt-4 rounded-xl border border-slate-200 bg-slate-50 p-3"><div className="mb-2 text-xs font-black text-slate-700">状态审计</div>{loading && <div className="py-4 text-center text-xs text-slate-400"><LoaderCircle className="mr-2 inline animate-spin" size={13} />加载中</div>}{error && <div className="text-xs font-semibold text-rose-600">{getErrorMessage(error)}</div>}<div className="space-y-2">{events.map((event) => <div key={event.id} className="flex items-start gap-3 rounded-lg bg-white p-3 text-xs"><span className={`mt-0.5 rounded-full px-2 py-0.5 font-bold ${summonStatusClass[event.to_status]}`}>{summonStatusLabel[event.to_status]}</span><div className="min-w-0 flex-1"><div className="text-slate-600">{event.message || `${event.from_status ? `${summonStatusLabel[event.from_status as BossSummonStatus] || event.from_status} → ` : ''}${summonStatusLabel[event.to_status]}`}</div><div className="mt-1 text-[10px] text-slate-400">{event.actor || '系统'} · {formatDate(event.created_at)}</div></div></div>)}{!loading && events.length === 0 && <div className="py-3 text-center text-xs text-slate-400">暂无审计事件</div>}</div></div>;

const formatDuration = (seconds: number) => {
  if (seconds <= 0) return '无';
  if (seconds % 86400 === 0) return `${seconds / 86400}天`;
  if (seconds % 3600 === 0) return `${seconds / 3600}小时`;
  if (seconds % 60 === 0) return `${seconds / 60}分钟`;
  return `${seconds}秒`;
};

const formatDate = (value?: string) => value ? new Date(value).toLocaleString('zh-CN') : '—';
