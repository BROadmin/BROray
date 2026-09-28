"""Origin-generation status has the same bounded admission as replacement status."""
import fcntl,json,subprocess,time,unittest
import test_installed_service_cycles as support

class OriginStatusContention(support.ServiceCycles):
 def test_status_waits_for_short_coordinator_owner_and_preserves_evidence(self):
  self.installed();ready=self.success('start')
  binding=json.loads((self.op/'platform-bootguard.json').read_bytes())
  args=[str(self.native),'recovery-status',str(self.root/'router'),self.op.name,
        binding['migrationIntentSha256'],binding['stopNonce']]
  state=self.files(self.op);platform=self.bytes_now()
  guard=self.op.parent.parent/'operations.guard';inode=guard.stat().st_ino
  with guard.open('r+b') as held:
   fcntl.flock(held,fcntl.LOCK_EX)
   child=subprocess.Popen(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
   try:
    time.sleep(.5)
    waiting=child.poll() is None
    fcntl.flock(held,fcntl.LOCK_UN)
    out,err=child.communicate(timeout=45)
    self.assertTrue(waiting,'origin status refused short coordinator contention: '+out+err)
    self.assertEqual(child.returncode,0,out+err)
    reply=json.loads(out)
    self.assertTrue(reply['platformReady'])
    self.assertEqual(reply['generationId'],ready['generationId'])
   finally:
    fcntl.flock(held,fcntl.LOCK_UN)
    if child.poll() is None:child.terminate();child.wait(timeout=5)
  self.assertEqual(guard.stat().st_ino,inode)
  self.assertEqual(self.files(self.op),state)
  self.assertEqual(self.bytes_now(),platform)
  self.one_live(ready['generationId'])
  # Start still refuses a competing writer immediately and creates nothing.
  with guard.open('r+b') as held:
   fcntl.flock(held,fcntl.LOCK_EX)
   start_args=list(args);start_args[1]='service-cycle-start'
   result=subprocess.run(start_args,capture_output=True,text=True,timeout=5)
   self.assertEqual(result.returncode,75,result.stdout+result.stderr)
  self.assertEqual(self.files(self.op),state)
  self.assertEqual(self.bytes_now(),platform)
  self.one_live(ready['generationId'])
  # Waiting never authorizes altered coordinator evidence.
  guard.write_bytes(b'corrupt guard evidence')
  try:
   result=subprocess.run(args,capture_output=True,text=True,timeout=5)
   self.assertEqual(result.returncode,75,result.stdout+result.stderr)
   self.assertEqual(guard.read_bytes(),b'corrupt guard evidence')
  finally:guard.write_bytes(b'')
  self.success('stop')

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(
  OriginStatusContention(name) for name in OriginStatusContention.__dict__ if name.startswith('test_')))
 raise SystemExit(not result.wasSuccessful())
