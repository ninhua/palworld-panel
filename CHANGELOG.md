# Changelog — palworld-panel custom-stable

基于 [uitok/palworld-panel](https://github.com/uitok/palworld-panel) 上游，叠加自定义修复。

---

## v1.3.1-custom.0.8.93 (2026-08-06)

### save-history-noise-filter1
- 默认差异仅统计玩家、据点和公会归属的物品与帕鲁
- 排除野外刷新、地图对象容器、未归属容器和纯世界帕鲁
- PalDefender 捕获、登录和制作日志作为事件证据
- 原始字段差异继续保留在诊断明细与导出中

---

## v1.3.1-custom.0.8.87 (2026-08-05)

### boss-registration1
- Boss 参与者注册，幂等 + 数据库 lease
- 参与人数上限 + 区域资格检查
- 注册审计追踪

---

## v1.3.1-custom.0.8.86 (2026-08-05)

### shop-fix1
- 商品目录产品编辑器，支持组合筛选
- PlayerUID/SteamID 账户解析
- 兑换尝试记录，含详细游戏错误诊断
- 生成交付载荷支持
- 兑换诊断 UI

---

## v1.3.1-custom.0.8.85 (2026-08-04)

### boss-lifecycle1
- 固定 Boss 召唤后进入 PalDefender 死亡检测，按击杀数自动完成
- SQLite 持久化检测游标、事件去重、多进程进度
- 面板重启恢复、日志轮转恢复
- 超时保持进行中 + 人工核对

### startergift-history1
- 全量重新发放前归档旧周期
- 下次登录重发保留旧周期，取消时撤销
- 礼包发放历史页面 + 查询接口
- 历史与当前分离

---

## v1.3.1-custom.0.8.84 (2026-08-04)

### boss-fixed1
- 独立 Boss 管理页面
- PalTemplate 直接选择，固定坐标召唤
- 数量/半径/捕捉规则控制

---

## v1.3.1-custom.0.8.83 (2026-08-04)

### player-identity-fix1
- PlayerUID 格式统一
- Steam64 日志匹配
- 积分账户合并去重
- 任务进度合并
- PalDefender 登录 IP 修复
- 死信自动忽略

---

## v1.3.1-custom.0.8.82 (2026-08-04)

### bridge 增强
- 管理员 API Key 可访问内网 HTTP 诊断控制台
- PalPanelBridge 在线玩家查询
- UE4SS 属性探测
- 桥接响应时间戳

---

## v1.3.1-custom.0.8.81 (2026-08-02)

### 上游 v0.8.81 → v0.8.81-boss-auto3
- boss RCON 执行器（paldefender 适配）
- boss 执行尝试记账、运行时锁、对账
- boss 自动执行：数据库 lease、多进程去重、崩溃恢复、暂停/恢复/跳过控制 + 审计

### 自定义修复
- **fix13**: `v1.3.0` → `v1.3.1`
- **fix15**: `normalizePatchFeatures` 去重 + `"strings"` import
- **fix8**: `PalDefenderItemCatalogEntry.collaboration` + `PalDefenderPalCatalogEntry.kind` → openapi.yaml + contracts.ts
- **actions 路由**: 补回 `POST /mods/configurations/{adapter}/actions`
- **5 custom features**: 补回 patch_info.go
- **contracts.ts 顺序**: boss 类型排序
- **TS6.0 兼容**: `{error &&` → `{!!error &&`

---

## v1.3.1-custom.0.8.80 (2026-08-02)

### 上游 v0.8.75 → v0.8.80
- 经济 UI 打磨
- 玩家在线状态监控
- game event bridge 重构 + 任务系统
- 商店经济系统

### 自定义修复
- ipv4Pattern 重复声明
- fix13/15/8/16
- 6 个 test 404 清理
- actions 路由补回
- contracts.ts 同步

---

## v1.3.1-custom.0.8.74 (2026-08-01)

### 上游
- 诊断控制台响应 JSON 格式化树

---

## v1.3.1-custom.0.8.72 (2026-08-01)

### 上游
- 签到规则完整配置：基础积分、连续签到递增、周期奖励
- 签到别名配置
- 裸服命令别名

---

## v1.3.1-custom.0.8.67 (2026-08-01)

### 上游 v0.8.67 + fix11
- Boss 多波次编排（最多 20 波，主/护卫/增援三种类型）
- 每波独立 Pal ID/等级/数量/倍率配置

---

## v1.3.0-custom.0.8.66 (2026-08-01)

### 上游 v0.8.66
- Boss 管理界面：奖励方案、模板、召唤状态追踪
- Boss 目录选择器、召唤审计、波次编辑器

---

## v1.3.0-custom.0.8.63 (2026-07-31)

### 上游 v0.8.63
- 商城批量交付接口 + 自动交付状态机
- 运营维护能力补齐

---

## v1.3.0-custom.0.8.52 (2026-07-30)

### 上游 v0.8.50 → v0.8.52
- 版本号递增发布
- 离线地图 vendor 镜像
- MapLibre v5 WebGL 降级

---

## v1.3.0-custom.0.8.47 (2026-07-30)

### 修复
- 地图资源 Release 完整包构建修复
- POI JSON 缺失时优雅降级

---

## v1.3.0-custom.0.8.42 (2026-07-30)

### 修复
- MapLibre 画布上限 4096×4096 显式设置

---

## v1.3.0-custom.0.8.41 (2026-07-30)

### 新增
- 诊断页面一键体检
- 脱敏支持包导出
- 自我托管 MapLibre 地图切片
- 离线 vendor 镜像

---

## v1.3.0-custom.0.8.32 (2026-07-29)

### 新增
- 配置修订历史（PalWorldSettings.ini 自动记录）
- 存档历史差异对比
- 崩溃循环保护
- 事件中心 + 签名 webhook
- UID 重映射自定义版本哨兵

---

## v1.3.0-custom.0.8.31 (2026-07-29)

### 新增
- 面板更新三种模式：auto / external / exec
- 无 systemd 环境适配
- 启动健康回滚

---

## v1.3.0-custom.0.8.28 (2026-07-29)

### 修复
- PalDefender RCON 回复包 ID=0 被忽略的问题

---

## v1.3.0-custom.0.8.27 (2026-07-29)

### 修复
- GitHub 不可达时安全防护页面无法显示 PalDefender 状态

---

## v1.3.0-custom.0.8.26 (2026-07-29)

### 修复
- RCON 命令检查改为后端执行，不再被浏览器中断
- 增加操作审计记录

---

## v1.3.0-custom.0.8.25 (2026-07-29)

### 修复
- Linux 正式安装环境面板热更新写入权限

---

## v1.3.0-custom.0.8.24 (2026-07-29)

### 新增
- PalDefender 运行时 RCON 命令诊断入口

---

## v1.3.0-custom.0.8.23 (2026-07-29)

### 修复
- PalDefender RCON 返回 `Unknown command` 时正确识别

---

## v1.3.0-custom.0.8.22 (2026-07-29)

### 修复
- 面板版本查询使用镜像源（避免 GitHub 不可达）

---

## v1.3.0-custom.0.8.21 (2026-07-29)

### 修复
- "下次进入视为新玩家" 不再删除已有发放记录
- 玩家标记后立即显示状态

---

## v1.3.0-custom.0.8.20 (2026-07-28)

### 新增
- 帕鲁模板多选筛选（分类、分级、用途、标签、状态）

---

## v1.3.0-custom.0.8.19 (2026-07-28)

### 新增
- 后端 API 使用文档
- 新玩家礼包功能完善

---

## v1.3.0-custom.0.8.18 (2026-07-28)

### 初始发布
- 从补丁链迁移到完整源码 Fork 的首个自定义稳定版本
- 基于上游 `v1.3.0`

---

## 自定义修复说明

| 编号 | 说明 | 引入版本 |
|------|------|----------|
| fix8 | `PalDefenderItemCatalogEntry.collaboration` + `PalDefenderPalCatalogEntry.kind` schema | all |
| fix12 | save-migration 路由保留（v0.8.77 起上游已删除，不再需要） | ≤0.8.76 |
| fix13 | 版本号 `v1.3.0` → `v1.3.1` | all |
| fix15 | `normalizePatchFeatures()` 去重 patch features | all |
| fix16 | `clampBridgeOffsetToPendingError` 防止 bridge offset 越界 | all |
| fix19 | host migration 备份安全 | ≥0.8.75 |
