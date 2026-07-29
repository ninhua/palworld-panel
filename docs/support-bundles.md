# 诊断与支持包

PalPanel `0.8.41` 在“运维与安全 → 诊断控制台”提供固定白名单体检和脱敏 ZIP 支持包。该功能只用于排障，不是备份。

## 收集内容

固定条目包括：

- 构建版本、提交和自定义版本；
- 操作系统、架构、运行方式和数据库 schema 版本；
- PalServer 状态、前置条件和主机能力；
- 最近 50 个任务、100 条审计记录和 20 个事件摘要；
- 不含密钥的安全配置状态；
- 可选的最近日志尾部。

支持包不收集环境变量、数据库文件、原始 `.sav`、备份内容或用户指定路径。

## 脱敏

写入 ZIP 前执行两层脱敏：

1. JSON 键名匹配密码、密钥、Token、Cookie、路径、UID、GUID、SteamID、IP 或地址时替换整个值。
2. 文本再次替换 Bearer Token、敏感赋值、Windows/Linux 绝对路径、IPv4、GUID、长十六进制和 15–20 位数字标识。

日志只读取 `LogsDir` 顶层最近 3 个 `.log` / `.txt` 普通文件，每个最多 128 KiB，总计最多 384 KiB；不读取子目录、符号链接或其他扩展名，ZIP 中使用 `recent-1` 等固定条目名，不暴露原日志文件名。

## 存储和保留

文件位于：

```text
<DataDir>/support-bundles/<32位ID>.zip
<DataDir>/support-bundles/<32位ID>.json
```

目录权限为 `0700`，文件权限为 `0600`。默认最多保留 5 份、最长 14 天，单个 ZIP 上限 50 MiB。

## API

```text
GET    /api/system/diagnostics/support-bundles/status
GET    /api/system/diagnostics/support-bundles
POST   /api/system/diagnostics/support-bundles
GET    /api/system/diagnostics/support-bundles/{id}/download
DELETE /api/system/diagnostics/support-bundles/{id}
```

生成请求：

```json
{
  "include_logs": true,
  "confirm": true
}
```

全部接口要求交互式管理员会话。
