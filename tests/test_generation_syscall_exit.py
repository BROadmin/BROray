"""Force a real death between syscall-stop and register inspection.

The outer tracer controls only its own supervisor child. Its tracees remain
owned exclusively by the production supervisor. No production test hooks.
"""
import ctypes,json,os,signal,subprocess,time,unittest
from test_updater_generation import Generation,GEN
from test_generation_birth_crash import Registers,trace,traced_start

class SyscallExit(Generation):
 def guard_boundary(self,death,leaf=False):
  script=self.home/'daemon.sh';body='while :; do sleep .1; done\n'
  if leaf:
   (self.home/'leaf.sh').write_text('while :; do sleep .1; done\n')
   # The parent publishes the unreaped child handle. The boundary under test
   # can precede the helper's first userspace instruction.
   body='/bin/ash "$TEST_HOME/leaf.sh" &\necho $! >"$TEST_HOME/leaf.pid"\n'+body
  script.write_text(body)
  log=open(self.home/'syscall-exit.log','wb');self.logs.append(log)
  p=subprocess.Popen([GEN,'run',str(self.domain),self.gid,self.sha,'--','/bin/ash',str(script)],preexec_fn=traced_start,env={**os.environ,'TEST_HOME':str(self.home)},stdout=log,stderr=log)
  self.processes.append(p);_,status=os.waitpid(p.pid,0);self.assertTrue(os.WIFSTOPPED(status));trace(0x4200,p.pid,0,1)
  entering=True;target=None;detached=False;deadline=time.monotonic()+12;observed={};wanted=None
  try:
   while time.monotonic()<deadline:
    trace(24,p.pid);_,status=os.waitpid(p.pid,0)
    self.assertTrue(os.WIFSTOPPED(status),f'early supervisor exit {status}')
    if os.WSTOPSIG(status)!=(signal.SIGTRAP|0x80):continue
    regs=Registers();trace(12,p.pid,0,ctypes.addressof(regs))
    if entering and regs.orig_rax==101 and regs.rdi==0x420e:
     observed[regs.rsi]=observed.get(regs.rsi,0)+1
     state=self.state()
     child_text=(self.home/'leaf.pid').read_text().strip() if leaf and (self.home/'leaf.pid').exists() else ''
     wanted=(int(child_text) if child_text.isdecimal() else None) if leaf else state['updater']['pid']
     if state['state']=='RUNNING' and regs.rsi==wanted:
      proc=f'/proc/{wanted}'
      with open(proc+'/stat') as f:birth=f.read().rsplit(') ',1)[1].split()[19]
      target={'pid':wanted,'startTicks':birth} if leaf else state['updater']
      self.assertEqual(birth,target['startTicks'])
      with open(proc+'/status') as f:self.assertIn(f'TracerPid:\t{p.pid}',f.read())
      if death:
       fd=os.pidfd_open(target['pid'])
       try:signal.pidfd_send_signal(fd,signal.SIGKILL)
       finally:os.close(fd)
      else:
       # Keep the tracee alive/stopped, but force this register query to fail.
       # An unproven error must NOT be treated as a confirmed exit.
       regs.orig_rax=2**64-1;trace(13,p.pid,0,ctypes.addressof(regs))
       trace(24,p.pid);_,exit_status=os.waitpid(p.pid,0)
       self.assertTrue(os.WIFSTOPPED(exit_status));trace(12,p.pid,0,ctypes.addressof(regs))
       regs.rax=2**64-3;trace(13,p.pid,0,ctypes.addressof(regs)) # -ESRCH
      trace(17,p.pid);detached=True;break
    entering=not entering
   self.assertTrue(detached,'did not reach exact syscall inspection boundary; '+json.dumps({'wanted':wanted,'observed':observed,'leafFile':(self.home/'leaf.pid').read_text() if (self.home/'leaf.pid').exists() else None,'state':self.state()}))
   return p,target
  finally:
   if not detached and p.returncode is None:
    os.kill(p.pid,signal.SIGKILL)
    try:os.waitpid(p.pid,0)
    except ChildProcessError:pass
    p.returncode=-signal.SIGKILL

 def test_confirmed_exit_does_not_kill_generation(self):
  foreign=subprocess.Popen(['/bin/ash','-c','sleep 60']);self.processes.append(foreign)
  p,target=self.guard_boundary(True)
  self.wait(lambda:p.poll() is not None or self.state()['state']=='STOPPED')
  self.assertIsNone(p.poll(),'confirmed task exit must not become SYSCALL_CONTAINMENT_UNCONFIRMED')
  end=self.stopped();self.assertEqual(end['children'],[]);self.assertEqual(end['exitedUnreaped'],[])
  self.assertFalse(self.live(target['pid']));self.assertIsNone(foreign.poll())
  self.assertEqual(self.call('STOP').returncode,0)
  before=self.latest_record().read_bytes();self.assertEqual(self.call('STOP').returncode,0);self.assertEqual(self.latest_record().read_bytes(),before)
  print('SYSCALL_EXIT_PROOF '+json.dumps({'target':target,'stopped':True,'foreignAlive':True}),flush=True)

 def test_unconfirmed_register_failure_still_fails_closed(self):
  p,target=self.guard_boundary(False)
  self.assertNotEqual(p.wait(timeout=4),0)
  self.wait(lambda:not self.live(target['pid']))
  self.assertNotEqual(self.state()['state'],'STOPPED')
  self.assertIn('SYSCALL_CONTAINMENT_UNCONFIRMED',(self.home/'syscall-exit.log').read_text())

 def test_child_exit_preserves_running_updater(self):
  p,target=self.guard_boundary(True,leaf=True)
  self.wait(lambda:p.poll() is not None or not self.live(target['pid']))
  self.assertIsNone(p.poll(),'exit of a helper must preserve updater')
  current=self.live_state();self.assertEqual(current['state'],'RUNNING')
  self.assertTrue(self.live(current['updater']['pid']))
  self.assertFalse(any(c['pid']==target['pid'] for c in current['children']))
  self.assertEqual(self.call('STOP').returncode,0);self.stopped()

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  SyscallExit('test_confirmed_exit_does_not_kill_generation'),
  SyscallExit('test_unconfirmed_register_failure_still_fails_closed'),
  SyscallExit('test_child_exit_preserves_running_updater')]))
 raise SystemExit(0 if result.wasSuccessful() else 1)
