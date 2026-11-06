"""Standard-library fixture tests: python3 -m unittest discover -s packaging/recipes -v."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[2] / "scripts/generate-recipes.py"
spec = importlib.util.spec_from_file_location("recipes", SCRIPT)
recipes = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recipes)


class RecipeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.dist = self.root / "dist"
        self.dist.mkdir()
        self.output = self.root / "recipes"
        self.names = []
        for os in ("linux", "macos"):
            for arch, classic in (("amd64", "x86_64"), ("arm64", "arm64")):
                for name in (f"nyist-connect-{os}-{arch}", f"auth-nyist.{os}.{classic}"):
                    (self.dist / name).write_bytes(b"fixture binary\x00" + name.encode())
                    self.names.append(name)

    def run_generator(self, version="v1.2.3"):
        return subprocess.run([sys.executable, str(SCRIPT), "--version", version,
                               "--dist", str(self.dist), "--output", str(self.output)],
                              text=True, capture_output=True)

    def test_all_actual_hashes_and_recipes(self):
        result = self.run_generator()
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((self.output / "manifest.json").read_text())
        self.assertEqual(len(manifest["assets"]), 8)
        brew = (self.output / "homebrew/authingnyist.rb").read_text()
        ports = (self.output / "macports/Portfile").read_text()
        nix = (self.output / "nix/default.nix").read_text()
        for entry in manifest["assets"]:
            content = (self.dist / entry["name"]).read_bytes()
            digest = hashlib.sha256(content).hexdigest()
            self.assertEqual(entry["sha256"], digest)
            self.assertEqual(entry["size"], len(content))
            self.assertIn(digest, brew)
            self.assertIn(digest, nix)
            self.assertIn("/v1.2.3/" + entry["name"], nix)
            if "macos" in entry["name"]:
                self.assertIn(digest, ports)
        for platform in ("x86_64-linux", "aarch64-linux", "x86_64-darwin", "aarch64-darwin"):
            self.assertIn(platform, nix)
        self.assertIn("platforms darwin", ports)
        self.assertIn("supported_archs x86_64 arm64", ports)
        self.assertIn("extract {}", ports)
        self.assertEqual(brew.count("using: :nounzip"), 8)
        self.assertIn("on_macos do", brew)
        self.assertIn("on_linux do", brew)
        self.assertIn("Hardware::CPU.is_64_bit?", brew)
        self.assertIn("nyist-connect --version", brew)
        self.assertIn("auth-nyist --version", brew)
        for recipe in (brew, ports, nix):
            self.assertIn("https://github.com/GUYU2233/AuthingNYIST", recipe)
            self.assertNotIn("Palvef", recipe)
        self.assertIn("worksrcdir .", ports)
        for forbidden in ("vendorHash", "services {", "startupitem", "launchctl", "systemctl"):
            self.assertNotIn(forbidden, brew + ports + nix)
        first = {p.relative_to(self.output): p.read_bytes() for p in self.output.rglob("*") if p.is_file()}
        self.assertEqual(self.run_generator().returncode, 0)
        self.assertEqual(first, {p.relative_to(self.output): p.read_bytes() for p in self.output.rglob("*") if p.is_file()})

    def test_safe_semver(self):
        for version in ("1.2.3", "v0.0.1-rc.1+build.2", "v2.0.0-alpha"):
            self.assertIsNotNone(recipes.SEMVER.fullmatch(version))
        for version in ("dev", "1.2", "01.2.3", "1.2.3-01", "1.2.3\n", '1.2.3"', "../../1.2.3", "1.2.3;touch x"):
            with self.subTest(version=version):
                self.assertNotEqual(self.run_generator(version).returncode, 0)
                self.assertFalse(self.output.exists())

    def test_missing_empty_and_symlink_fail_before_output(self):
        target = self.dist / self.names[-1]
        target.unlink()
        self.assertNotEqual(self.run_generator().returncode, 0)
        self.assertFalse(self.output.exists())
        target.touch()
        self.assertNotEqual(self.run_generator().returncode, 0)
        self.assertFalse(self.output.exists())
        target.unlink()
        target.symlink_to(self.dist / self.names[0])
        self.assertNotEqual(self.run_generator().returncode, 0)
        self.assertFalse(self.output.exists())


if __name__ == "__main__":
    unittest.main()
