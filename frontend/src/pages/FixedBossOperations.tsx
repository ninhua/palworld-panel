import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Archive,
  CheckCircle2,
  CircleAlert,
  Crown,
  Gift,
  LoaderCircle,
  MapPin,
  Package,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Search,
  ShieldAlert,
  Trash2,
  X,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import {
  bossApi,
  type BossExecutionAttempt,
  type BossRewardInput,
  type BossRewardItem,
  type BossSummon,
  type BossTemplate,
  type BossTemplateInput,
} from '../api/boss';
import { palDefenderGMApi } from '../api/paldefenderGM';
import { starterGiftApi, type PalTemplateInfo, type StarterGiftCatalogItem } from '../api/starterGift';
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

type EditorTab = 'boss-template' | 'reward-items' | 'reward-templates';
type ItemCategory =
  | 'all'
  | 'sphere'
  | 'tool'
  | 'melee'
  | 'firearm'
  | 'ammo'
  | 'armor'
  | 'food'
  | 'medicine'
  | 'basic-material'
  | 'advanced-material'
  | 'key-item'
  | 'schematic'
  | 'other';

interface CatalogTemplate extends PalTemplateInfo {
  live_path?: string;
  live_size?: number;
  live_modified_at?: string;
  indexed: boolean;
}

interface BossDraft {
  name: string;
  description: string;
  palTemplateFile: string;
  palID: string;
  palLevel: number;
  palNickname: string;
  count: number;
  spawnRadius: number;
  capturable: boolean;
  cooldownSeconds: number;
  lifecycleTimeoutSeconds: number;
  location: { x: number; y: number; z: number; label: string };
  enabled: boolean;
  rewardEnabled: boolean;
  rewardID: string;
  rewardPoints: number;
  rewardItems: BossRewardItem[];
  rewardPalTemplates: string[];
}

const emptyDraft = (): BossDraft => ({
  name: '',
  description: '',
  palTemplateFile: '',
  palID: '',
  palLevel: 1,
  palNickname: '',
  count: 1,
  spawnRadius: 0,
  capturable: false,
  cooldownSeconds: 300,
  lifecycleTimeoutSeconds: 3600,
  location: { x: 0, y: 0, z: 0, label: '' },
  enabled: true,
  rewardEnabled: false,
  rewardID: '',
  rewardPoints: 0,
  rewardItems: [],
  rewardPalTemplates: [],
});

const itemCategories: Array<{ id: ItemCategory; label: string }> = [
  { id: 'all', label: '全部' },
  { id: 'sphere', label: '帕鲁球' },
  { id: 'tool', label: '工具' },
  { id: 'melee', label: '近战武器' },
  { id: 'firearm', label: '枪械与弓' },
  { id: 'ammo', label: '弹药与投掷物' },
  { id: 'armor', label: '防具与饰品' },
  { id: 'food', label: '食物与农作物' },
  { id: 'medicine', label: '药品' },
  { id: 'basic-material', label: '基础材料' },
  { id: 'advanced-material', label: '高级材料' },
  { id: 'key-item', label: '鞍具与关键物品' },
  { id: 'schematic', label: '图纸与技能果实' },
  { id: 'other', label: '其他' },
];

const number = new Intl.NumberFormat('zh-CN');
const asRecord = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};
const metadataString = (metadata: unknown, key: string) => {
  const value = asRecord(metadata)[key];
  return typeof value === 'string' ? value.trim() : '';
};
const normalizeTemplateKey = (value: string) => value.trim().toLowerCase().replace(/\.json$/, '');
const isFixedBossMetadata = (metadata: unknown) => metadataString(metadata, 'activity_kind') === 'fixed_boss';
const templateDisplayName = (template: PalTemplateInfo) =>
  template.pal_name?.trim() || template.nickname?.trim() || template.name;
const templateSearchText = (template: PalTemplateInfo) =>
  `${template.pal_name || ''} ${template.english_name || ''} ${template.pal_id || ''} ${template.nickname || ''} ${template.category || ''} ${template.usage_category || ''} ${template.overall_grade || ''} ${(template.classification_tags || []).join(' ')} ${(template.passive_names || []).join(' ')} ${(template.index_names || []).join(' ')} ${template.name}`.toLowerCase();
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—';
const newRequestKey = () => {
  const uuid = globalThis.crypto?.randomUUID?.();
  return uuid ? `fixed-boss-${uuid}` : `fixed-boss-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
};
const uniqueStrings = (values: string[]) => Array.from(new Set(values.map((value) => value.trim()).filter(Boolean)));
const clampInt = (value: number, min: number, max: number) => Math.max(min, Math.min(max, Math.trunc(Number(value) || 0)));

const classifyItem = (item: StarterGiftCatalogItem): ItemCategory => {
  const value = `${item.id} ${item.name}`.toLowerCase();
  if (/sphere|palball|帕鲁球/.test(value)) return 'sphere';
  if (/pickaxe|axe|torch|grappling|fishing|repair|tool|镐|斧|火把|抓钩|钓竿|修理工具/.test(value)) return 'tool';
  if (/sword|spear|bat|club|knife|machete|melee|剑|矛|棍|棒|刀|近战/.test(value)) return 'melee';
  if (/rifle|shotgun|pistol|revolver|musket|gun|bow|crossbow|launcher|步枪|霰弹|手枪|左轮|火枪|弓|弩|发射器/.test(value)) return 'firearm';
  if (/bullet|ammo|arrow|rocket|missile|grenade|bomb|mine|子弹|弹药|箭|火箭|导弹|手榴弹|炸弹|地雷/.test(value)) return 'ammo';
  if (/armor|helmet|shield|cloth|wear|accessory|ring|pendant|underwear|盔甲|头盔|护盾|防具|服装|饰品|戒指|吊坠|内衣/.test(value)) return 'armor';
  if (/food|meat|bread|berry|milk|egg|honey|wheat|tomato|lettuce|potato|carrot|seed|食物|肉|面包|莓|牛奶|蛋|蜂蜜|小麦|番茄|生菜|土豆|胡萝卜|种子/.test(value)) return 'food';
  if (/medicine|drug|herb|potion|remedy|elixir|药|医疗|草药|药水|秘药/.test(value)) return 'medicine';
  if (/schematic|blueprint|skillfruit|skill_fruit|fruit_.*skill|图纸|设计图|技能果实/.test(value)) return 'schematic';
  if (/saddle|key|ticket|manual|technology|techpoint|point|treasure|鞍具|钥匙|券|手册|科技点|宝箱/.test(value)) return 'key-item';
  if (/ingot|ore|coal|sulfur|quartz|cement|polymer|carbon|circuit|parts|crystal|锭|矿石|煤|硫|石英|水泥|聚合物|碳纤维|电路|零件|结晶/.test(value)) return 'advanced-material';
  if (/wood|stone|fiber|wool|leather|bone|horn|fluid|oil|paladium|paldium|木材|石头|纤维|羊毛|皮革|骨头|角|体液|油|帕鲁矿/.test(value)) return 'basic-material';
  return 'other';
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
const summonLifecycle = (summon: BossSummon) => asRecord(asRecord(summon.result).boss_lifecycle);
const lifecycleNumber = (value: unknown) => Number.isFinite(Number(value)) ? Number(value) : 0;
const summonDisplayStatus = (summon: BossSummon) => {
  const lifecycle = summonLifecycle(summon);
  const state = String(lifecycle.state || '');
  if (summon.status === 'active' && state === 'monitoring') return `存活检测中 ${lifecycleNumber(lifecycle.observed_count)}/${lifecycleNumber(lifecycle.expected_count)}`;
  if (summon.status === 'active' && state === 'timed_out') return '检测超时 · 待人工核对';
  if (summon.status === 'completed' && state === 'completed') return '已确认击败';
  return summonStatusLabel[summon.status];
};
const summonResultText = (summon: BossSummon) => {
  const lifecycle = summonLifecycle(summon);
  const state = String(lifecycle.state || '');
  if (state === 'monitoring') return `已检测死亡 ${lifecycleNumber(lifecycle.observed_count)}/${lifecycleNumber(lifecycle.expected_count)}，截止 ${formatTime(String(lifecycle.deadline_at || ''))}`;
  if (state === 'completed') return `PalDefender 已确认 ${lifecycleNumber(lifecycle.observed_count)}/${lifecycleNumber(lifecycle.expected_count)} 只目标死亡`;
  if (state === 'timed_out') return String(lifecycle.last_error || '检测超时，需要人工核对游戏状态');
  return summon.failure || (summon.status === 'completed' ? '召唤命令已完成' : '—');
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
const ItemIcon: React.FC<{ item: StarterGiftCatalogItem }> = ({ item }) => {
  const source = item.icon ? `/assets/items/${encodeURIComponent(item.icon)}.webp` : '';
  return (
    <span className="relative flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-slate-200 bg-slate-50 text-slate-400">
      <Package size={18} />
      {source && <img src={source} alt="" loading="lazy" className="absolute inset-0 size-full object-contain p-1" onError={(event) => { event.currentTarget.style.display = 'none'; }} />}
    </span>
  );
};

export const FixedBossOperations: React.FC = () => {
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<Notice | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editorTab, setEditorTab] = useState<EditorTab>('boss-template');
  const [editingID, setEditingID] = useState('');
  const [draft, setDraft] = useState<BossDraft>(emptyDraft);
  const [definitionSearch, setDefinitionSearch] = useState('');
  const [bossTemplateSearch, setBossTemplateSearch] = useState('');
  const [bossTemplateFilters, setBossTemplateFilters] = useState(createEmptyPalTemplateFilters);
  const [rewardTemplateSearch, setRewardTemplateSearch] = useState('');
  const [rewardTemplateFilters, setRewardTemplateFilters] = useState(createEmptyPalTemplateFilters);
  const [itemSearch, setItemSearch] = useState('');
  const [itemCategory, setItemCategory] = useState<ItemCategory>('all');
  const [resolvingTemplate, setResolvingTemplate] = useState('');
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
  const rewardsQuery = useQuery({
    queryKey: ['fixed-boss', 'rewards'],
    queryFn: () => bossApi.rewards(true),
  });
  const summonsQuery = useQuery({
    queryKey: ['fixed-boss', 'summons'],
    queryFn: () => bossApi.summons(''),
    refetchInterval: 10_000,
  });
  const liveTemplateQuery = useQuery({
    queryKey: ['fixed-boss', 'paldefender-live-templates'],
    queryFn: palDefenderGMApi.templates,
    staleTime: 5 * 60 * 1000,
  });
  const itemCatalogQuery = useQuery({
    queryKey: ['fixed-boss', 'paldefender-items'],
    queryFn: () => palDefenderGMApi.items('', 5000),
    staleTime: 30 * 60 * 1000,
  });
  const enrichmentQuery = useQuery({
    queryKey: ['fixed-boss', 'template-enrichment'],
    queryFn: starterGiftApi.get,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['fixed-boss'] }),
      queryClient.invalidateQueries({ queryKey: ['boss'] }),
    ]);
  };

  const catalog = useMemo<CatalogTemplate[]>(() => {
    const enriched = new Map<string, PalTemplateInfo>();
    for (const item of enrichmentQuery.data?.templates || []) enriched.set(normalizeTemplateKey(item.name), item);
    return (liveTemplateQuery.data?.templates || []).map((live) => {
      const item = enriched.get(normalizeTemplateKey(live.name));
      return {
        name: live.name,
        pal_id: item?.pal_id,
        pal_name: item?.pal_name,
        english_name: item?.english_name,
        category: item?.category,
        usage_category: item?.usage_category,
        overall_grade: item?.overall_grade,
        index_names: item?.index_names || [],
        classification_tags: item?.classification_tags || [],
        graduation_pal: item?.graduation_pal,
        graduation_uses: item?.graduation_uses || [],
        current_graduation_use: item?.current_graduation_use,
        transitional: item?.transitional,
        passive_names: item?.passive_names || [],
        night_work_mode: item?.night_work_mode,
        nickname: item?.nickname,
        level: item?.level,
        modified_at: item?.modified_at || live.modified_at,
        size: item?.size || live.size,
        parse_error: item?.parse_error,
        live_path: live.path,
        live_size: live.size,
        live_modified_at: live.modified_at,
        indexed: Boolean(item),
      };
    }).sort((left, right) => templateDisplayName(left).localeCompare(templateDisplayName(right), 'zh-CN'));
  }, [enrichmentQuery.data, liveTemplateQuery.data]);
  const catalogByName = useMemo(() => new Map(catalog.map((item) => [normalizeTemplateKey(item.name), item])), [catalog]);
  const selectedTemplate = catalogByName.get(normalizeTemplateKey(draft.palTemplateFile));

  const allDefinitions = definitionsQuery.data?.items || [];
  const definitions = useMemo(() => allDefinitions.filter((item) => isFixedBossMetadata(item.metadata)), [allDefinitions]);
  const definitionIDs = useMemo(() => new Set(definitions.map((item) => item.id)), [definitions]);
  const summons = useMemo(
    () => (summonsQuery.data?.items || []).filter((item) => isFixedBossMetadata(item.metadata) || definitionIDs.has(item.template_id)),
    [summonsQuery.data, definitionIDs],
  );
  const rewards = rewardsQuery.data?.items || [];
  const rewardByID = useMemo(() => new Map(rewards.map((item) => [item.id, item])), [rewards]);
  const itemCatalog: StarterGiftCatalogItem[] = useMemo(
    () => (itemCatalogQuery.data?.items || []).map((item) => ({ id: item.id, name: item.name, icon: item.icon })),
    [itemCatalogQuery.data],
  );
  const itemByID = useMemo(() => new Map(itemCatalog.map((item) => [item.id, item])), [itemCatalog]);
  const selectedRewardItemIDs = useMemo(() => new Set(draft.rewardItems.map((item) => item.item_id)), [draft.rewardItems]);
  const selectedRewardTemplateNames = useMemo(() => new Set(draft.rewardPalTemplates), [draft.rewardPalTemplates]);

  const visibleDefinitions = useMemo(() => {
    const query = definitionSearch.trim().toLowerCase();
    return definitions
      .filter((item) => !query || `${item.name} ${item.description || ''} ${metadataString(item.metadata, 'pal_template_file')} ${item.location.label || ''}`.toLowerCase().includes(query))
      .sort((left, right) => right.updated_at.localeCompare(left.updated_at));
  }, [definitions, definitionSearch]);
  const visibleBossTemplates = useMemo(() => {
    const query = bossTemplateSearch.trim().toLowerCase();
    return catalog
      .filter((item) => palTemplateMatchesFilters(item, bossTemplateFilters))
      .filter((item) => !query || templateSearchText(item).includes(query))
      .slice(0, 500);
  }, [bossTemplateFilters, bossTemplateSearch, catalog]);
  const visibleRewardTemplates = useMemo(() => {
    const query = rewardTemplateSearch.trim().toLowerCase();
    return catalog
      .filter((item) => palTemplateMatchesFilters(item, rewardTemplateFilters))
      .filter((item) => !query || templateSearchText(item).includes(query))
      .slice(0, 500);
  }, [catalog, rewardTemplateFilters, rewardTemplateSearch]);
  const visibleItems = useMemo(() => {
    const query = itemSearch.trim().toLowerCase();
    return itemCatalog
      .filter((item) => itemCategory === 'all' || classifyItem(item) === itemCategory)
      .filter((item) => !query || `${item.id} ${item.name}`.toLowerCase().includes(query))
      .sort((left, right) => left.name.localeCompare(right.name, 'zh-CN'));
  }, [itemCatalog, itemCategory, itemSearch]);

  const chooseBossTemplate = async (item: CatalogTemplate) => {
    setResolvingTemplate(item.name);
    try {
      let palID = item.pal_id?.trim() || '';
      let level = Math.max(1, Math.trunc(item.level || 1));
      let nickname = item.nickname?.trim() || '';
      if (!palID) {
        const detail = asRecord(await palDefenderGMApi.template(item.name));
        palID = String(detail.PalID || '').trim();
        level = Math.max(1, Math.trunc(Number(detail.Level || 1)));
        nickname = String(detail.Nickname || '').trim();
      }
      if (!palID) throw new Error(`模板 ${item.name} 不包含 PalID，不能用于 PalDefender 召唤。`);
      setDraft((current) => ({
        ...current,
        palTemplateFile: item.name,
        palID,
        palLevel: level,
        palNickname: nickname,
        name: current.name.trim() ? current.name : templateDisplayName({ ...item, pal_id: palID, nickname }),
      }));
    } catch (error) {
      setNotice({ type: 'error', text: `读取 PalTemplate 失败：${getErrorMessage(error)}` });
    } finally {
      setResolvingTemplate('');
    }
  };

  const toggleRewardTemplate = (item: CatalogTemplate) => {
    setDraft((current) => ({
      ...current,
      rewardEnabled: true,
      rewardPalTemplates: current.rewardPalTemplates.includes(item.name)
        ? current.rewardPalTemplates.filter((name) => name !== item.name)
        : [...current.rewardPalTemplates, item.name],
    }));
  };
  const toggleRewardItem = (item: StarterGiftCatalogItem) => {
    setDraft((current) => ({
      ...current,
      rewardEnabled: true,
      rewardItems: current.rewardItems.some((entry) => entry.item_id === item.id)
        ? current.rewardItems.filter((entry) => entry.item_id !== item.id)
        : [...current.rewardItems, { item_id: item.id, count: 1 }],
    }));
  };
  const updateRewardItemCount = (itemID: string, value: number) => {
    setDraft((current) => ({
      ...current,
      rewardItems: current.rewardItems.map((item) => item.item_id === itemID ? { ...item, count: clampInt(value, 1, 999999) } : item),
    }));
  };

  const saveMutation = useMutation({
    mutationFn: async () => {
      if (!draft.name.trim()) throw new Error('Boss 名称不能为空。');
      if (!draft.palTemplateFile || !catalogByName.has(normalizeTemplateKey(draft.palTemplateFile))) {
        throw new Error('必须从当前 PalDefender 模板目录选择模板。');
      }
      if (!draft.palID.trim()) throw new Error('所选模板尚未解析出 PalID，请重新点击模板。');

      let rewardID = '';
      if (draft.rewardEnabled) {
        const rewardInput: BossRewardInput = {
          name: `${draft.name.trim()} 奖励`,
          description: `固定坐标 Boss“${draft.name.trim()}”的参与奖励`,
          points: clampInt(draft.rewardPoints, 0, 1_000_000_000),
          items: draft.rewardItems
            .filter((item) => item.item_id.trim() && itemByID.has(item.item_id))
            .map((item) => ({ item_id: item.item_id.trim(), count: clampInt(item.count, 1, 999999) })),
          pal_templates: uniqueStrings(draft.rewardPalTemplates).filter((name) => catalogByName.has(normalizeTemplateKey(name))),
          enabled: true,
          metadata: { activity_kind: 'fixed_boss_reward', source: 'fixed_boss_operations' },
        };
        const reward = draft.rewardID
          ? await bossApi.updateReward(draft.rewardID, rewardInput)
          : await bossApi.createReward(rewardInput);
        rewardID = reward.id;
      }

      const input: BossTemplateInput = {
        name: draft.name.trim(),
        description: draft.description.trim(),
        pal_id: draft.palID.trim(),
        level: clampInt(draft.palLevel, 1, 100),
        count: clampInt(draft.count, 1, 50),
        hp_multiplier: 1,
        attack_multiplier: 1,
        defense_multiplier: 1,
        spawn_radius: Math.max(0, Math.min(10000, Number(draft.spawnRadius) || 0)),
        capturable: draft.capturable,
        cooldown_seconds: clampInt(draft.cooldownSeconds, 0, 86400),
        reward_id: rewardID,
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
          boss_lifecycle_enabled: true,
          boss_lifecycle_timeout_seconds: clampInt(draft.lifecycleTimeoutSeconds, 60, 86400),
          pal_template_file: draft.palTemplateFile,
          pal_template_snapshot: {
            pal_id: draft.palID.trim(),
            level: clampInt(draft.palLevel, 1, 100),
            nickname: draft.palNickname.trim(),
            pal_name: selectedTemplate?.pal_name || '',
            modified_at: selectedTemplate?.live_modified_at || selectedTemplate?.modified_at || '',
            size: selectedTemplate?.live_size || selectedTemplate?.size || 0,
          },
        },
      };
      return editingID ? bossApi.updateTemplate(editingID, input) : bossApi.createTemplate(input);
    },
    onSuccess: async (item) => {
      setNotice({ type: 'success', text: `Boss 定义“${item.name}”已${editingID ? '更新' : '创建'}，目录模板和奖励配置已保存。` });
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
      if (!palTemplateFile || !catalogByName.has(normalizeTemplateKey(palTemplateFile))) {
        throw new Error('该 Boss 使用的 PalTemplate 当前不在 PalDefender 目录中，请编辑后重新选择。');
      }
      const result = await bossApi.createSummon({
        template_id: item.id,
        request_key: newRequestKey(),
        notes: (notes[item.id] || '').trim(),
        metadata: {
          activity_kind: 'fixed_boss',
          fixed_boss_definition_id: item.id,
          boss_lifecycle_enabled: true,
          boss_lifecycle_timeout_seconds: Number(asRecord(item.metadata).boss_lifecycle_timeout_seconds || 3600),
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
          ? `Boss“${result.summon.template_name}”已召唤，成功执行 ${attempt.completed_commands}/${attempt.command_count} 条命令。`
          : `${attemptLabel[attempt.status]}：${attempt.failure || '请查看执行记录。'}`,
      });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: `Boss 召唤失败：${getErrorMessage(error)}` }),
  });

  const resetEditorCatalogState = () => {
    setEditorTab('boss-template');
    setBossTemplateSearch('');
    setBossTemplateFilters(createEmptyPalTemplateFilters());
    setRewardTemplateSearch('');
    setRewardTemplateFilters(createEmptyPalTemplateFilters());
    setItemSearch('');
    setItemCategory('all');
  };
  const openCreate = () => {
    setEditingID('');
    setDraft(emptyDraft());
    resetEditorCatalogState();
    setEditorOpen(true);
  };
  const openEdit = (item: BossTemplate) => {
    const reward = item.reward_id ? rewardByID.get(item.reward_id) : undefined;
    setEditingID(item.id);
    setDraft({
      name: item.name,
      description: item.description || '',
      palTemplateFile: metadataString(item.metadata, 'pal_template_file'),
      palID: item.pal_id,
      palLevel: item.level,
      palNickname: String(asRecord(asRecord(item.metadata).pal_template_snapshot).nickname || ''),
      count: item.count,
      spawnRadius: item.spawn_radius,
      capturable: item.capturable,
      cooldownSeconds: item.cooldown_seconds,
      lifecycleTimeoutSeconds: Number(asRecord(item.metadata).boss_lifecycle_timeout_seconds || 3600),
      location: { x: item.location.x, y: item.location.y, z: item.location.z, label: item.location.label || '' },
      enabled: item.enabled,
      rewardEnabled: Boolean(reward),
      rewardID: reward?.id || '',
      rewardPoints: reward?.points || 0,
      rewardItems: reward?.items || [],
      rewardPalTemplates: reward?.pal_templates || [],
    });
    resetEditorCatalogState();
    setEditorOpen(true);
  };

  const activeSummons = summons.filter((item) => item.status === 'pending' || item.status === 'active').length;
  const execution = executionQuery.data;
  const loading = definitionsQuery.isLoading || liveTemplateQuery.isLoading || itemCatalogQuery.isLoading;
  const catalogError = liveTemplateQuery.error ? getErrorMessage(liveTemplateQuery.error) : '';
  const enrichmentWarning = enrichmentQuery.error ? '分类索引暂不可用；仍可选择全部 PalDefender 实际模板。' : '';

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <header className="flex flex-col gap-4 rounded-3xl border border-slate-200 bg-white p-5 shadow-sm lg:flex-row lg:items-center lg:justify-between">
        <div>
          <div className="flex items-center gap-2 text-xs font-black uppercase tracking-[0.18em] text-rose-500"><Crown size={16} />Fixed-coordinate Boss</div>
          <h1 className="mt-2 text-2xl font-black text-slate-900">Boss 管理</h1>
          <p className="mt-1 text-sm font-semibold text-slate-500">模板和奖励均从 PalDefender 当前目录直接选择；分类索引只用于筛选，不再限制可选模板。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button type="button" onClick={() => void refresh()} className="inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-4 py-2.5 text-xs font-black text-slate-600 hover:bg-slate-50"><RefreshCw size={15} />刷新目录</button>
          <button type="button" onClick={openCreate} disabled={Boolean(catalogError)} className="inline-flex items-center gap-2 rounded-xl bg-rose-600 px-4 py-2.5 text-xs font-black text-white hover:bg-rose-700 disabled:opacity-45"><Plus size={15} />新建 Boss</button>
        </div>
      </header>

      {notice && <div className={`rounded-2xl border px-4 py-3 text-xs font-bold ${notice.type === 'success' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-rose-200 bg-rose-50 text-rose-700'}`}>{notice.type === 'success' ? <CheckCircle2 className="mr-2 inline" size={15} /> : <CircleAlert className="mr-2 inline" size={15} />}{notice.text}</div>}
      {catalogError && <div className="rounded-2xl border border-rose-200 bg-rose-50 px-4 py-3 text-xs font-bold text-rose-700">PalDefender 模板目录读取失败：{catalogError}</div>}
      {enrichmentWarning && <div className="rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs font-bold text-amber-700">{enrichmentWarning}</div>}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        <Metric label="Boss 定义" value={number.format(definitions.length)} detail="独立于袭击模板" icon={<Crown size={18} />} />
        <Metric label="PalTemplate" value={number.format(catalog.length)} detail={`已分类 ${catalog.filter((item) => item.indexed).length}`} icon={<Gift size={18} />} />
        <Metric label="物品目录" value={number.format(itemCatalog.length)} detail="PalDefender 可发放物品" icon={<Package size={18} />} />
        <Metric label="等待/进行中" value={number.format(activeSummons)} detail="复用全局执行互斥" icon={<LoaderCircle size={18} />} />
        <Metric label="执行器" value={execution?.available ? '可用' : '不可用'} detail={execution?.message || '正在读取状态'} icon={<ShieldAlert size={18} />} />
      </div>

      <section className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div><h2 className="text-lg font-black text-slate-900">Boss 定义</h2><p className="mt-1 text-xs font-semibold text-slate-500">每个定义保存实际 PalTemplate 文件、固定坐标和奖励方案。</p></div>
          <label className="relative min-w-0 sm:w-80"><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} /><input value={definitionSearch} onChange={(event) => setDefinitionSearch(event.target.value)} placeholder="搜索名称、模板或坐标标签" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold outline-none focus:border-rose-400" /></label>
        </div>

        {loading ? (
          <div className="py-16 text-center text-xs font-bold text-slate-400"><LoaderCircle className="mr-2 inline animate-spin" size={16} />正在读取 Boss 与 PalDefender 目录...</div>
        ) : visibleDefinitions.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-slate-300 py-16 text-center text-sm font-bold text-slate-400">暂无 Boss 定义</div>
        ) : (
          <div className="grid gap-4 xl:grid-cols-2">
            {visibleDefinitions.map((item) => {
              const file = metadataString(item.metadata, 'pal_template_file');
              const info = catalogByName.get(normalizeTemplateKey(file));
              const reward = item.reward_id ? rewardByID.get(item.reward_id) : undefined;
              const unavailable = !info;
              return (
                <article key={item.id} className="rounded-2xl border border-slate-200 p-4">
                  <div className="flex items-start gap-3">
                    <PalIcon characterID={info?.pal_id || item.pal_id} name={info ? templateDisplayName(info) : item.name} className="size-14 rounded-2xl border border-slate-200" />
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2"><h3 className="truncate text-base font-black text-slate-900">{item.name}</h3>{item.enabled && !item.archived_at ? <span className="rounded-full bg-emerald-50 px-2 py-1 text-[10px] font-black text-emerald-700">已启用</span> : <span className="rounded-full bg-slate-100 px-2 py-1 text-[10px] font-black text-slate-500">已停用</span>}{unavailable && <span className="rounded-full bg-rose-50 px-2 py-1 text-[10px] font-black text-rose-700">模板缺失</span>}</div>
                      <p className="mt-1 truncate text-xs font-semibold text-slate-500">{info ? templateDisplayName(info) : file || item.pal_id}</p>
                      <p className="mt-1 text-[11px] font-mono text-slate-400">{file || '未保存 PalTemplate 文件'}</p>
                    </div>
                  </div>
                  <div className="mt-4 grid grid-cols-2 gap-2 text-xs font-semibold text-slate-600 sm:grid-cols-4"><span className="rounded-xl bg-slate-50 px-3 py-2">数量 {item.count}</span><span className="rounded-xl bg-slate-50 px-3 py-2">半径 {number.format(item.spawn_radius)}</span><span className="rounded-xl bg-slate-50 px-3 py-2">{item.capturable ? '允许捕捉' : '禁止捕捉'}</span><span className="rounded-xl bg-slate-50 px-3 py-2">检测 {Number(asRecord(item.metadata).boss_lifecycle_timeout_seconds || 3600)}s</span></div>
                  <div className="mt-3 rounded-xl border border-violet-100 bg-violet-50 px-3 py-2 text-xs font-semibold text-violet-700">奖励：{reward ? `${reward.points} 积分 · ${reward.items?.length || 0} 种物品 · ${reward.pal_templates?.length || 0} 个模板` : '未配置'}</div>
                  <div className="mt-3 flex items-start gap-2 rounded-xl border border-slate-100 bg-slate-50 px-3 py-2 text-xs font-semibold text-slate-600"><MapPin className="mt-0.5 shrink-0 text-rose-500" size={14} /><span>{item.location.label || '固定坐标'}：{item.location.x}, {item.location.y}, {item.location.z}</span></div>
                  <textarea value={notes[item.id] || ''} onChange={(event) => setNotes((current) => ({ ...current, [item.id]: event.target.value }))} placeholder="本次召唤备注（可选）" rows={2} className="mt-3 w-full resize-none rounded-xl border border-slate-200 px-3 py-2 text-xs font-semibold outline-none focus:border-rose-400" />
                  <div className="mt-3 flex flex-wrap justify-end gap-2"><button type="button" onClick={() => openEdit(item)} className="inline-flex items-center gap-1.5 rounded-xl border border-slate-200 px-3 py-2 text-xs font-black text-slate-600 hover:bg-slate-50"><Pencil size={14} />编辑</button>{!item.archived_at && <button type="button" onClick={() => archiveMutation.mutate(item)} disabled={archiveMutation.isPending} className="inline-flex items-center gap-1.5 rounded-xl border border-slate-200 px-3 py-2 text-xs font-black text-slate-600 hover:bg-slate-50 disabled:opacity-50"><Archive size={14} />归档</button>}<button type="button" onClick={() => summonMutation.mutate(item)} disabled={!item.enabled || Boolean(item.archived_at) || unavailable || summonMutation.isPending || !execution?.available} className="inline-flex items-center gap-1.5 rounded-xl bg-rose-600 px-4 py-2 text-xs font-black text-white hover:bg-rose-700 disabled:cursor-not-allowed disabled:opacity-45">{summonMutation.isPending ? <LoaderCircle className="animate-spin" size={14} /> : <Play size={14} />}立即召唤</button></div>
                </article>
              );
            })}
          </div>
        )}
      </section>

      <section className="rounded-3xl border border-slate-200 bg-white p-5 shadow-sm">
        <h2 className="text-lg font-black text-slate-900">最近召唤记录</h2>
        <p className="mt-1 text-xs font-semibold text-slate-500">只显示固定坐标 Boss，不混入据点袭击记录。</p>
        <div className="mt-4 overflow-x-auto"><table className="min-w-full text-left text-xs"><thead><tr className="border-b border-slate-200 text-[11px] font-black uppercase tracking-wider text-slate-400"><th className="px-3 py-3">Boss</th><th className="px-3 py-3">坐标</th><th className="px-3 py-3">状态</th><th className="px-3 py-3">发起时间</th><th className="px-3 py-3">结果</th></tr></thead><tbody>{summons.slice(0, 50).map((item) => <tr key={item.id} className="border-b border-slate-100 last:border-0"><td className="px-3 py-3"><strong className="block text-slate-800">{item.template_name}</strong><span className="text-[10px] font-mono text-slate-400">{item.id}</span></td><td className="px-3 py-3 font-mono text-slate-600">{item.location.x}, {item.location.y}, {item.location.z}</td><td className="px-3 py-3"><span className={`rounded-full border px-2 py-1 text-[10px] font-black ${statusClass(item.status)}`}>{summonDisplayStatus(item)}</span></td><td className="px-3 py-3 text-slate-500">{formatTime(item.requested_at)}</td><td className="max-w-xs px-3 py-3 text-slate-500">{summonResultText(item)}</td></tr>)}{summons.length === 0 && <tr><td colSpan={5} className="py-12 text-center text-xs font-bold text-slate-400">暂无 Boss 召唤记录</td></tr>}</tbody></table></div>
      </section>

      {editorOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/45 p-2 sm:p-5">
          <section className="flex max-h-[96vh] w-full max-w-7xl flex-col overflow-hidden rounded-3xl bg-white shadow-2xl">
            <header className="flex items-center justify-between border-b border-slate-200 px-5 py-4"><div><h2 className="text-lg font-black text-slate-900">{editingID ? '编辑 Boss' : '新建 Boss'}</h2><p className="mt-1 text-xs font-semibold text-slate-500">Boss、奖励物品和奖励帕鲁都必须从 PalDefender 当前目录点选。</p></div><button type="button" onClick={() => setEditorOpen(false)} className="rounded-xl border border-slate-200 p-2 text-slate-500 hover:bg-slate-50"><X size={17} /></button></header>
            <div className="grid min-h-0 flex-1 overflow-hidden lg:grid-cols-[420px_minmax(0,1fr)]">
              <div className="overflow-y-auto border-b border-slate-200 p-5 lg:border-b-0 lg:border-r">
                <div className="grid gap-4">
                  <label><FieldLabel>Boss 名称</FieldLabel><input value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} maxLength={128} className="w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm font-semibold outline-none focus:border-rose-400" /></label>
                  <label><FieldLabel>说明</FieldLabel><textarea value={draft.description} onChange={(event) => setDraft((current) => ({ ...current, description: event.target.value }))} rows={3} maxLength={4096} className="w-full resize-none rounded-xl border border-slate-200 px-3 py-2.5 text-sm font-semibold outline-none focus:border-rose-400" /></label>
                  <div className="rounded-2xl border border-slate-200 bg-slate-50 p-3"><FieldLabel>已选 Boss PalTemplate</FieldLabel>{draft.palTemplateFile ? <div className="flex items-center gap-3"><PalIcon characterID={draft.palID} name={selectedTemplate ? templateDisplayName(selectedTemplate) : draft.palNickname || draft.palTemplateFile} className="size-12 rounded-xl border border-slate-200" /><div className="min-w-0"><strong className="block truncate text-sm text-slate-900">{selectedTemplate ? templateDisplayName(selectedTemplate) : draft.palNickname || draft.palID}</strong><span className="block truncate text-[10px] font-mono text-slate-500">{draft.palTemplateFile}</span><span className="text-[10px] font-bold text-slate-400">{draft.palID || '待解析'} · Lv.{draft.palLevel}</span></div></div> : <p className="text-xs font-bold text-rose-600">尚未选择模板</p>}</div>
                  <div className="grid grid-cols-2 gap-3"><label><FieldLabel>召唤数量</FieldLabel><input type="number" min={1} max={50} value={draft.count} onChange={(event) => setDraft((current) => ({ ...current, count: Number(event.target.value) }))} className="w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm font-semibold outline-none focus:border-rose-400" /></label><label><FieldLabel>生成半径</FieldLabel><input type="number" min={0} max={10000} value={draft.spawnRadius} onChange={(event) => setDraft((current) => ({ ...current, spawnRadius: Number(event.target.value) }))} className="w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm font-semibold outline-none focus:border-rose-400" /></label></div>
                  <div className="grid grid-cols-2 gap-3"><label><FieldLabel>冷却秒数</FieldLabel><input type="number" min={0} max={86400} value={draft.cooldownSeconds} onChange={(event) => setDraft((current) => ({ ...current, cooldownSeconds: Number(event.target.value) }))} className="w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm font-semibold outline-none focus:border-rose-400" /></label><label><FieldLabel>检测超时</FieldLabel><input type="number" min={60} max={86400} value={draft.lifecycleTimeoutSeconds} onChange={(event) => setDraft((current) => ({ ...current, lifecycleTimeoutSeconds: Number(event.target.value) }))} className="w-full rounded-xl border border-slate-200 px-3 py-2.5 text-sm font-semibold outline-none focus:border-rose-400" /></label></div>
                  <div className="rounded-2xl border border-slate-200 p-3"><FieldLabel>固定世界坐标</FieldLabel><div className="grid grid-cols-3 gap-2">{(['x', 'y', 'z'] as const).map((key) => <label key={key}><span className="mb-1 block text-[10px] font-black uppercase text-slate-400">{key}</span><input type="number" value={draft.location[key]} onChange={(event) => setDraft((current) => ({ ...current, location: { ...current.location, [key]: Number(event.target.value) } }))} className="w-full rounded-xl border border-slate-200 px-2 py-2 text-xs font-semibold outline-none focus:border-rose-400" /></label>)}</div><input value={draft.location.label} onChange={(event) => setDraft((current) => ({ ...current, location: { ...current.location, label: event.target.value } }))} placeholder="坐标标签" maxLength={128} className="mt-2 w-full rounded-xl border border-slate-200 px-3 py-2 text-xs font-semibold outline-none focus:border-rose-400" /></div>
                  <label className="flex items-center justify-between rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-black text-slate-600"><span>允许捕捉</span><input type="checkbox" checked={draft.capturable} onChange={(event) => setDraft((current) => ({ ...current, capturable: event.target.checked }))} className="size-4 rounded border-slate-300 text-rose-600 focus:ring-rose-500" /></label>
                  <label className="flex items-center justify-between rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-black text-slate-600"><span>启用定义</span><input type="checkbox" checked={draft.enabled} onChange={(event) => setDraft((current) => ({ ...current, enabled: event.target.checked }))} className="size-4 rounded border-slate-300 text-rose-600 focus:ring-rose-500" /></label>
                  <div className="rounded-2xl border border-violet-200 bg-violet-50/60 p-3"><label className="flex items-center justify-between text-xs font-black text-violet-700"><span>启用 Boss 奖励</span><input type="checkbox" checked={draft.rewardEnabled} onChange={(event) => setDraft((current) => ({ ...current, rewardEnabled: event.target.checked }))} className="size-4 rounded border-violet-300 text-violet-600 focus:ring-violet-500" /></label>{draft.rewardEnabled && <div className="mt-3 grid gap-3"><label><FieldLabel>奖励积分</FieldLabel><input type="number" min={0} max={1000000000} value={draft.rewardPoints} onChange={(event) => setDraft((current) => ({ ...current, rewardPoints: Number(event.target.value) }))} className="w-full rounded-xl border border-violet-200 px-3 py-2 text-sm font-semibold outline-none focus:border-violet-400" /></label><div><FieldLabel>已选物品</FieldLabel><div className="grid gap-2">{draft.rewardItems.map((entry) => { const item = itemByID.get(entry.item_id); return <div key={entry.item_id} className="flex items-center gap-2 rounded-xl border border-violet-100 bg-white p-2"><span className="min-w-0 flex-1 truncate text-xs font-bold text-slate-700">{item?.name || entry.item_id}</span><input type="number" min={1} max={999999} value={entry.count} onChange={(event) => updateRewardItemCount(entry.item_id, Number(event.target.value))} className="w-20 rounded-lg border border-slate-200 px-2 py-1 text-xs" /><button type="button" onClick={() => toggleRewardItem(item || { id: entry.item_id, name: entry.item_id })} className="rounded-lg p-1.5 text-rose-500 hover:bg-rose-50"><Trash2 size={14} /></button></div>; })}{draft.rewardItems.length === 0 && <span className="text-[11px] font-semibold text-slate-400">未选择物品</span>}</div></div><div><FieldLabel>奖励 PalTemplate</FieldLabel><div className="flex flex-wrap gap-1.5">{draft.rewardPalTemplates.map((name) => <button key={name} type="button" onClick={() => toggleRewardTemplate(catalogByName.get(normalizeTemplateKey(name)) || { name, index_names: [], classification_tags: [], graduation_uses: [], passive_names: [], indexed: false })} className="rounded-full border border-violet-200 bg-white px-2 py-1 text-[10px] font-bold text-violet-700">{name} ×</button>)}{draft.rewardPalTemplates.length === 0 && <span className="text-[11px] font-semibold text-slate-400">未选择模板</span>}</div></div></div>}</div>
                </div>
              </div>

              <div className="flex min-h-0 flex-col overflow-hidden p-5">
                <div className="mb-3 flex flex-wrap gap-2"><button type="button" onClick={() => setEditorTab('boss-template')} className={`rounded-xl px-3 py-2 text-xs font-black ${editorTab === 'boss-template' ? 'bg-rose-600 text-white' : 'border border-slate-200 text-slate-600'}`}>Boss PalTemplate</button><button type="button" onClick={() => { setEditorTab('reward-items'); setDraft((current) => ({ ...current, rewardEnabled: true })); }} className={`rounded-xl px-3 py-2 text-xs font-black ${editorTab === 'reward-items' ? 'bg-violet-600 text-white' : 'border border-slate-200 text-slate-600'}`}>奖励物品</button><button type="button" onClick={() => { setEditorTab('reward-templates'); setDraft((current) => ({ ...current, rewardEnabled: true })); }} className={`rounded-xl px-3 py-2 text-xs font-black ${editorTab === 'reward-templates' ? 'bg-violet-600 text-white' : 'border border-slate-200 text-slate-600'}`}>奖励 PalTemplate</button></div>

                {editorTab === 'boss-template' && <><label className="relative mb-3"><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} /><input value={bossTemplateSearch} onChange={(event) => setBossTemplateSearch(event.target.value)} placeholder="搜索名称、PalID、分类、词条或模板文件" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold outline-none focus:border-rose-400" /></label><PalTemplateFilters templates={catalog} indexes={enrichmentQuery.data?.template_indexes || []} value={bossTemplateFilters} onChange={setBossTemplateFilters} /><div className="mt-3 min-h-0 flex-1 overflow-y-auto rounded-2xl border border-slate-200 p-2"><div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">{visibleBossTemplates.map((item) => { const selected = normalizeTemplateKey(draft.palTemplateFile) === normalizeTemplateKey(item.name); return <button key={item.name} type="button" onClick={() => void chooseBossTemplate(item)} disabled={resolvingTemplate === item.name} className={`flex items-center gap-2 rounded-xl border p-2 text-left transition ${selected ? 'border-rose-400 bg-rose-50 ring-1 ring-rose-300' : 'border-slate-200 hover:bg-slate-50'} disabled:opacity-60`}><PalIcon characterID={item.pal_id} name={templateDisplayName(item)} className="size-10 rounded-lg" /><span className="min-w-0"><strong className="block truncate text-xs text-slate-800">{templateDisplayName(item)}</strong><span className="block truncate text-[9px] font-mono text-slate-400">{item.name}</span><span className="block truncate text-[9px] font-bold text-slate-500">{item.category || '未分类'} · {item.overall_grade || '未分级'} · {item.pal_id ? `Lv.${item.level || 1}` : '点击读取'}</span></span>{resolvingTemplate === item.name && <LoaderCircle className="ml-auto animate-spin text-rose-500" size={14} />}</button>; })}</div>{visibleBossTemplates.length === 0 && <div className="py-12 text-center text-xs font-bold text-slate-400">没有符合条件的 PalTemplate</div>}</div></>}

                {editorTab === 'reward-items' && <><label className="relative mb-3"><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} /><input value={itemSearch} onChange={(event) => setItemSearch(event.target.value)} placeholder="搜索物品中文名或内部 ID" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold outline-none focus:border-violet-400" /></label><div className="mb-3 flex flex-wrap gap-1.5">{itemCategories.map((category) => <button key={category.id} type="button" onClick={() => setItemCategory(category.id)} className={`rounded-full border px-2.5 py-1.5 text-[10px] font-black ${itemCategory === category.id ? 'border-violet-500 bg-violet-600 text-white' : 'border-slate-200 bg-white text-slate-600'}`}>{category.label}</button>)}</div><div className="min-h-0 flex-1 overflow-y-auto rounded-2xl border border-slate-200 p-2"><div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">{visibleItems.map((item) => { const selected = selectedRewardItemIDs.has(item.id); return <button key={item.id} type="button" onClick={() => toggleRewardItem(item)} className={`flex items-center gap-2 rounded-xl border p-2 text-left transition ${selected ? 'border-violet-400 bg-violet-50 ring-1 ring-violet-300' : 'border-slate-200 hover:bg-slate-50'}`}><ItemIcon item={item} /><span className="min-w-0"><strong className="block truncate text-xs text-slate-800">{item.name}</strong><span className="block truncate text-[9px] font-mono text-slate-400">{item.id}</span><span className="block text-[9px] font-bold text-slate-500">{itemCategories.find((category) => category.id === classifyItem(item))?.label || '其他'}</span></span></button>; })}</div>{visibleItems.length === 0 && <div className="py-12 text-center text-xs font-bold text-slate-400">没有符合条件的物品</div>}</div></>}

                {editorTab === 'reward-templates' && <><label className="relative mb-3"><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} /><input value={rewardTemplateSearch} onChange={(event) => setRewardTemplateSearch(event.target.value)} placeholder="搜索奖励模板、PalID、分类或词条" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold outline-none focus:border-violet-400" /></label><PalTemplateFilters templates={catalog} indexes={enrichmentQuery.data?.template_indexes || []} value={rewardTemplateFilters} onChange={setRewardTemplateFilters} /><div className="mt-3 min-h-0 flex-1 overflow-y-auto rounded-2xl border border-slate-200 p-2"><div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">{visibleRewardTemplates.map((item) => { const selected = selectedRewardTemplateNames.has(item.name); return <button key={item.name} type="button" onClick={() => toggleRewardTemplate(item)} className={`flex items-center gap-2 rounded-xl border p-2 text-left transition ${selected ? 'border-violet-400 bg-violet-50 ring-1 ring-violet-300' : 'border-slate-200 hover:bg-slate-50'}`}><PalIcon characterID={item.pal_id} name={templateDisplayName(item)} className="size-10 rounded-lg" /><span className="min-w-0"><strong className="block truncate text-xs text-slate-800">{templateDisplayName(item)}</strong><span className="block truncate text-[9px] font-mono text-slate-400">{item.name}</span><span className="block truncate text-[9px] font-bold text-slate-500">{item.category || '未分类'} · {item.overall_grade || '未分级'}</span></span></button>; })}</div>{visibleRewardTemplates.length === 0 && <div className="py-12 text-center text-xs font-bold text-slate-400">没有符合条件的 PalTemplate</div>}</div></>}
              </div>
            </div>
            <footer className="flex items-center justify-between gap-3 border-t border-slate-200 px-5 py-4"><span className="text-[11px] font-semibold text-slate-400">目录来源：PalDefender GM 模板与物品接口；礼包索引仅提供分类元数据。</span><div className="flex gap-2"><button type="button" onClick={() => setEditorOpen(false)} className="rounded-xl border border-slate-200 px-4 py-2.5 text-xs font-black text-slate-600">取消</button><button type="button" onClick={() => saveMutation.mutate()} disabled={saveMutation.isPending || !draft.palTemplateFile || !draft.palID || !draft.name.trim()} className="inline-flex items-center gap-2 rounded-xl bg-rose-600 px-5 py-2.5 text-xs font-black text-white hover:bg-rose-700 disabled:opacity-45">{saveMutation.isPending && <LoaderCircle className="animate-spin" size={14} />}{editingID ? '保存修改' : '创建 Boss'}</button></div></footer>
          </section>
        </div>
      )}
    </div>
  );
};
