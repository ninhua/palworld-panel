# Boss 系统 API（0.8.81）

`0.8.81` 在现有 Boss 模板、奖励、波次、定时计划和召唤审计基础上增加安全的 PalDefender RCON 执行适配层。

计划到点仍先创建召唤记录和完整波次快照。管理员随后从召唤记录逐波执行；本版不会自动连续执行后续波次。

## 执行器

当前执行器代码：

```text
paldefender_rcon_palsummon
```

执行流程：

```text
选择下一条 pending 波次
→ 创建 running 执行尝试
→ 波次变为 active
→ 写入 PalDefender PalTemplate / PalSummon JSON
→ 通过 RCON 发送 /summon <PalSummon名称>
→ 成功时完成波次
→ 明确失败时标记波次失败
→ 结果不确定时保留 active，等待人工核对
```

支持：

- 固定世界坐标；
- 多只 Boss 按生成半径分散；
- 允许或禁止捕捉；
- 使用现有 PalDefender PalTemplate；
- 倍率全部为 `1` 时自动生成最小 PalTemplate；
- 每次命令和响应的持久化审计。

## PalTemplate 配置

模板或单个波次的 `metadata` 可以指定：

```json
{
  "pal_template_file": "ArenaBoss.json"
}
```

文件必须是有效 JSON，且其中的 `PalID` 和 `Level` 必须与波次配置一致。文件必须位于：

```text
PalDefender/Pals/Templates
```

当生命、攻击或防御倍率不为 `1` 时，必须指定经过管理员确认的 PalTemplate 文件。系统不会猜测 PalTemplate 的具体属性字段，也不会静默生成属性不一致的 Boss。

还可以配置 PalSummon 的状态禁用列表：

```json
{
  "disable_statuses": ["Burn", "Poison"]
}
```

## 执行安全

同一 PalPanel 进程一次只允许一个 Boss 波次执行。

以下情况会阻止继续执行：

- 已有波次处于 `active`；
- 上一次执行仍为 `running`；
- RCON 或 PalDefender 路径未配置；
- `AdminPassword` 为空；
- 指定的 PalTemplate 不存在；
- 非 1 倍率没有精确 PalTemplate。

当 RCON 命令已经发送，但连接在响应前断开，执行尝试标记为：

```text
uncertain
```

系统不会自动重试，避免重复生成。管理员应先进入游戏核对，再手动把活动波次更新为完成、失败或跳过。波次离开 `active` 后可以继续执行下一波；历史 `uncertain` 记录会保留。

## 执行状态

```text
GET /api/boss/execution/status
```

响应包含：

- 执行器是否可用；
- 当前是否忙碌；
- running / uncertain 尝试数量；
- active 波次数量；
- 是否需要人工核对；
- 支持能力与限制。

## 执行下一波

```text
POST /api/boss/summons/{id}/execute-next
```

无需请求体。接口选择第一条 `pending` 波次执行，返回执行尝试：

```json
{
  "id": "boss_exec_xxx",
  "summon_id": "summon_xxx",
  "wave_position": 1,
  "adapter": "paldefender_rcon_palsummon",
  "status": "succeeded",
  "command_count": 2,
  "completed_commands": 2,
  "commands": [
    "/summon PalPanelBoss_summon_xxx_W01_01",
    "/summon PalPanelBoss_summon_xxx_W01_02"
  ],
  "responses": ["...", "..."],
  "details": {}
}
```

`failed` 和 `uncertain` 也作为执行尝试返回，便于前端直接显示审计信息。

## 查询执行记录

```text
GET /api/boss/summons/{id}/executions
```

支持：

```text
limit
offset
```

## Boss 路由

```text
GET    /api/boss/summary
GET    /api/boss/execution/status

GET    /api/boss/rewards
POST   /api/boss/rewards
PUT    /api/boss/rewards/{id}
DELETE /api/boss/rewards/{id}

GET    /api/boss/templates
POST   /api/boss/templates
PUT    /api/boss/templates/{id}
DELETE /api/boss/templates/{id}
GET    /api/boss/templates/{id}/waves
PUT    /api/boss/templates/{id}/waves

GET    /api/boss/summons
POST   /api/boss/summons
POST   /api/boss/summons/{id}/transition
GET    /api/boss/summons/{id}/waves
POST   /api/boss/summons/{id}/waves/{position}/transition
GET    /api/boss/summons/{id}/events
POST   /api/boss/summons/{id}/execute-next
GET    /api/boss/summons/{id}/executions

GET    /api/boss/schedules
POST   /api/boss/schedules
PUT    /api/boss/schedules/{id}
DELETE /api/boss/schedules/{id}
POST   /api/boss/schedules/{id}/run-now
POST   /api/boss/schedules/{id}/test-warning
GET    /api/boss/schedule-events
POST   /api/boss/maintenance/run-due
```

## 当前限制

- 定时计划不会自动执行第一波；
- 不会按照 `delay_seconds` 自动连续推进；
- 不会自动判断 Boss 是否死亡；
- 尚未实现参与者、伤害、击杀归属和奖励结算；
- 未提供自动清场和传送。
