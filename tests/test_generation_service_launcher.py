"""Independent, exact-generation-authenticated app-service launch primitive.

Only isolated init fixtures; this is not acceptance of the real Xray service.
"""
from pathlib import Path
import hashlib,json,os,shlex,signal,subprocess,unittest
from test_updater_generation import Generation,GEN

class ServiceLauncher(Generation):
 def setUp(self):
  super().setUp();self.domain.rmdir();d=self.home/'generations';d.mkdir(mode=0o700);self.domain=d/'domain';self.domain.mkdir(mode=0o700)
  self.hostdir=self.home/'launcher';self.hostdir.mkdir(mode=0o700);self.root=self.home/'router';self.root.mkdir(mode=0o700)
  self.current=self.root/'opt/broray/current';(self.current/'init').mkdir(parents=True)
  (self.current/'.broray-slot').write_text('fixture-3.2.0\n');self.script=self.current/'init/S24broray'
  self.script.write_text('#!/bin/ash\ncase "$1" in\n start) /bin/sleep 60 </dev/null >/dev/null 2>&1 &\n echo $! >'+shlex.quote(str(self.home/'xray.pid'))+'\n echo start >>'+shlex.quote(str(self.home/'launches'))+';;\n status) exit 0;;\n *) exit 2;;\nesac\n');self.script.chmod(0o755)
  self.scriptsha=hashlib.sha256(self.script.read_bytes()).hexdigest();(self.current/'SHA256SUMS').write_text(self.scriptsha+'  init/S24broray\n')
  self.ash=os.path.realpath('/bin/ash');self.ashsha=hashlib.sha256(Path(self.ash).read_bytes()).hexdigest();self.service_identities=[]
 def tearDown(self):
  for pid,ticks in self.service_identities:
   p=Path(f'/proc/{pid}/stat')
   if p.exists() and p.read_text().rsplit(') ',1)[1].split()[19]==ticks:
    os.kill(pid,signal.SIGKILL)
  super().tearDown()
 def hostargs(self):return [GEN,'service-host',str(self.hostdir),str(self.domain),self.gid,self.sha,str(self.root),self.ash,self.ashsha]
 def serviceargs(self,request='service-one',action='start',service='S24broray',sha=None):
  return [GEN,'service',*self.hostargs()[2:],action,service,request,sha or self.scriptsha,'fixture-3.2.0']
 def host(self):
  log=open(self.home/'launcher.log','wb');self.logs.append(log);p=subprocess.Popen(self.hostargs(),stdout=log,stderr=log);self.processes.append(p)
  self.wait(lambda:(self.hostdir/'control').exists() or p.poll() is not None)
  self.assertIsNone(p.poll(),'independent service launcher unavailable: '+(self.home/'launcher.log').read_text());return p
 def command(self,**kw):return shlex.join(self.serviceargs(**kw))
 def launched(self):
  self.wait(lambda:(self.home/'client.rc').exists());self.assertEqual((self.home/'client.rc').read_text().strip(),'0',(self.home/'native.log').read_text())
  pid=int((self.home/'xray.pid').read_text());ticks=Path(f'/proc/{pid}/stat').read_text().rsplit(') ',1)[1].split()[19];self.service_identities.append((pid,ticks));return pid,ticks
 def begin(self,**kw):
  host=self.host();gen=self.start(self.command(**kw)+'\necho $? >"$TEST_HOME/client.rc.tmp"\nmv "$TEST_HOME/client.rc.tmp" "$TEST_HOME/client.rc"\nwhile :; do :; done\n');return host,gen
 def test_app_service_survives_generation_stop(self):
  self.begin();pid,ticks=self.launched();self.assertIn('TracerPid:\t0',Path(f'/proc/{pid}/status').read_text())
  self.assertEqual(self.call('STOP').returncode,0);self.stopped();self.assertTrue(self.live(pid))
  self.assertEqual(Path(f'/proc/{pid}/stat').read_text().rsplit(') ',1)[1].split()[19],ticks)
 def test_app_service_survives_supervisor_crash(self):
  host,gen=self.begin();pid,ticks=self.launched();gen.kill();gen.wait(timeout=3);self.assertTrue(self.live(pid));self.assertIsNone(host.poll())
  self.assertEqual(Path(f'/proc/{pid}/stat').read_text().rsplit(') ',1)[1].split()[19],ticks)
 def test_foreign_caller_cannot_start_service(self):
  self.host();self.start('while :; do :; done\n');self.running();r=subprocess.run(self.serviceargs(),capture_output=True,text=True,timeout=5)
  self.assertNotEqual(r.returncode,0);self.assertFalse((self.home/'xray.pid').exists());self.assertFalse(list(self.hostdir.glob('request-*')))
 def test_wrong_script_hash_has_zero_action(self):
  self.begin(sha='b'*64);self.wait(lambda:(self.home/'client.rc').exists());self.assertNotEqual((self.home/'client.rc').read_text().strip(),'0');self.assertFalse((self.home/'xray.pid').exists())
 def test_unlisted_service_has_zero_action(self):
  self.begin(service='S22broray-updater');self.wait(lambda:(self.home/'client.rc').exists());self.assertNotEqual((self.home/'client.rc').read_text().strip(),'0');self.assertFalse((self.home/'xray.pid').exists())
 def test_completed_request_is_not_executed_twice(self):
  self.host();self.start(self.command()+'\necho $? >"$TEST_HOME/first.rc.tmp"\nmv "$TEST_HOME/first.rc.tmp" "$TEST_HOME/first.rc"\n'+self.command()+'\necho $? >"$TEST_HOME/client.rc.tmp"\nmv "$TEST_HOME/client.rc.tmp" "$TEST_HOME/client.rc"\nwhile :; do :; done\n')
  self.launched();self.assertEqual((self.home/'first.rc').read_text().strip(),'0');self.assertEqual((self.home/'launches').read_text(),'start\n')
 def test_duplicate_launcher_preserves_evidence(self):
  self.host();before={p.name:p.read_bytes() for p in self.hostdir.iterdir() if p.is_file()}
  r=subprocess.run(self.hostargs(),capture_output=True,text=True,timeout=5);self.assertNotEqual(r.returncode,0)
  self.assertEqual({p.name:p.read_bytes() for p in self.hostdir.iterdir() if p.is_file()},before)
 def test_launcher_cannot_start_under_updater_tracer(self):
  self.start(shlex.join(self.hostargs())+'\necho $? >"$TEST_HOME/client.rc.tmp"\nmv "$TEST_HOME/client.rc.tmp" "$TEST_HOME/client.rc"\nwhile :; do :; done\n')
  self.wait(lambda:(self.home/'client.rc').exists());self.assertNotEqual((self.home/'client.rc').read_text().strip(),'0');self.assertEqual(list(self.hostdir.iterdir()),[])
 def test_world_writable_vm_root_refused(self):
  # Only the disposable no-network/no-host-mount VM has these fixed artifacts.
  self.assertTrue(Path('/work/tools/guest_suite_runner.py').is_file());self.assertTrue(Path(GEN).is_file())
  before=os.stat('/').st_mode&0o7777
  try:
   os.chmod('/',0o1777);r=subprocess.run(self.hostargs(),capture_output=True,text=True,timeout=5)
  finally:os.chmod('/',before)
  self.assertNotEqual(r.returncode,0);self.assertIn('LAUNCHER_ROOT_UNCONFIRMED',r.stderr);self.assertEqual(list(self.hostdir.iterdir()),[])

if __name__=='__main__':
 names=[n for n in ServiceLauncher.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(ServiceLauncher(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)
