#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
root_dir="$(cd -- "$script_dir/.." && pwd -P)"
packages_dir="$root_dir/dist/packages"
staging_dir="$packages_dir/staging"
webui_embed_dir="$root_dir/backend/internal/webui/embedded"
palops_map_stage_dir="$root_dir/frontend/public/map/palops"
maplibre_stage_dir="$root_dir/frontend/public/vendor/maplibre-gl"
vendor_work_dir="$staging_dir/vendor-work"

version=""
targets="linux-amd64"
skip_tests=0
clean=0
nuget_audit="${PALPANEL_NUGET_AUDIT:-false}"
dependency_mode="${PALPANEL_DEPENDENCY_MODE:-online}"
vendor_root="${PALPANEL_VENDOR_ROOT:-$root_dir/.vendor}"
vendor_prepared=0
cargo_network_args=()
npm_network_args=()
dotnet_restore_args=()

case "${nuget_audit,,}" in
  true|false) nuget_audit="${nuget_audit,,}" ;;
  *) printf 'PALPANEL_NUGET_AUDIT must be true or false\n' >&2; exit 64 ;;
esac

cleanup_webui_stage() {
  find "$webui_embed_dir" -mindepth 1 ! -name .keep -exec rm -rf -- {} + 2>/dev/null || true
}

cleanup_palops_map_stage() {
  rm -rf -- "$palops_map_stage_dir"
}

cleanup_maplibre_stage() {
  rm -rf -- "$maplibre_stage_dir"
}

cleanup_vendor_stage() {
  if (( vendor_prepared )); then
    python3 "$root_dir/scripts/vendorctl.py" cleanup \
      --repository "$root_dir" \
      --work-root "$vendor_work_dir" >/dev/null 2>&1 || true
  else
    rm -rf -- "$vendor_work_dir"
  fi
}

cleanup_staging_assets() {
  cleanup_webui_stage
  cleanup_palops_map_stage
  cleanup_maplibre_stage
  cleanup_vendor_stage
}
trap cleanup_staging_assets EXIT

setup_dependency_mode() {
  local profile
  case "$dependency_mode" in
    online) return ;;
    mirror) profile=source ;;
    offline) profile=build ;;
    *) printf 'PALPANEL_DEPENDENCY_MODE must be online, mirror, or offline\n' >&2; exit 64 ;;
  esac
  [[ -d "$vendor_root" ]] || { printf 'Vendor root does not exist: %s\n' "$vendor_root" >&2; exit 66; }
  printf '[palpanel] Verifying dependency mirror (%s)\n' "$profile"
  python3 "$root_dir/scripts/vendorctl.py" verify --root "$vendor_root" --profile "$profile"
  python3 "$root_dir/scripts/vendorctl.py" prepare \
    --root "$vendor_root" \
    --repository "$root_dir" \
    --work-root "$vendor_work_dir" \
    --profile "$profile" >/dev/null
  vendor_prepared=1
  eval "$(python3 "$root_dir/scripts/vendorctl.py" env --root "$vendor_root" --work-root "$vendor_work_dir" --format shell)"
  if [[ "$dependency_mode" == offline ]]; then
    export GOPROXY=off GOSUMDB=off CARGO_NET_OFFLINE=true NPM_CONFIG_OFFLINE=true
    export DOTNET_RESTORE_IGNORE_FAILED_SOURCES=true
    cargo_network_args+=(--offline)
    npm_network_args+=(--offline)
    dotnet_restore_args+=('-p:RestoreIgnoreFailedSources=true')
  fi
}

sync_palops_map_assets() {
  local source_dir="${PALPANEL_MAP_ASSETS_SOURCE_DIR:-${PALPANEL_PALOPS_MAP_SOURCE_DIR:-}}"
  local archive="${PALPANEL_MAP_ASSETS_ARCHIVE:-${PALPANEL_PALOPS_MAP_ARCHIVE:-}}"
  local tile_source="${PALPANEL_MAP_ASSETS_TILE_SOURCE_DIR:-${PALPANEL_PALOPS_TILE_SOURCE_DIR:-}}"
  local args=(
    "$root_dir/scripts/sync_palops_map_assets.py"
    --destination "$palops_map_stage_dir"
    --repository "${PALPANEL_MAP_ASSETS_REPOSITORY:-ninhua/palpanel-assets}"
    --ref "${PALPANEL_MAP_ASSETS_REF:-main}"
  )
  if [[ -n "$source_dir" ]]; then
    args+=(--source-dir "$source_dir")
  elif [[ -n "$archive" ]]; then
    args+=(--archive "$archive")
  else
    args+=(--allow-network)
  fi
  if [[ -n "$tile_source" ]]; then
    args+=(--tiles-source-dir "$tile_source")
  fi
  if [[ "${PALPANEL_ALLOW_MISSING_MAP_TILES:-false}" == "true" ]]; then
    args+=(--allow-missing-tiles)
  fi
  printf '[palpanel] Synchronizing PalPanel map assets from %s@%s\n' \
    "${PALPANEL_MAP_ASSETS_REPOSITORY:-ninhua/palpanel-assets}" \
    "${PALPANEL_MAP_ASSETS_REF:-main}"
  python3 "${args[@]}"
}

sync_maplibre_assets() {
  local args=(
    "$root_dir/scripts/sync_maplibre_assets.py"
    --destination "$maplibre_stage_dir"
  )
  if [[ -n "${PALPANEL_MAPLIBRE_SOURCE_DIR:-}" ]]; then
    args+=(--source-dir "$PALPANEL_MAPLIBRE_SOURCE_DIR")
  else
    args+=(--allow-network)
  fi
  printf '[palpanel] Synchronizing pinned MapLibre runtime\n'
  python3 "${args[@]}"
}

usage() {
  printf 'Usage: scripts/package.sh [--version VERSION] [--targets linux-amd64] [--skip-tests] [--clean]\n'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      version="${2:?--version requires a value}"
      shift 2
      ;;
    --version=*) version="${1#*=}"; shift ;;
    --targets)
      targets="${2:?--targets requires a value}"
      shift 2
      ;;
    --targets=*) targets="${1#*=}"; shift ;;
    --skip-tests) skip_tests=1; shift ;;
    --clean) clean=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) printf 'Unknown argument: %s\n' "$1" >&2; usage >&2; exit 64 ;;
  esac
done

if [[ -z "$version" ]]; then
  version="$(git -C "$root_dir" describe --tags --always --dirty 2>/dev/null || date -u +%Y%m%d%H%M%S)"
fi
version="${version//\//-}"
version="${version// /-}"
commit="$(git -C "$root_dir" rev-parse HEAD 2>/dev/null || printf 'unknown')"
build_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if (( clean )); then
  rm -rf "$packages_dir"
fi
mkdir -p "$packages_dir" "$staging_dir"

setup_dependency_mode
sync_palops_map_assets
sync_maplibre_assets

if (( ! skip_tests )); then
  printf '[palpanel] Running backend tests\n'
  (cd "$root_dir/backend" && go test -p=1 ./...)
  printf '[palpanel] Running sav-cli tests with cgo\n'
  (cd "$root_dir/sav-cli" && CGO_ENABLED=1 go test -p=1 ./...)
  printf '[palpanel] Running UID remapper tests\n'
  (cd "$root_dir/tools/palworld-uid-remap" && CARGO_TARGET_DIR="$staging_dir/uid-remapper-tests" cargo test --locked "${cargo_network_args[@]}")
  printf '[palpanel] Installing frontend dependencies\n'
  (cd "$root_dir/frontend" && npm ci "${npm_network_args[@]}")
  printf '[palpanel] Running frontend checks\n'
  (cd "$root_dir/frontend" && npm run check)
else
  printf '[palpanel] Skipping tests\n'
  (cd "$root_dir/frontend" && npm ci "${npm_network_args[@]}" && npm run build)
fi

cleanup_webui_stage
mkdir -p "$webui_embed_dir"
cp -R "$root_dir/frontend/dist/." "$webui_embed_dir/"
cleanup_palops_map_stage
cleanup_maplibre_stage

copy_common_files() {
  local package_dir="$1"
  local gooz_dir
  (cd "$root_dir/sav-cli" && go mod download github.com/oriath-net/gooz)
  gooz_dir="$(cd "$root_dir/sav-cli" && go list -m -f '{{.Dir}}' github.com/oriath-net/gooz)"
  [[ -n "$gooz_dir" && -f "$gooz_dir/COPYING" ]] || {
    printf 'Unable to locate downloaded gooz license source\n' >&2
    exit 69
  }
  mkdir -p "$package_dir/bin" "$package_dir/config" "$package_dir/backend/deployments" "$package_dir/systemd" "$package_dir/licenses"
  cp -R "$root_dir/backend/deployments/wine-runner" "$package_dir/backend/deployments/wine-runner"
  cp "$root_dir/scripts/palpanel.env.example" "$package_dir/config/palpanel.env.example"
  cp "$root_dir/scripts/package-README.md" "$package_dir/README.md"
  cp "$root_dir/scripts/palpanelctl" "$package_dir/palpanelctl"
  cp "$root_dir/scripts/systemd/palpanel.service" "$package_dir/systemd/palpanel.service"
  cp "$root_dir/scripts/systemd/palpanel-sav-cli.service" "$package_dir/systemd/palpanel-sav-cli.service"
  cp "$root_dir/scripts/systemd/palpanel-palcalc.service" "$package_dir/systemd/palpanel-palcalc.service"
  cp "$root_dir/scripts/systemd/palpanel-update.service" "$package_dir/systemd/palpanel-update.service"
  cp "$root_dir/scripts/systemd/palpanel-update.path" "$package_dir/systemd/palpanel-update.path"
  cp "$root_dir/LICENSE" "$package_dir/LICENSE"
  cp "$root_dir/THIRD_PARTY_LICENSES.txt" "$package_dir/THIRD_PARTY_LICENSES.txt"
  cp "$root_dir/sav-cli/LICENSE" "$package_dir/licenses/sav-cli-LICENSE.txt"
  cp "$root_dir/third_party/palcalc/LICENSE.txt" "$package_dir/licenses/PalCalc-MIT.txt"
  cp "$gooz_dir/COPYING" "$package_dir/licenses/GPL-3.0.txt"
  cp "$root_dir/backend/internal/pallocalize/LICENSE.apache-2.0" "$package_dir/licenses/pallocalize-Apache-2.0.txt"
  cp "$root_dir/backend/internal/paldefender/assets/LICENSE.txt" "$package_dir/licenses/PalDefender-MIT.txt"
  chmod 755 "$package_dir/palpanelctl"
}

build_linux() {
  local arch="$1"
  [[ "$arch" == "amd64" ]] || { printf 'Only linux-amd64 is supported\n' >&2; exit 64; }
  [[ "$(go env GOOS)" == "linux" && "$(go env GOARCH)" == "$arch" ]] || {
    printf 'The cgo sav-cli release must be built natively on linux-%s\n' "$arch" >&2
    exit 69
  }
  local package_name="palpanel_${version}_linux_${arch}"
  local package_dir="$staging_dir/$package_name"
  local archive="$packages_dir/$package_name.tar.gz"
  local checksum_tmp="$staging_dir/.${package_name}.checksums"
  rm -rf "$package_dir" "$archive"
  copy_common_files "$package_dir"

  printf '[palpanel] Building UID remapper linux-%s\n' "$arch"
  (cd "$root_dir/tools/palworld-uid-remap" && CARGO_TARGET_DIR="$staging_dir/uid-remapper-linux-$arch" cargo build --locked --release "${cargo_network_args[@]}")
  cp "$staging_dir/uid-remapper-linux-$arch/release/palworld-uid-remap" "$package_dir/bin/palworld-uid-remap"
  local helper_sha256
  helper_sha256="$(sha256sum "$package_dir/bin/palworld-uid-remap" | cut -d ' ' -f 1)"
  [[ "$helper_sha256" =~ ^[0-9a-f]{64}$ ]] || {
    printf 'Unable to calculate UID remapper SHA-256\n' >&2
    exit 69
  }
  local backend_ldflags="-s -w -X palpanel/internal/buildinfo.Version=$version -X palpanel/internal/buildinfo.Commit=$commit -X palpanel/internal/buildinfo.BuildTime=$build_time"
  local palpanel_ldflags="$backend_ldflags -X palpanel/internal/api.hostMigrationHelperSHA256=$helper_sha256"
  local sav_ldflags="-s -w -X palpanel/sav-cli/internal/buildinfo.Version=$version -X palpanel/sav-cli/internal/buildinfo.Commit=$commit -X palpanel/sav-cli/internal/buildinfo.BuildTime=$build_time"
  printf '[palpanel] Building backend linux-%s\n' "$arch"
  (cd "$root_dir/backend" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -tags embed_webui -trimpath -ldflags "$palpanel_ldflags" -o "$package_dir/bin/palpanel" ./cmd/palpanel)
  printf '[palpanel] Building external updater linux-%s\n' "$arch"
  (cd "$root_dir/backend" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "$backend_ldflags" -o "$package_dir/bin/palpanel-updater" ./cmd/palpanel-updater)
  printf '[palpanel] Building cgo sav-cli linux-%s\n' "$arch"
  (cd "$root_dir/sav-cli" && CGO_ENABLED=1 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "$sav_ldflags" -o "$package_dir/bin/sav-cli" ./cmd/sav_cli)
  printf '[palpanel] Publishing self-contained PalCalc bridge linux-%s\n' "$arch"
  # Local release packaging must remain deterministic when NuGet's advisory
  # endpoint is unavailable. GitHub CI explicitly enables the online audit.
  DOTNET_CLI_UI_LANGUAGE=en dotnet publish "$root_dir/palcalc-bridge/PalCalc.Bridge.csproj" -c Release -r linux-x64 --self-contained true -p:PublishSingleFile=true -p:IncludeNativeLibrariesForSelfExtract=true -p:InvariantGlobalization=true "-p:NuGetAudit=$nuget_audit" "${dotnet_restore_args[@]}" -o "$staging_dir/palcalc-linux"
  cp "$staging_dir/palcalc-linux/palcalc-bridge" "$package_dir/bin/palcalc-bridge"
  chmod 755 "$package_dir/bin/palpanel" "$package_dir/bin/palpanel-updater" "$package_dir/bin/sav-cli" "$package_dir/bin/palcalc-bridge" "$package_dir/bin/palworld-uid-remap"

  cat >"$package_dir/panel-update.json" <<EOF_PANEL_UPDATE
{
  "schema_version": 1,
  "version": "$version",
  "exec_hot_update": {
    "supported": true,
    "required_files": ["bin/palpanel"],
    "health_paths": ["/api/ready", "/api/patch/info"],
    "success_threshold": 3
  }
}
EOF_PANEL_UPDATE

  (cd "$package_dir" && find . -type f ! -name checksums.txt -print0 | sort -z | xargs -0 sha256sum) >"$checksum_tmp"
  mv "$checksum_tmp" "$package_dir/checksums.txt"
  tar --sort=name --owner=0 --group=0 --numeric-owner -czf "$archive" -C "$staging_dir" "$package_name"
  printf '[palpanel] Wrote %s\n' "$archive"
}

build_source_archive() {
  local source_name="palpanel-sav-cli_${version}_source"
  local source_root="$staging_dir/$source_name"
  local archive="$packages_dir/$source_name.tar.gz"
  rm -rf "$source_root" "$archive"
  mkdir -p "$source_root"
  (
    cd "$root_dir"
    while IFS= read -r -d '' path; do
      cp -a --parents "$path" "$source_root/"
    done < <(git ls-files --cached --others --exclude-standard -z -- sav-cli)
  )
  cp "$root_dir/LICENSE" "$source_root/LICENSE"
  cp "$root_dir/THIRD_PARTY_LICENSES.txt" "$source_root/THIRD_PARTY_LICENSES.txt"
  (cd "$source_root/sav-cli" && go mod vendor)
  tar --sort=name --owner=0 --group=0 --numeric-owner -czf "$archive" -C "$staging_dir" "$source_name"
  printf '[palpanel] Wrote %s\n' "$archive"
}

build_project_source_archive() {
  local source_name="palpanel_${version}_source"
  local source_root="$staging_dir/$source_name"
  local archive="$packages_dir/$source_name.tar.gz"
  rm -rf "$source_root" "$archive"
  mkdir -p "$source_root"
  (
    cd "$root_dir"
    while IFS= read -r -d '' path; do
      [[ "$path" == "third_party/palcalc" ]] && continue
      [[ "$path" == tools/palworld-uid-remap/target/* ]] && continue
      cp -a --parents "$path" "$source_root/"
    done < <(git ls-files --cached --others --exclude-standard -z)
  )
  for vendor_source in palcalc uesave; do
    local vendor_source_root="$root_dir/third_party/$vendor_source"
    [[ -d "$vendor_source_root" ]] || continue
    rm -rf "$source_root/third_party/$vendor_source"
    mkdir -p "$source_root/third_party/$vendor_source"
    (
      cd "$vendor_source_root"
      if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
        file_stream=(git ls-files --cached -z)
      else
        file_stream=(find . -type f -print0)
      fi
      while IFS= read -r -d '' path; do
        path="${path#./}"
        case "$path" in
          .git/*|*.dll|bin/*|*/bin/*|obj/*|*/obj/*|target/*|*/target/*) continue ;;
        esac
        mkdir -p "$source_root/third_party/$vendor_source/$(dirname "$path")"
        cp -a "$path" "$source_root/third_party/$vendor_source/$path"
      done < <("${file_stream[@]}")
    )
  done
  (cd "$source_root/sav-cli" && go mod vendor)
  tar --sort=name --owner=0 --group=0 --numeric-owner -czf "$archive" -C "$staging_dir" "$source_name"
  printf '[palpanel] Wrote %s\n' "$archive"
}

IFS=',' read -r -a target_list <<<"$targets"
for target in "${target_list[@]}"; do
  target="${target//[[:space:]]/}"
  case "$target" in
    linux-amd64) build_linux amd64 ;;
    "") ;;
    *) printf 'Unsupported release target: %s\n' "$target" >&2; exit 64 ;;
  esac
done
cleanup_webui_stage
build_source_archive
build_project_source_archive
rm -rf "$staging_dir"

cp "$root_dir/THIRD_PARTY_LICENSES.txt" "$packages_dir/THIRD_PARTY_LICENSES.txt"
(
  cd "$packages_dir"
  find . -maxdepth 1 -type f \( -name '*.tar.gz' -o -name '*.spdx.json' -o -name 'THIRD_PARTY_LICENSES.txt' \) -printf '%f\0' |
    sort -z | xargs -0 sha256sum >SHA256SUMS
)
printf '[palpanel] Package build completed successfully for %s\n' "$version"
