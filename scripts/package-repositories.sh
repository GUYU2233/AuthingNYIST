#!/usr/bin/env bash
# Build offline repository bundles from native packages, without publishing them.
set -euo pipefail
cd "$(dirname "$0")/.."

fail() { printf 'Repository packaging: %s\n' "$*" >&2; exit 1; }
[[ -n "${VERSION:-}" ]] || fail 'VERSION is required (for example VERSION=1.2.3).'
VERSION=${VERSION#v}
[[ "$VERSION" =~ ^[A-Za-z0-9][A-Za-z0-9.+_~-]*$ ]] || fail 'VERSION must be a safe filename component.'
for tool in apt-ftparchive dpkg-deb createrepo_c tar gzip mktemp cp; do
  command -v "$tool" >/dev/null 2>&1 || fail "Missing $tool. Install apt-utils and dpkg-dev (Debian/Ubuntu), createrepo-c (RPM metadata), and standard tar/gzip/coreutils before running this script."
done
if [[ -n "${REPOSITORY_SIGNING_KEY:-}" ]]; then
  command -v gpg >/dev/null 2>&1 || fail 'Missing gpg; install GnuPG or omit REPOSITORY_SIGNING_KEY for explicitly unsigned bundles.'
  gpg --batch --list-secret-keys "$REPOSITORY_SIGNING_KEY" >/dev/null 2>&1 || fail 'REPOSITORY_SIGNING_KEY must identify an available GPG secret signing key in GNUPGHOME.'
fi
shopt -s nullglob
debs=(dist/*.deb)
rpms=(dist/*.rpm)
((${#debs[@]})) || fail 'No dist/*.deb inputs. Run native package generation first.'
((${#rpms[@]})) || fail 'No dist/*.rpm inputs. Run native package generation first.'

stage=$(mktemp -d /tmp/authingnyist-repositories.XXXXXXXX)
# mktemp returns the absolute directory we own; guard cleanup against bad paths.
cleanup() {
  [[ "$stage" == /tmp/authingnyist-repositories.* && -d "$stage" && ! -L "$stage" ]] || return
  rm -rf -- "${stage:?}"
}
trap cleanup EXIT
apt_root="$stage/apt"
rpm_root="$stage/rpm"
mkdir -p "$apt_root/pool/main" "$apt_root/dists/stable/main" "$rpm_root/Packages"
for package in "${debs[@]}"; do
  arch=$(dpkg-deb --field "$package" Architecture)
  case "$arch" in amd64|arm64|all) ;; *) fail "Unsupported Debian architecture '$arch' in $package (expected amd64, arm64, or all)." ;; esac
  cp -- "$package" "$apt_root/pool/main/"
done
# Generate paths relative to the repository root. Filter by package metadata,
# never by filenames; Architecture: all belongs in both architecture indexes.
for arch in amd64 arm64; do
  index="$apt_root/dists/stable/main/binary-$arch"
  mkdir -p "$index"
  (cd "$apt_root"; apt-ftparchive --arch "$arch" packages pool/main) > "$index/Packages"
  gzip -n -9 -c "$index/Packages" > "$index/Packages.gz"
done
(
  cd "$apt_root"
  apt-ftparchive \
    -o APT::FTPArchive::Release::Origin=AuthingNYIST \
    -o APT::FTPArchive::Release::Label=AuthingNYIST \
    -o APT::FTPArchive::Release::Suite=stable \
    -o APT::FTPArchive::Release::Codename=stable \
    -o "APT::FTPArchive::Release::Version=$VERSION" \
    -o 'APT::FTPArchive::Release::Architectures=amd64 arm64' \
    -o APT::FTPArchive::Release::Components=main \
    -o 'APT::FTPArchive::Release::Description=AuthingNYIST native packages' \
    release dists/stable > "$stage/Release"
)
# Keep the output outside the scanned tree until checksums are complete.
mv -- "$stage/Release" "$apt_root/dists/stable/Release"
cp -- "${rpms[@]}" "$rpm_root/Packages/"
createrepo_c "$rpm_root"
if [[ -n "${REPOSITORY_SIGNING_KEY:-}" ]]; then
  gpg --batch --yes --local-user "$REPOSITORY_SIGNING_KEY" --armor --detach-sign \
    --output "$apt_root/dists/stable/Release.gpg" "$apt_root/dists/stable/Release"
  gpg --batch --yes --local-user "$REPOSITORY_SIGNING_KEY" --clearsign \
    --output "$apt_root/dists/stable/InRelease" "$apt_root/dists/stable/Release"
  gpg --batch --yes --local-user "$REPOSITORY_SIGNING_KEY" --armor --detach-sign \
    --output "$rpm_root/repodata/repomd.xml.asc" "$rpm_root/repodata/repomd.xml"
  gpg --batch --armor --export "$REPOSITORY_SIGNING_KEY" > "$stage/repository-signing-key.asc"
  cp -- "$stage/repository-signing-key.asc" "$apt_root/repository-signing-key.asc"
  cp -- "$stage/repository-signing-key.asc" "$rpm_root/repository-signing-key.asc"
  printf 'Metadata signed with %s. RPM package signatures are NOT created or changed.\n' "$REPOSITORY_SIGNING_KEY"
else
  printf 'WARNING: unsigned repositories; these bundles are NOT trusted by default. See packaging/repositories/README.md.\n' >&2
fi
apt_asset="authingnyist-apt-repository-$VERSION.tar.gz"
rpm_asset="authingnyist-rpm-repository-$VERSION.tar.gz"
tar -czf "$stage/$apt_asset" -C "$apt_root" .
tar -czf "$stage/$rpm_asset" -C "$rpm_root" .
# Publish only after both repository trees and archives have been generated.
cp -- "$stage/$apt_asset" "$stage/$rpm_asset" dist/
printf '%s\n' "$apt_asset" "$rpm_asset" > dist/repositories-assets.txt
printf 'Created dist/%s and dist/%s\n' "$apt_asset" "$rpm_asset"
