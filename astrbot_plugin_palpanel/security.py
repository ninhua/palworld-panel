from __future__ import annotations

import hashlib
import hmac
import json
import secrets
import time
from typing import Any
from urllib.parse import urlparse



def validate_http_service_url(raw: str, field: str = "URL") -> str:
    value = str(raw).strip().rstrip("/")
    parsed = urlparse(value)
    if parsed.scheme.lower() not in {"http", "https"} or not parsed.hostname:
        raise RuntimeError(f"{field} must use HTTP or HTTPS")
    return value

def body_bytes(payload: Any | None) -> bytes:
    if payload is None:
        return b""
    return json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")


def signature(secret: str, method: str, path: str, timestamp: str, nonce: str, body: bytes) -> str:
    digest = hashlib.sha256(body).hexdigest()
    canonical = "\n".join((method.upper(), path, timestamp, nonce, digest)).encode("utf-8")
    return hmac.new(secret.encode("utf-8"), canonical, hashlib.sha256).hexdigest()


def signed_headers(secret: str, panel_id: str, method: str, path: str, body: bytes) -> dict[str, str]:
    timestamp = str(int(time.time()))
    nonce = secrets.token_urlsafe(18)
    return {
        "X-PalPanel-Id": panel_id,
        "X-PalPanel-Timestamp": timestamp,
        "X-PalPanel-Nonce": nonce,
        "X-PalPanel-Signature": signature(secret, method, path, timestamp, nonce, body),
        "Content-Type": "application/json",
    }


def verify_headers(secret: str, method: str, path: str, headers: Any, body: bytes, max_age: int = 60) -> tuple[bool, str]:
    timestamp = str(headers.get("X-PalPanel-Timestamp", ""))
    nonce = str(headers.get("X-PalPanel-Nonce", ""))
    supplied = str(headers.get("X-PalPanel-Signature", ""))
    try:
        if abs(int(time.time()) - int(timestamp)) > max_age:
            return False, "expired timestamp"
    except ValueError:
        return False, "invalid timestamp"
    if not nonce or not supplied:
        return False, "missing signature headers"
    expected = signature(secret, method, path, timestamp, nonce, body)
    return (hmac.compare_digest(expected, supplied), nonce)
