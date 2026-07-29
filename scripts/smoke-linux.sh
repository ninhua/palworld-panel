#!/usr/bin/env bash
set -euo pipefail

archive="${1:?usage: smoke-linux.sh <linux-archive>}"
archive_name="$(basename -- "$archive")"
[[ "$archive_name" =~ ^palpanel_(v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?)_linux_amd64\.tar\.gz$ ]] || {
  printf 'unexpected archive name: %s\n' "$archive_name" >&2
  exit 1
}
version="${BASH_REMATCH[1]}"
tmp="$(mktemp -d)"
package_dir=""
cleanup() {
  if [[ -n "$package_dir" && -x "$package_dir/palpanelctl" ]]; then
    PALPANEL_LISTEN_ADDR="127.0.0.1:${panel_port:-18080}" PALPANEL_SAVE_INDEXER_PORT="${sav_port:-18090}" \
      "$package_dir/palpanelctl" stop >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

tar -xzf "$archive" -C "$tmp"
package_dir="$(find "$tmp" -mindepth 1 -maxdepth 1 -type d -print -quit)"
[[ -n "$package_dir" ]] || { printf 'package root not found\n' >&2; exit 1; }
(cd "$package_dir" && sha256sum -c checksums.txt >/dev/null)

panel_port="$((20000 + RANDOM % 15000))"
sav_port="$((35001 + RANDOM % 15000))"
palcalc_port="$((50002 + RANDOM % 10000))"
export PALPANEL_LISTEN_ADDR="127.0.0.1:$panel_port"
export PALPANEL_SAVE_INDEXER_PORT="$sav_port"
export PALPANEL_PALCALC_PORT="$palcalc_port"

"$package_dir/bin/palpanel" --version | grep -F "$version"
"$package_dir/bin/palpanel-updater" --version | grep -F "$version"
"$package_dir/bin/sav-cli" --version | grep -F "$version"
"$package_dir/palpanelctl" init >"$tmp/init.txt"
[[ "$(stat -c '%a' "$package_dir/config/palpanel.env")" == "600" ]]
grep -Eq '^PALWORLD_ADMIN_PASSWORD=[A-Za-z0-9_-]{40,}$' "$package_dir/config/palpanel.env"

"$package_dir/palpanelctl" start
"$package_dir/palpanelctl" status
curl --fail --silent "http://127.0.0.1:$panel_port/api/health" | grep -F "\"version\":\"$version\""
curl --fail --silent "http://127.0.0.1:$sav_port/health" | grep -F "\"build_version\":\"$version\""
curl --fail --silent "http://127.0.0.1:$palcalc_port/health" | grep -F '"upstream_version":"v1.17.6"'
curl --fail --silent --header 'Content-Type: application/json' \
  --data '{"request_id":"smoke-palcalc","save_fingerprint":"smoke","owned_pals":[{"instance_id":"smoke-anubis","pal_id":"Anubis","nickname":"Smoke Anubis","level":50,"owner_player_id":"smoke","gender":"male","passives":[],"rank":1,"iv_hp":0,"iv_attack":0,"iv_defense":0,"container_id":"smoke-box","slot_index":0,"location_type":"palbox"}],"target":{"pal_id":"Anubis","gender":"wildcard","required_passives":[],"optional_passives":[],"iv_hp":0,"iv_attack":0,"iv_defense":0},"settings":{"max_breeding_steps":1,"max_solver_iterations":1,"max_wild_pals":0,"max_input_irrelevant_passives":0,"max_bred_irrelevant_passives":0,"max_threads":1,"max_gold_cost":0,"use_gender_reversers":false},"game_settings":{"breeding_time_seconds":1,"massive_egg_incubation_minutes":1,"multiple_breeding_farms":true,"multiple_incubators":true},"result_limit":1}' \
  "http://127.0.0.1:$palcalc_port/v1/jobs" | grep -F '"id":"smoke-palcalc"'
palcalc_completed=0
for _ in $(seq 1 30); do
  palcalc_job="$(curl --fail --silent "http://127.0.0.1:$palcalc_port/v1/jobs/smoke-palcalc")"
  if grep -Fq '"status":"completed"' <<<"$palcalc_job"; then
    palcalc_completed=1
    break
  fi
  if grep -Eq '"status":"(failed|canceled)"' <<<"$palcalc_job"; then
    printf 'PalCalc smoke solve failed: %s\n' "$palcalc_job" >&2
    exit 1
  fi
  sleep 1
done
[[ "$palcalc_completed" == "1" ]] || { printf 'PalCalc smoke solve timed out\n' >&2; exit 1; }
curl --fail --silent "http://127.0.0.1:$palcalc_port/v1/jobs/smoke-palcalc/result" | grep -F '"pal_id":"Anubis"'
unauthorized="$(curl --silent --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:$panel_port/api/auth/me")"
[[ "$unauthorized" == "401" ]]
curl --fail --silent --cookie-jar "$tmp/cookies.txt" \
  -H 'Content-Type: application/json' \
  -H "Origin: http://127.0.0.1:$panel_port" \
  --data '{"username":"smoke-admin","password":"smoke-password-123"}' \
  "http://127.0.0.1:$panel_port/api/auth/register" | grep -F '"role":"admin"'
curl --fail --silent --cookie "$tmp/cookies.txt" "http://127.0.0.1:$panel_port/api/auth/me" | grep -F '"role":"admin"'
"$package_dir/palpanelctl" logs >/dev/null
"$package_dir/palpanelctl" restart
curl --fail --silent "http://127.0.0.1:$panel_port/api/health" >/dev/null
curl --fail --silent --cookie "$tmp/cookies.txt" "http://127.0.0.1:$panel_port/api/auth/me" | grep -F '"role":"admin"'
"$package_dir/palpanelctl" stop

[[ ! -e "$package_dir/run/backend.pid" && ! -e "$package_dir/run/sav-cli.pid" && ! -e "$package_dir/run/palcalc-bridge.pid" ]]
if pgrep -f "$package_dir/bin/(palpanel|sav-cli|palcalc-bridge)" >/dev/null 2>&1; then
  printf 'orphan process remains after stop\n' >&2
  exit 1
fi
printf 'linux package smoke test passed\n'
