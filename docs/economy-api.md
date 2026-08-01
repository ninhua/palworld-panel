# PalPanel 积分与签到 API

版本：PalPanel patch `0.8.72`

## 账户身份

积分账户以 Palworld `PlayerUID` 为唯一主键。昵称和 Steam ID 只作为辅助信息，不参与余额归属判断。

## 签到与游戏命令配置

```http
GET /api/economy/config
PUT /api/economy/config
Content-Type: application/json
```

完整配置示例：

```json
{
  "command_prefix": "!",
  "allow_bare_commands": true,
  "daily_checkin_points": 10,
  "checkin_streak_enabled": true,
  "checkin_streak_bonus_per_day": 2,
  "checkin_streak_max_days": 7,
  "checkin_cycle_days": 7,
  "checkin_cycle_bonus": 10,
  "checkin_aliases": ["签到", "qd", "checkin"],
  "points_aliases": ["积分", "jf", "points"],
  "help_aliases": ["帮助", "菜单", "help"]
}
```

积分计算规则：

```text
当日积分 = 基础积分 + 连续签到奖励 + 周期额外奖励
连续签到奖励 = min(连续天数 - 1, 递增封顶天数 - 1) × 每日递增奖励
```

- 漏签后连续天数从 1 重新开始。
- 同一天重复签到不会重复发放积分。
- `checkin_cycle_days=0` 表示关闭周期奖励。
- `allow_bare_commands=true` 时，配置 `!` 前缀后仍同时接受 `签到` 和 `!签到`。
- 修改规则不会重算已经入账的历史签到。

## 人工签到

```http
POST /api/economy/accounts/<player_uid>/checkin
Content-Type: application/json
```

```json
{
  "nickname": "玩家名称",
  "steam_id": "steam_7656119...",
  "local_date": "2026-08-01"
}
```

`points` 为可选字段。省略时使用当前配置的 `daily_checkin_points`；填写后只覆盖本次基础积分，连续奖励和周期奖励仍按配置计算。

## 签到历史

```http
GET /api/economy/accounts/<player_uid>/checkins?limit=30
```

每条记录包含：

- `points`：本次总积分。
- `base_points`：基础积分。
- `streak_bonus`：连续签到奖励。
- `cycle_bonus`：周期额外奖励。
- `streak_day`：连续签到天数。

## 游戏内命令

游戏聊天事件进入面板后，命令执行接口等价于：

```http
POST /api/economy/commands/execute
Content-Type: application/json
```

```json
{
  "event_id": "paldefender-chat-20260801-000001",
  "player_uid": "00000000000000000000000000000000",
  "nickname": "玩家名称",
  "steam_id": "steam_76561198000000000",
  "message": "签到"
}
```

同一 `event_id` 重试时返回原结果，不会重复签到或重复加分。当天首次签到成功后，事件链路还会派生一个任务事件：

```json
{
  "type": "CHECKIN_COMPLETED",
  "payload": {
    "count": 1,
    "local_date": "2026-08-01",
    "balance": 120
  }
}
```

因此签到任务应配置：

```text
event_type = CHECKIN_COMPLETED
amount_field = count
```

重复签到不会派生 `CHECKIN_COMPLETED`，也不会推进任务。

## 管理员调整积分

```http
POST /api/economy/accounts/<player_uid>/adjust
Content-Type: application/json
```

```json
{
  "nickname": "玩家名称",
  "steam_id": "steam_7656119...",
  "delta": 100,
  "reason": "活动补偿",
  "reference_type": "manual_adjustment",
  "reference_id": "ticket-20260801-001"
}
```

`reference_id` 应保持稳定，避免业务重试产生重复记账。
