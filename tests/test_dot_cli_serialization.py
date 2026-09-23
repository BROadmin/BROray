"""Real standalone CLI admission; replace only DoT work, never access a router."""
from pathlib import Path
import os,shutil,subprocess,tempfile,unittest

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
class CliSerialization(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='dot-cli-lock-');self.addCleanup(self.tmp.cleanup)
  self.home=Path(self.tmp.name)
  shutil.copytree(ROOT/'runtime/app/lib',self.home/'lib')
  (self.home/'bin').mkdir();(self.home/'tmp').mkdir()
  shutil.copyfile(ROOT/'runtime/app/bin/broray-routes-dot',self.home/'bin/broray-routes-dot')
  self.lib=self.home/'lib/fixture.sh';self.lib.write_text('''broray_dot_error() { printf '%s\\n' "$1" >&2; return 1; }
broray_dot_test() { printf 'test\\n' >>"$TEST_CALLED"; printf '{}\\n'; }
broray_dot_apply() { printf 'apply\\n' >>"$TEST_CALLED"; printf '{}\\n'; }
''')
  self.called=self.home/'called';self.lock=self.home/'global.lock'
  self.env={**os.environ,'BRORAY_ROOT':str(self.home),'BRORAY_DOT_LIB':str(self.lib),'TEST_CALLED':str(self.called),'BRORAY_ROUTES_API_LOCK':str(self.lock),'BRORAY_UPDATER_REQUEST_LOCK':str(self.home/'updater.lock'),'BRORAY_UPDATER_OPERATION_POINTER':str(self.home/'pointer'),'BRORAY_UPDATER_OPERATION_ROOT':str(self.home/'operations'),'BRORAY_LEGACY_GLOBAL_LOCK':str(self.home/'legacy.lock')}
  self.env.update(BRORAY_STATE_ROOT=str(self.home/'ops-state'),BRORAY_OPS_UPDATER_ROOT=str(self.home/'updater'),BRORAY_OPS_RAM_ROOT=str(self.home/'ram'),BRORAY_OPS_ASH='/bin/ash',BRORAY_OPS_GUARD=str(ROOT.parent/'.local/bin/linux-guard'),BRORAY_OPS_SUPERVISOR=str(ROOT.parent/'.local/bin/linux-supervisor'))
 def call(self,action):
  return subprocess.run(['/bin/ash',str(ROOT/'runtime/app/bin/broray-routes-dot'),action,str(self.home/'request.json')],env=self.env,capture_output=True,text=True,timeout=40)
 def test_busy_fence_blocks_apply_and_test(self):
  self.lock.mkdir();(self.lock/'foreign-evidence').write_bytes(b'KEEP\x00EXACT')
  for action in ['apply','test']:
   with self.subTest(action=action):
    r=self.call(action);self.assertNotEqual(r.returncode,0,(action,r.stdout,r.stderr));self.assertIn('ROUTES_OPERATION_BUSY',r.stderr)
    self.assertFalse(self.called.exists());self.assertEqual((self.lock/'foreign-evidence').read_bytes(),b'KEEP\x00EXACT')
    self.assertEqual([p.name for p in self.lock.iterdir()],['foreign-evidence'])
 def test_free_fence_allows_work_and_releases_own_lock(self):
  for action in ['apply','test']:
   r=self.call(action);self.assertEqual(r.returncode,0,r.stderr);self.assertFalse(self.lock.exists())
  self.assertEqual(self.called.read_text().splitlines(),['apply','test'])

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
