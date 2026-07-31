# 事件任务系统 API

版本：PalPanel patch `0.8.58`

## 功能范围

任务系统消费 `/api/integrations/game/events` 接收的可靠游戏事件，并把任务奖励写入统一积分账本。

支持三种周期：

- `daily`：按运营时区的自然日重置。
- `weekly`：按运营时区周一开始的新周重置。
- `once`：每个玩家永久只完成一次。

运营时区继续使用 `PALPANEL_OPERATIONS_TIMEZONE`，默认 `Asia/Shanghai`。

## 创建任务

```text
POST /api/tasks
```

需要 `config:write` 权限。

```json
{
  "name": "每日捕获 3 只棉悠悠",
  "description": "捕获指定帕鲁获得积分",
  "event_type": "PAL_CAPTURED",
  "target_amount": 3,
  "reward_points": 30,
  "cycle": "daily",
  "amount_field": "count",
  "filters": {
    "pal_id": "SheepBall"
  },
  "enabled": true
}
```

字段说明：

- `event_type`：与游戏事件的 `type` 完全匹配，保存时自动转为大写。
- `target_amount`：完成目标，必须大于 0。
- `reward_points`：完成后发放的积分，允许为 0。
- `amount_field`：从事件 `payload` 读取增量；留空时每个事件计数 1。支持 `boss.id` 这类点路径。
- `filters`：最多 16 个标量等值条件，同样支持点路径。
- `enabled`：是否立即参与事件匹配。

## 示例事件

```json
{
  "event_id": "capture-20260731-0001",
  "type": "PAL_CAPTURED",
  "player_uid": "00000000000000000000000000000000",
  "nickname": "Player",
  "steam_id": "76561198000000000",
  "occurred_at": "2026-07-31T14:00:00Z",
  "payload": {
    "pal_id": "SheepBall",
    "count": 1
  }
}
```

达到目标时，积分流水使用：

```text
reference_type = task_reward
reference_id   = <task_id>:<cycle_key>
actor          = task-system
```

同一玩家、任务和周期只能产生一笔奖励。事件失败重试、桥接器重复发送或人工重试待发奖励，都不会重复加分。

## 管理接口

```text
GET    /api/tasks?include_archived=false
PUT    /api/tasks/<task_id>
DELETE /api/tasks/<task_id>
GET    /api/tasks/progress/<player_uid>
POST   /api/tasks/maintenance/retry-rewards?limit=100
```

`DELETE` 为归档操作：任务会被禁用并保留历史进度、事件回执和奖励审计。

玩家进度接口返回所有当前启用任务。尚未产生进度的任务也会显示为 0，便于面板或机器人直接展示任务列表。

## 失败恢复

任务进度、事件回执和奖励状态分别保存：

1. 任务事件先按 `task_id + player_uid + cycle_key + event_id` 去重。
2. 达标后建立 `pending` 奖励记录。
3. 统一积分账本加分成功后标记为 `granted`。
4. 加分失败时标记为 `failed`，原游戏事件也会进入失败状态。
5. 使用相同游戏事件 ID 重试，或调用维护接口，可继续发放未完成奖励。

这种设计避免跨 SQLite 连接事务失败时出现重复奖励。
