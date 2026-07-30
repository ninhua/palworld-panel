#!/usr/bin/env python3
"""Synchronize the pinned self-hosted MapLibre GL JS runtime for PalPanel."""
from __future__ import annotations

import argparse
import hashlib
import json
import shutil
import sys
import tempfile
import urllib.request
from pathlib import Path

# MapLibre GL JS 6.0.0 is WebGL2-only and its initial release can fail during
# context setup on otherwise WebGL-capable browsers. Keep the latest maintained
# v5 line, which supports both WebGL2 and WebGL1, until the v6 initialization
# path is proven across PalPanel's browser matrix.
MAPLIBRE_VERSION = "5.24.0"
MAPLIBRE_BASE_URL = f"https://unpkg.com/maplibre-gl@{MAPLIBRE_VERSION}"
FILES = {
    "maplibre-gl.js": ("dist/maplibre-gl.js", 500_000, 4_000_000),
    "maplibre-gl.css": ("dist/maplibre-gl.css", 10_000, 500_000),
    "LICENSE.txt": ("LICENSE.txt", 500, 100_000),
}
MAX_TOTAL_BYTES = 6 * 1024 * 1024


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--destination", type=Path, required=True)
    parser.add_argument("--source-dir", type=Path)
    parser.add_argument("--allow-network", action="store_true")
    parser.add_argument("--skip-size-checks", action="store_true", help=argparse.SUPPRESS)
    return parser.parse_args()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def locate_source_file(source: Path, relative: str, name: str) -> Path:
    candidates = [source / relative, source / name]
    if name == "LICENSE.txt":
        candidates.extend([source / "dist" / name, source.parent / name])
    for candidate in candidates:
        if candidate.is_symlink():
            raise ValueError(f"MapLibre source file may not be a symlink: {candidate}")
        if candidate.is_file():
            return candidate
    raise ValueError(f"MapLibre source is missing {relative}")


def download_file(relative: str, target: Path, maximum: int) -> None:
    url = f"{MAPLIBRE_BASE_URL}/{relative}"
    request = urllib.request.Request(url, headers={"User-Agent": "PalPanel-maplibre-sync/1"})
    with urllib.request.urlopen(request, timeout=90) as response, target.open("wb") as output:
        content_type = response.headers.get("Content-Type", "").lower()
        if "text/html" in content_type:
            raise ValueError(f"MapLibre download returned HTML for {relative}")
        total = 0
        while True:
            chunk = response.read(1024 * 1024)
            if not chunk:
                break
            total += len(chunk)
            if total > maximum:
                raise ValueError(f"MapLibre download exceeds the size limit: {relative}")
            output.write(chunk)


def validate_file(path: Path, minimum: int, maximum: int, skip_size_checks: bool) -> None:
    size = path.stat().st_size
    if size <= 0 or size > maximum:
        raise ValueError(f"MapLibre asset has invalid size: {path.name} ({size})")
    if not skip_size_checks and size < minimum:
        raise ValueError(f"MapLibre asset is unexpectedly small: {path.name} ({size})")


def validate_runtime(module: str) -> None:
    if MAPLIBRE_VERSION not in module:
        raise ValueError("MapLibre bundle does not report the pinned version")
    if "maplibregl" not in module:
        raise ValueError("MapLibre bundle does not expose the expected browser runtime")


def main() -> int:
    args = parse_args()
    if args.source_dir is None and not args.allow_network:
        raise ValueError("provide --source-dir or enable --allow-network")

    with tempfile.TemporaryDirectory(prefix="palpanel-maplibre-") as temp_name:
        staged = Path(temp_name) / "staged"
        staged.mkdir()
        total = 0
        manifest_files = []
        for name, (relative, minimum, maximum) in FILES.items():
            target = staged / name
            if args.source_dir:
                source = locate_source_file(args.source_dir.resolve(), relative, name)
                shutil.copy2(source, target)
            else:
                download_file(relative, target, maximum)
            validate_file(target, minimum, maximum, args.skip_size_checks)
            total += target.stat().st_size
            if total > MAX_TOTAL_BYTES:
                raise ValueError("MapLibre runtime exceeds total size limit")
            manifest_files.append({
                "path": name,
                "size": target.stat().st_size,
                "sha256": sha256_file(target),
            })

        validate_runtime((staged / "maplibre-gl.js").read_text(encoding="utf-8", errors="strict"))

        manifest = {
            "schema_version": 1,
            "name": "maplibre-gl",
            "version": MAPLIBRE_VERSION,
            "source": MAPLIBRE_BASE_URL,
            "distribution": "browser-umd",
            "webgl": [1, 2],
            "license": "BSD-3-Clause",
            "files": manifest_files,
        }
        (staged / "palpanel-maplibre-assets.json").write_text(
            json.dumps(manifest, indent=2) + "\n", encoding="utf-8"
        )

        destination = args.destination.resolve()
        replacement = destination.with_name(destination.name + ".new")
        shutil.rmtree(replacement, ignore_errors=True)
        shutil.copytree(staged, replacement)
        shutil.rmtree(destination, ignore_errors=True)
        replacement.rename(destination)

    print(f"[palpanel] synchronized MapLibre GL JS {MAPLIBRE_VERSION} (WebGL1/2 runtime)")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, UnicodeDecodeError) as exc:
        print(f"maplibre sync failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
