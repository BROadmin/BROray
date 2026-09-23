"""A recovered publication must fsync its exact files and directory again."""
import json,os,signal,subprocess,time,unittest
from pathlib import Path
import ctypes
from test_generation_migration import Migration,GEN
from test_generation_birth_crash import Registers,trace,traced_start

class Durability(Migration):
 def test_replay_confirms_durability_before_success(self):
  self.assertEqual(self.invoke().returncode,0);before=self.inventory(self.stage)
  args=[GEN,'migration-stage',str(self.stage),str(self.live_root),str(self.payload),self.manifest_sha,'operation-one','nonce-one','running']
  p=subprocess.Popen(args,preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE);self.processes.append(p)
  _,status=os.waitpid(p.pid,0);self.assertTrue(os.WIFSTOPPED(status));trace(0x4200,p.pid,0,1)
  entering=True;pending=None;synced=set();deadline=time.monotonic()+8
  try:
   while time.monotonic()<deadline:
    trace(24,p.pid);_,status=os.waitpid(p.pid,0)
    if os.WIFEXITED(status):p.returncode=os.WEXITSTATUS(status);break
    self.assertTrue(os.WIFSTOPPED(status))
    if os.WSTOPSIG(status)!=(signal.SIGTRAP|0x80):continue
    regs=Registers();trace(12,p.pid,0,ctypes.addressof(regs))
    if regs.orig_rax==74:
     if entering:pending=os.readlink(f'/proc/{p.pid}/fd/{regs.rdi}')
     elif regs.rax==0 and pending:synced.add(pending);pending=None
    entering=not entering
   self.assertEqual(p.returncode,0,'replay did not complete');self.assertEqual(self.inventory(self.stage),before)
   required={str(self.stage.parent),str(self.stage),*[str(self.stage/n) for n in before]}
   print('MIGRATION_DURABILITY_RECEIPT '+json.dumps({'required':sorted(required),'successfulFsync':sorted(synced)}),flush=True)
   self.assertTrue(required<=synced,'missing durable replay confirmations: '+repr(sorted(required-synced)))
  finally:
   if p.returncode is None:
    os.kill(p.pid,signal.SIGKILL)
    try:os.waitpid(p.pid,0)
    except ChildProcessError:pass
    p.returncode=-signal.SIGKILL
   p.stdout.close();p.stderr.close()

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([Durability('test_replay_confirms_durability_before_success')]))
 raise SystemExit(0 if r.wasSuccessful() else 1)
