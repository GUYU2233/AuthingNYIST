#!/usr/bin/env python3
"""Generate pinned binary package recipes from a completed release dist directory.

Usage: python3 scripts/generate-recipes.py --version v1.2.3 --dist dist --output dist/recipes

The portable CLI archive (authingnyist-cli-extras-<version>.tar.gz) is the one
source every binary recipe pins. That keeps the release page small and means a
recipe can never point at an asset that is no longer published.

No downloads, builds, service registration, or authentication are performed.
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re

BASE = "https://github.com/GUYU2233/AuthingNYIST/releases/download"
SEMVER = re.compile(r"v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?", re.ASCII)
ROOT = Path(__file__).resolve().parents[1]


def sha256_file(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def cli_asset(path, version):
    path = Path(path)
    if not path.is_file() or path.is_symlink() or path.stat().st_size == 0:
        raise ValueError(f"missing, empty, or symlink CLI archive: {path}")
    if path.name != f"authingnyist-cli-extras-{version}.tar.gz":
        raise ValueError(f"CLI archive name does not match the release version: {path.name}")
    return {"name": path.name, "sha256": sha256_file(path), "size": path.stat().st_size}


def load_cask_renderer():
    path = ROOT / "packaging/homebrew/generate_cask.py"
    spec = importlib.util.spec_from_file_location("generate_cask", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def dmgs(dist, tag):
    result = {}
    for arch in ("amd64", "arm64"):
        name = f"NYIST-Connect-macos-{arch}.dmg"
        path = dist / name
        if not path.exists():
            continue
        if not path.is_file() or path.is_symlink() or path.stat().st_size == 0:
            raise ValueError(f"missing, empty, or symlink DMG: {path}")
        result[arch] = {"name": name, "sha256": sha256_file(path), "url": f"{BASE}/{tag}/{name}"}
    if len(result) == 1:
        raise ValueError("incomplete macOS DMG set: need both amd64 and arm64")
    return result


def brew(version, cli):
    lines = ["# Generated from the portable CLI release archive; do not edit hashes by hand.",
             "class Authingnyist < Formula",
             '  desc "NYIST campus network connector and authentication CLI"',
             '  homepage "https://github.com/GUYU2233/AuthingNYIST"',
             f'  version "{version}"',
             '  license "GPL-3.0-only"',
             "",
             "  on_linux do",
             f'    url "{cli["url"]}"',
             f'    sha256 "{cli["sha256"]}"',
             "  end",
             "",
             "  def install",
             '    arch = Hardware::CPU.arm? ? "arm64" : "amd64"',
             '    bin.install "cli/nyist-connect-linux-#{arch}" => "nyist-connect"',
             "  end",
             "",
             "  test do",
             '    assert_match version.to_s, shell_output("#{bin}/nyist-connect --version")',
             "  end",
             "end",
             ""]
    return "\n".join(lines)


def ports(version, cli):
    lines = ['# Generated binary-only port; installation never starts services.',
             '# The release archive carries both architectures; the matching CLI is installed.',
             'PortSystem 1.0', '', 'name authingnyist', f'version {version}', 'categories net',
             'maintainers nomaintainer', 'license GPL-3', 'description NYIST network connector',
             'long_description ${description}', 'homepage https://github.com/GUYU2233/AuthingNYIST',
             'platforms darwin', 'supported_archs x86_64 arm64', 'universal_variant no',
             f'distfiles {cli["name"]}', f'checksums {cli["name"]} sha256 {cli["sha256"]} size {cli["size"]}',
             f'master_sites {BASE}/{cli["tag"]}/', 'use_configure no', 'worksrcdir .', 'build {}',
             'extract {}', '', 'if {${build_arch} eq "x86_64"} {', '    set nyist_binary cli/nyist-connect-macos-amd64',
             '} else {', '    set nyist_binary cli/nyist-connect-macos-arm64', '}', '',
             'destroot {', '    xinstall -d ${destroot}${prefix}/bin',
             '    xinstall -m 0755 ${worksrcpath}/${nyist_binary} ${destroot}${prefix}/bin/nyist-connect', '}', '']
    return "\n".join(lines)


def nix(version, cli):
    lines = ['# Binary release package. Supply nixpkgs explicitly; no source/vendor hash.',
             '{ pkgs ? import <nixpkgs> {} }:', 'let', f'  release = pkgs.fetchurl {{',
             f'    url = "{cli["url"]}";', f'    sha256 = "{cli["sha256"]}";', '  };',
             'in pkgs.stdenvNoCC.mkDerivation {', '  pname = "authingnyist";', f'  version = "{version}";',
             '  src = release;', '  dontBuild = true;', '  dontStrip = true;', '  dontPatchELF = true;',
             '  dontPatchShebangs = true;', '  installPhase = \'\'', '    mkdir -p "$out/bin"',
             '    case "${pkgs.stdenv.hostPlatform.system}" in',
             '      x86_64-linux) binary=cli/nyist-connect-linux-amd64 ;;',
             '      aarch64-linux) binary=cli/nyist-connect-linux-arm64 ;;',
             '      x86_64-darwin) binary=cli/nyist-connect-macos-amd64 ;;',
             '      aarch64-darwin) binary=cli/nyist-connect-macos-arm64 ;;',
             '      *) echo "unsupported platform" >&2; exit 1 ;;', '    esac',
             '    install -m755 "$binary" "$out/bin/nyist-connect"', '  \'\';', '  doInstallCheck = true;',
             '  installCheckPhase = \'\'', '    "$out/bin/nyist-connect" --version', '  \'\';',
             '  meta = {', '    description = "NYIST network connector";',
             '    homepage = "https://github.com/GUYU2233/AuthingNYIST";',
             '    license = pkgs.lib.licenses.gpl3Only;',
             '    platforms = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];',
             '    mainProgram = "nyist-connect";', '  };', '}', '']
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--dist", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--cli-archive", type=Path,
                        help="portable CLI archive (default: DIST/authingnyist-cli-extras-VERSION.tar.gz)")
    args = parser.parse_args(argv)
    if not SEMVER.fullmatch(args.version):
        parser.error("--version must be a safe semantic version (optionally prefixed with v)")
    version = args.version.removeprefix("v")
    cli_path = args.cli_archive or args.dist / f"authingnyist-cli-extras-{version}.tar.gz"
    try:
        cli = cli_asset(cli_path, version)
        cli["url"] = f"{BASE}/{args.version}/{cli['name']}"
        cli["tag"] = args.version
        mac_dmgs = dmgs(args.dist, args.version)
    except (ValueError, OSError) as exc:
        parser.error(str(exc))

    outputs = {
        "homebrew/authingnyist.rb": brew(version, cli),
        "macports/Portfile": ports(version, cli),
        "nix/default.nix": nix(version, cli),
    }
    if mac_dmgs:
        renderer = load_cask_renderer()
        try:
            outputs["homebrew/Casks/nyist-connect.rb"] = renderer.render(
                version=version, tag=args.version, repo="GUYU2233/AuthingNYIST", dmgs=mac_dmgs)
        except ValueError as exc:
            parser.error(str(exc))
    manifest = {
        "version": version,
        "tag": args.version,
        "cli_archive": {key: cli[key] for key in ("name", "sha256", "size")},
        "dmgs": mac_dmgs,
    }
    outputs["manifest.json"] = json.dumps(manifest, indent=2, sort_keys=True) + "\n"
    for name, text in outputs.items():
        path = args.output / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
    print(f"Generated {len(outputs)} recipe files in {args.output}")


if __name__ == "__main__":
    main()
