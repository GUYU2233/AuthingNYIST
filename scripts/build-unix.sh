#!/usr/bin/env bash
# Build only: no network authentication or service changes.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p dist
export CGO_ENABLED=0
# Run resource generator for the host, then cross-compile the native GUI.
go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -manifest cmd/nyist/app.manifest -ico cmd/nyist/app.ico -o cmd/nyist/rsrc_windows_amd64.syso
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-H windowsgui -s -w' -o dist/nyist-connect.exe ./cmd/nyist
for arch in amd64 arm64; do
  GOOS=linux GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "dist/nyist-connect-linux-$arch" ./cmd/nyist
done
build_cli() {
  local os="$1" arch="$2" suffix="$3"
  GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "dist/auth-nyist.$suffix" ./cli
}
build_cli darwin amd64 macos.x86_64
build_cli darwin arm64 macos.arm64
build_cli windows amd64 win64.exe
build_cli linux amd64 linux.x86_64
build_cli linux arm64 linux.arm64
build_cli linux arm linux.arm
GOARM=5 build_cli linux arm linux.armv5
GOARM=6 build_cli linux arm linux.armv6
GOMIPS=softfloat build_cli linux mipsle linux.mipsle
GOMIPS=softfloat build_cli linux mips linux.mipsbe
build_cli linux ppc64le linux.ppc64le
build_cli linux riscv64 linux.riscv64
build_cli linux loong64 linux.loong64
