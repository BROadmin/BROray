"""Trace only the test-owned native caller; never trace or kill its writer tree.

No seccomp filter is inherited by spawned hosts or supervisors. The public init's existing
120-second invocation budget is retained for these new, whole-start scenarios.
No fault switch or target seccomp requirement is added to production code.
"""
import ctypes,json,os,signal,subprocess,time,struct
from test_generation_birth_crash import Registers,trace,traced_start
from test_link_interceptor_support import Filter,Program


def memory_bytes(pid,address,size):
 if not 0<=size<=16384:raise AssertionError('unbounded fixture read')
 return b''.join((trace(2,pid,address+i)&((1<<64)-1)).to_bytes(8,'little') for i in range(0,size,8))[:size]


def syscall_output(pid,regs):
 if regs.orig_rax==1:return memory_bytes(pid,regs.rsi,regs.rdx)
 if regs.orig_rax!=20:raise AssertionError('not write/writev')
 if not 0<regs.rdx<=8:raise AssertionError('unbounded iovec count')
 vectors=memory_bytes(pid,regs.rsi,regs.rdx*16);parts=[];total=0
 for i in range(regs.rdx):
  pointer,size=struct.unpack_from('<QQ',vectors,i*16);total+=size
  if total>16384:raise AssertionError('unbounded iovec output')
  if size:parts.append(memory_bytes(pid,pointer,size))
 return b''.join(parts)

def crash_native_caller(owner,numbers,match,label):
 p=subprocess.Popen(owner.guard_args(),preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 owner.processes.append(p);_,status=os.waitpid(p.pid,0);owner.assertTrue(os.WIFSTOPPED(status))
 trace(0x4200,p.pid,0,1|0x10);started=time.monotonic();matched=False;seen=0;boundary=None
 try:
  trace(24,p.pid)
  while time.monotonic()-started<120:
   _,status=os.waitpid(p.pid,0)
   if not os.WIFSTOPPED(status):
    p.returncode=os.waitstatus_to_exitcode(status);out,err=p.communicate(timeout=3)
    owner.fail('caller exited before '+label+': '+repr((p.returncode,out,err)))
   sig=os.WSTOPSIG(status)
   if sig==(signal.SIGTRAP|0x80):
    info=(ctypes.c_ubyte*128)();trace(0x420e,p.pid,ctypes.sizeof(info),ctypes.addressof(info))
    regs=Registers();trace(12,p.pid,0,ctypes.addressof(regs))
    if info[0]==1 and regs.orig_rax in numbers:
     seen+=1
     if match(p.pid,regs):matched=True;boundary={'syscall':regs.orig_rax,'entry':True};break
    trace(24,p.pid)
   elif status>>16==4:
    trace(24,p.pid)
   else:
    owner.assertNotEqual(sig,signal.SIGTRAP,'unexpected trap in native-only caller')
    trace(24,p.pid,0,sig)
  owner.assertTrue(matched,'caller boundary not observed: '+label)
  # This PID is the still-traced Popen child, never an updater/host identity.
  os.kill(p.pid,signal.SIGKILL)
  while True:
   _,status=os.waitpid(p.pid,0)
   if os.WIFSIGNALED(status) or os.WIFEXITED(status):break
   trace(7,p.pid,0,signal.SIGKILL)
  p.returncode=-signal.SIGKILL;out,err=p.communicate(timeout=3)
  print('CALLER_CRASH_RECEIPT '+json.dumps({'label':label,'boundary':boundary,'exit':p.returncode,'events':seen,'elapsedSeconds':time.monotonic()-started,'stdout':out.decode(),'stderr':err.decode()}),flush=True)
  return out,err
 finally:
  if p.returncode is None:
   os.kill(p.pid,signal.SIGKILL)
   try:os.waitpid(p.pid,0)
   except ChildProcessError:pass
   p.returncode=-signal.SIGKILL
  p.stdout.close();p.stderr.close()
