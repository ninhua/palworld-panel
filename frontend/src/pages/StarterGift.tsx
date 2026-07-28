import React, { useEffect, useMemo, useState } from 'react';
import {
  AlertTriangle, CheckCircle2, CheckSquare, ChevronDown, CircleDot, ClipboardList, Flag, Gift, LoaderCircle, Minus, Package,
  PackageCheck, PlayCircle, Plus, RefreshCw, RotateCcw, Save, Search, Settings2, ShieldCheck, Sparkles,
  Square, Trash2, UserCheck, Users, Wifi, WifiOff,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import {
  starterGiftApi,
  type PalTemplateInfo,
  type StarterGiftCatalogItem,
  type StarterGiftConfig,
  type StarterGiftPlayerAction,
  type StarterGiftSnapshot,
} from '../api/starterGift';
import { PalIcon } from '../components/gm/PalIcon';

const emptyConfig: StarterGiftConfig = {
  enabled: false,
  items: [],
  pal_templates: [],
  item_batch_size: 20,
  template_batch_size: 5,
  batch_delay_ms: 500,
};

type GiftTab = 'players' | 'items' | 'templates' | 'grants';
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

const categoryLabel = new Map(itemCategories.map((category) => [category.id, category.label]));

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

const statusLabel = (status: string) => ({
  pending: '等待发放', running: '正在分批发放', success: '发放完成', failed: '发放失败',
}[status] || status);

const statusClass = (status: string) => ({
  pending: 'border-amber-200 bg-amber-50 text-amber-700',
  running: 'border-sky-200 bg-sky-50 text-sky-700',
  success: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  failed: 'border-rose-200 bg-rose-50 text-rose-700',
}[status] || 'border-slate-200 bg-slate-50 text-slate-600');

const templateDisplayName = (template: PalTemplateInfo) =>
  template.pal_name?.trim() || template.name;

const templateCategoryName = (template: PalTemplateInfo) => template.category?.trim() || '未分类';

const templateSearchText = (template: PalTemplateInfo) =>
  `${template.pal_name || ''} ${template.english_name || ''} ${template.pal_id || ''} ${template.nickname || ''} ${template.category || ''} ${template.usage_category || ''} ${template.overall_grade || ''} ${template.classification_tags.join(' ')} ${template.passive_names.join(' ')} ${template.index_names.join(' ')} ${template.name}`.toLowerCase();

const decisionLabel = (decision: string) => ({
  starter_gift_created: '已创建礼包任务', marked_new_next_login: '已标记：下次进入发放', unseen_candidate: '未见过：候选新玩家', known_existing: '已知老玩家',
}[decision] || decision || '未知');

const decisionClass = (decision: string) => ({
  starter_gift_created: 'border-sky-200 bg-sky-50 text-sky-700', marked_new_next_login: 'border-violet-200 bg-violet-50 text-violet-700',
  unseen_candidate: 'border-emerald-200 bg-emerald-50 text-emerald-700', known_existing: 'border-slate-200 bg-slate-50 text-slate-600',
}[decision] || 'border-amber-200 bg-amber-50 text-amber-700');

const phaseLabel = (phase?: string) => ({
  queued: '已排队', resolving_player: '解析玩家', ready: '准备发放', items: '发放物品', templates: '发放模板', waiting_player: '等待玩家可用',
  failed: '失败', completed: '完成', paused: '暂停',
}[phase || ''] || phase || '等待处理');

const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—';

const ItemIcon: React.FC<{ item: StarterGiftCatalogItem; className?: string }> = ({ item, className = '' }) => {
  const source = item.icon ? `/assets/items/${encodeURIComponent(item.icon)}.webp` : '';
  const [failed, setFailed] = useState(!source);
  useEffect(() => setFailed(!source), [source]);
  return (
    <span className={`relative flex shrink-0 items-center justify-center overflow-hidden rounded-xl border border-slate-200 bg-slate-50 text-slate-400 ${className}`}>
      <Package size={20} aria-hidden="true" />
      {!failed && <img src={source} alt={`${item.name}图标`} loading="lazy" className="absolute inset-0 h-full w-full object-contain p-1" onError={() => setFailed(true)} />}
    </span>
  );
};

const SummaryCard: React.FC<{ label: string; value: React.ReactNode; detail: string; icon: React.ReactNode }> = ({ label, value, detail, icon }) => (
  <div className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm">
    <div className="flex items-start justify-between gap-3"><span className="text-[11px] font-black uppercase tracking-[0.16em] text-slate-400">{label}</span><span className="text-violet-500">{icon}</span></div>
    <strong className="mt-3 block truncate text-xl font-black text-slate-900">{value}</strong>
    <span className="mt-1 block text-[11px] font-semibold text-slate-500">{detail}</span>
  </div>
);

export const StarterGift: React.FC = () => {
  const [snapshot, setSnapshot] = useState<StarterGiftSnapshot | null>(null);
  const [config, setConfig] = useState<StarterGiftConfig>(emptyConfig);
  const [activeTab, setActiveTab] = useState<GiftTab>('items');
  const [itemSearch, setItemSearch] = useState('');
  const [itemCategory, setItemCategory] = useState<ItemCategory>('all');
  const [onlySelectedItems, setOnlySelectedItems] = useState(false);
  const [templateSearch, setTemplateSearch] = useState('');
  const [templateIndex, setTemplateIndex] = useState('all');
  const [templateCategory, setTemplateCategory] = useState('all');
  const [onlySelectedTemplates, setOnlySelectedTemplates] = useState(false);
  const [playerSearch, setPlayerSearch] = useState('');
  const [playerDecision, setPlayerDecision] = useState('all');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [noticeKind, setNoticeKind] = useState<'success' | 'error'>('success');

  const load = async (quiet = false, preserveConfig = false) => {
    if (!quiet) { setBusy(true); setNotice(''); }
    try {
      const next = await starterGiftApi.get();
      setSnapshot(next);
      if (!preserveConfig) setConfig(next.config);
    } catch (error) {
      if (!quiet) { setNoticeKind('error'); setNotice(getErrorMessage(error)); }
    } finally {
      if (!quiet) setBusy(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const selectedItemIDs = useMemo(() => new Set(config.items.map((item) => item.item_id)), [config.items]);
  const categoryCounts = useMemo(() => {
    const counts = new Map<ItemCategory, number>([['all', snapshot?.item_catalog.length || 0]]);
    for (const item of snapshot?.item_catalog || []) {
      const category = classifyItem(item);
      counts.set(category, (counts.get(category) || 0) + 1);
    }
    return counts;
  }, [snapshot]);
  const visibleItems = useMemo(() => {
    const query = itemSearch.trim().toLowerCase();
    return (snapshot?.item_catalog || [])
      .filter((item) => {
        if (itemCategory !== 'all' && classifyItem(item) !== itemCategory) return false;
        if (onlySelectedItems && !selectedItemIDs.has(item.id)) return false;
        return !query || `${item.id} ${item.name}`.toLowerCase().includes(query);
      })
      .sort((left, right) => left.name.localeCompare(right.name, 'zh-CN'));
  }, [snapshot, itemCategory, itemSearch, onlySelectedItems, selectedItemIDs]);

  const templates = useMemo(() => (snapshot?.templates || []).filter((item) => item.name), [snapshot]);
  const templateIndexes = useMemo(() => snapshot?.template_indexes || [], [snapshot]);
  const templateIndexLabels = useMemo(() => new Map(templateIndexes.map((index) => [index.name, index.label || index.name])), [templateIndexes]);
  const templateCategories = useMemo(() => Array.from(new Set<string>(templates.map(templateCategoryName))).sort((left, right) => left.localeCompare(right, 'zh-CN')), [templates]);
  const selectedTemplates = useMemo(() => new Set(config.pal_templates), [config.pal_templates]);
  const visibleTemplates = useMemo(() => {
    const query = templateSearch.trim().toLowerCase();
    return templates
      .filter((template) => {
        if (templateIndex !== 'all' && !template.index_names.includes(templateIndex)) return false;
        if (templateCategory !== 'all' && templateCategoryName(template) !== templateCategory) return false;
        if (onlySelectedTemplates && !selectedTemplates.has(template.name)) return false;
        return !query || templateSearchText(template).includes(query);
      })
      .sort((left, right) => {
        const byCategory = templateCategoryName(left).localeCompare(templateCategoryName(right), 'zh-CN');
        if (byCategory) return byCategory;
        const byPal = templateDisplayName(left).localeCompare(templateDisplayName(right), 'zh-CN');
        return byPal || left.name.localeCompare(right.name, 'zh-CN');
      });
  }, [templates, templateIndex, templateCategory, templateSearch, onlySelectedTemplates, selectedTemplates]);

  const grantCounts = useMemo(() => {
    const grants = snapshot?.grants || [];
    return {
      total: grants.length,
      pending: grants.filter((grant) => grant.status === 'pending' || grant.status === 'running').length,
      failed: grants.filter((grant) => grant.status === 'failed').length,
      success: grants.filter((grant) => grant.status === 'success').length,
    };
  }, [snapshot]);
  const selectedItemTotal = useMemo(() => config.items.reduce((sum, item) => sum + Math.max(0, Number(item.count) || 0), 0), [config.items]);
  const dirty = Boolean(snapshot && JSON.stringify(config) !== JSON.stringify(snapshot.config));
  const visiblePlayers = useMemo(() => {
    const query = playerSearch.trim().toLowerCase();
    return (snapshot?.players || []).filter((player) => {
      if (playerDecision !== 'all' && player.decision !== playerDecision) return false;
      return !query || `${player.nickname || ''} ${player.player_id} ${player.player_uid || ''} ${player.steam_id || ''} ${player.reason}`.toLowerCase().includes(query);
    }).sort((left, right) => Number(right.online) - Number(left.online) || (left.nickname || left.player_id).localeCompare(right.nickname || right.player_id, 'zh-CN'));
  }, [snapshot, playerDecision, playerSearch]);
  const activeGrant = Boolean(snapshot?.worker_running || snapshot?.grants.some((grant) => grant.status === 'pending' || grant.status === 'running'));

  useEffect(() => {
    if (!activeGrant) return;
    const timer = window.setInterval(() => void load(true, true), 3000);
    return () => window.clearInterval(timer);
  }, [activeGrant]);

  const toggleItem = (catalogItem: StarterGiftCatalogItem) => {
    setConfig((current) => current.items.some((item) => item.item_id === catalogItem.id)
      ? { ...current, items: current.items.filter((item) => item.item_id !== catalogItem.id) }
      : { ...current, items: [...current.items, { item_id: catalogItem.id, count: 1 }] });
  };

  const updateItemCount = (itemID: string, count: number) => {
    const nextCount = Math.max(1, Math.min(2147483647, Number(count) || 1));
    setConfig((current) => ({
      ...current,
      items: current.items.map((item) => item.item_id === itemID ? { ...item, count: nextCount } : item),
    }));
  };

  const setVisibleItemsSelected = (selected: boolean) => {
    const visible = new Set(visibleItems.map((item) => item.id));
    setConfig((current) => {
      if (!selected) return { ...current, items: current.items.filter((item) => !visible.has(item.item_id)) };
      const existing = new Set(current.items.map((item) => item.item_id));
      return {
        ...current,
        items: [...current.items, ...visibleItems.filter((item) => !existing.has(item.id)).map((item) => ({ item_id: item.id, count: 1 }))],
      };
    });
  };

  const setVisibleTemplatesSelected = (selected: boolean) => {
    const visibleNames = visibleTemplates.map((template) => template.name);
    const visible = new Set(visibleNames);
    setConfig((current) => selected
      ? { ...current, pal_templates: Array.from(new Set([...current.pal_templates, ...visibleNames])) }
      : { ...current, pal_templates: current.pal_templates.filter((name) => !visible.has(name)) });
  };

  const save = async () => {
    setBusy(true);
    setNotice('');
    try {
      const next = await starterGiftApi.save(config);
      setSnapshot(next);
      setConfig(next.config);
      setNoticeKind('success');
      setNotice('配置已保存。玩家基线、在线历史和发放进度仅作用于当前存档世界。');
    } catch (error) {
      setNoticeKind('error');
      setNotice(getErrorMessage(error));
    } finally {
      setBusy(false);
    }
  };

  const runPlayerAction = async (playerID: string, action: StarterGiftPlayerAction) => {
    const messages: Record<StarterGiftPlayerAction, string> = {
      retry: '已重新排队，将从未完成批次继续。', supplement: '已排队补发未完成内容，不会重复已成功批次。',
      reissue: '已创建完整重发任务，将从第一批重新发放。', next_login: '已标记为新玩家；当前在线时需先离线，再次进入后自动发放。',
    };
    const confirms: Partial<Record<StarterGiftPlayerAction, string>> = {
      reissue: '完整重发会从第一批重新发放，可能产生重复物品或帕鲁。确认继续？',
      next_login: '将清除该玩家现有礼包任务并标记为“下次进入视为新玩家”。确认继续？',
    };
    if (confirms[action] && !window.confirm(confirms[action])) return;
    setBusy(true); setNotice('');
    try {
      await starterGiftApi.action(playerID, action);
      await load(true);
      setNoticeKind('success'); setNotice(messages[action]);
    } catch (error) {
      setNoticeKind('error'); setNotice(getErrorMessage(error));
    } finally { setBusy(false); }
  };

  const forget = async (playerID: string) => {
    if (!window.confirm('重置当前存档中该玩家的礼包记录？玩家仍在线时，将等待其离线并重新进入。')) return;
    setBusy(true);
    setNotice('');
    try {
      await starterGiftApi.forget(playerID);
      await load();
      setNoticeKind('success');
      setNotice('当前存档中的记录已重置。');
    } catch (error) {
      setNoticeKind('error');
      setNotice(getErrorMessage(error));
      setBusy(false);
    }
  };

  const tabs: Array<{ id: GiftTab; label: string; count: number; icon: React.ReactNode }> = [
    { id: 'players', label: '玩家判定', count: snapshot?.players.length || 0, icon: <UserCheck size={15} /> },
    { id: 'items', label: '礼包物品', count: config.items.length, icon: <PackageCheck size={15} /> },
    { id: 'templates', label: '帕鲁模板', count: config.pal_templates.length, icon: <Sparkles size={15} /> },
    { id: 'grants', label: '发放记录', count: grantCounts.total, icon: <ClipboardList size={15} /> },
  ];

  return (
    <div className="page-shell pb-4">
      <div className="page-titlebar">
        <div className="min-w-0">
          <p className="eyebrow">New player onboarding</p>
          <h1>新玩家礼包</h1>
          <p>为当前存档世界中首次进入的玩家配置开局物品与帕鲁模板。</p>
        </div>
        <button type="button" className="pp-button" onClick={() => void load(false, dirty)} disabled={busy}><RefreshCw className={busy ? 'animate-spin' : ''} size={15} />刷新</button>
      </div>

      {notice && <div role={noticeKind === 'error' ? 'alert' : 'status'} className={`flex items-start gap-2 rounded-xl border px-4 py-3 text-xs font-semibold ${noticeKind === 'error' ? 'border-rose-200 bg-rose-50 text-rose-800' : 'border-emerald-200 bg-emerald-50 text-emerald-800'}`}>{noticeKind === 'error' ? <AlertTriangle className="mt-0.5 shrink-0" size={15} /> : <CheckCircle2 className="mt-0.5 shrink-0" size={15} />}<span>{notice}</span></div>}

      <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <SummaryCard label="当前世界" value={snapshot?.scope.world_id || '尚未解析'} detail="配置和发放记录按 WorldID 隔离" icon={<ShieldCheck size={18} />} />
        <SummaryCard label="自动发放" value={config.enabled ? '已启用' : '已停用'} detail={dirty ? '有未保存修改' : '配置已同步'} icon={<Gift size={18} />} />
        <SummaryCard label="礼包内容" value={`${config.items.length} 种 / ${selectedItemTotal} 件`} detail={`${config.pal_templates.length} 个帕鲁模板`} icon={<PackageCheck size={18} />} />
        <SummaryCard label="发放任务" value={snapshot?.worker_running ? '执行器运行中' : `${grantCounts.pending} 处理中`} detail={`${grantCounts.success} 完成 · ${grantCounts.failed} 失败 · 每 3 秒刷新`} icon={<Users size={18} />} />
      </section>

      <section className="pp-card">
        <div className="pp-card-head"><div><h2>发放策略</h2><p>启用时以当前存档中的已知玩家建立基线；新世界自动使用独立配置和记录。</p></div><Settings2 size={18} /></div>
        <div className="grid gap-3 lg:grid-cols-[minmax(15rem,1.2fr)_repeat(3,minmax(10rem,1fr))]">
          <label className={`flex cursor-pointer items-center justify-between gap-4 rounded-2xl border p-4 ${config.enabled ? 'border-emerald-200 bg-emerald-50' : 'border-slate-200 bg-slate-50'}`}>
            <span><strong className="block text-sm font-black text-slate-800">自动发放</strong><small className="mt-1 block text-[11px] font-semibold text-slate-500">新玩家首次进入时加入分批发放队列</small></span>
            <input className="h-5 w-5 accent-emerald-600" type="checkbox" checked={config.enabled} onChange={(event) => setConfig({ ...config, enabled: event.target.checked })} />
          </label>
          <label className="field-label">物品每批条目数<input type="number" min={1} max={100} value={config.item_batch_size} onChange={(event) => setConfig({ ...config, item_batch_size: Number(event.target.value) })} /></label>
          <label className="field-label">模板每批数量<input type="number" min={1} max={20} value={config.template_batch_size} onChange={(event) => setConfig({ ...config, template_batch_size: Number(event.target.value) })} /></label>
          <label className="field-label">批次间隔（毫秒）<input type="number" min={100} max={10000} step={100} value={config.batch_delay_ms} onChange={(event) => setConfig({ ...config, batch_delay_ms: Number(event.target.value) })} /></label>
        </div>
      </section>

      <section className="pp-card min-w-0">
        <div className="flex gap-1 overflow-x-auto rounded-xl border border-slate-200 bg-slate-100 p-1" role="tablist" aria-label="礼包配置分类">
          {tabs.map((tab) => <button type="button" role="tab" aria-selected={activeTab === tab.id} key={tab.id} onClick={() => setActiveTab(tab.id)} className={`inline-flex shrink-0 items-center gap-2 rounded-lg px-4 py-2.5 text-xs font-black ${activeTab === tab.id ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500 hover:text-slate-800'}`}>{tab.icon}{tab.label}<span className={`rounded-full px-2 py-0.5 text-[10px] ${activeTab === tab.id ? 'bg-violet-100 text-violet-700' : 'bg-slate-200 text-slate-500'}`}>{tab.count}</span></button>)}
        </div>

        {activeTab === 'players' && <div className="mt-5 min-w-0">
          {snapshot?.players_error && <div className="mb-4 flex items-start gap-2 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs font-semibold text-amber-800"><AlertTriangle className="mt-0.5 shrink-0" size={15} />玩家判定读取不完整：{snapshot.players_error}</div>}
          <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_14rem]">
            <label className="relative block"><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} /><input className="pp-input pp-input--icon w-full" value={playerSearch} onChange={(event) => setPlayerSearch(event.target.value)} placeholder="搜索昵称、PlayerUID、SteamID 或判定原因" /></label>
            <select className="pp-input" value={playerDecision} onChange={(event) => setPlayerDecision(event.target.value)} aria-label="筛选玩家判定"><option value="all">全部判定</option><option value="unseen_candidate">候选新玩家</option><option value="marked_new_next_login">已标记下次进入</option><option value="starter_gift_created">已有礼包任务</option><option value="known_existing">已知老玩家</option></select>
          </div>
          <div className="mt-3 rounded-xl border border-sky-200 bg-sky-50 px-4 py-3 text-[11px] font-semibold leading-5 text-sky-800"><strong>判定规则：</strong>当前 WorldID 内从未出现在 seen 基线中的账号，在首次在线采样时视为新玩家；人工“下次进入视为新玩家”会设置 rearm 标记，必须观察到离线后再次上线才触发。已有任务优先显示任务来源与状态。</div>
          <div className="mt-4 max-h-[42rem] overflow-y-auto rounded-2xl border border-slate-200 bg-slate-50/50 p-2">
            {visiblePlayers.map((player) => <article key={player.player_id} className="mb-2 rounded-2xl border border-slate-200 bg-white p-4 last:mb-0">
              <div className="flex flex-col gap-3 xl:flex-row xl:items-start">
                <div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><strong className="truncate text-sm font-black text-slate-800">{player.nickname || player.player_id}</strong><span className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-black ${player.online ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-slate-200 bg-slate-50 text-slate-500'}`}>{player.online ? <Wifi size={11} /> : <WifiOff size={11} />}{player.online ? '在线' : '离线'}</span><span className={`rounded-full border px-2 py-0.5 text-[10px] font-black ${decisionClass(player.decision)}`}>{decisionLabel(player.decision)}</span>{player.rearmed && <span className="rounded-full border border-violet-200 bg-violet-50 px-2 py-0.5 text-[10px] font-black text-violet-700">rearm</span>}</div>
                <p className="mt-2 text-xs font-semibold leading-5 text-slate-700">{player.reason}</p><div className="mt-2 flex flex-wrap gap-1.5">{player.evidence.map((evidence) => <span key={evidence} className="rounded-lg bg-slate-100 px-2 py-1 font-mono text-[9px] text-slate-600">{evidence}</span>)}</div><p className="mt-2 truncate font-mono text-[10px] text-slate-400">PlayerID {player.player_id}{player.player_uid ? ` · UID ${player.player_uid}` : ''}{player.steam_id ? ` · Steam ${player.steam_id}` : ''}</p></div>
                <div className="flex shrink-0 flex-wrap gap-2 xl:max-w-sm xl:justify-end"><button type="button" className="pp-button" disabled={busy} onClick={() => void runPlayerAction(player.player_id, 'next_login')}><Flag size={14} />下次进入视为新玩家</button>{player.grant_status && player.grant_status !== 'success' && <button type="button" className="pp-button" disabled={busy} onClick={() => void runPlayerAction(player.player_id, 'supplement')}><PackageCheck size={14} />补发未完成</button>}<button type="button" className="pp-button" disabled={busy || !config.enabled} title={config.enabled ? '从头创建完整发放任务' : '请先启用并保存自动发放配置'} onClick={() => void runPlayerAction(player.player_id, 'reissue')}><PlayCircle size={14} />立即完整重发</button></div>
              </div>
            </article>)}
            {visiblePlayers.length === 0 && <div className="flex min-h-56 flex-col items-center justify-center gap-2 p-8 text-center text-slate-400"><UserCheck size={30} /><strong className="text-sm text-slate-600">没有匹配玩家</strong><span className="text-xs">存档索引状态：{snapshot?.save_index_state || '未知'}</span></div>}
          </div>
        </div>}

        {activeTab === 'items' && <div className="mt-5 min-w-0">
          <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
            <label className="relative block min-w-0 flex-1"><span className="sr-only">搜索物品</span><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} /><input className="pp-input pp-input--icon w-full" value={itemSearch} onChange={(event) => setItemSearch(event.target.value)} placeholder="搜索中文名称或 ItemID" /></label>
            <div className="flex flex-wrap gap-2">
              <button type="button" className={`pp-button ${onlySelectedItems ? 'accent' : ''}`} onClick={() => setOnlySelectedItems((value) => !value)}>{onlySelectedItems ? <CheckSquare size={14} /> : <Square size={14} />}只看已选</button>
              <button type="button" className="pp-button" disabled={visibleItems.length === 0} onClick={() => setVisibleItemsSelected(true)}><CheckSquare size={14} />全选当前（{visibleItems.length}）</button>
              <button type="button" className="pp-button" disabled={visibleItems.length === 0} onClick={() => setVisibleItemsSelected(false)}><Square size={14} />取消当前</button>
              <button type="button" className="pp-button" disabled={config.items.length === 0} onClick={() => setConfig({ ...config, items: [] })}><Trash2 size={14} />清空</button>
            </div>
          </div>
          <div className="mt-3 flex gap-2 overflow-x-auto pb-1" aria-label="物品分类">
            {itemCategories.map((category) => <button type="button" key={category.id} onClick={() => setItemCategory(category.id)} className={`shrink-0 rounded-full border px-3 py-1.5 text-[11px] font-black ${itemCategory === category.id ? 'border-violet-300 bg-violet-100 text-violet-800' : 'border-slate-200 bg-white text-slate-600 hover:bg-slate-50'}`}>{category.label}<span className="ml-1.5 text-[10px] opacity-60">{categoryCounts.get(category.id) || 0}</span></button>)}
          </div>
          <div className="mt-4 max-h-[42rem] overflow-y-auto rounded-2xl border border-slate-200 bg-slate-50/50 p-2" role="listbox" aria-label="物品目录" aria-multiselectable="true">
            {visibleItems.map((item) => {
              const selected = selectedItemIDs.has(item.id);
              const selectedEntry = config.items.find((entry) => entry.item_id === item.id);
              return <div role="option" aria-selected={selected} className={`mb-2 flex min-w-0 flex-col gap-3 rounded-2xl border p-3 last:mb-0 sm:flex-row sm:items-center ${selected ? 'border-violet-300 bg-violet-50 shadow-sm' : 'border-slate-200 bg-white hover:border-slate-300'}`} key={item.id}>
                <button type="button" className="flex min-w-0 flex-1 items-center gap-3 text-left" onClick={() => toggleItem(item)}>
                  {selected ? <CheckSquare className="shrink-0 text-violet-600" size={18} /> : <Square className="shrink-0 text-slate-400" size={18} />}
                  <ItemIcon item={item} className="h-12 w-12" />
                  <span className="min-w-0"><strong className="block truncate text-sm font-black text-slate-800">{item.name}</strong><span className="mt-1 block truncate font-mono text-[10px] text-slate-400">{item.id}</span><span className="mt-1 inline-flex rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-bold text-slate-500">{categoryLabel.get(classifyItem(item))}</span></span>
                </button>
                {selected && selectedEntry && <div className="flex shrink-0 items-center justify-end gap-2 sm:justify-start">
                  <button type="button" className="flex h-9 w-9 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-600" aria-label={`减少 ${item.name} 数量`} onClick={() => updateItemCount(item.id, selectedEntry.count - 1)}><Minus size={14} /></button>
                  <input aria-label={`${item.name} 数量`} className="w-24 text-center" type="number" min={1} max={2147483647} value={selectedEntry.count} onChange={(event) => updateItemCount(item.id, Number(event.target.value))} />
                  <button type="button" className="flex h-9 w-9 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-600" aria-label={`增加 ${item.name} 数量`} onClick={() => updateItemCount(item.id, selectedEntry.count + 1)}><Plus size={14} /></button>
                  <button type="button" className="icon-danger" aria-label={`删除 ${item.name}`} onClick={() => toggleItem(item)}><Trash2 size={15} /></button>
                </div>}
              </div>;
            })}
            {visibleItems.length === 0 && <div className="flex min-h-56 flex-col items-center justify-center gap-2 p-8 text-center text-slate-400"><PackageCheck size={30} /><strong className="text-sm text-slate-600">没有匹配物品</strong><span className="text-xs">调整分类、关键词或“只看已选”后重试</span></div>}
          </div>
        </div>}

        {activeTab === 'templates' && <div className="mt-5 min-w-0">
          {snapshot?.template_error && <div className="mb-4 flex items-start gap-2 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs font-semibold text-amber-800"><AlertTriangle className="mt-0.5 shrink-0" size={15} />模板读取失败：{snapshot.template_error}</div>}
          <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
            <div className="grid min-w-0 flex-1 gap-2 md:grid-cols-[minmax(0,1fr)_13rem_11rem]">
              <label className="relative block min-w-0"><span className="sr-only">搜索模板</span><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} /><input className="pp-input pp-input--icon w-full" value={templateSearch} onChange={(event) => setTemplateSearch(event.target.value)} placeholder="搜索分类、中文名、英文名、PalID 或文件名" /></label>
              <select className="pp-input" aria-label="按模板索引筛选" value={templateIndex} onChange={(event) => setTemplateIndex(event.target.value)}>
                <option value="all">全部索引</option>
                {templateIndexes.map((index) => <option key={index.name} value={index.name}>{index.label}（{index.count}）</option>)}
              </select>
              <select className="pp-input" aria-label="按模板分类筛选" value={templateCategory} onChange={(event) => setTemplateCategory(event.target.value)}>
                <option value="all">全部分类</option>
                {templateCategories.map((category) => <option key={category} value={category}>{category}</option>)}
              </select>
            </div>
            <div className="flex flex-wrap gap-2">
              <button type="button" className={`pp-button ${onlySelectedTemplates ? 'accent' : ''}`} onClick={() => setOnlySelectedTemplates((value) => !value)}>{onlySelectedTemplates ? <CheckSquare size={14} /> : <Square size={14} />}只看已选</button>
              <button type="button" className="pp-button" disabled={visibleTemplates.length === 0} onClick={() => setVisibleTemplatesSelected(true)}><CheckSquare size={14} />全选当前（{visibleTemplates.length}）</button>
              <button type="button" className="pp-button" disabled={visibleTemplates.length === 0} onClick={() => setVisibleTemplatesSelected(false)}><Square size={14} />取消当前</button>
              <button type="button" className="pp-button" disabled={config.pal_templates.length === 0} onClick={() => setConfig({ ...config, pal_templates: [] })}><Trash2 size={14} />清空</button>
            </div>
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px] font-semibold text-slate-500">
            {templateIndexes.length > 0 ? <><span>已加载 {templateIndexes.length} 个模板索引</span><span className="text-slate-300">·</span><span>名称与分类优先读取索引；PalID 始终读取模板 JSON</span></> : <span>未检测到索引文件，模板名称按文件名显示。</span>}
          </div>
          <div className="mt-4 grid max-h-[42rem] gap-2 overflow-y-auto rounded-2xl border border-slate-200 bg-slate-50/50 p-2 md:grid-cols-2 2xl:grid-cols-3" role="listbox" aria-label="帕鲁模板" aria-multiselectable="true">
            {visibleTemplates.map((template) => {
              const checked = selectedTemplates.has(template.name);
              const displayName = templateDisplayName(template);
              const category = templateCategoryName(template);
              return <button type="button" role="option" aria-selected={checked} key={template.name} className={`flex min-w-0 items-center gap-3 rounded-2xl border p-3 text-left ${checked ? 'border-violet-300 bg-violet-50 shadow-sm' : 'border-slate-200 bg-white hover:border-slate-300'}`} onClick={() => setConfig((current) => ({ ...current, pal_templates: checked ? current.pal_templates.filter((item) => item !== template.name) : [...current.pal_templates, template.name] }))}>
                {checked ? <CheckSquare className="shrink-0 text-violet-600" size={18} /> : <Square className="shrink-0 text-slate-400" size={18} />}
                <PalIcon characterID={template.pal_id || ''} name={displayName} className="h-14 w-14 rounded-xl border border-slate-200" />
                <span className="min-w-0">
                  <strong className="block truncate text-sm font-black text-slate-800">{category} · {displayName}</strong>
                  <span className="mt-1 block truncate font-mono text-[10px] text-slate-400">{template.pal_id || 'PalID 未读取'}{template.english_name ? ` · ${template.english_name}` : ''} · {template.name}</span>
                  <span className="mt-1 flex flex-wrap gap-1.5">
                    {template.index_names.slice(0, 2).map((indexName) => <span key={indexName} className="inline-flex max-w-40 truncate rounded-full bg-sky-100 px-2 py-0.5 text-[10px] font-bold text-sky-700">{templateIndexLabels.get(indexName) || indexName}</span>)}
                    {template.index_names.length === 0 && <span className="inline-flex rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-bold text-slate-500">未命中索引</span>}
                    {template.level != null && template.level > 0 && <span className="inline-flex rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-bold text-slate-600">Lv.{template.level}</span>}
                    {template.nickname && <span className="inline-flex max-w-36 truncate rounded-full bg-violet-100 px-2 py-0.5 text-[10px] font-bold text-violet-700">{template.nickname}</span>}
                    {template.overall_grade && <span className="inline-flex rounded-full bg-amber-100 px-2 py-0.5 text-[10px] font-bold text-amber-800">{template.overall_grade}</span>}
                    {template.usage_category && <span className="inline-flex max-w-44 truncate rounded-full bg-emerald-100 px-2 py-0.5 text-[10px] font-bold text-emerald-700">{template.usage_category}</span>}
                    {template.classification_tags.slice(0, 2).map((tag) => <span key={tag} className="inline-flex max-w-44 truncate rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-bold text-slate-600">{tag}</span>)}
                    {template.graduation_pal && <span className="inline-flex rounded-full bg-orange-100 px-2 py-0.5 text-[10px] font-bold text-orange-800">毕业帕鲁</span>}
                    {template.transitional && <span className="inline-flex rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-bold text-slate-500">低阶/过渡</span>}
                    {template.passive_names.length > 0 && <span className="inline-flex max-w-52 truncate rounded-full bg-fuchsia-100 px-2 py-0.5 text-[10px] font-bold text-fuchsia-700" title={template.passive_names.join('、')}>{template.passive_names.join('、')}</span>}
                    {template.parse_error && <span className="inline-flex rounded-full bg-rose-100 px-2 py-0.5 text-[10px] font-bold text-rose-700" title={template.parse_error}>模板解析失败</span>}
                  </span>
                </span>
              </button>;
            })}
            {visibleTemplates.length === 0 && <div className="col-span-full flex min-h-56 flex-col items-center justify-center gap-2 p-8 text-center text-slate-400"><Sparkles size={30} /><strong className="text-sm text-slate-600">没有匹配模板</strong><span className="text-xs">检查 PalDefender 模板目录或调整搜索条件</span></div>}
          </div>
        </div>}

        {activeTab === 'grants' && <div className="mt-5 min-w-0">
          <div className="mb-4 grid gap-3 sm:grid-cols-4">
            <div className="rounded-xl border border-sky-200 bg-sky-50 p-3"><span className="text-[10px] font-black uppercase tracking-wider text-sky-600">处理中</span><strong className="mt-1 block text-xl font-black text-sky-900">{grantCounts.pending}</strong></div>
            <div className="rounded-xl border border-emerald-200 bg-emerald-50 p-3"><span className="text-[10px] font-black uppercase tracking-wider text-emerald-600">已完成</span><strong className="mt-1 block text-xl font-black text-emerald-900">{grantCounts.success}</strong></div>
            <div className="rounded-xl border border-rose-200 bg-rose-50 p-3"><span className="text-[10px] font-black uppercase tracking-wider text-rose-600">失败</span><strong className="mt-1 block text-xl font-black text-rose-900">{grantCounts.failed}</strong></div>
            <div className="rounded-xl border border-violet-200 bg-violet-50 p-3"><span className="text-[10px] font-black uppercase tracking-wider text-violet-600">执行器</span><strong className="mt-1 block text-sm font-black text-violet-900">{snapshot?.worker_running ? '运行中' : '空闲'}</strong></div>
          </div>
          <div className="max-h-[42rem] overflow-y-auto rounded-2xl border border-slate-200 bg-slate-50/50 p-2">
            {(snapshot?.grants || []).map((grant) => <article className="mb-2 rounded-2xl border border-slate-200 bg-white p-4 last:mb-0" key={grant.player_id}>
              <div className="flex flex-col gap-3 md:flex-row md:items-start"><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><strong className="truncate text-sm font-black text-slate-800">{grant.nickname || grant.player_id}</strong><span className={`rounded-full border px-2 py-0.5 text-[10px] font-black ${statusClass(grant.status)}`}>{statusLabel(grant.status)}</span><span className="rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[10px] font-black text-slate-600">{phaseLabel(grant.phase)}</span>{grant.manual && <span className="rounded-full border border-violet-200 bg-violet-50 px-2 py-0.5 text-[10px] font-black text-violet-700">人工任务</span>}</div>
              <p className="mt-2 text-[11px] font-semibold leading-5 text-slate-600">{grant.detection_reason || '旧记录未保存判定原因'}</p><div className="mt-3 h-2 overflow-hidden rounded-full bg-slate-100"><div className="h-full rounded-full bg-violet-500 transition-all" style={{ width: `${Math.max(0, Math.min(100, grant.progress_percent))}%` }} /></div><span className="mt-1 block text-[10px] font-bold text-slate-500">{grant.progress_percent}% · 物品 {grant.next_item}/{grant.item_total} · 模板 {grant.next_template}/{grant.template_total} · 尝试 {grant.attempts}</span>{grant.resolved_player_id && <span className="mt-1 block truncate font-mono text-[9px] text-slate-400">PalDefender UserId：{grant.resolved_player_id}</span>}{grant.last_error && <span className="mt-2 block break-words rounded-lg bg-rose-50 px-3 py-2 text-[11px] font-semibold text-rose-700">{grant.last_error}</span>}</div>
              <div className="flex shrink-0 flex-wrap justify-end gap-2">{grant.status !== 'success' && <button type="button" className="pp-button" disabled={busy} onClick={() => void runPlayerAction(grant.player_id, 'supplement')}><PackageCheck size={14} />补发未完成</button>}<button type="button" className="pp-button" disabled={busy || !config.enabled} onClick={() => void runPlayerAction(grant.player_id, 'reissue')}><PlayCircle size={14} />完整重发</button><button type="button" className="pp-button" disabled={busy} onClick={() => void runPlayerAction(grant.player_id, 'next_login')}><Flag size={14} />下次进入重发</button><button type="button" className="icon-danger" aria-label="删除玩家礼包任务与标记" disabled={busy} onClick={() => void forget(grant.player_id)}><Trash2 size={15} /></button></div></div>
              <details className="mt-3 rounded-xl border border-slate-200 bg-slate-50/70"><summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2.5 text-[11px] font-black text-slate-700"><ChevronDown size={13} />发放进程与判定日志（{grant.events.length}）</summary><div className="border-t border-slate-200 p-3"><ol className="space-y-2">{grant.events.slice().reverse().map((event, index) => <li key={`${event.at}-${index}`} className="flex gap-2 text-[10px]"><CircleDot className={`mt-0.5 shrink-0 ${event.level === 'error' ? 'text-rose-500' : event.level === 'warn' ? 'text-amber-500' : 'text-sky-500'}`} size={12} /><span className="min-w-0"><strong className="font-black text-slate-700">{phaseLabel(event.phase)}</strong><span className="ml-2 text-slate-400">{formatTime(event.at)}</span><span className="mt-0.5 block break-words font-semibold leading-4 text-slate-600">{event.message}</span></span></li>)}</ol>{grant.events.length === 0 && <p className="text-[10px] font-semibold text-slate-400">这是旧版创建的任务，尚无事件日志；下一次操作会开始记录。</p>}</div></details>
            </article>)}
            {(snapshot?.grants || []).length === 0 && <div className="flex min-h-56 flex-col items-center justify-center gap-2 p-8 text-center text-slate-400"><ClipboardList size={30} /><strong className="text-sm text-slate-600">当前世界暂无发放记录</strong><span className="text-xs">到“玩家判定”页可以直接标记或创建测试任务</span></div>}
          </div>
        </div>}
      </section>

      <div className="sticky bottom-4 z-20 flex flex-col gap-3 rounded-2xl border border-slate-300 bg-white/95 p-3 shadow-xl backdrop-blur sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0"><div className="flex flex-wrap items-center gap-2 text-xs font-black text-slate-700"><span>{config.items.length} 种物品</span><span className="text-slate-300">·</span><span>{selectedItemTotal} 件</span><span className="text-slate-300">·</span><span>{config.pal_templates.length} 个模板</span>{dirty && <span className="rounded-full bg-amber-100 px-2 py-0.5 text-[10px] text-amber-700">未保存</span>}</div><span className="mt-1 block truncate text-[10px] font-semibold text-slate-400">当前世界：{snapshot?.scope.world_id || '尚未解析'}</span></div>
        <div className="flex shrink-0 gap-2"><button type="button" className="pp-button" disabled={!snapshot || busy || !dirty} onClick={() => snapshot && setConfig(snapshot.config)}><RotateCcw size={14} />撤销修改</button><button type="button" className="pp-button accent" disabled={busy || !snapshot || !dirty} onClick={() => void save()}>{busy ? <LoaderCircle className="animate-spin" size={15} /> : <Save size={15} />}保存配置</button></div>
      </div>
    </div>
  );
};
