"""Canonical stop compatibility with the exact base updater supervised from birth.

This exercises the retained pathname observation branch, including platform
mutation refusal without lifetime platform-watch authority. The current daemon
requires canonical pipe launch and is tested by the installed-generation gates;
this frozen compatibility fixture cannot count as current daemon acceptance.
"""
from pathlib import Path
import hashlib,json,os,shutil,subprocess,time,unittest
from test_preflight_admission import Admission,CODE
from test_updater_generation import GEN

class GenerationStop(Admission):
 def setUp(self):
  super().setUp();self.env['BRORAY_OPS_GENERATION']=GEN
  payload=CODE/'share/updater-platform';self.current_sha=hashlib.sha256((payload/'SHA256SUMS').read_bytes()).hexdigest()
  for row in (payload/'SHA256SUMS').read_text().splitlines():
   name=row.split()[1];dst=self.root/name;dst.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(payload/name,dst);dst.chmod(0o755)
  legacy=(Path(__file__).parent/'fixtures/updater-daemon-baseline-3.2.0-r01c01.sh').read_bytes()
  self.assertEqual(hashlib.sha256(legacy).hexdigest(),'00ca7d1cad83f4b4df4ca77e2ed9cd00a43b4aec87614ab10d06543bb79253cd')
  (self.root/'opt/libexec/broray-updater/broray-updater.sh').write_bytes(legacy)
  manifest=''.join(hashlib.sha256((self.root/row.split()[1]).read_bytes()).hexdigest()+'  '+row.split()[1]+'\n' for row in (payload/'SHA256SUMS').read_text().splitlines())
  self.current_sha=hashlib.sha256(manifest.encode()).hexdigest()
  self.env['TEST_SHA']=self.current_sha;self.env['TEST_CURRENT_SHA']=self.current_sha;self.env['TEST_GENERATION']='generation-preflight'
  self.domain=self.updater/'generations/generation-preflight';self.domain.mkdir(parents=True,mode=0o700);self.domain.parent.chmod(0o700);self.updater.chmod(0o700)
  shim=self.home/'bin';shim.mkdir();(shim/'curl').write_text('#!/bin/ash\nexit 98\n');(shim/'curl').chmod(0o755)
  self.generation_log=open(self.home/'generation.log','wb');self.addCleanup(self.generation_log.close)
  self.native=subprocess.Popen([GEN,'run',str(self.domain),'generation-preflight',self.current_sha,'--','/bin/ash',str(self.root/'opt/libexec/broray-updater/broray-updater.sh'),'daemon'],env={**self.env,'BRORAY_UPDATER_ROOT_PREFIX':str(self.root),'BRORAY_UPDATER_PATH':str(shim)+':/usr/bin:/bin:/usr/sbin:/sbin','BRORAY_UPDATER_ASH':'/bin/ash'},stdout=self.generation_log,stderr=self.generation_log,start_new_session=True)
  self.processes.append(self.native)
  end=time.monotonic()+20
  while not (self.updater/'daemon.ready').exists() and time.monotonic()<end:time.sleep(.02)
  self.assertTrue((self.updater/'daemon.ready').exists(),(self.home/'generation.log').read_text());time.sleep(.1)
  self.updater_pid=int((self.updater/'daemon.pid').read_text())
 def latest(self):return json.loads(sorted(self.domain.glob('revision-*.json'))[-1].read_bytes())
 def stop(self,tail='',prefix=''):
  return self.shell(prefix+'\nbroray_ops_preflight_admit "$TEST_SHA" || exit $?\nbroray_ops_preflight_stop_intent "$TEST_SHA" || exit $?\nbroray_ops_preflight_stop_generation "$TEST_GENERATION" "$TEST_CURRENT_SHA" || exit $?\n'+tail,timeout=45)
 def test_actual_daemon_stops_with_generation_proof(self):
  before=self.freeze(self.root/'opt/libexec')
  script=str(self.root/'opt/libexec/broray-updater/broray-updater.sh').encode();matches=[]
  for path in Path('/proc').glob('[0-9]*/cmdline'):
   try:
    argv=path.read_bytes().split(b'\x00')
    if script in argv:matches.append({'pid':int(path.parent.name),'argv':[x.decode() for x in argv if x]})
   except FileNotFoundError:pass
  print('GENERATION_SERVICE_MATCHES '+json.dumps({'matches':matches,'native':self.latest()}),flush=True)
  p=self.stop()
  self.assertEqual(p.returncode,0,p.stdout+p.stderr);reply=json.loads(p.stdout)
  self.assertEqual(reply['phase'],'STOPPED');self.assertTrue(reply['serviceStopped']);self.assertFalse(reply['platformReady'])
  self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOPPED');self.assertTrue(self.lock.is_symlink())
  self.assertEqual(self.latest()['state'],'STOPPED');self.assertEqual(self.latest()['children'],[])
  self.assertEqual(self.freeze(self.root/'opt/libexec'),before)
  print('CANONICAL_GENERATION_STOP '+json.dumps({'reply':reply,'native':self.latest(),'coordinator':self.readstate()}),flush=True)

if __name__=='__main__':
 names=[n for n in GenerationStop.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(GenerationStop(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)
