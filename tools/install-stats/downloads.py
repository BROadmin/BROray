"""Normalized cumulative counters from an archive counter export."""

import re
from datetime import date

SCHEMA = """
CREATE TABLE IF NOT EXISTS download_snapshots (
    channel TEXT NOT NULL, asset TEXT NOT NULL, day TEXT NOT NULL,
    version TEXT NOT NULL, revision TEXT NOT NULL, architecture TEXT NOT NULL,
    total INTEGER, reset INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(channel, asset, day)
);
"""

FIELDS = {"channel", "asset", "day", "version", "revision", "architecture", "total"}


def validate_snapshots(snapshots, approved):
    if type(snapshots) is not list or len(snapshots) > 4096:
        raise ValueError("invalid archive snapshot batch")
    rows = []
    for snapshot in snapshots:
        if (type(snapshot) is not dict or snapshot.keys() != FIELDS
                or any(type(snapshot[field]) is not str for field in FIELDS - {"total"})):
            raise ValueError("invalid archive snapshot fields")
        channel, asset, day = (snapshot[field] for field in ("channel", "asset", "day"))
        build = tuple(snapshot[field] for field in ("version", "revision", "architecture"))
        total = snapshot["total"]
        if (channel not in {"GitHub", "Stable"} or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,95}", asset)
                or len(day) != 10 or date.fromisoformat(day).isoformat() != day or build not in approved
                or total is not None and (type(total) is not int or not 0 <= total <= 2**63 - 1)):
            raise ValueError("invalid archive snapshot")
        rows.append((channel, asset, day, *build, total))
    return rows


def record_snapshots(conn, rows):
    for channel, asset, day, version, revision, arch, total in rows:
        known = conn.execute("SELECT version, revision, architecture FROM download_snapshots "
                             "WHERE channel = ? AND asset = ? LIMIT 1", (channel, asset)).fetchone()
        if known is not None and tuple(known) != (version, revision, arch):
            raise ValueError("archive identity is already bound to another build")
        conn.execute("""
            INSERT INTO download_snapshots(channel, asset, day, version, revision, architecture, total)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(channel, asset, day) DO UPDATE SET
                reset = download_snapshots.reset OR COALESCE(excluded.total < download_snapshots.total, 0),
                total = excluded.total
            WHERE excluded.total IS NOT NULL
        """, (channel, asset, day, version, revision, arch, total))


def daily_report(conn):
    previous = {}
    report = []
    rows = conn.execute("SELECT * FROM download_snapshots ORDER BY day, channel, version, revision, architecture, asset")
    for row in rows:
        key = row["channel"], row["asset"]
        current = row["total"]
        status, count = "unavailable", None
        if current is not None:
            observed_day = date.fromisoformat(row["day"])
            earlier = previous.get(key)
            status = "baseline"
            if row["reset"] or earlier is not None and current < earlier[1]:
                status = "reset"
            elif earlier is not None:
                if (observed_day - earlier[0]).days == 1:
                    status, count = "measured", current - earlier[1]
                else:
                    status = "gap"
            previous[key] = observed_day, current
        report.append({**{field: row[field] for field in ("day", "channel", "version", "revision", "architecture", "asset")},
                       "status": status, "count": count})
    return report
