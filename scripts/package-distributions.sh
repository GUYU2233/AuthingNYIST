#!/usr/bin/env bash
# Generate package-manager assets from tested binaries; never install or publish.
set -euo pipefail
cd "$(dirname "$0")/.."
: "${VERSION:?Set VERSION to the release tag, e.g. v1.2.3}"
if [[ ! "$VERSION" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo 'Invalid VERSION: expected a release semantic version' >&2; exit 1
fi
PYTHONDONTWRITEBYTECODE=1 python3 packaging/linux/test_package.py
bash scripts/package-linux.sh --output-dir dist
bash scripts/package-sandbox.sh
python3 scripts/generate-recipes.py --version "$VERSION" --dist dist --output dist/package-recipes
# Archive pinned recipes as one whitelisted release asset.
tar -czf "dist/authingnyist-package-recipes-${VERSION#v}.tar.gz" -C dist package-recipes
printf '%s\n' "authingnyist-package-recipes-${VERSION#v}.tar.gz" > dist/distribution-assets.txt
if [[ "${BUILD_REPOSITORIES:-1}" == 1 ]]; then bash scripts/package-repositories.sh; fi
