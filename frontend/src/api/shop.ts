import { apiClient, handleRequest } from './client';

export type ShopOrderStatus = 'pending' | 'delivered' | 'cancelled';
export type ShopDeliveryMode = 'manual' | 'paldefender_items' | 'paldefender_pal_templates';
export type ShopDeliveryState = 'manual' | 'pending' | 'processing' | 'failed' | 'succeeded';
export type ShopDeliveryEventType = 'created' | 'started' | 'failed' | 'uncertain' | 'succeeded' | 'reset' | 'completed' | 'cancelled';

export interface ShopProductInput { name: string; description?: string; price: number; stock: number; per_player_limit: number; enabled: boolean; delivery_mode: ShopDeliveryMode; payload?: Record<string, unknown> }
export interface ShopProduct extends ShopProductInput { id: string; created_at: string; updated_at: string }
export interface ShopOrder { id: string; idempotency_key: string; product_id: string; product_name: string; player_uid: string; nickname?: string; steam_id?: string; quantity: number; unit_price: number; total_points: number; status: ShopOrderStatus; reservation_id: string; delivery_mode: ShopDeliveryMode; delivery_state: ShopDeliveryState; delivery_attempts: number; last_delivery_at?: string; payload?: Record<string, unknown>; delivery_receipt?: Record<string, unknown>; failure?: string; created_at: string; updated_at: string; delivered_at?: string; cancelled_at?: string }
export interface ShopSummary { products: number; enabled_products: number; pending_orders: number; delivered_orders: number; spent_points: number; failed_deliveries: number; processing_deliveries: number; delivery_events: number }
export interface ShopDeliveryEvent { id: number; order_id: string; event_type: ShopDeliveryEventType; delivery_state: ShopDeliveryState | ''; attempt: number; actor?: string; message?: string; details?: Record<string, unknown>; created_at: string }
export interface ShopBatchDeliveryInput { order_ids?: string[]; include_failed: boolean; limit: number }
export interface ShopBatchDeliveryItem { order_id: string; result: 'delivered' | 'failed' | 'uncertain' | 'skipped'; status: ShopOrderStatus; delivery_state: ShopDeliveryState; error?: string }
export interface ShopBatchDeliveryResult { selected: number; delivered: number; failed: number; uncertain: number; skipped: number; items: ShopBatchDeliveryItem[] }
export interface ShopOrderCreateInput { idempotency_key: string; product_id: string; player_uid: string; nickname?: string; steam_id?: string; quantity: number }
export interface ShopOrderResult { order: ShopOrder; account: { player_uid: string; nickname?: string; steam_id?: string; status: string; balance: number; created_at: string; updated_at: string }; duplicate: boolean }

export interface ShopRedemptionAttempt {
  id: string; source: string; event_id?: string; actor?: string;
  requested_player_uid?: string; requested_steam_id?: string; resolved_player_uid?: string;
  account_match?: string; account_warning?: string; product_id?: string; product_name?: string;
  quantity: number; unit_price: number; required_points: number; available_points: number;
  reserved_points: number; result: string; failure_code?: string; failure?: string; order_id?: string; created_at: string;
}

interface ProductListResult { items: ShopProduct[]; count: number }
interface OrderListResult { items: ShopOrder[]; count: number }
interface DeliveryEventListResult { items: ShopDeliveryEvent[]; count: number }
interface RedemptionAttemptListResult { items: ShopRedemptionAttempt[]; count: number }

const emptyProduct: ShopProduct = { id: '', name: '', description: '', price: 1, stock: -1, per_player_limit: 0, enabled: true, delivery_mode: 'manual', payload: {}, created_at: '', updated_at: '' };
const emptyOrder: ShopOrder = { id: '', idempotency_key: '', product_id: '', product_name: '', player_uid: '', quantity: 1, unit_price: 0, total_points: 0, status: 'pending', reservation_id: '', delivery_mode: 'manual', delivery_state: 'manual', delivery_attempts: 0, payload: {}, delivery_receipt: {}, created_at: '', updated_at: '' };
const emptyOrderResult: ShopOrderResult = { order: emptyOrder, account: { player_uid: '', status: 'active', balance: 0, created_at: '', updated_at: '' }, duplicate: false };

export const shopApi = {
  summary: () => handleRequest<unknown, ShopSummary>(() => apiClient.get('/shop/summary'), { products: 0, enabled_products: 0, pending_orders: 0, delivered_orders: 0, spent_points: 0, failed_deliveries: 0, processing_deliveries: 0, delivery_events: 0 }, { fallbackOnError: false }),
  products: (includeDisabled = true) => handleRequest<unknown, ProductListResult>(() => apiClient.get('/shop/products', { params: { include_disabled: includeDisabled, limit: 500 } }), { items: [], count: 0 }, { fallbackOnError: false }),
  createProduct: (input: ShopProductInput) => handleRequest<unknown, ShopProduct>(() => apiClient.post('/shop/products', input), emptyProduct, { fallbackOnError: false }),
  updateProduct: (id: string, input: ShopProductInput) => handleRequest<unknown, ShopProduct>(() => apiClient.put(`/shop/products/${encodeURIComponent(id)}`, input), { ...emptyProduct, id }, { fallbackOnError: false }),
  archiveProduct: (id: string) => handleRequest<unknown, ShopProduct>(() => apiClient.delete(`/shop/products/${encodeURIComponent(id)}`), { ...emptyProduct, id, enabled: false }, { fallbackOnError: false }),
  orders: (status = '', playerUID = '', deliveryState = '', deliveryMode = '') => handleRequest<unknown, OrderListResult>(() => apiClient.get('/shop/orders', { params: { status: status || undefined, player_uid: playerUID || undefined, delivery_state: deliveryState || undefined, delivery_mode: deliveryMode || undefined, limit: 500 } }), { items: [], count: 0 }, { fallbackOnError: false }),
  deliveryEvents: (orderID = '', eventType = '') => handleRequest<unknown, DeliveryEventListResult>(() => apiClient.get('/shop/delivery-events', { params: { order_id: orderID || undefined, event_type: eventType || undefined, limit: 500 } }), { items: [], count: 0 }, { fallbackOnError: false }),
  redemptionAttempts: (result = '', playerUID = '') => handleRequest<unknown, RedemptionAttemptListResult>(() => apiClient.get('/shop/redemption-attempts', { params: { result: result || undefined, player_uid: playerUID || undefined, limit: 500 } }), { items: [], count: 0 }, { fallbackOnError: false }),
  deliverBatch: (input: ShopBatchDeliveryInput) => handleRequest<unknown, ShopBatchDeliveryResult>(() => apiClient.post('/shop/maintenance/deliver', input), { selected: 0, delivered: 0, failed: 0, uncertain: 0, skipped: 0, items: [] }, { fallbackOnError: false }),
  createOrder: (input: ShopOrderCreateInput) => handleRequest<unknown, ShopOrderResult>(() => apiClient.post('/shop/orders', input), emptyOrderResult, { fallbackOnError: false }),
  deliverOrder: (id: string) => handleRequest<unknown, ShopOrderResult>(() => apiClient.post(`/shop/orders/${encodeURIComponent(id)}/deliver`), emptyOrderResult, { fallbackOnError: false }),
  resetDelivery: (id: string) => handleRequest<unknown, ShopOrder>(() => apiClient.post(`/shop/orders/${encodeURIComponent(id)}/delivery/reset`), { ...emptyOrder, id }, { fallbackOnError: false }),
  completeOrder: (id: string) => handleRequest<unknown, ShopOrderResult>(() => apiClient.post(`/shop/orders/${encodeURIComponent(id)}/complete`), emptyOrderResult, { fallbackOnError: false }),
  cancelOrder: (id: string) => handleRequest<unknown, ShopOrderResult>(() => apiClient.post(`/shop/orders/${encodeURIComponent(id)}/cancel`), emptyOrderResult, { fallbackOnError: false }),
};
