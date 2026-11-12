# Native Linux release packaging

Build the Go release binaries first with the repository build scripts. Packaging never executes these binaries, authenticates, or installs/enables services.

```sh
VERSION=v1.2.3 bash scripts/package-linux.sh --output-dir dist
python3 packaging/linux/test_package.py
```

VERSION is mandatory SemVer (optional leading `v` normalized). Never use the validation version `0.0.0-dev` for production releases. The AUR recipe also needs the portable CLI archive from the same release; pass `--cli-archive PATH` or let it default to `dist/authingnyist-cli-extras-VERSION.tar.gz`. Default builds DEB, RPM, Arch and an AUR source tarball for **both** amd64/x86_64 and arm64/aarch64. Use repeatable `--format deb|rpm|arch|aur` and `--arch amd64|arm64` to select a subset. `--input-dir` defaults to `dist`; output defaults to `dist/linux`. `--repo owner/repository` and `--tag TAG` pin the release identity (default repository GUYU2233/AuthingNYIST, default tag vVERSION).

Dependencies: Bash, Python 3, `dpkg-deb` (Debian dpkg package), `rpmbuild` (RPM rpm-build package, Debian rpm), and `zstd`. Select only the formats whose tools are available. Arch archives use Python's standard tarfile module plus the zstd compressor; they contain real `.PKGINFO` including architecture, installed byte size, license, version and timestamp. No makepkg or Arch host is needed. RPM uses `--target` to package prebuilt binaries, not cross-compile; no host-architecture BuildArch gate is set. `SOURCE_DATE_EPOCH` controls Arch payload timestamps (default 0).

Each package includes `/usr/bin/nyist-connect`, GPL LICENSE and both project guides under `/usr/share`. No maintainer script, install hook or systemd unit is installed. Native static Go binary requirements are not inferred from the build host. Temporary staging trees are private and removed by Python; user-supplied input/output paths are not recursively deleted. Paths containing spaces are supported.

DEB uses `~` for SemVer prerelease ordering; RPM uses `~` plus release 1 (build metadata becomes the release suffix); Arch replaces SemVer `-` with `_` because package versions cannot contain a hyphen. Native package filenames use those normalized versions. `linux-assets.txt` explicitly lists generated package basenames only; it does not glob unrelated output files.

The AUR tarball contains `nyist-connect-bin/PKGBUILD` and `.SRCINFO`. Both support x86_64 and aarch64 and pin one immutable release asset, `authingnyist-cli-extras-VERSION.tar.gz`, with its real SHA-256. That archive carries both Linux binaries and the documentation under `cli/` and `docs/`. The release no longer publishes it, so a recipe generated here points at a URL that will not exist: only use the AUR recipe together with a self-hosted copy of that archive, or edit the source URL to your own mirror. This is an offline generator: it does not claim the URL already exists or fetch remote content. Never edit release assets after recipe generation. Raw PKGBUILD and .SRCINFO are also emitted for review. No placeholder checksums or SKIP entries are used.

Install the resulting packages through the native package manager:

```sh
sudo apt install ./nyist-connect_VERSION_amd64.deb  # or dpkg -i
sudo dnf install ./nyist-connect-VERSION-1.x86_64.rpm # yum also supported
sudo zypper install ./nyist-connect-VERSION-1.x86_64.rpm # or rpm -Uvh
sudo pacman -U ./nyist-connect-VERSION-1-x86_64.pkg.tar.zst
# On Arch, review the AUR recipe, then makepkg -si as an unprivileged user.
```

The tests use inert fixture bytes, build both architecture packages with available tools, inspect DEB/Arch metadata and payloads, ensure no install hooks, inspect RPM scripts, validate the generated PKGBUILD syntax and verify real checksums. They do not install packages or require network access.
