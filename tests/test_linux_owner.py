"""Owner identity against actual /proc in the isolated Linux guest."""
import json, os, subprocess, tempfile, time, unittest
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
LIB=ROOT/'implementation/runtime/app/lib/operation-owner.sh'

@unittest.skipUnless(os.name=='posix','Linux guest only')
class Owner(unittest.TestCase):
    def shell(self,script,*args):
        env=os.environ.copy()
        for key in list(env):
            if key.startswith('BRORAY_OPS_TEST'):env.pop(key)
        code='OPS_PROC=/proc; OPS_APP=/opt/broray; . "$1"; shift; '+script
        return subprocess.run(['/bin/ash','-c',code,'owner-test',str(LIB),*args],env=env,capture_output=True,timeout=8)
    def capture(self,pid):
        result=self.shell('broray_ops_capture_owner "$1"',str(pid))
        self.assertEqual(result.returncode,0,result.stderr)
        return json.loads(result.stdout)
    def classify(self,owner):
        result=self.shell('broray_ops_classify_owner "$1"; printf "%s:%s" "$OPS_OWNER_STATUS" "$OPS_OWNER_REASON"',json.dumps(owner))
        self.assertEqual(result.returncode,0,result.stderr)
        return result.stdout.decode()
    def test_actual_identity_and_exit(self):
        child=subprocess.Popen(['/bin/sleep','30'])
        try:
            owner=self.capture(child.pid)
            self.assertEqual(owner['pid'],child.pid)
            self.assertEqual(self.classify(owner),'ACTIVE:identity_matches')
            child.terminate();child.wait(timeout=3)
            self.assertEqual(self.classify(owner),'STALE:absent')
        finally:
            if child.poll() is None:child.kill();child.wait()
    def test_mismatched_birth_never_signals_live_process(self):
        child=subprocess.Popen(['/bin/sleep','30'])
        try:
            owner=self.capture(child.pid);owner['startTicks']=str(int(owner['startTicks'])+1)
            self.assertEqual(self.classify(owner),'STALE:pid_reused')
            self.assertIsNone(child.poll())
        finally:child.kill();child.wait()
    def test_unconfirmed_executable_stays_ambiguous(self):
        owner=self.capture(os.getpid());owner['executable']='/unrelated/binary'
        self.assertEqual(self.classify(owner),'AMBIGUOUS:identity_changed')
    def test_parentheses_in_proc_comm(self):
        temp=Path(tempfile.mkdtemp(prefix='owner-stat-'))
        fields=['S']+['0']*18+['987654']+['0']*20
        (temp/'stat').write_text('77 (name with ) parentheses) '+' '.join(fields)+'\n')
        result=self.shell('broray_ops_start_ticks "$1"',str(temp))
        self.assertEqual(result.stdout.strip(),b'987654')
    def test_invalid_identity_is_ambiguous(self):
        self.assertEqual(self.classify({'pid':1}),'AMBIGUOUS:invalid_identity')

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Owner))
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'Isolated Linux guest, actual proc identities; mismatched birth simulated','routerAccessed':False}
    (ROOT/'docs/evidence/linux-owner-tests.json').write_text(json.dumps(report,indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
