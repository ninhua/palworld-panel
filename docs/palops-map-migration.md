# PalOps 世界地图迁移

PalPanel `0.8.39` 将旧版单图 SVG 地图替换为 PalOps Web 1.3.2 的双地图数据模型，并使用自托管 MapLibre GL JS 6.0.0 渲染。

## 固定来源

- 仓库：`CoderYiXin/PalOpsWeb`
- 版本：`1.3.2`
- 提交：`dc2ec173c77e759482e59d9b63d228c88132061c`
- 数据集：`2026.07.5-extended`
- 地图：`palpagos`、`world-tree`
- 固定 POI：每种语言 1,251 条

## 构建时同步

```bash
python3 scripts/sync_palops_map_assets.py \
  --destination frontend/public/map/palops \
  --allow-network
python3 scripts/sync_maplibre_assets.py \
  --destination frontend/public/vendor/maplibre-gl \
  --allow-network
```

第一条命令只同步固定 POI、图标、元数据和许可证，不默认复制栅格瓦片。第二条命令固定同步 MapLibre ESM、shared module、module worker、CSS 和 BSD 许可证；浏览器只从 PalPanel 自身加载这些文件。MapLibre 6.0.0 需要 WebGL2。也可以使用本地 PalOps 源码目录避免联网：

```bash
python3 scripts/sync_palops_map_assets.py \
  --destination frontend/public/map/palops \
  --source-dir /srv/source/PalOpsWeb
```

## 导入有权使用的本地瓦片

```bash
PALPANEL_PALOPS_TILE_RIGHTS_CONFIRMED=true \
python3 scripts/sync_palops_map_assets.py \
  --destination frontend/public/map/palops \
  --source-dir /srv/source/PalOpsWeb \
  --tiles-source-dir /srv/palops/wwwroot/map/tiles
```

`palpagos` 和 `world-tree` 必须分别包含从 `0/0/0.webp` 到 4 级金字塔的 341 个文件。

## 运行时数据

固定 POI 由浏览器读取 `/map/palops/data/default-pois.<locale>.json`。玩家、据点、帕鲁和地图对象继续通过 PalPanel 的 `/api/map/entities` 获取。浏览器不会调用 PalOps API，也不会获得 PalDefender Token。

## 离线构建 MapLibre

构建机不能访问 unpkg 时，可预先准备 MapLibre GL JS 6.0.0 npm 包的根目录，并设置：

```bash
PALPANEL_MAPLIBRE_SOURCE_DIR=/srv/source/maplibre-gl-6.0.0 \
  scripts/package.sh --version v1.3.0-custom.0.8.39
```

目录必须包含 `dist/maplibre-gl.mjs`、`maplibre-gl-shared.mjs`、`maplibre-gl-worker.mjs`、`maplibre-gl.css` 和根目录 `LICENSE.txt`。
