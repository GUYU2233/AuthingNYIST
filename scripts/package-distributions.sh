#!/usr/bin/env bash
# Generate the published Linux packages from tested binaries; never install or
# publish. The release carries installers only (EXE, DMGs, deb, rpm), so the
# default run builds exactly the deb and rpm sets.
#
# BUILD_EXTRAS=1 additionally builds the maintainer-only by-products: the
# portable CLI archive, Flatpak/Snap manifests, AUR/Homebrew/MacPorts/Nix
# recipes, Arch packages and repository metadata. Those are not published, and
# the recipes pin the CLI archive that the releaser no longer uploads, so they
# are only useful for local or self-hosted packaging experiments.
set -euo pipefail
cd "$(dirname "$0")/.."
: "${VERSION:?Set VERSION to the release tag, e.g. v1.2.3}"
if [[ ! "$VERSION" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]]; then
  echo 'Invalid VERSION: expected a release semantic version' >&2; exit 1
fi
version="${VERSION#v}"

PYTHONDONTWRITEBYTECODE=1 python3 packaging/linux/test_package.py

if [[ "${BUILD_EXTRAS:-0}" != 1 ]]; then
  # Published path: deb and rpm for both architectures.
  bash scripts/package-linux.sh \
    --output-dir dist \
    --format deb --format rpm
  rm -rf dist/package-recipes dist/sandbox
  rm -f dist/distribution-assets.txt
  exit 0
fi

# The portable CLI archive is created first: AUR, Flatpak, Snap, Homebrew,
# MacPorts and Nix recipes all pin its checksum.
bash scripts/build-extras.sh cli
cli_archive="$PWD/dist/authingnyist-cli-extras-${version}.tar.gz"

bash scripts/package-linux.sh --output-dir dist --cli-archive "$cli_archive"
CLI_ARCHIVE="$cli_archive" bash scripts/package-sandbox.sh "$VERSION"
rm -rf dist/package-recipes
python3 scripts/generate-recipes.py \
  --version "$VERSION" \
  --dist dist \
  --cli-archive "$cli_archive" \
  --output dist/package-recipes

if [[ "${BUILD_REPOSITORIES:-1}" == 1 ]]; then
  bash scripts/package-repositories.sh
fi

# Built last: it contains the generated recipes, but no recipe hashes it.
bash scripts/build-extras.sh packaging

# Staging trees are already inside the extras archives. Remove them so the
# workspace (and the CI artifact directory) holds only publishable files.
rm -rf dist/package-recipes dist/sandbox
rm -f dist/distribution-assets.txt
