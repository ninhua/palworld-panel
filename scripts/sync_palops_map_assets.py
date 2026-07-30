#!/usr/bin/env python3
"""Synchronize the open PalOps map data used by PalPanel.

The PalOps raster tiles declare redistributionAllowed=false in their bundled
metadata. This script therefore copies code/data/icons by default and only
copies raster tiles after an explicit rights confirmation or from an operator-
provided tile directory.
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
import urllib.request
import zipfile
from pathlib import Path, PurePosixPath
from typing import Iterable

PALOPS_REPOSITORY = "CoderYiXin/PalOpsWeb"
PALOPS_COMMIT = "dc2ec173c77e759482e59d9b63d228c88132061c"
PALOPS_VERSION = "1.3.2"
EXPECTED_POI_TOTAL = 1251
EXPECTED_TILE_COUNT_PER_LAYER = 341
LOCALES = ("zh-CN", "en-US", "ja-JP")
LAYERS = ("palpagos", "world-tree")
MAP_RELATIVE_ROOT = Path("src/PalOps.Web/wwwroot/map")
ALLOWED_EXTENSIONS = {
    ".json", ".webp", ".png", ".svg", ".txt", ".md", ".css", ".woff", ".woff2"
}
MAX_ARCHIVE_BYTES = 700 * 1024 * 1024
MAX_FILE_BYTES = 32 * 1024 * 1024
MAX_TOTAL_BYTES = 900 * 1024 * 1024


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--destination", type=Path, required=True)
    parser.add_argument("--source-dir", type=Path)
    parser.add_argument("--archive", type=Path)
    parser.add_argument("--allow-network", action="store_true")
    parser.add_argument("--include-repository-tiles", action="store_true")
    parser.add_argument("--tiles-source-dir", type=Path)
    parser.add_argument("--expected-poi-total", type=int, default=EXPECTED_POI_TOTAL, help=argparse.SUPPRESS)
    return parser.parse_args()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def ensure_safe_relative(relative: PurePosixPath) -> None:
    if relative.is_absolute() or any(part in {"", ".", ".."} for part in relative.parts):
        raise ValueError(f"unsafe archive path: {relative}")


def download_archive(target: Path) -> None:
    url = f"https://codeload.github.com/{PALOPS_REPOSITORY}/zip/{PALOPS_COMMIT}"
    request = urllib.request.Request(url, headers={"User-Agent": "PalPanel-map-sync/1"})
    with urllib.request.urlopen(request, timeout=90) as response, target.open("wb") as output:
        total = 0
        while True:
            chunk = response.read(1024 * 1024)
            if not chunk:
                break
            total += len(chunk)
            if total > MAX_ARCHIVE_BYTES:
                raise ValueError("PalOps source archive exceeds the configured size limit")
            output.write(chunk)


def archive_map_root(archive: Path, extract_root: Path, *, include_tiles: bool) -> Path:
    with zipfile.ZipFile(archive) as bundle:
        members = bundle.infolist()
        roots = {PurePosixPath(item.filename).parts[0] for item in members if item.filename}
        if len(roots) != 1:
            raise ValueError("PalOps archive must contain exactly one top-level directory")
        top = next(iter(roots))
        prefix = PurePosixPath(top) / PurePosixPath(MAP_RELATIVE_ROOT.as_posix())
        for info in members:
            relative = PurePosixPath(info.filename)
            ensure_safe_relative(relative)
            if not relative.is_relative_to(prefix):
                continue
            inside = relative.relative_to(prefix)
            if inside.parts and inside.parts[0] == "tiles" and not include_tiles:
                continue
            if info.is_dir():
                continue
            if info.file_size > MAX_FILE_BYTES:
                raise ValueError(f"map asset exceeds per-file limit: {relative}")
            mode = info.external_attr >> 16
            if stat.S_ISLNK(mode):
                raise ValueError(f"symlinks are forbidden in map assets: {relative}")
            suffix = Path(relative.name).suffix.lower()
            if not suffix or suffix not in ALLOWED_EXTENSIONS:
                continue
            destination = extract_root.joinpath(*relative.parts)
            destination.parent.mkdir(parents=True, exist_ok=True)
            with bundle.open(info) as source, destination.open("wb") as output:
                shutil.copyfileobj(source, output)
        candidate = extract_root / Path(*prefix.parts)
        if not candidate.is_dir():
            raise ValueError("PalOps archive does not contain the expected map directory")
        return candidate


def find_map_root(source: Path) -> Path:
    source = source.resolve()
    candidates = [source, source / MAP_RELATIVE_ROOT]
    candidates.extend(path for path in source.glob("*/src/PalOps.Web/wwwroot/map") if path.is_dir())
    for candidate in candidates:
        if (candidate / "data").is_dir():
            return candidate
    raise ValueError(f"unable to locate PalOps map root under {source}")


def copy_tree(source: Path, destination: Path, *, include_tiles: bool) -> None:
    total = 0
    for path in sorted(source.rglob("*")):
        relative = path.relative_to(source)
        if not relative.parts:
            continue
        if relative.parts[0] == "tiles" and not include_tiles:
            continue
        if path.is_symlink():
            raise ValueError(f"symlink is forbidden: {path}")
        if path.is_dir():
            continue
        if path.suffix.lower() not in ALLOWED_EXTENSIONS:
            continue
        size = path.stat().st_size
        if size > MAX_FILE_BYTES:
            raise ValueError(f"map asset exceeds per-file limit: {relative}")
        total += size
        if total > MAX_TOTAL_BYTES:
            raise ValueError("map asset set exceeds the configured total size limit")
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)


def copy_tiles(source: Path, destination: Path) -> None:
    if source.is_symlink():
        raise ValueError(f"tile source may not be a symlink: {source}")
    source = source.resolve()
    if source.name != "tiles" and (source / "tiles").is_dir():
        source = source / "tiles"
    total = 0
    for layer in LAYERS:
        layer_dir = source / layer
        if not layer_dir.is_dir():
            raise ValueError(f"tile source is missing layer {layer}")
        tile_files = sorted(layer_dir.rglob("*.webp"))
        if len(tile_files) != EXPECTED_TILE_COUNT_PER_LAYER:
            raise ValueError(
                f"tile layer {layer} contains {len(tile_files)} files; "
                f"expected {EXPECTED_TILE_COUNT_PER_LAYER}"
            )
        for tile in tile_files:
            if tile.is_symlink():
                raise ValueError(f"tile file may not be a symlink: {tile}")
            size = tile.stat().st_size
            if size <= 0 or size > MAX_FILE_BYTES:
                raise ValueError(f"tile file has invalid size: {tile}")
            total += size
            if total > MAX_TOTAL_BYTES:
                raise ValueError("tile source exceeds total size limit")
            relative = tile.relative_to(source)
            if len(relative.parts) != 4:
                raise ValueError(f"unexpected tile path: {relative}")
            layer_name, zoom, x_name, y_name = relative.parts
            if layer_name != layer or not zoom.isdigit() or not x_name.isdigit() or not y_name.endswith(".webp"):
                raise ValueError(f"unexpected tile path: {relative}")
            target = destination / "tiles" / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(tile, target)


def validate_pois(destination: Path, expected_total: int) -> tuple[int, dict[str, int]]:
    canonical: dict[str, tuple[str, float, float, float, float]] | None = None
    category_counts: dict[str, int] = {}
    for locale in LOCALES:
        path = destination / "data" / f"default-pois.{locale}.json"
        if not path.is_file():
            raise ValueError(f"missing POI locale file: {path}")
        items = json.loads(path.read_text(encoding="utf-8"))
        if not isinstance(items, list) or len(items) != expected_total:
            raise ValueError(f"{path.name} contains {len(items) if isinstance(items, list) else 'invalid'} records; expected {expected_total}")
        current: dict[str, tuple[str, str, float, float, float, float]] = {}
        for item in items:
            if not isinstance(item, dict):
                raise ValueError(f"invalid POI record in {path.name}")
            poi_id = item.get("id")
            category = item.get("category")
            map_id = item.get("map")
            if (
                not isinstance(poi_id, str) or not poi_id
                or not isinstance(category, str) or not category
                or map_id not in LAYERS
                or not isinstance(item.get("name"), str)
                or not isinstance(item.get("aliases"), list)
                or not isinstance(item.get("keywords"), list)
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
        if len(current) != expected_total:
            raise ValueError(f"duplicate POI IDs in {path.name}")
        if canonical is None:
            canonical = current
        elif current != canonical:
            raise ValueError(f"localized POI identity or coordinates differ in {path.name}")
    return expected_total, dict(sorted(category_counts.items()))


def write_manifest(destination: Path, poi_total: int, categories: dict[str, int], tiles_available: bool) -> None:
    files = []
    for path in sorted(destination.rglob("*")):
        if not path.is_file() or path.name == "palpanel-map-assets.json":
            continue
        files.append({
            "path": path.relative_to(destination).as_posix(),
            "size": path.stat().st_size,
            "sha256": sha256_file(path),
        })
    manifest = {
        "schema_version": 1,
        "source": {
            "repository": PALOPS_REPOSITORY,
            "commit": PALOPS_COMMIT,
            "version": PALOPS_VERSION,
        },
        "maps": list(LAYERS),
        "locales": list(LOCALES),
        "poi_total": poi_total,
        "category_counts": categories,
        "tiles_available": tiles_available,
        "tile_policy": (
            "operator-confirmed local import"
            if tiles_available
            else "not bundled: PalOps metadata marks the referenced raster tiles as non-redistributable"
        ),
        "files": files,
    }
    (destination / "palpanel-map-assets.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )


def main() -> int:
    args = parse_args()
    tile_import_requested = bool(args.include_repository_tiles or args.tiles_source_dir)
    if tile_import_requested and os.environ.get("PALPANEL_PALOPS_TILE_RIGHTS_CONFIRMED", "").lower() != "true":
        raise ValueError("tile import requires PALPANEL_PALOPS_TILE_RIGHTS_CONFIRMED=true")

    with tempfile.TemporaryDirectory(prefix="palpanel-palops-map-") as temp_name:
        temp = Path(temp_name)
        if args.source_dir:
            map_root = find_map_root(args.source_dir)
        else:
            archive = args.archive
            if archive is None:
                if not args.allow_network:
                    raise ValueError("provide --source-dir/--archive or enable --allow-network")
                archive = temp / "palops.zip"
                download_archive(archive)
            map_root = archive_map_root(archive.resolve(), temp / "archive", include_tiles=args.include_repository_tiles)

        staged = temp / "staged"
        staged.mkdir()
        copy_tree(map_root, staged, include_tiles=args.include_repository_tiles)

        if args.tiles_source_dir:
            copy_tiles(args.tiles_source_dir, staged)
        elif args.include_repository_tiles:
            if not (staged / "tiles").is_dir():
                copy_tiles(map_root / "tiles", staged)

        poi_total, categories = validate_pois(staged, args.expected_poi_total)
        tiles_available = all(
            len(list((staged / "tiles" / layer).rglob("*.webp"))) == EXPECTED_TILE_COUNT_PER_LAYER
            for layer in LAYERS
        )
        write_manifest(staged, poi_total, categories, tiles_available)

        destination = args.destination.resolve()
        replacement = destination.with_name(destination.name + ".new")
        shutil.rmtree(replacement, ignore_errors=True)
        shutil.copytree(staged, replacement)
        shutil.rmtree(destination, ignore_errors=True)
        replacement.rename(destination)

    print(
        f"[palpanel] synchronized PalOps {PALOPS_VERSION} map data: "
        f"{poi_total} POIs, tiles={'yes' if tiles_available else 'no'}"
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, json.JSONDecodeError, zipfile.BadZipFile) as exc:
        print(f"palops map sync failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
