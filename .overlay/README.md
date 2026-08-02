# Palworld Panel overlay 0.8.79 → 0.8.80

将本压缩包直接解压到 `ninhua/palworld-panel` 仓库根目录，覆盖同名文件。

## 内容

- 修复 0.8.79 游戏事件桥接与在线任务采样可能导致的后端响应延迟。
- 在线时长任务改为复用面板实时监控原有的 Palworld REST 玩家快照。
- 新玩家礼包、玩家在线状态、游戏日志身份匹配和在线任务共享同一份玩家状态。
- 删除游戏事件桥接中的独立 PalDefender 玩家目录轮询。
- 日志桥接改为每轮扫描结束后再等待，避免 Ticker 积压后连续扫描。
- 有新增日志时等待 1 秒，空闲时等待 3 秒。
- 每轮只处理最近两个 PalDefender 日志文件。
- PalDefender 日志目录和积分命令配置缓存 30 秒。
- 死信及游标统计不再放入后台高频扫描。
- 积分页面的桥接状态刷新降低频率，并显示日志扫描耗时、共享快照年龄。
- OpenAPI 操作数量仍为 348，补丁版本同步至 `0.8.80`。

## 覆盖前提

仓库当前补丁版本应为：

```go
patchVersion = "0.8.79"
```

## 关键行为

实时监控仍按原有 15 秒周期取得玩家列表；在线任务最多每 30 秒结算一次。游戏日志桥接只负责聊天、捕捉、击杀、登录和制作日志，不再查询玩家目录。

## 验证

```bash
git diff --check

cd backend
go test ./internal/monitor -count=1
go test ./internal/api -run '^TestParsePalDefender|^TestGameEventBridge|^TestPatchVersionArtifactsStayInSync$' -count=1
go test ./...

cd ../frontend
npm run check
```
