"""Fresh service-stop must settle its own protected fence, not reopen migration.

Actual installed daemon, from-birth stop, original independent host retirement.
No fake-proc proof, manual fence deletion or completed-operation rewrite.
"""
import json,os,subprocess,unittest
from test_installed_service_host_retired import InstalledHostRetired

class InstalledStopCompletion(InstalledHostRetired):
 def completion_env(self):
  live=self.root/'router'
  return {**os.environ,'BRORAY_ROOT':str(live/'opt/broray'),'BRORAY_STATE_ROOT':str(live/'opt/var/lib/broray'),
   'BRORAY_OPS_CODE_ROOT':str(self.code),'BRORAY_OPS_GUARD':str(self.code/'bin/broray-ops-guard'),
   'BRORAY_OPS_GENERATION':str(self.native),'BRORAY_OPS_ASH':str(live/'opt/bin/ash'),
   'BRORAY_ROUTES_API_LOCK':str(live/'opt/var/lock/broray/global-operation.lock'),
   'BRORAY_OPS_UPDATER_ROOT':str(self.updater),'BRORAY_LEGACY_GLOBAL_LOCK':str(live/'tmp/broray-global-operation.lock'),
   'BRORAY_OPS_RAM_ROOT':str(live/'tmp/broray-operations')}
 def complete_stop(self,operation,nonce):
  env=self.completion_env();env.update(STOP_OPERATION=operation,STOP_NONCE=nonce)
  return subprocess.run([str(self.root/'router/opt/bin/ash'),'-c',
   '. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"\nbroray_ops_call platform-preflight-stop-complete "$STOP_OPERATION" "$STOP_NONCE"'],
   env=env,capture_output=True,text=True,timeout=30)
 def after_host_retired(self,command,host,domain):
  terminal=json.loads(sorted(domain.glob('revision-*.json'))[-1].read_bytes())
  operation=terminal['stopOperationId'];nonce=terminal['stopNonce']
  op=self.root/'router/opt/var/lib/broray/operations'/operation
  state=json.loads((op/'state.json').read_bytes());self.assertTrue(state['running'])
  lock=self.root/'router/opt/var/lock/broray/global-operation.lock'
  self.assertTrue(lock.is_symlink());self.assertEqual(os.readlink(lock),str(op/'fence'))
  migration=(self.op/'state.json').read_bytes();platform=self.bytes_now()
  history={str(p):p.read_bytes() for folder in (host,domain) for p in folder.iterdir() if p.is_file()}
  r=self.complete_stop(operation,nonce)
  print('INSTALLED_STOP_COMPLETION '+json.dumps({'returnCode':r.returncode,'stdout':r.stdout,'stderr':r.stderr}),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  reply=json.loads(r.stdout);self.assertEqual(reply['phase'],'SERVICE_STOP_COMPLETED')
  self.assertTrue(reply['serviceStopped']);self.assertFalse(reply['platformReady']);self.assertFalse(reply['replayed'])
  self.assertFalse(lock.exists());self.assertFalse(lock.is_symlink());self.assertTrue((op/'retired-lock').is_symlink())
  finished=(op/'state.json').read_bytes();state=json.loads(finished)
  self.assertFalse(state['running']);self.assertEqual(state['state'],'completed')
  self.assertEqual(state['platformPreflight']['phase'],'STOPPED')
  r=self.complete_stop(operation,nonce);self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertTrue(json.loads(r.stdout)['replayed']);self.assertEqual((op/'state.json').read_bytes(),finished)
  self.assertEqual((self.op/'state.json').read_bytes(),migration);self.assertEqual(self.bytes_now(),platform)
  self.assertEqual({str(p):p.read_bytes() for folder in (host,domain) for p in folder.iterdir() if p.is_file()},history)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([InstalledStopCompletion('test_retired_host_proof_after_fresh_operation_stop')]))
 raise SystemExit(not r.wasSuccessful())
