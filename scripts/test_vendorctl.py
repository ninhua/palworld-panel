import json
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("vendorctl.py")
LOCK = Path(__file__).resolve().parent.parent / "third_party" / "vendor-lock.json"


class VendorCtlTest(unittest.TestCase):
    def run_tool(self, *args, check=True):
        return subprocess.run(["python3", str(SCRIPT), *map(str, args)], text=True, capture_output=True, check=check)

    def make_tree(self, root: Path, name: str, content: str = "data") -> Path:
        path = root / name
        path.mkdir(parents=True)
        (path / "file.txt").write_text(content, encoding="utf-8")
        return path

    def test_init_verify_and_tamper_detection(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            mirror = base / "mirror"
            args = ["init", "--root", mirror, "--lock", LOCK]
            for option, name in (("--map-assets", "map-assets"), ("--maplibre", "maplibre"), ("--palcalc", "palcalc"), ("--uesave", "uesave")):
                args += [option, self.make_tree(base, name)]
            self.run_tool(*args)
            manifest = json.loads((mirror / "vendor-manifest.json").read_text())
            self.assertEqual({item["id"] for item in manifest["components"]}, {"palpanel-map-assets", "maplibre-gl", "palcalc", "uesave"})
            self.run_tool("verify", "--root", mirror, "--lock", LOCK, "--profile", "source")
            result = self.run_tool("verify", "--root", mirror, "--lock", LOCK, "--profile", "build", check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("missing required components", result.stderr)
            (mirror / "sources/palpanel-map-assets/file.txt").write_text("changed", encoding="utf-8")
            result = self.run_tool("verify", "--root", mirror, "--lock", LOCK, "--profile", "source", check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("changed", result.stderr)

    def test_prepare_does_not_overwrite_unmanaged_source(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            mirror = base / "mirror"
            args = ["init", "--root", mirror, "--lock", LOCK]
            for option, name in (("--map-assets", "map-assets"), ("--maplibre", "maplibre"), ("--palcalc", "palcalc"), ("--uesave", "uesave")):
                args += [option, self.make_tree(base, name)]
            self.run_tool(*args)
            repository = base / "repo"
            unmanaged = self.make_tree(repository / "third_party", "palcalc", "keep")
            work = base / "work"
            self.run_tool("prepare", "--root", mirror, "--repository", repository, "--work-root", work, "--profile", "source")
            self.assertEqual((unmanaged / "file.txt").read_text(), "keep")
            self.assertTrue((repository / "third_party/uesave/.palpanel-vendor-managed").is_file())
            self.run_tool("cleanup", "--repository", repository, "--work-root", work)
            self.assertTrue(unmanaged.is_dir())
            self.assertFalse((repository / "third_party/uesave").exists())


    def test_environment_points_builds_at_complete_map_asset_snapshot(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            mirror = base / "mirror"
            work = base / "work"
            mirror.mkdir()
            work.mkdir()
            result = self.run_tool(
                "env", "--root", mirror, "--work-root", work, "--format", "json"
            )
            values = json.loads(result.stdout)
            self.assertEqual(
                values["PALPANEL_MAP_ASSETS_SOURCE_DIR"],
                str(mirror / "sources" / "palpanel-map-assets"),
            )
            self.assertNotIn("PALPANEL_PALOPS_TILE_SOURCE_DIR", values)

    def test_symlink_input_is_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            source = self.make_tree(base, "source")
            try:
                (source / "link").symlink_to(source / "file.txt")
            except OSError:
                self.skipTest("symlinks are unavailable")
            result = self.run_tool("init", "--root", base / "mirror", "--lock", LOCK, "--map-assets", source, check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("symlink", result.stderr)


if __name__ == "__main__":
    unittest.main()
