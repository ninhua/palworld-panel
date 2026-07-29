# 功能移植更新记录

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
