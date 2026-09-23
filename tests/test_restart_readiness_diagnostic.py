"""Observe readiness rejection; never replace the original success assertion."""
import json,time,unittest
from test_service_restart_pending import RestartPending

class ReadinessDiagnostic(RestartPending):
 def success(self,verb):
  start=time.monotonic()
  try:return super().success(verb)
  except BaseException:
   print('READINESS_FAILURE_ELAPSED '+json.dumps({'verb':verb,'seconds':time.monotonic()-start}),flush=True)
   for delay in [0,1]:
    if delay:time.sleep(delay)
    for p in self.updater.rglob('*'):
     if p.is_file() and p.name in ('supervisor.log','service-host.log','updater.log','daemon.ready','daemon.pid'):
      print('READY_DIAG_FILE '+str(p.relative_to(self.updater))+' '+p.read_text(errors='replace')[-2500:],flush=True)
    for domain in (self.updater/'generations').iterdir():
     if not domain.is_dir():continue
     records=sorted(domain.glob('revision-*.json'))
     if records:print('READY_DIAG_LEDGER '+str(records[-1])+' '+records[-1].read_text(errors='replace')[:11000],flush=True)
   raise

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([ReadinessDiagnostic('test_restart_replays_successor_intent_after_old_stop')]))
 raise SystemExit(not r.wasSuccessful())
