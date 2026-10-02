"""Full removal must account for files outside the application/state roots.

Runs actual finalizer functions against a private /opt prefix. No router access.
"""
import hashlib,json,os,re,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT','/work/implementation'))
SOURCE=ROOT/'runtime/app/lib/broray-page.sh'
class ExternalArtifacts(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='uninstall-images-');self.addCleanup(self.tmp.cleanup)
  self.home=Path(self.tmp.name);self.opt=self.home/'opt';self.app=self.opt/'broray';self.app.mkdir(parents=True)
  (self.app/'sentinel').write_text('owned app')
  self.ops=self.opt/'var/lib/broray/operations';self.ops.mkdir(parents=True,mode=0o700)
  self.op=self.ops/'op-fixture';self.op.mkdir(mode=0o700)
  (self.op/'state.json').write_text(json.dumps(dict(operation='system:platform-preflight',state='completed',running=False)))
  (self.op/'state.json').chmod(0o600)
  self.init=self.opt/'etc/init.d';self.init.mkdir(parents=True);(self.opt/'bin').mkdir()
  self.auth=self.home/'auth';self.auth.mkdir(mode=0o700)
  self.source=SOURCE.read_text()
  self.image=self.platform_image(0)
  self.legacy=self.opt/'var/lib/broray-platform-handoff';self.legacy.mkdir(mode=0o700)
  (self.legacy/'status.json').write_text(json.dumps(dict(schemaVersion=1,contract='broray-universal-platform-handoff/1',state='success',running=False)))
  (self.legacy/'phase').write_text('complete\n')
  for p in self.legacy.iterdir():p.chmod(0o600)
 def put(self,p,raw,mode=0o600):p.write_bytes(raw);p.chmod(mode)
 def platform_image(self,index):
  d=self.op/'platform-install';d.mkdir(mode=0o700,exist_ok=True)
  intent=b'BROray-platform-install/1\nINSTALLING\nop-fixture\n'+b'a'*32+b'\n'+b'b'*64+b'\n'+b'c'*64+b'\n'+b'd'*64+b'\n'+b'e'*64+b'\n'
  h=hashlib.sha256(intent).hexdigest();self.put(d/'intent.record',intent)
  self.put(self.op/'platform-install.record',('BROray-platform-install-binding/1\n'+h+'\n').encode())
  path=(self.opt/'bin' if index==0 else self.init)/f'.broray-pt-{h}-{index}.previous'
  self.put(path,b'#!/opt/bin/ash\necho old-owned-platform\n',0o755);s=path.stat()
  row=f'BROray-platform-install-entry/1\n{h}\n{index}\n{hashlib.sha256(path.read_bytes()).hexdigest()}\n0755\n'+('f'*64)+f'\n{s.st_dev}\n{s.st_ino}\n'
  self.put(d/f'entry-{index}.intent',row.encode())
  return path
 def run_finalizer(self):
  names=re.findall(r'^    (broray_system_uninstall_artifact_[a-z]+)\(\)',self.source,re.M)+['broray_system_uninstall_payload_finalize']
  blocks=[]
  for name in names:
   match=re.search(r'^    '+name+r'\(\) \{\n.*?^    \}',self.source,re.M|re.S)
   if match:blocks.append(match.group())
  self.assertTrue(blocks)
  relocated=re.sub(r'(?<![A-Za-z0-9_-])/opt(?=/|[\s"\'])',str(self.opt),'\n'.join(blocks))
  code='set -u\n'+relocated+'\nmode=full\npreserved_dir="$TEST_HOME/preserved"\nbroray_system_uninstall_payload_finalize\n'
  return subprocess.run(['/bin/ash','-c',code],env={**os.environ,'TEST_HOME':str(self.home),'BRORAY_OPKG_AUTH_ROOT':str(self.auth)},capture_output=True,text=True,timeout=10)
 def test_00_owned_external_images_and_terminal_legacy_state_removed(self):
  second=self.platform_image(1)
  p=self.run_finalizer();self.assertEqual(p.returncode,0,p.stderr)
  self.assertFalse(self.image.exists(),'owned platform image survives full removal')
  self.assertFalse(second.exists());self.assertFalse(self.legacy.exists());self.assertFalse(self.app.exists())
 def test_orphan_image_refuses_before_app_removal(self):
  (self.op/'platform-install.record').unlink()
  p=self.run_finalizer();self.assertNotEqual(p.returncode,0);self.assertTrue(self.image.exists());self.assertTrue(self.app.exists())
 def test_replaced_inode_preserved(self):
  raw=self.image.read_bytes();tmp=self.image.with_suffix('.new');self.put(tmp,raw,0o755);tmp.replace(self.image)
  p=self.run_finalizer();self.assertNotEqual(p.returncode,0);self.assertTrue(self.image.exists());self.assertTrue(self.app.exists())
 def test_changed_image_preserved(self):
  self.image.write_bytes(b'foreign bytes')
  p=self.run_finalizer();self.assertNotEqual(p.returncode,0);self.assertEqual(self.image.read_bytes(),b'foreign bytes');self.assertTrue(self.app.exists())
 def test_symlink_preserved(self):
  self.image.unlink();sentinel=self.home/'foreign';sentinel.write_text('FOREIGN');self.image.symlink_to(sentinel)
  p=self.run_finalizer();self.assertNotEqual(p.returncode,0);self.assertTrue(self.image.is_symlink());self.assertEqual(sentinel.read_text(),'FOREIGN');self.assertTrue(self.app.exists())
 def test_live_legacy_worker_fence_preserved(self):
  (self.legacy/'worker.lock').mkdir(mode=0o700)
  p=self.run_finalizer();self.assertNotEqual(p.returncode,0);self.assertTrue((self.legacy/'worker.lock').exists());self.assertTrue(self.app.exists())
 def test_foreign_legacy_state_preserved(self):
  (self.legacy/'status.json').write_text('{}')
  p=self.run_finalizer();self.assertNotEqual(p.returncode,0);self.assertTrue(self.legacy.exists());self.assertTrue(self.app.exists())
 def test_missing_optional_legacy_state_allowed(self):
  for p in self.legacy.iterdir():p.unlink()
  self.legacy.rmdir();p=self.run_finalizer();self.assertEqual(p.returncode,0,p.stderr);self.assertFalse(self.image.exists())
 def test_bootguard_displaced_file_removed(self):
  d=self.op/'platform-migration';d.mkdir(mode=0o700)
  g=self.op/'platform-bootguard';g.mkdir(mode=0o700)
  raw=b'#!/opt/bin/ash\necho old init\n';digest=hashlib.sha256(raw).hexdigest()
  intent=(f'BROray-updater-migration/1\n{d}\n/\n/source\n'+('a'*64)+f'\nop-fixture\nnonce\nrunning\nboot-id\nopt/etc/init.d/S22broray-updater\t1\t0755\t{digest}\t0755\t'+('b'*64)+'\n').encode()
  h=hashlib.sha256(intent).hexdigest();self.put(d/'intent.record',intent)
  self.put(g/'intent.record',f'BROray-boot-guard-staging/1\n{g}\n/\n{d}\n{h}\n'.encode());self.put(g/'before-1',raw)
  p=self.init/f'.broray-bg-{h}-1.previous';self.put(p,raw,0o755)
  r=self.run_finalizer();self.assertEqual(r.returncode,0,r.stderr);self.assertFalse(p.exists())
 def test_legacy_backup_hashes_verified_then_removed(self):
  d=self.legacy/'platform-backup';d.mkdir(mode=0o700)
  rel='opt/etc/init.d/S22broray-updater';p=d/rel
  for parent in reversed(p.parents):
   if parent!=d and d in parent.parents:parent.mkdir(mode=0o700,exist_ok=True)
  self.put(p,b'legacy init',0o755)
  self.put(d/'inventory.tsv',f'present\t{rel}\t{hashlib.sha256(p.read_bytes()).hexdigest()}\n'.encode())
  r=self.run_finalizer();self.assertEqual(r.returncode,0,r.stderr);self.assertFalse(d.exists())
 def test_hardlink_preserved(self):
  foreign=self.home/'foreign';os.link(self.image,foreign)
  r=self.run_finalizer();self.assertNotEqual(r.returncode,0);self.assertTrue(self.image.exists());self.assertTrue(foreign.exists());self.assertTrue(self.app.exists())
 def test_unknown_legacy_child_preserves_all(self):
  p=self.legacy/'unknown';self.put(p,b'foreign')
  r=self.run_finalizer();self.assertNotEqual(r.returncode,0);self.assertTrue(p.exists());self.assertTrue(self.image.exists());self.assertTrue(self.app.exists())
 def boot_records(self):
  manifest='a'*64
  request=dict(operationId='update-source',candidateId='candidate',payloadManifestSha256=manifest)
  self.put(self.legacy/'request.json',json.dumps(request).encode())
  reboot=dict(schemaVersion=1,contract='broray-first-platform-reboot/1',operationId='update-source',
    candidateId='candidate',preflightOperationId=self.op.name,bootId='11111111-2222-3333-4444-555555555555',platformManifestSha256=manifest)
  self.put(self.legacy/'reboot-request.json',json.dumps(reboot).encode())
  self.put(self.op/'state.json',json.dumps(dict(operation='system:platform-preflight',state='completed',running=False,
    platformPreflight=dict(expectedPlatformManifestSha256=manifest))).encode())
  reply=dict(ok=False,errorCode='UPDATER_LEGACY_REBOOT_REQUIRED',phase='REBOOT_REQUIRED',operationId=self.op.name,
    expectedPlatformManifestSha256=manifest,platformReady=False,serviceStopped=False,activationAllowed=False,signalsAuthorized=False)
  self.put(self.legacy/'preflight-result.123.json',json.dumps(reply).encode())
  for name in ['preflight-error.123','reboot-output','reboot-error']:self.put(self.legacy/name,b'')
 def test_completed_boot_evidence_removed(self):
  self.boot_records();r=self.run_finalizer();self.assertEqual(r.returncode,0,r.stderr)
  self.assertFalse(self.legacy.exists());self.assertFalse(self.app.exists())
 def test_pending_boot_evidence_preserved(self):
  self.boot_records();self.put(self.legacy/'phase',b'boot-pending\n')
  r=self.run_finalizer();self.assertNotEqual(r.returncode,0);self.assertTrue(self.legacy.exists());self.assertTrue(self.app.exists())
 def test_unbound_boot_evidence_preserved(self):
  self.boot_records();p=self.legacy/'preflight-result.123.json';v=json.loads(p.read_text());v['operationId']='op-foreign';self.put(p,json.dumps(v).encode())
  r=self.run_finalizer();self.assertNotEqual(r.returncode,0);self.assertTrue(self.legacy.exists());self.assertTrue(self.app.exists())
 def test_linked_boot_operation_without_external_images_preserved(self):
  self.boot_records();self.image.unlink()
  moved=self.home/'foreign-operation';self.op.rename(moved);self.op.symlink_to(moved,target_is_directory=True)
  r=self.run_finalizer();self.assertNotEqual(r.returncode,0);self.assertTrue(moved.exists());self.assertTrue(self.app.exists())
 def test_nonterminal_operation_preserved(self):
  self.put(self.op/'state.json',json.dumps(dict(operation='system:platform-preflight',state='running',running=True)).encode())
  r=self.run_finalizer();self.assertNotEqual(r.returncode,0);self.assertTrue(self.image.exists());self.assertTrue(self.app.exists())
 def test_partial_candidate_refuses(self):
  p=self.image.with_suffix('.candidate');self.put(p,b'partial',0o755)
  r=self.run_finalizer();self.assertNotEqual(r.returncode,0);self.assertTrue(p.exists());self.assertTrue(self.image.exists());self.assertTrue(self.app.exists())
 def test_linked_journal_root_preserved(self):
  moved=self.ops.with_name('foreign-operations');self.ops.rename(moved);self.ops.symlink_to(moved,target_is_directory=True)
  r=self.run_finalizer();self.assertNotEqual(r.returncode,0);self.assertTrue(self.image.exists());self.assertTrue(moved.exists());self.assertTrue(self.app.exists())
 def test_linked_artifact_parent_preserved(self):
  parent=self.image.parent;moved=self.opt/'foreign-bin';parent.rename(moved);parent.symlink_to(moved,target_is_directory=True)
  r=self.run_finalizer();self.assertNotEqual(r.returncode,0);self.assertTrue(self.image.exists());self.assertTrue(moved.exists());self.assertTrue(self.app.exists())

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
