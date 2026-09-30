"""Large route fixtures: exact preservation, bounded pure local comparison."""
import time,unittest,subprocess
import test_field_regressions as field

class RouteScaling(unittest.TestCase):
    def fixture(self,count):
        f=field.FieldRegressions();f.setUp();self.addCleanup(f.doCleanups)
        f.route_fixture(count=count);return f
    def call(self,f,body):
        script='. "$1"; CURRENT_OPERATION_DIR="$2"; CURRENT_OPERATION_LOG="$2/log"; '+body
        begin=time.monotonic()
        p=subprocess.run(['/bin/ash','-c',script,'test',str(f.library),str(f.operation)],env=f.env,capture_output=True,timeout=30)
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        print('ROUTE_SCALE_SECONDS='+str(round(time.monotonic()-begin,3)),flush=True)
    def preserve(self,count):
        f=self.fixture(count);before=f.running.read_bytes()
        self.call(f,'routes_capture && routes_restore_captured && routes_verify_captured')
        self.assertEqual(f.running.read_bytes(),before);self.assertEqual(f.startup.read_bytes(),before)
        self.assertFalse(f.commands.exists());self.assertEqual(len((f.operation/'managed-routes.before').read_text().splitlines()),count)
    def test_3500_unchanged_routes_preserved(self):self.preserve(3500)
    def test_10000_unchanged_routes_preserved(self):self.preserve(10000)
    def test_10000_restore_only_three_missing_routes(self):
        f=self.fixture(10000);self.call(f,'routes_capture')
        missing=[f.routes[0],f.routes[4999],f.routes[-1]]
        f.running.write_text('\n'.join(x for x in f.routes if x not in missing)+'\n'+f.foreign)
        self.call(f,'routes_restore_captured && routes_verify_captured')
        self.assertEqual(set(f.commands.read_text().splitlines()),set(missing+['system configuration save']))
        self.assertEqual(len(f.commands.read_text().splitlines()),4)
        self.assertIn(f.foreign,f.running.read_text());self.assertEqual(f.running.read_bytes(),f.startup.read_bytes())
    def test_exact_membership_rejects_prefix_substrings_and_missing_last_line(self):
        f=self.fixture(1);snap=f.operation/'managed-routes.before';snap.write_text('ip route 10.0.0.0 255.255.255.0 Proxy0 1200\n')
        for content in ['ip route 10.0.0.0 255.255.255.0 Proxy0 12000\n','extra ip route 10.0.0.0 255.255.255.0 Proxy0 1200\n','foreign\n']:
            f.running.write_text(content)
            p=f.routes_call('routes_snapshot_present_in "$TEST_RUNNING"');self.assertNotEqual(p.returncode,0)

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
