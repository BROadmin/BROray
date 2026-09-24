"""A new platform operation can stop A without completing B's installation.

This exercises the existing coordinator with a real supervised updater. It is
not acceptance of replacement/install/rollback and never changes platform bytes.
"""
import hashlib,json,os,subprocess,unittest,shutil
import ctypes,signal,time
from pathlib import Path
from test_updater_preflight import PreflightAfterBoot
from test_installed_generation_stop import InstalledGenerationStop

def interrupt_owned_stop_client(argv, env, native, domain, command_prefix=None):
 """Trace only this test-created owner's descendants; cut before STOP exec runs."""
 from test_generation_birth_crash import trace,traced_start
 expected=command_prefix or [str(native),'control',str(domain),'STOP']
 expected=[s.encode() for s in expected]
 p=subprocess.Popen(argv,env=env,preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 pid,status=os.waitpid(p.pid,0);assert pid==p.pid and os.WIFSTOPPED(status)
 tracked={pid};pending=set();matched=False;forks=0;deadline=time.monotonic()+120
 trace(0x4200,pid,0,2|4|8|0x10|0x40|0x100000)
 trace(7,pid)
 try:
  while tracked and time.monotonic()<deadline:
   pid,status=os.waitpid(-1,os.WNOHANG|0x40000000)
   if not pid:time.sleep(.002);continue
   if os.WIFEXITED(status) or os.WIFSIGNALED(status):
    tracked.discard(pid)
    if pid==p.pid:p.returncode=os.waitstatus_to_exitcode(status)
    continue
   assert os.WIFSTOPPED(status)
   if pid not in tracked:pending.add(pid);continue
   event=status>>16;sig=os.WSTOPSIG(status)
   if event in [1,2,3]:
    child=ctypes.c_ulong();trace(0x4201,pid,0,ctypes.addressof(child));tracked.add(child.value);forks+=1
    if child.value in pending:pending.remove(child.value);trace(7,child.value)
   if event==4:
    parts=Path('/proc/'+str(pid)+'/cmdline').read_bytes().rstrip(b'\0').split(b'\0')
    if parts[:len(expected)]==expected:
     matched=True;break
   trace(7,pid,0,0 if sig in [signal.SIGTRAP,signal.SIGSTOP] or event else sig)
  assert matched,'exact native exec boundary not reached: '+repr(expected)
 finally:
  # Every PID is held by ptrace lineage from the Popen above, never discovered
  # by name/age. The persistent updater was started before this traced tree.
  for pid in tracked:os.kill(pid,signal.SIGKILL)
  while tracked:
   pid,status=os.waitpid(-1,0x40000000)
   if os.WIFEXITED(status) or os.WIFSIGNALED(status):
    tracked.discard(pid)
    if pid==p.pid:p.returncode=os.waitstatus_to_exitcode(status)
   elif pid in tracked:trace(7,pid,0,signal.SIGKILL)
  out,err=p.communicate(timeout=3)
 print('STOP_INTENT_CRASH '+json.dumps(dict(boundary='native-exec-before-user-code',command=[s.decode() for s in expected],matched=matched,
       trackedForks=forks,stdout=out.decode(),stderr=err.decode())),flush=True)

def verify_native_only_stop_fence(home, root, updater, generation, crash_before_stop=False):
 """Real process fixture: same seven files, distinct target native runtime."""
 row=json.loads((updater/'generations'/generation/'state.json').read_bytes())
 manifest=row['platformManifestSha256'];old_hash=row['platformLaunch']['nativeSha256']
 old_native=updater/'runtimes'/old_hash/'runtime'
 slot=home/'native-only-interrupted-slot';assert not slot.exists()
 shutil.copytree(home/'post-boot-authenticated-slot',slot)
 for name in ['operation-platform-generation.sh','operation-coordinator.sh','universal-platform-handoff.sh']:
  override=Path('/work/upgrade-'+name)
  if override.is_file():(slot/'app/lib'/name).write_bytes(override.read_bytes())
 native=slot/'app/bin/broray-updater-generation'
 native.write_bytes(native.read_bytes()+b'\nBROray native-only interruption fixture\n')
 target_hash=hashlib.sha256(native.read_bytes()).hexdigest();assert target_hash!=old_hash
 outer=slot/'SHA256SUMS'
 outer.write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+p.relative_to(slot).as_posix()+'\n'
                  for p in sorted(slot.rglob('*')) if p.is_file() and p!=outer))
 code=slot/'app';state=root/'opt/var/lib/broray';fence=root/'opt/var/lock/broray/global-operation.lock'
 env={**os.environ,'BRORAY_ROOT':str(root/'opt/broray'),'BRORAY_STATE_ROOT':str(state),
      'BRORAY_OPS_CODE_ROOT':str(code),'BRORAY_OPS_GUARD':str(code/'bin/broray-ops-guard'),
      'BRORAY_OPS_GENERATION':str(old_native),'BRORAY_OPS_ASH':str(root/'opt/bin/ash'),
      'BRORAY_ROUTES_API_LOCK':str(fence),'BRORAY_OPS_UPDATER_ROOT':str(updater),
      'BRORAY_LEGACY_GLOBAL_LOCK':str(root/'tmp/broray-global-operation.lock'),
      'BRORAY_OPS_RAM_ROOT':str(root/'tmp/broray-operations')}
 # Same owner/API sequence as canonical preflight; interruption occurs after
 # verified STOPPED and before the first replacement-backup call.
 script='''. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"
broray_ops_preflight_admit "$1" || exit 71
broray_ops_preflight_stop_intent "$1" || exit 72
broray_ops_preflight_stop_generation "$2" "$1"
'''
 args=[str(root/'opt/bin/ash'),'-c',script,'native-only',manifest,generation]
 if crash_before_stop:interrupt_owned_stop_client(args,env,old_native,updater/'generations'/generation)
 else:
  r=subprocess.run(args,env=env,capture_output=True,text=True,timeout=90)
  print('NATIVE_ONLY_INTERRUPTED_STOP '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  assert r.returncode==0,r.stdout+r.stderr
  assert json.loads(r.stdout)['phase']=='STOPPED'
 op=fence.readlink().parent;before=(op/'state.json').read_bytes();nonce=json.loads(before)['platformPreflight']['stopNonce']
 assert not (op/'platform-replacement-backup.record').exists()
 if crash_before_stop:
  assert json.loads(before)['platformPreflight']['phase']=='STOP_INTENT'
  r=subprocess.run([str(old_native),'control',str(updater/'generations'/generation),'STATUS',generation,manifest,op.name,nonce],capture_output=True,text=True,timeout=10)
  assert r.returncode==0 and json.loads(r.stdout)['state']=='RUNNING' and json.loads(r.stdout)['stopOperationId']==''
  print('STOP_INTENT_CRASH_LIVE_OLD_PROVEN',flush=True)
 else:
  script='. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"\nbroray_ops_call platform-preflight-stop-complete "$1" "$2"\n'
  r=subprocess.run([str(root/'opt/bin/ash'),'-c',script,'settle',op.name,nonce],env=env,capture_output=True,text=True,timeout=45)
  print('NATIVE_ONLY_INTERRUPTED_SETTLEMENT '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr,targetNativeSha256=target_hash,oldNativeSha256=old_hash)),flush=True)
  assert r.returncode!=0,'NATIVE_ONLY_UPDATE_FENCE_RETIRED_AS_SERVICE_STOP'
  assert 'PLATFORM_TRANSACTION_PENDING' in r.stdout+r.stderr
  assert fence.is_symlink() and fence.readlink()==op/'fence'
  assert (op/'state.json').read_bytes()==before
  print('NATIVE_ONLY_INTERRUPTED_FENCE_PASS',flush=True)
 bootstrap_env={**os.environ,'BRORAY_HANDOFF_ROOT_PREFIX':str(root),'BRORAY_HANDOFF_ASH':'/bin/ash',
      'BRORAY_OPS_ASH':str(root/'opt/bin/ash'),'BRORAY_OPS_RAM_ROOT':str(home/'ram'),'TEST_ROOT':str(home)}
 if crash_before_stop:
  saved_native=native.read_bytes();saved_outer=outer.read_bytes()
  try:
   native.write_bytes(saved_native+b'\nwrong post-crash target\n')
   outer.write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+p.relative_to(slot).as_posix()+'\n'
                     for p in sorted(slot.rglob('*')) if p.is_file() and p!=outer))
   r=subprocess.run(['/bin/ash','/work/implementation/bootstrap/prepare-persistent-updater.sh',str(slot),
       hashlib.sha256(outer.read_bytes()).hexdigest(),manifest],env=bootstrap_env,capture_output=True,text=True,timeout=90)
   print('STOP_INTENT_WRONG_TARGET '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
   assert r.returncode!=0 and 'PLATFORM_REPLACEMENT_REQUEST_UNCONFIRMED' in r.stdout+r.stderr
   assert (op/'state.json').read_bytes()==before and fence.readlink()==op/'fence'
   r=subprocess.run([str(old_native),'control',str(updater/'generations'/generation),'STATUS',generation,manifest,op.name,nonce],capture_output=True,text=True,timeout=10)
   assert r.returncode==0 and json.loads(r.stdout)['state']=='RUNNING' and json.loads(r.stdout)['stopOperationId']==''
  finally:native.write_bytes(saved_native);outer.write_bytes(saved_outer)
 r=subprocess.run(['/bin/ash','/work/implementation/bootstrap/prepare-persistent-updater.sh',str(slot),
      hashlib.sha256(outer.read_bytes()).hexdigest(),manifest],env=bootstrap_env,capture_output=True,text=True,timeout=120)
 print('NATIVE_ONLY_INTERRUPTED_RESUME '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
 assert r.returncode==0,r.stdout+r.stderr
 assert json.loads(r.stdout)['phase']=='PREFLIGHT_COMPLETED'
 assert not fence.exists() and not fence.is_symlink()
 env={**os.environ,'BRORAY_UPDATER_ROOT_PREFIX':str(root)}
 for action in (['status'] if crash_before_stop else ['status','stop','start','status']):
  r=subprocess.run(['/bin/ash',str(root/'opt/etc/init.d/S22broray-updater'),action],env=env,capture_output=True,text=True,timeout=120)
  print('NATIVE_ONLY_RESUMED_LIFECYCLE '+json.dumps(dict(action=action,rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  assert r.returncode==0,r.stdout+r.stderr
 print('NATIVE_ONLY_INTERRUPTED_RESUME_PASS',flush=True)

class ReplacementStop(PreflightAfterBoot):
 fixture_type=InstalledGenerationStop
 def replacement_target_manifest(self):
  return hashlib.sha256(b'distinct target platform B').hexdigest()
 def test_new_manifest_stops_old_generation_and_keeps_install_fence(self):
  first=self.completed(self.bootstrap(self.slot,self.slot_sha));f=self.parent_fixture
  platform=self.snapshot();origin=(f.op/'state.json').read_bytes()
  target=self.replacement_target_manifest()
  self.assertNotEqual(target,self.sha)
  state=self.root/'opt/var/lib/broray'
  env={**os.environ,'BRORAY_ROOT':str(self.root/'opt/broray'),
   # Replacement is admitted by the authenticated target slot. Retained legacy
   # recovery code has no target native binary and cannot pin replacement B.
   'BRORAY_STATE_ROOT':str(state),'BRORAY_OPS_CODE_ROOT':str(self.slot/'app'),
   'BRORAY_OPS_GUARD':str(self.slot/'app/bin/broray-ops-guard'),
   'BRORAY_OPS_GENERATION':str(f.native),'BRORAY_OPS_ASH':str(self.root/'opt/bin/ash'),
   'BRORAY_ROUTES_API_LOCK':str(self.root/'opt/var/lock/broray/global-operation.lock'),
   'BRORAY_OPS_UPDATER_ROOT':str(f.updater),
   'BRORAY_LEGACY_GLOBAL_LOCK':str(self.root/'tmp/broray-global-operation.lock'),
   'BRORAY_OPS_RAM_ROOT':str(self.root/'tmp/broray-operations'),
   'TEST_TARGET':target,'TEST_OLD':self.sha,'TEST_GENERATION':first['generationId']}
  script='''
. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"
printf 'REPLACEMENT_STOP_STAGE admit.begin=%s\n' "$(date +%s)" >&2
broray_ops_preflight_admit "$TEST_TARGET" || exit 71
printf 'REPLACEMENT_STOP_STAGE intent.begin=%s\n' "$(date +%s)" >&2
broray_ops_preflight_stop_intent "$TEST_TARGET" || exit 72
printf 'REPLACEMENT_STOP_STAGE stop.begin=%s\n' "$(date +%s)" >&2
broray_ops_preflight_stop_generation "$TEST_GENERATION" "$TEST_OLD"
rc=$?
printf 'REPLACEMENT_STOP_STAGE stop.end=%s rc=%s\n' "$(date +%s)" "$rc" >&2
exit "$rc"
'''
  try:
   # Three serialized coordinator calls share this owner process. CP28/CP37
   # exhausted the 45s aggregate budget in offline QEMU before this fixture's
   # STOP completed. Keep native/individual API waits unchanged; give the
   # combined fixture a bounded 90s budget and retain per-stage measurements.
   r=subprocess.run([str(self.root/'opt/bin/ash'),'-c',script],env=env,
                    capture_output=True,text=True,timeout=90)
  except subprocess.TimeoutExpired as error:
   print('REPLACEMENT_STOP_TIMEOUT '+repr((error.stdout,error.stderr)),flush=True)
   for path in sorted((state/'operations').glob('op-*/state.json')):
    print('REPLACEMENT_TIMEOUT_STATE '+str(path)+' '+path.read_text(),flush=True)
   raise
  print('REPLACEMENT_TARGET_STOP '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  reply=json.loads(r.stdout);self.assertEqual(reply['phase'],'STOPPED')
  self.assertTrue(reply['serviceStopped']);self.assertFalse(reply['platformReady'])
  domain=f.updater/'generations'/first['generationId']
  ledger=json.loads(sorted(domain.glob('revision-*.json'))[-1].read_bytes())
  self.assertEqual(ledger['state'],'STOPPED')
  for key in ['children','awaitingBirth','exitedUnreaped']:self.assertEqual(ledger[key],[])
  operation=state/'operations'/ledger['stopOperationId']
  self.assertNotEqual(operation,f.op)
  current=json.loads((operation/'state.json').read_bytes())
  self.assertTrue(current['running'])
  self.assertEqual(current['platformPreflight']['expectedPlatformManifestSha256'],target)
  self.assertEqual(current['platformPreflight']['generationStop']['platformManifestSha256'],self.sha)
  fence=self.root/'opt/var/lock/broray/global-operation.lock'
  self.assertTrue(fence.is_symlink());self.assertEqual(fence.readlink(),operation/'fence')
  # A stopped old updater is not permission to finish the new platform update.
  settle='''
. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"
broray_ops_call platform-preflight-stop-complete "$1" "$2"
'''
  r=subprocess.run([str(self.root/'opt/bin/ash'),'-c',settle,'settle',operation.name,ledger['stopNonce']],
                   env=env,capture_output=True,text=True,timeout=15)
  print('REPLACEMENT_STOP_SETTLEMENT '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertNotEqual(r.returncode,0)
  self.assertIn('PLATFORM_TRANSACTION_PENDING',r.stdout+r.stderr)
  self.assertTrue(fence.is_symlink());self.assertEqual(fence.readlink(),operation/'fence')
  self.assertEqual((f.op/'state.json').read_bytes(),origin)
  self.assertEqual(self.snapshot(),platform)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  ReplacementStop('test_new_manifest_stops_old_generation_and_keeps_install_fence')]))
 raise SystemExit(not result.wasSuccessful())
