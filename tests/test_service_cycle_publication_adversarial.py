"""Interrupted intent publication: exact recovery, corruption stays fenced."""
import hashlib,json,os,stat,unittest
from pathlib import Path
from test_installed_service_cycles import ServiceCycles
from test_link_interceptor_support import intercepted_link
from test_generation_migration_crash import trace_string

class PublicationAdversarial(ServiceCycles):
 def guard_args(self):
  if not getattr(self,'intercept_start',False):return super().guard_args()
  b=json.loads((self.op/'platform-bootguard.json').read_bytes())
  return [str(self.native),'service-cycle-start',str(self.root/'router'),self.op.name,b['migrationIntentSha256'],b['stopNonce']]
 def inventory(self,root):
  out={}
  for p in sorted(root.rglob('*')):
   st=p.lstat();name=p.relative_to(root).as_posix()
   if stat.S_ISREG(st.st_mode):out[name]=('file',stat.S_IMODE(st.st_mode),st.st_ino,st.st_nlink,hashlib.sha256(p.read_bytes()).hexdigest())
   elif stat.S_ISLNK(st.st_mode):out[name]=('symlink',os.readlink(p))
   elif stat.S_ISDIR(st.st_mode):out[name]=('dir',stat.S_IMODE(st.st_mode))
   elif stat.S_ISSOCK(st.st_mode):out[name]=('socket',stat.S_IMODE(st.st_mode))
   else:self.fail('unknown fixture object '+name)
  return out
 def test_pending_intent_and_hardlink_attacks_are_preserved(self):
  self.installed();first=self.success('start');self.success('stop')
  before=self.files(self.op);self.intercept_start=True
  def match(pid,regs,entering):
   return entering and trace_string(pid,regs.r10)=='cycle-00000000000000000001.record'
  rc,out,err=intercepted_link(self,match);self.assertLess(rc,0)
  cycles=self.updater/'cycles';final=cycles/'cycle-00000000000000000001.record';pending=cycles/(final.name+'.pending')
  self.assertFalse(final.exists());self.assertTrue(pending.is_file())
  canonical=pending.read_bytes();gid=canonical.decode().splitlines()[3]
  self.assertEqual(pending.stat().st_nlink,1);seen=[]
  def refuses(label):
   state=self.inventory(self.updater);origin=self.files(self.op)
   r=self.init('start');self.assertNotEqual(r.returncode,0,r.stdout+r.stderr)
   self.assertEqual(self.inventory(self.updater),state,label+' changed evidence')
   self.assertEqual(self.files(self.op),origin);seen.append(label)
  pending.write_bytes(canonical+b'CORRUPTED\n')
  try:refuses('corrupt-pending')
  finally:pending.write_bytes(canonical)
  pending.chmod(0o644)
  try:refuses('wrong-permissions')
  finally:pending.chmod(0o600)
  extra=self.root/'foreign-hardlink';os.link(pending,extra)
  try:refuses('unaccounted-hardlink')
  finally:extra.unlink()
  # Same bytes on a DIFFERENT inode must not be mistaken for our linked pair.
  final.write_bytes(canonical);final.chmod(0o600)
  try:refuses('same-bytes-different-inode')
  finally:final.unlink()
  target=self.root/'foreign-record';target.write_bytes(canonical);target.chmod(0o600)
  final.symlink_to(target)
  try:
   refuses('published-name-symlink');self.assertEqual(target.read_bytes(),canonical)
  finally:final.unlink();target.unlink()
  self.assertEqual(self.files(self.op),before)
  resumed=self.success('start');self.assertEqual(resumed['generationId'],gid)
  self.assertNotEqual(gid,first['generationId']);self.one_live(gid)
  self.assertEqual(final.read_bytes(),canonical);self.assertFalse(pending.exists())
  self.assertEqual(final.stat().st_nlink,1)
  self.assertEqual(self.success('start')['generationId'],gid)
  self.success('stop');self.assertEqual(self.files(self.op),before)
  print('PUBLICATION_ADVERSARIAL_RECEIPT '+json.dumps({'crashBeforeLink':True,'refusedPreservingEvidence':seen,'resumedSameIntent':True,'unrelatedFilesUnchanged':True}),flush=True)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([PublicationAdversarial('test_pending_intent_and_hardlink_attacks_are_preserved')]))
 raise SystemExit(not r.wasSuccessful())
