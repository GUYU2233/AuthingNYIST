#!/usr/bin/env python3
"""Offline contract checks; fixture bytes are not represented as real releases."""
import hashlib
import importlib.util
import json
import pathlib
import subprocess
import tarfile
import tempfile
import unittest
import sys
sys.dont_write_bytecode = True
import xml.etree.ElementTree as ET

ROOT = pathlib.Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('generator', pathlib.Path(__file__).with_name('generate.py'))
generator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generator)


def write_cli_archive(dist, version):
    archive = dist / f'authingnyist-cli-extras-{version}.tar.gz'
    with tarfile.open(archive, 'w:gz') as tar:
        for arch in ['amd64', 'arm64']:
            body = dist / f'nyist-connect-linux-{arch}'
            body.write_bytes(('fixture-' + body.name).encode())
            tar.add(body, arcname=f'cli/{body.name}')
        for doc in ['LICENSE', 'README.md', 'NYIST-CONNECT.md']:
            body = dist / doc
            body.write_text(f'fixture {doc}\n')
            tar.add(body, arcname=f'docs/{doc}')
    return archive


class GenerationTests(unittest.TestCase):
    def test_generation_and_checksums(self):
        with tempfile.TemporaryDirectory() as tmp:
            dist = pathlib.Path(tmp)
            archive = write_cli_archive(dist, '1.2.3')
            out = dist / 'generated'
            generator.generate(ROOT, 'v1.2.3', dist, out)
            expected_digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            for arch, goarch in [('x86_64', 'amd64'), ('aarch64', 'arm64')]:
                m = json.loads((out / f'{generator.APP_ID}-{arch}.json').read_text())
                self.assertEqual(m['finish-args'], ['--share=network'])
                self.assertEqual(m['command'], 'nyist-connect')
                self.assertNotIn('auth-nyist', json.dumps(m))
                self.assertEqual(len(m['modules'][0]['sources']), 2)
                source = m['modules'][0]['sources'][0]
                self.assertEqual(source['type'], 'archive')
                self.assertIn('/v1.2.3/' + archive.name, source['url'])
                self.assertEqual(source['sha256'], expected_digest)
                self.assertIn(f'cli/nyist-connect-linux-{goarch}', m['modules'][0]['build-commands'][0])
                component = ET.fromstring(m['modules'][0]['sources'][1]['contents'])
                self.assertEqual(component.attrib['type'], 'console-application')
            for arch in ['amd64', 'arm64']:
                recipe = (out / f'snap-{arch}/snap/snapcraft.yaml').read_text()
                self.assertIn('confinement: classic', recipe)
                self.assertNotIn('auth-nyist', recipe)
                self.assertEqual(recipe.count('source-checksum: sha256/'), 1)
                self.assertIn('source-type: tar', recipe)
                self.assertIn(archive.name, recipe)
                self.assertIn(f'cli/nyist-connect-linux-{arch}', recipe)
                self.assertNotIn('daemon:', recipe)
                self.assertNotIn('hooks:', recipe)
            for bad in ['latest', '../v1.2.3', '1.2', '1.2.3;id', '01.2.3', '1.2.3-01']:
                with self.assertRaises(ValueError):
                    generator.generate(ROOT, bad, dist, out)
            write_cli_archive(dist, '0.0.0-dev')
            generator.generate(ROOT, '0.0.0-dev', dist, out)
            prerelease = json.loads((out / f'{generator.APP_ID}-x86_64.json').read_text())
            self.assertIn('/0.0.0-dev/', prerelease['modules'][0]['sources'][0]['url'])
            self.assertEqual(prerelease['runtime-version'], '25.08')
            self.assertIn('/GUYU2233/', prerelease['modules'][0]['sources'][0]['url'])
            result = subprocess.run(['bash', str(ROOT / 'scripts/package-sandbox.sh'),
                                     'v1.2.3', str(dist), str(out)], capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((dist / 'sandbox-assets.txt').read_text(), 'authingnyist-sandbox-1.2.3.tar.gz\n')

    def test_missing_inputs_fail_before_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            dist = pathlib.Path(tmp)
            with self.assertRaisesRegex(ValueError, 'Missing, empty, or symlink CLI archive'):
                generator.generate(ROOT, '1.2.3', dist, dist / 'out')
            self.assertFalse((dist / 'out').exists())


if __name__ == '__main__':
    unittest.main()
