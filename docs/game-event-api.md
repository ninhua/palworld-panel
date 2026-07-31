# 游戏事件接入 API

版本：PalPanel patch `0.8.58`

## 目的

该接口接收 AstrBot、PalDefender 事件转发器或其他受信任桥接程序上报的游戏事件。事件先写入去重账本，再执行命令或后续任务逻辑。

## 接口

```text
POST /api/integrations/game/events
```

请求体最大 1 MiB。接口不使用面板登录 Cookie，而是使用与现有 AstrBot 集成相同的 HMAC 凭据：

- `X-PalPanel-Id`
- `X-PalPanel-Timestamp`
- `X-PalPanel-Nonce`
- `X-PalPanel-Signature`

面板 ID 和共享密钥来自现有 AstrBot/PalPanel 集成配置。时间戳允许误差一分钟，nonce 两分钟内不可重复。

签名规范：

```text
BODY_SHA256 = hex(sha256(raw_request_body))
CANONICAL = UPPER(method) + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + BODY_SHA256
SIGNATURE = hex(hmac_sha256(shared_secret, CANONICAL))
```

签名中的 path 必须是 `/api/integrations/game/events`。

## 玩家聊天事件

```json
{
  "event_id": "paldefender-chat-20260731-000001",
  "type": "PLAYER_CHAT",
  "player_uid": "00000000000000000000000000000000",
  "nickname": "Player",
  "steam_id": "76561198000000000",
  "occurred_at": "2026-07-31T13:30:00Z",
  "payload": {
    "message": "!签到"
  }
}
```

处理流程：

1. 按 `event_id` 去重。
2. 读取积分系统当前命令前缀。
3. 执行签到、积分查询或帮助命令。
4. 玩家在线且 PalDefender REST 可用时，向玩家发送私聊回执。
5. 保存处理结果和回执状态。

未识别聊天不会创建命令流水，但游戏事件本身仍会保存为已完成事件。

## 其他事件

`0.8.58` 会把其他事件交给任务引擎。匹配已启用任务时，按事件数量推进当前日、周或永久任务；达到目标后通过统一积分账本幂等发放奖励。没有匹配任务的事件仍会正常保存。

建议事件类型：

```text
PLAYER_LOGIN
PLAYER_LOGOUT
PLAYER_CHAT
PAL_KILLED
PAL_CAPTURED
BOSS_KILLED
PLAY_MINUTES
CUSTOM
```

## 查询事件

需要面板登录和读取权限：

```text
GET /api/game-events?type=PLAYER_CHAT&player_uid=<uid>&limit=50&offset=0
GET /api/game-events/<event_id>
```

事件状态：

- `processing`：正在处理。
- `completed`：处理完成。
- `failed`：处理失败，可使用同一 `event_id` 重试。

已完成事件重复上报只返回原结果，不会重复签到或重复发奖。
