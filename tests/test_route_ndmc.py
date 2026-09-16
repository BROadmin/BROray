"""NDMC lane ownership, using local commands and no router configuration."""
import json,os,shutil,subprocess,tempfile,time,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]

class RouteNdmc(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='route-ndmc-',dir=ROOT/'.local'))
        self.app=self.temp/'app';shutil.copytree(ROOT/'implementation/runtime/app/lib',self.app/'lib')
        (self.app/'run').mkdir();self.lane=self.app/'run/routes-router-ndmc.lock'
        self.env=os.environ|{'BRORAY_ROOT':str(self.app),'BRORAY_OPS_ASH':'/bin/ash',
            'BRORAY_NDMC_RUNNER':str(ROOT/'.local/bin/linux-ndmc-run'),
            'BRORAY_OPS_GUARD':str(ROOT/'.local/bin/linux-guard')}
    def tearDown(self):
        assert self.temp.resolve().parent==(ROOT/'.local').resolve();shutil.rmtree(self.temp)
    def test_unknown_legacy_lane_is_preserved(self):
        self.lane.mkdir();sentinel=self.lane/'foreign';sentinel.write_text('KEEP')
        p=subprocess.run(['/bin/ash','-c','''
. "$BRORAY_ROOT/lib/routes-router-config.sh"
broray_routes_config_ndmc_capture /bin/true 'show running-config' "$BRORAY_ROOT/out" "$BRORAY_ROOT/err" 1
'''],env=self.env,capture_output=True,timeout=20)
        self.assertTrue(sentinel.exists(),'NDMC reader removed an unknown lane generation')
        self.assertEqual(sentinel.read_text(),'KEEP');self.assertNotEqual(p.returncode,0)

    def runner(self,command,limit=1,wait=1):
        return [str(ROOT/'.local/bin/linux-ndmc-run'),str(self.lane)+'.guard',str(self.lane),str(wait),str(limit),'/bin/ash',command]

    def test_exit_status_and_output(self):
        p=subprocess.run(self.runner('echo GOOD; echo ERROR >&2; exit 23'),capture_output=True,timeout=10)
        self.assertEqual((p.returncode,p.stdout,p.stderr),(23,b'GOOD\n',b'ERROR\n'))

    def test_timeout_drains_detached_descendants(self):
        p=subprocess.run(self.runner('setsid /bin/ash -c "sleep 4; echo BAD >$BRORAY_ROOT/late" & wait'),env=self.env,capture_output=True,timeout=10)
        self.assertEqual(p.returncode,124,p.stderr);time.sleep(4)
        self.assertFalse((self.app/'late').exists())
        self.assertEqual(subprocess.run(self.runner('exit 0')).returncode,0)

    def test_success_drains_background_descendants(self):
        p=subprocess.run(self.runner('setsid /bin/ash -c "sleep 3; echo BAD >$BRORAY_ROOT/late" & sleep 1; exit 0',3),env=self.env,capture_output=True,timeout=10)
        self.assertEqual(p.returncode,0,p.stderr);time.sleep(3)
        self.assertFalse((self.app/'late').exists())

    def test_lane_serializes_and_preserves_inode(self):
        p=subprocess.Popen(self.runner('echo READY >"$BRORAY_ROOT/ready"; sleep 3',5),env=self.env)
        try:
            deadline=time.monotonic()+5
            while not (self.app/'ready').exists():
                self.assertIsNone(p.poll());self.assertLess(time.monotonic(),deadline);time.sleep(.02)
            inode=Path(str(self.lane)+'.guard').stat().st_ino
            other=subprocess.run(self.runner('echo BAD >"$BRORAY_ROOT/late"'),env=self.env,timeout=5)
            self.assertEqual(other.returncode,125);self.assertFalse((self.app/'late').exists())
            self.assertEqual(p.wait(timeout=8),0)
            self.assertEqual(subprocess.run(self.runner('exit 0')).returncode,0)
            self.assertEqual(Path(str(self.lane)+'.guard').stat().st_ino,inode)
        finally:
            if p.poll() is None:p.terminate();p.wait(timeout=8)

    def test_foreign_guard_symlink_and_content_rejected(self):
        target=self.app/'foreign';target.write_text('KEEP')
        guard=Path(str(self.lane)+'.guard');guard.symlink_to(target)
        self.assertEqual(subprocess.run(self.runner('exit 0')).returncode,74)
        self.assertEqual(target.read_text(),'KEEP');guard.unlink();guard.write_text('KEEP')
        self.assertEqual(subprocess.run(self.runner('exit 0')).returncode,74)
        self.assertEqual(guard.read_text(),'KEEP')

    def test_wrapper_keeps_command_authorization(self):
        p=subprocess.run(['/bin/ash','-c','''
. "$BRORAY_ROOT/lib/routes-router-preflight.sh"
BRORAY_ROUTES_PREFLIGHT_NDMC=/bin/true
broray_routes_preflight_ndmc 'system reboot' "$BRORAY_ROOT/out" "$BRORAY_ROOT/err" 1
'''],env=self.env,capture_output=True,timeout=5)
        self.assertEqual(p.returncode,126,p.stderr)

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(RouteNdmc))
    (ROOT/'docs/evidence/route-ndmc-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
