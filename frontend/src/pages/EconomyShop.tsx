import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Archive, BadgeCheck, Ban, Bot, Boxes, CircleDollarSign, LoaderCircle, Pencil, Plus,
  RefreshCw, RotateCcw, Save, Search, ShoppingBag, X,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
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
  type ShopRedemptionAttempt,
} from '../api/shop';
import { ShopPayloadCatalogEditor } from '../components/shop/ShopPayloadCatalogEditor';

interface Notice { type: 'success' | 'error'; text: string }
type Tab = 'orders' | 'audit' | 'redemptions';
const number = new Intl.NumberFormat('zh-CN');
const productCode = (id: string) => (id.includes('_') ? id.slice(id.lastIndexOf('_') + 1) : id).slice(0, 8).toUpperCase();
const emptyProduct = (): ShopProductInput => ({ name: '', description: '', price: 10, stock: -1, per_player_limit: 0, enabled: true, delivery_mode: 'manual', payload: {} });
const newOrderDraft = (): ShopOrderCreateInput => ({ idempotency_key: `panel-${Date.now()}-${Math.random().toString(16).slice(2)}`, product_id: '', player_uid: '', nickname: '', steam_id: '', quantity: 1 });
const statusLabel: Record<ShopOrderStatus, string> = { pending: '待交付', delivered: '已交付', cancelled: '已取消' };
const deliveryModeLabel: Record<ShopDeliveryMode, string> = { manual: '人工交付', paldefender_items: 'PalDefender物品', paldefender_pal_templates: 'PalDefender帕鲁模板' };
const deliveryStateLabel: Record<ShopDeliveryState, string> = { manual: '人工处理', pending: '等待自动交付', processing: '交付状态待核对', failed: '自动交付失败', succeeded: '游戏内已发放' };
const eventTypeLabel: Record<ShopDeliveryEventType, string> = { created: '订单创建', started: '开始交付', failed: '交付失败', uncertain: '结果不确定', succeeded: '游戏内发放成功', reset: '人工重置', completed: '订单结算', cancelled: '订单取消' };
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';

const validatePayload = (mode: ShopDeliveryMode, payload: Record<string, unknown>) => {
  if (mode === 'paldefender_items') {
    const items = Array.isArray(payload.items) ? payload.items : [];
    if (items.length === 0) throw new Error('请至少从物品列表添加一种物品。');
  }
  if (mode === 'paldefender_pal_templates') {
    const templates = Array.isArray(payload.pal_templates) ? payload.pal_templates : [];
    if (templates.length === 0) throw new Error('请至少从帕鲁模板列表添加一个模板。');
  }
};

export const EconomyShop: React.FC = () => {
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<Notice | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingID, setEditingID] = useState('');
  const [productDraft, setProductDraft] = useState<ShopProductInput>(emptyProduct());
  const [orderDraft, setOrderDraft] = useState<ShopOrderCreateInput>(newOrderDraft());
  const [activeTab, setActiveTab] = useState<Tab>('orders');
  const [orderStatus, setOrderStatus] = useState('');
  const [orderPlayerUID, setOrderPlayerUID] = useState('');
  const [orderDeliveryState, setOrderDeliveryState] = useState('');
  const [orderDeliveryMode, setOrderDeliveryMode] = useState('');
  const [eventOrderID, setEventOrderID] = useState('');
  const [eventType, setEventType] = useState('');
  const [attemptResult, setAttemptResult] = useState('');
  const [attemptPlayer, setAttemptPlayer] = useState('');
  const [includeFailed, setIncludeFailed] = useState(false);

  const summaryQuery = useQuery({ queryKey: ['shop', 'summary'], queryFn: shopApi.summary });
  const productsQuery = useQuery({ queryKey: ['shop', 'products'], queryFn: () => shopApi.products(true) });
  const ordersQuery = useQuery({ queryKey: ['shop', 'orders', orderStatus, orderPlayerUID, orderDeliveryState, orderDeliveryMode], queryFn: () => shopApi.orders(orderStatus, orderPlayerUID.trim(), orderDeliveryState, orderDeliveryMode) });
  const eventsQuery = useQuery({ queryKey: ['shop', 'delivery-events', eventOrderID, eventType], queryFn: () => shopApi.deliveryEvents(eventOrderID.trim(), eventType), enabled: activeTab === 'audit' });
  const attemptsQuery = useQuery({ queryKey: ['shop', 'redemption-attempts', attemptResult, attemptPlayer], queryFn: () => shopApi.redemptionAttempts(attemptResult, attemptPlayer.trim()), enabled: activeTab === 'redemptions' });
  const refresh = async () => queryClient.invalidateQueries({ queryKey: ['shop'] });

  const saveProductMutation = useMutation({
    mutationFn: async () => {
      const input = { ...productDraft, name: productDraft.name.trim(), description: productDraft.description?.trim() || '', price: Number(productDraft.price), stock: Number(productDraft.stock), per_player_limit: Number(productDraft.per_player_limit), payload: productDraft.payload || {} };
      if (!input.name) throw new Error('请填写商品名称。');
      if (!Number.isSafeInteger(input.price) || input.price <= 0) throw new Error('商品价格必须是大于0的整数。');
      if (!Number.isSafeInteger(input.stock) || input.stock < -1) throw new Error('库存必须是-1或非负整数。');
      if (!Number.isSafeInteger(input.per_player_limit) || input.per_player_limit < 0) throw new Error('每人限购必须是非负整数。');
      validatePayload(input.delivery_mode, input.payload);
      return editingID ? shopApi.updateProduct(editingID, input) : shopApi.createProduct(input);
    },
    onSuccess: async (product) => { setNotice({ type: 'success', text: editingID ? `商品“${product.name}”已更新。` : `商品“${product.name}”已创建。` }); closeEditor(); await refresh(); },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });
  const archiveProductMutation = useMutation({ mutationFn: (product: ShopProduct) => shopApi.archiveProduct(product.id), onSuccess: async (product) => { setNotice({ type: 'success', text: `商品“${product.name}”已下架。` }); await refresh(); }, onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }) });
  const createOrderMutation = useMutation({
    mutationFn: async () => {
      const input = { ...orderDraft, idempotency_key: orderDraft.idempotency_key.trim(), product_id: orderDraft.product_id.trim(), player_uid: orderDraft.player_uid.trim(), nickname: orderDraft.nickname?.trim() || '', steam_id: orderDraft.steam_id?.trim() || '', quantity: Number(orderDraft.quantity) };
      if (!input.product_id || !input.player_uid || !input.idempotency_key) throw new Error('商品、PlayerUID和幂等键不能为空。');
      return shopApi.createOrder(input);
    },
    onSuccess: async (result) => { setNotice({ type: 'success', text: `订单 ${result.order.id} 已创建；当前可用积分 ${number.format(result.account.balance)}。` }); setOrderDraft(newOrderDraft()); await refresh(); },
    onError: async (error) => { setNotice({ type: 'error', text: `${getErrorMessage(error)}。失败详情已写入“兑换诊断”。` }); await refresh(); },
  });
  const settleOrderMutation = useMutation({
    mutationFn: async ({ order, action }: { order: ShopOrder; action: 'deliver' | 'complete' | 'cancel' | 'reset' }) => {
      if (action === 'deliver') return shopApi.deliverOrder(order.id);
      if (action === 'complete') return shopApi.completeOrder(order.id);
      if (action === 'cancel') return shopApi.cancelOrder(order.id);
      const reset = await shopApi.resetDelivery(order.id);
      return { order: reset, account: { player_uid: reset.player_uid, status: 'active', balance: 0, created_at: '', updated_at: '' }, duplicate: false };
    },
    onSuccess: async (result) => { setNotice({ type: 'success', text: `订单 ${result.order.id} 已更新为 ${statusLabel[result.order.status]}。` }); await refresh(); },
    onError: async (error) => { setNotice({ type: 'error', text: getErrorMessage(error) }); await refresh(); },
  });
  const batchDeliveryMutation = useMutation({ mutationFn: () => shopApi.deliverBatch({ include_failed: includeFailed, limit: 20 }), onSuccess: async (result) => { setNotice({ type: result.failed || result.uncertain ? 'error' : 'success', text: `批量交付：成功 ${result.delivered}，失败 ${result.failed}，待核对 ${result.uncertain}，跳过 ${result.skipped}。` }); await refresh(); }, onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }) });

  const products = productsQuery.data?.items || [];
  const enabledProducts = useMemo(() => products.filter((item) => item.enabled), [products]);
  const orders = ordersQuery.data?.items || [];
  const events = eventsQuery.data?.items || [];
  const attempts = attemptsQuery.data?.items || [];
  const summary = summaryQuery.data || { products: 0, enabled_products: 0, pending_orders: 0, delivered_orders: 0, spent_points: 0, failed_deliveries: 0, processing_deliveries: 0, delivery_events: 0 };

  function closeEditor() { setEditorOpen(false); setEditingID(''); setProductDraft(emptyProduct()); }
  function openCreate() { setEditingID(''); setProductDraft(emptyProduct()); setEditorOpen(true); setNotice(null); }
  function openEdit(product: ShopProduct) { setEditingID(product.id); setProductDraft({ name: product.name, description: product.description || '', price: product.price, stock: product.stock, per_player_limit: product.per_player_limit, enabled: product.enabled, delivery_mode: product.delivery_mode, payload: product.payload || {} }); setEditorOpen(true); setNotice(null); }
  function changeDeliveryMode(mode: ShopDeliveryMode) { setProductDraft((current) => ({ ...current, delivery_mode: mode, payload: {} })); }

  return <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
    {notice && <div className={`rounded-2xl border px-5 py-3 text-xs font-semibold ${notice.type === 'success' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-rose-100 bg-rose-50 text-rose-700'}`}>{notice.text}</div>}
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-8">
      <SummaryCard icon={<ShoppingBag size={18} />} label="商品总数" value={summary.products} /><SummaryCard icon={<BadgeCheck size={18} />} label="上架商品" value={summary.enabled_products} /><SummaryCard icon={<Boxes size={18} />} label="待交付" value={summary.pending_orders} /><SummaryCard icon={<Save size={18} />} label="已交付" value={summary.delivered_orders} /><SummaryCard icon={<CircleDollarSign size={18} />} label="已消费积分" value={summary.spent_points} /><SummaryCard icon={<Ban size={18} />} label="交付失败" value={summary.failed_deliveries} /><SummaryCard icon={<Bot size={18} />} label="待人工核对" value={summary.processing_deliveries} /><SummaryCard icon={<RotateCcw size={18} />} label="审计事件" value={summary.delivery_events} />
    </div>

    <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-sm">
      <div className="flex items-center justify-between gap-3"><div><h2 className="text-base font-bold text-slate-900">商品管理</h2><p className="mt-1 text-xs text-slate-500">自动商品直接从服务器物品目录或帕鲁模板目录选择，系统生成交付Payload。</p></div><div className="flex gap-2"><button type="button" onClick={() => refresh()} className="rounded-xl border border-slate-200 px-4 py-2 text-xs font-semibold text-slate-600"><RefreshCw size={14} className="mr-2 inline" />刷新</button><button type="button" onClick={openCreate} className="rounded-xl bg-sky-500 px-4 py-2 text-xs font-semibold text-white"><Plus size={14} className="mr-2 inline" />新增商品</button></div></div>
      {productsQuery.isLoading ? <Loading text="正在加载商品…" /> : products.length === 0 ? <Empty text="暂无商品。" /> : <div className="mt-5 grid gap-4 lg:grid-cols-2 xl:grid-cols-3">{products.map((product) => <div key={product.id} className={`rounded-2xl border p-4 ${product.enabled ? 'border-slate-100' : 'border-slate-100 bg-slate-50 opacity-70'}`}><div className="flex items-start justify-between"><div><strong className="text-sm text-slate-800">{product.name}</strong><div className="mt-1 text-[10px] text-slate-400">兑换码 <span className="font-mono font-bold text-sky-600">{productCode(product.id)}</span></div></div><span className="rounded-full bg-slate-100 px-2 py-1 text-[10px] font-bold">{product.enabled ? '上架' : '下架'}</span></div><p className="mt-3 min-h-8 text-xs text-slate-500">{product.description || '无说明'}</p><div className="mt-4 grid grid-cols-2 gap-2"><Metric label="价格" value={`${product.price}积分`} /><Metric label="库存" value={product.stock < 0 ? '不限' : String(product.stock)} /><Metric label="限购" value={product.per_player_limit ? String(product.per_player_limit) : '不限'} /><Metric label="交付" value={deliveryModeLabel[product.delivery_mode]} /></div><div className="mt-4 flex justify-end gap-2"><button type="button" onClick={() => openEdit(product)} className="rounded-lg bg-slate-100 px-3 py-2 text-xs font-semibold"><Pencil size={12} className="mr-1 inline" />编辑</button>{product.enabled && <button type="button" onClick={() => window.confirm(`下架商品“${product.name}”？`) && archiveProductMutation.mutate(product)} className="rounded-lg bg-rose-50 px-3 py-2 text-xs font-semibold text-rose-700"><Archive size={12} className="mr-1 inline" />下架</button>}</div></div>)}</div>}
    </section>

    <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-sm">
      <h2 className="text-base font-bold text-slate-900">创建兑换订单</h2><p className="mt-1 text-xs text-slate-500">可同时填写 PlayerUID 与 SteamID；系统会显示实际命中的积分账户。</p>
      <form onSubmit={(event) => { event.preventDefault(); createOrderMutation.mutate(); }} className="mt-5 grid gap-3 lg:grid-cols-6"><select value={orderDraft.product_id} onChange={(event) => setOrderDraft((current) => ({ ...current, product_id: event.target.value }))} className="pp-input lg:col-span-2"><option value="">选择上架商品</option>{enabledProducts.map((product) => <option key={product.id} value={product.id}>[{productCode(product.id)}] {product.name} · {product.price}积分</option>)}</select><input value={orderDraft.player_uid} onChange={(event) => setOrderDraft((current) => ({ ...current, player_uid: event.target.value }))} placeholder="PlayerUID" className="pp-input lg:col-span-2" /><input type="number" min={1} value={orderDraft.quantity} onChange={(event) => setOrderDraft((current) => ({ ...current, quantity: Number(event.target.value) }))} className="pp-input" /><button type="submit" disabled={createOrderMutation.isPending} className="rounded-xl bg-sky-500 px-4 py-2 text-xs font-semibold text-white disabled:opacity-50">创建订单</button><input value={orderDraft.nickname || ''} onChange={(event) => setOrderDraft((current) => ({ ...current, nickname: event.target.value }))} placeholder="昵称（可选）" className="pp-input lg:col-span-2" /><input value={orderDraft.steam_id || ''} onChange={(event) => setOrderDraft((current) => ({ ...current, steam_id: event.target.value }))} placeholder="SteamID（建议填写）" className="pp-input lg:col-span-2" /><input value={orderDraft.idempotency_key} onChange={(event) => setOrderDraft((current) => ({ ...current, idempotency_key: event.target.value }))} placeholder="幂等键" className="pp-input lg:col-span-2" /></form>
    </section>

    <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-sm">
      <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between"><div className="flex rounded-xl bg-slate-100 p-1 text-xs font-semibold">{(['orders','audit','redemptions'] as Tab[]).map((tab) => <button key={tab} type="button" onClick={() => setActiveTab(tab)} className={`rounded-lg px-4 py-2 ${activeTab === tab ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500'}`}>{tab === 'orders' ? '订单' : tab === 'audit' ? '交付审计' : '兑换诊断'}</button>)}</div>{activeTab === 'orders' && <div className="flex items-center gap-2"><label className="text-xs"><input type="checkbox" checked={includeFailed} onChange={(event) => setIncludeFailed(event.target.checked)} /> 包含失败订单</label><button type="button" onClick={() => batchDeliveryMutation.mutate()} className="rounded-xl bg-sky-50 px-4 py-2 text-xs font-semibold text-sky-700">批量自动交付</button></div>}</div>
      {activeTab === 'orders' ? <OrdersView orders={orders} loading={ordersQuery.isLoading} filters={{ orderStatus, setOrderStatus, orderPlayerUID, setOrderPlayerUID, orderDeliveryState, setOrderDeliveryState, orderDeliveryMode, setOrderDeliveryMode }} mutation={settleOrderMutation} /> : activeTab === 'audit' ? <AuditView events={events} loading={eventsQuery.isLoading} eventOrderID={eventOrderID} setEventOrderID={setEventOrderID} eventType={eventType} setEventType={setEventType} /> : <RedemptionView attempts={attempts} loading={attemptsQuery.isLoading} result={attemptResult} setResult={setAttemptResult} player={attemptPlayer} setPlayer={setAttemptPlayer} />}
    </section>

    {editorOpen && <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/30 p-4 backdrop-blur-sm"><form onSubmit={(event) => { event.preventDefault(); saveProductMutation.mutate(); }} className="max-h-[94vh] w-full max-w-5xl overflow-y-auto rounded-3xl bg-white p-6 shadow-2xl"><div className="flex items-center justify-between"><div><h2 className="text-lg font-bold">{editingID ? '编辑商品' : '新增商品'}</h2><p className="mt-1 text-xs text-slate-500">库存 -1 表示不限；自动交付内容必须从目录选择。</p></div><button type="button" onClick={closeEditor}><X size={18} /></button></div><div className="mt-6 grid gap-4 sm:grid-cols-2"><Field label="商品名称"><input value={productDraft.name} onChange={(event) => setProductDraft((current) => ({ ...current, name: event.target.value }))} className="pp-input" /></Field><Field label="价格（积分）"><input type="number" min={1} value={productDraft.price} onChange={(event) => setProductDraft((current) => ({ ...current, price: Number(event.target.value) }))} className="pp-input" /></Field><Field label="库存"><input type="number" min={-1} value={productDraft.stock} onChange={(event) => setProductDraft((current) => ({ ...current, stock: Number(event.target.value) }))} className="pp-input" /></Field><Field label="每人限购"><input type="number" min={0} value={productDraft.per_player_limit} onChange={(event) => setProductDraft((current) => ({ ...current, per_player_limit: Number(event.target.value) }))} className="pp-input" /></Field><Field label="交付方式"><select value={productDraft.delivery_mode} onChange={(event) => changeDeliveryMode(event.target.value as ShopDeliveryMode)} className="pp-input"><option value="manual">人工交付</option><option value="paldefender_items">PalDefender物品</option><option value="paldefender_pal_templates">PalDefender帕鲁模板</option></select></Field><label className="flex items-center gap-2 self-end rounded-xl border border-slate-200 p-3 text-xs font-semibold"><input type="checkbox" checked={productDraft.enabled} onChange={(event) => setProductDraft((current) => ({ ...current, enabled: event.target.checked }))} />立即上架</label><div className="sm:col-span-2"><Field label="商品说明"><textarea rows={3} value={productDraft.description || ''} onChange={(event) => setProductDraft((current) => ({ ...current, description: event.target.value }))} className="pp-input resize-y" /></Field></div><div className="sm:col-span-2">{productDraft.delivery_mode === 'manual' ? <Field label="人工交付备注 JSON"><textarea rows={6} value={JSON.stringify(productDraft.payload || {}, null, 2)} onChange={(event) => { try { setProductDraft((current) => ({ ...current, payload: JSON.parse(event.target.value) as Record<string, unknown> })); } catch { /* keep last valid payload */ } }} className="pp-input resize-y font-mono text-[11px]" /></Field> : <ShopPayloadCatalogEditor mode={productDraft.delivery_mode} payload={productDraft.payload || {}} onChange={(payload) => setProductDraft((current) => ({ ...current, payload }))} />}</div><div className="sm:col-span-2"><Field label="最终 Payload 预览"><textarea readOnly rows={5} value={JSON.stringify(productDraft.payload || {}, null, 2)} className="pp-input resize-y bg-slate-50 font-mono text-[11px]" /></Field></div></div><div className="mt-6 flex justify-end gap-2"><button type="button" onClick={closeEditor} className="rounded-xl border px-4 py-2 text-xs">取消</button><button type="submit" disabled={saveProductMutation.isPending} className="rounded-xl bg-sky-500 px-4 py-2 text-xs font-semibold text-white">保存商品</button></div></form></div>}
  </div>;
};

const OrdersView: React.FC<{ orders: ShopOrder[]; loading: boolean; filters: any; mutation: any }> = ({ orders, loading, filters, mutation }) => <div className="mt-5"><div className="grid gap-2 md:grid-cols-2 xl:grid-cols-5"><select value={filters.orderStatus} onChange={(event) => filters.setOrderStatus(event.target.value)} className="pp-input"><option value="">全部订单状态</option><option value="pending">待交付</option><option value="delivered">已交付</option><option value="cancelled">已取消</option></select><select value={filters.orderDeliveryState} onChange={(event) => filters.setOrderDeliveryState(event.target.value)} className="pp-input"><option value="">全部交付状态</option><option value="pending">等待自动交付</option><option value="failed">自动交付失败</option><option value="processing">待人工核对</option><option value="succeeded">游戏内已发放</option><option value="manual">人工处理</option></select><select value={filters.orderDeliveryMode} onChange={(event) => filters.setOrderDeliveryMode(event.target.value)} className="pp-input"><option value="">全部交付方式</option><option value="automatic">全部自动交付</option><option value="manual">人工交付</option><option value="paldefender_items">PalDefender物品</option><option value="paldefender_pal_templates">PalDefender帕鲁模板</option></select><div className="relative md:col-span-2"><Search size={14} className="absolute left-3 top-3 text-slate-400" /><input value={filters.orderPlayerUID} onChange={(event) => filters.setOrderPlayerUID(event.target.value)} placeholder="按PlayerUID筛选" className="pp-input w-full pl-9" /></div></div>{loading ? <Loading text="正在加载订单…" /> : orders.length === 0 ? <Empty text="暂无匹配订单。" /> : <div className="mt-4 overflow-x-auto"><table className="w-full min-w-[1000px] text-left text-xs"><thead><tr className="bg-slate-50 text-slate-400"><th className="p-3">订单</th><th className="p-3">玩家</th><th className="p-3">商品</th><th className="p-3">积分</th><th className="p-3">状态</th><th className="p-3">操作</th></tr></thead><tbody>{orders.map((order) => <tr key={order.id} className="border-b"><td className="p-3 font-mono text-[10px]">{order.id}</td><td className="p-3">{order.nickname || '-'}<div className="font-mono text-[10px] text-slate-400">{order.player_uid}</div></td><td className="p-3">{order.product_name} ×{order.quantity}</td><td className="p-3 font-bold">{order.total_points}</td><td className="p-3">{statusLabel[order.status]} / {deliveryStateLabel[order.delivery_state]}{order.failure && <div className="mt-1 text-[10px] text-rose-600">{order.failure}</div>}</td><td className="p-3">{order.status === 'pending' && <div className="flex gap-1"><button type="button" onClick={() => mutation.mutate({ order, action: 'deliver' })} className="rounded bg-sky-50 px-2 py-1 text-sky-700">交付</button><button type="button" onClick={() => mutation.mutate({ order, action: 'complete' })} className="rounded bg-emerald-50 px-2 py-1 text-emerald-700">确认</button><button type="button" onClick={() => mutation.mutate({ order, action: 'cancel' })} className="rounded bg-rose-50 px-2 py-1 text-rose-700">取消</button></div>}</td></tr>)}</tbody></table></div>}</div>;
const AuditView: React.FC<any> = ({ events, loading, eventOrderID, setEventOrderID, eventType, setEventType }) => <div className="mt-5"><div className="grid gap-2 md:grid-cols-3"><input value={eventOrderID} onChange={(e) => setEventOrderID(e.target.value)} placeholder="按订单ID筛选" className="pp-input md:col-span-2" /><select value={eventType} onChange={(e) => setEventType(e.target.value)} className="pp-input"><option value="">全部事件</option>{Object.entries(eventTypeLabel).map(([value,label]) => <option key={value} value={value}>{label}</option>)}</select></div>{loading ? <Loading text="正在加载交付审计…" /> : events.length === 0 ? <Empty text="暂无交付事件。" /> : <div className="mt-4 space-y-2">{events.map((event: any) => <div key={event.id} className="rounded-xl border p-3 text-xs"><strong>{eventTypeLabel[event.event_type as ShopDeliveryEventType]}</strong> · {event.order_id}<div className="mt-1 text-slate-500">{event.message || '-'} · {formatTime(event.created_at)}</div></div>)}</div>}</div>;
const RedemptionView: React.FC<{ attempts: ShopRedemptionAttempt[]; loading: boolean; result: string; setResult: (v:string)=>void; player:string; setPlayer:(v:string)=>void }> = ({ attempts, loading, result, setResult, player, setPlayer }) => <div className="mt-5"><div className="grid gap-2 md:grid-cols-3"><select value={result} onChange={(e) => setResult(e.target.value)} className="pp-input"><option value="">全部结果</option><option value="rejected">兑换失败</option><option value="order_created">订单已创建</option><option value="duplicate">重复请求</option></select><div className="relative md:col-span-2"><Search size={14} className="absolute left-3 top-3 text-slate-400" /><input value={player} onChange={(e) => setPlayer(e.target.value)} placeholder="按请求PlayerUID、实际账户或SteamID筛选" className="pp-input w-full pl-9" /></div></div>{loading ? <Loading text="正在加载兑换诊断…" /> : attempts.length === 0 ? <Empty text="暂无兑换尝试。" /> : <div className="mt-4 overflow-x-auto"><table className="w-full min-w-[1200px] text-left text-xs"><thead><tr className="bg-slate-50 text-slate-400"><th className="p-3">诊断编号</th><th className="p-3">身份解析</th><th className="p-3">商品</th><th className="p-3">积分核对</th><th className="p-3">结果</th><th className="p-3">时间</th></tr></thead><tbody>{attempts.map((a) => <tr key={a.id} className="border-b align-top"><td className="p-3 font-mono text-[10px]">{a.id}<div className="mt-1 text-slate-400">{a.source}</div></td><td className="p-3"><div>请求：<span className="font-mono text-[10px]">{a.requested_player_uid || '-'}</span></div><div>Steam：<span className="font-mono text-[10px]">{a.requested_steam_id || '-'}</span></div><div>账户：<span className="font-mono text-[10px]">{a.resolved_player_uid || '-'}</span></div><div className="mt-1 font-semibold text-sky-700">{a.account_match || '-'}</div>{a.account_warning && <div className="mt-1 max-w-80 text-[10px] text-amber-700">{a.account_warning}</div>}</td><td className="p-3">{a.product_name || a.product_id || '-'} ×{a.quantity}<div className="font-mono text-[10px] text-slate-400">{a.product_id}</div></td><td className="p-3"><div>单价 {a.unit_price}</div><div className="font-bold">需要 {a.required_points}</div><div className={a.available_points < a.required_points ? 'font-bold text-rose-600' : 'font-bold text-emerald-600'}>可用 {a.available_points}</div><div className="text-amber-700">预留 {a.reserved_points}</div></td><td className="p-3"><span className={`rounded-full px-2 py-1 text-[10px] font-bold ${a.result === 'rejected' ? 'bg-rose-50 text-rose-700' : 'bg-emerald-50 text-emerald-700'}`}>{a.result}</span><div className="mt-2 font-mono text-[10px] text-rose-600">{a.failure_code || '-'}</div>{a.failure && <div className="mt-1 max-w-96 text-[10px] text-slate-500">{a.failure}</div>}</td><td className="p-3 text-slate-400">{formatTime(a.created_at)}</td></tr>)}</tbody></table></div>}</div>;
const SummaryCard: React.FC<{ icon: React.ReactNode; label: string; value: number }> = ({ icon, label, value }) => <div className="rounded-2xl border bg-white p-4 shadow-sm"><div className="flex items-center gap-2 text-slate-400">{icon}<span className="text-xs">{label}</span></div><div className="mt-3 text-2xl font-bold">{number.format(value)}</div></div>;
const Field: React.FC<React.PropsWithChildren<{ label: string }>> = ({ label, children }) => <label className="flex flex-col gap-2 text-xs font-semibold text-slate-600"><span>{label}</span>{children}</label>;
const Metric: React.FC<{ label: string; value: string }> = ({ label, value }) => <div className="rounded-xl bg-slate-50 px-3 py-2 text-xs"><div className="text-[10px] text-slate-400">{label}</div><strong>{value}</strong></div>;
const Loading: React.FC<{ text: string }> = ({ text }) => <div className="py-10 text-center text-xs text-slate-400"><LoaderCircle size={14} className="mr-2 inline animate-spin" />{text}</div>;
const Empty: React.FC<{ text: string }> = ({ text }) => <div className="mt-4 rounded-2xl border border-dashed py-10 text-center text-xs text-slate-400">{text}</div>;
