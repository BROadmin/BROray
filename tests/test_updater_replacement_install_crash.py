"""Kill the test-owned bounded writer at a real rename; resume exact B files."""
import ctypes,fcntl,json,os,signal,subprocess,time,unittest
from test_updater_replacement_install import ReplacementInstall
from test_updater_preflight import TARGETS,UPDATER,digest
from test_native_platform_backup import FILES
from test_generation_birth_crash import Registers,trace,traced_start
from test_generation_migration_crash import trace_string

def installed_reboot_probe(home,root,updater,generation=None,resume=False,boundary='installed'):
 """Cut canonical preflight after exact install and before start-intent exec."""
 import hashlib,shutil
 from pathlib import Path
 sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
 assert boundary in ['installed','start-intent']
 receipt=home/('pending-'+boundary+'-reboot.json');target=home/('pending-'+boundary+'-authenticated-slot')
 env={**os.environ,'BRORAY_HANDOFF_ROOT_PREFIX':str(root),'BRORAY_HANDOFF_ASH':'/bin/ash',
      'BRORAY_OPS_ASH':str(root/'opt/bin/ash'),'BRORAY_OPS_RAM_ROOT':str(home/'ram'),'TEST_ROOT':str(home)}
 if not resume:
  assert not receipt.exists() and not target.exists()
  shutil.copytree(home/'post-boot-authenticated-slot',target)
  for name in ['operation-platform-generation.sh','operation-coordinator.sh','universal-platform-handoff.sh']:
   (target/'app/lib'/name).write_bytes((Path('/work')/('upgrade-'+name)).read_bytes())
  (target/'app/bin/broray-updater-generation').write_bytes(Path('/work/upgrade-native').read_bytes())
  daemon=target/'app/share/updater-platform'/UPDATER
  daemon.write_bytes(daemon.read_bytes()+b'\n# Exact installed-before-reboot fixture.\n')
  inner=target/'app/share/updater-platform/SHA256SUMS'
  inner.write_text(''.join(sha(inner.parent/n)+'  '+n+'\n' for n in TARGETS))
  outer=target/'SHA256SUMS'
  outer.write_text(''.join(sha(p)+'  '+p.relative_to(target).as_posix()+'\n' for p in sorted(target.rglob('*')) if p.is_file() and p!=outer))
 else:
  saved=json.loads(receipt.read_bytes())
  assert saved['bootId']!=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
  inner=target/'app/share/updater-platform/SHA256SUMS';outer=target/'SHA256SUMS'
  assert sha(inner)==saved['targetManifest'] and sha(outer)==saved['slotManifest']
 args=['/bin/ash','/work/implementation/bootstrap/prepare-persistent-updater.sh',str(target),sha(outer),sha(inner)]
 fence=root/'opt/var/lock/broray/global-operation.lock'
 if not resume:
  from test_updater_platform_target_stop import interrupt_owned_stop_client
  native=target/'app/bin/broray-updater-generation'
  command='replacement-start-intent' if boundary=='installed' else 'replacement-start'
  interrupt_owned_stop_client(args,env,native,None,command_prefix=[str(native),command])
  op=fence.readlink().parent;state=(op/'state.json').read_bytes()
  assert json.loads(state)['platformPreflight']['phase']=='STOPPED'
  assert (op/'platform-replacement-install/installed.receipt').is_file()
  if boundary=='installed':assert not (op/'platform-replacement-start').exists()
  else:
   intent=op/'platform-replacement-start/intent.record';assert intent.is_file()
   new_generation=intent.read_text().splitlines()[13]
   assert not (updater/'generations'/new_generation).exists()
   assert not (updater/'hosts'/new_generation).exists()
   assert not (op/'platform-replacement-start/birth.record').exists()
  assert all(sha(root/n)==sha(inner.parent/n) for n in TARGETS)
  rows={p.relative_to(op).as_posix():sha(p) for p in op.rglob('*') if p.is_file()}
  data=dict(boundary=boundary,bootId=Path('/proc/sys/kernel/random/boot_id').read_text().strip(),operation=str(op),
       targetManifest=sha(inner),slotManifest=sha(outer),stateSha256=hashlib.sha256(state).hexdigest(),records=rows)
  with receipt.open('x') as f:json.dump(data,f);f.flush();os.fsync(f.fileno())
  os.sync();print('INSTALLED_REBOOT_PREPARED '+json.dumps(data),flush=True)
 else:
  op=Path(saved['operation']);assert fence.readlink()==op/'fence'
  assert sha(op/'state.json')==saved['stateSha256']
  stopped=json.loads((op/'state.json').read_bytes())['platformPreflight']['generationStop']
  host=updater/'hosts'/stopped['generationId']/'host.record';original=host.read_bytes()
  fields=original.decode().splitlines();native=target/'app/bin/broray-updater-generation'
  proof=[str(native),'service-retired',*fields[1:8],sha(host)]
  good=subprocess.run(proof,capture_output=True,text=True,timeout=15)
  assert good.returncode==0,good.stdout+good.stderr
  # A reboot permits verified historical proof, never arbitrary boot evidence.
  identity=json.loads(fields[8]);wrong=original.replace(identity['bootId'].encode(),b'00000000-0000-0000-0000-000000000000')
  assert wrong!=original
  try:
   host.write_bytes(wrong)
   bad=subprocess.run([*proof[:-1],sha(host)],capture_output=True,text=True,timeout=15)
   assert bad.returncode!=0 and 'LAUNCHER_TERMINAL_UNCONFIRMED' in bad.stderr
   assert host.read_bytes()==wrong and sha(op/'state.json')==saved['stateSha256']
  finally:host.write_bytes(original)
  print('TERMINAL_BOOT_PROOF '+json.dumps(dict(historicalBoot='PASS',wrongBootRefused='PASS',evidencePreserved=True)),flush=True)
  r=subprocess.run(args,env=env,capture_output=True,text=True,timeout=180)
  print('INSTALLED_REBOOT_RESUME '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  assert r.returncode==0,r.stdout+r.stderr
  reply=json.loads(r.stdout);assert reply['phase']=='PREFLIGHT_COMPLETED' and reply['platformReady'] is True
  assert not fence.exists() and not fence.is_symlink()
  assert all(sha(root/n)==sha(inner.parent/n) for n in TARGETS)
  for name,h in saved['records'].items():
   if name=='state.json':continue
   assert sha(op/name)==h,('prior evidence changed',name)
  if boundary=='start-intent':
   birth=op/'platform-replacement-start/birth.record';original_birth=birth.read_bytes()
   fields=original_birth.decode().splitlines()
   assert fields[0]=='BROray-replacement-birth/1' and fields[2]==reply['generationId']
   assert fields[3]==Path('/proc/sys/kernel/random/boot_id').read_text().strip()
   assert fields[3]!=saved['bootId']
   commit=sha(op/'platform-replacement-committed.record');state=sha(op/'state.json')
   domains={p.name:p.stat().st_ino for p in (updater/'generations').iterdir()}
   for kind in ['corrupt','missing','wrong-boot']:
    try:
     if kind=='missing':birth.unlink()
     else:
      birth.write_bytes(b'{broken' if kind=='corrupt' else original_birth.replace(fields[3].encode(),b'00000000-0000-0000-0000-000000000000'))
     bad_bytes=birth.read_bytes() if birth.exists() else None
     bad=subprocess.run(args,env=env,capture_output=True,text=True,timeout=60)
     assert bad.returncode!=0,(kind,bad.stdout,bad.stderr)
     assert (birth.read_bytes() if birth.exists() else None)==bad_bytes
     assert sha(op/'state.json')==state and sha(op/'platform-replacement-committed.record')==commit
     assert {p.name:p.stat().st_ino for p in (updater/'generations').iterdir()}==domains
     print('BIRTH_EVIDENCE_REFUSED '+json.dumps(dict(case=kind,rc=bad.returncode,stdout=bad.stdout)),flush=True)
    finally:
     birth.write_bytes(original_birth);birth.chmod(0o600)
   again=subprocess.run(args,env=env,capture_output=True,text=True,timeout=60)
   assert again.returncode==0,again.stdout+again.stderr
   assert json.loads(again.stdout)['generationId']==reply['generationId']
   assert sha(op/'platform-replacement-committed.record')==commit
   print('BIRTH_REPLAY_PASS sameGeneration=true sameCommit=true',flush=True)
  print('INSTALLED_REBOOT_RESUME_PASS '+json.dumps(dict(operationId=op.name,generationId=reply['generationId'],evidencePreserved=True)),flush=True)
  final=dict(bootId=Path('/proc/sys/kernel/random/boot_id').read_text().strip(),generation=reply['generationId'],
   origin=str(op),originFiles={str(p.relative_to(op)):sha(p) for p in op.rglob('*') if p.is_file()},
   platform={n:sha(root/n) for n in TARGETS},
   inodes={str(p.relative_to(home)):[p.stat().st_dev,p.stat().st_ino] for p in updater.rglob('control')})
  with (home/'final-replacement-boot-receipt.json').open('x') as f:json.dump(final,f);f.flush();os.fsync(f.fileno())
  os.sync()

class ReplacementInstallCrash(ReplacementInstall):
 def test_interrupted_replacement_resumes_without_releasing_fence(self):
  self.test_replacement_backup_binds_both_generations_and_preserves_fence()
  f=self.parent_fixture;fence=self.root/'opt/var/lock/broray/global-operation.lock'
  operation=fence.readlink().parent;state=operation/'state.json';state_bytes=state.read_bytes()
  row=json.loads(state_bytes);stop=row['platformPreflight']['generationStop'];nonce=row['platformPreflight']['stopNonce']
  host=f.updater/'hosts'/stop['generationId']/'host.record';fields=host.read_text().splitlines()
  self.assertEqual(fields[0],'BROray-independent-app-service/1')
  args=[str(self.slot/'app/bin/broray-updater-generation'),'replacement-install',*fields[1:8],
        digest(host),operation.name,nonce,row['platformPreflight']['expectedPlatformManifestSha256'],
        stop['nativeSha256'],digest(state),str(self.next_payload)]
  backup=operation/'platform-replacement-backup';saved={p.name:p.read_bytes() for p in backup.iterdir()}
  # The same native entry must refuse without the inherited canonical guard.
  r=subprocess.run(args,capture_output=True,text=True,timeout=15)
  self.assertNotEqual(r.returncode,0);self.assertFalse((operation/'platform-replacement-install').exists())
  self.assertEqual(set(FILES),set(TARGETS))
  # Transaction entry numbers follow the canonical native allowlist, not the
  # independent textual order of the SHA256SUMS manifest.
  for index,path in enumerate(FILES):
   guard=os.open(self.root/'opt/var/lib/broray/operations.guard',os.O_RDWR|os.O_NOFOLLOW)
   p=None;matched=False;started=time.monotonic()
   try:
    fcntl.flock(guard,fcntl.LOCK_EX|fcntl.LOCK_NB)
    p=subprocess.Popen(args,pass_fds=(guard,),preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    _,status=os.waitpid(p.pid,0);self.assertTrue(os.WIFSTOPPED(status));trace(0x4200,p.pid,0,1|0x10)
    trace(24,p.pid)
    while time.monotonic()-started<120:
     _,status=os.waitpid(p.pid,0)
     if not os.WIFSTOPPED(status):
      p.returncode=os.waitstatus_to_exitcode(status);out,err=p.communicate(timeout=3)
      self.fail('writer exited before rename boundary: '+repr((p.returncode,out,err)))
     sig=os.WSTOPSIG(status)
     if sig==(signal.SIGTRAP|0x80):
      info=(ctypes.c_ubyte*128)();trace(0x420e,p.pid,ctypes.sizeof(info),ctypes.addressof(info))
      regs=Registers();trace(12,p.pid,0,ctypes.addressof(regs))
      if info[0]==1 and regs.orig_rax==316 and trace_string(p.pid,regs.r10)==path.rsplit('/',1)[-1]:
       old=trace_string(p.pid,regs.rsi)
       if old.endswith('-'+str(index)+'.candidate'):
        self.assertEqual(regs.r8,1);matched=True;break
      trace(24,p.pid)
     elif status>>16==4:trace(24,p.pid)
     else:
      self.assertNotEqual(sig,signal.SIGTRAP);trace(24,p.pid,0,sig)
    self.assertTrue(matched,'rename boundary not observed')
    # p.pid is the held, test-created tracee, never a production PID lookup.
    os.kill(p.pid,signal.SIGKILL)
    while True:
     _,status=os.waitpid(p.pid,0)
     if os.WIFSIGNALED(status) or os.WIFEXITED(status):break
     trace(7,p.pid,0,signal.SIGKILL)
    p.returncode=-signal.SIGKILL;out,err=p.communicate(timeout=3)
    evidence=operation/'platform-replacement-install'
    self.assertEqual(len(list(evidence.glob('entry-*.done'))),index)
    self.assertFalse((evidence/'installed.receipt').exists())
    self.assertEqual(state.read_bytes(),state_bytes);self.assertEqual(fence.readlink(),operation/'fence')
    print('REPLACEMENT_CRASH '+json.dumps(dict(boundary='before-file-'+str(index)+'-candidate-rename',
          syscall=316,exit=p.returncode,completedFiles=index,seconds=time.monotonic()-started,
          stdout=out.decode(),stderr=err.decode(),fenceRetained=True)),flush=True)
   finally:
    if p is not None:
     if p.returncode is None:
      os.kill(p.pid,signal.SIGKILL)
      while True:
       _,status=os.waitpid(p.pid,0)
       if os.WIFSIGNALED(status) or os.WIFEXITED(status):break
       trace(7,p.pid,0,signal.SIGKILL)
      p.returncode=-signal.SIGKILL
     p.stdout.close();p.stderr.close()
    os.close(guard)
  # Re-enter through the unchanged native protocol with the actual guard.
  guard=os.open(self.root/'opt/var/lib/broray/operations.guard',os.O_RDWR|os.O_NOFOLLOW)
  try:
   fcntl.flock(guard,fcntl.LOCK_EX|fcntl.LOCK_NB)
   r=subprocess.run(args,pass_fds=(guard,),capture_output=True,text=True,timeout=45)
  finally:os.close(guard)
  print('REPLACEMENT_CRASH_RESUME '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(json.loads(r.stdout)['phase'],'INSTALLED')
  for name in TARGETS:
   self.assertEqual((self.root/name).read_bytes(),(self.next_payload/name).read_bytes())
   self.assertEqual((self.root/name).stat().st_mode&0o777,0o755)
  self.assertEqual(state.read_bytes(),state_bytes);self.assertEqual(fence.readlink(),operation/'fence')
  self.assertEqual(saved,{p.name:p.read_bytes() for p in backup.iterdir()})

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  ReplacementInstallCrash('test_interrupted_replacement_resumes_without_releasing_fence')]))
 raise SystemExit(not result.wasSuccessful())
