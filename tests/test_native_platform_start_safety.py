"""Unknown/missing start evidence must survive exact refusal without launch."""
import json,unittest
from test_native_platform_start_intent import StartIntent

class StartSafety(StartIntent):
 def installed(self):
  self.first();r=self.backup();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  r=self.invoke_phase('recovery-install');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
 def prepared(self):
  self.installed();r=self.invoke_phase('recovery-start-intent');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  return self.updater/'starts'/json.loads(r.stdout)['generationId']
 def refuse_start(self):
  before=self.snapshot();r=self.invoke_phase('recovery-start-intent');self.assertNotEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual(self.snapshot(),before);self.assertFalse((self.updater/'generations').exists());self.assertTrue((self.op/'fence').is_dir())
 def test_corrupt_launch_bytes_preserved(self):
  p=self.prepared()/'launch.record';p.write_bytes(b'{broken');self.refuse_start();self.assertEqual(p.read_bytes(),b'{broken')
 def test_missing_launch_not_recreated(self):
  p=self.prepared()/'launch.record';p.unlink();self.refuse_start();self.assertFalse(p.exists())
 def test_corrupt_transaction_preserved(self):
  p=self.prepared()/'transaction.record';p.write_bytes(b'{broken');self.refuse_start();self.assertEqual(p.read_bytes(),b'{broken')
 def test_missing_external_binding_not_recreated(self):
  self.prepared();p=self.op/'platform-start.record';p.unlink();self.refuse_start();self.assertFalse(p.exists())
 def test_foreign_start_directory_refused_before_binding(self):
  self.installed();p=self.updater/'starts';p.mkdir(mode=0o700);(p/'foreign').write_bytes(b'KEEP')
  self.refuse_start();self.assertEqual((p/'foreign').read_bytes(),b'KEEP');self.assertFalse((self.op/'platform-start.record').exists())
 def test_incomplete_install_cannot_authorize_start(self):
  self.installed();p=self.op/'platform-install/installed.receipt';p.unlink();self.refuse_start()
  self.assertFalse((self.op/'platform-start.record').exists());self.assertFalse((self.updater/'starts').exists());self.assertFalse(p.exists())

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(StartSafety(n) for n in StartSafety.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
