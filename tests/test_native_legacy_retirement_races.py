"""External corruption at a rename boundary must not yield verified STOPPED."""
import hashlib,json,shutil,unittest
from test_canonical_boot_context_resume import CanonicalBootResume
from test_generation_bootguard_crash import GuardCrash
from test_generation_migration_crash import trace_string

class LegacyRetirementRaces(CanonicalBootResume):
 intercepted=GuardCrash.intercepted
 def setUp(self):
  super().setUp();self.processes=[];self.retired=self.op/'platform-legacy-retirement';self.addCleanup(self.clear_added)
 def clear_added(self):
  if self.retired.exists():shutil.rmtree(self.retired)
  p=self.op/'platform-legacy-retirement.json'
  if p.exists():p.unlink()
 def restore(self):
  # The shared read-only fixture restores files only. Retirement also moves
  # the original empty daemon.lock directory; every case needs its full input.
  for row in self.e['rows']:
   if row['kind']=='directory':
    p=self.root/row['path'];p.mkdir(parents=True,exist_ok=True);p.chmod(row['mode'])
  super().restore()
 def guard_args(self):
  b=json.loads((self.op/'platform-bootguard.json').read_bytes())
  return [str(self.native),'recovery-retire',str(self.root/'router'),self.e['operationId'],b['migrationIntentSha256'],self.e['stopNonce']]
 def boundary(self,name='daemon.pid'):
  return lambda pid,r,entry:entry and r.orig_rax==316 and trace_string(pid,r.rsi)==name
 def corrupt(self,path,boundary='daemon.pid'):
  rc,out,err=self.intercepted(self.boundary(boundary),lambda:path.write_bytes(b'{broken'))
  print('RETIREMENT_RACE_EVIDENCE '+json.dumps({'path':str(path),'bytes':path.read_text(),'exit':rc,'stoppedReceiptExists':(self.retired/'stopped.receipt').exists(),'stdout':out.decode(),'stderr':err.decode()}),flush=True)
  self.assertNotEqual(rc,0);self.assertEqual(path.read_bytes(),b'{broken');self.assertFalse((self.retired/'stopped.receipt').exists())
 def test_changed_original_owner_cannot_authorize_stopped(self):self.corrupt(self.op/'owner.json')
 def test_changed_legacy_binding_cannot_authorize_stopped(self):self.corrupt(self.op/'platform-legacy-control.json')
 def test_changed_retirement_intent_cannot_authorize_stopped(self):self.corrupt(self.retired/'intent.record','daemon.lock')
 def test_changed_platform_bytes_cannot_authorize_stopped(self):self.corrupt(self.root/'router/opt/bin/broray-updaterctl')
 def test_changed_retained_code_cannot_authorize_stopped(self):self.corrupt(self.code/'lib/operation-platform-recovery.sh')
 def test_foreign_source_at_move_is_preserved(self):
  path=self.updater/'daemon.pid';rc,_,_=self.intercepted(self.boundary(),lambda:path.write_bytes(b'foreign'))
  self.assertNotEqual(rc,0);self.assertFalse((self.retired/'stopped.receipt').exists());self.assertEqual((self.retired/'objects/daemon.pid').read_bytes(),b'foreign')
 def test_foreign_destination_is_not_overwritten(self):
  path=self.retired/'objects/daemon.pid';rc,_,_=self.intercepted(self.boundary(),lambda:path.write_bytes(b'foreign'))
  self.assertNotEqual(rc,0);self.assertFalse((self.retired/'stopped.receipt').exists());self.assertEqual(path.read_bytes(),b'foreign');self.assertTrue((self.updater/'daemon.pid').exists())
 def test_publisher_created_during_retirement_is_preserved(self):
  path=self.updater/'request.lock';self.addCleanup(lambda:path.rmdir() if path.exists() else None)
  rc,_,_=self.intercepted(self.boundary('daemon.lock'),lambda:path.mkdir(mode=0o700))
  self.assertNotEqual(rc,0);self.assertTrue(path.is_dir());self.assertFalse((self.retired/'stopped.receipt').exists())

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(LegacyRetirementRaces(n) for n in LegacyRetirementRaces.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
