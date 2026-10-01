"""Real lock callers + native coordinator; isolated Linux, no router access."""
import json
import os
from pathlib import Path
import shutil
import select
import subprocess
import tempfile
import unittest

APP = Path(__file__).resolve().parents[1] / 'runtime/app'
GUARD = Path(__file__).resolve().parents[2] / '.local/bin/linux-guard'


class GlobalAdmissionPublication(unittest.TestCase):
    def setUp(self):
        self.root = Path(tempfile.mkdtemp(prefix='global-publication-'))
        self.state = self.root / 'state'
        self.lock = self.root / 'global.lock'
        self.env = dict(os.environ, BRORAY_ROOT=str(APP), BRORAY_BASE=str(self.root / 'app'),
            BRORAY_STATE_ROOT=str(self.state), BRORAY_ROUTES_API_LOCK=str(self.lock),
            BRORAY_CLEANUP_GLOBAL_LOCK=str(self.lock), BRORAY_OPS_TEST='1',
            BRORAY_OPS_GUARD=str(GUARD), BRORAY_OPS_ASH='/bin/busybox',
            BRORAY_OPS_PROC_ROOT='/proc', BRORAY_OPS_UPDATER_ROOT=str(self.root / 'updater'),
            BRORAY_UPDATER_REQUEST_LOCK=str(self.root / 'updater/request.lock'),
            BRORAY_CLEANUP_UPDATER_LOCK=str(self.root / 'updater/request.lock'),
            BRORAY_LEGACY_GLOBAL_LOCK=str(self.root / 'legacy.lock'),
            BRORAY_CLEANUP_SYSTEM_LOCK=str(self.root / 'legacy.lock'),
            BRORAY_OPS_RAM_ROOT=str(self.root / 'ram'),
            BRORAY_ROUTES_API_PROGRESS_DIR=str(self.root / 'routes'),
            TEST_ROOT=str(self.root))
        self.env.pop('BRORAY_OPS_TEST_IDENTITIES', None)

    def tearDown(self):
        shutil.rmtree(self.root)

    def run_shell(self, script, expected=0):
        p = subprocess.run(['/bin/busybox', 'ash', '-c', script], env=self.env,
                           capture_output=True, timeout=30)
        self.assertEqual(p.returncode, expected, (p.stdout, p.stderr))
        return p

    def caller(self, kind):
        if kind == 'routes':
            return ('. "$BRORAY_ROOT/lib/routes-api-operation.sh"\n',
                    "broray_routes_api_lock_acquire servers:quality-refresh-save servers",
                    'broray_routes_api_lock_release')
        return ('. "$BRORAY_ROOT/lib/broray-cleanup.sh"\n',
                'broray_cleanup_global_lock_acquire', 'broray_cleanup_global_lock_release')

    def normal(self, kind):
        source, acquire, release = self.caller(kind)
        self.run_shell(source + acquire + ''' || exit 91
test -L "$BRORAY_ROUTES_API_LOCK" || exit 92
test -s "$BRORAY_ROUTES_API_LOCK/owner.json" || exit 93
cp "$BRORAY_ROUTES_API_LOCK/owner.json" "$TEST_ROOT/observed-owner.json"
''' + release + ''' || exit 94
test ! -e "$BRORAY_ROUTES_API_LOCK" && test ! -L "$BRORAY_ROUTES_API_LOCK" || exit 95
''' + acquire + ' || exit 96\n' + release + ' || exit 97\n')
        owner = json.loads((self.root / 'observed-owner.json').read_bytes())['owner']
        self.assertEqual(owner['bootId'], Path('/proc/sys/kernel/random/boot_id').read_text().strip())
        self.assertTrue(owner['startTicks'].isdigit())
        self.assertEqual(len(owner['commandDigest']), 64)

    def test_routes_complete_publication_release_and_next_operation(self):
        self.normal('routes')

    def test_cleanup_complete_publication_release_and_next_operation(self):
        self.normal('cleanup')

    def test_legacy_empty_directory_remains_untouched(self):
        self.lock.mkdir()
        identity = self.lock.stat().st_ino
        for kind in ('routes', 'cleanup'):
            source, acquire, _ = self.caller(kind)
            self.run_shell(source + acquire, 2)
            self.assertEqual(self.lock.stat().st_ino, identity)
            self.assertEqual(list(self.lock.iterdir()), [])

    def test_interrupted_private_publication_never_exposes_ownerless_fence(self):
        self.env['BRORAY_OPS_TEST_LAUNCH_CRASH'] = 'directory'
        for kind in ('routes', 'cleanup'):
            source, acquire, _ = self.caller(kind)
            p = subprocess.run(['/bin/busybox', 'ash', '-c', source + acquire],
                               env=self.env, capture_output=True, timeout=30)
            self.assertNotEqual(p.returncode, 0, (kind, p.stdout, p.stderr))
            self.assertFalse(self.lock.exists())
            self.assertFalse(self.lock.is_symlink())

    def test_api_error_is_not_recorded_as_completed(self):
        source, acquire, _ = self.caller('routes')
        self.run_shell(source + acquire + ''' || exit 91
. "$BRORAY_ROOT/web-new/api/auth-common.sh"
broray_api_error '400 Bad Request' INVALID_INPUT 'Rejected input'
''')
        states = list((self.state / 'operations').glob('op-*/state.json'))
        self.assertEqual(len(states), 1)
        self.assertEqual(json.loads(states[0].read_bytes())['state'], 'failed')

    def test_unconfirmed_release_cannot_return_success(self):
        source, acquire, _ = self.caller('routes')
        p = self.run_shell(source + acquire + ''' || exit 91
printf '{broken' >"$BRORAY_ROUTES_API_LOCK/owner.json"
. "$BRORAY_ROOT/web-new/api/auth-common.sh"
broray_api_success '{}'
''')
        self.assertIn(b'503 Service Unavailable', p.stdout)
        self.assertIn(b'OPERATION_FINALIZATION_FAILED', p.stdout)
        self.assertTrue(self.lock.is_symlink())
        self.assertEqual((self.lock / 'owner.json').read_bytes(), b'{broken')

    def test_live_owner_blocks_second_caller_without_changing_evidence(self):
        source, acquire, release = self.caller('routes')
        p = subprocess.Popen(['/bin/busybox', 'ash', '-c', source + acquire +
            ' || exit 91\necho READY\nread -r reply\n' + release + ' completed\n'],
            env=self.env, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            self.assertTrue(select.select([p.stdout], [], [], 30)[0])
            self.assertEqual(p.stdout.readline(), b'READY\n')
            before = {f.name: f.read_bytes() for f in self.lock.iterdir()}
            self.run_shell(source + acquire, 2)
            self.assertEqual({f.name: f.read_bytes() for f in self.lock.iterdir()}, before)
            p.stdin.write(b'finish\n'); p.stdin.flush()
            out, err = p.communicate(timeout=30)
            self.assertEqual(p.returncode, 0, (out, err))
            self.assertFalse(self.lock.is_symlink())
        finally:
            if p.poll() is None:
                p.kill(); p.communicate(timeout=5)

    def test_child_cannot_release_parent_owner(self):
        source, acquire, release = self.caller('routes')
        self.run_shell(source + acquire + ' || exit 91\nrc=0\n(' + release +
            ' completed) || rc=$?\ntest "$rc" != 0 || exit 92\n'
            'test -L "$BRORAY_ROUTES_API_LOCK" || exit 93\n' + release + ' completed\n')
