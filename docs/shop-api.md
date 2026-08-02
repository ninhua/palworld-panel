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

### 查询订单

```http
GET /api/shop/orders?status=pending&player_uid=001122...&delivery_state=failed&delivery_mode=automatic&limit=100&offset=0
```

筛选参数：

- `status`：`pending`、`delivered`、`cancelled`。
- `player_uid`：规范化后的 PlayerUID。
- `delivery_state`：`manual`、`pending`、`processing`、`failed`、`succeeded`。
- `delivery_mode`：具体模式，或 `automatic` 表示所有非人工模式。

### 单个自动交付或重试

```http
POST /api/shop/orders/{id}/deliver
```

- `pending` 或 `failed`：执行 PalDefender 发放。
- `succeeded`：只重试积分结算，不再次发放。
- `processing`：拒绝执行，要求管理员先核对。

### 批量自动交付

```http
POST /api/shop/maintenance/deliver
Content-Type: application/json
```

```json
{
  "order_ids": [],
  "include_failed": false,
  "limit": 20
}
```

- `order_ids` 为空时按创建时间选择安全可处理的自动订单。
- 单次最多 50 个订单，按顺序串行执行。
- 默认不重试 `failed`；只有 `include_failed=true` 才会纳入。
- `processing` 永不自动重试。
- 可传入明确的 `order_ids`，不符合安全条件的订单会返回 `skipped`。

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

## 交付审计

```http
GET /api/shop/delivery-events?order_id=order_xxx&event_type=failed&limit=100&offset=0
```

事件类型：

- `created`
- `started`
- `failed`
- `uncertain`
- `succeeded`
- `reset`
- `completed`
- `cancelled`

审计表为追加式记录，不包含 PalDefender 令牌或其他密钥。失败信息限制为 500 字节。

## 汇总

```http
GET /api/shop/summary
```

返回商品总数、上架数、待交付订单、已交付订单、已消费积分、明确失败数、待人工核对数以及审计事件总数。

## 数据库升级

`0.8.63` 自动创建 `shop_delivery_events` 表和索引。现有商品、订单、积分预留、库存和 `0.8.62` 交付状态保持不变。

## 玩家侧商城与游戏命令

`0.8.77` 增加签名保护的玩家侧接口。它们与游戏事件入口使用相同的 `X-PalPanel-*` HMAC 请求头，不接受匿名公网调用。

### 查询玩家目录

```http
POST /api/integrations/game/shop/catalog
Content-Type: application/json
```

```json
{
  "player_uid": "00112233445566778899aabbccddeeff",
  "nickname": "Player",
  "steam_id": "steam_76561198000000000",
  "query": "帕鲁球",
  "limit": 5,
  "offset": 0
}
```

响应包含玩家当前积分、商品兑换码、库存、已购买数量和剩余个人限购。兑换码由商品 ID 的哈希部分生成，显示为 8 位大写字符；如果发生极低概率的兑换码冲突，购买时会要求使用完整商品 ID 或唯一商品名称。

### 玩家创建订单

```http
POST /api/integrations/game/shop/orders
Content-Type: application/json
```

```json
{
  "player_uid": "00112233445566778899aabbccddeeff",
  "nickname": "Player",
  "steam_id": "steam_76561198000000000",
  "idempotency_key": "chat-message-or-external-event-id",
  "product": "A1B2C3D4",
  "quantity": 2
}
```

`product` 可以是商城显示的 8 位兑换码、完整商品 ID，或唯一的完整商品名称。自动交付商品会立即调用 PalDefender；人工商品会保留为待管理员交付订单。

### 查询玩家订单

```http
POST /api/integrations/game/shop/orders/query
Content-Type: application/json
```

```json
{
  "player_uid": "00112233445566778899aabbccddeeff",
  "status": "pending",
  "limit": 20,
  "offset": 0
}
```

接口只返回请求中 `player_uid` 对应的订单，不能通过请求体读取其他玩家订单。

### 游戏聊天命令

游戏事件桥接支持以下命令，并遵循积分系统中的命令前缀和“允许无前缀命令”设置：

```text
商城
商城 2
商城 帕鲁球
兑换 A1B2C3D4
兑换 A1B2C3D4 2
我的订单
我的订单 2
```

也接受别名 `商店`、`购买` 和 `订单`。命令结果通过 PalDefender 私人聊天回复，包括余额、兑换码、库存/限购状态、订单状态以及自动交付结果。

相同游戏事件 ID 会映射到同一订单幂等键。重复处理不会重复扣分、减库存或发放商品。
