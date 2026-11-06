#!/usr/bin/env bash
# Generate pinned recipes from release binaries; builds are explicit and never publish.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if [[ "${1:-}" == test ]]; then
  exec python3 "$ROOT/packaging/flatpak/test_generation.py"
fi
if [[ $# == 0 && -n "${VERSION:-}" ]]; then
  set -- "$VERSION"
fi
if [[ $# -lt 1 || $# -gt 4 ]]; then
  echo "Usage: $0 VERSION [DIST_DIR [OUTPUT_DIR [generate|flatpak|snap]]] | test" >&2
  exit 2
fi
exec python3 "$ROOT/packaging/flatpak/generate.py" "$ROOT" "$@"
