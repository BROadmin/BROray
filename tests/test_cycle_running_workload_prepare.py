"""Generate real post-READY descendants via a rejected queue request, no download."""
import json,os,time,unittest
from pathlib import Path
from test_cycle_running_boot_prepare import CycleRunningBootPrepare

class RunningWorkloadPrepare(CycleRunningBootPrepare):
 def test_export_running_after_rejected_request(self):
  self.installed();first=self.success('start');self.success('start')
  gid=first['generationId'];origin=self.files(self.op);platform=self.bytes_now()
  pinned=(self.updater/'cycles'/('ready-'+gid+'.record')).read_text()
  anchor=json.loads(pinned[pinned.index('{'):])['revision']
  reqid='boot-proof-rejected-package';lock=self.updater/'request.lock'
  self.assertFalse(lock.exists());lock.mkdir(mode=0o700)
  fields={'pid':str(os.getpid()),'owner-start':Path('/proc/self/stat').read_text().rsplit(')',1)[1].split()[19],'operation-id':reqid}
  for name,value in fields.items():p=lock/name;p.write_text(value+'\n');p.chmod(0o600)
  target={'candidateId':'fixture-rejected','releaseId':'fixture-rejected','appVersion':'0','packageVersion':'0','architecture':'intentionally-incompatible',
   'bundle':{},'appSlot':{'layout':'broray-compact-app-slot/1','logicalBytes':1,'fileCount':1,'directoryCount':1,'maxFileBytes':1},
   'sharedRuntime':{'xray':{'path':'/opt/broray/runtime/xray','mode':'preserve-installed','bundled':False}}}
  queue=self.updater/'queue'/(reqid+'.json');tmp=queue.with_suffix('.preparing')
  tmp.write_text(json.dumps({'schemaVersion':1,'operationId':reqid,'operation':'update','target':target}));tmp.chmod(0o600);tmp.rename(queue)
  until=time.monotonic()+60
  while (queue.exists() or lock.exists()) and time.monotonic()<until:time.sleep(.05)
  self.assertFalse(queue.exists(),'real updater did not finish rejected request');self.assertFalse(lock.exists())
  status=json.loads((self.op.parent/reqid/'state.json').read_bytes())
  self.assertEqual(status['state'],'error');self.assertEqual(status['error'],'PACKAGE_TRACK_MISMATCH')
  self.assertFalse(status['mutationStarted']);self.assertFalse(self.fetch.exists())
  self.assertEqual(self.success('start')['generationId'],gid);self.one_live(gid)
  self.assertEqual(self.files(self.op),origin);self.assertEqual(self.bytes_now(),platform)
  latest=json.loads(sorted((self.updater/'generations'/gid).glob('revision-*.json'))[-1].read_bytes())
  self.assertGreater(latest['revision'],anchor);self.assertTrue(latest['platformReady'])
  print('POST_READY_WORKLOAD '+json.dumps({'readyRevision':anchor,'lastRevision':latest['revision'],'error':status['error'],'generation':gid,'platformUnchanged':True}),flush=True)
  self.export_stopped(gid,origin)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([RunningWorkloadPrepare('test_export_running_after_rejected_request')]))
 raise SystemExit(not r.wasSuccessful())
