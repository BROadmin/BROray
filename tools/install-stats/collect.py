"""Collect daily aggregate counters for the allowlisted BROray release archives."""

import argparse
import concurrent.futures
import json
import re
import urllib.request
from datetime import date, datetime
from pathlib import Path

from receiver import MOSCOW, Store, strict_object, valid_build

GITHUB_REPOSITORY = "BROadmin/BROray"
MAX_METADATA = 64 * 1024
MAX_EXPORT_BYTES = 1024 * 1024
BUILD_FIELDS = ("version", "revision", "architecture")
ARCHIVE_FIELDS = {*BUILD_FIELDS, "name", "size", "sha256", "githubId", "stableAsset"}
DIGEST = re.compile(r"[a-f0-9]{64}")
ASSET = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,30}")
NAME = re.compile(r"broray-[A-Za-z0-9][A-Za-z0-9_.-]{0,85}\.tar\.gz")
GITHUB_ASSET_URL = re.compile(r"https://api\.github\.com/repos/BROadmin/BROray/releases/assets/[1-9][0-9]{0,18}")


def valid_count(count):
    return type(count) is int and 0 <= count <= 2**63 - 1


def valid_digest(value):
    return type(value) is str and DIGEST.fullmatch(value)


def validate_archives(archives):
    if type(archives) is not list or not 1 <= len(archives) <= 64:
        raise ValueError("invalid archive inventory")
    github_ids, stable_ids = set(), set()
    for entry in archives:
        if (type(entry) is not dict or entry.keys() != ARCHIVE_FIELDS
                or not valid_build(*(entry[field] for field in BUILD_FIELDS))
                or type(entry["name"]) is not str or not NAME.fullmatch(entry["name"])
                or not valid_count(entry["size"]) or not 0 < entry["size"] <= 128 * 1024 * 1024
                or not valid_digest(entry["sha256"])):
            raise ValueError("invalid archive identity")
        github, stable = entry["githubId"], entry["stableAsset"]
        if github is None and stable is None:
            raise ValueError("archive has no counter source")
        if github is not None:
            if not valid_count(github) or github == 0 or github in github_ids:
                raise ValueError("invalid GitHub asset identity")
            github_ids.add(github)
        if stable is not None:
            if type(stable) is not str or not ASSET.fullmatch(stable) or stable in stable_ids:
                raise ValueError("invalid Stable asset identity")
            stable_ids.add(stable)
    return archives


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def fetch_github(url):
    if not GITHUB_ASSET_URL.fullmatch(url):
        return None
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    request = urllib.request.Request(url, headers={
        "Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2026-03-10",
        "User-Agent": "BROray-release-counts/1",
    })
    try:
        with opener.open(request, timeout=5) as response:
            if response.status != 200 or response.headers.get_content_type() != "application/json":
                return None
            raw = response.read(MAX_METADATA + 1)
    except (OSError, ValueError):
        return None
    return raw if len(raw) <= MAX_METADATA else None


def github_total(entry, fetch):
    asset_id = entry["githubId"]
    url = f"https://api.github.com/repos/{GITHUB_REPOSITORY}/releases/assets/{asset_id}"
    raw = fetch(url)
    if type(raw) is not bytes or len(raw) > MAX_METADATA:
        return None
    try:
        asset = json.loads(raw.decode("utf-8"), object_pairs_hook=strict_object)
    except (ValueError, RecursionError):
        return None
    if (type(asset) is not dict or type(asset.get("id")) is not int or asset["id"] != asset_id
            or asset.get("name") != entry["name"] or asset.get("digest") != "sha256:" + entry["sha256"]
            or type(asset.get("size")) is not int or asset["size"] != entry["size"]
            or asset.get("state") != "uploaded" or not valid_count(asset.get("download_count"))):
        return None
    return asset["download_count"]


def stable_index(export, day):
    if (type(export) is not dict or export.keys() != {"schema", "day", "archives"}
            or type(export["schema"]) is not int or export["schema"] != 1 or export["day"] != day
            or type(export["archives"]) is not list or len(export["archives"]) > 64):
        return {}
    index = {}
    for row in export["archives"]:
        if (type(row) is not dict or row.keys() != {"asset", "sha256", "size", "total"}
                or type(row["asset"]) is not str or not ASSET.fullmatch(row["asset"]) or row["asset"] in index
                or not valid_digest(row["sha256"]) or not valid_count(row["size"]) or not valid_count(row["total"])):
            return {}
        index[row["asset"]] = row
    return index


def collect_snapshots(archives, observed, stable_export, *, stable_day=None, fetch=fetch_github):
    archives = validate_archives(archives)
    day = observed.isoformat()
    stable_day = (stable_day or observed).isoformat()
    stable = stable_index(stable_export, stable_day)
    snapshots = []
    for entry in archives:
        common = {field: entry[field] for field in BUILD_FIELDS}
        common["day"] = day
        if entry["githubId"] is not None:
            snapshots.append({**common, "channel": "GitHub", "asset": f"github-{entry['githubId']}-{entry['sha256']}",
                              "total": github_total(entry, fetch)})
        if entry["stableAsset"] is not None:
            stable_id = entry["stableAsset"]
            match = stable.get(stable_id)
            total = None
            if match is not None and match["sha256"] == entry["sha256"] and match["size"] == entry["size"]:
                total = match["total"]
            snapshots.append({**common, "day": stable_day, "channel": "Stable",
                              "asset": f"{stable_id}-{entry['sha256']}", "total": total})
    return snapshots


def read_json(path):
    with path.open("rb") as stream:
        raw = stream.read(MAX_EXPORT_BYTES + 1)
    if len(raw) > MAX_EXPORT_BYTES:
        raise ValueError("aggregate input too large")
    return json.loads(raw.decode("utf-8"), object_pairs_hook=strict_object)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--database", required=True, type=Path)
    parser.add_argument("--archives", required=True, type=Path)
    parser.add_argument("--stable-export", type=Path)
    parser.add_argument("--stable-day", type=date.fromisoformat, help="Moscow date of a closed Stable ledger")
    args = parser.parse_args()
    try:
        archive_map = validate_archives(read_json(args.archives))
    except (ValueError, OSError, RecursionError):
        parser.error("archive inventory is unavailable or invalid")
    archive_day = datetime.now(MOSCOW).date()
    if args.stable_day is not None and (not args.stable_export or args.stable_day > archive_day):
        parser.error("Stable day needs a corresponding export and cannot be in the future")
    try:
        stable = read_json(args.stable_export) if args.stable_export else None
    except (ValueError, OSError, RecursionError):
        stable = None
    github_urls = [f"https://api.github.com/repos/{GITHUB_REPOSITORY}/releases/assets/{row['githubId']}"
                   for row in archive_map if row["githubId"] is not None]
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as workers:
        observed = dict(zip(github_urls, workers.map(fetch_github, github_urls)))
    if datetime.now(MOSCOW).date() != archive_day:
        parser.error("collection crossed Moscow midnight; repeat on one reporting day")
    snapshots = collect_snapshots(archive_map, archive_day, stable, stable_day=args.stable_day, fetch=observed.get)
    store = Store(args.database, {tuple(entry[field] for field in BUILD_FIELDS) for entry in archive_map})
    store.import_downloads(snapshots)
    print(json.dumps({
        channel: sum(s["total"] is not None for s in snapshots if s["channel"] == channel)
        for channel in ("GitHub", "Stable")
    }))


if __name__ == "__main__":
    main()
