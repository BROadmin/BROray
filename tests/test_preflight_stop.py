"""Stage 04: durable intent + real ptrace helper tree. No actual router services."""
import ctypes,hashlib,json,os,signal,subprocess,time,unittest
from pathlib import Path
from test_preflight_admission import Admission,CODE,GUARD,SHA
SUP=Path('/work/.local/bin/linux-supervisor')

def ticks(pid):
    try:return Path('/proc',str(pid),'stat').read_text().rsplit(') ',1)[1].split()[19]
    except FileNotFoundError:return None

class StopIntent(unittest.TestCase):
    shell=Admission.shell;ok=Admission.ok;once=Admission.once;cleanup=Admission.cleanup
    waitfile=Admission.waitfile;freeze=Admission.freeze;operation=Admission.operation;readstate=Admission.readstate
    def setUp(self):
        Admission.setUp(self)
        self.env['BRORAY_OPS_SUPERVISOR']=str(SUP)
        self.native=[];self.i=None;self.t=None
    def api(self,*args):
        return subprocess.run([str(GUARD),str(self.state/'operations.guard'),'/bin/ash',str(CODE/'lib/operation-coordinator.sh'),*map(str,args)],env=self.env,capture_output=True,text=True,timeout=20)
    def begin(self):
        self.state.mkdir(exist_ok=True,parents=True)
        p=self.api('platform-preflight-begin',SHA,os.getpid(),'1'*32);self.assertEqual(p.returncode,0,p.stderr)
        r=json.loads(p.stdout);self.i=r['operationId'];self.t=r['token'];self.op=self.state/'operations'/self.i
        self.env.update(BRORAY_BACKGROUND_OPERATION_ID=self.i,BRORAY_BACKGROUND_OPERATION_TOKEN=self.t,BRORAY_PREFLIGHT_STOP_NONCE='2'*32)
        q=self.api('ack',self.i,self.t,os.getpid());self.assertEqual(q.returncode,0,q.stderr)
    def intent(self,sha=SHA,nonce='2'*32,pid=None):
        return self.api('platform-preflight-stop-intent',self.i,self.t,pid or os.getpid(),sha,nonce)
    def prepared(self):
        self.begin();p=self.intent();self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
    def native_launch(self,body,timeout=15,mode='--protected-platform',extra=None):
        script=self.home/('worker-'+str(len(self.native))+'.sh');script.write_text('set -eu\n'+body+'\n')
        f=open(self.home/('native-'+str(len(self.native))+'.err'),'w+')
        argv=[str(SUP)]+([mode] if mode else [])+['/bin/ash',str(CODE/'lib/operation-supervisor-control.sh'),str(self.op/'cancel.json'),str(timeout),'0','1','--','/bin/ash',str(script)]
        p=subprocess.Popen(argv,env={**self.env,**(extra or {})},stdout=subprocess.DEVNULL,stderr=f,start_new_session=True)
        self.native.append((p,f));self.addCleanup(self.clean_native,p,f);return p
    def clean_native(self,p,f):
        if p.poll() is None:p.kill()
        p.wait(timeout=5);f.close()
        self.reap()
    def reap(self):
        # Only test children adopted through subreaper. Live processes untouched.
        while True:
            try:
                pid,_=os.waitpid(-1,os.WNOHANG)
                if pid==0:return
            except ChildProcessError:return
    def marker(self,p):
        end=time.monotonic()+15
        while time.monotonic()<end:
            if (self.home/'executing').exists():return
            if p.poll() is not None:
                errors='\n'.join(f.read_text() for f in self.home.glob('*.err'))
                self.fail('Helper ended early: '+str(p.returncode)+' '+errors)
            time.sleep(.02)
        self.fail('Helper gate/marker deadline')
    def ledger(self):
        r=json.loads((self.op/'platform-stop-supervision.json').read_text())
        return self.home/'ram/supervisors'/self.i/r['supervisorId']/'children.json'
    def drain(self):
        end=time.monotonic()+8
        while True:
            self.reap();p=self.api('helpers-drain',self.i,self.t)
            if p.returncode==0:return p
            if time.monotonic()>end:self.fail((p.stdout,p.stderr))
            time.sleep(.05)
    def assert_retained(self):
        self.assertTrue(self.lock.is_symlink())
        p=self.api('finish',self.i,self.t,'completed','');self.assertNotEqual(p.returncode,0)
        self.assertTrue(self.lock.is_symlink())
    def running(self):
        return 'printf "%s\\n" "$$" >"$TEST_HOME/executing"\ntrap "" TERM\nwhile :; do sleep .1; done'
    def test_intent_is_durable_and_idempotent(self):
        self.prepared();before=(self.op/'state.json').read_bytes();s=json.loads(before)
        self.assertEqual(s['platformPreflight']['phase'],'STOP_INTENT');self.assertTrue(s['platformPreflight']['mutationStarted'])
        p=self.intent();self.assertEqual(p.returncode,0,p.stderr);self.assertEqual((self.op/'state.json').read_bytes(),before)
        q=self.intent(nonce='3'*32);self.assertNotEqual(q.returncode,0);self.assertEqual((self.op/'state.json').read_bytes(),before)
    def test_wrong_manifest_or_owner_cannot_write_intent(self):
        self.begin();before=(self.op/'state.json').read_bytes()
        for p in [self.intent(sha='b'*64),self.intent(pid=2147483646)]:self.assertNotEqual(p.returncode,0)
        self.assertEqual((self.op/'state.json').read_bytes(),before)
    def test_missing_ack_refuses_intent(self):
        self.begin();s=json.loads((self.op/'state.json').read_text());s['acknowledged']=False;(self.op/'state.json').write_text(json.dumps(s))
        p=self.intent();self.assertNotEqual(p.returncode,0);self.assertEqual(self.readstate()['platformPreflight']['phase'],'PREPARED')
    def test_helper_before_intent_never_executes(self):
        self.begin();p=self.native_launch('touch "$TEST_HOME/forbidden"');self.assertEqual(p.wait(timeout=15),74)
        self.assertFalse((self.home/'forbidden').exists())
    def test_wrong_stop_nonce_never_executes(self):
        self.prepared();p=self.native_launch('touch "$TEST_HOME/forbidden"',extra={'BRORAY_PREFLIGHT_STOP_NONCE':'3'*32})
        self.assertEqual(p.wait(timeout=15),74);self.assertFalse((self.home/'forbidden').exists())
    def test_cooperative_mode_cannot_run_stop_job(self):
        self.prepared();p=self.native_launch('touch "$TEST_HOME/forbidden"',mode='')
        self.assertEqual(p.wait(timeout=15),74);self.assertFalse((self.home/'forbidden').exists())
    def test_helper_reads_intent_before_first_action(self):
        self.prepared()
        body='jq -e \' .platformPreflight.phase=="STOP_INTENT" and .platformPreflight.mutationStarted==true \' "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/state.json" >/dev/null\n[ "$BRORAY_OPS_PLATFORM_SUPERVISED" = ptrace/1 ]\ntouch "$TEST_HOME/executing"'
        p=self.native_launch(body);self.assertEqual(p.wait(timeout=15),0);self.assertTrue((self.home/'executing').exists())
        self.drain();self.assert_retained()
    def test_live_helper_blocks_drain_cancel_finish(self):
        self.prepared();p=self.native_launch(self.running());self.marker(p)
        for args in [('helpers-drain',self.i,self.t),('cancel',self.i),('finish',self.i,self.t,'completed','')]:
            q=self.api(*args);self.assertNotEqual(q.returncode,0,q.stdout)
        self.assertIsNone(p.poll());p.kill();p.wait(timeout=5);self.drain();self.assert_retained()
    def test_supervisor_kill_stops_helper_tree(self):
        self.prepared();p=self.native_launch(self.running());self.marker(p)
        records=json.loads(self.ledger().read_text())['children'];self.assertGreaterEqual(len(records),1)
        p.kill();p.wait(timeout=5);self.drain()
        for child in records:self.assertNotEqual(ticks(child['pid']),child['startTicks'])
        self.assert_retained()
    def test_timeout_does_not_touch_unrelated_xray_process(self):
        self.prepared();xray=subprocess.Popen(['/bin/sleep','30']);self.addCleanup(lambda:xray.poll() is None and xray.terminate())
        before=ticks(xray.pid);p=self.native_launch(self.running(),timeout=2)
        self.assertEqual(p.wait(timeout=15),124);self.drain()
        self.assertIsNone(xray.poll());self.assertEqual(ticks(xray.pid),before)
        xray.terminate();xray.wait(timeout=5);self.assert_retained()
    def test_detached_descendant_cannot_survive_helper_exit(self):
        self.prepared()
        p=self.native_launch('setsid /bin/ash -c \'echo $$ >"$TEST_HOME/detached"; trap "" TERM; while :; do sleep .1; done\' &\nwhile [ ! -s "$TEST_HOME/detached" ]; do sleep .05; done\ntouch "$TEST_HOME/executing"')
        self.assertEqual(p.wait(timeout=15),0);self.assertTrue((self.home/'detached').exists())
        self.drain();self.assertFalse(Path('/proc',(self.home/'detached').read_text().strip()).exists());self.assert_retained()
    def test_same_stop_helper_cannot_be_replayed(self):
        self.prepared();p=self.native_launch('touch "$TEST_HOME/first"');self.assertEqual(p.wait(timeout=15),0);self.drain()
        q=self.native_launch('touch "$TEST_HOME/replayed"');self.assertEqual(q.wait(timeout=15),74)
        self.assertFalse((self.home/'replayed').exists());self.assert_retained()
    def test_missing_ledger_blocks_recovery(self):
        self.prepared();p=self.native_launch(self.running());self.marker(p);ledger=self.ledger()
        p.kill();p.wait(timeout=5);time.sleep(.05);self.reap();saved=ledger.read_bytes();ledger.unlink()
        q=self.api('helpers-drain',self.i,self.t);self.assertNotEqual(q.returncode,0)
        self.assert_retained();ledger.write_bytes(saved);self.drain()
    def test_live_foreign_pid_in_ledger_is_not_killed_or_ignored(self):
        self.prepared();p=self.native_launch(self.running());self.marker(p);ledger=self.ledger()
        p.kill();p.wait(timeout=5);time.sleep(.05);self.reap();saved=ledger.read_bytes();data=json.loads(saved)
        foreign=subprocess.Popen(['/bin/sleep','30']);self.addCleanup(lambda:foreign.poll() is None and foreign.terminate())
        data['children']=[{'pid':foreign.pid,'startTicks':ticks(foreign.pid),'bootId':data['bootId']}];ledger.write_text(json.dumps(data))
        q=self.api('helpers-drain',self.i,self.t);self.assertNotEqual(q.returncode,0);self.assertIsNone(foreign.poll())
        foreign.terminate();foreign.wait(timeout=5);ledger.write_bytes(saved);self.drain();self.assert_retained()
    def test_owner_death_closes_gate_and_kills_only_descendants(self):
        script=self.home/'owner.sh';script.write_text('set -eu\n. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"\nbroray_ops_preflight_admit "$TEST_SHA"\nbroray_ops_preflight_stop_intent "$TEST_SHA"\nbroray_ops_run_platform_stop_helper 20 -- /bin/ash -c \'echo $$ >"$TEST_HOME/executing"; trap "" TERM; while :; do sleep .1; done\'\n')
        f=open(self.home/'owner.err','w+');owner=subprocess.Popen(['/bin/ash',str(script)],env=self.env,stdout=subprocess.DEVNULL,stderr=f,start_new_session=True)
        self.addCleanup(f.close);self.processes.append(owner);self.marker(owner)
        op=self.operation();r=json.loads((op/'owner.json').read_text());self.i=r['operationId'];self.t=r['token'];self.op=op
        self.assertEqual(r['owner']['pid'],owner.pid)
        # Kill just the parent, not its process group: test actual orphan handling.
        owner.kill();owner.wait(timeout=5);self.drain();self.assert_retained()
        q=self.api('platform-preflight-begin',SHA,os.getpid(),'4'*32);self.assertNotEqual(q.returncode,0)
    def test_client_lost_intent_reply_does_not_run_twice(self):
        raw=(CODE/'lib/operation-client.sh').read_text().replace('broray_ops_call()','broray_ops_call_original()',1)
        shim=self.home/'shim.sh';shim.write_text(raw+'''
broray_ops_call() {
 local out rc
 rc=0;out="$(broray_ops_call_original "$@")" || rc=$?
 if [ "$1" = platform-preflight-stop-intent ] && [ "$rc" = 0 ] && [ ! -e "$TEST_HOME/dropped" ]; then touch "$TEST_HOME/dropped";return 0;fi
 [ -z "$out" ] || printf '%s\\n' "$out"
 return "$rc"
}
''')
        body='. "'+str(shim)+'"\nbroray_ops_preflight_admit "$TEST_SHA" || exit $?\nbroray_ops_preflight_stop_intent "$TEST_SHA" || exit $?\nbroray_ops_run_platform_stop_helper 10 -- /bin/ash -c \'echo once >>"$TEST_HOME/executions"\'\n'
        p=self.shell(body,timeout=35);self.assertEqual(p.returncode,0,(p.stdout,p.stderr));self.assertEqual((self.home/'executions').read_text(),'once\n')
        self.assertTrue((self.home/'dropped').exists());self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT')
    def test_guard_write_failure_never_opens_helper_gate(self):
        shim=self.home/'guard-shim';shim.write_text('#!/bin/ash\nif [ "${1:-}" = --replace-file ] && grep -q STOP_INTENT "$2"; then exit 74; fi\nexec '+str(GUARD)+' "$@"\n');shim.chmod(0o755)
        p=self.shell('broray_ops_preflight_admit "$TEST_SHA" || exit $?\nbroray_ops_preflight_stop_intent "$TEST_SHA" || exit $?\nbroray_ops_run_platform_stop_helper 10 -- /bin/ash -c \'touch "$TEST_HOME/forbidden"\'',env={**self.env,'BRORAY_OPS_GUARD':str(shim)},timeout=35)
        self.assertNotEqual(p.returncode,0);self.assertFalse((self.home/'forbidden').exists())
        self.assertEqual(self.readstate()['platformPreflight']['phase'],'PREPARED')
    def test_incompatible_old_supervisor_refuses_before_exec(self):
        fake=self.home/'old-supervisor';fake.write_text('#!/bin/ash\necho old-supervisor\n');fake.chmod(0o755)
        body='broray_ops_preflight_admit "$TEST_SHA" || exit $?\nbroray_ops_preflight_stop_intent "$TEST_SHA" || exit $?\nbroray_ops_run_platform_stop_helper 10 -- /bin/ash -c \'touch "$TEST_HOME/forbidden"\''
        p=self.shell(body,env={**self.env,'BRORAY_OPS_SUPERVISOR':str(fake)})
        self.assertNotEqual(p.returncode,0);self.assertFalse((self.home/'forbidden').exists());self.assertTrue(self.lock.is_symlink())

    def test_unconfirmed_retry_fsync_keeps_command_gate_closed(self):
        shim=self.home/'sync-fail-guard'
        shim.write_text('#!/bin/ash\nif [ "${1:-}" = --sync-state ]; then touch "$TEST_HOME/sync-attempt"; exit 74; fi\nif [ "${1:-}" = --replace-file ] && grep -q STOP_INTENT "$2"; then '+str(GUARD)+' "$@" || exit $?; exit 74; fi\nexec '+str(GUARD)+' "$@"\n');shim.chmod(0o755)
        body='broray_ops_preflight_admit "$TEST_SHA" || exit $?\nbroray_ops_preflight_stop_intent "$TEST_SHA" || :\nbroray_ops_preflight_stop_intent "$TEST_SHA" || exit $?\nbroray_ops_run_platform_stop_helper 10 -- /bin/ash -c \'touch "$TEST_HOME/forbidden"\''
        p=self.shell(body,env={**self.env,'BRORAY_OPS_GUARD':str(shim)},timeout=35)
        self.assertNotEqual(p.returncode,0);self.assertTrue((self.home/'sync-attempt').exists())
        self.assertFalse((self.home/'forbidden').exists());self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT')
    def test_terminal_flag_never_releases_stop_intent(self):
        self.prepared();s=json.loads((self.op/'state.json').read_text());s['running']=False;s['state']='completed'
        (self.op/'state.json').write_text(json.dumps(s));self.assert_retained()
        q=self.api('recover');self.assertNotEqual(q.returncode,0);self.assertTrue(self.lock.is_symlink())

if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(StopIntent))
    print('STOP_INTENT_RECEIPT '+json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'routerAccess':False,'realUpdaterStopped':False,'platformFilesChanged':False}),flush=True)
    raise SystemExit(0 if result.wasSuccessful() else 1)
