"""A bound recovery service action must not require queue readiness.

Real protected platform launch, real independent service host, and real ptrace
ownership; only the daemon payload and app init action are fixture programs.
Ready remains false until the pending queue is cleared and READY validates it.
"""
from pathlib import Path
import hashlib,json,os,shlex,subprocess,time,unittest
from test_generation_idle_daemon import IdleDaemon
from test_updater_generation import GEN

def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()

class StartupServiceReadiness(IdleDaemon):
 def fixture(self,bad_script=False):
  (self.updater/'hosts').mkdir(mode=0o700)
  self.hostdir=self.updater/'hosts'/self.gid;self.hostdir.mkdir(mode=0o700)
  self.current=self.root/'opt/broray/current';(self.current/'init').mkdir(parents=True)
  (self.current/'.broray-slot').write_text('recovery-slot\n')
  init=self.current/'init/S24broray'
  init.write_text('#!/bin/ash\n[ "$1" = start ] || exit 2\nsed -n "/^TracerPid:/p" /proc/self/status >'+shlex.quote(str(self.home/'action-tracer'))+'\necho started >'+shlex.quote(str(self.home/'started'))+'\n')
  init.chmod(0o755);self.scriptsha=digest(init)
  (self.current/'SHA256SUMS').write_text(self.scriptsha+'  init/S24broray\n')
  shell=os.path.realpath('/bin/ash');self.shell=shell;self.shellsha=digest(Path(shell))
  native=shlex.quote(GEN);q=shlex.quote
  # Placeholders avoid using environment variables as authority. Launch pins
  # the fixture payload/manifest and its exact independent host record.
  control=f'{native} control {q(str(self.domain))}'
  daemon=self.root/'opt/libexec/broray-updater/broray-updater.sh'
  template='''#!/bin/ash
set -eu
mkdir -m 700 '__UP__/queue' '__UP__/daemon.lock'
printf '%s\n' "$$" >'__UP__/daemon.pid'
: >'__UP__/queue/pending-recovery.json'
'''.replace('__UP__',str(self.updater))
  # The real daemon normally supplies this invocation from service_call().
  # The service client still goes through all native authentication checks.
  tail=f'''rc=0
{native} service {q(str(self.hostdir))} {q(str(self.domain))} "$BRORAY_UPDATER_GENERATION" "$BRORAY_UPDATER_MANIFEST_SHA256" {q(str(self.root))} {q(shell)} {self.shellsha} start S24broray recover-service {'f'*64 if bad_script else self.scriptsha} recovery-slot >{q(str(self.home/'service.reply'))} || rc=$?
echo "$rc" >{q(str(self.home/'service.rc'))}
while [ ! -e {q(str(self.home/'finish'))} ]; do sleep .02; done
rm {q(str(self.updater/'queue/pending-recovery.json'))}
printf '%s\\n' "$$" >{q(str(self.updater/'daemon.ready'))}
{control} READY "$BRORAY_UPDATER_GENERATION" "$BRORAY_UPDATER_MANIFEST_SHA256" "$BRORAY_UPDATER_OPERATION_ID" "$BRORAY_UPDATER_STOP_NONCE"
while :; do sleep 1; done
'''
  daemon.write_text(template+tail);daemon.chmod(0o755)
  manifest=''.join(digest(self.root/p)+'  '+p+'\n' for p in self.files)
  self.sha=hashlib.sha256(manifest.encode()).hexdigest()
  record='BROray-platform-launch/2\n'+'\n'.join([str(self.root),str(self.domain),self.gid,self.sha,digest(Path(GEN)),shell,self.shellsha,'operation-idle','b'*32])+'\n'+manifest
  launch=self.startdir/'launch.record';launch.write_text(record);launch.chmod(0o600);self.launch_sha=digest(launch)
  transaction=self.startdir/'transaction.record';transaction.write_text('isolated-startup-service-recovery-fixture\n');transaction.chmod(0o600)
  hostargs=[GEN,'service-host',str(self.hostdir),str(self.domain),self.gid,self.sha,str(self.root),shell,self.shellsha]
  log=open(self.home/'host.log','wb');self.logs.append(log)
  h=subprocess.Popen(hostargs,stdout=log,stderr=log);self.processes.append(h)
  self.wait(lambda:(self.hostdir/'control').exists() or h.poll() is not None);self.assertIsNone(h.poll(),(self.home/'host.log').read_text())
  hostsha=digest(self.hostdir/'host.record')
  log=open(self.home/'native.log','wb');self.logs.append(log)
  p=subprocess.Popen([GEN,'run',str(self.domain),self.gid,self.sha,'--',GEN,'platform-daemon',str(self.startdir),self.launch_sha,digest(transaction),hostsha],stdout=log,stderr=log);self.processes.append(p)
  def finished():
   f=self.home/'service.rc'
   return f.exists() and f.read_text().strip().isdecimal()
  self.wait(finished,seconds=20)
  return p,hostargs

 def test_recovery_action_before_ready_and_later_queue_ready(self):
  p,hostargs=self.fixture()
  self.assertEqual((self.home/'service.rc').read_text().strip(),'0',(self.home/'host.log').read_text())
  self.assertEqual((self.home/'started').read_text(),'started\n')
  self.assertEqual((self.home/'action-tracer').read_text().strip(),'TracerPid:\t0')
  state=self.live_state();self.assertFalse(state['platformReady']);self.assertTrue(state['supervisedFromBirth'])
  self.assertTrue((self.updater/'queue/pending-recovery.json').exists())
  self.assertFalse((self.updater/'daemon.ready').exists())
  before={f.name:f.read_bytes() for f in self.hostdir.glob('request-*')}
  foreign=subprocess.run([GEN,'service',*hostargs[2:],'start','S24broray','foreign',self.scriptsha,'recovery-slot'],capture_output=True,text=True,timeout=4)
  self.assertNotEqual(foreign.returncode,0);self.assertEqual({f.name:f.read_bytes() for f in self.hostdir.glob('request-*')},before)
  (self.home/'finish').touch()
  self.wait(lambda:self.state().get('platformReady') is True,seconds=10)
  self.assertIsNone(p.poll());self.assertEqual(self.call('STOP').returncode,0);self.stopped()

 def test_wrong_script_still_refused_before_ready(self):
  p,_=self.fixture(bad_script=True)
  self.assertNotEqual((self.home/'service.rc').read_text().strip(),'0')
  self.assertFalse((self.home/'started').exists());self.assertFalse(list(self.hostdir.glob('request-*')))
  self.assertFalse(self.live_state()['platformReady'])
  self.assertEqual(self.call('STOP').returncode,0);self.stopped()

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(StartupServiceReadiness(n) for n in StartupServiceReadiness.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())
