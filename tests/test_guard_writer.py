"""A forked native publisher must retain exclusion after coordinator death."""
import ctypes,json,os,subprocess,time,unittest
from test_kernel_guard import Guard,ROOT,GUARD
FIXTURE=ROOT/'.local/bin/linux-guard-writer-fixture'
class Writer(unittest.TestCase):
    setUp=Guard.setUp
    run_guard=Guard.run_guard
    prepare_fence=Guard.prepare_fence
    private_file=Guard.private_file
    def scenario(self,mode):
        ready=self.temp/'ready';release=self.temp/'release'
        if mode=='--replace-file':
            source=self.private_file('new.json','{"complete":true}')
            target=self.private_file('state.json','{"complete":false}')
        else:
            source=self.prepare_fence();target=self.temp/'global.lock'
        p=subprocess.Popen([str(GUARD),str(self.lock),str(FIXTURE),str(GUARD),str(ready),str(release),mode,str(source),str(target)],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        out,err=p.communicate(timeout=5)
        self.assertEqual(p.returncode,-9,(out,err))
        child=int(ready.read_text())
        try:contender=self.run_guard('/bin/true')
        finally:
            release.touch()
            deadline=time.monotonic()+5
            while True:
                done,status=os.waitpid(child,os.WNOHANG)
                if done:break
                if time.monotonic()>deadline:self.fail('owned adopted publisher did not exit')
                time.sleep(.02)
            self.assertEqual(os.waitstatus_to_exitcode(status),0)
        if mode=='--replace-file':self.assertEqual(json.loads(target.read_text()),{'complete':True})
        else:self.assertEqual(target.readlink(),source)
        self.assertEqual(self.run_guard('/bin/true').returncode,0)
        self.assertEqual(contender.returncode,75,'Another coordinator entered while the orphan native publisher still held its inherited descriptor')
    def test_replace_child_retains_guard(self):self.scenario('--replace-file')
    def test_publish_child_retains_guard(self):self.scenario('--publish-fence')
if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Writer))
    (ROOT/'docs/evidence/guard-writer-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'actual Linux fork, parent self-SIGKILL, adopted native publisher and kernel guard','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
