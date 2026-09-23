"""Prove helper tracing reaches a nested exec and never substitutes the root."""
import json,sys,time,unittest
from test_public_stop_trace import PublicStopTrace
class TraceProbe(unittest.TestCase):
 def command(self):
  inner="import subprocess;subprocess.run(['/bin/ash','-c','exit 0','guard-boundary-fixture'],check=True)"
  return [sys.executable,'-c','import subprocess,sys;subprocess.run([sys.executable,"-c",'+repr(inner)+'],check=True)']
 def exercise(self,kill):
  t=PublicStopTrace(self,self.command())
  try:
   deadline=time.monotonic()+10
   pid=t.wait_exec(lambda a:b'guard-boundary-fixture' in a,deadline)
   self.assertEqual(t.tasks[t.tasks[pid]['parent']]['parent'],t.p.pid)
   self.assertGreaterEqual(len(t.history),3)
   if kill:self.assertEqual(t.kill_command().returncode,-9)
   else:
    t._resume(pid);self.assertEqual(t.run_to_completion(deadline).returncode,0)
   self.assertFalse(t.tasks)
  finally:t.close()
 def test_nested_exec_and_exact_command_interruption(self):self.exercise(True)
 def test_nested_exec_sigchld_forwarding_and_completion(self):self.exercise(False)
if __name__=='__main__':unittest.main(verbosity=2)
