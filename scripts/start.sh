#!/usr/bin/env bash
set -euo pipefail

export TZ="${TZ:-Asia/Shanghai}"

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
if [[ -x "$script_dir/../palpanelctl" ]]; then
  exec "$script_dir/../palpanelctl" run
fi
exec "$script_dir/palpanelctl" run
