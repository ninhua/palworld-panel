#!/usr/bin/env python3
"""Create and verify a self-owned PalPanel dependency mirror.

The mirror is content-addressed by a deterministic manifest. It intentionally
contains no credentials and refuses symlinks, device files, and paths outside
its root.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import stat
import sys
from pathlib import Path
from typing import Iterable

SCHEMA_VERSION = 1
MANIFEST_NAME = "vendor-manifest.json"
MANAGED_MARKER = ".palpanel-vendor-managed"
COMPONENT_ARGS = {
    "palops_web": "palops-web",
    "maplibre": "maplibre-gl",
    "palcalc": "palcalc",
    "uesave": "uesave",
    "map_tiles": "palops-map-tiles",
    "ue4ss_sdk": "ue4ss-sdk",
    "github_actions_bundle": "github-actions-bundle",
    "npm_cache": "npm-cache",
    "cargo_home": "cargo-home",
    "go_mod_cache": "go-mod-cache",
    "nuget_packages": "nuget-packages",
}


def repo_root() -> Path:
    return Path(__file__).resolve().parent.parent


def load_lock(path: Path | None = None) -> dict:
    path = path or repo_root() / "third_party" / "vendor-lock.json"
    data = json.loads(path.read_text(encoding="utf-8"))
    if data.get("schema_version") != SCHEMA_VERSION:
        raise ValueError(f"unsupported vendor lock schema: {data.get('schema_version')!r}")
    components = data.get("components")
    if not isinstance(components, list) or not components:
        raise ValueError("vendor lock must contain components")
    ids = [item.get("id") for item in components]
    if len(ids) != len(set(ids)) or any(not isinstance(item, str) or not item for item in ids):
        raise ValueError("vendor lock contains invalid or duplicate component ids")
    return data


def ensure_safe_tree(source: Path) -> None:
    source = source.resolve(strict=True)
    if not source.is_dir():
        raise ValueError(f"source is not a directory: {source}")
    for root, dirs, files in os.walk(source, followlinks=False):
        current = Path(root)
        for name in dirs + files:
            path = current / name
            mode = path.lstat().st_mode
            if stat.S_ISLNK(mode):
                raise ValueError(f"symlink is not allowed in vendor input: {path}")
            if not (stat.S_ISDIR(mode) or stat.S_ISREG(mode)):
                raise ValueError(f"special file is not allowed in vendor input: {path}")


def copy_tree(source: Path, destination: Path) -> None:
    ensure_safe_tree(source)
    if destination.exists():
        shutil.rmtree(destination)
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copytree(source, destination, symlinks=False)


def iter_files(root: Path) -> Iterable[Path]:
    for path in sorted(root.rglob("*"), key=lambda item: item.as_posix()):
        mode = path.lstat().st_mode
        if stat.S_ISLNK(mode):
            raise ValueError(f"symlink is not allowed in vendor mirror: {path}")
        if stat.S_ISREG(mode):
            yield path
        elif not stat.S_ISDIR(mode):
            raise ValueError(f"special file is not allowed in vendor mirror: {path}")


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def component_snapshot(root: Path, relative: str) -> dict:
    component_root = (root / relative).resolve()
    if root.resolve() not in component_root.parents:
        raise ValueError(f"component path escapes vendor root: {relative}")
    if not component_root.is_dir():
        raise ValueError(f"component is missing: {relative}")
    files = []
    tree = hashlib.sha256()
    total = 0
    for path in iter_files(component_root):
        rel = path.relative_to(component_root).as_posix()
        size = path.stat().st_size
        digest = sha256_file(path)
        files.append({"path": rel, "size": size, "sha256": digest})
        tree.update(rel.encode("utf-8"))
        tree.update(b"\0")
        tree.update(str(size).encode("ascii"))
        tree.update(b"\0")
        tree.update(digest.encode("ascii"))
        tree.update(b"\n")
        total += size
    if not files:
        raise ValueError(f"component is empty: {relative}")
    return {
        "path": relative,
        "file_count": len(files),
        "size_bytes": total,
        "tree_sha256": tree.hexdigest(),
        "files": files,
    }


def build_manifest(root: Path, lock: dict) -> dict:
    components = []
    for spec in lock["components"]:
        path = root / spec["path"]
        if not path.exists():
            continue
        snapshot = component_snapshot(root, spec["path"])
        snapshot.update({key: spec[key] for key in ("id", "kind") if key in spec})
        for key in ("upstream", "revision", "version", "license"):
            if key in spec:
                snapshot[key] = spec[key]
        components.append(snapshot)
    if not components:
        raise ValueError("no components were imported")
    return {
        "schema_version": SCHEMA_VERSION,
        "layout_version": lock.get("layout_version", 1),
        "components": components,
    }


def write_manifest(root: Path, manifest: dict) -> None:
    target = root / MANIFEST_NAME
    temporary = target.with_suffix(".tmp")
    payload = json.dumps(manifest, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
    temporary.write_text(payload, encoding="utf-8")
    os.replace(temporary, target)


def verify(root: Path, lock: dict, profile: str) -> dict:
    manifest_path = root / MANIFEST_NAME
    if not manifest_path.is_file():
        raise ValueError(f"vendor manifest is missing: {manifest_path}")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if manifest.get("schema_version") != SCHEMA_VERSION:
        raise ValueError("vendor manifest schema mismatch")
    manifest_components = {item.get("id"): item for item in manifest.get("components", [])}
    lock_components = {item["id"]: item for item in lock["components"]}
    required = {
        item["id"]
        for item in lock["components"]
        if profile in item.get("required_profiles", [])
    }
    missing = sorted(required - manifest_components.keys())
    if missing:
        raise ValueError(f"vendor mirror is missing required components for {profile}: {', '.join(missing)}")
    for component_id, recorded in manifest_components.items():
        spec = lock_components.get(component_id)
        if spec is None:
            raise ValueError(f"vendor manifest contains unknown component: {component_id}")
        current = component_snapshot(root, spec["path"])
        for key in ("file_count", "size_bytes", "tree_sha256"):
            if current[key] != recorded.get(key):
                raise ValueError(f"vendor component changed: {component_id} ({key})")
        expected_files = {(item["path"], item["size"], item["sha256"]) for item in recorded.get("files", [])}
        current_files = {(item["path"], item["size"], item["sha256"]) for item in current["files"]}
        if current_files != expected_files:
            raise ValueError(f"vendor component file manifest changed: {component_id}")
    return manifest


def managed_copy(source: Path, destination: Path) -> bool:
    marker = destination / MANAGED_MARKER
    if destination.exists():
        if marker.is_file():
            shutil.rmtree(destination)
        else:
            return False
    copy_tree(source, destination)
    marker.write_text("managed by scripts/vendorctl.py\n", encoding="utf-8")
    return True


def prepare(root: Path, repository: Path, work_root: Path, profile: str) -> dict:
    lock = load_lock()
    verify(root, lock, profile)
    repository = repository.resolve(strict=True)
    work_root = work_root.resolve() if work_root.exists() else work_root.absolute()
    work_root.mkdir(parents=True, exist_ok=True)
    prepared = []
    for component_id in ("palcalc", "uesave"):
        spec = next(item for item in lock["components"] if item["id"] == component_id)
        source = root / spec["path"]
        destination = repository / "third_party" / component_id
        if managed_copy(source, destination):
            prepared.append(destination.as_posix())
    cache_root = work_root / "caches"
    if cache_root.exists():
        shutil.rmtree(cache_root)
    cache_root.mkdir(parents=True)
    for component_id in ("npm-cache", "cargo-home", "go-mod-cache", "nuget-packages"):
        spec = next(item for item in lock["components"] if item["id"] == component_id)
        source = root / spec["path"]
        if source.is_dir():
            copy_tree(source, cache_root / component_id)
    state = {"prepared_directories": prepared, "work_root": work_root.as_posix()}
    (work_root / "prepare-state.json").write_text(json.dumps(state, indent=2) + "\n", encoding="utf-8")
    return state


def cleanup(repository: Path, work_root: Path | None) -> None:
    for component_id in ("palcalc", "uesave"):
        destination = repository / "third_party" / component_id
        if (destination / MANAGED_MARKER).is_file():
            shutil.rmtree(destination)
    if work_root and work_root.exists():
        shutil.rmtree(work_root)


def environment(root: Path, work_root: Path, fmt: str) -> str:
    values = {
        "PALPANEL_PALOPS_MAP_SOURCE_DIR": root / "sources/palops-web",
        "PALPANEL_MAPLIBRE_SOURCE_DIR": root / "sources/maplibre-gl",
        "NPM_CONFIG_CACHE": work_root / "caches/npm-cache",
        "CARGO_HOME": work_root / "caches/cargo-home",
        "GOMODCACHE": work_root / "caches/go-mod-cache",
        "NUGET_PACKAGES": work_root / "caches/nuget-packages",
    }
    tiles = root / "assets/palops-map-tiles"
    if tiles.is_dir():
        values["PALPANEL_PALOPS_TILE_SOURCE_DIR"] = tiles
        values["PALPANEL_PALOPS_TILE_RIGHTS_CONFIRMED"] = "true"
    if fmt == "json":
        return json.dumps({key: str(value) for key, value in values.items()}, indent=2) + "\n"
    if fmt == "shell":
        import shlex
        return "".join(f"export {key}={shlex.quote(str(value))}\n" for key, value in values.items())
    if fmt == "powershell":
        lines = []
        for key, value in values.items():
            escaped = str(value).replace("'", "''")
            lines.append(f"$env:{key} = '{escaped}'")
        return "\n".join(lines) + "\n"
    raise ValueError(f"unsupported environment format: {fmt}")


def command_init(args: argparse.Namespace) -> None:
    root = Path(args.root).resolve()
    root.mkdir(parents=True, exist_ok=True)
    lock = load_lock(Path(args.lock) if args.lock else None)
    specs = {item["id"]: item for item in lock["components"]}
    imported = 0
    for argument_name, component_id in COMPONENT_ARGS.items():
        raw = getattr(args, argument_name, None)
        if not raw:
            continue
        if component_id not in specs:
            raise ValueError(f"component is not declared in lock: {component_id}")
        source = Path(raw).resolve(strict=True)
        destination = (root / specs[component_id]["path"]).resolve()
        if source == destination or root == source or root in source.parents:
            raise ValueError(f"vendor input must be outside the destination root: {source}")
        copy_tree(source, destination)
        imported += 1
    if imported == 0:
        raise ValueError("init requires at least one component or cache directory")
    write_manifest(root, build_manifest(root, lock))
    print(root / MANIFEST_NAME)


def command_refresh(args: argparse.Namespace) -> None:
    root = Path(args.root).resolve(strict=True)
    lock = load_lock(Path(args.lock) if args.lock else None)
    write_manifest(root, build_manifest(root, lock))
    print(root / MANIFEST_NAME)


def command_verify(args: argparse.Namespace) -> None:
    root = Path(args.root).resolve(strict=True)
    manifest = verify(root, load_lock(Path(args.lock) if args.lock else None), args.profile)
    print(f"verified {len(manifest['components'])} vendor components for profile {args.profile}")


def command_prepare(args: argparse.Namespace) -> None:
    state = prepare(Path(args.root).resolve(strict=True), Path(args.repository), Path(args.work_root), args.profile)
    print(json.dumps(state, indent=2))


def command_cleanup(args: argparse.Namespace) -> None:
    cleanup(Path(args.repository).resolve(strict=True), Path(args.work_root) if args.work_root else None)


def command_env(args: argparse.Namespace) -> None:
    sys.stdout.write(environment(Path(args.root).resolve(strict=True), Path(args.work_root).resolve(strict=True), args.format))


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    sub = result.add_subparsers(dest="command", required=True)
    init = sub.add_parser("init", help="copy dependency snapshots and write a manifest")
    init.add_argument("--root", required=True)
    init.add_argument("--lock")
    for name in COMPONENT_ARGS:
        init.add_argument("--" + name.replace("_", "-"))
    init.set_defaults(func=command_init)
    refresh = sub.add_parser("refresh", help="rebuild the manifest after an intentional mirror update")
    refresh.add_argument("--root", required=True)
    refresh.add_argument("--lock")
    refresh.set_defaults(func=command_refresh)
    check = sub.add_parser("verify", help="verify hashes and required components")
    check.add_argument("--root", required=True)
    check.add_argument("--lock")
    check.add_argument("--profile", choices=("source", "build", "bridge", "full"), default="source")
    check.set_defaults(func=command_verify)
    prep = sub.add_parser("prepare", help="prepare repository sources and writable build caches")
    prep.add_argument("--root", required=True)
    prep.add_argument("--repository", required=True)
    prep.add_argument("--work-root", required=True)
    prep.add_argument("--profile", choices=("source", "build", "bridge", "full"), default="source")
    prep.set_defaults(func=command_prepare)
    clean = sub.add_parser("cleanup", help="remove only vendor-managed prepared sources")
    clean.add_argument("--repository", required=True)
    clean.add_argument("--work-root")
    clean.set_defaults(func=command_cleanup)
    env = sub.add_parser("env", help="emit build environment variables")
    env.add_argument("--root", required=True)
    env.add_argument("--work-root", required=True)
    env.add_argument("--format", choices=("shell", "powershell", "json"), default="shell")
    env.set_defaults(func=command_env)
    return result


def main() -> int:
    try:
        args = parser().parse_args()
        args.func(args)
        return 0
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"vendorctl: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
