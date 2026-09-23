"""Canonical observational check after real boot; fixture loader verifies code.

This trusted fixture loader is not a production automatic recovery entry.
"""
import base64,hashlib,json,os,stat,subprocess,unittest
from pathlib import Path

class CanonicalBootResume(unittest.TestCase):
 def setUp(self):
  self.e=json.loads(Path('/work/migration-transfer.json').read_bytes());self.assertEqual(self.e['fixture'],'canonical-boot-context/1')
  self.root=Path(self.e['root']);self.assertRegex(str(self.root),r'^/tmp/f02-admission-[a-z0-9_]+$')
  self.state=self.root/'router/opt/var/lib/broray';self.op=self.state/'operations'/self.e['operationId'];self.updater=self.root/'router/opt/var/lib/broray-updater'
  self.code=self.op/'platform-recovery-code/code';self.native=self.updater/'runtimes'/self.e['nativeSha256']/'runtime'
  self.boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip();self.assertNotEqual(self.boot,self.e['oldBootId'])
  self.restore();self.addCleanup(self.restore)
 def restore(self):
  for row in self.e['rows']:
   if row['kind']=='file':
    p=self.root/row['path'];p.write_bytes(base64.b64decode(row['base64'],validate=True));p.chmod(row['mode'])
 def snapshot(self):
  return {str(p.relative_to(self.root)):(stat.S_IMODE(p.lstat().st_mode),os.readlink(p) if p.is_symlink() else hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else 'directory') for p in self.root.rglob('*')}
 def inspect(self,nonce=None):
  # Fixture loader uses the trusted export hash before running retained bytes.
  self.assertEqual(hashlib.sha256(self.native.read_bytes()).hexdigest(),self.e['nativeSha256'])
  self.assertEqual(hashlib.sha256((self.code/'bin/broray-ops-guard').read_bytes()).hexdigest(),self.e['linuxGuardSha256'])
  binding=json.loads((self.op/'platform-bootguard.json').read_bytes())
  r=subprocess.run([str(self.native),'recovery-code-verify',str(self.op),str(self.root/'router'),str(self.op/'platform-migration'),binding['migrationIntentSha256']],capture_output=True,text=True,timeout=5)
  if r.returncode:return r
  env={**os.environ,'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','BRORAY_ROOT':str(self.root/'router/opt/broray'),
    'BRORAY_STATE_ROOT':str(self.state),'BRORAY_OPS_CODE_ROOT':str(self.code),'BRORAY_OPS_GUARD':str(self.code/'bin/broray-ops-guard'),
    'BRORAY_OPS_ASH':'/bin/ash','BRORAY_ROUTES_API_LOCK':str(self.root/'router/opt/var/lock/broray/global-operation.lock'),
    'BRORAY_OPS_UPDATER_ROOT':str(self.updater),'BRORAY_LEGACY_GLOBAL_LOCK':str(self.root/'router/tmp/broray-global-operation.lock'),
    'BRORAY_OPS_RAM_ROOT':str(self.root/'ram'),'TEST_ID':self.e['operationId'],'TEST_NONCE':nonce or self.e['stopNonce']}
  return subprocess.run(['/bin/ash','-c','set -u\n. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"\nbroray_ops_call platform-preflight-boot-context "$TEST_ID" "$TEST_NONCE"'],env=env,capture_output=True,text=True,timeout=15)
 def refuse(self,**kwargs):
  before=self.snapshot();r=self.inspect(**kwargs);self.assertNotEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(self.snapshot(),before)
 def test_real_boot_and_removed_source_preserve_all_state(self):
  self.assertTrue(self.e['legacyOwnerLiveAtExport']);self.assertTrue(self.e['temporaryCodeRemoved']);self.assertFalse((self.root/'temporary-code').exists())
  before=self.snapshot()
  for _ in range(2):
   r=self.inspect();self.assertEqual(r.returncode,0,r.stdout+r.stderr);p=json.loads(r.stdout)
   self.assertEqual(p['phase'],'BOOT_CONTEXT_VERIFIED');self.assertTrue(p['oldBootEnded']);self.assertEqual(p['oldBootId'],self.e['oldBootId']);self.assertEqual(p['currentBootId'],self.boot)
   for flag in ['serviceStopped','signalsAuthorized','activationAllowed','executorAuthorized','platformReady']:self.assertFalse(p[flag])
   self.assertEqual(self.snapshot(),before)
  self.assertFalse((self.op/'executor.json').exists())
  print('CANONICAL_BOOT_CONTEXT_RECEIPT '+json.dumps({'proof':p,'scope':self.e['scope'],'automaticBootEntry':False}),flush=True)
 def test_wrong_nonce_preserved(self):self.refuse(nonce='f'*32)
 def test_corrupt_fence_owner_preserved(self):(self.op/'fence/owner.json').write_bytes(b'{broken');self.refuse()
 def test_missing_operations_guard_not_recreated(self):p=self.state/'operations.guard';p.unlink();self.refuse();self.assertFalse(p.exists())
 def test_empty_publisher_fence_preserved(self):
  p=self.updater/'request.lock';p.mkdir(mode=0o700);self.addCleanup(p.rmdir);self.refuse()
 def test_changed_legacy_projection_preserved(self):(self.updater/'daemon.pid').write_bytes(b'999999\n');self.refuse()
 def test_corrupt_service_snapshot_preserved(self):(self.op/'platform-service.json').write_bytes(b'{broken');self.refuse()
 def test_child_registry_not_collected(self):
  p=self.op/'children.json';p.write_text('{"children":[]}');self.addCleanup(p.unlink);self.refuse()

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(CanonicalBootResume(n) for n in CanonicalBootResume.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
