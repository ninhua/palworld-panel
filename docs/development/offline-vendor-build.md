# 第三方镜像与完全离线构建

## 目录

建议把镜像放在独立仓库或磁盘目录：

```text
palpanel-vendor/
├── vendor-manifest.json
├── sources/
│   ├── palops-web/
│   ├── maplibre-gl/
│   ├── palcalc/
│   └── uesave/
├── assets/
│   └── palops-map-tiles/
└── caches/
    ├── npm/
    ├── cargo/
    ├── go-mod/
    └── nuget/
```

`third_party/github-actions-lock.json` 记录当前 workflow 使用的外部 Action；建议把对应源码快照存入 `sources/github-actions`。

大型瓦片和缓存建议存放在独立私有仓库、Git LFS 或 Release 资产，不要写入 PalPanel 主仓库历史。

## 初始化

先准备各组件的本地目录，然后执行：

```bash
python3 scripts/vendorctl.py init \
  --root /srv/palpanel-vendor \
  --palops-web /source/PalOpsWeb \
  --maplibre /source/maplibre-gl-6.0.0 \
  --palcalc third_party/palcalc \
  --uesave third_party/uesave \
  --map-tiles /source/authorized-palops-tiles \
  --ue4ss-sdk /source/RE-UE4SS-complete \
  --github-actions-bundle /source/github-actions-bundle \
  --npm-cache ~/.npm \
  --cargo-home ~/.cargo \
  --go-mod-cache "$(go env GOMODCACHE)" \
  --nuget-packages ~/.nuget/packages
```

初始化会复制文件并生成逐文件 SHA-256。镜像内容有意更新后运行：

```bash
python3 scripts/vendorctl.py refresh --root /srv/palpanel-vendor
```

## 构建模式

```bash
# 镜像优先，缺少包时允许联网补齐
PALPANEL_DEPENDENCY_MODE=mirror \
PALPANEL_VENDOR_ROOT=/srv/palpanel-vendor \
scripts/package.sh --version v1.3.0-custom.0.8.40 --clean

# 完全离线，要求源码、瓦片和四类缓存完整
PALPANEL_DEPENDENCY_MODE=offline \
PALPANEL_VENDOR_ROOT=/srv/palpanel-vendor \
scripts/package.sh --version v1.3.0-custom.0.8.40 --clean
```

Windows 使用相同环境变量运行 `scripts/package.ps1`。

## 权限与安全

- `ue4ss-sdk` 必须是包含已获许可私有子模块和固定文本编辑器历史的完整快照；Token 不得进入镜像。
- GitHub Actions 源码可以归档到 `sources/github-actions`，但云端首个 `actions/checkout` 仍依赖 GitHub；完全自主构建应使用预装依赖的自托管 Runner。
- 镜像中不得保存 GitHub Token、NuGet 凭据、npm Token、Cookie 或私人授权文件原件。
- 已授权地图瓦片放在 `assets/palops-map-tiles`；公开仓库应另存授权摘要，不公开含个人信息的授权书。
- `vendorctl` 拒绝符号链接和特殊文件，并在每次构建前验证文件数量、大小和 SHA-256。
- `offline` 模式仍要求构建主机预装 Go、Node.js、Python、Rust、.NET SDK、MinGW 等工具链；它归档的是项目依赖，不是操作系统安装介质。
