# 事件任务系统 API

版本：PalPanel patch `0.8.79`

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

## 面板管理界面

Patch `0.8.59` 新增 `/operations-tasks` 页面，并在侧边栏“积分系统”分组中提供入口。

界面支持：

- 新建、编辑、复制和归档任务。
- 每日捕获、每日击杀、在线时长和 Boss 参与快速模板。
- 自定义事件类型、数量字段和 Payload JSON 过滤条件。
- 查看启用、停用和归档状态。
- 按 PlayerUID 查询当前任务进度及奖励状态。
- 手动重试最多 100 条待发或失败奖励。

原有 `/tasks` 页面仍为 PalPanel 后台作业与计划任务队列；运营任务页面使用独立路径，避免路由语义冲突。

## 任务事件诊断与重放

```text
POST /api/tasks/diagnostics/evaluate
POST /api/tasks/diagnostics/replay
```

两个接口都接收标准任务事件对象：

```json
{
  "event_id": "task-test-001",
  "type": "PAL_CAPTURED",
  "player_uid": "00000000000000000000000000000000",
  "payload": {
    "pal_id": "SheepBall",
    "count": 1
  }
}
```

`evaluate` 只读检查全部任务定义，不写入进度或奖励。每条结果包含状态、原因、相关字段、期望值、实际值和预计进度。

`replay` 使用现有任务处理链路实际推进进度，需要 `players:write` 权限。必须提供 `event_id`、`type` 和 `player_uid`。相同事件ID对同一玩家、任务和周期仍只处理一次。

## 玩家游戏内任务查询（0.8.79）

游戏聊天支持：

```text
任务
我的任务
任务进度
任务 2
任务 捕捉
```

命令遵循积分系统配置的命令前缀和“允许无前缀命令”开关。每页最多显示 5 个任务，包含当前进度、周期和积分奖励状态。任务奖励仍为达标后自动发放，不需要玩家手动领取。

签名集成也可以按玩家查询任务：

```text
POST /api/integrations/game/tasks/query
```

```json
{
  "player_uid": "00000000000000000000000000000000",
  "query": "捕捉",
  "limit": 5,
  "offset": 0
}
```

该接口复用游戏事件入口的 HMAC 请求头，且只返回请求中 `player_uid` 的任务进度。

## 在线时长自动结算（0.8.79）

PalPanel 每 30 秒调用 PalDefender 玩家目录，对 `Status=Online` 的玩家累计完整分钟，并生成：

```json
{
  "type": "PLAYER_ONLINE",
  "payload": {
    "minutes": 1,
    "seconds": 60,
    "from_minute": 1,
    "to_minute": 1
  }
}
```

在线任务应设置：

```text
事件类型：PLAYER_ONLINE
数量字段：minutes
```

追踪状态持久化在 `operations_task_online_tracking`：

- 面板重启后的首轮采样只恢复状态，不把停机时间计入。
- 正常采样间隔最多计入 90 秒，避免 PalDefender 暂时不可用后过量累计。
- 玩家下线时补结算最后一个采样区间。
- 不足一分钟的秒数保留到玩家下次上线继续累计。
- 每个分钟区间生成稳定事件 ID；进度写入成功但追踪状态更新前异常退出时，重试仍不会重复推进。

管理接口：

```text
GET /api/tasks/online-tracking?limit=100
```

游戏事件桥接状态也会返回当前在线人数、跟踪玩家数、累计提交分钟、最近采样时间和采样错误。
