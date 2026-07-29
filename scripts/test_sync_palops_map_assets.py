#!/usr/bin/env python3
from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("sync_palops_map_assets.py")


class SyncPalOpsMapAssetsTest(unittest.TestCase):
    def make_source(self, root: Path, total: int = 2) -> Path:
        map_root = root / "src" / "PalOps.Web" / "wwwroot" / "map"
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
        (map_root / "tiles" / "palpagos" / "0" / "0").mkdir(parents=True)
        (map_root / "tiles" / "palpagos" / "0" / "0" / "0.webp").write_bytes(b"tile")
        return root

    def test_syncs_open_assets_without_tiles(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source")
            destination = root / "destination"
            result = subprocess.run(
                [
                    "python3", str(SCRIPT), "--source-dir", str(source),
                    "--destination", str(destination), "--expected-poi-total", "2",
                ],
                text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            manifest = json.loads((destination / "palpanel-map-assets.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["poi_total"], 2)
            self.assertFalse(manifest["tiles_available"])
            self.assertFalse((destination / "tiles").exists())

    def test_rejects_localized_coordinate_drift(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source")
            path = source / "src" / "PalOps.Web" / "wwwroot" / "map" / "data" / "default-pois.en-US.json"
            items = json.loads(path.read_text(encoding="utf-8"))
            items[0]["mapX"] = 99
            path.write_text(json.dumps(items), encoding="utf-8")
            result = subprocess.run(
                [
                    "python3", str(SCRIPT), "--source-dir", str(source),
                    "--destination", str(root / "destination"), "--expected-poi-total", "2",
                ],
                text=True, capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("coordinates differ", result.stderr)

    def test_repository_tiles_require_explicit_rights_confirmation(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source")
            result = subprocess.run(
                [
                    "python3", str(SCRIPT), "--source-dir", str(source),
                    "--destination", str(root / "destination"), "--expected-poi-total", "2",
                    "--include-repository-tiles",
                ],
                text=True, capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("RIGHTS_CONFIRMED", result.stderr)

    def test_local_tiles_require_explicit_rights_confirmation(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source")
            result = subprocess.run(
                [
                    "python3", str(SCRIPT), "--source-dir", str(source),
                    "--destination", str(root / "destination"), "--expected-poi-total", "2",
                    "--tiles-source-dir", str(source / "src" / "PalOps.Web" / "wwwroot" / "map" / "tiles"),
                ],
                text=True, capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("RIGHTS_CONFIRMED", result.stderr)


if __name__ == "__main__":
    unittest.main()
