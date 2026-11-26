import concurrent.futures
import http.client
import io
import json
import sqlite3
import tempfile
import threading
import unittest
from contextlib import closing, redirect_stderr
from datetime import datetime, timezone
from pathlib import Path
from wsgiref.simple_server import make_server

from receiver import App, QuietHandler, RECEIPT_WINDOW, Store


class ReceiverTest(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.path = Path(self.folder.name) / "metrics.sqlite3"
        self.now = int(datetime(2026, 11, 24, 20, 59, tzinfo=timezone.utc).timestamp())
        self.builds = {("3.5.10", "r14", "arm64"), ("3.5.9", "r9", "arm64")}
        self.store = Store(self.path, self.builds, clock=lambda: self.now)
        self.event = {
            "eventId": "a" * 32, "type": "install", "version": "3.5.10", "revision": "r14",
            "architecture": "arm64", "model": "KN-2710", "deviceKey": "b" * 64,
        }

    def accept(self, change=None):
        event = dict(self.event)
        event.update(change or {})
        return self.store.accept(json.dumps(event).encode("utf-8"))

    def test_atomic_duplicates_and_moscow_dates(self):
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
            results = list(executor.map(lambda _: self.accept(), range(20)))
        self.assertTrue(all(result == (200, {"eventId": "a" * 32, "accepted": True}) for result in results))
        self.assertEqual(self.accept({"model": "KN-2810"})[0], 409)
        self.now += 120
        self.assertEqual(self.accept({
            "eventId": "c" * 32, "type": "update", "previousVersion": "3.5.9", "previousRevision": "r9",
        })[0], 200)
        report = self.store.report()
        self.assertEqual([(r["day"], r["kind"], r["total"], r["with_key"]) for r in report["operations"]],
                         [("2026-11-24", "install", 1, 1), ("2026-11-25", "update", 1, 1)])
        self.assertEqual(report["routers"], [{"day": "2026-11-24", "model": "KN-2710", "total": 1}])
        self.assertNotIn("b" * 64, json.dumps(report))

    def test_validation_and_model_reconciliation(self):
        self.assertEqual(self.accept({"serial": "PRIVATE-SYNTHETIC"})[0], 400)
        self.assertEqual(self.accept({"revision": "invalid revision"})[0], 400)
        self.assertEqual(self.accept({"architecture": "x86"})[0], 400)
        self.assertEqual(self.accept({
            "type": "update", "previousVersion": "3.5.10", "previousRevision": "r14",
        })[0], 400)
        self.assertEqual(self.store.accept(b'{"eventId":"x","eventId":"y"}')[0], 400)
        first = dict(self.event)
        del first["model"]
        self.assertEqual(self.store.accept(json.dumps(first).encode())[0], 200)
        self.assertEqual(self.accept({"eventId": "c" * 32})[0], 200)
        self.assertEqual(self.accept({"eventId": "d" * 32, "model": "KN-2810"})[0], 200)
        report = self.store.report()
        self.assertEqual(report["routers"], [{"day": "2026-11-24", "model": "KN-2710", "total": 1}])
        self.assertTrue(any(r["kind"] == "model_conflict" and r["total"] == 1 for r in report["anomalies"]))

    def test_failed_transaction_ack_only_after_commit(self):
        with closing(sqlite3.connect(self.path)) as conn:
            conn.execute("CREATE TRIGGER unavailable BEFORE INSERT ON operations "
                         "BEGIN SELECT RAISE(ABORT, 'fixture unavailable'); END")
            conn.commit()
        self.assertEqual(self.accept()[0], 503)
        with closing(sqlite3.connect(self.path)) as conn:
            self.assertEqual(conn.execute("SELECT COUNT(*) FROM receipts").fetchone()[0], 0)
            self.assertEqual(conn.execute("SELECT COUNT(*) FROM routers").fetchone()[0], 0)
            conn.execute("DROP TRIGGER unavailable")
            conn.commit()
        self.assertEqual(self.accept()[0], 200)
        self.assertEqual(self.store.report()["operations"][0]["total"], 1)

    def test_netcraze_update_fills_existing_router_model_once(self):
        self.store = Store(self.path, self.builds | {("3.5.10", "r1", "arm64"), ("3.5.10", "r2", "arm64")}, clock=lambda: self.now)
        baseline = {key: value for key, value in self.event.items() if key != "model"}
        baseline.update(type="update", revision="r1", previousVersion="3.5.9", previousRevision="r9")
        self.assertEqual(self.store.accept(json.dumps(baseline).encode())[0], 200)
        expected = {"eventId": "c" * 32, "type": "update", "revision": "r2", "previousVersion": "3.5.10",
                    "previousRevision": "r1", "model": "NC-1812"}
        for model in ("NC-18120", "NC-１８１２", "Ultra (NC-1812)", "XX-1812"):
            self.assertEqual(self.accept({**expected, "model": model})[0], 400)
        self.assertEqual(self.accept(expected)[0], 200)
        self.assertEqual(self.accept(expected)[0], 200)
        report = self.store.report()
        self.assertEqual(report["routers"], [{"day": "2026-11-24", "model": "NC-1812", "total": 1}])
        self.assertEqual([(row["revision"], row["model"], row["total"]) for row in report["operations"]],
                         [("r1", "", 1), ("r2", "NC-1812", 1)])

    def test_http_is_bounded_and_has_no_public_report(self):
        app = App(self.store)
        body = json.dumps(self.event).encode()
        def request(path, payload, length=None):
            observed = []
            env = {"REQUEST_METHOD": "POST", "PATH_INFO": path, "CONTENT_TYPE": "application/json",
                   "CONTENT_LENGTH": str(length if length is not None else len(payload)), "wsgi.input": io.BytesIO(payload),
                   "REMOTE_ADDR": "192.0.2.250"}
            data = b"".join(app(env, lambda status, headers: observed.append(status)))
            return observed[0], json.loads(data)
        self.assertEqual(request("/report", body)[0], "404 Not Found")
        self.assertTrue(request("/v1/install-events", body, 1025)[0].startswith("413 "))
        self.assertEqual(request("/v1/install-events", body), ("200 OK", {"eventId": "a" * 32, "accepted": True}))
        self.assertNotIn("192.0.2.250", self.path.read_bytes().decode("latin1"))

    def test_http_ack_and_retention_without_access_logs(self):
        output = io.StringIO()
        with redirect_stderr(output), make_server("127.0.0.1", 0, App(self.store), handler_class=QuietHandler) as server:
            worker = threading.Thread(target=server.serve_forever)
            worker.start()
            try:
                for payload, code in ((self.event, 200), ({**self.event, "serial": "SYNTHETIC-RAW-IDENTIFIER"}, 400)):
                    with closing(http.client.HTTPConnection("127.0.0.1", server.server_port, timeout=3)) as client:
                        client.request("POST", "/v1/install-events", json.dumps(payload), {
                            "Content-Type": "application/json", "X-Forwarded-For": "192.0.2.18",
                        })
                        response = client.getresponse()
                        self.assertEqual(response.status, code)
                        if code == 200:
                            self.assertEqual(json.loads(response.read()), {"eventId": "a" * 32, "accepted": True})
            finally:
                server.shutdown()
                worker.join()
        self.assertEqual(output.getvalue(), "")
        self.now += RECEIPT_WINDOW - 1
        self.assertEqual(self.accept()[0], 200)
        self.now += 2
        self.assertEqual(self.store.report()["operations"][0]["total"], 1)
        with closing(sqlite3.connect(self.path)) as conn:
            self.assertEqual(conn.execute("SELECT COUNT(*) FROM receipts").fetchone()[0], 0)
            self.assertEqual(conn.execute("SELECT COUNT(*) FROM routers").fetchone()[0], 1)

    def test_download_snapshots_distinguish_repeat_gap_zero_and_reset(self):
        def snapshot(channel, day, total, asset="archive-14"):
            return {"channel": channel, "day": day, "asset": asset, "total": total,
                    "version": "3.5.10", "revision": "r14", "architecture": "arm64"}
        self.store.import_downloads([
            snapshot("GitHub", "2026-11-23", 100), snapshot("GitHub", "2026-11-24", 106),
            snapshot("GitHub", "2026-11-24", 106), snapshot("Stable", "2026-11-23", 12),
            snapshot("Stable", "2026-11-24", 12), snapshot("GitHub", "2026-11-25", None),
            snapshot("GitHub", "2026-11-26", 128), snapshot("GitHub", "2026-11-27", 8),
            snapshot("GitHub", "2026-11-28", 4, "reissued14"),
        ])
        rows = self.store.report()["downloads"]
        observed = {(row["channel"], row["day"]): (row["status"], row["count"]) for row in rows}
        self.assertEqual(observed["GitHub", "2026-11-24"], ("measured", 6))
        self.assertEqual(observed["Stable", "2026-11-24"], ("measured", 0))
        self.assertEqual(observed["GitHub", "2026-11-25"], ("unavailable", None))
        self.assertEqual(observed["GitHub", "2026-11-26"], ("gap", None))
        self.assertEqual(observed["GitHub", "2026-11-27"], ("reset", None))
        self.assertEqual(observed["GitHub", "2026-11-28"], ("baseline", None))
        self.store.import_downloads([snapshot("GitHub", "2026-11-25", 120)])
        rows = self.store.report()["downloads"]
        recovered = {row["day"]: row["count"] for row in rows if row["channel"] == "GitHub"}
        self.assertEqual(recovered["2026-11-26"], 8)
        with self.assertRaises(ValueError):
            self.store.import_downloads([snapshot("Stable", "2026-11-25", 14),
                                         {**snapshot("Stable", "2026-11-26", 15), "ip": "192.0.2.18"}])
        self.assertEqual(self.store.report()["downloads"], rows)
    def test_operations_accept_releases_outside_download_inventory(self):
        original = {"version": "4.0.1", "revision": "r21", "architecture": "mips/softfloat"}
        update = {"eventId": "c" * 32, "type": "update", "version": "4.0.2", "revision": "r1",
                  "previousVersion": original["version"], "previousRevision": original["revision"],
                  "architecture": original["architecture"]}
        self.assertEqual(self.accept(original), (200, {"eventId": "a" * 32, "accepted": True}))
        self.assertEqual(self.accept(update), (200, {"eventId": "c" * 32, "accepted": True}))
        self.assertEqual(self.accept(update), (200, {"eventId": "c" * 32, "accepted": True}))
        self.assertEqual(self.accept({**update, "revision": "r2"})[0], 409)
        report = self.store.report()
        self.assertEqual(sum(row["total"] for row in report["operations"]), 2)
        self.assertEqual(sum(row["total"] for row in report["routers"]), 1)


if __name__ == "__main__":
    unittest.main()
