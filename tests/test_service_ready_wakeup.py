"""The original concurrent-start/Xray case also verifies the READY wake-up boundary."""
import re,unittest
from test_installed_service_cycles import ServiceCycles
class ReadyWakeup(ServiceCycles):
 def init(self,verb):
  result=super().init(verb)
  if verb=='restart' and result.returncode==0:
   matches=re.findall(r'SERVICE_START_WAIT ready=(\d+) elapsed_ms=(\d+) polls=(\d+) poll_ms=(\d+) wakeup=published-ready-revision',result.stderr)
   self.assertEqual(len(matches),1,'restart must wait on the published revision before its full live proof')
   ready,elapsed,polls,poll_ms=map(int,matches[0]);self.assertEqual(ready,1)
   self.assertEqual(polls,1,'the intact READY fixture must not poll full proofs during traced startup')
   self.wakeup_checked=True
   print('READY_WAKEUP_RECEIPT '+str({'ready':ready,'elapsed_ms':elapsed,'authenticatedStatusPolls':polls,'poll_ms':poll_ms,'deadlineUnchanged':10000}),flush=True)
  return result
 def test_parallel_start_restart_does_not_poll_before_ready(self):
  self.wakeup_checked=False
  self.test_parallel_starts_and_foreign_xray_process_are_safe()
  self.assertTrue(self.wakeup_checked)
if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([ReadyWakeup('test_parallel_start_restart_does_not_poll_before_ready')]))
 raise SystemExit(not result.wasSuccessful())
