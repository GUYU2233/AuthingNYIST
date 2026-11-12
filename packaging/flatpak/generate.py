#!/usr/bin/env python3
"""Dependency-free pinned release recipe generation and optional local builds."""
import hashlib
import json
import pathlib
import re
import shutil
import subprocess
import sys

APP_ID = 'io.github.GUYU2233.AuthingNYIST'


def generate(root, version, dist, out, cli_archive=None):
    semver = r'v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?'
    if not re.fullmatch(semver, version, re.ASCII):
        raise ValueError('VERSION must be a safe semantic release version, e.g. v1.2.3 or 0.0.0-dev')
    tag = version  # Preserve the exact release tag, including optional v prefix.
    version = version.removeprefix('v')
    base = f'https://github.com/GUYU2233/AuthingNYIST/releases/download/{tag}/'
    cli_archive = pathlib.Path(cli_archive) if cli_archive else dist / f'authingnyist-cli-extras-{version}.tar.gz'
    if (not cli_archive.is_file() or cli_archive.is_symlink()
            or cli_archive.stat().st_size == 0):
        raise ValueError(f'Missing, empty, or symlink CLI archive: {cli_archive}')
    if cli_archive.name != f'authingnyist-cli-extras-{version}.tar.gz':
        raise ValueError(f'CLI archive name does not match VERSION: {cli_archive.name}')
    cli_digest = hashlib.sha256(cli_archive.read_bytes()).hexdigest()
    # Validate every input before writing any output.
    out.mkdir(parents=True, exist_ok=True)
    for arch, fp_arch in [('amd64', 'x86_64'), ('arm64', 'aarch64')]:
        sources = [{'type': 'archive', 'url': base + cli_archive.name, 'sha256': cli_digest}]
        metadata = (root / 'packaging/flatpak' / f'{APP_ID}.metainfo.xml').read_text()
        sources.append({'type': 'inline', 'dest-filename': f'{APP_ID}.metainfo.xml', 'contents': metadata})
        manifest = {'app-id': APP_ID, 'runtime': 'org.freedesktop.Platform',
                    'runtime-version': '25.08', 'sdk': 'org.freedesktop.Sdk',
                    'command': 'nyist-connect', 'finish-args': ['--share=network'],
                    'modules': [{'name': 'nyist-connect', 'buildsystem': 'simple',
                                 'build-commands': [
                                     f'install -Dm755 cli/nyist-connect-linux-{arch} /app/bin/nyist-connect',
                                     f'install -Dm644 {APP_ID}.metainfo.xml /app/share/metainfo/{APP_ID}.metainfo.xml'],
                                 'sources': sources}]}
        (out / f'{APP_ID}-{fp_arch}.json').write_text(json.dumps(manifest, indent=2) + '\n')
        snap_dir = out / f'snap-{arch}' / 'snap'
        snap_dir.mkdir(parents=True, exist_ok=True)
        parts = [f'''  nyist-connect:
    plugin: nil
    source: {base + cli_archive.name}
    source-type: tar
    source-checksum: sha256/{cli_digest}
    override-build: |
      install -Dm755 cli/nyist-connect-linux-{arch} "$CRAFT_PART_INSTALL/bin/nyist-connect"
''']
        recipe = f'''name: nyist-connect
version: '{version}'
summary: NYIST campus network terminal clients
description: |
  Terminal-only nyist-connect client.
  No service is installed or started automatically.
base: core24
grade: stable
confinement: classic
platforms:
  {arch}:
apps:
  nyist-connect:
    command: bin/nyist-connect
parts:
''' + ''.join(parts)
        (snap_dir / 'snapcraft.yaml').write_text(recipe)
    return version


def main():
    root = pathlib.Path(sys.argv[1]).resolve()
    version = sys.argv[2]
    dist = pathlib.Path(sys.argv[3]).resolve() if len(sys.argv) > 3 else root / 'dist'
    out = pathlib.Path(sys.argv[4]).resolve() if len(sys.argv) > 4 else root / 'dist/sandbox'
    import os
    cli_archive = os.environ.get('CLI_ARCHIVE')
    action = sys.argv[5] if len(sys.argv) > 5 else 'generate'
    if action not in ('generate', 'flatpak', 'snap'):
        raise ValueError('action must be generate, flatpak or snap')
    version = generate(root, version, dist, out, cli_archive)
    if action == 'flatpak':
        for tool in ['flatpak', 'flatpak-builder']:
            if not shutil.which(tool):
                raise ValueError(f'{tool} is required for explicit Flatpak builds')
        arch = subprocess.check_output(['flatpak', '--default-arch'], text=True).strip()
        if arch not in ('x86_64', 'aarch64'):
            raise ValueError('Flatpak build supports x86_64/aarch64 only')
        subprocess.run(['flatpak-builder', '--repo=' + str(out / 'repo'),
                        str(out / ('build-' + arch)), str(out / f'{APP_ID}-{arch}.json')], check=True)
        subprocess.run(['flatpak', 'build-bundle', '--arch=' + arch, str(out / 'repo'),
                        str(out / f'nyist-connect-{version}-{arch}.flatpak'), APP_ID], check=True)
    elif action == 'snap':
        if not shutil.which('snapcraft'):
            raise ValueError('snapcraft is required for explicit Snap builds')
        machine = __import__('platform').machine()
        arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(machine)
        if not arch:
            raise ValueError('Snap build supports amd64/arm64 only')
        subprocess.run(['snapcraft', '--output', str(out / f'nyist-connect-{version}-{arch}.snap')],
                       cwd=out / f'snap-{arch}', check=True)
    import tarfile
    archive = dist / f'authingnyist-sandbox-{version}.tar.gz'
    with tarfile.open(archive, 'w:gz') as bundle:
        for path in sorted(out.rglob('*')):
            if path.is_file() and path.suffix in ('.json', '.yaml'):
                bundle.add(path, arcname=str(path.relative_to(out)))
        for directory in ['flatpak', 'snap']:
            for path in sorted((root / 'packaging' / directory).glob('*')):
                if path.is_file():
                    bundle.add(path, arcname=f'packaging/{directory}/{path.name}')
        bundle.add(root / 'scripts/package-sandbox.sh', arcname='scripts/package-sandbox.sh')
    assets = [archive.name]
    extensions = {'generate': [], 'flatpak': [f'nyist-connect-{version}-*.flatpak'],
                  'snap': [f'nyist-connect-{version}-*.snap']}[action]
    for extension in extensions:
        for path in out.glob(extension):
            target = dist / path.name
            if path.resolve() != target.resolve():
                shutil.copy2(path, target)
            assets.append(target.name)
    (dist / 'sandbox-assets.txt').write_text('\n'.join(assets) + '\n')
    print(f'Pinned recipes generated in {out}; archive: {archive}; no artifacts published.')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)
