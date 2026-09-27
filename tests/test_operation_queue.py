"""Priority requests through the real coordinator; process identities are synthetic."""
import hashlib
import json
import shutil
import unittest
import uuid
import test_operations as support


class OperationQueue(unittest.TestCase):
    setUp = support.Operations.setUp
    set_owner = support.Operations.set_owner
    call = support.Operations.call

    def tearDown(self):
        shutil.rmtree(self.temp)

    def submit(self, action='servers:quality', target='batch', source='SERVER_CHECK_AUTO',
               context='a'*64, nonce=None, expected=0):
        return self.call('queue-submit', action, target, source, context,
                         nonce or uuid.uuid4().hex, expected=expected)

    def queue_file(self):
        return self.temp/'ram/queue/boot-one/state.json'

    def test_priority_and_fifo_without_waiting_processes(self):
        subscription = self.submit('subscriptions:refresh', 'sub-one', 'SUBSCRIPTION_AUTO')
        quality = self.submit(target='first')
        self.submit(target='second')
        health = self.submit('servers:active-health', 'active', 'AUTO_SWITCH')
        self.assertEqual(self.call('queue-next')['requestId'], health['requestId'])
        self.assertEqual(health['priority'], 0)
        self.call('queue-cancel', health['requestId'])
        self.assertEqual(self.call('queue-next')['requestId'], quality['requestId'])
        self.assertEqual(subscription['state'], 'queued')
        self.assertFalse((self.temp/'global.lock').exists())
        self.assertEqual(list((self.state/'operations').glob('*/owner.json')), [])

    def test_duplicate_and_lost_reply_resolve_same_request(self):
        nonce = uuid.uuid4().hex
        original = self.submit(nonce=nonce)
        self.assertEqual(self.call('queue-lookup', nonce)['requestId'], original['requestId'])
        self.assertEqual(self.submit(nonce=nonce)['requestId'], original['requestId'])
        alias = uuid.uuid4().hex
        duplicate = self.submit(nonce=alias)
        self.assertTrue(duplicate['coalesced'])
        self.assertEqual(duplicate['requestId'], original['requestId'])
        self.assertEqual(self.call('queue-lookup', alias)['requestId'], original['requestId'])
        self.assertEqual(len(json.loads(self.queue_file().read_bytes())['requests']), 1)
        self.assertEqual(self.submit(target='changed', nonce=nonce, expected=2)['errorCode'], 'REQUEST_MISMATCH')

    def test_queue_limit_rejects_without_changing_existing_requests(self):
        ids = [self.submit(target=f'server-{i}')['requestId'] for i in range(64)]
        before = self.queue_file().read_bytes()
        result = self.submit(target='overflow', expected=2)
        self.assertEqual(result['errorCode'], 'QUEUE_FULL')
        self.assertEqual(self.queue_file().read_bytes(), before)
        self.assertEqual(len(set(ids)), 64)

    def test_corrupt_queue_is_never_replaced(self):
        self.submit()
        self.queue_file().write_bytes(b'{broken')
        for command in [('queue-next',), ('queue-lookup', uuid.uuid4().hex)]:
            self.assertEqual(self.call(*command, expected=2)['errorCode'], 'QUEUE_STATE_INVALID')
            self.assertEqual(self.queue_file().read_bytes(), b'{broken')
        self.assertEqual(self.submit(expected=2)['errorCode'], 'QUEUE_STATE_INVALID')
        self.assertEqual(self.queue_file().read_bytes(), b'{broken')

    def test_missing_initialized_queue_is_not_recreated(self):
        self.submit()
        self.queue_file().unlink()
        self.assertEqual(self.submit(expected=2)['errorCode'], 'QUEUE_STATE_INVALID')
        self.assertFalse(self.queue_file().exists())

    def test_boot_boundary_does_not_replay_user_request(self):
        nonce = uuid.uuid4().hex
        self.submit(source='USER', nonce=nonce)
        before = self.queue_file().read_bytes()
        (self.proc/'sys/kernel/random/boot_id').write_text('boot-two\n')
        self.assertIsNone(self.call('queue-next')['requestId'])
        self.assertEqual(self.call('queue-lookup', nonce, expected=2)['errorCode'], 'REQUEST_UNCONFIRMED')
        self.assertEqual(self.queue_file().read_bytes(), before)

    def test_context_change_is_not_coalesced_and_paused_auto_cannot_run(self):
        one = self.submit()
        two = self.submit(context='b'*64)
        self.assertNotEqual(one['requestId'], two['requestId'])
        self.call('pause')
        self.assertIsNone(self.call('queue-next')['requestId'])
        manual = self.submit(target='manual', source='USER')
        self.assertEqual(self.call('queue-next')['requestId'], manual['requestId'])
        self.call('queue-cancel', manual['requestId'])
        self.call('resume')
        self.assertEqual(self.call('queue-next')['requestId'], one['requestId'])

    def test_unsafe_queue_and_untrusted_action_fail_closed(self):
        self.submit()
        before = self.queue_file().read_bytes()
        for action, source in [('xray:stop', 'AUTO_SWITCH'), ('servers:active-health', 'SUBSCRIPTION_AUTO'),
                               ('servers:failover', 'USER')]:
            self.assertEqual(self.submit(action, 'target', source, expected=2)['errorCode'], 'INVALID_QUEUE_ACTION')
        self.assertEqual(self.queue_file().read_bytes(), before)
        foreign = self.temp/'foreign.json'
        foreign.write_bytes(b'preserve')
        self.queue_file().unlink()
        self.queue_file().symlink_to(foreign)
        self.assertEqual(self.submit(expected=2)['errorCode'], 'QUEUE_STATE_INVALID')
        self.assertEqual(foreign.read_bytes(), b'preserve')


if __name__ == '__main__':
    unittest.main(verbosity=2, failfast=True)
