"""Real shell executor handoff with native helpers and no service installation."""
import ctypes,json,os,subprocess,time,unittest,uuid
from pathlib import Path
from test_supervisor_integration import Integration,ROOT,APP

class ActualHandoff(unittest.TestCase):
    setUpFixture=Integration.setUpFixture
    setUp=Integration.setUp
    tearDown=Integration.tearDown
    call=Integration.call
    begin=Integration.begin
    def wait_file(self,path,p):
        until=time.monotonic()+15
        while time.monotonic()<until:
            if path.exists():return
            if p.poll() is not None:self.fail(p.communicate())
            time.sleep(.02)
        self.fail('worker did not reach its guarded stage')
    def worker(self,body):
        nonce=uuid.uuid4().hex;self.nonce=nonce
        script=self.temp/'worker.sh'
        script.write_text('set -eu\n. "$BRORAY_ROOT/lib/operation-client.sh"\nbroray_ops_accept_handoff "$TEST_HANDOFF_NONCE"\n'+body+'\n')
        p=subprocess.Popen(['/bin/ash',str(script)],env={**self.env,'TEST_HANDOFF_NONCE':nonce,'TEST_WORK':str(self.temp)},stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.processes.append(p);return p
    def transfer(self,p,owner=None):
        self.call('handoff',self.id,self.token,str(owner or os.getpid()),str(p.pid),self.nonce)
    def communicate_worker(self,p,timeout=25):
        # This fixture is a subreaper, unlike the production CGI. Reap only
        # dead, adopted helper children identified in this test's RAM ledgers;
        # leave all direct Popen handles to Popen. Otherwise their zombies
        # would artificially prevent the shell client's helpers-drain.
        until=time.monotonic()+timeout
        protected={child.pid for child in self.processes}
        while time.monotonic()<until:
            try:return p.communicate(timeout=.1)
            except subprocess.TimeoutExpired:pass
            for ledger in self.ram.glob('supervisors/*/*/children.json'):
                try:data=json.loads(ledger.read_text())
                except (FileNotFoundError,json.JSONDecodeError):continue
                for child in data['children']:
                    if child['pid'] in protected:continue
                    try:os.waitpid(child['pid'],os.WNOHANG)
                    except ChildProcessError:pass
        self.fail('worker timeout after fixture reaped its adopted helper children')
    def test_worker_stays_gated_until_transfer(self):
        self.begin(action='xray:update');marker=self.temp/'accepted'
        p=self.worker('echo yes >"$TEST_WORK/accepted"; broray_ops_finish completed')
        time.sleep(.2);self.assertFalse(marker.exists());self.transfer(p)
        out,err=self.communicate_worker(p);self.assertEqual(p.returncode,0,(out,err))
        self.assertTrue(marker.exists());self.assertFalse((self.temp/'global.lock').exists())
    def test_new_native_helper_observes_worker_not_old_parent(self):
        parent=subprocess.Popen(['/bin/sleep','30']);self.processes.append(parent)
        self.begin(parent.pid,action='xray:update')
        p=self.worker('''rc=0
broray_ops_run_helper 25 -- /bin/ash -c 'echo yes >"$TEST_WORK/helper-ready"; trap "" TERM; sleep 60' || rc=$?
[ "$rc" = 130 ]
broray_ops_finish aborted CANCELLED''')
        self.transfer(p,parent.pid);self.wait_file(self.temp/'helper-ready',p)
        parent.kill();parent.wait(timeout=5);time.sleep(.2)
        self.assertIsNone(p.poll())
        self.assertEqual(self.call('classify',self.id)['ownerStatus'],'ACTIVE')
        self.assertEqual(self.call('finish',self.id,self.token,'completed','',expected=2)['errorCode'],'OWNER_CHANGED')
        self.call('cancel',self.id)
        out,err=self.communicate_worker(p);self.assertEqual(p.returncode,0,(out,err))
        self.assertFalse((self.temp/'global.lock').exists())
    def test_parent_client_drops_its_old_authority(self):
        # Both sides use the production shell helpers, including the parent's
        # $$ identity. The child inherits only the pre-transfer invitation.
        worker=self.temp/'child.sh';worker.write_text('set -eu\n. "$BRORAY_ROOT/lib/operation-client.sh"\nbroray_ops_accept_handoff "$TEST_HANDOFF_NONCE"\necho yes >"$TEST_WORK/done"\nbroray_ops_finish completed\n')
        parent=self.temp/'parent.sh';parent.write_text('''set -eu
. "$BRORAY_ROOT/lib/operation-client.sh"
broray_ops_begin routes xray:update xray USER protected
/bin/ash "$TEST_WORK/child.sh" & child=$!
broray_ops_handoff_to "$child" "$TEST_HANDOFF_NONCE"
[ -z "${BRORAY_BACKGROUND_OPERATION_ID:-}" ]
[ -z "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ]
broray_ops_finish completed
wait "$child"
''')
        p=subprocess.Popen(['/bin/ash',str(parent)],env={**self.env,'TEST_HANDOFF_NONCE':uuid.uuid4().hex,'TEST_WORK':str(self.temp)},stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.processes.append(p);out,err=self.communicate_worker(p)
        self.assertEqual(p.returncode,0,(out,err));self.assertTrue((self.temp/'done').exists());self.assertFalse((self.temp/'global.lock').exists())

if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(ActualHandoff))
    (ROOT/'docs/evidence/handoff-integration-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'real Linux shell owners, coordinator/client and native helpers; no service installation','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
