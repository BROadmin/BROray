"""Read-only capability collector in a fixture; never executes on a router."""
import hashlib,json,os,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
SCRIPT=ROOT/'implementation/tests/router-preflight.sh'

class Preflight(unittest.TestCase):
    def setUp(self):
        self.root=Path(tempfile.mkdtemp(prefix='preflight-'))
        (self.root/'proc/sys/kernel/random').mkdir(parents=True)
        (self.root/'proc/sys/kernel/random/boot_id').write_text('synthetic-boot')
        (self.root/'proc/uptime').write_text('12.5 0.0\n')
        (self.root/'opt/broray/web-new').mkdir(parents=True)
        (self.root/'opt/var/lock/broray').mkdir(parents=True)
    def call(self):
        def snapshot():return {str(p.relative_to(self.root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in self.root.rglob('*') if p.is_file() and not p.is_symlink()}
        before=snapshot()
        p=subprocess.run(['/bin/sh',str(SCRIPT)],env={**os.environ,'BRORAY_PREFLIGHT_FIXTURE_ROOT':str(self.root)},capture_output=True,timeout=10)
        self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(before,snapshot())
        result=json.loads(p.stdout);self.assertTrue(result['readOnly']);self.assertTrue(result['testFixture']);self.assertFalse(result['readyToDeploy'])
        return result
    def test_partial_snapshot_is_explicit(self):
        report=self.call();self.assertEqual(report['globalFenceShape'],'absent');self.assertEqual(report['uptimeSeconds'],12.5)
        self.assertIn('VPN continuity',report['notChecked'])
    def test_build_secret_is_rejected(self):
        canary='https://private.example/SECRET'
        (self.root/'opt/broray/web-new/build.json').write_text(json.dumps({'appVersion':'3.1.0','candidateId':canary,'secret':canary}))
        report=self.call();self.assertNotIn(canary,json.dumps(report));self.assertIsNone(report['build']['candidateId'])
    def test_foreign_lock_is_described_without_following(self):
        lock=self.root/'opt/var/lock/broray/global-operation.lock';lock.symlink_to('/not-an-existing-target')
        self.assertEqual(self.call()['globalFenceShape'],'symlink');self.assertTrue(lock.is_symlink())

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Preflight))
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'Linux guest, synthetic read-only router-preflight fixture','routerAccessed':False}
    (ROOT/'docs/evidence/preflight-tests.json').write_text(json.dumps(report,indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
