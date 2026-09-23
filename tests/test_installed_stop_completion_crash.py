"""Actual coordinator death after durable terminal state, before fence rename.

Uses the existing isolated-root journal crash contract. The fixture holds real
kernel flocks passed as inherited descriptors; no synthetic STOPPED evidence.
Public re-entry uses the actual native settlement guard after the crash.
"""
import fcntl,json,os,subprocess,unittest
from test_installed_stop_completion import InstalledStopCompletion

class StopCompletionCrash(InstalledStopCompletion):
 def after_host_retired(self,command,host,domain):
  terminal=json.loads(sorted(domain.glob('revision-*.json'))[-1].read_bytes())
  operation=terminal['stopOperationId'];nonce=terminal['stopNonce']
  live=self.root/'router';state_root=live/'opt/var/lib/broray';op=state_root/'operations'/operation
  lock=live/'opt/var/lock/broray/global-operation.lock';platform=self.bytes_now()
  prior=(self.op/'state.json').read_bytes()
  generation_fd=os.open(domain.parent/'.generation-lifetime.lock',os.O_RDWR)
  host_fd=os.open(host,os.O_RDONLY|os.O_DIRECTORY)
  try:
   fcntl.flock(generation_fd,fcntl.LOCK_EX|fcntl.LOCK_NB);fcntl.flock(host_fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
   env=self.completion_env();env.update(BRORAY_SERVICE_STOP_GENERATIONS_FD=str(generation_fd),
    BRORAY_SERVICE_STOP_HOST_FD=str(host_fd),BRORAY_OPS_TEST='1',BRORAY_OPS_TEST_JOURNAL_CRASH='reserved')
   r=subprocess.run([str(self.code/'bin/broray-ops-guard'),str(state_root/'operations.guard'),str(live/'opt/bin/ash'),
    str(self.code/'lib/operation-coordinator.sh'),'platform-preflight-stop-settle',operation,nonce],
    env=env,pass_fds=(generation_fd,host_fd),capture_output=True,text=True,timeout=30)
   self.assertEqual(r.returncode,-9,r.stdout+r.stderr)
   terminal_state=(op/'state.json').read_bytes();self.assertEqual(json.loads(terminal_state)['state'],'completed')
   self.assertFalse(json.loads(terminal_state)['running']);self.assertTrue(lock.is_symlink())
   self.assertFalse((op/'retired-lock').is_symlink())
   journal=(state_root/'operation-events/head.json').read_bytes();self.assertTrue(json.loads(journal)['pending'])
  finally:os.close(host_fd);os.close(generation_fd)
  r=self.complete_stop(operation,nonce);self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  reply=json.loads(r.stdout);self.assertTrue(reply['replayed']);self.assertEqual(reply['phase'],'SERVICE_STOP_COMPLETED')
  self.assertEqual((op/'state.json').read_bytes(),terminal_state);self.assertFalse(lock.is_symlink())
  self.assertTrue((op/'retired-lock').is_symlink());self.assertEqual(self.bytes_now(),platform)
  self.assertEqual((self.op/'state.json').read_bytes(),prior)
  self.assertEqual((state_root/'operation-events/head.json').read_bytes(),journal,'lost journal reservation must remain inspectable')
  print('STOP_COMPLETION_CRASH '+json.dumps({'realCoordinatorKilled':True,'durableTerminalState':True,
   'fenceRetirementResumed':True,'stateReplayExact':True,'journalReservationPreserved':True}),flush=True)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([StopCompletionCrash('test_retired_host_proof_after_fresh_operation_stop')]))
 raise SystemExit(not r.wasSuccessful())
