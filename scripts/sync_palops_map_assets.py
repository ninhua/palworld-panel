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
import tarfile
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
    ".json", ".geojson", ".webp", ".png", ".svg", ".txt", ".md", ".css", ".woff", ".woff2"
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
    parser.add_argument(
        "--release-tag",
        default=os.environ.get("PALPANEL_MAP_ASSETS_RELEASE_TAG", "latest"),
        help="GitHub Release tag containing tile archives, or latest",
    )
    parser.add_argument(
        "--release-asset",
        default=os.environ.get("PALPANEL_MAP_ASSETS_RELEASE_ASSET", ""),
        help="Exact GitHub Release asset name; auto-detected when omitted",
    )
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


def github_request(
    url: str,
    token: str = "",
    *,
    accept: str = "application/vnd.github+json",
) -> urllib.request.Request:
    headers = {
        "Accept": accept,
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




def release_metadata(repository: str, release_tag: str, token: str = "") -> dict[str, Any]:
    owner, name = repository_parts(repository)
    if not release_tag or release_tag == "latest":
        url = f"https://api.github.com/repos/{owner}/{name}/releases/latest"
    else:
        encoded_tag = urllib.parse.quote(release_tag, safe="")
        url = f"https://api.github.com/repos/{owner}/{name}/releases/tags/{encoded_tag}"
    try:
        return read_json_url(url, token)
    except ValueError as exc:
        raise ValueError(
            f"unable to read map asset GitHub Release {release_tag!r}; "
            "verify the release exists and the token has contents:read access"
        ) from exc


def archive_asset_name(name: str) -> bool:
    lowered = name.lower()
    return lowered.endswith((".zip", ".tar.gz", ".tgz", ".tar"))


def select_release_assets(
    metadata: dict[str, Any],
    requested_name: str,
) -> list[dict[str, Any]]:
    raw_assets = metadata.get("assets")
    if not isinstance(raw_assets, list):
        raise ValueError("map asset GitHub Release returned an invalid asset list")
    assets = [asset for asset in raw_assets if isinstance(asset, dict)]
    if requested_name:
        selected = [asset for asset in assets if asset.get("name") == requested_name]
        if not selected:
            available = ", ".join(str(asset.get("name", "")) for asset in assets) or "none"
            raise ValueError(
                f"map asset Release does not contain {requested_name!r}; available assets: {available}"
            )
        return selected
    archives = [
        asset for asset in assets
        if isinstance(asset.get("name"), str) and archive_asset_name(asset["name"])
    ]
    strong_keywords = ("palops", "map", "tile", "palpagos", "world-tree", "worldtree")
    preferred = [
        asset for asset in archives
        if any(keyword in asset["name"].lower() for keyword in strong_keywords)
    ]
    if preferred:
        return preferred
    weak_keywords = ("palpanel", "poi", "asset")
    fallback = [
        asset for asset in archives
        if any(keyword in asset["name"].lower() for keyword in weak_keywords)
    ]
    if len(fallback) == 1:
        return fallback
    if len(archives) == 1:
        return archives
    available = ", ".join(str(asset.get("name", "")) for asset in assets) or "none"
    raise ValueError(
        "unable to choose a map asset Release archive automatically; "
        f"set PALPANEL_MAP_ASSETS_RELEASE_ASSET. Available assets: {available}"
    )


def download_release_asset(
    target: Path,
    asset: dict[str, Any],
    token: str = "",
) -> None:
    url = asset.get("url")
    expected_size = asset.get("size")
    name = asset.get("name")
    if not isinstance(url, str) or not isinstance(name, str):
        raise ValueError("map asset GitHub Release contains invalid asset metadata")
    if isinstance(expected_size, int) and expected_size > MAX_ARCHIVE_BYTES:
        raise ValueError(f"map asset Release archive exceeds the size limit: {name}")
    try:
        request = github_request(url, token, accept="application/octet-stream")
        with urllib.request.urlopen(request, timeout=300) as response, target.open("wb") as output:
            total = 0
            while True:
                chunk = response.read(1024 * 1024)
                if not chunk:
                    break
                total += len(chunk)
                if total > MAX_ARCHIVE_BYTES:
                    raise ValueError(f"map asset Release archive exceeds the size limit: {name}")
                output.write(chunk)
    except urllib.error.HTTPError as exc:
        if exc.code in {401, 403, 404}:
            raise ValueError(
                f"unable to download map asset Release archive {name!r}; "
                "provide PALPANEL_MAP_ASSETS_TOKEN with contents:read access"
            ) from exc
        raise


def extract_release_archive(archive: Path, extract_root: Path) -> Path:
    extract_root.mkdir(parents=True, exist_ok=True)
    total = 0
    extracted_paths: set[str] = set()

    def prepare(relative_text: str, size: int, mode: int = 0) -> Path | None:
        nonlocal total
        relative = PurePosixPath(relative_text)
        ensure_safe_relative(relative)
        if stat.S_ISLNK(mode):
            raise ValueError(f"symlinks are forbidden in map Release assets: {relative}")
        if size > MAX_FILE_BYTES:
            raise ValueError(f"map Release asset exceeds per-file limit: {relative}")
        suffix = Path(relative.name).suffix.lower()
        if suffix not in ALLOWED_EXTENSIONS and relative.name not in ALLOWED_BASENAMES:
            return None
        total += size
        if total > MAX_TOTAL_BYTES:
            raise ValueError("map Release asset set exceeds the configured total size limit")
        normalized = relative.as_posix().casefold()
        if normalized in extracted_paths:
            raise ValueError(f"duplicate or case-colliding Release asset path: {relative}")
        extracted_paths.add(normalized)
        destination = extract_root.joinpath(*relative.parts)
        destination.parent.mkdir(parents=True, exist_ok=True)
        return destination

    lowered = archive.name.lower()
    if lowered.endswith(".zip"):
        with zipfile.ZipFile(archive) as bundle:
            for info in bundle.infolist():
                if info.is_dir():
                    continue
                destination = prepare(info.filename, info.file_size, info.external_attr >> 16)
                if destination is None:
                    continue
                with bundle.open(info) as source, destination.open("wb") as output:
                    shutil.copyfileobj(source, output)
    elif lowered.endswith((".tar.gz", ".tgz", ".tar")):
        with tarfile.open(archive, mode="r:*") as bundle:
            for member in bundle.getmembers():
                if member.isdir():
                    continue
                if member.issym() or member.islnk():
                    raise ValueError(f"links are forbidden in map Release assets: {member.name}")
                if not member.isfile():
                    continue
                destination = prepare(member.name, member.size, member.mode)
                if destination is None:
                    continue
                source = bundle.extractfile(member)
                if source is None:
                    raise ValueError(f"unable to read map Release asset member: {member.name}")
                with source, destination.open("wb") as output:
                    shutil.copyfileobj(source, output)
    else:
        raise ValueError(f"unsupported map Release archive format: {archive.name}")
    return extract_root


def download_release_tile_archives(
    destination: Path,
    repository: str,
    release_tag: str,
    release_asset: str,
    token: str,
) -> tuple[str, list[str]]:
    metadata = release_metadata(repository, release_tag, token)
    selected = select_release_assets(metadata, release_asset)
    resolved_tag = metadata.get("tag_name")
    if not isinstance(resolved_tag, str) or not resolved_tag:
        resolved_tag = release_tag
    declared_total = sum(
        asset.get("size", 0) for asset in selected if isinstance(asset.get("size"), int)
    )
    if declared_total > MAX_TOTAL_BYTES:
        raise ValueError("selected map Release archives exceed the configured total size limit")
    downloaded: list[str] = []
    for index, asset in enumerate(selected):
        name = str(asset["name"])
        archive = destination.parent / f"release-{index}-{Path(name).name}"
        download_release_asset(archive, asset, token)
        extract_release_archive(archive, destination / f"asset-{index}")
        downloaded.append(name)
    return resolved_tag, downloaded


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


def is_normalized_map_root(candidate: Path) -> bool:
    return (candidate / "data" / "default-pois.zh-CN.json").is_file()


def is_poi_json_path(path: Path) -> bool:
    if path.suffix.lower() not in {".json", ".geojson"}:
        return False
    if path.name in {"palpanel-map-assets.json", "package.json", "package-lock.json", "tsconfig.json"}:
        return False
    return not path.name.endswith(".schema.json")


def is_xyz_tile_path(path: Path) -> bool:
    if path.suffix.lower() != ".webp" or len(path.parts) < 4:
        return False
    zoom, x_name, y_name = path.parts[-3:]
    return zoom.isdigit() and x_name.isdigit() and y_name.removesuffix(".webp").isdigit()


def has_discoverable_pois(candidate: Path) -> bool:
    return any(is_poi_json_path(path) for path in candidate.rglob("*"))


def has_discoverable_assets(candidate: Path) -> bool:
    return has_discoverable_pois(candidate) and any(
        is_xyz_tile_path(path.relative_to(candidate)) for path in candidate.rglob("*.webp")
    )


def find_map_root(source: Path, *, require_tiles: bool = True) -> Path:
    source = source.resolve()
    candidates = [source, *(source / relative for relative in COMMON_MAP_ROOTS)]
    for manifest in source.rglob("palpanel-map-assets.json"):
        candidates.append(manifest.parent)
    for pois in source.rglob("default-pois.zh-CN.json"):
        if pois.parent.name == "data":
            candidates.append(pois.parent.parent)
    seen: set[Path] = set()
    resolved_candidates: list[Path] = []
    for candidate in candidates:
        candidate = candidate.resolve()
        if candidate in seen or not candidate.is_dir():
            continue
        seen.add(candidate)
        resolved_candidates.append(candidate)
        if is_normalized_map_root(candidate):
            if not require_tiles or any(is_xyz_tile_path(path.relative_to(candidate)) for path in candidate.rglob("*.webp")):
                return candidate
    predicate = has_discoverable_assets if require_tiles else has_discoverable_pois
    for candidate in resolved_candidates:
        if predicate(candidate):
            return candidate
    if predicate(source):
        return source
    json_candidates = sorted(
        path.relative_to(source).as_posix()
        for path in source.rglob("*")
        if path.is_file() and path.suffix.lower() in {".json", ".geojson"}
    )[:20]
    suffix = f"; JSON files found: {', '.join(json_candidates)}" if json_candidates else "; no JSON files found"
    raise ValueError(
        f"unable to locate map assets under {source}; expected localized/default POI JSON"
        f"{' and XYZ WebP tiles' if require_tiles else ''}{suffix}"
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



def prune_noncanonical_tiles(root: Path) -> None:
    for tile in sorted(root.rglob("*.webp")):
        if not tile.is_file():
            continue
        relative = tile.relative_to(root)
        canonical = (
            len(relative.parts) == 5
            and relative.parts[0] == "tiles"
            and relative.parts[1] in LAYERS
            and is_xyz_tile_path(Path(*relative.parts[1:]))
        )
        if is_xyz_tile_path(relative) and not canonical:
            tile.unlink()
    for directory in sorted((path for path in root.rglob("*") if path.is_dir()), reverse=True):
        try:
            directory.rmdir()
        except OSError:
            pass


def localized_tokens(locale: str) -> tuple[str, ...]:
    if locale == "zh-CN":
        return ("zh-cn", "zh_cn", "zhcn", "chinese", "simplified", "-cn", "_cn")
    if locale == "en-US":
        return ("en-us", "en_us", "enus", "english", "-en", "_en")
    return ("ja-jp", "ja_jp", "jajp", "japanese", "-ja", "_ja", "-jp", "_jp")


def path_locale(path: Path) -> str | None:
    normalized = path.as_posix().lower()
    for locale in LOCALES:
        if any(token in normalized for token in localized_tokens(locale)):
            return locale
    return None


def poi_context_for_key(key: str, inherited: dict[str, Any]) -> dict[str, Any]:
    context = dict(inherited)
    normalized = key.lower().replace("_", "-").replace(" ", "-")
    map_key = False
    if "tree" in normalized:
        context["map"] = "world-tree"
        map_key = True
    elif any(token in normalized for token in ("palpagos", "palworld", "main-map", "overworld")):
        context["map"] = "palpagos"
        map_key = True
    wrapper_keys = {"pois", "items", "markers", "locations", "data", "features", "points", "records"}
    metadata_keys = {"source", "metadata", "meta", "schema", "version", "license", "licenses", "files"}
    if (
        not map_key
        and normalized not in wrapper_keys | metadata_keys
        and not normalized.startswith(("zh-", "en-", "ja-"))
    ):
        context.setdefault("category", key)
    return context


def apply_poi_context(record: dict[str, Any], context: dict[str, Any]) -> dict[str, Any]:
    result = dict(record)
    if "map" in context and first_value(result, ("map", "mapId", "map_id", "layer", "world")) is None:
        result["map"] = context["map"]
    if "category" in context and first_value(result, ("category", "categoryId", "category_id", "type", "kind")) is None:
        result["category"] = context["category"]
    return result


def unwrap_poi_records(value: Any) -> list[dict[str, Any]] | None:
    records: list[dict[str, Any]] = []

    def visit(current: Any, context: dict[str, Any]) -> None:
        if isinstance(current, list):
            for item in current:
                visit(item, context)
            return
        if not isinstance(current, dict):
            return
        candidate = apply_poi_context(current, context)
        merged = merged_poi_record(candidate)
        identity = first_value(merged, ("id", "poiId", "poi_id", "name", "title", "label", "type", "kind", "category"))
        if identity is not None and poi_coordinates(merged) is not None:
            records.append(candidate)
            return
        for key, nested in current.items():
            if key in {"properties", "geometry"}:
                continue
            visit(nested, poi_context_for_key(str(key), context))

    visit(value, {})
    return records or None


def merged_poi_record(record: dict[str, Any]) -> dict[str, Any]:
    merged = dict(record)
    properties = record.get("properties")
    if isinstance(properties, dict):
        merged.update(properties)
    geometry = record.get("geometry")
    if isinstance(geometry, dict):
        coordinates = geometry.get("coordinates")
        if isinstance(coordinates, list) and len(coordinates) >= 2:
            merged.setdefault("coordinates", coordinates)
    return merged


def nested_value(record: dict[str, Any], key: str) -> Any:
    value: Any = record
    for part in key.split("."):
        if not isinstance(value, dict):
            return None
        value = value.get(part)
    return value


def first_value(record: dict[str, Any], keys: Iterable[str]) -> Any:
    for key in keys:
        value = nested_value(record, key)
        if value is not None:
            return value
    return None


def finite_number(value: Any) -> float | None:
    try:
        number = float(value)
    except (TypeError, ValueError):
        return None
    return number if math.isfinite(number) else None


def poi_coordinates(record: dict[str, Any]) -> tuple[float, float, float, float] | None:
    map_x = finite_number(first_value(record, ("mapX", "map_x", "map.x", "position.x", "x", "lng", "longitude")))
    map_y = finite_number(first_value(record, ("mapY", "map_y", "map.y", "position.y", "y", "lat", "latitude")))
    coordinates = first_value(record, ("coordinates", "coordinate", "coords", "position"))
    if (map_x is None or map_y is None) and isinstance(coordinates, (list, tuple)) and len(coordinates) >= 2:
        map_x = map_x if map_x is not None else finite_number(coordinates[0])
        map_y = map_y if map_y is not None else finite_number(coordinates[1])
    if (map_x is None or map_y is None) and isinstance(coordinates, dict):
        map_x = map_x if map_x is not None else finite_number(first_value(coordinates, ("x", "lng", "longitude")))
        map_y = map_y if map_y is not None else finite_number(first_value(coordinates, ("y", "lat", "latitude")))
    if map_x is None or map_y is None:
        return None
    world_x = finite_number(first_value(record, ("worldX", "world_x", "world.x", "gameX", "game_x")))
    world_y = finite_number(first_value(record, ("worldY", "world_y", "world.y", "gameY", "game_y")))
    return map_x, map_y, world_x if world_x is not None else map_x, world_y if world_y is not None else map_y


def localized_value(value: Any, locale: str) -> str:
    if isinstance(value, str):
        return value.strip()
    if not isinstance(value, dict):
        return ""
    aliases = {
        "zh-CN": ("zh-CN", "zh_CN", "zh", "cn", "chinese"),
        "en-US": ("en-US", "en_US", "en", "english"),
        "ja-JP": ("ja-JP", "ja_JP", "ja", "jp", "japanese"),
    }[locale]
    for key in aliases:
        result = value.get(key)
        if isinstance(result, str) and result.strip():
            return result.strip()
    for result in value.values():
        if isinstance(result, str) and result.strip():
            return result.strip()
    return ""


def localized_record_text(record: dict[str, Any], base: str, locale: str) -> str:
    direct = localized_value(record.get(base), locale)
    if direct:
        return direct
    suffixes = {
        "zh-CN": ("zhCN", "zh_CN", "zh", "cn"),
        "en-US": ("enUS", "en_US", "en"),
        "ja-JP": ("jaJP", "ja_JP", "ja", "jp"),
    }[locale]
    for suffix in suffixes:
        for key in (f"{base}_{suffix}", f"{base}{suffix[0].upper()}{suffix[1:]}"):
            value = record.get(key)
            if isinstance(value, str) and value.strip():
                return value.strip()
    return ""


def string_list(value: Any, locale: str) -> list[str]:
    if isinstance(value, dict):
        for key in localized_tokens(locale):
            candidate = value.get(key)
            if candidate is not None:
                return string_list(candidate, locale)
        for candidate in value.values():
            if isinstance(candidate, (list, str)):
                return string_list(candidate, locale)
    if isinstance(value, list):
        return [item.strip() for item in value if isinstance(item, str) and item.strip()]
    if isinstance(value, str):
        return [item.strip() for item in value.replace(";", ",").split(",") if item.strip()]
    return []


def normalize_layer_id(value: Any, default: str = "palpagos") -> str:
    normalized = str(value or "").lower().replace("_", "-").replace(" ", "-")
    if "tree" in normalized:
        return "world-tree"
    if any(token in normalized for token in ("palpagos", "palworld", "main", "overworld")):
        return "palpagos"
    return default


def layer_from_path(path: Path) -> str:
    normalized = path.as_posix().lower().replace("_", "-")
    return "world-tree" if "tree" in normalized else "palpagos"


def normalize_category(value: Any) -> str:
    raw = str(value or "location").strip().lower().replace("_", "-").replace(" ", "-")
    if raw.startswith("poi-"):
        return raw
    group = "location"
    if any(token in raw for token in ("boss", "enemy", "dungeon", "raid")):
        group = "enemy"
    elif any(token in raw for token in ("ore", "resource", "mining", "sulfur", "coal", "quartz")):
        group = "resource"
    elif any(token in raw for token in ("collect", "chest", "effigy", "journal")):
        group = "collectible"
    elif "npc" in raw or "merchant" in raw or "vendor" in raw:
        group = "npc"
    elif "pal" in raw:
        group = "pal"
    return f"poi-{group}-{raw or group}"


def normalize_poi_record(
    record: dict[str, Any],
    locale: str,
    index: int,
    source_version: str,
    default_layer: str,
) -> dict[str, Any]:
    record = merged_poi_record(record)
    coordinates = poi_coordinates(record)
    if coordinates is None:
        raise ValueError(f"POI record {index + 1} has no usable coordinates")
    map_x, map_y, world_x, world_y = coordinates
    raw_type = first_value(record, ("type", "kind", "category", "group"))
    category = normalize_category(first_value(record, ("category", "categoryId", "category_id", "type", "kind")))
    layer = normalize_layer_id(
        first_value(record, ("map", "mapId", "map_id", "layer", "world")),
        default_layer,
    )
    raw_id = first_value(record, ("id", "poiId", "poi_id", "uid", "uuid", "key"))
    if isinstance(raw_id, (str, int)) and str(raw_id).strip():
        poi_id = str(raw_id).strip()
    else:
        digest = hashlib.sha256(
            f"{layer}|{category}|{map_x:.8f}|{map_y:.8f}|{raw_type or ''}".encode("utf-8")
        ).hexdigest()[:20]
        poi_id = f"poi-import-{digest}"
    name = (
        localized_record_text(record, "name", locale)
        or localized_record_text(record, "title", locale)
        or localized_record_text(record, "label", locale)
        or poi_id
    )
    icon = first_value(record, ("iconId", "icon_id", "icon", "marker", "sprite"))
    source = first_value(record, ("source", "origin", "provider"))
    license_name = first_value(record, ("license", "licence"))
    version = first_value(record, ("version", "datasetVersion", "dataset_version"))
    return {
        "id": poi_id,
        "type": str(raw_type or category).strip(),
        "category": category,
        "map": layer,
        "name": name,
        "aliases": string_list(first_value(record, ("aliases", "alias", "alternateNames", "alternate_names")), locale),
        "keywords": string_list(first_value(record, ("keywords", "tags", "search")), locale),
        "mapX": map_x,
        "mapY": map_y,
        "worldX": world_x,
        "worldY": world_y,
        "source": str(source or "ninhua/palpanel-assets"),
        "license": str(license_name or "repository-defined"),
        "version": str(version or source_version),
        "iconId": str(icon or raw_type or category),
    }


def poi_candidate_score(path: Path, records: list[dict[str, Any]]) -> int:
    score = min(len(records), 1000)
    normalized = path.as_posix().lower()
    if "poi" in normalized:
        score += 5000
    if "default" in normalized:
        score += 1000
    sample = records[:20]
    score += sum(100 for item in sample if poi_coordinates(merged_poi_record(item)) is not None)
    score += sum(20 for item in sample if first_value(merged_poi_record(item), ("name", "title", "label")) is not None)
    return score


def discover_poi_datasets(root: Path) -> list[tuple[Path, list[dict[str, Any]]]]:
    candidates: list[tuple[int, Path, list[dict[str, Any]]]] = []
    for path in sorted(root.rglob("*")):
        if not path.is_file() or not is_poi_json_path(path):
            continue
        try:
            records = unwrap_poi_records(json.loads(path.read_text(encoding="utf-8-sig")))
        except (OSError, UnicodeDecodeError, json.JSONDecodeError):
            continue
        if not records:
            continue
        sample = [merged_poi_record(item) for item in records[:20]]
        has_coordinates = any(poi_coordinates(item) is not None for item in sample)
        has_identity = any(
            first_value(item, ("id", "poiId", "poi_id", "name", "title", "label", "type", "kind", "category")) is not None
            for item in sample
        )
        if not has_coordinates or not has_identity:
            continue
        candidates.append((poi_candidate_score(path, records), path, records))
    return [(path, records) for _, path, records in sorted(candidates, key=lambda item: (-item[0], item[1].as_posix()))]


def normalize_pois(root: Path, source_manifest: dict[str, Any], ref: str) -> None:
    datasets = discover_poi_datasets(root)
    if not datasets:
        json_candidates = sorted(
            path.relative_to(root).as_posix()
            for path in root.rglob("*")
            if path.is_file() and path.suffix.lower() in {".json", ".geojson"}
        )[:20]
        raise ValueError(
            "map asset repository contains no recognizable POI dataset; "
            f"JSON candidates: {', '.join(json_candidates) or 'none'}"
        )
    source_version = (
        manifest_text(source_manifest, "asset_version")
        or manifest_text(source_manifest, "version")
        or manifest_text(source_manifest, "source", "version")
        or ref
    )
    generic = [(path, records) for path, records in datasets if path_locale(path.relative_to(root)) is None]
    data_dir = root / "data"
    data_dir.mkdir(parents=True, exist_ok=True)
    for locale in LOCALES:
        localized = [
            (path, records)
            for path, records in datasets
            if path_locale(path.relative_to(root)) == locale
        ]
        selected = localized or generic or datasets
        by_id: dict[str, dict[str, Any]] = {}
        for source_path, records in selected:
            default_layer = layer_from_path(source_path.relative_to(root))
            for index, record in enumerate(records):
                item = normalize_poi_record(
                    record,
                    locale,
                    index,
                    source_version,
                    default_layer,
                )
                existing = by_id.get(item["id"])
                if existing is not None:
                    if any(existing[key] != item[key] for key in ("map", "category", "mapX", "mapY", "worldX", "worldY")):
                        raise ValueError(
                            f"conflicting duplicate POI ID {item['id']} in {source_path.relative_to(root)}"
                        )
                    continue
                by_id[item["id"]] = item
        normalized = [by_id[poi_id] for poi_id in sorted(by_id)]
        if not normalized:
            raise ValueError(f"no POI records were produced for locale {locale}")
        target = data_dir / f"default-pois.{locale}.json"
        target.write_text(json.dumps(normalized, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def tile_group_key(root: Path, tile: Path) -> Path:
    relative = tile.relative_to(root)
    return Path(*relative.parts[:-3])


def tile_layer_score(path: Path, layer: str) -> int:
    normalized = ''.join(character for character in path.as_posix().lower() if character.isalnum())
    aliases = {
        "palpagos": ("palpagos", "palworld", "mainmap", "overworld"),
        "world-tree": ("worldtree", "treeworld", "worldtreemap"),
    }[layer]
    return max((len(alias) for alias in aliases if alias in normalized), default=0)


def discover_tile_groups(root: Path) -> dict[Path, dict[tuple[int, int, int], Path]]:
    groups: dict[Path, dict[tuple[int, int, int], Path]] = {}
    for tile in sorted(root.rglob("*.webp")):
        if not tile.is_file() or tile.is_symlink():
            continue
        relative = tile.relative_to(root)
        if not is_xyz_tile_path(relative):
            continue
        zoom_name, x_name, y_name = relative.parts[-3:]
        coordinate = (int(zoom_name), int(x_name), int(y_name.removesuffix(".webp")))
        groups.setdefault(tile_group_key(root, tile), {})[coordinate] = tile
    return groups


def normalize_tiles(root: Path) -> None:
    groups = discover_tile_groups(root)
    expected = set(expected_tile_paths())
    selected: dict[str, tuple[Path, dict[tuple[int, int, int], Path]]] = {}
    used: set[Path] = set()
    for layer in LAYERS:
        ranked = sorted(
            (
                (tile_layer_score(path, layer), len(expected & set(files)), path, files)
                for path, files in groups.items()
                if path not in used
            ),
            key=lambda item: (-item[0], -item[1], item[2].as_posix()),
        )
        if ranked and ranked[0][0] > 0:
            _, _, path, files = ranked[0]
            selected[layer] = (path, files)
            used.add(path)
    unresolved = [layer for layer in LAYERS if layer not in selected]
    remaining = sorted(
        ((len(expected & set(files)), path, files) for path, files in groups.items() if path not in used),
        key=lambda item: (-item[0], item[1].as_posix()),
    )
    if len(unresolved) == 1 and remaining:
        _, path, files = remaining[0]
        selected[unresolved[0]] = (path, files)
    elif len(unresolved) == 2 and len(remaining) == 2 and all(count == len(expected) for count, _, _ in remaining):
        for layer, (_, path, files) in zip(unresolved, sorted(remaining, key=lambda item: item[1].as_posix())):
            selected[layer] = (path, files)
        print(
            "[palpanel] map tile directories did not contain layer names; "
            "assigned them deterministically by path order",
            file=sys.stderr,
        )
    if any(layer not in selected for layer in LAYERS):
        discovered = ", ".join(f"{path.as_posix()} ({len(files)} tiles)" for path, files in groups.items()) or "none"
        raise ValueError(f"unable to identify both map tile layers; discovered XYZ WebP groups: {discovered}")
    temporary = root / ".palpanel-normalized-tiles"
    shutil.rmtree(temporary, ignore_errors=True)
    for layer, (path, files) in selected.items():
        missing = expected - set(files)
        if missing:
            raise ValueError(
                f"tile layer {layer} is incomplete: {len(expected) - len(missing)} files; "
                f"expected {EXPECTED_TILE_COUNT_PER_LAYER} (selected from {path.as_posix()})"
            )
        for zoom, x, y in sorted(expected):
            source = files[(zoom, x, y)]
            target = temporary / layer / str(zoom) / str(x) / f"{y}.webp"
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target)
    shutil.rmtree(root / "tiles", ignore_errors=True)
    temporary.rename(root / "tiles")

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
    release_tag: str = "",
    release_assets: list[str] | None = None,
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
        "release": {
            "tag": release_tag,
            "assets": release_assets or [],
        },
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
        token = github_token()
        source_commit = args.source_commit.strip()
        if args.source_dir:
            repository_root = args.source_dir.resolve()
            map_root = find_map_root(
                repository_root,
                require_tiles=not (args.allow_missing_tiles or args.tiles_source_dir or args.allow_network),
            )
            source_commit = source_commit or f"local-{sha256_tree(map_root)}"
        else:
            archive = args.archive
            if archive is None:
                if not args.allow_network:
                    raise ValueError("provide --source-dir/--archive or enable --allow-network")
                source_commit = resolve_repository_commit(repository, ref, token)
                archive = temp / "map-assets.zip"
                download_repository_archive(archive, repository, source_commit, token)
            else:
                archive = archive.resolve()
                if archive.stat().st_size > MAX_ARCHIVE_BYTES:
                    raise ValueError("map asset archive exceeds the configured size limit")
                source_commit = source_commit or f"archive-{sha256_file(archive)}"
            repository_root = extract_archive(archive, temp / "archive")
            map_root = find_map_root(
                repository_root,
                require_tiles=not (args.allow_missing_tiles or args.tiles_source_dir or args.allow_network),
            )

        source_manifest = load_source_manifest(map_root)
        verify_source_manifest(map_root, source_manifest)

        staged = temp / "staged"
        staged.mkdir()
        copy_tree(map_root, staged)
        normalize_pois(staged, source_manifest, ref)
        release_tag = ""
        release_assets: list[str] = []
        if args.tiles_source_dir:
            copy_tiles(args.tiles_source_dir, staged)
        else:
            source_has_tiles = any(
                is_xyz_tile_path(path.relative_to(staged)) for path in staged.rglob("*.webp")
            )
            release_requested = args.allow_network and args.release_tag.strip().lower() not in {"none", "off", "disabled"}
            if release_requested:
                tile_import = temp / "release-assets"
                try:
                    release_tag, release_assets = download_release_tile_archives(
                        tile_import,
                        repository,
                        args.release_tag.strip() or "latest",
                        args.release_asset.strip(),
                        token,
                    )
                    normalize_tiles(tile_import)
                    copy_tiles(tile_import / "tiles", staged)
                except ValueError as exc:
                    if not source_has_tiles:
                        raise
                    print(
                        f"[palpanel] map Release tiles unavailable ({exc}); using repository archive tiles",
                        file=sys.stderr,
                    )
                    normalize_tiles(staged)
            elif source_has_tiles or not args.allow_missing_tiles:
                normalize_tiles(staged)
        prune_noncanonical_tiles(staged)

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
            release_tag=release_tag,
            release_assets=release_assets,
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
        + (f", release={release_tag} ({', '.join(release_assets)})" if release_assets else "")
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, json.JSONDecodeError, zipfile.BadZipFile, tarfile.TarError) as exc:
        print(f"map asset sync failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
