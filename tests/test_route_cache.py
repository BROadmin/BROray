"""Configured-route cache preserves unknown legacy locks."""
import json,subprocess,unittest
from test_route_ndmc import ROOT,RouteNdmc
class RouteCache(unittest.TestCase):
    setUp=RouteNdmc.setUp
    tearDown=RouteNdmc.tearDown
    def test_cache_refresh_and_fresh_read_keep_guard_inode(self):
        fixture=self.app/'running-config';fixture.write_text('!\nip route 203.0.113.0 255.255.255.0 Proxy0\n')
        self.env['BRORAY_ROUTES_CONFIG_FIXTURE']=str(fixture)
        cmd=['/bin/ash','-c','. "$BRORAY_ROOT/lib/routes-router-config.sh"; broray_routes_config_get_cache']
        p=subprocess.run(cmd,env=self.env,capture_output=True,timeout=20)
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        cache=self.app/'run/routes-router-config-cache.json';data=json.loads(cache.read_bytes())
        self.assertEqual(data['source'],'running-config');self.assertEqual(len(data['routes']),1)
        guard=self.app/'run/routes-router-config.lock.guard';inode=guard.stat().st_ino
        self.assertEqual(subprocess.run(cmd,env=self.env,capture_output=True,timeout=20).returncode,0)
        self.assertEqual(guard.stat().st_ino,inode)
    def test_unknown_legacy_cache_lock_preserved(self):
        lock=self.app/'run/routes-router-config.lock';lock.mkdir()
        (lock/'foreign').write_text('KEEP')
        p=subprocess.run(['/bin/ash','-c','''
. "$BRORAY_ROOT/lib/routes-router-config.sh"
broray_routes_config_fetch() { return 1; }
broray_routes_config_get_cache
'''],env=self.env,capture_output=True,timeout=20)
        self.assertTrue((lock/'foreign').exists(),'Cache reader removed unknown lock')
        self.assertEqual((lock/'foreign').read_text(),'KEEP');self.assertNotEqual(p.returncode,0)
if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(RouteCache))
    (ROOT/'docs/evidence/route-cache-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
