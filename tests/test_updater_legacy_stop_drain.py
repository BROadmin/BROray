"""Repeat the drain contract with exact original r12 libraries from the router."""
import hashlib,unittest
from pathlib import Path
import test_updater_stop_drain as current

class LegacyStopDrain(current.StopDrain):
 def setUp(self):
  super().setUp()
  root=Path('/baseline/r12')
  source=(root/'service-lifecycle.sh').read_bytes()
  self.assertEqual(hashlib.sha256(source).hexdigest(),'091140d59a9b1ad8649a4b816a7134a82c705abf2ec853662e8566ae98634511')
  for name in ['service-lifecycle.sh','operation-owner.sh']:(self.app/'lib'/name).write_bytes((root/name).read_bytes())

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
