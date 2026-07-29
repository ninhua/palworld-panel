# 功能移植已知问题

## 双模式面板更新

- `0.8.30` 本身没有 exec 通道；无 systemd 环境首次升级到 `0.8.31` 需要由外层启动脚本安装完整 Release。运行 `0.8.31` 后，后续兼容版本才可从面板内使用 exec 热更新。
- exec 模式只能安全更新主程序。Release 的 `panel-update.json` 声明需要同步更新侧车、控制脚本或安装结构时，面板会拒绝 exec 更新。
- exec 更新会让面板 HTTP 连接短暂断开，但 PID 保持不变，PalServer 与侧车不停止。
- 进程被 `SIGKILL` 或宿主机在健康验证期间断电时，事务会在下次启动继续检查并按标记回滚；外层平台不再重新启动面板时需要人工重启容器。
- 运行目录必须允许当前面板用户写入 `bin/palpanel` 和同目录备份文件。只读目录只能使用 external 模式。
- 从 `0.8.28` 或更早版本首次使用 external 模式时，仍需运行新版安装脚本或 `palpanelctl install` 安装 root 更新器和 systemd 路径单元。
- external 模式目前只用于 Linux amd64 systemd 正式安装；Windows 继续使用 Windows 升级程序。

## 诊断控制台

- 主机终端执行默认关闭，设置 `PALPANEL_DIAGNOSTIC_SHELL_ENABLED=true` 并重启 PalPanel 后才会开放。
- 诊断接口只接受管理员浏览器会话；API Key 和关闭登录验证的部署不能调用。
- 命令以 PalPanel 服务账号权限执行，不提供容器或文件系统沙箱；调试结束后必须重新关闭。

## 验证环境

- 本地执行环境只有 Go `1.23.2`，仓库要求 Go `1.25.12`，因此完整 Go 测试由 GitHub Actions 验证。
