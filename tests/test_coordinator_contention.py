"""Bounded client retries against a real Linux flock, with no router access."""
import json,os,subprocess,time,unittest
from test_supervisor_integration import Integration,ROOT,GUARD,APP

class Contention(unittest.TestCase):
    setUpFixture=Integration.setUpFixture
    tearDown=Integration.tearDown
    call=Integration.call
    begin=Integration.begin
    finish=Integration.finish
    def setUp(self): Integration.setUp(self)
    def hold(self,seconds):
        p=subprocess.Popen([str(GUARD),str(self.state/'operations.guard'),'/bin/ash','-c',f'echo READY; sleep {seconds}'],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.processes.append(p);self.assertEqual(p.stdout.readline(),b'READY\n');return p
    def client(self,command,expected=0):
        start=time.monotonic()
        p=subprocess.run(['/bin/ash','-c','. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call '+command],env=self.env,capture_output=True,timeout=12)
        self.assertEqual(p.returncode,expected,(p.stdout,p.stderr))
        return p,time.monotonic()-start
    def test_cancel_waits_for_short_contention_and_preserves_fence(self):
        self.begin();fence=(self.temp/'global.lock').readlink();holder=self.hold(4)
        p,elapsed=self.client('cancel '+self.id)
        self.assertTrue(json.loads(p.stdout)['cancelRequested']);self.assertGreater(elapsed,3.5)
        holder.communicate(timeout=2);self.assertEqual((self.temp/'global.lock').readlink(),fence)
        self.finish('aborted','CANCELLED')
    def test_stop_all_waits_and_records_cancel(self):
        self.begin();holder=self.hold(4)
        p,_=self.client('stop-background');d=json.loads(p.stdout)
        self.assertTrue(d['automationPaused']);self.assertTrue(d['operations'][0]['cancelRequested'])
        holder.communicate(timeout=2);self.finish('aborted','CANCELLED')
    def test_permanent_contention_is_bounded_without_unlink_or_mutation(self):
        self.call('initialize');guard=self.state/'operations.guard';before=guard.stat().st_ino;holder=self.hold(10)
        p,elapsed=self.client('pause',75)
        self.assertEqual(p.stdout,b'');self.assertGreater(elapsed,5.5);self.assertLess(elapsed,9)
        self.assertEqual(guard.stat().st_ino,before);self.assertFalse((self.state/'background-automation.json').exists())
        holder.communicate(timeout=5)
    def test_structured_publication_failure_is_not_replayed(self):
        fake=self.temp/'structured-guard';count=self.temp/'calls'
        fake.write_text('#!/bin/ash\necho call >>"'+str(count)+'"\nprintf \'{"ok":false,"errorCode":"PUBLICATION_UNCONFIRMED"}\\n\'\nexit 75\n');fake.chmod(0o700)
        self.env['BRORAY_OPS_GUARD']=str(fake)
        p,_=self.client('pause',75);self.assertEqual(count.read_text().splitlines(),['call'])
        self.assertEqual(json.loads(p.stdout)['errorCode'],'PUBLICATION_UNCONFIRMED')
    def test_unsafe_guard_is_not_retried_or_replaced(self):
        guard=self.state/'operations.guard';guard.write_text('KEEP');guard.chmod(0o644)
        p,elapsed=self.client('pause',74)
        self.assertEqual(p.stdout,b'');self.assertLess(elapsed,2);self.assertEqual(guard.read_text(),'KEEP')

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Contention))
    (ROOT/'docs/evidence/coordinator-contention-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
