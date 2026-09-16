"""Actual Linux ptrace processes; isolated control callback, no router or network."""
import ctypes,json,os,subprocess,tempfile,time,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2];BINARY=ROOT/'.local/bin/linux-supervisor'
def ticks(pid):
    try:return Path(f'/proc/{pid}/stat').read_text().rsplit(') ',1)[1].split()[19]
    except FileNotFoundError:return None
class Supervisor(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='supervisor-'));self.cancel=self.temp/'cancel.json';self.ledger=self.temp/'children.json'
        self.control=self.temp/'control.sh';self.control.write_text('printf "%s\\t%s\\t%s\\t%s\\n" "$TEST_OWNER_PID" "$TEST_OWNER_TICKS" "$TEST_BOOT" "$TEST_LEDGER"\n')
        self.env={**os.environ,'BRORAY_BACKGROUND_OPERATION_ID':'op-20260915123400-3456-012345abcdef',
          'TEST_OWNER_PID':str(os.getpid()),'TEST_OWNER_TICKS':ticks(os.getpid()),'TEST_BOOT':Path('/proc/sys/kernel/random/boot_id').read_text().strip(),'TEST_LEDGER':str(self.ledger)}
        self.processes=[]
    def tearDown(self):
        for p in self.processes:
            if p.poll() is None:p.kill()
            p.wait(timeout=5)
    def launch(self,script,timeout=20,cooperative=0,term=1,env=None,protected_route=False):
        p=subprocess.Popen([str(BINARY),*(['--protected-route'] if protected_route else []),'/bin/sh',str(self.control),str(self.cancel),str(timeout),str(cooperative),str(term),'--','/bin/sh','-c',script],env={**self.env,**(env or {})},stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.processes.append(p);return p
    def wait_ready(self,p,file):
        until=time.monotonic()+5
        while time.monotonic()<until:
            if file.exists():return
            if p.poll() is not None:self.fail(p.communicate())
            time.sleep(.01)
        self.fail('helper did not become ready')
    def verify_gone(self):
        data=json.loads(self.ledger.read_text());until=time.monotonic()+5
        while time.monotonic()<until:
            live=[]
            for item in data['children']:
                try:os.waitpid(item['pid'],os.WNOHANG)
                except ChildProcessError:pass
                if ticks(item['pid'])==item['startTicks']:live.append(item['pid'])
            if not live:return data
            time.sleep(.01)
        self.fail(f'tracees survived: {live}')
    def test_normal_output_and_exit_code(self):
        p=self.launch("printf '%s' 'literal spaces $HOME'; exit 23")
        out,err=p.communicate(timeout=5);self.assertEqual(p.returncode,23,(out,err));self.assertEqual(out,b'literal spaces $HOME');self.verify_gone()
    def test_protected_route_ignores_old_user_cancel_marker(self):
        self.cancel.write_text('{}')
        p=self.launch('sleep 1; echo completed',protected_route=True)
        out,err=p.communicate(timeout=5)
        self.assertEqual(p.returncode,0,(out,err));self.assertEqual(out,b'completed\n');self.verify_gone()
    def test_protected_route_still_enforces_internal_timeout(self):
        p=self.launch('trap "" TERM; sleep 60',timeout=1,term=1,protected_route=True)
        p.communicate(timeout=6);self.assertEqual(p.returncode,124);self.verify_gone()
    def test_shell_forks_and_execs_complete(self):
        p=self.launch('echo first; sleep 1; echo last')
        out,err=p.communicate(timeout=5);self.assertEqual(p.returncode,0,(out,err));self.assertEqual(out,b'first\nlast\n');self.verify_gone()
    def test_cancel_before_gate_does_not_run_command(self):
        self.cancel.write_text('{}');marker=self.temp/'forbidden'
        p=self.launch(f'echo BAD >"{marker}"');p.communicate(timeout=5)
        self.assertEqual(p.returncode,130);self.assertFalse(marker.exists());self.verify_gone()
    def test_term_cooperative_root(self):
        marker=self.temp/'ready';p=self.launch(f'trap "exit 0" TERM; echo yes >"{marker}"; while :; do sleep 1; done')
        self.wait_ready(p,marker);self.cancel.write_text('{}');p.communicate(timeout=6)
        self.assertEqual(p.returncode,130);self.assertTrue(self.verify_gone()['termSent'])
    def test_ignored_term_uses_kernel_exitkill_for_tree(self):
        marker=self.temp/'ready';p=self.launch(f'trap "" TERM; echo yes >"{marker}"; while :; do sleep 1; done')
        self.wait_ready(p,marker);self.cancel.write_text('{}');p.communicate(timeout=6)
        self.assertEqual(p.returncode,130);data=self.verify_gone();self.assertTrue(data['termSent']);self.assertTrue(data['killTriggered'])
    def test_timeout_is_bounded(self):
        start=time.monotonic();p=self.launch('trap "" TERM; sleep 60',timeout=1);p.communicate(timeout=6)
        self.assertEqual(p.returncode,124);self.assertLess(time.monotonic()-start,6);self.verify_gone()
    def test_session_escape_is_still_killed_and_sentinel_survives(self):
        sentinel=subprocess.Popen(['/bin/sleep','30']);self.processes.append(sentinel)
        marker=self.temp/'ready';p=self.launch(f'setsid /bin/sh -c \'trap "" TERM; sleep 60\' & echo yes >"{marker}"; wait')
        self.wait_ready(p,marker);self.cancel.write_text('{}');p.communicate(timeout=6)
        self.assertEqual(p.returncode,130);self.verify_gone();self.assertIsNone(sentinel.poll())
    def test_tracer_crash_kills_registered_tracees(self):
        marker=self.temp/'ready';p=self.launch(f'sleep 60 & echo yes >"{marker}"; wait')
        self.wait_ready(p,marker);time.sleep(.1);p.kill();p.communicate(timeout=5)
        self.assertEqual(p.returncode,-9);self.verify_gone()
    def test_owner_death_terminates_helper(self):
        owner=subprocess.Popen(['/bin/sleep','30']);self.processes.append(owner)
        marker=self.temp/'ready';p=self.launch(f'echo yes >"{marker}"; sleep 60',env={'TEST_OWNER_PID':str(owner.pid),'TEST_OWNER_TICKS':ticks(owner.pid)})
        self.wait_ready(p,marker);owner.kill();owner.wait(timeout=5);p.communicate(timeout=5)
        self.assertEqual(p.returncode,125);self.verify_gone()
    def test_exec_from_nonleader_thread(self):
        p=self.launch(f'exec "{ROOT}/.local/bin/linux-supervisor-fixture" thread-exec')
        out,err=p.communicate(timeout=8)
        self.assertEqual(p.returncode,19,(out,err));self.assertEqual(out,b'thread-exec-ok');self.verify_gone()
    def test_thread_churn(self):
        p=self.launch(f'exec "{ROOT}/.local/bin/linux-supervisor-fixture" thread-churn')
        out,err=p.communicate(timeout=15)
        self.assertEqual(p.returncode,0,(out,err));self.verify_gone()
    def test_group_stopped_helper_is_cancelled(self):
        marker=self.temp/'ready';p=self.launch(f'echo yes >"{marker}"; kill -STOP $$; sleep 60')
        self.wait_ready(p,marker);time.sleep(.1);self.cancel.write_text('{}');p.communicate(timeout=6)
        self.assertEqual(p.returncode,130);self.verify_gone()

if __name__=='__main__':
    if os.name=='nt':raise SystemExit('Linux guest only')
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0 # PR_SET_CHILD_SUBREAPER, so tests reap their own orphan fixtures.
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Supervisor))
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'Linux ptrace supervisor + synthetic registration callback; actual child processes','routerAccessed':False}
    (ROOT/'docs/evidence/supervisor-tests.json').write_text(json.dumps(report,indent=2)+'\n');raise SystemExit(0 if result.wasSuccessful() else 1)
