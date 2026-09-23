"""Postboot native START_INTENT accepts the verified root Entware interpreter."""
import os,stat,unittest
from pathlib import Path
from test_native_platform_start_intent import StartIntent

class EntwareStartIntent(StartIntent):
 def test_root_setuid_interpreter_start_intent_exact_replay(self):
  shell=Path(os.path.realpath(self.root/'router/opt/bin/ash'))
  self.assertEqual(shell.stat().st_uid,0)
  mode=stat.S_IMODE(shell.stat().st_mode)
  try:
   shell.chmod(0o4755)
   self.test_start_intent_is_bound_durable_and_exact_on_replay()
  finally:shell.chmod(mode)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([EntwareStartIntent('test_root_setuid_interpreter_start_intent_exact_replay')]))
 raise SystemExit(not r.wasSuccessful())
