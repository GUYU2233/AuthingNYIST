# Snap release recipes (classic confinement)

Generate both `dist/sandbox/snap-amd64/snap/snapcraft.yaml` and the arm64
counterpart using `VERSION=v1.2.3 bash scripts/package-sandbox.sh` after creating
release binaries. Both CLI commands are shipped; no daemon, install hook,
auto-start, interface auto-connect or automatic service installation is defined.
URLs and SHA-256 are pinned to that release and computed from local dist assets.
The source-checksum field verifies the downloaded binary before staging it.

We choose **classic** because nyist-connect uses host sysfs, NetworkManager's
nmcli, configuration credentials, and optional explicit host systemd operations.
Classic grants broad host access and is not a security sandbox. Strict mode
would need redesigned host integration and explicit reviewed interfaces; simply
adding `network` would not make existing host nmcli/systemd integration work.
Even classic does not bundle or guarantee host nmcli/systemctl availability.
Credentials are local plaintext: protect files and avoid command-line passwords.

Classic distribution through the Snap Store **requires explicit store approval**
for classic confinement and potentially name reservation; these recipes do not
imply approval, and nothing uploads or publishes. Review this broad trust choice
before installation. Native packages may be preferable for host service support.

Install Snapcraft and a supported build provider separately. Build for the host
amd64 or arm64 architecture explicitly:

```
bash scripts/package-sandbox.sh v1.2.3 dist dist/sandbox snap
# Equivalent: cd dist/sandbox/snap-amd64 && snapcraft --output ../nyist-connect-1.2.3-amd64.snap
sudo snap install --dangerous --classic dist/sandbox/nyist-connect-1.2.3-amd64.snap
nyist-connect --version
nyist-connect.auth-nyist --help
```

Local unasserted installation requires the shown `--dangerous` acknowledgement;
installation is not performed by our script. User-requested host service commands
are not Snap-managed services; no service is enabled by packaging. Classic Snap
is not a TUN/VPN or hardware-binding bypass. Join campus networking first; classic
portal-only status/auth can use explicit `--network nyist-stu` or `nyist-tec`.
`bash scripts/package-sandbox.sh test` validates generation without Snapcraft.
The nil-plugin/file-source recipe uses common Snapcraft source properties;
Snapcraft/schema validation and actual core24 provider builds were not available
in the implementation environment. Treat generated YAML as unverified until an
explicit Snapcraft build succeeds; static tests do not establish installability.
