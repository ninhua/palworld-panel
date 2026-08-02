# Changelog — palworld-panel custom-stable

基于 [uitok/palworld-panel](https://github.com/uitok/palworld-panel) 上游，叠加自定义修复。

## v1.3.1-custom.0.8.81 (2026-08-02)

### 上游 v0.8.81 → v0.8.81-boss-auto3
- boss RCON 执行器（paldefender 适配）
- boss 执行尝试记账、运行时锁、对账
- boss 自动执行（lease / 去重 / 崩溃恢复 / 暂停恢复跳过控制）

### 自定义修复
- **fix13**: 版本号 `v1.3.0` → `v1.3.1`
- **fix15**: `normalizePatchFeatures` 去重 + `"strings"` import
- **fix8**: `PalDefenderItemCatalogEntry.collaboration` + `PalDefenderPalCatalogEntry.kind` → openapi.yaml + contracts.ts
- **actions 路由**: 补回 `POST /mods/configurations/{adapter}/actions`（v0.8.81 缺失）
- **5 custom features**: 补回 `save-migration-wizard-entry` 等
- **contracts.ts 顺序**: boss 类型按字母排到正确位置
- **TS6.0 兼容**: 4 处 `{error &&` → `{!!error &&`（TypeScript 6.0 不允许 `unknown` 做 JSX 子元素）

## v1.3.1-custom.0.8.80 (2026-08-02)

### 上游 v0.8.80
- 经济 UI 打磨
- 玩家在线状态监控

### 自定义修复
- **ipv4Pattern**: 删除 v0.8.80 重复声明（复用 `support_bundles.go`）
- **fix13/15**: 版本号 + normalizePatchFeatures
- **fix8**: collaboration/kind → openapi.yaml
- **6 个 test 404**: 删除 `save_migration_api_test.go`，移除 `POST /bases/:id/clean` 断言
- **5 features**: 补回 patch_info.go
- **actions 路由**: 补回 openapi.yaml（v0.8.77 误删）
- **contracts.ts**: 手动同步 collaboration/kind

## v1.3.1-custom.0.8.79 (2026-08-02)

### 上游 v0.8.79
- game event bridge 重构
- 任务系统 + 经济 UI

### 自定义修复
- **ipv4Pattern**: 删除重复声明
- **TotalEmittedMinutes**: 修正字段名（`TotalEmittedMinutess`）
- **fix16**: 从 git 历史恢复 `clampBridgeOffsetToPendingError`（v0.8.77 覆盖）

## v1.3.1-custom.0.8.78 (2026-08-02)

### 上游 v0.8.78
- game event bridge 细化 + 经济 UI

### 自定义修复
- fix8 + fix13(v1.3.1) + fix15

## v1.3.1-custom.0.8.77 (2026-08-02)

### 上游 v0.8.77
- 商店经济系统

### 自定义修复
- fix8 + fix12 + fix13(v1.3.1) + fix15
- 移除已删除的 save-migration 路由测试
- 移除 `POST /api/bases/:id/clean` 断言（路由已在 v0.8.77 删除）

## v1.3.1-custom.0.8.76 (2026-08-02)

### 上游 v0.8.76
- 任务系统调度 + match reason 报告

### 自定义修复
- fix8 + fix12 + fix13(v1.3.1) + fix15 + fix19

## v1.3.1-custom.0.8.75 (2026-08-02)

### 上游 v0.8.75
- 诊断布局刷新

### 自定义修复
- fix8 + fix12 + fix13(v1.3.1) + fix15 + fix19（host migration backup safety）

---

## 修复说明

| 编号 | 说明 |
|------|------|
| fix8 | `PalDefenderItemCatalogEntry.collaboration` + `PalDefenderPalCatalogEntry.kind` schema |
| fix12 | v0.8.77 前需保留的 save-migration 路由（v0.8.77 起不再需要） |
| fix13 | 版本号：`v1.3.0` → `v1.3.1` |
| fix15 | `normalizePatchFeatures()` 去重 patch features |
| fix16 | `clampBridgeOffsetToPendingError` 防止 bridge offset 越界 |
| fix19 | host migration 备份安全 |
