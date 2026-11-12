#!/usr/bin/env bash
#
# Offline test for scripts/check-release-assets.sh. Builds a fake release
# bundle that satisfies the allowlist + updater contract, then mutates it in
# ways that must be rejected.

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
checker="$root/scripts/check-release-assets.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

version="1.2.3"
dist="$tmp/dist"
mkdir -p "$dist"

fixture_assets=(
	"nyist-connect.exe"
	"NYIST-Connect-macos-amd64.dmg"
	"NYIST-Connect-macos-arm64.dmg"
	"nyist-connect_${version}_amd64.deb"
	"nyist-connect_${version}_arm64.deb"
	"nyist-connect-${version}-1.x86_64.rpm"
	"nyist-connect-${version}-1.aarch64.rpm"
)

reset_bundle() {
	rm -rf "$dist"
	mkdir -p "$dist"
	local name
	for name in "${fixture_assets[@]}"; do
		printf 'payload for %s\n' "$name" > "$dist/$name"
	done
	(
		cd "$dist"
		sha256sum "${fixture_assets[@]}" > SHA256SUMS.txt
		printf '%s\n' "${fixture_assets[@]}" SHA256SUMS.txt > release-assets.txt
	)
}

expect_ok() {
	local label="$1"
	bash "$checker" "$dist" >/dev/null ||
		{ printf 'FAIL (expected OK): %s\n' "$label" >&2; exit 1; }
	printf 'ok: %s\n' "$label"
}

expect_fail() {
	local label="$1"
	if bash "$checker" "$dist" >/dev/null 2>&1; then
		printf 'FAIL (expected rejection): %s\n' "$label" >&2
		exit 1
	fi
	printf 'ok: %s rejected\n' "$label"
}

reset_bundle
expect_ok "valid bundle"

reset_bundle
printf 'stray-build.log\n' >> "$dist/release-assets.txt"
expect_fail "non-allowlisted asset"

reset_bundle
printf 'auth-nyist_1.0_amd64.deb\n' >> "$dist/release-assets.txt"
expect_fail "legacy asset name"

reset_bundle
printf 'nyist-connect.exe\n' >> "$dist/release-assets.txt"
expect_fail "duplicate asset name"

reset_bundle
rm "$dist/nyist-connect.exe"
expect_fail "missing Windows updater asset"

reset_bundle
rm "$dist/SHA256SUMS.txt"
expect_fail "missing SHA256SUMS.txt"

reset_bundle
rm "$dist/nyist-connect_${version}_arm64.deb"
expect_fail "published asset without file"

reset_bundle
printf 'authingnyist-cli-extras-%s.tar.gz\n' "$version" >> "$dist/release-assets.txt"
expect_fail "extras archive no longer published"

reset_bundle
ln -s "nyist-connect.exe" "$dist/evil-link"
printf 'evil-link\n' >> "$dist/release-assets.txt"
expect_fail "symlink asset"

reset_bundle
printf 'not-a-checksum  nyist-connect.exe\n' >> "$dist/SHA256SUMS.txt"
expect_fail "malformed checksum line"

reset_bundle
grep -v "NYIST-Connect-macos-arm64.dmg" "$dist/SHA256SUMS.txt" > "$dist/SHA256SUMS.new"
mv "$dist/SHA256SUMS.new" "$dist/SHA256SUMS.txt"
expect_fail "checksum coverage gap"

reset_bundle
printf '%064d  stray.txt\n' 0 >> "$dist/SHA256SUMS.txt"
expect_fail "checksum for unpublished asset"

reset_bundle
printf '# tampered\n' >> "$dist/nyist-connect.exe"
expect_fail "digest mismatch"

# A pre-release deb name with a Debian '~' is legitimate and must stay allowed.
reset_bundle
rm "$dist/nyist-connect_${version}_amd64.deb"
prerelease_deb="nyist-connect_${version}~rc.1_amd64.deb"
printf 'payload for pre-release\n' > "$dist/$prerelease_deb"
prerelease_assets=()
for name in "${fixture_assets[@]}"; do
	if [[ "$name" == "nyist-connect_${version}_amd64.deb" ]]; then
		prerelease_assets+=("$prerelease_deb")
	else
		prerelease_assets+=("$name")
	fi
done
(
	cd "$dist"
	sha256sum "${prerelease_assets[@]}" > SHA256SUMS.txt
	printf '%s\n' "${prerelease_assets[@]}" SHA256SUMS.txt > release-assets.txt
)
expect_ok "pre-release punctuation"

printf 'release-asset tests: OK\n'
