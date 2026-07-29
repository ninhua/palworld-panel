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
- 面板补丁热更新

## 2. 版本规则

项目同时保留上游版本和自定义版本：

```text
上游版本：v1.3.0
自定义版本：0.8.29
完整标签：v1.3.0-custom.0.8.29
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

1. 查询稳定 Release。
2. 选择版本最高且包含 Linux amd64 包与 `SHA256SUMS` 的版本。
3. Web 进程下载并验证归档，再写入受限更新请求。
4. `palpanel-update.path` 启动 root 级 `palpanel-updater`。
5. 更新器重新从官方 Release 获取 `SHA256SUMS`，验证完整包和包内 `checksums.txt`。
6. 安装新的版本目录，更新 systemd 单元和更新器，再原子切换 `current`。
7. 重启全部服务并验证 `/api/ready` 与 `/api/health` 中的目标版本。
8. 健康检查失败时恢复旧版本目录、旧 systemd 单元和旧更新器。

注意事项：

- GitHub API `403 rate limit` 与 TCP 超时不是同一问题。
- API 限流时会回退到 `/releases/latest`。
- `api.github.com` 和 `github.com` 同时连接超时时，必须使用可用网络或代理。
- 从 `0.8.22` 开始，面板更新会使用“系统设置 → 网络代理 → 安装与下载代理”。
- 未启用托管代理时保持直连；设置了 `HTTPS_PROXY` 时，默认 HTTP Transport 可使用环境代理。
- 代理不是强制配置，不能在未启用时偷偷切换到第三方公共镜像。
- Release 下载必须继续校验 `SHA256SUMS`；root 更新器必须独立进行第二次官方校验。
- Linux/Windows 正式包必须包含 `palworld-uid-remap`；打包时必须先构建 helper、计算 SHA-256，再通过 `palpanel/internal/api.hostMigrationHelperSHA256` 注入面板后端。不要调整为后端先构建，否则旧安装无法安全自举 helper。
- 从旧版首次升级到 `0.8.29` 时，需要通过安装脚本或手动运行新版 `palpanelctl install` 安装更新器和 systemd 路径单元。

关键文件：

```text
backend/internal/server/panel_update.go
backend/internal/server/panel_update_test.go
backend/internal/panelupdater/
backend/cmd/palpanel-updater/
scripts/systemd/palpanel-update.service
scripts/systemd/palpanel-update.path
.github/workflows/custom-release.yml
docs/panel-update-api.md
```

## 8. PalDefender 与 GM 命令

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

## 9. 玩家身份归并

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

## 10. 新玩家礼包状态

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

## 11. OpenAPI 与接口维护

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

## 12. 安全边界

- 浏览器不能提交任意 RCON 命令。
- 后端只能暴露有类型、有验证的管理动作。
- GM 写操作必须校验权限和 `Idempotency-Key`。
- 不确定是否成功的写操作不能自动重试。
- 下载更新、Mod 和二进制必须限制大小、重定向和目标地址。
- 不得把 Token、代理密码或 PalDefender REST Token 写入日志、Release 或前端响应。
- 存档解析保持只读，不允许浏览器直接获得原始 `.sav`。
- 更新替换必须保留备份和启动失败回滚能力。

## 13. 提交建议

按功能拆分提交，例如：

```text
feat: add starter gift manual rearm controls
fix: coalesce online and offline player identities
fix: route panel updates through install proxy
fix: handle PalDefender RCON command compatibility
docs: add release notes for 0.8.24
```

同一尚未发布功能可以在发布前整理提交。已经发布的提交不要改写 SHA。

## 14. 发布完成检查表

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
