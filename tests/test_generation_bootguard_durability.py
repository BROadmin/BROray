"""A lost staging reply must not acknowledge non-durable files on replay."""
import ctypes,json,os,signal,subprocess,time,unittest
from test_generation_bootguard import BootGuard,FILES
from test_generation_birth_crash import Registers,trace,traced_start

class GuardDurability(BootGuard):
 def trace_sync(self,args):
  p=subprocess.Popen(args,preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE);self.processes.append(p)
  _,status=os.waitpid(p.pid,0);self.assertTrue(os.WIFSTOPPED(status));trace(0x4200,p.pid,0,1)
  entering=True;pending=None;synced=set();deadline=time.monotonic()+12
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
   self.assertEqual(p.returncode,0,'staging/replay did not complete')
   required={str(self.guards),str(self.guards.parent),*[str(p) for p in self.guards.iterdir()]}
   for i in [1,5]:required.update([str(self.live_root/FILES[i]),str((self.live_root/FILES[i]).parent)])
   print('BOOTGUARD_DURABILITY_RECEIPT '+json.dumps({'required':sorted(required),'successfulFsync':sorted(synced)}),flush=True)
   self.assertTrue(required<=synced,'missing durable confirmation: '+repr(sorted(required-synced)))
  finally:
   if p.returncode is None:
    os.kill(p.pid,signal.SIGKILL)
    try:os.waitpid(p.pid,0)
    except ChildProcessError:pass
    p.returncode=-signal.SIGKILL
   p.stdout.close();p.stderr.close()
 def test_replay_confirms_durable_evidence_and_guards(self):
  self.assertEqual(self.guard().returncode,0);before=self.inventory(self.home)
  self.trace_sync(self.guard_args());self.assertEqual(self.inventory(self.home),before)
 def test_first_staging_confirms_durable_evidence_and_guards(self):
  self.trace_sync(self.guard_args())

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(GuardDurability(n) for n in GuardDurability.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
