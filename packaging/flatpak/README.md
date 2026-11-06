# Terminal-only Flatpak release recipes

Generate with `VERSION=v1.2.3 bash scripts/package-sandbox.sh` (or pass the version
as argument). Optional positional arguments: release binary directory, output
directory, and `generate|flatpak|snap`. The defaults are `dist`, `dist/sandbox`,
and `generate`. Both amd64/arm64 nyist-connect Linux binaries must already exist; recipes
pin the matching GitHub release URLs and SHA-256 computed from those files.
Publish those exact binaries under the matching tag before remote builds.
Semantic versions including prereleases are accepted; no `latest`, branches or placeholders.
The exact tag (including presence or absence of `v`) is preserved in source URLs.

`bash scripts/package-sandbox.sh test` runs offline static generation tests.
The recipes themselves are the portable source/build inputs, not installed apps.

## Build and run

Install flatpak-builder plus the matching org.freedesktop.Platform and SDK 25.08
runtime for your architecture from a trusted remote yourself. No command here
adds remotes, installs runtime packages, publishes, or escalates privilege.

```
bash scripts/package-sandbox.sh v1.2.3 dist dist/sandbox flatpak
flatpak install --user dist/sandbox/nyist-connect-1.2.3-x86_64.flatpak
flatpak run io.github.GUYU2233.AuthingNYIST --version
```

For arm64 the Flatpak filename architecture is `aarch64`. The explicit build
mode builds the host architecture only. It does not silently download SDKs.
There is no desktop launcher, GUI, display socket or GUI metadata.

## Important capability limits

Only network sharing is granted. Config stays in Flatpak's per-app user area;
there is no host home/system-bus/sysfs permission or host command escape.
Credentials remain plaintext local config: protect them, do not include them in
release assets, and avoid command-line passwords (visible to process inspection).

The current nyist-connect Linux application automatically inspects
`/sys/class/net`, then invokes `nmcli` on wireless machines. Even explicit
`--network nyist-stu`/`nyist-tec` does not skip EnsureNetwork on connection.
Flatpak cannot promise these host operations work. Auto detection, Wi-Fi
selection, host nmcli, systemd/autostart and system services are **unsupported**.
Do not use service menu entries or `--autostart`/`--system` in this package.
Network sharing does not provide hardware binding or bypass a host VPN/TUN.

Join campus Wi-Fi manually on the host before running nyist-connect.
The package contains no separate portal-only fallback client.
These are campus captive-portal authentication tools, not xdg-desktop-portal
networking integration. Configure credentials as supported by `--help`.
Connectivity still depends on campus routing and the host's VPN/TUN policy.
Use native distribution packages when host Wi-Fi or service integration matters.
