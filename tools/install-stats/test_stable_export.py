import io
import unittest
from datetime import date, datetime, timezone

from stable_export import build_export


class StableExportTest(unittest.TestCase):
    def setUp(self):
        self.day = date(2026, 11, 24)
        self.inventory = [
            {"version": "3.5.10", "revision": "r14", "architecture": "arm64",
             "name": "broray-3.5.10-aarch64-r14.tar.gz", "size": 1440, "sha256": "d" * 64,
             "githubId": None, "stableAsset": "r14-aarch64"},
            {"version": "3.5.10", "revision": "r14", "architecture": "arm/7",
             "name": "broray-3.5.10-armv7-r14.tar.gz", "size": 1510, "sha256": "e" * 64,
             "githubId": None, "stableAsset": "r14-armv7"},
        ]
        self.before = int(datetime(2026, 11, 24, 20, 59, tzinfo=timezone.utc).timestamp())
        self.after = int(datetime(2026, 11, 24, 21, 0, tzinfo=timezone.utc).timestamp())

    @staticmethod
    def lines(*entries):
        return io.BytesIO("".join(f"{ts}.000|{archive}|{size}\n" for ts, archive, size in entries).encode("ascii"))

    def test_cumulative_verified_sizes_and_moscow_day(self):
        data = build_export(self.inventory, self.day, [
            self.lines((self.before - 86400, "r14-aarch64", 1440), (self.before, "r14-aarch64", 1440)),
            self.lines((self.before, "r14-aarch64", 100), (self.after, "r14-aarch64", 1440)),
        ])
        self.assertEqual(data["day"], "2026-11-24")
        self.assertEqual([(row["asset"], row["total"]) for row in data["archives"]], [("r14-aarch64", 2), ("r14-armv7", 0)])
        self.assertEqual(data["archives"][0]["sha256"], "d" * 64)

    def test_unknown_raw_or_partial_record_rejected(self):
        for bad in (
            self.lines((self.before, "unmapped", 1440)),
            io.BytesIO(b"192.0.2.4 GET /download HTTP/1.0 200\n"),
            io.BytesIO(f"{self.before}.000|r14-aarch64|1440".encode()),
        ):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                build_export(self.inventory, self.day, [bad])


if __name__ == "__main__":
    unittest.main()
