# PalPanel 世界地图资源接入

PalPanel `0.8.43` 将世界地图瓦片、固定 POI、图标、元数据和许可证统一切换到专用资源仓库：

```text
https://github.com/ninhua/palpanel-assets
```

浏览器仍由自托管 MapLibre GL JS 6.0.0 渲染，不直接访问 GitHub。Linux 和 Windows 打包脚本会在构建阶段下载并验证资源，然后将其嵌入 PalPanel Web UI。

## 资源仓库目录约定

同步器会自动寻找包含 `data/default-pois.zh-CN.json` 的地图根目录。推荐结构：

```text
map/palops/
├── palpanel-map-assets.json       # 可选：源资源版本与文件 SHA-256 清单
├── data/
│   ├── default-pois.zh-CN.json
│   ├── default-pois.en-US.json
│   └── default-pois.ja-JP.json
├── tiles/
│   ├── palpagos/{z}/{x}/{y}.webp
│   └── world-tree/{z}/{x}/{y}.webp
├── icons/
└── licenses/
```

源清单建议至少声明资源版本、数据集版本和 POI 总数：

```json
{
  "schema_version": 1,
  "asset_version": "2026.07.30.1",
  "dataset_version": "palworld-0.7.0",
  "poi_total": 1251,
  "files": [
    {
      "path": "data/default-pois.zh-CN.json",
      "size": 123456,
      "sha256": "<64 位小写 SHA-256>"
    }
  ]
}
```

`files` 可以覆盖全部关键资源，也可以省略；只要存在，列出的每个文件都会被严格校验。

同步器也兼容以下根目录：

- 仓库根目录本身
- `palops/`
- `assets/map/palops/`
- `frontend/public/map/palops/`
- 旧 PalOps Web 的 `src/PalOps.Web/wwwroot/map/`

## 强制验证

发布构建默认要求：

- `zh-CN`、`en-US`、`ja-JP` 三份 POI 数据均存在。
- 三份数据的 POI ID、地图、类别和坐标完全一致。
- 若源清单声明 `poi_total`（或 `pois.total`），实际数量必须匹配；未声明时以三种语言一致的非空数据集为准。
- `palpagos` 与 `world-tree` 都包含完整的 0–4 级瓦片金字塔。
- 每张地图必须恰好包含 341 个 WebP 瓦片。
- 路径、扩展名、单文件大小和总资源大小符合安全限制。
- 若源仓库提供 `palpanel-map-assets.json.files`，每个条目的大小和 SHA-256 必须匹配。

同步完成后会重新生成运行时清单：

```text
frontend/public/map/palops/palpanel-map-assets.json
```

清单记录资源仓库、实际 Git 提交、请求的 ref、资源版本、数据集版本、POI 数量、瓦片数量和所有打包文件的 SHA-256。

## 默认联网构建

```bash
python3 scripts/sync_palops_map_assets.py \
  --destination frontend/public/map/palops \
  --repository ninhua/palpanel-assets \
  --ref main \
  --allow-network
```

同步器先通过 GitHub API 将 `main` 解析为完整提交 SHA，再按该提交下载归档。因此同一次构建使用不可变资源快照，而不是在下载过程中继续跟随分支变化。

## 私有资源仓库

若 `palpanel-assets` 为私有仓库，在 `ninhua/palworld-panel` 中创建 Actions Secret：

```text
PALPANEL_MAP_ASSETS_TOKEN
```

Token 只需要对 `ninhua/palpanel-assets` 的 `contents:read` 权限。CI、Release、Custom package release 和 Windows package 工作流会将该 Secret 传给同步器。

本地构建可直接设置：

```bash
PALPANEL_MAP_ASSETS_TOKEN=github_pat_xxx \
PALPANEL_MAP_ASSETS_REPOSITORY=ninhua/palpanel-assets \
PALPANEL_MAP_ASSETS_REF=main \
scripts/package.sh --version v1.3.0-custom.0.8.43
```

## 离线或本地资源构建

使用本地资源仓库目录：

```bash
PALPANEL_MAP_ASSETS_SOURCE_DIR=/srv/source/palpanel-assets \
scripts/package.sh --version v1.3.0-custom.0.8.43
```

使用预下载 ZIP：

```bash
PALPANEL_MAP_ASSETS_ARCHIVE=/srv/source/palpanel-assets.zip \
scripts/package.sh --version v1.3.0-custom.0.8.43
```

仅在开发诊断时允许缺少瓦片：

```bash
PALPANEL_ALLOW_MISSING_MAP_TILES=true scripts/package.sh --version dev
```

正式 Release 不应启用该选项。

## 兼容环境变量

旧变量暂时仍作为别名接受：

- `PALPANEL_PALOPS_MAP_SOURCE_DIR`
- `PALPANEL_PALOPS_MAP_ARCHIVE`
- `PALPANEL_PALOPS_TILE_SOURCE_DIR`

新部署应使用：

- `PALPANEL_MAP_ASSETS_REPOSITORY`
- `PALPANEL_MAP_ASSETS_REF`
- `PALPANEL_MAP_ASSETS_TOKEN`
- `PALPANEL_MAP_ASSETS_SOURCE_DIR`
- `PALPANEL_MAP_ASSETS_ARCHIVE`
- `PALPANEL_MAP_ASSETS_TILE_SOURCE_DIR`

## 运行时数据

固定 POI 由浏览器读取：

```text
/map/palops/data/default-pois.<locale>.json
```

瓦片由 MapLibre 读取：

```text
/map/palops/tiles/<map>/<z>/<x>/<y>.webp
```

玩家、据点、帕鲁和地图对象继续通过 PalPanel 的 `/api/map/entities` 获取。浏览器不会访问 GitHub，也不会获得地图资源仓库 Token。
