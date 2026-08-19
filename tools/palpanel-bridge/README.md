# PalPanelBridge

PalPanelBridge exposes authenticated localhost diagnostics and a bounded mutation queue:

```text
localhost HTTP -> bounded job queue -> UE4SS on_update game-thread callback
```

It does not expose arbitrary UObject calls. Mutation requests require explicit confirmation,
target identity, expected current values, post-write verification, and rollback on mismatch.

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
`PalPanelBridge-v0.1.47-ue4ss-c838a8ac.zip`, containing the complete
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

1. Extract `PalPanelBridge-v0.1.47-ue4ss-c838a8ac.zip`.
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
curl -X POST -H "Authorization: Bearer <token>" http://127.0.0.1:18083/v1/bases/modules
curl -X POST -H "Authorization: Bearer <token>" -H "Content-Type: application/json" \
  --data @mutation.json http://127.0.0.1:18083/v1/mutations
curl -H "Authorization: Bearer <token>" http://127.0.0.1:18083/v1/jobs/<job_id>
```

Success means the job changes from `queued` to `completed` and reports
`game_thread_tick_seen=true` plus `unreal_initialized=true`.

Normal online-player jobs return only stable player data. Large reflection
details are restricted to the dedicated metadata endpoint so routine responses
stay bounded.

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

Version `0.1.26` adds read-only player detail object references. The normal
online-player response now additively reports `guild_name`,
`guild_admin_player_uid`, `base_camp_count`, `inventory_found`/`inventory`
(`PalPlayerInventoryData`) with `inventory_container_count`,
`pal_storage_found`/`pal_storage` (`PalPlayerDataPalStorage`), and
`otomo_found`/`otomo` (`PalPlayerOtomoData`). Only object identity, one string,
one GUID, and bounded collection sizes are read; container contents are not
enumerated. The metadata probe additionally returns
`detail_property_metadata.inventory`, `.pal_storage`, and `.otomo` keyword
property lists (up to 64 per object) and broadens the guild keyword list with
`base`, `camp`, `territory`, and `map` so base-camp fields can be discovered.
As before, matched values, arrays, maps, nested contents, and functions are
never read.

Version `0.1.27` turns the confirmed field names into values. `base_camp_count`
reads the guild's `BaseCampIds` array length and `base_camp_level` reads the
numeric `BaseCampLevel`. Inventory weight is reported as `now_item_weight` and
`max_inventory_weight` when both numeric properties exist. The pal-storage
object reference `pal_container` points at `TargetContainer`
(`PalIndividualCharacterContainer`), and the metadata probe adds
`detail_property_metadata.pal_container` so the container's pal slots can be
discovered next. Only identity, strings, GUIDs, numeric scalars, and bounded
collection sizes are read; pal or item slot contents are still not enumerated.

Version `0.1.28` reads the pal slot array and probes inventory containers. The
online-player response now includes `pal_slot_array` (`found`, `slot_count`,
and up to 10 `slots`; each slot reports `individual_id` and the `Handle`
object reference when present) from `PalIndividualCharacterContainer.SlotArray`,
and `inventory_containers`: a list of the found named containers on
`PalPlayerInventoryData` (`EssentialContainer`, `PlayerInventoryContainer`,
`EquipmentContainer`, `LoadoutContainer`, `ItemContainer`, `InventoryContainer`),
each with its object identity and `slot_count` from a `Slots`/`ItemSlots`
array. On metadata probes, each found item container also returns
`container_property_metadata` so the real slot field names can be confirmed.
Only up to 10 pal slots and a few named containers are inspected; item slot
contents and per-pal details are not read yet.

Version `0.1.29` adds local runtime diagnosis for the confirmed object paths.
The online-player response adds `inventory_helper_found`/`inventory_helper`
(`PalItemContainerMultiHelper`, the real inventory container entry object) and
the metadata probe returns `detail_property_metadata.inventory_helper` and
`detail_property_metadata.pal_slot_object` (the first `PalIndividualCharacterSlot`
object's keyword properties) so the individual-id and item-slot field names can
be confirmed before reading actual pal/item contents.

Version `0.1.30` follows the confirmed runtime paths in one bounded probe.
Inventory containers are read from `InventoryMultiHelper.Containers`; the
response reports each container and its slot-array size. Metadata probes expand
the first container and first slot element. Pal slots now distinguish the slot
object from its real `Handle`, expose `ReplicateHandleID` as bounded raw hex,
and expand `ReplicateIndividualParameter`, handle, ID-struct, and base-camp-ID
metadata. The probe remains read-only and inspects at most 16 item containers
and 10 pal slots.

Version `0.1.31` treats newly connected players as `initializing` until UID,
inventory, and pal storage are available. Inventory slots use the confirmed
`ItemSlotArray` field. Pal details use `CachedNonEmptySlots_InServer` instead
of scanning the first empty entries of the 960-slot box, and `PalInstanceID`
is decoded as its `PlayerUId` and `InstanceId` GUID fields without exposing the
structure's `DebugName` memory.

Version `0.1.32` returns up to 32 `PalItemSlot` objects per inventory
container with their numeric `SlotIndex` and `StackCount`. Metadata probes also
expand the nested `PalItemId` structure once, allowing the next read step to
decode the real static/dynamic item identifier without guessing field names.
For valid pal parameters it also attempts bounded reads of nickname, level,
rank, experience, HP, max HP, fullness, and sanity, while returning a filtered
data-property metadata list instead of delegate-heavy class metadata.

Version `0.1.33` fixes HTTP responses larger than one Winsock send buffer by
looping until the complete response is transmitted. Routine player responses
no longer include legacy `property_candidates`/`property_details`; inventory
slots omit redundant UObject names, and metadata discovery stays on the
dedicated endpoint. The panel query helper accepts diagnostic responses up to
2 MiB.

Version `0.1.34` reads each bounded inventory slot's `PalItemId.StaticId` and
uses confirmed read-only Pal functions to return `character_id`, `level`,
`passive_skill_ids`, and numeric `equipped_waza_ids` for each valid Pal
parameter. Function results are range-bounded and no mutation function is
called. Each call first verifies the reflected return type, parameter-buffer
size, and enum element width; mismatches fail closed without invoking it.

Version `0.1.35` adds three allowlisted game-thread mutation operations through
`POST /v1/mutations`: `item_set_count`, `pal_replace_passive`, and
`pal_set_stats` (`Level`, `Talent_HP`, `Talent_Shot`, `Talent_Defense`). Every
request requires `confirm=true`, an online `player_uid`, the exact current
item/Pal identity, and expected current values. Writes are immediately reread;
verification failure triggers an inverse write or snapshot restore and reports
`rolled_back` or `rollback_failed`. Item creation/removal remains on the panel's
existing audited PalDefender API. Back up the world save before any accepted
mutation; DLL deployment backup does not back up save data.

Version `0.1.36` accepts the all-zero owner GUID used by live Pal storage slots.
The slot remains scoped through the selected online player's own `PalStorage`,
and mutation still requires the exact Pal instance ID, character ID, and current
value/list. Any other non-matching owner GUID remains rejected.

Version `0.1.37` adds `party_pal_slots` to online-player results and resolves
carried Pals through the target player's `PalOtomoHolderComponentBase`, bounded
party slots, handle, parameter, and instance ID. Pal mutations now require an
explicit `pal_scope` of `party` or `storage`; the two locations never fall back
to each other. A currently spawned party Pal is rejected until recalled.
Direct `StackCount` writes remain runtime-only; persistent item add/remove stays
on the game's native/audited management paths.

Version `0.1.38` adds the read-only `POST /v1/bases/modules` game-thread probe.
It resolves `PalBaseCampManager`, bounded base IDs/models, and each model's
bounded `ModuleArray`, then reports module identity plus only work/task/facility/
assignment-related property and function ABI metadata. It never invokes unknown
module functions and does not change production tasks or worker assignments.

Version `0.1.39` extends that probe with bounded collection counts and Map key/
value schemas. This identifies the concrete types behind work-info and facility-
usage maps before any entry traversal or server function call is implemented.

Version `0.1.40` expands one bounded level of reflected array/struct fields and
serializes the resulting metadata tree with a fixed depth limit. It identifies
the fields inside `PalBaseCampModuleResourceCollectWorkInfo` without reading Map
entry values or invoking work functions.

Version `0.1.41` reads the strictly validated resource-work Map entries as
`map_object_id`/`work_id` pairs and reports metadata for bounded, loaded
`PalWorkBase` class exemplars. It remains read-only: no facility-use, production,
or worker-assignment function is invoked.

Version `0.1.42` reads every bounded loaded `PalWorkBase` instance's validated
`GetWorkId`, links matching facility object IDs, then calls only the confirmed read-only assignment getters.
It reports assignment counts plus reflected `PalWorkAssignInfo` and assigned-slot
metadata; it still invokes no assignment or production setter.

Version `0.1.43` limits returned work objects to WorkIds referenced by the current
base's resource-work Map, preventing unrelated world jobs from overflowing the
panel diagnostic response.

Version `0.1.44` additionally retains unmatched work objects only when a read-only
assignment getter reports a non-zero assignment or assigned-character count. This
discovers active facility jobs without restoring the oversized world-wide output.

Version `0.1.45` adds a bounded, read-only loaded-object search for base-camp
worker, assignment, facility, and `PalWorkAssign` candidates. It returns only
identity plus relevant reflected metadata and never calls candidate functions.

Version `0.1.46` excludes reflected class definitions and `Default__` class
defaults from that search so the bounded result contains runtime instances.

Version `0.1.47` further requires candidate full names to belong to `/Game/`,
excluding `/Script` enum, function, delegate, and struct definitions.

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

For a panel-managed local Windows server, pass `--local-dll` together with
`--panel-url`, `--panel-api-key`, and `--bridge-token`. The same
helper waits for CI and verifies the artifact, asks the panel to stop the game,
atomically replaces only `dlls/main.dll`, restores the old DLL on failure,
starts the game, and waits for `/v1/health` to report the expected version.
`config.ini` is never modified. Secrets may also be supplied through
`PALPANEL_API_KEY` and `PALPANEL_BRIDGE_TOKEN` environment variables.
Local deployment uses the panel's safe-stop job, not force-stop. Before stopping
and again before starting, it requires a readable `GameUserSettings.ini`, a
non-empty `DedicatedServerName`, and an existing bound `Level.sav`. If the
binding cannot be proven, the server remains stopped instead of creating a new
world.
On local Windows servers that rewrite only `GameUserSettings.ini` with an empty
DACL during safe stop, the helper restores inheritance on that exact file and
then revalidates the unchanged world ID and non-empty `Level.sav`. It never
changes the INI contents or broad directory permissions.
Use `--download-only` when only CI waiting, artifact verification, and the
versioned snapshot are needed; this mode never contacts the panel or changes
the running server. The workflow derives the package version from
`ModVersion` in `src/main.cpp` so artifact names cannot silently lag behind.

Calling `/v1/runtime` twice should show an increasing
`game_thread_tick_count`. It also reports the last game-thread tick time and
bridge uptime without modifying game state.

`POST /v1/world` queues a game-thread-safe UE object lookup. Poll its returned
job ID through `/v1/jobs/<job_id>`; a successful result reports the current
World object's name, full name, and class without changing the object.

`POST /v1/players/online` enumerates live `PalPlayerController` instances on
the game thread and returns the associated PlayerState identity, Pawn, location,
guild/base summary, inventory containers and stack counts, and bounded Pal-box
slot details. Native and blueprint controller class names are both checked;
when no controller is visible, live PlayerState objects are returned as a
read-only fallback. The endpoint never modifies the objects.

`POST /v1/players/online/metadata` reuses the same enumeration and returns bounded
`detail_property_metadata` for the confirmed guild, inventory, item-slot, Pal storage,
Pal handle, Pal parameter, and related structures. Use it to discover candidate field
names before a separately reviewed value-reading change.

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
