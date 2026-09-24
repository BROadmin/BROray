"""A replacement needs a distinct durable start and from-birth READY proof."""
import json,os,socket,subprocess,unittest
from test_updater_replacement_install import ReplacementInstall

def verify_retained_platform_retry(home,root,updater,generation,leave_unready=False):
 """Public preflight timeout keeps B; retry continues its original generation."""
 import hashlib,shutil
 from pathlib import Path
 from test_updater_preflight import TARGETS,UPDATER
 sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
 target=home/'slow-ready-authenticated-slot';assert not target.exists()
 shutil.copytree(home/'post-boot-authenticated-slot',target)
 for name in ['operation-platform-generation.sh','operation-coordinator.sh','universal-platform-handoff.sh']:
  (target/'app/lib'/name).write_bytes((Path('/work')/('upgrade-'+name)).read_bytes())
 native=target/'app/bin/broray-updater-generation'
 native.write_bytes(Path('/work/upgrade-native').read_bytes())
 gate=home/'allow-replacement-ready';assert not gate.exists()
 daemon=target/'app/share/updater-platform'/UPDATER
 body=daemon.read_bytes();entry=b'\nmain "$@"\n';assert body.count(entry)==1
 delay=('if [ "${1:-}" = daemon ]; then\n  while [ ! -f "'+str(gate)+'" ]; do sleep 1; done\nfi\n').encode()
 daemon.write_bytes(body.replace(entry,b'\n'+delay+b'main "$@"\n',1))
 payload=target/'app/share/updater-platform';inner=payload/'SHA256SUMS'
 inner.write_text(''.join(sha(payload/n)+'  '+n+'\n' for n in TARGETS))
 outer=target/'SHA256SUMS'
 outer.write_text(''.join(sha(p)+'  '+p.relative_to(target).as_posix()+'\n' for p in sorted(target.rglob('*')) if p.is_file() and p!=outer))
 env={**os.environ,'BRORAY_HANDOFF_ROOT_PREFIX':str(root),'BRORAY_HANDOFF_ASH':'/bin/ash',
      'BRORAY_OPS_ASH':str(root/'opt/bin/ash'),'BRORAY_OPS_RAM_ROOT':str(home/'ram'),'TEST_ROOT':str(home)}
 def run():
  # Includes the deliberately exhausted, unchanged 60-second native readiness
  # wait plus normal stop/install calls. This is a new aggregate fixture budget.
  r=subprocess.run(['/bin/ash','/work/implementation/bootstrap/prepare-persistent-updater.sh',
       str(target),sha(outer),sha(inner)],env=env,capture_output=True,text=True,timeout=180)
  print('RETAINED_PLATFORM_PREFLIGHT '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  return r
 r=run();assert r.returncode==75,r.stdout+r.stderr
 assert 'PLATFORM_REPLACEMENT_START_UNCONFIRMED' in r.stdout+r.stderr
 assert 'SERVICE_START_WAIT ready=0' in r.stderr,'must exercise actual readiness expiry'
 fence=root/'opt/var/lock/broray/global-operation.lock';op=fence.readlink().parent
 state=(op/'state.json').read_bytes();assert json.loads(state)['running'] is True
 intent=op/'platform-replacement-start/intent.record';before=intent.read_bytes()
 next_generation=before.decode().splitlines()[-1];assert next_generation!=generation
 assert not (op/'platform-replacement-committed.record').exists()
 assert not (op/'platform-replacement-rollback').exists()
 assert all(sha(root/n)==sha(payload/n) for n in TARGETS),'new platform was rolled back'
 domains=set(p.name for p in (updater/'generations').iterdir() if p.is_dir())
 assert next_generation in domains
 logs={str(p.relative_to(updater)):p.read_bytes() for p in (updater/'starts'/next_generation).glob('*.log')}
 if leave_unready:
  data=dict(bootId=Path('/proc/sys/kernel/random/boot_id').read_text().strip(),generation=next_generation,
   operation=str(op),target=str(target),slotManifest=sha(outer),platformManifest=sha(inner),stateSha256=sha(op/'state.json'),
   originalIntentSha256=sha(intent),domainFiles={str(p.relative_to(updater)):sha(p) for folder in ['starts','generations','hosts'] for p in (updater/folder/next_generation).rglob('*') if p.is_file()})
  with (home/'unready-reboot.json').open('x') as f:json.dump(data,f);f.flush();os.fsync(f.fileno())
  os.sync();print('UNREADY_REBOOT_PREPARED '+json.dumps(data),flush=True)
  return
 gate.touch()
 r=run();assert r.returncode==0,r.stdout+r.stderr
 reply=json.loads(r.stdout);assert reply['phase']=='PREFLIGHT_COMPLETED' and reply['generationId']==next_generation
 assert not fence.exists() and not fence.is_symlink()
 assert intent.read_bytes()==before and all(sha(root/n)==sha(payload/n) for n in TARGETS)
 assert set(p.name for p in (updater/'generations').iterdir() if p.is_dir())==domains,'retry created a second generation'
 for name,body in logs.items():assert (updater/name).read_bytes().startswith(body),'startup logs lost'
 committed=(op/'platform-replacement-committed.record').read_bytes()
 r=run();assert r.returncode==0 and json.loads(r.stdout)['generationId']==next_generation
 assert (op/'platform-replacement-committed.record').read_bytes()==committed
 print('RETAINED_PLATFORM_RETRY_PASS '+json.dumps(dict(generationId=next_generation,newBytesRetained=True,
       fenceRetainedUntilCommit=True,sameGeneration=True,logsPreserved=True,applicationActivation='NOT_RUN')),flush=True)

def resume_unready_reboot(home,root,updater):
 import hashlib
 from pathlib import Path
 from test_updater_preflight import TARGETS
 sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
 saved=json.loads((home/'unready-reboot.json').read_bytes());op=Path(saved['operation']);target=Path(saved['target'])
 assert saved['bootId']!=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
 assert sha(op/'state.json')==saved['stateSha256']
 assert sha(target/'SHA256SUMS')==saved['slotManifest']
 assert sha(target/'app/share/updater-platform/SHA256SUMS')==saved['platformManifest']
 for name,h in saved['domainFiles'].items():assert sha(updater/name)==h,name
 (home/'allow-replacement-ready').touch()
 env={**os.environ,'BRORAY_HANDOFF_ROOT_PREFIX':str(root),'BRORAY_HANDOFF_ASH':'/bin/ash',
      'BRORAY_OPS_ASH':str(root/'opt/bin/ash'),'BRORAY_OPS_RAM_ROOT':str(home/'ram'),'TEST_ROOT':str(home)}
 # The saved CP100 assertions below remain unchanged. Refusal cases exercise
 # the new boot proof before a successor may be created.
 before_domains={p.name for p in (updater/'generations').iterdir() if p.is_dir()}
 old_domain=updater/'generations'/saved['generation']
 checks=[old_domain/'state.json',op/'platform-replacement-start/birth.record',
         updater/'starts'/saved['generation']/'ledger-witnesses/state.json']
 for file in checks:
  original=file.read_bytes()
  try:
   file.write_bytes(b'{corrupt pending boot evidence')
   refused=subprocess.run(['/bin/ash','/work/implementation/bootstrap/prepare-persistent-updater.sh',str(target),saved['slotManifest'],saved['platformManifest']],
    env=env,capture_output=True,text=True,timeout=180)
   print('UNREADY_REBOOT_CORRUPTION '+json.dumps(dict(file=str(file),rc=refused.returncode,stdout=refused.stdout,stderr=refused.stderr)),flush=True)
   assert refused.returncode!=0 and file.read_bytes()==b'{corrupt pending boot evidence'
   assert sha(op/'state.json')==saved['stateSha256']
   assert {p.name for p in (updater/'generations').iterdir() if p.is_dir()}==before_domains
   assert not (old_domain/'pending-boot-ended.receipt').exists()
  finally:file.write_bytes(original)
 for file in [old_domain/'state.json',sorted(old_domain.glob('revision-*.json'))[-1]]:
  # Removing the last revision simulates rollback while its independent
  # witness remains. Neither a missing origin nor a truncated ledger is new.
  hidden=home/('pending-proof-hidden-'+file.name);assert not hidden.exists();file.rename(hidden)
  try:
   refused=subprocess.run(['/bin/ash','/work/implementation/bootstrap/prepare-persistent-updater.sh',str(target),saved['slotManifest'],saved['platformManifest']],
    env=env,capture_output=True,text=True,timeout=180)
   print('UNREADY_REBOOT_MISSING '+json.dumps(dict(file=str(file),rc=refused.returncode,stdout=refused.stdout,stderr=refused.stderr)),flush=True)
   assert refused.returncode!=0 and not file.exists()
   assert {p.name for p in (updater/'generations').iterdir() if p.is_dir()}==before_domains
   assert not (old_domain/'pending-boot-ended.receipt').exists()
  finally:hidden.rename(file)
 r=subprocess.run(['/bin/ash','/work/implementation/bootstrap/prepare-persistent-updater.sh',str(target),saved['slotManifest'],saved['platformManifest']],
  env=env,capture_output=True,text=True,timeout=180)
 print('UNREADY_REBOOT_RESUME '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
 for name,h in saved['domainFiles'].items():assert sha(updater/name)==h,('old evidence changed',name)
 assert r.returncode==0,r.stdout+r.stderr
 reply=json.loads(r.stdout);assert reply['phase']=='PREFLIGHT_COMPLETED' and reply['platformReady'] is True
 assert reply['generationId']!=saved['generation'],'a previous boot generation must not be reused'
 fence=root/'opt/var/lock/broray/global-operation.lock';assert not fence.exists() and not fence.is_symlink()
 assert all(sha(root/n)==sha(target/'app/share/updater-platform'/n) for n in TARGETS)
 assert sha(op/'platform-replacement-start/intent.record')==saved['originalIntentSha256']
 commit=(op/'platform-replacement-committed.record').read_bytes()
 replay=subprocess.run(['/bin/ash','/work/implementation/bootstrap/prepare-persistent-updater.sh',str(target),saved['slotManifest'],saved['platformManifest']],
  env=env,capture_output=True,text=True,timeout=180)
 print('UNREADY_REBOOT_REPLAY '+json.dumps(dict(rc=replay.returncode,stdout=replay.stdout,stderr=replay.stderr)),flush=True)
 assert replay.returncode==0 and json.loads(replay.stdout)['generationId']==reply['generationId']
 assert (op/'platform-replacement-committed.record').read_bytes()==commit
 assert {p.name for p in (updater/'generations').iterdir() if p.is_dir()}==before_domains|{reply['generationId']}
 final=dict(bootId=Path('/proc/sys/kernel/random/boot_id').read_text().strip(),generation=reply['generationId'],origin=str(op),
   originFiles={str(p.relative_to(op)):sha(p) for p in op.rglob('*') if p.is_file()},
   platform={name:sha(root/name) for name in TARGETS},
   inodes={str(p.relative_to(home)):[p.stat().st_dev,p.stat().st_ino] for p in updater.rglob('control')})
 with (home/'final-replacement-boot-receipt.json').open('x') as f:json.dump(final,f);f.flush();os.fsync(f.fileno())
 os.sync()
 print('UNREADY_REBOOT_RESUME_PASS',flush=True)

def retry_publication_reboot(home,root,updater,resume=False):
 """Real syscall cut after durable retry intent, before any new launch domain."""
 import ctypes,fcntl,hashlib,signal,time
 from pathlib import Path
 from test_generation_birth_crash import Registers,trace,traced_start
 from test_generation_migration_crash import trace_string
 from test_updater_preflight import TARGETS
 sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
 saved=json.loads((home/'unready-reboot.json').read_bytes());op=Path(saved['operation']);target=Path(saved['target'])
 boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
 assert saved['bootId']!=boot and sha(target/'SHA256SUMS')==saved['slotManifest']
 assert sha(op/'state.json')==saved['stateSha256']
 gate=home/'allow-replacement-ready';gate.touch()
 receipt=home/'retry-publication-reboot.json'
 env={**os.environ,'BRORAY_HANDOFF_ROOT_PREFIX':str(root),'BRORAY_HANDOFF_ASH':'/bin/ash',
      'BRORAY_OPS_ASH':str(root/'opt/bin/ash'),'BRORAY_OPS_RAM_ROOT':str(home/'ram'),'TEST_ROOT':str(home)}
 if not resume:
  assert not receipt.exists()
  native=target/'app/bin/broray-updater-generation';state=json.loads((op/'state.json').read_bytes())
  guard=os.open(root/'opt/var/lib/broray/operations.guard',os.O_RDWR|os.O_NOFOLLOW)
  p=None;matched=False
  try:
   fcntl.flock(guard,fcntl.LOCK_EX|fcntl.LOCK_NB)
   args=[str(native),'replacement-start',str(root),op.name,state['platformPreflight']['stopNonce'],saved['platformManifest'],sha(op/'state.json')]
   p=subprocess.Popen(args,pass_fds=(guard,),preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
   pid,status=os.waitpid(p.pid,0);assert pid==p.pid and os.WIFSTOPPED(status)
   trace(0x4200,p.pid,0,1|0x10|0x100000);trace(24,p.pid)
   deadline=time.monotonic()+60;stops=0;last_syscall=None
   while time.monotonic()<deadline:
    # One held native tracee, before any fork: wait for its next ptrace stop
    # directly instead of adding a polling sleep to every syscall boundary.
    pid,status=os.waitpid(p.pid,0);stops+=1
    assert os.WIFSTOPPED(status),('native exited before retry publication cut',status)
    sig=os.WSTOPSIG(status)
    if sig==signal.SIGTRAP|0x80:
     info=(ctypes.c_ubyte*128)();trace(0x420e,p.pid,128,ctypes.addressof(info))
     regs=Registers();trace(12,p.pid,0,ctypes.addressof(regs));last_syscall=int(regs.orig_rax)
     if info[0]==1 and regs.orig_rax==258:
      name=trace_string(p.pid,regs.rsi)
      parent=Path('/proc/'+str(p.pid)+'/fd/'+str(regs.rdi)).readlink()
      if str(parent)==str(updater/'starts') and name.startswith('g-'):
       records=list((op/'platform-replacement-start').glob('attempt-*.record'));assert len(records)==1
       assert records[0].read_text().splitlines()[-1]==name
       new_id=name;matched=True;break
     trace(24,p.pid)
    elif status>>16==4:trace(24,p.pid)
    else:assert sig!=signal.SIGTRAP;trace(24,p.pid,0,sig)
   print('RETRY_TRACE_PROGRESS '+json.dumps(dict(stops=stops,lastSyscall=last_syscall,matched=matched)),flush=True)
   assert matched,'durable retry-before-start-directory boundary not reached'
  finally:
   if p is not None:
    if p.returncode is None:
     os.kill(p.pid,signal.SIGKILL)
     while True:
      _,status=os.waitpid(p.pid,0)
      if os.WIFEXITED(status) or os.WIFSIGNALED(status):break
      trace(7,p.pid,0,signal.SIGKILL)
     p.returncode=-signal.SIGKILL
    out,err=p.communicate(timeout=3)
    print('RETRY_PUBLICATION_CUT '+json.dumps(dict(matched=matched,stdout=out.decode(),stderr=err.decode())),flush=True)
   os.close(guard)
  assert not (updater/'starts'/new_id).exists() and not (updater/'hosts'/new_id).exists() and not (updater/'generations'/new_id).exists()
  data=dict(bootId=boot,generation=new_id,records={str(p.relative_to(op)):sha(p) for p in op.rglob('*') if p.is_file()})
  with receipt.open('x') as f:json.dump(data,f);f.flush();os.fsync(f.fileno())
  os.sync();print('RETRY_PUBLICATION_PREPARED '+json.dumps(data),flush=True)
  return
 before=json.loads(receipt.read_bytes());assert before['bootId']!=boot
 for name,h in before['records'].items():assert sha(op/name)==h,name
 r=subprocess.run(['/bin/ash','/work/implementation/bootstrap/prepare-persistent-updater.sh',str(target),saved['slotManifest'],saved['platformManifest']],env=env,capture_output=True,text=True,timeout=180)
 print('RETRY_PUBLICATION_RESUME '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
 assert r.returncode==0,r.stdout+r.stderr
 reply=json.loads(r.stdout);assert reply['phase']=='PREFLIGHT_COMPLETED' and reply['platformReady'] is True
 assert reply['generationId']==before['generation'],'unexecuted retry must keep its prepared generation ID'
 attempt=op/'platform-replacement-start/attempt-00000000000000000001.record'
 assert sha(attempt)==before['records'][str(attempt.relative_to(op))],'retry intent must not be rewritten'
 birth=op/('platform-replacement-start/birth-'+reply['generationId']+'.record')
 anchor=birth.with_suffix('.anchor')
 rows=birth.read_text().splitlines()
 assert rows[0]=='BROray-replacement-retry-birth/1' and rows[-1]==boot and rows[-2]==sha(attempt)
 for name,h in saved['domainFiles'].items():assert sha(updater/name)==h,name
 assert sha(op/'platform-replacement-start/intent.record')==saved['originalIntentSha256']
 assert all(sha(root/name)==sha(target/'app/share/updater-platform'/name) for name in TARGETS)
 assert not (root/'opt/var/lock/broray/global-operation.lock').is_symlink()
 # Once a process has been born, neither corruption nor loss of its birth
 # evidence may be treated as a new preparation or modify the live generation.
 domains={p.name for p in (updater/'generations').iterdir() if p.is_dir()}
 command=['/bin/ash','/work/implementation/bootstrap/prepare-persistent-updater.sh',str(target),saved['slotManifest'],saved['platformManifest']]
 for file in [birth,anchor]:
  original=file.read_bytes()
  try:
   file.write_bytes(b'{corrupt retry birth evidence')
   refused=subprocess.run(command,env=env,capture_output=True,text=True,timeout=180)
   print('RETRY_BIRTH_CORRUPTION '+json.dumps(dict(file=str(file),rc=refused.returncode,stdout=refused.stdout,stderr=refused.stderr)),flush=True)
   assert refused.returncode!=0 and file.read_bytes()==b'{corrupt retry birth evidence'
   assert {p.name for p in (updater/'generations').iterdir() if p.is_dir()}==domains
  finally:file.write_bytes(original)
 hidden=home/'held-retry-birth.record';assert not hidden.exists();birth.rename(hidden)
 try:
  refused=subprocess.run(command,env=env,capture_output=True,text=True,timeout=180)
  print('RETRY_BIRTH_MISSING '+json.dumps(dict(rc=refused.returncode,stdout=refused.stdout,stderr=refused.stderr)),flush=True)
  assert refused.returncode!=0 and not birth.exists()
 finally:hidden.rename(birth)
 commit=(op/'platform-replacement-committed.record').read_bytes()
 replay=subprocess.run(command,env=env,capture_output=True,text=True,timeout=180)
 assert replay.returncode==0 and json.loads(replay.stdout)['generationId']==reply['generationId'],replay.stdout+replay.stderr
 assert (op/'platform-replacement-committed.record').read_bytes()==commit
 assert {p.name for p in (updater/'generations').iterdir() if p.is_dir()}==domains
 final=dict(bootId=boot,generation=reply['generationId'],origin=str(op),
   originFiles={str(p.relative_to(op)):sha(p) for p in op.rglob('*') if p.is_file()},
   platform={name:sha(root/name) for name in TARGETS},
   inodes={str(p.relative_to(home)):[p.stat().st_dev,p.stat().st_ino] for p in updater.rglob('control')})
 with (home/'final-replacement-boot-receipt.json').open('x') as f:json.dump(final,f);f.flush();os.fsync(f.fileno())
 os.sync()
 print('RETRY_PUBLICATION_REBOOT_PASS',flush=True)

def retry_birth_boundary(home,root,updater,boundary,resume=False):
 """An interrupted birth publication retains exact evidence and its fence."""
 import ctypes,fcntl,hashlib,signal,time
 from pathlib import Path
 from test_generation_birth_crash import Registers,trace,traced_start
 from test_generation_migration_crash import trace_string
 from test_updater_preflight import TARGETS
 assert boundary in ['partial-birth','before-host']
 sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
 saved=json.loads((home/'unready-reboot.json').read_bytes());op=Path(saved['operation']);target=Path(saved['target'])
 previous=json.loads((home/'retry-publication-reboot.json').read_bytes());gen=previous['generation']
 boot=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
 receipt=home/('retry-'+boundary+'.json');start=op/'platform-replacement-start'
 fence=root/'opt/var/lock/broray/global-operation.lock'
 assert sha(target/'SHA256SUMS')==saved['slotManifest']
 env={**os.environ,'BRORAY_HANDOFF_ROOT_PREFIX':str(root),'BRORAY_HANDOFF_ASH':'/bin/ash',
      'BRORAY_OPS_ASH':str(root/'opt/bin/ash'),'BRORAY_OPS_RAM_ROOT':str(home/'ram'),'TEST_ROOT':str(home)}
 if not resume:
  assert not receipt.exists() and not (updater/'starts'/gen).exists()
  state=json.loads((op/'state.json').read_bytes());p=None;matched=False
  guard=os.open(root/'opt/var/lib/broray/operations.guard',os.O_RDWR|os.O_NOFOLLOW)
  try:
   fcntl.flock(guard,fcntl.LOCK_EX|fcntl.LOCK_NB)
   args=[str(target/'app/bin/broray-updater-generation'),'replacement-start',str(root),op.name,
         state['platformPreflight']['stopNonce'],saved['platformManifest'],sha(op/'state.json')]
   p=subprocess.Popen(args,pass_fds=(guard,),preexec_fn=traced_start,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
   _,status=os.waitpid(p.pid,0);assert os.WIFSTOPPED(status)
   trace(0x4200,p.pid,0,1|0x10|0x100000);trace(24,p.pid)
   deadline=time.monotonic()+60
   while time.monotonic()<deadline:
    _,status=os.waitpid(p.pid,0);assert os.WIFSTOPPED(status),status
    sig=os.WSTOPSIG(status)
    if sig==(signal.SIGTRAP|0x80):
     info=(ctypes.c_ubyte*128)();trace(0x420e,p.pid,128,ctypes.addressof(info))
     regs=Registers();trace(12,p.pid,0,ctypes.addressof(regs))
     if info[0]==1 and regs.orig_rax==(257 if boundary=='partial-birth' else 258):
      name=trace_string(p.pid,regs.rsi)
      parent=Path('/proc/'+str(p.pid)+'/fd/'+str(regs.rdi)).readlink()
      if boundary=='partial-birth':
       matched=name=='birth-'+gen+'.record.pending' and parent==start and bool(regs.rdx&os.O_CREAT)
      else:matched=name==gen and parent==updater/'hosts'
      if matched:break
     trace(24,p.pid)
    elif status>>16==4:trace(24,p.pid)
    else:assert sig!=signal.SIGTRAP;trace(24,p.pid,0,sig)
   assert matched,('birth publication boundary not reached',boundary)
  finally:
   if p is not None:
    os.kill(p.pid,signal.SIGKILL)
    while True:
     _,status=os.waitpid(p.pid,0)
     if os.WIFEXITED(status) or os.WIFSIGNALED(status):break
     trace(7,p.pid,0,signal.SIGKILL)
    p.returncode=-signal.SIGKILL;out,err=p.communicate(timeout=3)
    print('RETRY_BIRTH_CUT '+json.dumps(dict(boundary=boundary,matched=matched,stdout=out.decode(),stderr=err.decode())),flush=True)
   os.close(guard)
  assert not (updater/'hosts'/gen).exists() and not (updater/'generations'/gen).exists()
  assert (start/('birth-'+gen+'.anchor')).is_file()
  assert (start/('birth-'+gen+'.record')).exists()==(boundary=='before-host')
  saved=dict(bootId=boot,operation={str(p.relative_to(op)):sha(p) for p in op.rglob('*') if p.is_file()},
             platform={n:sha(root/n) for n in TARGETS},launch={p.name:sha(p) for p in (updater/'starts'/gen).iterdir() if p.is_file()})
  with receipt.open('x') as f:json.dump(saved,f);f.flush();os.fsync(f.fileno())
  os.sync();return
 before=json.loads(receipt.read_bytes());assert before['bootId']!=boot
 r=subprocess.run(['/bin/ash','/work/implementation/bootstrap/prepare-persistent-updater.sh',str(target),saved['slotManifest'],saved['platformManifest']],env=env,capture_output=True,text=True,timeout=180)
 print('RETRY_BIRTH_BOUNDARY_REFUSAL '+json.dumps(dict(boundary=boundary,rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
 assert r.returncode!=0 and 'PLATFORM_REPLACEMENT_START_UNCONFIRMED' in r.stdout+r.stderr
 assert fence.readlink()==op/'fence' and json.loads((op/'state.json').read_bytes())['running'] is True
 assert {str(p.relative_to(op)):sha(p) for p in op.rglob('*') if p.is_file()}==before['operation']
 assert {n:sha(root/n) for n in TARGETS}==before['platform']
 assert {p.name:sha(p) for p in (updater/'starts'/gen).iterdir() if p.is_file()}==before['launch']
 assert not (updater/'hosts'/gen).exists() and not (updater/'generations'/gen).exists()
 print('RETRY_BIRTH_BOUNDARY_FAIL_CLOSED_PASS '+boundary,flush=True)

class ReplacementStart(ReplacementInstall):
 def test_replacement_starts_new_generation_with_bound_readiness(self):
  self.test_exact_platform_replacement_replay_and_foreign_change_refusal()
  f=self.parent_fixture;fence=self.root/'opt/var/lock/broray/global-operation.lock'
  op=fence.readlink().parent;state=(op/'state.json').read_bytes();row=json.loads(state)
  nonce=row['platformPreflight']['stopNonce'];old=row['platformPreflight']['generationStop']['generationId']
  origin=(f.op/'state.json').read_bytes()
  env={**os.environ,'BRORAY_ROOT':str(self.root/'opt/broray'),
   'BRORAY_STATE_ROOT':str(self.root/'opt/var/lib/broray'),
   'BRORAY_OPS_CODE_ROOT':str(self.slot/'app'),'BRORAY_OPS_GUARD':str(self.slot/'app/bin/broray-ops-guard'),
   'BRORAY_OPS_GENERATION':str(f.native),'BRORAY_OPS_ASH':str(self.root/'opt/bin/ash'),
   'BRORAY_ROUTES_API_LOCK':str(fence),'BRORAY_OPS_UPDATER_ROOT':str(f.updater),
   'BRORAY_LEGACY_GLOBAL_LOCK':str(self.root/'tmp/broray-global-operation.lock'),
   'BRORAY_OPS_RAM_ROOT':str(self.root/'tmp/broray-operations')}
  script='. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"\nbroray_ops_call "$1" "$2" "$3"\n'
  def run(command):
   try:
    return subprocess.run([str(self.root/'opt/bin/ash'),'-c',script,'start',command,op.name,nonce],
                          env=env,capture_output=True,text=True,timeout=120)
   except subprocess.TimeoutExpired as error:
    print('REPLACEMENT_START_TIMEOUT '+repr((error.stdout,error.stderr)),flush=True)
    for folder in ['starts','generations','hosts']:
     for path in sorted((f.updater/folder).glob('*/*')):
      if path.is_file() and (path.suffix in {'.log','.json','.record'} or path.name=='failed'):
       print('REPLACEMENT_START_EVIDENCE '+str(path)+' '+path.read_text(errors='replace')[-16000:],flush=True)
    for proc in sorted(__import__('pathlib').Path('/proc').glob('[0-9]*')):
     try:
      cmd=(proc/'cmdline').read_bytes().replace(b'\0',b' ').decode(errors='replace')
      if str(self.root) in cmd or 'replacement-start' in cmd:
       print('REPLACEMENT_START_PROCESS '+str(proc)+' '+cmd+' '+(proc/'wchan').read_text()+' '+(proc/'stat').read_text(),flush=True)
     except (OSError,ProcessLookupError):pass
    raise
  r=run('platform-replacement-start-intent')
  print('REPLACEMENT_START_INTENT '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  intent=json.loads(r.stdout);self.assertEqual(intent['phase'],'START_INTENT')
  self.assertFalse(intent['platformReady']);self.assertNotEqual(intent['generationId'],old)
  start=f.updater/'starts'/intent['generationId']
  records={p.name:p.read_bytes() for p in start.iterdir() if p.is_file()}
  self.assertIn('launch.record',records);self.assertIn('transaction.record',records)
  repeated=run('platform-replacement-start-intent')
  self.assertEqual(repeated.returncode,0,repeated.stdout+repeated.stderr)
  self.assertEqual(json.loads(repeated.stdout)['generationId'],intent['generationId'])
  # A different endpoint at the retired path must not inherit its identity.
  control=f.updater/'hosts'/old/'control';saved_control=self.home/'retired-control'
  control.rename(saved_control);foreign=socket.socket(socket.AF_UNIX,socket.SOCK_SEQPACKET)
  try:
   foreign.bind(str(control));control.chmod(0o700)
   rejected=run('platform-replacement-start');self.assertNotEqual(rejected.returncode,0)
   self.assertEqual(json.loads(rejected.stdout)['errorCode'],'PLATFORM_REPLACEMENT_START_UNCONFIRMED')
   self.assertEqual(rejected.stderr.count('SERVICE_FIRST_ERROR='),1,'structured refusal must not trigger an implicit retry')
   self.assertEqual(foreign.getsockname(),str(control))
   self.assertFalse((f.updater/'generations'/intent['generationId']).exists())
  finally:
   foreign.close();control.unlink();saved_control.rename(control)
  evidence=op/'platform-replacement-start/intent.record';saved=evidence.read_bytes()
  try:
   evidence.write_bytes(b'{broken replacement start')
   rejected=run('platform-replacement-start')
   self.assertNotEqual(rejected.returncode,0)
   self.assertEqual(json.loads(rejected.stdout)['errorCode'],'PLATFORM_REPLACEMENT_START_UNCONFIRMED')
   self.assertEqual(rejected.stderr.count('SERVICE_FIRST_ERROR='),1)
   self.assertEqual(evidence.read_bytes(),b'{broken replacement start')
   self.assertFalse((f.updater/'generations'/intent['generationId']).exists())
  finally:evidence.write_bytes(saved)
  r=run('platform-replacement-start')
  print('REPLACEMENT_START '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);ready=json.loads(r.stdout)
  self.assertEqual(ready['phase'],'READY');self.assertTrue(ready['platformReady'])
  self.assertFalse(ready['activationAllowed']);self.assertEqual(ready['generationId'],intent['generationId'])
  domain=f.updater/'generations'/ready['generationId']
  status=subprocess.run([str(f.native),'control',str(domain),'STATUS',ready['generationId'],
       row['platformPreflight']['expectedPlatformManifestSha256'],op.name,nonce],
       capture_output=True,text=True,timeout=4)
  self.assertEqual(status.returncode,0,status.stdout+status.stderr)
  proof=json.loads(status.stdout);self.assertTrue(proof['supervisedFromBirth']);self.assertTrue(proof['platformReady'])
  self.assertEqual(proof['platformLaunch']['operationId'],op.name)
  self.assertEqual(proof['platformLaunch']['stopNonce'],nonce)
  r=run('platform-replacement-start');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual(json.loads(r.stdout)['generationId'],ready['generationId'])
  self.assertEqual((f.op/'state.json').read_bytes(),origin)
  self.assertEqual((op/'state.json').read_bytes(),state)
  self.assertEqual(fence.readlink(),op/'fence')
  for name,body in records.items():self.assertEqual((start/name).read_bytes(),body)
  self.replacement_run=run
  self.replacement_operation=op
  self.replacement_ready=ready

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  ReplacementStart('test_replacement_starts_new_generation_with_bound_readiness')]))
 raise SystemExit(not result.wasSuccessful())
