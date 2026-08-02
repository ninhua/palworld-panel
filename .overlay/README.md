# Palworld Panel 0.8.81 Boss 自动执行维护增量 2

将压缩包直接解压到 `ninhua/palworld-panel` 仓库根目录，覆盖同名文件。

## 基线

- 分支：`custom-stable`
- 基线提交：`944c37c20cf499149eddc0f4ab453aaf19a95401`
- 补丁版本：`0.8.81`
- 前置维护增量：`boss-auto1`

覆盖前请确认当前存在：

```text
backend/internal/boss/auto_execution.go
```

且其 Git blob SHA 为：

```text
c371cf70ad4304e90f5d435b865a08e1ae87b671
```

## 本次内容

- 自动波次执行增加 SQLite 数据库租约。
- 同一数据库只允许一个面板进程执行自动波次扫描。
- 租约每轮自动续期，面板正常停止时主动释放。
- 面板异常退出后租约最多 30 秒自动过期。
- 旧进程不能释放新进程持有的租约。
- 保留单周期最多执行一波、活动波次阻断和不确定结果阻断规则。

## 验证

```bash
git diff --check

cd backend
go test ./internal/boss -run '^TestAutoExecution' -count=1
go test ./internal/api -run '^TestPatchInfo$' -count=1
```

本维护增量不修改 OpenAPI、生成合同或 `patchVersion`。
