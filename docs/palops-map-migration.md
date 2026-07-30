# PalPanel 世界地图资源接入

PalPanel `0.8.47` 默认把专用资源仓库的 GitHub Release 附件作为完整地图资源包，统一同步 POI、图标、元数据、许可证与瓦片：

```text
https://github.com/ninhua/palpanel-assets
```

浏览器由自托管 MapLibre GL JS 5.24.0 渲染并自动选择 WebGL2 / WebGL1，不直接访问 GitHub。Linux 和 Windows 打包脚本会在构建阶段下载并验证资源，然后将其嵌入 PalPanel Web UI。

## 资源仓库目录约定

同步器优先识别标准化目录；若资源仓库保留自己的目录和文件名，也会自动发现 POI JSON 与 XYZ WebP 瓦片并转换为运行时结构。推荐标准结构：

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

### 资源仓库原生布局自动发现

从 `0.8.45` 起，资源仓库不必预先改名为 `default-pois.<locale>.json`。同步器会递归扫描：

- 自动检查 JSON 与 GeoJSON 内容；文件名不必包含固定关键字。
- JSON/GeoJSON 顶层数组、FeatureCollection，或 `pois`、`items`、`markers`、`locations`、`data`、`features`、`points`、`records` 数组。
- 按地图、类别或语言分组的嵌套对象，以及分散在多个文件中的数据。
- 路径末尾符合 `<z>/<x>/<y>.webp` 的 XYZ 瓦片。
- 名称包含 `palpagos`、`palworld`、`mainmap`、`worldtree` 等标识的瓦片目录。

单份 POI 数据会生成三种语言运行时文件；名称字段可以是字符串，也可以是带 `zh-CN`、`en-US`、`ja-JP` 键的对象。已有三语言数据仍会分别使用。同步器会将常见的 `poiId`、`mapId`、`position.x/y`、GeoJSON `properties`/`geometry.coordinates` 等字段归一化为 PalPanel POI 契约。

自动发现只改变源仓库布局要求；发布包中的路径仍固定为：

```text
/map/palops/data/default-pois.<locale>.json
/map/palops/tiles/<map>/<z>/<x>/<y>.webp
```

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

同步器先读取指定 GitHub Release，并下载选中的完整资源附件。源码分支仍会解析为完整提交 SHA，用作可选元数据和旧布局回退；源码归档中没有 POI 时不会阻止 Release 资源导入。生成清单会记录实际 Release 标签、附件名和源码提交。

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
PALPANEL_MAP_ASSETS_RELEASE_TAG=latest \
scripts/package.sh --version v1.3.0-custom.0.8.47
```

## 离线或本地资源构建

使用本地资源仓库目录：

```bash
PALPANEL_MAP_ASSETS_SOURCE_DIR=/srv/source/palpanel-assets \
scripts/package.sh --version v1.3.0-custom.0.8.47
```

使用预下载 ZIP：

```bash
PALPANEL_MAP_ASSETS_ARCHIVE=/srv/source/palpanel-assets.zip \
scripts/package.sh --version v1.3.0-custom.0.8.47
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
- `PALPANEL_MAP_ASSETS_RELEASE_TAG`
- `PALPANEL_MAP_ASSETS_RELEASE_ASSET`
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


## Release 完整资源归档

构建默认从 `palpanel-assets` 最新 GitHub Release 中自动选择名称包含 `map`、`tile`、`palops` 或 `asset` 的资源附件，并从同一个附件读取 POI 与瓦片。支持 `.zip`、`.tar`、`.tar.gz`、`.tgz`、`.tar.zst` 和 `.tzst`。

当前推荐附件名：

```text
palops-map-assets.tar.zst
```

可通过 `PALPANEL_MAP_ASSETS_RELEASE_TAG` 指定标签，通过 `PALPANEL_MAP_ASSETS_RELEASE_ASSET` 指定确切附件名。`.tar.zst` 构建需要 `zstd`；官方 Actions 工作流会在 Linux 和 Windows 构建作业中安装该工具，本地构建也可用 `PALPANEL_ZSTD` 指定可执行文件路径。
