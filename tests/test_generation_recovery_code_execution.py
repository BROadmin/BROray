"""Execute retained coordinator+guard after source removal in the Linux fixture.

The fixture uses the existing verified Linux guard built from project source;
the production ARM guard is not executable on this x86 guest. This is not an
ARM or router acceptance result and does not exercise boot recovery admission.
"""
import json,os,shutil,subprocess,unittest
from test_generation_recovery_code import RecoveryCode
from test_preflight_admission import GUARD

class RecoveryCodeExecution(RecoveryCode):
 def test_retained_guard_and_coordinator_execute_after_source_removed(self):
  shutil.copy2(GUARD,self.source/'bin/broray-ops-guard')
  self.assertIn('broray-ops-guard/6',subprocess.check_output([str(GUARD),'--version'],text=True))
  self.assertEqual(self.code_call().returncode,0);shutil.rmtree(self.source)
  state=self.home/'isolated-ops-state';state.mkdir(mode=0o700);(state/'operations').mkdir(mode=0o700)
  (state/'operations.guard').touch(mode=0o600)
  before=self.snapshot();r=self.code_call(True);self.assertEqual(r.returncode,0,r.stderr)
  env={**os.environ,'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','BRORAY_ROOT':str(self.live_root/'opt/broray'),
    'BRORAY_STATE_ROOT':str(state),'BRORAY_OPS_CODE_ROOT':str(self.code),'BRORAY_OPS_GUARD':str(self.code/'bin/broray-ops-guard'),
    'BRORAY_OPS_ASH':'/bin/ash','BRORAY_OPS_RAM_ROOT':str(self.home/'ram'),'BRORAY_ROUTES_API_LOCK':str(self.home/'global.lock'),
    'BRORAY_OPS_UPDATER_ROOT':str(self.home/'no-updater-state'),'BRORAY_LEGACY_GLOBAL_LOCK':str(self.home/'legacy.lock')}
  q=subprocess.run(['/bin/ash','-c','set -u\n. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"\nbroray_ops_call status'],env=env,capture_output=True,text=True,timeout=10)
  self.assertEqual(q.returncode,0,q.stdout+q.stderr);reply=json.loads(q.stdout);self.assertTrue(reply['ok']);self.assertEqual(reply['operations'],[])
  self.assertEqual(self.snapshot(),before);self.assertFalse(self.source.exists())
  print('RETAINED_CODE_EXECUTION '+json.dumps({'sourceAbsent':True,'guard':str(self.code/'bin/broray-ops-guard'),'status':reply,'scope':'Linux fixture guard and real retained shell coordinator; no boot admission, no ARM/router acceptance'}),flush=True)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([RecoveryCodeExecution('test_retained_guard_and_coordinator_execute_after_source_removed')]))
 raise SystemExit(not r.wasSuccessful())
