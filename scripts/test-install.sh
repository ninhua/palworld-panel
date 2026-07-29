#!/usr/bin/env bash
set -euo pipefail

archive="${1:?usage: test-install.sh <linux-archive>}"
tmp="$(mktemp -d)"
systemctl_log="$tmp/systemctl.log"
fake_bin="$tmp/fake-bin"
if [[ "$(id -u)" -eq 0 ]]; then
  service_user="palpanel-ci-$RANDOM"
else
  service_user="$(id -un)"
  export PALPANEL_TEST_MODE=1
fi
cleanup() {
  if [[ "$(id -u)" -eq 0 ]]; then
    userdel "$service_user" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

mkdir -p "$fake_bin"
cat >"$fake_bin/systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${PALPANEL_SYSTEMCTL_LOG:?}"
EOF
chmod +x "$fake_bin/systemctl"
export PALPANEL_SYSTEMCTL_LOG="$systemctl_log"
export PATH="$fake_bin:$PATH"

mkdir -p "$tmp/package"
tar -xzf "$archive" -C "$tmp/package"
package_dir="$(find "$tmp/package" -mindepth 1 -maxdepth 1 -type d -print -quit)"
ctl="$package_dir/palpanelctl"

portable_test="$tmp/portable-test"
mkdir -p "$portable_test/bin"
cp "$ctl" "$portable_test/palpanelctl"
cat >"$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${PALPANEL_FAKE_PREEXISTING_HEALTH:-0}" == "1" ]]; then
  exit 0
fi
events="${PALPANEL_FAKE_EVENTS:-}"
[[ -n "$events" && -f "$events" ]] || exit 1
url="${*: -1}"
case "$url" in
  *:8090/health) grep -qx 'sav-cli-start' "$events" ;;
  *:8091/health) grep -qx 'palcalc-start' "$events" ;;
  *:18080/api/health) grep -qx 'backend-start' "$events" ;;
  *) exit 1 ;;
esac
EOF
cat >"$portable_test/bin/palpanel" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
config=""
initialize=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --config)
      config="$2"
      shift
      ;;
    --init-config) initialize=1 ;;
  esac
  shift
done
if (( initialize )); then
  mkdir -p "$(dirname -- "$config")"
  printf 'PALPANEL_LISTEN_ADDR=127.0.0.1:18080\n' >"$config"
  exit 0
fi
printf 'backend-start\n' >>"${PALPANEL_FAKE_EVENTS:?}"
if [[ "${PALPANEL_FAKE_EXIT_CHILD:-}" == "backend" ]]; then
  sleep 1
  printf 'backend-exit\n' >>"${PALPANEL_FAKE_EVENTS:?}"
  exit 19
fi
trap 'printf "backend-stop\n" >>"${PALPANEL_FAKE_EVENTS:?}"; exit 0' TERM INT HUP
while :; do sleep 1; done
EOF
cat >"$portable_test/bin/sav-cli" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'sav-cli-start\n' >>"${PALPANEL_FAKE_EVENTS:?}"
if [[ "${PALPANEL_FAKE_EXIT_CHILD:-}" == "sav-cli" ]]; then
  sleep 1
  printf 'sav-cli-exit\n' >>"${PALPANEL_FAKE_EVENTS:?}"
  exit 17
fi
trap 'printf "sav-cli-stop\n" >>"${PALPANEL_FAKE_EVENTS:?}"; exit 0' TERM INT HUP
while :; do sleep 1; done
EOF
cat >"$portable_test/bin/palcalc-bridge" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'palcalc-start\n' >>"${PALPANEL_FAKE_EVENTS:?}"
if [[ "${PALPANEL_FAKE_EXIT_CHILD:-}" == "palcalc" ]]; then
  sleep 1
  printf 'palcalc-exit\n' >>"${PALPANEL_FAKE_EVENTS:?}"
  exit 18
fi
trap 'printf "palcalc-stop\n" >>"${PALPANEL_FAKE_EVENTS:?}"; exit 0' TERM INT HUP
while :; do sleep 1; done
EOF
chmod +x "$fake_bin/curl" "$portable_test/bin/palpanel" "$portable_test/bin/sav-cli" "$portable_test/bin/palcalc-bridge"
verify_portable_failure() {
  local exiting="$1"
  local events="$tmp/portable-$exiting-events.log"
  rm -rf "$portable_test/config" "$portable_test/data" "$portable_test/run" "$portable_test/logs"
  if PALPANEL_FAKE_EVENTS="$events" \
    PALPANEL_FAKE_EXIT_CHILD="$exiting" \
    PALPANEL_CONFIG="$portable_test/config/palpanel.env" \
    PALPANEL_DATA_DIR="$portable_test/data" \
    PALPANEL_RUNTIME_DIR="$portable_test/run" \
    PALPANEL_LOG_DIR="$portable_test/logs" \
    "$portable_test/palpanelctl" start >"$tmp/portable-$exiting.out" 2>"$tmp/portable-$exiting.err"; then
    printf 'portable startup accepted an unexpected %s exit\n' "$exiting" >&2
    exit 1
  fi
  if grep -Fq 'PalPanel started:' "$tmp/portable-$exiting.out"; then
    printf 'portable startup reported success after an unexpected %s exit\n' "$exiting" >&2
    exit 1
  fi
  grep -qx 'sav-cli-start' "$events"
  if [[ "$exiting" != "sav-cli" ]]; then grep -qx 'palcalc-start' "$events"; fi
  if [[ "$exiting" == "sav-cli" ]]; then
    grep -qx 'sav-cli-exit' "$events"
    if grep -qx 'backend-start' "$events"; then
      printf 'backend started after the sav-cli fixture had already exited\n' >&2
      exit 1
    fi
  elif [[ "$exiting" == "palcalc" ]]; then
    grep -qx 'palcalc-exit' "$events"
    if grep -qx 'backend-start' "$events"; then
      printf 'backend started after the palcalc fixture had already exited\n' >&2
      exit 1
    fi
    grep -qx 'sav-cli-stop' "$events"
  else
    grep -qx 'backend-start' "$events"
    grep -qx 'backend-exit' "$events"
    grep -qx 'sav-cli-stop' "$events"
    grep -qx 'palcalc-stop' "$events"
  fi
  [[ ! -e "$portable_test/run/backend.pid" && ! -e "$portable_test/run/sav-cli.pid" && ! -e "$portable_test/run/palcalc-bridge.pid" ]]
  [[ ! -e "$portable_test/run/supervisor.pid" && ! -e "$portable_test/run/ready" ]]
}
verify_portable_failure sav-cli
verify_portable_failure palcalc
verify_portable_failure backend

rm -rf "$portable_test/config" "$portable_test/data" "$portable_test/run" "$portable_test/logs"
preexisting_out="$tmp/portable-preexisting.out"
preexisting_err="$tmp/portable-preexisting.err"
if PALPANEL_FAKE_PREEXISTING_HEALTH=1 \
  PALPANEL_CONFIG="$portable_test/config/palpanel.env" \
  PALPANEL_DATA_DIR="$portable_test/data" \
  PALPANEL_RUNTIME_DIR="$portable_test/run" \
  PALPANEL_LOG_DIR="$portable_test/logs" \
  "$portable_test/palpanelctl" start >"$preexisting_out" 2>"$preexisting_err"; then
  printf 'portable startup accepted a pre-existing health endpoint\n' >&2
  exit 1
fi
if grep -Fq 'PalPanel started:' "$preexisting_out"; then
  printf 'portable startup reported success while the health endpoint was already occupied\n' >&2
  exit 1
fi
grep -Fq 'health endpoint is already responding before startup' "$preexisting_err"
[[ ! -e "$portable_test/run/supervisor.pid" && ! -e "$portable_test/run/ready" ]]

export PALPANEL_INSTALL_ROOT="$tmp/opt/palpanel"
export PALPANEL_ETC_DIR="$tmp/etc/palpanel"
export PALPANEL_SYSTEM_DATA_DIR="$tmp/var/lib/palpanel"
export PALPANEL_SYSTEMD_DIR="$tmp/systemd"
export PALPANEL_LIBEXEC_DIR="$tmp/libexec"
export PALPANEL_SERVICE_USER="$service_user"
export PALPANEL_SKIP_SYSTEMD=0

"$ctl" install >/dev/null
[[ -L "$PALPANEL_INSTALL_ROOT/current" ]]
[[ "$(stat -c '%a' "$PALPANEL_INSTALL_ROOT")" == "755" ]]
[[ "$(stat -c '%a' "$(readlink -f "$PALPANEL_INSTALL_ROOT/current")")" == "755" ]]
[[ "$(stat -c '%a' "$PALPANEL_ETC_DIR")" == "750" ]]
[[ "$(stat -c '%a' "$PALPANEL_ETC_DIR/palpanel.env")" == "600" ]]
[[ -d "$PALPANEL_SYSTEM_DATA_DIR/docker-client" ]]
[[ -d "$PALPANEL_SYSTEM_DATA_DIR/panel-update/operations" ]]
[[ "$(stat -c '%a' "$PALPANEL_SYSTEM_DATA_DIR/docker-client")" == "700" ]]
[[ "$(stat -c '%a' "$PALPANEL_SYSTEM_DATA_DIR/panel-update")" == "700" ]]
if [[ "$(id -u)" -eq 0 ]]; then
  [[ "$(stat -c '%U:%G' "$PALPANEL_SYSTEM_DATA_DIR/docker-client")" == "$service_user:$service_user" ]]
fi
grep -Eq '^PALWORLD_ADMIN_PASSWORD=[A-Za-z0-9_-]{40,}$' "$PALPANEL_ETC_DIR/palpanel.env"
installed_dir="$(readlink -f "$PALPANEL_INSTALL_ROOT/current")"
[[ "$(stat -c '%a' "$installed_dir/bin")" == "755" ]]
if [[ "$(id -u)" -eq 0 ]]; then
  [[ "$(stat -c '%U:%G' "$installed_dir/bin")" == "root:root" ]]
fi
[[ -x "$PALPANEL_LIBEXEC_DIR/palpanel-updater" ]]
[[ -f "$installed_dir/LICENSE" ]]
[[ -f "$installed_dir/licenses/GPL-3.0.txt" ]]
[[ -f "$installed_dir/licenses/sav-cli-LICENSE.txt" ]]
[[ -f "$installed_dir/licenses/PalCalc-MIT.txt" ]]
[[ -f "$installed_dir/THIRD_PARTY_LICENSES.txt" ]]
[[ "$("$installed_dir/palpanelctl" config)" == "$PALPANEL_ETC_DIR/palpanel.env" ]]
grep -qx 'Wants=palpanel-sav-cli.service palpanel-palcalc.service' "$PALPANEL_SYSTEMD_DIR/palpanel.service"
grep -Fxq "Environment=HOME=$PALPANEL_SYSTEM_DATA_DIR" "$PALPANEL_SYSTEMD_DIR/palpanel.service"
grep -Fxq "Environment=DOCKER_CONFIG=$PALPANEL_SYSTEM_DATA_DIR/docker-client" "$PALPANEL_SYSTEMD_DIR/palpanel.service"
grep -Fxq 'ProtectHome=true' "$PALPANEL_SYSTEMD_DIR/palpanel.service"
grep -Fxq "ReadWritePaths=$PALPANEL_SYSTEM_DATA_DIR" "$PALPANEL_SYSTEMD_DIR/palpanel.service"
grep -qx 'PartOf=palpanel.service' "$PALPANEL_SYSTEMD_DIR/palpanel-sav-cli.service"
grep -qx 'Restart=always' "$PALPANEL_SYSTEMD_DIR/palpanel-sav-cli.service"
grep -qx 'PartOf=palpanel.service' "$PALPANEL_SYSTEMD_DIR/palpanel-palcalc.service"
grep -qx 'Restart=always' "$PALPANEL_SYSTEMD_DIR/palpanel-palcalc.service"
grep -Fxq "ExecStart=$PALPANEL_LIBEXEC_DIR/palpanel-updater --request $PALPANEL_SYSTEM_DATA_DIR/panel-update/request.json" "$PALPANEL_SYSTEMD_DIR/palpanel-update.service"
grep -Fxq "PathExists=$PALPANEL_SYSTEM_DATA_DIR/panel-update/request.json" "$PALPANEL_SYSTEMD_DIR/palpanel-update.path"
grep -Fxq "PathExists=$PALPANEL_SYSTEM_DATA_DIR/panel-update/request.in-progress.json" "$PALPANEL_SYSTEMD_DIR/palpanel-update.path"
grep -Fxq "ReadWritePaths=$PALPANEL_INSTALL_ROOT $PALPANEL_SYSTEMD_DIR $PALPANEL_LIBEXEC_DIR $PALPANEL_SYSTEM_DATA_DIR" "$PALPANEL_SYSTEMD_DIR/palpanel-update.service"
grep -Fxq 'enable palpanel-update.path palpanel-sav-cli.service palpanel-palcalc.service palpanel.service' "$systemctl_log"
grep -Fxq 'start palpanel-update.path' "$systemctl_log"
grep -Fxq 'restart palpanel-sav-cli.service palpanel-palcalc.service palpanel.service' "$systemctl_log"
"$installed_dir/palpanelctl" start
grep -Fxq 'start palpanel-sav-cli.service palpanel-palcalc.service palpanel.service' "$systemctl_log"
printf '# preserve-config\n' >>"$PALPANEL_ETC_DIR/palpanel.env"
printf 'preserve-data\n' >"$PALPANEL_SYSTEM_DATA_DIR/preserve.marker"
printf 'preserve-docker-client\n' >"$PALPANEL_SYSTEM_DATA_DIR/docker-client/config.json"
chmod 755 "$PALPANEL_SYSTEM_DATA_DIR/docker-client"
if [[ "$(id -u)" -eq 0 ]]; then
  chown root:root "$PALPANEL_SYSTEM_DATA_DIR/docker-client"
fi
config_hash="$(sha256sum "$PALPANEL_ETC_DIR/palpanel.env" | awk '{print $1}')"
docker_client_hash="$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/docker-client/config.json" | awk '{print $1}')"

"$ctl" install >/dev/null
[[ "$(sha256sum "$PALPANEL_ETC_DIR/palpanel.env" | awk '{print $1}')" == "$config_hash" ]]
[[ -f "$PALPANEL_SYSTEM_DATA_DIR/preserve.marker" ]]
[[ "$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/docker-client/config.json" | awk '{print $1}')" == "$docker_client_hash" ]]
[[ "$(stat -c '%a' "$PALPANEL_SYSTEM_DATA_DIR/docker-client")" == "700" ]]
[[ "$(stat -c '%a' "$PALPANEL_SYSTEM_DATA_DIR/panel-update")" == "700" ]]
if [[ "$(id -u)" -eq 0 ]]; then
  [[ "$(stat -c '%U:%G' "$PALPANEL_SYSTEM_DATA_DIR/docker-client")" == "$service_user:$service_user" ]]

  docker_config_dir="$PALPANEL_SYSTEM_DATA_DIR/docker-client"
  saved_docker_config_dir="$PALPANEL_SYSTEM_DATA_DIR/docker-client.real"
  sentinel_dir="$tmp/docker-client-sentinel"
  mv "$docker_config_dir" "$saved_docker_config_dir"
  mkdir "$sentinel_dir"
  chmod 751 "$sentinel_dir"
  chown root:root "$sentinel_dir"
  sentinel_stat="$(stat -c '%a:%u:%g' "$sentinel_dir")"
  ln -s "$sentinel_dir" "$docker_config_dir"
  if "$ctl" install >"$tmp/symlink-install.out" 2>"$tmp/symlink-install.err"; then
    printf 'install accepted a symbolic-link docker client directory\n' >&2
    exit 1
  fi
  grep -Fq 'symbolic link' "$tmp/symlink-install.err"
  [[ -L "$docker_config_dir" ]]
  [[ "$(stat -c '%a:%u:%g' "$sentinel_dir")" == "$sentinel_stat" ]]
  rm "$docker_config_dir"
  mv "$saved_docker_config_dir" "$docker_config_dir"
fi
[[ "$(grep -Fxc 'enable palpanel-update.path palpanel-sav-cli.service palpanel-palcalc.service palpanel.service' "$systemctl_log")" -eq 2 ]]
[[ "$(grep -Fxc 'start palpanel-update.path' "$systemctl_log")" -eq 2 ]]
[[ "$(grep -Fxc 'restart palpanel-sav-cli.service palpanel-palcalc.service palpanel.service' "$systemctl_log")" -eq 2 ]]
"$ctl" uninstall >/dev/null
[[ ! -e "$PALPANEL_INSTALL_ROOT" ]]
[[ ! -e "$PALPANEL_LIBEXEC_DIR/palpanel-updater" ]]
[[ -f "$PALPANEL_ETC_DIR/palpanel.env" && -f "$PALPANEL_SYSTEM_DATA_DIR/preserve.marker" ]]

"$ctl" install >/dev/null
"$ctl" uninstall --purge >/dev/null
[[ ! -e "$PALPANEL_INSTALL_ROOT" && ! -e "$PALPANEL_ETC_DIR" && ! -e "$PALPANEL_SYSTEM_DATA_DIR" ]]
[[ ! -e "$PALPANEL_LIBEXEC_DIR/palpanel-updater" ]]
printf 'install, upgrade, uninstall, and purge verification passed\n'
