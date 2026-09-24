"""Rollback must restore exact A inodes without claiming service readiness."""
import json,os,subprocess,unittest
from test_updater_replacement_install import ReplacementInstall
from test_updater_preflight import TARGETS,UPDATER

class ReplacementRollback(ReplacementInstall):
 def test_exact_replacement_rollback_and_replay(self):
  self.test_exact_platform_replacement_replay_and_foreign_change_refusal()
  f=self.parent_fixture
  fence=self.root/'opt/var/lock/broray/global-operation.lock';op=fence.readlink().parent
  state=(op/'state.json').read_bytes();nonce=json.loads(state)['platformPreflight']['stopNonce']
  backup=op/'platform-replacement-backup'
  original={p.name:p.read_bytes() for p in backup.iterdir()}
  env={**os.environ,'BRORAY_ROOT':str(self.root/'opt/broray'),
   'BRORAY_STATE_ROOT':str(self.root/'opt/var/lib/broray'),
   'BRORAY_OPS_CODE_ROOT':str(self.slot/'app'),'BRORAY_OPS_GUARD':str(self.slot/'app/bin/broray-ops-guard'),
   'BRORAY_OPS_GENERATION':str(f.native),'BRORAY_OPS_ASH':str(self.root/'opt/bin/ash'),
   'BRORAY_ROUTES_API_LOCK':str(fence),'BRORAY_OPS_UPDATER_ROOT':str(f.updater),
   'BRORAY_LEGACY_GLOBAL_LOCK':str(self.root/'tmp/broray-global-operation.lock'),
   'BRORAY_OPS_RAM_ROOT':str(self.root/'tmp/broray-operations')}
  script='. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"\nbroray_ops_call platform-replacement-rollback "$1" "$2"\n'
  def run():
   return subprocess.run([str(self.root/'opt/bin/ash'),'-c',script,'rollback',op.name,nonce],
                         env=env,capture_output=True,text=True,timeout=45)
  # Rollback cannot depend on the still-available download or slot payload.
  self.next_payload.rename(self.next_payload.with_name('payload-unavailable'))
  r=run();print('REPLACEMENT_ROLLBACK '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  result=json.loads(r.stdout)
  self.assertEqual(result['phase'],'NEEDS_RECOVERY')
  self.assertTrue(result['platformRestored']);self.assertFalse(result['serviceStateRestored'])
  self.assertFalse(result['activationAllowed'])
  inventory=json.loads((backup/'intent.json').read_bytes())['before']
  for i,row in enumerate(inventory):
   self.assertEqual((self.root/row['path']).read_bytes(),original['before-'+str(i)])
   self.assertEqual((self.root/row['path']).stat().st_mode&0o777,row['mode'])
  self.assertEqual((op/'state.json').read_bytes(),state)
  self.assertEqual(fence.readlink(),op/'fence')
  self.assertEqual(original,{p.name:p.read_bytes() for p in backup.iterdir()})
  r=run();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertTrue(json.loads(r.stdout)['replayed'])
  path=self.root/UPDATER;restored=path.read_bytes()
  try:
   path.write_bytes(b'foreign rollback mutation\n')
   r=run();self.assertNotEqual(r.returncode,0)
   self.assertEqual(path.read_bytes(),b'foreign rollback mutation\n')
   self.assertEqual(fence.readlink(),op/'fence')
  finally:path.write_bytes(restored)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  ReplacementRollback('test_exact_replacement_rollback_and_replay')]))
 raise SystemExit(not result.wasSuccessful())
