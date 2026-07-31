import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Archive,
  BadgeCheck,
  CircleAlert,
  Copy,
  Gift,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  RotateCcw,
  Save,
  Search,
  Target,
  Trophy,
  X,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import {
  operationsTasksApi,
  type OperationsTaskCycle,
  type OperationsTaskDefinition,
  type OperationsTaskInput,
  type OperationsTaskProgress,
} from '../api/operationsTasks';

interface Notice {
  type: 'success' | 'error';
  text: string;
}

interface TaskPreset {
  name: string;
  description: string;
  input: OperationsTaskInput;
}

const emptyInput = (): OperationsTaskInput => ({
  name: '',
  description: '',
  event_type: 'PAL_CAPTURED',
  target_amount: 1,
  reward_points: 10,
  cycle: 'daily',
  amount_field: 'count',
  filters: {},
  enabled: true,
});

const presets: TaskPreset[] = [
  {
    name: '每日捕获',
    description: '捕获指定数量帕鲁后发放积分。',
    input: {
      name: '每日捕获 3 只帕鲁',
      description: '每天捕获任意 3 只帕鲁。',
      event_type: 'PAL_CAPTURED',
      target_amount: 3,
      reward_points: 30,
      cycle: 'daily',
      amount_field: 'count',
      filters: {},
      enabled: true,
    },
  },
  {
    name: '每日击杀',
    description: '按击杀事件累计任务进度。',
    input: {
      name: '每日击杀 20 个目标',
      description: '每天击杀 20 个帕鲁或敌对目标。',
      event_type: 'PAL_KILLED',
      target_amount: 20,
      reward_points: 40,
      cycle: 'daily',
      amount_field: 'count',
      filters: {},
      enabled: true,
    },
  },
  {
    name: '在线时长',
    description: '由在线事件中的 minutes 字段推进。',
    input: {
      name: '每日在线 60 分钟',
      description: '每天累计在线 60 分钟。',
      event_type: 'PLAYER_ONLINE',
      target_amount: 60,
      reward_points: 50,
      cycle: 'daily',
      amount_field: 'minutes',
      filters: {},
      enabled: true,
    },
  },
  {
    name: 'Boss参与',
    description: '指定 Boss 击杀或活动完成任务。',
    input: {
      name: '每周击败指定 Boss',
      description: '每周参与并完成一次指定 Boss 击杀。',
      event_type: 'BOSS_KILLED',
      target_amount: 1,
      reward_points: 100,
      cycle: 'weekly',
      amount_field: 'count',
      filters: { boss_id: 'ExampleBoss' },
      enabled: true,
    },
  },
];

const eventSuggestions = ['PAL_CAPTURED', 'PAL_KILLED', 'PLAYER_ONLINE', 'BOSS_KILLED', 'PLAYER_LOGIN', 'ITEM_CRAFTED'];
const number = new Intl.NumberFormat('zh-CN');

const cycleLabel: Record<OperationsTaskCycle, string> = {
  once: '永久一次',
  daily: '每日',
  weekly: '每周',
};

const parseFilters = (value: string): Record<string, unknown> => {
  const trimmed = value.trim();
  if (!trimmed) return {};
  const parsed = JSON.parse(trimmed) as unknown;
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error('Payload过滤条件必须是JSON对象。');
  }
  return parsed as Record<string, unknown>;
};

const prettyFilters = (filters?: Record<string, unknown>) => JSON.stringify(filters || {}, null, 2);

export const OperationsTasks: React.FC = () => {
  const queryClient = useQueryClient();
  const [includeArchived, setIncludeArchived] = useState(false);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingID, setEditingID] = useState('');
  const [draft, setDraft] = useState<OperationsTaskInput>(emptyInput());
  const [filtersText, setFiltersText] = useState('{}');
  const [playerInput, setPlayerInput] = useState('');
  const [progressPlayerUID, setProgressPlayerUID] = useState('');
  const [notice, setNotice] = useState<Notice | null>(null);

  const definitionsQuery = useQuery({
    queryKey: ['operations-tasks', 'definitions', includeArchived],
    queryFn: () => operationsTasksApi.list(includeArchived),
  });

  const progressQuery = useQuery({
    queryKey: ['operations-tasks', 'progress', progressPlayerUID],
    queryFn: () => operationsTasksApi.progress(progressPlayerUID),
    enabled: Boolean(progressPlayerUID),
  });

  const saveMutation = useMutation({
    mutationFn: async () => {
      const input: OperationsTaskInput = {
        ...draft,
        name: draft.name.trim(),
        description: draft.description?.trim() || '',
        event_type: draft.event_type.trim().toUpperCase(),
        amount_field: draft.amount_field?.trim() || '',
        target_amount: Number(draft.target_amount),
        reward_points: Number(draft.reward_points),
        filters: parseFilters(filtersText),
      };
      if (!input.name) throw new Error('请填写任务名称。');
      if (!input.event_type) throw new Error('请填写事件类型。');
      if (!Number.isSafeInteger(input.target_amount) || input.target_amount <= 0) throw new Error('目标数量必须是大于0的整数。');
      if (!Number.isSafeInteger(input.reward_points) || input.reward_points < 0) throw new Error('奖励积分必须是非负整数。');
      return editingID ? operationsTasksApi.update(editingID, input) : operationsTasksApi.create(input);
    },
    onSuccess: async (definition) => {
      setNotice({ type: 'success', text: editingID ? `任务“${definition.name}”已更新。` : `任务“${definition.name}”已创建。` });
      closeEditor();
      await queryClient.invalidateQueries({ queryKey: ['operations-tasks'] });
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const archiveMutation = useMutation({
    mutationFn: (definition: OperationsTaskDefinition) => operationsTasksApi.archive(definition.id),
    onSuccess: async (definition) => {
      setNotice({ type: 'success', text: `任务“${definition.name}”已归档，历史进度和奖励记录仍然保留。` });
      await queryClient.invalidateQueries({ queryKey: ['operations-tasks'] });
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const retryMutation = useMutation({
    mutationFn: () => operationsTasksApi.retryRewards(100),
    onSuccess: async (result) => {
      setNotice({
        type: result.failed > 0 ? 'error' : 'success',
        text: `奖励重试完成：选中 ${result.selected}，成功 ${result.granted}，失败 ${result.failed}。`,
      });
      await queryClient.invalidateQueries({ queryKey: ['operations-tasks', 'progress'] });
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const definitions = definitionsQuery.data?.items || [];
  const activeDefinitions = useMemo(() => definitions.filter((item) => !item.archived_at), [definitions]);
  const enabledDefinitions = useMemo(() => activeDefinitions.filter((item) => item.enabled), [activeDefinitions]);
  const archivedDefinitions = useMemo(() => definitions.filter((item) => Boolean(item.archived_at)), [definitions]);
  const totalRewardPoints = useMemo(() => enabledDefinitions.reduce((sum, item) => sum + item.reward_points, 0), [enabledDefinitions]);

  function closeEditor() {
    setEditorOpen(false);
    setEditingID('');
    setDraft(emptyInput());
    setFiltersText('{}');
  }

  const openCreate = (input = emptyInput()) => {
    setEditingID('');
    setDraft({ ...input, filters: { ...(input.filters || {}) } });
    setFiltersText(prettyFilters(input.filters));
    setEditorOpen(true);
    setNotice(null);
  };

  const openEdit = (definition: OperationsTaskDefinition) => {
    setEditingID(definition.id);
    setDraft({
      name: definition.name,
      description: definition.description || '',
      event_type: definition.event_type,
      target_amount: definition.target_amount,
      reward_points: definition.reward_points,
      cycle: definition.cycle,
      amount_field: definition.amount_field || '',
      filters: definition.filters || {},
      enabled: definition.enabled,
    });
    setFiltersText(prettyFilters(definition.filters));
    setEditorOpen(true);
    setNotice(null);
  };

  const cloneDefinition = (definition: OperationsTaskDefinition) => {
    openCreate({
      name: `${definition.name}（副本）`,
      description: definition.description || '',
      event_type: definition.event_type,
      target_amount: definition.target_amount,
      reward_points: definition.reward_points,
      cycle: definition.cycle,
      amount_field: definition.amount_field || '',
      filters: { ...(definition.filters || {}) },
      enabled: false,
    });
  };

  const lookupProgress = () => {
    const value = playerInput.trim();
    if (!value) {
      setNotice({ type: 'error', text: '请输入PlayerUID。' });
      return;
    }
    setProgressPlayerUID(value);
    setNotice(null);
  };

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['operations-tasks'] });
  };

  return (
    <div className="mx-auto flex w-full max-w-[1500px] flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <div className="mb-2 flex items-center gap-2 text-xs font-bold uppercase tracking-[0.18em] text-violet-600"><Target size={15} />运营任务</div>
            <h1 className="text-2xl font-black tracking-tight text-slate-900">任务系统</h1>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-500">配置每日、每周和永久一次任务。游戏事件按事件类型、数量字段和Payload过滤条件推进进度，完成后通过统一积分账本发放奖励。</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <button type="button" onClick={() => void refresh()} className="pp-button"><RefreshCw size={14} />刷新</button>
            <button type="button" onClick={() => retryMutation.mutate()} disabled={retryMutation.isPending} className="pp-button">
              {retryMutation.isPending ? <LoaderCircle className="animate-spin" size={14} /> : <RotateCcw size={14} />}重试奖励
            </button>
            <button type="button" onClick={() => openCreate()} className="pp-btn pp-btn--primary"><Plus size={15} />新建任务</button>
          </div>
        </div>
        {notice && <div className={`mt-4 rounded-xl border px-4 py-3 text-sm font-semibold ${notice.type === 'success' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-rose-200 bg-rose-50 text-rose-700'}`}>{notice.text}</div>}
      </section>

      <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Metric label="任务模板" value={activeDefinitions.length} icon={<Target size={18} />} />
        <Metric label="已启用" value={enabledDefinitions.length} icon={<BadgeCheck size={18} />} />
        <Metric label="单周期总奖励" value={totalRewardPoints} suffix="积分" icon={<Gift size={18} />} />
        <Metric label="已归档" value={archivedDefinitions.length} icon={<Archive size={18} />} />
      </section>

      <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <h2 className="font-black text-slate-900">快速模板</h2>
            <p className="mt-1 text-xs leading-5 text-slate-400">模板只填充编辑器，不会直接保存。事件桥接器必须上报相同的事件类型和字段。</p>
          </div>
          <label className="flex items-center gap-2 text-xs font-bold text-slate-500">
            <input type="checkbox" checked={includeArchived} onChange={(event) => setIncludeArchived(event.target.checked)} className="h-4 w-4 rounded border-slate-300" />显示归档任务
          </label>
        </div>
        <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          {presets.map((preset) => (
            <button key={preset.name} type="button" onClick={() => openCreate(preset.input)} className="rounded-xl border border-slate-200 p-4 text-left transition hover:border-violet-300 hover:bg-violet-50/40">
              <div className="font-black text-slate-800">{preset.name}</div>
              <div className="mt-1 text-xs leading-5 text-slate-500">{preset.description}</div>
              <div className="mt-3 font-mono text-[11px] font-bold text-violet-600">{preset.input.event_type}</div>
            </button>
          ))}
        </div>
      </section>

      <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="mb-4 flex items-center justify-between gap-3">
          <div>
            <h2 className="font-black text-slate-900">任务定义</h2>
            <p className="mt-1 text-xs text-slate-400">归档不会删除玩家历史进度和已发放奖励。</p>
          </div>
          {definitionsQuery.isFetching && <LoaderCircle className="animate-spin text-slate-400" size={18} />}
        </div>
        {definitionsQuery.error && <div className="mb-4 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm font-semibold text-rose-700">{getErrorMessage(definitionsQuery.error)}</div>}
        <div className="overflow-x-auto rounded-xl border border-slate-100">
          <table className="min-w-full text-left text-sm">
            <thead className="bg-slate-50 text-xs font-bold text-slate-500">
              <tr><th className="px-4 py-3">任务</th><th className="px-4 py-3">事件</th><th className="px-4 py-3">周期</th><th className="px-4 py-3 text-right">目标</th><th className="px-4 py-3 text-right">奖励</th><th className="px-4 py-3">状态</th><th className="px-4 py-3 text-right">操作</th></tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {definitions.map((definition) => (
                <tr key={definition.id} className={definition.archived_at ? 'bg-slate-50/70 text-slate-400' : 'hover:bg-slate-50/50'}>
                  <td className="px-4 py-3"><div className="font-bold text-slate-800">{definition.name}</div><div className="mt-1 max-w-md text-xs leading-5 text-slate-400">{definition.description || definition.id}</div></td>
                  <td className="px-4 py-3"><code className="rounded bg-slate-100 px-2 py-1 text-[11px] font-bold text-slate-600">{definition.event_type}</code><div className="mt-1 text-[11px] text-slate-400">字段：{definition.amount_field || '默认1'}</div></td>
                  <td className="px-4 py-3 text-xs font-bold text-slate-600">{cycleLabel[definition.cycle]}</td>
                  <td className="px-4 py-3 text-right font-black text-slate-800">{number.format(definition.target_amount)}</td>
                  <td className="px-4 py-3 text-right font-black text-violet-600">{number.format(definition.reward_points)}</td>
                  <td className="px-4 py-3"><Status definition={definition} /></td>
                  <td className="px-4 py-3"><div className="flex justify-end gap-1.5">
                    <IconButton title="复制任务" onClick={() => cloneDefinition(definition)} icon={<Copy size={14} />} />
                    {!definition.archived_at && <IconButton title="编辑任务" onClick={() => openEdit(definition)} icon={<Pencil size={14} />} />}
                    {!definition.archived_at && <IconButton title="归档任务" danger onClick={() => {
                      if (window.confirm(`归档任务“${definition.name}”？历史记录会保留。`)) archiveMutation.mutate(definition);
                    }} icon={<Archive size={14} />} />}
                  </div></td>
                </tr>
              ))}
              {!definitionsQuery.isLoading && definitions.length === 0 && <tr><td colSpan={7} className="px-4 py-12 text-center text-sm text-slate-400">暂无任务定义。可以从快速模板开始创建。</td></tr>}
            </tbody>
          </table>
        </div>
      </section>

      <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <h2 className="font-black text-slate-900">玩家任务进度</h2>
            <p className="mt-1 text-xs text-slate-400">输入完整PlayerUID查看当前周期任务、完成状态和奖励发放结果。</p>
          </div>
          <div className="flex w-full max-w-xl gap-2">
            <label className="relative block min-w-0 flex-1"><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} /><input value={playerInput} onChange={(event) => setPlayerInput(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') lookupProgress(); }} className="pp-input w-full pl-9 font-mono" placeholder="PlayerUID" /></label>
            <button type="button" onClick={lookupProgress} className="pp-btn pp-btn--primary">查询</button>
          </div>
        </div>
        {progressPlayerUID && <div className="mt-5 grid gap-3 lg:grid-cols-2">
          {progressQuery.error && <div className="col-span-full rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm font-semibold text-rose-700">{getErrorMessage(progressQuery.error)}</div>}
          {(progressQuery.data?.items || []).map((item) => <ProgressCard key={`${item.task_id}-${item.cycle_key}`} item={item} />)}
          {progressQuery.isLoading && <div className="col-span-full py-10 text-center text-sm text-slate-400"><LoaderCircle className="mr-2 inline animate-spin" size={15} />正在查询任务进度...</div>}
          {!progressQuery.isLoading && (progressQuery.data?.items.length || 0) === 0 && <div className="col-span-full rounded-xl bg-slate-50 py-10 text-center text-sm text-slate-400">该玩家当前没有匹配的任务进度。</div>}
        </div>}
      </section>

      {editorOpen && <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/40 p-4" onMouseDown={(event) => { if (event.target === event.currentTarget) closeEditor(); }}>
        <div className="max-h-[92vh] w-full max-w-3xl overflow-y-auto rounded-2xl border border-slate-200 bg-white shadow-2xl">
          <div className="sticky top-0 z-10 flex items-center justify-between border-b border-slate-100 bg-white px-5 py-4">
            <div><h2 className="font-black text-slate-900">{editingID ? '编辑任务' : '新建任务'}</h2><p className="mt-1 font-mono text-[11px] text-slate-400">{editingID || '保存后生成任务ID'}</p></div>
            <button type="button" onClick={closeEditor} className="rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700"><X size={18} /></button>
          </div>
          <div className="grid gap-4 p-5 sm:grid-cols-2">
            <label className="block sm:col-span-2"><FieldLabel>任务名称</FieldLabel><input value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} maxLength={120} className="pp-input w-full" /></label>
            <label className="block sm:col-span-2"><FieldLabel>说明</FieldLabel><textarea value={draft.description || ''} onChange={(event) => setDraft((current) => ({ ...current, description: event.target.value }))} rows={2} maxLength={500} className="pp-input w-full resize-y" /></label>
            <label className="block"><FieldLabel>事件类型</FieldLabel><input list="operations-task-events" value={draft.event_type} onChange={(event) => setDraft((current) => ({ ...current, event_type: event.target.value }))} className="pp-input w-full font-mono" /><datalist id="operations-task-events">{eventSuggestions.map((eventType) => <option key={eventType} value={eventType} />)}</datalist></label>
            <label className="block"><FieldLabel>周期</FieldLabel><select value={draft.cycle} onChange={(event) => setDraft((current) => ({ ...current, cycle: event.target.value as OperationsTaskCycle }))} className="pp-input w-full"><option value="daily">每日</option><option value="weekly">每周</option><option value="once">永久一次</option></select></label>
            <label className="block"><FieldLabel>目标数量</FieldLabel><input type="number" min={1} max={1000000000000} value={draft.target_amount} onChange={(event) => setDraft((current) => ({ ...current, target_amount: Number(event.target.value) }))} className="pp-input w-full" /></label>
            <label className="block"><FieldLabel>奖励积分</FieldLabel><input type="number" min={0} max={1000000000000} value={draft.reward_points} onChange={(event) => setDraft((current) => ({ ...current, reward_points: Number(event.target.value) }))} className="pp-input w-full" /></label>
            <label className="block sm:col-span-2"><FieldLabel>数量字段</FieldLabel><input value={draft.amount_field || ''} onChange={(event) => setDraft((current) => ({ ...current, amount_field: event.target.value }))} className="pp-input w-full font-mono" placeholder="count、minutes；留空时每个事件增加1" /><span className="mt-1.5 block text-xs leading-5 text-slate-400">支持Payload中的字段路径。字段不存在、不是正数或超过单事件上限时，事件不会推进该任务。</span></label>
            <label className="block sm:col-span-2"><FieldLabel>Payload过滤条件（JSON对象）</FieldLabel><textarea value={filtersText} onChange={(event) => setFiltersText(event.target.value)} rows={6} spellCheck={false} className="pp-input w-full resize-y font-mono text-xs" placeholder={'{\n  "pal_id": "SheepBall"\n}'} /><span className="mt-1.5 block text-xs leading-5 text-slate-400">所有字段都必须匹配。空对象表示接受该事件类型的所有事件。</span></label>
            <label className="flex items-center gap-3 rounded-xl border border-slate-200 p-4 sm:col-span-2"><input type="checkbox" checked={draft.enabled} onChange={(event) => setDraft((current) => ({ ...current, enabled: event.target.checked }))} className="h-4 w-4 rounded border-slate-300" /><span><span className="block text-sm font-black text-slate-800">立即启用</span><span className="mt-1 block text-xs text-slate-400">关闭时任务定义仍保留，但新事件不会推进进度。</span></span></label>
          </div>
          <div className="flex justify-end gap-2 border-t border-slate-100 px-5 py-4"><button type="button" onClick={closeEditor} className="pp-button">取消</button><button type="button" disabled={saveMutation.isPending} onClick={() => saveMutation.mutate()} className="pp-btn pp-btn--primary">{saveMutation.isPending ? <LoaderCircle className="animate-spin" size={15} /> : <Save size={15} />}{editingID ? '保存修改' : '创建任务'}</button></div>
        </div>
      </div>}
    </div>
  );
};

const FieldLabel: React.FC<React.PropsWithChildren> = ({ children }) => <span className="mb-1.5 block text-xs font-bold text-slate-500">{children}</span>;

const Metric: React.FC<{ label: string; value: number; suffix?: string; icon: React.ReactNode }> = ({ label, value, suffix, icon }) => (
  <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm"><div className="mb-4 flex items-center justify-between text-slate-400"><span className="text-xs font-bold uppercase tracking-wider">{label}</span><span className="rounded-lg bg-violet-50 p-2 text-violet-500">{icon}</span></div><div className="text-2xl font-black tracking-tight text-slate-900">{number.format(value)}{suffix && <span className="ml-1 text-sm text-slate-400">{suffix}</span>}</div></div>
);

const Status: React.FC<{ definition: OperationsTaskDefinition }> = ({ definition }) => {
  if (definition.archived_at) return <span className="inline-flex rounded-full bg-slate-100 px-2.5 py-1 text-[11px] font-bold text-slate-500">已归档</span>;
  return definition.enabled
    ? <span className="inline-flex rounded-full bg-emerald-50 px-2.5 py-1 text-[11px] font-bold text-emerald-700">启用</span>
    : <span className="inline-flex rounded-full bg-amber-50 px-2.5 py-1 text-[11px] font-bold text-amber-700">停用</span>;
};

const IconButton: React.FC<{ title: string; icon: React.ReactNode; onClick: () => void; danger?: boolean }> = ({ title, icon, onClick, danger = false }) => (
  <button type="button" title={title} aria-label={title} onClick={onClick} className={`rounded-lg border p-2 transition ${danger ? 'border-rose-100 text-rose-500 hover:bg-rose-50' : 'border-slate-200 text-slate-500 hover:bg-slate-50 hover:text-slate-800'}`}>{icon}</button>
);

const ProgressCard: React.FC<{ item: OperationsTaskProgress }> = ({ item }) => {
  const percent = Math.min(100, Math.max(0, item.target_amount > 0 ? Math.round((item.progress / item.target_amount) * 100) : 0));
  const rewardStatus = item.reward_granted ? '奖励已发放' : item.reward_status === 'failed' ? '奖励失败' : item.completed ? '等待奖励' : '进行中';
  const rewardClass = item.reward_granted ? 'text-emerald-700 bg-emerald-50' : item.reward_status === 'failed' ? 'text-rose-700 bg-rose-50' : 'text-amber-700 bg-amber-50';
  return <article className="rounded-xl border border-slate-200 p-4">
    <div className="flex items-start justify-between gap-3"><div><div className="font-black text-slate-800">{item.task_name}</div><div className="mt-1 text-xs text-slate-400">{cycleLabel[item.cycle]} · {item.cycle_key}</div></div><span className={`rounded-full px-2.5 py-1 text-[11px] font-bold ${rewardClass}`}>{rewardStatus}</span></div>
    <div className="mt-4 h-2 overflow-hidden rounded-full bg-slate-100"><div className="h-full rounded-full bg-violet-500 transition-all" style={{ width: `${percent}%` }} /></div>
    <div className="mt-2 flex items-center justify-between text-xs"><span className="font-bold text-slate-500">{number.format(item.progress)} / {number.format(item.target_amount)}</span><span className="font-black text-violet-600">奖励 {number.format(item.reward_points)} 积分</span></div>
    {item.reward_error && <div className="mt-3 flex gap-2 rounded-lg bg-rose-50 p-3 text-xs leading-5 text-rose-700"><CircleAlert className="mt-0.5 shrink-0" size={14} /><span>{item.reward_error}</span></div>}
    {item.completed_at && <div className="mt-3 flex items-center gap-2 text-[11px] text-slate-400"><Trophy size={13} />完成于 {new Date(item.completed_at).toLocaleString()}</div>}
  </article>;
};
