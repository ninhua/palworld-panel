# 游戏事件与 PalDefender 日志桥接

版本：PalPanel patch `0.8.72`

## 内置日志桥接

PalPanel 启动后会自动读取 PalDefender 的 `Logs/*.log`，无需额外部署转发程序。桥接器每秒检查一次新增内容，并保存每个日志文件的读取偏移。

首次启用时从当前日志末尾开始，不回放旧日志，避免更新后重复发奖。后续重启会从已保存偏移继续。

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
POST /api/game-events/bridge/repair
```

观察状态：

- `processed`：日志已识别、玩家已匹配并进入积分/任务链路。
- `unmatched_player`：识别了事件，但无法从 PalDefender 玩家目录匹配 PlayerUID。
- `error`：处理、积分、任务或存储阶段失败。

排查顺序：

1. 确认桥接进程为“运行中”。
2. 确认对应日志开关为“已启用”。
3. 在游戏内产生一条新的聊天、捕捉或死亡事件。
4. 检查最近观察记录是否出现。
5. 若为 `unmatched_player`，检查日志中是否包含昵称、PlayerUID 或 UserID，并确认 PalDefender 玩家目录可读取。
6. 若事件为 `processed` 但任务无进度，检查任务事件类型、`amount_field` 和 Payload 过滤条件。

## 游戏聊天签到回执

聊天命令执行后，PalPanel 通过 PalDefender 玩家消息接口发送结果。以下条件任一不满足时，签到可能已经入账，但游戏内看不到回执：

- PalDefender 已安装并成功加载。
- REST API 已启用并配置有效 Token。
- 玩家可在 PalDefender 玩家目录中匹配。
- PalDefender 消息接口可用。

可通过 `GET /api/game-events/<event_id>` 查看 `reply_delivery` 和 `reply_error`。

## 签名事件入口

外部桥接仍可使用：

```text
POST /api/integrations/game/events
```

该入口继续使用 AstrBot/PalPanel 集成的 HMAC 凭据。事件按 `event_id` 去重，并与内置日志桥接进入同一任务处理链路。
