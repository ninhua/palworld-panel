# 积分商城 API

积分商城使用现有 `palpanel.db`，积分扣款通过统一经济账本的预留机制完成。

## 交付模型

`0.8.61` 仅启用 `manual` 人工交付：

1. 创建订单时检查商品上架状态、库存、限购和玩家余额。
2. 系统预扣积分并占用库存，订单进入 `pending`。
3. 管理员在游戏内交付商品后调用确认接口，积分预留转为已结算，订单进入 `delivered`。
4. 未交付订单可取消，系统自动退还积分并恢复有限库存。

订单使用 `player_uid + idempotency_key` 幂等去重。重复请求不会重复扣积分或重复占用库存。

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
  "description": "管理员确认订单后在游戏内人工交付",
  "price": 100,
  "stock": 20,
  "per_player_limit": 2,
  "enabled": true,
  "delivery_mode": "manual",
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
  "idempotency_key": "panel-20260731-player-order-001",
  "product_id": "product_xxx",
  "player_uid": "00112233445566778899aabbccddeeff",
  "nickname": "Player",
  "steam_id": "76561198000000000",
  "quantity": 1
}
```

余额不足、库存不足、商品下架或超过每人限购时返回 HTTP `409`。

### 查询订单

```http
GET /api/shop/orders?status=pending&player_uid=001122...&limit=100&offset=0
```

### 确认交付

```http
POST /api/shop/orders/{id}/complete
```

仅允许处理 `pending` 订单。确认后积分预留被提交，不再允许取消。

### 取消并退款

```http
POST /api/shop/orders/{id}/cancel
```

仅允许处理 `pending` 订单。取消后积分退还，有限库存恢复。

## 汇总

```http
GET /api/shop/summary
```

返回商品总数、上架数、待交付订单、已交付订单和已消费积分。
