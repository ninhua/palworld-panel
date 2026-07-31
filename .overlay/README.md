# 根目录覆盖增量包

基线：`ninhua/palworld-panel` `custom-stable`，自定义版本 `0.8.52`
目标版本：`0.8.53`

使用前先确认仓库中的：

```text
backend/internal/api/patch_info.go
```

仍显示：

```go
patchVersion = "0.8.52"
```

然后将压缩包内容直接解压到仓库根目录，允许覆盖同名文件。

本包不会修改 Git 历史，不包含 `.git`，不会提交或推送。

建议覆盖后执行：

```bash
git diff --check
cd backend && go test ./...
cd ../frontend && npm run check
```

本版本只提供积分后端基础和接口，尚未接管 AstrBot 旧积分数据库，也尚未加入积分管理前端页面。
