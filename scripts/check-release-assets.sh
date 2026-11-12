#!/usr/bin/env bash
#
# Validate the contents of a release asset bundle without networking.
#
# Usage: bash scripts/check-release-assets.sh [DIST_DIR]
#
# Checks performed:
#   * every name in release-assets.txt matches the user-facing allowlist;
#   * no duplicates, no path components, no symlinks or empty files;
#   * the updater contract is present: nyist-connect.exe and SHA256SUMS.txt;
#   * SHA256SUMS.txt covers every published asset except itself, and only those;
#   * every recorded digest is correct.
#
# This is deliberately usable both from CI and from a local dry run.

set -euo pipefail

die() {
	printf 'release-assets: %s\n' "$*" >&2
	exit 1
}

dist="${1:-dist}"
manifest="$dist/release-assets.txt"
sums="$dist/SHA256SUMS.txt"

[[ -d "$dist" ]] || die "missing dist directory: $dist"
[[ -f "$manifest" ]] || die "missing $manifest"
[[ -f "$sums" ]] || die "missing $sums"

# User-facing installers and the checksum file only. Recipes, AUR, Arch,
# Flatpak/Snap manifests, repositories and shell completions are built by the
# packaging scripts but are not published as release assets.
# Keep this list in sync with scripts/package-release.sh.
allowlist=(
	'^nyist-connect\.exe$'
	'^NYIST-Connect-macos-(amd64|arm64)\.dmg$'
	'^nyist-connect_[A-Za-z0-9.+~_-]+_(amd64|arm64)\.deb$'
	'^nyist-connect-[A-Za-z0-9.+~_-]+\.(x86_64|aarch64)\.rpm$'
	'^SHA256SUMS\.txt$'
)

name_allowed() {
	local name="$1" pattern
	for pattern in "${allowlist[@]}"; do
		if [[ "$name" =~ $pattern ]]; then
			return 0
		fi
	done
	return 1
}

mapfile -t assets < "$manifest"
((${#assets[@]})) || die "release-assets.txt is empty"

declare -A seen=()
for name in "${assets[@]}"; do
	[[ -n "$name" ]] || die "release-assets.txt contains an empty line"
	[[ "$name" != */* ]] || die "asset names must not contain a path: $name"
	[[ "$name" =~ ^[A-Za-z0-9][A-Za-z0-9._+~-]*$ ]] ||
		die "asset name has unsupported characters: $name"
	name_allowed "$name" || die "asset is not on the allowlist: $name"
	[[ -z "${seen[$name]:-}" ]] || die "duplicate asset name: $name"
	seen[$name]=1

	path="$dist/$name"
	[[ -f "$path" ]] || die "asset is missing or not a regular file: $path"
	[[ ! -L "$path" ]] || die "asset must not be a symlink: $path"
	[[ -s "$path" ]] || die "asset is empty: $path"
done

# The Windows updater looks for these two names verbatim at the top level of
# the release; never hide either one inside an archive.
[[ -n "${seen[nyist-connect.exe]:-}" ]] ||
	die "missing nyist-connect.exe (required by the in-app updater)"
[[ -n "${seen[SHA256SUMS.txt]:-}" ]] ||
	die "missing SHA256SUMS.txt (required by the in-app updater)"

declare -A sums_by_name=()
while IFS= read -r line || [[ -n "$line" ]]; do
	[[ -n "$line" ]] || die "SHA256SUMS.txt contains an empty line"
	if [[ ! "$line" =~ ^([0-9a-fA-F]{64})\ [\ *]([A-Za-z0-9][A-Za-z0-9._+~-]*)$ ]]; then
		die "malformed line in SHA256SUMS.txt: $line"
	fi
	digest="${BASH_REMATCH[1],,}"
	sum_name="${BASH_REMATCH[2]}"
	[[ -z "${sums_by_name[$sum_name]:-}" ]] ||
		die "duplicate entry in SHA256SUMS.txt: $sum_name"
	sums_by_name[$sum_name]="$digest"
done < "$sums"

for name in "${assets[@]}"; do
	[[ "$name" == "SHA256SUMS.txt" ]] && continue
	[[ -n "${sums_by_name[$name]:-}" ]] ||
		die "SHA256SUMS.txt does not cover published asset: $name"
done

for name in "${!sums_by_name[@]}"; do
	[[ -n "${seen[$name]:-}" ]] ||
		die "SHA256SUMS.txt covers an asset that is not published: $name"
done

if command -v sha256sum >/dev/null 2>&1; then
	(cd "$dist" && sha256sum --check --strict SHA256SUMS.txt >/dev/null) ||
		die "checksum verification failed"
fi

printf 'release-assets: OK (%d assets)\n' "${#assets[@]}"
