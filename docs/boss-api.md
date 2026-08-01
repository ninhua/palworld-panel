# Boss 系统 API（0.8.68）

`0.8.64` 建立 Boss 模板、奖励和召唤审计；`0.8.67` 增加多波次编排；`0.8.68` 增加定时计划、提前预警审计和后台到期扫描器。

Boss 执行模式仍为 `record_only`：计划到点后会自动创建召唤记录与波次快照，但不会向 PalDefender 或 RCON 发送实际生成 Boss 的命令。

## 定时计划

计划支持两种模式：

```text
daily  每天固定时间
cron   五段 Cron：分 时 日 月 周
```

每个计划保存：

- Boss 模板；
- 时区；
- 每日时间或 Cron 表达式；
- 提前预警分钟数；
- 预警标题和消息；
- 可选坐标覆盖；
- 启用状态和扩展 metadata；
- 下次运行、上次运行和上次预警时间。

后台工作器启动后 5 秒进行第一次扫描，之后每 30 秒扫描一次。应用重启后会根据数据库中的 `next_run_at` 继续运行。

## Cron 语法

Cron 使用五段格式：

```text
分 时 日 月 周
```

支持：

- `*` 通配符；
- 逗号列表，例如 `1,15,30`；
- 范围，例如 `1-5`；
- 步长，例如 `*/15`、`1-10/2`；
- 周日可写 `0` 或 `7`。

示例：

```text
0 20 * * 6      每周六 20:00
0 20 * * 0      每周日 20:00
30 19 * * 1-5   工作日 19:30
*/15 * * * *    每 15 分钟
```

时间按计划的 IANA 时区解释，例如 `Asia/Shanghai`。

## 提前预警

当当前时间进入 `next_run_at - warning_minutes` 到 `next_run_at` 之间时，系统写入一次 `warning` 审计事件。唯一约束保证同一计划、同一计划时间不会重复生成预警。

预警消息支持变量：

```text
{{minutes}}  提前分钟数
{{schedule}} 计划名称
{{boss}}     Boss 模板名称
```

`0.8.68` 只把预警写入 `boss_schedule_events`，不会自动发送游戏广播。后续 PalDefender 广播执行器会消费这些审计记录。

## 到期执行

计划到期时，系统使用确定性的请求键创建召唤记录：

```text
boss-schedule:<schedule_id>:<planned_unix>
```

因此重复扫描不会重复创建召唤。召唤 metadata 会包含：

```json
{
  "schedule_id": "schedule_xxx",
  "schedule_name": "周六晚间Boss",
  "planned_for": "2026-08-01T12:00:00Z",
  "source": "scheduled"
}
```

计划执行失败也会写入审计，并把计划推进到下一次时间，避免每 30 秒无限重试同一个过期时间点。

## 路由

```text
GET    /api/boss/summary

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

GET    /api/boss/schedules
POST   /api/boss/schedules
PUT    /api/boss/schedules/{id}
DELETE /api/boss/schedules/{id}
POST   /api/boss/schedules/{id}/run-now
GET    /api/boss/schedule-events
POST   /api/boss/maintenance/run-due
```

## 创建每日计划示例

```json
{
  "name": "每日晚八点Boss",
  "template_id": "template_xxx",
  "mode": "daily",
  "daily_time": "20:00",
  "cron": "",
  "timezone": "Asia/Shanghai",
  "warning_minutes": 30,
  "warning_title": "Boss活动即将开始",
  "warning_message": "{{boss}}将在{{minutes}}分钟后开始，请提前前往活动区域。",
  "enabled": true,
  "metadata": {}
}
```

## 创建 Cron 计划示例

```json
{
  "name": "周六晚间Boss",
  "template_id": "template_xxx",
  "mode": "cron",
  "daily_time": "",
  "cron": "0 20 * * 6",
  "timezone": "Asia/Shanghai",
  "warning_minutes": 60,
  "warning_title": "周末Boss预警",
  "warning_message": "{{schedule}}将在{{minutes}}分钟后开始。",
  "location_override": {
    "x": 100,
    "y": 200,
    "z": 300,
    "label": "火山竞技场"
  },
  "enabled": true,
  "metadata": {}
}
```

## 面板操作

`/boss` 页面新增“定时计划”页签：

- 创建和编辑每日/Cron 计划；
- 配置时区、预警和坐标覆盖；
- 查看下次运行和上次计划时间；
- 立即创建一次召唤记录；
- 手动触发到期扫描；
- 查看预警和召唤审计。
