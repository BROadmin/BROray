"""Directed probe for the previously observed UNREGISTERED_EXIT; same native.
Real ptrace exit notification precedes the real parent's later zombie reap.
"""
import json,os,socket,time,unittest
from test_updater_generation import Generation

class ExitRace(Generation):
 def test_adopt_previously_observed_zombie(self):
  (self.home/'reap.py').write_text('''import os,time
from pathlib import Path
h=Path(os.environ['TEST_HOME'])
middle=os.fork()
if middle==0:
 leaf=os.fork()
 if leaf==0:
  (h/'leaf.pid.tmp').write_text(str(os.getpid()))
  (h/'leaf.pid.tmp').rename(h/'leaf.pid')
  os._exit(0)
 while not (h/'release-middle').exists():time.sleep(.02)
 os._exit(0)
while True:time.sleep(.1)
''')
  p=self.start('exec /usr/bin/python3 "$TEST_HOME/reap.py"\n')
  leaf=self.wait(lambda:int((self.home/'leaf.pid').read_text()))
  self.wait(lambda:all(x['pid']!=leaf for x in self.state()['children']))
  (self.home/'release-middle').touch()
  time.sleep(.2)
  self.assertIsNone(p.poll(),'A confirmed exit subsequently reparented to subreaper must not become unknown')
  r=self.call('STOP');self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.stopped()
  self.assertIsNone(p.poll())
 def test_control_connected_before_message(self):
  self.start('while :; do :; done\n');self.running()
  with socket.socket(socket.AF_UNIX,socket.SOCK_SEQPACKET) as s:
   s.settimeout(2);s.connect(str(self.domain/'control'));time.sleep(.03)
   s.send(f'STATUS {self.gid} {self.sha} operation-one nonce-one'.encode())
   self.assertEqual(json.loads(s.recv(65536))['generationId'],self.gid)

if __name__=='__main__':
 tests=[ExitRace('test_adopt_previously_observed_zombie'),ExitRace('test_control_connected_before_message')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(tests));raise SystemExit(0 if result.wasSuccessful() else 1)
