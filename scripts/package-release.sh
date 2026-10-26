#!/usr/bin/env bash
# Whitelist release assets: never publish config files or leftover test binaries.
set -euo pipefail
cd "$(dirname "$0")/.."
assets=(
  nyist-connect.exe nyist-connect-linux-amd64 nyist-connect-linux-arm64
  nyist-connect-macos-amd64 nyist-connect-macos-arm64
  auth-nyist.macos.x86_64 auth-nyist.macos.arm64 auth-nyist.win64.exe
  auth-nyist.linux.x86_64 auth-nyist.linux.arm64 auth-nyist.linux.arm
  auth-nyist.linux.armv5 auth-nyist.linux.armv6 auth-nyist.linux.mipsle
  auth-nyist.linux.mipsbe auth-nyist.linux.ppc64le auth-nyist.linux.riscv64
  auth-nyist.linux.loong64
)
# GitHub's macOS runner provides real DMGs; other builders may explicitly opt in.
if [[ "${REQUIRE_MACOS_DMG:-0}" == 1 ]]; then
  assets+=(NYIST-Connect-macos-amd64.dmg NYIST-Connect-macos-arm64.dmg)
fi
for file in "${assets[@]}"; do
  test -s "dist/$file" || { echo "Missing release binary: $file" >&2; exit 1; }
done
cp README.md NYIST-CONNECT.md LICENSE dist/
assets+=(README.md NYIST-CONNECT.md LICENSE)
(
  cd dist
  sha256sum "${assets[@]}" > SHA256SUMS.txt
  sha256sum --check SHA256SUMS.txt
)
printf '%s\n' "${assets[@]}" SHA256SUMS.txt > dist/release-assets.txt
