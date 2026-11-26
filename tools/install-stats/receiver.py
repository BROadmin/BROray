"""Private storage and HTTP intake for anonymous BROray operation counters."""

import argparse
import hashlib
import json
import os
import re
import sqlite3
import time
from contextlib import closing
from datetime import datetime
from http import HTTPStatus
from pathlib import Path
from wsgiref.simple_server import WSGIRequestHandler, make_server
from zoneinfo import ZoneInfo

import downloads

EVENT_PATH = "/v1/install-events"
MAX_BODY = 1024
RECEIPT_WINDOW = 7 * 24 * 60 * 60
MOSCOW = ZoneInfo("Europe/Moscow")
ARCHITECTURES = {"arm64", "arm/7", "mips/softfloat", "mipsle/softfloat"}
VERSION = re.compile(r"[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}(?:-[0-9A-Za-z]+(?:\.[0-9A-Za-z]+)*)?")
REVISION = re.compile(r"[A-Za-z0-9]{1,12}(?:\.[A-Za-z0-9]{1,12}){0,2}")
EVENT_FIELDS = {
    "eventId", "type", "version", "revision", "architecture", "model", "deviceKey",
    "previousVersion", "previousRevision",
}
REQUIRED_FIELDS = {"eventId", "type", "version", "revision", "architecture"}

SCHEMA = """
CREATE TABLE IF NOT EXISTS receipts (
    event_id TEXT PRIMARY KEY, digest TEXT NOT NULL, expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS receipt_expiry ON receipts(expires_at);
CREATE TABLE IF NOT EXISTS routers (
    device_key TEXT PRIMARY KEY, first_day TEXT NOT NULL, model TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS operations (
    day TEXT NOT NULL, kind TEXT NOT NULL, version TEXT NOT NULL, revision TEXT NOT NULL,
    architecture TEXT NOT NULL, model TEXT NOT NULL, previous_version TEXT NOT NULL,
    previous_revision TEXT NOT NULL, total INTEGER NOT NULL, with_key INTEGER NOT NULL,
    PRIMARY KEY(day, kind, version, revision, architecture, model, previous_version, previous_revision)
);
CREATE TABLE IF NOT EXISTS anomalies (
    day TEXT NOT NULL, kind TEXT NOT NULL, total INTEGER NOT NULL, PRIMARY KEY(day, kind)
);
"""


def strict_object(pairs):
    fields = {}
    for name, value in pairs:
        if name in fields:
            raise ValueError("duplicate field")
        fields[name] = value
    return fields


def valid_build(version, revision, architecture):
    return (type(version) is str and len(version) <= 64 and VERSION.fullmatch(version)
            and type(revision) is str and REVISION.fullmatch(revision)
            and type(architecture) is str and architecture in ARCHITECTURES)


def parse_event(raw):
    if len(raw) > MAX_BODY:
        raise ValueError("large event")
    fields = json.loads(raw.decode("utf-8"), object_pairs_hook=strict_object)
    if (type(fields) is not dict or not REQUIRED_FIELDS <= fields.keys() or not fields.keys() <= EVENT_FIELDS
            or any(type(value) is not str or not value for value in fields.values())):
        raise ValueError("invalid event fields")
    if (not re.fullmatch(r"[a-f0-9]{32}", fields["eventId"])
            or "model" in fields and not re.fullmatch(r"(?:KN|NC)-[0-9]{4}", fields["model"])
            or "deviceKey" in fields and not re.fullmatch(r"[a-f0-9]{64}", fields["deviceKey"])):
        raise ValueError("invalid event identity")
    installed = (fields["version"], fields["revision"], fields["architecture"])
    if not valid_build(*installed):
        raise ValueError("invalid current build")
    previous = None
    if fields["type"] == "install":
        if "previousVersion" in fields or "previousRevision" in fields:
            raise ValueError("install has a predecessor")
    elif fields["type"] == "update":
        previous = (fields.get("previousVersion"), fields.get("previousRevision"), installed[2])
        if not valid_build(*previous) or previous == installed:
            raise ValueError("invalid previous build")
    else:
        raise ValueError("unknown event kind")
    return fields, installed, previous


class Store:
    def __init__(self, path, approved, *, clock=time.time):
        self.path = Path(path)
        self.approved = frozenset(approved)
        if not all(valid_build(*build) for build in self.approved):
            raise ValueError("invalid release allowlist")
        self.clock = clock
        with closing(self.connect()) as conn:
            conn.execute("PRAGMA journal_mode=WAL")
            conn.executescript(SCHEMA + downloads.SCHEMA)

    def connect(self):
        conn = sqlite3.connect(self.path, isolation_level=None, timeout=3)
        conn.row_factory = sqlite3.Row
        conn.execute("PRAGMA synchronous=FULL")
        return conn

    def accept(self, raw):
        try:
            event, installed, previous = parse_event(raw)
        except (ValueError, RecursionError):
            return 400, {"accepted": False}
        canonical = json.dumps(event, sort_keys=True, separators=(",", ":")).encode("ascii")
        digest = hashlib.sha256(canonical).hexdigest()
        now = int(self.clock())
        day = datetime.fromtimestamp(now, MOSCOW).date().isoformat()
        event_id = event["eventId"]
        try:
            with closing(self.connect()) as conn:
                conn.execute("BEGIN IMMEDIATE")
                conn.execute("DELETE FROM receipts WHERE expires_at <= ?", (now,))
                row = conn.execute("SELECT digest FROM receipts WHERE event_id = ?", (event_id,)).fetchone()
                if row is None:
                    conn.execute("INSERT INTO receipts VALUES (?, ?, ?)", (event_id, digest, now + RECEIPT_WINDOW))
                    self.count(conn, day, event, installed, previous)
                elif row["digest"] != digest:
                    self.anomaly(conn, day, "event_id_conflict")
                    conn.commit()
                    return 409, {"accepted": False}
                conn.commit()
        except sqlite3.Error:
            # Closing a connection with an active transaction rolls it back.
            return 503, {"accepted": False}
        return 200, {"eventId": event_id, "accepted": True}

    @staticmethod
    def anomaly(conn, day, kind):
        conn.execute("INSERT INTO anomalies VALUES (?, ?, 1) "
                     "ON CONFLICT(day, kind) DO UPDATE SET total = total + 1", (day, kind))

    def count(self, conn, day, event, installed, previous):
        model = event.get("model", "")
        key = event.get("deviceKey", "")
        conn.execute("INSERT INTO operations VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?) "
                     "ON CONFLICT DO UPDATE SET total = total + 1, with_key = with_key + excluded.with_key",
                     (day, event["type"], *installed, model, *(previous[:2] if previous else ("", "")), int(bool(key))))
        if not key:
            return
        known = conn.execute("SELECT model FROM routers WHERE device_key = ?", (key,)).fetchone()
        if known is None:
            conn.execute("INSERT INTO routers VALUES (?, ?, ?)", (key, day, model))
        elif model and not known["model"]:
            conn.execute("UPDATE routers SET model = ? WHERE device_key = ?", (model, key))
        elif model and model != known["model"]:
            self.anomaly(conn, day, "model_conflict")

    def report(self):
        now = int(self.clock())
        with closing(self.connect()) as conn:
            conn.execute("BEGIN IMMEDIATE")
            conn.execute("DELETE FROM receipts WHERE expires_at <= ?", (now,))
            operations = conn.execute("SELECT * FROM operations ORDER BY day, kind, version, revision, architecture, model")
            routers = conn.execute("SELECT first_day AS day, model, COUNT(*) AS total FROM routers "
                                   "GROUP BY first_day, model ORDER BY first_day, model")
            anomalies = conn.execute("SELECT * FROM anomalies ORDER BY day, kind")
            data = {"operations": [dict(row) for row in operations],
                    "routers": [dict(row) for row in routers], "anomalies": [dict(row) for row in anomalies]}
            counts = conn.execute("""
                WITH daily AS (SELECT day, SUM(total) AS total FROM operations GROUP BY day)
                SELECT day, 'daily_volume' AS kind, total FROM daily AS current
                 WHERE current.total >= 50 AND current.total > 2 * (
                     SELECT MAX(prior.total) FROM daily AS prior
                      WHERE prior.day < current.day AND prior.day >= date(current.day, '-7 days')
                 ) ORDER BY day
            """)
            data["anomalies"].extend(dict(row) for row in counts)
            data["downloads"] = downloads.daily_report(conn)
            conn.commit()
        return data

    def import_downloads(self, snapshots):
        rows = downloads.validate_snapshots(snapshots, self.approved)
        with closing(self.connect()) as conn:
            conn.execute("BEGIN IMMEDIATE")
            downloads.record_snapshots(conn, rows)
            conn.commit()


class App:
    def __init__(self, store):
        self.store = store

    def __call__(self, env, start_response):
        if env.get("PATH_INFO") != EVENT_PATH or env.get("QUERY_STRING"):
            result = 404, {"accepted": False}
        elif env.get("REQUEST_METHOD") != "POST":
            result = 405, {"accepted": False}
        else:
            result = self.post(env)
        status, document = result
        raw = json.dumps(document, separators=(",", ":")).encode("ascii")
        headers = [("Content-Type", "application/json"), ("Content-Length", str(len(raw))), ("Cache-Control", "no-store")]
        if status == 405:
            headers.append(("Allow", "POST"))
        start_response(f"{status} {HTTPStatus(status).phrase}", headers)
        return [raw]

    def post(self, env):
        if env.get("CONTENT_TYPE", "").partition(";")[0].strip().lower() != "application/json":
            return 415, {"accepted": False}
        length = env.get("CONTENT_LENGTH", "")
        if not re.fullmatch(r"[0-9]{1,8}", length):
            return 400, {"accepted": False}
        length = int(length)
        if length > MAX_BODY:
            return 413, {"accepted": False}
        try:
            raw = env["wsgi.input"].read(length)
        except OSError:
            return 400, {"accepted": False}
        if len(raw) != length:
            return 400, {"accepted": False}
        return self.store.accept(raw)


class QuietHandler(WSGIRequestHandler):
    def log_message(self, *_):
        pass


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--database", required=True, type=Path)
    commands = parser.add_subparsers(dest="command", required=True)
    serve = commands.add_parser("serve", help="listen on localhost for a private reverse-proxy upstream")
    serve.add_argument("--port", default=8091, type=int)
    serve.add_argument("--allowlist", type=Path, help=argparse.SUPPRESS)  # Legacy systemd units.
    commands.add_parser("report", help="output aggregate counters without device keys")
    ingest = commands.add_parser("import-downloads", help="load normalized cumulative archive snapshots")
    ingest.add_argument("--allowlist", required=True, type=Path, help="JSON array of [version, revision, architecture]")
    ingest.add_argument("--input", required=True, type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    if args.command == "report":
        if not args.database.is_file():
            parser.error("statistics database not found")
        print(json.dumps(Store(args.database, ()).report(), ensure_ascii=False, indent=2))
        return
    if args.command == "serve":
        app = App(Store(args.database, ()))
        with make_server("127.0.0.1", args.port, app, handler_class=QuietHandler) as server:
            server.serve_forever()
        return
    with args.allowlist.open(encoding="utf-8") as file:
        approved = json.load(file)
    if not isinstance(approved, list) or not approved or any(
        not isinstance(build, list) or len(build) != 3 or not valid_build(*build) for build in approved
    ):
        parser.error("invalid release allowlist")
    store = Store(args.database, map(tuple, approved))
    with args.input.open(encoding="utf-8") as file:
        store.import_downloads(json.load(file, object_pairs_hook=strict_object))


if __name__ == "__main__":
    main()
