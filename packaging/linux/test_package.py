#!/usr/bin/env python3
"""Offline integration tests use inert fixture binaries, never real authentication."""
import hashlib
import io
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'scripts/package-linux.sh'


class PackagingTests(unittest.TestCase):
    def test_reject_versions(self):
        for version in ('', '1.2', '01.2.3', '1.2.3-01', '1.2.3;touch x'):
            result = subprocess.run(['bash', str(SCRIPT), '--version', version], capture_output=True)
            self.assertNotEqual(result.returncode, 0, version)
            self.assertIn(b'valid SemVer', result.stderr)

    def test_packages(self):
        with tempfile.TemporaryDirectory(prefix='nyist package test ') as tmp:
            tmp = Path(tmp)
            inputs, outputs = tmp / 'inputs', tmp / 'outputs'
            inputs.mkdir()
            for name in ('nyist-connect-linux-amd64', 'nyist-connect-linux-arm64'):
                (inputs / name).write_bytes(b'fixture-only-not-executable\n')
            cli_archive = inputs / 'authingnyist-cli-extras-1.2.3-rc.1+test.tar.gz'
            with tarfile.open(cli_archive, 'w:gz') as tar:
                for name in ('nyist-connect-linux-amd64', 'nyist-connect-linux-arm64'):
                    tar.add(inputs / name, arcname=f'cli/{name}')
                for name, body in (('LICENSE', 'fixture license\n'), ('README.md', '# fixture\n'), ('NYIST-CONNECT.md', '# fixture cli\n')):
                    member = tarfile.TarInfo(f'docs/{name}')
                    data = body.encode()
                    member.size = len(data)
                    member.mode = 0o644
                    tar.addfile(member, io.BytesIO(data))
            formats = ['aur']
            for tool, fmt in [('dpkg-deb', 'deb'), ('rpmbuild', 'rpm'), ('zstd', 'arch')]:
                if shutil.which(tool):
                    formats.append(fmt)
            command = ['bash', str(SCRIPT), '--version', '1.2.3-rc.1+test', '--input-dir', str(inputs), '--output-dir', str(outputs), '--cli-archive', str(cli_archive)]
            for fmt in formats:
                command += ['--format', fmt]
            subprocess.run(command, check=True, stdout=subprocess.DEVNULL)
            recipe = (outputs / 'PKGBUILD').read_text()
            self.assertNotIn('SKIP', recipe)
            self.assertNotIn('auth-nyist', recipe)
            self.assertNotIn('auth-nyist', (outputs / '.SRCINFO').read_text())
            self.assertIn(hashlib.sha256(cli_archive.read_bytes()).hexdigest(), recipe)
            self.assertIn('https://github.com/GUYU2233/AuthingNYIST/releases/download/v1.2.3-rc.1+test/authingnyist-cli-extras-1.2.3-rc.1+test.tar.gz', recipe)
            self.assertIn('cli/nyist-connect-linux-amd64', recipe)
            self.assertIn('docs/LICENSE', recipe)
            self.assertIn('source=(', recipe)
            self.assertNotIn('source_x86_64', recipe)
            srcinfo = (outputs / '.SRCINFO').read_text()
            self.assertIn('pkgver = 1.2.3_rc.1.test', srcinfo)
            self.assertIn('source = authingnyist-cli-extras-1.2.3-rc.1+test.tar.gz::https://github.com/GUYU2233/AuthingNYIST/releases/download/v1.2.3-rc.1+test/', srcinfo)
            subprocess.run(['bash', '-n', str(outputs / 'PKGBUILD')], check=True)
            if 'deb' in formats:
                debs = list(outputs.glob('*.deb'))
                self.assertEqual(len(debs), 2)
                for deb in debs:
                    control = subprocess.check_output(['dpkg-deb', '--ctrl-tarfile', str(deb)])
                    with tarfile.open(fileobj=io.BytesIO(control)) as tar:
                        self.assertFalse(any(Path(n).name in ('postinst', 'preinst', 'prerm', 'postrm') for n in tar.getnames()))
                    listing = subprocess.check_output(['dpkg-deb', '-c', str(deb)]).decode()
                    self.assertNotIn('./usr/bin/auth-nyist', listing)
                    self.assertIn('./usr/bin/nyist-connect', listing)
            if 'arch' in formats:
                for package in outputs.glob('*.pkg.tar.zst'):
                    raw = subprocess.check_output(['zstd', '-qd', '-c', str(package)])
                    with tarfile.open(fileobj=io.BytesIO(raw)) as tar:
                        self.assertIn('.PKGINFO', tar.getnames())
                        self.assertNotIn('.INSTALL', tar.getnames())
                        info = tar.extractfile('.PKGINFO').read().decode()
                        self.assertIn('pkgver = 1.2.3_rc.1+test-1', info)
                        self.assertNotIn('usr/bin/auth-nyist', tar.getnames())
                        self.assertIn('usr/bin/nyist-connect', tar.getnames())
            if 'rpm' in formats:
                rpms = list(outputs.glob('*.rpm'))
                self.assertEqual(len(rpms), 2)
                if shutil.which('rpm'):
                    for rpm in rpms:
                        scripts = subprocess.check_output(['rpm', '-qp', '--scripts', str(rpm)])
                        self.assertEqual(scripts, b'')


if __name__ == '__main__':
    unittest.main()
