"""Executor transfer protocol under the real kernel guard; identities simulated."""
import json,os,unittest,uuid
from pathlib import Path
from test_operations import Operations,WORKSPACE

class Handoff(unittest.TestCase):
    call=Operations.call
    opfile=Operations.opfile
    set_owner=Operations.set_owner
    def setUp(self):
        Operations.setUp(self)
        self.worker={**self.owner,'pid':900002,'startTicks':'200','commandDigest':'b'*64}
        self.actors()
        self.nonce=uuid.uuid4().hex
    def actors(self,parent=None,worker=None):
        self.identities.write_text(json.dumps({'900001':parent or self.owner,'900002':worker or self.worker}))
    def begin(self,mode='protected',action='xray:update'):
        self.launch=uuid.uuid4().hex
        self.a=self.call('begin','routes',action,'xray','USER','900001',mode,self.launch)
        self.id=self.a['operationId'];self.old=self.a['token']
        self.call('ack',self.id,self.old,'900001')
        return self.a
    def transfer(self,expected=0):
        return self.call('handoff',self.id,self.old,'900001','900002',self.nonce,expected=expected)
    def accept(self,expected=0):
        return self.call('accept-handoff',self.id,self.old,'900002',self.nonce,expected=expected)
    def test_old_cleanup_cannot_release_transferred_fence(self):
        self.begin();before=self.opfile(self.a,'owner.json').read_bytes();self.transfer()
        self.assertEqual(self.opfile(self.a,'owner.json').read_bytes(),before)
        self.assertEqual(self.call('finish',self.id,self.old,'completed','',expected=2)['errorCode'],'OWNER_CHANGED')
        new=self.accept()['token'];self.assertNotEqual(new,self.old)
        self.call('finish',self.id,new,'completed','');self.assertFalse((self.temp/'global.lock').exists())
    def test_handoff_and_accept_retries_are_idempotent(self):
        self.begin();first=self.transfer();record=self.opfile(self.a,'executor.json').read_bytes()
        self.assertEqual(self.transfer(),first);self.assertEqual(self.opfile(self.a,'executor.json').read_bytes(),record)
        accepted=self.accept();record=self.opfile(self.a,'executor.json').read_bytes()
        self.assertEqual(self.accept(),accepted);self.assertEqual(self.opfile(self.a,'executor.json').read_bytes(),record)
        self.call('tick',self.id,accepted['token'],'committing')
        self.assertEqual(self.transfer(),first)
        self.call('finish',self.id,accepted['token'],'completed','')
        self.actors(worker={'status':'absent'});self.assertEqual(self.transfer(),first)
    def test_worker_cannot_work_before_acceptance(self):
        self.begin();self.transfer();new=json.loads(self.opfile(self.a,'executor.json').read_text())['token']
        for args in [('tick',self.id,new,'committing'),('finish',self.id,new,'completed',''),('ack',self.id,new,'900002')]:
            self.assertEqual(self.call(*args,expected=2)['errorCode'],'NOT_ACKNOWLEDGED')
        row=self.call('status')['operations'][0];self.assertEqual(row['phase'],'waiting');self.assertTrue(row['running'])
    def test_parent_cannot_accept_worker_invitation(self):
        self.begin();self.transfer()
        self.assertEqual(self.call('accept-handoff',self.id,self.old,'900001',self.nonce,expected=2)['errorCode'],'OWNER_CHANGED')
        self.assertEqual(self.call('accept-handoff',self.id,self.old,'900002',uuid.uuid4().hex,expected=2)['errorCode'],'OWNER_CHANGED')
    def test_dead_parent_does_not_make_live_worker_stale(self):
        self.begin();self.transfer();self.accept();self.actors(parent={'status':'absent'})
        self.assertEqual(self.call('classify',self.id)['ownerStatus'],'ACTIVE')
        self.assertEqual(self.call('recover',expected=2)['result'],'ACTIVE')
    def test_dead_unacknowledged_worker_recovers_protected_admission(self):
        self.begin();self.transfer();self.actors(worker={'status':'absent'})
        self.assertEqual(self.call('recover')['result'],'recovered')
        self.assertFalse((self.temp/'global.lock').exists())
    def test_dead_acknowledged_protected_worker_requires_domain_recovery(self):
        self.begin();self.transfer();self.accept();self.actors(worker={'status':'absent'})
        self.assertEqual(self.call('recover',expected=2)['result'],'protected_recovery')
        self.assertTrue((self.temp/'global.lock').exists())
    def test_worker_pid_reuse_cannot_accept_or_write(self):
        self.begin();self.transfer();self.actors(worker={**self.worker,'startTicks':'201'})
        self.assertEqual(self.accept(expected=2)['errorCode'],'OWNER_CHANGED')
        self.assertEqual(self.call('recover')['result'],'recovered')
    def test_commit_and_cancel_block_transfer(self):
        self.begin();self.call('tick',self.id,self.old,'committing')
        self.assertEqual(self.transfer(expected=2)['errorCode'],'HANDOFF_NOT_ALLOWED')
        self.call('finish',self.id,self.old,'completed','')
        self.begin('cooperative');self.call('cancel',self.id)
        self.assertEqual(self.transfer(expected=2)['errorCode'],'CANCELLED')
    def test_cancel_between_transfer_and_accept_never_admits_worker(self):
        self.begin('cooperative');self.transfer();self.call('cancel',self.id)
        self.assertEqual(self.accept(expected=2)['errorCode'],'CANCELLED')
        self.actors(worker={'status':'absent'});self.assertEqual(self.call('recover')['result'],'recovered')
    def test_live_child_blocks_transfer(self):
        self.begin();self.opfile(self.a,'children.json').write_text(json.dumps({'children':[self.worker]}))
        self.assertEqual(self.transfer(expected=2)['errorCode'],'CHILDREN_UNCONFIRMED')
    def test_malformed_executor_does_not_fall_back_to_old_owner(self):
        self.begin();self.transfer();self.opfile(self.a,'executor.json').write_text('{broken')
        self.assertFalse(self.call('status')['ok'])
        self.assertEqual(self.call('finish',self.id,self.old,'completed','',expected=1)['errorCode'],'STATE_UNAVAILABLE')
    def test_original_launch_retry_cannot_reacquire_transferred_authority(self):
        self.begin();self.transfer()
        self.assertEqual(self.call('begin','routes','xray:update','xray','USER','900001','protected',self.launch,expected=2)['errorCode'],'OWNER_CHANGED')

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Handoff))
    (WORKSPACE/'docs/evidence/handoff-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'Linux native guard and production coordinator; synthetic executor identities','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
