#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("sync_palops_map_assets.py")
LAYERS = ("palpagos", "world-tree")


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
