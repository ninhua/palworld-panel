#!/usr/bin/env python3
"""Query PalPanelBridge online players through PalPanel's HTTP diagnostics API."""

from __future__ import annotations

import argparse
import getpass
import json
import os
import re
import sys
import time
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

DEFAULT_PANEL_URL = "http://play.simpfun.cn:12559"
DIAGNOSTIC_PATH = "/api/system/diagnostics/http"
BRIDGE_PLAYERS_URL = "http://127.0.0.1:18083/v1/players/online"
BRIDGE_METADATA_URL = "http://127.0.0.1:18083/v1/players/online/metadata"
BRIDGE_JOB_URL_PREFIX = "http://127.0.0.1:18083/v1/jobs/"
JOB_ID_PATTERN = re.compile(r"^[A-Za-z0-9_-]{1,128}$")


class ProbeError(Exception):
    """An expected probe failure safe to report without request headers."""


def read_secret(name: str, prompt: str) -> str:
    value = os.environ.get(name)
    if value:
        return value
    try:
        value = getpass.getpass(prompt)
    except (EOFError, KeyboardInterrupt) as exc:
        raise ProbeError(f"未能读取 {name}") from exc
    if not value:
        raise ProbeError(f"未提供 {name}")
    return value


def json_object(raw: bytes, description: str) -> dict[str, Any]:
    try:
        value = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ProbeError(f"{description}不是有效 JSON") from exc
    if not isinstance(value, dict):
        raise ProbeError(f"{description}必须是 JSON 对象")
    return value


def diagnostic_url(panel_url: str) -> str:
    return panel_url.rstrip("/") + DIAGNOSTIC_PATH


def diagnostic_request(panel_url: str, panel_key: str, method: str, url: str, bridge_token: str) -> dict[str, Any]:
    payload = {
        "method": method,
        "url": url,
        "headers": {"Authorization": f"Bearer {bridge_token}"},
        "body": "" if method == "GET" else "{}",
    }
    request = Request(
        diagnostic_url(panel_url),
        data=json.dumps(payload).encode("utf-8"),
        headers={"Authorization": f"Bearer {panel_key}", "Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urlopen(request, timeout=15) as response:
            raw = response.read(64 * 1024 + 1)
            status = response.status
    except HTTPError as exc:
        raise ProbeError(f"Panel HTTP 错误：{exc.code}") from exc
    except URLError as exc:
        raise ProbeError(f"Panel 请求失败：{type(exc.reason).__name__}") from exc
    except (TimeoutError, OSError) as exc:
        raise ProbeError(f"Panel 请求失败：{type(exc).__name__}") from exc
    if len(raw) > 64 * 1024:
        raise ProbeError("Panel 响应超过 64 KiB")
    if status < 200 or status >= 300:
        raise ProbeError(f"Panel HTTP 状态异常：{status}")
    return json_object(raw, "Panel 响应")


def nested_body(response: dict[str, Any], description: str) -> dict[str, Any]:
    if response.get("ok") is not True:
        raise ProbeError(f"{description}的 Panel 状态不是成功")
    data = response.get("data")
    if not isinstance(data, dict):
        raise ProbeError(f"{description}缺少 data 对象")
    status_code = data.get("status_code")
    if not isinstance(status_code, int) or not 200 <= status_code < 300:
        raise ProbeError(f"{description}的 Bridge HTTP 状态异常：{status_code!s}")
    if not isinstance(data.get("body"), str):
        raise ProbeError(f"{description}缺少字符串 data.body")
    return json_object(data["body"].encode("utf-8"), f"{description}的 data.body")


def get_job_id(submission: dict[str, Any]) -> str:
    job_id = nested_body(submission, "提交响应").get("job_id")
    if not isinstance(job_id, str) or not JOB_ID_PATTERN.fullmatch(job_id):
        raise ProbeError("提交响应包含无效 job_id")
    return job_id


def get_job(response: dict[str, Any]) -> dict[str, Any]:
    job = nested_body(response, "任务响应").get("job")
    if not isinstance(job, dict):
        raise ProbeError("任务响应缺少 job 对象")
    return job


def report_progress(message: str) -> None:
    print(message, file=sys.stderr, flush=True)


def compact_job(job: dict[str, Any]) -> dict[str, Any]:
    result = job.get("result") if isinstance(job.get("result"), dict) else {}
    players = result.get("players") if isinstance(result.get("players"), list) else []
    compact_players = []
    for player in players:
        if not isinstance(player, dict):
            continue
        compact_player = {
            "source": player.get("source"),
            "account_name": player.get("account_name"),
            "player_uid": player.get("player_uid"),
            "identity_error": player.get("identity_error"),
            "controller": {
                "name": player.get("name"),
                "full_name": player.get("full_name"),
                "class_name": player.get("class_name"),
            },
            "player_state_found": player.get("player_state_found"),
            "player_state": player.get("player_state"),
            "pawn_found": player.get("pawn_found"),
            "pawn": player.get("pawn"),
            "cached_location_found": player.get("cached_location_found"),
            "cached_location": player.get("cached_location"),
            "cached_location_error": player.get("cached_location_error"),
            "guild_found": player.get("guild_found"),
            "guild": player.get("guild"),
            "guild_name": player.get("guild_name"),
            "guild_admin_player_uid": player.get("guild_admin_player_uid"),
            "base_camp_count": player.get("base_camp_count"),
            "base_camp_level_found": player.get("base_camp_level_found"),
            "base_camp_level": player.get("base_camp_level"),
            "inventory_found": player.get("inventory_found"),
            "inventory": player.get("inventory"),
            "inventory_container_count": player.get("inventory_container_count"),
            "inventory_helper_found": player.get("inventory_helper_found"),
            "inventory_helper": player.get("inventory_helper"),
            "inventory_weight_found": player.get("inventory_weight_found"),
            "now_item_weight": player.get("now_item_weight"),
            "max_inventory_weight": player.get("max_inventory_weight"),
            "pal_storage_found": player.get("pal_storage_found"),
            "pal_storage": player.get("pal_storage"),
            "pal_container_found": player.get("pal_container_found"),
            "pal_container": player.get("pal_container"),
            "pal_slot_array": player.get("pal_slot_array"),
            "otomo_found": player.get("otomo_found"),
            "otomo": player.get("otomo"),
            "inventory_containers": player.get("inventory_containers"),
            "character_parameter_found": player.get("character_parameter_found"),
            "character_parameter": player.get("character_parameter"),
        }
        if "top_level_property_metadata" in player:
            compact_player["top_level_property_metadata"] = player.get("top_level_property_metadata")
        if "detail_property_metadata" in player:
            compact_player["detail_property_metadata"] = player.get("detail_property_metadata")
        compact_players.append(compact_player)
    compact_result = {
        "unreal_initialized": result.get("unreal_initialized"),
        "game_thread_tick_seen": result.get("game_thread_tick_seen"),
        "controller_object_count": result.get("controller_object_count"),
        "player_state_object_count": result.get("player_state_object_count"),
        "pal_utility_available": result.get("pal_utility_available"),
        "pal_utility_player_state_count": result.get("pal_utility_player_state_count"),
        "pal_utility_error": result.get("pal_utility_error"),
        "query_world_found": result.get("query_world_found"),
        "query_world": result.get("query_world"),
        "game_state_found": result.get("game_state_found"),
        "game_state_player_array_available": result.get("game_state_player_array_available"),
        "game_state_player_state_count": result.get("game_state_player_state_count"),
        "game_state_error": result.get("game_state_error"),
        "online_player_count": result.get("online_player_count"),
        "players": compact_players,
    }
    if result.get("metadata_probe") is True:
        compact_result.update(
            {
                "metadata_probe": True,
                "metadata_player_limit": result.get("metadata_player_limit"),
                "metadata_player_count": result.get("metadata_player_count"),
                "metadata_truncated": result.get("metadata_truncated"),
            }
        )
    return {
        "id": job.get("id"),
        "type": job.get("type"),
        "status": job.get("status"),
        "queued_at_unix_ms": job.get("queued_at_unix_ms"),
        "queued_at_china": job.get("queued_at_china"),
        "executed_at_unix_ms": job.get("executed_at_unix_ms"),
        "executed_at_china": job.get("executed_at_china"),
        "game_thread_tick_count_at_execution": job.get("game_thread_tick_count_at_execution"),
        "result": compact_result,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description="通过 PalPanel 诊断接口查询 PalPanelBridge 在线玩家")
    parser.add_argument("--panel-url", default=DEFAULT_PANEL_URL, help=f"Panel 地址（默认：{DEFAULT_PANEL_URL}）")
    parser.add_argument("--interval", type=float, default=3.0, help="轮询间隔秒数（默认：3）")
    parser.add_argument("--timeout", type=float, default=60.0, help="总超时秒数（默认：60）")
    parser.add_argument("--pretty", action="store_true", help="格式化最终 JSON")
    parser.add_argument("--full", action="store_true", help="输出完整属性诊断树，而不是默认玩家摘要")
    parser.add_argument("--metadata", action="store_true", help="查询在线玩家顶层属性元数据")
    args = parser.parse_args()
    if args.interval <= 0 or args.timeout <= 0:
        parser.error("--interval 和 --timeout 必须大于 0")

    try:
        panel_key = read_secret("PALPANEL_API_KEY", "Panel API Key（隐藏输入）：")
        bridge_token = read_secret("PALPANEL_BRIDGE_TOKEN", "Bridge Token（隐藏输入）：")
        report_progress("提交在线玩家元数据查询任务…" if args.metadata else "提交在线玩家查询任务…")
        bridge_url = BRIDGE_METADATA_URL if args.metadata else BRIDGE_PLAYERS_URL
        submission = diagnostic_request(args.panel_url, panel_key, "POST", bridge_url, bridge_token)
        job_id = get_job_id(submission)
        report_progress(f"任务已提交：{job_id}；等待完成…")
        deadline = time.monotonic() + args.timeout
        job: dict[str, Any] | None = None
        while True:
            if job is not None and job.get("status") in {"completed", "failed"}:
                break
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise ProbeError("轮询超时")
            if job is not None:
                time.sleep(min(args.interval, remaining))
            job = get_job(
                diagnostic_request(args.panel_url, panel_key, "GET", BRIDGE_JOB_URL_PREFIX + job_id, bridge_token)
            )
            report_progress(f"任务状态：{job.get('status')!s}")
        output = job if args.full else compact_job(job)
        print(json.dumps(output, ensure_ascii=False, indent=2 if args.pretty else None))
        return 0 if job.get("status") == "completed" else 1
    except ProbeError as exc:
        print(f"错误：{exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
