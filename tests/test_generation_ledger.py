"""Write-once evidence: whole-chain preservation and metadata rejection."""
import hashlib,json,os,unittest
from test_updater_generation import Generation,GEN

class Ledger(Generation):
 def evidence(self):
  return {p.name:p.read_bytes() for p in sorted(self.domain.glob('*.json'))}
 def validate_chain(self):
  records=[self.domain/'state.json',*sorted(self.domain.glob('revision-*.json'))]
  previous=''
  for revision,path in enumerate(records,1):
   data=path.read_bytes();r=json.loads(data);st=path.stat()
   self.assertEqual(r['revision'],revision);self.assertEqual(r['previousRecordSha256'],previous)
   self.assertEqual(r['generationId'],self.gid);self.assertEqual(r['platformManifestSha256'],self.sha)
   self.assertEqual(st.st_mode&0o7777,0o600);self.assertEqual(st.st_nlink,1);self.assertEqual(st.st_uid,os.geteuid())
   previous=hashlib.sha256(data).hexdigest()
  return len(records)
 def test_chain_durable_and_start_anchor_unchanged(self):
  self.start();self.running();anchor=(self.domain/'state.json').read_bytes();self.assertEqual(self.call('STOP').returncode,0);self.stopped()
  self.assertGreater(self.validate_chain(),5);self.assertEqual((self.domain/'state.json').read_bytes(),anchor)
  self.assertEqual(list(self.domain.glob('pending-*')),[])
 def test_duplicate_preserves_entire_chain(self):
  import subprocess
  self.start('while :; do :; done\n');self.running();before=self.evidence()
  r=subprocess.run([GEN,'run',str(self.domain),'duplicate',self.sha,'--','/bin/ash','-c','exit 99'],capture_output=True,timeout=5)
  self.assertNotEqual(r.returncode,0);self.assertEqual(self.evidence(),before)
 def test_wrong_stop_and_replay_preserve_entire_chain(self):
  self.start('while :; do :; done\n');self.running();before=self.evidence()
  self.assertNotEqual(self.call('STOP',gid='wrong').returncode,0);self.assertEqual(self.evidence(),before)
  self.assertEqual(self.call('STOP').returncode,0);self.stopped();before=self.evidence()
  self.assertNotEqual(self.call('STOP',nonce='wrong').returncode,0);self.assertEqual(self.evidence(),before)
  self.assertEqual(self.call('STOP').returncode,0);self.assertEqual(self.evidence(),before)
 def corrupt_old_record(self,kind):
  p=self.start(self.writer_body());pid=self.writer_pid();self.wait(lambda:len(list(self.domain.glob('revision-*.json')))>3)
  record=sorted(self.domain.glob('revision-*.json'))[0];foreign=self.home/'foreign';foreign.write_text('FOREIGN')
  if kind=='missing':record.unlink()
  elif kind=='corrupt':record.write_text('{old-broken')
  elif kind=='mode':record.chmod(0o644)
  elif kind=='link':os.link(record,self.home/'hardlink')
  elif kind=='symlink':record.unlink();record.symlink_to(foreign)
  self.assertNotEqual(p.wait(timeout=3),0);self.wait(lambda:not self.live(pid))
  self.assertEqual(foreign.read_text(),'FOREIGN')
  if kind=='missing':self.assertFalse(record.exists())
  elif kind=='corrupt':self.assertEqual(record.read_text(),'{old-broken')
  elif kind=='mode':self.assertEqual(record.stat().st_mode&0o777,0o644)
  elif kind=='link':self.assertEqual(record.stat().st_nlink,2)
  elif kind=='symlink':self.assertTrue(record.is_symlink())
 def test_old_revision_missing(self):self.corrupt_old_record('missing')
 def test_old_revision_corrupt(self):self.corrupt_old_record('corrupt')
 def test_old_revision_mode_changed(self):self.corrupt_old_record('mode')
 def test_old_revision_hardlink(self):self.corrupt_old_record('link')
 def test_old_revision_symlink(self):self.corrupt_old_record('symlink')
 def test_old_revision_mmap_fails_closed(self):
  import mmap
  p=self.start(self.writer_body());pid=self.writer_pid()
  record=sorted(self.domain.glob('revision-*.json'))[0]
  original=record.read_bytes();corrupt=b'!'+original[1:]
  with record.open('r+b') as f:
   with mmap.mmap(f.fileno(),0) as mapping:mapping[0:1]=b'!';mapping.flush()
  self.assertNotEqual(p.wait(timeout=3),0);self.wait(lambda:not self.live(pid));self.assertEqual(record.read_bytes(),corrupt)
 def test_status_validates_historical_bytes_before_success(self):
  import mmap
  p=self.start('i=0; while [ "$i" -lt 12 ]; do /bin/true; i=$((i+1)); done\necho ready >"$TEST_HOME/history.ready"\nwhile :; do :; done\n')
  self.wait(lambda:(self.home/'history.ready').exists())
  record=sorted(self.domain.glob('revision-*.json'))[8];original=record.read_bytes();corrupt=b'!'+original[1:]
  with record.open('r+b') as f:
   with mmap.mmap(f.fileno(),0) as mapping:mapping[0:1]=b'!';mapping.flush()
  self.assertNotEqual(self.call('STATUS').returncode,0);self.assertNotEqual(p.wait(timeout=3),0);self.assertEqual(record.read_bytes(),corrupt)

if __name__=='__main__':
 names=[n for n in Ledger.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([Ledger(n) for n in names]));raise SystemExit(0 if result.wasSuccessful() else 1)
