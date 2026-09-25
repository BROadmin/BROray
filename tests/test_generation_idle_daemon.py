"""Real updater idle loop must not grow lifetime evidence without requests.

Observation intervals measure resource use, never authorize lock retirement.
The exact production daemon enters through the authenticated native launcher.
Only the isolated offline-VM installation and curl tripwire are test fixtures.
"""
from pathlib import Path
import hashlib,json,os,shutil,subprocess,time,unittest
from test_updater_generation import Generation,GEN

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
UPDATER=ROOT/'runtime/app/share/updater-platform/opt/libexec/broray-updater/broray-updater.sh'

class IdleDaemon(Generation):
 def setUp(self):
  super().setUp();self.domain.rmdir()
  self.root=self.home/'r';self.root.mkdir(mode=0o700)
  self.ram=self.root/'tmp';self.ram.mkdir(mode=0o700);self.ram_mounted=False
  subprocess.run(['mount','-t','tmpfs','-o','mode=1777,size=16m','tmpfs',str(self.ram)],check=True,timeout=5)
  self.ram_mounted=True;self.addCleanup(self.unmount_ram)
  self.updater=self.root/'opt/var/lib/broray-updater';self.updater.mkdir(parents=True,mode=0o700)
  (self.updater/'generations').mkdir(mode=0o700);(self.updater/'starts').mkdir(mode=0o700)
  self.gid='g1';self.domain=self.updater/'generations'/self.gid;self.domain.mkdir(mode=0o700)
  self.startdir=self.updater/'starts'/self.gid;self.startdir.mkdir(mode=0o700)
  self.files=['opt/bin/broray-updaterctl','opt/etc/init.d/S22broray-updater',
   'opt/libexec/broray-updater/broray-compat.sh','opt/libexec/broray-updater/broray-migrate-legacy.sh',
   'opt/libexec/broray-updater/minisign','opt/libexec/broray-updater/broray-updater.sh',
   'opt/libexec/broray-updater/xray-wrapper']
  for rel in self.files:
   p=self.root/rel;p.parent.mkdir(parents=True,exist_ok=True)
   shutil.copyfile(ROOT/'runtime/app/share/updater-platform'/rel,p);p.chmod(0o755)
  digest=lambda b:hashlib.sha256(b).hexdigest()
  self.sha=digest(''.join(digest((self.root/p).read_bytes())+'  '+p+'\n' for p in self.files).encode())
  shell=os.path.realpath('/bin/ash')
  record=('BROray-platform-launch/1\n'+'\n'.join([str(self.root),str(self.domain),self.gid,self.sha,
   digest(Path(GEN).read_bytes()),shell,digest(Path(shell).read_bytes()),'operation-idle','b'*32])+'\n').encode()
  self.launch_sha=digest(record);p=self.startdir/'launch.record';p.write_bytes(record);p.chmod(0o600)
  # Native launch deliberately clears inherited environment. Put the network
  # tripwire in its real PATH inside this mount-free, network-free test VM.
  shim=Path('/opt/bin');created=not shim.exists();shim.mkdir(parents=True,exist_ok=True)
  curl=shim/'curl';self.assertFalse(curl.exists() or curl.is_symlink())
  body=('#!/bin/ash\nprintf UNEXPECTED_CURL >"'+str(self.home/'unexpected-curl')+'"\nexit 98\n').encode()
  curl.write_bytes(body);curl.chmod(0o755)
  def remove_tripwire():
   self.assertEqual(curl.read_bytes(),body);curl.unlink()
   if created:shim.rmdir()
  self.addCleanup(remove_tripwire)
 def unmount_ram(self):
  if self.ram_mounted:
   subprocess.run(['umount',str(self.ram)],check=True,timeout=5);self.ram_mounted=False
 def tearDown(self):
  # Stop only our exact Popen fixtures before releasing their temporary mount.
  for p in reversed(self.processes):
   if p.poll() is None:p.kill()
   p.wait(timeout=3)
  self.unmount_ram();super().tearDown()
 def test_empty_queue_has_no_periodic_children_or_ledger_growth(self):
  data=UPDATER.read_bytes();self.assertTrue(data.endswith(b'main "$@"\n'))
  ready=self.updater/'daemon.ready';log=open(self.home/'native.log','wb');self.logs.append(log)
  p=subprocess.Popen([GEN,'run',str(self.domain),self.gid,self.sha,'--',GEN,'platform-daemon',str(self.startdir),self.launch_sha],stdout=log,stderr=log)
  self.processes.append(p)
  try:self.wait(lambda:ready.exists() and self.state().get('platformReady') is True,seconds=20)
  finally:print('IDLE_DAEMON_START '+json.dumps({'processExit':p.poll(),'latest':self.state(),'routerFiles':sorted(x.relative_to(self.root).as_posix() for x in self.root.rglob('*'))}),flush=True)
  r=self.call();self.assertEqual(r.returncode,0,r.stdout+r.stderr);status=json.loads(r.stdout)
  self.assertTrue(status['platformReady']);self.assertTrue(status['supervisedFromBirth'])
  self.assertEqual(status['platformLaunch']['daemonSha256'],hashlib.sha256(data).hexdigest())
  self.assertEqual(status['platformLaunch']['startIntentSha256'],self.launch_sha)
  time.sleep(.5);before=self.state();files_before=sorted(x.name for x in self.domain.glob('revision-*.json'))
  time.sleep(5);after=self.state();files_after=sorted(x.name for x in self.domain.glob('revision-*.json'))
  print('IDLE_DAEMON_OBSERVATION '+json.dumps({'sourceSha256':hashlib.sha256(data).hexdigest(),'intervalSeconds':5,'beforeRevision':before['revision'],'afterRevision':after['revision'],'beforeFiles':len(files_before),'afterFiles':len(files_after),'updaterBefore':before['updater'],'updaterAfter':after['updater']}),flush=True)
  self.assertIsNone(p.poll());self.assertEqual(after['updater'],before['updater'])
  self.assertFalse((self.home/'unexpected-curl').exists())
  self.assertEqual(after['revision'],before['revision'],'idle daemon created process-lifecycle evidence without a request')
  self.assertEqual(files_after,files_before)
  self.assertEqual(len(self.live_state()['children']),1)
  self.assertEqual(self.call('STOP').returncode,0);self.stopped()
  records=list(self.domain.glob('*.json'));witnesses=list((self.startdir/'ledger-witnesses').glob('*.json'))
  self.assertEqual({p.name for p in records},{p.name for p in witnesses})
  total=sum(p.stat().st_size for p in records+witnesses)
  self.assertLessEqual(total,65536)
  print('PLATFORM_CHECKPOINT_SIZE '+json.dumps(dict(records=len(records),witnesses=len(witnesses),bytes=total)),flush=True)

if __name__=='__main__':
 names=[n for n in IdleDaemon.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(IdleDaemon(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)
