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
from datetime import datetime, timezone
from pathlib import Path
from zipfile import ZipFile

import paramiko


WORKFLOW = "PalPanelBridge build"
ARTIFACT_PATTERN = re.compile(r"^PalPanelBridge-v([0-9]+(?:\.[0-9]+)*)-.*-server-package$")


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


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-id", type=int)
    parser.add_argument("--head-sha")
    parser.add_argument("--repo", default="ninhua/palworld-panel")
    parser.add_argument("--branch", default="custom-stable")
    parser.add_argument("--poll-seconds", type=int, default=30)
    parser.add_argument("--timeout-seconds", type=int, default=2700)
    parser.add_argument("--output-root", type=Path, default=Path(__file__).resolve().parents[2].parent)
    parser.add_argument("--host")
    parser.add_argument("--port", type=int, default=int(os.environ.get("PALPANEL_SFTP_PORT", "22")))
    parser.add_argument("--username")
    parser.add_argument("--fingerprint")
    parser.add_argument("--remote-dir", default=os.environ.get("PALPANEL_SFTP_REMOTE_DIR", "/palworld_win/server/Pal/Binaries/Win64/ue4ss/Mods/PalPanelBridge/dlls"))
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
    host = env_or_argument(args.host, "PALPANEL_SFTP_HOST")
    username = env_or_argument(args.username, "PALPANEL_SFTP_USERNAME")
    fingerprint = env_or_argument(args.fingerprint, "PALPANEL_SFTP_HOSTKEY_SHA256")
    password = os.environ.get("PALPANEL_SFTP_PASSWORD") or getpass.getpass("SFTP password: ")
    backup = deploy(dll, checksum, version, host, args.port, username, password, fingerprint, args.remote_dir)
    print(json.dumps({
        "ok": True,
        "version": version,
        "run_id": run_id,
        "run_url": run["url"],
        "sha256": checksum,
        "local_package": str(local_root),
        "remote_backup": backup,
        "config_preserved": True,
        "server_restarted": False,
    }, ensure_ascii=True, indent=2))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=True), file=sys.stderr)
        raise SystemExit(1)
