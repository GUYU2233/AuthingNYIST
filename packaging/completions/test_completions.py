#!/usr/bin/env python3
"""Offline tests for the generated shell completions."""
import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts/generate-completions.py"
spec = importlib.util.spec_from_file_location("completions", SCRIPT)
completions = importlib.util.module_from_spec(spec)
spec.loader.exec_module(completions)

EXPECTED_FLAGS = {
    "autostart", "check-update", "config", "config-json", "configure", "connect",
    "daemon", "disconnect", "doctor", "network", "save-config-stdin", "status",
    "status-json", "system", "tray", "version",
}


class CompletionTests(unittest.TestCase):
    def test_flags_parsed_from_source(self):
        flags = completions.parse_flags(ROOT)
        self.assertEqual(set(flags), EXPECTED_FLAGS)
        self.assertEqual(flags["autostart"], "String")
        self.assertEqual(flags["network"], "String")
        self.assertEqual(flags["connect"], "Bool")

    def test_committed_files_are_fresh(self):
        result = subprocess.run([sys.executable, str(SCRIPT), "--check"], text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        files = completions.generated(ROOT)
        self.assertEqual(set(files), set(completions.FILES.values()))
        for name, text in files.items():
            self.assertEqual((ROOT / "completions" / name).read_text(), text, name)

    def test_each_shell_covers_flags_and_values(self):
        files = completions.generated(ROOT)
        for name, text in files.items():
            with self.subTest(shell=name):
                for flag in EXPECTED_FLAGS:
                    self.assertIn(flag, text)
                self.assertIn("nyist-tec", text)
                self.assertIn("nyist-stu", text)
                self.assertIn("enable", text)
                self.assertIn("disable", text)
                self.assertNotIn("auth-nyist", text)
                self.assertIn("do not edit by hand", text)
        bash = files["nyist-connect.bash"]
        self.assertIn("complete -F _nyist_connect nyist-connect", bash)
        zsh = files["_nyist-connect"]
        self.assertIn("#compdef nyist-connect", zsh)
        self.assertIn("_arguments", zsh)
        fish = files["nyist-connect.fish"]
        self.assertIn("complete -c nyist-connect", fish)
        powershell = files["nyist-connect.ps1"]
        self.assertIn("Register-ArgumentCompleter", powershell)
        self.assertIn("CompletionResult", powershell)

    def test_bash_syntax(self):
        result = subprocess.run(["bash", "-n", str(ROOT / "completions/nyist-connect.bash")])
        self.assertEqual(result.returncode, 0)

    def test_stale_output_detected(self):
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
            for name in completions.FILES.values():
                (out / name).write_text("stale\n")
            result = subprocess.run([sys.executable, str(SCRIPT), "--check", "--output", str(out)],
                                    text=True, capture_output=True)
            self.assertEqual(result.returncode, 1)
            self.assertIn("Stale completions", result.stderr)

    def test_new_flag_requires_regeneration(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            platform = root / "internal/platform"
            platform.mkdir(parents=True)
            (platform / "linux_cli.go").write_text(
                'flags.BoolVar(&a, "connect", false, "x")\n'
                'flags.StringVar(&b, "network", "", "x")\n'
                'flags.BoolVar(&c, "brand-new-flag", false, "x")\n')
            (platform / "macos_cli.go").write_text('flags.BoolVar(&a, "connect", false, "x")\n')
            flags = completions.parse_flags(root)
            self.assertIn("brand-new-flag", flags)
            rendered = completions.render_bash(flags)
            self.assertIn("--brand-new-flag", rendered)

    def test_rejects_inconsistent_or_empty_sources(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            platform = root / "internal/platform"
            platform.mkdir(parents=True)
            (platform / "linux_cli.go").write_text('flags.BoolVar(&a, "same", false, "x")\n')
            (platform / "macos_cli.go").write_text('flags.StringVar(&a, "same", "", "x")\n')
            with self.assertRaises(ValueError):
                completions.parse_flags(root)
            (platform / "linux_cli.go").write_text("// no flags here\n")
            (platform / "macos_cli.go").write_text("// no flags here\n")
            with self.assertRaises(ValueError):
                completions.parse_flags(root)


if __name__ == "__main__":
    unittest.main()
