#!/usr/bin/env python3
from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("sync_maplibre_assets.py")


class SyncMapLibreAssetsTest(unittest.TestCase):
    def make_source(self, root: Path, *, include_worker: bool = True) -> Path:
        dist = root / "dist"
        dist.mkdir(parents=True)
        (dist / "maplibre-gl.mjs").write_text(
            "/** MapLibre GL JS */ var br=`6.0.0`; import './maplibre-gl-shared.mjs'; "
            "const worker='maplibre-gl-worker.mjs'; export {br as version};\n",
            encoding="utf-8",
        )
        (dist / "maplibre-gl-shared.mjs").write_text("export const shared = true;\n", encoding="utf-8")
        if include_worker:
            (dist / "maplibre-gl-worker.mjs").write_text("self.onmessage=()=>{};\n", encoding="utf-8")
        (dist / "maplibre-gl.css").write_text(".maplibregl-map{position:relative}\n", encoding="utf-8")
        (root / "LICENSE.txt").write_text("BSD-3-Clause fixture\n", encoding="utf-8")
        return root

    def test_syncs_pinned_runtime_from_local_source(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source")
            destination = root / "destination"
            result = subprocess.run(
                [
                    "python3", str(SCRIPT), "--source-dir", str(source),
                    "--destination", str(destination), "--skip-size-checks",
                ],
                text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            manifest = json.loads((destination / "palpanel-maplibre-assets.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["version"], "6.0.0")
            self.assertEqual(len(manifest["files"]), 5)

    def test_rejects_incomplete_runtime(self) -> None:
        with tempfile.TemporaryDirectory() as temp_name:
            root = Path(temp_name)
            source = self.make_source(root / "source", include_worker=False)
            result = subprocess.run(
                [
                    "python3", str(SCRIPT), "--source-dir", str(source),
                    "--destination", str(root / "destination"), "--skip-size-checks",
                ],
                text=True, capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("missing", result.stderr)


if __name__ == "__main__":
    unittest.main()
