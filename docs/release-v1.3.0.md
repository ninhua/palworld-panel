# PalPanel v1.3.0

这次更新主要集中在存档、日志和 PalDefender 管理功能。发布包会同时提供 Windows amd64、Linux amd64、源码和 `sav-cli` 源码包。

## 更新内容

- 兼容当前实机验证的 Palworld Dedicated Server `v1.0.1.100619`，对应 Build `24181105`。
- 修复存档索引在玩家身份、存档变化和部分玩家数据异常时的处理，索引失败时会保留可用的上一次结果并显示状态和警告。
- 新增存档归档导入，支持 ZIP、TAR、TAR.GZ 和 TGZ。导入前会检查路径穿越、软链接/硬链接、文件数量、解压大小，并验证 `Level.sav`；归档包含多个世界时，需要先选择要导入的世界。
- 修复 PalDefender 物品发放流程，补充帕鲁、自定义模板和批量发放。发放操作要求 PalDefender 已正确加载、REST 接口已启用，并且目标玩家在线。
- 游戏日志支持按来源查看：游戏日志、启动器日志和 PalDefender REST 日志。
- 在线玩家状态会与存档历史数据合并展示，并按可用的玩家 UID/Steam ID 去重。
- 修复 Docker/Wine 升级时对游戏挂载目录的处理，升级不再递归接管 `server`、`logs` 和 `wineprefix`；已有容器只按配置调整 UID/GID 权限。
- Windows 支持通过配置监听局域网地址，并提供仅允许专用网络和本地子网的防火墙示例。

## 存档说明

导入的存档可以在面板中激活，用于查看和分析。激活只改变面板当前使用的数据源，不会直接覆盖正在运行的游戏存档。当前仍不支持 Xbox WGS 存档。

## 验证

本版本沿用已通过的 Linux、Windows、E2E 和 Docker/Wine 升级回归检查；Windows 实机验证覆盖服务端启停、REST/RCON、备份、`Level.sav` 索引和安全关服流程。

安装和升级步骤见 [README.md](../README.md)。
