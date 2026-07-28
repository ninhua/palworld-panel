from __future__ import annotations

import asyncio
import json
import secrets
from datetime import datetime
from pathlib import Path
from urllib.parse import quote
from zoneinfo import ZoneInfo

from aiohttp import ClientSession, web
from astrbot.api import AstrBotConfig, logger
from astrbot.api.event import AstrMessageEvent, filter
from astrbot.api.star import Context, Star
from astrbot.core.utils.astrbot_path import get_astrbot_data_path

from .security import body_bytes, signed_headers, validate_http_service_url, verify_headers
from .storage import PalPanelStore
from .operations import (
    admin_allowed,
    CooldownGuard,
    format_online_players,
    format_rooms,
    format_server_status,
    group_allowed,
    parse_wait_seconds,
)


class Main(Star):
    def __init__(self, context: Context, config: AstrBotConfig):
        super().__init__(context)
        self.config = config
        data_dir = Path(get_astrbot_data_path()) / "plugin_data" / "astrbot_plugin_palpanel"
        self.store = PalPanelStore(data_dir / "palpanel.sqlite3")
        self.http: ClientSession | None = None
        self.runner: web.AppRunner | None = None
        self.nonces: dict[str, float] = {}
        self.cooldowns = CooldownGuard(
            query_seconds=max(0, float(self.config.get("query_cooldown_seconds", 5))),
            control_seconds=max(0, float(self.config.get("control_cooldown_seconds", 15))),
        )

    @filter.on_astrbot_loaded()
    async def loaded(self):
        await self.store.initialize()
        self.http = ClientSession()
        app = web.Application(client_max_size=4 * 1024 * 1024)
        app.middlewares.append(self._auth_middleware)
        app.router.add_get("/v1/health", self._health)
        app.router.add_post("/v1/catalog/sync", self._catalog_sync)
        app.router.add_post("/v1/tickets/exchange", self._ticket_exchange)
        app.router.add_post("/v1/credits/reserve", self._credit_reserve)
        app.router.add_post("/v1/credits/commit", self._credit_commit)
        app.router.add_post("/v1/credits/release", self._credit_release)
        self.runner = web.AppRunner(app)
        await self.runner.setup()
        await web.TCPSite(self.runner, str(self.config.get("listen_host", "127.0.0.1")), int(self.config.get("listen_port", 8092))).start()
        logger.info("PalPanel plugin API listening on %s:%s", self.config.get("listen_host", "127.0.0.1"), self.config.get("listen_port", 8092))

    async def terminate(self):
        if self.runner:
            await self.runner.cleanup()
        if self.http:
            await self.http.close()

    def _allowed(self, event: AstrMessageEvent) -> bool:
        return group_allowed(self.config.get("allowed_group_id", ""), event.get_group_id())

    def _is_admin(self, event: AstrMessageEvent) -> bool:
        return admin_allowed(self.config.get("admin_qq_ids", ""), event.get_sender_id())

    def _admin_ids(self) -> set[str]:
        return {item.strip() for item in str(self.config.get("admin_qq_ids", "")).split(",") if item.strip()}

    def _command_retry_after(self, event: AstrMessageEvent, action: str, *, control: bool = False) -> int:
        return self.cooldowns.retry_after(
            str(event.get_group_id() or "private"), event.get_sender_id(), action, control=control
        )

    async def _server_status_payload(self) -> dict:
        return await self._panel_post("/api/integrations/astrbot/server-status", {})

    async def _server_control(self, event: AstrMessageEvent, action: str, waittime: int = 60) -> dict:
        return await self._panel_post("/api/integrations/astrbot/server-control", {
            "actor_qq_id": event.get_sender_id(),
            "group_id": str(event.get_group_id() or ""),
            "action": action,
            "waittime": waittime,
            "message": "服务器将在倒计时结束后进行维护，请尽快前往安全地点。",
        })

    @filter.command("服状态", alias={"serverstatus", "服务器状态"})
    async def server_status_command(self, event: AstrMessageEvent):
        """查询 Palworld 服务器运行状态。"""
        if not self._allowed(event):
            return
        retry = self._command_retry_after(event, "server_status")
        if retry:
            yield event.plain_result(f"查询太频繁，请 {retry} 秒后再试。")
            return
        try:
            yield event.plain_result(format_server_status(await self._server_status_payload()))
        except Exception as exc:
            logger.warning("server status command failed: %s", exc)
            yield event.plain_result("暂时无法查询服务器状态，请稍后重试。")

    @filter.command("在线", alias={"online", "在线玩家"})
    async def online_command(self, event: AstrMessageEvent):
        """查询当前在线玩家。"""
        if not self._allowed(event):
            return
        retry = self._command_retry_after(event, "online")
        if retry:
            yield event.plain_result(f"查询太频繁，请 {retry} 秒后再试。")
            return
        try:
            maximum = max(200, int(self.config.get("output_max_chars", 1800)))
            yield event.plain_result(format_online_players(await self._server_status_payload(), maximum))
        except Exception as exc:
            logger.warning("online players command failed: %s", exc)
            yield event.plain_result("暂时无法查询在线玩家，请稍后重试。")

    @filter.command("房间", alias={"rooms", "社区服"})
    async def rooms_command(self, event: AstrMessageEvent, query: str = ""):
        """查询可发现社区服务器：/房间 [关键词]。"""
        if not self._allowed(event):
            return
        retry = self._command_retry_after(event, "rooms")
        if retry:
            yield event.plain_result(f"查询太频繁，请 {retry} 秒后再试。")
            return
        try:
            limit = min(10, max(1, int(self.config.get("max_room_results", 10))))
            maximum = max(200, int(self.config.get("output_max_chars", 1800)))
            payload = await self._panel_post("/api/integrations/astrbot/community-servers", {
                "query": query.strip(), "limit": limit, "country": "CN",
            })
            yield event.plain_result(format_rooms(payload, limit, maximum))
        except Exception as exc:
            logger.warning("community rooms command failed: %s", exc)
            yield event.plain_result("社区服务器列表暂时不可用，请稍后重试。")

    async def _run_control_command(self, event: AstrMessageEvent, action: str, wait: str | int | None = None):
        if not self._allowed(event):
            yield event.plain_result("该命令只能在配置的 QQ 群中使用。")
            return
        if not self._is_admin(event):
            yield event.plain_result("你没有服务器控制权限。")
            return
        try:
            waittime = parse_wait_seconds(wait, 60)
        except ValueError as exc:
            yield event.plain_result(str(exc))
            return
        retry = self._command_retry_after(event, "server_control", control=True)
        if retry:
            yield event.plain_result(f"控制操作冷却中，请 {retry} 秒后再试。")
            return
        try:
            payload = await self._server_control(event, action, waittime)
            data = payload.get("data", payload)
            job = data if isinstance(data, dict) else {}
            job_id = job.get("id") or (job.get("job", {}) if isinstance(job.get("job"), dict) else {}).get("id")
            labels = {"start": "开服", "safe_stop": "安全关服", "safe_restart": "安全重启", "force_stop": "强制关服"}
            suffix = f"，任务 ID：{job_id}" if job_id else ""
            yield event.plain_result(f"{labels[action]}操作已接受{suffix}。")
        except Exception as exc:
            logger.warning("server control command %s failed: %s", action, exc)
            yield event.plain_result("服务器控制操作失败，请检查 PalPanel 状态和审计日志。")

    @filter.command("开服", alias={"serverstart"})
    async def start_server_command(self, event: AstrMessageEvent):
        """管理员启动服务器。"""
        async for result in self._run_control_command(event, "start", 60):
            yield result

    @filter.command("关服", alias={"serverstop"})
    async def stop_server_command(self, event: AstrMessageEvent, wait: str = "60"):
        """管理员安全关服：/关服 [5-300 秒]。"""
        async for result in self._run_control_command(event, "safe_stop", wait):
            yield result

    @filter.command("重启", alias={"serverrestart"})
    async def restart_server_command(self, event: AstrMessageEvent, wait: str = "60"):
        """管理员安全重启：/重启 [5-300 秒]。"""
        async for result in self._run_control_command(event, "safe_restart", wait):
            yield result

    @filter.command("强关", alias={"serverforcestop"})
    async def force_stop_server_command(self, event: AstrMessageEvent):
        """管理员强制停止服务器，可能造成未保存进度丢失。"""
        async for result in self._run_control_command(event, "force_stop", 60):
            yield result

    @filter.command("bd", alias={"绑定"})
    async def bind(self, event: AstrMessageEvent, nickname: str):
        """绑定游戏角色：/bd 游戏昵称"""
        if not self._allowed(event):
            yield event.plain_result("该命令只能在配置的 QQ 群中使用。")
            return
        players = await self.store.player_by_nickname(nickname)
        if len(players) != 1:
            yield event.plain_result("没有找到唯一匹配的游戏昵称，请确认存档已同步且昵称完全一致。")
            return
        player = players[0]
        if not player["online"]:
            yield event.plain_result("该角色当前不在线，无法发送游戏内验证码。")
            return
        code = f"{secrets.randbelow(1_000_000):06d}"
        await self.store.create_challenge(event.get_sender_id(), player["player_uid"], player["nickname"], code)
        try:
            await self._panel_post("/api/integrations/astrbot/binding-challenges", {
                "player_uid": player["player_uid"], "nickname": player["nickname"],
                "message": f"PalPanel QQ 绑定验证码：{code}（5 分钟内在群里发送 /bdqr {code}）",
            })
        except Exception as exc:
            logger.warning("failed to send binding challenge: %s", exc)
            yield event.plain_result("PalDefender 暂时无法发送验证码，请稍后重试。")
            return
        yield event.plain_result("验证码已通过 PalDefender 私发到游戏内，请在 5 分钟内发送 /bdqr 验证码。")

    @filter.command("bdqr", alias={"绑定确认"})
    async def bind_confirm(self, event: AstrMessageEvent, code: str):
        """确认游戏内验证码：/bdqr 123456"""
        result = await self.store.confirm_challenge(event.get_sender_id(), code.strip())
        if not result:
            yield event.plain_result("验证码无效或已过期。")
            return
        yield event.plain_result(f"绑定成功：{result['nickname']}。现在可以签到并使用配种计算。")

    @filter.command("qd", alias={"签到"})
    async def checkin(self, event: AstrMessageEvent):
        """每日签到领取积分"""
        if not self._allowed(event):
            return
        timezone = ZoneInfo(str(self.config.get("timezone", "Asia/Shanghai")))
        local_date = datetime.now(timezone).date().isoformat()
        awarded, balance = await self.store.checkin(event.get_sender_id(), local_date, max(0, int(self.config.get("daily_points", 10))))
        yield event.plain_result(f"{'签到成功' if awarded else '今天已经签到过了'}，当前积分：{balance}。")

    @filter.command("jf", alias={"积分"})
    async def points(self, event: AstrMessageEvent):
        """查看积分"""
        binding = await self.store.binding(event.get_sender_id())
        balance = await self.store.balance(event.get_sender_id())
        bind_text = f"已绑定 {binding['nickname']}" if binding else "尚未绑定角色"
        yield event.plain_result(f"{bind_text}，当前积分：{balance}。")

    @filter.command("pz", alias={"配种"})
    async def breeding(self, event: AstrMessageEvent, query: str = ""):
        """打开配种面板；可附带目标帕鲁作为快捷搜索"""
        binding = await self.store.binding(event.get_sender_id())
        if not binding or binding.get("status") != "active":
            yield event.plain_result("请先使用 /bd 游戏昵称 完成绑定。")
            return
        summary = ""
        if query.strip():
            parts = query.strip().split()
            target, passives = parts[0], parts[1:5]
            try:
                submitted = await self._panel_post("/api/integrations/astrbot/quick-solves", {
                    "qq_id": event.get_sender_id(), "player_uid": binding["player_uid"],
                    "target": target, "passives": passives,
                })
                job = submitted.get("data", submitted).get("job", {})
                job_id = str(job.get("id", ""))
                if not job_id:
                    raise RuntimeError("PalPanel did not return a job id")
                status_payload = {}
                for _ in range(max(1, int(self.config.get("quick_solve_timeout_seconds", 300)) // 2)):
                    await asyncio.sleep(2)
                    status_payload = await self._panel_post("/api/integrations/astrbot/quick-solves", {
                        "qq_id": event.get_sender_id(), "job_id": job_id,
                    })
                    current = status_payload.get("data", status_payload)
                    status = str(current.get("job", {}).get("status", ""))
                    if status in {"completed", "failed", "canceled"}:
                        break
                current = status_payload.get("data", status_payload)
                if str(current.get("job", {}).get("status", "")) != "completed":
                    error = current.get("job", {}).get("error") or "计算失败或超时，已自动退还预留积分"
                    yield event.plain_result(str(error))
                    return
                results = current.get("result", {}).get("results", [])
                if results:
                    best = results[0]
                    minutes = max(1, round(float(best.get("effort_seconds", 0)) / 60))
                    summary = f"最优路线：{best.get('pal_name', target)}，{best.get('breeding_steps', 0)} 步，约 {best.get('eggs', 0)} 枚蛋 / {minutes} 分钟。\n"
                else:
                    summary = "计算完成，但当前帕鲁来源中没有可行路线。\n"
            except Exception as exc:
                logger.warning("quick breeding solve failed: %s", exc)
                yield event.plain_result("快捷配种计算失败；若已预留积分，面板会自动退款。")
                return

        token = await self.store.issue_ticket(event.get_sender_id(), max(60, int(self.config.get("ticket_ttl_seconds", 300))))
        base = str(self.config.get("panel_public_url", self.config.get("panel_url", ""))).rstrip("/")
        link = f"{base}/breeding?ticket={quote(token)}"
        if query.strip():
            link += f"&quick={quote(query.strip())}"
        yield event.plain_result(f"{summary}配种实验室：{link}\n链接仅可使用一次，并将在 5 分钟后失效。")

    @filter.command("paladmin", alias={"帕鲁管理"})
    async def admin(self, event: AstrMessageEvent, action: str, arguments: str = ""):
        """PalPanel 插件管理：解绑/冻结/绑定/积分/流水"""
        if not self._is_admin(event):
            yield event.plain_result("你没有 PalPanel 插件管理权限。")
            return
        parts = arguments.strip().split()
        actor = f"qq:{event.get_sender_id()}"
        try:
            if action in {"解绑", "unbind"} and len(parts) == 1:
                changed = await self.store.set_binding_status(actor, parts[0], "unbound")
                yield event.plain_result("解绑完成。" if changed else "未找到绑定。")
            elif action in {"冻结", "freeze"} and len(parts) == 1:
                changed = await self.store.set_binding_status(actor, parts[0], "frozen")
                yield event.plain_result("冻结完成。" if changed else "未找到绑定。")
            elif action in {"绑定", "bind"} and len(parts) >= 3:
                await self.store.admin_binding(actor, parts[0], parts[1], " ".join(parts[2:]))
                yield event.plain_result("人工绑定完成，操作已写入审计。")
            elif action in {"积分", "credits"} and len(parts) >= 2:
                balance = await self.store.adjust_points(actor, parts[0], int(parts[1]), " ".join(parts[2:]) or "admin_adjustment")
                yield event.plain_result(f"积分调整完成，当前余额：{balance}。")
            elif action in {"流水", "ledger"} and len(parts) == 1:
                rows = await self.store.ledger(parts[0])
                detail = "\n".join(f"{item['delta']:+d} {item['reason']}" for item in rows) or "暂无流水"
                yield event.plain_result(detail)
            else:
                yield event.plain_result("用法：/paladmin 解绑 QQ；冻结 QQ；绑定 QQ PlayerUID 昵称；积分 QQ 增量 原因；流水 QQ")
        except Exception as exc:
            logger.warning("PalPanel admin command failed: %s", exc)
            yield event.plain_result("管理操作失败，请检查参数和插件日志。")

    @web.middleware
    async def _auth_middleware(self, request: web.Request, handler):
        if request.path == "/v1/health":
            return await handler(request)
        body = await request.read()
        request["raw_body"] = body
        expected_panel = str(self.config.get("panel_id", "palpanel"))
        ok, nonce = verify_headers(str(self.config.get("shared_secret", "")), request.method, request.path, request.headers, body)
        ok = ok and request.headers.get("X-PalPanel-Id", "") == expected_panel
        if not ok or nonce in self.nonces:
            raise web.HTTPUnauthorized(text="invalid signature")
        self.nonces[nonce] = asyncio.get_running_loop().time()
        cutoff = asyncio.get_running_loop().time() - 120
        self.nonces = {key: value for key, value in self.nonces.items() if value >= cutoff}
        return await handler(request)

    async def _json(self, request: web.Request) -> dict:
        return json.loads((request.get("raw_body") or b"{}").decode("utf-8"))

    async def _health(self, _request: web.Request):
        return web.json_response({"status": "ok", "plugin": "astrbot_plugin_palpanel", "version": "0.2.0"})

    async def _catalog_sync(self, request: web.Request):
        data = await self._json(request)
        await self.store.sync_catalog(list(data.get("players", [])), str(data.get("fingerprint", "")))
        return web.json_response({"ok": True, "count": len(data.get("players", []))})

    async def _ticket_exchange(self, request: web.Request):
        data = await self._json(request)
        result = await self.store.exchange_ticket(str(data.get("ticket", "")))
        if not result:
            raise web.HTTPUnauthorized(text="invalid ticket")
        return web.json_response(result)

    async def _credit_reserve(self, request: web.Request):
        data = await self._json(request)
        qq_id = str(data["qq_id"])
        if qq_id in self._admin_ids():
            return web.json_response({"ok": True, "reservation_id": f"admin:{data['reference_id']}", "balance": await self.store.balance(qq_id)})
        ok, reservation, balance = await self.store.reserve(qq_id, str(data["reference_id"]), int(data.get("amount", self.config.get("solve_cost", 1))))
        return web.json_response({"ok": ok, "reservation_id": reservation, "balance": balance}, status=200 if ok else 409)

    async def _credit_commit(self, request: web.Request):
        return await self._settle_credit(request, commit=True)

    async def _credit_release(self, request: web.Request):
        return await self._settle_credit(request, commit=False)

    async def _settle_credit(self, request: web.Request, commit: bool):
        data = await self._json(request)
        reservation_id = str(data["reservation_id"])
        if reservation_id.startswith("admin:"):
            return web.json_response({"ok": True})
        return web.json_response({"ok": await self.store.settle(reservation_id, commit)})

    async def _panel_post(self, path: str, payload: dict) -> dict:
        if not self.http:
            raise RuntimeError("plugin is not initialized")
        base_url = validate_http_service_url(
            str(self.config.get("panel_url", "http://127.0.0.1:8080")),
            "panel_url",
        )
        raw = body_bytes(payload)
        headers = signed_headers(str(self.config.get("shared_secret", "")), str(self.config.get("panel_id", "palpanel")), "POST", path, raw)
        url = base_url + path
        async with self.http.post(url, data=raw, headers=headers) as response:
            response.raise_for_status()
            return await response.json()
