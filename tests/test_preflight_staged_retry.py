"""A new authenticated target must not reuse the failed target's transaction."""
from pathlib import Path
import hashlib,json,shutil,subprocess,unittest
from test_preflight_staged_recovery import StagedRecovery
from test_preflight_admission import CODE,GUARD
from test_updater_generation import GEN

class StagedRetry(StagedRecovery):
 def changed_target(self):
  app=self.home/'new-authenticated-code';shutil.copytree(CODE,app)
  for name,source in [('broray-updater-generation',GEN),('broray-ops-guard',GUARD)]:
   p=app/'bin'/name;shutil.copyfile(source,p);p.chmod(0o755)
  payload=app/'share/updater-platform'
  init=payload/'opt/etc/init.d/S22broray-updater';init.write_bytes(init.read_bytes()+b'\n# isolated next target\n')
  manifest=payload/'SHA256SUMS';rows=[]
  for line in manifest.read_text().splitlines():
   _,name=line.split('  ',1);rows.append(hashlib.sha256((payload/name).read_bytes()).hexdigest()+'  '+name+'\n')
  manifest.write_text(''.join(rows))
  digest=hashlib.sha256(manifest.read_bytes()).hexdigest()
  env={**self.env,'BRORAY_OPS_CODE_ROOT':str(app),'BRORAY_HANDOFF_ROOT_PREFIX':str(self.root),
       'BRORAY_HANDOFF_APP_ROOT':str(self.live),'BRORAY_HANDOFF_PAYLOAD_ROOT':str(payload),
       'BRORAY_HANDOFF_ASH':'/bin/ash','BRORAY_HANDOFF_PATH':self.env['PATH']}
  return app,digest,env
 def test_new_target_aborts_staging_then_gets_new_operation_and_reboot_gate(self):
  service,_=self.start_service();self.assertEqual(self.stage().returncode,0);old=self.operation()
  preserved=self.freeze(old/'platform-migration');app,digest,env=self.changed_target()
  self.assertNotEqual(digest,self.env['TEST_SHA'])
  result=subprocess.run(['/bin/ash',str(app/'lib/universal-platform-handoff.sh'),'preflight',digest],
                        env=env,capture_output=True,text=True,timeout=120)
  self.assertEqual(result.returncode,75,result.stdout+result.stderr)
  reply=json.loads(result.stdout);self.assertEqual(reply['errorCode'],'UPDATER_LEGACY_REBOOT_REQUIRED')
  self.assertNotEqual(reply['operationId'],old.name)
  self.assertEqual(self.readstate()['platformPreflight']['expectedPlatformManifestSha256'],digest)
  self.assertEqual(json.loads((old/'state.json').read_text())['state'],'aborted')
  self.assertTrue((old/'retired-lock').is_symlink());self.assertEqual(self.freeze(old/'platform-migration'),preserved)
  self.assertFalse((self.updater/'request.lock').exists());self.assertFalse((self.updater/'queue').exists())
  self.assertIsNone(service.poll())

if __name__=='__main__':
 tests=[StagedRetry(n) for n in StagedRetry.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(tests))
 raise SystemExit(0 if result.wasSuccessful() else 1)
