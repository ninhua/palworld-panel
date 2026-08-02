# Palworld Panel 0.8.81 Boss 自动执行维护增量 3

将本压缩包直接解压到 `ninhua/palworld-panel` 仓库根目录并覆盖同名文件。

## 前置条件

必须已经应用并上传：

```text
Palworld-Panel-overlay-v0.8.81-to-v0.8.81-boss-auto2.zip
```

本包不重复包含 `boss-auto2` 的数据库租约文件，只包含 auto2 到 auto3 的差异。

## 本次内容

- 自动波次暂停。
- 自动波次继续。
- 跳过第一条等待波次。
- 控制动作与自动执行器使用同一数据库租约。
- 暂停状态持久化并支持重启恢复。
- 控制动作写入 Boss 召唤审计。
- 复用现有召唤 transition 路由，不增加路由数量。

## 调用

```bash
curl -X POST \
  -H 'Content-Type: application/json' \
  -d '{"status":"pause"}' \
  http://127.0.0.1:8080/api/boss/summons/SUMMON_ID/transition
```

`status` 可使用：

```text
pause
resume
skip_current
```

## 验证

```bash
git diff --check

cd backend
gofmt -w internal/boss/auto_execution_control.go internal/boss/auto_execution_control_test.go \
  internal/api/boss.go internal/api/boss_auto_execution_control.go internal/api/boss_auto_execution_features.go
go test ./internal/boss -run '^TestNormalizeAutoExecutionControlAction$|^TestCurrentPendingAutoExecutionWave' -count=1
go test ./...
```
