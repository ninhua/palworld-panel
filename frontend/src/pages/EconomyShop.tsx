import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Archive,
  BadgeCheck,
  Ban,
  Bot,
  Boxes,
  CircleDollarSign,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  RotateCcw,
  Save,
  Search,
  ShoppingBag,
  X,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { palDefenderGMApi } from '../api/paldefenderGM';
import {
  shopApi,
  type ShopDeliveryEventType,
  type ShopDeliveryMode,
  type ShopDeliveryState,
  type ShopOrder,
  type ShopOrderCreateInput,
  type ShopOrderStatus,
  type ShopProduct,
  type ShopProductInput,
} from '../api/shop';

interface Notice {
  type: 'success' | 'error';
  text: string;
}

const number = new Intl.NumberFormat('zh-CN');

const emptyProduct = (): ShopProductInput => ({
  name: '',
  description: '',
  price: 10,
  stock: -1,
  per_player_limit: 0,
  enabled: true,
  delivery_mode: 'manual',
  payload: {},
});

const newOrderDraft = (): ShopOrderCreateInput => ({
  idempotency_key: `panel-${Date.now()}-${Math.random().toString(16).slice(2)}`,
  product_id: '',
  player_uid: '',
  nickname: '',
  steam_id: '',
  quantity: 1,
});

const statusLabel: Record<ShopOrderStatus, string> = {
  pending: '待交付',
  delivered: '已交付',
  cancelled: '已取消',
};

const deliveryModeLabel: Record<ShopDeliveryMode, string> = {
  manual: '人工交付',
  paldefender_items: 'PalDefender物品',
  paldefender_pal_templates: 'PalDefender帕鲁模板',
};

const deliveryStateLabel: Record<ShopDeliveryState, string> = {
  manual: '人工处理',
  pending: '等待自动交付',
  processing: '交付状态待核对',
  failed: '自动交付失败',
  succeeded: '游戏内已发放',
};

const eventTypeLabel: Record<ShopDeliveryEventType, string> = {
  created: '订单创建',
  started: '开始交付',
  failed: '交付失败',
  uncertain: '结果不确定',
  succeeded: '游戏内发放成功',
  reset: '人工重置',
  completed: '订单结算',
  cancelled: '订单取消',
};

interface ShopPayloadItem {
  item_id: string;
  count: number;
}

const payloadItems = (payload?: Record<string, unknown>): ShopPayloadItem[] => {
  const raw = payload?.items;
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((entry) => {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return [];
    const record = entry as Record<string, unknown>;
    const itemID = String(record.item_id || '').trim();
    const count = Number(record.count || 0);
    if (!itemID || !Number.isSafeInteger(count) || count <= 0) return [];
    return [{ item_id: itemID, count }];
  });
};

const payloadTemplates = (payload?: Record<string, unknown>): string[] => {
  const raw = payload?.pal_templates;
  if (!Array.isArray(raw)) return [];
  return [...new Set(raw.map((value) => String(value || '').trim()).filter(Boolean))];
};

const automaticPayload = (mode: ShopDeliveryMode, items: ShopPayloadItem[], templates: string[]): Record<string, unknown> => {
  if (mode === 'paldefender_items') return { items: items.map((item) => ({ item_id: item.item_id, count: item.count })) };
  if (mode === 'paldefender_pal_templates') return { pal_templates: templates };
  return {};
};

const formatTime = (value?: string) => {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN');
};

const parsePayload = (value: string): Record<string, unknown> => {
  const trimmed = value.trim();
  if (!trimmed) return {};
  const parsed = JSON.parse(trimmed) as unknown;
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error('商品Payload必须是JSON对象。');
  }
  return parsed as Record<string, unknown>;
};

export const EconomyShop: React.FC = () => {
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<Notice | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingID, setEditingID] = useState('');
  const [productDraft, setProductDraft] = useState<ShopProductInput>(emptyProduct());
  const [payloadText, setPayloadText] = useState('{}');
  const [selectedItems, setSelectedItems] = useState<ShopPayloadItem[]>([]);
  const [selectedTemplates, setSelectedTemplates] = useState<string[]>([]);
  const [itemSearch, setItemSearch] = useState('');
  const [itemChoice, setItemChoice] = useState('');
  const [templateSearch, setTemplateSearch] = useState('');
  const [templateChoice, setTemplateChoice] = useState('');
  const [orderDraft, setOrderDraft] = useState<ShopOrderCreateInput>(newOrderDraft());
  const [activeTab, setActiveTab] = useState<'orders' | 'audit'>('orders');
  const [orderStatus, setOrderStatus] = useState('');
  const [orderPlayerUID, setOrderPlayerUID] = useState('');
  const [orderDeliveryState, setOrderDeliveryState] = useState('');
  const [orderDeliveryMode, setOrderDeliveryMode] = useState('');
  const [eventOrderID, setEventOrderID] = useState('');
  const [eventType, setEventType] = useState('');
  const [includeFailed, setIncludeFailed] = useState(false);

  const summaryQuery = useQuery({ queryKey: ['shop', 'summary'], queryFn: shopApi.summary });
  const productsQuery = useQuery({ queryKey: ['shop', 'products'], queryFn: () => shopApi.products(true) });
  const itemCatalogQuery = useQuery({
    queryKey: ['shop', 'catalog', 'items'],
    queryFn: () => palDefenderGMApi.items('', 5000),
    enabled: editorOpen && productDraft.delivery_mode === 'paldefender_items',
    staleTime: 30 * 60 * 1000,
  });
  const templateCatalogQuery = useQuery({
    queryKey: ['shop', 'catalog', 'pal-templates'],
    queryFn: palDefenderGMApi.templates,
    enabled: editorOpen && productDraft.delivery_mode === 'paldefender_pal_templates',
    staleTime: 5 * 60 * 1000,
  });
  const ordersQuery = useQuery({
    queryKey: ['shop', 'orders', orderStatus, orderPlayerUID, orderDeliveryState, orderDeliveryMode],
    queryFn: () => shopApi.orders(orderStatus, orderPlayerUID.trim(), orderDeliveryState, orderDeliveryMode),
  });
  const eventsQuery = useQuery({
    queryKey: ['shop', 'delivery-events', eventOrderID, eventType],
    queryFn: () => shopApi.deliveryEvents(eventOrderID.trim(), eventType),
    enabled: activeTab === 'audit',
  });

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['shop'] });
  };

  const saveProductMutation = useMutation({
    mutationFn: async () => {
      let payload: Record<string, unknown>;
      if (productDraft.delivery_mode === 'paldefender_items') {
        if (selectedItems.length === 0) throw new Error('请至少从物品列表添加一种物品。');
        if (selectedItems.some((item) => !Number.isSafeInteger(item.count) || item.count <= 0)) throw new Error('物品数量必须是大于0的整数。');
        payload = automaticPayload(productDraft.delivery_mode, selectedItems, selectedTemplates);
      } else if (productDraft.delivery_mode === 'paldefender_pal_templates') {
        if (selectedTemplates.length === 0) throw new Error('请至少从帕鲁模板列表添加一个模板。');
        payload = automaticPayload(productDraft.delivery_mode, selectedItems, selectedTemplates);
      } else {
        payload = parsePayload(payloadText);
      }
      const input: ShopProductInput = {
        ...productDraft,
        name: productDraft.name.trim(),
        description: productDraft.description?.trim() || '',
        price: Number(productDraft.price),
        stock: Number(productDraft.stock),
        per_player_limit: Number(productDraft.per_player_limit),
        payload,
      };
      if (!input.name) throw new Error('请填写商品名称。');
      if (!Number.isSafeInteger(input.price) || input.price <= 0) throw new Error('商品价格必须是大于0的整数。');
      if (!Number.isSafeInteger(input.stock) || input.stock < -1) throw new Error('库存必须是-1或非负整数。');
      if (!Number.isSafeInteger(input.per_player_limit) || input.per_player_limit < 0) throw new Error('每人限购必须是非负整数。');
      return editingID ? shopApi.updateProduct(editingID, input) : shopApi.createProduct(input);
    },
    onSuccess: async (product) => {
      setNotice({ type: 'success', text: editingID ? `商品“${product.name}”已更新。` : `商品“${product.name}”已创建。` });
      closeEditor();
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const archiveProductMutation = useMutation({
    mutationFn: (product: ShopProduct) => shopApi.archiveProduct(product.id),
    onSuccess: async (product) => {
      setNotice({ type: 'success', text: `商品“${product.name}”已下架，历史订单保留。` });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const createOrderMutation = useMutation({
    mutationFn: async () => {
      const input: ShopOrderCreateInput = {
        ...orderDraft,
        idempotency_key: orderDraft.idempotency_key.trim(),
        product_id: orderDraft.product_id.trim(),
        player_uid: orderDraft.player_uid.trim(),
        nickname: orderDraft.nickname?.trim() || '',
        steam_id: orderDraft.steam_id?.trim() || '',
        quantity: Number(orderDraft.quantity),
      };
      if (!input.product_id) throw new Error('请选择商品。');
      if (!input.player_uid) throw new Error('请填写PlayerUID。');
      if (!input.idempotency_key) throw new Error('幂等键不能为空。');
      if (!Number.isSafeInteger(input.quantity) || input.quantity < 1) throw new Error('兑换数量必须是正整数。');
      return shopApi.createOrder(input);
    },
    onSuccess: async (result) => {
      const hasRisk = result.order.delivery_state === 'failed' || result.order.delivery_state === 'processing';
      setNotice({
        type: hasRisk ? 'error' : 'success',
        text: result.duplicate
          ? `检测到重复请求，已返回原订单 ${result.order.id}。`
          : result.order.status === 'delivered'
            ? `订单 ${result.order.id} 已自动交付并结算，当前余额 ${number.format(result.account.balance)}。`
            : hasRisk
              ? `订单已创建并预扣积分，但自动交付需要处理：${result.order.failure || deliveryStateLabel[result.order.delivery_state]}`
              : `订单 ${result.order.id} 已创建并预扣 ${number.format(result.order.total_points)} 积分。`,
      });
      setOrderDraft(newOrderDraft());
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const settleOrderMutation = useMutation({
    mutationFn: async ({ order, action }: { order: ShopOrder; action: 'deliver' | 'complete' | 'cancel' | 'reset' }) => {
      if (action === 'deliver') return shopApi.deliverOrder(order.id);
      if (action === 'complete') return shopApi.completeOrder(order.id);
      if (action === 'cancel') return shopApi.cancelOrder(order.id);
      const resetOrder = await shopApi.resetDelivery(order.id);
      return {
        order: resetOrder,
        account: { player_uid: resetOrder.player_uid, status: 'active', balance: 0, created_at: '', updated_at: '' },
        duplicate: false,
      };
    },
    onSuccess: async (result) => {
      setNotice({
        type: 'success',
        text: result.order.status === 'delivered'
          ? `订单 ${result.order.id} 已完成交付和积分结算。`
          : result.order.status === 'cancelled'
            ? `订单 ${result.order.id} 已取消，积分已退回。`
            : `订单 ${result.order.id} 的交付状态已重置。`,
      });
      await refresh();
    },
    onError: async (error) => {
      setNotice({ type: 'error', text: getErrorMessage(error) });
      await refresh();
    },
  });

  const batchDeliveryMutation = useMutation({
    mutationFn: () => shopApi.deliverBatch({ include_failed: includeFailed, limit: 20 }),
    onSuccess: async (result) => {
      const risky = result.failed > 0 || result.uncertain > 0;
      setNotice({
        type: risky ? 'error' : 'success',
        text: `批量交付完成：选择 ${result.selected}，成功 ${result.delivered}，失败 ${result.failed}，待核对 ${result.uncertain}，跳过 ${result.skipped}。`,
      });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const products = productsQuery.data?.items || [];
  const enabledProducts = useMemo(() => products.filter((item) => item.enabled), [products]);
  const orders = ordersQuery.data?.items || [];
  const events = eventsQuery.data?.items || [];
  const summary = summaryQuery.data || {
    products: 0,
    enabled_products: 0,
    pending_orders: 0,
    delivered_orders: 0,
    spent_points: 0,
    failed_deliveries: 0,
    processing_deliveries: 0,
    delivery_events: 0,
  };
  const catalogItems = itemCatalogQuery.data?.items || [];
  const templates = templateCatalogQuery.data?.templates || [];
  const filteredCatalogItems = useMemo(() => {
    const needle = itemSearch.trim().toLowerCase();
    if (!needle) return catalogItems.slice(0, 200);
    return catalogItems.filter((item) => `${item.id} ${item.name}`.toLowerCase().includes(needle)).slice(0, 200);
  }, [catalogItems, itemSearch]);
  const filteredTemplates = useMemo(() => {
    const needle = templateSearch.trim().toLowerCase();
    if (!needle) return templates.slice(0, 200);
    return templates.filter((template) => `${template.name} ${template.path}`.toLowerCase().includes(needle)).slice(0, 200);
  }, [templateSearch, templates]);
  const itemNameByID = useMemo(() => new Map(catalogItems.map((item) => [item.id, item.name])), [catalogItems]);
  const generatedPayload = automaticPayload(productDraft.delivery_mode, selectedItems, selectedTemplates);
  const payloadPreview = productDraft.delivery_mode === 'manual' ? payloadText : JSON.stringify(generatedPayload, null, 2);

  function resetCatalogDraft() {
    setSelectedItems([]);
    setSelectedTemplates([]);
    setItemSearch('');
    setItemChoice('');
    setTemplateSearch('');
    setTemplateChoice('');
  }

  function closeEditor() {
    setEditorOpen(false);
    setEditingID('');
    setProductDraft(emptyProduct());
    setPayloadText('{}');
    resetCatalogDraft();
  }

  const openCreate = () => {
    setEditingID('');
    setProductDraft(emptyProduct());
    setPayloadText('{}');
    resetCatalogDraft();
    setEditorOpen(true);
    setNotice(null);
  };

  const openEdit = (product: ShopProduct) => {
    setEditingID(product.id);
    setProductDraft({
      name: product.name,
      description: product.description || '',
      price: product.price,
      stock: product.stock,
      per_player_limit: product.per_player_limit,
      enabled: product.enabled,
      delivery_mode: product.delivery_mode,
      payload: product.payload || {},
    });
    setPayloadText(JSON.stringify(product.payload || {}, null, 2));
    setSelectedItems(payloadItems(product.payload));
    setSelectedTemplates(payloadTemplates(product.payload));
    setItemSearch('');
    setItemChoice('');
    setTemplateSearch('');
    setTemplateChoice('');
    setEditorOpen(true);
    setNotice(null);
  };

  const changeDeliveryMode = (mode: ShopDeliveryMode) => {
    setProductDraft((current) => ({ ...current, delivery_mode: mode }));
    if (mode === 'manual') setPayloadText('{}');
    if (mode === 'paldefender_items') setSelectedTemplates([]);
    if (mode === 'paldefender_pal_templates') setSelectedItems([]);
  };

  const addItem = () => {
    const itemID = itemChoice.trim();
    if (!itemID) return;
    setSelectedItems((current) => {
      if (current.some((item) => item.item_id === itemID) || current.length >= 100) return current;
      return [...current, { item_id: itemID, count: 1 }];
    });
    setItemChoice('');
  };

  const addTemplate = () => {
    const name = templateChoice.trim();
    if (!name) return;
    setSelectedTemplates((current) => {
      if (current.includes(name) || current.length >= 20) return current;
      return [...current, name];
    });
    setTemplateChoice('');
  };

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      {notice && (
        <div className={`rounded-2xl border px-5 py-3 text-xs font-semibold ${notice.type === 'success' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-rose-100 bg-rose-50 text-rose-700'}`}>
          {notice.text}
        </div>
      )}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-8">
        <SummaryCard icon={<ShoppingBag size={18} />} label="商品总数" value={summary.products} />
        <SummaryCard icon={<BadgeCheck size={18} />} label="上架商品" value={summary.enabled_products} />
        <SummaryCard icon={<Boxes size={18} />} label="待交付" value={summary.pending_orders} />
        <SummaryCard icon={<Save size={18} />} label="已交付" value={summary.delivered_orders} />
        <SummaryCard icon={<CircleDollarSign size={18} />} label="已消费积分" value={summary.spent_points} />
        <SummaryCard icon={<Ban size={18} />} label="交付失败" value={summary.failed_deliveries} />
        <SummaryCard icon={<Bot size={18} />} label="待人工核对" value={summary.processing_deliveries} />
        <SummaryCard icon={<RotateCcw size={18} />} label="审计事件" value={summary.delivery_events} />
      </div>

      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-sm">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div><h2 className="text-base font-bold text-slate-900">商品管理</h2><p className="mt-1 text-xs text-slate-500">配置积分价格、库存、限购和交付Payload。</p></div>
          <div className="flex gap-2">
            <button type="button" onClick={() => refresh()} className="flex items-center gap-2 rounded-xl border border-slate-200 px-4 py-2 text-xs font-semibold text-slate-600"><RefreshCw size={14} />刷新</button>
            <button type="button" onClick={openCreate} className="flex items-center gap-2 rounded-xl bg-sky-500 px-4 py-2 text-xs font-semibold text-white"><Plus size={14} />新增商品</button>
          </div>
        </div>
        {productsQuery.isLoading ? <Loading text="正在加载商品..." /> : products.length === 0 ? <Empty text="暂无商品。" /> : (
          <div className="mt-5 grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
            {products.map((product) => (
              <div key={product.id} className={`rounded-2xl border p-4 ${product.enabled ? 'border-slate-100' : 'border-slate-100 bg-slate-50/70 opacity-70'}`}>
                <div className="flex items-start justify-between gap-3"><div><div className="font-bold text-slate-800">{product.name}</div><div className="mt-1 text-[10px] text-slate-400">{product.id}</div></div><span className={`rounded-full px-2 py-1 text-[10px] font-bold ${product.enabled ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{product.enabled ? '上架' : '下架'}</span></div>
                <p className="mt-3 min-h-8 text-xs leading-5 text-slate-500">{product.description || '无说明'}</p>
                <div className="mt-4 grid grid-cols-2 gap-2 text-xs"><Metric label="价格" value={`${number.format(product.price)} 积分`} /><Metric label="库存" value={product.stock < 0 ? '不限' : number.format(product.stock)} /><Metric label="每人限购" value={product.per_player_limit === 0 ? '不限' : number.format(product.per_player_limit)} /><Metric label="交付" value={deliveryModeLabel[product.delivery_mode]} /></div>
                <div className="mt-4 flex justify-end gap-2"><button type="button" onClick={() => openEdit(product)} className="flex items-center gap-1 rounded-lg bg-slate-100 px-3 py-2 text-xs font-semibold text-slate-600"><Pencil size={12} />编辑</button>{product.enabled && <button type="button" onClick={() => { if (window.confirm(`下架商品“${product.name}”？`)) archiveProductMutation.mutate(product); }} className="flex items-center gap-1 rounded-lg bg-rose-50 px-3 py-2 text-xs font-semibold text-rose-700"><Archive size={12} />下架</button>}</div>
              </div>
            ))}
          </div>
        )}
      </section>

      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-sm">
        <div><h2 className="text-base font-bold text-slate-900">创建兑换订单</h2><p className="mt-1 text-xs text-slate-500">管理员代玩家兑换；自动商品会立即尝试通过PalDefender发放。</p></div>
        <form onSubmit={(event) => { event.preventDefault(); createOrderMutation.mutate(); }} className="mt-5 grid gap-3 lg:grid-cols-6">
          <select value={orderDraft.product_id} onChange={(event) => setOrderDraft((current) => ({ ...current, product_id: event.target.value }))} className="pp-input lg:col-span-2"><option value="">选择上架商品</option>{enabledProducts.map((product) => <option key={product.id} value={product.id}>{product.name} · {product.price}积分</option>)}</select>
          <input value={orderDraft.player_uid} onChange={(event) => setOrderDraft((current) => ({ ...current, player_uid: event.target.value }))} placeholder="PlayerUID" className="pp-input lg:col-span-2" />
          <input type="number" min={1} max={1000} value={orderDraft.quantity} onChange={(event) => setOrderDraft((current) => ({ ...current, quantity: Number(event.target.value) }))} className="pp-input" />
          <button disabled={createOrderMutation.isPending} type="submit" className="flex items-center justify-center gap-2 rounded-xl bg-sky-500 px-4 py-2 text-xs font-semibold text-white disabled:opacity-50">{createOrderMutation.isPending ? <LoaderCircle size={14} className="animate-spin" /> : <ShoppingBag size={14} />}创建订单</button>
          <input value={orderDraft.nickname || ''} onChange={(event) => setOrderDraft((current) => ({ ...current, nickname: event.target.value }))} placeholder="昵称（可选）" className="pp-input lg:col-span-2" />
          <input value={orderDraft.steam_id || ''} onChange={(event) => setOrderDraft((current) => ({ ...current, steam_id: event.target.value }))} placeholder="SteamID（可选）" className="pp-input lg:col-span-2" />
          <input value={orderDraft.idempotency_key} onChange={(event) => setOrderDraft((current) => ({ ...current, idempotency_key: event.target.value }))} placeholder="幂等键" className="pp-input lg:col-span-2" />
        </form>
      </section>

      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-sm">
        <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex rounded-xl bg-slate-100 p-1 text-xs font-semibold"><button type="button" onClick={() => setActiveTab('orders')} className={`rounded-lg px-4 py-2 ${activeTab === 'orders' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500'}`}>订单</button><button type="button" onClick={() => setActiveTab('audit')} className={`rounded-lg px-4 py-2 ${activeTab === 'audit' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500'}`}>交付审计</button></div>
          {activeTab === 'orders' && <div className="flex flex-wrap items-center gap-2"><label className="flex items-center gap-2 rounded-xl border border-slate-200 px-3 py-2 text-xs font-semibold text-slate-600"><input type="checkbox" checked={includeFailed} onChange={(event) => setIncludeFailed(event.target.checked)} />包含失败订单</label><button type="button" disabled={batchDeliveryMutation.isPending} onClick={() => { if (window.confirm('批量处理最多20个安全可重试的自动交付订单？处理中订单不会被重试。')) batchDeliveryMutation.mutate(); }} className="flex items-center gap-2 rounded-xl bg-sky-50 px-4 py-2 text-xs font-semibold text-sky-700 disabled:opacity-50">{batchDeliveryMutation.isPending ? <LoaderCircle size={14} className="animate-spin" /> : <RefreshCw size={14} />}批量自动交付</button></div>}
        </div>

        {activeTab === 'orders' ? (
          <div className="mt-5">
            <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-5"><select value={orderStatus} onChange={(event) => setOrderStatus(event.target.value)} className="pp-input"><option value="">全部订单状态</option><option value="pending">待交付</option><option value="delivered">已交付</option><option value="cancelled">已取消</option></select><select value={orderDeliveryState} onChange={(event) => setOrderDeliveryState(event.target.value)} className="pp-input"><option value="">全部交付状态</option><option value="pending">等待自动交付</option><option value="failed">自动交付失败</option><option value="processing">待人工核对</option><option value="succeeded">游戏内已发放</option><option value="manual">人工处理</option></select><select value={orderDeliveryMode} onChange={(event) => setOrderDeliveryMode(event.target.value)} className="pp-input"><option value="">全部交付方式</option><option value="automatic">全部自动交付</option><option value="manual">人工交付</option><option value="paldefender_items">PalDefender物品</option><option value="paldefender_pal_templates">PalDefender帕鲁模板</option></select><div className="relative md:col-span-2 xl:col-span-2"><Search size={14} className="absolute left-3 top-3 text-slate-400" /><input value={orderPlayerUID} onChange={(event) => setOrderPlayerUID(event.target.value)} placeholder="按PlayerUID筛选" className="pp-input w-full pl-9" /></div></div>
            {ordersQuery.isLoading ? <Loading text="正在加载订单..." /> : orders.length === 0 ? <Empty text="暂无匹配订单。" /> : <OrderTable orders={orders} mutation={settleOrderMutation} />}
          </div>
        ) : (
          <div className="mt-5">
            <div className="grid gap-2 md:grid-cols-3"><input value={eventOrderID} onChange={(event) => setEventOrderID(event.target.value)} placeholder="按订单ID筛选" className="pp-input md:col-span-2" /><select value={eventType} onChange={(event) => setEventType(event.target.value)} className="pp-input"><option value="">全部事件类型</option>{Object.entries(eventTypeLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></div>
            {eventsQuery.isLoading ? <Loading text="正在加载交付审计..." /> : events.length === 0 ? <Empty text="暂无交付审计事件。" /> : <div className="mt-4 overflow-x-auto"><table className="w-full min-w-[820px] text-left text-xs"><thead className="bg-slate-50 text-[10px] uppercase text-slate-400"><tr><th className="px-3 py-3">事件</th><th className="px-3 py-3">订单</th><th className="px-3 py-3">状态/尝试</th><th className="px-3 py-3">操作者</th><th className="px-3 py-3">说明</th><th className="px-3 py-3">时间</th></tr></thead><tbody>{events.map((event) => <tr key={event.id} className="border-b border-slate-50"><td className="px-3 py-3 font-semibold text-slate-700">{eventTypeLabel[event.event_type]}</td><td className="px-3 py-3 font-mono text-[10px] text-slate-500">{event.order_id}</td><td className="px-3 py-3 text-slate-500">{event.delivery_state ? deliveryStateLabel[event.delivery_state] : '-'} · {event.attempt}</td><td className="px-3 py-3 text-slate-500">{event.actor || '-'}</td><td className="max-w-80 px-3 py-3 text-slate-500">{event.message || '-'}</td><td className="px-3 py-3 text-slate-400">{formatTime(event.created_at)}</td></tr>)}</tbody></table></div>}
          </div>
        )}
      </section>

      {editorOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/30 p-4 backdrop-blur-sm">
          <form onSubmit={(event) => { event.preventDefault(); saveProductMutation.mutate(); }} className="max-h-[92vh] w-full max-w-3xl overflow-y-auto rounded-3xl bg-white p-6 shadow-2xl">
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-lg font-bold text-slate-900">{editingID ? '编辑商品' : '新增商品'}</h2>
                <p className="mt-1 text-xs text-slate-500">库存-1表示不限；自动交付商品可直接从目录选择，系统会生成Payload。</p>
              </div>
              <button type="button" onClick={closeEditor} className="rounded-xl p-2 text-slate-500 hover:bg-slate-100"><X size={18} /></button>
            </div>

            <div className="mt-6 grid gap-4 sm:grid-cols-2">
              <Field label="商品名称"><input value={productDraft.name} onChange={(event) => setProductDraft((current) => ({ ...current, name: event.target.value }))} className="pp-input w-full" /></Field>
              <Field label="价格（积分）"><input type="number" min={1} value={productDraft.price} onChange={(event) => setProductDraft((current) => ({ ...current, price: Number(event.target.value) }))} className="pp-input w-full" /></Field>
              <Field label="库存"><input type="number" min={-1} value={productDraft.stock} onChange={(event) => setProductDraft((current) => ({ ...current, stock: Number(event.target.value) }))} className="pp-input w-full" /></Field>
              <Field label="每人限购"><input type="number" min={0} value={productDraft.per_player_limit} onChange={(event) => setProductDraft((current) => ({ ...current, per_player_limit: Number(event.target.value) }))} className="pp-input w-full" /></Field>
              <Field label="交付方式">
                <select value={productDraft.delivery_mode} onChange={(event) => changeDeliveryMode(event.target.value as ShopDeliveryMode)} className="pp-input w-full">
                  <option value="manual">人工交付</option>
                  <option value="paldefender_items">PalDefender物品</option>
                  <option value="paldefender_pal_templates">PalDefender帕鲁模板</option>
                </select>
              </Field>
              <label className="flex items-center gap-3 self-end rounded-xl border border-slate-200 px-4 py-3 text-xs font-semibold text-slate-600"><input type="checkbox" checked={productDraft.enabled} onChange={(event) => setProductDraft((current) => ({ ...current, enabled: event.target.checked }))} />立即上架</label>
              <div className="sm:col-span-2"><Field label="商品说明"><textarea value={productDraft.description || ''} onChange={(event) => setProductDraft((current) => ({ ...current, description: event.target.value }))} rows={3} className="pp-input w-full resize-y" /></Field></div>
            </div>

            {productDraft.delivery_mode === 'paldefender_items' && (
              <section className="mt-5 rounded-2xl border border-slate-200 p-4">
                <div className="flex items-start justify-between gap-3"><div><h3 className="text-sm font-black text-slate-800">选择物品</h3><p className="mt-1 text-xs leading-5 text-slate-500">目录来自面板内置物品本地化列表。订单数量会再乘以这里配置的每件数量。</p></div>{itemCatalogQuery.isFetching && <LoaderCircle size={16} className="animate-spin text-slate-400" />}</div>
                <div className="mt-4 grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
                  <div className="relative"><Search size={14} className="absolute left-3 top-3 text-slate-400" /><input value={itemSearch} onChange={(event) => setItemSearch(event.target.value)} placeholder="搜索物品名称或ID" className="pp-input w-full pl-9" /></div>
                  <select value={itemChoice} onChange={(event) => setItemChoice(event.target.value)} className="pp-input min-w-0"><option value="">选择物品</option>{filteredCatalogItems.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.id}</option>)}</select>
                  <button type="button" onClick={addItem} disabled={!itemChoice} className="pp-btn pp-btn--primary disabled:opacity-40"><Plus size={14} />添加</button>
                </div>
                {itemCatalogQuery.error && <p className="mt-3 rounded-xl bg-rose-50 px-3 py-2 text-xs font-semibold text-rose-700">{getErrorMessage(itemCatalogQuery.error)}</p>}
                <div className="mt-4 space-y-2">
                  {selectedItems.map((item) => (
                    <div key={item.item_id} className="grid items-center gap-2 rounded-xl bg-slate-50 px-3 py-2 sm:grid-cols-[1fr_140px_auto]">
                      <div className="min-w-0"><div className="truncate text-xs font-bold text-slate-700">{itemNameByID.get(item.item_id) || item.item_id}</div><div className="truncate font-mono text-[10px] text-slate-400">{item.item_id}</div></div>
                      <label className="flex items-center gap-2 text-xs font-semibold text-slate-500">数量<input type="number" min={1} max={2147483647} value={item.count} onChange={(event) => { const count = Number(event.target.value); setSelectedItems((current) => current.map((entry) => entry.item_id === item.item_id ? { ...entry, count } : entry)); }} className="pp-input w-24" /></label>
                      <button type="button" onClick={() => setSelectedItems((current) => current.filter((entry) => entry.item_id !== item.item_id))} className="rounded-lg p-2 text-rose-500 hover:bg-rose-50" aria-label={`移除${item.item_id}`}><X size={15} /></button>
                    </div>
                  ))}
                  {selectedItems.length === 0 && <div className="rounded-xl border border-dashed border-slate-200 py-6 text-center text-xs text-slate-400">尚未添加物品。</div>}
                </div>
              </section>
            )}

            {productDraft.delivery_mode === 'paldefender_pal_templates' && (
              <section className="mt-5 rounded-2xl border border-slate-200 p-4">
                <div className="flex items-start justify-between gap-3"><div><h3 className="text-sm font-black text-slate-800">选择帕鲁模板</h3><p className="mt-1 text-xs leading-5 text-slate-500">列表来自PalDefender模板目录。订单数量大于1时，会重复发放整组模板。</p></div>{templateCatalogQuery.isFetching && <LoaderCircle size={16} className="animate-spin text-slate-400" />}</div>
                <div className="mt-4 grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
                  <div className="relative"><Search size={14} className="absolute left-3 top-3 text-slate-400" /><input value={templateSearch} onChange={(event) => setTemplateSearch(event.target.value)} placeholder="搜索模板名称" className="pp-input w-full pl-9" /></div>
                  <select value={templateChoice} onChange={(event) => setTemplateChoice(event.target.value)} className="pp-input min-w-0"><option value="">选择模板</option>{filteredTemplates.map((template) => <option key={template.name} value={template.name}>{template.name}</option>)}</select>
                  <button type="button" onClick={addTemplate} disabled={!templateChoice} className="pp-btn pp-btn--primary disabled:opacity-40"><Plus size={14} />添加</button>
                </div>
                {templateCatalogQuery.error && <p className="mt-3 rounded-xl bg-rose-50 px-3 py-2 text-xs font-semibold text-rose-700">{getErrorMessage(templateCatalogQuery.error)}</p>}
                <div className="mt-4 flex flex-wrap gap-2">
                  {selectedTemplates.map((name) => <span key={name} className="inline-flex items-center gap-2 rounded-full bg-violet-50 px-3 py-2 font-mono text-[11px] font-bold text-violet-700">{name}<button type="button" onClick={() => setSelectedTemplates((current) => current.filter((entry) => entry !== name))} className="text-violet-400 hover:text-rose-500" aria-label={`移除${name}`}><X size={13} /></button></span>)}
                  {selectedTemplates.length === 0 && <div className="w-full rounded-xl border border-dashed border-slate-200 py-6 text-center text-xs text-slate-400">尚未添加帕鲁模板。</div>}
                </div>
              </section>
            )}

            {productDraft.delivery_mode === 'manual' && (
              <section className="mt-5 rounded-2xl border border-slate-200 p-4">
                <Field label="人工交付附加Payload（可选JSON对象）"><textarea value={payloadText} onChange={(event) => setPayloadText(event.target.value)} rows={5} spellCheck={false} className="pp-input w-full resize-y font-mono text-[11px]" /></Field>
                <p className="mt-2 text-xs leading-5 text-slate-400">人工商品通常使用空对象 <code>{'{}'}</code>。这里的数据只随商品和订单保存，供管理员或后续扩展读取。</p>
              </section>
            )}

            <details className="mt-5 rounded-2xl border border-sky-100 bg-sky-50/50 p-4">
              <summary className="cursor-pointer text-xs font-black text-sky-800">查看系统生成的最终Payload</summary>
              <p className="mt-3 text-xs leading-5 text-sky-700">Payload是商品的机器可读交付参数。物品商品保存物品ID和数量；帕鲁商品保存模板文件名。管理员无需再手写。</p>
              <pre className="mt-3 overflow-x-auto rounded-xl bg-slate-950 p-4 text-[11px] leading-5 text-slate-100">{payloadPreview || '{}'}</pre>
            </details>

            <div className="mt-6 flex justify-end gap-2"><button type="button" onClick={closeEditor} className="rounded-xl border border-slate-200 px-4 py-2 text-xs font-semibold text-slate-600">取消</button><button disabled={saveProductMutation.isPending} type="submit" className="flex items-center gap-2 rounded-xl bg-sky-500 px-4 py-2 text-xs font-semibold text-white disabled:opacity-50">{saveProductMutation.isPending ? <LoaderCircle size={14} className="animate-spin" /> : <Save size={14} />}保存商品</button></div>
          </form>
        </div>
      )}
    </div>
  );
};

const OrderTable: React.FC<{
  orders: ShopOrder[];
  mutation: { mutate: (variables: { order: ShopOrder; action: 'deliver' | 'complete' | 'cancel' | 'reset' }) => void };
}> = ({ orders, mutation }) => (
  <div className="mt-4 overflow-x-auto"><table className="w-full min-w-[1060px] text-left text-xs"><thead className="bg-slate-50 text-[10px] uppercase text-slate-400"><tr><th className="px-3 py-3">订单</th><th className="px-3 py-3">玩家</th><th className="px-3 py-3">商品</th><th className="px-3 py-3">积分</th><th className="px-3 py-3">状态</th><th className="px-3 py-3">时间</th><th className="px-3 py-3 text-center">操作</th></tr></thead><tbody>{orders.map((order) => <tr key={order.id} className="border-b border-slate-50 align-top"><td className="px-3 py-3 font-mono text-[10px] text-slate-500">{order.id}</td><td className="px-3 py-3"><div className="font-semibold text-slate-700">{order.nickname || '-'}</div><div className="mt-1 font-mono text-[10px] text-slate-400">{order.player_uid}</div></td><td className="px-3 py-3"><div className="font-semibold text-slate-700">{order.product_name}</div><div className="mt-1 text-slate-400">× {order.quantity}</div></td><td className="px-3 py-3 font-bold text-slate-700">{number.format(order.total_points)}</td><td className="px-3 py-3"><span className={`rounded-full px-2 py-1 text-[10px] font-bold ${order.status === 'pending' ? 'bg-amber-50 text-amber-700' : order.status === 'delivered' ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{statusLabel[order.status]}</span><div className={`mt-2 text-[10px] font-semibold ${order.delivery_state === 'failed' ? 'text-rose-600' : order.delivery_state === 'processing' ? 'text-amber-700' : 'text-slate-400'}`}>{deliveryStateLabel[order.delivery_state]} · 尝试{order.delivery_attempts}次</div>{order.failure && <div className="mt-1 max-w-56 text-[10px] leading-4 text-rose-500">{order.failure}</div>}</td><td className="px-3 py-3 text-slate-400"><div>{formatTime(order.created_at)}</div>{order.last_delivery_at && <div className="mt-1 text-[10px]">交付：{formatTime(order.last_delivery_at)}</div>}</td><td className="px-3 py-3">{order.status === 'pending' ? <div className="flex flex-wrap justify-center gap-1">{order.delivery_mode !== 'manual' && order.delivery_state !== 'processing' && <button type="button" onClick={() => mutation.mutate({ order, action: 'deliver' })} className="flex items-center gap-1 rounded-lg bg-sky-50 px-2 py-1.5 font-semibold text-sky-700"><Bot size={12} />{order.delivery_state === 'failed' ? '重试' : order.delivery_state === 'succeeded' ? '完成结算' : '自动交付'}</button>}{(order.delivery_mode === 'manual' || order.delivery_state !== 'succeeded') && <button type="button" onClick={() => { if (window.confirm(`确认订单 ${order.id} 已在游戏内完成交付？`)) mutation.mutate({ order, action: 'complete' }); }} className="flex items-center gap-1 rounded-lg bg-emerald-50 px-2 py-1.5 font-semibold text-emerald-700"><BadgeCheck size={12} />确认交付</button>}{order.delivery_mode !== 'manual' && (order.delivery_state === 'processing' || order.delivery_state === 'failed') && <button type="button" onClick={() => { if (window.confirm(`仅在确认游戏内没有发放后重置订单 ${order.id}。继续？`)) mutation.mutate({ order, action: 'reset' }); }} className="flex items-center gap-1 rounded-lg bg-amber-50 px-2 py-1.5 font-semibold text-amber-700"><RotateCcw size={12} />重置</button>}{(order.delivery_mode === 'manual' || order.delivery_state === 'pending' || order.delivery_state === 'failed') && <button type="button" onClick={() => { if (window.confirm(`取消订单 ${order.id} 并退还积分？`)) mutation.mutate({ order, action: 'cancel' }); }} className="flex items-center gap-1 rounded-lg bg-rose-50 px-2 py-1.5 font-semibold text-rose-700"><Ban size={12} />取消</button>}</div> : <div className="text-center text-slate-300">-</div>}</td></tr>)}</tbody></table></div>
);

const SummaryCard: React.FC<{ icon: React.ReactNode; label: string; value: number }> = ({ icon, label, value }) => <div className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm"><div className="flex items-center gap-2 text-slate-400">{icon}<span className="text-xs font-semibold">{label}</span></div><div className="mt-3 text-2xl font-bold text-slate-900">{number.format(value)}</div></div>;
const Field: React.FC<{ label: string; children: React.ReactNode }> = ({ label, children }) => <label className="flex flex-col gap-2 text-xs font-semibold text-slate-600"><span>{label}</span>{children}</label>;
const Metric: React.FC<{ label: string; value: string }> = ({ label, value }) => <div className="rounded-xl bg-slate-50 px-3 py-2"><div className="text-[10px] text-slate-400">{label}</div><div className="mt-1 font-bold text-slate-700">{value}</div></div>;
const Loading: React.FC<{ text: string }> = ({ text }) => <div className="py-10 text-center text-xs font-semibold text-slate-400"><LoaderCircle size={14} className="mr-2 inline animate-spin" />{text}</div>;
const Empty: React.FC<{ text: string }> = ({ text }) => <div className="rounded-2xl border border-dashed border-slate-200 py-10 text-center text-xs font-semibold text-slate-400">{text}</div>;
