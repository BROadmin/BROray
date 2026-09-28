"""Ended-boot handling preserves corrupt/incomplete history, never invents READY."""
import json,subprocess,unittest
from test_cycle_running_boot_resume import CycleRunningBootResume

class UnreadyBootGuards(CycleRunningBootResume):
 def setUp(self):
  super().setUp()
  # Each independent case restores old socket pathnames without kernel peers.
  # Remove only the exact socket inode created by this fixture between cases.
  sockets=[(self.root/r['path'],(self.root/r['path']).lstat().st_ino) for r in self.e.get('socketPaths',[])]
  def remove_sockets():
   for p,inode in sockets:
    self.assertTrue(p.is_socket());self.assertEqual(p.lstat().st_ino,inode);p.unlink()
  self.addCleanup(remove_sockets)
 def refuse(self,target,remove=False):
  body=target.read_bytes();mode=target.stat().st_mode&0o777
  if remove:target.unlink()
  else:target.write_bytes(b'{broken')
  before=self.files(self.updater);origin=self.files(self.op)
  foreign=subprocess.Popen(['/bin/ash','-c','while :; do sleep 1; done','xray-foreign-fixture'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  try:
   r=self.init('start');self.assertNotEqual(r.returncode,0,r.stdout+r.stderr)
   self.assertEqual(self.files(self.updater),before);self.assertEqual(self.files(self.op),origin)
   self.assertIsNone(foreign.poll());print('UNREADY_GUARD_REFUSED '+json.dumps(dict(target=str(target),removed=remove,rc=r.returncode)),flush=True)
  finally:
   target.write_bytes(body);target.chmod(mode)
   foreign.terminate();foreign.wait(timeout=5)
 def test_corrupt_ledger_preserved(self):
  self.refuse(sorted((self.updater/'generations'/self.e['stoppedGeneration']).glob('revision-*.json'))[-1])
 def test_missing_witness_refused(self):
  self.refuse(sorted((self.updater/'starts'/self.e['stoppedGeneration']/'ledger-witnesses').glob('revision-*.json'))[-1],True)
 def test_ledger_tail_rollback_refused(self):
  self.refuse(sorted((self.updater/'generations'/self.e['stoppedGeneration']).glob('revision-*.json'))[-1],True)
 def test_lost_ready_is_not_never_ready(self):
  self.assertFalse(self.e.get('unreadyBoot'))
  self.refuse(self.updater/'cycles'/('ready-'+self.e['stoppedGeneration']+'.record'),True)

if __name__=='__main__':
 value=json.loads(__import__('pathlib').Path('/work/migration-transfer.json').read_bytes())
 names=['test_corrupt_ledger_preserved','test_missing_witness_refused','test_ledger_tail_rollback_refused'] if value.get('unreadyBoot') else ['test_lost_ready_is_not_never_ready']
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(UnreadyBootGuards(n) for n in names))
 raise SystemExit(not r.wasSuccessful())
