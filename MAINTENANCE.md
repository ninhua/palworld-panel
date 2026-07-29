# PalPanel 自定义源码维护指南

本文用于维护 `ninhua/palworld-panel` 自定义源码 Fork。开始修改、同步上游或发布版本前，请先阅读本文。

## 1. 仓库定位

- 官方上游：`uitok/palworld-panel`
- 自定义源码：`ninhua/palworld-panel`
- 当前上游基线：`v1.3.0`
- 实际维护和发布分支：`custom-stable`
- 纯上游稳定分支：`upstream-stable`
- 临时升级分支：`upgrade/<上游版本>`

禁止直接在 `upstream-stable` 上开发自定义功能。该分支只能指向未经修改的上游稳定版本。

本项目已经从补丁链迁移为完整源码 Fork。不要恢复以下旧流程：

- `.patch` 文件堆叠
- `apply-source-patch.sh`
- 补丁 hunk 重定位
- 补丁目录 SHA256 校验
- `Palworld-Panel-Patches` 补丁 Release 热更新

## 2. 版本规则

项目同时保留上游版本和自定义版本：

```text
上游版本：v1.3.0
自定义版本：0.8.40
完整标签：v1.3.0-custom.0.8.40
```

版本源位于：

```text
backend/internal/api/patch_info.go
```

修改自定义版本时，必须同步：

- `backend/internal/api/patch_info.go`
- `docs/openapi.yaml`
- `frontend/src/api/generated/contracts.ts`
- `docs/panel-update-api.md` 中的当前示例
- `docs/releases/<自定义版本>.md`

不要覆盖已经公开发布的标签和 Release。任何发布后的修复都递增自定义版本。

## 3. 每次发布必须有中文更新日志

每个版本必须新增：

```text
docs/releases/<自定义版本>.md
```

更新日志至少包含：

- 版本号和发布日期
- 新增功能
- 修复问题
- 行为、配置或接口变更
- 升级注意事项
- GitHub Actions 验证范围
- 完整变更比较链接

`.github/workflows/custom-release.yml` 会强制检查该文件。缺少或为空时，Release 工作流必须失败，不能创建只有提交比较链接的空 Release。

已发布 Release 的说明需要与仓库更新日志一致。

## 4. 开发与验证流程

正常修改流程：

1. 在 `custom-stable` 修改源码。
2. 使用 UTF-8 保存所有文件。
3. Go 文件执行 `gofmt`。
4. 执行 `git diff --check`。
5. 按功能创建普通 Git 提交。
6. 推送 `custom-stable`。
7. 手动触发 GitHub Actions 的 `CI`。
8. CI 全部通过后，触发 `Custom package release`。
9. 核对 Release、中文更新日志、Linux/Windows 包和 `SHA256SUMS`。

本项目默认使用云端 Actions 完成编译和测试，不要求在维护机器本地运行完整测试或构建。不要因为未执行本地构建而跳过云端完整验证。

PalPanelBridge 的固定 UE4SS 提交、安装目录、鉴权请求和故障判断见：

```text
docs/palpanel-bridge.md
```

Bridge 是只读技术探针。扩展任何游戏数据修改接口前，必须先完成 health、游戏线程任务队列和鉴权边界验证。

完整 CI 应覆盖：

- Go 测试和竞态检查
- 前端类型、测试和构建
- Linux 构建、安装和冒烟验证
- Windows 构建、升级、卸载、恢复及离线 E2E
- Go 覆盖率门禁
- 依赖漏洞检查
- Secret、Shell 和 systemd 检查

Release 工作流应覆盖：

- 自动读取上游和自定义版本
- 校验 Release 不存在
- 校验中文更新日志存在
- Linux amd64 打包与验证
- Windows amd64 打包与验证
- 源码包与 sav-cli 源码包
- `SHA256SUMS`
- 正式 GitHub Release

## 5. CI 已知注意事项

### Windows 配置草稿过期测试

`TestConfigDraftMaintenanceExpiresAtStartupAndOnTicker` 涉及真实时间和定时器，曾在 Release Windows 打包中偶发竞态失败，而同一提交的完整 Windows CI 可以通过。

处理顺序：

1. 先查看失败日志，确认是否只有该测试失败。
2. 若同一提交的完整 Windows CI 已通过，可先只重跑失败任务。
3. 若连续复现，不要反复重跑，应修复测试的时间依赖。

### Actions Node.js 弃用警告

GitHub Runner 可能提示 `actions/checkout@v4` 等 Action 的 Node.js 20 弃用警告。这是警告，不应当作构建失败，但后续应统一升级官方 Action 主版本。

### Go 缓存警告

仓库根目录没有 `go.sum` 时，某些 Action 可能提示根目录依赖缓存无法恢复。实际 Go 模块位于子目录。除非任务因此失败，否则不要误判为业务构建错误。

## 6. 上游同步

上游发布新稳定版本后：

```bash
git fetch upstream --tags
git switch upstream-stable
git reset --hard <新上游标签>
git push --force-with-lease origin upstream-stable

git switch custom-stable
git switch -c upgrade/<新上游标签>
git merge --no-ff upstream-stable
```

解决冲突并完成云端 CI 后：

```bash
git switch custom-stable
git merge --ff-only upgrade/<新上游标签>
git push origin custom-stable
git branch -d upgrade/<新上游标签>
```

长期发布分支使用 merge，不要对已经公开的自定义历史执行 rebase。

同步上游时重点检查：

- `backend/internal/api/patch_info.go`
- `.github/workflows/`
- `backend/internal/server/panel_update.go`
- `backend/internal/startergift/`
- `backend/internal/paldefender/`
- `backend/internal/playerpresence/`
- `frontend/src/pages/StarterGift.tsx`
- `frontend/src/pages/PlayerCenter.tsx`
- OpenAPI 与前端生成契约

## 7. 面板自身更新

面板更新源固定为：

```text
ninhua/palworld-panel
```

更新流程：

1. 查询稳定 Release，下载 Linux amd64 归档和 `SHA256SUMS`。
2. Web 进程校验外层归档、候选二进制版本和包内 `checksums.txt`。
3. `PALPANEL_UPDATE_MODE=auto` 自动选择更新通道。
4. systemd 正式安装检测到 root 更新器时，走外部完整包切换、服务重启和健康回滚。
5. 无 systemd、但当前二进制目录可写时，要求 Release 的 `panel-update.json` 明确声明仅替换 `bin/palpanel`，然后同目录原子替换并通过 `syscall.Exec` 保持 PID。
6. exec 新进程启动后连续验证 `/api/ready` 与 `/api/patch/info` 的目标版本；启动函数提前报错、监听失败或健康超时均恢复旧二进制并再次 `exec`。
7. Release 声明需要同步更新侧车或其他文件时，exec 通道必须拒绝并提示使用外部完整包更新。

注意事项：

- GitHub API `403 rate limit` 与 TCP 超时不是同一问题。
- API 限流时会回退到 `/releases/latest`。
- `api.github.com` 和 `github.com` 同时连接超时时，必须使用可用网络或代理。
- 从 `0.8.22` 开始，面板更新会使用“系统设置 → 网络代理 → 安装与下载代理”。
- 未启用托管代理时保持直连；设置了 `HTTPS_PROXY` 时，默认 HTTP Transport 可使用环境代理。
- 代理不是强制配置，不能在未启用时偷偷切换到第三方公共镜像。
- Release 下载必须继续校验 `SHA256SUMS`；root 更新器必须独立进行第二次官方校验。
- Linux/Windows 正式包必须包含 `palworld-uid-remap`；打包时必须先构建 helper、计算 SHA-256，再通过 `palpanel/internal/api.hostMigrationHelperSHA256` 注入面板后端。不要调整为后端先构建，否则旧安装无法安全自举 helper。
- exec 热更新只能接受 `panel-update.json` 中 `required_files=["bin/palpanel"]` 的 Release；不能默认为所有 Release 都可单文件热更。
- `PALPANEL_UPDATE_MODE` 支持 `auto`、`external`、`exec`；默认 `auto`。
- 从旧版首次升级到 `0.8.29` 时，需要通过安装脚本或手动运行新版 `palpanelctl install` 安装更新器和 systemd 路径单元；无 systemd 环境可直接使用 exec 通道。

关键文件：

```text
backend/internal/server/panel_update.go
backend/internal/server/panel_update_test.go
backend/internal/server/panel_update_exec_linux.go
backend/internal/panelupdater/
backend/cmd/palpanel-updater/
scripts/systemd/palpanel-update.service
scripts/systemd/palpanel-update.path
.github/workflows/custom-release.yml
docs/panel-update-api.md
```

## 8. PalWorld 配置修订历史

`PalWorldSettings.ini` 的编辑继续使用“草稿 → 应用 → 健康检查 → 失败恢复”事务。修订历史只能扩展该事务，不能绕过草稿直接覆盖活动配置。

维护规则：

- 私密快照存放在 `DataDir/config-revisions`，文件权限必须为 `0600`，目录权限必须为 `0700`。
- API 只能返回修订 ID、SHA-256、时间、来源和字段差异；不得返回快照路径或密码原文。
- `AdminPassword` 与 `ServerPassword` 的差异按字段名大小写无关规则只能显示“已配置/未配置”。
- 修订快照路径必须严格等于 `DataDir/config-revisions/<revision-id>.ini`；读取、淘汰和回滚均不得接受数据库中的任意受管路径。
- 配置应用 journal 存在时不得捕获当前磁盘文件为正式修订，也不得生成新的回滚草稿。
- 恢复历史版本必须先生成草稿，再调用现有 `/api/config/palworld/apply`；应用失败继续由原事务恢复应用前配置。
- 成功应用前必须保存当前基线，成功健康检查后才写入新的活动修订。
- 默认只保留最近 50 个修订；删除的私密快照使用可重试清理队列处理。

关键文件：

```text
backend/internal/db/config_revisions.go
backend/internal/server/config_revisions.go
backend/internal/api/palworld_config_revisions.go
frontend/src/components/ConfigRevisionHistory.tsx
```

## 9. 存档索引快照与差异

存档差异只比较 `sav-cli` 已生成的结构化索引，不复制、修改或下载原始 `.sav`。快照按当前世界目录的不可逆哈希隔离。

维护规则：

- 每次成功索引可记录一份 gzip JSON；相同指纹不得重复保存。
- 快照目录权限必须为 `0700`，文件和清单权限必须为 `0600`。
- 保存前必须清除 `source_path`、玩家 IP、Ping 和原始解析载荷；API 不得返回私密目录或内部归档 SHA-256。
- 快照 ID 必须匹配固定时间戳和 32 位指纹格式；读取不得接受任意路径。
- 读取快照前必须校验压缩文件 SHA-256、schema、世界键、快照 ID 和指纹。
- 每个世界默认保留 24 份，总压缩体积最多 512 MiB，单份最多 128 MiB。
- 差异只比较同一当前存档源下的两份快照；切换存档源后不得跨世界读取。
- 差异接口必须分页并限制最大 500 项，不能直接返回整个索引。

关键文件：

```text
backend/internal/saveindex/history.go
backend/internal/api/save_history.go
frontend/src/pages/SaveHistory.tsx
```

## 10. PalServer 崩溃守卫

崩溃守卫由 PalPanel 进程自身运行，不依赖 systemd。默认每 5 秒观察一次，10 分钟内累计 3 次异常退出、OOM 或容器重启时熔断。

维护规则：

- PalPanel 发起停止、重启和配置应用前必须写入短时“预期退出”标记，避免正常操作被计为崩溃。
- 观察线程与生命周期线程对守卫状态的读改写必须串行化，不能用旧状态覆盖预期停服或恢复时间。
- Docker 熔断必须先执行 `docker update --restart=no`，再停止容器；顺序不可颠倒。
- 熔断状态必须持久化，普通启动和重启入口必须返回 `crash_guard_tripped`，不能因重启面板而绕过。
- 恢复必须由具有 `server:control` 权限的管理员显式确认，可选择仅解除熔断或同时启动；恢复后以确认时间重置统计窗口并关闭对应告警，恢复启动失败时重新熔断。
- 配置应用和世界重置在熔断期间必须拒绝，避免已停止服务后因启动阻断留下半完成事务；服务端更新可以继续执行，但不得自动重新启动。
- 事件只保存运行模式、退出码、OOM、restart count 和时间，不保存日志正文、文件路径、命令参数或玩家数据。
- 默认保留最近 50 条事件。阈值和窗口修改必须同步 API、前端说明和测试。

关键文件：

```text
backend/internal/db/crash_guard.go
backend/internal/server/crash_guard.go
backend/internal/api/crash_guard.go
frontend/src/pages/Monitor.tsx
```

## 11. 通知与事件中心

事件中心把监控告警、崩溃守卫和失败任务归并为可跟踪的故障对象。它不是日志镜像，禁止存储原始日志、文件路径、命令行或未脱敏错误正文。

维护规则：

- 相同根因必须使用稳定 `dedupe_key` 归并，重复发生只增加次数和时间线；已解决事件再次发生时自动重新打开。
- 事件状态仅允许 `open`、`acknowledged`、`resolved`；确认、解决和重新打开必须写入时间线并进入操作审计。
- 后台任务事件只保存任务 ID、任务类型和稳定错误码，不保存 `job.error` 原文。
- 监控告警的确认与解决必须同步到对应事件；崩溃守卫恢复时必须解决 `crash-guard` 来源事件。
- Webhook 只能通过环境变量启用，远程目标必须使用 HTTPS；HTTP 只允许回环地址。URL 不允许用户信息、查询参数或片段。
- Webhook 请求必须使用 HMAC-SHA256 签名，签名密钥不得返回 API、日志或前端；载荷只包含脱敏事件、状态变更和固定产品元数据。
- 投递采用至少一次语义，接收方必须按 `X-PalPanel-Delivery` 去重。禁止因投递失败再创建新的事件，避免递归风暴。
- 最多重试 6 次；已解决事件默认保留 90 天。事件写入和状态转换必须串行化，避免并发唯一键冲突或状态覆盖。

关键文件：

```text
backend/internal/db/incidents.go
backend/internal/incidents/service.go
backend/internal/api/incidents.go
frontend/src/pages/Incidents.tsx
```

## 12. PalOps 世界地图资源

世界地图固定使用 `CoderYiXin/PalOpsWeb` 1.3.2 提交 `dc2ec173c77e759482e59d9b63d228c88132061c` 的数据模型。只迁移前端地图、固定 POI、坐标和资源清单，不引入 ASP.NET Core、SignalR 或 PalOps 账户系统。

维护规则：

- `scripts/sync_palops_map_assets.py` 必须固定仓库、提交和 POI 总数，禁止跟随 `main` 或下载任意 URL。
- `scripts/sync_maplibre_assets.py` 必须固定 MapLibre GL JS 6.0.0，只同步 ESM、shared、worker、CSS 和 BSD 许可证；浏览器不得从 CDN 加载运行时代码。
- 三种语言 POI 必须均为 1,251 条，ID、地图和四组坐标必须完全一致。
- 归档提取必须拒绝路径穿越、符号链接、未知扩展名和超限文件。
- PalOps 当前栅格瓦片元数据声明 `redistributionAllowed=false`，不得默认打包或在 CI 中静默复制。
- 导入完整瓦片必须使用管理员提供的本地目录；每个地图层必须恰好包含 341 张 `.webp`。
- 地图不允许 iframe、远程脚本或浏览器直连 PalOps 后端；服务器动态图层只能使用 PalPanel 自己的受认证 API。
- PalOps 来源、提交、版本、数据集和每个 POI 的许可证必须在页面或随包说明中保留。

关键文件：

```text
frontend/src/pages/LiveMap.tsx
frontend/src/components/map/PalOpsMapViewport.tsx
frontend/src/map/palopsMap.ts
scripts/sync_palops_map_assets.py
scripts/sync_maplibre_assets.py
```

## 13. PalDefender 与 GM 命令

PalDefender 同时使用 REST 和 Source RCON：

- REST：玩家、物品读取、物品与帕鲁发放、消息、处罚等。
- RCON：传送、删除物品、白名单、管理员状态、目录发现等。

不要因为 HTTP 状态为 `200` 就认定 RCON 命令成功。必须检查 PalDefender 返回文本。

需要判定为失败的典型输出：

```text
Unknown command
Command not found
Invalid command
```

从 `0.8.23` 开始：

- 带 `/` 的命令明确返回 `Unknown command` 时，兼容重试一次不带 `/` 的格式。
- 只有明确未执行时才允许重试。
- 超时、连接断开、无响应或结果不确定时，禁止自动重试破坏性命令。
- 两种格式均失败时返回 `paldefender_rcon_command_rejected`，审计状态必须为失败。

PalDefender 显示“已安装、已加载、REST 已启用”仍不代表某条 RCON 命令一定可用。运行时能力应以 `/getrconcmds` 返回结果为准。

关键文件：

```text
backend/internal/paldefender/rcon.go
backend/internal/paldefender/gm_actions.go
backend/internal/api/paldefender_gm.go
frontend/src/pages/PalDefenderGM.tsx
frontend/src/pages/PlayerCenter.tsx
```

修改传送、删除、发放等操作时，必须同时检查：

- 命令格式
- 玩家标识类型
- 在线状态
- RCON/REST 能力
- 失败文本
- 幂等键
- 审计结果
- 是否存在危险的自动重试

## 14. 玩家身份归并

同一个玩家可能同时出现：

- SteamID，例如 `steam_765...`
- PlayerUID，带或不带连字符
- PalDefender UserId
- 存档中的离线身份
- 在线 REST 中的实时身份

不能直接按原始字符串创建玩家卡片。必须先标准化并归并别名。

特别注意：

- UUID 大小写和连字符差异不能产生两个玩家。
- 离线存档记录与上线后的 SteamID 记录必须合并。
- 归并应传播 `seen`、`online`、`rearm` 和礼包任务关联。
- 玩家中心、新玩家礼包和审计对象必须使用一致的身份规则。
- 玩家中心的玩家、详情、背包和帕鲁必须读取同一个 `source=server` 存档索引，不能混用当前激活导入存档。
- 背包容器和帕鲁归属匹配必须兼容 PlayerUID 的 UUID/紧凑 GUID 格式，以及 SteamID 的 `steam_` 前缀差异。
- 不要把昵称作为唯一身份；昵称只可作为辅助证据。

关键文件：

```text
backend/internal/playerpresence/
backend/internal/startergift/service.go
frontend/src/pages/PlayerCenter.tsx
frontend/src/pages/StarterGift.tsx
```

## 15. 新玩家礼包状态

礼包状态至少区分：

- 已见玩家 `seen`
- 当前在线 `online`
- 下次进入重新触发 `rearm`
- 已有发放记录 `grant`

必须遵守：

- 设置“下次进入视为新玩家”不能删除已有发放记录。
- 设置后要立即显示取消按钮。
- 取消只清除 `rearm`，不清除 `seen` 和发放历史。
- 当前在线玩家需要先观察到离线，再次上线才触发。
- 真实触发新任务时可重置任务进度，但必须保留历史事件日志。
- 有发放记录且处于 `rearm` 时，玩家判定优先显示“等待下次进入”，同时保留任务状态。
- 旧版已删除的数据不能假装可以恢复。
- 礼包任务创建时必须冻结物品、帕鲁模板和科技计划；修改全局配置不能改变已经排队的任务。
- 全科技使用 PalDefender `learntech <UserId> all` 语义，接口层标准化为 `All`；不要使用 `*`。
- 科技点必须区分普通科技点与古代科技点，并分别传给 PalDefender progression 接口。

模板筛选规则：

- 同一维度多选为 OR。
- 不同维度之间为 AND。
- `+` 与 `＋` 组合用途要拆分匹配。
- 选择“手工作业”必须能匹配“手工作业 + 播种”等复合用途。
- 必须提供一键清空筛选。

## 16. OpenAPI 与接口维护

后端接口变更时必须同步：

- 路由实现
- 请求/响应类型
- `docs/openapi.yaml`
- `frontend/src/api/generated/contracts.ts`
- 对应接口测试
- `docs/panel-update-api.md` 或其他对接文档

机器人对接入口和使用方式应保持在仓库文档中，不能只存在于聊天记录。

错误码应区分：

- 网络不可达
- 超时
- 认证失败
- 功能未配置
- 玩家离线或不存在
- 上游返回无效响应
- 命令被拒绝
- 请求参数无效

不要用一个笼统的 `500` 覆盖所有失败。

## 第三方镜像与离线构建

第三方源码和大型资源不得依赖未锁定的运行时下载。`third_party/vendor-lock.json` 固定镜像布局，`scripts/vendorctl.py` 负责初始化、逐文件 SHA-256 清单、验证、准备和清理。

构建模式：

- `online`：使用本地源码，缺少地图资源时允许访问固定上游。
- `mirror`：优先使用 `PALPANEL_VENDOR_ROOT` 中的 PalOps、MapLibre、PalCalc 和 uesave，缺少缓存时仍允许公共包源。
- `offline`：要求完整源码和 npm/Cargo/Go/NuGet 缓存，设置包管理器离线开关并禁止 Go 代理。

镜像目录不得包含符号链接、设备文件、Token 或私人授权书原件。Palpagos / World Tree 瓦片只有在授权允许再分发时才能放入 `assets/palops-map-tiles`；公开仓库只保留授权摘要和原件 SHA-256。

`vendorctl prepare` 只会创建带 `.palpanel-vendor-managed` 标记的 `third_party/palcalc` 和 `third_party/uesave`。清理时不得删除没有该标记的人工工作目录。

## 17. 安全边界

- 浏览器不能提交任意 RCON 命令。
- 后端只能暴露有类型、有验证的管理动作。
- 诊断控制台的私网 HTTP 请求只允许回环或私网目标；主机终端必须由环境变量显式启用，并只接受管理员浏览器会话、固定超时与输出上限。发布配置不得默认启用。
- GM 写操作必须校验权限和 `Idempotency-Key`。
- 不确定是否成功的写操作不能自动重试。
- 下载更新、Mod 和二进制必须限制大小、重定向和目标地址。
- 不得把 Token、代理密码或 PalDefender REST Token 写入日志、Release 或前端响应。
- 存档解析保持只读，不允许浏览器直接获得原始 `.sav`。
- 更新替换必须保留备份和启动失败回滚能力。

## 18. 提交建议

### PalPanelBridge 技术预览

- `tools/palpanel-bridge` 当前只能执行只读游戏线程探针，禁止在验证持久化、复制和回滚前加入帕鲁或背包写操作。
- DLL 必须使用与运行时完全一致的 UE4SS v3.0.1 SDK 和 Release CRT 构建；UE4SS 官方升级时必须重新构建。
- UE4SS v3.0.1 SDK 依赖受限的 UEPseudo 仓库；`PalPanelBridge build` Action 只能通过仓库 Secret `UEPSEUDO_TOKEN` 读取，Token 不得写入源码、日志或产物。本地构建脚本仅作为备用方式。
- HTTP 只能绑定 `127.0.0.1`，必须配置随机 Bearer Token，不得增加任意 UObject/UFunction 执行接口。
- 网络线程只能排队，Unreal 对象访问必须在 `on_update` 游戏线程回调中执行。

按功能拆分提交，例如：

```text
feat: add starter gift manual rearm controls
fix: coalesce online and offline player identities
fix: route panel updates through install proxy
fix: handle PalDefender RCON command compatibility
docs: add release notes for 0.8.24
```

同一尚未发布功能可以在发布前整理提交。已经发布的提交不要改写 SHA。

## 19. 发布完成检查表

- [ ] 自定义版本已递增
- [ ] OpenAPI 与前端契约已同步
- [ ] `docs/releases/<版本>.md` 已创建
- [ ] 中文更新日志描述与实际提交一致
- [ ] `git diff --check` 通过
- [ ] `custom-stable` 已推送
- [ ] 完整 CI 的 Linux、Windows、vulnerability 全部成功
- [ ] Custom package release 全部成功
- [ ] Release 不是 Draft 或 Prerelease（除非明确要求）
- [ ] Linux amd64 包存在
- [ ] Windows amd64 包存在
- [ ] 源码包与 sav-cli 源码包存在
- [ ] `SHA256SUMS` 存在
- [ ] Release 页面显示中文更新日志
- [ ] 仓库工作区无意外未提交文件
