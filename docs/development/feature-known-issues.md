# 功能移植已知问题

## 诊断控制台

- 主机终端执行默认关闭，设置 `PALPANEL_DIAGNOSTIC_SHELL_ENABLED=true` 并重启 PalPanel 后才会开放。
- 诊断接口只接受管理员浏览器会话；API Key 和关闭登录验证的部署不能调用。
- 命令以 PalPanel 服务账号权限执行，不提供容器或文件系统沙箱；调试结束后必须重新关闭。

## 外部更新器

- 从 `0.8.28` 或更早版本首次升级到 `0.8.29` 时，需要运行新版安装脚本或新版 `palpanelctl install`，用于安装 root 更新器和 systemd 路径单元。完成后才能继续使用面板内更新。
- 当前外部更新器只用于 Linux amd64 正式 systemd 安装；便携模式和 Windows 不执行该流程。
- 本地执行环境无法下载仓库要求的 Go `1.25.12` 工具链及缺失模块，因此完整 Go 测试由 GitHub Actions 验证。
