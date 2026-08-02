# Palworld Panel 0.8.81 袭击据点与混合波次增量 1

将本压缩包直接解压到 `ninhua/palworld-panel` 仓库根目录并覆盖同名文件，然后重新构建并重启面板。

## 前置条件

仓库必须已经包含 `boss-guard1`。本包不包含已放弃的 `boss-detect1` 草案。

## 本次内容

- 原 Boss 活动导航调整为“袭击管理”。
- `/raids` 为新的袭击设计器；旧 `/boss` 地址继续进入袭击管理。
- 旧奖励、定时计划及完整审计界面保留在 `/raid-legacy`。
- 袭击模板必须选择当前存档索引中的据点。
- 模板仅保存据点 ID；执行每一波前重新解析据点实时坐标。
- 据点被删除、索引不可用或坐标无效时阻止执行，不回退到历史坐标。
- 一个袭击包含最多 20 个波次。
- 每个波次包含最多 20 个 PalTemplate 生成组。
- 同一波可以混合多种帕鲁；每组独立设置数量、生成半径和是否允许捕捉。
- 每组最多 100 只，每波合计最多 200 只。
- PalTemplate 选择器复用新玩家礼包的索引、分类、综合分级、用途、标签和模板状态筛选。
- 执行前校验模板文件、PalID 和等级快照，防止模板替换后误召唤。
- 无 `raid_base_id` 和 `spawn_groups` 的旧召唤记录继续走原执行逻辑。

## 验证

```bash
cd backend
gofmt -w internal/boss/raid_execution.go internal/boss/raid_execution_test.go \
  internal/api/raid_base_resolver.go internal/api/raid_base_features.go internal/api/boss.go
go test ./internal/boss -count=1
go test ./...

cd ../frontend
npm run check
```

## 注意

- 当前后端 API 和 SQLite 表仍使用 `/boss` 与 `boss_*` 名称，以保证旧数据和现有计划兼容。
- 独立的固定坐标 Boss 模块将在下一增量加入。
- 袭击死亡检测、参与统计和奖励结算尚未实现。
- 本维护增量不修改 `patchVersion`、OpenAPI 或生成合同。
