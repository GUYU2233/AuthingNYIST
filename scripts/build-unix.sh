#!/usr/bin/env bash
# Build only: no network authentication or service changes.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p dist
export CGO_ENABLED=0
VERSION="${VERSION:-${GITHUB_REF_NAME:-${CI_COMMIT_TAG:-dev}}}"
LDFLAGS="-s -w -X github.com/Palvef/AuthingNYIST/internal/version.Version=$VERSION"
# Run resource generator for the host, then cross-compile the native GUI.
go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -manifest cmd/nyist/app.manifest -ico cmd/nyist/app.ico -o cmd/nyist/rsrc_windows_amd64.syso
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-H windowsgui $LDFLAGS" -o dist/nyist-connect.exe ./cmd/nyist
for arch in amd64 arm64; do
  GOOS=linux GOARCH="$arch" go build -trimpath -ldflags="$LDFLAGS" -o "dist/nyist-connect-linux-$arch" ./cmd/nyist
done
for arch in amd64 arm64; do
  GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags="$LDFLAGS" -o "dist/nyist-connect-macos-$arch" ./cmd/nyist
done
