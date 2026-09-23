"""Crash, publication races and actual fsync proof for retained native bytes."""
import ctypes,json,os,signal,subprocess,time,unittest
from test_generation_runtime_store import RuntimeStore,GEN
from test_generation_bootguard_crash import GuardCrash
from test_generation_birth_crash import Registers,trace,traced_start
from test_generation_migration_crash import trace_string

class RuntimeEvidence(RuntimeStore):
 intercepted=GuardCrash.intercepted
 def guard_args(self):return [GEN,'runtime-retain',str(self.store),self.sha]
 def boundary(self,kind,when):
  def match(pid,r,entry):
   if entry!=(when=='before'):return False
   if kind=='runtime-open':return r.orig_rax==257 and trace_string(pid,r.rsi)=='runtime' and bool(r.rdx&os.O_CREAT)
   if kind=='identity-link':return r.orig_rax==265 and trace_string(pid,r.r10)=='identity.json'
   if kind=='identity-sync':return r.orig_rax==74 and os.readlink(f'/proc/{pid}/fd/{r.rdi}')==str(self.entry/'identity.json')
   return False
  return match
 def test_corruption_during_publication_preserves_exact_unknown_bytes(self):
  identity=self.entry/'identity.json'
  rc,_,_=self.intercepted(self.boundary('identity-link','before'),lambda:identity.write_bytes(b'{broken'))
  self.assertNotEqual(rc,0);self.assertEqual(identity.read_bytes(),b'{broken');self.refuse()
 def test_runtime_corruption_before_final_fsync_is_not_accepted(self):
  rc,_,_=self.intercepted(self.boundary('identity-sync','before'),lambda:(self.entry/'runtime').write_bytes(b'foreign'))
  self.assertNotEqual(rc,0);self.assertEqual((self.entry/'runtime').read_bytes(),b'foreign');self.refuse()
 def test_identity_corruption_before_final_fsync_is_not_accepted(self):
  rc,_,_=self.intercepted(self.boundary('identity-sync','before'),lambda:(self.entry/'identity.json').write_bytes(b'{broken'))
  self.assertNotEqual(rc,0);self.assertEqual((self.entry/'identity.json').read_bytes(),b'{broken');self.refuse()
 def test_concurrent_retention_cannot_change_inflight_entry(self):
  results=[]
  def concurrent():
   before=self.inventory();r=self.invoke();results.append(r.returncode);self.assertNotEqual(r.returncode,0);self.assertEqual(self.inventory(),before)
  rc,_,_=self.intercepted(self.boundary('identity-link','before'),concurrent)
  self.assertEqual(rc,0);self.assertEqual(len(results),1);self.assertEqual(self.invoke().returncode,0)
 def crash(self,kind,when):
  self.intercepted(self.boundary(kind,when));self.assertTrue(self.entry.exists());self.refuse()
 def trace_sync(self):
  p=subprocess.Popen(self.guard_args(),preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE);self.processes.append(p)
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
   required={str(self.store.parent),str(self.store),str(self.entry),str(self.entry/'runtime'),str(self.entry/'identity.json')}
   self.assertTrue(required<=synced,repr(sorted(required-synced)))
   print('RUNTIME_DURABILITY '+json.dumps({'required':sorted(required),'successfulFsync':sorted(synced)}),flush=True)
  finally:
   if p.returncode is None:
    os.kill(p.pid,signal.SIGKILL)
    try:os.waitpid(p.pid,0)
    except ChildProcessError:pass
    p.returncode=-signal.SIGKILL
   p.stdout.close();p.stderr.close()
 def test_initial_publication_durable(self):self.trace_sync()
 def test_exact_replay_durable(self):
  self.assertEqual(self.invoke().returncode,0);before=self.inventory();self.trace_sync();self.assertEqual(self.inventory(),before)

for kind in ['runtime-open','identity-link']:
 for when in ['before','after']:
  def test(self,kind=kind,when=when):self.crash(kind,when)
  setattr(RuntimeEvidence,'test_crash_'+kind.replace('-','_')+'_'+when,test)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(RuntimeEvidence(n) for n in RuntimeEvidence.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
