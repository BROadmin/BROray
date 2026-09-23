"""Native retained-code entry; existing guard, canonical paths, no mutation."""
import fcntl,json,os,shutil,subprocess,unittest
from test_preflight_boot_context import BootContext
from test_preflight_admission import GUARD
from test_generation_recovery_code import FILES

class BootEntry(BootContext):
 def prepare(self):
  source=self.home/'temporary-code'
  from test_preflight_migration_staging import CODE
  for rel in FILES:
   dest=source/rel;dest.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(GUARD if rel=='bin/broray-ops-guard' else CODE/rel,dest)
  shutil.copytree(CODE/'share',source/'share')
  self.env.update(BRORAY_OPS_CODE_ROOT=str(source),BRORAY_OPS_GUARD=str(source/'bin/broray-ops-guard'))
  shell=self.home/'router/opt/bin/ash';shell.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile('/bin/busybox',shell);shell.chmod(0o755)
  super().prepare();self.source=source;shutil.rmtree(source)
  b=json.loads((self.op/'platform-bootguard.json').read_bytes());self.native=self.updater/'runtimes'/b['nativeSha256']/'runtime';self.migration_sha=b['migrationIntentSha256']
 def entry(self,extra=None):
  return subprocess.run([str(self.native),'recovery-inspect',str(self.home/'router'),self.op.name,self.migration_sha,self.nonce],env={**os.environ,**(extra or {})},capture_output=True,text=True,timeout=15)
 def test_native_entry_uses_retained_code_after_source_removed(self):
  self.prepare();before=self.snapshot();r=self.entry();self.assertEqual(r.returncode,0,r.stdout+r.stderr);p=json.loads(r.stdout)
  self.assertEqual(p['phase'],'BOOT_CONTEXT_VERIFIED');self.assertFalse(p['oldBootEnded'])
  for k in ['serviceStopped','signalsAuthorized','activationAllowed','executorAuthorized','platformReady']:self.assertFalse(p[k])
  self.assertEqual(self.snapshot(),before);self.assertIsNone(self.service.poll());self.assertFalse(self.source.exists())
 def test_corrupt_script_is_preserved_and_never_executed(self):
  self.prepare();marker=self.home/'executed-unknown-code';p=self.op/'platform-recovery-code/code/lib/operation-coordinator.sh'
  p.write_text('echo corrupt >"'+str(marker)+'"\nexit 0\n');before=self.snapshot();r=self.entry();self.assertNotEqual(r.returncode,0);self.assertEqual(self.snapshot(),before);self.assertFalse(marker.exists())
 def test_missing_guard_not_created(self):
  self.prepare();p=self.state/'operations.guard';p.unlink();before=self.snapshot();r=self.entry();self.assertNotEqual(r.returncode,0);self.assertFalse(p.exists());self.assertEqual(self.snapshot(),before)
 def test_existing_lock_excludes_entry_without_evidence_changes(self):
  self.prepare();before=self.snapshot()
  with (self.state/'operations.guard').open('r+') as f:
   fcntl.flock(f,fcntl.LOCK_EX);r=self.entry();self.assertNotEqual(r.returncode,0)
  self.assertEqual(self.snapshot(),before);self.assertIsNone(self.service.poll())
 def test_caller_environment_cannot_redirect_coordinator(self):
  self.prepare();marker=self.home/'injected';script=self.home/'env.sh';script.write_text('echo bad >"'+str(marker)+'"\n')
  before=self.snapshot();r=self.entry({'BRORAY_OPS_CODE_ROOT':'/missing','BRORAY_STATE_ROOT':'/missing','BRORAY_OPS_PROC_ROOT':'/missing','BRORAY_OPS_TEST':'1','BRORAY_OPS_TEST_IDENTITIES':'/missing','BRORAY_OPS_GUARD':'/missing','ENV':str(script),'BASH_ENV':str(script),'PATH':'/missing'})
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(self.snapshot(),before);self.assertFalse(marker.exists())

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(BootEntry(n) for n in BootEntry.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
