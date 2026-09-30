"""Committed replacement exits before ordinary init has sealed its origin.

The historical migration fixture supplies A; authenticated exact current code
supplies B. A pidfd bound to the proven daemon requests normal TERM handling.
No ledger is fabricated and no production timeout is changed.
"""
import hashlib,json,os,select,shutil,signal,subprocess,time,unittest
from pathlib import Path
from test_updater_platform_replacement import ReplacementFixture

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()

class UnsealedReplacementRestart(ReplacementFixture):
 def test_stopped_replacement_first_start_creates_live_successor(self):
  self.installed();a=self.init('start')
  self.assertEqual(a.returncode,0,a.stdout+a.stderr)
  target=self.root/'current-authenticated-target';target.mkdir(mode=0o700)
  self.addCleanup(shutil.rmtree,target)
  shutil.copytree(ROOT/'runtime/app/lib',target/'app/lib')
  shutil.copytree(ROOT/'runtime/app/share/updater-platform',target/'app/share/updater-platform')
  for p in (target/'app').rglob('*'):
   if p.is_file():p.chmod(0o755)
  (target/'app/bin').mkdir()
  for name,fixture in [('broray-ops-guard','linux-guard'),('broray-updater-generation','linux-generation')]:
   p=target/'app/bin'/name;shutil.copyfile(Path('/work/.local/bin')/fixture,p);p.chmod(0o755)
  shutil.copyfile(ROOT/'runtime/release.json',target/'release.json')
  manifest=target/'SHA256SUMS'
  manifest.write_text(''.join(sha(p)+'  '+p.relative_to(target).as_posix()+'\n' for p in sorted(target.rglob('*')) if p.is_file()))
  live=self.root/'router'
  env={**os.environ,'BRORAY_HANDOFF_ROOT_PREFIX':str(live),'BRORAY_HANDOFF_ASH':'/bin/ash',
       'BRORAY_OPS_ASH':str(live/'opt/bin/ash'),'BRORAY_OPS_RAM_ROOT':str(self.root/'ram')}
  r=subprocess.run(['/bin/ash',str(ROOT/'bootstrap/prepare-persistent-updater.sh'),str(target),sha(manifest),
      sha(target/'app/share/updater-platform/SHA256SUMS')],env=env,capture_output=True,text=True,timeout=180)
  print('UNSEALED_REPLACEMENT_PREPARE '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  prepared=json.loads(r.stdout);generation=prepared['generationId'];origin=prepared['operationId']
  self.replacement_native=self.updater/'runtimes'/sha(target/'app/bin/broray-updater-generation')/'runtime'
  cycle=self.updater/('cycles-'+origin);self.assertFalse(cycle.exists())
  domain=self.updater/'generations'/generation
  anchor=json.loads((domain/'state.json').read_bytes());launch=anchor['platformLaunch']
  args=[str(self.replacement_native),'control',str(domain),'LIVE',generation,anchor['platformManifestSha256'],origin,launch['stopNonce']]
  r=subprocess.run(args,capture_output=True,text=True,timeout=10)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);ready=json.loads(r.stdout)
  self.assertTrue(ready['platformReady']);self.assertEqual(ready['state'],'RUNNING')
  unrelated=subprocess.Popen(['/bin/ash','-c','while :; do sleep 1; done','xray-unrelated-fixture'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  def stop_foreign():
   if unrelated.poll() is None:unrelated.terminate()
   unrelated.wait(timeout=5)
  self.addCleanup(stop_foreign)
  owner=ready['updater'];proc=Path('/proc')/str(owner['pid']);fd=os.pidfd_open(owner['pid'])
  try:
   self.assertEqual((proc/'stat').read_text().rsplit(') ',1)[1].split()[19],owner['startTicks'])
   self.assertEqual(Path('/proc/sys/kernel/random/boot_id').read_text().strip(),owner['bootId'])
   self.assertEqual(os.readlink(proc/'exe'),owner['executable'])
   self.assertEqual(sha(proc/'cmdline'),owner['commandDigest'])
   signal.pidfd_send_signal(fd,signal.SIGTERM)
   self.assertTrue(select.select([fd],[],[],15)[0],'exact daemon did not finish its normal TERM handler')
  finally:os.close(fd)
  deadline=time.monotonic()+15
  while True:
   r=subprocess.run(args,capture_output=True,text=True,timeout=10)
   self.assertEqual(r.returncode,0,r.stdout+r.stderr);stopped=json.loads(r.stdout)
   if stopped['state']=='STOPPED':break
   self.assertLess(time.monotonic(),deadline,'owned tree did not drain');time.sleep(.02)
  self.assertEqual(stopped['children'],[]);self.assertEqual(stopped['awaitingBirth'],[]);self.assertEqual(stopped['exitedUnreaped'],[])
  self.assertFalse(stopped['platformReady']);self.assertFalse(cycle.exists())
  print('UNSEALED_STOPPED '+json.dumps(stopped),flush=True)
  operations=live/'opt/var/lib/broray/operations'
  preserved={p:p.read_bytes() for p in (operations/origin).rglob('*') if p.is_file()}
  platform=self.bytes_now()
  if getattr(self,'adversarial',False):
   candidates=[operations/origin/'platform-replacement-committed.record',
       operations/origin/'platform-replacement-committed.anchor',
       operations/origin/'platform-replacement-start'/('ready-'+generation+'.record')]
   for p in candidates:
    body=p.read_bytes();mode=p.stat().st_mode&0o777
    try:
     p.write_bytes(b'{corrupt origin')
     refused=self.init('start')
     self.assertNotEqual(refused.returncode,0,'corrupt origin accepted: '+str(p))
     self.assertEqual(p.read_bytes(),b'{corrupt origin');self.assertFalse(cycle.exists())
     p.unlink();refused=self.init('start')
     self.assertNotEqual(refused.returncode,0,'missing origin accepted: '+str(p))
     self.assertFalse(p.exists());self.assertFalse(cycle.exists())
     self.assertIsNone(unrelated.poll());self.assertEqual(self.bytes_now(),platform)
    finally:p.write_bytes(body);p.chmod(mode)
   print('UNSEALED_CORRUPT_MISSING_ORIGIN_PASS cases=6',flush=True)
  r=self.init('start')
  print('UNSEALED_FIRST_START '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  restarted=json.loads(r.stdout);self.assertTrue(restarted['platformReady'])
  self.assertNotEqual(restarted['generationId'],generation)
  self.assertIsNone(unrelated.poll());self.assertEqual(self.bytes_now(),platform)
  for p,body in preserved.items():self.assertEqual(p.read_bytes(),body,str(p))
  repeat=self.init('start');self.assertEqual(repeat.returncode,0,repeat.stdout+repeat.stderr)
  self.assertEqual(json.loads(repeat.stdout)['generationId'],restarted['generationId'])
  status=self.init('status');self.assertEqual(status.returncode,0,status.stdout+status.stderr)
  self.assertEqual(json.loads(status.stdout)['generationId'],restarted['generationId'])
  self.assertIsNone(unrelated.poll())
  if getattr(self,'adversarial',False):
   intent=cycle/('natural-stop-'+generation+'.record');body=intent.read_bytes();mode=intent.stat().st_mode&0o777
   existing={p.name for p in (self.updater/'generations').iterdir()}
   try:
    intent.write_bytes(b'{corrupt natural retirement')
    refused=self.init('start');self.assertNotEqual(refused.returncode,0)
    self.assertEqual(intent.read_bytes(),b'{corrupt natural retirement')
    self.assertEqual(existing,{p.name for p in (self.updater/'generations').iterdir()})
    intent.unlink();refused=self.init('start')
    self.assertNotEqual(refused.returncode,0,'missing durable retirement intent accepted')
    self.assertFalse(intent.exists());self.assertEqual(existing,{p.name for p in (self.updater/'generations').iterdir()})
   finally:intent.write_bytes(body);intent.chmod(mode)
   self.assertIsNone(unrelated.poll());self.assertEqual(self.bytes_now(),platform)
   print('NATURAL_RETIREMENT_CORRUPT_MISSING_PASS cases=2',flush=True)
  if getattr(self,'cycles',False):
   stop=self.init('stop');self.assertEqual(stop.returncode,0,stop.stdout+stop.stderr)
   self.assertTrue(json.loads(stop.stdout)['serviceStopped'])
   start=self.init('start');self.assertEqual(start.returncode,0,start.stdout+start.stderr)
   second=json.loads(start.stdout);self.assertNotEqual(second['generationId'],restarted['generationId'])
   restart=self.init('restart');self.assertEqual(restart.returncode,0,restart.stdout+restart.stderr)
   third=json.loads(restart.stdout);self.assertNotIn(third['generationId'],[generation,restarted['generationId'],second['generationId']])
   self.assertTrue(third['platformReady']);self.assertEqual(self.bytes_now(),platform);self.assertIsNone(unrelated.poll())
   for p,body in preserved.items():self.assertEqual(p.read_bytes(),body,str(p))
   print('NATURAL_EXIT_PUBLIC_CYCLES_PASS distinct=4',flush=True)

 def test_corrupt_and_missing_origin_or_retirement_evidence_refuses(self):
  self.adversarial=True
  self.test_stopped_replacement_first_start_creates_live_successor()

 def test_public_stop_start_restart_after_natural_exit_preserves_origin(self):
  self.cycles=True
  self.test_stopped_replacement_first_start_creates_live_successor()

if __name__=='__main__':unittest.main()
