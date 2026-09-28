"""A real ordinary updater fails recovery before READY; export its ended tree."""
import base64,hashlib,json,os,stat,time,unittest,threading
from pathlib import Path
from test_installed_service_cycles import ServiceCycles

class UnreadyBootPrepare(ServiceCycles):
 def observe_wait(self):
  for p in sorted(self.root.rglob('*')):
   if p.is_file() and (p.name in ('supervisor.log','service-host.log','daemon.log','state.json') or p.name.startswith('revision-')):
    if p.name.startswith('revision-') and p!=sorted(p.parent.glob('revision-*.json'))[-1]:continue
    print('UNREADY_WAIT_OBSERVATION '+str(p)+' '+p.read_text(errors='replace')[-2500:],flush=True)
 def test_export_failed_before_ready(self):
  self.installed();observer=threading.Timer(45,self.observe_wait);observer.daemon=True;observer.start()
  try:first=self.success('start')
  finally:observer.cancel()
  self.success('stop')
  origin=self.files(self.op);pointer=self.root/'router/opt/var/lib/broray/last-operation'
  previous=pointer.read_bytes() if pointer.exists() else None
  failure=pointer.parent/'operations/reinstall-unready-fixture'
  self.assertFalse(failure.exists());failure.mkdir(mode=0o700)
  state=failure/'state.json';state.write_text(json.dumps({'running':False,'state':'recovery-required'}));state.chmod(0o600)
  pointer.write_text(failure.name+'\n');pointer.chmod(0o600)
  try:
   r=self.init('start');self.assertNotEqual(r.returncode,0,r.stdout+r.stderr)
   intent=sorted((self.updater/'cycles').glob('cycle-*.record'))[-1]
   gid=intent.read_text().splitlines()[3];self.assertNotEqual(gid,first['generationId'])
   domain=self.updater/'generations'/gid;end=time.monotonic()+5
   while True:
    latest=json.loads(sorted(domain.glob('revision-*.json'))[-1].read_bytes())
    if latest['state']=='STOPPED':break
    self.assertLess(time.monotonic(),end,'failed daemon must drain');time.sleep(.02)
   self.assertFalse(latest['platformReady']);self.assertTrue(latest['supervisedFromBirth'])
   for key in ['children','awaitingBirth','exitedUnreaped']:self.assertEqual(latest[key],[])
   self.assertFalse((self.updater/'cycles'/('ready-'+gid+'.record')).exists())
   before=self.files(domain);r=self.init('start');self.assertNotEqual(r.returncode,0)
   self.assertEqual(self.files(domain),before,'same boot cannot declare ended history')
  finally:
   # The real recovery refusal appends log.txt to this fixture operation.
   # Preserve it in console evidence before removing only our injected data.
   for p in failure.iterdir():
    self.assertIn(p.name,['state.json','log.txt']);self.assertTrue(p.is_file());self.assertFalse(p.is_symlink())
    print('UNREADY_INJECTED_OPERATION '+p.name+' '+p.read_text(),flush=True);p.unlink()
   failure.rmdir()
   if previous is None:pointer.unlink()
   else:pointer.write_bytes(previous)
  self.assertEqual(self.files(self.op),origin)
  rows=[];sockets=[]
  for p in sorted(self.root.rglob('*')):
   rel=p.relative_to(self.root).as_posix();mode=stat.S_IMODE(p.lstat().st_mode)
   if p.is_symlink():
    self.assertEqual(p.name,'retired-lock');target=str(p.parent/'fence');self.assertEqual(os.readlink(p),target)
    rows.append(dict(path=rel,kind='symlink',mode=mode,target=target,targetSha256=hashlib.sha256(target.encode()).hexdigest()))
   elif p.is_dir():rows.append(dict(path=rel,kind='directory',mode=mode))
   elif p.is_file():
    body=p.read_bytes();rows.append(dict(path=rel,kind='file',mode=mode,sha256=hashlib.sha256(body).hexdigest(),base64=base64.b64encode(body).decode()))
   elif stat.S_ISSOCK(p.lstat().st_mode):
    self.assertEqual(p.name,'control')
    if p.parent.name!=gid:
     # The first, explicitly retired fixture also leaves a socket pathname.
     # Its kernel socket is not a persistent file and need not be recreated.
     self.assertEqual(p.parent.name,first['generationId'])
     self.assertTrue((p.parent/'retirement.receipt').is_file())
     continue
    sockets.append(dict(path=rel,mode=mode))
   else:self.fail('non-transferable '+rel)
  value=dict(self.e);value.update(fixture='canonical-boot-context/1',serviceCycleBoot=True,serviceCycleBootActive=True,
   unreadyBoot=True,root=str(self.root),rows=rows,socketPaths=sockets,oldBootId=Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
   stoppedGeneration=gid,completedOriginFiles=origin,legacyOwnerLiveAtExport=False,
   scope='Ordinary supervised updater exited before READY; real new Linux boot required')
  print('UNREADY_EXPORT '+json.dumps(dict(generationId=gid,state=latest['state'],platformReady=False)),flush=True)
  print('MIGRATION_BOOT_EXPORT_BEGIN\n'+base64.b64encode(json.dumps(value,separators=(',',':')).encode()).decode()+'\nMIGRATION_BOOT_EXPORT_END',flush=True)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([UnreadyBootPrepare('test_export_failed_before_ready')]))
 raise SystemExit(not r.wasSuccessful())
