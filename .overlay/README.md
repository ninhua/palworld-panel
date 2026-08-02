# Palworld Panel 0.8.81 Boss 自动续波维护增量 1

将本压缩包直接解压到 `ninhua/palworld-panel` 仓库根目录，覆盖同名文件。

## 基线

- 分支：`custom-stable`
- 基线提交：`a890aa9b14cb2cb64b9edf252726cd8ab4905133`
- 面板补丁版本：`0.8.81`
- 本包为同版本维护增量，不修改 `patchVersion`、OpenAPI 或生成合同。

## 内容

- 新增显式启用的 Boss 自动续波执行器。
- 支持波次 `delay_seconds`。
- 支持重启后的持久化待执行扫描。
- 遇到活动波次或不确定执行记录时停止推进。
- 单周期最多执行一波，失败或不确定结果不自动重试。

## 启用示例

创建召唤时传入：

```json
{
  "metadata": {
    "auto_execute": true
  }
}
```

## 验证

```bash
git diff --check

cd backend
gofmt -w internal/boss/auto_execution.go internal/boss/auto_execution_test.go internal/api/boss_auto_execution_features.go
go test ./internal/boss -run '^TestAutoExecution' -count=1
go test ./internal/api -run '^TestPatchInfo$' -count=1
go test ./...

cd ../frontend
npm run check
```
