"""Adversarial clone flag: no updater descendant may opt out of tracing."""
from pathlib import Path
import json,os,time,unittest
from test_updater_generation import Generation

class CloneFlags(Generation):
 def test_clone_untraced_cannot_create_writer_outside_generation(self):
  (self.home/'clone.py').write_text('''import ctypes,os,time
from pathlib import Path
h=Path(os.environ['TEST_HOME']);libc=ctypes.CDLL(None,use_errno=True)
# x86_64 isolated guest only: syscall clone(flags, stack, ptid, ctid, tls).
pid=libc.syscall(56,0x00800000|17,0,0,0,0)
if pid==-1:
 (h/'refused').write_text(str(ctypes.get_errno()))
elif pid==0:
 (h/'escaped.pid.tmp').write_text(str(os.getpid()))
 (h/'escaped.pid.tmp').rename(h/'escaped.pid')
 while not (h/'release').exists():time.sleep(.02)
 (h/'platform').write_text('CORRUPT')
 os._exit(0)
else:(h/'created').write_text(str(pid))
while True:time.sleep(.1)
''')
  (self.home/'platform').write_text('original')
  self.start('exec /usr/bin/python3 "$TEST_HOME/clone.py"\n')
  self.wait(lambda:(self.home/'refused').exists() or (self.home/'escaped.pid').exists())
  pid=int((self.home/'escaped.pid').read_text()) if (self.home/'escaped.pid').exists() else None
  self.assertEqual(self.call('STOP').returncode,0);self.stopped()
  alive=pid is not None and self.live(pid)
  (self.home/'release').touch();time.sleep(.15)
  print('CLONE_UNTRACED_EVIDENCE '+json.dumps({'childPid':pid,'childAliveAfterStopped':alive,'cloneRefused':(self.home/'refused').exists(),'platform':(self.home/'platform').read_text()}),flush=True)
  self.assertFalse(alive,'CLONE_UNTRACED_WRITER_ESCAPED_GENERATION')
  self.assertEqual((self.home/'platform').read_text(),'original','No writer may mutate platform after STOPPED')

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([CloneFlags('test_clone_untraced_cannot_create_writer_outside_generation')]));raise SystemExit(0 if result.wasSuccessful() else 1)
