# 积分商城 API

积分商城使用现有 `palpanel.db`。订单扣款通过统一经济账本的积分预留完成，只有交付确认成功后才提交预留。

## 交付模式

商品支持三种 `delivery_mode`：

- `manual`：管理员在游戏内人工发放，再确认订单。
- `paldefender_items`：通过 PalDefender REST 发放物品。
- `paldefender_pal_templates`：通过 PalDefender REST 发放帕鲁模板。

自动交付商品创建订单后会立即尝试发放。订单仍使用 `player_uid + idempotency_key` 幂等去重；重复请求不会重复扣积分、占用库存或创建新订单。

### 自动物品 Payload

```json
{
  "items": [
    {"item_id": "LegendSphere", "count": 10}
  ]
}
```

订单 `quantity` 会乘算每项 `count`。单次最多 100 个物品条目，每项最终数量不得超过 `2147483647`。

### 自动帕鲁模板 Payload

```json
{
  "pal_templates": [
    "starter_pal.json"
  ]
}
```

订单 `quantity` 会重复整个模板列表。单次展开后最多 20 个模板。

## 交付状态

订单包含独立的 `delivery_state`：

- `manual`：人工交付订单。
- `pending`：等待自动交付或可安全重试。
- `processing`：外部请求已开始，但结果不确定，必须人工核对。
- `failed`：确认未交付，可安全重试或取消退款。
- `succeeded`：游戏内已发放，等待或已经完成积分结算。

PalDefender 超时、连接中断、返回体损坏或服务端 5xx 会被视为结果不确定。此时订单保持 `processing`，系统禁止自动重试和取消退款，以避免重复发放或玩家同时获得商品与退款。

管理员核对游戏内结果后只能选择：

1. 已发放：调用人工确认接口完成积分结算。
2. 未发放：调用交付状态重置接口，再重新自动交付或取消。

## 商品

### 查询商品

```http
GET /api/shop/products?include_disabled=true&limit=100&offset=0
```

### 创建商品

```http
POST /api/shop/products
Content-Type: application/json
```

```json
{
  "name": "高级帕鲁球礼包",
  "description": "自动发放 10 个高级帕鲁球",
  "price": 100,
  "stock": 20,
  "per_player_limit": 2,
  "enabled": true,
  "delivery_mode": "paldefender_items",
  "payload": {
    "items": [
      {"item_id": "LegendSphere", "count": 10}
    ]
  }
}
```

`stock = -1` 表示不限库存；`per_player_limit = 0` 表示不限购。

### 更新与下架

```http
PUT    /api/shop/products/{id}
DELETE /api/shop/products/{id}
```

删除操作为下架，不删除历史订单。

## 订单

### 创建订单

```http
POST /api/shop/orders
Content-Type: application/json
```

```json
{
  "idempotency_key": "panel-20260801-player-order-001",
  "product_id": "product_xxx",
  "player_uid": "00112233445566778899aabbccddeeff",
  "nickname": "Player",
  "steam_id": "76561198000000000",
  "quantity": 1
}
```

自动商品会在订单持久化和积分预留成功后立即尝试 PalDefender 交付。交付失败不会删除订单；响应中的 `delivery_state` 和 `failure` 用于后续处理。

余额不足、库存不足、商品下架、超过每人限购或自动交付数量超限时返回 HTTP `409` 或 `400`。

### 查询订单

```http
GET /api/shop/orders?status=pending&player_uid=001122...&limit=100&offset=0
```

### 自动交付或重试

```http
POST /api/shop/orders/{id}/deliver
```

仅适用于自动交付订单：

- `pending` 或 `failed`：执行 PalDefender 发放。
- `succeeded`：只重试积分结算，不再次发放。
- `processing`：拒绝执行，要求管理员先核对。

### 重置不确定交付状态

```http
POST /api/shop/orders/{id}/delivery/reset
```

仅允许将 `processing` 或 `failed` 的自动订单重置为 `pending`。管理员必须先确认游戏内未实际发放；错误重置可能导致重复奖励。

### 人工确认交付

```http
POST /api/shop/orders/{id}/complete
```

人工订单在游戏内交付后使用此接口。自动订单处于 `processing` 时，也可在确认已经发放后使用此接口完成对账和积分结算。

### 取消并退款

```http
POST /api/shop/orders/{id}/cancel
```

仅允许取消人工订单，或自动订单的 `pending` / `failed` 状态。取消后积分退还，有限库存恢复。`processing` 和 `succeeded` 禁止退款。

## 汇总

```http
GET /api/shop/summary
```

返回商品总数、上架数、待交付订单、已交付订单、已消费积分以及需要处理的异常交付订单数。

## 数据库升级

`0.8.62` 会自动重建 `0.8.61` 的商城表约束，以允许新的自动交付模式，并为旧订单补充交付状态字段。旧人工订单迁移后保持 `delivery_mode=manual`、`delivery_state=manual`，不会触发自动发放。
