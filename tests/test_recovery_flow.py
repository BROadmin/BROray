"""Emergency recovery contract with isolated state and synthetic process identities."""
import json, os, subprocess, unittest, uuid
from pathlib import Path
from test_operations import Operations, APP, BB, WORKSPACE
from test_operations_http import HTTP


class Recovery(unittest.TestCase):
    def setUp(self):
        self.ops = Operations('test_normal_finish_and_next_begin')
        self.ops.setUp()

    def tearDown(self):
        self.ops.tearDown()

    def test_recovery_pauses_before_requesting_stop_and_keeps_live_fence(self):
        op = self.ops.begin()
        result = self.ops.call('recover', expected=2)
        self.assertTrue(self.ops.call('status')['automationPaused'])
        self.assertTrue(self.ops.opfile(op, 'cancel.json').exists())
        self.assertTrue((self.ops.temp / 'global.lock').is_symlink())
        self.assertEqual(result['errorCode'], 'RECOVERY_BLOCKED')
        self.assertTrue(result['retryable'])
        self.assertEqual(result['result'], 'ACTIVE')

    def test_next_check_releases_only_after_owner_disappears(self):
        op = self.ops.begin()
        self.ops.call('recover', expected=2)
        self.ops.set_owner({'status': 'absent'})
        result = self.ops.call('recover')
        self.assertTrue(result['automationPaused'])
        self.assertFalse(result['retryable'])
        self.assertEqual(result['result'], 'recovered')
        self.assertFalse((self.ops.temp / 'global.lock').is_symlink())
        self.assertEqual(json.loads(self.ops.opfile(op, 'state.json').read_bytes())['state'], 'recovered')

    def test_failed_pause_never_retires_stale_fence(self):
        op = self.ops.begin()
        self.ops.set_owner({'status': 'absent'})
        (self.ops.state / 'background-automation.json').mkdir()
        self.assertEqual(self.ops.call('recover', expected=1)['errorCode'], 'STATE_UNAVAILABLE')
        self.assertTrue((self.ops.temp / 'global.lock').is_symlink())
        self.assertFalse(self.ops.opfile(op, 'cancel.json').exists())

    def test_protected_routes_are_neither_cancelled_nor_retried(self):
        op = self.ops.call('begin', 'routes', 'export', 'fixture', 'USER', '900001', 'cooperative', uuid.uuid4().hex)
        self.ops.call('ack', op['operationId'], op['token'], '900001')
        before = self.ops.opfile(op, 'state.json').read_bytes()
        result = self.ops.call('recover', expected=2)
        self.assertFalse(result['retryable'])
        self.assertFalse(self.ops.opfile(op, 'cancel.json').exists())
        self.assertEqual(self.ops.opfile(op, 'state.json').read_bytes(), before)
        self.ops.set_owner({'status': 'absent'})
        self.assertEqual(self.ops.call('recover', expected=2)['result'], 'protected_recovery')

    def test_legacy_lock_preserved_with_explicit_reason(self):
        lock = self.ops.temp / 'global.lock'
        lock.mkdir()
        for key, value in {'pid': '900001', 'scope': 'system', 'action': 'auto-switch', 'bundle': '', 'startedAt': '2020'}.items():
            (lock / key).write_text(value + '\n')
        before = {p.name: p.read_bytes() for p in lock.iterdir()}
        result = self.ops.call('recover', expected=2)
        self.assertEqual(result['result'], 'legacy_owner_ambiguous')
        self.assertEqual(result['errorCode'], 'RECOVERY_BLOCKED')
        self.assertFalse(result['retryable'])
        self.assertTrue(result['automationPaused'])
        self.assertEqual({p.name: p.read_bytes() for p in lock.iterdir()}, before)

    def test_live_child_keeps_fence_after_parent_exit(self):
        op = self.ops.begin()
        child = {**self.ops.owner, 'startTicks': '999'}
        self.ops.opfile(op, 'children.json').write_text(json.dumps({'children': [child]}))
        self.ops.set_owner(child)
        result = self.ops.call('recover', expected=2)
        self.assertEqual(result['result'], 'children_unconfirmed')
        self.assertTrue(result['retryable'])
        self.assertTrue((self.ops.temp / 'global.lock').is_symlink())

    def test_unknown_owner_is_not_retried_or_removed(self):
        self.ops.begin()
        self.ops.set_owner({'status': 'unreadable'})
        result = self.ops.call('recover', expected=2)
        self.assertEqual(result['result'], 'AMBIGUOUS')
        self.assertFalse(result['retryable'])
        self.assertTrue((self.ops.temp / 'global.lock').is_symlink())

    def test_updater_fence_is_reported_even_without_background_job(self):
        lock = self.ops.temp / 'updater/request.lock'
        lock.mkdir(parents=True)
        (lock / 'private').write_text('updater-owned')
        result = self.ops.call('recover', expected=2)
        self.assertEqual(result['result'], 'updater_pending')
        self.assertFalse(result['retryable'])
        self.assertEqual((lock / 'private').read_text(), 'updater-owned')

    def test_idle_recovery_is_idempotent_and_stays_paused(self):
        for _ in range(2):
            result = self.ops.call('recover')
            self.assertEqual(result['result'], 'absent')
            self.assertTrue(result['automationPaused'])
            self.assertFalse(result['retryable'])

    def test_fifo_updater_pointer_is_not_read_and_blocks_recovery(self):
        pointer = self.ops.state / 'last-operation'
        os.mkfifo(pointer)
        result = self.ops.call('recover', expected=2)
        self.assertEqual(result['result'], 'updater_pending')
        self.assertTrue(pointer.is_fifo())

    def test_updater_pointer_state_is_checked_without_mutation(self):
        directory = self.ops.state / 'operations/update-fixture'
        directory.mkdir(parents=True)
        state = directory / 'state.json'
        state.write_text('{"kind":"update","running":true}')
        (self.ops.state / 'last-operation').write_text('update-fixture\n')
        before = state.read_bytes()
        self.assertEqual(self.ops.call('recover', expected=2)['result'], 'updater_pending')
        self.assertEqual(state.read_bytes(), before)
        state.write_text('{"kind":"update","running":false}')
        self.assertTrue(self.ops.call('recover')['ok'])


class RecoveryHTTP(HTTP):
    # Run the new recovery API cases only; the general HTTP suite is separate.
    def test_recovery_conflict_explains_pending_stop(self):
        op = self.ops.begin()
        _, result = self.request('recover', 'POST', {}, status=409)
        self.assertTrue(result['retryable'])
        self.assertEqual(result['errorCode'], 'RECOVERY_BLOCKED')
        self.ops.set_owner({'status': 'absent'})
        _, result = self.request('recover', 'POST', {}, status=202)
        self.assertTrue(result['automationPaused'])
        self.assertNotIn(op['token'], json.dumps(result))


if __name__ == '__main__':
    if os.name == 'nt':
        raise SystemExit('Use the isolated Linux VM.')
    Path('/opt').mkdir(exist_ok=True)
    if not Path('/opt/broray').exists():
        Path('/opt/broray').symlink_to(APP, target_is_directory=True)
    Path('/opt/bin').mkdir(exist_ok=True)
    subprocess.run([str(BB), '--install', '-s', '/opt/bin'], check=True)
    if not Path('/opt/bin/jq').exists():
        Path('/opt/bin/jq').symlink_to('/usr/bin/jq')
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(Recovery)
    suite.addTest(RecoveryHTTP('test_recovery_conflict_explains_pending_stop'))
    result = unittest.TextTestRunner(verbosity=2, failfast=True).run(suite)
    report = {'status': 'PASS' if result.wasSuccessful() else 'FAIL', 'testsRun': result.testsRun,
              'environment': 'Linux native guard, synthetic process identities, authenticated CGI', 'routerAccessed': False}
    (WORKSPACE / 'docs/evidence/recovery-flow-tests.json').write_text(json.dumps(report, indent=2) + '\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
