"""Retained code publication: deterministic own-process crashes and races."""
import ctypes,json,os,signal,subprocess,time,unittest
from test_generation_recovery_code import RecoveryCode,FILES
from test_generation_bootguard_crash import GuardCrash
from test_generation_birth_crash import Registers,trace,traced_start
from test_generation_migration_crash import trace_string

class RecoveryCodeEvidence(RecoveryCode):
 intercepted=GuardCrash.intercepted
 def guard_args(self):return self.code_args()
 def boundary(self,name,kind='link',when='before'):
  def match(pid,r,entry):
   if entry!=(when=='before'):return False
   if kind=='link':return r.orig_rax==265 and trace_string(pid,r.r10)==name
   if kind=='open':return r.orig_rax==257 and trace_string(pid,r.rsi)==name and bool(r.rdx&os.O_CREAT)
   return False
  return match
 def target(self,which):
  return {'binding':self.code_binding,'manifest':self.retained/'manifest.record','guard':self.code/FILES[0],'library':self.code/FILES[2],'source':self.source/FILES[2]}[which]
 def corrupt(self,which):
  p=self.target(which);rc,_,_=self.intercepted(self.boundary('staged.receipt'),lambda:p.write_bytes(b'{broken'))
  self.assertNotEqual(rc,0);self.assertEqual(p.read_bytes(),b'{broken');self.refuse()
 def remove(self,which):
  p=self.target(which);rc,_,_=self.intercepted(self.boundary('staged.receipt'),p.unlink)
  self.assertNotEqual(rc,0);self.assertFalse(p.exists());self.refuse();self.refuse(True)
 def test_competing_stage_cannot_change_inflight_evidence(self):
  rc,_,_=self.intercepted(self.boundary('manifest.record'),lambda:self.refuse())
  self.assertEqual(rc,0);self.assertEqual(self.code_call().returncode,0)
 def test_foreign_library_at_copy_not_overwritten(self):
  p=self.code/FILES[2];rc,_,_=self.intercepted(self.boundary(p.name,'open'),lambda:p.write_bytes(b'KEEP'))
  self.assertNotEqual(rc,0);self.assertEqual(p.read_bytes(),b'KEEP');self.refuse();self.refuse(True)
 def crash(self,name,kind,when):
  before=self.inventory(self.live_root);self.intercepted(self.boundary(name,kind,when))
  self.assertEqual(self.inventory(self.live_root),before);self.refuse();self.refuse(True)
 def trace_sync(self,verify=False):
  p=subprocess.Popen(self.code_args(verify),preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE);self.processes.append(p)
  _,status=os.waitpid(p.pid,0);self.assertTrue(os.WIFSTOPPED(status));trace(0x4200,p.pid,0,1)
  entering=True;pending=None;synced=set();deadline=time.monotonic()+12
  try:
   while time.monotonic()<deadline:
    trace(24,p.pid);_,status=os.waitpid(p.pid,0)
    if os.WIFEXITED(status):p.returncode=os.WEXITSTATUS(status);break
    self.assertTrue(os.WIFSTOPPED(status))
    if os.WSTOPSIG(status)!=(signal.SIGTRAP|0x80):continue
    r=Registers();trace(12,p.pid,0,ctypes.addressof(r))
    if r.orig_rax==74:
     if entering:pending=os.readlink(f'/proc/{p.pid}/fd/{r.rdi}')
     elif r.rax==0 and pending:synced.add(pending);pending=None
    entering=not entering
   self.assertEqual(p.returncode,0)
   required={str(self.op),str(self.code_binding),str(self.retained),str(self.retained/'manifest.record'),str(self.retained/'staged.receipt'),str(self.code),str(self.code/'bin'),str(self.code/'lib'),*[str(self.code/n) for n in FILES]}
   self.assertTrue(required<=synced,repr(sorted(required-synced)))
   print('RECOVERY_CODE_DURABILITY '+json.dumps({'required':sorted(required),'successfulFsync':sorted(synced)}),flush=True)
  finally:
   if p.returncode is None:
    os.kill(p.pid,signal.SIGKILL)
    try:os.waitpid(p.pid,0)
    except ChildProcessError:pass
    p.returncode=-signal.SIGKILL
   p.stdout.close();p.stderr.close()
 def test_initial_files_and_directories_durable(self):self.trace_sync()
 def test_verified_replay_files_and_directories_durable(self):
  self.assertEqual(self.code_call().returncode,0);before=self.snapshot();self.trace_sync(True);self.assertEqual(self.snapshot(),before)

for which in ['binding','manifest','guard','library','source']:
 def test(self,which=which):self.corrupt(which)
 setattr(RecoveryCodeEvidence,'test_concurrent_corruption_'+which,test)
for which in ['binding','manifest','guard','library']:
 def test(self,which=which):self.remove(which)
 setattr(RecoveryCodeEvidence,'test_concurrent_removal_'+which,test)
for kind,names in [('link',['platform-recovery-code.json','manifest.record','staged.receipt']),('open',['broray-ops-guard','operation-coordinator.sh','operation-report-public.jq'])]:
 for name in names:
  for when in ['before','after']:
   def test(self,name=name,kind=kind,when=when):self.crash(name,kind,when)
   setattr(RecoveryCodeEvidence,'test_crash_'+name.replace('.','_').replace('-','_')+'_'+when,test)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(RecoveryCodeEvidence(n) for n in RecoveryCodeEvidence.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
