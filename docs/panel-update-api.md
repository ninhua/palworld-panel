# PalPanel 面板更新接口与使用方法

本文档说明 PalPanel 如何从 `ninhua/palworld-panel` Release 检查并安装面板更新。

## 更新模式

Linux amd64 支持两种更新模式：

| 模式 | 适用环境 | 行为 |
|---|---|---|
| `external` | systemd 正式安装 | root 更新器切换完整 Release 目录，并重启 PalPanel 与侧车 |
| `exec` | 启动脚本、容器或便携安装 | 只原子替换 `bin/palpanel`，再通过 `syscall.Exec` 保持原 PID 启动新版本 |

默认配置：

```env
PALPANEL_UPDATE_MODE=auto
```

可选值：

```text
auto      优先 external；不可用时自动使用 exec
external  强制完整包更新，缺少 systemd 更新器时拒绝执行
exec      强制 PID 保持不变的主程序热更新
```

`exec` 不要求修改外层启动脚本。更新时 PalServer、`sav-cli` 和 `palcalc-bridge` 不停止，面板 HTTP 连接会短暂断开。

为了防止组件版本混杂，exec 模式只接受 Release 内 `panel-update.json` 明确声明只需要更新 `bin/palpanel` 的版本。需要同步更新侧车、控制脚本或安装结构的 Release 必须使用 external 模式或外部部署脚本更新。

正式版本格式：

```text
v<上游版本>-custom.<自定义版本>
```

示例：

```text
v1.3.0-custom.0.8.36
```

## 鉴权与权限

所有更新接口位于 `/api` 下，需要登录会话或 API Key。

```http
Authorization: Bearer ppk_...
```

| 接口 | 方法 | 权限 |
|---|---|---|
| `/api/panel/update/status` | `GET` | 已登录 |
| `/api/panel/update/check` | `POST` | `server:control` |
| `/api/panel/update` | `POST` | `server:control` |
| `/api/jobs/:id` | `GET` | 已登录 |

以下示例使用：

```bash
PANEL_URL="http://127.0.0.1:8080"
PANEL_TOKEN="ppk_..."
```

## 查询更新状态

```bash
curl -fsS \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/panel/update/status"
```

成功响应示例：

```json
{
  "ok": true,
  "data": {
    "current_version": "v1.3.0-custom.0.8.30",
    "latest_version": "v1.3.0-custom.0.8.36",
    "release_tag": "v1.3.0-custom.0.8.36",
    "release_url": "https://github.com/ninhua/palworld-panel/releases/tag/v1.3.0-custom.0.8.36",
    "update_available": true,
    "update_mode": "exec",
    "update_mode_note": "主进程通过 syscall.Exec 原地热更新，PID 保持不变并执行启动健康回滚",
    "checked_at": "2026-07-29T09:00:00Z",
    "message": "发现面板新版本 v1.3.0-custom.0.8.36"
  }
}
```

Release 查询只接受：

- 非草稿、非预发布 Release；
- 符合 `vX.Y.Z-custom.X.Y.Z` 的版本；
- 同时包含 Linux amd64 包和 `SHA256SUMS`。

GitHub Releases API 受限时会回退到 `/releases/latest` 重定向解析。

## 创建检查任务

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/panel/update/check"
```

该任务只检查 Release，不下载或替换程序。

## 创建更新任务

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/panel/update"
```

成功时返回 `202 Accepted` 和 `panel_update` 任务。

### 公共准备阶段

两种模式都会执行：

1. 查询最新正式 Release。
2. 下载并解析 `SHA256SUMS`。
3. 下载 Linux 完整包并校验官方 SHA-256。
4. 提取候选 `bin/palpanel`。
5. 校验包内 `checksums.txt`。
6. 执行候选程序 `--version`，确认与 Release 标签一致。

### exec 热更新阶段

1. 校验 `panel-update.json`，确认该 Release 只需要替换 `bin/palpanel`。
2. 备份当前主程序到 `bin/.palpanel-update-backups/`。
3. 写入 `bin/.palpanel-update-state.json`。
4. 在同目录原子替换主程序。
5. 使用 `syscall.Exec` 让新程序接管当前 PID、参数和环境变量。
6. 新程序启动后连续三次验证 `/api/ready` 和 `/api/patch/info` 的目标版本。
7. 验证成功后删除事务标记，并保留最近四份备份。
8. 启动报错、监听失败、就绪超时或版本不匹配时，恢复旧主程序并再次 `exec` 旧版本。

外层 `start.sh` 等待的 PID 不变，因此不会因为面板热更新而结束容器。该模式不重启游戏服务和侧车。

### external 完整包阶段

1. Web 进程写入受限更新请求。
2. `palpanel-update.path` 启动 root 级 `palpanel-updater`。
3. 更新器独立重新获取官方 `SHA256SUMS`。
4. 验证完整包及包内所有文件。
5. 安装新版本目录、更新 systemd 单元和更新器。
6. 原子切换 `/opt/palpanel/current`。
7. 重启 PalPanel、`sav-cli` 和 `palcalc-bridge`。
8. 验证 `/api/ready` 与目标版本；失败时恢复旧目录、旧单元和旧更新器。

执行中的外部请求保存在 `request.in-progress.json`，主机重启后可继续或回滚。

## 查询任务结果

```bash
JOB_ID="job_..."

curl -fsS \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/jobs/${JOB_ID}"
```

| 状态 | 含义 |
|---|---|
| `waiting` | 等待执行 |
| `running` | 正在下载、校验、切换或验证启动状态 |
| `completed` | 新版本已通过健康和版本验证 |
| `failed` | 更新失败或已回滚，查看 `error_code` 与 `error` |

exec 切换期间连接短暂断开属于正常现象。恢复后可查询：

```bash
curl -fsS "${PANEL_URL}/api/ready"
curl -fsS "${PANEL_URL}/api/patch/info"
```

## Release 资产约定

发布工作流必须生成：

```text
palpanel_<Release Tag>_linux_amd64.tar.gz
palpanel_<Release Tag>_windows_amd64.zip
SHA256SUMS
```

Linux 包必须包含：

```text
bin/palpanel
bin/palpanel-updater
bin/sav-cli
bin/palcalc-bridge
bin/palworld-uid-remap
palpanelctl
panel-update.json
checksums.txt
systemd/palpanel.service
systemd/palpanel-sav-cli.service
systemd/palpanel-palcalc.service
systemd/palpanel-update.service
systemd/palpanel-update.path
```

`panel-update.json` 示例：

```json
{
  "schema_version": 1,
  "version": "v1.3.0-custom.0.8.36",
  "exec_hot_update": {
    "supported": true,
    "required_files": ["bin/palpanel"],
    "health_paths": ["/api/ready", "/api/patch/info"],
    "success_threshold": 3
  }
}
```

当某个版本必须同步更新侧车或安装文件时，发布流程必须把 `supported` 改为 `false`，或在 `required_files` 中列出所需文件。exec 模式会拒绝此类 Release。

## 首次启用 external 模式

从 `0.8.28` 或更早版本首次升级到包含外部更新器的版本时，需要通过安装脚本或新版 Release 执行：

```bash
sudo ./palpanelctl install
```

这只影响 external/systemd 模式。启动脚本环境不需要安装 root 更新器；由于 `0.8.30` 本身尚未包含 exec 通道，无 systemd 环境首次升级到 `0.8.31` 仍需由外层 `start.sh` 下载完整 Release。从 `0.8.31` 开始，后续明确声明仅更新主程序的兼容版本可在面板内热更新。

## GitHub 访问配置

```env
PALPANEL_GITHUB_TOKEN=github_token
```

也兼容：

```env
GITHUB_TOKEN=github_token
GH_TOKEN=github_token
```

更新下载同时使用“系统设置 → 网络与代理 → 安装与下载代理”。

## 常见错误

| `error_code` | 含义 |
|---|---|
| `panel_update_check_failed` | 无法查询或解析 Release |
| `panel_update_mode_unavailable` | external 与 exec 均不可用，或强制模式不满足环境要求 |
| `panel_exec_package_incompatible` | Release 未声明可安全地只热更主程序 |
| `panel_exec_update_failed` | 主程序备份、替换或 `exec` 失败 |
| `panel_exec_activation_interrupted` | 事务标记已写入，但旧程序仍是活动文件 |
| `panel_exec_startup_verification_failed` | 新程序未通过就绪或目标版本验证 |
| `panel_exec_update_rolled_back` | exec 热更新失败，旧程序已恢复并重新启动 |
| `panel_external_updater_unavailable` | 外部更新器、systemd 单元或版本化安装结构缺失 |
| `panel_update_handoff_failed` | 无法写入外部更新请求或已有更新正在执行 |
| `panel_update_official_checksum_failed` | root 更新器无法取得官方 `SHA256SUMS` |
| `panel_update_archive_untrusted` | 完整包与官方 SHA-256 不一致 |
| `panel_update_health_failed` | external 新版本健康检查失败并已尝试回滚 |
| `panel_release_lookup_failed` | 未找到符合约定的正式 Release |
| `panel_checksums_download_failed` | `SHA256SUMS` 下载失败 |
| `panel_checksums_invalid` | 校验文件格式或路径不安全 |
| `panel_archive_download_failed` | Linux 包下载失败 |
| `panel_archive_checksum_failed` | Linux 包 SHA-256 不匹配 |
| `panel_archive_invalid` | 归档结构或主程序条目不符合约定 |
| `panel_binary_probe_failed` | 候选程序无法运行或版本不匹配 |

启动脚本环境排查：

```bash
ls -la /home/container/palworld_win/app/bin/.palpanel-update-*
tail -n 200 /home/container/palworld_win/logs/palpanel-console.log
```

systemd 环境排查：

```bash
sudo /opt/palpanel/current/palpanelctl status
sudo journalctl -u palpanel.service -u palpanel-update.service --no-pager -n 200
```
