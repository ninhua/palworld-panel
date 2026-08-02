# Palworld Panel 0.8.81 游戏内任务完成反馈维护增量 1

本包是 `player-identity-fix1` 之后的独立增量。将压缩包内容直接解压到 `ninhua/palworld-panel` 仓库根目录，覆盖同名文件，然后重新构建并重启面板。

## 修复内容

- 玩家通过捕捉、击杀、制作、签到衍生事件或在线时长完成任务后，会收到私人系统消息。
- 有积分奖励时明确提示奖励已经到账。
- 无积分奖励的任务也会提示完成。
- 只有任务刚完成时通知，普通进度变化不刷屏。
- 奖励仍处于 `pending` 或 `failed` 时不发送“已到账”消息；奖励重试成功后再通知。
- 玩家离线、PalDefender 暂时不可用或消息发送失败时，通知保留在 SQLite 队列中，每 30 秒重试。
- 通知使用数据库状态抢占，多面板进程不会同时发送同一条消息。
- 升级前已经完成的历史任务不会集中补发消息。

## 游戏内示例

```text
[SYSTEM] 【任务完成】捕捉帕鲁（3/3），奖励20积分已到账。
```

无积分奖励时：

```text
[SYSTEM] 【任务完成】到达活动区（1/1）。
```

## 前置建议

建议先应用 `player-identity-fix1`。这样 PalDefender 的 `[Player::SendMsg::*]` 系统回执不会再次进入解析失败死信，并且通知会使用已统一的玩家身份。

## 应用后

1. 重新构建并重启面板。
2. 创建或重置一个容易完成的测试任务。
3. 在游戏中触发对应事件。
4. 确认积分到账后约 1 秒内收到私人任务完成消息。
5. 离线完成或发送失败的通知会在玩家上线后继续重试。

## 验证命令

```bash
cd backend
gofmt -w internal/tasks/completion_notifications.go internal/tasks/completion_notifications_test.go internal/api/tasks.go
go test ./internal/tasks ./internal/api
go test ./...
```
