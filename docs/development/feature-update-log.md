# 功能移植更新记录

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
