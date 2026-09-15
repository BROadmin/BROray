"""Actual /proc, native supervisor, durable coordinator and production client."""
import ctypes,json,os,subprocess,time,unittest,uuid
from pathlib import Path
from test_supervisor import Supervisor,ROOT,ticks

APP=ROOT/'implementation/runtime/app';GUARD=ROOT/'.local/bin/linux-guard'

class Integration(unittest.TestCase):
    setUpFixture=Supervisor.setUp
    tearDown=Supervisor.tearDown
    launch=Supervisor.launch
    wait_ready=Supervisor.wait_ready
    verify_gone=Supervisor.verify_gone
    def setUp(self):
        self.setUpFixture()
        self.state=self.temp/'state';self.state.mkdir();self.ram=self.temp/'ram'
        self.env.update({'BRORAY_ROOT':str(APP),'BRORAY_STATE_ROOT':str(self.state),
          'BRORAY_ROUTES_API_LOCK':str(self.temp/'global.lock'),'BRORAY_LEGACY_GLOBAL_LOCK':str(self.temp/'legacy.lock'),
          'BRORAY_OPS_UPDATER_ROOT':str(self.temp/'updater'),'BRORAY_OPS_RAM_ROOT':str(self.ram),
          'BRORAY_OPS_GUARD':str(GUARD),'BRORAY_OPS_ASH':'/bin/ash','BRORAY_OPS_SUPERVISOR':str(ROOT/'.local/bin/linux-supervisor')})
        for key in ['BRORAY_OPS_TEST','BRORAY_OPS_TEST_IDENTITIES','BRORAY_OPS_PROC_ROOT']:
            self.env.pop(key,None)
        self.control=APP/'lib/operation-supervisor-control.sh'
        self.owner=None
    def call(self,*args,expected=0):
        p=subprocess.run([str(GUARD),str(self.state/'operations.guard'),'/bin/ash',str(APP/'lib/operation-coordinator.sh'),*args],env=self.env,capture_output=True,timeout=20)
        self.assertEqual(p.returncode,expected,(args,p.stdout,p.stderr))
        return json.loads(p.stdout)
    def begin(self,owner=None,action='subscriptions:scheduler'):
        a=self.call('begin','system',action,'subscriptions','USER',str(owner or os.getpid()),'cooperative',uuid.uuid4().hex)
        self.call('ack',a['operationId'],a['token'],str(owner or os.getpid()))
        self.id=a['operationId'];self.token=a['token'];self.op=self.state/'operations'/self.id
        self.env.update({'BRORAY_BACKGROUND_OPERATION_ID':self.id,'BRORAY_BACKGROUND_OPERATION_TOKEN':self.token})
        self.cancel=self.op/'cancel.json'
    def ready(self,p,marker):
        self.wait_ready(p,marker)
        records=json.loads((self.op/'supervisors.json').read_text())['supervisors']
        self.assertEqual(len(records),1)
        self.ledger=self.ram/'supervisors'/self.id/records[0]['supervisorId']/'children.json'
        return records[0]
    def drain(self):
        until=time.monotonic()+6
        while True:
            p=subprocess.run([str(GUARD),str(self.state/'operations.guard'),'/bin/ash',str(APP/'lib/operation-coordinator.sh'),'helpers-drain',self.id,self.token],env=self.env,capture_output=True,timeout=20)
            if p.returncode==0:return
            if time.monotonic()>until:self.fail((p.stdout,p.stderr))
            time.sleep(.1)
    def finish(self,state='completed',code=''):
        self.call('finish',self.id,self.token,state,code)
        self.assertFalse((self.temp/'global.lock').exists())
    def test_live_helper_blocks_commit_finish_and_drain(self):
        self.begin();marker=self.temp/'ready';p=self.launch(f'echo yes >"{marker}"; sleep 60')
        self.ready(p,marker)
        for args in [('tick',self.id,self.token,'committing'),('finish',self.id,self.token,'completed',''),('helpers-drain',self.id,self.token)]:
            self.assertEqual(self.call(*args,expected=2)['errorCode'],'CHILDREN_UNCONFIRMED')
        self.call('cancel',self.id);p.communicate(timeout=6);self.assertEqual(p.returncode,130)
        self.verify_gone();self.drain();self.finish('aborted','CANCELLED')
    def test_client_runs_repeated_helpers_and_bounds_registry(self):
        self.begin()
        script='. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_run_helper 10 -- /bin/sh -c \'printf "literal $HOME"\''
        for _ in range(3):
            p=subprocess.run(['/bin/ash','-c',script],env=self.env,capture_output=True,timeout=20)
            self.assertEqual(p.returncode,0,(p.stdout,p.stderr));self.assertTrue(p.stdout.startswith(b'literal '))
            self.assertEqual(json.loads((self.op/'supervisors.json').read_text())['supervisors'],[])
        self.call('tick',self.id,self.token,'committing');self.finish()
    def test_term_kill_journal_and_idempotent_collection(self):
        self.begin();marker=self.temp/'ready';p=self.launch(f'trap "" TERM; echo yes >"{marker}"; sleep 60')
        self.ready(p,marker);self.call('cancel',self.id);p.communicate(timeout=6)
        self.assertEqual(p.returncode,130);self.verify_gone();self.drain()
        before=self.call('events');self.drain();self.assertEqual(self.call('events'),before)
        for event in ['term','kill']:
            rows=[r for r in before['events'] if r['event']==event]
            self.assertEqual(len(rows),1);self.assertEqual(len(rows[0]['eventId']),32)
        self.finish('aborted','CANCELLED')
    def test_missing_ledger_does_not_release_fence(self):
        self.begin();marker=self.temp/'ready';p=self.launch(f'echo yes >"{marker}"; sleep 60')
        self.ready(p,marker);p.kill();p.communicate(timeout=5);self.verify_gone()
        saved=self.ledger.read_bytes();self.ledger.unlink()
        self.assertEqual(self.call('helpers-drain',self.id,self.token,expected=2)['errorCode'],'CHILDREN_UNCONFIRMED')
        self.assertTrue((self.temp/'global.lock').exists());self.ledger.write_bytes(saved)
        self.drain();self.finish('failed','OPERATION_FAILED')
    def test_live_recorded_child_blocks_even_after_native_exit(self):
        self.begin();marker=self.temp/'ready';p=self.launch(f'echo yes >"{marker}"; sleep 60')
        self.ready(p,marker);p.kill();p.communicate(timeout=5);self.verify_gone()
        saved=self.ledger.read_bytes();data=json.loads(saved)
        sentinel=subprocess.Popen(['/bin/sleep','30']);self.processes.append(sentinel)
        data['children']=[{'pid':sentinel.pid,'startTicks':ticks(sentinel.pid),'bootId':self.env['TEST_BOOT']}]
        self.ledger.write_text(json.dumps(data))
        self.assertEqual(self.call('helpers-drain',self.id,self.token,expected=2)['errorCode'],'CHILDREN_UNCONFIRMED')
        self.assertIsNone(sentinel.poll());self.ledger.write_bytes(saved)
        self.drain();self.finish('failed','OPERATION_FAILED')
    def test_owner_death_stops_helpers_then_allows_recovery(self):
        owner=subprocess.Popen(['/bin/sleep','30']);self.processes.append(owner);self.begin(owner.pid)
        marker=self.temp/'ready';p=self.launch(f'echo yes >"{marker}"; sleep 60')
        self.ready(p,marker);owner.kill();owner.wait(timeout=5);p.communicate(timeout=6)
        self.assertEqual(p.returncode,125);self.verify_gone()
        self.assertEqual(self.call('recover')['result'],'recovered')
        self.assertFalse((self.temp/'global.lock').exists())
    def test_cancelled_admission_never_opens_command_gate(self):
        self.begin();self.call('cancel',self.id);marker=self.temp/'forbidden'
        p=self.launch(f'echo BAD >"{marker}"');p.communicate(timeout=5)
        self.assertEqual(p.returncode,74);self.assertFalse(marker.exists());self.finish('aborted','CANCELLED')
    def test_protected_phase_rejects_helper_launch(self):
        self.begin();self.call('tick',self.id,self.token,'committing');marker=self.temp/'forbidden'
        p=self.launch(f'echo BAD >"{marker}"');p.communicate(timeout=5)
        self.assertEqual(p.returncode,74);self.assertFalse(marker.exists());self.finish()

if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Integration))
    (ROOT/'docs/evidence/supervisor-integration-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'actual Linux processes, production coordinator/client/native supervisor; isolated state','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
