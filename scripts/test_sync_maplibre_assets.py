#!/usr/bin/env python3
from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("sync_maplibre_assets.py")


class SyncMapLibreAssetsTest(unittest.TestCase):
    def make_source(self, root: Path, *, include_bundle: bool = True, version: str = "5.24.0") -> Path:
        dist = root / "dist"
        dist.mkdir(parents=True)
        if include_bundle:
            (dist / "maplibre-gl.js").write_text(
                f"/** MapLibre GL JS {version} */ window.maplibregl={{version:'{version}',Map:function(){{}},NavigationControl:function(){{}}}};\n",
                encoding="utf-8",
            )
        (dist / "maplibre-gl.css").write_text(".maplibregl-map{position:relative}\n", encoding="utf-8")
        (root / "LICENSE.txt").write_text("BSD-3-Clause fixture\n", encoding="utf-8")
        return root

    def run_sync(self, source: Path, destination: Path) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [
                "python3", str(SCRIPT), "--source-dir", str(source),
                "--destination", str(destination), "--skip-size-checks",
            ],
            text=True, capture_output=True,
        )

    def test_syncs_pinned_webgl1_and_webgl2_runtime_from_local_source(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source")
            destination = root / "destination"
            result = self.run_sync(source, destination)
            self.assertEqual(result.returncode, 0, result.stderr)
            manifest = json.loads((destination / "palpanel-maplibre-assets.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["version"], "5.24.0")
            self.assertEqual(manifest["distribution"], "browser-umd")
            self.assertEqual(manifest["webgl"], [1, 2])
            self.assertEqual(len(manifest["files"]), 3)
            self.assertTrue((destination / "maplibre-gl.js").is_file())
            self.assertFalse((destination / "maplibre-gl.mjs").exists())

    def test_rejects_incomplete_runtime(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source", include_bundle=False)
            result = self.run_sync(source, root / "destination")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("missing", result.stderr)

    def test_rejects_wrong_runtime_version(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source", version="6.0.0")
            result = self.run_sync(source, root / "destination")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("pinned version", result.stderr)


if __name__ == "__main__":
    unittest.main()
