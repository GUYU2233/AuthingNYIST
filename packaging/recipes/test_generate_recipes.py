"""Standard-library fixture tests: python3 -m unittest discover -s packaging/recipes -v."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tarfile
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
        self.version = "1.2.3"
        self.archive = self.dist / f"authingnyist-cli-extras-{self.version}.tar.gz"
        with tarfile.open(self.archive, "w:gz") as tar:
            for os in ("linux", "macos"):
                for arch in ("amd64", "arm64"):
                    name = f"nyist-connect-{os}-{arch}"
                    body = self.root / name
                    body.write_bytes(b"fixture binary\x00" + name.encode())
                    tar.add(body, arcname=f"cli/{name}")
            for doc in ("LICENSE", "README.md", "NYIST-CONNECT.md"):
                body = self.root / doc
                body.write_text(f"fixture {doc}\n")
                tar.add(body, arcname=f"docs/{doc}")
        for arch in ("amd64", "arm64"):
            (self.dist / f"NYIST-Connect-macos-{arch}.dmg").write_bytes(b"fixture dmg " + arch.encode())

    def run_generator(self, *args):
        return subprocess.run([sys.executable, str(SCRIPT), *args],
                              text=True, capture_output=True)

    def defaults(self):
        return self.run_generator("--version", "v" + self.version, "--dist", str(self.dist),
                                  "--output", str(self.output))

    def test_all_actual_hashes_and_recipes(self):
        result = self.defaults()
        self.assertEqual(result.returncode, 0, result.stderr)
        archive_bytes = self.archive.read_bytes()
        digest = hashlib.sha256(archive_bytes).hexdigest()
        manifest = json.loads((self.output / "manifest.json").read_text())
        self.assertEqual(manifest["cli_archive"]["name"], self.archive.name)
        self.assertEqual(manifest["cli_archive"]["sha256"], digest)
        self.assertEqual(manifest["cli_archive"]["size"], len(archive_bytes))
        self.assertEqual(set(manifest["dmgs"]), {"amd64", "arm64"})

        brew = (self.output / "homebrew/authingnyist.rb").read_text()
        ports = (self.output / "macports/Portfile").read_text()
        nix = (self.output / "nix/default.nix").read_text()
        cask = (self.output / "homebrew/Casks/nyist-connect.rb").read_text()
        for recipe in (brew, ports, nix):
            self.assertIn(digest, recipe)
            self.assertIn(self.archive.name, recipe)
            self.assertIn("https://github.com/GUYU2233/AuthingNYIST", recipe)
            self.assertNotIn("Palvef", recipe)
            self.assertNotIn("auth-nyist", recipe)
        for recipe in (brew, nix):
            self.assertIn("/v1.2.3/" + self.archive.name, recipe)
        self.assertIn("master_sites https://github.com/GUYU2233/AuthingNYIST/releases/download/v1.2.3/", ports)
        self.assertIn("on_linux do", brew)
        self.assertNotIn("on_macos do", brew)
        self.assertIn('cli/nyist-connect-linux-#{arch}', brew)
        self.assertIn("nyist-connect --version", brew)
        self.assertIn("platforms darwin", ports)
        self.assertIn("supported_archs x86_64 arm64", ports)
        self.assertIn("extract {}", ports)
        self.assertIn("cli/nyist-connect-macos-amd64", ports)
        self.assertIn("cli/nyist-connect-macos-arm64", ports)
        for platform in ("x86_64-linux", "aarch64-linux", "x86_64-darwin", "aarch64-darwin"):
            self.assertIn(platform, nix)
        self.assertIn("x86_64-linux) binary=cli/nyist-connect-linux-amd64", nix)
        for forbidden in ("vendorHash", "services {", "startupitem", "launchctl", "systemctl"):
            self.assertNotIn(forbidden, brew + ports + nix)
        self.assertIn('cask "nyist-connect" do', cask)
        self.assertIn('app "NYIST Connect.app"', cask)
        self.assertIn("on_intel do", cask)
        self.assertIn("on_arm do", cask)

        first = {p.relative_to(self.output): p.read_bytes() for p in self.output.rglob("*") if p.is_file()}
        self.assertEqual(self.defaults().returncode, 0)
        self.assertEqual(first, {p.relative_to(self.output): p.read_bytes() for p in self.output.rglob("*") if p.is_file()})

    def test_safe_semver(self):
        for version in ("1.2.3", "v0.0.1-rc.1+build.2", "v2.0.0-alpha"):
            self.assertIsNotNone(recipes.SEMVER.fullmatch(version))
        for version in ("dev", "1.2", "01.2.3", "1.2.3-01", "1.2.3\n", '1.2.3"', "../../1.2.3", "1.2.3;touch x"):
            with self.subTest(version=version):
                self.assertNotEqual(self.run_generator("--version", version, "--dist", str(self.dist),
                                                       "--output", str(self.output)).returncode, 0)
                self.assertFalse(self.output.exists())

    def test_missing_empty_and_symlink_cli_archive_fail_before_output(self):
        payload = self.archive.read_bytes()
        self.archive.unlink()
        self.assertNotEqual(self.defaults().returncode, 0)
        self.assertFalse(self.output.exists())
        self.archive.write_bytes(b"")
        self.assertNotEqual(self.defaults().returncode, 0)
        self.assertFalse(self.output.exists())
        self.archive.unlink()
        self.archive.symlink_to(self.dist / "NYIST-Connect-macos-amd64.dmg")
        self.assertNotEqual(self.defaults().returncode, 0)
        self.assertFalse(self.output.exists())
        self.archive.unlink()
        self.archive.write_bytes(payload)

    def test_archive_name_must_match_version(self):
        renamed = self.dist / "authingnyist-cli-extras-9.9.9.tar.gz"
        self.archive.rename(renamed)
        result = self.run_generator("--version", "v" + self.version, "--dist", str(self.dist),
                                    "--cli-archive", str(renamed), "--output", str(self.output))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not match", result.stderr)
        self.assertFalse(self.output.exists())

    def test_partial_dmg_matrix_fails(self):
        (self.dist / "NYIST-Connect-macos-arm64.dmg").unlink()
        result = self.defaults()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("incomplete macOS DMG", result.stderr)
        self.assertFalse(self.output.exists())

    def test_no_dmgs_skips_cask(self):
        for arch in ("amd64", "arm64"):
            (self.dist / f"NYIST-Connect-macos-{arch}.dmg").unlink()
        result = self.defaults()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.output / "homebrew/Casks/nyist-connect.rb").exists())


if __name__ == "__main__":
    unittest.main()
