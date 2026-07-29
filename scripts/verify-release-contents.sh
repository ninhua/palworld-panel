#!/usr/bin/env bash
set -euo pipefail

archive="${1:?usage: verify-release-contents.sh <linux-archive> [sav-source-archive] [project-source-archive]}"
source_archive="${2:-}"
project_source_archive="${3:-}"
[[ -f "$archive" ]] || { printf 'archive not found: %s\n' "$archive" >&2; exit 1; }

listing="$(tar -tzf "$archive")"
if grep -E '(^|/)(data|logs|run)/|(^|/)palpanel\.env$|(^|/)\.env($|\.)|\.db$|\.sqlite$|\.log$' <<<"$listing"; then
  printf 'release archive contains runtime data, secrets, database, or logs\n' >&2
  exit 1
fi

required=(
  '/bin/palpanel'
  '/bin/palpanel-updater'
  '/bin/sav-cli'
  '/bin/palcalc-bridge'
  '/bin/palworld-uid-remap'
  '/palpanelctl'
  '/config/palpanel.env.example'
  '/systemd/palpanel.service'
  '/systemd/palpanel-sav-cli.service'
  '/systemd/palpanel-palcalc.service'
  '/systemd/palpanel-update.service'
  '/systemd/palpanel-update.path'
  '/LICENSE'
  '/THIRD_PARTY_LICENSES.txt'
  '/licenses/GPL-3.0.txt'
  '/licenses/PalDefender-MIT.txt'
  '/licenses/PalCalc-MIT.txt'
  '/panel-update.json'
  '/checksums.txt'
)
for item in "${required[@]}"; do
  grep -Fq "$item" <<<"$listing" || { printf 'release archive is missing %s\n' "$item" >&2; exit 1; }
done
if grep -Eq '(^|/)frontend/' <<<"$listing"; then
  printf 'release archive contains a separate frontend directory\n' >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
tar -xzf "$archive" -C "$tmp"
package_dir="$(find "$tmp" -mindepth 1 -maxdepth 1 -type d -print -quit)"
(cd "$package_dir" && sha256sum -c checksums.txt >/dev/null)
helper_sha256="$(awk '$2 == "./bin/palworld-uid-remap" { print $1 }' "$package_dir/checksums.txt")"
[[ "$helper_sha256" =~ ^[0-9a-f]{64}$ ]] || {
  printf 'release checksums do not contain a valid UID remapper SHA-256\n' >&2
  exit 1
}
grep -aFq "$helper_sha256" "$package_dir/bin/palpanel" || {
  printf 'panel binary does not embed the packaged UID remapper SHA-256\n' >&2
  exit 1
}
python3 - "$package_dir/panel-update.json" "$(basename "$package_dir" | sed -E 's/^palpanel_(.*)_linux_amd64$/\1/')" <<'PY_PANEL_UPDATE_MANIFEST'
from pathlib import Path
import json
import sys

path = Path(sys.argv[1])
expected_version = sys.argv[2]
payload = json.loads(path.read_text(encoding="utf-8"))
assert payload.get("schema_version") == 1, "invalid panel-update.json schema"
assert payload.get("version") == expected_version, "panel-update.json version mismatch"
exec_update = payload.get("exec_hot_update") or {}
assert exec_update.get("supported") is True, "exec hot update is not enabled"
assert exec_update.get("required_files") == ["bin/palpanel"], "exec hot update scope is not main-binary-only"
assert exec_update.get("health_paths") == ["/api/ready", "/api/patch/info"], "exec health paths mismatch"
assert int(exec_update.get("success_threshold", 0)) == 3, "exec success threshold mismatch"
PY_PANEL_UPDATE_MANIFEST
if grep -RInI -E 'STEAM_WEB_API_KEY[[:space:]]*=[[:space:]]*[A-Za-z0-9_+/=-]{20,}' "$package_dir" --exclude='checksums.txt'; then
  printf 'release archive contains a configured secret\n' >&2
  exit 1
fi

if [[ -n "$source_archive" ]]; then
  [[ -f "$source_archive" ]] || { printf 'source archive not found: %s\n' "$source_archive" >&2; exit 1; }
  source_listing="$(tar -tzf "$source_archive")"
  if grep -E '/sav-cli/(data|logs|run|dist)/|/sav-cli/\.env($|\.)|\.(db|sqlite|log|sav|zip|exe|dll|o|a)$' <<<"$source_listing"; then
    printf 'source archive contains runtime data, secrets, database, logs, or build artifacts\n' >&2
    exit 1
  fi
  grep -Fq '/LICENSE' <<<"$source_listing" || { printf 'source archive is missing project LICENSE\n' >&2; exit 1; }
  grep -Fq '/THIRD_PARTY_LICENSES.txt' <<<"$source_listing" || { printf 'source archive is missing third-party inventory\n' >&2; exit 1; }
  grep -Fq '/sav-cli/LICENSE' <<<"$source_listing" || { printf 'source archive is missing sav-cli/LICENSE\n' >&2; exit 1; }
  grep -Fq '/sav-cli/vendor/github.com/oriath-net/gooz/COPYING' <<<"$source_listing" || {
    printf 'source archive is missing vendored gooz license\n' >&2
    exit 1
  }
  grep -Fq '/sav-cli/vendor/github.com/oriath-net/gooz/kraken.cpp' <<<"$source_listing" || {
    printf 'source archive is missing vendored gooz source\n' >&2
    exit 1
  }
fi

if [[ -n "$project_source_archive" ]]; then
  [[ -f "$project_source_archive" ]] || { printf 'project source archive not found: %s\n' "$project_source_archive" >&2; exit 1; }
  project_listing="$(tar -tzf "$project_source_archive")"
  first_party_listing="$(grep -Ev '/third_party/' <<<"$project_listing" || true)"
  if grep -E '/(data|logs|run|dist|node_modules)/|/\.env$|/\.env\..*\.local$|\.(db|sqlite|log|sav|zip|exe|o|a)$' <<<"$first_party_listing"; then
    printf 'project source archive contains runtime data, secrets, dependencies, or build artifacts\n' >&2
    exit 1
  fi
  third_party_forbidden="$(grep -E '/third_party/.*/(bin|obj|logs|run|dist|node_modules)/|/third_party/.*/\.env($|\.)|\.(db|sqlite|log|zip|exe|o|a)$' <<<"$project_listing" || true)"
  if [[ -n "$third_party_forbidden" ]]; then
    printf '%s\nproject source archive contains forbidden third-party build or runtime artifacts\n' "$third_party_forbidden" >&2
    exit 1
  fi
  unexpected_dlls="$(grep -Ei '\.dll$' <<<"$project_listing" || true)"
  if [[ -n "$unexpected_dlls" ]]; then
    printf '%s\nproject source archive contains an unexpected DLL\n' "$unexpected_dlls" >&2
    exit 1
  fi
  for item in '/LICENSE' '/backend/go.mod' '/frontend/package.json' '/sav-cli/go.mod' '/scripts/package.ps1' '/sav-cli/vendor/github.com/oriath-net/gooz/kraken.cpp' '/backend/internal/paldefender/assets/LICENSE.txt'; do
    grep -Fq "$item" <<<"$project_listing" || { printf 'project source archive is missing %s\n' "$item" >&2; exit 1; }
  done
fi
printf 'release content verification passed\n'
