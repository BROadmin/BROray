"""Reuse real Linux PIDs in the isolated VM. No fake /proc identity oracle."""
from pathlib import Path
import json,os,signal,time,unittest
from test_updater_generation import Generation

class PidReuse(Generation):
 def replacement(self,pid):
  Path('/proc/sys/kernel/ns_last_pid').write_text(str(pid-1))
  child=os.fork()
  if child==0:
   while True:time.sleep(1)
  self.assertEqual(child,pid,'fixture must actually reuse exact PID')
  return child
 def reap_replacement(self,pid):
  # This is our own unreaped direct child, never an arbitrary PID lookup.
  os.kill(pid,signal.SIGKILL);os.waitpid(pid,0)
 def test_dead_updater_pid_reused_then_stop_replayed(self):
  self.start('while :; do :; done\n');old=self.running()['updater']
  self.assertEqual(self.call('STOP').returncode,0);self.stopped()
  replacement=self.replacement(old['pid'])
  try:
   birth=Path(f'/proc/{replacement}/stat').read_text().rsplit(') ',1)[1].split()[19]
   self.assertNotEqual(birth,old['startTicks'])
   self.assertEqual(self.call('STOP').returncode,0);self.assertTrue(self.live(replacement))
   self.assertEqual(self.state()['children'],[])
   print('REAL_PID_REUSE '+json.dumps({'kind':'updater','pid':replacement,'oldTicks':old['startTicks'],'newTicks':birth,'foreignAlive':True}),flush=True)
  finally:self.reap_replacement(replacement)
 def test_dead_writer_pid_reused_before_generation_stop(self):
  (self.home/'fork.py').write_text('''import os,time
from pathlib import Path
h=Path(os.environ['TEST_HOME'])
child=os.fork()
if child==0:
 (h/'writer.pid.tmp').write_text(str(os.getpid()))
 (h/'writer.pid.tmp').rename(h/'writer.pid')
 while not (h/'release-writer').exists():time.sleep(.01)
 os._exit(0)
os.waitpid(child,0)
(h/'reaped').touch()
while True:time.sleep(.1)
''')
  self.start('exec /usr/bin/python3 "$TEST_HOME/fork.py"\n')
  pid=self.wait(lambda:int((self.home/'writer.pid').read_text()))
  old=Path(f'/proc/{pid}/stat').read_text().rsplit(') ',1)[1].split()[19]
  (self.home/'release-writer').touch();self.wait(lambda:(self.home/'reaped').exists())
  def writer_reaped():
   live=self.live_state();return all(x['pid']!=pid for x in live['children']+live['exitedUnreaped'])
  self.wait(writer_reaped)
  replacement=self.replacement(pid)
  try:
   birth=Path(f'/proc/{pid}/stat').read_text().rsplit(') ',1)[1].split()[19];self.assertNotEqual(old,birth)
   self.assertEqual(self.call('STOP').returncode,0);self.stopped();self.assertTrue(self.live(pid))
   print('REAL_PID_REUSE '+json.dumps({'kind':'writer','pid':pid,'oldTicks':old,'newTicks':birth,'foreignAlive':True}),flush=True)
  finally:self.reap_replacement(replacement)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([PidReuse('test_dead_updater_pid_reused_then_stop_replayed'),PidReuse('test_dead_writer_pid_reused_before_generation_stop')]));raise SystemExit(0 if result.wasSuccessful() else 1)
