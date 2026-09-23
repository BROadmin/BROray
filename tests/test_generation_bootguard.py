"""Local legacy boot-guard staging; never claims STOPPED or boot activation."""
from pathlib import Path
import hashlib,json,os,subprocess,unittest
from test_generation_migration import Migration,FILES,GEN

GUARDED=[1,5]
class BootGuard(Migration):
 def setUp(self):
  super().setUp();r=self.invoke();self.assertEqual(r.returncode,0,r.stderr)
  self.intent_sha=hashlib.sha256((self.stage/'intent.record').read_bytes()).hexdigest()
  self.guards=self.home/'guards';self.guards.mkdir(mode=0o700)
  self.original={i:(self.live_root/FILES[i]).read_bytes() for i in GUARDED}
 def guard_args(self,verb='guard-stage',sha=None):
  return [GEN,verb,str(self.guards),str(self.live_root),str(self.stage),sha or self.intent_sha]
 def guard(self,verb='guard-stage',sha=None):
  return subprocess.run(self.guard_args(verb,sha),capture_output=True,text=True,timeout=5)
 def test_stage_guards_preserves_old_inodes(self):
  before=self.inventory(self.live_root);held=[open(self.live_root/FILES[i],'rb') for i in GUARDED]
  try:
   r=self.guard();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
   report=json.loads(r.stdout);self.assertEqual(report['phase'],'BOOT_GUARDS_STAGED')
   self.assertFalse(report['serviceStopped']);self.assertFalse(report['activationAllowed'])
   for i,f in zip(GUARDED,held):
    self.assertEqual(f.read(),self.original[i]);self.assertEqual((self.guards/f'before-{i}').read_bytes(),self.original[i])
    p=self.live_root/FILES[i];self.assertEqual(p.stat().st_mode&0o777,0o755)
    for action in ['start','daemon','restart']:
     q=subprocess.run(['/bin/ash',str(p),action],capture_output=True,text=True,timeout=3)
     self.assertEqual(q.returncode,75);self.assertIn('MIGRATION_ACTIVATION_PENDING',q.stderr)
   for i,p in enumerate(FILES):
    if i not in GUARDED:self.assertEqual(self.inventory(self.live_root)[p],before[p])
  finally:
   for f in held:f.close()
 def test_exact_replay_and_verify_preserve_all_bytes(self):
  self.assertEqual(self.guard().returncode,0);before=self.inventory(self.home)
  self.assertEqual(self.guard().returncode,0);self.assertEqual(self.guard('guard-verify').returncode,0)
  self.assertEqual(self.inventory(self.home),before)
 def test_changed_entry_preserved_and_not_repaired(self):
  self.assertEqual(self.guard().returncode,0);p=self.live_root/FILES[1];p.write_bytes(b'FOREIGN')
  before=self.inventory(self.home);self.assertNotEqual(self.guard().returncode,0);self.assertEqual(self.inventory(self.home),before)
 def test_missing_committed_entry_not_recreated(self):
  self.assertEqual(self.guard().returncode,0);(self.live_root/FILES[5]).unlink()
  before=self.inventory(self.home);self.assertNotEqual(self.guard().returncode,0);self.assertEqual(self.inventory(self.home),before)
 def test_wrong_migration_binding_no_mutation(self):
  before=self.inventory(self.home);self.assertNotEqual(self.guard(sha='0'*64).returncode,0);self.assertEqual(self.inventory(self.home),before)
 def test_corrupt_guard_intent_preserved(self):
  self.assertEqual(self.guard().returncode,0);(self.guards/'intent.record').write_bytes(b'{broken')
  before=self.inventory(self.home);self.assertNotEqual(self.guard().returncode,0);self.assertEqual(self.inventory(self.home),before)
 def test_missing_guard_intent_not_recreated(self):
  self.assertEqual(self.guard().returncode,0);(self.guards/'intent.record').unlink()
  before=self.inventory(self.home);self.assertNotEqual(self.guard().returncode,0);self.assertEqual(self.inventory(self.home),before)
 def test_corrupt_migration_evidence_preserved(self):
  (self.stage/'file-0').write_bytes(b'FOREIGN');before=self.inventory(self.home)
  self.assertNotEqual(self.guard().returncode,0);self.assertEqual(self.inventory(self.home),before)
 def test_symlink_entry_refused_before_mutation(self):
  p=self.live_root/FILES[1];p.unlink();p.symlink_to(self.live_root/FILES[0]);before=self.inventory(self.home)
  self.assertNotEqual(self.guard().returncode,0);self.assertEqual(self.inventory(self.home),before);self.assertTrue(p.is_symlink())
 def test_changed_unrelated_platform_refused_before_mutation(self):
  (self.live_root/FILES[2]).write_bytes(b'FOREIGN');before=self.inventory(self.home)
  self.assertNotEqual(self.guard().returncode,0);self.assertEqual(self.inventory(self.home),before)

if __name__=='__main__':
 names=[n for n in BootGuard.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(BootGuard(n) for n in names))
 raise SystemExit(not result.wasSuccessful())
