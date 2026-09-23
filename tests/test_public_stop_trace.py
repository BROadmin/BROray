"""Trace exec/fork events only inside the test-created public command tree.

The already-running updater, its descendants and independent host are NOT
children of this Popen and are never traced or signalled by this driver.
No seccomp filter, guessed PID, production fault hook or relaxed deadline.
"""
import ctypes,json,os,signal,subprocess,time
from pathlib import Path
from test_generation_birth_crash import trace,traced_start

class PublicStopTrace:
 OPTIONS=1|2|4|8|16  # TRACESYSGOOD, TRACEFORK/VFORK/CLONE/EXEC
 def __init__(self,owner,command,env=None):
  self.owner=owner;self.command=command;self.tasks={};self.history=[];self.killing=False
  self.p=subprocess.Popen(command,env=env,preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
  self._add(self.p.pid,None,False)
  _,status=os.waitpid(self.p.pid,0);owner.assertTrue(os.WIFSTOPPED(status))
  trace(0x4200,self.p.pid,0,self.OPTIONS);self._resume(self.p.pid)
 def _add(self,pid,parent,initial=True):
  self.owner.assertNotIn(pid,self.tasks)
  fd=os.pidfd_open(pid)
  ticks=Path('/proc',str(pid),'stat').read_text().rsplit(') ',1)[1].split()[19]
  self.tasks[pid]={'fd':fd,'parent':parent,'initial':initial,'mode':7}
  self.history.append({'pid':pid,'parent':parent,'startTicks':ticks})
  if self.killing:signal.pidfd_send_signal(fd,signal.SIGKILL)
 def _resume(self,pid,sig=0):
  task=self.tasks[pid];trace(task['mode'],pid,0,signal.SIGKILL if self.killing else sig)
 def _step(self,deadline):
  while self.tasks:
   self.owner.assertLess(time.monotonic(),deadline,'test-owned command event deadline')
   for pid in list(self.tasks):
    got,status=os.waitpid(pid,os.WNOHANG|0x40000000) # __WALL for ptrace children
    if not got:continue
    task=self.tasks[pid]
    if os.WIFEXITED(status) or os.WIFSIGNALED(status):
     rc=os.waitstatus_to_exitcode(status)
     if pid==self.p.pid:self.p.returncode=rc
     os.close(task['fd']);del self.tasks[pid];break
    self.owner.assertTrue(os.WIFSTOPPED(status),(pid,status))
    event=status>>16;sig=os.WSTOPSIG(status)
    if event in (1,2,3):
     child=ctypes.c_ulong();trace(0x4201,pid,0,ctypes.addressof(child))
     self._add(child.value,pid);self._resume(pid);break
    if task['initial']:
     self.owner.assertEqual(sig,signal.SIGSTOP,(pid,status))
     task['initial']=False;trace(0x4200,pid,0,self.OPTIONS);self._resume(pid);break
    if event==4:
     argv=Path('/proc',str(pid),'cmdline').read_bytes().split(b'\0')
     return ('exec',pid,argv)
    if sig==(signal.SIGTRAP|0x80):return ('syscall',pid,None)
    self.owner.assertNotEqual(sig,signal.SIGTRAP,('unexpected trace event',pid,status))
    self._resume(pid,sig);break
   else:time.sleep(.001)
  return None
 def wait_exec(self,predicate,deadline):
  while self.tasks:
   item=self._step(deadline)
   if item is None:break
   kind,pid,argv=item
   if kind=='exec' and predicate(argv):
    self.owner.assertNotEqual(pid,self.p.pid,'guard must be the actual helper descendant')
    print('PUBLIC_COMMAND_EXEC '+json.dumps({'rootPid':self.p.pid,'guardPid':pid,'argv':[x.decode() for x in argv if x],'observedForks':self.history}),flush=True)
    return pid
   self._resume(pid)
  out,err=self.p.communicate(timeout=3)
  self.owner.fail('public command exited before selected exec: '+repr((self.p.returncode,out,err)))
 def syscall_mode(self,pid):
  self.tasks[pid]['mode']=24;self._resume(pid)
 def next_syscall(self,pid,deadline):
  while self.tasks:
   item=self._step(deadline)
   if item is None:break
   kind,who,_=item
   if who==pid and kind=='syscall':return
   self._resume(who)
  self.owner.fail('selected guard exited before syscall boundary')
 def run_to_completion(self,deadline):
  while self.tasks:
   item=self._step(deadline)
   if item:self._resume(item[1])
  out,err=self.p.communicate(timeout=3)
  return subprocess.CompletedProcess(self.command,self.p.returncode,out,err)
 def kill_command(self):
  self.killing=True
  for task in list(self.tasks.values()):
   try:signal.pidfd_send_signal(task['fd'],signal.SIGKILL)
   except ProcessLookupError:pass
  result=self.run_to_completion(time.monotonic()+10)
  self.owner.assertEqual(result.returncode,-signal.SIGKILL)
  return result
 def close(self):
  if self.tasks:self.kill_command()
  self.p.stdout.close();self.p.stderr.close()
