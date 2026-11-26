"""Produce a Stable aggregate from a complete set of closed archive-only ledgers."""

import argparse
import json
import os
import re
import tempfile
from contextlib import ExitStack
from datetime import date, datetime
from pathlib import Path

from collect import read_json, validate_archives
from receiver import MOSCOW

RECORD = re.compile(rb"([0-9]{10})\.[0-9]{3}\|([A-Za-z0-9][A-Za-z0-9_.-]{0,30})\|([0-9]{1,12})\n")
MAX_LINE = 64


def build_export(archives, day, ledgers):
    selected = {entry["stableAsset"]: entry for entry in validate_archives(archives) if entry["stableAsset"] is not None}
    if not selected or not ledgers:
        raise ValueError("missing Stable inventory or ledger")
    totals = dict.fromkeys(selected, 0)
    for ledger in ledgers:
        while line := ledger.readline(MAX_LINE + 1):
            match = RECORD.fullmatch(line)
            if match is None:
                raise ValueError("invalid or partial counter record")
            observed, name, length = match.groups()
            name = name.decode("ascii")
            if name not in selected:
                raise ValueError("unmapped Stable archive instance")
            if int(length) != selected[name]["size"]:
                continue
            if datetime.fromtimestamp(int(observed), MOSCOW).date() <= day:
                totals[name] += 1
    return {
        "schema": 1, "day": day.isoformat(),
        "archives": [{"asset": name, "sha256": entry["sha256"], "size": entry["size"], "total": totals[name]}
                     for name, entry in selected.items()],
    }


def write_export(path, data):
    temp = None
    try:
        with tempfile.NamedTemporaryFile(dir=path.parent, prefix=".broray-counts-", delete=False, mode="w", encoding="utf-8") as stream:
            temp = Path(stream.name)
            json.dump(data, stream, ensure_ascii=True, separators=(",", ":"))
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temp, path)
    finally:
        if temp is not None:
            temp.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archives", required=True, type=Path)
    parser.add_argument("--ledger", action="append", required=True, type=Path)
    parser.add_argument("--day", required=True, type=date.fromisoformat)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    try:
        with ExitStack() as stack:
            ledgers = [stack.enter_context(path.open("rb")) for path in args.ledger]
            metadata = [os.fstat(ledger.fileno()) for ledger in ledgers]
            seen = {(entry.st_dev, entry.st_ino) for entry in metadata}
            if len(seen) != len(ledgers):
                raise ValueError("duplicate ledger")
            output = build_export(read_json(args.archives), args.day, ledgers)
            for ledger, before in zip(ledgers, metadata):
                after = os.fstat(ledger.fileno())
                if (before.st_size, before.st_mtime_ns) != (after.st_size, after.st_mtime_ns):
                    raise ValueError("ledger is not closed")
        write_export(args.output, output)
    except (ValueError, OSError, RecursionError) as error:
        parser.error("Stable ledger or export is unavailable or invalid (" + type(error).__name__ + ")")


if __name__ == "__main__":
    main()
