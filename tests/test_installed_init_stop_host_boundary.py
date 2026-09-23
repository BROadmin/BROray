"""Deterministic host-retirement scheduling at the actual public stop entry.

VM-only ptrace observes the finite init/coordinator process. The persistent
generation is not traced. A pidfd plus full recorded identity pauses only its
confirmed independent host after durable STOP_INTENT. Release occurs after
the real host-directory flock returns EWOULDBLOCK, not after a timing guess.
"""
import ctypes,errno,fcntl,hashlib,json,os,signal,subprocess,threading,time,unittest
from pathlib import Path
from test_installed_init_stop import InstalledInitStop
from test_generation_birth_crash import Registers,trace
from test_public_stop_trace import PublicStopTrace

class InstalledHostBoundary(InstalledInitStop):
 def init(self,verb):
  if verb!='stop':return super().init(verb)
  records=list((self.updater/'hosts').glob('*/host.record'));self.assertEqual(len(records),1)
  record=records[0];host=record.parent;rows=record.read_text().splitlines();owner=json.loads(rows[-1]);domain=Path(rows[2])
  self.assertEqual(owner['executable'],str(self.native));self.assertEqual(rows[5],str(self.root/'router'))
  proc=Path('/proc')/str(owner['pid']);hfd=os.pidfd_open(owner['pid']);paused=threading.Event();finished=threading.Event();errors=[]
  def identity():
   self.assertEqual(proc.joinpath('stat').read_text().rsplit(') ',1)[1].split()[19],owner['startTicks'])
   self.assertEqual(Path('/proc/sys/kernel/random/boot_id').read_text().strip(),owner['bootId'])
   self.assertEqual(os.readlink(proc/'exe'),owner['executable'])
   self.assertEqual(hashlib.sha256(proc.joinpath('cmdline').read_bytes()).hexdigest(),owner['commandDigest'])
  identity()
  def pause_after_intent():
   try:
    deadline=time.monotonic()+120
    while not finished.is_set():
     self.assertLess(time.monotonic(),deadline,'durable service STOP_INTENT not reached')
     for path in self.op.parent.glob('op-*/state.json'):
      value=json.loads(path.read_bytes())
      if value.get('serviceStop',{}).get('contract')=='broray-service-stop/1' and value.get('platformPreflight',{}).get('phase')=='STOP_INTENT':
       identity();self.assertFalse((domain/'retirement.receipt').exists())
       signal.pidfd_send_signal(hfd,signal.SIGSTOP);paused.set();return
     time.sleep(.02)
   except BaseException as exc:errors.append(repr(exc))
  watcher=threading.Thread(target=pause_after_intent,daemon=True);watcher.start()
  command=['/bin/ash',str(self.root/'router/opt/etc/init.d/S22broray-updater'),'stop']
  env={**os.environ,'BRORAY_UPDATER_ROOT_PREFIX':str(self.root/'router')}
  traced=PublicStopTrace(self,command,env);released=False;boundary=False
  try:
   deadline=time.monotonic()+120
   guard=traced.wait_exec(lambda argv:len(argv)>1 and argv[1]==b'service-stop-guard',deadline)
   self.assertFalse(errors,errors)
   self.assertTrue(paused.is_set());identity();self.assertFalse((host/'retirement.receipt').exists())
   self.assertTrue((domain/'retirement.receipt').exists(),'updater retirement precedes host settlement')
   traced.syscall_mode(guard);waiting_exit=False
   while time.monotonic()<deadline:
    traced.next_syscall(guard,deadline)
    info=(ctypes.c_ubyte*128)();trace(0x420e,guard,ctypes.sizeof(info),ctypes.addressof(info))
    regs=Registers();trace(12,guard,0,ctypes.addressof(regs))
    if info[0]==1 and regs.orig_rax==73 and regs.rsi==(fcntl.LOCK_EX|fcntl.LOCK_NB):
     waiting_exit=os.readlink('/proc/%s/fd/%s'%(guard,regs.rdi))==str(host)
    elif waiting_exit and info[0]==2:
     result=ctypes.c_longlong(regs.rax).value;self.assertEqual(result,-errno.EWOULDBLOCK)
     boundary=True;identity()
     print('HOST_HANDOFF_BARRIER '+json.dumps({'flockResult':result,'hostIdentity':owner,'generationRetired':True,'hostReceiptPresent':False,'actualGuardPid':guard}),flush=True)
     signal.pidfd_send_signal(hfd,signal.SIGCONT);released=True
     traced.tasks[guard]['mode']=7;traced._resume(guard);break
    traced._resume(guard)
   self.assertTrue(boundary,'exact host-lock contention boundary was not reached')
   completed=traced.run_to_completion(time.monotonic()+30)
   # Read-only diagnosis after the same invocation; never rerun the stop.
   proof_command=[str(self.native),'service-retired',*rows[1:8],hashlib.sha256(record.read_bytes()).hexdigest()]
   end=time.monotonic()+5
   while True:
    proof=subprocess.run(proof_command,capture_output=True,text=True,timeout=4)
    if proof.returncode==0:break
    self.assertLess(time.monotonic(),end,proof.stdout+proof.stderr);time.sleep(.02)
   print('HOST_HANDOFF_RESULT '+json.dumps({'publicStopExit':completed.returncode,'hostRetiredAfterRelease':True,'terminalProof':json.loads(proof.stdout)}),flush=True)
   return completed
  finally:
   finished.set();watcher.join(timeout=2)
   if paused.is_set() and not released:
    try:signal.pidfd_send_signal(hfd,signal.SIGCONT)
    except ProcessLookupError:pass
   traced.close();os.close(hfd)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([InstalledHostBoundary('test_public_stop_preserves_origin_platform_and_foreign_process')]))
 raise SystemExit(not r.wasSuccessful())
