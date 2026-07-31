import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Archive,
  BadgeCheck,
  Ban,
  Boxes,
  CircleDollarSign,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  Search,
  ShoppingBag,
  X,
} from 'lucide-react';
import { getErrorMessage } from '../api/client';
import {
  shopApi,
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
  const [orderDraft, setOrderDraft] = useState<ShopOrderCreateInput>(newOrderDraft());
  const [orderStatus, setOrderStatus] = useState('');
  const [orderPlayerUID, setOrderPlayerUID] = useState('');

  const summaryQuery = useQuery({ queryKey: ['shop', 'summary'], queryFn: shopApi.summary });
  const productsQuery = useQuery({ queryKey: ['shop', 'products'], queryFn: () => shopApi.products(true) });
  const ordersQuery = useQuery({
    queryKey: ['shop', 'orders', orderStatus, orderPlayerUID],
    queryFn: () => shopApi.orders(orderStatus, orderPlayerUID.trim()),
  });

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['shop'] });
  };

  const saveProductMutation = useMutation({
    mutationFn: async () => {
      const input: ShopProductInput = {
        ...productDraft,
        name: productDraft.name.trim(),
        description: productDraft.description?.trim() || '',
        price: Number(productDraft.price),
        stock: Number(productDraft.stock),
        per_player_limit: Number(productDraft.per_player_limit),
        delivery_mode: 'manual',
        payload: parsePayload(payloadText),
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
      setNotice({
        type: 'success',
        text: result.duplicate
          ? `检测到重复请求，已返回原订单 ${result.order.id}。`
          : `兑换订单已创建，已预扣 ${number.format(result.order.total_points)} 积分，当前余额 ${number.format(result.account.balance)}。`,
      });
      setOrderDraft(newOrderDraft());
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const settleOrderMutation = useMutation({
    mutationFn: ({ order, action }: { order: ShopOrder; action: 'complete' | 'cancel' }) =>
      action === 'complete' ? shopApi.completeOrder(order.id) : shopApi.cancelOrder(order.id),
    onSuccess: async (result) => {
      setNotice({
        type: 'success',
        text: result.order.status === 'delivered'
          ? `订单 ${result.order.id} 已确认交付。`
          : `订单 ${result.order.id} 已取消，积分已退回，当前余额 ${number.format(result.account.balance)}。`,
      });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const products = productsQuery.data?.items || [];
  const enabledProducts = useMemo(() => products.filter((item) => item.enabled), [products]);
  const orders = ordersQuery.data?.items || [];
  const summary = summaryQuery.data || { products: 0, enabled_products: 0, pending_orders: 0, delivered_orders: 0, spent_points: 0 };

  function closeEditor() {
    setEditorOpen(false);
    setEditingID('');
    setProductDraft(emptyProduct());
    setPayloadText('{}');
  }

  const openCreate = () => {
    setEditingID('');
    setProductDraft(emptyProduct());
    setPayloadText('{}');
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
      delivery_mode: 'manual',
      payload: product.payload || {},
    });
    setPayloadText(JSON.stringify(product.payload || {}, null, 2));
    setEditorOpen(true);
    setNotice(null);
  };

  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      {notice && (
        <div className={`rounded-2xl border px-5 py-3 text-xs font-semibold ${notice.type === 'success' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-rose-100 bg-rose-50 text-rose-700'}`}>
          {notice.text}
        </div>
      )}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        <SummaryCard icon={<ShoppingBag size={18} />} label="商品总数" value={summary.products} />
        <SummaryCard icon={<BadgeCheck size={18} />} label="上架商品" value={summary.enabled_products} />
        <SummaryCard icon={<Boxes size={18} />} label="待交付订单" value={summary.pending_orders} />
        <SummaryCard icon={<Save size={18} />} label="已交付订单" value={summary.delivered_orders} />
        <SummaryCard icon={<CircleDollarSign size={18} />} label="已消费积分" value={summary.spent_points} />
      </div>

      <section className="rounded-3xl border border-slate-100 bg-white p-5 shadow-sm sm:p-6">
        <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-base font-bold text-slate-900">商品管理</h2>
            <p className="mt-1 text-xs text-slate-500">当前版本采用人工交付。创建订单时预扣积分，确认交付后结算；取消订单自动退款并恢复库存。</p>
          </div>
          <div className="flex gap-2">
            <button type="button" onClick={() => void refresh()} className="flex items-center gap-2 rounded-xl border border-slate-200 px-4 py-2 text-xs font-semibold text-slate-600 hover:bg-slate-50">
              <RefreshCw size={14} />刷新
            </button>
            <button type="button" onClick={openCreate} className="flex items-center gap-2 rounded-xl bg-sky-500 px-4 py-2 text-xs font-semibold text-white hover:bg-sky-600">
              <Plus size={14} />新增商品
            </button>
          </div>
        </div>

        {productsQuery.isLoading ? (
          <Loading text="正在加载商品..." />
        ) : products.length === 0 ? (
          <Empty text="暂无商品。先创建一个人工交付商品。" />
        ) : (
          <div className="grid gap-3 lg:grid-cols-2">
            {products.map((product) => (
              <article key={product.id} className="rounded-2xl border border-slate-100 p-4">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="flex items-center gap-2">
                      <h3 className="font-bold text-slate-900">{product.name}</h3>
                      <span className={`rounded-full px-2 py-0.5 text-[10px] font-bold ${product.enabled ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{product.enabled ? '上架' : '下架'}</span>
                    </div>
                    <p className="mt-1 text-xs text-slate-500">{product.description || '无说明'}</p>
                  </div>
                  <div className="flex gap-1">
                    <button type="button" onClick={() => openEdit(product)} className="rounded-lg p-2 text-slate-500 hover:bg-slate-100" title="编辑"><Pencil size={14} /></button>
                    {product.enabled && <button type="button" onClick={() => { if (window.confirm(`下架商品“${product.name}”？历史订单会保留。`)) archiveProductMutation.mutate(product); }} className="rounded-lg p-2 text-rose-500 hover:bg-rose-50" title="下架"><Archive size={14} /></button>}
                  </div>
                </div>
                <div className="mt-4 grid grid-cols-3 gap-2 text-xs">
                  <Metric label="价格" value={`${number.format(product.price)}积分`} />
                  <Metric label="库存" value={product.stock < 0 ? '不限' : number.format(product.stock)} />
                  <Metric label="每人限购" value={product.per_player_limit === 0 ? '不限' : number.format(product.per_player_limit)} />
                </div>
              </article>
            ))}
          </div>
        )}
      </section>

      <section className="grid gap-6 xl:grid-cols-[360px_minmax(0,1fr)]">
        <form
          onSubmit={(event) => { event.preventDefault(); createOrderMutation.mutate(); }}
          className="rounded-3xl border border-slate-100 bg-white p-5 shadow-sm sm:p-6"
        >
          <h2 className="text-base font-bold text-slate-900">代玩家兑换</h2>
          <p className="mt-1 text-xs text-slate-500">面板管理员创建人工交付订单。相同PlayerUID与幂等键不会重复扣款。</p>
          <div className="mt-5 flex flex-col gap-4">
            <Field label="商品">
              <select value={orderDraft.product_id} onChange={(event) => setOrderDraft((current) => ({ ...current, product_id: event.target.value }))} className="pp-input w-full">
                <option value="">请选择商品</option>
                {enabledProducts.map((product) => <option key={product.id} value={product.id}>{product.name} · {product.price}积分</option>)}
              </select>
            </Field>
            <Field label="PlayerUID">
              <input value={orderDraft.player_uid} onChange={(event) => setOrderDraft((current) => ({ ...current, player_uid: event.target.value }))} className="pp-input w-full" placeholder="玩家PlayerUID" />
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="昵称"><input value={orderDraft.nickname || ''} onChange={(event) => setOrderDraft((current) => ({ ...current, nickname: event.target.value }))} className="pp-input w-full" /></Field>
              <Field label="数量"><input type="number" min={1} max={1000} value={orderDraft.quantity} onChange={(event) => setOrderDraft((current) => ({ ...current, quantity: Number(event.target.value) }))} className="pp-input w-full" /></Field>
            </div>
            <Field label="SteamID（可选）"><input value={orderDraft.steam_id || ''} onChange={(event) => setOrderDraft((current) => ({ ...current, steam_id: event.target.value }))} className="pp-input w-full" /></Field>
            <Field label="幂等键">
              <input value={orderDraft.idempotency_key} onChange={(event) => setOrderDraft((current) => ({ ...current, idempotency_key: event.target.value }))} className="pp-input w-full font-mono" />
            </Field>
            <button disabled={createOrderMutation.isPending} type="submit" className="flex items-center justify-center gap-2 rounded-xl bg-sky-500 px-4 py-3 text-xs font-bold text-white hover:bg-sky-600 disabled:opacity-50">
              {createOrderMutation.isPending ? <LoaderCircle size={14} className="animate-spin" /> : <ShoppingBag size={14} />}
              创建兑换订单
            </button>
          </div>
        </form>

        <div className="rounded-3xl border border-slate-100 bg-white p-5 shadow-sm sm:p-6">
          <div className="mb-5 flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <h2 className="text-base font-bold text-slate-900">兑换订单</h2>
              <p className="mt-1 text-xs text-slate-500">待交付订单仍处于积分预扣状态。交付实物后再点击确认交付。</p>
            </div>
            <div className="flex flex-col gap-2 sm:flex-row">
              <select value={orderStatus} onChange={(event) => setOrderStatus(event.target.value)} className="pp-input sm:w-32">
                <option value="">全部状态</option>
                <option value="pending">待交付</option>
                <option value="delivered">已交付</option>
                <option value="cancelled">已取消</option>
              </select>
              <label className="relative">
                <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
                <input value={orderPlayerUID} onChange={(event) => setOrderPlayerUID(event.target.value)} className="pp-input w-full pl-9" placeholder="筛选PlayerUID" />
              </label>
            </div>
          </div>

          {ordersQuery.isLoading ? <Loading text="正在加载订单..." /> : orders.length === 0 ? <Empty text="暂无匹配订单。" /> : (
            <div className="overflow-x-auto">
              <table className="min-w-full text-left text-xs">
                <thead><tr className="border-b border-slate-100 text-slate-400"><th className="px-3 py-3">订单</th><th className="px-3 py-3">玩家</th><th className="px-3 py-3">商品</th><th className="px-3 py-3">积分</th><th className="px-3 py-3">状态</th><th className="px-3 py-3">时间</th><th className="px-3 py-3 text-center">操作</th></tr></thead>
                <tbody>
                  {orders.map((order) => (
                    <tr key={order.id} className="border-b border-slate-50 align-top">
                      <td className="px-3 py-3 font-mono text-[10px] text-slate-500">{order.id}</td>
                      <td className="px-3 py-3"><div className="font-semibold text-slate-700">{order.nickname || '-'}</div><div className="mt-1 font-mono text-[10px] text-slate-400">{order.player_uid}</div></td>
                      <td className="px-3 py-3"><div className="font-semibold text-slate-700">{order.product_name}</div><div className="mt-1 text-slate-400">× {order.quantity}</div></td>
                      <td className="px-3 py-3 font-bold text-slate-700">{number.format(order.total_points)}</td>
                      <td className="px-3 py-3"><span className={`rounded-full px-2 py-1 text-[10px] font-bold ${order.status === 'pending' ? 'bg-amber-50 text-amber-700' : order.status === 'delivered' ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{statusLabel[order.status]}</span></td>
                      <td className="px-3 py-3 text-slate-400">{formatTime(order.created_at)}</td>
                      <td className="px-3 py-3">
                        {order.status === 'pending' ? (
                          <div className="flex justify-center gap-1">
                            <button type="button" onClick={() => { if (window.confirm(`确认订单 ${order.id} 已在游戏内完成交付？`)) settleOrderMutation.mutate({ order, action: 'complete' }); }} className="flex items-center gap-1 rounded-lg bg-emerald-50 px-2 py-1.5 font-semibold text-emerald-700 hover:bg-emerald-100"><BadgeCheck size={12} />交付</button>
                            <button type="button" onClick={() => { if (window.confirm(`取消订单 ${order.id} 并退还积分？`)) settleOrderMutation.mutate({ order, action: 'cancel' }); }} className="flex items-center gap-1 rounded-lg bg-rose-50 px-2 py-1.5 font-semibold text-rose-700 hover:bg-rose-100"><Ban size={12} />取消</button>
                          </div>
                        ) : <div className="text-center text-slate-300">-</div>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </section>

      {editorOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/30 p-4 backdrop-blur-sm">
          <form onSubmit={(event) => { event.preventDefault(); saveProductMutation.mutate(); }} className="max-h-[92vh] w-full max-w-2xl overflow-y-auto rounded-3xl bg-white p-6 shadow-2xl">
            <div className="flex items-center justify-between">
              <div><h2 className="text-lg font-bold text-slate-900">{editingID ? '编辑商品' : '新增商品'}</h2><p className="mt-1 text-xs text-slate-500">库存-1表示不限；每人限购0表示不限。</p></div>
              <button type="button" onClick={closeEditor} className="rounded-xl p-2 text-slate-500 hover:bg-slate-100"><X size={18} /></button>
            </div>
            <div className="mt-6 grid gap-4 sm:grid-cols-2">
              <Field label="商品名称"><input value={productDraft.name} onChange={(event) => setProductDraft((current) => ({ ...current, name: event.target.value }))} className="pp-input w-full" /></Field>
              <Field label="价格（积分）"><input type="number" min={1} value={productDraft.price} onChange={(event) => setProductDraft((current) => ({ ...current, price: Number(event.target.value) }))} className="pp-input w-full" /></Field>
              <Field label="库存"><input type="number" min={-1} value={productDraft.stock} onChange={(event) => setProductDraft((current) => ({ ...current, stock: Number(event.target.value) }))} className="pp-input w-full" /></Field>
              <Field label="每人限购"><input type="number" min={0} value={productDraft.per_player_limit} onChange={(event) => setProductDraft((current) => ({ ...current, per_player_limit: Number(event.target.value) }))} className="pp-input w-full" /></Field>
              <Field label="交付方式"><input value="人工交付" disabled className="pp-input w-full bg-slate-50 text-slate-400" /></Field>
              <label className="flex items-center gap-3 self-end rounded-xl border border-slate-200 px-4 py-3 text-xs font-semibold text-slate-600"><input type="checkbox" checked={productDraft.enabled} onChange={(event) => setProductDraft((current) => ({ ...current, enabled: event.target.checked }))} />立即上架</label>
              <div className="sm:col-span-2"><Field label="商品说明"><textarea value={productDraft.description || ''} onChange={(event) => setProductDraft((current) => ({ ...current, description: event.target.value }))} rows={3} className="pp-input w-full resize-y" /></Field></div>
              <div className="sm:col-span-2"><Field label="Payload JSON（为后续自动交付保留）"><textarea value={payloadText} onChange={(event) => setPayloadText(event.target.value)} rows={6} className="pp-input w-full resize-y font-mono text-[11px]" /></Field></div>
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button type="button" onClick={closeEditor} className="rounded-xl border border-slate-200 px-4 py-2 text-xs font-semibold text-slate-600">取消</button>
              <button disabled={saveProductMutation.isPending} type="submit" className="flex items-center gap-2 rounded-xl bg-sky-500 px-4 py-2 text-xs font-semibold text-white disabled:opacity-50">
                {saveProductMutation.isPending ? <LoaderCircle size={14} className="animate-spin" /> : <Save size={14} />}保存商品
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
};

const SummaryCard: React.FC<{ icon: React.ReactNode; label: string; value: number }> = ({ icon, label, value }) => (
  <div className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
    <div className="flex items-center gap-2 text-slate-400">{icon}<span className="text-xs font-semibold">{label}</span></div>
    <div className="mt-3 text-2xl font-bold text-slate-900">{number.format(value)}</div>
  </div>
);

const Field: React.FC<{ label: string; children: React.ReactNode }> = ({ label, children }) => (
  <label className="flex flex-col gap-2 text-xs font-semibold text-slate-600"><span>{label}</span>{children}</label>
);

const Metric: React.FC<{ label: string; value: string }> = ({ label, value }) => (
  <div className="rounded-xl bg-slate-50 px-3 py-2"><div className="text-[10px] text-slate-400">{label}</div><div className="mt-1 font-bold text-slate-700">{value}</div></div>
);

const Loading: React.FC<{ text: string }> = ({ text }) => <div className="py-10 text-center text-xs font-semibold text-slate-400"><LoaderCircle size={14} className="mr-2 inline animate-spin" />{text}</div>;
const Empty: React.FC<{ text: string }> = ({ text }) => <div className="rounded-2xl border border-dashed border-slate-200 py-10 text-center text-xs font-semibold text-slate-400">{text}</div>;
