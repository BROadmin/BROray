"""S25 must not replay old preparation over an already installed exact platform.

Physical baseline: r12 --update-* -> c37, 1101 routes, S25 exits 1 because
the old preparing request belongs to r12. Run the real schedule entry with
exact platform files; no service or readiness shim supplies success.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(os.environ.get('BRORAY_TEST_ROOT', Path(__file__).resolve().parents[1]))
PAYLOAD = ROOT / 'runtime/app/share/updater-platform'
HANDOFF = ROOT / 'runtime/app/lib/universal-platform-handoff.sh'


class LegacySchedule(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory(prefix='legacy-schedule-')
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.app = self.root / 'opt/broray'
        current = self.app / 'current'
        current.mkdir(parents=True)
        (current / '.broray-slot').write_text('new--update-test\n')
        (current / 'release.json').write_text('{"candidateId":"new"}\n')
        self.payload = current / 'app/share/updater-platform'
        shutil.copytree(PAYLOAD, self.payload)
        self.files = [line.split()[1] for line in (PAYLOAD / 'SHA256SUMS').read_text().splitlines()]
        for name in self.files:
            source = self.payload / name
            source.chmod(0o755)
            live = self.root / name
            live.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, live)
        self.state = self.root / 'opt/var/lib/broray-platform-handoff'
        self.state.mkdir(parents=True)
        (self.state / 'phase').write_text('preparing\n')
        # This is deliberately bound to the old slot, as in the physical FAIL.
        request = dict(schemaVersion=1, contract='broray-universal-platform-handoff/1',
                       operationId='old-update', previousSlot='r11--direct-bootstrap',
                       targetSlot='r12--update-old', candidateId='r12',
                       payloadManifestSha256='c' * 64, createdAt='2026-09-30T07:37:27Z')
        (self.state / 'request.json').write_text(json.dumps(request))
        (self.state / 'status.json').write_text('{"code":"PLATFORM_DAEMON_STOP_FAILED"}\n')
        (self.state / 'platform-backup').mkdir()
        (self.state / 'platform-backup/inventory.tsv').write_text('historical backup evidence\n')
        self.env = {**os.environ, 'BRORAY_HANDOFF_ROOT_PREFIX': str(self.root),
                    'BRORAY_HANDOFF_ASH': '/bin/ash', 'BRORAY_HANDOFF_TEST_MODE': '1',
                    'BRORAY_HANDOFF_NO_ASYNC': '1'}

    def inventory(self):
        return {str(p.relative_to(self.root)): (p.read_bytes(), p.stat().st_mode & 0o777)
                for p in self.root.rglob('*') if p.is_file()}

    def run_schedule(self):
        before = self.inventory()
        p = subprocess.run(['/bin/ash', str(HANDOFF), 'schedule'], env=self.env,
                           capture_output=True, text=True, timeout=15)
        self.assertEqual(self.inventory(), before, 'schedule must preserve old evidence and platform bytes')
        return p

    def test_00_preparing_old_request_exact_current_platform_is_noop(self):
        p = self.run_schedule()
        self.assertEqual(p.returncode, 0, p.stdout + p.stderr)

    def test_mismatched_live_platform_still_refuses(self):
        (self.root / self.files[0]).write_text('different installed bytes\n')
        self.assertNotEqual(self.run_schedule().returncode, 0)

    def test_installing_is_not_superseded_by_matching_bytes(self):
        (self.state / 'phase').write_text('installing\n')
        self.assertNotEqual(self.run_schedule().returncode, 0)

    def test_restarting_is_not_superseded_by_matching_bytes(self):
        (self.state / 'phase').write_text('restarting\n')
        self.assertNotEqual(self.run_schedule().returncode, 0)

    def test_old_worker_fence_is_never_removed(self):
        lock = self.state / 'worker.lock'
        lock.mkdir()
        (lock / 'unknown-owner').write_bytes(b'preserve')
        p = self.run_schedule()
        self.assertEqual(p.returncode, 0, p.stdout + p.stderr)
        self.assertTrue(lock.is_dir())


if __name__ == '__main__':
    unittest.main(verbosity=2, failfast=True)
