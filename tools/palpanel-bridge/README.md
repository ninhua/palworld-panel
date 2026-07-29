# PalPanelBridge read-only probe

This is the first, deliberately read-only UE4SS bridge milestone. It proves:

```text
localhost HTTP -> bounded job queue -> UE4SS on_update game-thread callback
```

It does not expose arbitrary UObject calls and cannot modify players, inventory, pals, or saves.

## Build with GitHub Actions

The official UE4SS v3.0.1 source tree depends on the restricted UEPseudo
repository. The repository owner must link Epic Games and GitHub, accept the
EpicGames organization invitation, and add a read-capable personal access token
as the repository Actions secret `UEPSEUDO_TOKEN`.

Run the `PalPanelBridge build` workflow. It produces
`PalPanelBridge-v0.1.0-ue4ss-v3.0.1.zip`, containing the complete
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

1. Extract `PalPanelBridge-v0.1.0-ue4ss-v3.0.1.zip`.
2. Copy the extracted `PalPanelBridge` directory into
   `Pal/Binaries/Win64/Mods/`.
3. Edit `PalPanelBridge/config.ini` and replace the placeholder token.
4. Restart PalServer and check `UE4SS.log` for `PalPanelBridge`.

The package includes `enabled.txt`, so it does not overwrite the server's
existing `Mods/mods.txt`.

The bridge binds only to `127.0.0.1`. All requests require:

```text
Authorization: Bearer <token>
```

## Probe

```bash
curl -H "Authorization: Bearer <token>" http://127.0.0.1:18082/v1/health
curl -X POST -H "Authorization: Bearer <token>" http://127.0.0.1:18082/v1/probe/game-thread
curl -H "Authorization: Bearer <token>" http://127.0.0.1:18082/v1/jobs/<job_id>
```

Success means the job changes from `queued` to `completed` and reports
`game_thread_tick_seen=true` plus `unreal_initialized=true`.
