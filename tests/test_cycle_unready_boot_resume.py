"""Exact never-ready evidence survives a real new boot and permits a fresh start."""
import json,subprocess,unittest
from test_cycle_running_boot_resume import CycleRunningBootResume

class UnreadyBootResume(CycleRunningBootResume):
 def setUp(self):
  super().setUp()
  # A reboot remounts Entware's temporary workspace in RAM. The serialized
  # filesystem export does not transfer mounts; use the same real tmpfs as
  # InstalledInit, without changing the production RAM admission check.
  temporary=self.root/'router/tmp'
  subprocess.run(['mount','-t','tmpfs','-o','mode=700','tmpfs',str(temporary)],check=True,capture_output=True)
  def unmount():
   self.stop_created_generation()
   subprocess.run(['umount',str(temporary)],check=True,capture_output=True)
  self.addCleanup(unmount)
 def test_unready_history_recovers_without_forged_ready(self):
  self.assertTrue(self.e['unreadyBoot']);gid=self.e['stoppedGeneration']
  origin=self.files(self.op);platform=self.bytes_now();domain=self.updater/'generations'/gid
  old=self.files(domain);self.assertFalse((self.updater/'cycles'/('ready-'+gid+'.record')).exists())
  before=self.files(self.updater);r=self.init('status');self.assertNotEqual(r.returncode,0)
  self.assertEqual(self.files(self.updater),before)
  current=self.success('start');self.assertNotEqual(current['generationId'],gid);self.one_live(current['generationId'])
  now=self.files(domain);self.assertEqual({k:now[k] for k in old},old)
  self.assertEqual(set(now)-set(old),{'boot-ended.receipt'})
  self.assertTrue((domain/'boot-ended.receipt').read_text().startswith('BROray-generation-unready-boot-ended/1\n'))
  self.assertFalse((self.updater/'cycles'/('ready-'+gid+'.record')).exists(),'never fabricate readiness')
  self.assertEqual(self.success('start')['generationId'],current['generationId'])
  self.success('restart');self.success('stop');self.assertEqual(self.files(self.op),origin);self.assertEqual(self.bytes_now(),platform)
  print('UNREADY_BOOT_RECOVERY_PASS '+json.dumps(dict(oldBoot=self.e['oldBootId'],newBoot=self.boot,oldGeneration=gid,newGeneration=current['generationId'],historyUnchanged=True)),flush=True)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([UnreadyBootResume('test_unready_history_recovers_without_forged_ready')]))
 raise SystemExit(not r.wasSuccessful())
