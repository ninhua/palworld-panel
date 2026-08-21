# 功能移植更新记录

## 2026-08-21：PalPanelBridge 0.1.57 离线固定指派排队

- 0.1.56 由 `deploy.py` 完成 Action `32455516387`、SHA256
  `b0a6975b85308b36408014400775f9fbbfe678c0d7b58bec75cdd01b0a3aa4fe` 的构建、
  校验、部署和重启；`2026-08-21T14:45:34.091+08:00` 健康检查返回 0.1.56。
- 无玩家在线时 `/v1/bases/works` 返回 43 个工作，原生只读预检确认当前阿努比斯对 43
  个目标均存在固定指派槽，证明此前失败点是休眠据点没有工人 Pawn/AI，而非目标不兼容。
- 验证通过但工人 AI 未激活时，mutation 改为 `pending_activation`；插件在当前服务进程
  内保存精确请求，每 30 秒低频重试一个，成功后更新原任务结果并保留权威回读门禁。
- 重试具备幂等完成检测：若原生网络调用异步完成，下一次回读目标槽即转为成功，不重复
  写入。运行时 work ID 重启后变化，因此不跨服务器进程猜测恢复待处理请求。
- 三份文档和版本一次更新；不在本地编译测试，继续仅由 GitHub Actions 构建和
  `deploy.py` 等待、校验、部署及重启验证。

## 2026-08-21：PalPanelBridge 0.1.56 固定指派原生预检

- 0.1.55 由 `deploy.py` 完成 Action `32454711486` 构建、SHA256
  `61a546d7d45e271c2fe06dcf3587494c336293fa445c0515b66c14bd1e0ac5db` 校验、部署、
  重启；`2026-08-21T14:33:41.942+08:00` 健康检查返回 `bridge_version=0.1.55`。
- 离线读取仍为 1 个据点、1 只阿努比斯；随机选择的 `PalWorkCollectResource` 返回
  `offline worker AI fixed-assignment path unavailable`。仅按工作类名不能证明该目标支持
  固定指派，因此停止继续猜目标。
- `/v1/bases/works` 新增 `fixed_assignable_worker_instance_ids`，对同据点每个工人 Handle
  调用原生只读 `IsExistAssignableSlot(handle, true)`；面板可直接选择已验证兼容组合。
- mutation 在任何写调用前重复同一预检，不兼容时安全拒绝；其余身份、ABI、线程和回读
  门禁保持不变。不在本地编译测试，仅交由对应 GitHub Actions 和 `deploy.py` 验证。

## 2026-08-21：PalPanelBridge 0.1.55 离线工人 AI 固定指派

- 0.1.54 实机离线验证：Action `32453642469`、SHA256
  `83b55a884aeb090a81010beb10d49557cd53c4618026723dbe763a9a52b0bb3e` 已由
  `deploy.py` 校验、部署、重启；`/v1/health` 于 `2026-08-21T14:21:16.527+08:00`
  返回 `bridge_version=0.1.54`。
- 无玩家在线时读取到 1 个据点、1 只阿努比斯及 42 个工作对象；两次原生网络 RPC 分别
  指向 `PalWorkProgress` 与 `PalWorkCollectResource`，均安全返回“未回读到分配”，且延迟
  回读仍为 0，证明无客户端所有权时仅调用网络组件不足。
- 新增服务端 AI 兜底：通过据点槽 `Handle` 与动作返回的 `IndividualHandle` 指针严格唯一
  匹配 `PalAIActionCompositeWorker`，校验后调用原生 `RegisterFixedAssignWork(WorkId)` 并
  触发 `TryFindNextWork`；仍以目标工作 `GetAssignedCharacters` 回读作为唯一成功标准。
- 不构造无反射字段的 `PalWorkAssignRequirementParameter`，不直接写工作数组；版本和三份
  文档一次更新，仅交给 GitHub Actions 构建和 `deploy.py` 部署验证。

## 2026-08-21：PalPanelBridge 0.1.54 离线据点工作指派

- `base_assign_worker` 不再强制要求在线玩家；`player_uid` 改为可选兼容字段。
- 无在线控制器时，先调用 `PalUtility.GetNetworkTransmitter` 获取服务端 World 发射器；
  若不可用，再从已加载对象中只接受唯一、存活、非默认对象且暴露原生固定指派 RPC 的
  `PalNetworkBaseCampComponent`；候选为零或多个时安全拒绝，不猜测目标。
- 保留据点、工作、帕鲁槽、owner/实例 GUID、预期分配数量、反射 ABI、游戏线程及
  `GetAssignedCharacters` 回读验证，未确认成功时不报告成功。
- 显式提供 `player_uid` 时仍要求唯一在线控制器；身份解析失败直接拒绝，不会静默降级
  为全局离线权限。
- README、接口文档、版本号和中文日志一次性同步；不在本地编译或测试，仅由对应
  GitHub Actions 构建，再由 `deploy.py` 等待结果、校验、部署、重启并验证。

## 2026-08-21：PalPanelBridge 0.1.53 据点工作固定指派

- 新增精简只读 `POST /v1/bases/works`，返回当前据点 ID，以及有界工作对象的
  `work_id`、运行时类、`base_id`、设施对象 ID 和已分配角色数量；详细反射信息仍留在
  `/v1/bases/modules`。
- 新增受限 `base_assign_worker` 游戏线程 mutation。请求必须提供在线调用者 UID、据点/
  工作/帕鲁 GUID、帕鲁槽 owner UID、预期当前分配数量及 `confirm=true`。
- 使用游戏原生 `RequestFixedAssignWorkInBaseCamp_ToServer`，调用前唯一核对在线控制器、
  `PalNetworkBaseCampComponent`、WorkerDirector、帕鲁槽和工作对象，并严格校验反射 ABI；
  调用后仅在 `GetAssignedCharacters` 立即回读到目标槽时报告成功。
- 不猜测 `PalWorkAssignRequirementParameter`，不直接写任务数组；回读未确认时不自动调用
  unassign，避免异步成功后被错误撤销。
- 版本、README、接口文档和中文更新日志一次性同步为 `0.1.53`；不在本地编译或测试，
  仅由 GitHub Actions 构建。构建、部署和实机响应待 CI 后补充。

## 2026-08-20：PalPanelBridge 0.1.52 主世界选择修复

- 复现服务器启动后 `/v1/bases/workers` 返回
  `PalUtility.GetBaseCampManager ABI mismatch`；同一时刻 `/v1/world` 显示误选
  `/_Generated_/MainGrid_L6_X-1_Y0_DL0.PL_MainWorld5` 子 World。
- 新增有界 `World` 类定向选择器，优先 Pal 主世界、非 Generated 路径和有效
  `GameState`；Generated 子世界硬过滤且不回退任意 World。据点模块、据点工人、世界
  和在线玩家任务统一使用，不缓存对象指针。
- 版本、README 和接口文档一次性同步为 `0.1.52`；不在本地编译，仅由 GitHub Actions
  构建。

构建与部署结果（2026-08-20 22:36，中国时区）：GitHub Actions run
`32380912814` 成功；DLL SHA256 为
`54b55bba9663e5eae231a304a5f67b44be7488cb13fea63fb2912b72e5b03831`。`deploy.py`
完成快照保存、旧 DLL 备份、原子替换、面板重启及 0.1.52 健康检查，`config.ini`
保持不变。

实机结果：在服务器刚重启且无人上线的原复现条件下，`/v1/world` 正确返回
`World /Game/Pal/Maps/MainWorld_5/PL_MainWorld5.PL_MainWorld5`，不再命中 Generated
子世界；`/v1/bases/workers` 无错误返回据点
`4D89F67843C3C9655D4C02B314977A92`、1 个 Worker，以及 50 级 `Anubis`
`5C47471E455508BF80A131A6643551B9`。

## 2026-08-19：PalPanelBridge 0.1.51 工作适应性枚举 ABI 修复

- 0.1.50 实机精简接口成功返回 1 个据点和 1 只 `Anubis`，响应无截断/错误，但
  `work_suitabilities` 为空；确认原 `FByteProperty` 类型门禁未匹配本服枚举反射。
- 输入改为“唯一非返回参数且尺寸严格为 1 字节”，兼容 `FEnumProperty`；返回等级只
  接受 `byte` 或 `int32`，仍保留参数数量、缓冲区大小和等级范围门禁。
- 版本和三份文档同步为 `0.1.51`；不在本地编译，仅由 GitHub Actions 构建。

构建与部署结果（2026-08-19 17:05，中国时区）：GitHub Actions run
`32235581758` 成功；DLL SHA256 为
`e78f64974ac8d86ecc91dff909890944f4944ad546b6cced7e9dd095ae1f70cb`。`deploy.py`
完成快照保存、旧 DLL 备份、原子替换、面板重启和 0.1.51 健康检查，`config.ini`
保持不变。

实机结果：`/v1/bases/workers` 完成且无错误，返回据点
`4D89F67843C3C9655D4C02B314977A92`、1 个 Worker、1 个 WorkerTask。测试阿努比斯
`5C47471E455508BF80A131A6643551B9` 为 50 级，运行时工作适应性仅返回
`Handcraft=10`、`Mining=10`、`Transport=10`，`current_works=[]`。由此确认
PalDefender `ExtraWorkSuitabilities` 只强化该帕鲁原生工作类型，不能新增缺失类型。

## 2026-08-19：PalPanelBridge 0.1.50 据点帕鲁精简读取

- 新增 `POST /v1/bases/workers`，只读取据点 ID、WorkerDirector 槽、WorkerTask 和实际
  分配关系，不返回模块/函数/属性 metadata，解决面板代理 65536 字节截断。
- 对每只据点帕鲁有界调用 `GetWorkSuitabilityRankWithCharacterRank`，不兼容时回退
  `GetWorkSuitabilityRank`，读取 13 类工作适应性的正数等级。
- 通过 `PalWorkBase.GetAssignedCharacters()` 的槽对象身份关联 `current_works`；不把
  WorkerTask 类名误当成当前工作，不调用任何据点写函数。
- 实机升级前已在 0.1.49 直接 Bridge 响应确认：据点
  `4D89F67843C3C9655D4C02B314977A92` 当前有 1 只 `Anubis`，实例
  `5C47471E455508BF80A131A6643551B9`，等级 50。
- 版本、README 和接口文档一次性同步为 `0.1.50`；不在本地编译，仅由 GitHub Actions
  构建。构建、部署和实机响应待本次 CI 后补充。

## 2026-08-19：PalPanelBridge 0.1.49 据点工人、分配请求与任务读取

- 从 `PalBaseCampWorkerDirector.CharacterContainer.SlotArray` 使用既有有界帕鲁槽读取器
  返回据点工人；复用 Handle/参数/实例 ID ABI 门禁，最多输出 10 槽详情。
- 展开 `RequiredAssignWorks` 的 `PalBaseCampWorkAssignRequest` 结构；读取最多 16 个
  `WorkerTasks` 实例的相关属性和函数 metadata，不调用任务函数。
- 版本、README 和接口文档同步为 `0.1.49`；不在本地编译。

构建与部署结果（2026-08-19 16:25，中国时区）：GitHub Actions run
`32232398142` 成功；DLL SHA256 为
`603cf36cec580565161d45caba6b27e91a4a7ce90a042f0aeb475989c9d7e77e`。

实机结果：任务完成且无截断/错误；WorkerDirector 的角色容器有效，`SlotArray` 数量为
1，但当前槽没有 Handle 或参数对象（多次重启后尚未加载实际据点工人）；
`RequiredAssignWorks` 数量仍为 23，结构字段本版未展开；`WorkerTasks` 读取到 1 个
`PalBaseCampWorkerTask_IgnitionTorchAtNight`。下一步在玩家上线并靠近据点后复测工人槽，
同时改用 `GetCharacterHandleSlots` 和完整结构字段枚举。

## 2026-08-19：PalPanelBridge 0.1.48 定向 Worker/Assign 探针

- 删除全局 UObject 遍历，改为定向查找 `PalBaseCampWorkerDirector`、Battle、
  `PalBaseCampWorkCollection`、ReplicationList、GroupedWork Base/Farm 和
  `PalWorkAssign` 七类实例；最多返回 32 项。
- 保留 `/Game/`、CDO 与只读 metadata 门禁；版本和三份文档同步为 `0.1.48`，不在
  本地编译。

构建与部署结果（2026-08-19 16:20，中国时区）：GitHub Actions run
`32231986709` 成功；DLL SHA256 为
`0c6238b90fdbb86630f6e9e4ac48ab676636d69a5ca2af7c0cbaa64788757e9a`。

实机结果：任务约 2 秒完成、无截断、无错误；读取 4 个实例。WorkerDirector 的
`CharacterContainer` 有效，`RequiredAssignWorks=23`、`WorkerTasks=1`；WorkCollection
的 `WorkIds=30`。确认 `GetCharacterHandleSlots`、`OnRequiredAssignWork_ServerInternal`
与 `OnNotifiedUnassignWork_ServerInternal` 精确 ABI，进入 0.1.49 读取实际工人与请求结构。

## 2026-08-19：PalPanelBridge 0.1.47 世界运行对象过滤

- Worker/Assign 候选完整路径必须属于 `/Game/`，排除 `/Script` 下的 Enum、Function、
  DelegateFunction、ScriptStruct 等反射定义；其余 32 项上限和只读约束不变。
- 版本、README 和接口文档同步为 `0.1.47`；不在本地编译。

构建与部署结果（2026-08-19 16:15，中国时区）：GitHub Actions run
`32231518606` 成功；DLL SHA256 为
`7f5530c3d17502bc4f03e2daedc6173d15e07dc404e951a779a6fc1e81c87501`。

实机结果：全局 UObject 遍历在游戏线程运行超过 60 秒且任务无法结束；已立即通过面板
API 重启恢复服务器，未执行写操作。该实现判定不可接受，0.1.48 改为已知类名定向查找。

## 2026-08-19：PalPanelBridge 0.1.46 据点 Worker 运行实例探针

- 过滤 `class_name=Class` 的反射类定义和 `Default__` 类默认对象，只保留运行时
  Worker/Assign/Facility 候选实例，避免占满 32 项上限。
- 版本、README 和接口文档同步为 `0.1.46`；不在本地编译，仍不调用候选函数。

构建与部署结果（2026-08-19 16:11，中国时区）：GitHub Actions run
`32231215876` 成功；DLL SHA256 为
`3303edc420a487380dc657e5a95e33d80dd579938b8957c5b78a72426da82986`。

实机结果：过滤 Class/CDO 后仍有 32 个 `/Script` 反射对象占满上限，包括
`FindWorkAssignableObject`、`GetWorkAssign`、`PalBaseCampWorkAssignRequest`、
`GetTargetBaseCampWorkerCharacterContainer` 和 `CollectBaseCampWorkerInfo`；进入 0.1.47
只保留 `/Game/` 世界运行对象。

## 2026-08-19：PalPanelBridge 0.1.45 据点 Worker/Assign 对象探针

- 在游戏线程有界扫描已加载对象，最多返回 32 个类名/对象名匹配据点 Worker、Assign、
  Facility 或 `PalWorkAssign` 的候选；同类仅首个带相关属性和函数 ABI。
- 只读 metadata，不调用候选函数；与现有模块/工作 metadata 共享 4096 节点预算。
- 版本、README 和接口文档同步为 `0.1.45`；不在本地编译。

构建过程：首次 run `32190089731` 因误用 `RC::Unreal::LoopAction` 编译失败；只按失败
步骤日志改为 SDK 实际的 `RC::LoopAction` 后，run `32190313188` 成功。DLL SHA256 为
`c28f1e78b38657530be4cce5dbb92020a5ba3fd3e38ef6ba3d733f98d3988766`，于
2026-08-19 16:07（中国时区）安全部署并重启。

实机结果：响应未截断，但 32 项候选均为反射类定义；确认关键类名包含
`PalBaseCampWorkerDirector`、`PalBaseCampWorkCollection`、
`PalBaseCampWorkCollectionReplicationList` 与 `PalBaseCampGroupedWorkBase`，进入
0.1.46 过滤类定义并读取实际实例。

## 2026-08-19：PalPanelBridge 0.1.44 活动设施工作探针

- 保留当前据点映射工作；其他已加载工作仅当 `GetWorkAssignInfo` 或
  `GetAssignedCharacters` 返回非零数量时输出，用于定位真实活动设施和工作帕鲁。
- 空且与据点资源 Map 无关的工作对象不进入响应，保持 65536 字节以内；仍不调用写函数。
- 版本、README 和接口文档同步为 `0.1.44`；不在本地编译。

构建与部署结果（2026-08-19 05:50，中国时区）：GitHub Actions run
`32189642382` 成功；DLL SHA256 为
`8248ac26d59a48838a6779d9ecfa22ae32a7e6ac102cb4b12611364454473437`。

实机结果：响应未截断、无错误；仍返回 33 个据点资源工作，未发现分配信息或角色数量
非零的其他 `PalWorkBase`。说明当前自动劳动/据点帕鲁安排不在这两个工作 getter 中，
进入 0.1.45 查找 `PalWorkAssign` 与据点 Worker 管理对象。

## 2026-08-19：PalPanelBridge 0.1.43 据点工作响应收敛

- 仅保留当前据点 33 条 `MapObjectWorkInfoMap` 能匹配到的工作实例，过滤全世界无关
  `PalWorkBase`，避免面板诊断响应超过 65536 字节；只读 ABI 与数量门禁不变。
- 版本、README 和接口文档同步为 `0.1.43`；不在本地编译。

构建与部署结果（2026-08-19 05:47，中国时区）：GitHub Actions run
`32189334772` 成功；DLL SHA256 为
`e68cfe82743bd2fea5cf49a1a88177f760ea2563a515786fcdecebcc8be57f3c`。

实机结果：响应未截断、无 ABI 错误；1 个据点的 33 条工作全部匹配，包含 27 个
`PalWorkDeforestFoliage` 和 6 个 `PalWorkCollectResource`。这 33 项当前分配信息与
已分配角色数量均为 0；进入 0.1.44 定位 Map 之外的非零活动设施工作。

## 2026-08-19：PalPanelBridge 0.1.42 据点工作与分配只读关联

- 最多读取 128 个已加载 `PalWorkBase` 实例，不再仅按类保留一个样本；通过完整
  `FGuid` ABI 校验调用 `GetWorkId`，用于关联 0.1.41 的设施对象工作映射。
- 严格校验并调用只读 `GetWorkAssignInfo(out PalWorkAssignInfo[])` 与
  `GetAssignedCharacters(out PalIndividualCharacterSlot[])`；数组各限 256 项，返回数量、
  借用槽对象身份及每类首个结构/槽位 metadata，不持有或释放游戏对象。
- 不调用分配、取消分配、设施使用或生产 setter；版本与三份文档同步为 `0.1.42`，
  不在本地编译。

构建与部署结果（2026-08-19 05:43，中国时区）：GitHub Actions run
`32188989447` 成功；DLL SHA256 为
`b6043f791912dad4589f9ef911464fbc6d7f827f3e3683680e7f15aabacb0aa9`。

实机结果：`GetWorkId`、两个只读 out-array getter 均通过 ABI 门禁；
`PalWorkAssignInfo` 字段确认为 `LocationIndex` 与 `WorkAssign`。设施 WorkId 可关联到对应
`map_object_id`，当前已看到的分配数量为 0。由于同时返回全世界 128 个工作对象，面板
诊断响应在 65536 字节截断，进入 0.1.43 做最小范围收敛。

## 2026-08-19：PalPanelBridge 0.1.41 据点工作条目与工作对象探针

- 严格校验 `MapObjectWorkInfoMap` 的键、值及 `WorkId` 均符合已确认 ABI 后，有界读取
  最多 256 条 `map_object_id -> work_id`，数量或结构异常时关闭路径。
- 新增 `loaded_work_objects`，按类去重返回最多 64 个已加载 `PalWorkBase` 样本的相关
  属性和函数 ABI，与据点模块共享 4096 metadata 节点预算。
- 仍为只读探针，不调用设施使用、生产或帕鲁分配函数；版本、README 和接口文档一次性
  同步为 `0.1.41`，不在本地编译。

构建与部署结果（2026-08-19 05:31，中国时区）：GitHub Actions run
`32187942626` 成功；DLL SHA256 为
`7b0378524559f92047aeef385836c6fc77dbf4dc5f9ab6cac6b3d505756f34ec`。

实机结果：据点 `4D89F67843C3C9655D4C02B314977A92` 返回完整 33 条
`map_object_id -> work_id`，无截断、无错误；已加载工作类包括资源采集、运输、伐木、
等级对象采集和工作进度。共同只读入口确认为 `GetWorkId() -> Guid`、
`GetWorkAssignInfo(out PalWorkAssignInfo[])` 与
`GetAssignedCharacters(out PalIndividualCharacterSlot[])`，据此进入 0.1.42。

## 2026-08-19：PalPanelBridge 0.1.40 据点工作信息结构探针

- 在 0.1.39 Map 键值类型基础上，只读展开一层数组/结构体字段；metadata 输出递归
  固定最多 3 层，每个结构最多 32 个字段，并设置每任务 4096 节点总预算；超限返回
  `truncated=true`。
- 目标是确认 `PalBaseCampModuleResourceCollectWorkInfo` 内的工作对象、分配与状态字段，
  仍不遍历 Map 条目或调用工作函数。
- 版本、README 和接口文档一次性同步为 `0.1.40`；不在本地编译。

构建与部署结果（2026-08-19 05:45，中国时区）：GitHub Actions run
`32187084150` 成功；DLL SHA256 为
`4898ab8edf790b9b3c03ae6d0ab2562a50ba86d42be59c620afd4afd6d309175`。

实机结果：`PalBaseCampModuleResourceCollectWorkInfo` 只有 `WorkId: Guid`；因此 33 条
`MapObjectWorkInfoMap` 记录表示“地图设施对象 GUID → WorkId GUID”。
`FacilityUsageInfoSetMap.InfoMap` 与 `FacilityCounts` 当前均为 0；本版尚未读取 Map 条目。

## 2026-08-19：PalPanelBridge 0.1.39 据点工作 Map 类型探针

- 在 0.1.38 据点模块结果上增加数组/Map 有界元素数量、Map 键值粗粒度类型与声明
  类型；仅展开一层且固定反射限深，不遍历未知条目内存。
- 目标是确认 `MapObjectWorkInfoMap`、`FacilityUsageInfoSetMap`、`FacilityCounts`
  的键值结构，随后才能按确定 ABI 读取实际工作台与分配记录。
- 本版仍不调用 `OnStartUseFacility_ServerInternal` 等候选函数，不设置工作安排。
- 版本、README 和接口文档一次性同步为 `0.1.39`；不在本地编译。

构建与部署结果（2026-08-19 05:15，中国时区）：GitHub Actions run
`32186555233` 成功；DLL SHA256 为
`a43b5cffce475320f41c0081eb4c817c3198ebc53455305b9f9c1eaa0de6e0e5`。

实机结果：`MapObjectWorkInfoMap` 有 33 条，类型为
`TMap<FGuid, PalBaseCampModuleResourceCollectWorkInfo>`；`FacilityUsageInfoSetMap` 当前
0 条，值类型为 `PalBaseCampFacilityUsageInfoSet`；尚未读取条目或执行工作安排。

## 2026-08-19：PalPanelBridge 0.1.38 据点设施模块探针

- 新增只读 `POST /v1/bases/modules`，在游戏线程解析
  `PalUtility.GetBaseCampManager -> GetBaseCampIds -> TryGetModel -> ModuleArray`。
- 据点最多 32 个、每据点模块最多 64 个；数量或反射 ABI 不匹配时关闭路径。
- 每个模块只返回对象身份，以及工作、任务、设施、分配、生产、配方、建造相关属性
  与函数参数元数据；不调用未知模块函数，不改任务或帕鲁分配。
- 已确认参考源码没有可靠的工作台任务/分配 setter 或服务器 RPC；本版先取得实机
  工作台类与函数证据，再决定后续写接口。
- 版本、README 和接口文档一次性同步为 `0.1.38`；不在本地编译。

构建与部署结果（2026-08-19 05:08，中国时区）：GitHub Actions run
`32185914699` 成功；DLL SHA256 为
`df59c9f01a44fff8e3eebc4e5ebe2b8b73536bb57181be14ef001864102e61f5`。

实机结果：读取 1 个据点 `4D89F67843C3C9655D4C02B314977A92`、9 个模块；发现
`PalBaseCampModuleResourceCollector.MapObjectWorkInfoMap`、
`PalBaseCampModuleFacilityReservation.FacilityUsageInfoSetMap`，以及候选服务器函数
`OnStartUseFacility_ServerInternal(Model, IndividualHandle)`、
`OnFinishUseFacility_ServerInternal(Model)`。当前仍缺 Map 条目类型与实际 Model，未写入。

## 2026-08-19：PalPanelBridge 0.1.37 携带帕鲁与终端帕鲁分离

- 在线玩家结果新增 `party_pal_slots`/`party_pal_error`，从目标玩家所属
  `PalOtomoHolderComponentBase` 读取最多 20 个携带槽位。
- 携带路径严格使用 `GetMaxOtomoNum`、`GetOtomoIndividualHandle`、
  `TryGetIndividualParameter`、`GetPalId.InstanceId`，逐项校验反射 ABI。
- 帕鲁修改新增必填 `pal_scope=party|storage`，两种位置不互相回退；当前召唤中的
  携带帕鲁安全拒绝，要求先收回。
- 实机字段探针只在 `PalPlayerOtomoData` 发现容器 ID，未猜测不存在证据的队伍数组；
  使用已验证的 Holder 函数链定位真实携带对象。
- 明确 `item_set_count` 仅运行时有效，不能作为持久化接口；物品持久增减继续走
  游戏原生/面板审计路径，不使用负数添加或猜测删除函数。
- 版本、README 和接口文档一次性同步为 `0.1.37`；不在本地编译。

构建与部署结果（2026-08-19 04:49，中国时区）：GitHub Actions run
`32184234018` 成功；DLL SHA256 为
`62b0a1930d52f4600a4ad94a9c2e7a465c3a563ddee8f47264fa4a53ec88df84`。
`deploy.py` 保存快照、备份旧 DLL、保留 `config.ini`，并通过面板安全重启。

实机结果（2026-08-19 04:52，中国时区）：

- 正确读取人物携带队伍 5 个槽位中的 1 只帕鲁：槽位 0、实例
  `E91537EF4A8915B81A397D95E7160140`、`AmaterasuWolf`、等级 1、
  被动 `Deffence_up1`；终端非空缓存为 0，确认两条路径已分离。
- 使用 `pal_scope=party` 成功完成等级 `1→2→1`、HP 个体值 `34→35→34`、
  被动删除后加回；最终全部恢复，未触碰终端帕鲁。
- 按用户要求再次修改并保留结果：携带 `AmaterasuWolf` 等级 `1→2`、HP 个体值
  `34→35`、删除 `Deffence_up1`；三项回读成功并调用服务器 Save，不再恢复原值。
- 用户已在游戏内确认上述携带帕鲁变化可见。
- 测试前备份 `20260818T205221.265196900Z-manual.zip` 成功。
- 据点调查已确认可从 `PalBaseCampManager -> BaseCampModel.ModuleArray` 读取设施模块；
  尚未找到工作台任务或帕鲁分配的可靠服务器 setter/RPC，当前不执行未知字段写入。

## 2026-08-19：PalPanelBridge 0.1.36 帕鲁槽位 owner UID 兼容

- 0.1.35 实机已确认背包由 Bridge 直接完成 Wood `994→995→994`，两次回读均成功，
  最终数量恢复；没有调用 RCON。
- 帕鲁等级测试在写入前安全拒绝：完整槽位中的实例 ID 与种类正确，但 owner UID 为
  全零，旧条件只接受玩家 UID，因此未发生写入。
- 定位仍先限定在线玩家自己的 `PalStorage`，仅额外允许实机使用的全零 owner UID；
  实例 ID、`character_id`、原属性值或完整原词条列表仍必须精确匹配。
- 世界存档备份：`20260818T201935.957091600Z-manual.zip`。
- 版本、README 和接口文档一次性同步为 `0.1.36`；不在本地编译。

构建与部署结果（2026-08-19 04:28，中国时区）：GitHub Actions run
`32181997917` 成功；DLL SHA256 为
`0db5220869ce557646efa5985b12c9ffd93108d5f16a7e7f39fbde0533112501`。
`deploy.py` 保存快照、备份旧 DLL、保留 `config.ini`，并通过面板安全重启。

实机结果（2026-08-19 04:34，中国时区）：

- 帕鲁等级成功 `1→2→1`；HP 个体值成功 `79→80→79`。
- 被动词条 `PAL_Sanity_Up_1` 成功删除并加回；最终等级、个体值和词条均恢复。
- 最终在线读取：等级 1、被动 `[PAL_Sanity_Up_1]`、Wood 994。
- 0.1.35 的 Wood `994→9999` 运行时回读成功且客户端可见，但执行 Save 并重启后
  恢复为 994，确认直接写 `StackCount` 没有进入持久化链；后续改用游戏原生增减 API。
- 备份任务首次被 PalServer 重写的 39 个空 ACL INI 阻断；仅恢复这些确定文件的
  父级继承后，备份 `20260818T203441.870956000Z-manual.zip` 成功。

## 2026-08-19：PalPanelBridge 0.1.35 受限写任务

已完成：

- 新增 `POST /v1/mutations`，所有操作进入 UE4SS `on_update` 游戏线程队列。
- `item_set_count` 修改既有槽位数量；强制核对在线玩家 UID、容器/槽位数组序号、
  `item_static_id`、原数量和 0..9999 范围，写后回读，失败恢复原数量。
- `pal_replace_passive` 支持增加/删除单个被动词条；强制核对玩家 UID、帕鲁实例 ID、
  `character_id` 和完整原被动列表，调用前校验反射 ABI，失败执行逆操作并再次回读。
- 帕鲁定位优先使用服务端非空槽缓存，缓存未命中时回退完整 `TargetContainer.SlotArray`。
- `pal_set_stats` 首批只开放等级与 HP/攻击/防御个体值；强制核对实例身份和原值，
  仅写 `SaveParameter` 的 Byte 字段并调用零参数 `OnRep_SaveParameter`。
- 请求必须 `confirm=true`；结果区分 `rejected`、`succeeded`、`rolled_back`、
  `rollback_failed`，保留修改前后值及错误；插件日志记录操作、玩家 UID 和结果状态。
  正式修改前必须另行备份世界存档。
- 物品增删继续走现有 PalDefender 审计接口。2026-08-19 03:54（中国时区）的
  Wood `994→995→994` 回归测试使用该接口，因此服务器出现 `delitems` RCON 日志；
  最终数量恢复，该测试不是 Bridge 直接写入。
- 版本、README 和接口文档一次性同步为 `0.1.35`。

构建与部署结果（2026-08-19 04:16，中国时区）：GitHub Actions run
`32181105388` 成功；DLL SHA256 为
`3bf4c1193e642d2459a56e254cc18f4b7c8e4c3d04911e446e28c6d246471e2a`。
`deploy.py` 保存版本快照与旧 DLL 备份，保留 `config.ini`，通过面板安全重启；
`/v1/health` 已确认 `bridge_version=0.1.35`、UE4SS 与游戏线程正常。
重启后 04:17 查询在线人数为 0，尚未执行 Bridge 写入；等待玩家重新进入后做净零验证。

## 2026-08-19：PalPanelBridge 0.1.34 物品 ID 与帕鲁技能批量读取

已完成：

- 背包槽位沿已确认的 `PalItemId.StaticId` 路径读取物品静态 ID，与 `slot_index`、`stack_count` 同项返回。
- 对有效 `PalIndividualCharacterParameter` 集中调用已确认的只读函数：`GetCharacterID`、`GetLevel`、`GetPassiveSkillList`、`GetEquipWaza`。
- 帕鲁结果新增 `character_id`、`level`、`passive_skill_ids`、`equipped_waza_ids`；数组限制最多 16 项，等级限制 0–1000，不调用修改函数。
- 每次调用前校验反射返回类型、参数缓冲大小与主动技能枚举宽度；ABI 不一致时关闭读取路径，不执行函数。
- 首次 CI 在 UE4SS beta SDK 中因 `FEnumProperty` 不满足 `CastField` 约束而失败；最小修复仅移除该不可用类型转换，保留数组返回类型、参数大小与 2 字节元素宽度校验。
- 版本、README、接口文档一次性统一为 `0.1.34`。

验证计划：

- GitHub Actions 成功后安全部署，在线验证主背包非空槽的物品 ID、首只有效帕鲁的种类、等级、被动与主动技能。

构建与部署结果（2026-08-19 03:42，中国时区）：首次 Action `32177655824` 因上述 SDK 类型转换失败；最小修复后 Action `32177850831` 成功，SHA256 为 `6d4f5024dfd5618897579bb0fbc382de9ca85d60b2e8dc3bb2df6ae4c388b2ad`。首次部署在安全停服后因游戏把 `GameUserSettings.ini` 重写为空 DACL 而按保护策略保持停服，未覆盖 0.1.33 DLL；恢复该单文件继承并确认仍绑定 `D755E4CC4E9B23F85AE4E2B865E19DBD` 后，0.1.34 原子替换、备份、启动和健康检查成功。部署脚本随后增加同一单文件的受限 ACL 自动恢复与世界 ID 二次校验，避免再次人工处理中断。

实机结果（2026-08-19 03:45，中国时区）：在线任务 `players_1787082338892_1` 完成，玩家 `tiantian` 数据为 `ready`、据点数 1。主背包非空槽读取成功：`Wood ×994`、`BerrySeeds ×1`、`Fiber ×1`、`PalSphere_Ancient_1 ×100`、`Stone ×999`。帕鲁实例 `746BB6C84A4D2045267D388D9CBF6AC8` 返回 `character_id=BadCatgirl`、`level=1`、被动 `PAL_Sanity_Up_1`、主动技能枚举值 `160`；物品 ID 和四个帕鲁只读函数均通过运行时 ABI 门禁。

面板数据修复（2026-08-19 03:32，中国时区）：本地开发启动原先未运行 `sav-cli`，导致 `/api/save/index/status` 为 `disabled`。已从仓库最新 Windows Release 下载并校验完整包，只提取 `sav-cli.exe` 接入本地 `start-dev.ps1`/`stop-dev.ps1`，启用只读索引并配置 Bridge 环境变量。重建后索引为 `ready`：玩家 1、公会 1、据点 1、帕鲁 2、容器 64、地图实体 31，无解析警告。

## 2026-08-19：PalPanelBridge 0.1.33 大响应完整发送

已完成：

- 定位在线玩家任务响应在 65528 字节处截断：单次 Winsock `send` 允许部分发送，原实现未继续发送剩余内容。
- HTTP 响应改为循环发送至完整结束；失败时记录已发送字节数、总字节数及 Winsock 错误。
- 普通在线查询移除旧的 `property_candidates`/`property_details`，背包槽位去除重复 UObject 名称；反射详情仅由 metadata 接口返回。
- 面板查询脚本的响应保护上限从 64 KiB 提高到 2 MiB；版本、README 和接口文档一次性统一为 `0.1.33`。

验证计划：

- 仅通过 GitHub Actions 构建；部署后验证超过 64 KiB 的任务 JSON 可完整解析，并继续确认背包 `PalItemId` 与帕鲁参数真实字段。

实际结果（2026-08-19 03:16，中国时区）：Action `32175315223` 构建及本地安全部署成功，健康检查为 `0.1.33`；在线任务 `players_1787080601720_1` 完整解析，不再在 65528 字节截断。玩家 `tiantian` 数据为 `ready`，UID、Pawn、位置、公会、据点 1/等级 1、6 个背包容器和重量均有效；主背包读到堆叠数 `100/999/994/1`。帕鲁仓库为 960 槽，首个实例 `746BB6C84A4D2045267D388D9CBF6AC8` 的 Handle 与参数对象有效。metadata 任务 `players_1787080622209_2` 确认物品 ID 结构为 `StaticId + DynamicId`，帕鲁数值位于 `SaveParameter` 结构；下一版按这两个稳定入口集中读取，避免继续逐字段猜测。

## 2026-08-19：本地世界绑定恢复与部署停服保护

事故与根因：

- 0.1.32 部署前使用普通 `/api/server/stop`；Windows 后端实际通过 `TerminateJobObject`/`Kill` 强制结束 PalServer，不是游戏内保存退出。
- `GameUserSettings.ini` 当时 ACL 为受保护空 DACL `D:PAI`，重启进程无法读取原 `DedicatedServerName=D755E4CC4E9B23F85AE4E2B865E19DBD`，于 02:52 创建新世界 `5AEE8D9446F10BC85FF5B8B0EAAB311C`。
- 原世界、玩家文件和自动备份均未被覆盖；旧世界最后正常保存时间为 02:49。

恢复与修复：

- 通过面板 `/api/server/safe-stop` 等待任务完成；将两套世界、`PalWorldSettings.ini`、原 ACL 和 `GameUserSettings.ini` 备份到 `E:\PalPanelRuntime\recovery\world-restore-20260819-0301`。
- 重置单个 `GameUserSettings.ini` ACL 后确认其内容仍绑定旧世界；启动后游戏 REST API 返回 `worldguid=D755E4CC4E9B23F85AE4E2B865E19DBD`，旧世界恢复成功。
- `deploy.py --local-dll` 改用安全停服任务；停服前及启动前双重校验 `DedicatedServerName` 和对应非空 `Level.sav`。绑定不可验证时保持停服，不再自动启动新世界。

## 2026-08-19：PalPanelBridge 0.1.32 物品槽位数量读取

已完成：

- 每个背包容器读取最多 32 个 `ItemSlotArray` 对象，返回槽位对象、`SlotIndex` 与 `StackCount`。
- metadata 新增 `inventory_item_id_struct`，展开首个 `PalItemId` 内部字段；不猜测或复制可能含非平凡对象的整块结构内存。
- 有效帕鲁参数尝试读取昵称、等级、Rank、经验、HP/最大 HP、饱食度和理智；参数 metadata 排除 Delegate，优先保留种类、技能、被动、工作适应性和状态字段。
- 版本、README、接口文档一次性统一为 `0.1.32`。

验证计划：

- CI 成功后部署并在 `player_data_state=ready` 时确认各容器槽位数量、堆叠数和 `PalItemId` 字段。
- 当前角色的 `CachedNonEmptySlots_InServer` 仍返回 0，因此以实际 `SlotArray` 非零实例为主；验证 Handle、参数对象及数值字段。

## 2026-08-19：PalPanelBridge 0.1.31 登录同步、物品槽位与非空帕鲁

已完成：

- 实机确认玩家进入约 30 秒内可能只有 Controller/Pawn，UID 为零且背包、帕鲁、公会引用为空；新增 `player_data_ready`/`player_data_state`，明确区分 `initializing` 与 `ready`，避免把暂态空数据当最终结果缓存。
- 背包确认 6 个 `PalItemContainer`，真实槽位字段为 `ItemSlotArray`（`PalItemSlot[]`）；改用该字段返回槽位数量并展开首个槽位元数据。
- 帕鲁仓库总槽位 960，前 10 个为空；改读 `PalStorage.CachedNonEmptySlots_InServer` 并新增 `pal_non_empty_slot_array`，直接检查最多 10 个非空帕鲁槽位。
- `PalInstanceID` 确认为 `PlayerUId`、`InstanceId`、`DebugName`；仅解析两个 GUID，`individual_id` 返回 `InstanceId`，不再输出整块结构原始内存。
- 版本、README、接口文档、紧凑查询输出一次性统一为 `0.1.31`。

验证计划：

- GitHub Actions 成功后由本地部署脚本自动校验、停服替换、启动并确认 `/v1/health` 为 0.1.31。
- 玩家在线且 `player_data_state=ready` 时调用一次 metadata，确认 `PalItemSlot`、非空帕鲁 Handle 与参数对象字段。

实际结果（2026-08-19 02:44，中国时区）：`player_data_state=ready`；6 个背包容器可读，`PalItemSlot` 字段确认包含 `SlotIndex`、`ItemId`、`StackCount`、`DynamicItemData`；终端 `SlotArray` 首槽读到实例 ID `746BB6C84A4D2045267D388D9CBF6AC8`，Handle 与 `ReplicateIndividualParameter` 有效；`CachedNonEmptySlots_InServer` 仍为 0，不能作为唯一非空来源；据点数已变为 1。

## 2026-08-19：PalPanelBridge 0.1.30 背包、帕鲁与据点合并探针

已完成：

- 0.1.29 本地实机确认：`InventoryMultiHelper.Containers` 是 `PalItemContainer[]`；帕鲁槽位包含 `Handle`、`ReplicateHandleID` 和 `ReplicateIndividualParameter`；公会对象包含 `BaseCampIds`/`BaseCampLevel`。
- 背包改为从 helper 的 `Containers` 对象数组读取，返回最多 16 个容器、对象身份和 `Slots`/`ItemSlots`/`SlotArray` 数量；metadata 同时展开首个容器与首个槽位元素。
- 帕鲁槽位区分 `slot_object` 与真实 `handle`，读取 `ReplicateHandleID` 受限原始十六进制值，并返回 `ReplicateIndividualParameter` 对象；metadata 同时展开 Handle、参数对象、ID 结构字段。
- 据点 metadata 新增 `BaseCampIds` 元素结构；本地部署脚本新增停服、SHA-256 校验、原子替换、健康失败回滚、面板启动及 Bridge 版本健康检查流程，保留 `config.ini`；查询脚本在 Windows 强制 UTF-8 输出。
- 修复工作流包名硬编码为 0.1.29：改为从 `src/main.cpp` 的 `ModVersion` 自动解析；部署脚本增加 Windows DLL 解锁等待和 `--download-only`，工具修复验证不再触发服务器重启。
- 版本与 README、接口文档一次性统一为 `0.1.30`；所有人类可读时间继续使用中国时区。

验证计划：

- 仅通过 `PalPanelBridge build` 构建；CI 成功后由 `deploy.py --local-dll` 自动下载、校验、停服替换、启动并等待 `/v1/health` 返回 `bridge_version=0.1.30`。
- 玩家在线时调用一次 `POST /v1/players/online/metadata`，确认物品槽位字段、帕鲁 Handle/参数字段及据点 ID 结构，再进入实际物品和帕鲁详情读取。

实际结果（2026-08-19 02:24，中国时区）：

- 0.1.30 DLL 已加载，`/v1/health` 返回 `bridge_version=0.1.30`、`unreal_initialized=true`、`game_thread_tick_seen=true`。
- 首次 metadata 调用完成，但服务器重启后在线玩家数为 0；待玩家重新进入后复测详细字段。

## 2026-08-19：PalPanelBridge 0.1.29 本地运行时诊断（帕鲁槽位对象 + 背包容器入口）

已完成：

- 修复 `PalIndividualCharacterContainer.SlotArray` 的对象数组读取：槽位元素按 `UObject` 解析，`pal_slot_array.slots` 实机返回 `PalIndividualCharacterSlot` 对象（每槽 `Handle` 引用有效）；`individual_id` 待字段名确认。
- 新增 `inventory_helper_found`/`inventory_helper`：读取 `PalPlayerInventoryData.InventoryMultiHelper`（`PalItemContainerMultiHelper`，背包容器真实入口对象）。
- metadata 探针新增 `detail_property_metadata.inventory_helper` 与 `.pal_slot_object`（首个 `PalIndividualCharacterSlot` 对象关键词属性），用于确认个体 ID 与物品槽位字段名。
- 版本统一为 `0.1.29`；`query_online_players.py` 紧凑输出保留新字段。

验证：

- 本地服务器（experimental UE4SS + 0.1.29）实机：`pal_slot_array.found=true`、`slot_count=960`，槽位对象引用有效；`inventory_helper` 待构建后确认。

## 2026-08-19：面板默认 UE4SS 改为 GitHub experimental-latest

- `backend/internal/appconfig/config.go` 默认 UE4SS 下载源从 `v3.0.1` release 改为 GitHub `experimental-latest`（`UE4SS_v3.0.1-1029-g69f1bd11.zip`），版本标记 `experimental-latest`，SHA-256 更新为 `d42ca456316c2ff7b0cbc0c978a6c391eb8f71726048574082fa8aee94eff5fa`。
- 目的：PalPanelBridge 按 UE4SS experimental `c838a8ac` 编译（创意工坊 item `3625223587` 同源），release `v3.0.1` 的 C++ 模组 ABI 不兼容，加载报 `0x7f`；experimental-latest 已验证可正常加载 0.1.28 main.dll。
- 面板的 UE4SS 标识（WorkshopID `3625223587`）保持不变；仅统一下载源与哈希固定。

## 2026-08-06：PalPanelBridge 0.1.28 帕鲁槽位与背包容器读取

已完成：

- 新增 `pal_slot_array`：从 `PalIndividualCharacterContainer.SlotArray` 读取 `found`、`slot_count` 和最多 10 个槽位，每个槽位含 `individual_id`（16 字节实例 ID）与 `Handle` 对象引用（`PalIndividualCharacterHandle`）。
- 新增 `inventory_containers`：在 `PalPlayerInventoryData` 上按名称探测 `EssentialContainer`、`PlayerInventoryContainer`、`EquipmentContainer`、`LoadoutContainer`、`ItemContainer`、`InventoryContainer`，每个容器返回对象身份与 `Slots`/`ItemSlots` 数组规模。
- metadata 探针为每个找到的物品容器返回 `container_property_metadata`，确认真实槽位字段名（关键词：slot/item/container/equipment/loadout/weapon/armor）。
- 版本统一为 `0.1.28`；`query_online_players.py` 紧凑输出保留新字段。

验证：

- 尚未进行运行时验证；等待 PalPanelBridge build 成功后部署、面板 API 重启并用 Panel 诊断中转实测。

## 2026-08-06：PalPanelBridge 0.1.27 据点/重量/帕鲁容器数值读取

已完成：

- `base_camp_count` 改为读取公会 `BaseCampIds` 数组长度；新增 `base_camp_level_found`/`base_camp_level` 读取数值 `BaseCampLevel`。
- 背包新增 `inventory_weight_found`、`now_item_weight`、`max_inventory_weight`（数值属性存在时读取）。
- 帕鲁存储新增 `pal_container_found`/`pal_container`（`PalStorage.TargetContainer` → `PalIndividualCharacterContainer` 对象引用）。
- metadata 探针新增 `detail_property_metadata.pal_container`（关键词：pal/character/slot/handle/container/individual/otomo），为下一步读取帕鲁槽位做准备。
- 版本统一为 `0.1.27`；`query_online_players.py` 紧凑输出保留新字段。

验证：

- 实机验证完成（2026-08-06）：Action `31037770382` 已部署，SFTP 备份替换成功，面板 API `/api/server/restart` 重启成功（同步返回 `status=restarted`），health 返回 `bridge_version=0.1.27`。任务 `players_1785957095373_2`：`base_camp_count=1`、`base_camp_level=1`、`now_item_weight=0`、`max_inventory_weight=300`、`pal_container` 指向 `PalIndividualCharacterContainer_2147481595`。metadata 探针确认帕鲁容器有 `SlotArray`（array，元素 `PalIndividualCharacterSlot`），`pal_storage` 有 `TargetContainer`/`SlotObserver`/`PalDimensionStorage`，为下一步读取帕鲁槽位打基础。`inventory_container_count` 仍 -1：背包容器属性不在 `InventoryData` 顶层关键词枚举内，故 0.1.28 改为按名称探测常见容器属性。

## 2026-08-06：PalPanelBridge 0.1.26 玩家详细对象引用与背包/帕鲁元数据探针

已完成：

- 普通在线查询 additive 返回 `guild_name`、`guild_admin_player_uid`、`base_camp_count`、`inventory_found`/`inventory`（`PalPlayerInventoryData`，附 `inventory_container_count`）、`pal_storage_found`/`pal_storage`（`PalPlayerDataPalStorage`）、`otomo_found`/`otomo`（`PalPlayerOtomoData`）。
- 只读取对象身份、`GuildName`/`GroupName` 字符串、`AdminPlayerUId` GUID、`BaseCampMap`/`BaseCamps` Map 规模、`InventoryData.Containers` 数组规模；容器内容、物品槽位和帕鲁数组元素一律不读取。
- metadata 探针新增 `detail_property_metadata.inventory`、`.pal_storage`、`.otomo` 关键词属性列表（每个对象最多 64 项），guild 关键词扩展 `base`、`camp`、`territory`、`map` 以发现据点字段。
- 版本统一为 `0.1.26`；`query_online_players.py` 紧凑输出保留新字段。

验证：

- 实机验证完成（2026-08-06）：Action `31036005059`、SHA `e18a575db8bfaa7d08a41fd3ad8cda467ba34f14246cb2be4a0495ed52f3e36b` 已部署；服务器进程重新加载后 `bridge_version=0.1.26`。任务 `players_1785956478968_1`：`inventory_found=true`（`BP_PalPlayerInventoryData_C`）、`pal_storage_found=true`（`PalPlayerDataPalStorage`）、`otomo_found=true`（`PalPlayerOtomoData`）、`guild_name` 与 `guild_admin_player_uid`（=玩家自身 UID，会长）读取成功。`base_camp_count` 与 `inventory_container_count` 为 -1：实机 metadata 确认据点真实字段是 `BaseCampIds`（数组）与 `BaseCampLevel`（数值），背包容器属性名未出现在关键词枚举中，重量字段 `NowItemWeight`/`MaxInventoryWeight` 可用；帕鲁真实入口是 `PalStorage.TargetContainer`（`PalIndividualCharacterContainer`）。据此推进 0.1.27。

## 2026-08-06：PalPanelBridge 构建加速缓存

- `.github/workflows/palpanel-bridge.yml` 增加两个 GitHub Actions 缓存：RE-UE4SS SDK checkout（按 `UE4SS_COMMIT` 作 key）和 CMake 构建目录（按提交 sha 作 key、`palpanel-build-` 作恢复前缀）。
- SDK 缓存命中时跳过 `git fetch` + `submodule update --init --recursive`，只做本地校验；构建目录缓存命中后 CMake 配置与编译走增量路径，后续只改插件源码时预计把单次构建从十几分钟降到数分钟。
- 构建逻辑仍调用 `tools/palpanel-bridge/build-local.ps1`，未复制脚本逻辑到工作流；版本、产物名和上传逻辑不变。

## 2026-08-05：PalPanelBridge 0.1.25 受限玩家详细字段元数据探针

- 关键词枚举 `GuildBelongTo` 与 `Pawn.CharacterParameterComponent` 的属性元数据：当前类和 `TSuperStructRange` 父类逐层使用 `IncludeDeprecated`，按小写属性名匹配并按名称去重，每个对象最多 64 项，达到上限立即停止。
- 普通在线查询只 additive 返回 `character_parameter_found` 与对象描述；只有 `metadata_probe` 且首名玩家被选中时返回 `detail_property_metadata`。探针不读取匹配属性值、数组、Map、嵌套内容，也不调用未知函数。
- 版本统一为 `0.1.25`；紧凑查询保留新增字段和存在时的详细元数据。
- `0.1.24` Action `30932965144`、SHA `2b8634fb3e8ad30f985cdf8e6ef5a25d2191df5e8a499aabebd20a8e09df067a` 已部署，备份成功且未自动重启；实机任务 `players_1785866795757_1` 于 `2026-08-05T02:06:35.759+08:00` 完成，`cached_location=(-374896,237490,-919.81)`，`guild=PalGroupGuild_2147480614/class PalGroupGuild`，错误为空，队列到执行 2ms；metadata 已证实 Pawn 有 `CharacterParameterComponent(object PalCharacterParameterComponent)`。
- `0.1.25` 实机验证完成（2026-08-06）：Action `31033828265`、SHA `d9246add053d376b46f27538f37477e845952d164543a2bf398bc8e86639ace7` 已部署；服务器进程于 `2026-08-06T02:31:11+08:00` 重新加载并 `Starting C++ mod 'PalPanelBridge'`，监听 `127.0.0.1:18083`、token 已配置。玩家 `tiantian` 上线后任务 `players_1785955082888_3` 完成：`controller_object_count=1`、`online_player_count=1`、UID `F23D556C000000000000000000000000`、Pawn `BP_Player_Female_C`、`cached_location=(-375708,236900,-954.49)`、`guild=PalGroupGuild_2147480614`、`character_parameter_found=true` 且 `PalCharacterParameterComponent` 对象引用有效。metadata 任务 `players_1785955093545_4` 返回 `detail_property_metadata`：guild 关键词属性 16 项（含 `GuildName`、`GroupName`、`AdminPlayerUId`、`GuildChestAllowedRoles`），character_parameter 关键词属性 14 项（含 `IndividualParameter` → `PalIndividualCharacterParameter`、`MaxHPRate_ForTowerBoss`、`DyingHP`、`DyingMaxHP`）。队列到执行约 2-3ms，tick 随查询递增。

## 2026-08-05：PalPanelBridge 0.1.24 首批玩家详细字段

已完成：

- PlayerState 只读返回 `CachedPlayerLocation` 与 `GuildBelongTo` 对象引用，新增字段为 additive：`cached_location_found`、`cached_location`、`cached_location_error`、`guild_found`、`guild`。
- 位置读取执行属性查找、`FStructProperty`、完整类型名、12/24 字节尺寸和 finite 三重防护；公会对象仅在 `IsReal` 成功时返回。
- 普通在线玩家查询与 metadata 查询均填充这些字段，紧凑脚本保留五项字段。

上一版实机记录：`0.1.23` Action `30930148809`，SHA `a2b1f73b802acbdf0212db3e2dae2ad9a24bfbe41d108e894bd92aa388204ed8`；部署备份成功，重启后任务 `players_1785862619310_1` 于 `2026-08-05T00:56:59.314+08:00` 完成，父类字段修复生效。

本版尚未运行验证。

## 2026-08-05：PalPanelBridge 0.1.23 元数据继承链修复

- 基于 `0.1.22` 的运行验证：`metadata_probe=true` 时，PlayerState 仅返回 6 项、Pawn 仅返回 24 项当前蓝图字段，缺少已知继承字段。
- 修复目的：让 `/v1/players/online/metadata` 按 UE4SS 自身范式先枚举当前类，再用 `TSuperStructRange` 显式逐层枚举父类；每层使用 `TFieldRange` 的 `IncludeDeprecated`。
- 每个对象最多收集 96 项，达到上限后立即停止后续枚举；不读取属性值，普通 `/v1/players/online` 响应与业务行为不变。
- 本次 `0.1.23` 修复尚未进行运行时验证。

## 2026-08-05：PalPanelBridge 0.1.22 继承属性元数据修复

- `0.1.21` 已由 GitHub Actions `30925986780` 成功构建并经 `deploy.py` 完成 SHA256 校验、远程备份和原子替换；`config.ini` 保持不变，服务器未自动重启。
- 重启后实机元数据任务 `players_1785859837829_1` 已从队列进入 `completed`，但 PlayerState 仅返回 6 项、Pawn 仅返回 24 项，且缺少已知继承字段，证明原实现只遍历当前蓝图类。
- 元数据枚举改用 UE4SS SDK 已验证的 `TFieldRange<FProperty>` 与 `IncludeSuper | IncludeDeprecated`，继续只返回 `name`、`kind`、`declared_type`，不读取未知值。
- 版本升级至 `0.1.22`；仍保持首名玩家、每对象最多 96 项和普通在线接口零扩容。

## 2026-08-04：PalPanelBridge 0.1.21 在线玩家顶层属性元数据诊断

- 新增 `POST /v1/players/online/metadata`，独立创建带 `metadata_probe` 标志的在线玩家 job。
- 复用在线玩家枚举和身份读取，只对首名玩家的 PlayerState/Pawn 描述最多 96 个顶层属性，避免超过诊断响应上限。
- 只返回属性名、粗粒度 kind 和声明类型；不读取未知值、集合内容、嵌套结构，也不调用未知 UE 函数。
- 普通 `POST /v1/players/online` 的响应与既有属性诊断行为保持不变。
- `query_online_players.py --metadata` 保留元数据计数、截断状态和玩家元数据列表；未存储任何凭据。

## 2026-08-04：PalPanelBridge 在线玩家只读查询脚本

- 新增仅使用 Python 标准库的 `tools/palpanel-bridge/query_online_players.py`。
- 通过 PalPanel HTTP 诊断接口提交并轮询在线玩家任务，完成、失败和超时状态均可由退出码区分。
- 凭据优先从环境变量读取，缺失时使用隐藏输入；进度写入 stderr，玩家摘要 JSON 写入 stdout。
- 默认只保留 UID、名称、Controller、PlayerState、Pawn、任务时间和 tick，减少日志与模型 token 消耗；`--full` 可按需输出完整诊断树。

## 2026-08-03：PalPanelBridge 在线任务阻塞修复

问题与证据：

- `0.1.19` 部署后，`/v1/health` 初始正常；执行在线玩家任务后，读取 `players_1785699574044_1` 超时，随后所有 Bridge 连接均无响应。
- 在线任务原先在 `jobs_mutex_` 持锁期间执行完整 UE 反射；HTTP worker 查询 job 时等待同一锁，单线程监听因此无法继续处理 health 等新请求。
- `0.1.19` 对最多 64 个候选函数逐项枚举参数，进一步放大了游戏线程耗时和响应体大小。

修复：

- PalPanelBridge 升级至 `0.1.20`。
- job 在锁内领取并标记为 `running`，UE 反射在锁外执行，完成后再短暂加锁提交不可变快照。
- 暂停所有函数参数遍历，函数候选保留空 `parameters` 数组以兼容 `0.1.19` 响应结构；待稳定后再以独立单函数探针重新引入。
- HTTP 查询只在锁内复制 job 快照，并在锁外生成 JSON；运行中任务不会阻塞其他请求。
- job 表只淘汰 `completed/failed`；64 条任务全部处于 `queued/running` 时返回 `503 job_queue_full`，不再删除活动任务。
- 不改变在线玩家来源、去重、身份读取或只读安全边界。

验证要求：

- 仅通过 GitHub Actions 构建；部署后确认在线任务处于 `running` 时 health 仍可响应。
- 连续创建新 job，比较 job ID、执行时间和 Tick；不得重复读取旧 job 作为新数据。
- 真实玩家进入、离开和重连后分别验证来源计数与玩家结果。

## 2026-08-03：诊断状态与内网 HTTP 的管理员 API Key 访问

已完成：

- 诊断状态和内网 HTTP 测试新增专用权限：管理员 session 或管理员 API Key 可用；operator/viewer API Key 拒绝。
- 管理员 API Key 的 HTTP 测试仅允许 `http` 及 GET/HEAD/POST；管理员 session 保持原有 http/https 与方法兼容性。
- Shell 和支持包继续保持管理员 session-only，未放宽其高风险能力。
- 保留回环/私网目标、禁代理、重定向重检、15 秒超时、64 KiB 限制和 Header 校验。

残余风险：管理员 API Key 仍可发起受限内网 HTTP 请求，应使用短期、可撤销的 Key，并避免通过公网明文传输。

## 2026-08-03：统一 PalPanelBridge 开发路线与交付流程

已完成：

- 新增 PalPanelBridge 独立路线图，统一 P0 至 P3 优先级、当前状态和完成标准。
- 固定“源码与文档更新 → `custom-stable` → GitHub Actions → 校验部署 → 诊断控制台 → 实机结果入日志”的交付顺序。
- 明确插件不在本地编译或测试，构建失败不部署，上传只替换 DLL、保留 `config.ini` 且不自动重启服务器。
- 所有接口响应、日志和人类可读时间统一使用 `Asia/Shanghai`。

## 2026-08-03：函数参数探针与中国标准时间

已完成：

- PalPanelBridge 升级至 `0.1.19`。
- 为关键玩家组件的候选函数返回参数名、类型、大小和返回值标记。
- 所有可读响应、排队和执行时间改为中国标准时间（UTC+8）。
- Unix 毫秒时间戳保持不变；本版本仍不调用游戏函数。
- GitHub Actions `30762655374` 已成功构建，`main.dll` SHA-256 为 `37f75990922635087f90a2e5893e0d9455e8f0cc8fd294485b8725a1923c8420`。
- 部署脚本已完成远程旧 DLL 备份、临时上传、远程回读校验和原子替换；`config.ini` 保持不变，服务器未自动重启。
- 部署前旧版 job 响应确认 1 名在线玩家，Controller、PlayerState、PalUtility 和 GameState 四个来源计数均为 1；该历史 job 在执行后约 27 秒被读取。
- 服务器运行时版本和中国时区字段仍需在人工重启 PalServer 后通过诊断控制台验证。

部署工具修正：

- `deploy.py` 默认将下载并校验后的构建快照保存到工作区外层 `PalPanelBridge/versions`，不再写入工作区根目录。

## 2026-08-03：关键组件集合计数与函数探针

已完成：

- PalPanelBridge 升级至 `0.1.18`。
- 数组和 Map 返回经过范围校验的元素数量。
- 两个关键玩家组件返回最多 64 个帕鲁、队伍、装备和槽位相关函数元数据。
- 不调用函数、不读取集合元素、不修改游戏对象。

## 2026-08-03：关键玩家组件完整属性探针

已完成：

- PalPanelBridge 升级至 `0.1.17`。
- 对 `PalItemSelectorComponent` 和 `BP_OtomoPalHolderComponent` 返回最多 96 个完整属性元数据。
- 其他在线对象继续使用关键词过滤，控制响应体大小。
- 仍不读取属性值、数组或容器内容，不修改游戏对象。

## 2026-08-03：PalPanelBridge 自动校验部署脚本

已完成：

- 增加 `tools/palpanel-bridge/deploy.py`，可自动检查最新插件 Action 是否成功。
- 默认绑定当前提交并持续等待对应 Action，每 30 秒检查一次，最长等待 45 分钟。
- 构建失败时禁止上传，同时保存失败步骤日志并通过脚本错误返回给调用方。
- 自动下载并校验 artifact、固定 SFTP 主机指纹、备份旧 DLL、原子替换并回读校验。
- 部署脚本或插件文档单独修改不再触发插件构建；仅源码、构建配置和包配置变化时运行 Action。
- 仅部署 `dlls/main.dll`，保留远端 `config.ini`；凭据只从环境变量或交互输入读取。

## 2026-08-03：在线玩家对象候选单层展开

已完成：

- PalPanelBridge 升级至 `0.1.16`。
- 对在线玩家的对象类型候选增加实际对象状态、运行时对象信息及一层内部候选属性。
- 优先支持继续定位 `BP_OtomoPalHolderComponent` 和 `LoadoutItemSelector`。
- 不读取数组或容器内容，不递归展开多层，不修改游戏对象。

## 2026-08-03：CI 构建路径收敛

已完成：

- 主 `CI` 仅在面板后端、前端、打包脚本、运行时工具或 CI 自身发生变化时自动运行。
- `tools/palpanel-bridge/**` 由独立的 `PalPanelBridge build` 工作流负责，不再触发整套面板 CI。
- 文档和插件说明的单独修改不触发面板编译；`workflow_dispatch` 手动验证入口保持不变。

## 2026-08-03：在线玩家属性类型探针

已完成：

- PalPanelBridge 升级至 `0.1.15`。
- 保留 `property_candidates` 兼容字段，新增 Controller、PlayerState、Pawn 三组 `property_details`。
- 每项返回属性名、粗粒度类型及可识别的声明类型，用于定位背包、装备和帕鲁容器入口。
- 仅读取反射元数据，不读取容器内容、不修改游戏对象。

验证标准：

- GitHub Actions 使用固定 UE4SS `c838a8ac` SDK 成功构建服务器包。
- 在线玩家任务仍返回原有字段，并新增 `property_details`。
- 部署后由真实服务器确认候选类型随当前玩家对象返回。

## 2026-08-02：在线玩家数据属性探针

已完成：

- PalPanelBridge 升级至 `0.1.14`。
- 在线玩家结果增加 Controller、PlayerState、Pawn 三组运行时属性候选。
- 仅保留背包、容器、装备、物品、槽位、队伍和帕鲁相关名称，每组最多 64 项。
- 探针只读取反射元数据中的属性名，不读取属性值，不修改游戏对象。

## 2026-08-02：修复 GameState 玩家数组读取崩溃

已完成：

- PalPanelBridge 升级至 `0.1.13`。
- 修复 `0.1.12` 将 UE5 `TObjectPtr<APlayerState>` 数组元素当作裸 `UObject*` 读取导致的服务器崩溃。
- 改用 UE4SS 数组助手定位元素，并通过内部对象属性安全解析每个 PlayerState。
- 保持 GameState 权威玩家枚举、响应时间和诊断字段不变，全过程只读。

## 2026-08-02：PalPanelBridge 从 GameState 权威数组读取在线玩家

已完成：

- PalPanelBridge 升级至 `0.1.12`，在线查询优先读取当前 World 的 `GameState.PlayerArray`。
- 返回 GameState 对象信息、PlayerArray 可用状态、原始玩家数量和反射错误，便于实机核对。
- 由权威数组发现的玩家以 `game_state_player_array` 标记，原 PalUtility 与对象扫描继续作为诊断回退。
- 保留 `0.1.11` 的响应、排队、执行时间及 Tick 计数，确保每次结果可验证是否变化。
- 全过程只读，不修改玩家、背包、帕鲁或存档。

## 2026-08-02：PalPanelBridge 全响应时间与在线查询上下文

已完成：

- PalPanelBridge 升级至 `0.1.11`，所有成功与错误 JSON 响应统一返回 Unix 毫秒时间和 UTC 时间。
- 任务保存排队时间、游戏线程执行时间及执行时 Tick 计数，便于确认连续查询不是旧缓存。
- 在线玩家结果返回实际用于 `PalUtility.GetAllPlayerStates` 的 World 对象信息。
- 时间字段在 HTTP 响应生成时注入，任务执行字段在 UE4SS 游戏线程中记录。
- 不改变在线玩家查询逻辑，不执行任何玩家写操作。

## 2026-08-02：PalPanelBridge 使用 PalUtility 获取在线玩家

已完成：

- `0.1.9` 实机确认 Controller 与 PlayerState 的 `FindAllOf` 均可能在玩家在线时返回 0。
- PalPanelBridge 升级至 `0.1.10`，以已验证的当前 World 作为上下文，只读调用 `PalUtility.GetAllPlayerStates`。
- 调用前校验 World、CDO、UFunction、对象参数、输出数组类型和数组尺寸；输出数组读取后执行销毁。
- 结果增加 PalUtility 可用状态、玩家数量和错误信息，原 Controller/PlayerState 扫描继续作为对照。
- 不读取背包/帕鲁，不执行任何玩家写操作。

## 2026-08-02：PalPanelBridge 在线玩家枚举回退

已完成：

- 修复 `0.1.8` 实机在线但 Controller 单类查询返回 0 项的问题。
- PalPanelBridge 升级至 `0.1.9`，同时检查原生与蓝图 Controller/PlayerState 类名并按 UObject 指针去重。
- Controller 不可见时以活动 PlayerState 作为只读回退，继续返回昵称与 PlayerUID。
- 结果增加 Controller/PlayerState 候选数量和 `source`，便于继续定位运行时类生命周期。
- 不读取背包/帕鲁，不执行任何玩家写操作。

## 2026-08-01：PalPanelBridge 在线玩家身份只读解析

已完成：

- `0.1.7` 已在实机找到 1 个在线 `BP_PalPlayerController_C` 对象。
- PalPanelBridge 升级至 `0.1.8`，在线玩家任务继续只读解析关联 PlayerState 和 Pawn。
- 从 PlayerState 读取 `AccountName` 与 FGuid 类型的 `PlayerUId`，读取前校验反射属性类型和 GUID 尺寸。
- 字段不存在或类型变化时返回 `identity_error`，不会写入对象，也不会让其他玩家结果丢失。
- 同步更新 Action 安装包名称和中英文验证文档。

## 2026-08-01：PalPanelBridge 在线玩家对象只读探针

已完成：

- `0.1.6` 已在实机找到 World：`PL_MainWorld5`，类名为 `World`。
- PalPanelBridge 升级至 `0.1.7`，新增只读 `POST /v1/players/online` 任务接口。
- 在 UE4SS `on_update` 游戏线程枚举当前 `PalPlayerController` 实例；离线玩家不进入结果。
- 返回在线对象数量、对象名、完整名和类名，不读取背包/帕鲁，不修改任何玩家数据。
- 同步更新 Action 安装包名称和中英文验证文档。

## 2026-08-01：PalPanelBridge World 对象只读探针

已完成：

- `0.1.5` 实机验证中，20.53 秒内游戏线程 Tick 增加 4002，最后 Tick 延迟为 1–2 ms。
- PalPanelBridge 升级至 `0.1.6`，新增只读 `POST /v1/world` 任务接口。
- World 查找仅在 UE4SS `on_update` 游戏线程执行，HTTP 线程不直接访问 UObject。
- 任务结果返回 World 对象名、完整名和类名，不修改任何游戏对象。
- 同步更新 Action 安装包名称和中英文验证文档。

## 2026-08-01：PalPanelBridge 游戏线程运行状态

已完成：

- PalPanelBridge 升级至 `0.1.5`，新增只读 `GET /v1/runtime` 接口。
- 返回游戏线程 Tick 计数、最后 Tick 时间、Tick 延迟和插件运行时长。
- 连续请求时 Tick 计数应持续增长，用于验证 HTTP 与 UE4SS 游戏线程持续连通。
- 同步更新服务器安装包名称、使用文档和 Action 产物名称。

## 2026-07-30：诊断与支持包

目标版本：`v1.3.0-custom.0.8.41`

已完成：

- 诊断页面新增固定白名单体检、支持包生成、列表、下载和删除。
- 支持包包含构建信息、运行方式、服务器状态、前置条件、主机能力、近期任务、审计和事件摘要。
- 可选附带最近 3 个日志尾部；单文件 128 KiB、总计 384 KiB，写入前执行脱敏。
- JSON 同时按敏感键名和文本模式脱敏，排除密码、Token、Cookie、API Key、绝对路径、IP、GUID、SteamID 和长标识。
- 原始存档、数据库、环境变量和用户指定路径不进入 ZIP。
- 目录与文件使用私有权限；ZIP 条目排序并执行路径安全检查。
- 默认最多保留 5 份、14 天、单包 50 MiB。

验证：

- 新增文本/键名脱敏、日志白名单、ZIP 顺序和权限、路径穿越及 ID 校验测试。
- OpenAPI、生成契约、路由契约、维护指南、计划、已知问题和中文 Release 说明已同步。
- 完整 Go、前端和 Linux/Windows Release 验证由 GitHub Actions 执行。

## 2026-07-30：PalPanelBridge UE4SS 只读链路实机验证

已完成：

- 按服务器实际 UE4SS 提交 `c838a8ac` 和 `Game__Shipping__Win64` 配置编译 `PalPanelBridge 0.1.4`。
- 将 HTTP 初始化延迟到 `on_unreal_init`，避免阻塞 PalServer 启动。
- 默认端口由与 `PalPanelSteamAPIProxy` 冲突的 `18082` 改为 `18083`。
- 配置文件改为根据 `dlls/main.dll` 自身路径定位，不再依赖 Wine 工作目录。
- 新增不泄露 Token 的 `PalPanelBridge.log`，记录配置路径、监听状态和 Winsock/bind 错误。
- 新增 `docs/palpanel-bridge.md`，说明安装、鉴权、游戏线程探针及故障判断。

实机验证：

- PalServer 和 UE4SS 能正常完成启动。
- `127.0.0.1:18083/v1/health` 已返回 PalPanelBridge JSON。
- 未携带 Bearer Token 时按预期返回 `401 Unauthorized`，证明监听、路由和鉴权边界均已生效。
- 下一步验证携带正确 Token 的 health 响应，以及任务从 `queued` 进入 `completed`。

## 2026-07-30：第三方镜像与完全离线构建

目标版本：`v1.3.0-custom.0.8.40`

已完成：

- 新增统一依赖锁定清单和 `vendorctl` 镜像工具。
- 支持 PalOps、MapLibre、PalCalc、uesave、授权地图瓦片及 npm/Cargo/Go/NuGet 缓存归档。
- 镜像初始化生成逐文件 SHA-256；构建前验证文件数量、大小和内容哈希。
- Linux 与 Windows 打包新增 `online`、`mirror`、`offline` 模式。
- 离线模式关闭 Go 代理并启用 npm/Cargo 离线开关；镜像缓存复制到临时工作目录，避免构建污染主镜像。
- 临时准备的第三方源码带管理标记，清理不会删除人工维护的目录。

验证：

- 新增镜像初始化、缺失组件、篡改检测、符号链接拒绝和安全清理测试。
- 新增 CI 执行入口、Bash 语法和 PowerShell 结构检查。
- 修复实时地图标记异步加载完成前同步断言，导致 Linux/Windows CI 同时失败的问题。
- 完整断网 Linux/Windows Release 构建需在镜像内容初始化后执行。

## 2026-07-30：PalPanelBridge UE4SS 只读链路实机验证

已完成：

- 按服务器实际 UE4SS 提交 `c838a8ac` 和 `Game__Shipping__Win64` 配置编译 `PalPanelBridge 0.1.4`。
- 将 HTTP 初始化延迟到 `on_unreal_init`，避免阻塞 PalServer 启动。
- 默认端口由与 `PalPanelSteamAPIProxy` 冲突的 `18082` 改为 `18083`。
- 配置文件改为根据 `dlls/main.dll` 自身路径定位，不再依赖 Wine 工作目录。
- 新增不泄露 Token 的 `PalPanelBridge.log`，记录配置路径、监听状态和 Winsock/bind 错误。
- 新增 `docs/palpanel-bridge.md`，说明安装、鉴权、游戏线程探针及故障判断。

实机验证：

- PalServer 和 UE4SS 能正常完成启动。
- `127.0.0.1:18083/v1/health` 已返回 PalPanelBridge JSON。
- 未携带 Bearer Token 时按预期返回 `401 Unauthorized`，证明监听、路由和鉴权边界均已生效。
- 下一步验证携带正确 Token 的 health 响应，以及任务从 `queued` 进入 `completed`。

## 2026-07-29：PalOps 世界地图迁移

目标版本：`v1.3.0-custom.0.8.39`

已完成：

- 删除旧版单图 SVG 地图，迁移 PalOps Web 1.3.2 的 Palpagos / World Tree 双图层、瓦片金字塔和世界坐标仿射变换。
- 使用自托管 MapLibre GL JS 6.0.0 渲染离线栅格和 GeoJSON 标记，ESM、worker、CSS 与许可证均由固定版本同步，不使用 CDN。
- 新增固定 POI 六大分组、关键词搜索、地图切换、未发现筛选和浏览器本地探索进度。
- 保留 PalPanel 的玩家、据点、帕鲁、地图对象及存档索引 API，动态图层不依赖 PalOps 后端。
- 玩家位置刷新间隔支持 1/2/3/5/10/15/30 秒并持久化。
- 新增固定提交资源同步器，校验三语 POI 数量、稳定 ID、坐标、扩展名、压缩包路径、符号链接和文件大小。
- 由于 PalOps 元数据将当前栅格瓦片标记为不可再分发，默认发布仅同步开放数据；完整瓦片必须从管理员有权使用的本地来源显式导入。

验证：

- 新增仿射坐标、World Tree 识别、像素边界、Locale 和 POI 分组测试。
- 新增地图页面固定 POI、动态图层、筛选和探索记录测试。
- 新增资源同步器开放数据、三语坐标一致性和瓦片权利确认测试。
- 新增 MapLibre 运行时完整性同步测试、Web Mercator 坐标往返测试。
- 完整前端、Linux/Windows 发布和真实地图瓦片验证由 GitHub Actions 与实机完成。

## 2026-07-29：房主存档 UID 重映射 CustomVersionData 修复

目标版本：`v1.3.0-custom.0.8.38`

已完成：

- 修复合作房主固定源 UID `00000000-0000-0000-0000-000000000001` 在 `Level.sav` 的 `worldSaveData.ItemContainerSaveData[n].Value.CustomVersionData` 中形成字节级误报、导致存档转移中止的问题。
- 仅对上述精确文件、路径、候选类型和固定房主 UID 应用窄范围豁免；不修改 `CustomVersionData` 原始字节。
- 成功迁移报告中保留 opaque candidate，并增加“版本元数据已原样保留”的验证警告。
- 任意其他 Raw、unknown、trailing、自定义 UID、目标 UID 或相似路径仍执行原有 fail-closed 安全门。

验证：

- 新增精确路径允许、相似路径拒绝、任意源 UID 拒绝和目标 UID 拒绝测试。
- 保留语义指纹和双重规范化 round-trip 校验，确保忽略的是扫描碰撞而不是跳过存档完整性验证。
- 完整 Rust、Go、Linux/Windows 发布验证由 GitHub Actions 执行。

## 2026-07-29：通知与事件中心

目标版本：`v1.3.0-custom.0.8.37`

已完成：

- 新增持久事件、事件时间线、失败任务关联和 Webhook 投递队列表。
- 监控告警、崩溃守卫和失败后台任务自动形成事件；相同根因累计次数，已解决后再次发生会自动重新打开。
- 新增事件列表、详情、确认、解决、重新打开和 Webhook 测试 API。
- 新增“通知与事件”页面，支持状态、严重级别、来源和关键词筛选，并显示时间线与脱敏投递状态。
- 可选 Webhook 使用 HMAC-SHA256、固定 Delivery ID、禁止重定向和最多 6 次退避重试。
- 后台任务只写入稳定错误码，不存储原始错误正文；Webhook 不发送日志、路径、命令参数或密钥。
- 事件状态写入使用独立互斥，避免同根因并发到达时发生唯一键冲突。
- 已解决事件默认保留 90 天；Webhook 投递失败不会生成新的事件。

验证：

- 新增事件归并、自动重开、任务错误脱敏、告警状态同步和投递队列测试。
- 新增 Webhook HMAC 签名、密钥不泄露、禁止重定向和配置边界测试。
- OpenAPI、生成契约、路由契约、维护指南、计划、已知问题和中文 Release 说明已同步。
- 完整 Go、前端、Linux/Windows 发布验证由 GitHub Actions 执行。

## 2026-07-29：PalServer 崩溃守卫

目标版本：`v1.3.0-custom.0.8.36`

已完成：

- 新增 5 秒运行状态监督器，识别异常退出、OOM 和 Docker restart count 增量。
- 10 分钟内累计 3 次异常时持久熔断，PalPanel 重启后继续阻止启动和安全重启。
- Docker 熔断按“关闭 restart policy → 停止容器”顺序执行，避免 `unless-stopped` 立即拉起。
- PalPanel 发起的停止、重启、配置应用等生命周期操作标记为预期退出，不计入崩溃。
- 观察线程和生命周期线程使用独立状态互斥，防止并发回写丢失预期停服标记。
- 新增崩溃守卫状态、最近事件和确认恢复 API。
- 监控页新增崩溃守卫卡片、最近事件、诊断控制台入口、“仅解除熔断”和“解除并启动”。
- 配置应用和世界重置在熔断期间返回固定冲突，避免进入无法重新启动的半完成事务。
- 熔断时创建严重级别故障告警；恢复时关闭告警并重置统计窗口，历史事件继续保留。
- 事件不记录服务器路径、日志正文或玩家数据。

验证：

- 新增 Docker 重启循环、OOM、手动停服排除、持久熔断和恢复测试。
- 新增并发预期停服标记不被观察线程覆盖的回归测试。
- 新增数据库迁移、事件保留、无效事件拒绝和前端 API 测试。
- OpenAPI、生成契约、维护指南、计划、已知问题和中文 Release 说明已同步。
- 完整 Go、前端、Linux/Windows 发布验证由 GitHub Actions 执行。

## 2026-07-29：存档索引快照与差异分析

目标版本：`v1.3.0-custom.0.8.34`

已完成：

- 每次成功索引自动记录脱敏 gzip 快照，相同指纹不重复。
- 快照按当前世界目录哈希隔离，切换服务器存档和导入存档不会混合历史。
- 新增快照列表和差异 API，支持玩家、公会、基地、帕鲁、容器和物品总量。
- 新增类别、关键词、limit 和 offset 筛选，单次最多返回 500 项。
- 新增“存档差异”页面，可选择两个时间点并查看变化汇总和字段明细。
- 快照清除存档路径、玩家 IP、Ping 和原始解析载荷；API 不返回内部归档 SHA-256。
- 快照读取校验 SHA-256、schema、世界键、ID 和指纹，拒绝路径穿越与篡改文件。
- 默认保留 24 份、单份最多 128 MiB、总压缩体积最多 512 MiB。

验证：

- 新增脱敏、权限、同指纹去重、路径穿越和快照篡改测试。
- 新增玩家等级、帕鲁新增、容器内容和物品总量差异测试。
- 新增 API 私密字段过滤、范围校验和筛选测试。
- OpenAPI、生成契约、路由契约和前端 API 测试已同步。
- 完整 Go、前端、Linux/Windows 发布验证由 GitHub Actions 执行。

## 2026-07-29：配置修订历史与安全回滚

目标版本：`v1.3.0-custom.0.8.32`

已完成：

- 新增数据库迁移和私密配置修订文件库，默认保留最近 50 个版本。
- 配置应用前记录活动基线，健康检查成功后记录新修订、父 SHA-256 和修改字段。
- 新增修订列表、字段差异和生成回滚草稿 API。
- 普通字段显示历史值与当前值；管理员密码和服务器密码按字段名大小写无关规则只返回“已配置/未配置”。
- 配置应用事务进行中拒绝生成回滚草稿，也不会把尚未通过健康检查的临时文件记录为正式修订。
- 修订文件读取和清理均校验固定的 `DataDir/config-revisions/<revision-id>.ini` 路径，拒绝数据库路径重定向。
- 恢复历史版本不会直接覆盖活动文件，而是生成草稿并复用原有停服、启动、健康检查和失败自动恢复事务。
- 新增设置页“配置修订历史”面板，可比较版本、生成草稿并提交回滚任务。
- 同步 OpenAPI、生成契约、维护指南、计划、已知问题和中文 Release 说明。

验证：

- 新增数据库修订持久化与保留清理测试。
- 新增 API 密码脱敏、字段差异、应用中回滚拒绝和回滚草稿不修改活动文件测试。
- 新增并发去重、临时配置不入库、快照 SHA-256 篡改及路径重定向拒绝测试。
- 完整 Go、前端、OpenAPI 和发布包检查由 GitHub Actions 执行。

## 2026-07-29：CI 覆盖率门槛

- Go 包覆盖率最低门槛统一调整为 50%。
- 仍由 GitHub Actions 执行完整 Linux、Windows、前端和安全检查。

## 2026-07-29：双模式面板更新与健康回滚

目标版本：`v1.3.0-custom.0.8.31`

已完成：

- 保留 `0.8.29` 的 systemd 外部完整包更新器。
- 恢复原补丁使用的 `syscall.Exec` 热更新通道，保持面板 PID、参数和环境变量不变。
- `auto` 模式优先选择 external；无 systemd 或非版本化安装时自动选择 exec。
- exec 模式校验官方 `SHA256SUMS`、包内 `checksums.txt`、候选 `--version` 和 `panel-update.json`。
- Release 必须明确声明只需替换 `bin/palpanel`；涉及侧车或安装结构时拒绝 exec 热更新。
- 新进程连续三次验证 `/api/ready` 和 `/api/patch/info` 的目标版本后才提交事务。
- 启动错误、监听失败、就绪超时、目标版本不匹配或二进制校验异常时，自动恢复旧主程序并再次 `exec`。
- exec 更新不停止 PalServer、`sav-cli` 和 `palcalc-bridge`；外层启动脚本无需修改。
- 更新状态接口新增当前模式和模式说明。
- 同步计划、问题记录、OpenAPI、前端契约、发布说明和更新接口文档。

验证：

- 新增 Release 热更新能力清单及主程序范围测试。
- 新增 `/api/ready` 与目标版本联合探测测试。
- Shell 脚本和 Release 能力清单静态验证通过。
- 完整仓库 Go 测试由 GitHub Actions 使用仓库指定的 Go `1.25.12` 执行。

## 2026-07-29：诊断控制台

目标版本：`v1.3.0-custom.0.8.30`

已完成：

- 新增独立诊断控制台页面。
- 新增仅允许回环与私网地址的 HTTP/HTTPS 接口测试。
- 新增默认关闭、需环境变量显式启用的主机终端执行。
- 两类执行都限制为 15 秒和 64 KiB 输出，并写入操作审计。
- 同步 OpenAPI、前端契约、维护指南、接口指南和中文 Release 更新日志。

验证：

- 由 GitHub Actions 验证 Linux、Windows、前端、OpenAPI 契约和安全扫描。

## 2026-07-29：外部更新器与完整包回滚

目标版本：`v1.3.0-custom.0.8.29`

已完成：

- 新增独立 Go 更新器 `palpanel-updater`。
- 新增 systemd 更新服务和路径触发单元。
- 完整校验外层 Release、官方 `SHA256SUMS` 和包内 `checksums.txt`。
- 更新整个版本目录、systemd 单元和更新器，不再只替换主二进制。
- 新版本未通过就绪和版本检查时自动恢复旧版本。
- 更新请求进入 `request.in-progress.json` 后发生进程退出或主机重启时，可继续完成更新；目标目录已经切换时会重新安装运行单元、执行健康验证，失败则恢复旧版本。
- 正式版本目录恢复为 root 只读，Web 服务只写 `/var/lib/palpanel`。
- 安装脚本、发布包检查、CI 单元检查和安装测试已同步调整。
