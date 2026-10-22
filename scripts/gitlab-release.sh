#!/usr/bin/env bash
# Publish only packaged release assets. Requires curl + Python 3, never jq.
# The job artifacts are transport only: release links target the package registry.
set -euo pipefail
set +x
cd "$(dirname "$0")/.."
python3 - <<'PY'
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
from urllib.parse import quote, urlsplit


def fail(message):
    raise SystemExit(message)


def required(name):
    value = os.environ.get(name, "")
    if not value:
        fail("Missing required CI variable: " + name)
    return value


api = required("CI_API_V4_URL").rstrip("/")
project = required("CI_PROJECT_ID")
tag = required("CI_COMMIT_TAG")
token = required("CI_JOB_TOKEN")
if not tag.startswith("v"):
    fail("Only v* tags may publish releases")
if urlsplit(api).scheme not in ("http", "https") or urlsplit(api).username:
    fail("CI_API_V4_URL must be an HTTP(S) API URL without credentials")

# Keep this allowlist aligned with package-release.sh; never glob dist/*.
expected = {
    "nyist-connect.exe", "nyist-connect-linux-amd64", "nyist-connect-linux-arm64",
    "README.md", "NYIST-CONNECT.md", "LICENSE", "SHA256SUMS.txt",
}
expected.update("auth-nyist." + suffix for suffix in (
    "macos.x86_64", "macos.arm64", "win64.exe", "linux.x86_64", "linux.arm64",
    "linux.arm", "linux.armv5", "linux.armv6", "linux.mipsle", "linux.mipsbe",
    "linux.ppc64le", "linux.riscv64", "linux.loong64",
))
dist = Path("dist")
manifest = dist / "release-assets.txt"
if not manifest.is_file():
    fail("Missing dist/release-assets.txt; run package-release.sh first")
assets = manifest.read_text(encoding="utf-8").splitlines()
if len(assets) != len(set(assets)) or set(assets) != expected:
    fail("Release manifest must contain exactly the approved binaries, docs and checksums")
for name in assets:
    path = dist / name
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", name) or path.is_symlink() or not path.is_file() or not path.stat().st_size:
        fail("Missing, empty or unsafe release asset: " + name)


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest() if hasattr(hashlib, "file_digest") else hash_stream(stream)


def hash_stream(stream):
    hasher = hashlib.sha256()
    for chunk in iter(lambda: stream.read(1024 * 1024), b""):
        hasher.update(chunk)
    return hasher.hexdigest()


checksums = {}
for line in (dist / "SHA256SUMS.txt").read_text(encoding="utf-8").splitlines():
    match = re.fullmatch(r"([0-9a-fA-F]{64}) [ *]([A-Za-z0-9_.-]+)", line)
    if not match or match[2] in checksums:
        fail("Invalid or duplicate checksum entry")
    checksums[match[2]] = match[1].lower()
if set(checksums) != expected - {"SHA256SUMS.txt"}:
    fail("Checksum list must cover exactly the release binaries and docs")
for name, checksum in checksums.items():
    if digest(dist / name) != checksum:
        fail("Checksum mismatch: " + name)

encode = lambda value: quote(str(value), safe="")
base = api + "/projects/" + encode(project)
release_url = base + "/releases/" + encode(tag)
# Generic package versions have a stricter character set than Git tags.
# Hash the full tag so even tags with slashes/Unicode have a stable valid version.
package_version = "tag-" + hashlib.sha256(tag.encode("utf-8")).hexdigest()
package_url = base + "/packages/generic/authingnyist/" + encode(package_version)
links = [{"name": name, "url": package_url + "/" + encode(name), "link_type": "other"} for name in assets]

with tempfile.TemporaryDirectory(prefix="gitlab-release-") as directory:
    response = Path(directory) / "response"
    payload_file = Path(directory) / "payload.json"

    def request(method, url, payload=None, upload=None, allow_missing=False):
        command = ["curl", "--silent", "--show-error", "--fail-with-body",
                   "--connect-timeout", "30", "--max-time", "600",
                   "--request", method, "--header", "JOB-TOKEN: " + token,
                   "--output", str(response), "--write-out", "%{http_code}", url]
        if payload is not None:
            payload_file.write_text(json.dumps(payload), encoding="utf-8")
            command += ["--header", "Content-Type: application/json", "--data-binary", "@" + str(payload_file)]
        if upload is not None:
            command += ["--upload-file", str(upload)]
        # Never log the command, request headers, token or API error bodies.
        result = subprocess.run(command, capture_output=True, text=True)
        status = result.stdout.strip()
        if status == "404" and allow_missing and result.returncode in (0, 22):
            return False
        if result.returncode or not status.isdigit() or not 200 <= int(status) < 300:
            fail("GitLab API " + method + " failed (HTTP " + (status if status.isdigit() else "unknown") + ")")
        return True

    # Check existing immutable files before uploading: retries do not create
    # duplicate package versions and never silently replace different contents.
    for link in links:
        name = link["name"]
        if request("GET", link["url"], allow_missing=True):
            if digest(response) != digest(dist / name):
                fail("Package asset already exists with different contents: " + name)
        else:
            request("PUT", link["url"], upload=dist / name)

    description = "Tested AuthingNYIST binaries, documentation and SHA-256 checksums. Assets are stored in the persistent GitLab Generic Package Registry."
    if re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?", tag):
        notes = Path("releases") / (tag + ".md")
        if notes.is_file():
            description = notes.read_text(encoding="utf-8")
    if request("GET", release_url, allow_missing=True):
        release = json.loads(response.read_text(encoding="utf-8"))
        request("PUT", release_url, {"name": tag, "description": description})
        existing = {link["name"]: link for link in release.get("assets", {}).get("links", [])}
        for link in links:
            if link["name"] in existing:
                old = existing[link["name"]]
                request("PUT", release_url + "/assets/links/" + encode(old["id"]), link)
            else:
                request("POST", release_url + "/assets/links", link)
    else:
        request("POST", base + "/releases", {
            "tag_name": tag, "name": tag, "description": description,
            "assets": {"links": links},
        })
print("Published " + str(len(assets)) + " persistent release assets for " + tag)
PY
