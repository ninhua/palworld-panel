# Palworld Panel 根目录覆盖包

升级范围：`0.8.66 -> 0.8.67`

## 本版内容

### Boss 多波次编排

Boss 模板新增波次配置：

- 每个模板最多 20 波；
- 支持主 Boss、护卫怪和增援；
- 每波独立配置 Pal ID、等级、数量、生命/攻击/防御倍率、生成半径、延迟和捕获规则；
- 支持添加、删除和上下调整顺序；
- Pal ID 可直接从现有帕鲁目录下拉选择；
- 清空显式波次后，新召唤会自动使用模板本身生成一个隐式主 Boss 波次。

### 召唤波次快照与状态

创建召唤记录时会复制当时的模板波次。以后修改模板不会改变历史记录。

波次状态中文显示为：

- 等待执行；
- 进行中；
- 已完成；
- 失败；
- 已跳过。

前置波次未结束时不能启动后续波次。第一波开始时召唤记录自动进入“进行中”；全部波次完成或跳过后自动进入“已完成”；任一波次失败时召唤记录进入“失败”，其余未结束波次自动跳过。

每次逐波操作都会写入原有召唤审计事件。

## 前置条件

必须已经应用到 `0.8.66`，并确认：

```go
patchVersion = "0.8.66"
```

## 安装

将压缩包内容直接解压到 `ninhua/palworld-panel` 仓库根目录，覆盖同名文件。

压缩包顶层直接是 `.overlay`、`backend`、`docs` 和 `frontend`，不包含额外目录层级。

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

## 使用注意

- 当前执行模式仍为 `record_only`，不会自动调用 PalDefender 或 RCON 生成 Boss。
- “延迟秒数”目前用于编排和审计，不会自动等待或执行命令。
- 管理员应在游戏内完成每一波实际操作后，再在面板推进对应波次状态。
- 原有单 Boss 模板不需要迁移；未配置波次时会自动生成兼容的隐式波次。
