"""Exact seven-file backup after verified legacy STOPPED, no platform mutation."""
import hashlib,json,stat,subprocess,unittest,shutil
from test_native_legacy_retirement_evidence import LegacyRetirementEvidence

FILES=['opt/bin/broray-updaterctl','opt/etc/init.d/S22broray-updater',
 'opt/libexec/broray-updater/broray-compat.sh','opt/libexec/broray-updater/broray-migrate-legacy.sh',
 'opt/libexec/broray-updater/minisign','opt/libexec/broray-updater/broray-updater.sh',
 'opt/libexec/broray-updater/xray-wrapper']

class PlatformBackup(LegacyRetirementEvidence):
 def setUp(self):
  super().setUp();self.backup_dir=self.op/'platform-backup';self.addCleanup(self.clear_backup)
 def clear_backup(self):
  if self.backup_dir.exists():shutil.rmtree(self.backup_dir)
  p=self.op/'platform-backup.json'
  if p.exists():p.unlink()
 def backup(self):
  args=self.guard_args();args[1]='recovery-backup'
  return subprocess.run(args,capture_output=True,text=True,timeout=15)
 def test_backup_preserves_platform_and_is_exact_on_replay(self):
  self.first();before=self.snapshot()
  original={rel:(self.root/'router'/rel).read_bytes() for rel in FILES}
  modes={rel:stat.S_IMODE((self.root/'router'/rel).stat().st_mode) for rel in FILES}
  r=self.backup();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  reply=json.loads(r.stdout);self.assertEqual(reply['phase'],'BACKUP_READY');self.assertFalse(reply['activationAllowed'])
  backup=self.op/'platform-backup';intent=json.loads((backup/'intent.json').read_bytes())
  self.assertEqual(intent['operationId'],self.e['operationId']);self.assertEqual(intent['stopNonce'],self.e['stopNonce'])
  self.assertTrue(intent['oldServiceWasRunning']);self.assertEqual(intent['oldBootId'],self.e['oldBootId'])
  self.assertEqual(intent['oldGenerationId'],'legacy@'+self.e['oldBootId'])
  self.assertEqual(len(intent['before']),7)
  for i,rel in enumerate(FILES):
   self.assertEqual((self.root/'router'/rel).read_bytes(),original[rel]);self.assertEqual(stat.S_IMODE((self.root/'router'/rel).stat().st_mode),modes[rel])
   self.assertEqual((backup/f'before-{i}').read_bytes(),original[rel])
   self.assertEqual(intent['before'][i],{'path':rel,'present':True,'mode':modes[rel],'sha256':hashlib.sha256(original[rel]).hexdigest()})
  after=self.snapshot()
  for path,value in before.items():self.assertEqual(after[path],value,path)
  again=self.backup();self.assertEqual(again.returncode,0,again.stdout+again.stderr);self.assertTrue(json.loads(again.stdout)['replayed']);self.assertEqual(self.snapshot(),after)
  print('PLATFORM_BACKUP_RECEIPT '+json.dumps({'phase':reply['phase'],'files':len(FILES),'replayExact':True,'platformMutated':False}),flush=True)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([PlatformBackup('test_backup_preserves_platform_and_is_exact_on_replay')]))
 raise SystemExit(not r.wasSuccessful())
