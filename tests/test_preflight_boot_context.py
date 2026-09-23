"""Boot-context inspection has no executor/cleanup/STOPPED authority."""
import json,stat,unittest
from test_preflight_bootguard import PreflightBootguard

class BootContext(PreflightBootguard):
 def prepare(self):
  self.service,_=self.start_service();r=self.guarded();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.op=self.operation();self.nonce=self.bound()['stopNonce']
 def snapshot(self):
  return {str(p.relative_to(self.home)):(stat.S_IMODE(p.lstat().st_mode),str(p.readlink()) if p.is_symlink() else p.read_bytes() if p.is_file() else 'directory') for p in self.home.rglob('*')}
 def inspect(self,nonce=None):
  return self.shell('broray_ops_call platform-preflight-boot-context "$TEST_ID" "$TEST_NONCE"',env={**self.env,'TEST_ID':self.op.name,'TEST_NONCE':nonce or self.nonce},timeout=15)
 def refuse(self,**kwargs):
  before=self.snapshot();r=self.inspect(**kwargs);self.assertNotEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(self.snapshot(),before);self.assertIsNone(self.service.poll())
 def test_same_boot_read_only_proof_does_not_authorize_stop(self):
  self.prepare();before=self.snapshot();r=self.inspect();self.assertEqual(r.returncode,0,r.stdout+r.stderr);p=json.loads(r.stdout)
  self.assertEqual(p['phase'],'BOOT_CONTEXT_VERIFIED');self.assertFalse(p['oldBootEnded']);self.assertFalse(p['serviceStopped']);self.assertFalse(p['activationAllowed']);self.assertFalse(p['executorAuthorized']);self.assertFalse(p['signalsAuthorized'])
  self.assertEqual(self.snapshot(),before);self.assertIsNone(self.service.poll());self.assertFalse((self.op/'executor.json').exists())
 def test_wrong_nonce_preserves_evidence(self):self.prepare();self.refuse(nonce='1'*32)
 def test_corrupt_code_preserved(self):self.prepare();(self.op/'platform-recovery-code/code/lib/operation-coordinator.sh').write_bytes(b'{broken');self.refuse()
 def test_missing_operations_guard_is_not_recreated(self):self.prepare();p=self.state/'operations.guard';p.unlink();self.refuse();self.assertFalse(p.exists())
 def test_nonempty_child_registry_is_not_collected(self):
  self.prepare();owner=json.loads((self.op/'owner.json').read_bytes())['owner'];(self.op/'children.json').write_text(json.dumps({'children':[owner]}));self.refuse()
 def test_live_empty_publisher_fence_is_preserved(self):self.prepare();(self.updater/'request.lock').mkdir(mode=0o700);self.refuse()

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(BootContext(n) for n in BootContext.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
