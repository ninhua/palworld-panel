# PalPanel  | 幻兽帕鲁管理面板

<p align="center">
  <img src="frontend/public/brand/palpanel-lockup.svg" width="360" alt="PalPanel · Server Control">
</p>

PalPanel 是给《幻兽帕鲁》专用服务器用的自托管面板。后端用 Go，前端用 React，存档解析由 `sav-cli` 处理，配种计算通过独立的 `palcalc-bridge` 运行。

PalPanel 用来管理《幻兽帕鲁》专用服务器：服务端启停与更新、备份、Mod、存档索引、配种查询和 PalZones 区域权限编辑集中在一个面板里，同时支持多存档、v26 帕鲁图标、简体中文/English 切换、PalDefender GM、WebDAV 归档和 AstrBot QQ 插件。

本仓库是基于 [`uitok/palworld-panel`](https://github.com/uitok/palworld-panel) 维护的自定义源码 Fork。实际发布源码位于 `custom-stable`，`upstream-stable` 只用于同步官方稳定版本。面板完整版本采用 `v<上游版本>-custom.<自定义版本>` 格式。当前版本：`v1.3.1-custom.0.8.67`（已同步上游 v1.3.1 帕鲁图标、存档迁移、PalZones 编辑器等）。

维护本 Fork、同步上游或发布新版本前，请先阅读 [`MAINTENANCE.md`](MAINTENANCE.md)。其中记录了版本与分支规则、强制中文更新日志、Actions 发布流程，以及面板更新、PalDefender、玩家身份归并和新玩家礼包等高风险注意事项。

<p align="center">
  <a href="https://github.com/ninhua/palworld-panel/releases"><img alt="GitHub Release" src="https://img.shields.io/github/v/release/ninhua/palworld-panel?display_name=tag&sort=semver"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-GPL--3.0--or--later-356a9a"></a>
  <img alt="Windows amd64" src="https://img.shields.io/badge/Windows-amd64-607d9b">
  <img alt="Linux amd64" src="https://img.shields.io/badge/Linux-amd64-b06f52">
</p>

## 界面

![服务器总览](docs/images/dashboard-new.png)

<p align="center">
  <img src="docs/images/save-sources-new.png" width="49%" alt="存档中心">
  <img src="docs/images/breeding-lab-new.png" width="49%" alt="配种实验室">
</p>

<p align="center">
  <img src="docs/images/live-map-new.png" width="49%" alt="世界地图（截图待更新）">
  <img src="docs/images/backups-webdav-new.png" width="49%" alt="备份与 WebDAV">
</p>

<p align="center">
  <img src="docs/images/breeding-mobile-new.png" width="360" alt="移动端配种页面">
</p>

## 已经能做什么

### 服务器维护

- 在 Windows 上通过 SteamCMD 安装或接管现有 `PalServer.exe`
- 在 Linux 上使用 Docker + Wine 管理服务端
- 启动、停止、保存世界、安全重启、检查更新和查看日志；日志支持按来源查看：游戏日志、启动器日志和 PalDefender REST 日志
- 查看 CPU、内存、磁盘、在线人数、Server FPS 和运行时间
- 编辑启动参数与 `PalWorldSettings.ini`
- 管理 Workshop、Pak、LogicMods、UE4SS 和 PalDefender
- 配置每日或按间隔执行的安全重启与自动备份
- 创建、校验、下载和恢复备份，并在备份完成后上传到 WebDAV
- 通过后端缓存查询中国区或全球可发现社区服务器；国内网络可配置 HTTP/HTTPS/SOCKS5 代理或自建 API 镜像
- 在“系统设置 → 网络与代理”中分别配置公共下载/服务器更新代理和社区服查询代理；前者覆盖 SteamCMD、Workshop、GitHub/HTTP(S) MOD、PalDefender 与 UE4SS，代理密码不会回显，保存后从下一次任务生效
- 使用保存世界、广播倒计时、正常退出和受控兜底组成的安全关服流程
- 为 `PalWorldSettings.ini` 保留私密修订历史，支持脱敏字段差异、回滚草稿和健康失败自动恢复
- 为当前存档源保留脱敏结构化索引快照，把变化解释为玩家升级、装备与物品增减、帕鲁获得/转移、据点扩建/拆除、工作帕鲁与公会成员事件，并保留原始字段差异用于审计
- 使用 PalPanel 专用地图资源仓库提供双地图瓦片与固定 POI，玩家/据点动态图层完全在本地加载
- 使用崩溃守卫识别短周期异常退出、OOM 和 Docker 重启循环，自动暂停重启并提供确认恢复入口
- 使用统一通知与事件中心归并监控告警、崩溃熔断和失败任务，支持确认、解决、重新打开及可选签名 Webhook
- 在 Linux amd64 上从本仓库 Release 更新面板：systemd 安装走独立 root 完整包更新器，无 systemd 的可写便携环境走保持 PID 的 `syscall.Exec` 热更新；两种模式都执行版本与就绪检查并支持失败回滚

### 存档与地图

- 世界地图使用自托管 MapLibre GL JS 5.24.0（WebGL2 / WebGL1 自动选择）；瓦片、固定 POI、图标和许可证由 `ninhua/palpanel-assets` 在构建时同步，浏览器运行时不访问 CDN 或 GitHub。
- 主机角色迁移以后台生命周期任务执行，页面断开或反向代理短暂不可用不会中止迁移。

- 把当前服务器世界作为内置存档源
- 导入带有 `Level.sav` 的标准 ZIP、TAR、TAR.GZ 或 TGZ 存档
- 切换、重命名、重建和删除导入的数据源
- 通过五步迁移向导把旧存档玩家自动迁移到新 SteamID；自动识别 Steam/NoSteam UID 模式，预检目标冲突，停服前创建完整备份，失败时自动回滚
- 使用 `sav-cli` 为服务器和导入存档建立索引；存档文件变化会标记索引过期，有可用缓存时会保留上一次成功的索引，并显示重建错误和警告
- 查询玩家、公会、基地、容器和帕鲁
- 读取帕鲁的性别、IV、星级、技能、被动词条、主人和所在容器
- 使用 Palpagos / World Tree 双地图、资源仓库固定 POI、搜索、探索进度和动态图层显示玩家、据点及存档实体

导入流程是先上传归档，再在检查页确认结果。面板会检查路径穿越、软链接/硬链接、文件数量和解压大小，并验证 `Level.sav` 是否可读取、非空且能被索引器解析；归档里有多个世界时，需要选择具体世界后才能导入。导入后的存档可以激活，用于面板查看和分析，但激活不会直接覆盖正在运行的游戏存档。当前支持 Steam 与 Palworld Dedicated Server 存档，不支持 Xbox WGS。

### 玩家与 GM

- 在线玩家状态会和存档历史数据合并展示，并按可用的玩家 UID/Steam ID 去重
- PalDefender 支持物品、帕鲁和自定义模板发放，也支持批量发放
- 发放功能需要 PalDefender 已安装并通过启动日志确认加载，PalDefender REST 已启用，且目标玩家在线；缺少这些条件时面板会拒绝请求

### 配种实验室

项目固定使用 [PalCalc v1.17.6](https://github.com/tylercamp/palcalc/tree/v1.17.6)，上游提交为 `8b7e2f779e47fddae16ddcb973e828ba20c02b80`。

- 设置目标帕鲁、性别、必需/可选被动与 IV 下限
- 从玩家、容器、自定义帕鲁或允许的野生帕鲁中选材料
- 调整最大步数、迭代数、线程数、无关词条和手术词条等参数
- 排队、暂停、继续或取消计算任务
- 查看候选路线、概率、蛋数、预计时间和完整配种树
- 存档变化后保留旧结果，同时标记可能过期的路线

PalCalc 通过 .NET 9 侧车运行。侧车不可用时只会关闭配种功能，不影响服务器管理和存档浏览。

### Mod 配置中心

- PalDefender、UE4SS Experimental、PalSchema、Extended Base Range、QualityOfLife 和 PalZones 提供专用配置界面
- 其他已识别 Mod 可以安全编辑受支持的 UTF-8 文本配置，并查看差异、备份时间线和恢复入口
- Lua 保存需要额外确认；二进制文件、Pak、DLL、清单文件、符号链接和 Mod 根目录外文件不可编辑
- 当前 Workshop 热门候选审查见 [`docs/workshop-mod-candidates-2026-07-18.md`](docs/workshop-mod-candidates-2026-07-18.md)

### AstrBot QQ 插件

插件在 [`astrbot_plugin_palpanel`](astrbot_plugin_palpanel)，目标环境为 AstrBot `>=4.18,<5`、NapCat/OneBot v11（`aiocqhttp`）。已经实现的命令包括：

| 命令 | 用途 |
| --- | --- |
| `/bd <游戏昵称>` | 请求游戏内绑定验证码 |
| `/bdqr <验证码>` | 确认 QQ 与 PlayerUID 的绑定 |
| `/qd` | 每日签到 |
| `/jf` | 查看积分与近期流水 |
| `/pz` | 生成一次性面板链接 |
| `/pz <目标帕鲁> [被动词条...]` | 提交快捷配种计算 |
| `/服状态`、`/在线` | 查询服务器状态与在线玩家 |
| `/房间 [关键词]` | 查询后端可发现的中国区社区服务器 |
| `/开服`、`/关服 [秒]`、`/重启 [秒]`、`/强关` | `admin_qq_ids` 管理员执行服务器控制 |
| `/paladmin ...` | 人工绑定、解绑、冻结和积分调整 |

默认签到奖励 10 分，成功计算消耗 1 分。计算失败、取消或超时会退回预留积分。验证码只保存哈希，5 分钟后失效，并要求玩家在线且 PalDefender 能发送私聊消息。

插件的存储、积分并发约束和 HMAC 签名单元测试已经通过；NapCat、真实 QQ 群与在线玩家的完整联调仍需要部署者自己的机器人和群环境。

## 实机验证

2026-07-18 在 Windows 测试服上使用 Palworld Dedicated Server Build `24181105`、游戏版本 `v1.0.1.100619` 做过以下检查：

- 面板识别并启动现有服务端
- 官方 REST `info/players` 和 RCON `Info/ShowPlayers/Save` 均返回真实服务端响应
- RCON 保存世界后 `.sav` 文件实际更新时间发生变化
- 在线创建备份并通过 manifest/hash 校验
- 使用 Windows CGO 版 `sav-cli` 解析真实 `Level.sav`
- 安全重启完成，重启前后的 PalServer PID 不同，REST/RCON 随后恢复
- 官方 REST 直接关服能够退出真实 PalServer 进程
- 面板安全关服完成保存、倒计时、正常退出和审计验证，没有使用托管强停兜底
- 临时开发 Token 可访问受保护接口，撤销后立即返回 401

测试世界里没有玩家角色，所以这次实机索引的玩家、帕鲁和公会数量为 0；非空存档解析继续由固定样本和自动化测试覆盖。

## 安装

### Windows amd64

1. 从 [Releases](https://github.com/ninhua/palworld-panel/releases) 下载 Windows ZIP 和 `SHA256SUMS`。
2. 校验后解压到固定的可写目录，例如 `D:\PalPanel`。
3. 运行 `PalPanel.exe`，在浏览器中注册第一个管理员。
4. 在开服向导中安装服务端，或接管已有的 `PalServer.exe` 目录。

如果 SteamCMD、Workshop、GitHub/HTTP(S) MOD、PalDefender 或 UE4SS 下载失败，进入“系统设置 → 网络与代理”，为“公共下载与服务器更新”填写可用的 `http://`、`https://`、`socks5://` 或 `socks5h://` 地址并启用。Windows SteamCMD 执行时会临时使用当前用户代理，任务结束后自动恢复；其他下载使用进程内客户端代理。面板不会在 API、日志或任务消息中返回代理密码。

不要直接在 ZIP 里运行程序。当前 Windows 包没有 Authenticode 签名，SmartScreen 可能显示“未知发布者”。

Windows 首次启动后会在解压目录生成配置文件 `config\palpanel.env`，例如 `D:\PalPanel\config\palpanel.env`。修改配置前先退出正在运行的 `PalPanel.exe`，修改完成后重新启动；升级程序会保留该文件。

面板默认只监听 `127.0.0.1:8080`。需要从同一局域网的其他设备访问时，把配置文件中的监听地址改为：

```env
PALPANEL_LISTEN_ADDR=0.0.0.0:8080
```

然后在管理员 PowerShell 中仅为专用网络和本地子网放行端口：

```powershell
New-NetFirewallRule `
  -DisplayName "PalPanel TCP 8080 (LAN)" `
  -Direction Inbound `
  -Action Allow `
  -Protocol TCP `
  -LocalPort 8080 `
  -Profile Private `
  -RemoteAddress LocalSubnet
```

其他设备使用 `http://<Windows 局域网 IPv4>:8080` 访问；可通过 `ipconfig` 查询 IPv4 地址。`0.0.0.0` 只是监听地址，不能直接作为浏览器访问地址。请保持 `PALPANEL_REQUIRE_AUTH=true`，不要把未配置 HTTPS 的 8080 端口直接映射到公网。

### 诊断控制台

“运维与安全 → 诊断控制台”可以由已登录的管理员从 PalPanel 后端测试回环或私网 HTTP 接口。请求最多执行 15 秒，请求体和响应输出均限制为 64 KiB，公网目标会被拒绝。

### 诊断与支持包

`0.8.41` 在诊断页面新增固定白名单体检和脱敏 ZIP 支持包。支持包可包含版本、运行方式、服务器状态、前置条件、主机能力、近期任务、审计和事件摘要；可选附带最近日志尾部。环境变量、数据库、原始存档、密码、Token、完整路径、IP 和玩家标识不会写入 ZIP。默认最多保留 5 份，每份上限 50 MiB。详细边界见 [`docs/support-bundles.md`](docs/support-bundles.md)。

主机终端执行默认关闭。如确需临时调试，在 Windows 的 `config\palpanel.env` 或 Linux 的 `/etc/palpanel/palpanel.env` 中加入：

```env
PALPANEL_DIAGNOSTIC_SHELL_ENABLED=true
```

重启 PalPanel 后生效。命令使用 PalPanel 服务账号权限执行，单次最多 15 秒、输出最多 64 KiB，并记录到操作审计。调试结束后应删除该配置或改回 `false` 并重启；该接口不接受 API Key，也不能在关闭面板登录验证时使用。

### 通知与事件中心

“运维与安全 → 通知与事件”会归并监控告警、崩溃守卫和失败后台任务。相同根因只增加发生次数，不会重复刷屏；管理员可以确认、解决或重新打开事件。

可选 Webhook 通过环境变量启用：

```env
PALPANEL_INCIDENT_WEBHOOK_URL=https://ops.example.com/hooks/palpanel
PALPANEL_INCIDENT_WEBHOOK_SECRET=replace-with-at-least-16-random-characters
PALPANEL_INCIDENT_WEBHOOK_TIMEOUT_SECONDS=10
```

远程目标必须使用 HTTPS；HTTP 只允许 `127.0.0.1`、`localhost` 或其他回环地址。请求包含 `X-PalPanel-Delivery`、时间戳和 HMAC-SHA256 签名。接收方应按 Delivery ID 去重。面板 API 不返回完整 URL 或签名密钥。

### Linux amd64

安装最新正式版。默认使用 `https://ghfast.top/` 加速获取 GitHub 上的安装脚本：

```bash
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/ninhua/palworld-panel/custom-stable/install.sh | sudo bash
```

如果加速地址不可用，可改用 GitHub 官方直链：

```bash
curl -fsSL https://raw.githubusercontent.com/ninhua/palworld-panel/custom-stable/install.sh | sudo bash
```

默认只监听 `127.0.0.1:8080`。需要从局域网访问时，可以在安装时指定地址：

```bash
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/ninhua/palworld-panel/custom-stable/install.sh | sudo bash -s -- --listen 0.0.0.0:8080
```

常用命令：

```bash
sudo /opt/palpanel/current/palpanelctl status
sudo /opt/palpanel/current/palpanelctl logs -f
sudo /opt/palpanel/current/palpanelctl restart
sudo /opt/palpanel/current/palpanelctl uninstall
```

升级 Linux Docker/Wine 部署时，先校验发布包，再在解压目录执行：

```bash
sha256sum -c checksums.txt
sudo ./palpanelctl install --docker --listen 0.0.0.0:8080
sudo /opt/palpanel/current/palpanelctl status
curl --fail http://127.0.0.1:8080/api/health
```

升级默认保留 `/etc/palpanel`、`/var/lib/palpanel`、游戏存档、游戏日志和
Wine 前缀。Docker/Wine 模式不会再递归接管 `server`、`logs`、`wineprefix`
挂载目录；已有容器时只按容器配置的 UID/GID 调整其可写权限，面板自己的
`docker-client` 目录仍由 `palpanel` 用户保护。

如果升级后出现游戏容器权限错误，可检查：

```bash
docker inspect -f '{{.Config.User}}' palworld-wine-server
stat -c '%U:%G %a %n' /var/lib/palpanel/server /var/lib/palpanel/logs /var/lib/palpanel/wineprefix
sudo /opt/palpanel/current/palpanelctl status
sudo journalctl -u palpanel.service -u palpanel-sav-cli.service --no-pager -n 100
docker logs --tail 100 palworld-wine-server
```

请不要把没有 HTTPS 和访问控制的面板直接暴露到公网。

## AstrBot 配置

把 `astrbot_plugin_palpanel` 作为本地插件安装，在 AstrBot WebUI 中填写配置。字段定义见 [`_conf_schema.json`](astrbot_plugin_palpanel/_conf_schema.json)。

| AstrBot 配置 | PalPanel 配置 | 说明 |
| --- | --- | --- |
| `panel_url` | PalPanel 地址 | 同机通常使用 `http://127.0.0.1:8080` |
| `panel_public_url` | — | QQ 用户打开一次性链接时使用的地址 |
| `panel_id` | `PALPANEL_ASTRBOT_PANEL_ID` | 两边必须一致 |
| `shared_secret` | `PALPANEL_ASTRBOT_SHARED_SECRET` | 两边使用同一条随机密钥 |
| `listen_host:listen_port` | `PALPANEL_ASTRBOT_PLUGIN_URL` | 默认 `127.0.0.1:8092` |
| `allowed_group_id` | — | 允许使用命令的 QQ 群 |

双方请求使用 HMAC-SHA256，并校验时间戳和随机数。PalPanel 与 AstrBot 之间支持 HTTP 和 HTTPS；跨公网使用 HTTP 时传输内容不会被加密。

## 从源码运行

需要 Go `1.25.13`、Node.js 22、npm、.NET 9 SDK，以及 Rust/Cargo。Windows 构建 `sav-cli` 还需要 MinGW-w64。

```bash
git clone --recurse-submodules --branch custom-stable https://github.com/ninhua/palworld-panel.git
cd palworld-panel
```

主要目录：

```text
backend/                    Go API、任务和数据服务
frontend/                   React 管理界面与 QQ 受限页面
sav-cli/                    Palworld 存档解析侧车
palcalc-bridge/             PalCalc .NET 9 求解侧车
tools/palworld-uid-remap/   玩家 PlayerUID 存档迁移工具
third_party/palcalc/        固定版本的 PalCalc 子模块
astrbot_plugin_palpanel/    AstrBot QQ 插件
scripts/                    安装、打包和维护脚本
docs/                       OpenAPI、发布说明和截图
```

本地检查：

```bash
(cd backend && go test -p=1 ./...)
(cd sav-cli && CGO_ENABLED=1 go test -p=1 ./...)
(cd tools/palworld-uid-remap && cargo test --locked)
(cd palcalc-bridge && dotnet build -c Release)
(cd frontend && npm ci && npm run check && npm run test:e2e)
python -m unittest discover -s astrbot_plugin_palpanel/tests
```

接口定义在 [`docs/openapi.yaml`](docs/openapi.yaml)。CI 通过 GitHub Actions 验证 Linux 和 Windows 构建，正式版本以本仓库的 [Releases](https://github.com/ninhua/palworld-panel/releases) 为准。

## 镜像与离线构建

`0.8.40` 新增统一第三方镜像目录。先使用 `scripts/vendorctl.py init` 导入固定的 PalOps、MapLibre、PalCalc、uesave、授权地图瓦片及包管理器缓存，再设置：

```bash
export PALPANEL_DEPENDENCY_MODE=offline
export PALPANEL_VENDOR_ROOT=/srv/palpanel-vendor
scripts/package.sh --version v1.3.1-custom.0.8.67 --targets linux-amd64 --clean
```

`mirror` 模式使用自有镜像但允许缺失包从公共源补齐；`offline` 模式要求完整缓存并执行 SHA-256 门禁。详细目录和初始化方法见 [`docs/development/offline-vendor-build.md`](docs/development/offline-vendor-build.md)。

## 面板更新

Linux amd64 可以直接在面板任务队列或设置页执行“更新面板”。面板会：

1. 检查 [`ninhua/palworld-panel`](https://github.com/ninhua/palworld-panel/releases) 最新的非草稿、非预发布 Release。
2. 下载 `palpanel_<版本>_linux_amd64.tar.gz` 和 `SHA256SUMS`，并校验外层归档、候选版本、包内 `checksums.txt` 与 `panel-update.json`。
3. 默认 `PALPANEL_UPDATE_MODE=auto`：检测到 systemd root 更新器时走完整 Release 目录切换；否则在当前二进制目录可写时走 exec 热更新。
4. exec 模式只接受 Release 明确声明 `required_files=["bin/palpanel"]` 的版本，原子替换主二进制后通过 `syscall.Exec` 保持原 PID。
5. 新进程必须连续通过 `/api/ready` 和 `/api/patch/info` 目标版本检查；启动报错、监听失败或健康超时会恢复旧二进制并重新执行。
6. Release 需要同步更新 sav-cli、PalCalc、UID remapper、控制脚本或 systemd 单元时，exec 模式会拒绝，必须使用外部完整包更新器。

该流程不再依赖 `Palworld-Panel-Patches` 补丁链。Windows 版本目前通过下载新 Release ZIP 后运行升级程序更新。

完整后端接口和机器人对接方法见 [`docs/backend-api-guide.md`](docs/backend-api-guide.md)；面板更新的任务、Release 资产和错误码详见 [`docs/panel-update-api.md`](docs/panel-update-api.md)。

## 安全与限制

- 管理员会话和 QQ 配种会话使用不同的 Cookie 与权限检查
- QQ 用户不能读取其他玩家、原始存档路径或管理接口
- WebDAV 密码、开发 Token、HMAC 密钥和游戏管理员密码不会写入日志
- WebDAV、AstrBot 和其他可配置服务地址支持 HTTP 与 HTTPS；跨公网使用 HTTP 时凭据和数据不会被加密
- 当前不支持 Xbox WGS、多 PalPanel 租户和 QQ 用户自行上传个人存档

## 交流

<p align="center">
  <img src="docs/images/2.jpg" width="320" style="max-width: 100%; height: auto;" alt="PalPanel QQ 交流群二维码">
</p>

提交问题时请附上 PalPanel 版本、操作系统、运行方式和复现步骤，并先删掉日志中的密码、Token、API Key 与公网地址。

## 许可证

PalPanel 使用 [GPL-3.0-or-later](LICENSE)。PalCalc v1.17.6 保留 MIT 许可证；其余第三方组件与地图素材见 [`THIRD_PARTY_LICENSES.txt`](THIRD_PARTY_LICENSES.txt)。

## 发布包运行时说明

- Windows 与 Linux 发布包都包含自包含的 PalCalc/.NET 9 运行时，不需要另行安装系统 .NET。
- Linux 压缩包以 invariant globalization 发布并在便携启动器、systemd 服务中显式启用，因此不依赖系统 ICU；中文存档数据和 JSON 协议保持 UTF-8。
- Windows 的 CPU 与内存指标汇总 PalPanel 托管的完整 `PalServer.exe` 进程树，包括实际承载游戏负载的 `PalServer-Win64-Shipping-Cmd.exe`。
- 备份下载使用浏览器原生附件流，不会先把整个 ZIP 读入页面内存。
- Linux Docker/Wine 的 SteamCMD 下载代理会在任务期间创建仅监听宿主机 `127.0.0.1` 的临时 HTTP 桥接器，并通过 host network 供容器访问。Docker 基础镜像首次拉取仍由 Docker daemon 负责，需要单独配置 daemon proxy 或镜像加速。
