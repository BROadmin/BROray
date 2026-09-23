"""Corrupt the real coordinator's binding at a deterministic native boundary."""
import hashlib,re,subprocess,unittest
from test_generation_bootguard_crash import GuardCrash
from test_generation_migration import Migration,FILES,GEN
from test_preflight_service_binding import CODE

class GuardBinding(GuardCrash):
 def setUp(self):
  Migration.setUp(self)
  self.stage.rmdir();self.op=self.home/'operation-one';self.op.mkdir(mode=0o700)
  self.stage=self.op/'platform-migration';self.stage.mkdir(mode=0o700)
  self.guards=self.op/'platform-bootguard';self.guards.mkdir(mode=0o700)
  r=self.invoke();self.assertEqual(r.returncode,0,r.stderr)
  self.intent_sha=hashlib.sha256((self.stage/'intent.record').read_bytes()).hexdigest()
  self.original={i:(self.live_root/FILES[i]).read_bytes() for i in [1,5]}
  # Follow the actual production call contract, not a separate invented path.
  source=(CODE/'lib/operation-platform-bootguard.sh').read_text()
  matches=re.findall(r'response="\$\("\$generation" (guard-stage[-a-z]*) ',source)
  self.assertEqual(len(matches),1);self.stage_verb=matches[0]
  self.binding=self.op/'platform-bootguard.json'
  r=self.bind();self.assertEqual(r.returncode,0,r.stderr)
 def bind(self):return subprocess.run([GEN,'guard-bind',str(self.op),str(self.live_root),str(self.stage),self.intent_sha],capture_output=True,text=True,timeout=5)
 def guard_args(self,verb=None,sha=None):
  return [GEN,verb or self.stage_verb,str(self.guards),str(self.live_root),str(self.stage),sha or self.intent_sha]
 def stage_bound(self):return subprocess.run(self.guard_args(),capture_output=True,text=True,timeout=5)
 def test_corrupt_binding_during_last_move_refuses_success(self):
  rc,out,err=self.intercepted(self.move_match(5,'new','before'),lambda:self.binding.write_bytes(b'{broken'))
  self.assertNotEqual(rc,0,'coordinator binding changed during platform mutation')
  self.assertEqual(self.binding.read_bytes(),b'{broken');self.assertFalse((self.guards/'staged.receipt').exists())
 def test_missing_binding_before_mutation_is_not_ignored(self):
  self.binding.unlink();before=self.inventory(self.home);r=self.stage_bound()
  self.assertNotEqual(r.returncode,0);self.assertEqual(self.inventory(self.home),before)
 def test_corrupt_binding_before_mutation_is_not_ignored(self):
  self.binding.write_bytes(b'{broken');before=self.inventory(self.home);r=self.stage_bound()
  self.assertNotEqual(r.returncode,0);self.assertEqual(self.inventory(self.home),before)
 def test_bind_replay_cannot_overwrite_foreign_bytes(self):
  self.binding.write_bytes(b'{broken');before=self.inventory(self.home);r=self.bind()
  self.assertNotEqual(r.returncode,0);self.assertEqual(self.inventory(self.home),before)
 def test_exact_bound_replay_is_read_only(self):
  self.assertEqual(self.stage_bound().returncode,0);before=self.inventory(self.home)
  self.assertEqual(self.bind().returncode,0);self.assertEqual(self.stage_bound().returncode,0);self.assertEqual(self.inventory(self.home),before)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(GuardBinding(n) for n in GuardBinding.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
