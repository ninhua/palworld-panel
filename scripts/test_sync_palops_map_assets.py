#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import importlib.util
import json
import shutil
import subprocess
import sys
import tempfile
import tarfile
import unittest
from unittest import mock
import zipfile
from pathlib import Path

SCRIPT = Path(__file__).with_name("sync_palops_map_assets.py")
LAYERS = ("palpagos", "world-tree")

SPEC = importlib.util.spec_from_file_location("sync_palops_map_assets", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
SYNC_MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SYNC_MODULE)


class SyncPalOpsMapAssetsTest(unittest.TestCase):
    def make_source(self, root: Path, total: int = 2, *, include_manifest: bool = True) -> Path:
        map_root = root / "map" / "palops"
        (map_root / "data").mkdir(parents=True)
        records = [
            {
                "id": f"poi-{index}",
                "type": "fast-travel",
                "category": "poi-location-fast-travel",
                "map": "palpagos" if index == 0 else "world-tree",
                "name": f"POI {index}",
                "aliases": [],
                "keywords": [],
                "mapX": float(index),
                "mapY": float(index + 1),
                "worldX": float(index + 2),
                "worldY": float(index + 3),
                "source": "fixture",
                "license": "CC-BY-SA-4.0",
                "version": "fixture",
                "iconId": "fast-travel",
            }
            for index in range(total)
        ]
        for locale in ("zh-CN", "en-US", "ja-JP"):
            localized = [{**record, "name": f"{locale} {record['id']}"} for record in records]
            (map_root / "data" / f"default-pois.{locale}.json").write_text(
                json.dumps(localized), encoding="utf-8"
            )
        (map_root / "licenses").mkdir()
        (map_root / "licenses" / "README.md").write_text("fixture license", encoding="utf-8")
        for layer in LAYERS:
            for zoom in range(5):
                edge = 2**zoom
                for x in range(edge):
                    for y in range(edge):
                        tile = map_root / "tiles" / layer / str(zoom) / str(x) / f"{y}.webp"
                        tile.parent.mkdir(parents=True, exist_ok=True)
                        payload = f"{layer}:{zoom}:{x}:{y}".encode()
                        tile.write_bytes(b"RIFF" + (len(payload) + 4).to_bytes(4, "little") + b"WEBP" + payload)
        if include_manifest:
            poi_path = map_root / "data" / "default-pois.zh-CN.json"
            manifest = {
                "schema_version": 1,
                "asset_version": "fixture-assets-1",
                "dataset_version": "fixture-dataset-1",
                "poi_total": total,
                "files": [{
                    "path": "data/default-pois.zh-CN.json",
                    "size": poi_path.stat().st_size,
                    "sha256": hashlib.sha256(poi_path.read_bytes()).hexdigest(),
                }],
            }
            (map_root / "palpanel-map-assets.json").write_text(json.dumps(manifest), encoding="utf-8")
        return root

    def run_sync(self, source: Path, destination: Path, *extra: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [
                "python3", str(SCRIPT),
                "--source-dir", str(source),
                "--destination", str(destination),
                "--repository", "ninhua/palpanel-assets",
                "--ref", "fixture",
                "--expected-poi-total", "2",
                *extra,
            ],
            text=True,
            capture_output=True,
        )


    def test_selects_map_release_archives_and_honors_exact_name(self) -> None:
        metadata = {
            "assets": [
                {"name": "checksums.txt", "url": "https://example.invalid/checksums"},
                {"name": "palpanel-map-tiles.zip", "url": "https://example.invalid/tiles"},
                {"name": "server-source.tar.gz", "url": "https://example.invalid/source"},
            ]
        }
        selected = SYNC_MODULE.select_release_assets(metadata, "")
        self.assertEqual([asset["name"] for asset in selected], ["palpanel-map-tiles.zip"])
        exact = SYNC_MODULE.select_release_assets(metadata, "server-source.tar.gz")
        self.assertEqual([asset["name"] for asset in exact], ["server-source.tar.gz"])



    def test_network_build_combines_repository_pois_with_release_tiles(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source_repository = self.make_source(root / "source-repository", include_manifest=False)
            source_tiles = source_repository / "map" / "palops" / "tiles"
            shutil.rmtree(source_tiles)
            source_archive = Path(shutil.make_archive(
                str(root / "source-archive"),
                "zip",
                root_dir=source_repository.parent,
                base_dir=source_repository.name,
            ))

            release_repository = self.make_source(root / "release-repository", include_manifest=False)
            release_archive = Path(shutil.make_archive(
                str(root / "palpanel-map-tiles"),
                "zip",
                root_dir=release_repository / "map" / "palops",
                base_dir="tiles",
            ))
            release_metadata = {
                "tag_name": "maps-v1",
                "assets": [{
                    "name": "palpanel-map-tiles.zip",
                    "url": "https://example.invalid/assets/1",
                    "size": release_archive.stat().st_size,
                }],
            }
            destination = root / "destination"

            def copy_source(target: Path, _repository: str, _commit: str, _token: str = "") -> None:
                shutil.copy2(source_archive, target)

            def copy_release(target: Path, _asset: dict, _token: str = "") -> None:
                shutil.copy2(release_archive, target)

            argv = [
                str(SCRIPT),
                "--destination", str(destination),
                "--repository", "ninhua/palpanel-assets",
                "--ref", "main",
                "--allow-network",
                "--expected-poi-total", "2",
            ]
            with mock.patch.object(sys, "argv", argv), mock.patch.object(
                SYNC_MODULE, "resolve_repository_commit", return_value="0" * 40
            ), mock.patch.object(
                SYNC_MODULE, "download_repository_archive", side_effect=copy_source
            ), mock.patch.object(
                SYNC_MODULE, "release_metadata", return_value=release_metadata
            ), mock.patch.object(
                SYNC_MODULE, "download_release_asset", side_effect=copy_release
            ):
                result = SYNC_MODULE.main()

            self.assertEqual(result, 0)
            manifest = json.loads((destination / "palpanel-map-assets.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["release"]["tag"], "maps-v1")
            self.assertEqual(manifest["release"]["assets"], ["palpanel-map-tiles.zip"])
            self.assertEqual(manifest["tiles"]["counts"], {"palpagos": 341, "world-tree": 341})
            self.assertTrue((destination / "tiles" / "world-tree" / "4" / "15" / "15.webp").is_file())

    def test_downloads_and_normalizes_latest_release_tile_archive(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            payload_root = root / "payload"
            for layer in LAYERS:
                for zoom in range(5):
                    edge = 2**zoom
                    for x in range(edge):
                        for y in range(edge):
                            tile = payload_root / "release-bundle" / layer / str(zoom) / str(x) / f"{y}.webp"
                            tile.parent.mkdir(parents=True, exist_ok=True)
                            payload = f"{layer}:{zoom}:{x}:{y}".encode()
                            tile.write_bytes(b"RIFF" + (len(payload) + 4).to_bytes(4, "little") + b"WEBP" + payload)
            archive = Path(shutil.make_archive(str(root / "palpanel-map-tiles"), "zip", root_dir=payload_root))
            metadata = {
                "tag_name": "maps-2026-07-30",
                "assets": [{
                    "name": "palpanel-map-tiles.zip",
                    "url": "https://example.invalid/assets/1",
                    "size": archive.stat().st_size,
                }],
            }

            def copy_archive(target: Path, _asset: dict, _token: str = "") -> None:
                shutil.copy2(archive, target)

            destination = root / "release-import"
            with mock.patch.object(SYNC_MODULE, "release_metadata", return_value=metadata), mock.patch.object(
                SYNC_MODULE, "download_release_asset", side_effect=copy_archive
            ):
                tag, names = SYNC_MODULE.download_release_tile_archives(
                    destination, "ninhua/palpanel-assets", "latest", "", "token"
                )
            self.assertEqual(tag, "maps-2026-07-30")
            self.assertEqual(names, ["palpanel-map-tiles.zip"])
            SYNC_MODULE.normalize_tiles(destination)
            available, counts = SYNC_MODULE.validate_tiles(destination, required=True)
            self.assertTrue(available)
            self.assertEqual(counts, {"palpagos": 341, "world-tree": 341})

    @unittest.skipUnless(shutil.which("zstd"), "zstd is required for tar.zst fixture")
    def test_network_build_uses_complete_tar_zst_release_when_source_has_no_pois(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source_repository = root / "source-repository"
            source_repository.mkdir()
            (source_repository / "README.md").write_text("Release-only map bundle", encoding="utf-8")
            source_archive = Path(shutil.make_archive(
                str(root / "source-archive"),
                "zip",
                root_dir=source_repository.parent,
                base_dir=source_repository.name,
            ))

            release_repository = self.make_source(root / "release-repository", include_manifest=False)
            release_payload = release_repository / "map" / "palops"
            release_tar = root / "palops-map-assets.tar"
            with tarfile.open(release_tar, "w") as bundle:
                for path in sorted(release_payload.rglob("*")):
                    if path.is_file():
                        bundle.add(path, arcname=path.relative_to(release_payload).as_posix())
            release_archive = root / "palops-map-assets.tar.zst"
            subprocess.run(
                ["zstd", "-q", "-f", "-o", str(release_archive), str(release_tar)],
                check=True,
            )
            release_metadata = {
                "tag_name": "palops-map-assets-2026.07.30",
                "assets": [{
                    "name": "palops-map-assets.tar.zst",
                    "url": "https://example.invalid/assets/1",
                    "size": release_archive.stat().st_size,
                }],
            }
            destination = root / "destination"

            def copy_source(target: Path, _repository: str, _commit: str, _token: str = "") -> None:
                shutil.copy2(source_archive, target)

            def copy_release(target: Path, _asset: dict, _token: str = "") -> None:
                shutil.copy2(release_archive, target)

            argv = [
                str(SCRIPT),
                "--destination", str(destination),
                "--repository", "ninhua/palpanel-assets",
                "--ref", "main",
                "--allow-network",
                "--expected-poi-total", "2",
            ]
            with mock.patch.object(sys, "argv", argv), mock.patch.object(
                SYNC_MODULE, "resolve_repository_commit", return_value="1" * 40
            ), mock.patch.object(
                SYNC_MODULE, "download_repository_archive", side_effect=copy_source
            ), mock.patch.object(
                SYNC_MODULE, "release_metadata", return_value=release_metadata
            ), mock.patch.object(
                SYNC_MODULE, "download_release_asset", side_effect=copy_release
            ):
                result = SYNC_MODULE.main()

            self.assertEqual(result, 0)
            manifest = json.loads((destination / "palpanel-map-assets.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["release"]["tag"], "palops-map-assets-2026.07.30")
            self.assertEqual(manifest["release"]["assets"], ["palops-map-assets.tar.zst"])
            self.assertEqual(manifest["dataset_version"], "palops-map-assets-2026.07.30")
            self.assertEqual(manifest["poi_total"], 2)
            self.assertEqual(manifest["tiles"]["counts"], {"palpagos": 341, "world-tree": 341})
            self.assertTrue((destination / "data" / "default-pois.zh-CN.json").is_file())
            self.assertTrue((destination / "tiles" / "palpagos" / "4" / "15" / "15.webp").is_file())

    def test_selects_tar_zst_release_asset(self) -> None:
        metadata = {
            "assets": [{
                "name": "palops-map-assets.tar.zst",
                "url": "https://example.invalid/assets/1",
            }]
        }
        selected = SYNC_MODULE.select_release_assets(metadata, "")
        self.assertEqual([asset["name"] for asset in selected], ["palops-map-assets.tar.zst"])

    def test_extracts_release_zip_without_single_top_level_directory(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            archive = root / "map-tiles.zip"
            with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as bundle:
                bundle.writestr("palpagos/0/0/0.webp", b"RIFF\x04\x00\x00\x00WEBP")
                bundle.writestr("world-tree/0/0/0.webp", b"RIFF\x04\x00\x00\x00WEBP")
            extracted = SYNC_MODULE.extract_release_archive(archive, root / "extracted")
            self.assertTrue((extracted / "palpagos" / "0" / "0" / "0.webp").is_file())
            self.assertTrue((extracted / "world-tree" / "0" / "0" / "0.webp").is_file())

    def test_finds_poi_only_repository_root_for_release_tile_builds(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            poi_root = root / "resources" / "poi"
            poi_root.mkdir(parents=True)
            (poi_root / "points.geojson").write_text(
                json.dumps({
                    "type": "FeatureCollection",
                    "features": [{
                        "type": "Feature",
                        "properties": {"id": "poi-1", "name": "POI", "category": "test"},
                        "geometry": {"type": "Point", "coordinates": [1, 2]},
                    }],
                }),
                encoding="utf-8",
            )
            self.assertEqual(
                SYNC_MODULE.find_map_root(root, require_tiles=False),
                root.resolve(),
            )
            with self.assertRaises(ValueError):
                SYNC_MODULE.find_map_root(root, require_tiles=True)

    def test_syncs_dedicated_repository_with_tiles_and_pois(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source")
            destination = root / "destination"
            result = self.run_sync(source, destination)
            self.assertEqual(result.returncode, 0, result.stderr)
            manifest = json.loads((destination / "palpanel-map-assets.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["source"]["repository"], "ninhua/palpanel-assets")
            self.assertEqual(manifest["source"]["version"], "fixture-assets-1")
            self.assertEqual(manifest["dataset_version"], "fixture-dataset-1")
            self.assertEqual(manifest["poi_total"], 2)
            self.assertTrue(manifest["tiles_available"])
            self.assertEqual(manifest["tiles"]["counts"], {"palpagos": 341, "world-tree": 341})
            self.assertTrue((destination / "tiles" / "palpagos" / "4" / "15" / "15.webp").is_file())


    def test_discovers_and_normalizes_repository_native_layout(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = root / "source"
            poi_dir = source / "resources" / "poi"
            poi_dir.mkdir(parents=True)
            features = [
                {
                    "type": "Feature",
                    "id": f"native-{index}",
                    "properties": {
                        "kind": "fast-travel" if index == 0 else "boss",
                        "mapId": "Palpagos" if index == 0 else "WorldTree",
                        "name": {
                            "zh-CN": f"地点 {index}",
                            "en-US": f"Location {index}",
                            "ja-JP": f"場所 {index}",
                        },
                    },
                    "geometry": {
                        "type": "Point",
                        "coordinates": [index + 0.25, index + 0.75],
                    },
                }
                for index in range(2)
            ]
            (poi_dir / "map-data.geojson").write_text(
                json.dumps({"type": "FeatureCollection", "features": features}, ensure_ascii=False),
                encoding="utf-8",
            )
            for directory in ("PalpagosMap", "WorldTreeMap"):
                for zoom in range(5):
                    edge = 2**zoom
                    for x in range(edge):
                        for y in range(edge):
                            tile = source / "resources" / "raster" / directory / str(zoom) / str(x) / f"{y}.webp"
                            tile.parent.mkdir(parents=True, exist_ok=True)
                            payload = f"{directory}:{zoom}:{x}:{y}".encode()
                            tile.write_bytes(b"RIFF" + (len(payload) + 4).to_bytes(4, "little") + b"WEBP" + payload)
            archive = Path(shutil.make_archive(str(root / "native-assets"), "zip", root_dir=root, base_dir=source.name))
            destination = root / "destination"
            result = subprocess.run(
                [
                    "python3", str(SCRIPT),
                    "--archive", str(archive),
                    "--destination", str(destination),
                    "--repository", "ninhua/palpanel-assets",
                    "--ref", "fixture",
                    "--source-commit", "0123456789abcdef0123456789abcdef01234567",
                    "--expected-poi-total", "2",
                ],
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            zh = json.loads((destination / "data" / "default-pois.zh-CN.json").read_text(encoding="utf-8"))
            en = json.loads((destination / "data" / "default-pois.en-US.json").read_text(encoding="utf-8"))
            self.assertEqual(zh[0]["name"], "地点 0")
            self.assertEqual(en[0]["name"], "Location 0")
            self.assertEqual(zh[1]["map"], "world-tree")
            self.assertTrue((destination / "tiles" / "palpagos" / "4" / "15" / "15.webp").is_file())
            self.assertTrue((destination / "tiles" / "world-tree" / "4" / "15" / "15.webp").is_file())

    def test_syncs_single_root_archive_and_uses_manifest_poi_total(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            repository = self.make_source(root / "repository")
            archive_base = root / "palpanel-assets-fixture"
            archive = Path(shutil.make_archive(
                str(archive_base),
                "zip",
                root_dir=repository.parent,
                base_dir=repository.name,
            ))
            destination = root / "destination"
            result = subprocess.run(
                [
                    "python3", str(SCRIPT),
                    "--archive", str(archive),
                    "--destination", str(destination),
                    "--repository", "ninhua/palpanel-assets",
                    "--ref", "fixture",
                    "--source-commit", "0123456789abcdef0123456789abcdef01234567",
                ],
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            manifest = json.loads((destination / "palpanel-map-assets.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["poi_total"], 2)
            self.assertEqual(manifest["source"]["commit"], "0123456789abcdef0123456789abcdef01234567")

    def test_rejects_localized_coordinate_drift(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source")
            path = source / "map" / "palops" / "data" / "default-pois.en-US.json"
            items = json.loads(path.read_text(encoding="utf-8"))
            items[0]["mapX"] = 99
            path.write_text(json.dumps(items), encoding="utf-8")
            result = self.run_sync(source, root / "destination")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("coordinates differ", result.stderr)

    def test_rejects_incomplete_tile_pyramid(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source")
            (source / "map" / "palops" / "tiles" / "world-tree" / "4" / "15" / "15.webp").unlink()
            result = self.run_sync(source, root / "destination")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("tile layer world-tree is incomplete", result.stderr)

    def test_rejects_source_manifest_checksum_drift(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source")
            path = source / "map" / "palops" / "data" / "default-pois.zh-CN.json"
            path.write_text(path.read_text(encoding="utf-8") + " ", encoding="utf-8")
            result = self.run_sync(source, root / "destination")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("source manifest", result.stderr)


    def test_rejects_git_lfs_pointer_instead_of_webp_tile(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source")
            tile = source / "map" / "palops" / "tiles" / "palpagos" / "0" / "0" / "0.webp"
            tile.write_text("version https://git-lfs.github.com/spec/v1\n", encoding="utf-8")
            result = self.run_sync(source, root / "destination")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("Git LFS pointer or corrupt asset", result.stderr)

    def test_can_stage_pois_without_tiles_only_when_explicitly_allowed(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source", include_manifest=False)
            tile_root = source / "map" / "palops" / "tiles"
            for path in sorted(tile_root.rglob("*"), reverse=True):
                if path.is_file():
                    path.unlink()
                elif path.is_dir():
                    path.rmdir()
            result = self.run_sync(source, root / "destination", "--allow-missing-tiles")
            self.assertEqual(result.returncode, 0, result.stderr)
            manifest = json.loads((root / "destination" / "palpanel-map-assets.json").read_text(encoding="utf-8"))
            self.assertFalse(manifest["tiles_available"])


if __name__ == "__main__":
    unittest.main()
