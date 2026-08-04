# PalPanelBridge 开发路线图

本文件是 `custom-stable` 分支后续 PalPanelBridge 开发的统一优先级、完成标准和交付流程。功能状态以本表和实际 GitHub Actions、实机响应证据为准；历史实现细节记录在 [`feature-update-log.md`](feature-update-log.md)。

## 功能优先级

| 优先级 | 功能 | 当前状态 | 完成标准 |
|---|---|---|---|
| P0 | `/v1/health` 健康检查 | 已完成 | 返回插件版本、UE4SS 状态、游戏线程状态 |
| P0 | `/v1/world` 世界检测 | 已完成 | 返回当前 World 名称和完整路径 |
| P0 | `/v1/players/online` 在线玩家 | 已完成基础版 | 稳定返回玩家名称、UID、PlayerState、Pawn |
| P0 | 在线玩家时间信息 | 已完成 | 返回排队时间、执行时间、响应时间、游戏线程 Tick |
| P0 | 在线玩家异常诊断 | 进行中 | 玩家进入或离开后数据能正确变化，不长期返回旧缓存 |
| P1 | 玩家详细信息 | 字段发现中 | 先通过 `/v1/players/online/metadata` 发现真实字段；位置、等级、公会值仍未读取 |
| P1 | 玩家背包读取 | 待开发 | 返回物品 ID、数量、耐久、容器位置 |
| P1 | 玩家装备读取 | 待开发 | 返回武器、防具、饰品和装备槽 |
| P1 | 玩家帕鲁读取 | 待开发 | 返回帕鲁 UID、名称、等级、技能、被动、工作适应性 |
| P1 | 玩家中心数据对接 | 待开发 | 前端正确显示在线和离线玩家，不重复拆分同一玩家 |
| P2 | 离线玩家/存档玩家读取 | 待开发 | 通过存档索引读取离线玩家基础资料 |
| P2 | 帕鲁词条修改 | 规划中 | 可安全修改等级、技能、被动和工作词条 |
| P2 | 物品和背包修改 | 规划中 | 支持添加、删除、修改数量，并返回任务结果 |
| P2 | 科技解锁和科技点 | 规划中 | 支持普通科技点、古代科技点以及 `/learntech <UserId> all` |
| P2 | 玩家传送和 GM 操作 | 规划中 | 根据实际暴露命令或 UE API 执行，不依赖未经验证的 RCON 假设 |
| P3 | UE4SS 函数/属性诊断 | 部分完成 | 查看对象、函数、属性和参数，辅助后续开发 |
| P3 | 诊断控制台 | 已有基础版 | 测试私网 HTTP 接口，显示状态、响应头和响应体 |
| P3 | 操作审计 | 已有基础版 | 记录调用者、接口、参数摘要、结果和时间 |
| P3 | 插件配置和权限 | 进行中 | Token、监听地址、端口和权限可配置 |

## 统一制作流程

每项功能严格按以下顺序交付，成功后再进入下一项：

1. 修改 `tools/palpanel-bridge` 源码。
2. 更新源码 README、接口文档和中文更新日志。
3. 提交并推送到 `custom-stable`。
4. 使用 GitHub Actions 构建，不在本地编译或测试插件。
5. 运行 `tools/palpanel-bridge/deploy.py`，等待对应 Action、下载并校验构建包，然后通过 SFTP 上传。
6. 使用诊断控制台测试接口。
7. 将实际响应和验证结果写入更新日志。
8. GitHub Actions、部署校验和实机验证均成功后，才进入下一项功能。

## 交付约束

- CI 失败时只读取失败步骤日志并做最小修复；不得上传失败或未经校验的构建。
- 不频繁轮询 GitHub Actions。
- 新构建快照只保存到工作区外层的 `PalPanelBridge/versions`，该目录不是开发源码。
- 部署时只替换远程 `dlls/main.dll`，保留 `config.ini`，不自动重启服务器。
- 任一步骤失败都不得覆盖远程正式 DLL。
- 所有接口响应、日志和人类可读时间统一使用 `Asia/Shanghai`；Unix 时间戳保持原语义。
- 任何写操作类接口必须先明确权限、审计、失败回滚和实机验收边界。
- `/v1/players/online/metadata` 仅用于安全发现首名玩家的 PlayerState/Pawn 顶层字段元数据，最多 96 个属性/对象；不得据此宣称已完成位置、等级或公会详细信息。

## 权威路径

- 开发源码：`tools/palpanel-bridge`
- 自动构建上传脚本：`tools/palpanel-bridge/deploy.py`
- 接口与实机验证文档：`docs/palpanel-bridge.md`
- 中文更新日志：`docs/development/feature-update-log.md`
- 历史构建包：工作区外层 `PalPanelBridge/versions`
