"""Existing coordinator stop must work against the actual installed generation.

No completed operation is reopened. A fresh canonical operation owns STOP.
This is a component prerequisite, not acceptance of init stop/restart.
"""
import json,os,subprocess,time,unittest
from pathlib import Path
from test_installed_init_control import InstalledInit

class InstalledGenerationStop(InstalledInit):
 def stop_created_generation(self):
  # Multiple immutable retired launches are expected after public service
  # cycles. They never authorize signalling another live generation.
  starts=list((self.updater/'starts').glob('*/launch.record'));active=[]
  for launch in starts:
   rows=launch.read_text().splitlines();domain=Path(rows[2])
   self.assertEqual(rows[3],launch.parent.name)
   self.assertEqual(domain,self.updater/'generations'/rows[3])
   if (domain/'boot-ended.receipt').exists():
    r=subprocess.run([str(self.native),'generation-boot-verify',str(domain)],capture_output=True,text=True,timeout=4)
    self.assertEqual(r.returncode,0,r.stdout+r.stderr);proof=json.loads(r.stdout)
    self.assertEqual(proof['phase'],'BOOT_ENDED_HISTORY_VERIFIED');self.assertFalse(proof['signalsAuthorized'])
    self.assertNotEqual(proof['oldBootId'],Path('/proc/sys/kernel/random/boot_id').read_text().strip())
    continue
   latest=sorted(domain.glob('revision-*.json'))
   if not latest:
    self.assertFalse((domain/'state.json').exists(),'incomplete born generation must not disappear from cleanup')
    continue
   ledger=json.loads(latest[-1].read_bytes())
   operation=ledger.get('stopOperationId') or self.op.name
   nonce=ledger.get('stopNonce') or self.e['stopNonce']
   if (domain/'retirement.receipt').exists():
    self.assertEqual(ledger['state'],'STOPPED');self.assertEqual(ledger['children'],[])
    self.assertEqual(ledger['awaitingBirth'],[]);self.assertEqual(ledger['exitedUnreaped'],[])
    r=subprocess.run([str(self.native),'control',str(domain),'RETIRE',rows[3],rows[4],operation,nonce],capture_output=True,text=True,timeout=4)
    self.assertEqual(r.returncode,0,r.stdout+r.stderr)
   else:active.append((rows,domain,operation,nonce))
  self.assertLessEqual(len(active),1,'more than one unretired fixture generation')
  for rows,domain,operation,nonce in active:
   def control(verb):return subprocess.run([str(self.native),'control',str(domain),verb,rows[3],rows[4],operation,nonce],capture_output=True,text=True,timeout=4)
   r=control('STOP');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
   end=time.monotonic()+5
   while True:
    r=control('STATUS');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
    if json.loads(r.stdout)['state']=='STOPPED':break
    self.assertLess(time.monotonic(),end,'fixture cleanup did not drain owned generation');time.sleep(.02)
   r=control('RETIRE');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
 def test_fresh_coordinator_stops_installed_generation_preserving_completed_migration(self):
  self.installed();r=self.init('start');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  prior=(self.op/'state.json').read_bytes();platform=self.bytes_now()
  generation=json.loads(r.stdout)['generationId']
  rows=(self.updater/'starts'/generation/'launch.record').read_text().splitlines();domain=Path(rows[2])
  self.assertEqual(rows[3],generation)
  live=self.root/'router';state=live/'opt/var/lib/broray';manifest=rows[4]
  env={**os.environ,'BRORAY_ROOT':str(live/'opt/broray'),'BRORAY_STATE_ROOT':str(state),
   'BRORAY_OPS_CODE_ROOT':str(self.code),'BRORAY_OPS_GUARD':str(self.code/'bin/broray-ops-guard'),
   'BRORAY_OPS_GENERATION':str(self.native),'BRORAY_OPS_ASH':str(live/'opt/bin/ash'),
   'BRORAY_ROUTES_API_LOCK':str(live/'opt/var/lock/broray/global-operation.lock'),
   'BRORAY_OPS_UPDATER_ROOT':str(self.updater),'BRORAY_LEGACY_GLOBAL_LOCK':str(live/'tmp/broray-global-operation.lock'),
   'BRORAY_OPS_RAM_ROOT':str(live/'tmp/broray-operations'),'TEST_MANIFEST':manifest,'TEST_GENERATION':rows[3]}
  script='''
. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"
broray_ops_preflight_admit "$TEST_MANIFEST" || { printf '%s\n' "$BRORAY_OPS_LAST_ERROR"; exit 71; }
broray_ops_preflight_stop_intent "$TEST_MANIFEST" || exit 72
printf 'NEW_OPERATION=%s\n' "$BRORAY_BACKGROUND_OPERATION_ID" >&2
broray_ops_preflight_stop_generation "$TEST_GENERATION" "$TEST_MANIFEST"
'''
  r=subprocess.run([str(live/'opt/bin/ash'),'-c',script],env=env,capture_output=True,text=True,timeout=45)
  print('INSTALLED_GENERATION_STOP '+json.dumps({'returnCode':r.returncode,'stdout':r.stdout,'stderr':r.stderr}),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  reply=json.loads(r.stdout);self.assertEqual(reply['phase'],'STOPPED');self.assertTrue(reply['serviceStopped'])
  terminal=json.loads(sorted(domain.glob('revision-*.json'))[-1].read_bytes())
  self.assertEqual(terminal['state'],'STOPPED');self.assertEqual(terminal['children'],[])
  self.assertEqual(terminal['awaitingBirth'],[]);self.assertEqual(terminal['exitedUnreaped'],[])
  self.assertNotEqual(terminal['stopOperationId'],self.op.name)
  new=state/'operations'/terminal['stopOperationId'];self.assertTrue((new/'state.json').exists())
  self.assertEqual(json.loads((new/'state.json').read_bytes())['platformPreflight']['phase'],'STOPPED')
  self.assertEqual((self.op/'state.json').read_bytes(),prior)
  self.assertEqual(self.bytes_now(),platform);self.assertFalse(self.fetch.exists())

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([InstalledGenerationStop('test_fresh_coordinator_stops_installed_generation_preserving_completed_migration')]))
 raise SystemExit(not r.wasSuccessful())
