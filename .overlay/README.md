# Palworld Panel 根目录覆盖增量包

基线：`ninhua/palworld-panel` `custom-stable`，自定义版本 `0.8.53`
目标版本：`0.8.54`

## 使用方法

1. 先确认仓库已经应用 `Palworld-Panel-overlay-v0.8.52-to-v0.8.53`，并且：

   ```go
   patchVersion = "0.8.53"
   ```

2. 备份当前工作区或先执行 `git status`。
3. 将本压缩包内容直接解压到仓库根目录，允许覆盖同名文件。
4. 重新构建前后端。

本包不包含 `.git`，不会提交、推送或修改 Git 历史。

## 本版内容

- 游戏命令前缀可在面板中配置。
- 前缀支持任意无空白字符串，最多 16 个 Unicode 字符。
- 前缀可以留空；留空后使用 `签到`、`积分`、`帮助`。
- 无前缀模式不会把普通聊天识别为命令。
- 每日签到积分可在面板中配置。
- 新增 `/economy` 积分管理页面。
- 支持账户搜索、流水查看和管理员人工调整积分。

## 覆盖后验证

```bash
git diff --check

cd backend
go test ./...

cd ../frontend
npm run check
```

然后检查：

```text
GET /api/patch/info
```

应返回：

```json
{
  "patch": {
    "version": "0.8.54"
  }
}
```

登录面板后进入“世界管理 → 积分系统”，可将命令前缀设置为 `!`、`/`、`#` 或留空。

## 数据说明

升级会在现有 `palpanel.db` 中自动建立 `economy_settings` 表，不删除或重建已有积分数据。

首次建立设置时可读取：

```env
PALPANEL_GAME_COMMAND_PREFIX=!
PALPANEL_DAILY_CHECKIN_POINTS=10
PALPANEL_OPERATIONS_TIMEZONE=Asia/Shanghai
```

配置写入数据库后，后续启动不会被环境变量覆盖。
