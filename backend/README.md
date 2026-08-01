# PalPanel Backend

Go/Gin backend for a Palworld dedicated-server panel. It manages a Windows edition Palworld Dedicated Server through either Windows SteamCMD or Docker + Wine, exposes REST APIs for lifecycle control, Palworld config, server mods, PalDefender, jobs, backups, and Palworld REST API proxying.

## Development

```powershell
cd backend
$env:PALPANEL_REQUIRE_AUTH="true"
$env:PALPANEL_CORS_ORIGINS="http://127.0.0.1:63107,http://localhost:63107"
go test -p=1 ./...
go run ./cmd/palpanel
```

The API listens on `127.0.0.1:8080` by default. Runtime data is stored in the repository `data` directory unless `PALPANEL_DATA_DIR` is set. On a new database, open the web UI and register the first administrator; later visits use the server-managed session. The Vite frontend port comes from `VITE_DEV_PORT` and defaults to `63107`; if it is opened from a LAN address such as `http://192.168.x.x:63107`, include that exact origin in `PALPANEL_CORS_ORIGINS` or use `PALPANEL_CORS_ORIGINS=*` only for isolated local development.

Proxy environment variables such as `HTTP_PROXY`, `HTTPS_PROXY`, and `ALL_PROXY` are optional host-level settings. They are intentionally not enabled by default in `.env.example`.

## Production Package

The repository-level `scripts/package.sh` builds the supported Linux amd64 release. It builds the frontend first and embeds it in the Go binary, then packages native cgo/Oodle support in sav-cli, `palpanelctl`, and systemd units. `scripts/package.ps1` builds the equivalent unsigned Windows amd64 ZIP.

In the Linux package the backend binary serves the embedded UI and `palpanelctl` sets package-local defaults:

- `PALPANEL_BACKEND_DIR=<package>/backend`
- `PALPANEL_DATA_DIR=<package>/data`
- `PALPANEL_RUNNER_DIR=<package>/backend/deployments/wine-runner`

`palpanelctl init` creates a private configuration and directs the user to browser registration. `palpanelctl install` uses `/opt/palpanel/<version>`, `/etc/palpanel`, and `/var/lib/palpanel` with separate systemd services for the web process and sav-cli. Installation enables and starts both units; portable mode and the Windows Launcher also start sav-cli before the backend and wait for its health endpoint.

## Auth

Browser authentication uses the `palpanel_session` HttpOnly, SameSite=Lax Cookie returned by registration or login. State-changing session requests must also be same-origin. Automation uses a revocable development key created in Settings:

```http
Authorization: Bearer ppk_...
```

The complete development key is returned only in its create response; SQLite stores only its digest. Revocation and `palpanel admin reset-password` invalidate credentials immediately. `GET /api/health`, `GET /api/ready`, `GET /api/auth/status`, `POST /api/auth/register`, and `POST /api/auth/login` are public as required by startup and login flows.

`GET /api/auth/me` returns the authenticated role and permission names without returning credentials. The destructive `world:reset` and `ai:config` permissions belong only to the admin role; Workshop translation uses the operator-compatible `mods:write` permission.

Set `PALPANEL_REQUIRE_AUTH=false` only for isolated local development.

## Production Web Serving

Release builds use the `embed_webui` build tag and serve the embedded SPA, including route fallback and immutable asset caching, from the API port. Normal development builds may set `PALPANEL_FRONTEND_DIST` to an external frontend `dist` directory; without either source, API routes continue to work and return JSON 404 responses. For reverse proxies, forward the single backend origin over HTTPS.

## Configuration And CLI

The backend accepts `--config <path>`, `--init-config`, and `--version`. Config files use strict `KEY=VALUE` parsing and are never sourced or executed. Process environment values take precedence over file values. Initial configuration uses Unix mode `0600` and does not contain authentication credentials. Use `palpanel admin reset-password [--username NAME]` for local account recovery; it revokes that account's sessions and development keys.

Static authentication variables from older releases are ignored. After upgrading such an installation, register the first administrator in the browser; existing configuration, SQLite data, Palworld server data, saves and Mods remain in place.

Named provider settings are `PALPANEL_STEAM_API_BASE_URL`, `PALPANEL_STEAM_API_TIMEOUT_SECONDS` (15 seconds), and `PALPANEL_AI_TRANSLATION_TIMEOUT_SECONDS` (90 seconds). Optional `PALPANEL_LOG_LEVEL` accepts `debug`, `info`, `warn`, or `error`; `debug` is the startup default for PalPanel's own debug log when no persisted runtime choice exists.

PalPanel runtime Debug logging is independently switchable through
`GET/PUT /api/system/debug`, persists in SQLite, and mirrors standard logs plus
request timing and health-probe results into the bounded
`LogsDir/palpanel-debug.log`. It never records request bodies or credentials.

## Runtime Modes

- `windows_steamcmd`: recommended for production Windows hosts. The backend downloads SteamCMD into `data/tools/steamcmd` when needed and installs the Windows dedicated server with `steamcmd +login anonymous +app_update 2394010 validate +quit`.
- `wine_docker`: keeps the existing Docker + Wine flow for Windows edition server mods and containerized operation. Official Palworld docs warn against Docker Desktop for production save-data IO, so update operations create backups first. Version checks use the existing Wine runner image; build or install once before checking remote version in this mode.

Official REST and RCON health checks distinguish authentication failures,
disabled services, and Docker mapping mismatches. `PALPANEL_RCON_HOST` defaults
to loopback but can target a container DNS name or host gateway during a legacy
container migration. In Wine Docker mode, loopback checks require
`RESTAPIPort=PALPANEL_REST_PORT` and `RCONPort=PALPANEL_RCON_PORT`.

The Wine runner build uses `PALPANEL_DOCKER_RUNNER_BASE_IMAGE` as its base image and keeps the pinned digest by default. If Docker Hub metadata requests time out, the backend retries the same image through comma-separated `PALPANEL_DOCKER_RUNNER_BASE_IMAGE_MIRRORS` prefixes such as `docker.1ms.run` or `registry.cyou`.

Startup arguments are managed separately from `PalWorldSettings.ini` through `GET/PUT /api/server/startup`. PalPanel always adds `-enable-gamedata-api`, `-log`, `-stdout`, and `-FullStdOutLogOutput` (case-insensitively deduplicated) so the bounded game-data proxy and live log collection work on Palworld 1.0 servers.

## Steam Workshop

Workshop search uses a byte-wise XOR-obfuscated key bundled in release binaries by default. `STEAM_WEB_API_KEY` overrides it at runtime. Item details use the public `ISteamRemoteStorage/GetPublishedFileDetails` endpoint without a key. API responses expose only the `bundled` or `environment` source and never the key itself. XOR obfuscation is not encryption; a bundled value remains recoverable from a public binary. `PALPANEL_WORKSHOP_APP_ID` defaults to `1623730` and is used by search metadata and the SteamCMD `workshop_download_item` flow.

Windows and Linux/Docker Workshop authentication store an explicit account and password while using Steam Guard or Steam Mobile approval to authorize the managed SteamCMD installation. Steam Guard codes are accepted only for the current verification attempt and are never persisted. After a successful login, PalPanel stores the account and password in the private `data/secrets/steam-workshop-credentials.json` file (Windows ACL or Linux mode 0600), while SteamCMD retains its machine authorization under the managed runtime. On Linux the complete `data/steamcmd-workshop-config` directory is mounted at `/root/Steam`; mounting `/opt/steamcmd/config` is incorrect because Linux SteamCMD writes its user data and machine grant under `$HOME/Steam`. This is intentionally less secure than the former cache-only design, but secrets are never returned through the API or written to PalPanel job/log output. Login tests use private temporary runscripts; Linux mounts the script read-only into the Docker/Wine runner without placing credentials in Docker arguments. Downloads use cached-account login without the password in the download script, avoiding repeated mobile confirmations. Missing or expired cached authorization sends the user back through reauthorization. Login operations retain the admin-only `security:write` and true-loopback requirements. Workshop search, details, and translation remain available without Steam credentials; only downloads and Workshop imports are gated. The `palpanelctl steam-login` command remains available as a Linux recovery fallback but is no longer the primary panel flow.

## Community Server Discovery

`GET /api/community-servers` exposes discoverable Palworld community servers through a backend-only BattleMetrics client. China and online servers are the defaults. Results are cached for 60 seconds; the most recent successful response can be served as stale data for up to 24 hours. Browsers and AstrBot never call BattleMetrics directly.

Admins can configure separate HTTP, HTTPS, SOCKS5, or SOCKS5H proxies from Settings for public downloads/server updates and community-server discovery. The download scope covers SteamCMD, Palworld server updates, Workshop metadata and downloads, GitHub/public HTTP(S) mod imports, PalDefender, and UE4SS. Managed proxy secrets are stored in `data/secrets/network-proxy.json`, are never returned with user information, and take effect on the next task or query. `PALPANEL_STEAMCMD_PROXY_URL` and `PALPANEL_COMMUNITY_SERVERS_PROXY_URL` remain startup fallbacks when no managed file exists. A self-hosted compatible community source can still be selected with `PALPANEL_COMMUNITY_SERVERS_API_BASE_URL`. Source status, cache freshness, persistent-cache failures, and the last sanitized upstream error are available from `GET /api/community-servers/source-status`.

Native Windows SteamCMD does not reliably consume application-scoped proxy environment variables. PalPanel therefore starts a loopback-only HTTP bridge for each proxied SteamCMD command, temporarily points the current-user Internet proxy at that bridge, and restores the exact prior values afterward. A mode-0600 restoration marker under `data/secrets/steamcmd-proxy-restore.json` allows the next PalPanel startup to recover settings after an interrupted process. Docker/Wine install and Workshop commands receive proxy variables by environment name so credentials are not embedded in Docker command arguments or job errors.

## Mod Configuration Center

The first typed adapters cover PalDefender, Workshop UE4SS Experimental `3625223587`, PalSchema `3625280368`, and Extended Base Range `3625907101`. Other recognized Workshop, UE4SS, PalSchema, Pak, and LogicMods installations use a restricted UTF-8 text editor for JSON, INI, CFG, TOML, YAML, TXT, and Lua files.

The generic editor rejects files larger than 1 MiB, binary/NUL content, symlinks, reparse points, traversal, and files outside the validated Mod root. It never edits DLL, Pak, executable, `Info.json`, `InstallManifest.json`, or `PalModSettings.ini`. Writes use opaque file IDs, revision checks, parsing where supported, atomic replacement, and backups. Lua is marked as executable code and requires an explicit confirmation; PalPanel does not execute or semantically validate Lua. See [`../docs/workshop-mod-candidates-2026-07-18.md`](../docs/workshop-mod-candidates-2026-07-18.md) for the current popular-Mod review.

Unified Mod import accepts a Workshop ID/URL, a public GitHub repository or Release URL, a public HTTP(S) ZIP, or a local ZIP. Every source is inspected before installation. Remote downloads ignore ambient proxy variables but use the explicit managed download proxy when enabled; they still reject credentials, redirects to non-public addresses, oversized payloads, unsafe ZIP paths, links, special files, multiple `Info.json` files, and missing `PackageName`. New Mods install disabled; updates preserve record identity and enabled state, and enabled updates set `pending_restart` without restarting PalServer.

## AI Translation

Admins configure an OpenAI-compatible Base URL, model, and API key from Settings. Base URLs may use HTTP or HTTPS. Embedded URL credentials, redirects, and oversized responses are rejected; HTTP transports credentials and prompt content without encryption. The API key is atomically stored in `data/secrets/ai-translation.key` with mode `0600` and is never returned by APIs, application logs, or audit records.

Workshop detail translation always fetches the authoritative description from Steam instead of accepting arbitrary browser text. Cached translations are keyed by Workshop ID, source SHA-256, target language, provider URL, and model, so source or model changes invalidate the cache automatically.

## PalDefender Latest Stable And GM

Install and update jobs query `Ultimeit/PalDefender` through GitHub's latest stable Release endpoint. They prefer the official `d3d9.dll` and `PalDefender.dll` assets, require GitHub-published SHA-256 digests for both downloads, and transactionally replace the installed files with backup and rollback. A digest-verified `PalDefender.zip` is accepted only when the direct DLL assets are absent. The installed version is persisted from the Release tag, so the current v1.8.3 is installed without pinning future updates to 1.8.3. PalPanel no longer embeds a PalDefender binary.

The `/gm` frontend uses typed PalPanel DTOs instead of calling PalDefender from the browser. The backend proxy covers version/status, player lists and individual player details, six inventory containers, progression and technology reads/writes, Pal reads, direct Pal grants, PalTemplate grants, batches of up to 100 item grants or removals, typed coordinate/player teleport, guarded single-Pal release, direct messages, broadcasts, alerts, kick, ban, IP-ban and unban operations. PalTemplate files are managed transactionally under the PalDefender directory and expose the supported level, IV, soul, skill, passive and work-suitability fields. Player exports written by `/exportpals` under `Pals/Exported/<UserId>/` can be listed and read through typed endpoints, then saved as a new managed template before granting. The local catalogs contain 2,455 ItemIDs plus Chinese Pal and technology metadata for in-panel search; runtime `/gettechids` results mark technology support for the connected server.

Whitelist listing and mutation, session-scoped `/setadmin`, Pal export, live technology/skin catalogs and runtime command discovery use typed Source RCON operations. PalPanel never accepts an arbitrary RCON command from the browser. RCON connects only to `127.0.0.1`, requires Palworld `RCONEnabled=True` and a non-empty `AdminPassword`, and refuses commands while PalDefender `RCONbase64` is enabled. Access settings cover `useWhitelist`, `whitelistMessage`, `useAdminWhitelist`, `adminAutoLogin` and `adminIPs`; changes require an explicit PalDefender config reload. `/setadmin` toggles the current server session and is not persistent across a Palworld restart.

GM reads require `read`; item grants, messages and punishments require `players:write`. Every GM write also requires an `Idempotency-Key` containing 8-128 safe ASCII characters. Repeating the same key and normalized request within ten minutes replays the first result without contacting PalDefender again; reusing it for another route or payload returns `409 idempotency_key_reused`. This bounded cache belongs to the current backend process, so after a process restart an operator must inspect the audit log and player state before retrying an operation whose outcome was uncertain.

PalDefender installation, configuration and REST Token creation require `security:write`, which is admin-only. GM calls are rejected until the DLLs exist, startup-log loading is verified, and the PalDefender REST API is enabled. The REST API defaults to `http://127.0.0.1:17993`; the backend rejects non-loopback endpoints, inherited proxies and redirects so the bearer token cannot be forwarded to another host. Timeout, unavailable, invalid-response, player-offline and player-not-found failures have separate API codes. Never publish the PalDefender Bearer Token in source, logs, screenshots, proxy configuration or browser code.

## World Reset

`GET /api/server/world` previews the active `DedicatedServerName`, save timestamp, and running state. Admin reset requests must include the previewed world ID and the exact confirmation phrase `RESET WORLD`. The asynchronous task rechecks the active world, saves/notifies and stops a running server, creates and verifies a `pre-world-reset` backup, then atomically stages only `SaveGames/0/<world-id>`.

When the server was running, the task starts it again and waits up to 180 seconds for a non-empty new `Level.sav`. Success removes the staged old world but keeps the verified backup. Start or generation failures stop the new process and retain both the backup and `.palpanel-world-reset/<job-id>` directory with recovery paths in the Job error. Server binaries, INI files, Workshop mods, PalDefender, and panel data are not removed.

## Server Logs

Wine mode mounts `data/logs` at `/data/logs` and mirrors PalServer stdout/stderr to both Docker stdout and `palserver.log`. Persistent files and Docker `json-file` output each use a 20 MiB limit with five backups. Windows SteamCMD mode uses the same bounded persistent-file policy.

`GET /api/server/logs` reads the persistent file first and falls back to `docker logs` only when the file is missing or unreadable. It remains available after the server stops and returns `source`, `available`, `reason`, and `updated_at` metadata alongside the compatible `logs` field.

## Performance Knobs

Read-only Palworld REST calls use a short timeout so offline save pages can fall back quickly when the official REST API is unavailable:

```bash
PALPANEL_PALWORLD_REST_READ_TIMEOUT_MS=1200
PALPANEL_PERF_SLOW_REQUEST_MS=500
PALPANEL_MONITOR_RETENTION_DAYS=7
```

`PALPANEL_PERF_SLOW_REQUEST_MS` controls slow-request logging. API responses also include `Server-Timing` and `X-Palpanel-Cache` headers where cached read paths are used.
Monitor samples are pruned hourly after seven days by default. Set `PALPANEL_MONITOR_RETENTION_DAYS=0` to disable monitor history persistence.

`GET /api/ready` is the public readiness probe. It verifies SQLite, the applied schema version, and required data directories; `GET /api/health` remains a process liveness probe.

## Save Indexer

Offline players, guilds, bases, pals, inventories, and map points come from a separate read-only save indexer. Start the sidecar from the repository root:

```bash
docker build -t palpanel-sav-cli:local ./sav-cli
docker run --rm --network host \
  -v "$PWD/data/server:$PWD/data/server:ro" \
  palpanel-sav-cli:local
```

Then set:

```bash
PALPANEL_SAVE_INDEXER_ENABLED=true
PALPANEL_SAVE_INDEXER_URL=http://127.0.0.1:8090
PALPANEL_SAVE_INDEX_CACHE_DIR=../data/save-index
```

The sidecar is the self-developed Go `sav-cli` in `sav-cli/` and never writes back to `.sav`. Packaged launchers start it automatically with the panel. It reads per-player `Players/*.sav` files and associates `InventoryInfo` container IDs with `OwnerType=player`; a missing or damaged individual player save adds a warning while the rest of the world index continues. Unsupported world containers or schema changes are reported as `parser_incompatible`; the backend keeps the last successful cache when available and does not block other panel features.

`GET /api/map/entities` combines that save snapshot with online-player data. PalDefender `WorldLocation` is preferred, with `MapLocation` as its fallback; the official Palworld REST player list remains the availability fallback when PalDefender REST is unavailable. Live data is cached for two seconds. The `/map` frontend polls on the same interval and projects entities onto the bundled Palpagos map using the attributed community coordinate transform. Players and bases are enabled by default, while pals and map objects are opt-in filters.

## Version Checks

Steam Build IDs remain the authoritative update signal for AppID `2394010`. The local Build ID comes from `data/server/steamapps/appmanifest_2394010.acf`; the latest public Build ID comes from SteamCMD `app_info_print 2394010`.

While the server is running, `GET /api/server/version` also reads the semantic game version from the official `/info` endpoint. The response reports the supported `1.x` compatibility range, a compatibility result, and warnings for enabled Workshop mods, PalDefender, and save-parser verification. Steam Build IDs remain the update authority, so future releases do not require a hard-coded semantic version before they can be detected and installed. An offline server still reports Build IDs without pretending the configuration schema version is the game version.

## Palworld 1.0 REST

The backend contracts cover the official `/info`, `/players`, `/settings`, `/metrics`, and `/game-data` response shapes. `GET /api/server/game-data` is deliberately excluded from periodic polling, has a 3-second default timeout, and rejects responses larger than 16 MiB. Configure those bounds with `PALPANEL_GAME_DATA_TIMEOUT_MS` and `PALPANEL_GAME_DATA_MAX_MB`.

Metrics retain the existing frontend fields and additionally map `basecampnum` to `active_bases` and expose `days`.

## Main Endpoints

- Lifecycle: `POST /api/server/install`, `POST /api/server/update`, `POST /api/server/update-if-needed`, `POST /api/server/start`, `POST /api/server/stop`, `POST /api/server/restart`, `POST /api/server/bootstrap`
- Safe stop/restart: `POST /api/server/safe-stop`, `POST /api/server/safe-restart`
- Setup: `GET /api/server/prerequisites`, `GET/PUT /api/server/runtime`, `GET/PUT /api/server/startup`, `POST /api/server/initialize-config`
- Authentication: `GET /api/auth/status`, `POST /api/auth/register`, `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/me`, `GET/POST/DELETE /api/auth/api-keys`
- Status/logs/jobs: `GET /api/server/status`, `GET /api/server/version`, `POST /api/server/version/check`, `GET /api/server/metrics`, `GET /api/server/game-data`, `GET /api/server/logs?tail=200`, `GET /api/jobs`, `GET /api/jobs/{id}`
- World reset: `GET /api/server/world`, `POST /api/server/world/reset`
- Backups: `POST /api/server/backup`, `GET /api/backups`, `POST /api/backups/{name}/restore`, `GET/PUT /api/backups/webdav/config`, `POST /api/backups/webdav/test`, `POST /api/backups/{name}/upload-webdav`
- Schedules: `GET/POST /api/schedules`, `PUT/DELETE /api/schedules/{id}`, `POST /api/schedules/{id}/run`; supported tasks include world save, backup, safe restart, update, and version checks
- Audit: `GET /api/audit-logs`
- Player access: `GET/POST/DELETE /api/players/bans`, `GET/PUT /api/players/whitelist`, `POST /api/players/{id}/kick`
- Palworld config: `GET /api/config/palworld`, `PUT /api/config/palworld`, `GET /api/config/palworld/schema`, `POST /api/config/palworld/validate`
- Mods: `GET /api/mods`, `POST /api/mods/import/inspect`, `POST /api/mods/import/inspect/{id}/select`, `POST /api/mods/import`, `GET /api/mods/workshop/auth/status`, `POST /api/mods/workshop/auth/start`, `POST /api/mods/workshop/auth/verify`, `GET /api/mods/workshop/search`, `GET /api/mods/workshop/{id}`, `POST /api/mods/workshop/{id}/translate`, plus compatible `/api/mods/upload` and `/api/mods/workshop` endpoints
- Mod configuration: typed adapters under `/api/mods/configurations`; restricted raw files, revision checks, backups, and restores under `/api/mods/{id}/files`
- Community servers: `GET /api/community-servers`, `GET /api/community-servers/source-status`, `POST /api/community-servers/refresh`
- Breeding sidecar status: `GET /api/breeding/status`
- AI translation: `GET/PUT /api/ai/translation/config`, `POST /api/ai/translation/test`
- PalDefender: `GET /api/security/paldefender/releases`, `GET /api/security/paldefender/status`, `POST /api/security/paldefender/install`, `POST /api/security/paldefender/update`, `POST /api/security/paldefender/rollback`, `GET/PUT /api/security/paldefender/config`, `POST /api/security/paldefender/apply-preset`, `POST /api/security/paldefender/rest-token`, `POST /api/security/paldefender/reload-config`
- PalDefender GM: status, players, inventory, progression, technologies, Pals, item grants, PalTemplate management/grants, messages and punishments under `/api/security/paldefender/gm`; typed RCON catalogs and commands under `/api/security/paldefender/gm/commands` and `/api/security/paldefender/gm/catalog`; access settings, whitelist and session-admin operations under `/api/security/paldefender/access`, `/api/security/paldefender/whitelist` and `/api/security/paldefender/admins`

## Paths

- Palworld config: `data/server/Pal/Saved/Config/WindowsServer/PalWorldSettings.ini`
- Official server mods: `data/server/Mods/Workshop/<mod>/Info.json`
- Mod settings: `data/server/Mods/PalModSettings.ini`
- PalDefender binaries: `data/server/Pal/Binaries/Win64/PalDefender.dll` and `data/server/Pal/Binaries/Win64/d3d9.dll`
- PalDefender config after first server start: `data/server/Pal/Binaries/Win64/PalDefender/Config.json`
- Persistent PalServer log: `data/logs/palserver.log` (plus `.1` through `.5`)
- AI translation API key: `data/secrets/ai-translation.key`
- Network proxy credentials: `data/secrets/network-proxy.json`

Server files, Wine prefix, tools, mods, saves, backups, logs, PalDefender files, and SQLite data are kept outside the backend source tree.
