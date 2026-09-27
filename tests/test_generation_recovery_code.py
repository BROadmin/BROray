"""Retained coordinator closure must survive the authenticated temporary source."""
import hashlib,json,os,shutil,stat,subprocess,unittest
from test_generation_bootguard_binding import GuardBinding,GEN,CODE

FILES=['bin/broray-ops-guard']+['lib/'+n for n in ['operation-client.sh','operation-coordinator.sh','operation-owner.sh','operation-journal.sh','operation-report.sh','operation-report-facts.sh','operation-publication.sh','operation-route-recovery.sh','operation-platform-recovery.sh','operation-platform-service.sh','operation-platform-generation.sh','operation-platform-bootguard.sh','operation-public.jq','operation-report-public.jq','operation-scheduling.sh']]
class RecoveryCode(GuardBinding):
 def setUp(self):
  super().setUp();self.source=self.home/'temporary-code';self.source.mkdir(mode=0o700)
  for rel in FILES:
   p=self.source/rel;p.parent.mkdir(mode=0o700,exist_ok=True);shutil.copy2(CODE/rel,p)
  self.retained=self.op/'platform-recovery-code';self.code=self.retained/'code';self.code_binding=self.op/'platform-recovery-code.json'
 def code_args(self,verify=False):
  args=[GEN,'recovery-code-verify' if verify else 'recovery-code-stage',str(self.op),str(self.live_root),str(self.stage),self.intent_sha]
  return args if verify else args+[str(self.source)]
 def code_call(self,verify=False):return subprocess.run(self.code_args(verify),capture_output=True,text=True,timeout=5)
 def snapshot(self):
  return {str(p.relative_to(self.home)):(stat.S_IMODE(p.lstat().st_mode),str(p.readlink()) if p.is_symlink() else hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else 'directory') for p in self.home.rglob('*')}
 def refuse(self,verify=False):
  before=self.snapshot();r=self.code_call(verify);self.assertNotEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(self.snapshot(),before)
 def test_retains_exact_code_without_live_mutation(self):
  before=self.inventory(self.live_root);r=self.code_call();self.assertEqual(r.returncode,0,r.stderr);reply=json.loads(r.stdout)
  self.assertEqual(reply['phase'],'RECOVERY_CODE_STAGED');self.assertFalse(reply['activationAllowed']);self.assertFalse(reply['processAuthority'])
  self.assertEqual(reply['codeRoot'],str(self.code));self.assertEqual(self.inventory(self.source),self.inventory(self.code));self.assertEqual(self.inventory(self.live_root),before)
  held=self.snapshot();self.assertEqual(self.code_call().returncode,0);self.assertEqual(self.snapshot(),held)
 def test_verify_survives_removed_temporary_source(self):
  self.assertEqual(self.code_call().returncode,0);shutil.rmtree(self.source);before=self.snapshot();r=self.code_call(True)
  self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(json.loads(r.stdout)['phase'],'RECOVERY_CODE_VERIFIED');self.assertEqual(self.snapshot(),before)
 def test_retained_coordinator_runs_after_temporary_source_removed(self):
  self.assertEqual(self.code_call().returncode,0)
  shutil.rmtree(self.source)
  env=dict(os.environ,BRORAY_ROOT=str(self.live_root/'opt/broray'),BRORAY_OPS_CODE_ROOT=str(self.code),
   BRORAY_OPS_GUARD_HELD='1',BRORAY_STATE_ROOT=str(self.home/'probe-state'),
   BRORAY_OPS_RAM_ROOT=str(self.home/'probe-ram'),BRORAY_GLOBAL_LOCK=str(self.home/'probe-lock'))
  r=subprocess.run(['/bin/ash',str(self.code/'lib/operation-coordinator.sh'),'unsupported-readiness-probe'],env=env,capture_output=True,text=True,timeout=5)
  self.assertEqual(r.returncode,1,r.stdout+r.stderr)
  self.assertEqual(json.loads(r.stdout),{'ok':False,'errorCode':'INVALID_REQUEST'})
  self.assertEqual((self.code/'lib/operation-scheduling.sh').read_bytes(),(CODE/'lib/operation-scheduling.sh').read_bytes())
 def test_verify_cannot_create_missing_store(self):self.refuse(True)
 def test_missing_retained_library_not_recreated(self):self.assertEqual(self.code_call().returncode,0);(self.code/FILES[2]).unlink();self.refuse();self.refuse(True)
 def test_corrupt_retained_library_preserved(self):self.assertEqual(self.code_call().returncode,0);(self.code/FILES[2]).write_bytes(b'FOREIGN');self.refuse();self.refuse(True)
 def test_missing_binding_not_recreated(self):self.assertEqual(self.code_call().returncode,0);self.code_binding.unlink();self.refuse();self.refuse(True)
 def test_corrupt_binding_preserved(self):self.assertEqual(self.code_call().returncode,0);self.code_binding.write_bytes(b'{broken');self.refuse();self.refuse(True)
 def test_changed_source_refused_without_retained_mutation(self):self.assertEqual(self.code_call().returncode,0);(self.source/FILES[2]).write_bytes(b'CHANGED');self.refuse()
 def test_source_symlink_refused_before_binding(self):
  p=self.source/FILES[2];p.unlink();p.symlink_to(CODE/FILES[2]);self.refuse();self.assertFalse(self.code_binding.exists())
 def test_unknown_retained_entry_preserved(self):self.assertEqual(self.code_call().returncode,0);(self.code/'lib/foreign').write_bytes(b'KEEP');self.refuse();self.refuse(True)
 def test_preexisting_empty_store_not_adopted(self):self.retained.mkdir(mode=0o700);self.refuse()
 def test_missing_staged_receipt_not_recreated(self):self.assertEqual(self.code_call().returncode,0);(self.retained/'staged.receipt').unlink();self.refuse();self.refuse(True)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(RecoveryCode(n) for n in RecoveryCode.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
