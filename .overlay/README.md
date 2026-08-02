# Palworld Panel 0.8.81 Boss 重试维护增量

将本压缩包直接解压到 `ninhua/palworld-panel` 仓库根目录并覆盖同名文件。

## 前置条件

必须已经包含：

```text
v0.8.81-boss-auto3
```

本包不累计玩家身份修复或任务完成反馈补丁，与它们没有文件冲突。

## 本次内容

- 明确失败波次安全重试。
- 不确定结果及部分执行结果禁止重试。
- 默认 5 秒重试延迟。
- 失败波次和系统跳过的后续波次状态恢复。
- 手动跳过状态保留。
- 重试审计和元数据计数。
- 复用现有 Boss summon transition 接口，不增加路由数量。

## 调用

```bash
curl -X POST \
  -H 'Content-Type: application/json' \
  -d '{"status":"retry_failed"}' \
  http://127.0.0.1:8080/api/boss/summons/SUMMON_ID/transition
```

## 验证

```bash
git diff --check

cd backend
gofmt -w internal/boss/auto_execution_control.go \
  internal/boss/auto_execution_retry_test.go \
  internal/api/boss_auto_execution_control.go \
  internal/api/boss_auto_execution_features.go

go test ./internal/boss -run '^TestNormalizeAutoExecutionRetryAction$|^TestAutoExecutionRetryDelay$|^TestAutoExecutionMetadataInt$' -count=1
go test ./...
```

本维护增量不修改 `patchVersion`、OpenAPI 或生成合同。
