#!/usr/bin/env bash
# Native macOS packager. Requires Go and Xcode Command Line Tools; no CI changes.
# Usage: ./scripts/build-macos.sh [arm64|amd64|all|universal] (default: all)
set -euo pipefail
umask 077

if [[ "$(uname -s)" != Darwin ]]; then
  echo 'error: this script requires macOS (Swift/AppKit, codesign and hdiutil).' >&2
  exit 1
fi
for tool in go xcrun xcode-select hdiutil codesign lipo file plutil ditto; do
  command -v "$tool" >/dev/null || { echo "error: required tool missing: $tool" >&2; exit 1; }
done
xcode-select -p >/dev/null
xcrun --find swiftc >/dev/null
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
SDK="$(xcrun --sdk macosx --show-sdk-path)"
MODE="${1:-all}"
[[ $# -le 1 ]] || { echo 'usage: build-macos.sh [arm64|amd64|all|universal]' >&2; exit 1; }
case "$MODE" in arm64|amd64|all|universal) ;; *) echo 'error: unknown architecture' >&2; exit 1 ;; esac
OUT="$ROOT/dist"
mkdir -p "$OUT"
TEMP="$(mktemp -d "${TMPDIR:-/tmp}/nyist-macos.XXXXXXXX")"
cleanup() { rm -rf -- "$TEMP"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# This is an ad-hoc local integrity signature, NOT a Developer ID identity.
# Artifacts are unsigned for distribution (unnotarized); Gatekeeper may block them.
echo 'NOTICE: ad-hoc signed only; unsigned for distribution / unnotarized.'
echo 'NOTICE: app.ico is Windows-only. This macOS bundle has no custom app icon.'
echo 'NOTICE: macOS 13+; requires a local macOS build and runtime smoke test.'

build_slice() {
  local arch="$1" swift_arch="$1"
  [[ "$arch" != amd64 ]] || swift_arch=x86_64
  mkdir -p "$TEMP/$arch"
  (cd "$ROOT" && GOOS=darwin GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -o "$TEMP/$arch/nyist-core" ./cmd/nyist)
  xcrun --sdk macosx swiftc -swift-version 5 -parse-as-library -O \
    -sdk "$SDK" -target "$swift_arch-apple-macosx13.0" \
    -module-cache-path "$TEMP/module-cache-$arch" \
    -framework SwiftUI -framework AppKit \
    "$ROOT/macos/NYISTConnect.swift" -o "$TEMP/$arch/NYIST-Connect"
  lipo -verify_arch "$swift_arch" "$TEMP/$arch/nyist-core"
  lipo -verify_arch "$swift_arch" "$TEMP/$arch/NYIST-Connect"
  file "$TEMP/$arch/nyist-core" "$TEMP/$arch/NYIST-Connect"
}

package_app() {
  local arch="$1" staging="$TEMP/staging-$1" app
  mkdir -p "$staging/NYIST Connect.app/Contents/MacOS"
  app="$staging/NYIST Connect.app"
  ditto "$TEMP/$arch/nyist-core" "$app/Contents/MacOS/nyist-core"
  ditto "$TEMP/$arch/NYIST-Connect" "$app/Contents/MacOS/NYIST-Connect"
  chmod 755 "$app/Contents/MacOS/nyist-core" "$app/Contents/MacOS/NYIST-Connect"
  cat > "$app/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>cn.edu.nyist.authing.connect</string>
<key>CFBundleName</key><string>NYIST Connect</string>
<key>CFBundleDisplayName</key><string>NYIST Connect</string>
<key>CFBundleExecutable</key><string>NYIST-Connect</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>1.0.0</string>
<key>CFBundleVersion</key><string>1</string>
<key>LSMinimumSystemVersion</key><string>13.0</string>
<key>NSHighResolutionCapable</key><true/>
<key>NSPrincipalClass</key><string>NSApplication</string>
</dict></plist>
PLIST
  plutil -lint "$app/Contents/Info.plist"
  cat > "$staging/BUILD-NOTICE.txt" <<'NOTICE'
NYIST Connect requires macOS 13 or newer.
Ad-hoc signed only: unsigned for distribution and NOT notarized by Apple.
Gatekeeper may block this application. No Developer ID signing is claimed.
No custom macOS app icon is bundled; the repository's app.ico is Windows-only.
Password storage: plaintext in ~/Library/Application Support/AuthingNYIST/config.json,
mode 0600, not Keychain. Quit stops keepalive but does not log out.
Install by dragging NYIST Connect.app to Applications before opening.
Only run one GUI/core keepalive per config; do not run an external daemon concurrently.
NOTICE
  ln -s /Applications "$staging/Applications"
  # umask 077 protects work-in-progress; release payload must be readable by all installers.
  chmod 755 "$staging" "$app" "$app/Contents" "$app/Contents/MacOS"
  chmod 644 "$app/Contents/Info.plist" "$staging/BUILD-NOTICE.txt"
  codesign --force --sign - --timestamp=none "$app/Contents/MacOS/nyist-core"
  codesign --force --sign - --timestamp=none "$app"
  codesign --verify --strict --verbose=2 "$app/Contents/MacOS/nyist-core"
  codesign --verify --deep --strict --verbose=2 "$app"
  local expected
  case "$arch" in amd64) expected=x86_64 ;; arm64) expected=arm64 ;; universal) expected='arm64 x86_64' ;; esac
  # Intentional word splitting: lipo accepts one architecture per argument.
  lipo -verify_arch $expected "$app/Contents/MacOS/nyist-core"
  lipo -verify_arch $expected "$app/Contents/MacOS/NYIST-Connect"
  local dmg="$TEMP/NYIST-Connect-macos-$arch.dmg"
  hdiutil create -volname 'NYIST Connect' -srcfolder "$staging" -format UDZO -ov "$dmg"
  hdiutil verify "$dmg"
  # Only replace previous completed artifacts after every verification succeeds.
  ditto "$app" "$TEMP/export-$arch.app"
  rm -rf -- "$OUT/NYIST-Connect-macos-$arch.app"
  mv -- "$TEMP/export-$arch.app" "$OUT/NYIST-Connect-macos-$arch.app"
  mv -f -- "$dmg" "$OUT/NYIST-Connect-macos-$arch.dmg"
  chmod 644 "$OUT/NYIST-Connect-macos-$arch.dmg"
  echo "Created: $OUT/NYIST-Connect-macos-$arch.dmg"
}

case "$MODE" in
  arm64|amd64) build_slice "$MODE"; package_app "$MODE" ;;
  all) build_slice arm64; build_slice amd64; package_app arm64; package_app amd64 ;;
  universal)
    build_slice arm64; build_slice amd64
    mkdir -p "$TEMP/universal"
    for name in nyist-core NYIST-Connect; do
      lipo -create "$TEMP/arm64/$name" "$TEMP/amd64/$name" -output "$TEMP/universal/$name"
    done
    package_app universal
    ;;
esac
