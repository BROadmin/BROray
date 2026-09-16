"""A bounded NDMC runner nested under the actual protected route tracer."""
import ctypes,json,subprocess,unittest
from pathlib import Path
from test_route_entry import ROOT,RouteEntry
class RouteNdmcIntegration(unittest.TestCase):
    setUp=RouteEntry.setUp
    tearDown=RouteEntry.tearDown
    wait_cli=RouteEntry.wait_cli
    run_cli=RouteEntry.run_cli
    def test_native_timeout_under_protected_route_supervision(self):
        self.env['BRORAY_NDMC_RUNNER']=str(ROOT/'.local/bin/linux-ndmc-run')
        fixture=self.app/'bin/ndmc-fixture'
        fixture.write_text('#!/bin/ash\nsetsid /bin/ash -c \'sleep 6; echo BAD >"$BRORAY_ROOT/late"\' &\nwait\n');fixture.chmod(0o700)
        (self.app/'lib/routes-download.sh').write_text('''broray_routes_check_run() {
    . "$BRORAY_ROOT/lib/routes-router-config.sh"
    rc=0
    broray_routes_config_ndmc_capture "$BRORAY_ROOT/bin/ndmc-fixture" 'show running-config' "$BRORAY_ROOT/out" "$BRORAY_ROOT/err" 1 || rc=$?
    [ "$rc" = 124 ] || return 95
    echo CHANGED >"$BRORAY_ROOT/changed"
}
''')
        p=self.run_cli();self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertTrue((self.app/'changed').exists());self.assertFalse((self.app/'late').exists())
        self.assertFalse(self.lock.exists());self.assertFalse(self.lock.is_symlink())
if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    Path('/opt/bin').mkdir(parents=True,exist_ok=True)
    subprocess.run(['/bin/busybox','--install','-s','/opt/bin'],check=True)
    if not Path('/opt/bin/jq').exists():Path('/opt/bin/jq').symlink_to('/usr/bin/jq')
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(RouteNdmcIntegration))
    (ROOT/'docs/evidence/route-ndmc-integration-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
