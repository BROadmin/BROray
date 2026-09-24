"""A -> B backup under the retained replacement fence; no install/start yet."""
import hashlib,json,os,subprocess,unittest
from pathlib import Path
from test_updater_platform_target_stop import ReplacementStop

class ReplacementBackup(ReplacementStop):
 def test_replacement_backup_binds_both_generations_and_preserves_fence(self):
  self.test_new_manifest_stops_old_generation_and_keeps_install_fence()
  f=self.parent_fixture
  fence=self.root/'opt/var/lock/broray/global-operation.lock'
  operation=fence.readlink().parent
  current=json.loads((operation/'state.json').read_bytes())
  nonce=current['platformPreflight']['stopNonce']
  old=current['platformPreflight']['generationStop']
  before=self.snapshot();origin=(f.op/'state.json').read_bytes()
  env={**os.environ,'BRORAY_ROOT':str(self.root/'opt/broray'),
   'BRORAY_STATE_ROOT':str(self.root/'opt/var/lib/broray'),
   'BRORAY_OPS_CODE_ROOT':str(self.slot/'app'),'BRORAY_OPS_GUARD':str(self.slot/'app/bin/broray-ops-guard'),
   'BRORAY_OPS_GENERATION':str(f.native),'BRORAY_OPS_ASH':str(self.root/'opt/bin/ash'),
   'BRORAY_ROUTES_API_LOCK':str(fence),'BRORAY_OPS_UPDATER_ROOT':str(f.updater),
   'BRORAY_LEGACY_GLOBAL_LOCK':str(self.root/'tmp/broray-global-operation.lock'),
   'BRORAY_OPS_RAM_ROOT':str(self.root/'tmp/broray-operations')}
  script='''
. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"
broray_ops_call platform-replacement-backup "$1" "$2"
'''
  def run():
   return subprocess.run([str(self.root/'opt/bin/ash'),'-c',script,'backup',operation.name,nonce],
                         env=env,capture_output=True,text=True,timeout=45)
  r=run();print('REPLACEMENT_BACKUP '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  result=json.loads(r.stdout);self.assertEqual(result['phase'],'BACKUP_READY')
  self.assertFalse(result['platformReady']);self.assertFalse(result['activationAllowed'])
  self.assertEqual(self.snapshot(),before);self.assertEqual((f.op/'state.json').read_bytes(),origin)
  self.assertEqual(fence.readlink(),operation/'fence')
  backup=operation/'platform-replacement-backup'
  intent=json.loads((backup/'intent.json').read_bytes())
  self.assertEqual(intent['oldGenerationId'],old['generationId'])
  self.assertEqual(intent['oldPlatformManifestSha256'],old['platformManifestSha256'])
  self.assertEqual(intent['expectedPlatformManifestSha256'],current['platformPreflight']['expectedPlatformManifestSha256'])
  self.assertTrue(intent['oldServiceWasRunning'])
  self.assertEqual(intent['oldNativeSha256'],old['nativeSha256'])
  for i,row in enumerate(intent['before']):
   data,mode=before[row['path']]
   self.assertTrue(row['present']);self.assertEqual(row['mode'],mode)
   self.assertEqual(row['sha256'],hashlib.sha256(data).hexdigest())
   self.assertEqual((backup/f'before-{i}').read_bytes(),data)
  saved={p.name:p.read_bytes() for p in backup.iterdir()}
  r=run();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertTrue(json.loads(r.stdout)['replayed'])
  self.assertEqual(saved,{p.name:p.read_bytes() for p in backup.iterdir()})
  # A partial/corrupt backup is evidence: do not reconstruct or overwrite it.
  image=backup/'before-0';data=image.read_bytes()
  try:
   image.write_bytes(b'{broken')
   r=run();self.assertNotEqual(r.returncode,0)
   self.assertIn('PLATFORM_REPLACEMENT_BACKUP_UNCONFIRMED',r.stdout+r.stderr)
   self.assertEqual(image.read_bytes(),b'{broken')
   self.assertEqual(self.snapshot(),before);self.assertEqual(fence.readlink(),operation/'fence')
  finally:image.write_bytes(data)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  ReplacementBackup('test_replacement_backup_binds_both_generations_and_preserves_fence')]))
 raise SystemExit(not result.wasSuccessful())
