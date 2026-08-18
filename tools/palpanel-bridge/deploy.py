#!/usr/bin/env python3
"""Verify the latest PalPanelBridge Action artifact and deploy its DLL over SFTP.

Run with:
  uv run --with paramiko python tools/palpanel-bridge/deploy.py [--run-id ID]
"""

from __future__ import annotations

import argparse
import base64
import getpass
import hashlib
import json
import os
import posixpath
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen
from zipfile import ZipFile

import paramiko


WORKFLOW = "PalPanelBridge build"
ARTIFACT_PATTERN = re.compile(r"^PalPanelBridge-v([0-9]+(?:\.[0-9]+)*)-.*-server-package$")
DEDICATED_SERVER_NAME_PATTERN = re.compile(
    r"^\s*DedicatedServerName\s*=\s*([A-Za-z0-9_-]+)\s*$", re.MULTILINE | re.IGNORECASE
)


def command_json(arguments: list[str]) -> object:
    completed = subprocess.run(arguments, capture_output=True, text=True, encoding="utf-8")
    if completed.returncode != 0:
        raise RuntimeError(completed.stderr.strip() or "command failed: " + " ".join(arguments))
    return json.loads(completed.stdout)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def env_or_argument(value: str | None, name: str) -> str:
    resolved = value or os.environ.get(name, "")
    if not resolved:
        raise RuntimeError(f"{name} is required")
    return resolved


def failed_run_log(repo: str, run_id: int, output_root: Path) -> Path:
    completed = subprocess.run(
        ["gh", "run", "view", str(run_id), "--repo", repo, "--log-failed"],
        capture_output=True,
        text=True,
        encoding="utf-8",
    )
    output_root.mkdir(parents=True, exist_ok=True)
    path = output_root / f"PalPanelBridge-failed-run-{run_id}.log"
    content = completed.stdout
    if completed.stderr:
        content += ("\n" if content else "") + completed.stderr
    path.write_text(content or "No failed-step log was returned by GitHub.\n", encoding="utf-8")
    return path


def resolve_run(
    repo: str,
    branch: str,
    run_id: int | None,
    head_sha: str,
    output_root: Path,
    timeout_seconds: int,
    poll_seconds: int,
) -> dict[str, object]:
    deadline = time.monotonic() + timeout_seconds
    consecutive_query_failures = 0
    while True:
        try:
            if run_id is not None:
                run = command_json(
                    ["gh", "run", "view", str(run_id), "--repo", repo, "--json", "databaseId,status,conclusion,headSha,url"]
                )
            else:
                runs = command_json(
                    [
                        "gh", "run", "list", "--repo", repo, "--workflow", WORKFLOW,
                        "--branch", branch, "--commit", head_sha, "--limit", "1",
                        "--json", "databaseId,status,conclusion,headSha,url",
                    ]
                )
                run = runs[0] if isinstance(runs, list) and runs else None
            consecutive_query_failures = 0
        except RuntimeError as error:
            consecutive_query_failures += 1
            if consecutive_query_failures >= 5:
                raise RuntimeError(f"GitHub Action query failed repeatedly: {error}") from error
            print(f"Action query failed; retrying in {poll_seconds}s: {error}", flush=True)
            run = None

        if run is not None and not isinstance(run, dict):
            raise RuntimeError("GitHub returned an invalid workflow run")
        if isinstance(run, dict) and run.get("status") == "completed":
            if run.get("conclusion") == "success":
                return run
            failed_id = int(run["databaseId"])
            log_path = failed_run_log(repo, failed_id, output_root)
            raise RuntimeError(
                f"workflow run {failed_id} failed with conclusion={run.get('conclusion')}; "
                f"failed-step log: {log_path}"
            )

        if time.monotonic() >= deadline:
            target = f"run {run_id}" if run_id is not None else f"commit {head_sha}"
            raise TimeoutError(f"timed out waiting for PalPanelBridge Action for {target}")
        state = run.get("status") if isinstance(run, dict) else "not_created"
        print(f"Waiting for PalPanelBridge Action ({state}); next check in {poll_seconds}s", flush=True)
        time.sleep(poll_seconds)


def resolve_artifact(repo: str, run_id: int) -> tuple[int, str, str]:
    payload = command_json(["gh", "api", f"repos/{repo}/actions/runs/{run_id}/artifacts"])
    artifacts = payload.get("artifacts", []) if isinstance(payload, dict) else []
    matches: list[tuple[int, str, str]] = []
    for artifact in artifacts:
        if not isinstance(artifact, dict) or artifact.get("expired"):
            continue
        name = str(artifact.get("name", ""))
        match = ARTIFACT_PATTERN.match(name)
        if match:
            matches.append((int(artifact["id"]), name, match.group(1)))
    if len(matches) != 1:
        raise RuntimeError(f"expected one non-expired PalPanelBridge artifact, found {len(matches)}")
    return matches[0]


def download_and_verify(repo: str, run_id: int, artifact_name: str, version: str, output_root: Path) -> tuple[Path, str, Path]:
    with tempfile.TemporaryDirectory(prefix="palpanel-bridge-") as temporary:
        temp = Path(temporary)
        completed = subprocess.run(
            ["gh", "run", "download", str(run_id), "--repo", repo, "--name", artifact_name, "--dir", str(temp)],
            capture_output=True,
            text=True,
            encoding="utf-8",
        )
        if completed.returncode != 0:
            raise RuntimeError(completed.stderr.strip() or "artifact download failed")
        archives = list(temp.glob("*.zip"))
        if len(archives) != 1:
            raise RuntimeError(f"expected one zip in artifact, found {len(archives)}")
        archive = archives[0]
        extracted = temp / "extracted"
        with ZipFile(archive) as package:
            for entry in package.infolist():
                destination = (extracted / entry.filename).resolve()
                if not destination.is_relative_to(extracted.resolve()):
                    raise RuntimeError(f"unsafe archive entry: {entry.filename}")
            package.extractall(extracted)
        package_root = extracted / "PalPanelBridge"
        dll = package_root / "dlls" / "main.dll"
        sums = package_root / "SHA256SUMS"
        if not dll.is_file() or not sums.is_file():
            raise RuntimeError("artifact is missing main.dll or SHA256SUMS")
        expected = sums.read_text(encoding="ascii").strip().split()[0].lower()
        actual = sha256(dll)
        if actual != expected:
            raise RuntimeError(f"artifact checksum mismatch: expected {expected}, got {actual}")

        local_root = output_root / f"PalPanelBridge-v{version}-run-{run_id}"
        local_root.mkdir(parents=True, exist_ok=True)
        shutil.copy2(archive, local_root / archive.name)
        copied_package = local_root / "extracted" / "PalPanelBridge"
        if not copied_package.exists():
            shutil.copytree(package_root, copied_package)
        return copied_package / "dlls" / "main.dll", actual, local_root


def connect_sftp(host: str, port: int, username: str, password: str, expected_fingerprint: str) -> tuple[paramiko.Transport, paramiko.SFTPClient]:
    connection = socket.create_connection((host, port), timeout=15)
    transport = paramiko.Transport(connection)
    transport.start_client(timeout=15)
    server_key = transport.get_remote_server_key()
    actual = "SHA256:" + base64.b64encode(hashlib.sha256(server_key.asbytes()).digest()).decode("ascii").rstrip("=")
    if actual != expected_fingerprint.rstrip("="):
        transport.close()
        raise RuntimeError(f"SFTP host key mismatch: expected {expected_fingerprint}, got {actual}")
    transport.auth_password(username=username, password=password)
    return transport, paramiko.SFTPClient.from_transport(transport)


def deploy(dll: Path, checksum: str, version: str, host: str, port: int, username: str, password: str, fingerprint: str, remote_dir: str) -> str:
    transport, sftp = connect_sftp(host, port, username, password, fingerprint)
    timestamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    remote_main = posixpath.join(remote_dir, "main.dll")
    remote_temp = posixpath.join(remote_dir, f"main.dll.upload-v{version}-{timestamp}")
    remote_backup = posixpath.join(remote_dir, f"main.dll.bak-before-v{version}-{timestamp}")
    moved_original = False
    deployed_ok = False
    try:
        current = sftp.stat(remote_main)
        sftp.put(str(dll), remote_temp)
        sftp.chmod(remote_temp, current.st_mode & 0o777)
        with tempfile.TemporaryDirectory(prefix="palpanel-bridge-verify-") as temporary:
            uploaded = Path(temporary) / "uploaded-main.dll"
            sftp.get(remote_temp, str(uploaded))
            if sha256(uploaded) != checksum:
                raise RuntimeError("temporary remote DLL checksum mismatch")
        sftp.rename(remote_main, remote_backup)
        moved_original = True
        try:
            sftp.rename(remote_temp, remote_main)
        except Exception:
            sftp.rename(remote_backup, remote_main)
            moved_original = False
            raise
        with tempfile.TemporaryDirectory(prefix="palpanel-bridge-verify-") as temporary:
            deployed = Path(temporary) / "deployed-main.dll"
            sftp.get(remote_main, str(deployed))
            if sha256(deployed) != checksum:
                raise RuntimeError("deployed remote DLL checksum mismatch")
        deployed_ok = True
        return remote_backup
    finally:
        if moved_original and not deployed_ok:
            try:
                sftp.remove(remote_main)
            except OSError:
                pass
            try:
                sftp.rename(remote_backup, remote_main)
            except OSError:
                pass
        try:
            sftp.stat(remote_temp)
            sftp.remove(remote_temp)
        except OSError:
            pass
        sftp.close()
        transport.close()


def deploy_local(dll: Path, checksum: str, version: str, local_main: Path) -> Path:
    """Atomically replace a stopped local server's DLL and keep a rollback copy."""
    local_main = local_main.resolve()
    if not local_main.is_file():
        raise RuntimeError(f"local DLL does not exist: {local_main}")
    timestamp = datetime.now(timezone(timedelta(hours=8))).strftime("%Y%m%dT%H%M%S+0800")
    local_temp = local_main.with_name(f"main.dll.upload-v{version}-{timestamp}")
    local_backup = local_main.with_name(f"main.dll.bak-before-v{version}-{timestamp}")
    moved_original = False
    deployed_ok = False
    try:
        shutil.copy2(dll, local_temp)
        if sha256(local_temp) != checksum:
            raise RuntimeError("temporary local DLL checksum mismatch")
        replace_with_retry(local_main, local_backup)
        moved_original = True
        try:
            replace_with_retry(local_temp, local_main)
        except Exception:
            replace_with_retry(local_backup, local_main)
            moved_original = False
            raise
        if sha256(local_main) != checksum:
            raise RuntimeError("deployed local DLL checksum mismatch")
        deployed_ok = True
        return local_backup
    finally:
        if moved_original and not deployed_ok:
            try:
                replace_with_retry(local_backup, local_main)
            except OSError as rollback_error:
                raise RuntimeError(
                    f"local DLL deployment failed and rollback also failed: {rollback_error}"
                ) from rollback_error
        local_temp.unlink(missing_ok=True)


def replace_with_retry(source: Path, target: Path, timeout_seconds: int = 90) -> None:
    """Atomically replace a Windows path after the game process releases it."""
    deadline = time.monotonic() + timeout_seconds
    while True:
        try:
            source.replace(target)
            return
        except OSError as error:
            locked = isinstance(error, PermissionError) or getattr(error, "winerror", None) in {5, 32}
            if not locked or time.monotonic() >= deadline:
                raise
            time.sleep(1)


def repair_windows_settings_acl(settings: Path) -> None:
    """Restore inheritance only on the known local GameUserSettings.ini file."""
    if os.name != "nt" or settings.name.casefold() != "gameusersettings.ini":
        raise RuntimeError("automatic GameUserSettings.ini ACL repair is only supported on Windows")
    completed = subprocess.run(
        ["icacls", str(settings), "/inheritance:e"],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if completed.returncode != 0:
        raise RuntimeError("failed to restore GameUserSettings.ini ACL inheritance")


def validate_local_world_binding(local_main: Path, repair_acl: bool = False) -> tuple[Path, str]:
    """Fail closed if the local server cannot prove which existing world it will load."""
    pal_roots = [
        parent for parent in local_main.resolve().parents
        if parent.name.casefold() == "pal" and (parent / "Saved").is_dir()
    ]
    if len(pal_roots) != 1:
        raise RuntimeError("could not uniquely derive the local Pal directory from --local-dll")
    settings = pal_roots[0] / "Saved" / "Config" / "WindowsServer" / "GameUserSettings.ini"
    try:
        content = settings.read_text(encoding="utf-8")
    except OSError as error:
        permission_denied = isinstance(error, PermissionError) or getattr(error, "winerror", None) == 5
        if not repair_acl or not permission_denied:
            raise RuntimeError(f"refusing to start: GameUserSettings.ini is unreadable: {error}") from error
        repair_windows_settings_acl(settings)
        try:
            content = settings.read_text(encoding="utf-8")
        except OSError as retry_error:
            raise RuntimeError(
                f"refusing to start: GameUserSettings.ini remains unreadable after ACL repair: {retry_error}"
            ) from retry_error
    match = DEDICATED_SERVER_NAME_PATTERN.search(content)
    if not match:
        raise RuntimeError("refusing to start: DedicatedServerName is missing from GameUserSettings.ini")
    world_id = match.group(1)
    level = pal_roots[0] / "Saved" / "SaveGames" / "0" / world_id / "Level.sav"
    if not level.is_file() or level.stat().st_size <= 0:
        raise RuntimeError(f"refusing to start: bound world has no non-empty Level.sav: {world_id}")
    return settings, world_id


def panel_request(panel_url: str, api_key: str, method: str, path: str, body: object | None = None, timeout_seconds: int = 120) -> dict[str, object]:
    url = panel_url.rstrip("/") + path
    data = json.dumps(body).encode("utf-8") if body is not None else None
    headers = {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    }
    request = Request(url, data=data, headers=headers, method=method)
    try:
        with urlopen(request, timeout=timeout_seconds) as response:
            raw = response.read(1024 * 1024 + 1)
            if len(raw) > 1024 * 1024:
                raise RuntimeError("Panel response exceeds 1 MiB")
            value = json.loads(raw.decode("utf-8"))
    except HTTPError as error:
        raise RuntimeError(f"Panel API HTTP {error.code}: {error.read().decode('utf-8', errors='replace')[:500]}") from error
    except URLError as error:
        raise RuntimeError(f"Panel API request failed: {error.reason}") from error
    except (TimeoutError, OSError) as error:
        raise RuntimeError(f"Panel API request failed: {type(error).__name__}") from error
    if not isinstance(value, dict):
        raise RuntimeError("Panel API returned a non-object response")
    return value


def panel_restart(panel_url: str, api_key: str, version: str, timeout_seconds: int = 900, poll_seconds: int = 15) -> dict[str, object]:
    """Restart the game server through the panel API and wait for completion."""
    submitted = panel_request(panel_url, api_key, "POST", "/api/server/restart", timeout_seconds=300)
    data = submitted.get("data") if isinstance(submitted.get("data"), dict) else {}
    status = data.get("status")
    if status == "restarted" or submitted.get("ok") is True:
        return {"status": status, "response": submitted}
    raise RuntimeError(f"Panel restart returned an unexpected response: {submitted}")


def panel_lifecycle(panel_url: str, api_key: str, action: str) -> dict[str, object]:
    response = panel_request(panel_url, api_key, "POST", f"/api/server/{action}", timeout_seconds=300)
    data = response.get("data") if isinstance(response.get("data"), dict) else {}
    expected_status = {"start": "started", "stop": "stopped"}.get(action)
    if expected_status is None:
        raise ValueError(f"unsupported panel lifecycle action: {action}")
    if data.get("status") != expected_status and response.get("ok") is not True:
        raise RuntimeError(f"Panel {action} returned an unexpected response: {response}")
    return response


def panel_safe_stop(
    panel_url: str,
    api_key: str,
    message: str = "PalPanelBridge safe deployment",
    timeout_seconds: int = 300,
    poll_seconds: int = 3,
) -> dict[str, object]:
    submitted = panel_request(
        panel_url,
        api_key,
        "POST",
        "/api/server/safe-stop",
        {"waittime": 5, "message": message},
        timeout_seconds=30,
    )
    data = submitted.get("data") if isinstance(submitted.get("data"), dict) else {}
    job_id = str(data.get("id", ""))
    if not job_id or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", job_id):
        raise RuntimeError(f"Panel safe-stop returned no valid job id: {submitted}")
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        job_response = panel_request(panel_url, api_key, "GET", f"/api/jobs/{job_id}", timeout_seconds=15)
        job = job_response.get("data") if isinstance(job_response.get("data"), dict) else {}
        status = str(job.get("status", ""))
        if status == "completed":
            return job_response
        if status == "failed":
            raise RuntimeError(f"Panel safe-stop failed: {job_response}")
        time.sleep(poll_seconds)
    raise TimeoutError(f"timed out waiting for Panel safe-stop job {job_id}")


def wait_bridge_health(bridge_url: str, token: str, version: str, timeout_seconds: int = 600, poll_seconds: int = 10) -> dict[str, object]:
    deadline = time.monotonic() + timeout_seconds
    last_error = "not started"
    while time.monotonic() < deadline:
        request = Request(
            bridge_url.rstrip("/") + "/v1/health",
            headers={"Authorization": f"Bearer {token}"},
            method="GET",
        )
        try:
            with urlopen(request, timeout=10) as response:
                payload = json.loads(response.read(1024 * 1024).decode("utf-8"))
            actual = str(payload.get("bridge_version", payload.get("version", ""))) if isinstance(payload, dict) else ""
            if actual == version:
                return payload
            last_error = f"bridge version is {actual or 'unknown'}, expected {version}"
        except (HTTPError, URLError, TimeoutError, OSError, json.JSONDecodeError) as error:
            last_error = type(error).__name__
        time.sleep(poll_seconds)
    raise TimeoutError(f"timed out waiting for PalPanelBridge v{version}: {last_error}")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-id", type=int)
    parser.add_argument("--head-sha")
    parser.add_argument("--repo", default="ninhua/palworld-panel")
    parser.add_argument("--branch", default="custom-stable")
    parser.add_argument("--poll-seconds", type=int, default=30)
    parser.add_argument("--timeout-seconds", type=int, default=2700)
    parser.add_argument(
        "--output-root",
        type=Path,
        default=Path(__file__).resolve().parents[3] / "PalPanelBridge" / "versions",
    )
    parser.add_argument("--host")
    parser.add_argument("--port", type=int, default=int(os.environ.get("PALPANEL_SFTP_PORT", "22")))
    parser.add_argument("--username")
    parser.add_argument("--fingerprint")
    parser.add_argument("--remote-dir", default=os.environ.get("PALPANEL_SFTP_REMOTE_DIR", "/palworld_win/server/Pal/Binaries/Win64/ue4ss/Mods/PalPanelBridge/dlls"))
    parser.add_argument("--local-dll", type=Path, help="local server main.dll; stops and starts the server around atomic replacement")
    parser.add_argument("--panel-url", default=os.environ.get("PALPANEL_API_URL", ""))
    parser.add_argument("--panel-api-key", default=os.environ.get("PALPANEL_API_KEY", ""))
    parser.add_argument("--bridge-url", default=os.environ.get("PALPANEL_BRIDGE_URL", "http://127.0.0.1:18083"))
    parser.add_argument("--bridge-token", default=os.environ.get("PALPANEL_BRIDGE_TOKEN", ""))
    parser.add_argument("--download-only", action="store_true", help="wait, download, and verify without deploying")
    parser.add_argument("--restart", action="store_true", help="restart the game server via the panel API after deploying")
    args = parser.parse_args()

    if args.poll_seconds < 10:
        raise RuntimeError("--poll-seconds must be at least 10")
    if args.timeout_seconds < args.poll_seconds:
        raise RuntimeError("--timeout-seconds must be at least --poll-seconds")
    head_sha = args.head_sha
    if args.run_id is None and not head_sha:
        completed = subprocess.run(
            ["git", "rev-parse", "HEAD"], capture_output=True, text=True, encoding="utf-8"
        )
        if completed.returncode != 0:
            raise RuntimeError(completed.stderr.strip() or "could not resolve current git commit")
        head_sha = completed.stdout.strip()

    output_root = args.output_root.resolve()
    run = resolve_run(
        args.repo,
        args.branch,
        args.run_id,
        head_sha or "",
        output_root,
        args.timeout_seconds,
        args.poll_seconds,
    )
    run_id = int(run["databaseId"])
    _, artifact_name, version = resolve_artifact(args.repo, run_id)
    dll, checksum, local_root = download_and_verify(args.repo, run_id, artifact_name, version, output_root)
    if args.download_only:
        print(json.dumps({
            "ok": True,
            "version": version,
            "run_id": run_id,
            "run_url": run["url"],
            "sha256": checksum,
            "local_package": str(local_root),
            "deployed": False,
        }, ensure_ascii=True, indent=2))
        return
    restarted = False
    health: dict[str, object] | None = None
    if args.local_dll is not None:
        panel_url = env_or_argument(args.panel_url, "PALPANEL_API_URL")
        api_key = env_or_argument(args.panel_api_key, "PALPANEL_API_KEY")
        bridge_token = env_or_argument(args.bridge_token, "PALPANEL_BRIDGE_TOKEN")
        local_main = args.local_dll.resolve()
        _, expected_world_id = validate_local_world_binding(local_main)
        stopped = False
        safe_to_start = True
        local_backup: Path | None = None
        try:
            panel_safe_stop(panel_url, api_key)
            stopped = True
            safe_to_start = False
            _, stopped_world_id = validate_local_world_binding(local_main, repair_acl=True)
            if stopped_world_id != expected_world_id:
                raise RuntimeError(
                    f"refusing to deploy: world binding changed from {expected_world_id} to {stopped_world_id}"
                )
            safe_to_start = True
            local_backup = deploy_local(dll, checksum, version, local_main)
            backup = str(local_backup)
            panel_lifecycle(panel_url, api_key, "start")
            stopped = False
            restarted = True
            health = wait_bridge_health(args.bridge_url, bridge_token, version)
        except Exception:
            if local_backup is not None and local_backup.is_file():
                if not stopped:
                    panel_safe_stop(panel_url, api_key, "PalPanelBridge rollback")
                    stopped = True
                safe_to_start = False
                replace_with_retry(local_backup, local_main)
                _, rollback_world_id = validate_local_world_binding(local_main)
                if rollback_world_id != expected_world_id:
                    raise RuntimeError(
                        f"rollback completed but world binding changed to {rollback_world_id}; server remains stopped"
                    )
                safe_to_start = True
                panel_lifecycle(panel_url, api_key, "start")
                stopped = False
            raise
        finally:
            if stopped and safe_to_start:
                try:
                    panel_lifecycle(panel_url, api_key, "start")
                except Exception as recovery_error:
                    print(f"WARNING: failed to restart server after deployment error: {recovery_error}", file=sys.stderr)
            elif stopped:
                print("WARNING: server remains stopped because the world binding could not be validated", file=sys.stderr)
    else:
        host = env_or_argument(args.host, "PALPANEL_SFTP_HOST")
        username = env_or_argument(args.username, "PALPANEL_SFTP_USERNAME")
        fingerprint = env_or_argument(args.fingerprint, "PALPANEL_SFTP_HOSTKEY_SHA256")
        password = os.environ.get("PALPANEL_SFTP_PASSWORD") or getpass.getpass("SFTP password: ")
        backup = deploy(dll, checksum, version, host, args.port, username, password, fingerprint, args.remote_dir)
        if args.restart:
            panel_url = env_or_argument(args.panel_url, "PALPANEL_API_URL")
            api_key = env_or_argument(args.panel_api_key, "PALPANEL_API_KEY")
            restart_result = panel_restart(panel_url, api_key, version)
            restarted = True
            print(f"Panel restart submitted: {restart_result}", flush=True)
    print(json.dumps({
        "ok": True,
        "version": version,
        "run_id": run_id,
        "run_url": run["url"],
        "sha256": checksum,
        "local_package": str(local_root),
        "backup": backup,
        "config_preserved": True,
        "server_restarted": restarted,
        "health": health,
    }, ensure_ascii=True, indent=2))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=True), file=sys.stderr)
        raise SystemExit(1)
