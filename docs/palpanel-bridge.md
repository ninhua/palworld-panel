# PalPanelBridge 安装与验证

`PalPanelBridge` 是 PalPanel 的 UE4SS C++ 本机通信插件，用于读取运行时数据，
并通过受限游戏线程任务修改背包数量和帕鲁数据：

```text
PalPanel 后端 HTTP → PalPanelBridge → UE4SS on_update 游戏线程
```

插件不开放任意 UObject 调用。修改必须带确认标记、目标身份和预期原值，写后立即
回读；不一致时尝试恢复原值。正式修改前仍必须先备份世界存档。

后续功能优先级、完成标准和 GitHub Actions 构建部署门禁统一记录在
[`development/palpanel-bridge-roadmap.md`](development/palpanel-bridge-roadmap.md)。

## 兼容版本

- PalPanelBridge：`0.1.35`
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
  "bridge_version": "0.1.35",
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
来源。还会以当前 World 为上下文只读调用 `PalUtility.GetAllPlayerStates` 作为回退。
当前版本返回 UID、Pawn、位置、公会/据点、背包容器及堆叠数、受限帕鲁槽位详情；
写接口仅支持下文列出的三个操作。

字段发现接口：

```text
POST http://127.0.0.1:18083/v1/players/online/metadata
```

任务结果带有 `metadata_probe: true` 和受限的 `detail_property_metadata`；普通在线
任务不返回反射诊断大列表。

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

`0.1.29` 增加本地实机诊断路径。在线玩家响应新增 `inventory_helper_found`/
`inventory_helper`（`PalItemContainerMultiHelper`，已确认的背包容器的真实入口
对象）；metadata 探针新增 `detail_property_metadata.inventory_helper` 与
`detail_property_metadata.pal_slot_object`（首个 `PalIndividualCharacterSlot`
对象的关键词属性），用于确认个体 ID 与物品槽位字段名后再读取实际帕鲁/物品
内容。

`0.1.30` 根据实机确认路径一次展开背包、帕鲁与据点诊断：背包容器从
`InventoryMultiHelper.Containers` 对象数组读取，返回最多 16 个容器及槽位数组
规模，并为首个容器、首个物品槽位返回属性元数据；帕鲁槽位区分槽位对象与真实
`Handle`，返回 `ReplicateHandleID` 的受限十六进制值，并展开
`ReplicateIndividualParameter`、Handle、ID 结构和 `BaseCampIds` 元素结构的
元数据。全部逻辑仍为只读，每次最多检查 10 个帕鲁槽位。

`0.1.31` 增加 `player_data_ready`/`player_data_state`：玩家刚进入且 UID、背包、
帕鲁存储尚未同步时明确返回 `initializing`，同步完成后返回 `ready`。背包槽位改读
实机确认的 `ItemSlotArray`；帕鲁详情改读 `CachedNonEmptySlots_InServer`，避免只取
960 槽位中的前 10 个空槽。`PalInstanceID` 仅解析内部 `PlayerUId` 和
`InstanceId` 两个 GUID，不输出含 `DebugName` 的整块原始内存。

`0.1.32` 为每个背包容器返回最多 32 个 `PalItemSlot`，读取数值
`SlotIndex`/`StackCount`；metadata 新增 `inventory_item_id_struct`，展开首个
`PalItemId` 的内部字段，为读取真实物品 ID 提供一次性结构证据。有效帕鲁参数
同时尝试读取昵称、等级、Rank、经验、HP/最大 HP、饱食度和理智，并过滤大量
Delegate 元数据，优先返回技能、词条、种类、等级和状态字段。

`0.1.33` 修复大响应只调用一次 Winsock `send` 导致 65528 字节后截断的问题，
改为循环发送完整响应。普通在线查询移除旧的 `property_candidates`/
`property_details`，物品槽位不再重复返回 UObject 名称；反射详情集中到 metadata
接口，面板查询脚本的诊断响应上限同步提高到 2 MiB。

`0.1.34` 根据实机确认结构读取每个受限背包槽位的 `PalItemId.StaticId`；对有效
帕鲁参数对象调用已确认的只读函数，返回 `character_id`、`level`、
`passive_skill_ids` 和数值 `equipped_waza_ids`。数组与数值均执行范围限制，不调用
任何修改函数。调用前还会核对反射返回类型、参数缓冲大小和枚举元素宽度；不匹配
时关闭该读取路径，不执行函数。

## 受限修改接口（0.1.35）

先调用 `/v1/players/online` 取得最新 UID、容器数组序号、槽位数组序号、物品 ID、
数量、帕鲁实例 ID、种类和被动列表；再备份世界存档。修改请求：

```text
POST http://127.0.0.1:18083/v1/mutations
GET  http://127.0.0.1:18083/v1/jobs/<job_id>
```

背包数量示例（`container_index`/`slot_index` 是返回数组中的序号）：

```json
{
  "operation": "item_set_count",
  "confirm": true,
  "player_uid": "32位玩家UID",
  "container_index": 0,
  "slot_index": 0,
  "expected_item_static_id": "Wood",
  "expected_stack_count": 994,
  "stack_count": 995
}
```

增加或删除被动词条（删除时 `add_passive=false`）：

```json
{
  "operation": "pal_replace_passive",
  "confirm": true,
  "player_uid": "32位玩家UID",
  "instance_id": "32位帕鲁实例ID",
  "expected_character_id": "BadCatgirl",
  "expected_passive_skill_ids": ["PAL_Sanity_Up_1"],
  "passive_skill_id": "待修改词条ID",
  "add_passive": true
}
```

修改等级或个体值：

```json
{
  "operation": "pal_set_stats",
  "confirm": true,
  "player_uid": "32位玩家UID",
  "instance_id": "32位帕鲁实例ID",
  "expected_character_id": "BadCatgirl",
  "field": "Talent_HP",
  "expected_value": 0,
  "value": 1
}
```

`field` 只允许 `Level`（1..80）、`Talent_HP`、`Talent_Shot`、
`Talent_Defense`（0..100）。任务结果的 `mutation_status` 为 `succeeded`、
`rejected`、`rolled_back` 或 `rollback_failed`，并附带 `before`、`after`、
`error` 和 `rollback_status`。任一目标身份或预期原值不一致都不会写入。
物品增删继续使用面板已有的 PalDefender 审计接口；它会产生 RCON 命令日志，
不属于 Bridge 直接修改。DLL 部署备份不等于世界存档备份。

本地 Windows 服务端可使用 `deploy.py --local-dll <main.dll路径>`。脚本会等待
对应 CI、校验构建包、通过面板停服、只原子替换 `dlls/main.dll`、失败恢复旧 DLL、
重新启动并等待健康检查返回目标版本；不会修改 `config.ini`。面板与 Bridge 密钥
通过参数或环境变量提供，不写入仓库；本地模式必须提供 Bridge Token，只有健康
检查确认目标版本后脚本才报告成功。

本地部署使用面板安全停服任务，不调用 Windows 强制 Stop。脚本在停服前和启动前
分别校验 `GameUserSettings.ini` 可读、`DedicatedServerName` 非空且对应世界存在
非空 `Level.sav`；校验失败时保持停服，禁止 PalServer 随机创建新世界。
Windows 本地服若仅在安全停服后把该 INI 重写为空 DACL，脚本只对这个确定文件恢复
父目录权限继承，然后再次核对世界 ID 未变化及 `Level.sav` 非空；不修改 INI 内容，
也不批量修改目录 ACL。

仅需验证 CI 和保存构建快照时使用 `--download-only`，该模式不会连接面板、不会
停服或替换 DLL。工作流从 `src/main.cpp` 的 `ModVersion` 自动生成压缩包及
Artifact 名称，避免版本号漏同步。
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
