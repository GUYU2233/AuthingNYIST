#!/usr/bin/env bash
# Package prebuilt release binaries; never authenticate or manage services.
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 "$ROOT/packaging/linux/package.py" "$@"
