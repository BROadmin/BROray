"""Legacy staging does not activate, signal, or infer STOPPED from /proc."""
from pathlib import Path
import hashlib,json,os,stat,subprocess,unittest
from test_updater_generation import Generation,GEN

FILES=['opt/bin/broray-updaterctl','opt/etc/init.d/S22broray-updater',
 'opt/libexec/broray-updater/broray-compat.sh','opt/libexec/broray-updater/broray-migrate-legacy.sh',
 'opt/libexec/broray-updater/minisign','opt/libexec/broray-updater/broray-updater.sh','opt/libexec/broray-updater/xray-wrapper']
class Migration(Generation):
 def setUp(self):
  super().setUp();self.payload=self.home/'payload';self.live_root=self.home/'live';self.stage=self.home/'migration'
  self.stage.mkdir(mode=0o700);self.payload.mkdir(mode=0o700);self.live_root.mkdir(mode=0o700)
  rows=[]
  for i,rel in enumerate(FILES):
   for root,body in [(self.payload,f'new-{i}\n'),(self.live_root,f'old-{i}\n')]:
    f=root/rel;f.parent.mkdir(parents=True,exist_ok=True);f.write_text(body);f.chmod(0o755)
   rows.append(hashlib.sha256((self.payload/rel).read_bytes()).hexdigest()+'  '+rel+'\n')
  (self.payload/'SHA256SUMS').write_text(''.join(rows));self.manifest_sha=hashlib.sha256((self.payload/'SHA256SUMS').read_bytes()).hexdigest()
 def invoke(self,verb='migration-stage',sha=None,nonce='nonce-one'):
  return subprocess.run([GEN,verb,str(self.stage),str(self.live_root),str(self.payload),sha or self.manifest_sha,'operation-one',nonce,'running'],capture_output=True,text=True,timeout=5)
 def inventory(self,root):
  return {f.relative_to(root).as_posix():(hashlib.sha256(f.read_bytes()).hexdigest(),stat.S_IMODE(f.stat().st_mode)) for f in root.rglob('*') if f.is_file()}
 def test_stage_exact_payload_without_live_mutation(self):
  foreign=subprocess.Popen(['/bin/ash','-c','while :; do sleep 1; done']);self.processes.append(foreign)
  before=self.inventory(self.live_root);r=self.invoke();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  receipt=json.loads(r.stdout);self.assertEqual(receipt['phase'],'REBOOT_REQUIRED');self.assertFalse(receipt['activationAllowed']);self.assertFalse(receipt['serviceStopped'])
  self.assertEqual(self.inventory(self.live_root),before);self.assertIsNone(foreign.poll())
  self.assertTrue((self.stage/'intent.record').is_file());self.assertTrue((self.stage/'staged.receipt').is_file())
  for i,rel in enumerate(FILES):self.assertEqual((self.stage/f'file-{i}').read_bytes(),(self.payload/rel).read_bytes())
 def test_retry_is_exact_and_read_only(self):
  self.assertEqual(self.invoke().returncode,0);before=self.inventory(self.stage)
  self.assertEqual(self.invoke().returncode,0);self.assertEqual(self.inventory(self.stage),before)
  self.assertNotEqual(self.invoke(nonce='other').returncode,0);self.assertEqual(self.inventory(self.stage),before)
 def test_current_boot_cannot_authorize_activation(self):
  self.assertEqual(self.invoke().returncode,0);before=self.inventory(self.stage)
  r=self.invoke('migration-boundary');self.assertNotEqual(r.returncode,0);self.assertIn('MIGRATION_REBOOT_REQUIRED',r.stderr)
  self.assertEqual(self.inventory(self.stage),before);self.assertFalse((self.stage/'boot.receipt').exists())
 def test_wrong_manifest_no_intent_or_live_mutation(self):
  before=self.inventory(self.live_root);self.assertNotEqual(self.invoke(sha='b'*64).returncode,0)
  self.assertEqual(self.inventory(self.live_root),before);self.assertFalse((self.stage/'intent.record').exists())
 def test_changed_staged_bytes_preserved(self):
  self.assertEqual(self.invoke().returncode,0);(self.stage/'file-2').write_bytes(b'foreign')
  before=self.inventory(self.stage);self.assertNotEqual(self.invoke().returncode,0);self.assertEqual(self.inventory(self.stage),before)
 def test_corrupt_intent_preserved(self):
  self.assertEqual(self.invoke().returncode,0);(self.stage/'intent.record').write_bytes(b'{broken')
  before=self.inventory(self.stage);self.assertNotEqual(self.invoke().returncode,0);self.assertEqual(self.inventory(self.stage),before)
 def test_missing_intent_not_recreated(self):
  self.assertEqual(self.invoke().returncode,0);(self.stage/'intent.record').unlink()
  before=self.inventory(self.stage);self.assertNotEqual(self.invoke().returncode,0);self.assertEqual(self.inventory(self.stage),before)
 def test_missing_committed_staged_file_not_recreated(self):
  self.assertEqual(self.invoke().returncode,0);(self.stage/'file-3').unlink()
  before=self.inventory(self.stage);self.assertNotEqual(self.invoke().returncode,0);self.assertEqual(self.inventory(self.stage),before)
 def test_changed_live_platform_refused_and_preserved(self):
  self.assertEqual(self.invoke().returncode,0);(self.live_root/FILES[0]).write_text('external mutation')
  before=self.inventory(self.live_root);evidence=self.inventory(self.stage)
  self.assertNotEqual(self.invoke().returncode,0);self.assertEqual(self.inventory(self.live_root),before);self.assertEqual(self.inventory(self.stage),evidence)
 def test_symlink_source_refused(self):
  f=self.payload/FILES[1];f.unlink();f.symlink_to(self.live_root/FILES[1]);self.assertNotEqual(self.invoke().returncode,0);self.assertFalse((self.stage/'intent.record').exists())
 def test_unknown_stage_evidence_never_overwritten(self):
  (self.stage/'foreign').write_bytes(b'KEEP');before=self.inventory(self.stage)
  self.assertNotEqual(self.invoke().returncode,0);self.assertEqual(self.inventory(self.stage),before)
 def test_manifest_extra_path_refused(self):
  p=self.payload/'SHA256SUMS';p.write_text(p.read_text()+'0'*64+'  ../../foreign\n');self.manifest_sha=hashlib.sha256(p.read_bytes()).hexdigest()
  self.assertNotEqual(self.invoke().returncode,0);self.assertFalse((self.stage/'intent.record').exists())
 def test_staged_inspection_is_exact_and_read_only(self):
  self.assertEqual(self.invoke().returncode,0)
  before=self.inventory(self.stage);live=self.inventory(self.live_root)
  r=self.invoke('migration-check-staged');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual(json.loads(r.stdout)['phase'],'STAGED_ONLY_VERIFIED')
  self.assertEqual(self.inventory(self.stage),before);self.assertEqual(self.inventory(self.live_root),live)
 def test_staged_inspection_cannot_create_initial_evidence(self):
  r=self.invoke('migration-check-staged');self.assertNotEqual(r.returncode,0)
  self.assertEqual(self.inventory(self.stage),{})
 def test_staged_inspection_wrong_nonce_preserves_evidence(self):
  self.assertEqual(self.invoke().returncode,0);before=self.inventory(self.stage)
  self.assertNotEqual(self.invoke('migration-check-staged',nonce='other').returncode,0)
  self.assertEqual(self.inventory(self.stage),before)
 def test_staged_inspection_rejects_new_boot(self):
  self.assertEqual(self.invoke().returncode,0)
  p=self.stage/'intent.record';lines=p.read_text().splitlines();lines[8]='00000000-0000-0000-0000-000000000000'
  p.write_text('\n'.join(lines)+'\n')
  (self.stage/'staged.receipt').write_text('BROray-migration-staged/1\n'+hashlib.sha256(p.read_bytes()).hexdigest()+'\n')
  before=self.inventory(self.stage)
  self.assertNotEqual(self.invoke('migration-check-staged').returncode,0)
  self.assertEqual(self.inventory(self.stage),before)

def missing_staged_check(name):
 def test(self):
  self.assertEqual(self.invoke().returncode,0);(self.stage/name).unlink();before=self.inventory(self.stage)
  self.assertNotEqual(self.invoke('migration-check-staged').returncode,0)
  self.assertEqual(self.inventory(self.stage),before)
 return test
for index,name in enumerate(['intent.record','manifest.record','staged.receipt']+['file-'+str(i) for i in range(7)]):
 setattr(Migration,'test_staged_inspection_missing_record_'+str(index),missing_staged_check(name))

if __name__=='__main__':
 names=[n for n in Migration.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(Migration(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)
