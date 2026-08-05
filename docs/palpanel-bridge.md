# PalPanelBridge 安装与验证

`PalPanelBridge` 是 PalPanel 的 UE4SS C++ 只读通信探针，用于验证：

```text
PalPanel 后端 HTTP → PalPanelBridge → UE4SS on_update 游戏线程
```

当前版本不会修改玩家、背包、帕鲁或存档。

后续功能优先级、完成标准和 GitHub Actions 构建部署门禁统一记录在
[`development/palpanel-bridge-roadmap.md`](development/palpanel-bridge-roadmap.md)。

## 兼容版本

- PalPanelBridge：`0.1.28`
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
  "bridge_version": "0.1.28",
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

字段发现接口：

```text
POST http://127.0.0.1:18083/v1/players/online/metadata
```

任务结果带有 `metadata_probe: true`，每个已收集玩家可带有
`top_level_property_metadata: {"player_state": [...], "pawn": [...]}`；普通在线
任务不返回该大列表。

玩家结果始终 additive 返回 `character_parameter_found` 与
`character_parameter`（仅对象名、完整名和类名）。只有 metadata 任务且当前玩家
确实被选为收集对象时，才会增加
`detail_property_metadata: {"guild": [...], "character_parameter": [...]}`。
两组属性元数据分别从 `GuildBelongTo` 与 `Pawn.CharacterParameterComponent` 对象
按当前类和 `TSuperStructRange` 父类逐层枚举，使用 `IncludeDeprecated`，按小写属性名
匹配固定关键词并按名称去重，每个对象最多 64 项；不读取匹配属性值、数组、Map、嵌套
内容，也不调用未知函数。普通查询不执行这两组枚举。

`0.1.13` 从当前 World 的 `GameState.PlayerArray` 读取服务端维护的权威
PlayerState 数组，并返回 `game_state_found`、`game_state`、
`game_state_player_array_available`、`game_state_player_state_count` 和
`game_state_error`。由该数组发现的玩家项以
`source: "game_state_player_array"` 标记；原有 PalUtility 和全局对象扫描仅保留为
只读诊断回退。

`0.1.13` 使用 UE4SS `FScriptArrayHelper_InContainer` 和数组内部对象属性逐项解析
UE5 `TObjectPtr<APlayerState>`，避免把对象句柄误当成裸指针导致游戏进程崩溃。

`0.1.14` 在在线玩家结果中增加只读 `property_candidates`，分别列出 Controller、
PlayerState 和 Pawn 上名称含背包、容器、装备、物品、槽位、队伍或帕鲁关键词的
运行时属性。该探针只返回属性名，不读取属性值。

`0.1.15` 保留上述名称列表，并新增只读 `property_details`。每个候选项包含属性名、
粗粒度类型（`object`、`array`、`struct` 或 `other`），以及 UE4SS 可识别时的声明对象类、
数组元素类或结构体名。此版本仍不读取背包、装备或帕鲁容器内容，也不修改游戏对象；
这些类型信息用于确定下一步应安全读取的真实入口。

`0.1.16` 对 `object` 类型候选执行一次受限展开，增加 `object_value_found`、
`object_value` 和 `nested_candidates`。展开仅限一层且仍只读取反射元数据，主要用于确认
`BP_OtomoPalHolderComponent`、`LoadoutItemSelector` 等对象的实际实例及内部入口。

`0.1.17` 仅对已确认的 `PalItemSelectorComponent` 与 `BP_OtomoPalHolderComponent`
返回最多 96 个未经过关键词过滤的内部属性；其他对象继续使用原过滤规则。该探针用于发现
帕鲁队伍和装备数据的真实入口，仍不读取属性值、数组或容器内容。

`0.1.18` 为数组和 Map 增加经过范围校验的 `collection_count`，并为上述两个关键对象
增加最多 64 个关键词匹配的 `function_candidates`，包含函数名和参数缓冲区大小。
插件不会调用这些函数，也不会读取集合元素。

`0.1.19` 为每个函数候选增加 `parameters`，只返回参数名、类型、大小和返回值标记，
用于确认 `GetContainer`、`TryGetContainer` 与 `TryGetLoadedOtomoData` 的安全调用结构；
本版本仍不会调用这些函数。所有可读时间改为中国标准时间（UTC+8）。

`0.1.20` 将 UE 反射扫描移出 job 表互斥区。在线玩家扫描耗时时，任务会保持
`running`，但 `/v1/health`、`/v1/runtime` 和 `/v1/jobs/<job_id>` 不再被同一把锁
阻塞。由于 `0.1.19` 实机出现在线任务后整体 HTTP 无响应，函数参数遍历暂时停用；
所有函数候选保留空 `parameters` 数组以兼容响应结构。job 表满 64 条时只淘汰已完成
或失败的任务；若全部任务仍在排队或执行，新请求返回 `503 job_queue_full`，不会删除
运行中的任务。

`0.1.21` 新增独立的 `POST /v1/players/online/metadata` 字段发现接口。它复用在线
玩家枚举和身份读取，只对首名玩家的 PlayerState 与 Pawn 读取顶层 `FProperty`
元数据；每个对象最多描述 96 个属性，仅返回 `name`、`kind` 和 `declared_type`。
该接口不读取未知属性值、数组/Map 内容、嵌套结构，也不调用未知 UE 函数；它不是
位置、等级、公会等详细信息完成接口。

`0.1.25` 在 PlayerState 上保留只读 `CachedPlayerLocation` 与 `GuildBelongTo`，并增加
受限的公会和角色参数组件元数据探针；`0.1.24` 的字段行为保持不变。

`0.1.26` 增加只读玩家详细对象引用。普通在线查询 additive 返回 `guild_name`、
`guild_admin_player_uid`、`base_camp_count`、`inventory_found`/`inventory`
（`PalPlayerInventoryData`，附 `inventory_container_count`）、
`pal_storage_found`/`pal_storage`（`PalPlayerDataPalStorage`）、
`otomo_found`/`otomo`（`PalPlayerOtomoData`）。只读取对象身份、一个字符串、
一个 GUID 和有界集合规模，不枚举容器内容。metadata 探针新增
`detail_property_metadata.inventory`、`.pal_storage`、`.otomo` 关键词属性列表
（每个对象最多 64 项），并把 guild 关键词列表扩展 `base`、`camp`、
`territory`、`map` 以发现据点字段。匹配的属性值、数组、Map、嵌套内容和函数
仍然一律不读取。

`0.1.27` 把实机确认的字段名变成数值：`base_camp_count` 读取公会 `BaseCampIds`
数组长度，`base_camp_level` 读取数值 `BaseCampLevel`；背包重量在数值属性存在时
返回 `now_item_weight` 与 `max_inventory_weight`；帕鲁存储对象引用
`pal_container` 指向 `TargetContainer`（`PalIndividualCharacterContainer`），
metadata 探针新增 `detail_property_metadata.pal_container` 以便下一步发现帕鲁
槽位。仍然只读身份、字符串、GUID、数值标量和有界集合规模，不枚举帕鲁或物品
槽位内容。

`0.1.28` 开始读取帕鲁槽位并探测背包容器。在线玩家响应新增 `pal_slot_array`
（`found`、`slot_count` 和最多 10 个 `slots`，每个槽位含 `individual_id` 与
`Handle` 对象引用）来自 `PalIndividualCharacterContainer.SlotArray`；以及
`inventory_containers`：在 `PalPlayerInventoryData` 上按名称探测
`EssentialContainer`、`PlayerInventoryContainer`、`EquipmentContainer`、
`LoadoutContainer`、`ItemContainer`、`InventoryContainer`，每个容器返回对象
身份和 `Slots`/`ItemSlots` 数组规模。metadata 探针还会为每个找到的物品容器
返回 `container_property_metadata` 以确认真实槽位字段名。最多读取 10 个帕鲁
槽位和少数命名容器，物品槽位内容与单只帕鲁详情暂不读取。
位置字段仅接受完整类型名 `ScriptStruct /Script/CoreUObject.Vector`、12 或 24
字节属性，并对三个坐标执行有限值校验；失败时不输出伪位置值并返回
`cached_location_error`。公会对象仅在 `UObject::IsReal` 成功时返回。

`0.1.23` 修复元数据探针只看到蓝图当前类属性的问题：先枚举当前类，再用
UE4SS 已验证的 `TSuperStructRange` 逐层枚举父类，每层使用
`TFieldRange<FProperty>` 与 `IncludeDeprecated`。该修复仍只返回属性元数据，
不读取属性值，并在达到每个对象 96 项上限后立即停止；`metadata_truncated`
仍只表示玩家数截断，不表示字段截断。

## 响应与任务时间

所有 JSON 响应均包含 `response_time_unix_ms` 和中国标准时间格式的
`response_time_china`。任务详情同时包含：

- `queued_at_unix_ms` / `queued_at_china`：进入队列的时间。
- `executed_at_unix_ms` / `executed_at_china`：游戏线程开始执行该任务的时间。
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
