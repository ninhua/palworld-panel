#!/usr/bin/env bash
set -euo pipefail

archive="${1:?usage: test-bootstrap-install.sh <linux-archive>}"
archive="$(readlink -f -- "$archive")"
archive_name="$(basename -- "$archive")"
[[ "$archive_name" =~ ^palpanel_(v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?)_linux_amd64\.tar\.gz$ ]] || {
  printf 'unexpected archive name: %s\n' "$archive_name" >&2
  exit 1
}
version="${BASH_REMATCH[1]}"
root_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/release" "$tmp/systemd"
cp "$archive" "$tmp/release/$archive_name"
(cd "$tmp/release" && sha256sum "$archive_name" >SHA256SUMS)

export PALPANEL_TEST_MODE=1
export PALPANEL_RELEASE_BASE_URL="file://$tmp/release"
export PALPANEL_INSTALL_ROOT="$tmp/opt/palpanel"
export PALPANEL_ETC_DIR="$tmp/etc/palpanel"
export PALPANEL_SYSTEM_DATA_DIR="$tmp/var/lib/palpanel"
export PALPANEL_SYSTEMD_DIR="$tmp/systemd"
export PALPANEL_LIBEXEC_DIR="$tmp/libexec"
PALPANEL_SERVICE_USER="$(id -un)"
export PALPANEL_SERVICE_USER
export PALPANEL_SKIP_SYSTEMD=1
export PALPANEL_SKIP_HEALTH_CHECK=1

"$root_dir/install.sh" --version "$version" --listen 127.0.0.1:18080 --no-docker >"$tmp/install.out"
grep -qx 'PALPANEL_LISTEN_ADDR=127.0.0.1:18080' "$PALPANEL_ETC_DIR/palpanel.env"
[[ -x "$PALPANEL_INSTALL_ROOT/current/bin/palpanel" ]]
[[ -x "$PALPANEL_INSTALL_ROOT/current/bin/sav-cli" ]]
[[ -x "$PALPANEL_INSTALL_ROOT/current/bin/palcalc-bridge" ]]
[[ -x "$PALPANEL_INSTALL_ROOT/current/bin/palpanel-updater" ]]
[[ -x "$PALPANEL_LIBEXEC_DIR/palpanel-updater" ]]
grep -q '^Panel URL: http://127.0.0.1:18080/$' "$tmp/install.out"
grep -q '^Open the panel URL to register the first administrator\.$' "$tmp/install.out"

"$root_dir/install.sh" --version "$version" --no-docker >"$tmp/upgrade.out"
grep -qx 'PALPANEL_LISTEN_ADDR=127.0.0.1:18080' "$PALPANEL_ETC_DIR/palpanel.env"
grep -q '^Panel URL: http://127.0.0.1:18080/$' "$tmp/upgrade.out"

printf '%064d  %s\n' 0 "$archive_name" >"$tmp/release/SHA256SUMS"
if "$root_dir/install.sh" --version "$version" --no-docker >"$tmp/bad.out" 2>"$tmp/bad.err"; then
  printf 'installer accepted an invalid checksum\n' >&2
  exit 1
fi
grep -q 'checksum verification failed' "$tmp/bad.err"
(cd "$tmp/release" && sha256sum "$archive_name" >SHA256SUMS)

"$PALPANEL_INSTALL_ROOT/current/palpanelctl" uninstall --purge >/dev/null

mkdir -p "$tmp/fake-bin" "$tmp/legacy-data"
printf 'legacy-data\n' >"$tmp/legacy-data/preserve.marker"
cat >"$tmp/legacy.env" <<'EOF'
PALPANEL_DATA_DIR=/app/data
PALPANEL_LISTEN_ADDR=127.0.0.1:18081
PALPANEL_SERVER_DIR=/app/data/server
PALPANEL_RCON_PORT=25570
PALWORLD_ADMIN_PASSWORD=legacy-secret
EOF
cat >"$tmp/fake-bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
command_name="${1:-}"
shift || true
case "$command_name" in
  inspect)
    if [[ "${1:-}" != "--format" ]]; then
      printf '{}\n'
      exit 0
    fi
    template="$2"
    case "$template" in
      *Config.Image*) printf 'uitok/palworld-panel:legacy\n' ;;
      *Config.Env*) cat "${PALPANEL_FAKE_DOCKER_ENV:?}" ;;
      *Mounts*) printf '%s|/app/data\n' "${PALPANEL_FAKE_DOCKER_DATA:?}" ;;
      *State.Running*) printf 'true\n' ;;
      *) exit 1 ;;
    esac
    ;;
  stop|start)
    printf '%s %s\n' "$command_name" "${1:-}" >>"${PALPANEL_FAKE_DOCKER_LOG:?}"
    ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$tmp/fake-bin/docker"
export PATH="$tmp/fake-bin:$PATH"
export PALPANEL_FAKE_DOCKER_ENV="$tmp/legacy.env"
export PALPANEL_FAKE_DOCKER_DATA="$tmp/legacy-data"
export PALPANEL_FAKE_DOCKER_LOG="$tmp/docker.log"
export PALPANEL_INSTALL_ROOT="$tmp/migrate/opt/palpanel"
export PALPANEL_ETC_DIR="$tmp/migrate/etc/palpanel"
export PALPANEL_SYSTEM_DATA_DIR="$tmp/migrate/unused-default"
export PALPANEL_SYSTEMD_DIR="$tmp/migrate/systemd"
export PALPANEL_LIBEXEC_DIR="$tmp/migrate/libexec"
"$root_dir/install.sh" --version "$version" --migrate-container legacy-palpanel --no-docker >"$tmp/migrate.out"
grep -qx "PALPANEL_DATA_DIR=$tmp/legacy-data" "$PALPANEL_ETC_DIR/palpanel.env"
grep -qx "PALPANEL_SERVER_DIR=$tmp/legacy-data/server" "$PALPANEL_ETC_DIR/palpanel.env"
grep -qx 'PALWORLD_ADMIN_PASSWORD=legacy-secret' "$PALPANEL_ETC_DIR/palpanel.env"
grep -qx 'stop legacy-palpanel' "$tmp/docker.log"
if grep -q '^start ' "$tmp/docker.log"; then
  printf 'successful migration restarted the legacy container\n' >&2
  exit 1
fi
[[ -f "$tmp/legacy-data/preserve.marker" ]]
grep -q '^Legacy container: legacy-palpanel is stopped and retained for rollback\.$' "$tmp/migrate.out"
printf 'GitHub bootstrap installer verification passed\n'
