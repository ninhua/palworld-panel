# PalPanel 积分系统 API

## 身份规则

积分账户以 Palworld `PlayerUID` 为唯一主键。昵称和 Steam ID 只作为可更新的辅助字段，不作为余额归属依据。

## 积分与游戏命令配置

查询配置：

```http
GET /api/economy/config
```

修改配置：

```http
PUT /api/economy/config
Content-Type: application/json
```

```json
{
  "command_prefix": "!",
  "daily_checkin_points": 10
}
```

`command_prefix` 规则：

- 默认值为 `!`。
- 可以设置为 `/`、`#`、`。`、`指令:` 等任意不含空白或控制字符的前缀。
- 最多 16 个 Unicode 字符。
- 可以保存为空字符串。留空后玩家直接发送 `签到`、`积分`、`帮助`。
- 无前缀模式只识别完整的已知命令；普通聊天不会作为命令处理。

`daily_checkin_points` 允许 `0` 到 `1000000`。设置为 `0` 时仍记录当日签到，但不增加积分。

首次建立积分设置时支持以下环境变量作为初始值：

```env
PALPANEL_GAME_COMMAND_PREFIX=!
PALPANEL_DAILY_CHECKIN_POINTS=10
PALPANEL_OPERATIONS_TIMEZONE=Asia/Shanghai
```

设置写入数据库后，以数据库配置为准。环境变量不会覆盖已经保存的配置。

## 管理员调整积分

```http
POST /api/economy/accounts/<player_uid>/adjust
Content-Type: application/json
```

```json
{
  "nickname": "玩家名称",
  "steam_id": "7656119...",
  "delta": 100,
  "reason": "admin_compensation",
  "reference_type": "manual_adjustment",
  "reference_id": "ticket-20260731-001",
  "metadata": {
    "note": "活动补偿"
  }
}
```

`reference_id` 建议始终填写。相同玩家、业务类型和业务引用重复提交时不会重复记账。

## 每日签到

```http
POST /api/economy/accounts/<player_uid>/checkin
Content-Type: application/json
```

```json
{
  "nickname": "玩家名称",
  "steam_id": "7656119..."
}
```

不传 `points` 或传入 `0` 时使用积分系统中保存的每日签到奖励。`local_date` 省略时由后端按 `PALPANEL_OPERATIONS_TIMEZONE` 计算。

## 游戏内命令入口

```http
POST /api/economy/commands/execute
Content-Type: application/json
```

```json
{
  "event_id": "paldefender-chat-20260731-000001",
  "player_uid": "00000000000000000000000000000000",
  "nickname": "玩家名称",
  "steam_id": "7656119...",
  "message": "!签到"
}
```

命令入口始终使用数据库中的当前命令前缀。返回的 `reply` 由事件采集器通过 PalDefender 私聊发回游戏。`event_id` 必须稳定且唯一；同一事件重试会返回原结果。

未匹配当前前缀、未知命令或普通聊天返回：

```json
{
  "handled": false
}
```

这类消息不会创建积分账户，也不会写入命令去重表。

## 积分预留

下单前预留：

```http
POST /api/economy/reservations
```

```json
{
  "player_uid": "00000000000000000000000000000000",
  "reference_id": "shop-order-0001",
  "amount": 300,
  "ttl_seconds": 900
}
```

发货成功：

```http
POST /api/economy/reservations/<reservation_id>/commit
```

发货失败或取消：

```http
POST /api/economy/reservations/<reservation_id>/release
```

预留时余额立即减少；提交只确认最终状态，释放和过期会原额退款。

## 面板页面

管理员登录后访问：

```text
/economy
```

页面支持：

- 查看积分账户数、流通积分、今日签到和最近 24 小时净发放。
- 设置游戏命令前缀及每日签到奖励。
- 按昵称、PlayerUID 或 Steam ID 查询账户。
- 查看单个玩家最近流水。
- 人工增加或扣除积分。

## AstrBot旧积分数据库迁移

该入口只接受浏览器上传的SQLite文件，不接受服务器任意文件路径。最大文件大小为64 MiB，并要求管理员浏览器会话。

检查数据库，不写入积分：

```http
POST /api/economy/imports/astrbot/inspect
Content-Type: multipart/form-data
```

表单字段：

```text
database=<astrbot_plugin_palpanel/palpanel.sqlite3>
```

确认迁移：

```http
POST /api/economy/imports/astrbot
Content-Type: multipart/form-data
```

迁移规则：

- 从旧库 `accounts`、`bindings` 和 `checkins` 表读取数据。
- 只将已经绑定 `PlayerUID` 的QQ账户迁入面板积分系统。
- 余额以 `PlayerUID` 入账，QQ号只保留在迁移审计元数据中。
- 同一QQ账户重复上传不会重复加分。
- 如果旧库余额比上次迁移时更高，只导入新增差额。
- 如果旧库余额下降，不自动扣除面板积分，检查结果会标记 `source_balance_decreased`。
- 旧签到记录会写入面板签到去重表，防止迁移当天再次签到重复领取。
- 每次确认迁移都会生成独立批次ID和积分流水。

推荐步骤：

1. 停止AstrBot旧插件的签到和积分写入。
2. 备份插件目录中的 `palpanel.sqlite3`。
3. 在积分系统页面上传并执行“检查数据库”。
4. 核对可导入积分、未绑定账户和余额下降账户。
5. 点击“确认迁移”。
6. 将AstrBot签到和积分查询改为调用PalPanel统一积分接口。
