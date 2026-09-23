"""Kill supervisor at actual Linux syscalls; no production crash hooks.
Only the test-owned supervisor is syscall-traced; its fork children are traced
exclusively by the production supervisor. x86_64 fixture, not physical ARM QA.
"""
from pathlib import Path
import ctypes,json,os,resource,signal,subprocess,time,unittest
from test_updater_generation import Generation,GEN

libc=ctypes.CDLL(None,use_errno=True);libc.ptrace.restype=ctypes.c_long
class Registers(ctypes.Structure):
 _fields_=[(s,ctypes.c_ulonglong) for s in 'r15 r14 r13 r12 rbp rbx r11 r10 r9 r8 rax rcx rdx rsi rdi orig_rax rip cs eflags rsp ss fs_base gs_base ds es fs gs'.split()]
def trace(req,pid,addr=0,data=0):
 ctypes.set_errno(0);r=libc.ptrace(ctypes.c_ulong(req),ctypes.c_ulong(pid),ctypes.c_void_p(addr),ctypes.c_void_p(data))
 if r==-1 and ctypes.get_errno():raise OSError(ctypes.get_errno(),os.strerror(ctypes.get_errno()))
 return r
def traced_start():
 resource.setrlimit(resource.RLIMIT_NOFILE,(128,128));trace(0,0)

class BirthCrash(Generation):
 def crash_boundary(self,boundary):
  script=self.home/'daemon.sh';script.write_text('echo UNSAFE >"$TEST_HOME/executed"\nwhile :; do sleep 2; done\n')
  log=open(self.home/'birth.log','wb');self.logs.append(log)
  p=subprocess.Popen([GEN,'run',str(self.domain),self.gid,self.sha,'--','/bin/ash',str(script)],preexec_fn=traced_start,env={**os.environ,'TEST_HOME':str(self.home)},stdout=log,stderr=log)
  self.processes.append(p);pid,status=os.waitpid(p.pid,0);self.assertTrue(os.WIFSTOPPED(status));trace(0x4200,p.pid,0,1) # TRACESYSGOOD, no fork tracing
  entering=True;matched=False;child=None;deadline=time.monotonic()+8
  try:
   while time.monotonic()<deadline:
    trace(24,p.pid) # SYSCALL
    _,status=os.waitpid(p.pid,0)
    self.assertTrue(os.WIFSTOPPED(status),f'early exit {status}')
    if os.WSTOPSIG(status)!=(signal.SIGTRAP|0x80):continue
    regs=Registers();trace(12,p.pid,0,ctypes.addressof(regs))
    if entering:
     if boundary=='before-registration' and regs.orig_rax==101 and regs.rdi==0x4206:
      child=regs.rsi;matched=True;break
     if boundary=='after-registration' and regs.orig_rax==1 and regs.rdx==1:
      word=trace(2,p.pid,regs.rsi)&255
      if word==ord('G'):
       s=self.state();self.assertEqual(s['state'],'STARTING');child=s['updater']['pid'];self.assertTrue(any(x['pid']==child for x in s['children']));matched=True;break
    entering=not entering
   self.assertTrue(matched,'syscall boundary was not reached')
   child_before=Path(f'/proc/{child}/stat').read_text().rsplit(') ',1)[1].split()[19]
   os.kill(p.pid,signal.SIGKILL)
   while True:
    _,status=os.waitpid(p.pid,0)
    if os.WIFSIGNALED(status) or os.WIFEXITED(status):break
    trace(7,p.pid,0,signal.SIGKILL)
   p.returncode=-signal.SIGKILL
   self.wait(lambda:not self.live(child));self.assertFalse((self.home/'executed').exists())
   self.assertNotEqual(self.state()['state'],'STOPPED')
   print('BIRTH_CRASH_EVIDENCE '+json.dumps({'boundary':boundary,'supervisor':p.pid,'child':child,'childStartTicks':child_before,'childAliveAfterCrash':self.live(child),'updaterUserCodeExecuted':False}),flush=True)
  finally:
   if p.returncode is None:
    os.kill(p.pid,signal.SIGKILL)
    try:os.waitpid(p.pid,0)
    except ChildProcessError:pass
    p.returncode=-signal.SIGKILL
 def test_death_before_registration(self):self.crash_boundary('before-registration')
 def test_death_after_registration_before_gate(self):self.crash_boundary('after-registration')

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([BirthCrash('test_death_before_registration'),BirthCrash('test_death_after_registration_before_gate')]));raise SystemExit(0 if result.wasSuccessful() else 1)
