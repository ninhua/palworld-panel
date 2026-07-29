# PalPanel 后端 API 与机器人对接指南

本文档面向机器人、运维脚本和其他外部程序。接口基址：

```text
http://<PalPanel 地址>/api
```

完整的机器可读定义位于 [`openapi.yaml`](openapi.yaml)。登录后还可以调用：

```http
GET /api/catalog
```

该接口会返回当前运行版本实际注册的全部 API，包括方法、路径、分类、权限、用途和处理器。机器人应优先用 `/api/catalog` 做能力发现，避免面板升级后依赖过期的静态接口列表。

## 1. 鉴权

### API Key

普通机器人建议使用独立 API Key：

```http
Authorization: Bearer ppk_...
```

不要把管理员浏览器 Cookie 交给机器人。按最小权限创建 Key：

| 用途 | 建议权限 |
| --- | --- |
| 查询服务器、玩家、仓库、帕鲁 | 只读 |
| 修改玩家备注、踢出或封禁 | `players:write` |
| 启停、保存、广播、更新 | `server:control` |
| 修改配置 | `config:write` |
| 管理 PalDefender | `security:write` |

通用调用示例：

```bash
PANEL_URL="http://127.0.0.1:8080"
PANEL_TOKEN="ppk_..."

curl -fsS \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/server/status"
```

### AstrBot HMAC 接口

内置 AstrBot 集成接口不使用 API Key，而使用以下请求头：

```text
X-PalPanel-Id
X-PalPanel-Timestamp
X-PalPanel-Nonce
X-PalPanel-Signature
```

签名原文：

```text
大写 HTTP 方法 + "\n"
+ URL Path + "\n"
+ Unix 时间戳 + "\n"
+ 随机 Nonce + "\n"
+ 请求 Body 的 SHA256 十六进制
```

使用 `PALPANEL_ASTRBOT_PANEL_ID` 和 `PALPANEL_ASTRBOT_SHARED_SECRET` 配置。已有插件 [`astrbot_plugin_palpanel`](../astrbot_plugin_palpanel) 已实现签名、重放保护和命令调用，通常不需要机器人开发者重新实现。

## 2. 响应格式

成功：

```json
{
  "ok": true,
  "data": {}
}
```

失败：

```json
{
  "ok": false,
  "error": {
    "code": "error_code",
    "message": "错误说明"
  }
}
```

常见状态码：

| 状态码 | 含义 |
| --- | --- |
| `200` | 查询或同步操作成功 |
| `202` | 已创建异步任务 |
| `400` | 参数错误 |
| `401` | 未登录、API Key 或签名无效 |
| `403` | 权限不足或浏览器跨站写请求被拒绝 |
| `404` | 玩家、帕鲁、任务等对象不存在 |
| `409` | 当前状态冲突 |
| `429` | 请求过于频繁 |
| `500` | 面板内部错误 |
| `502/503` | GitHub、PalServer、PalDefender 等依赖不可用 |

## 3. 机器人常用只读接口

### 系统与版本

| 方法 | 路径 | 用途 | 鉴权 |
| --- | --- | --- | --- |
| `GET` | `/api/health` | 进程健康检查 | 公开 |
| `GET` | `/api/ready` | 数据库和服务就绪状态 | 公开 |
| `GET` | `/api/patch/info` | 上游版本、自定义版本、构建信息、功能列表 | 公开 |
| `GET` | `/api/catalog` | 当前运行时全部 API 目录 | 登录/API Key |
| `GET` | `/api/panel/update/status` | 当前版本和最新 Release | 登录/API Key |

### 服务器状态

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/server/status` | 面板管理的服务器状态 |
| `GET` | `/api/server/runtime` | 运行模式和路径 |
| `GET` | `/api/server/info` | PalServer 官方 REST 信息 |
| `GET` | `/api/server/players` | 当前在线玩家 |
| `GET` | `/api/server/settings` | 运行中的服务器设置 |
| `GET` | `/api/server/metrics` | Server FPS、在线数等指标 |
| `GET` | `/api/server/version` | 当前游戏服务端版本 |
| `GET` | `/api/server/logs` | 服务端日志 |
| `GET` | `/api/monitor/snapshot` | 当前 CPU、内存、磁盘快照 |
| `GET` | `/api/monitor/history` | 监控历史 |
| `GET` | `/api/alerts` | 当前告警 |

### 玩家

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/players` | 存档玩家列表、在线状态、备注和在线历史 |
| `GET` | `/api/players/{id}` | 玩家详情 |
| `GET` | `/api/players/{id}/inventory` | 玩家背包与物品槽位 |
| `GET` | `/api/players/bans` | 封禁列表 |
| `GET` | `/api/players/whitelist` | 白名单 |

`{id}` 可以使用接口返回的玩家 ID。玩家接口支持 `source_id` 选择存档源；未指定时使用当前激活源。

示例：

```bash
curl -fsS \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/players?source_id=server"
```

### 公会与基地

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/guilds` | 公会列表 |
| `GET` | `/api/guilds/{id}` | 公会详情、会长、成员和关联基地 |
| `GET` | `/api/bases` | 基地列表 |
| `GET` | `/api/bases/{id}` | 基地详情 |
| `GET` | `/api/bases/{id}/storage` | 基地仓库详情、容器、槽位和物品 |
| `GET` | `/api/bases/{id}/workers` | 基地工作帕鲁 |
| `GET` | `/api/bases/{id}/feed-boxes` | 饲料箱内容摘要 |

查询基地仓库：

```bash
BASE_ID="..."

curl -fsS \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/bases/${BASE_ID}/storage"
```

### 全服仓库/库存

```http
GET /api/inventory
```

该接口聚合玩家背包、基地仓库和未识别容器，适合机器人实现“查询全服某物品”“物品在哪个基地”等命令。

查询参数：

| 参数 | 说明 |
| --- | --- |
| `q` | 物品 ID 或本地化名称关键词 |
| `category` | 物品分类 |
| `owner_type` | `all`、`base`、`player`、`unknown` |
| `sort` | `count_desc`、`name_asc`、`name_desc` |
| `limit` | 1–500，默认 200 |
| `offset` | 分页偏移 |

示例：

```bash
curl -fsS \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/inventory?q=帕鲁矿碎块&owner_type=base&sort=count_desc&limit=50"
```

响应 `data` 主要包含：

```text
items       聚合后的物品及 locations
summary     总数、位置数、分页信息
filters     可用分类和 owner_type
status      存档索引状态
source_id   实际使用的存档源
unattended  无人时段库存净变化状态
```

### 帕鲁

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/pals` | 查询全部帕鲁并筛选、排序、分页 |
| `GET` | `/api/pals/{id}` | 查询单只帕鲁详情 |

`GET /api/pals` 支持：

| 参数 | 说明 |
| --- | --- |
| `q` | 实例 ID、帕鲁 ID、中文名、昵称或主人关键词 |
| `status` | 状态 |
| `owner_player_uid` | 主人 PlayerUID |
| `guild_id` | 公会 ID |
| `container_id` | 容器 ID |
| `gender` | 性别 |
| `location` | `storage`、`party`、`base`、`expedition`、`unknown` |
| `min_level` | 最低等级，0–65 |
| `min_stars` | 最低星级，0–4 |
| `min_iv_average` | 最低平均 IV，0–100 |
| `passive` | 必须包含的被动；多个值用逗号分隔 |
| `sort` | 默认等级降序；也支持 `iv_desc`、`stars_desc`、`name_asc` |
| `limit`、`offset` | 分页 |

示例：

```bash
curl -fsS \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  "${PANEL_URL}/api/pals?min_level=50&min_iv_average=90&passive=传说,脑筋&sort=iv_desc"
```

### 地图、存档与备份

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/map/entities` | 玩家、基地、帕鲁和地图对象坐标 |
| `GET` | `/api/save/index/status` | 存档索引状态 |
| `GET` | `/api/save-sources` | 存档源列表 |
| `GET` | `/api/backups` | 备份列表 |
| `GET` | `/api/backups/{name}/verify` | 验证备份 |
| `GET` | `/api/backups/{name}/download` | 下载备份 |

### 社区服务器与配种

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/community-servers` | 查询可发现社区服务器 |
| `GET` | `/api/community-servers/source-status` | 社区服数据源状态 |
| `GET` | `/api/breeding/catalog` | 管理端配种目录 |
| `GET` | `/api/breeding/status` | 配种侧车状态 |
| `GET` | `/api/breeding/history` | 配种任务历史 |

## 4. 机器人常用写接口

写接口必须使用相应权限，并应在机器人侧增加管理员白名单、二次确认和审计日志。

### 服务器控制

| 方法 | 路径 | 权限 | 用途 |
| --- | --- | --- | --- |
| `POST` | `/api/server/start` | `server:control` | 启动服务器 |
| `POST` | `/api/server/stop` | `server:control` | 停止服务器 |
| `POST` | `/api/server/restart` | `server:control` | 重启服务器 |
| `POST` | `/api/server/safe-stop` | `server:control` | 保存、广播并安全关服 |
| `POST` | `/api/server/safe-restart` | `server:control` | 保存、广播并安全重启 |
| `POST` | `/api/server/force-stop` | `server:control` | 强制停止，仅用于故障处理 |
| `POST` | `/api/server/save` | `server:control` | 保存世界 |
| `POST` | `/api/server/announce` | `server:control` | 游戏内广播 |
| `POST` | `/api/server/update-if-needed` | `server:control` | 检查后更新游戏服务端 |
| `POST` | `/api/server/backup` | `server:control` | 创建备份 |

多数控制接口返回 `202` 异步任务。取得 `data.id` 后轮询 `/api/jobs/{id}`。

### 玩家管理

| 方法 | 路径 | 权限 | 用途 |
| --- | --- | --- | --- |
| `PUT` | `/api/players/{id}/annotation` | `players:write` | 保存玩家备注和标签 |
| `DELETE` | `/api/players/{id}/annotation` | `players:write` | 删除备注和标签 |
| `POST` | `/api/players/{id}/kick` | `players:write` | 踢出玩家 |
| `POST` | `/api/players/{id}/ban` | `players:write` | 封禁玩家 |
| `POST` | `/api/players/{id}/unban` | `players:write` | 解封玩家 |
| `POST` | `/api/players/bans` | `players:write` | 添加封禁 |
| `DELETE` | `/api/players/bans/{steam_id}` | `players:write` | 删除封禁 |
| `PUT` | `/api/players/whitelist` | `players:write` | 替换白名单 |

备注示例：

```bash
curl -fsS -X PUT \
  -H "Authorization: Bearer ${PANEL_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"note":"机器人登记","tags":["活跃玩家"]}' \
  "${PANEL_URL}/api/players/${PLAYER_ID}/annotation"
```

### PalDefender GM

常用接口：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/security/paldefender/gm/status` | GM 能力状态 |
| `GET` | `/api/security/paldefender/gm/players` | GM 在线玩家 |
| `GET` | `/api/security/paldefender/gm/items` | 物品目录 |
| `GET` | `/api/security/paldefender/gm/players/{id}/inventory` | 实时玩家背包 |
| `POST` | `/api/security/paldefender/gm/players/{id}/items` | 发放物品 |
| `POST` | `/api/security/paldefender/gm/players/{id}/items/remove` | 移除物品 |
| `GET` | `/api/security/paldefender/gm/players/{id}/pals` | 玩家实时帕鲁 |
| `POST` | `/api/security/paldefender/gm/players/{id}/custom-pal` | 发放自定义帕鲁 |
| `POST` | `/api/security/paldefender/gm/players/{id}/message` | 私聊玩家 |
| `POST` | `/api/security/paldefender/gm/broadcast` | 全服广播 |
| `POST` | `/api/security/paldefender/gm/players/{id}/kick` | 踢出 |
| `POST` | `/api/security/paldefender/gm/players/{id}/ban` | 封禁 |

精确 JSON 请求体应从 `/api/catalog`、[`openapi.yaml`](openapi.yaml) 或对应前端调用获取。发放、删除和封禁类命令建议携带 `Idempotency-Key`，避免机器人超时重试导致重复执行。

## 5. 内置 AstrBot 专用接口

这些接口要求 HMAC 签名：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `POST` | `/api/integrations/astrbot/binding-challenges` | 创建游戏绑定验证码 |
| `POST` | `/api/integrations/astrbot/quick-solves` | 提交快捷配种 |
| `POST` | `/api/integrations/astrbot/server-status` | 查询服务器状态 |
| `POST` | `/api/integrations/astrbot/server-control` | 开服、关服、重启等控制 |
| `POST` | `/api/integrations/astrbot/community-servers` | 查询社区服务器 |

AstrBot 插件目前已经提供：

```text
/bd、/bdqr、/qd、/jf、/pz
/服状态、/在线、/房间
/开服、/关服、/重启、/强关
```

如果是其他机器人框架，通常更简单的方案是创建最小权限 API Key，直接调用第 3、4 节的通用接口；只有需要复用 QQ 绑定、积分和签名信任链时才对接 AstrBot 专用接口。

## 6. 异步任务

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/jobs?limit=50` | 最近任务 |
| `GET` | `/api/jobs/{id}` | 单个任务状态 |

任务字段：

```json
{
  "id": "job_...",
  "type": "panel_update",
  "status": "running",
  "progress": 55,
  "message": "...",
  "error": "...",
  "error_code": "...",
  "created_at": "...",
  "updated_at": "..."
}
```

状态包括 `waiting`、`running`、`completed`、`failed`。机器人不要因为 HTTP `202` 就回复“操作成功”，应轮询到终态后再反馈用户。

## 7. 面板更新

| 方法 | 路径 | 权限 | 用途 |
| --- | --- | --- | --- |
| `GET` | `/api/panel/update/status` | 只读 | 查询本仓库最新 Release |
| `POST` | `/api/panel/update/check` | `server:control` | 创建检查任务 |
| `POST` | `/api/panel/update` | `server:control` | Linux amd64 完整 Release 更新 |

详细更新过程、Release 资产约定、任务错误码和回滚机制见 [`panel-update-api.md`](panel-update-api.md)。

## 8. 对接建议

- 为每个机器人创建独立 API Key，便于单独撤销和审计。
- 查询类命令做 2–10 秒短缓存，避免群聊并发反复解析大结果。
- 使用 `limit`、`offset` 分页，不要默认把全部仓库或帕鲁发送到聊天窗口。
- 展示玩家信息时避免泄露 IP、原始存档路径、Token 和管理备注。
- 写操作设置管理员白名单，并对封禁、强停、存档覆盖等动作二次确认。
- 对异步任务轮询设置超时，但不要在超时后自动重复提交写操作。
- 先检查 `data.status` 中的存档索引状态；缓存过期时可以展示旧数据并提示用户。
- 生产环境使用 HTTPS 或可信内网，不要通过公网明文 HTTP 传输 API Key。
## 9. 诊断控制台接口

诊断接口面向面板中的交互式管理员调试，不面向机器人或无人值守任务。它们只接受已登录管理员的同源浏览器会话；Bearer API Key、普通操作员、查看者和关闭登录验证的部署都会被拒绝。

### 查询诊断能力

```http
GET /api/system/diagnostics
```

响应会说明 HTTP 测试和终端命令是否启用、执行超时、输出上限和后端平台。

### 测试内网 HTTP 接口

```http
POST /api/system/diagnostics/http
Content-Type: application/json

{
  "method": "GET",
  "url": "http://127.0.0.1:17993/",
  "headers": {
    "Authorization": "Bearer <临时调试 Token>"
  },
  "body": ""
}
```

仅允许 `http`、`https` 和回环/私网目标，禁止公网目标、URL 用户信息及代理相关请求头。最多跟随 3 次仍满足私网限制的重定向；单次最长 15 秒，请求体和响应体最多 64 KiB。

### 执行主机终端命令

先在 PalPanel 服务环境中设置 `PALPANEL_DIAGNOSTIC_SHELL_ENABLED=true` 并重启，再请求：

```http
POST /api/system/diagnostics/shell
Content-Type: application/json

{
  "command": "ss -lntp",
  "confirm": true
}
```

Windows 使用 `cmd.exe /d /s /c`，Linux 使用 `/bin/sh -lc`。命令以 PalPanel 服务账号权限执行，最长 15 秒、合并输出最多 64 KiB。响应中的 `exit_code`、`success`、`timed_out` 和 `truncated` 用于判断结果；非零退出码仍会返回 HTTP 200，但操作审计会标记为失败。

调试结束后应关闭环境变量并重启服务。请求和响应可能包含敏感信息，复制日志或审计记录前需要删除 Token、密码和内网地址。
