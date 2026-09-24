"""A completed platform replacement must remain controllable through S22."""
import hashlib,json,subprocess,unittest,shutil,time,os
from pathlib import Path
from test_updater_replacement_commit import ReplacementCommit

def fail_ready_generation(root,updater,generation):
 """Reproduce the saved physical watchdog error in a private Linux fixture."""
 domain=updater/'generations'/generation
 anchor=json.loads((domain/'state.json').read_bytes());launch=anchor['platformLaunch']
 native=updater/'runtimes'/launch['nativeSha256']/'runtime'
 args=[str(native),'control',str(domain),'STATUS',generation,anchor['platformManifestSha256'],launch['operationId'],launch['stopNonce']]
 r=subprocess.run(args,capture_output=True,text=True,timeout=10)
 assert r.returncode==0,r.stdout+r.stderr
 ready=json.loads(r.stdout);assert ready['state']=='RUNNING' and ready['platformReady'] is True
 watched=root/'opt/etc/init.d/S22broray-updater';body=watched.read_bytes();mode=watched.stat().st_mode&0o777
 log=updater/'starts'/generation/'supervisor.log';assert 'GENERATION_FIRST_ERROR=' not in log.read_text()
 # Same-mode metadata change produces the real inotify failure. No binary,
 # script contents, process signals or fabricated generation state are used.
 os.chmod(watched,mode)
 deadline=time.monotonic()+15
 while time.monotonic()<deadline and 'GENERATION_FIRST_ERROR=PLATFORM_LAUNCH_BYTES_CHANGED' not in log.read_text():time.sleep(.05)
 assert 'GENERATION_FIRST_ERROR=PLATFORM_LAUNCH_BYTES_CHANGED' in log.read_text()
 assert watched.read_bytes()==body and watched.stat().st_mode&0o777==mode
 def writer_alive(identity):
  try:fields=Path('/proc/'+str(identity['pid'])+'/stat').read_text().rsplit(') ',1)[1].split()
  except (FileNotFoundError,ProcessLookupError):return False
  return fields[0]!='Z' and fields[19]==str(identity['startTicks'])
 while time.monotonic()<deadline and any(writer_alive(ready[k]) for k in ['supervisor','updater']):time.sleep(.05)
 assert not any(writer_alive(ready[k]) for k in ['supervisor','updater'])
 r=subprocess.run(args,capture_output=True,text=True,timeout=10);assert r.returncode!=0,'failed generation claimed live readiness'
 print('FAILED_READY_GENERATION_PROVEN '+json.dumps(dict(generation=generation,error='PLATFORM_LAUNCH_BYTES_CHANGED',
       platformBytesUnchanged=True,supervisorGone=True,updaterGone=True,statusRc=r.returncode)),flush=True)

def verify_unsealed_boot_refusals(updater, origin, generation, command):
 """First post-update boot: corrupt/missing original pins cannot seed cycles."""
 cycle=updater/('cycles-'+origin.name)
 assert not cycle.exists(),'requires the genuine first-boot boundary'
 paths=[origin/'platform-replacement-committed.record',
        origin/'platform-replacement-start'/('ready-'+generation+'.record'),
        updater/'generations'/generation/'state.json']
 for p in paths:
  original=p.read_bytes();mode=p.stat().st_mode&0o777
  try:
   p.write_bytes(b'{corrupt first-boot fixture')
   r=command('start');assert r.returncode!=0,'corrupt historical evidence accepted: '+str(p)
   assert p.read_bytes()==b'{corrupt first-boot fixture'
   assert not cycle.exists(),'failed original proof published a lifecycle'
   p.unlink()
   r=command('start');assert r.returncode!=0,'missing historical evidence accepted: '+str(p)
   assert not p.exists() and not cycle.exists(),'missing original evidence recreated'
  finally:p.write_bytes(original);p.chmod(mode)
 print('UNSEALED_BOOT_NEGATIVES_PASS corrupt=3 missing=3 preserved=true',flush=True)

def verify_boot_ended_origin(updater, generation):
 """Called only by the persistent-disk fixture after a real kernel reboot."""
 domain=updater/'generations'/generation
 anchor=json.loads((domain/'state.json').read_bytes())
 native=updater/'runtimes'/anchor['platformLaunch']['nativeSha256']/'runtime'
 def verify():
  return subprocess.run([str(native),'generation-boot-verify',str(domain)],capture_output=True,text=True,timeout=10)
 r=verify();assert r.returncode==0,r.stdout+r.stderr
 proof=json.loads(r.stdout)
 assert proof['phase']=='BOOT_ENDED_HISTORY_VERIFIED' and proof['signalsAuthorized'] is False
 assert proof['oldBootId']!=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
 origin=updater/('cycles-'+anchor['platformLaunch']['operationId'])
 assert origin.is_dir()
 before=(domain/'boot-ended.receipt').read_bytes()
 duplicate=updater/'cycles-op-duplicate-boot-fixture'
 duplicate.mkdir(mode=0o700)
 try:
  for name in ['origin.record','origin.anchor']:shutil.copyfile(origin/name,duplicate/name);(duplicate/name).chmod(0o600)
  r=verify();assert r.returncode!=0,'duplicate sealed lifecycle accepted'
  assert (domain/'boot-ended.receipt').read_bytes()==before
 finally:
  # Only the newly created offline fixture namespace; no real origin removed.
  for name in ['origin.record','origin.anchor']:(duplicate/name).unlink()
  duplicate.rmdir()
 corrupt=origin/'origin.record';saved=corrupt.read_bytes()
 try:
  corrupt.write_bytes(b'{corrupt boot origin')
  r=verify();assert r.returncode!=0,'corrupt owned lifecycle evidence accepted'
  assert corrupt.read_bytes()==b'{corrupt boot origin'
  assert (domain/'boot-ended.receipt').read_bytes()==before
 finally:corrupt.write_bytes(saved)
 r=verify();assert r.returncode==0,r.stdout+r.stderr
 print('BOOT_ORIGIN_NEGATIVES_PASS duplicate_preserved=true corrupt_preserved=true',flush=True)

class ReplacementLifecycle(ReplacementCommit):
 def test_public_status_uses_retained_replacement_code(self):
  self.test_commit_and_completion_require_exact_readiness_and_preserve_evidence()
  f=self.parent_fixture;op=self.replacement_operation;first=self.replacement_ready['generationId']
  state=(op/'state.json').read_bytes();old=(f.op/'state.json').read_bytes();platform=self.snapshot()
  # Removing the authenticated download/slot is safe only after its exact
  # closure was durably retained. Public init must not fall back to that slot.
  detached=self.slot.with_name(self.slot.name+'-not-available');self.slot.rename(detached)
  try:
   for verb in ['status','start']:
    r=f.init(verb);print('REPLACEMENT_PUBLIC '+json.dumps(dict(verb=verb,rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
    self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(json.loads(r.stdout)['generationId'],first)
   for relative in ['platform-replacement-service.json','platform-replacement-service.anchor',
       'platform-replacement-code/manifest.record','platform-replacement-code/code/lib/operation-coordinator.sh']:
    path=op/relative;body=path.read_bytes();mode=path.stat().st_mode&0o777
    try:
     path.write_bytes(b'{corrupt retained origin')
     r=f.init('status');self.assertNotEqual(r.returncode,0,relative)
     self.assertEqual(path.read_bytes(),b'{corrupt retained origin')
     path.unlink();r=f.init('status');self.assertNotEqual(r.returncode,0,relative)
     self.assertFalse(path.exists(),relative+' must not be recreated')
    finally:path.write_bytes(body);path.chmod(mode)
   r=f.init('status');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
   self.assertEqual(json.loads(r.stdout)['generationId'],first)
   self.assertEqual((op/'state.json').read_bytes(),state);self.assertEqual((f.op/'state.json').read_bytes(),old)
   self.assertEqual(self.snapshot(),platform)
  finally:detached.rename(self.slot)
 def test_replaced_platform_public_lifecycle_preserves_both_origins(self):
  self.test_commit_and_completion_require_exact_readiness_and_preserve_evidence()
  f=self.parent_fixture;op=self.replacement_operation
  # Match the physical Entware utility contract in the retained coordinator's
  # real PATH. Do not alter the coordinator, native proof or service replies.
  sleeper=self.root/'opt/bin/sleep';self.assertFalse(sleeper.exists())
  sleeper.write_text('''#!/bin/ash
case "$1" in ''|*[!0-9]*) echo "sleep: invalid number '$1'" >&2; exit 1;; esac
exec /bin/sleep "$1"
''');sleeper.chmod(0o755)
  self.addCleanup(lambda:sleeper.unlink(missing_ok=True))
  old=(f.op/'state.json').read_bytes();new=(op/'state.json').read_bytes();platform=self.snapshot()
  first=self.replacement_ready['generationId']
  unrelated=subprocess.Popen(['/bin/ash','-c','while :; do sleep 1; done','xray-unrelated-fixture'],
                              stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  def call(verb):
   r=f.init(verb)
   print('REPLACEMENT_PUBLIC '+json.dumps(dict(verb=verb,rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
   self.assertEqual(r.returncode,0,r.stdout+r.stderr)
   self.assertIsNone(unrelated.poll());self.assertEqual((f.op/'state.json').read_bytes(),old)
   self.assertEqual((op/'state.json').read_bytes(),new);self.assertEqual(self.snapshot(),platform)
   return json.loads(r.stdout)
  try:
   self.assertEqual(call('status')['generationId'],first)
   self.assertEqual(call('start')['generationId'],first)
   self.assertTrue(call('stop')['serviceStopped'])
   second=call('start');self.assertTrue(second['platformReady']);self.assertNotEqual(second['generationId'],first)
   self.assertEqual(call('status')['generationId'],second['generationId'])
   third=call('restart');self.assertTrue(third['platformReady'])
   self.assertNotIn(third['generationId'],[first,second['generationId']])
   self.assertTrue(call('stop')['serviceStopped'])
  finally:
   if unrelated.poll() is None:unrelated.terminate()
   unrelated.wait(timeout=5)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  ReplacementLifecycle('test_public_status_uses_retained_replacement_code'),
  ReplacementLifecycle('test_replaced_platform_public_lifecycle_preserves_both_origins')]))
 raise SystemExit(not result.wasSuccessful())
