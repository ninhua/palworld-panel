# PalPanelBridge read-only probe

This is the first, deliberately read-only UE4SS bridge milestone. It proves:

```text
localhost HTTP -> bounded job queue -> UE4SS on_update game-thread callback
```

It does not expose arbitrary UObject calls and cannot modify players, inventory, pals, or saves.

## Install the Action artifact

1. Copy `PalPanelBridge` into `Pal/Binaries/Win64/Mods/`.
2. Rename `config.ini.example` to `config.ini` and replace the token.
3. Add `PalPanelBridge : 1` to `Pal/Binaries/Win64/Mods/mods.txt`.
4. Restart PalServer and check `UE4SS.log` for `PalPanelBridge`.

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
