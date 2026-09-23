"""Real native service host with Entware's root-owned setuid BusyBox mode.

Only an isolated copy of the VM interpreter is changed. No host/router files.
"""
from pathlib import Path
import hashlib,os,shutil,subprocess,unittest
from test_generation_service_launcher import ServiceLauncher

class EntwareInterpreter(ServiceLauncher):
 def setUp(self):
  super().setUp()
  target=self.home/'entware-busybox';shutil.copyfile(self.ash,target)
  target.chmod(0o4755);self.ash=str(target);self.ashsha=hashlib.sha256(target.read_bytes()).hexdigest()
 def refuse(self,mode=None,prepare=None):
  if mode is not None:Path(self.ash).chmod(mode)
  if prepare:prepare()
  r=subprocess.run(self.hostargs(),capture_output=True,text=True,timeout=5)
  self.assertNotEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertIn('ASH_BYTES_UNCONFIRMED',r.stderr)
  self.assertEqual(list(self.hostdir.iterdir()),[])
 def test_root_setuid_busybox_preserves_service_and_generation_contract(self):
  self.assertEqual(os.getuid(),0);self.assertEqual(os.geteuid(),0)
  self.test_app_service_survives_generation_stop()
 def test_setgid_is_refused(self):self.refuse(0o6755)
 def test_sticky_is_refused(self):self.refuse(0o5755)
 def test_group_writable_is_refused(self):self.refuse(0o4775)
 def test_world_writable_is_refused(self):self.refuse(0o4757)
 def test_non_executable_is_refused(self):self.refuse(0o4644)
 def test_multiple_links_are_refused(self):self.refuse(prepare=lambda:os.link(self.ash,self.home/'second-link'))
 def test_foreign_owner_is_refused(self):self.refuse(prepare=lambda:os.chown(self.ash,1,1))
 def test_wrong_hash_is_refused(self):self.ashsha='f'*64;self.refuse()
 def test_real_uid_must_also_be_root(self):
  r=subprocess.run(self.hostargs(),capture_output=True,text=True,timeout=5,preexec_fn=lambda:os.setresuid(1,0,0))
  self.assertNotEqual(r.returncode,0,r.stdout+r.stderr);self.assertIn('ASH_BYTES_UNCONFIRMED',r.stderr)
  self.assertEqual(list(self.hostdir.iterdir()),[])

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(EntwareInterpreter(n) for n in EntwareInterpreter.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
