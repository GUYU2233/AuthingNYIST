#!/usr/bin/env python3
"""Native packages from an explicit, immutable-version binary release."""
import argparse
import hashlib
import io
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[2]
ARCHES = {'amd64': ('amd64', 'x86_64', 'x86_64'), 'arm64': ('arm64', 'aarch64', 'arm64')}
DOCS = ('LICENSE', 'README.md', 'NYIST-CONNECT.md')


def run(*args):
    subprocess.run([str(a) for a in args], check=True)


def stage(path, binaries):
    for name, source in zip(('nyist-connect', 'auth-nyist'), binaries):
        target = path / 'usr/bin' / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)
        target.chmod(0o755)
    for name in DOCS:
        target = path / ('usr/share/licenses/nyist-connect' if name == 'LICENSE' else 'usr/share/doc/nyist-connect') / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / name, target)
        target.chmod(0o644)


def make_aur(out, version, tag, repo, inputs):
    # Checksums are computed from the exact local release assets, never SKIP.
    srcinfo = ['pkgbase = nyist-connect-bin', f'\tpkgver = {version.replace("-", "_")}', '\tpkgrel = 1', '\tpkgdesc = NYIST campus network connector and authentication CLI', f'\turl = https://github.com/{repo}', '\tarch = x86_64', '\tarch = aarch64', '\tlicense = GPL-3.0-or-later', '\tprovides = nyist-connect', '\tprovides = auth-nyist', '\tconflicts = nyist-connect', '\toptions = !strip']
    lines = ['# Generated from release assets; publish these exact bytes at the URLs below.',
             'pkgname=nyist-connect-bin', f'pkgver={version.replace("-", "_")}', 'pkgrel=1',
             "pkgdesc='NYIST campus network connector and authentication CLI'",
             "arch=('x86_64' 'aarch64')", f"url='https://github.com/{repo}'", "license=('GPL-3.0-or-later')",
             "provides=('nyist-connect' 'auth-nyist')", "conflicts=('nyist-connect')", "options=('!strip')"]
    for goarch, (_, arch, cli) in ARCHES.items():
        assets = [f'nyist-connect-linux-{goarch}', f'auth-nyist.linux.{cli}']
        sources = [(name, inputs / name) for name in assets] + [(name, ROOT / name) for name in DOCS]
        lines.append(f'source_{arch}=(')
        for name, _ in sources:
            # Documentation names are shared across architectures in a single release.
            lines.append(f"  '{name}::https://github.com/{repo}/releases/download/{tag}/{name}'")
        lines.append(')')
        srcinfo.extend(f'\tsource_{arch} = {name}::https://github.com/{repo}/releases/download/{tag}/{name}' for name, _ in sources)
        srcinfo.extend(f'\tsha256sums_{arch} = {hashlib.sha256(path.read_bytes()).hexdigest()}' for _, path in sources)
        lines.append(f'sha256sums_{arch}=(')
        lines.extend(f"  '{hashlib.sha256(path.read_bytes()).hexdigest()}'" for _, path in sources)
        lines.append(')')
    lines += ['', 'package() {', '  case "$CARCH" in',
              '    x86_64) gui=nyist-connect-linux-amd64; cli=auth-nyist.linux.x86_64 ;;',
              '    aarch64) gui=nyist-connect-linux-arm64; cli=auth-nyist.linux.arm64 ;;',
              '  esac', '  install -Dm755 "$srcdir/$gui" "$pkgdir/usr/bin/nyist-connect"',
              '  install -Dm755 "$srcdir/$cli" "$pkgdir/usr/bin/auth-nyist"',
              '  install -Dm644 "$srcdir/LICENSE" "$pkgdir/usr/share/licenses/nyist-connect/LICENSE"',
              '  install -Dm644 "$srcdir/README.md" "$pkgdir/usr/share/doc/nyist-connect/README.md"',
              '  install -Dm644 "$srcdir/NYIST-CONNECT.md" "$pkgdir/usr/share/doc/nyist-connect/NYIST-CONNECT.md"', '}']
    (out / 'PKGBUILD').write_text('\n'.join(lines) + '\n')
    (out / '.SRCINFO').write_text('\n'.join(srcinfo + ['', 'pkgname = nyist-connect-bin']) + '\n')


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--version', default=os.environ.get('VERSION'), help='required SemVer, e.g. 1.2.3 or 1.2.3-rc.1')
    p.add_argument('--arch', choices=ARCHES, action='append', help='repeat to select architectures (default: both)')
    p.add_argument('--format', choices=('deb', 'rpm', 'arch', 'aur', 'all'), action='append', help='repeat; default all')
    p.add_argument('--input-dir', type=Path, default=ROOT / 'dist')
    p.add_argument('--output-dir', type=Path, default=ROOT / 'dist/linux')
    p.add_argument('--repo', default='GUYU2233/AuthingNYIST', help='GitHub owner/repository for pinned AUR URLs')
    p.add_argument('--tag', help='release tag (default vVERSION)')
    a = p.parse_args()
    if a.version and a.version.startswith('v'):
        a.version = a.version[1:]
    num = r'(?:0|[1-9][0-9]*)'
    ident = r'(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
    if not a.version or not re.fullmatch(rf'{num}\.{num}\.{num}(?:-{ident}(?:\.{ident})*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?', a.version):
        p.error('VERSION/--version must be explicit valid SemVer (optional leading v)')
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', a.repo):
        p.error('--repo must be owner/repository')
    tag = a.tag or 'v' + a.version
    if not re.fullmatch(r'[A-Za-z0-9_.+-]+', tag):
        p.error('--tag must be a simple release tag')
    formats = set(a.format or ['all'])
    if 'all' in formats:
        formats = {'deb', 'rpm', 'arch', 'aur'}
    arches = a.arch or list(ARCHES)
    inputs, out = a.input_dir.resolve(), a.output_dir.resolve()
    out.mkdir(parents=True, exist_ok=True)
    for tool, fmt in [('dpkg-deb', 'deb'), ('rpmbuild', 'rpm'), ('zstd', 'arch')]:
        if fmt in formats and not shutil.which(tool):
            p.error(f'{fmt} requires {tool}; install it or select other --format values')
    required = list(ARCHES) if 'aur' in formats else arches
    for arch in required:
        for name in (f'nyist-connect-linux-{arch}', f'auth-nyist.linux.{ARCHES[arch][2]}'):
            if not (inputs / name).is_file() or (inputs / name).stat().st_size == 0:
                p.error(f'missing nonempty release asset: {inputs / name}')
    for name in DOCS:
        if not (ROOT / name).is_file():
            p.error(f'missing document: {name}')
    # Version encodings preserve prerelease ordering; build metadata is retained.
    debver = a.version.replace('-', '~', 1)
    nativever = a.version.replace('-', '_')
    epoch = int(os.environ.get('SOURCE_DATE_EPOCH', '0'))
    assets = []
    for arch in arches:
        debarch, nativearch, cli = ARCHES[arch]
        if 'deb' in formats:
            assets.append(f'nyist-connect_{debver}_{debarch}.deb')
        if 'arch' in formats:
            assets.append(f'nyist-connect-{nativever}-1-{nativearch}.pkg.tar.zst')
        binaries = (inputs / f'nyist-connect-linux-{arch}', inputs / f'auth-nyist.linux.{cli}')
        with tempfile.TemporaryDirectory(prefix='nyist-linux-') as tmp:
            base = Path(tmp)
            payload = base / 'payload'
            stage(payload, binaries)
            size = sum(f.stat().st_size for f in payload.rglob('*') if f.is_file())
            if 'deb' in formats:
                control = payload / 'DEBIAN/control'
                control.parent.mkdir()
                control.write_text(f'Package: nyist-connect\nVersion: {debver}\nArchitecture: {debarch}\nMaintainer: AuthingNYIST maintainers <noreply@github.com>\nSection: net\nPriority: optional\nInstalled-Size: {(size + 1023) // 1024}\nHomepage: https://github.com/{a.repo}\nDescription: NYIST campus network connector and authentication CLI\n Includes nyist-connect and auth-nyist. No automatic login or service changes.\n')
                run('dpkg-deb', '--root-owner-group', '--build', payload, out / f'nyist-connect_{debver}_{debarch}.deb')
                shutil.rmtree(control.parent)
            if 'arch' in formats:
                info = f'pkgname = nyist-connect\npkgbase = nyist-connect\npkgver = {nativever}-1\npkgdesc = NYIST campus network connector and authentication CLI\nurl = https://github.com/{a.repo}\nbuilddate = {epoch}\npackager = AuthingNYIST maintainers\nsize = {size}\narch = {nativearch}\nlicense = GPL-3.0-or-later\n'
                archive = base / 'package.tar'
                with tarfile.open(archive, 'w', format=tarfile.GNU_FORMAT) as tar:
                    data = info.encode()
                    member = tarfile.TarInfo('.PKGINFO')
                    member.size, member.mode, member.mtime = len(data), 0o644, epoch
                    tar.addfile(member, io.BytesIO(data))
                    for path in sorted(payload.rglob('*')):
                        member = tar.gettarinfo(str(path), str(path.relative_to(payload)))
                        member.uid = member.gid = 0
                        member.uname = member.gname = 'root'
                        member.mtime = epoch
                        if path.is_file():
                            with path.open('rb') as stream:
                                tar.addfile(member, stream)
                        else:
                            tar.addfile(member)
                run('zstd', '-q', '-f', archive, '-o', out / f'nyist-connect-{nativever}-1-{nativearch}.pkg.tar.zst')
            if 'rpm' in formats:
                top = base / 'rpmbuild'
                for name in ('BUILD', 'BUILDROOT', 'RPMS', 'SOURCES', 'SPECS', 'SRPMS'):
                    (top / name).mkdir(parents=True)
                # No shell interpolation of source paths in the generated spec.
                shutil.copytree(payload, top / 'SOURCES/payload')
                rpmver = a.version.split('+')[0].replace('-', '~', 1)
                release = '1' + ('.' + a.version.split('+', 1)[1] if '+' in a.version else '')
                spec = (ROOT / 'packaging/linux/nyist-connect.spec.in').read_text().replace('@VERSION@', rpmver).replace('@RELEASE@', release).replace('@ARCH@', nativearch).replace('@REPO@', a.repo)
                specpath = top / 'SPECS/nyist-connect.spec'
                specpath.write_text(spec)
                run('rpmbuild', '-bb', '--define', f'_topdir {top}', '--target', nativearch, specpath)
                for rpm in (top / 'RPMS').rglob('*.rpm'):
                    shutil.copyfile(rpm, out / rpm.name)
                    assets.append(rpm.name)
    if 'aur' in formats:
        make_aur(out, a.version, tag, a.repo, inputs)
        aur_name = f'nyist-connect-aur-{a.version}.tar.gz'
        with tarfile.open(out / aur_name, 'w:gz') as tar:
            for name in ('PKGBUILD', '.SRCINFO'):
                tar.add(out / name, arcname=f'nyist-connect-bin/{name}')
        assets.append(aur_name)
    (out / 'linux-assets.txt').write_text('\n'.join(assets) + '\n')
    print(f'Linux packages written to {out}')


if __name__ == '__main__':
    main()
