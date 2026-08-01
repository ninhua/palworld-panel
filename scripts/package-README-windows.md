# PalPanel Windows Package

This ZIP contains the PalPanel backend with its embedded web UI, native `sav-cli`,
self-contained `palcalc-bridge`, and the double-click Windows Launcher.

## Start

1. Extract the whole ZIP to a writable directory.
2. Double-click `PalPanel.exe`.
3. Confirm the first-run data location prompt.
4. Register the first administrator in the browser. The frontend and API use
   the same address, so no backend URL is required.

## Runtime locations

The release ZIP uses a portable sidecar layout. Unless `PALPANEL_RUNTIME_ROOT` or
`--runtime-root` is deliberately supplied, configuration lives in `config\` and all
managed runtime data lives in `data\` next to `PalPanel.exe`. This includes the database,
backups, logs, game installation, saves, Mods, UE4SS, and PalDefender files managed by the
panel. Do not point a release ZIP at a source-tree runtime by accident.

Player Save Migration in the Save Center uses the bundled
`palworld-uid-remap.exe`. The guided workflow calculates Steam and NoSteam
PlayerUID values, blocks existing target UIDs, creates a verified full backup,
stops the game server, performs an atomic world replacement, and rolls back
automatically when verification or restart fails.

The setup page can also bind an existing Palworld Dedicated Server directory.
That import is in-place: PalPanel stores the validated absolute path and manages
the existing configuration, saves, Mods, backups, launches, and updates there.
It does not copy the game installation into the ZIP directory. Stop any
separately launched PalServer process before importing the directory.

Source-tree development is different: a checkout uses
`<repository>\dev-runtime\windows\` as its managed runtime root. Development scripts
never need to write release data beside a packaged executable, and release maintenance
commands never delete source-tree files.

Do not run the Launcher from inside the ZIP. The Launcher prevents duplicate
instances, starts and health-checks `sav-cli` and `palcalc-bridge` before the
backend, supervises all child processes, and stops them when it exits. The read-only sidecar maps
player-save inventory containers to their owners; one missing or damaged
player save produces a warning instead of failing the complete world index.

`windows_steamcmd` is the recommended runtime on a native Windows host. The
game server itself is installed from the web setup page and is not bundled in
this ZIP. Current Palworld releases run `PalServer-Win64-Shipping-Cmd.exe`;
PalPanel also retains compatibility with the older `PalServer-Win64-Test-Cmd.exe`
name. Because current builds may not emit stdout, PalPanel records its own bounded
lifecycle log and uses component-native logs for dependency load status.

If SteamCMD reports `Steam needs to be online to update`, open System Settings >
Network & proxy and configure the Server installation & updates proxy. HTTP,
HTTPS, SOCKS5, and SOCKS5H URLs are accepted. Credentials are stored under
`data\secrets\network-proxy.json` and are never returned by the API or written to
task output. PalPanel temporarily applies a loopback bridge to the current-user
Windows proxy only while SteamCMD is running, restores the previous values when
the task ends, and performs startup recovery after an interrupted process.

## LAN access

The safe default is `127.0.0.1:8080`. To listen on every IPv4 interface, stop
PalPanel, edit `config\palpanel.env`, set
`PALPANEL_LISTEN_ADDR=0.0.0.0:8080`, and start `PalPanel.exe` again. Allow TCP
port 8080 through Windows Defender Firewall, then connect from another LAN
device using the Windows host's real LAN address, such as
`http://192.168.1.20:8080`. `0.0.0.0` is a listen address, not a browser URL.
Do not publish the panel directly to the Internet; use HTTPS or a controlled
VPN/reverse-proxy entry point.

## Steam Workshop login

Workshop search, details, and translation are available without Steam login.
Before downloading a Workshop item, a local administrator configures the Steam
account and password in PalPanel. The password is stored as plaintext in the
ACL-restricted `data\secrets\steam-workshop-credentials.json` file; Steam Guard
codes are used for one verification attempt and are never stored. Each download
logs in and downloads in one SteamCMD process through a temporary private script.
The script is deleted afterward, and stale scripts are cleaned before the next
attempt. Credential operations remain restricted to the same Windows host and
the `security:write` permission.

GitHub, public HTTPS ZIP, local ZIP, and local Mod actions do not require this
Steam login. PalDefender is also outside the Workshop gate.

Installation always checks and installs pinned UE4SS `v3.0.1` first. It then
queries the official PalDefender GitHub latest stable Release and downloads
both `d3d9.dll` and `PalDefender.dll` with the published SHA-256 digests. A
digest-verified ZIP is used only when those direct assets are absent. The
current official release is v1.8.3, but PalPanel follows GitHub Latest instead
of embedding or pinning that version.
Load status is read from native `UE4SS.log` and `PalDefender\Logs\*.log`
evidence. After an admin installs and configures PalDefender, `/gm` provides
player/inventory/progression/technology/Pal views, 2,455-item icon search,
batch item and Pal grants, editable PalTemplates, messages, kick and ban
controls. Whitelist, temporary admin, Pal export, and live ID-catalog actions
also require Palworld RCON with `RCONEnabled=True`, a non-empty
`AdminPassword`, and PalDefender `RCONbase64=false`. Access-config changes must
be followed by a PalDefender config reload; `/setadmin` lasts only for the
current Palworld server session.

The Real-time Map page refreshes online player positions every two seconds when
PalDefender REST is available. Bases, offline players, pals, and map objects use
the latest read-only save index. The background is a coordinate schematic, not
an extracted Palworld terrain image.

Keep PalDefender REST on loopback or a controlled trusted network and never
expose its Bearer Token. OpenAI-compatible translation accepts a Base URL,
model and API key in Settings; real keys must not be placed in package config,
logs, screenshots or support reports.

## Upgrade, uninstall, and recovery

Extract the candidate ZIP completely before upgrading. From the existing extracted release
directory, run its included maintenance script and give it the candidate ZIP path:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\maintenance\upgrade-windows.ps1 `
  -InstallRoot $PWD `
  -Archive C:\Downloads\palpanel_vNEXT_windows_amd64.zip
```

Use `-ExpectedSHA256 <published-64-character-sha256>` when a release checksum is available.
The upgrade stops only processes whose executable path belongs to this installation, stages
and verifies the ZIP, snapshots the previous program payload and managed database, then
starts the upgraded Launcher without a browser to validate the database migration. If that
validation fails, the previous payload and database snapshot are restored; the retained
transaction directory is `.palpanel-maintenance\tx\...`. Configuration, game
files, saves, Mods, UE4SS, PalDefender, and backups are never overlaid by the upgrade.

Normal uninstall removes only the packaged program payload and stops tracked PalPanel/game
processes. It preserves `config\`, `data\`, and recovery snapshots:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\maintenance\uninstall-windows.ps1 `
  -InstallRoot $PWD
```

Complete cleanup is intentionally a separate, explicit action. Run it before normal
uninstall (or from a separately retained copy of the maintenance script):

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\maintenance\uninstall-windows.ps1 `
  -InstallRoot $PWD `
  -PurgeData `
  -ConfirmPurge PURGE_PALPANEL_MANAGED_DATA
```

That command removes only this verified installation's `config\`, `data\`, and
`.palpanel-maintenance\` children. It does not remove the installation root itself, the
ZIP archive, source directories, or configuration/data paths redirected outside the managed
sidecar.

After a damaged configuration prevents startup, first list retained snapshots:

```powershell
.\maintenance\recover-windows-config.ps1 -InstallRoot $PWD -ListBackups
```

Restore the latest snapshot only with confirmation, or generate a new secure configuration
while retaining the unreadable original:

```powershell
.\maintenance\recover-windows-config.ps1 -InstallRoot $PWD `
  -RestoreLatest -ConfirmRecovery RESTORE_PALPANEL_CONFIG

.\maintenance\recover-windows-config.ps1 -InstallRoot $PWD `
  -Recreate -ConfirmRecovery RECREATE_PALPANEL_CONFIG
```

These scripts validate archive paths, reject reparse points, and do not claim a real game
server was started. A package upgrade validates the Launcher and database migration only;
use the documented Windows live validation separately for SteamCMD and Palworld evidence.
If `palpanel.env` deliberately redirects a runtime or database path outside the release
`data\` sidecar, the upgrade refuses automatic startup validation; use
`-SkipStartupValidation` and validate/migrate that custom location manually.

## Signature warning

The executables are not Authenticode-signed. Windows SmartScreen may show an
unknown publisher warning. Verify the ZIP with the release `SHA256SUMS` before
running it.

## License

PalPanel is licensed under GPL-3.0-or-later; see `LICENSE`. Third-party
components retain their own licenses as listed in `THIRD_PARTY_LICENSES.txt`.
