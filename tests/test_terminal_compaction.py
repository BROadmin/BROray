"""Explicit terminal-history compaction; actual Linux processes, offline."""
import hashlib,json,os,subprocess
from test_generation_installation import Installation
from test_updater_generation import GEN

class TerminalCompaction(Installation):
 def compact(self,revision=None):
  return subprocess.run([GEN,'compact-retired',str(self.domain),self.gid,self.sha,
                         'operation-one','nonce-one']+([] if revision is None else [str(revision)]),capture_output=True,text=True,timeout=15)
 def test_compact_retired_then_restart_and_replay(self):
  self.retire();last=self.latest_record().read_bytes();receipt=(self.domain/'retirement.receipt').read_bytes()
  before=self.inventory();r=self.compact();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertIn('TERMINAL_HISTORY_COMPACTED',r.stdout)
  self.assertTrue(json.loads(r.stdout)['storageReclaimed'])
  scope=hashlib.sha256(str(self.domain).encode()).hexdigest()
  stage=self.home.parent/('.broray-retention-'+scope)
  self.assertFalse((stage/'replacement').exists())
  self.assertEqual((stage/'before.tree').read_bytes(),(stage/'reclaimed.receipt').read_bytes())
  self.assertTrue((self.domain/'retention.record').exists())
  self.assertEqual(self.latest_record().read_bytes(),last)
  self.assertEqual((self.domain/'retirement.receipt').read_bytes(),receipt)
  self.assertLess(len(list(self.domain.glob('*.json'))),sum(n.endswith('.json') for n in before))
  after=self.inventory();self.assertEqual(self.compact().returncode,0);self.assertEqual(self.inventory(),after)
  self.assertEqual(self.call('RETIRE').returncode,0)
  p,other=self.launch_next();self.wait(lambda:(other/'state.json').exists());self.assertIsNone(p.poll())
  self.assertEqual(self.inventory(),after)
 def test_compact_live_refuses_without_mutation(self):
  p=self.start('while :; do :; done\n');self.running();before=self.inventory()
  self.assertNotEqual(self.compact().returncode,0);self.assertEqual(self.inventory(),before);self.assertIsNone(p.poll())
 def test_compact_corrupt_retired_preserves_exact_bytes(self):
  self.retire();(self.domain/'state.json').write_bytes(b'{broken');before=self.inventory()
  self.assertNotEqual(self.compact().returncode,0);self.assertEqual(self.inventory(),before)
 def test_compact_unknown_file_preserved(self):
  self.retire();(self.domain/'unknown').write_bytes(b'foreign');before=self.inventory()
  self.assertNotEqual(self.compact().returncode,0);self.assertEqual(self.inventory(),before)
 def test_compact_corrupt_summary_blocks_successor(self):
  self.retire();self.assertEqual(self.compact().returncode,0)
  (self.domain/'retention.record').write_bytes(b'{broken');self.assert_next_refused()
 def test_compact_hardlink_preserved(self):
  self.retire();os.link(self.latest_record(),self.home/'foreign-link');before=self.inventory()
  self.assertNotEqual(self.compact().returncode,0);self.assertEqual(self.inventory(),before)
 def test_compact_pinned_ready_revision_is_preserved(self):
  p=self.start('while :; do :; done\n');ready=self.running();record=self.latest_record();body=record.read_bytes()
  self.assertEqual(self.call('STOP').returncode,0);self.stopped();self.assertEqual(self.call('RETIRE').returncode,0);self.assertEqual(p.wait(timeout=2),0)
  r=self.compact(ready['revision']);self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(record.read_bytes(),body)
 def test_compact_missing_anchor_refuses(self):
  self.retire();self.assertEqual(self.compact().returncode,0)
  (self.domain/'retention.anchor').unlink();self.assert_next_refused()
 def test_compact_changed_retained_terminal_refuses(self):
  self.retire();self.assertEqual(self.compact().returncode,0)
  self.latest_record().write_bytes(b'{broken');self.assert_next_refused()
 def test_compact_wrong_owner_refuses(self):
  self.retire();before=self.inventory()
  r=subprocess.run([GEN,'compact-retired',str(self.domain),self.gid,self.sha,'foreign-operation','nonce-one'],capture_output=True,timeout=10)
  self.assertNotEqual(r.returncode,0);self.assertEqual(self.inventory(),before)
 def test_compact_source_namespace_swap_preserves_unknown_bytes(self):
  # Stop the real static native executable at renameat2 entry. The public
  # pathname is replaced AFTER its final pre-capture validation. No production
  # hook, sleeps or probabilistic race is involved.
  import ctypes,signal,time
  self.retire();libc=ctypes.CDLL(None,use_errno=True)
  class Registers(ctypes.Structure):
   _fields_=[(name,ctypes.c_ulonglong) for name in 'r15 r14 r13 r12 rbp rbx r11 r10 r9 r8 rax rcx rdx rsi rdi orig_rax rip cs eflags rsp ss fs_base gs_base ds es fs gs'.split()]
  def trace(request,pid,address=0,data=0):
   value=libc.ptrace(request,pid,ctypes.c_void_p(address),data if not isinstance(data,int) else ctypes.c_void_p(data))
   if value==-1:raise OSError(ctypes.get_errno(),'ptrace')
  def child():
   if libc.ptrace(0,0,None,None):os._exit(111)
  p=subprocess.Popen([GEN,'compact-retired',str(self.domain),self.gid,self.sha,'operation-one','nonce-one'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,preexec_fn=child)
  self.processes.append(p);pid,status=os.waitpid(p.pid,0);self.assertTrue(os.WIFSTOPPED(status));trace(0x4200,p.pid,0,1)
  swapped=False;deadline=time.monotonic()+30
  while True:
   self.assertLess(time.monotonic(),deadline)
   trace(24,p.pid);pid,status=os.waitpid(p.pid,0)
   if os.WIFEXITED(status):p.returncode=os.WEXITSTATUS(status);break
   if os.WIFSIGNALED(status):p.returncode=-os.WTERMSIG(status);break
   if os.WSTOPSIG(status)!=(signal.SIGTRAP|0x80):continue
   registers=Registers();trace(12,p.pid,0,ctypes.byref(registers))
   if not swapped and registers.orig_rax==316 and registers.r8==2 and registers.rax==ctypes.c_ulonglong(-38).value:
    self.domain.rename(self.home/'original-held');self.domain.mkdir(mode=0o700)
    marker=self.domain/'state.json';marker.write_bytes(b'{broken');marker.chmod(0o600);swapped=True
  stdout,stderr=p.communicate();self.assertTrue(swapped);self.assertNotEqual(p.returncode,0,(stdout,stderr))
  scope=hashlib.sha256(str(self.domain).encode()).hexdigest();stage=self.home.parent/('.broray-retention-'+scope)
  self.assertEqual((stage/'replacement/state.json').read_bytes(),b'{broken')
  self.assertTrue((self.home/'original-held/retirement.receipt').exists())
  self.assertFalse((stage/'reclaimed.receipt').exists())
  # A compact projection exists, but cleanup never completed. A retry must
  # retain this refusal, not report storageReclaimed merely from projection.
  self.assertNotEqual(self.compact().returncode,0,'CAPTURE_FAILURE_HIDDEN_ON_RETRY')
  self.assertEqual((stage/'replacement/state.json').read_bytes(),b'{broken')
