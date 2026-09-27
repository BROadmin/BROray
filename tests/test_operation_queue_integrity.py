"""Bounded receipts must not lose live request identity or recreate its queue."""
import json
import unittest
import test_operation_resources as support


class QueueIntegrity(unittest.TestCase):
    setUp = support.OperationResources.setUp
    tearDown = support.OperationResources.tearDown
    set_owner = support.OperationResources.set_owner
    call = support.OperationResources.call
    submit = support.OperationResources.submit
    queue_file = support.OperationResources.queue_file
    claim = support.OperationResources.claim
    ack = support.OperationResources.ack
    opfile = support.OperationResources.opfile

    def test_live_nonce_remains_bound_after_receipt_eviction(self):
        nonce = '7'*32
        self.submit(nonce=nonce)
        data = json.loads(self.queue_file().read_bytes())
        # Equivalent bounded-receipt eviction while the original job is alive.
        data['receipts'] = []
        self.queue_file().write_text(json.dumps(data))
        before = self.queue_file().read_bytes()
        self.assertEqual(self.submit(target='different', nonce=nonce, expected=2)['errorCode'], 'REQUEST_MISMATCH')
        self.assertEqual(self.queue_file().read_bytes(), before)
        self.assertEqual(self.submit(nonce=nonce)['requestId'], 'q-'+nonce)

    def test_missing_namespace_with_same_boot_step_is_not_recreated(self):
        operation = self.claim(self.submit())
        self.ack(operation)
        self.queue_file().unlink()
        self.queue_file().parent.rmdir()
        self.assertEqual(self.submit(expected=2)['errorCode'], 'QUEUE_STATE_INVALID')
        self.assertFalse(self.queue_file().parent.exists())
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_queue_priority_tamper_is_rejected_before_selection(self):
        self.submit()
        data = json.loads(self.queue_file().read_bytes())
        data['requests'][0]['priority'] = 0
        self.queue_file().write_text(json.dumps(data))
        before = self.queue_file().read_bytes()
        self.assertEqual(self.call('queue-next', expected=2)['errorCode'], 'QUEUE_STATE_INVALID')
        self.assertEqual(self.queue_file().read_bytes(), before)


if __name__ == '__main__':
    unittest.main(verbosity=2, failfast=True)
