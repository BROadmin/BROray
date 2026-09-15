"""Native Linux writer/crashes; explicit fake-owner matrix plus one real producer."""
import ctypes,json,os,shutil,subprocess,unittest,uuid
from test_operations import Operations,APP,BB,GUARD,WORKSPACE
from test_auto_switch_jobs import AutoSwitchJobs,ROOT

class Publications(unittest.TestCase):
    set_owner=Operations.set_owner
    opfile=Operations.opfile
    def setUp(self):
        Operations.setUp(self)
        self.app=self.temp/'app';(self.app/'lib').mkdir(parents=True)
        for file in (APP/'lib').glob('operation-*'):shutil.copy2(file,self.app/'lib'/file.name)
        (self.app/'run/server-quality').mkdir(parents=True)
        self.env['BRORAY_ROOT']=str(self.app)
        self.target=self.app/'run/server-auto-switch-state.json'
        self.input=self.app/'run/input.json'
        self.a=None
    def raw(self,*args):
        return subprocess.run([str(GUARD),str(self.state/'operations.guard'),str(BB),'ash',str(self.app/'lib/operation-coordinator.sh'),*map(str,args)],env=self.env,capture_output=True,timeout=30)
    def call(self,*args,expected=0):
        p=self.raw(*args);self.assertEqual(p.returncode,expected,(args,p.stdout,p.stderr));return json.loads(p.stdout)
    def start(self,mode='cooperative'):
        self.a=self.call('begin','system','auto-switch','servers','AUTO_SWITCH','900001',mode,uuid.uuid4().hex)
        self.call('ack',self.a['operationId'],self.a['token'],'900001')
        self.payload={'schemaVersion':3,'backgroundOperationId':self.a['operationId'],'enabled':False,'status':'disabled','consecutiveFailures':0,'candidateCount':0,
          'qualityRefresh':dict(status='disabled',totalCount=0,checkedCount=0,availableCount=0,unavailableCount=0,errorCount=0)}
        self.write_input();return self.request()
    def write_input(self):
        self.input.write_text(json.dumps(self.payload));self.input.chmod(0o600)
    def request(self):
        revision=json.loads(self.opfile(self.a,'state.json').read_text())['revision']
        return ['publish-json',self.a['operationId'],self.a['token'],'900001','auto-state','',str(self.input),uuid.uuid4().hex,str(revision)]
    def crash(self,point,mode='cooperative'):
        args=self.start(mode);self.env['BRORAY_OPS_TEST_PUBLICATION_CRASH']=point
        p=self.raw(*args);self.assertEqual(p.returncode,-9,(point,p.stdout,p.stderr))
        del self.env['BRORAY_OPS_TEST_PUBLICATION_CRASH'];return args
    def dead(self):self.set_owner({'status':'absent'})
    def test_publish_restores_phase_and_lost_response_retries(self):
        args=self.start();self.call(*args);before=self.target.read_bytes()
        self.assertTrue(self.call(*args)['alreadyPublished']);self.assertEqual(self.target.read_bytes(),before)
        self.assertEqual(json.loads(self.opfile(self.a,'state.json').read_text())['phase'],'working')
        self.call('finish',self.a['operationId'],self.a['token'],'completed','')
    def test_old_request_cannot_overwrite_later_publication(self):
        old=self.start();self.call(*old);self.payload['status']='manual-off';self.write_input();self.call(*self.request())
        before=self.target.read_bytes();self.payload['status']='disabled';self.write_input()
        self.assertEqual(self.call(*old,expected=75)['errorCode'],'STALE_PUBLICATION');self.assertEqual(self.target.read_bytes(),before)
    def test_cancel_before_reservation_writes_no_business_state(self):
        args=self.start();self.call('cancel',self.a['operationId'])
        self.assertEqual(self.call(*args,expected=2)['errorCode'],'CANCELLED');self.assertFalse(self.target.exists())
    def test_cancel_after_reserved_crash_does_not_enter_commit(self):
        args=self.crash('reserved');self.call('cancel',self.a['operationId'])
        self.assertEqual(self.call(*args,expected=2)['errorCode'],'CANCELLED');self.assertFalse(self.target.exists())
        self.call('finish',self.a['operationId'],self.a['token'],'aborted','CANCELLED')
    def test_wrong_actual_owner_rejected(self):
        args=self.start();args[3]='900002';self.call(*args,expected=1);self.assertFalse(self.target.exists())
    def test_invalid_resource_and_key_rejected(self):
        args=self.start();args[4]='arbitrary-file';self.call(*args,expected=1)
        args[4]='server-quality';args[5]='../foreign';self.call(*args,expected=1);self.assertFalse(self.target.exists())
    def test_input_symlink_and_multiple_documents_rejected(self):
        args=self.start();source=self.input.with_suffix('.real');self.input.rename(source);self.input.symlink_to(source)
        self.call(*args,expected=1);self.input.unlink();self.input.write_text(source.read_text()+'\n'+source.read_text());self.input.chmod(0o600)
        self.call(*args,expected=1);self.assertFalse(self.target.exists())
    def test_target_symlink_and_hardlink_preserved(self):
        args=self.start();foreign=self.app/'run/foreign';foreign.write_text('KEEP');foreign.chmod(0o600);self.target.symlink_to(foreign)
        self.call(*args,expected=1);self.assertEqual(foreign.read_text(),'KEEP')
        self.target.unlink();os.link(foreign,self.target);self.call(*args,expected=1);self.assertEqual(foreign.read_text(),'KEEP')
    def test_unknown_candidate_preserved(self):
        args=self.start();pending=self.target.with_name(self.target.name+'.ops-pending');pending.write_text('KEEP');pending.chmod(0o600)
        self.call(*args,expected=75);self.assertEqual(pending.read_text(),'KEEP');self.assertFalse(self.target.exists())
    def test_third_target_state_blocks_recovery(self):
        self.crash('replaced');self.target.write_text('{"foreign":true}\n');before=self.target.read_bytes();self.dead()
        self.assertEqual(self.call('recover',expected=2)['result'],'publication_unconfirmed')
        self.assertEqual(self.target.read_bytes(),before);self.assertTrue((self.temp/'global.lock').is_symlink())
    def test_nested_protected_transaction_remains_fenced(self):
        self.crash('replaced','protected');self.dead()
        self.assertEqual(self.call('recover',expected=2)['result'],'protected_recovery')
        self.assertTrue((self.temp/'global.lock').is_symlink());self.assertTrue(json.loads(self.opfile(self.a,'publication.json').read_text())['complete'])
    def test_pending_publication_blocks_tick_and_finish(self):
        self.crash('protected')
        self.call('tick',self.a['operationId'],self.a['token'],'working',expected=75)
        self.call('finish',self.a['operationId'],self.a['token'],'completed','',expected=75)
    def test_failed_sync_retains_fence_until_retry(self):
        self.crash('replaced');self.dead()
        shim=self.app/'sync-failure';shim.write_text('#!/bin/ash\n[ "$1" != --sync-state ] || exit 74\nexec "'+str(GUARD)+'" "$@"\n');shim.chmod(0o755)
        self.env['BRORAY_OPS_GUARD']=str(shim)
        self.assertEqual(self.call('recover',expected=2)['result'],'publication_unconfirmed');self.assertTrue((self.temp/'global.lock').is_symlink())
        self.env['BRORAY_OPS_GUARD']=str(GUARD);self.assertTrue(self.call('recover')['ok'])

def crash_case(point):
    def test(self):
        self.crash(point);existed=self.target.exists();before=self.target.read_bytes() if existed else None
        self.dead();self.assertTrue(self.call('recover')['ok']);self.assertFalse((self.temp/'global.lock').is_symlink())
        self.assertEqual(self.target.read_bytes() if self.target.exists() else None,before)
        self.assertTrue(self.call('recover')['ok'])
    return test
for point in ['reserved','protected','prepared','replaced','restored']:
    setattr(Publications,'test_crash_'+point+'_recovers_observed_content',crash_case(point))

class ActualPublication(unittest.TestCase):
    setUp=AutoSwitchJobs.setUp
    shell=AutoSwitchJobs.shell
    collect=AutoSwitchJobs.collect
    states=AutoSwitchJobs.states
    reap_adopted_helpers=AutoSwitchJobs.reap_adopted_helpers
    once=AutoSwitchJobs.once
    def test_real_auto_job_crash_after_publish_recovers(self):
        file=self.app/'bin/broray-server-auto-switch';source=file.read_text()
        anchor='    S_LOADED_SIGNATURE="$CURRENT_SIGNATURE"'
        self.assertEqual(source.count(anchor),1);file.write_text(source.replace(anchor,'    kill -KILL "$$"\n'+anchor))
        self.shell(self.once(),expected=137)
        self.assertEqual(self.states()[0]['phase'],'working')
        self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call recover')
        self.assertFalse((self.temp/'global.lock').is_symlink())

if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    suite=unittest.TestSuite(unittest.defaultTestLoader.loadTestsFromTestCase(cls) for cls in [Publications,ActualPublication])
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(suite)
    (WORKSPACE/'docs/evidence/atomic-publication-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'native Linux; labelled fake-owner matrix and actual producer self-crash','routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
