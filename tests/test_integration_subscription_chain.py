"""Cross-contract integration: real async owner/parse/commit, fixture HTTP/ndmc.

No router or network access. Existing suites remain unchanged. The cancellation
case gates the fixture copy immediately after its real numeric progress write.
"""
import base64
import ctypes
import json
import os
import unittest
from pathlib import Path

from test_subscription_async import SubscriptionAsync
from test_subscription_device_update import DeviceUpdate
from test_subscription_jobs import SubscriptionJobs
from test_subscription_metadata_update import Updates
from test_subscription_partial_update import Partial, BAD, uri


class IntegrationSubscriptionChain(unittest.TestCase):
    setUp = DeviceUpdate.setUp
    clean_fixture = SubscriptionJobs.clean_fixture
    record = SubscriptionJobs.record
    states = SubscriptionJobs.states
    shell = SubscriptionJobs.shell
    reap_adopted_helpers = SubscriptionJobs.reap_adopted_helpers
    response = Updates.response
    calls = DeviceUpdate.calls
    seed = Partial.seed
    snapshot = Partial.snapshot
    assert_clean = Partial.assert_clean
    wait = SubscriptionAsync.wait
    terminal = SubscriptionAsync.terminal
    cancel = SubscriptionAsync.cancel

    def launch_existing(self):
        response = self.shell('''
. "$BRORAY_ROOT/lib/subscription-service.sh"
. "$BRORAY_ROOT/lib/subscription-web-job.sh"
broray_job_begin system subscriptions:refresh subscriptions USER cooperative || exit $?
trap 'broray_job_exit "$?"' EXIT
broray_subscription_launch_update test manual
''', timeout=60)
        accepted = json.loads(response.stdout)
        self.assertTrue(accepted['accepted'])
        self.assertNotIn('PRIVATE_CANARY', response.stdout.decode())
        return accepted

    def assert_headers(self):
        calls = self.calls()
        self.assertEqual(len(calls), 1)
        for value in ['X-Device-Model: KN-2710', 'X-Device-OS: KeeneticOS',
                      'X-Ver-OS: 5.1.1', 'X-App-Version: 3.1.1', 'Client/2',
                      'x-hwid: broray-1234567890abcdef1234567890abcdef']:
            self.assertIn(value, calls[0])

    def test_async_base64_metadata_device_partial_commit(self):
        path = self.record(sendDeviceInfo=True, httpUserAgent='Client/2')
        before = json.loads(path.read_bytes())
        old = self.seed(['old'])['old']
        saved = old.read_bytes()
        payload = '#profile-title: Body provider\n' + uri('new') + '\n' + BAD
        self.response('profile-title: HTTP provider\r\nprofile-update-interval: 12\r\n',
                      body=base64.b64encode(payload.encode()).decode())
        accepted = self.launch_existing()
        self.wait(self.terminal, timeout=120)
        after = json.loads(path.read_bytes())
        result = after['lastUpdateResult']
        self.assertEqual(self.states()[0]['operationId'], accepted['backgroundOperationId'])
        self.assertEqual(self.states()[0]['state'], 'completed')
        self.assertEqual(after['lastUpdateStatus'], 'partial')
        self.assertEqual((result['received'], result['accepted'], result['rejected'],
                          result['retained'], result['removed'], result['catalogTotal']),
                         (2, 1, 1, 1, 0, 2))
        self.assertEqual(old.read_bytes(), saved)
        self.assertEqual(after['providerMetadata']['title'], 'Body provider')
        self.assertEqual(after['providerMetadata']['suggestedUpdateMinutes'], 720)
        for key in ['name', 'updateIntervalMinutes', 'clientHwid', 'sendDeviceInfo', 'httpUserAgent']:
            self.assertEqual(after[key], before[key])
        self.assert_headers()
        self.assert_clean()

    def test_async_cancel_after_fetch_numeric_progress_preserves_old_metadata_and_nodes(self):
        path = self.record(sendDeviceInfo=True, httpUserAgent='Client/2',
                           providerMetadata={'schemaVersion': 1, 'title': 'Old provider'})
        self.seed(['old'])
        before = self.snapshot()
        self.response('profile-title: Must not commit\r\n', body=uri('new') + '\n' + BAD)
        ready = self.temp / 'progress-ready'
        self.env['TEST_PROGRESS_READY'] = str(ready)
        library = self.app / 'lib/subscription-service.sh'
        source = library.read_text()
        marker = '    broray_subscription_parse_progress 0 || return 1\n'
        self.assertEqual(source.count(marker), 1)
        library.write_text(source.replace(marker, marker +
            '    echo ready >"$TEST_PROGRESS_READY"; sleep 60\n', 1))
        accepted = self.launch_existing()
        try:
            self.wait(ready.exists, timeout=90)
            public = json.loads(self.shell('. "$BRORAY_ROOT/lib/subscription-service.sh"; '
                                           'broray_subscription_get test').stdout)
            self.assertEqual(public['parseProgress'], {'processed': 0, 'total': 2})
            self.assertTrue(public['updateOperation']['canCancel'])
            self.assert_headers()
        finally:
            self.cancel(accepted['backgroundOperationId'])
            self.wait(self.terminal, timeout=90)
        after = json.loads(path.read_bytes())
        self.assertEqual(self.states()[0]['state'], 'aborted')
        self.assertEqual(after['lastUpdateResult']['errorCode'], 'CANCELLED')
        self.assertEqual(after['providerMetadata'], {'schemaVersion': 1, 'title': 'Old provider'})
        self.assertEqual(self.snapshot(), before)
        self.assert_clean()


if __name__ == '__main__':
    assert os.name != 'nt'
    assert ctypes.CDLL(None).prctl(36, 1, 0, 0, 0) == 0
    result = unittest.TextTestRunner(verbosity=2, failfast=True).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(IntegrationSubscriptionChain))
    raise SystemExit(not result.wasSuccessful())
