"""A checked canonical replay must not recreate evidence removed before native admission."""
import re,shutil,unittest
from test_generation_runtime_evidence import RuntimeEvidence
from test_generation_migration_crash import trace_string
from test_preflight_service_binding import CODE

class RuntimeReplayRace(RuntimeEvidence):
 def setUp(self):
  super().setUp()
  source=(CODE/'lib/operation-platform-bootguard.sh').read_text().split('ops_platform_bootguard_stage()',1)[0]
  verbs=re.findall(r'response="\$\("\$generation" (runtime-[a-z]+) ',source)
  self.assertTrue(verbs,'no canonical runtime command');self.replay_verb=verbs[-1]
 def guard_args(self):return [__import__('test_updater_generation').GEN,self.replay_verb,str(self.store),self.sha]
 def test_full_entry_removed_during_replay_is_not_reconstructed(self):
  self.assertEqual(self.invoke().returncode,0);self.assertTrue((self.entry/'identity.json').is_file())
  def match(pid,r,entering):return entering and r.orig_rax==262 and trace_string(pid,r.rsi)==self.sha
  rc,out,err=self.intercepted(match,lambda:shutil.rmtree(self.entry))
  self.assertNotEqual(rc,0,'canonical replay recreated a whole missing runtime evidence directory')
  self.assertFalse(self.entry.exists(),'missing evidence must remain missing after failed replay')

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([RuntimeReplayRace('test_full_entry_removed_during_replay_is_not_reconstructed')]))
 raise SystemExit(not r.wasSuccessful())
