"""Deterministic control-snapshot crash/race/fsync proof in an offline Linux VM."""
import ctypes,json,os,signal,subprocess,time,unittest
from test_generation_legacy_control import LegacyControlNative
from test_generation_bootguard_crash import GuardCrash
from test_generation_birth_crash import Registers,trace,traced_start
from test_generation_migration_crash import trace_string

class LegacyControlEvidence(LegacyControlNative):
 intercepted=GuardCrash.intercepted
 def guard_args(self):return self.control_args()
 def boundary(self,name,kind='link',when='before'):
  def match(pid,r,entry):
   if entry!=(when=='before'):return False
   if kind=='link':return r.orig_rax==265 and trace_string(pid,r.r10)==name
   if kind=='sync':return r.orig_rax==74 and os.readlink(f'/proc/{pid}/fd/{r.rdi}')==str(self.control/name)
   return False
  return match
 def corrupt_at_receipt(self,which):
  target={'snapshot':self.control/'snapshot.json','backup':self.control/'file-0','binding':self.binding,'service':self.service_file,'guard':self.op/'platform-bootguard.json','live':self.updater/'daemon.pid'}[which]
  rc,_,_=self.intercepted(self.boundary('staged.receipt'),lambda:target.write_bytes(b'{broken'))
  self.assertNotEqual(rc,0);self.assertEqual(target.read_bytes(),b'{broken');self.assertIsNone(self.service.poll())
  self.refused()
 def missing_at_receipt(self,which):
  target={'snapshot':self.control/'snapshot.json','backup':self.control/'file-0','binding':self.binding}[which]
  rc,_,_=self.intercepted(self.boundary('staged.receipt'),target.unlink)
  self.assertNotEqual(rc,0);self.assertFalse(target.exists());self.refused()
 def test_foreign_snapshot_at_publication_never_overwritten(self):
  target=self.control/'snapshot.json'
  rc,_,_=self.intercepted(self.boundary('snapshot.json'),lambda:target.write_bytes(b'{broken'))
  self.assertNotEqual(rc,0);self.assertEqual(target.read_bytes(),b'{broken');self.refused()
 def test_live_owner_exit_during_publication_cannot_report_success(self):
  def terminate():self.service.terminate();self.service.wait(timeout=3)
  rc,_,_=self.intercepted(self.boundary('staged.receipt'),terminate)
  self.assertNotEqual(rc,0);before=self.stable_inventory();self.assertNotEqual(self.observe().returncode,0);self.assertEqual(self.stable_inventory(),before)
 def test_competing_snapshot_request_cannot_change_inflight_evidence(self):
  def compete():self.refused()
  rc,_,_=self.intercepted(self.boundary('snapshot.json'),compete)
  self.assertEqual(rc,0);self.assertEqual(self.observe().returncode,0)
 def crash(self,name,when):
  before_live=self.inventory(self.live_root)
  self.intercepted(self.boundary(name,when=when))
  self.assertEqual(self.inventory(self.live_root),before_live);self.assertIsNone(self.service.poll())
  self.refused()
 def trace_sync(self):
  p=subprocess.Popen(self.control_args(),preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE);self.processes.append(p)
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
   required={str(self.op),str(self.binding),str(self.control),*[str(self.control/n) for n in ['snapshot.json','file-0','file-1','staged.receipt']]}
   self.assertTrue(required<=synced,repr(sorted(required-synced)))
   print('LEGACY_CONTROL_DURABILITY '+json.dumps({'required':sorted(required),'successfulFsync':sorted(synced)}),flush=True)
  finally:
   if p.returncode is None:
    os.kill(p.pid,signal.SIGKILL)
    try:os.waitpid(p.pid,0)
    except ChildProcessError:pass
    p.returncode=-signal.SIGKILL
   p.stdout.close();p.stderr.close()
 def test_first_snapshot_durable(self):self.trace_sync()
 def test_replay_durable_unchanged(self):
  self.assertEqual(self.observe().returncode,0);before=self.stable_inventory();self.trace_sync();self.assertEqual(self.stable_inventory(),before)

for which in ['snapshot','backup','binding','service','guard','live']:
 def test(self,which=which):self.corrupt_at_receipt(which)
 setattr(LegacyControlEvidence,'test_concurrent_corruption_'+which,test)
for which in ['snapshot','backup','binding']:
 def test(self,which=which):self.missing_at_receipt(which)
 setattr(LegacyControlEvidence,'test_concurrent_removal_'+which,test)
for name in ['platform-legacy-control.json','snapshot.json','file-0','file-1','staged.receipt']:
 for when in ['before','after']:
  def test(self,name=name,when=when):self.crash(name,when)
  setattr(LegacyControlEvidence,'test_crash_'+name.replace('.','_').replace('-','_')+'_'+when,test)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(LegacyControlEvidence(n) for n in LegacyControlEvidence.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
