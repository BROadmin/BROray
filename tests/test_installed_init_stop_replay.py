"""A lost successful init-stop reply cannot cause a second stop or new fence."""
import json,unittest
from test_installed_init_stop import InstalledInitStop

class InstalledInitStopReplay(InstalledInitStop):
 def test_public_stop_exact_replay(self):
  self.test_public_stop_preserves_origin_platform_and_foreign_process()
  before=self.snapshot()
  r=self.init('stop')
  print('INSTALLED_PUBLIC_STOP_REPLAY '+json.dumps({'returnCode':r.returncode,'stdout':r.stdout,'stderr':r.stderr}),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  reply=json.loads(r.stdout)
  self.assertEqual(reply['phase'],'SERVICE_STOP_COMPLETED');self.assertTrue(reply['serviceStopped'])
  self.assertFalse(reply['platformReady']);self.assertTrue(reply['replayed'])
  self.assertEqual(self.snapshot(),before,'completed init-stop reply must not mutate evidence or create another operation')

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([InstalledInitStopReplay('test_public_stop_exact_replay')]))
 raise SystemExit(not r.wasSuccessful())
