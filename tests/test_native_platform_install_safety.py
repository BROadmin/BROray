"""Install evidence is exact; unknown bytes and original strict checks survive."""
import json,shutil,subprocess,unittest
from test_native_platform_install import PlatformInstall,FILES

class InstallFixture(PlatformInstall):
 def ready(self):
  self.first();r=self.backup();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
 def install(self):return self.invoke_phase('recovery-install')
 def installed(self):
  self.ready();r=self.install();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
 def refuse_install(self):
  before=self.snapshot();r=self.install();self.assertNotEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(self.snapshot(),before)
 def guard(self,verb):
  b=json.loads((self.op/'platform-bootguard.json').read_bytes())
  return subprocess.run([str(self.native),verb,str(self.op/'platform-bootguard'),str(self.root/'router'),str(self.op/'platform-migration'),b['migrationIntentSha256']],capture_output=True,text=True,timeout=5)

class InstallSafety(InstallFixture):
 def test_no_backup_cannot_install(self):self.first();self.refuse_install();self.assertFalse((self.op/'platform-install').exists())
 def test_missing_copy_cannot_install(self):self.ready();p=self.backup_dir/'before-0';p.unlink();self.refuse_install();self.assertFalse(p.exists())
 def test_corrupt_copy_preserved(self):self.ready();p=self.backup_dir/'before-1';p.write_bytes(b'{broken');self.refuse_install();self.assertEqual(p.read_bytes(),b'{broken')
 def test_foreign_live_file_preserved(self):self.ready();p=self.root/'router'/FILES[4];p.write_bytes(b'FOREIGN');self.refuse_install();self.assertEqual(p.read_bytes(),b'FOREIGN')
 def test_missing_terminal_cannot_be_recreated(self):self.installed();p=self.op/'platform-install/installed.receipt';p.unlink();self.refuse_install();self.assertFalse(p.exists())
 def test_corrupt_intent_preserved(self):self.installed();p=self.op/'platform-install/intent.record';p.write_bytes(b'{broken');self.refuse_install();self.assertEqual(p.read_bytes(),b'{broken')
 def test_missing_file_done_preserved(self):self.installed();p=self.op/'platform-install/entry-3.done';p.unlink();self.refuse_install();self.assertFalse(p.exists())
 def test_unknown_install_evidence_preserved(self):self.installed();p=self.op/'platform-install/foreign';p.write_bytes(b'KEEP');self.refuse_install();self.assertEqual(p.read_bytes(),b'KEEP')
 def test_same_bytes_foreign_inode_is_not_adopted(self):
  self.installed();p=self.root/'router'/FILES[0];data=p.read_bytes();p.unlink();p.write_bytes(data);p.chmod(0o755);self.refuse_install()
 def test_strict_guard_still_refuses_installed_files(self):
  self.installed();before=self.snapshot();r=self.guard('guard-verify-bound');self.assertNotEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(self.snapshot(),before)
  r=self.guard('guard-evidence-bound');self.assertEqual(r.returncode,0,r.stdout+r.stderr);proof=json.loads(r.stdout)
  self.assertEqual(proof['phase'],'BOOT_GUARD_EVIDENCE_VERIFIED')
  for k in ['serviceStopped','activationAllowed','processAuthority','platformReady']:self.assertFalse(proof[k])
  self.assertEqual(self.snapshot(),before)
 def test_corrupt_displaced_legacy_inode_refuses_evidence(self):
  p=next((self.root/'router/opt/etc/init.d').glob('.broray-bg-*.previous'));p.write_bytes(b'{broken');before=self.snapshot()
  r=self.guard('guard-evidence-bound');self.assertNotEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(self.snapshot(),before)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(InstallSafety(n) for n in InstallSafety.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
