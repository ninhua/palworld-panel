# Palworld Panel overlay 0.8.80 → 0.8.81

将本压缩包直接解压到 `ninhua/palworld-panel` 仓库根目录，覆盖同名文件。

## 内容

- 新增 Boss PalDefender RCON `PalSummon` 执行适配层。
- 召唤记录可以逐波执行，不再只能保存 `record_only` 审计。
- 支持固定坐标、数量、生成半径和禁止捕捉。
- 支持模板级或波次级 `metadata.pal_template_file`。
- 指定 PalTemplate 时校验 JSON、PalID 和等级。
- 倍率不为 `1` 且未指定精确 PalTemplate 时阻止执行，避免生成错误属性。
- 新增进程内执行锁和持久化执行尝试账本。
- 命令已发送但结果无法确认时标记 `uncertain`，不会自动重试。
- 面板重启时遗留的 `running` 尝试自动转换为 `uncertain`。
- Boss 页面新增执行器状态、执行下一波按钮、PalTemplate 配置和执行记录。
- 新增 3 个 API，OpenAPI 操作数量更新为 351。
- 补丁版本同步至 `0.8.81`。

## 覆盖前提

仓库当前补丁版本应为：

```go
patchVersion = "0.8.80"
```

## 使用条件

实际执行需要：

- PalDefender 已安装并至少成功启动一次；
- `PalDefender/Pals/Templates` 和 `PalDefender/Pals/Summons` 可用；
- `PalWorldSettings.ini` 中 `RCONEnabled=True`；
- 管理员密码不为空；
- 面板配置的 RCON 主机和端口可访问。

创建召唤记录后，在“Boss管理 → 召唤记录”点击“执行下一波”。本版本不会自动连续执行后续波次。

## 精确 PalTemplate

生命、攻击或防御倍率不为 `1` 时，在 Boss 模板或波次中填写 PalDefender PalTemplate 文件名，例如：

```text
ArenaBoss.json
```

对应 JSON 必须位于：

```text
PalDefender/Pals/Templates/ArenaBoss.json
```

其中的 `PalID` 和 `Level` 必须与波次一致。

## 安全行为

当 RCON 命令可能已执行，但面板没有收到确定响应时：

- 执行记录标记为 `uncertain`；
- 当前波次保留为 `active`；
- 系统禁止继续执行下一波；
- 管理员需先进入游戏核对，再手动将波次更新为完成、失败或跳过。

## 验证

```bash
git diff --check

cd backend
go test ./internal/boss -count=1
go test ./internal/api -run '^TestNewContractRoutes$|^TestPatchVersionArtifactsStayInSync$' -count=1
go test ./...

cd ../frontend
npm run check
```
