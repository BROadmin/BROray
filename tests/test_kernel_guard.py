"""Real Linux process/lock tests; only private VM temporary paths are used."""
import json,os,signal,subprocess,tempfile,time,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
GUARD=ROOT/'.local/bin/linux-guard'

@unittest.skipUnless(os.name=='posix','Linux guest only')
class Guard(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='kernel-guard-'))
        self.lock=self.temp/'guard'
    def run_guard(self,*command):
        return subprocess.run([str(GUARD),str(self.lock),*command],capture_output=True,timeout=8)
    def test_exec_holds_lock_and_death_releases(self):
        p=subprocess.Popen([str(GUARD),str(self.lock),'/bin/sh','-c','echo READY; exec sleep 30'],stdout=subprocess.PIPE)
        try:
            self.assertEqual(p.stdout.readline().strip(),b'READY')
            self.assertEqual(self.run_guard('/bin/true').returncode,75)
            p.kill();p.wait(timeout=3)
            self.assertEqual(self.run_guard('/bin/true').returncode,0)
            self.assertTrue(self.lock.is_file())
        finally:
            if p.poll() is None:p.kill();p.wait()
    def test_command_failure_releases_lock(self):
        self.assertEqual(self.run_guard('/bin/false').returncode,1)
        self.assertEqual(self.run_guard('/bin/true').returncode,0)
    def test_symlink_rejected(self):
        dest=self.temp/'foreign';dest.write_text('KEEP')
        self.lock.symlink_to(dest)
        self.assertEqual(self.run_guard('/bin/true').returncode,74)
        self.assertEqual(dest.read_text(),'KEEP')
    def test_hardlink_rejected(self):
        self.lock.touch(mode=0o600);os.link(self.lock,self.temp/'second')
        self.assertEqual(self.run_guard('/bin/true').returncode,74)
    def test_public_permissions_rejected(self):
        self.lock.touch();self.lock.chmod(0o644)
        self.assertEqual(self.run_guard('/bin/true').returncode,74)
    def test_missing_command_releases(self):
        self.assertEqual(self.run_guard('/definitely/missing').returncode,74)
        self.assertEqual(self.run_guard('/bin/true').returncode,0)
    def test_directory_rejected(self):
        self.lock.mkdir()
        self.assertEqual(self.run_guard('/bin/true').returncode,74)
    def test_multiple_writers_do_not_overlap(self):
        critical=self.temp/'critical'
        script='mkdir "$1" || exit 99; sleep 0.1; rmdir "$1"'
        processes=[subprocess.Popen([str(GUARD),str(self.lock),'/bin/sh','-c',script,'guard-test',str(critical)]) for _ in range(6)]
        self.assertEqual([p.wait(timeout=8) for p in processes],[0]*6)
    def prepare_fence(self):
        op=self.temp/'operation';fence=op/'fence';fence.mkdir(parents=True)
        for name in ['owner.json','state.json']:(op/name).write_text('{}')
        for name in ['owner.json','pid','scope','action','bundle','startedAt']:(fence/name).write_text('test')
        return fence
    def publish(self,fence):
        return subprocess.run([str(GUARD),'--publish-fence',str(fence),str(self.lock)],capture_output=True,timeout=5)
    def test_publication_does_not_replace_existing_directory(self):
        fence=self.prepare_fence();self.lock.mkdir();(self.lock/'foreign').write_text('KEEP')
        self.assertEqual(self.publish(fence).returncode,75)
        self.assertEqual([p.name for p in self.lock.iterdir()],['foreign'])
    def test_publication_is_complete_and_nonreplacing(self):
        fence=self.prepare_fence()
        self.assertEqual(self.publish(fence).returncode,0)
        self.assertTrue(self.lock.is_symlink())
        self.assertEqual(self.publish(fence).returncode,75)
        self.assertEqual(self.lock.readlink(),fence)
    def test_incomplete_fence_never_becomes_visible(self):
        fence=self.prepare_fence();(fence/'owner.json').unlink()
        self.assertEqual(self.publish(fence).returncode,74)
        self.assertFalse(self.lock.exists());self.assertFalse(self.lock.is_symlink())

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Guard))
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'Isolated Linux QEMU guest, native x86_64 guard, real kernel processes','network':'disabled','routerAccessed':False}
    (ROOT/'docs/evidence/linux-kernel-guard-tests.json').write_text(json.dumps(report,indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
