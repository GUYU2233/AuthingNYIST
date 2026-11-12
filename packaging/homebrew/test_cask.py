#!/usr/bin/env python3
"""Offline tests for the generated Homebrew Cask."""
import hashlib
import importlib.util
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
MODULE_PATH = Path(__file__).resolve().parent / "generate_cask.py"
spec = importlib.util.spec_from_file_location("generate_cask", MODULE_PATH)
cask = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cask)


def sample_dmgs():
    return {
        "amd64": {"sha256": "a" * 64, "url": "https://example.invalid/NYIST-Connect-macos-amd64.dmg"},
        "arm64": {"sha256": "b" * 64, "url": "https://example.invalid/NYIST-Connect-macos-arm64.dmg"},
    }


class CaskTests(unittest.TestCase):
    def test_structure(self):
        text = cask.render(version="1.2.3", tag="v1.2.3", repo="GUYU2233/AuthingNYIST", dmgs=sample_dmgs())
        self.assertIn('cask "nyist-connect" do', text)
        self.assertIn('version "1.2.3"', text)
        self.assertIn("on_intel do", text)
        self.assertIn("on_arm do", text)
        self.assertEqual(text.count('sha256 "'), 2)
        self.assertEqual(text.count("url "), 2)
        self.assertIn('sha256 "' + "a" * 64 + '"', text)
        self.assertIn('url "https://example.invalid/NYIST-Connect-macos-amd64.dmg"', text)
        self.assertIn('sha256 "' + "b" * 64 + '"', text)
        self.assertIn('url "https://example.invalid/NYIST-Connect-macos-arm64.dmg"', text)
        self.assertIn('app "NYIST Connect.app"', text)
        self.assertIn('binary "#{appdir}/NYIST Connect.app/Contents/MacOS/nyist-core", target: "nyist-connect"', text)
        self.assertIn('depends_on macos: :ventura', text)
        self.assertNotIn(":no_check", text)
        self.assertNotIn("Formula", text)

    def test_version_and_tag_substitution(self):
        dmgs = {
            "amd64": {"sha256": "c" * 64, "url": "https://github.com/GUYU2233/AuthingNYIST/releases/download/v2.0.0-rc.1/NYIST-Connect-macos-amd64.dmg"},
            "arm64": {"sha256": "d" * 64, "url": "https://github.com/GUYU2233/AuthingNYIST/releases/download/v2.0.0-rc.1/NYIST-Connect-macos-arm64.dmg"},
        }
        text = cask.render(version="2.0.0-rc.1", tag="v2.0.0-rc.1", repo="GUYU2233/AuthingNYIST", dmgs=dmgs)
        self.assertIn('version "2.0.0-rc.1"', text)
        self.assertIn("/v2.0.0-rc.1/NYIST-Connect-macos-amd64.dmg", text)
        self.assertIn("/v2.0.0-rc.1/NYIST-Connect-macos-arm64.dmg", text)

    def test_rejects_partial_matrix_or_bad_hash(self):
        with self.assertRaises(ValueError):
            cask.render(version="1.2.3", tag="v1.2.3", repo="a/b", dmgs={"amd64": sample_dmgs()["amd64"]})
        bad = sample_dmgs()
        bad["arm64"]["sha256"] = "xyz"
        with self.assertRaises(ValueError):
            cask.render(version="1.2.3", tag="v1.2.3", repo="a/b", dmgs=bad)
        with self.assertRaises(ValueError):
            cask.render(version="v1.2.3", tag="v1.2.3", repo="a/b", dmgs=sample_dmgs())

    def test_cli_hashes_dmgs_and_writes_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            dist = Path(tmp) / "dist"
            dist.mkdir()
            for arch in ("amd64", "arm64"):
                (dist / f"NYIST-Connect-macos-{arch}.dmg").write_bytes(f"dmg-{arch}".encode())
            out = Path(tmp) / "Casks/nyist-connect.rb"
            subprocess.run(
                ["python3", str(MODULE_PATH), "--version", "1.2.3", "--tag", "v1.2.3",
                 "--dist", str(dist), "--output", str(out)],
                check=True, stdout=subprocess.DEVNULL,
            )
            text = out.read_text()
            for arch in ("amd64", "arm64"):
                digest = hashlib.sha256(f"dmg-{arch}".encode()).hexdigest()
                self.assertIn(digest, text)
                self.assertIn(f"/v1.2.3/NYIST-Connect-macos-{arch}.dmg", text)
            if shutil.which("ruby"):
                subprocess.run(["ruby", "-c", str(out)], check=True, stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    unittest.main()
