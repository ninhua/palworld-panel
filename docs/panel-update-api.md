# PalPanel 面板更新接口与使用方法

本文档说明 PalPanel 如何从 [`ninhua/palworld-panel`](https://github.com/ninhua/palworld-panel) Release 检查并安装完整面板更新。

## 适用范围

- 检查更新：所有受支持平台均可调用。
- 执行自更新：目前仅支持 Linux amd64。
- Windows：从 [Releases](https://github.com/ninhua/palworld-panel/releases) 下载新的 Windows ZIP，校验 `SHA256SUMS` 后使用包内升级程序更新。
- 更新类型：完整 Release 更新，不使用补丁文件、补丁 manifest 或旧补丁仓库。

正式版本格式：

```text
v<上游版本>-custom.<自定义版本>
```

示例：

```text
v1.3.0-custom.0.8.29
```

## 鉴权与权限

所有接口都位于 `/api` 下，需要登录会话或 API Key。

API Key 使用：

```http
Authorization: Bearer ppk_...
```

权限要求：

| 接口 | 方法 | 权限 |
| --- | --- | --- |
| `/api/panel/update/status` | `GET` | 已登录 |
| `/api/panel/update/check` | `POST` | `server:control` |
| `/api/panel/update` | `POST` | `server:control` |
| `/api/jobs/:id` | `GET` | 已登录 |
| `/api/jobs?limit=50` | `GET` | 已登录 |

浏览器登录会话调用 `POST` 接口时还必须满足同源检查。外部自动化建议使用具有 `server:control` 权限的 API Key。

以下示例使用：

```bash
PANEL_URL="http://127.0.0.1:8080"
PANEL_TOKEN="ppk_..."
```

## 查询更新状态

```http
GET /api/panel/update/status
```

示例：

```bash
curl -fsS \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/panel/update/status"
```

成功响应：

```json
{
  "ok": true,
  "data": {
    "current_version": "v1.3.0-custom.0.8.19",
    "latest_version": "v1.3.0-custom.0.8.29",
    "release_tag": "v1.3.0-custom.0.8.29",
    "release_url": "https://github.com/ninhua/palworld-panel/releases/tag/v1.3.0-custom.0.8.29",
    "update_available": true,
    "checked_at": "2026-07-29T04:00:00Z",
    "message": "发现面板新版本 v1.3.0-custom.0.8.29"
  }
}
```

该接口优先查询 GitHub Releases API；如果匿名 API 配额耗尽，会自动通过 GitHub `releases/latest`
网页重定向解析最新版，不要求服务器配置 GitHub Token。查询结果只选择：

- 非草稿 Release；
- 非预发布 Release；
- 符合 `vX.Y.Z-custom.X.Y.Z` 格式；
- 同时包含 Linux amd64 包和 `SHA256SUMS`。

GitHub 查询失败时返回 `502`：

```json
{
  "ok": false,
  "error": {
    "code": "panel_update_status_failed",
    "message": "..."
  }
}
```

## 创建“检查更新”任务

```http
POST /api/panel/update/check
```

此接口只检查 Release，不下载或替换面板程序。

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/panel/update/check"
```

成功时返回 `202 Accepted` 和任务：

```json
{
  "ok": true,
  "data": {
    "id": "job_...",
    "type": "panel_update_check",
    "status": "waiting",
    "progress": 0,
    "message": "queued panel update check",
    "created_at": "...",
    "updated_at": "..."
  }
}
```

## 创建面板更新任务

```http
POST /api/panel/update
```

Linux amd64 示例：

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/panel/update"
```

成功时返回 `202 Accepted`：

```json
{
  "ok": true,
  "data": {
    "id": "job_...",
    "type": "panel_update",
    "status": "waiting",
    "progress": 0,
    "message": "queued panel update",
    "created_at": "...",
    "updated_at": "..."
  }
}
```

任务随后执行：

1. 查询最新正式 Release。
2. 如果当前版本已是最新，直接完成任务。
3. 下载 `SHA256SUMS` 和 Linux 完整包并进行第一轮校验。
4. 提取并运行候选 `palpanel --version`。
5. 将归档交给 `palpanel-update.path` 触发的 root 级外部更新器。
6. 外部更新器从官方 Release 重新获取 `SHA256SUMS`，再验证外层归档和包内 `checksums.txt`。
7. 安装完整版本目录，更新 systemd 单元与更新器，并原子切换 `/opt/palpanel/current`。
8. 重启 `sav-cli`、`palcalc-bridge` 和 PalPanel。
9. 连续验证 `/api/ready` 与 `/api/health` 返回目标版本。
10. 验证失败时恢复旧版本目录、旧 systemd 单元和旧更新器，再验证回滚版本。

更新请求被 root 更新器接管后会保存为 `request.in-progress.json`。如果更新器退出或主机重启，`palpanel-update.path` 会再次触发该请求；目标版本目录已经切换时，更新器会继续安装运行单元并执行健康检查，而不是盲目覆盖或直接判定失败。

非 Linux amd64 平台调用该接口会返回 `500`，错误信息为：

```text
panel self-update currently requires linux-amd64
```

## 查询任务结果

使用创建任务响应中的 `id`：

```bash
JOB_ID="job_..."

curl -fsS \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/jobs/${JOB_ID}"
```

任务状态：

| 状态 | 含义 |
| --- | --- |
| `waiting` | 等待任务执行 |
| `running` | 正在检查、下载、校验或替换 |
| `completed` | 已完成；更新任务通常会紧接着重启面板 |
| `failed` | 失败，查看 `error_code` 和 `error` |

轮询示例：

```bash
while true; do
  curl -fsS \
    -H "Authorization: Bearer ${PANEL_TOKEN}" \
    "${PANEL_URL}/api/jobs/${JOB_ID}"
  sleep 2
done
```

更新过程中连接短暂断开属于正常现象。面板重启后，可检查：

```bash
curl -fsS "${PANEL_URL}/api/health"

curl -fsS \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/panel/update/status"
```

## Release 资产约定

发布工作流必须生成：

```text
palpanel_<Release Tag>_linux_amd64.tar.gz
palpanel_<Release Tag>_windows_amd64.zip
SHA256SUMS
```

Linux 归档必须包含 `palpanel`、`palpanel-updater`、`sav-cli`、`palcalc-bridge`、`palpanelctl`、五个 systemd 单元和包内 `checksums.txt`。外部更新器拒绝符号链接、硬链接、路径穿越、未被包内校验覆盖的文件以及与官方 `SHA256SUMS` 不一致的归档。

当前正式发布示例：

```text
palpanel_v1.3.0-custom.0.8.29_linux_amd64.tar.gz
palpanel_v1.3.0-custom.0.8.29_windows_amd64.zip
```

## 首次启用外部更新器

`0.8.29` 是新的完整包更新框架首次落地版本。旧版安装中尚不存在 root 更新器和 `palpanel-update.path`，因此首次升级到本版本必须通过安装脚本或解压新版 Release 后执行：

```bash
sudo ./palpanelctl install
```

完成一次安装后，后续 Linux amd64 版本可继续从面板内执行完整包更新。

## GitHub 访问配置

公开仓库通常不需要 Token。遇到 GitHub API 限流时，可在 PalPanel 进程环境中设置：

```env
PALPANEL_GITHUB_TOKEN=github_token
```

也兼容：

```env
GITHUB_TOKEN=github_token
GH_TOKEN=github_token
```

Token 只用于 GitHub Release API 和资产请求，不应写入日志、命令历史或公开配置。

## 常见错误

| `error_code` | 含义 |
| --- | --- |
| `panel_update_check_failed` | 检查任务无法查询或解析 Release |
| `panel_external_updater_unavailable` | 外部更新器、systemd 单元或版本化安装结构缺失 |
| `panel_update_handoff_failed` | 无法写入受限更新请求或已有更新正在进行 |
| `panel_update_official_checksum_failed` | root 更新器无法取得官方 `SHA256SUMS` |
| `panel_update_archive_untrusted` | 归档哈希与官方 Release 不一致 |
| `panel_update_health_failed` | 新版本未通过就绪和版本健康检查，已尝试回滚 |
| `panel_release_lookup_failed` | 没有找到符合约定的正式 Release |
| `panel_checksums_download_failed` | `SHA256SUMS` 下载失败 |
| `panel_checksums_invalid` | 校验文件格式或路径不安全 |
| `panel_archive_download_failed` | Linux 完整包下载失败 |
| `panel_archive_checksum_failed` | 完整包 SHA256 不匹配 |
| `panel_archive_invalid` | 归档结构或二进制条目不符合约定 |
| `panel_binary_probe_failed` | 候选程序无法运行或版本不匹配 |
| `panel_backup_failed` | 无法备份当前程序 |
| `panel_replacement_failed` | 无法原子替换当前程序 |
| `panel_activation_checksum_failed` | 替换后的程序校验失败 |
| `panel_restart_failed` | 替换成功但进程重启失败，旧程序会被恢复 |
| `panel_startup_verification_failed` | 新进程启动校验失败并触发回滚 |

排查时可查看任务详情和服务日志：

```bash
sudo /opt/palpanel/current/palpanelctl status
sudo /opt/palpanel/current/palpanelctl logs -f
sudo journalctl -u palpanel.service --no-pager -n 200
```
