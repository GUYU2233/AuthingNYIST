#!/usr/bin/env bash
# Whitelist release assets: never publish config files or leftover test binaries.
set -euo pipefail
cd "$(dirname "$0")/.."
assets=(
  nyist-connect.exe nyist-connect-linux-amd64 nyist-connect-linux-arm64
  nyist-connect-macos-amd64 nyist-connect-macos-arm64
)
# GitHub's macOS runner provides real DMGs; other builders may explicitly opt in.
if [[ "${REQUIRE_MACOS_DMG:-0}" == 1 ]]; then
  assets+=(NYIST-Connect-macos-amd64.dmg NYIST-Connect-macos-arm64.dmg)
fi
for file in "${assets[@]}"; do
  test -s "dist/$file" || { echo "Missing release binary: $file" >&2; exit 1; }
done
# Distribution assets are admitted only through validated producer manifests.
for manifest in dist/linux-assets.txt dist/sandbox-assets.txt dist/repositories-assets.txt dist/distribution-assets.txt; do
  [[ -f "$manifest" ]] || continue
  while IFS= read -r file; do
    [[ "$file" =~ ^[A-Za-z0-9][A-Za-z0-9._+~-]*$ ]] || { echo "Invalid distribution asset name: $file" >&2; exit 1; }
    test -s "dist/$file" && test ! -L "dist/$file" || { echo "Missing distribution asset: $file" >&2; exit 1; }
    assets+=("$file")
  done < "$manifest"
done
cp README.md NYIST-CONNECT.md LICENSE dist/
cp docs/PACKAGING.md dist/PACKAGING.md
assets+=(README.md NYIST-CONNECT.md LICENSE PACKAGING.md)
(
  cd dist
  sha256sum "${assets[@]}" > SHA256SUMS.txt
  sha256sum --check SHA256SUMS.txt
)
printf '%s\n' "${assets[@]}" SHA256SUMS.txt > dist/release-assets.txt
