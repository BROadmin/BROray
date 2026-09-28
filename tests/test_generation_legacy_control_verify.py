"""A verifier never creates evidence or grants authority to stop/activate."""
import json,subprocess,unittest
from test_generation_legacy_control import LegacyControlNative

class LegacyControlVerify(LegacyControlNative):
 def verify(self):
  args=self.control_args();args[1]='legacy-control-verify'
  return subprocess.run(args,capture_output=True,text=True,timeout=5)
 def test_exact_verify_in_original_boot_is_read_only_not_stopped(self):
  self.assertEqual(self.observe().returncode,0);before=self.stable_inventory();r=self.verify()
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);p=json.loads(r.stdout)
  self.assertEqual(p['phase'],'LEGACY_CONTROL_VERIFIED');self.assertFalse(p['oldBootEnded']);self.assertEqual(p['oldBootId'],self.owner['bootId']);self.assertEqual(p['currentBootId'],self.owner['bootId'])
  self.assertFalse(p['signalsAuthorized']);self.assertFalse(p['serviceStopped']);self.assertFalse(p['activationAllowed'])
  self.assertEqual(self.stable_inventory(),before);self.assertIsNone(self.service.poll())
 def test_verify_without_snapshot_has_no_creation_authority(self):
  before=self.stable_inventory();r=self.verify();self.assertNotEqual(r.returncode,0);self.assertEqual(self.stable_inventory(),before)
 def test_verify_missing_binding_cannot_recreate_it(self):
  self.assertEqual(self.observe().returncode,0);self.binding.unlink();before=self.stable_inventory()
  self.assertNotEqual(self.verify().returncode,0);self.assertEqual(self.stable_inventory(),before)
 def test_same_boot_shutdown_projection_loss_is_not_authorized(self):
  self.assertEqual(self.observe().returncode,0)
  (self.updater/'daemon.ready').unlink();before=self.stable_inventory()
  self.assertNotEqual(self.verify().returncode,0);self.assertEqual(self.stable_inventory(),before)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(LegacyControlVerify(n) for n in LegacyControlVerify.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
