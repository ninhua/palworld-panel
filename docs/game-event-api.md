# 游戏事件与 PalDefender 日志桥接

版本：PalPanel patch `0.8.78`

## 内置日志桥接

PalPanel 启动后会自动读取 PalDefender 的 `Logs/*.log`，无需额外部署转发程序。桥接器在读取完成后等待下一轮：有新增内容时等待 1 秒，空闲时等待 3 秒，不再使用会追赶积压 Tick 的每秒轮询。只检查最近两个日志文件，并将读取偏移、文件前缀指纹和轮转恢复次数保存到 SQLite。

首次启用时从当前日志末尾开始，不回放旧日志，避免更新后重复发奖。后续重启会从持久化游标继续。日志文件被截断、同名替换或轮转后，桥接器会识别文件身份变化并从新文件开头继续；所有标准事件仍通过原始事件 ID 幂等去重。

支持的标准化事件：

| 事件类型 | 来源 | 常用 Payload |
| --- | --- | --- |
| `PLAYER_CHAT` | 游戏聊天日志 | `message` |
| `PAL_CAPTURED` | PalDefender 捕捉日志 | `pal_id`, `pal_name`, `count`, `x`, `y`, `z` |
| `PAL_KILLED` | 可识别的死亡/击杀日志 | `pal_id`, `target_name`, `count` |
| `PLAYER_LOGIN` | 登录日志 | `count` |
| `ITEM_CRAFTED` | 制作日志 | `item_name`, `count` |
| `CHECKIN_COMPLETED` | 当天首次签到命令成功后派生 | `count`, `local_date`, `balance` |

捕捉任务推荐配置：

```text
event_type = PAL_CAPTURED
amount_field = count
```

只统计某种帕鲁时，过滤条件使用内部 Pal ID：

```json
{
  "pal_id": "SheepBall"
}
```

击杀日志在不同 PalDefender 版本或场景中可能只包含目标名称。先在桥接观察记录中确认实际 Payload/日志格式，再决定使用 `pal_id` 或 `target_name` 过滤。

## 必需的 PalDefender 日志开关

管理页面“一键修复日志开关”会启用：

```text
logChat
logPlayerUID
logPlayerCaptures
logPlayerDeaths
logPlayerLogins
logCraftings
```

随后尝试热重载 PalDefender 配置。热重载失败时需要重启游戏服务端。

## 桥接诊断 API

```text
GET  /api/game-events/bridge/status
GET  /api/game-events/bridge/observations?status=processed&limit=50&offset=0
GET  /api/game-events/bridge/dead-letters?status=pending&limit=50&offset=0
POST /api/game-events/bridge/dead-letters/<id>/replay
POST /api/game-events/bridge/dead-letters/<id>/dismiss
POST /api/game-events/bridge/repair
```

观察状态：

- `processed`：日志已识别、玩家已匹配并进入积分/任务链路。
- `replayed`：管理员从死信记录成功重放。
- `unmatched_player`：识别了事件，但无法从实时监控共享的玩家快照匹配 PlayerUID。
- `parse_failed`：日志看起来属于聊天、捕捉、击杀、登录或制作事件，但当前解析器无法识别。
- `cursor_reset`：检测到日志截断或同名文件替换，持久化游标已安全恢复。
- `error`：处理、积分、任务或存储阶段失败。

排查顺序：

1. 确认桥接进程为“运行中”。
2. 确认对应日志开关为“已启用”。
3. 在游戏内产生一条新的聊天、捕捉或死亡事件。
4. 检查最近观察记录是否出现。
5. 若为 `unmatched_player`，检查日志中是否包含昵称、PlayerUID 或 UserID，并确认面板实时监控已产生玩家快照。
6. 若事件为 `processed` 但任务无进度，检查任务事件类型、`amount_field` 和 Payload 过滤条件。

## 游戏聊天签到回执

聊天命令执行后，PalPanel 通过 PalDefender 玩家消息接口发送结果。以下条件任一不满足时，签到可能已经入账，但游戏内看不到回执：

- PalDefender 已安装并成功加载。
- REST API 已启用并配置有效 Token。
- 玩家可在实时监控共享快照中匹配。
- PalDefender 消息接口可用。

可通过 `GET /api/game-events/<event_id>` 查看 `reply_delivery` 和 `reply_error`。

## 签名事件入口

外部桥接仍可使用：

```text
POST /api/integrations/game/events
```

该入口继续使用 AstrBot/PalPanel 集成的 HMAC 凭据。事件按 `event_id` 去重，并与内置日志桥接进入同一任务处理链路。


## 死信与人工重放

以下情况不会再静默丢弃日志，而是写入 `game_event_bridge_dead_letters`：

- 疑似游戏事件但当前解析器无法识别。
- 已识别事件，但玩家昵称、PlayerUID 或 PalDefender UserID 无法匹配。
- 积分命令、商城交付、任务推进或数据库写入失败。

每条死信保存原始事件 ID、事件类型、玩家提示、Payload、日志原文、失败原因和重试次数。重放继续使用原始事件 ID，因此签到、积分、任务进度、商城订单和奖励均由现有幂等机制防止重复执行。

玩家未匹配时，重放请求可以指定 PlayerUID：

```json
{
  "player_uid": "00000000000000000000000000000001"
}
```

忽略操作只把死信标记为 `dismissed`，不会删除审计记录，也不会执行事件。

## 持久化游标

`GET /api/game-events/bridge/status` 现在同时返回：

- `pending_dead_letters`：待处理死信数量。
- `cursor_files`：已持久化游标的日志文件数量。
- `rotation_resets`：当前进程检测到的日志轮转/截断恢复次数。
- `offsets`：每个文件的偏移、文件大小、前缀指纹、累计恢复次数和最后恢复原因。

日志文件小于 256 字节时暂不生成前缀指纹，避免文件仍在写入时产生不稳定身份；达到 256 字节后自动保存稳定指纹。

## 玩家任务命令与在线事件（0.8.79）

PalDefender 日志桥接现在识别：

```text
任务
我的任务
任务进度
任务 2
任务 捕捉
```

命令结果通过 PalDefender 私人消息返回。相同聊天日志事件仍由游戏事件账本去重。

在线时长不依赖聊天或日志文本。实时监控原本每 15 秒获取一次 Palworld REST 玩家列表；任务系统直接复用这份玩家快照，最多每 30 秒结算一次完整分钟，不再从日志桥接器单独查询 PalDefender 玩家目录。追踪游标保存在 SQLite；下线、重连和面板重启不会造成重复分钟事件。
