# PalPanel 积分系统 API

## 身份规则

积分账户以 Palworld `PlayerUID` 为唯一主键。昵称和 Steam ID 只作为可更新的辅助字段，不作为余额归属依据。

## 管理员调整积分

```http
POST /api/economy/accounts/<player_uid>/adjust
Content-Type: application/json
```

```json
{
  "nickname": "玩家名称",
  "steam_id": "7656119...",
  "delta": 100,
  "reason": "admin_compensation",
  "reference_type": "manual_adjustment",
  "reference_id": "ticket-20260731-001",
  "metadata": {
    "note": "活动补偿"
  }
}
```

`reference_id` 建议始终填写。相同玩家、业务类型和业务引用重复提交时不会重复记账。

## 每日签到

```http
POST /api/economy/accounts/<player_uid>/checkin
Content-Type: application/json
```

```json
{
  "nickname": "玩家名称",
  "steam_id": "7656119...",
  "points": 10
}
```

`local_date` 省略时由后端按 `PALPANEL_OPERATIONS_TIMEZONE` 计算。

## 游戏内命令入口

```http
POST /api/economy/commands/execute
Content-Type: application/json
```

```json
{
  "event_id": "paldefender-chat-20260731-000001",
  "player_uid": "00000000000000000000000000000000",
  "nickname": "玩家名称",
  "steam_id": "7656119...",
  "message": "!签到"
}
```

返回的 `reply` 由事件采集器通过 PalDefender 私聊发回游戏。`event_id` 必须稳定且唯一；同一事件重试会返回原结果。

## 积分预留

下单前预留：

```http
POST /api/economy/reservations
```

```json
{
  "player_uid": "00000000000000000000000000000000",
  "reference_id": "shop-order-0001",
  "amount": 300,
  "ttl_seconds": 900
}
```

发货成功：

```http
POST /api/economy/reservations/<reservation_id>/commit
```

发货失败或取消：

```http
POST /api/economy/reservations/<reservation_id>/release
```

预留时余额立即减少；提交只确认最终状态，释放和过期会原额退款。
