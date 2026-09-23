"""Kill only the fixture migration process at exact publication syscalls."""
from pathlib import Path
import ctypes,json,os,signal,subprocess,time,unittest
from test_generation_migration import Migration,GEN
from test_generation_birth_crash import Registers,trace,traced_start

def trace_string(pid,address):
 result=b''
 for i in range(512//8):
  block=(trace(2,pid,address+i*8)&((1<<64)-1)).to_bytes(8,'little')
  result+=block
  if b'\0' in block:return result.split(b'\0',1)[0].decode()
 raise AssertionError('unterminated syscall path')

class MigrationCrash(Migration):
 def crash(self,name,point):
  before=self.inventory(self.live_root)
  args=[GEN,'migration-stage',str(self.stage),str(self.live_root),str(self.payload),self.manifest_sha,'operation-one','nonce-one','running']
  p=subprocess.Popen(args,preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE);self.processes.append(p)
  _,status=os.waitpid(p.pid,0);self.assertTrue(os.WIFSTOPPED(status));trace(0x4200,p.pid,0,1)
  entering=True;matched=False;fd=None;deadline=time.monotonic()+8
  try:
   while time.monotonic()<deadline:
    trace(24,p.pid);_,status=os.waitpid(p.pid,0)
    self.assertTrue(os.WIFSTOPPED(status),f'process exited before boundary: {status}')
    if os.WSTOPSIG(status)!=(signal.SIGTRAP|0x80):continue
    regs=Registers();trace(12,p.pid,0,ctypes.addressof(regs))
    if entering:
     if regs.orig_rax==257 and trace_string(p.pid,regs.rsi)==name+'.pending':
      if point=='before-open':matched=True;break
     if regs.orig_rax==1:
      try:target=os.readlink(f'/proc/{p.pid}/fd/{regs.rdi}')
      except FileNotFoundError:target=''
      if target==str(self.stage/(name+'.pending')) and point=='before-write':matched=True;break
     if regs.orig_rax==263 and trace_string(p.pid,regs.rsi)==name+'.pending' and point=='after-link':matched=True;break
     if regs.orig_rax==74 and point=='before-dir-fsync':
      try:target=os.readlink(f'/proc/{p.pid}/fd/{regs.rdi}')
      except FileNotFoundError:target=''
      if target==str(self.stage) and (self.stage/name).exists() and not (self.stage/(name+'.pending')).exists():matched=True;break
    entering=not entering
   self.assertTrue(matched,f'boundary not reached: {name}/{point}')
   os.kill(p.pid,signal.SIGKILL)
   while True:
    _,status=os.waitpid(p.pid,0)
    if os.WIFSIGNALED(status) or os.WIFEXITED(status):break
    trace(7,p.pid,0,signal.SIGKILL)
   p.returncode=-signal.SIGKILL
   self.assertEqual(self.inventory(self.live_root),before)
   saved=self.inventory(self.stage);r=self.invoke()
   if point in ('before-write','after-link'):
    self.assertNotEqual(r.returncode,0);self.assertEqual(self.inventory(self.stage),saved)
   else:
    self.assertEqual(r.returncode,0,r.stderr+r.stdout)
    for rel,data in saved.items():self.assertEqual(self.inventory(self.stage)[rel],data)
   self.assertEqual(self.inventory(self.live_root),before)
   print('MIGRATION_CRASH_RECEIPT '+json.dumps({'record':name,'point':point,'retryExit':r.returncode,'liveUnchanged':True,'oldEvidencePreserved':True}),flush=True)
  finally:
   if p.returncode is None:
    os.kill(p.pid,signal.SIGKILL)
    try:os.waitpid(p.pid,0)
    except ChildProcessError:pass
    p.returncode=-signal.SIGKILL
   p.stdout.close();p.stderr.close()

for name in ['intent.record','manifest.record',*[f'file-{i}' for i in range(7)],'staged.receipt']:
 for point in ['before-open','before-write','after-link','before-dir-fsync']:
  def check(self,name=name,point=point):self.crash(name,point)
  setattr(MigrationCrash,'test_'+name.replace('.','_').replace('-','_')+'_'+point.replace('-','_'),check)

if __name__=='__main__':
 names=[n for n in MigrationCrash.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(MigrationCrash(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)
