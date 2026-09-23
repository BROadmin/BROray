"""Protected installation consumes the exact seven staged platform files."""
import json,stat,subprocess,unittest,shutil
from test_native_platform_backup import PlatformBackup,FILES

class PlatformInstall(PlatformBackup):
 def setUp(self):
  super().setUp();self.addCleanup(self.clear_install)
 def clear_install(self):
  # Only the fixture's newly created install evidence and displaced files.
  # Restore the exported input afterwards through the existing base cleanup.
  for rel in FILES:
   for p in (self.root/'router'/rel).parent.glob('.broray-pt-*'):p.unlink()
  p=self.op/'platform-install'
  if p.exists():shutil.rmtree(p)
  for name in ['platform-install.record','platform-install.record.pending']:
   p=self.op/name
   if p.exists():p.unlink()
 def invoke_phase(self,verb):
  args=self.guard_args();args[1]=verb
  return subprocess.run(args,capture_output=True,text=True,timeout=30)
 def test_exact_install_preserves_all_nonplatform_inputs_and_replays(self):
  self.first();r=self.backup();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  before=self.snapshot();target={rel:(self.op/'platform-migration'/f'file-{i}').read_bytes() for i,rel in enumerate(FILES)}
  changed={str((self.root/'router'/rel).relative_to(self.root)) for rel in FILES}
  r=self.invoke_phase('recovery-install');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  reply=json.loads(r.stdout);self.assertEqual(reply['phase'],'INSTALLED');self.assertFalse(reply['activationAllowed'])
  for rel,body in target.items():
   path=self.root/'router'/rel;self.assertEqual(path.read_bytes(),body);self.assertEqual(stat.S_IMODE(path.stat().st_mode),0o755)
  after=self.snapshot()
  for path,value in before.items():
   if path not in changed:self.assertEqual(after[path],value,path)
  r=self.invoke_phase('recovery-install');self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertTrue(json.loads(r.stdout)['replayed']);self.assertEqual(self.snapshot(),after)
  self.assertTrue((self.op/'fence').is_dir());self.assertFalse((self.updater/'generations').exists())
  print('PLATFORM_INSTALL_RECEIPT '+json.dumps({'files':7,'phase':reply['phase'],'replayedExact':True,'serviceStarted':False,'fenceRetained':True}),flush=True)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([PlatformInstall('test_exact_install_preserves_all_nonplatform_inputs_and_replays')]))
 raise SystemExit(not r.wasSuccessful())
