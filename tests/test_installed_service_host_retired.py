"""Stopped updater's independent app-service host needs exact terminal proof.

The host must retire itself from immutable generation evidence. This command
must observe/validate only: it must not signal a host or manufacture evidence.
"""
import fcntl,hashlib,json,os,subprocess,time,unittest
from pathlib import Path
from test_installed_generation_stop import InstalledGenerationStop

class InstalledHostRetired(InstalledGenerationStop):
 def after_host_retired(self,command,host,domain):pass
 def test_retired_host_proof_after_fresh_operation_stop(self):
  self.test_fresh_coordinator_stops_installed_generation_preserving_completed_migration()
  launches=list((self.updater/'starts').glob('*/launch.record'));self.assertEqual(len(launches),1)
  rows=launches[0].read_text().splitlines();domain=Path(rows[2]);host=self.updater/'hosts'/rows[3]
  terminal=json.loads(sorted(domain.glob('revision-*.json'))[-1].read_bytes())
  self.assertEqual(terminal['state'],'STOPPED');self.assertFalse(terminal['platformReady'])
  expected=terminal['serviceHostRecordSha256'];self.assertRegex(expected,r'^[0-9a-f]{64}$')
  self.assertEqual(hashlib.sha256((host/'host.record').read_bytes()).hexdigest(),expected)
  command=[str(self.native),'service-retired',str(host),str(domain),rows[3],rows[4],rows[1],rows[6],rows[7],expected]
  before={p.name:p.read_bytes() for p in host.iterdir() if p.is_file()}
  r=subprocess.run(command,capture_output=True,text=True,timeout=5)
  self.assertNotEqual(r.returncode,0,'host remains live before explicit generation retirement')
  self.assertEqual({p.name:p.read_bytes() for p in host.iterdir() if p.is_file()},before)
  r=subprocess.run([str(self.native),'control',str(domain),'RETIRE',rows[3],rows[4],terminal['stopOperationId'],terminal['stopNonce']],capture_output=True,text=True,timeout=4)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  end=time.monotonic()+5
  while not (host/'retirement.receipt').exists():
   self.assertLess(time.monotonic(),end,'original host must publish its own terminal receipt');time.sleep(.02)
  held=os.open(host,os.O_RDONLY|os.O_DIRECTORY)
  try:
   while True:
    try:fcntl.flock(held,fcntl.LOCK_EX|fcntl.LOCK_NB);break
    except BlockingIOError:
     self.assertLess(time.monotonic(),end,'fixture waits original publisher exit, not just receipt appearance');time.sleep(.02)
  finally:os.close(held)
  history={p.name:p.read_bytes() for p in host.iterdir() if p.is_file()}
  r=subprocess.run(command,capture_output=True,text=True,timeout=5)
  print('INSTALLED_SERVICE_HOST_RETIRED '+json.dumps({'returnCode':r.returncode,'stdout':r.stdout,'stderr':r.stderr}),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  reply=json.loads(r.stdout);self.assertTrue(reply['ok']);self.assertTrue(reply['hostRetired'])
  self.assertEqual(reply['generationId'],rows[3]);self.assertEqual(reply['hostRecordSha256'],expected)
  self.assertEqual(reply['retirementReceiptSha256'],hashlib.sha256(history['retirement.receipt']).hexdigest())
  self.assertEqual({p.name:p.read_bytes() for p in host.iterdir() if p.is_file()},history)
  self.after_host_retired(command,host,domain)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([InstalledHostRetired('test_retired_host_proof_after_fresh_operation_stop')]))
 raise SystemExit(not r.wasSuccessful())
