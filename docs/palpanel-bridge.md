# PalPanelBridge 安装与验证

`PalPanelBridge` 是 PalPanel 的 UE4SS C++ 只读通信探针，用于验证：

```text
PalPanel 后端 HTTP → PalPanelBridge → UE4SS on_update 游戏线程
```

当前版本不会修改玩家、背包、帕鲁或存档。

## 兼容版本

- PalPanelBridge：`0.1.13`
- UE4SS Git SHA：`c838a8acaade1a0f860bdf249f039e58f4e10088`
- UE4SS 构建配置：`Game__Shipping__Win64`
- 默认监听：`127.0.0.1:18083`

C++ 模组必须和服务器实际运行的 UE4SS 使用相同提交、构建配置及 C Runtime。不要使用按发布标签 `v3.0.1` 的 `d935b5b` 编译的旧测试包。

## 安装

将压缩包中的 `PalPanelBridge` 文件夹放入：

```text
Pal/Binaries/Win64/ue4ss/Mods/PalPanelBridge
```

包内已有 `enabled.txt`。如果 `ue4ss/Mods/mods.txt` 中也存在 `PalPanelBridge : 1`，删除该行，只保留一种启用方式。

编辑 `PalPanelBridge/config.ini`：

```ini
listen=127.0.0.1
port=18083
token=替换为随机Token
```

`18082` 已由 `PalPanelSteamAPIProxy` 使用，不要作为 Bridge 端口。修改后重启 PalServer。

## 在诊断控制台验证

方法选择 `GET`，URL 填写：

```text
http://127.0.0.1:18083/v1/health
```

“请求头（JSON 对象）”填写：

```json
{
  "Authorization": "Bearer 与config.ini完全相同的Token"
}
```

请求体留空。成功响应应包含：

```json
{
  "ok": true,
  "bridge_version": "0.1.13",
  "ue4ss_loaded": true,
  "configured": true,
  "unreal_initialized": true,
  "game_thread_tick_seen": true
}
```

继续验证游戏线程队列：

```text
POST http://127.0.0.1:18083/v1/probe/game-thread
GET  http://127.0.0.1:18083/v1/jobs/<job_id>
```

两次请求都必须携带同一个 Authorization 请求头。最终任务应从 `queued` 变为 `completed`。

## 游戏线程运行状态

携带同一个 Authorization 请求头执行：

```text
GET http://127.0.0.1:18083/v1/runtime
```

响应包含 `game_thread_tick_count`、`last_game_thread_tick_unix_ms`、
`last_game_thread_tick_age_ms` 和 `bridge_uptime_ms`。间隔几秒请求两次，
`game_thread_tick_count` 应持续增长；该接口只读，不会修改游戏状态。

## 当前 World 对象探针

方法选择 `POST`，携带 Authorization 请求头执行：

```text
POST http://127.0.0.1:18083/v1/world
```

使用返回的 `job_id` 继续查询：

```text
GET http://127.0.0.1:18083/v1/jobs/<job_id>
```

任务在 UE4SS `on_update` 游戏线程中执行，成功时 `status` 为
`completed`、`world_found` 为 `true`，并返回 `world_name`、
`world_full_name` 和 `world_class_name`。该接口只读。

实机已验证 World 为 `PL_MainWorld5`，完整名为
`World /Game/Pal/Maps/MainWorld_5/PL_MainWorld5.PL_MainWorld5`。

## 在线玩家对象探针

玩家进入服务器后，方法选择 `POST`，携带 Authorization 请求头执行：

```text
POST http://127.0.0.1:18083/v1/players/online
```

使用返回的 `job_id` 查询：

```text
GET http://127.0.0.1:18083/v1/jobs/<job_id>
```

任务在游戏线程同时检查 `PalPlayerController`、`BP_PalPlayerController_C`，并只读
解析关联的 `PlayerState` 与 Pawn。如果 Controller 暂时不可见，则回退枚举
`PalPlayerState` 与 `BP_PalPlayerState_C`。结果增加 `controller_object_count`、
`player_state_object_count` 和每项的 `source`，用于区分 Controller 与 PlayerState
来源。`0.1.10` 还会以当前 World 为上下文只读调用帕鲁原生反射函数
`PalUtility.GetAllPlayerStates`，返回 `pal_utility_available`、
`pal_utility_player_state_count` 和 `pal_utility_error`。当前阶段不读取 SteamID、
背包或帕鲁数据，也不修改对象。

`0.1.13` 从当前 World 的 `GameState.PlayerArray` 读取服务端维护的权威
PlayerState 数组，并返回 `game_state_found`、`game_state`、
`game_state_player_array_available`、`game_state_player_state_count` 和
`game_state_error`。由该数组发现的玩家项以
`source: "game_state_player_array"` 标记；原有 PalUtility 和全局对象扫描仅保留为
只读诊断回退。

`0.1.13` 使用 UE4SS `FScriptArrayHelper_InContainer` 和数组内部对象属性逐项解析
UE5 `TObjectPtr<APlayerState>`，避免把对象句柄误当成裸指针导致游戏进程崩溃。

## 响应与任务时间

所有 JSON 响应均包含 `response_time_unix_ms` 和 UTC 格式的
`response_time_utc`。任务详情同时包含：

- `queued_at_unix_ms` / `queued_at_utc`：进入队列的时间。
- `executed_at_unix_ms` / `executed_at_utc`：游戏线程开始执行该任务的时间。
- `game_thread_tick_count_at_execution`：执行时的游戏线程 Tick 计数。

在线玩家结果还返回 `query_world_found` 和 `query_world`，用于确认每次查询实际使用
的 World 是否为 `PL_MainWorld5`。连续创建新任务时，任务 ID、时间和 Tick 计数都应变化。

## 故障判断

- `401 Unauthorized` 且响应是 PalPanelBridge JSON：监听已经成功，请求头缺失或 Token 不匹配。
- `404` 且响应头出现 `PalPanelSteamAPIProxy`：请求误发到 `18082`，改用 `18083`。
- `connect: connection refused`：Bridge 未监听。检查是否重启 PalServer，并查看 `PalPanelBridge.log`。
- PalServer 停在 `Starting C++ mod 'PalPanelBridge'`：DLL 与 UE4SS 的提交、构建配置或 CRT 不兼容。

启动诊断日志位于：

```text
Pal/Binaries/Win64/ue4ss/Mods/PalPanelBridge/PalPanelBridge.log
```

正常日志应包含：

```text
token_configured=true
listening on 127.0.0.1:18083
```

日志只记录配置文件路径、Token 是否已配置、监听端口和 Winsock 错误，不记录 Token 内容。
