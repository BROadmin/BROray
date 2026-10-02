"""Real ash/native guard and parallel requests; no HTTP server or router."""
import json,os,shutil,subprocess,tempfile,time,unittest
from pathlib import Path
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT','/field'))
class Sessions(unittest.TestCase):
 def setUp(self):
  self.t=tempfile.TemporaryDirectory();self.addCleanup(self.t.cleanup);self.p=Path(self.t.name)
  self.app=self.p/'app';shutil.copytree(ROOT/'runtime/app/lib',self.app/'lib')
  (self.app/'bin').mkdir();shutil.copyfile('/.local/bin/linux-guard',self.app/'bin/broray-ops-guard');(self.app/'bin/broray-ops-guard').chmod(0o755)
  self.sessions=self.app/'run/web-new/sessions';self.sessions.mkdir(parents=True)
  self.token='a'*64;self.file=self.sessions/self.token
  self.file.write_text(json.dumps(dict(username='fixture',expiresAt=int(time.time())+1000)))
  helper=self.p/'native-fixture.sh'
  helper.write_text('''jq() { /usr/bin/jq "$@"; }
mv() {
 if [ "${PAUSE_RENEW:-}" = yes ]; then
  echo ready >"$AUDIT/ready"
  while [ ! -f "$AUDIT/release" ]; do sleep 1; done
 fi
 command mv "$@"
}
''')
  self.env={**os.environ,'BRORAY_BASE':str(self.app),'BRORAY_NATIVE_AUTH_LIBRARY':str(helper),'AUDIT':str(self.p)}
 def shell(self,code,extra=None):
  return subprocess.run(['/bin/ash','-c','. "$BRORAY_BASE/lib/web-auth.sh"; '+code],env={**self.env,**(extra or {})},capture_output=True,text=True,timeout=12)
 def test_logout_after_inflight_refresh_cannot_resurrect(self):
  cmd=['/bin/ash','-c','. "$BRORAY_BASE/lib/web-auth.sh"; broray_session_validate '+self.token]
  renew=subprocess.Popen(cmd,env={**self.env,'PAUSE_RENEW':'yes'},stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  self.addCleanup(lambda: renew.kill() if renew.poll() is None else None)
  end=time.monotonic()+5
  while not (self.p/'ready').exists() and renew.poll() is None and time.monotonic()<end:time.sleep(.01)
  self.assertTrue((self.p/'ready').exists(),'renewal did not reach publication')
  logout=subprocess.Popen(['/bin/ash','-c','. "$BRORAY_BASE/lib/web-auth.sh"; echo ready >"$AUDIT/logout-start"; broray_session_delete '+self.token],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  self.addCleanup(lambda: logout.kill() if logout.poll() is None else None)
  end=time.monotonic()+1
  while not (self.p/'logout-start').exists() and time.monotonic()<end:time.sleep(.01)
  self.assertTrue((self.p/'logout-start').exists())
  time.sleep(.15);(self.p/'release').touch()
  out,err=renew.communicate(timeout=6);self.assertEqual(renew.returncode,0,(out,err))
  out,err=logout.communicate(timeout=6);self.assertEqual(logout.returncode,0,(out,err))
  self.assertFalse(self.file.exists(),'session resurrected after logout')
  self.assertNotEqual(self.shell('broray_session_validate '+self.token).returncode,0)
 def test_normal_renewal_preserves_username_and_extends_session(self):
  r=self.shell('broray_session_validate '+self.token+' && printf "%s" "$BRORAY_SESSION_USERNAME"')
  self.assertEqual((r.returncode,r.stdout),(0,'fixture'),r.stderr)
  self.assertGreater(json.loads(self.file.read_text())['expiresAt'],int(time.time())+1500)
 def test_cleanup_preserves_guard_and_inflight_temporary(self):
  other=self.sessions/(self.token+'.tmp.fixture');other.write_text('not yet JSON')
  r=self.shell('broray_sessions_cleanup');self.assertEqual(r.returncode,0,r.stderr)
  self.assertTrue(other.exists())
 def test_expired_session_is_denied(self):
  self.file.write_text('{"username":"fixture","expiresAt":1}')
  self.assertNotEqual(self.shell('broray_session_validate '+self.token).returncode,0)
