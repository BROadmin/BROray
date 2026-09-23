"""An old daemon's writer escaped before native adoption: exact source, no hooks.
The real /proc ancestry is captured before orphaning. Only private test paths
are written; no router, network or host mounts.
"""
from pathlib import Path
import hashlib,json,os,subprocess,time,unittest
from test_preflight_service_stop import BoundStop

def process_stat(pid):
 fields=Path(f'/proc/{pid}/stat').read_text().rsplit(') ',1)[1].split()
 return {'pid':pid,'state':fields[0],'ppid':int(fields[1]),'startTicks':fields[19]}

class DetachedWriter(BoundStop):
 def test_preexisting_detached_writer_prevents_stopped(self):
  script=self.live.parent/'libexec/broray-updater/broray-updater.sh'
  script.parent.mkdir(parents=True,exist_ok=True)
  writer=self.home/'detached-writer.sh'
  writer.write_text('''#!/bin/ash
printf '%s\\n' "$$" >"$TEST_HOME/writer-pid.tmp"
mv "$TEST_HOME/writer-pid.tmp" "$TEST_HOME/writer-pid"
while [ ! -e "$TEST_HOME/release-writer" ]; do sleep .02; done
printf '\\n# DETACHED_UPDATER_WRITE\\n' >>"$WRITER_TARGET"
: >"$TEST_HOME/writer-finished"
''')
  intermediate=self.home/'intermediate.sh'
  intermediate.write_text('''#!/bin/ash
/bin/ash "$TEST_HOME/detached-writer.sh" </dev/null >/dev/null 2>&1 &
while [ ! -e "$TEST_HOME/release-intermediate" ]; do sleep .02; done
''')
  script.write_text('''#!/bin/ash
trap 'rm -f "$SERVICE_STATE/daemon.pid" "$SERVICE_STATE/daemon.ready"; rmdir "$SERVICE_STATE/daemon.lock"; printf "TERM\\n" >>"$SERVICE_EVENTS"; exit 0' TERM
/bin/ash "$TEST_HOME/intermediate.sh"
: >"$TEST_HOME/daemon-idle"
while :; do sleep 2; done
''');script.chmod(0o755)
  self.env['WRITER_TARGET']=str(script)
  self.updater.mkdir(parents=True,exist_ok=True)
  service=subprocess.Popen(['/bin/ash',str(script),'daemon'],env=self.env,start_new_session=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  self.processes.append(service)
  (self.updater/'daemon.pid').write_text(str(service.pid)+'\n')
  (self.updater/'daemon.ready').write_text(str(service.pid)+'\n');(self.updater/'daemon.lock').mkdir()
  try:
   self.waitfile(self.home/'writer-pid',service,seconds=5)
   pid=int((self.home/'writer-pid').read_text())
   origin=process_stat(pid);middle=process_stat(origin['ppid'])
   self.assertEqual(middle['ppid'],service.pid,'Fixture must prove real updater ancestry')
   (self.home/'release-intermediate').touch()
   self.waitfile(self.home/'daemon-idle',service,seconds=5)
   deadline=time.monotonic()+5
   while process_stat(pid)['ppid']==middle['pid'] and time.monotonic()<deadline:time.sleep(.02)
   detached=process_stat(pid)
   self.assertNotEqual(detached['ppid'],middle['pid'])
   self.assertEqual(detached['startTicks'],origin['startTicks'])
   before=hashlib.sha256(script.read_bytes()).hexdigest()
   result=self.stop()
   phase=self.readstate()['platformPreflight']['phase']
   alive=process_stat(pid)['state'] not in ['Z','X']
   self.assertEqual(hashlib.sha256(script.read_bytes()).hexdigest(),before,'No writer mutation before STOPPED observation')
   (self.home/'release-writer').touch()
   deadline=time.monotonic()+5
   while not (self.home/'writer-finished').exists() and time.monotonic()<deadline:time.sleep(.02)
   after=hashlib.sha256(script.read_bytes()).hexdigest()
   facts={'daemonPid':service.pid,'writerOrigin':origin,'intermediateOrigin':middle,
      'detachedWriter':detached,'writerAliveAfterStop':alive,'stopExit':result.returncode,
      'phase':phase,'fenceRetained':self.lock.is_symlink(),'beforeSha256':before,'afterSha256':after,
      'writerChangedPlatformAfterStop':before!=after,'writerFinished':(self.home/'writer-finished').exists(),
      'remainingSupervisorRegistry':json.loads((self.operation()/'supervisors.json').read_text()) if (self.operation()/'supervisors.json').exists() else None,
      'productionCodeChangedByTest':False,
      'routerAccess':False,'network':False,'hostMounts':False}
   print('DETACHED_WRITER_EVIDENCE '+json.dumps(facts),flush=True)
   self.assertNotEqual(result.returncode,0,'BOUND_STOP_PREEXISTING_WRITER_UNACCOUNTED: STOPPED accepted while a proven former updater descendant can still write platform bytes')
   self.assertNotEqual(phase,'STOPPED')
   self.assertFalse(self.events.exists(),'Legacy refusal must happen before TERM')
  finally:
   (self.home/'release-intermediate').touch();(self.home/'release-writer').touch()

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([DetachedWriter('test_preexisting_detached_writer_prevents_stopped')]))
 raise SystemExit(0 if r.wasSuccessful() else 1)
