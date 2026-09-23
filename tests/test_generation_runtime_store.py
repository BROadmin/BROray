"""Updater lifetime runtime must outlive prunable operation history."""
import hashlib,json,os,shutil,stat,subprocess,unittest
from pathlib import Path
from test_updater_generation import Generation,GEN

class RuntimeStore(Generation):
 def setUp(self):
  super().setUp();self.store=self.home/'runtimes';self.store.mkdir(mode=0o700)
  self.sha=hashlib.sha256(Path(GEN).read_bytes()).hexdigest();self.entry=self.store/self.sha
 def invoke(self,sha=None):return subprocess.run([GEN,'runtime-retain',str(self.store),sha or self.sha],capture_output=True,text=True,timeout=5)
 def inventory(self):
  return {str(p.relative_to(self.store)):(stat.S_IMODE(p.lstat().st_mode),os.readlink(p) if p.is_symlink() else hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else 'directory') for p in self.store.rglob('*')}
 def refuse(self,**args):
  before=self.inventory();r=self.invoke(**args);self.assertNotEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(self.inventory(),before)
 def test_exact_persistent_copy_survives_removal_of_operation_directory(self):
  r=self.invoke();self.assertEqual(r.returncode,0,r.stderr);reply=json.loads(r.stdout)
  self.assertTrue(reply['ok']);self.assertEqual(reply['runtimeSha256'],self.sha);self.assertEqual(reply['runtimePath'],str(self.entry/'runtime'))
  self.assertEqual((self.entry/'runtime').read_bytes(),Path(GEN).read_bytes());self.assertEqual(stat.S_IMODE((self.entry/'runtime').stat().st_mode),0o700)
  self.assertEqual((self.entry/'runtime').stat().st_nlink,1);self.assertEqual(stat.S_IMODE((self.entry/'identity.json').stat().st_mode),0o600)
  saved=json.loads((self.entry/'identity.json').read_bytes());self.assertEqual(saved['runtimeSha256'],self.sha);self.assertEqual(saved['contract'],'broray-updater-runtime/1')
  op=self.home/'operation';op.mkdir();shutil.copyfile(self.entry/'runtime',op/'temporary');shutil.rmtree(op)
  q=subprocess.run([str(self.entry/'runtime'),'--version'],capture_output=True,text=True,timeout=3)
  self.assertEqual(q.returncode,0);self.assertEqual(q.stdout.strip(),'broray-updater-generation/2 supervised-from-birth syscall-containment')
  before=self.inventory();self.assertEqual(self.invoke().returncode,0);self.assertEqual(self.inventory(),before)
 def test_wrong_expected_hash_creates_nothing(self):self.refuse(sha='0'*64)
 def test_preexisting_empty_entry_is_not_adopted(self):self.entry.mkdir(mode=0o700);self.refuse()
 def test_missing_runtime_is_not_recreated(self):self.assertEqual(self.invoke().returncode,0);(self.entry/'runtime').unlink();self.refuse()
 def test_corrupt_runtime_is_preserved(self):self.assertEqual(self.invoke().returncode,0);(self.entry/'runtime').write_bytes(b'foreign');self.refuse()
 def test_missing_identity_is_not_recreated(self):self.assertEqual(self.invoke().returncode,0);(self.entry/'identity.json').unlink();self.refuse()
 def test_corrupt_identity_is_preserved(self):self.assertEqual(self.invoke().returncode,0);(self.entry/'identity.json').write_bytes(b'{broken');self.refuse()
 def test_foreign_extra_entry_preserved(self):self.assertEqual(self.invoke().returncode,0);(self.entry/'foreign').write_bytes(b'foreign');self.refuse()
 def test_symlink_entry_cannot_redirect_writes(self):
  other=self.home/'foreign';other.mkdir();self.entry.symlink_to(other);self.refuse();self.assertEqual(list(other.iterdir()),[])
 def test_hardlinked_runtime_refused(self):
  self.assertEqual(self.invoke().returncode,0);other=self.home/'foreign-runtime';os.link(self.entry/'runtime',other);self.refuse();self.assertEqual(other.read_bytes(),Path(GEN).read_bytes())
 def test_writable_store_refused(self):self.store.chmod(0o777);self.refuse()

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(RuntimeStore(n) for n in RuntimeStore.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
