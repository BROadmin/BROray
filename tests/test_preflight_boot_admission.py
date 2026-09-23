"""No post-reboot executor may be admitted in the original live boot."""
import subprocess,unittest
from test_preflight_boot_entry import BootEntry

class BootAdmission(BootEntry):
 def test_same_boot_cannot_record_executor_or_stop_live_legacy_service(self):
  self.prepare();before=self.snapshot()
  r=subprocess.run([str(self.native),'recovery-admit',str(self.home/'router'),self.op.name,self.migration_sha,self.nonce],capture_output=True,text=True,timeout=15)
  self.assertNotEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual(self.snapshot(),before);self.assertIsNone(self.service.poll())
  self.assertFalse((self.op/'platform-boot-admission.json').exists())
  self.assertFalse((self.op/'platform-boot-admission.anchor').exists())

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([BootAdmission('test_same_boot_cannot_record_executor_or_stop_live_legacy_service')]))
 raise SystemExit(not r.wasSuccessful())
