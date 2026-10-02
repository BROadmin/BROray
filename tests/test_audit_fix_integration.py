import ctypes,json,os,shutil,subprocess,unittest
from pathlib import Path
assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
import test_supervisor as supervisor
import test_session_logout_race as sessions
import test_operation_step_helpers as steps
import test_route_resource_locks as routes
import test_preflight_stop as stop
import test_supervisor_integration as coordinator

class LedgerReader(supervisor.Supervisor):
 def view(self,mode):
  return subprocess.run(['/bin/ash','-c','. /field/runtime/app/lib/operation-owner.sh; broray_ops_supervisor_ledger_view "$1" "$2"','view',str(self.authority),mode],capture_output=True,text=True)
 def test_crashed_supervisor_keeps_absence_proof_available(self):
  ready=self.temp/'ready';p=self.launch(f'echo yes >"{ready}"; sleep 60')
  self.wait_ready(p,ready);p.kill();p.communicate(timeout=5);self.verify_gone()
  r=self.view('absent');self.assertEqual(r.returncode,0,r.stderr)
  self.assertNotEqual(self.view('finished').returncode,0)
 def test_complete_requires_exact_terminal(self):
  p=self.launch('/bin/true');p.communicate(timeout=5)
  self.assertEqual(self.view('finished').returncode,0)
  Path(str(self.authority)+'.terminal').write_bytes(b'{broken')
  self.assertNotEqual(self.view('absent').returncode,0)

class AuthPublication(sessions.Sessions):
 def setUp(self):
  super().setUp()
  helper=Path(self.env['BRORAY_NATIVE_AUTH_LIBRARY'])
  helper.write_text(helper.read_text()+'\nhexdump() { /bin/busybox hexdump "$@"; }\n')
 def test_create_failure_never_returns_token(self):
  helper=Path(self.env['BRORAY_NATIVE_AUTH_LIBRARY'])
  helper.write_text(helper.read_text()+'\nmv() { echo reached >"$AUDIT/publish-called"; return 1; }\n')
  r=self.shell('broray_session_create fixture')
  self.assertTrue((self.p/'publish-called').exists(),r.stderr)
  self.assertNotEqual(r.returncode,0);self.assertEqual(r.stdout,'')
 def test_create_validate_delete(self):
  r=self.shell('t="$(broray_session_create fixture)" && broray_session_validate "$t" && broray_session_delete "$t" && ! broray_session_validate "$t"')
  self.assertEqual(r.returncode,0,r.stderr)

class RouteMarker(routes.RouteLocks):
 def test_marker_blocks_all_eight_consumers(self):
  marker=self.app/'routes/rollback-required.json';marker.write_bytes(b'{broken')
  for module,prefix in routes.SPECS:
   with self.subTest(module=module):
    self.shell(f'. "$BRORAY_ROOT/lib/{module}"; ! {prefix}_acquire fixture')
    self.assertFalse(self.lock.exists());self.assertEqual(marker.read_bytes(),b'{broken')

class Steps(steps.StepHelpers):pass

class PrivateStopFixture(stop.StopIntent):
 def setUp(self):
  # Production evidence is private. The existing restoration test recreates
  # its deleted fixture with write_bytes, which otherwise defaults to 0644.
  old=os.umask(0o077);self.addCleanup(os.umask,old)
  super().setUp()

class PrivateCoordinatorFixture(coordinator.Integration):
 def setUp(self):
  old=os.umask(0o077);self.addCleanup(os.umask,old)
  super().setUp()

class LogoutCGI(sessions.Sessions):
 def setUp(self):
  super().setUp()
  self.link=Path('/opt/broray');self.assertFalse(self.link.exists())
  self.link.symlink_to(self.app);self.addCleanup(self.link.unlink)
 def request(self):
  return subprocess.run(['/bin/ash','/field/runtime/app/web-new/api/logout.cgi'],env={**self.env,'REQUEST_METHOD':'POST','HTTP_COOKIE':'BRORAY_SESSION='+self.token},capture_output=True,timeout=10)
 def test_http_logout_removes_token_and_cookie(self):
  r=self.request();self.assertEqual(r.returncode,0,r.stderr)
  self.assertIn(b'Status: 200 OK',r.stdout);self.assertIn(b'Max-Age=0',r.stdout);self.assertFalse(self.file.exists())
 def test_guard_refusal_returns_503_and_preserves_token(self):
  (self.app/'bin/broray-ops-guard').unlink()
  r=self.request();self.assertEqual(r.returncode,0)
  self.assertIn(b'Status: 503 ',r.stdout);self.assertIn(b'SESSION_REVOKE_FAILED',r.stdout)
  self.assertNotIn(b'Max-Age=0',r.stdout);self.assertTrue(self.file.exists())
