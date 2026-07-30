#!/usr/bin/env python3
"""Synchronize PalPanel map tiles, POIs, icons, metadata, and licenses.

The canonical bundle is maintained in ninhua/palpanel-assets. Network builds
resolve the requested ref to an immutable commit, download that archive, verify
its optional checksum manifest, validate the localized POI datasets and both
raster tile pyramids, then stage a normalized /map/palops tree for the frontend.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import shutil
import stat
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request
import zipfile
from pathlib import Path, PurePosixPath
from typing import Any, Iterable

ASSET_REPOSITORY = "ninhua/palpanel-assets"
ASSET_REF = "main"
EXPECTED_POI_TOTAL = 0
EXPECTED_TILE_COUNT_PER_LAYER = 341
LOCALES = ("zh-CN", "en-US", "ja-JP")
LAYERS = ("palpagos", "world-tree")
LEGACY_MAP_RELATIVE_ROOT = Path("src/PalOps.Web/wwwroot/map")
COMMON_MAP_ROOTS = (
    Path("map/palops"),
    Path("palops"),
    Path("assets/map/palops"),
    Path("frontend/public/map/palops"),
    LEGACY_MAP_RELATIVE_ROOT,
)
ALLOWED_EXTENSIONS = {
    ".json", ".webp", ".png", ".svg", ".txt", ".md", ".css", ".woff", ".woff2"
}
ALLOWED_BASENAMES = {"LICENSE", "NOTICE", "COPYING"}
SKIPPED_BASENAMES = {".gitignore", ".gitattributes", ".gitkeep", ".DS_Store"}
MAX_ARCHIVE_BYTES = 1024 * 1024 * 1024
MAX_FILE_BYTES = 64 * 1024 * 1024
MAX_TOTAL_BYTES = 1400 * 1024 * 1024


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--destination", type=Path, required=True)
    parser.add_argument("--source-dir", type=Path)
    parser.add_argument("--archive", type=Path)
    parser.add_argument("--allow-network", action="store_true")
    parser.add_argument("--repository", default=os.environ.get("PALPANEL_MAP_ASSETS_REPOSITORY", ASSET_REPOSITORY))
    parser.add_argument("--ref", default=os.environ.get("PALPANEL_MAP_ASSETS_REF", ASSET_REF))
    parser.add_argument("--source-commit", default="")
    parser.add_argument("--tiles-source-dir", type=Path)
    parser.add_argument("--allow-missing-tiles", action="store_true")
    parser.add_argument(
        "--expected-poi-total",
        type=int,
        default=EXPECTED_POI_TOTAL,
        help=argparse.SUPPRESS,
    )
    return parser.parse_args()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def sha256_tree(root: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted(item for item in root.rglob("*") if item.is_file()):
        relative = path.relative_to(root).as_posix().encode("utf-8")
        digest.update(relative)
        digest.update(b"\0")
        digest.update(bytes.fromhex(sha256_file(path)))
    return digest.hexdigest()


def ensure_safe_relative(relative: PurePosixPath) -> None:
    if relative.is_absolute() or any(part in {"", ".", ".."} for part in relative.parts):
        raise ValueError(f"unsafe archive path: {relative}")


def repository_parts(repository: str) -> tuple[str, str]:
    parts = repository.strip().strip("/").split("/")
    if len(parts) != 2 or not all(parts):
        raise ValueError("map asset repository must use owner/name form")
    return parts[0], parts[1]


def github_token() -> str:
    return (
        os.environ.get("PALPANEL_MAP_ASSETS_TOKEN", "").strip()
        or os.environ.get("GH_TOKEN", "").strip()
    )


def github_request(url: str, token: str = "") -> urllib.request.Request:
    headers = {
        "Accept": "application/vnd.github+json",
        "User-Agent": "PalPanel-map-assets-sync/2",
        "X-GitHub-Api-Version": "2022-11-28",
    }
    if token:
        headers["Authorization"] = f"Bearer {token}"
    return urllib.request.Request(url, headers=headers)


def read_json_url(url: str, token: str = "") -> dict[str, Any]:
    try:
        with urllib.request.urlopen(github_request(url, token), timeout=90) as response:
            value = json.load(response)
    except urllib.error.HTTPError as exc:
        if exc.code in {401, 403, 404}:
            raise ValueError(
                "unable to read the map asset repository; make it public or provide "
                "PALPANEL_MAP_ASSETS_TOKEN with contents:read access"
            ) from exc
        raise
    if not isinstance(value, dict):
        raise ValueError("GitHub returned invalid repository metadata")
    return value


def resolve_repository_commit(repository: str, ref: str, token: str = "") -> str:
    owner, name = repository_parts(repository)
    encoded_ref = urllib.parse.quote(ref, safe="")
    metadata = read_json_url(
        f"https://api.github.com/repos/{owner}/{name}/commits/{encoded_ref}",
        token,
    )
    commit = metadata.get("sha")
    if not isinstance(commit, str) or len(commit) != 40:
        raise ValueError("GitHub did not return a full map asset commit SHA")
    return commit


def download_repository_archive(target: Path, repository: str, commit: str, token: str = "") -> None:
    owner, name = repository_parts(repository)
    url = f"https://api.github.com/repos/{owner}/{name}/zipball/{commit}"
    try:
        with urllib.request.urlopen(github_request(url, token), timeout=180) as response, target.open("wb") as output:
            total = 0
            while True:
                chunk = response.read(1024 * 1024)
                if not chunk:
                    break
                total += len(chunk)
                if total > MAX_ARCHIVE_BYTES:
                    raise ValueError("map asset archive exceeds the configured size limit")
                output.write(chunk)
    except urllib.error.HTTPError as exc:
        if exc.code in {401, 403, 404}:
            raise ValueError(
                "unable to download the map asset archive; make it public or provide "
                "PALPANEL_MAP_ASSETS_TOKEN with contents:read access"
            ) from exc
        raise


def extract_archive(archive: Path, extract_root: Path) -> Path:
    with zipfile.ZipFile(archive) as bundle:
        members = bundle.infolist()
        roots = {PurePosixPath(item.filename).parts[0] for item in members if item.filename}
        if len(roots) != 1:
            raise ValueError("map asset archive must contain exactly one top-level directory")
        top = next(iter(roots))
        total = 0
        extracted_paths: set[str] = set()
        for info in members:
            relative = PurePosixPath(info.filename)
            ensure_safe_relative(relative)
            if info.is_dir():
                continue
            mode = info.external_attr >> 16
            if stat.S_ISLNK(mode):
                raise ValueError(f"symlinks are forbidden in map assets: {relative}")
            if info.file_size > MAX_FILE_BYTES:
                raise ValueError(f"map asset exceeds per-file limit: {relative}")
            suffix = Path(relative.name).suffix.lower()
            if (
                suffix not in ALLOWED_EXTENSIONS
                and relative.name not in ALLOWED_BASENAMES
            ):
                continue
            total += info.file_size
            if total > MAX_TOTAL_BYTES:
                raise ValueError("map asset set exceeds the configured total size limit")
            normalized = relative.as_posix().casefold()
            if normalized in extracted_paths:
                raise ValueError(f"duplicate or case-colliding archive path: {relative}")
            extracted_paths.add(normalized)
            destination = extract_root.joinpath(*relative.parts)
            destination.parent.mkdir(parents=True, exist_ok=True)
            with bundle.open(info) as source, destination.open("wb") as output:
                shutil.copyfileobj(source, output)
    root = extract_root / top
    if not root.is_dir():
        raise ValueError("map asset archive extraction failed")
    return root


def is_map_root(candidate: Path) -> bool:
    return (candidate / "data" / "default-pois.zh-CN.json").is_file()


def find_map_root(source: Path) -> Path:
    source = source.resolve()
    candidates = [source, *(source / relative for relative in COMMON_MAP_ROOTS)]
    for manifest in source.rglob("palpanel-map-assets.json"):
        candidates.append(manifest.parent)
    for pois in source.rglob("default-pois.zh-CN.json"):
        if pois.parent.name == "data":
            candidates.append(pois.parent.parent)
    seen: set[Path] = set()
    for candidate in candidates:
        candidate = candidate.resolve()
        if candidate in seen:
            continue
        seen.add(candidate)
        if is_map_root(candidate):
            return candidate
    raise ValueError(
        f"unable to locate map assets under {source}; expected data/default-pois.zh-CN.json"
    )


def load_source_manifest(source: Path) -> dict[str, Any]:
    path = source / "palpanel-map-assets.json"
    if not path.is_file():
        return {}
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError("source palpanel-map-assets.json must contain an object")
    return value


def verify_source_manifest(source: Path, manifest: dict[str, Any]) -> None:
    schema_version = manifest.get("schema_version")
    if schema_version is not None and schema_version != 1:
        raise ValueError(f"unsupported source manifest schema: {schema_version!r}")
    for path in (("poi_total",), ("pois", "total")):
        value: Any = manifest
        for key in path:
            if not isinstance(value, dict) or key not in value:
                value = None
                break
            value = value[key]
        if value is not None and (not isinstance(value, int) or value <= 0):
            raise ValueError(f"source manifest {'.'.join(path)} must be a positive integer")
    entries = manifest.get("files")
    if entries is None:
        return
    if not isinstance(entries, list):
        raise ValueError("source manifest files must be an array")
    for entry in entries:
        if not isinstance(entry, dict):
            raise ValueError("source manifest contains an invalid file entry")
        relative_text = entry.get("path")
        expected_size = entry.get("size")
        expected_hash = entry.get("sha256")
        if not isinstance(relative_text, str):
            raise ValueError("source manifest file path is invalid")
        relative = PurePosixPath(relative_text)
        ensure_safe_relative(relative)
        path = source.joinpath(*relative.parts)
        if not path.is_file():
            raise ValueError(f"source manifest file is missing: {relative_text}")
        if isinstance(expected_size, int) and path.stat().st_size != expected_size:
            raise ValueError(f"source manifest size mismatch: {relative_text}")
        if isinstance(expected_hash, str) and sha256_file(path) != expected_hash.lower():
            raise ValueError(f"source manifest SHA-256 mismatch: {relative_text}")


def copy_tree(source: Path, destination: Path) -> None:
    total = 0
    copied_paths: set[str] = set()
    for path in sorted(source.rglob("*")):
        relative = path.relative_to(source)
        if (
            not relative.parts
            or relative.as_posix() == "palpanel-map-assets.json"
            or path.name in SKIPPED_BASENAMES
        ):
            continue
        if path.is_symlink():
            raise ValueError(f"symlink is forbidden: {path}")
        if path.is_dir():
            continue
        if (
            path.suffix.lower() not in ALLOWED_EXTENSIONS
            and path.name not in ALLOWED_BASENAMES
        ):
            raise ValueError(f"unexpected map asset extension: {relative}")
        size = path.stat().st_size
        if size <= 0 or size > MAX_FILE_BYTES:
            raise ValueError(f"map asset has invalid size: {relative}")
        total += size
        if total > MAX_TOTAL_BYTES:
            raise ValueError("map asset set exceeds the configured total size limit")
        normalized = relative.as_posix().casefold()
        if normalized in copied_paths:
            raise ValueError(f"duplicate or case-colliding map asset path: {relative}")
        copied_paths.add(normalized)
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)


def expected_tile_paths() -> Iterable[tuple[int, int, int]]:
    for zoom in range(5):
        edge = 2**zoom
        for x in range(edge):
            for y in range(edge):
                yield zoom, x, y


def validate_tiles(root: Path, *, required: bool) -> tuple[bool, dict[str, int]]:
    tiles_root = root / "tiles"
    if not tiles_root.is_dir():
        if required:
            raise ValueError("map asset repository is missing the tiles directory")
        return False, {}
    expected = set(expected_tile_paths())
    counts: dict[str, int] = {}
    for layer in LAYERS:
        layer_dir = tiles_root / layer
        if not layer_dir.is_dir():
            raise ValueError(f"tile source is missing layer {layer}")
        actual: set[tuple[int, int, int]] = set()
        for tile in sorted(layer_dir.rglob("*.webp")):
            if tile.is_symlink():
                raise ValueError(f"tile file may not be a symlink: {tile}")
            size = tile.stat().st_size
            if size <= 0 or size > MAX_FILE_BYTES:
                raise ValueError(f"tile file has invalid size: {tile}")
            with tile.open("rb") as handle:
                header = handle.read(12)
            if len(header) < 12 or header[:4] != b"RIFF" or header[8:12] != b"WEBP":
                raise ValueError(
                    f"tile is not a WebP image: {tile} (Git LFS pointer or corrupt asset)"
                )
            relative = tile.relative_to(layer_dir)
            if len(relative.parts) != 3:
                raise ValueError(f"unexpected tile path: {relative}")
            zoom, x_name, y_name = relative.parts
            if not zoom.isdigit() or not x_name.isdigit() or not y_name.endswith(".webp"):
                raise ValueError(f"unexpected tile path: {relative}")
            y_value = y_name.removesuffix(".webp")
            if not y_value.isdigit():
                raise ValueError(f"unexpected tile path: {relative}")
            actual.add((int(zoom), int(x_name), int(y_value)))
        if actual != expected:
            missing = len(expected - actual)
            extra = len(actual - expected)
            raise ValueError(
                f"tile layer {layer} is incomplete: {len(actual)} files; "
                f"expected {EXPECTED_TILE_COUNT_PER_LAYER} (missing={missing}, extra={extra})"
            )
        counts[layer] = len(actual)
    return True, counts


def copy_tiles(source: Path, destination: Path) -> None:
    if source.is_symlink():
        raise ValueError(f"tile source may not be a symlink: {source}")
    source = source.resolve()
    if source.name != "tiles" and (source / "tiles").is_dir():
        source = source / "tiles"
    staged = destination / "tiles"
    shutil.rmtree(staged, ignore_errors=True)
    total = 0
    for layer in LAYERS:
        layer_dir = source / layer
        if not layer_dir.is_dir():
            raise ValueError(f"tile source is missing layer {layer}")
        for tile in sorted(layer_dir.rglob("*.webp")):
            if tile.is_symlink():
                raise ValueError(f"tile file may not be a symlink: {tile}")
            size = tile.stat().st_size
            if size <= 0 or size > MAX_FILE_BYTES:
                raise ValueError(f"tile file has invalid size: {tile}")
            total += size
            if total > MAX_TOTAL_BYTES:
                raise ValueError("tile source exceeds the configured total size limit")
            relative = tile.relative_to(source)
            target = staged / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(tile, target)


def validate_pois(destination: Path, expected_total: int) -> tuple[int, dict[str, int]]:
    canonical: dict[str, tuple[str, str, float, float, float, float]] | None = None
    category_counts: dict[str, int] = {}
    detected_total = 0
    for locale in LOCALES:
        path = destination / "data" / f"default-pois.{locale}.json"
        if not path.is_file():
            raise ValueError(f"missing POI locale file: {path}")
        items = json.loads(path.read_text(encoding="utf-8"))
        if not isinstance(items, list) or not items:
            raise ValueError(f"{path.name} must contain a non-empty array")
        if detected_total == 0:
            detected_total = len(items)
        if len(items) != detected_total:
            raise ValueError(f"localized POI count differs in {path.name}")
        if expected_total > 0 and len(items) != expected_total:
            raise ValueError(f"{path.name} contains {len(items)} records; expected {expected_total}")
        current: dict[str, tuple[str, str, float, float, float, float]] = {}
        for item in items:
            if not isinstance(item, dict):
                raise ValueError(f"invalid POI record in {path.name}")
            poi_id = item.get("id")
            category = item.get("category")
            map_id = item.get("map")
            aliases = item.get("aliases")
            keywords = item.get("keywords")
            if (
                not isinstance(poi_id, str) or not poi_id
                or not isinstance(item.get("type"), str) or not item["type"]
                or not isinstance(category, str) or not category
                or map_id not in LAYERS
                or not isinstance(item.get("name"), str) or not item["name"]
                or not isinstance(aliases, list)
                or not all(isinstance(value, str) for value in aliases)
                or not isinstance(keywords, list)
                or not all(isinstance(value, str) for value in keywords)
                or not all(
                    isinstance(item.get(key), str)
                    for key in ("source", "license", "version", "iconId")
                )
            ):
                raise ValueError(f"invalid POI identity in {path.name}")
            try:
                coords = tuple(float(item[key]) for key in ("mapX", "mapY", "worldX", "worldY"))
            except (KeyError, TypeError, ValueError) as exc:
                raise ValueError(f"invalid POI coordinates in {path.name}") from exc
            if not all(math.isfinite(value) for value in coords):
                raise ValueError(f"non-finite POI coordinates in {path.name}")
            current[poi_id] = (str(map_id), category, *coords)
            if locale == LOCALES[0]:
                category_counts[category] = category_counts.get(category, 0) + 1
        if len(current) != detected_total:
            raise ValueError(f"duplicate POI IDs in {path.name}")
        if canonical is None:
            canonical = current
        elif current != canonical:
            raise ValueError(f"localized POI identity or coordinates differ in {path.name}")
    return detected_total, dict(sorted(category_counts.items()))


def manifest_integer(manifest: dict[str, Any], *keys: str) -> int:
    value: Any = manifest
    for key in keys:
        if not isinstance(value, dict):
            return 0
        value = value.get(key)
    return value if isinstance(value, int) and value > 0 else 0


def manifest_text(manifest: dict[str, Any], *keys: str) -> str:
    value: Any = manifest
    for key in keys:
        if not isinstance(value, dict):
            return ""
        value = value.get(key)
    return value.strip() if isinstance(value, str) else ""


def write_manifest(
    destination: Path,
    *,
    repository: str,
    commit: str,
    ref: str,
    source_manifest: dict[str, Any],
    poi_total: int,
    categories: dict[str, int],
    tile_counts: dict[str, int],
) -> None:
    files = []
    for path in sorted(destination.rglob("*")):
        if not path.is_file() or path.name == "palpanel-map-assets.json":
            continue
        files.append({
            "path": path.relative_to(destination).as_posix(),
            "size": path.stat().st_size,
            "sha256": sha256_file(path),
        })
    source_version = (
        manifest_text(source_manifest, "asset_version")
        or manifest_text(source_manifest, "version")
        or manifest_text(source_manifest, "source", "version")
        or ref
    )
    dataset_version = (
        manifest_text(source_manifest, "dataset_version")
        or manifest_text(source_manifest, "dataset", "version")
        or source_version
    )
    manifest = {
        "schema_version": 1,
        "source": {
            "repository": repository,
            "commit": commit,
            "ref": ref,
            "version": source_version,
        },
        "dataset_version": dataset_version,
        "maps": list(LAYERS),
        "locales": list(LOCALES),
        "poi_total": poi_total,
        "category_counts": categories,
        "tiles_available": bool(tile_counts),
        "tile_policy": "bundled from the dedicated PalPanel map asset repository",
        "tiles": {
            "format": "webp",
            "template": "tiles/{map}/{z}/{x}/{y}.webp",
            "tile_size": 512,
            "minimum_zoom": 0,
            "maximum_zoom": 4,
            "counts": tile_counts,
        },
        "files": files,
    }
    (destination / "palpanel-map-assets.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )


def main() -> int:
    args = parse_args()
    if args.source_dir and args.archive:
        raise ValueError("--source-dir and --archive are mutually exclusive")
    repository = args.repository.strip()
    ref = args.ref.strip()
    if not repository or not ref:
        raise ValueError("map asset repository and ref may not be empty")

    with tempfile.TemporaryDirectory(prefix="palpanel-map-assets-") as temp_name:
        temp = Path(temp_name)
        source_commit = args.source_commit.strip()
        if args.source_dir:
            repository_root = args.source_dir.resolve()
            map_root = find_map_root(repository_root)
            source_commit = source_commit or f"local-{sha256_tree(map_root)}"
        else:
            archive = args.archive
            if archive is None:
                if not args.allow_network:
                    raise ValueError("provide --source-dir/--archive or enable --allow-network")
                token = github_token()
                source_commit = resolve_repository_commit(repository, ref, token)
                archive = temp / "map-assets.zip"
                download_repository_archive(archive, repository, source_commit, token)
            else:
                archive = archive.resolve()
                if archive.stat().st_size > MAX_ARCHIVE_BYTES:
                    raise ValueError("map asset archive exceeds the configured size limit")
                source_commit = source_commit or f"archive-{sha256_file(archive)}"
            repository_root = extract_archive(archive, temp / "archive")
            map_root = find_map_root(repository_root)

        source_manifest = load_source_manifest(map_root)
        verify_source_manifest(map_root, source_manifest)

        staged = temp / "staged"
        staged.mkdir()
        copy_tree(map_root, staged)
        if args.tiles_source_dir:
            copy_tiles(args.tiles_source_dir, staged)

        expected_poi_total = (
            args.expected_poi_total
            or manifest_integer(source_manifest, "poi_total")
            or manifest_integer(source_manifest, "pois", "total")
        )
        poi_total, categories = validate_pois(staged, expected_poi_total)
        tiles_available, tile_counts = validate_tiles(
            staged,
            required=not args.allow_missing_tiles,
        )
        if not tiles_available:
            tile_counts = {}
        write_manifest(
            staged,
            repository=repository,
            commit=source_commit,
            ref=ref,
            source_manifest=source_manifest,
            poi_total=poi_total,
            categories=categories,
            tile_counts=tile_counts,
        )

        destination = args.destination.resolve()
        replacement = destination.with_name(destination.name + ".new")
        shutil.rmtree(replacement, ignore_errors=True)
        shutil.copytree(staged, replacement)
        shutil.rmtree(destination, ignore_errors=True)
        replacement.rename(destination)

    print(
        f"[palpanel] synchronized {repository}@{source_commit[:12]}: "
        f"{poi_total} POIs, tiles={'yes' if tiles_available else 'no'}"
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, json.JSONDecodeError, zipfile.BadZipFile) as exc:
        print(f"map asset sync failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
