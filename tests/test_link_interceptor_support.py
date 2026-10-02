"""Offline Linux x86-64 fixture: intercept every linkat entry AND exit.

The kernel filters unrelated syscalls only. This changes no tested operation,
boundary, assertion or 12-second watchdog. It is not target production code.
"""
import ctypes,json,os,signal,subprocess,time
from test_generation_birth_crash import Registers,trace,traced_start
from test_generation_migration_crash import trace_string

class Filter(ctypes.Structure):
 _fields_=[('code',ctypes.c_ushort),('jt',ctypes.c_ubyte),('jf',ctypes.c_ubyte),('k',ctypes.c_uint)]
class Program(ctypes.Structure):
 _fields_=[('length',ctypes.c_ushort),('filter',ctypes.POINTER(Filter))]

def link_traced_start():
 traced_start()
 # Check x86-64 architecture, then TRACE linkat; ALLOW other syscalls.
 # A wrong architecture kills this isolated fixture, never weakens coverage.
 f=(Filter*7)(Filter(0x20,0,0,4),Filter(0x15,1,0,0xc000003e),Filter(0x06,0,0,0),
              Filter(0x20,0,0,0),Filter(0x15,0,1,265),Filter(0x06,0,0,0x7ff00000),Filter(0x06,0,0,0x7fff0000))
 p=Program(len(f),f);libc=ctypes.CDLL(None,use_errno=True)
 if libc.prctl(38,1,0,0,0) or libc.prctl(22,2,ctypes.byref(p),0,0):
  raise OSError(ctypes.get_errno(),'offline seccomp link tracing unavailable')

def intercepted_link(owner,match,change=None):
 owner.assertIsNone(change,'filtered driver is exclusively for kill-at-link crash cases')
 p=subprocess.Popen(owner.guard_args(),preexec_fn=link_traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 owner.processes.append(p);_,status=os.waitpid(p.pid,0);owner.assertTrue(os.WIFSTOPPED(status))
 trace(0x4200,p.pid,0,1|0x80) # TRACESYSGOOD | TRACESECCOMP
 began=time.monotonic();deadline=began+12;matched=False;at=None;events=[];signals=[];timings=[]
 def next_stop(request):
  trace(request,p.pid)
  while True:
   _,status=os.waitpid(p.pid,0)
   if not os.WIFSTOPPED(status):
    p.returncode=os.waitstatus_to_exitcode(status)
    out,err=p.communicate(timeout=3)
    print('LINK_EARLY_EXIT '+json.dumps({'exit':p.returncode,'stdout':out.decode(),'stderr':err.decode(),'events':events}),flush=True)
   owner.assertTrue(os.WIFSTOPPED(status),f'exited before selected boundary {status}')
   stop=os.WSTOPSIG(status)
   if status>>16 or stop==signal.SIGTRAP|0x80:return status
   owner.assertNotEqual(stop,signal.SIGTRAP,'unexpected plain trap')
   signals.append(stop)
   # Forward ordinary delivery stops (including SIGCHLD) and keep the same
   # trace mode. Neither a child exit nor a signal is a link boundary.
   trace(request,p.pid,0,stop)
 try:
  while time.monotonic()<deadline:
   status=next_stop(7)
   owner.assertEqual(status>>16,7,'expected kernel SECCOMP entry event')
   regs=Registers();trace(12,p.pid,0,ctypes.addressof(regs));owner.assertEqual(regs.orig_rax,265)
   events.append('entry')
   timings.append({'event':'entry','target':trace_string(p.pid,regs.r10),'ms':round((time.monotonic()-began)*1000)})
   if match(p.pid,regs,True):matched=True;at={'syscall':regs.orig_rax,'entry':True};break
   status=next_stop(24)
   owner.assertTrue(os.WIFSTOPPED(status));owner.assertEqual(os.WSTOPSIG(status),signal.SIGTRAP|0x80)
   regs=Registers();trace(12,p.pid,0,ctypes.addressof(regs));owner.assertEqual(regs.orig_rax,265)
   events.append('exit')
   timings.append({'event':'exit','target':trace_string(p.pid,regs.r10),'ms':round((time.monotonic()-began)*1000)})
   if match(p.pid,regs,False):matched=True;at={'syscall':regs.orig_rax,'entry':False};break
  diagnostic_stderr=''
  if not matched:
   os.set_blocking(p.stderr.fileno(),False)
   try:diagnostic_stderr=os.read(p.stderr.fileno(),65536).decode()
   except BlockingIOError:pass
  print('LINK_TIMING_DIAGNOSTIC '+json.dumps({'matched':matched,'elapsedMs':round((time.monotonic()-began)*1000),'deadlineMs':12000,'events':timings,'signalsForwarded':signals,'stderr':diagnostic_stderr}),flush=True)
  owner.assertTrue(matched,'requested syscall boundary not reached')
  if change is None:
   os.kill(p.pid,signal.SIGKILL)
   while True:
    _,status=os.waitpid(p.pid,0)
    if os.WIFSIGNALED(status) or os.WIFEXITED(status):break
    trace(7,p.pid,0,signal.SIGKILL)
   p.returncode=-signal.SIGKILL
  else:
   change();trace(17,p.pid,0,0);p.wait(timeout=5)
  out,err=p.communicate(timeout=3)
  print('LINK_INTERCEPTOR_RECEIPT '+json.dumps({'boundary':at,'events':events,'signalsForwarded':signals,'exit':p.returncode,'stdout':out.decode(),'stderr':err.decode()}),flush=True)
  return p.returncode,out,err
 finally:
  if p.returncode is None:
   os.kill(p.pid,signal.SIGKILL)
   try:os.waitpid(p.pid,0)
   except ChildProcessError:pass
   p.returncode=-signal.SIGKILL
  p.stdout.close();p.stderr.close()
