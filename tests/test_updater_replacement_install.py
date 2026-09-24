"""Exact A -> B files under a protected replacement, before service activation."""
import json,os,subprocess,unittest
from test_updater_replacement_backup import ReplacementBackup
from test_updater_preflight import TARGETS,UPDATER,digest

class ReplacementInstall(ReplacementBackup):
 def replacement_target_manifest(self):
  self.next_payload=self.slot/'app/share/updater-platform'
  daemon=self.next_payload/UPDATER
  daemon.write_bytes(daemon.read_bytes()+b'\n# Exact replacement B fixture.\n')
  manifest=self.next_payload/'SHA256SUMS'
  manifest.write_text(''.join(digest(self.next_payload/n)+'  '+n+'\n' for n in TARGETS))
  return digest(manifest)

 def test_exact_platform_replacement_replay_and_foreign_change_refusal(self):
  self.test_replacement_backup_binds_both_generations_and_preserves_fence()
  f=self.parent_fixture;before=self.snapshot();origin=(f.op/'state.json').read_bytes()
  fence=self.root/'opt/var/lock/broray/global-operation.lock';operation=fence.readlink().parent
  current=json.loads((operation/'state.json').read_bytes());nonce=current['platformPreflight']['stopNonce']
  backup=operation/'platform-replacement-backup'
  saved={p.name:p.read_bytes() for p in backup.iterdir()}
  env={**os.environ,'BRORAY_ROOT':str(self.root/'opt/broray'),
   'BRORAY_STATE_ROOT':str(self.root/'opt/var/lib/broray'),
   'BRORAY_OPS_CODE_ROOT':str(self.slot/'app'),'BRORAY_OPS_GUARD':str(self.slot/'app/bin/broray-ops-guard'),
   'BRORAY_OPS_GENERATION':str(f.native),'BRORAY_OPS_ASH':str(self.root/'opt/bin/ash'),
   'BRORAY_ROUTES_API_LOCK':str(fence),'BRORAY_OPS_UPDATER_ROOT':str(f.updater),
   'BRORAY_LEGACY_GLOBAL_LOCK':str(self.root/'tmp/broray-global-operation.lock'),
   'BRORAY_OPS_RAM_ROOT':str(self.root/'tmp/broray-operations')}
  script='''
. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"
broray_ops_call platform-replacement-install "$1" "$2"
'''
  def run():
   return subprocess.run([str(self.root/'opt/bin/ash'),'-c',script,'install',operation.name,nonce],
                         env=env,capture_output=True,text=True,timeout=45)
  r=run();print('REPLACEMENT_INSTALL '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  result=json.loads(r.stdout);self.assertEqual(result['phase'],'INSTALLED')
  self.assertFalse(result['platformReady']);self.assertFalse(result['activationAllowed'])
  for name in TARGETS:
   self.assertEqual((self.root/name).read_bytes(),(self.next_payload/name).read_bytes())
   self.assertEqual((self.root/name).stat().st_mode&0o777,0o755)
  self.assertEqual(fence.readlink(),operation/'fence')
  self.assertEqual((f.op/'state.json').read_bytes(),origin)
  self.assertEqual(saved,{p.name:p.read_bytes() for p in backup.iterdir()})
  r=run();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertTrue(json.loads(r.stdout)['replayed'])
  # A third party's bytes are not treated as another retry or rolled over.
  path=self.root/UPDATER;installed=path.read_bytes()
  try:
   path.write_bytes(b'foreign change must survive\n')
   r=run();self.assertNotEqual(r.returncode,0)
   self.assertIn('PLATFORM_REPLACEMENT_INSTALL_UNCONFIRMED',r.stdout+r.stderr)
   self.assertEqual(path.read_bytes(),b'foreign change must survive\n')
   self.assertEqual(fence.readlink(),operation/'fence')
   self.assertEqual(saved,{p.name:p.read_bytes() for p in backup.iterdir()})
  finally:path.write_bytes(installed)
  self.assertNotEqual(self.snapshot(),before)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  ReplacementInstall('test_exact_platform_replacement_replay_and_foreign_change_refusal')]))
 raise SystemExit(not result.wasSuccessful())
