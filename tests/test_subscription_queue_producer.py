"""Scheduler admission only: no helper, metadata write, parser or router."""
import ctypes
import json
import time
import unittest
from pathlib import Path
from test_dot_auto_jobs import Jobs
from test_subscription_jobs import SubscriptionJobs
from test_server_jobs import ServerJobs


class Producer(unittest.TestCase):
    setUp=Jobs.setUp
    shell=ServerJobs.shell
    collect=ServerJobs.collect
    reap_adopted_helpers=ServerJobs.reap_adopted_helpers
    record=SubscriptionJobs.record

    def producer(self):
        return json.loads(self.shell('''
. "$BRORAY_ROOT/lib/subscription-service.sh"
broray_subscription_enqueue_due
''').stdout)

    def test_due_subscriptions_and_dot_coalesce_without_domain_mutation(self):
        path=self.record()
        second=json.loads(path.read_bytes());second['id']='second'
        other=self.subdir/'second.json';other.write_text(json.dumps(second));other.chmod(0o600)
        before={p:p.read_bytes() for p in [path,other,self.dot/'config.json',self.dot/'state.json']}
        first=self.producer()
        again=self.producer()
        self.assertFalse(first['paused'])
        self.assertEqual(len(first['subscriptionRequests']),2)
        self.assertEqual([r['requestId'] for r in first['subscriptionRequests']],
                         [r['requestId'] for r in again['subscriptionRequests']])
        self.assertEqual(first['dotRequest']['requestId'],again['dotRequest']['requestId'])
        self.assertEqual(first['dotRequest']['priority'],5)
        self.assertTrue(all(r['priority']==4 for r in first['subscriptionRequests']))
        self.assertEqual({p:p.read_bytes() for p in before},before)
        self.assertFalse((self.app/'tls-calls').exists())
        self.assertFalse((self.temp/'global.lock').exists())
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
        boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
        queue=json.loads((self.temp/'ram/queue'/boot/'state.json').read_bytes())
        self.assertEqual(len(queue['requests']),3)
        self.assertNotIn('PRIVATE_CANARY',json.dumps(queue))

    def test_paused_scheduler_admits_nothing(self):
        path=self.record()
        before=path.read_bytes()
        self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call pause')
        result=self.producer()
        self.assertTrue(result['paused'])
        self.assertEqual(result['subscriptionRequests'],[])
        self.assertIsNone(result['dotRequest'])
        self.assertEqual(path.read_bytes(),before)
        self.assertFalse((self.temp/'ram/requests').exists())

    def test_fresh_or_disabled_jobs_are_not_queued(self):
        path=self.record(nextUpdateEpoch=int(time.time())+3600)
        (self.dot/'auto-check.json').write_text('{"schemaVersion":1,"enabled":false}')
        before=path.read_bytes()
        result=self.producer()
        self.assertEqual(result['subscriptionRequests'],[])
        self.assertIsNone(result['dotRequest'])
        self.assertEqual(path.read_bytes(),before)
        self.assertFalse((self.temp/'ram/requests').exists())


if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    unittest.main(verbosity=2,failfast=True)
