# Boss 系统 API（0.8.67）

`0.8.64` 建立 Boss 模板、奖励配置和手动召唤审计账本；`0.8.66` 增加 `/boss` 管理界面；`0.8.67` 增加多波次编排、召唤波次快照和逐波状态记录。

当前执行模式仍为 `record_only`：面板负责配置、顺序、状态和审计，不会直接向 PalDefender 或 RCON 发送生成 Boss 的命令。

## 波次模型

每个 Boss 模板最多配置 20 个波次。波次按 `position` 从小到大排列，支持以下类型：

```text
main           主 Boss
minion         护卫怪
reinforcement  增援
```

每个波次包含：

- 波次名称和类型；
- Pal ID、等级和数量；
- HP、攻击、防御倍率；
- 生成半径；
- 上一波结束后的延迟秒数；
- 是否允许捕获；
- 扩展 metadata。

模板没有显式波次时，创建召唤记录会自动生成一个隐式 `main` 波次，参数来自 Boss 模板本身。这样可以保持 `0.8.64` 以前的单 Boss 模板兼容性。

## 波次快照

`POST /api/boss/summons` 创建召唤记录时，会把当时的模板波次复制到 `boss_summon_waves`。

之后修改模板波次不会改变已经存在的召唤记录。快照会保存来源波次 ID、顺序和全部战斗参数。

波次状态：

```text
pending -> active | failed | skipped
active  -> completed | failed | skipped
```

中文含义：

```text
pending    等待执行
active     进行中
completed  已完成
failed     失败
skipped    已跳过
```

系统要求前置波次结束后才能把后续波次设为 `active`。第一波进入 `active` 时，召唤记录会自动进入 `active`；所有波次完成或跳过后，召唤记录自动进入 `completed`；任一波次失败时，召唤记录进入 `failed`，剩余未结束波次被标记为 `skipped`。

管理员直接把召唤记录更新为终态时，仍处于 `pending` 或 `active` 的波次会被标记为 `skipped`，避免汇总数据残留。

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
```

## 保存模板波次示例

```json
{
  "waves": [
    {
      "name": "外围护卫",
      "kind": "minion",
      "pal_id": "SheepBall",
      "level": 40,
      "count": 8,
      "hp_multiplier": 2,
      "attack_multiplier": 1,
      "defense_multiplier": 1,
      "spawn_radius": 350,
      "delay_seconds": 0,
      "capturable": false,
      "metadata": {}
    },
    {
      "name": "空涡龙本体",
      "kind": "main",
      "pal_id": "JetDragon",
      "level": 60,
      "count": 1,
      "hp_multiplier": 5,
      "attack_multiplier": 2,
      "defense_multiplier": 1.5,
      "spawn_radius": 500,
      "delay_seconds": 30,
      "capturable": false,
      "metadata": {}
    }
  ]
}
```

提交空数组会清除显式波次：

```json
{"waves": []}
```

后续新召唤将使用模板本身生成一个隐式主 Boss 波次。

## 更新波次状态示例

```json
{
  "status": "active",
  "message": "管理员已在游戏内执行第一波命令",
  "result": {
    "rcon_reference": "manual-001"
  }
}
```

`result` 只用于审计，不会触发外部命令。

## 面板操作

`/boss` 页面现在支持：

- 在 Boss 模板卡片中打开“配置波次”；
- 添加、删除和上下移动波次；
- 从帕鲁目录下拉选择 Pal ID；
- 配置类型、等级、数量、倍率、半径、延迟和捕获规则；
- 查看每次召唤保存的波次快照；
- 按中文状态逐波推进、失败或跳过；
- 在同一区域查看召唤状态审计。

这些功能只建立人工执行闭环。实际生成命令将在后续 PalDefender/RCON 执行器版本接入。
