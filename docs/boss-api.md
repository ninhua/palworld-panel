# Boss 系统 API（0.8.64）

`0.8.64` 建立 Boss 模板、奖励配置和手动召唤审计账本。当前版本只记录召唤请求和状态，不会向 PalDefender 或 RCON 发送生成命令。

## 数据模型

### 奖励

奖励可配置：

- 积分数量；
- 物品 ID 与数量；
- PalDefender 帕鲁模板名称；
- 启用状态；
- 扩展 metadata。

奖励在本版不会自动发放。后续 Boss 参与者结算器会复用该配置。

### Boss 模板

模板包含：

- Pal ID、等级和数量；
- HP、攻击、防御倍率；
- 生成半径；
- 是否可捕获；
- 冷却秒数；
- 默认坐标；
- 可选奖励 ID；
- 启用和归档状态。

### 手动召唤记录

`POST /api/boss/summons` 使用 `request_key` 幂等创建记录。返回字段：

```json
{
  "execution_mode": "record_only",
  "status": "pending"
}
```

`record_only` 表示面板只保存请求快照，不会实际召唤 Boss。

允许的状态流转：

```text
pending -> active | failed | cancelled
active  -> completed | failed | cancelled
```

终态不能再次修改。每次创建和流转都会写入 `boss_summon_events`。

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

GET    /api/boss/summons
POST   /api/boss/summons
POST   /api/boss/summons/{id}/transition
GET    /api/boss/summons/{id}/events
```

## 创建奖励示例

```json
{
  "name": "空涡龙首杀奖励",
  "description": "活动结算配置",
  "points": 500,
  "items": [
    {"item_id": "LegendSphere", "count": 10}
  ],
  "pal_templates": ["boss_reward_pal.json"],
  "enabled": true,
  "metadata": {}
}
```

## 创建模板示例

```json
{
  "name": "空涡龙试炼",
  "description": "单 Boss 模板",
  "pal_id": "JetDragon",
  "level": 60,
  "count": 1,
  "hp_multiplier": 5,
  "attack_multiplier": 2,
  "defense_multiplier": 1.5,
  "spawn_radius": 500,
  "capturable": false,
  "cooldown_seconds": 3600,
  "reward_id": "reward_xxx",
  "location": {"x": 100, "y": 200, "z": 300, "label": "Arena"},
  "enabled": true,
  "metadata": {}
}
```

## 创建召唤记录示例

```json
{
  "template_id": "template_xxx",
  "request_key": "manual-20260801-001",
  "notes": "管理员在游戏内手动执行后更新状态",
  "metadata": {}
}
```

重复提交相同 `request_key` 会返回原记录，并将 `duplicate` 设为 `true`。
