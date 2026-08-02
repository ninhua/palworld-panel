# Palworld Panel 0.8.81 Boss 活动互斥维护增量 1

将本压缩包直接解压到 `ninhua/palworld-panel` 仓库根目录并覆盖同名文件，然后重新构建并重启面板。

## 前置条件

必须已经包含 `boss-retry1`。本包不重复携带重试控制文件，与 `player-identity-fix1`、`task-feedback1` 运行时代码独立。

## 本次内容

- 同一服务器数据库只允许一个 Boss 活动持有执行权。
- 自动和手动 `ExecuteNextWave` 使用同一个 SQLite 活动锁。
- 首波成功后，活动在后续波次延迟期间继续持有锁。
- 暂停中的已启动活动继续持有锁，避免其他活动插入。
- 面板重启后自动恢复已有 `active` 召唤的活动所有权。
- 完成、失败、取消或删除召唤后自动释放锁。
- `running`、`uncertain` 和活动波次继续阻断其他活动，直到人工对账。
- 相同 `schedule_id`、`plan_id` 或 `auto_execute_plan_key` 按创建顺序执行，后续计划不能越过前序计划。
- 手动执行在跨事务切换为 `active` 前保留 30 秒临时所有权；异常退出后自动清理。
- 每次首次取得或恢复活动锁都会写入 Boss 召唤审计。

现有定时计划仍通过唯一 `request_key` 防止同一计划时间点重复建单；本包补充的是跨波次生命周期和跨进程的启动互斥。

## 验证

```bash
cd backend
gofmt -w internal/boss/activity_guard.go internal/boss/activity_guard_test.go \
  internal/boss/auto_execution.go internal/boss/execution.go \
  internal/api/boss_activity_guard_features.go
go test ./internal/boss -count=1
go test ./...
```

## 注意

- 活动被暂停或处于不确定状态时，其他活动不会自动启动。
- 需要先完成、取消、失败结算或人工对账当前活动，才会释放执行权。
- 本维护增量不修改 `patchVersion`、OpenAPI 或生成合同。
