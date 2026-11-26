#!/usr/bin/env python3
"""Create or update a GitHub Release and attach release assets (APK, bridge binaries, checksums).

Stdlib-only helper for environments without `gh` CLI. Reads GITHUB_PERSONAL_ACCESS_TOKEN.

    scripts/gh-release.py <version> <notes.md> <asset>... [--draft] [--repo owner/name]
"""

import json
import mimetypes
import os
import sys
import urllib.error
import urllib.request

API = "https://api.github.com"


class Refused(Exception):
    def __init__(self, code, detail):
        self.code = code
        self.detail = detail
        super().__init__(f"{code}: {detail}")


def call(url, token, data=None, content_type="application/json", method="POST", timeout=180):
    request = urllib.request.Request(url, data=data, method=method)
    request.add_header("Authorization", f"Bearer {token}")
    request.add_header("Accept", "application/vnd.github+json")
    request.add_header("Content-Type", content_type)
    request.add_header("User-Agent", "perch-release")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            body = response.read().decode("utf-8")
            return json.loads(body) if body.strip() else {}
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", "replace")[:500]
        raise Refused(exc.code, f"{exc.reason}: {detail}") from None


def existing_release(repo, tag, token):
    try:
        return call(f"{API}/repos/{repo}/releases/tags/{tag}", token, method="GET")
    except Refused as exc:
        if exc.code != 404:
            raise
    for release in call(f"{API}/repos/{repo}/releases?per_page=100", token, method="GET"):
        if isinstance(release, dict) and release.get("tag_name") == tag:
            return release
    return None


def content_type_for(name):
    if name.endswith(".apk"):
        return "application/vnd.android.package-archive"
    if name == "SHA256SUMS" or name.endswith(".txt"):
        return "text/plain; charset=utf-8"
    guessed, _ = mimetypes.guess_type(name)
    return guessed or "application/octet-stream"


def main(argv):
    args = list(argv[1:])
    repo = os.environ.get("PERCH_GITHUB_REPO", "th2zz/perch")
    if "--repo" in args:
        idx = args.index("--repo")
        if idx + 1 >= len(args):
            raise SystemExit(__doc__)
        repo = args[idx + 1]
        del args[idx : idx + 2]
    draft = "--draft" in args
    args = [a for a in args if a != "--draft"]
    if len(args) < 3:
        raise SystemExit(__doc__)

    version, notes_path = args[0], args[1]
    asset_paths = args[2:]

    token = os.environ.get("GITHUB_PERSONAL_ACCESS_TOKEN")
    if not token:
        raise SystemExit("no GITHUB_PERSONAL_ACCESS_TOKEN in the environment")

    with open(notes_path, encoding="utf-8") as handle:
        notes = handle.read().strip()

    assets = []
    for path in asset_paths:
        with open(path, "rb") as handle:
            assets.append((os.path.basename(path), handle.read()))

    tag = f"v{version}"
    release = existing_release(repo, tag, token)
    if release is None:
        release = call(
            f"{API}/repos/{repo}/releases",
            token,
            json.dumps({"tag_name": tag, "name": f"v{version}", "body": notes, "draft": draft}).encode("utf-8"),
        )
    else:
        release = call(
            f"{API}/repos/{repo}/releases/{release['id']}",
            token,
            json.dumps({"body": notes}).encode("utf-8"),
            method="PATCH",
        )

    existing_by_name = {a["name"]: a["id"] for a in release.get("assets", [])}
    for name, payload in assets:
        if name in existing_by_name:
            call(f"{API}/repos/{repo}/releases/assets/{existing_by_name[name]}", token, method="DELETE")
        call(
            f"https://uploads.github.com/repos/{repo}/releases/{release['id']}/assets?name={name}",
            token,
            payload,
            content_type=content_type_for(name),
        )

    if release.get("draft") and not draft:
        release = call(
            f"{API}/repos/{repo}/releases/{release['id']}",
            token,
            json.dumps({"draft": False}).encode("utf-8"),
            method="PATCH",
        )

    print(release["html_url"])


if __name__ == "__main__":
    main(sys.argv)
