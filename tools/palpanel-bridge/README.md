# PalPanelBridge read-only probe

This is the first, deliberately read-only UE4SS bridge milestone. It proves:

```text
localhost HTTP -> bounded job queue -> UE4SS on_update game-thread callback
```

It does not expose arbitrary UObject calls and cannot modify players, inventory, pals, or saves.

For the Chinese installation, diagnostic-console examples, and troubleshooting
matrix, see [`docs/palpanel-bridge.md`](../../docs/palpanel-bridge.md).
The unified feature priorities and CI-only delivery gates are maintained in
[`docs/development/palpanel-bridge-roadmap.md`](../../docs/development/palpanel-bridge-roadmap.md).

## Build with GitHub Actions

The Palworld experimental UE4SS build used by the panel reports Git SHA
`c838a8ac`. C++ mods must use the same UE4SS commit, build configuration, and
C runtime. That source tree depends on the restricted UEPseudo repository. The
repository owner must link Epic Games and GitHub, accept the EpicGames
organization invitation, and add a read-capable personal access token as the
repository Actions secret `UEPSEUDO_TOKEN`.

Run the `PalPanelBridge build` workflow. It produces
`PalPanelBridge-v0.1.25-ue4ss-c838a8ac.zip`, containing the complete
`PalPanelBridge` mod directory, configuration, documentation, license, and
SHA-256 checksum. The token used to fetch the SDK is not included in the
package.

## Local build script

The project delivery workflow does not compile or test PalPanelBridge locally.
Use the dedicated GitHub Actions workflow for every accepted build. The script
below is retained only as an emergency SDK diagnostic reference and is not a
release or deployment path.

Run from an MSVC developer shell with CMake, Ninja, and Rust available:

```powershell
./build-local.ps1 -UE4SSRoot C:\src\RE-UE4SS-v3.0.1
```

The DLL is written to:

```text
build/artifact/PalPanelBridge/dlls/main.dll
```

## Install the server package

1. Extract `PalPanelBridge-v0.1.25-ue4ss-c838a8ac.zip`.
2. Copy the extracted `PalPanelBridge` directory into
   `Pal/Binaries/Win64/ue4ss/Mods/`.
3. Edit `PalPanelBridge/config.ini` and replace the placeholder token.
4. Restart PalServer and check `UE4SS.log` for `PalPanelBridge`.

The package includes `enabled.txt`, so it does not overwrite the server's
existing `Mods/mods.txt`.

Startup diagnostics are written to `PalPanelBridge/PalPanelBridge.log`. The
configuration path is resolved from `dlls/main.dll`, not from Wine's process
working directory.

The bridge binds only to `127.0.0.1`. All requests require:

```text
Authorization: Bearer <token>
```

## Probe

```bash
curl -H "Authorization: Bearer <token>" http://127.0.0.1:18083/v1/health
curl -H "Authorization: Bearer <token>" http://127.0.0.1:18083/v1/runtime
curl -X POST -H "Authorization: Bearer <token>" http://127.0.0.1:18083/v1/world
curl -X POST -H "Authorization: Bearer <token>" http://127.0.0.1:18083/v1/players/online
curl -X POST -H "Authorization: Bearer <token>" http://127.0.0.1:18083/v1/players/online/metadata
curl -X POST -H "Authorization: Bearer <token>" http://127.0.0.1:18083/v1/probe/game-thread
curl -H "Authorization: Bearer <token>" http://127.0.0.1:18083/v1/jobs/<job_id>
```

Success means the job changes from `queued` to `completed` and reports
`game_thread_tick_seen=true` plus `unreal_initialized=true`.

Online-player jobs keep the `property_candidates` name lists and also return
`property_details`. Each detail reports the reflected property `name`, its
coarse `kind` (`object`, `array`, `struct`, or `other`), and the declared
object/array-element/struct type when UE4SS exposes one. This is metadata only;
the bridge does not read container contents or modify game objects.

Version `0.1.16` resolves object-type candidates one level further. Details
include whether the current object value exists, its runtime object identity,
and filtered `nested_candidates` from that object. Traversal stops after this
single metadata level and still does not read arrays or container contents.

Version `0.1.17` returns up to 96 unfiltered reflected properties for the two
confirmed data-entry objects, `PalItemSelectorComponent` and
`BP_OtomoPalHolderComponent`. Other objects keep the keyword filter so the JSON
response remains bounded. Values and container contents are still not read.

Version `0.1.18` reports validated element counts for reflected arrays and maps.
It also lists up to 64 keyword-matched reflected functions on the two confirmed
entry objects, including function names and parameter-buffer sizes. Functions
are never invoked and collection elements are never read.

Version `0.1.19` adds parameter metadata to those function candidates without
invoking them. Human-readable timestamps use China Standard Time (`+08:00`),
while Unix millisecond fields remain unchanged.

Version `0.1.20` executes UE reflection outside the job-map mutex. A long-running
online-player scan now reports `running` without blocking health checks or other
HTTP requests. Function-parameter traversal is temporarily disabled after the
`0.1.19` runtime stall; function candidates keep an empty `parameters` array for
wire compatibility. When all 64 job slots are queued or running, new jobs return
`503 job_queue_full` instead of evicting an active job.

Version `0.1.21` adds `POST /v1/players/online/metadata`, a bounded field-discovery
probe for top-level PlayerState and Pawn properties. It describes at most 96 reflected
properties per object for the first online player, without reading property values,
collection contents, nested structures, or invoking functions. This is not a detailed
player-information endpoint.

Version `0.1.24` adds read-only PlayerState `CachedPlayerLocation` and
`GuildBelongTo` object references. Location values are accepted only when the
reflected type is `ScriptStruct /Script/CoreUObject.Vector`, the property size is
12 or 24 bytes, and all three decoded coordinates are finite. Guild objects are
reported only when `UObject::IsReal` succeeds. Failed location reads return no
pseudo-values and include `cached_location_error`.

Version `0.1.25` adds the additive `character_parameter_found` and
`character_parameter` object-reference fields. A metadata probe for the first
collected player also returns `detail_property_metadata` for the `GuildBelongTo`
object and the player's `CharacterParameterComponent`. Each object is enumerated through
the current class and `TSuperStructRange`, uses `IncludeDeprecated`, filters
property names by a fixed keyword list, deduplicates by name, and stops at 64
matches. It reports only property metadata and never reads the matched values,
arrays, maps, nested contents, or unknown functions. Normal online queries do
not perform this metadata enumeration.

Version `0.1.23` fixes the metadata probe to include inherited PlayerState and
Pawn properties. It enumerates the current class and then each parent class with
UE4SS's `TSuperStructRange` and `TFieldRange<FProperty>` using
`IncludeDeprecated`; the probe remains metadata-only, stops at 96 properties, and
`metadata_truncated` still only describes the player-count limit.

## Verified SFTP deployment

After the dedicated Action completes, run the repository deployment helper with
`uv run --with paramiko python tools/palpanel-bridge/deploy.py`. It checks the
latest workflow result, downloads and verifies the artifact, pins the SFTP host
key, uploads a temporary DLL, backs up the current DLL, atomically renames the
new file, and downloads it again for SHA-256 verification. Downloaded build
snapshots are stored in the workspace-level `PalPanelBridge/versions` directory
by default; `--output-root` may override that location. Set
`PALPANEL_SFTP_HOST`, `PALPANEL_SFTP_USERNAME`, `PALPANEL_SFTP_PASSWORD`, and
`PALPANEL_SFTP_HOSTKEY_SHA256`; optionally set `PALPANEL_SFTP_PORT` and
`PALPANEL_SFTP_REMOTE_DIR`. Credentials are never stored in the repository.
Without `--run-id`, the helper binds to the current Git commit, waits up to 45
minutes for its Action to appear and finish, and checks every 30 seconds. A
failed build is never deployed; failed-step output is saved as
`PalPanelBridge-failed-run-<id>.log` and returned to the caller.

Calling `/v1/runtime` twice should show an increasing
`game_thread_tick_count`. It also reports the last game-thread tick time and
bridge uptime without modifying game state.

`POST /v1/world` queues a game-thread-safe UE object lookup. Poll its returned
job ID through `/v1/jobs/<job_id>`; a successful result reports the current
World object's name, full name, and class without changing the object.

`POST /v1/players/online` enumerates live `PalPlayerController` instances on
the game thread and reads the associated PlayerState account name, PlayerUID,
and Pawn metadata. Native and blueprint controller class names are both checked;
when no controller is visible, live PlayerState objects are returned as a
read-only fallback. The primary fallback calls Palworld's reflected
`PalUtility.GetAllPlayerStates` with the current World context. It does not read
inventory or Pal data and never modifies the objects.

`POST /v1/players/online/metadata` reuses the same enumeration and identity reads, but
only returns top-level metadata under `top_level_property_metadata.player_state` and
`.pawn`. Use it to discover candidate field names before a separately reviewed
value-reading change.

Every JSON response includes `response_time_unix_ms` and `response_time_china`.
Jobs also preserve their queue time, game-thread execution time, and tick count
at execution. Online-player results identify the exact World object used for
the PalUtility query.

## 通过 PalPanel 诊断接口查询在线玩家

只读脚本 [`query_online_players.py`](query_online_players.py) 会提交
`POST /v1/players/online`，然后按任务返回的安全 `job_id` 轮询
`GET /v1/jobs/<job_id>`，直到任务完成。进度写入 stderr，完成或失败时的玩家摘要
JSON 写入 stdout；失败、超时、HTTP 错误和 JSON 错误均以非零状态退出。摘要保留
UID、名称、Controller、PlayerState、Pawn、来源、计数、任务时间和游戏线程 tick，
不会输出庞大的属性诊断树；需要完整响应时显式增加 `--full`。
增加 `--metadata` 可提交顶层属性元数据任务；默认紧凑输出会保留元数据计数、截断状态
以及每个已收集玩家的 PlayerState/Pawn 属性元数据。该模式只用于字段发现，不代表已经
读取位置、等级或公会等详细信息。

推荐先在 PowerShell 中设置凭据（也可不设置，脚本会改用隐藏输入）：

```powershell
$env:PALPANEL_API_KEY = "<Panel API Key>"
$env:PALPANEL_BRIDGE_TOKEN = "<Bridge Token>"
python tools/palpanel-bridge/query_online_players.py --pretty
```

使用隐藏输入：

```powershell
Remove-Item Env:PALPANEL_API_KEY, Env:PALPANEL_BRIDGE_TOKEN -ErrorAction SilentlyContinue
python tools/palpanel-bridge/query_online_players.py --panel-url http://play.simpfun.cn:12559
```

可用 `--interval` 和 `--timeout` 覆盖默认的 3 秒轮询间隔与 60 秒总超时，
用 `--full` 输出完整属性与函数候选。
脚本仅使用 Python 标准库，不接受命令行 secret 参数，也不会把凭据写入日志。
默认 Panel URL 是 `http://play.simpfun.cn:12559`。该地址使用公网 HTTP 明文传输，
API Key 可能被窃听；生产使用应改用受保护的 HTTPS/内网通道，并在使用后撤销或轮换凭据。
