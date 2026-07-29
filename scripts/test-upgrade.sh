#!/usr/bin/env bash
set -euo pipefail

candidate_archive="${1:?usage: test-upgrade.sh <candidate-linux-archive> [previous-version]}"
previous_version="${2:-v1.0.2}"
tmp="$(mktemp -d)"
service_user="palpanel-upgrade-$RANDOM"

cleanup() {
  if [[ "$(id -u)" -eq 0 ]]; then
    userdel "$service_user" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

previous_name="palpanel_${previous_version}_linux_amd64.tar.gz"
release_base="https://github.com/uitok/palworld-panel/releases/download/${previous_version}"
curl -fsSL --retry 3 --retry-delay 2 -o "$tmp/$previous_name" "$release_base/$previous_name"
curl -fsSL --retry 3 --retry-delay 2 -o "$tmp/SHA256SUMS" "$release_base/SHA256SUMS"
(
  cd "$tmp"
  checksum_line="$(grep -E "[[:space:]]${previous_name}$" SHA256SUMS)"
  [[ -n "$checksum_line" ]]
  printf '%s\n' "$checksum_line" | sha256sum -c -
)

mkdir -p "$tmp/previous" "$tmp/candidate"
tar -xzf "$tmp/$previous_name" -C "$tmp/previous"
tar -xzf "$candidate_archive" -C "$tmp/candidate"
previous_dir="$(find "$tmp/previous" -mindepth 1 -maxdepth 1 -type d -print -quit)"
candidate_dir="$(find "$tmp/candidate" -mindepth 1 -maxdepth 1 -type d -print -quit)"
[[ -n "$previous_dir" && -n "$candidate_dir" ]]

fake_bin="$tmp/fake-bin"
mkdir -p "$fake_bin"
cat >"$fake_bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "inspect" ]]; then
  printf '%s\n' "${PALPANEL_FAKE_DOCKER_USER:?}"
  exit 0
fi
printf 'unexpected fake docker invocation: %s\n' "$*" >&2
exit 1
EOF
chmod +x "$fake_bin/docker"

if [[ "$(id -u)" -eq 0 ]]; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "$service_user"
else
  service_user="$(id -un)"
  export PALPANEL_TEST_MODE=1
fi

export PALPANEL_INSTALL_ROOT="$tmp/opt/palpanel"
export PALPANEL_ETC_DIR="$tmp/etc/palpanel"
export PALPANEL_SYSTEM_DATA_DIR="$tmp/var/lib/palpanel"
export PALPANEL_SYSTEMD_DIR="$tmp/systemd"
export PALPANEL_SERVICE_USER="$service_user"
export PALPANEL_SKIP_SYSTEMD=1

"$previous_dir/palpanelctl" install >/dev/null
printf '# upgrade-preserve-config\n' >>"$PALPANEL_ETC_DIR/palpanel.env"
mkdir -p \
  "$PALPANEL_SYSTEM_DATA_DIR/docker-client" \
  "$PALPANEL_SYSTEM_DATA_DIR/server/Pal/Saved/SaveGames/0/world" \
  "$PALPANEL_SYSTEM_DATA_DIR/logs" \
  "$PALPANEL_SYSTEM_DATA_DIR/wineprefix" \
  "$PALPANEL_SYSTEM_DATA_DIR/server/Pal/Saved/Config/WindowsServer" \
  "$PALPANEL_SYSTEM_DATA_DIR/server/Mods/Workshop/existing-mod" \
  "$PALPANEL_SYSTEM_DATA_DIR/backups"
printf 'sqlite-marker\n' >"$PALPANEL_SYSTEM_DATA_DIR/palpanel.db"
printf 'save-marker\n' >"$PALPANEL_SYSTEM_DATA_DIR/server/Pal/Saved/SaveGames/0/world/Level.sav"
printf 'game-log-marker\n' >"$PALPANEL_SYSTEM_DATA_DIR/logs/palserver.log"
printf 'wine-prefix-marker\n' >"$PALPANEL_SYSTEM_DATA_DIR/wineprefix/prefix.marker"
printf 'palworld-config-marker\n' >"$PALPANEL_SYSTEM_DATA_DIR/server/Pal/Saved/Config/WindowsServer/PalWorldSettings.ini"
printf 'mod-settings-marker\n' >"$PALPANEL_SYSTEM_DATA_DIR/server/Mods/PalModSettings.ini"
printf 'mod-marker\n' >"$PALPANEL_SYSTEM_DATA_DIR/server/Mods/Workshop/existing-mod/Info.json"
printf 'backup-marker\n' >"$PALPANEL_SYSTEM_DATA_DIR/backups/upgrade-test.zip"
printf 'docker-client-marker\n' >"$PALPANEL_SYSTEM_DATA_DIR/docker-client/config.json"
chmod 755 "$PALPANEL_SYSTEM_DATA_DIR/docker-client"
if [[ "$(id -u)" -eq 0 ]]; then
  chown root:root "$PALPANEL_SYSTEM_DATA_DIR/docker-client"
fi

config_hash="$(sha256sum "$PALPANEL_ETC_DIR/palpanel.env" | awk '{print $1}')"
database_hash="$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/palpanel.db" | awk '{print $1}')"
save_hash="$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/server/Pal/Saved/SaveGames/0/world/Level.sav" | awk '{print $1}')"
palworld_config_hash="$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/server/Pal/Saved/Config/WindowsServer/PalWorldSettings.ini" | awk '{print $1}')"
mod_settings_hash="$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/server/Mods/PalModSettings.ini" | awk '{print $1}')"
mod_hash="$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/server/Mods/Workshop/existing-mod/Info.json" | awk '{print $1}')"
backup_hash="$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/backups/upgrade-test.zip" | awk '{print $1}')"
docker_client_hash="$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/docker-client/config.json" | awk '{print $1}')"
game_log_hash="$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/logs/palserver.log" | awk '{print $1}')"
wine_prefix_hash="$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/wineprefix/prefix.marker" | awk '{print $1}')"

if [[ "$(id -u)" -eq 0 ]]; then
  chown -R root:root \
    "$PALPANEL_SYSTEM_DATA_DIR/server" \
    "$PALPANEL_SYSTEM_DATA_DIR/logs" \
    "$PALPANEL_SYSTEM_DATA_DIR/wineprefix"
  PATH="$fake_bin:$PATH" PALPANEL_FAKE_DOCKER_USER=1000:989 \
    "$candidate_dir/palpanelctl" install --docker >/dev/null
else
  "$candidate_dir/palpanelctl" install >/dev/null
fi
[[ "$(sha256sum "$PALPANEL_ETC_DIR/palpanel.env" | awk '{print $1}')" == "$config_hash" ]]
[[ "$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/palpanel.db" | awk '{print $1}')" == "$database_hash" ]]
[[ "$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/server/Pal/Saved/SaveGames/0/world/Level.sav" | awk '{print $1}')" == "$save_hash" ]]
[[ "$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/server/Pal/Saved/Config/WindowsServer/PalWorldSettings.ini" | awk '{print $1}')" == "$palworld_config_hash" ]]
[[ "$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/server/Mods/PalModSettings.ini" | awk '{print $1}')" == "$mod_settings_hash" ]]
[[ "$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/server/Mods/Workshop/existing-mod/Info.json" | awk '{print $1}')" == "$mod_hash" ]]
[[ "$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/backups/upgrade-test.zip" | awk '{print $1}')" == "$backup_hash" ]]
[[ "$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/docker-client/config.json" | awk '{print $1}')" == "$docker_client_hash" ]]
[[ "$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/logs/palserver.log" | awk '{print $1}')" == "$game_log_hash" ]]
[[ "$(sha256sum "$PALPANEL_SYSTEM_DATA_DIR/wineprefix/prefix.marker" | awk '{print $1}')" == "$wine_prefix_hash" ]]
[[ "$(stat -c '%a' "$PALPANEL_SYSTEM_DATA_DIR/docker-client")" == "700" ]]
if [[ "$(id -u)" -eq 0 ]]; then
  [[ "$(stat -c '%U:%G' "$PALPANEL_SYSTEM_DATA_DIR/docker-client")" == "$service_user:$service_user" ]]
  [[ "$(stat -c '%u:%g' "$PALPANEL_SYSTEM_DATA_DIR/server")" == "1000:989" ]]
  [[ "$(stat -c '%u:%g' "$PALPANEL_SYSTEM_DATA_DIR/logs")" == "1000:989" ]]
  [[ "$(stat -c '%u:%g' "$PALPANEL_SYSTEM_DATA_DIR/wineprefix")" == "1000:989" ]]
  mount_mode="$(stat -c '%a' "$PALPANEL_SYSTEM_DATA_DIR/server")"
  log_mode="$(stat -c '%a' "$PALPANEL_SYSTEM_DATA_DIR/logs/palserver.log")"
  (( (8#$mount_mode & 070) == 070 ))
  (( (8#$log_mode & 060) == 060 ))
fi
grep -Fxq "Environment=HOME=$PALPANEL_SYSTEM_DATA_DIR" "$PALPANEL_SYSTEMD_DIR/palpanel.service"
grep -Fxq "Environment=DOCKER_CONFIG=$PALPANEL_SYSTEM_DATA_DIR/docker-client" "$PALPANEL_SYSTEMD_DIR/palpanel.service"
grep -Fxq 'ProtectHome=true' "$PALPANEL_SYSTEMD_DIR/palpanel.service"
[[ -L "$PALPANEL_INSTALL_ROOT/current" ]]
[[ "$(readlink -f "$PALPANEL_INSTALL_ROOT/current")" == "$PALPANEL_INSTALL_ROOT/$(basename "$candidate_dir" | sed 's/^palpanel_//; s/_linux_amd64$//')" ]]
installed_dir="$(readlink -f "$PALPANEL_INSTALL_ROOT/current")"
[[ "$(stat -c '%a' "$installed_dir/bin")" == "775" ]]
if [[ "$(id -u)" -eq 0 ]]; then
  [[ "$(stat -c '%U:%G' "$installed_dir/bin")" == "root:$PALPANEL_SERVICE_USER" ]]
fi
grep -Fxq "ReadWritePaths=$PALPANEL_SYSTEM_DATA_DIR $PALPANEL_INSTALL_ROOT/current/bin" "$PALPANEL_SYSTEMD_DIR/palpanel.service"

"$candidate_dir/palpanelctl" uninstall --purge >/dev/null
printf 'upgrade preservation verification passed: %s -> candidate\n' "$previous_version"
