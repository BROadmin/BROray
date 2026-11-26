import json
import unittest
from datetime import date

from collect import GITHUB_REPOSITORY, collect_snapshots


class CollectorTest(unittest.TestCase):
    def setUp(self):
        self.day = date(2026, 11, 24)
        self.sha = "a" * 64
        self.archive = {
            "version": "3.5.10", "revision": "r14", "architecture": "arm64",
            "name": "broray-3.5.10-aarch64-r14.tar.gz", "size": 1540, "sha256": self.sha,
            "githubId": 81, "stableAsset": "stable-instance-1",
        }
        self.github = {
            "id": 81, "name": self.archive["name"], "size": self.archive["size"],
            "digest": "sha256:" + self.sha, "state": "uploaded", "download_count": 42,
        }
        self.stable = {
            "schema": 1, "day": str(self.day), "archives": [
                {"asset": "stable-instance-1", "sha256": self.sha, "size": 1540, "total": 8},
            ],
        }

    def test_channel_counters_are_bound_to_exact_archives(self):
        probes = []
        def fetch(url):
            probes.append(url)
            return json.dumps(self.github).encode()
        results = collect_snapshots([self.archive], self.day, self.stable, fetch=fetch)
        self.assertEqual(probes, [f"https://api.github.com/repos/{GITHUB_REPOSITORY}/releases/assets/81"])
        self.assertEqual([(r["channel"], r["total"]) for r in results], [("GitHub", 42), ("Stable", 8)])
        self.assertEqual([r["asset"] for r in results], ["github-81-" + self.sha, "stable-instance-1-" + self.sha])
        self.assertTrue(all(r["day"] == str(self.day) for r in results))

    def test_opaque_errors_or_foreign_counters_do_not_become_zero(self):
        for changed in (
            {"id": 82}, {"name": "another.tgz"}, {"digest": "sha256:" + "b" * 64},
            {"size": 99}, {"download_count": True}, {"download_count": -1}, {"state": "open"},
        ):
            with self.subTest(changed=changed):
                counters = collect_snapshots([self.archive], self.day, self.stable,
                    fetch=lambda _: json.dumps({**self.github, **changed}).encode())
                self.assertIsNone(counters[0]["total"])
                self.assertEqual(counters[1]["total"], 8)
        counters = collect_snapshots([self.archive], self.day, {**self.stable, "day": "2026-11-23"},
                                    fetch=lambda _: b"service error")
        self.assertTrue(all(r["total"] is None for r in counters))
        counters = collect_snapshots([self.archive], self.day, self.stable,
                                     fetch=lambda _: json.dumps({**self.github, "download_count": 0}).encode())
        self.assertEqual(counters[0]["total"], 0)

    def test_unexpected_source_export_or_map_is_rejected(self):
        entry = self.stable["archives"][0]
        for rows in ([{**entry, "ip": "192.0.2.88"}], [entry, entry], [{**entry, "size": 22}]):
            with self.subTest(rows=rows):
                counters = collect_snapshots([self.archive], self.day, {**self.stable, "archives": rows},
                                            fetch=lambda _: json.dumps(self.github).encode())
                self.assertIsNone(counters[1]["total"])
        with self.assertRaises(ValueError):
            collect_snapshots([{**self.archive, "url": "https://example.invalid/counter"}],
                              self.day, self.stable, fetch=lambda _: b"{}")

    def test_closed_stable_day_can_be_imported_after_midnight(self):
        counters = collect_snapshots([self.archive], date(2026, 11, 25), self.stable, stable_day=self.day,
                                     fetch=lambda _: json.dumps(self.github).encode())
        self.assertEqual([(row["channel"], row["day"], row["total"]) for row in counters],
                         [("GitHub", "2026-11-25", 42), ("Stable", "2026-11-24", 8)])


if __name__ == "__main__":
    unittest.main()
