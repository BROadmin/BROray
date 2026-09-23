"""A new generation's readiness timeout must not blame retired boot history.

Offline VM only: pause the exact newly published supervisor through a pidfd.
The public start and ownership checks remain intact; the approved wait is 60 seconds.
"""
import hashlib,json,os,re,signal,subprocess,time,unittest
from pathlib import Path
from test_cycle_running_boot_resume import CycleRunningBootResume

class CycleStartDiagnostic(CycleRunningBootResume):
 def test_readiness_timeout_identifies_new_start_not_previous_boot(self):
  origin=self.files(self.op);platform=self.bytes_now()
  r=self.init('status');self.assertNotEqual(r.returncode,0)
  env={**os.environ,'BRORAY_UPDATER_ROOT_PREFIX':str(self.root/'router')}
  script=self.root/'router/opt/etc/init.d/S22broray-updater'
  child=subprocess.Popen(['/bin/ash',str(script),'start'],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
  pidfd=None;owner=None;domain=None;paused=False
  try:
   until=time.monotonic()+120
   while time.monotonic()<until and child.poll() is None:
    for candidate in (self.updater/'generations').iterdir():
     if candidate.name==self.e['stoppedGeneration']:continue
     p=candidate/'state.json'
     if not p.is_file():continue
     state=json.loads(p.read_bytes())
     if state['bootId']!=self.boot:continue
     owner=state['supervisor'];domain=candidate
     self.assertEqual(state['bootId'],self.boot)
     self.assertEqual(state['generationId'],candidate.name)
     pid=int(owner['pid']);proc=Path('/proc')/str(pid)
     pidfd=os.pidfd_open(pid)
     ticks=int((proc/'stat').read_text().rsplit(')',1)[1].split()[19])
     self.assertEqual(ticks,int(owner['startTicks']))
     self.assertEqual(os.readlink(proc/'exe'),owner['executable'])
     self.assertEqual(hashlib.sha256((proc/'exe').read_bytes()).hexdigest(),self.e['nativeSha256'])
     cmd=(proc/'cmdline').read_bytes()
     self.assertEqual(hashlib.sha256(cmd).hexdigest(),owner['commandDigest'])
     self.assertIn(b'\0run\0'+str(candidate).encode()+b'\0'+candidate.name.encode()+b'\0',cmd)
     self.assertEqual(int((proc/'stat').read_text().rsplit(')',1)[1].split()[19]),ticks)
     signal.pidfd_send_signal(pidfd,signal.SIGSTOP)
     paused=True
     break
    if pidfd is not None:break
    time.sleep(.01)
   self.assertIsNotNone(pidfd,'fresh supervisor was not observed before public start completed')
   out,err=child.communicate(timeout=120)
   print('DELAYED_START_RESULT '+json.dumps({'rc':child.returncode,'stdout':out,'stderr':err,'owner':owner,'domain':str(domain),'signalViaPidfd':True}),flush=True)
  finally:
   if pidfd is not None:
    if paused:signal.pidfd_send_signal(pidfd,signal.SIGCONT)
    os.close(pidfd)
  self.assertEqual(child.returncode,75,out+err)
  self.assertIn('SERVICE_START_WAIT ready=0',err)
  self.assertIn('polls=0',err)
  elapsed=int(re.search(r'elapsed_ms=(\d+)',err).group(1))
  self.assertGreaterEqual(elapsed,60000,'approved startup deadline is sixty seconds')
  self.assertLess(elapsed,65000,'startup wait must remain bounded')
  self.assertIn('SERVICE_READY_REJECTED=readiness-not-observed',err)
  self.assertIn('SERVICE_CYCLE_FIRST_ERROR=SERVICE_START_UNCONFIRMED',err)
  self.assertNotIn('SERVICE_BOOT_HISTORY_UNCONFIRMED',err)
  self.assertNotIn('SERVICE_READY_REJECTED=launch-or-boot',err)
  self.assertEqual(self.files(self.op),origin)
  self.assertEqual(self.bytes_now(),platform)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([CycleStartDiagnostic('test_readiness_timeout_identifies_new_start_not_previous_boot')]))
 raise SystemExit(not r.wasSuccessful())
