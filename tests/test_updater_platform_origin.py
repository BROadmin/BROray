"""Installed-origin discovery only; native readiness is separately authenticated.

The retained executable is a test receipt writer, not a fake readiness result.
No router, native process domain or application is present in these fixtures.
"""
import hashlib,json,os,shutil,subprocess,tempfile,unittest
from pathlib import Path

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
PLATFORM=ROOT/'runtime/app/share/updater-platform'
FILES=[line.split()[1] for line in (PLATFORM/'SHA256SUMS').read_text().splitlines()]
INIT='opt/etc/init.d/S22broray-updater'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()

class InstalledOrigin(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='origin-');self.addCleanup(self.tmp.cleanup)
  self.root=Path(self.tmp.name)
  for name in FILES:
   dst=self.root/name;dst.parent.mkdir(parents=True,exist_ok=True)
   shutil.copyfile(PLATFORM/name,dst);dst.chmod(0o755)
  raw=''.join(sha(self.root/n)+'  '+n+'\n' for n in sorted(FILES))
  self.manifest=hashlib.sha256(raw.encode()).hexdigest()
  self.operations=self.root/'opt/var/lib/broray/operations';self.operations.mkdir(parents=True)
  self.env={**os.environ,'BRORAY_UPDATER_ROOT_PREFIX':str(self.root)}
 def origin(self,name,manifest,stop=False):
  op=self.operations/name;op.mkdir()
  state=dict(operation='system:platform-preflight',state='completed',running=False,
             platformPreflight=dict(expectedPlatformManifestSha256=manifest))
  if stop:state['serviceStop']=dict(schemaVersion=1,contract='broray-service-stop/1',originOperationId='op-current')
  (op/'state.json').write_text(json.dumps(state))
  native=op/'platform-bootguard/runtime';native.parent.mkdir()
  native.write_text('#!/bin/ash\nprintf \'SELECTED=%s\\n\' "$3"\n');native.chmod(0o700)
  binding=dict(schemaVersion=1,contract='broray-platform-bootguard/1',operationId=name,
               nativeSha256=sha(native),migrationIntentSha256='1'*64,stopNonce='2'*32)
  (op/'platform-bootguard.json').write_text(json.dumps(binding))
  binding.update(contract='broray-recovery-code/1',bootGuardBindingSha256=sha(op/'platform-bootguard.json'))
  (op/'platform-recovery-code.json').write_text(json.dumps(binding))
  return op
 def run_init(self,verb='status'):
  return subprocess.run(['/bin/ash',str(self.root/INIT),verb],env=self.env,
                        capture_output=True,text=True,timeout=15)
 def background_owner(self):
  op=self.operations/'op-background';op.mkdir(mode=0o700)
  state=op/'state.json'
  state.write_text(json.dumps(dict(operationId=op.name,operation='auto-switch:tick',
      kind='background',type='auto_switch',state='running',running=True)))
  state.chmod(0o600);(op/'fence').mkdir(mode=0o700)
  lock=self.root/'opt/var/lock/broray/global-operation.lock'
  lock.parent.mkdir(parents=True);lock.symlink_to(op/'fence')
  return state,lock
 def test_status_discovers_updater_during_unrelated_background_operation(self):
  self.origin('op-current',self.manifest);state,lock=self.background_owner()
  before=state.read_bytes();target=lock.readlink()
  r=self.run_init();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual(r.stdout,'SELECTED=op-current\n')
  self.assertEqual(state.read_bytes(),before);self.assertEqual(lock.readlink(),target)
 def test_background_owner_blocks_mutating_init_without_becoming_updater_origin(self):
  self.origin('op-current',self.manifest);state,lock=self.background_owner()
  before=state.read_bytes();target=lock.readlink()
  for verb in ['start','stop','restart']:
   with self.subTest(verb=verb):
    r=self.run_init(verb);self.assertEqual(r.returncode,75,r.stdout+r.stderr)
    self.assertIn('UPDATER_SERVICE_OPERATION_BUSY',r.stdout);self.assertNotIn('SELECTED=',r.stdout)
    self.assertEqual(state.read_bytes(),before);self.assertEqual(lock.readlink(),target)
 def test_corrupt_background_state_is_preserved_and_refused(self):
  self.origin('op-current',self.manifest);state,lock=self.background_owner()
  state.write_text('{broken');target=lock.readlink()
  r=self.run_init();self.assertEqual(r.returncode,75,r.stdout+r.stderr)
  self.assertNotIn('SELECTED=',r.stdout);self.assertEqual(state.read_text(),'{broken')
  self.assertEqual(lock.readlink(),target)
 def test_active_platform_fence_remains_the_selected_origin(self):
  op=self.origin('op-current',self.manifest)
  row=json.loads((op/'state.json').read_bytes());row.update(operationId=op.name,state='running',running=True)
  (op/'state.json').write_text(json.dumps(row));(op/'state.json').chmod(0o600)
  (op/'fence').mkdir(mode=0o700)
  lock=self.root/'opt/var/lock/broray/global-operation.lock'
  lock.parent.mkdir(parents=True);lock.symlink_to(op/'fence')
  r=self.run_init();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual(r.stdout,'SELECTED=op-current\n');self.assertEqual(lock.readlink(),op/'fence')
 def test_historical_origin_does_not_hide_installed_origin(self):
  old=self.origin('op-old','0'*64);self.origin('op-current',self.manifest)
  before={str(p):p.read_bytes() for p in old.rglob('*') if p.is_file()}
  r=self.run_init();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual(r.stdout,'SELECTED=op-current\n')
  self.assertEqual(before,{str(p):p.read_bytes() for p in old.rglob('*') if p.is_file()})
 def test_two_origins_for_installed_bytes_remain_ambiguous(self):
  self.origin('op-a',self.manifest);self.origin('op-b',self.manifest)
  r=self.run_init();self.assertEqual(r.returncode,75);self.assertIn('UPDATER_SERVICE_IDENTITY_AMBIGUOUS',r.stdout)
 def test_unknown_installed_platform_refuses(self):
  self.origin('op-old','0'*64)
  r=self.run_init();self.assertEqual(r.returncode,75);self.assertNotIn('SELECTED=',r.stdout)
 def test_tampered_platform_refuses(self):
  self.origin('op-current',self.manifest)
  p=self.root/FILES[0];p.write_bytes(p.read_bytes()+b'\n# tampered\n')
  r=self.run_init();self.assertEqual(r.returncode,75);self.assertNotIn('SELECTED=',r.stdout)
 def test_non_executable_platform_refuses(self):
  self.origin('op-current',self.manifest);(self.root/FILES[0]).chmod(0o644)
  r=self.run_init();self.assertEqual(r.returncode,75);self.assertNotIn('SELECTED=',r.stdout)
 def test_stopped_operation_is_not_a_second_origin(self):
  self.origin('op-current',self.manifest);self.origin('op-stop',self.manifest,stop=True)
  r=self.run_init();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual(r.stdout,'SELECTED=op-current\n')
 def replacement(self):
  old=self.origin('op-old',self.manifest);new=self.origin('op-current',self.manifest)
  generation='g-'+'A'*22;native='1'*64
  anchor=self.root/'opt/var/lib/broray-updater/generations'/generation/'state.json'
  anchor.parent.mkdir(parents=True)
  anchor.write_text(json.dumps(dict(generationId=generation,platformManifestSha256=self.manifest,
      platformLaunch=dict(operationId='op-old',nativeSha256=native))))
  state=new/'state.json';row=json.loads(state.read_text())
  row['platformPreflight']['generationStop']=dict(contract='broray-platform-generation-stop/1',
      generationId=generation,platformManifestSha256=self.manifest,nativeSha256=native)
  state.write_text(json.dumps(row))
  return old,new,anchor
 def test_native_only_replacement_selects_unique_successor(self):
  old,new,anchor=self.replacement();before={p:p.read_bytes() for p in [old/'state.json',new/'state.json',anchor]}
  r=self.run_init();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual(r.stdout,'SELECTED=op-current\n')
  self.assertEqual(before,{p:p.read_bytes() for p in before})
 def test_broken_replacement_ancestry_is_not_ignored(self):
  old,new,anchor=self.replacement();anchor.write_text('{broken')
  r=self.run_init();self.assertEqual(r.returncode,75,r.stdout+r.stderr)
  self.assertNotIn('SELECTED=',r.stdout);self.assertEqual(anchor.read_text(),'{broken')

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
