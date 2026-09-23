"""Public start after completed public stop; no migration state reopening."""
import hashlib,json,subprocess,unittest
from pathlib import Path
from test_installed_generation_stop import InstalledGenerationStop

class InstalledStartAfterStop(InstalledGenerationStop):
 def test_start_after_stop_creates_fresh_supervised_generation(self):
  self.installed();r=self.init('start');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  first=json.loads(r.stdout)['generationId'];platform=self.bytes_now()
  r=self.init('stop');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  before={str(p.relative_to(self.op)):(hashlib.sha256(p.read_bytes()).hexdigest(),p.stat().st_mode&0o7777) for p in self.op.rglob('*') if p.is_file() and not p.is_symlink()}
  r=self.init('start')
  print('PUBLIC_START_AFTER_STOP '+json.dumps({'returnCode':r.returncode,'stdout':r.stdout,'stderr':r.stderr}),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  reply=json.loads(r.stdout);self.assertTrue(reply['platformReady']);self.assertNotEqual(reply['generationId'],first)
  self.assertEqual(self.bytes_now(),platform)
  self.assertEqual({str(p.relative_to(self.op)):(hashlib.sha256(p.read_bytes()).hexdigest(),p.stat().st_mode&0o7777) for p in self.op.rglob('*') if p.is_file() and not p.is_symlink()},before)
  r=self.init('status');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual(json.loads(r.stdout)['generationId'],reply['generationId'])
  r=self.init('stop');self.assertEqual(r.returncode,0,r.stdout+r.stderr)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([InstalledStartAfterStop('test_start_after_stop_creates_fresh_supervised_generation')]))
 raise SystemExit(not r.wasSuccessful())
