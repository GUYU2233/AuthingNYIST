#!/usr/bin/env python3
"""Generate pinned binary package recipes from a completed release dist directory.

Usage: python3 scripts/generate-recipes.py --version v1.2.3 --dist dist --output dist/recipes
No downloads, builds, service registration, or authentication are performed.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re

BASE = "https://github.com/GUYU2233/AuthingNYIST/releases/download"
SEMVER = re.compile(r"v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?", re.ASCII)


def assets(dist):
    result = {}
    for os in ("linux", "macos"):
        for arch in ("amd64", "arm64"):
            names = (f"nyist-connect-{os}-{arch}",)
            entries = []
            for name in names:
                path = dist / name
                if not path.is_file() or path.is_symlink() or path.stat().st_size == 0:
                    raise ValueError(f"missing, empty, or symlink release binary: {path}")
                digest = hashlib.sha256()
                with path.open("rb") as stream:
                    for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                        digest.update(chunk)
                entries.append({"name": name, "sha256": digest.hexdigest(), "size": path.stat().st_size})
            result[os, arch] = entries
    return result


def brew(version, tag, data):
    lines = ["# Generated from release binaries; do not edit hashes by hand.", "class Authingnyist < Formula", '  desc "NYIST network connector"', '  homepage "https://github.com/GUYU2233/AuthingNYIST"', f'  version "{version}"', '  license "GPL-3.0-only"']
    for os, brew_os in (("macos", "macos"), ("linux", "linux")):
        lines.append(f"  on_{brew_os} do")
        for arch, condition in (("arm64", "Hardware::CPU.arm?"), ("amd64", "Hardware::CPU.intel?")):
            nyist, = data[os, arch]
            lines += [f"    if {condition} && Hardware::CPU.is_64_bit?", f'      url "{BASE}/{tag}/{nyist["name"]}", using: :nounzip', f'      sha256 "{nyist["sha256"]}"', '    end']
        lines.append("  end")
    lines += ['  def install', '    binary = Dir["nyist-connect-*"].first', '    bin.install binary => "nyist-connect"', '  end', '  test do', '    assert_match version.to_s, shell_output("#{bin}/nyist-connect --version")', '  end', 'end', '']
    return "\n".join(lines)


def ports(version, tag, data):
    lines = ['# Generated binary-only port; installation never starts services.', 'PortSystem 1.0', '', 'name authingnyist', f'version {version}', 'categories net', 'maintainers nomaintainer', 'license GPL-3', 'description NYIST network connector', 'long_description ${description}', 'homepage https://github.com/GUYU2233/AuthingNYIST', 'platforms darwin', 'supported_archs x86_64 arm64', 'universal_variant no', f'master_sites {BASE}/{tag}/', 'use_configure no', 'worksrcdir .', 'build {}', 'extract {}', '']
    for arch, port_arch in (("amd64", "x86_64"), ("arm64", "arm64")):
        nyist, = data["macos", arch]
        lines += [f'if {{${{build_arch}} eq "{port_arch}"}} {{', f'    set nyist_binary {nyist["name"]}', '    distfiles ${nyist_binary}', f'    checksums ${{nyist_binary}} sha256 {nyist["sha256"]} size {nyist["size"]}', '}']
    lines += ['destroot {', '    xinstall -d ${destroot}${prefix}/bin', '    xinstall -m 0755 ${distpath}/${nyist_binary} ${destroot}${prefix}/bin/nyist-connect', '}', '']
    return "\n".join(lines)


def nix(version, tag, data):
    lines = ['# Binary release package. Supply nixpkgs explicitly; no source/vendor hash.', '{ pkgs ? import <nixpkgs> {} }:', 'let', '  releases = {']
    for os, nix_os in (("linux", "linux"), ("macos", "darwin")):
        for arch, nix_arch in (("amd64", "x86_64"), ("arm64", "aarch64")):
            lines += [f'    "{nix_arch}-{nix_os}" = {{']
            for key, asset in zip(("nyist",), data[os, arch]):
                lines += [f'      {key} = pkgs.fetchurl {{', f'        url = "{BASE}/{tag}/{asset["name"]}";', f'        sha256 = "{asset["sha256"]}";', '      };']
            lines.append('    };')
    lines += ['  };', '  binaries = releases.${pkgs.stdenv.hostPlatform.system} or (throw "AuthingNYIST supports only x86_64/aarch64 Linux and Darwin");', 'in pkgs.stdenvNoCC.mkDerivation {', '  pname = "authingnyist";', f'  version = "{version}";', '  dontUnpack = true;', '  dontBuild = true;', '  dontStrip = true;', '  dontPatchELF = true;', '  dontPatchShebangs = true;', '  installPhase = \'\'', '    mkdir -p "$out/bin"', '    install -m755 ${binaries.nyist} "$out/bin/nyist-connect"', '  \'\';', '  doInstallCheck = true;', '  installCheckPhase = \'\'', '    "$out/bin/nyist-connect" --version', '  \'\';', '  meta = {', '    description = "NYIST network connector";', '    homepage = "https://github.com/GUYU2233/AuthingNYIST";', '    license = pkgs.lib.licenses.gpl3Only;', '    platforms = builtins.attrNames releases;', '    mainProgram = "nyist-connect";', '  };', '}', '']
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--dist", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args(argv)
    if not SEMVER.fullmatch(args.version):
        parser.error("--version must be a safe semantic version (optionally prefixed with v)")
    version = args.version.removeprefix("v")
    try:
        data = assets(args.dist)
    except (ValueError, OSError) as exc:
        parser.error(str(exc))
    outputs = {"homebrew/authingnyist.rb": brew(version, args.version, data), "macports/Portfile": ports(version, args.version, data), "nix/default.nix": nix(version, args.version, data)}
    manifest = {"version": version, "tag": args.version, "assets": [entry for entries in data.values() for entry in entries]}
    outputs["manifest.json"] = json.dumps(manifest, indent=2, sort_keys=True) + "\n"
    for name, text in outputs.items():
        path = args.output / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
    print(f"Generated {len(outputs)} recipe files in {args.output}")


if __name__ == "__main__":
    main()
