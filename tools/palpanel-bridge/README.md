# PalPanelBridge read-only probe

This is the first, deliberately read-only UE4SS bridge milestone. It proves:

```text
localhost HTTP -> bounded job queue -> UE4SS on_update game-thread callback
```

It does not expose arbitrary UObject calls and cannot modify players, inventory, pals, or saves.

For the Chinese installation, diagnostic-console examples, and troubleshooting
matrix, see [`docs/palpanel-bridge.md`](../../docs/palpanel-bridge.md).

## Build with GitHub Actions

The Palworld experimental UE4SS build used by the panel reports Git SHA
`c838a8ac`. C++ mods must use the same UE4SS commit, build configuration, and
C runtime. That source tree depends on the restricted UEPseudo repository. The
repository owner must link Epic Games and GitHub, accept the EpicGames
organization invitation, and add a read-capable personal access token as the
repository Actions secret `UEPSEUDO_TOKEN`.

Run the `PalPanelBridge build` workflow. It produces
`PalPanelBridge-v0.1.16-ue4ss-c838a8ac.zip`, containing the complete
`PalPanelBridge` mod directory, configuration, documentation, license, and
SHA-256 checksum. The token used to fetch the SDK is not included in the
package.

## Optional local build

Run from an MSVC developer shell with CMake, Ninja, and Rust available:

```powershell
./build-local.ps1 -UE4SSRoot C:\src\RE-UE4SS-v3.0.1
```

The DLL is written to:

```text
build/artifact/PalPanelBridge/dlls/main.dll
```

## Install the server package

1. Extract `PalPanelBridge-v0.1.16-ue4ss-c838a8ac.zip`.
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

Every JSON response includes `response_time_unix_ms` and `response_time_utc`.
Jobs also preserve their queue time, game-thread execution time, and tick count
at execution. Online-player results identify the exact World object used for
the PalUtility query.
