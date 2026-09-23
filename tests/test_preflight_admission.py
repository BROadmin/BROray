"""Step 03: real coordinator/guard/identities; private FS, no router services."""
from pathlib import Path
import os, json, signal, subprocess, tempfile, time, shutil, hashlib, unittest
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT','/work/implementation'))
CODE=ROOT/'runtime/app'
GUARD=Path('/work/.local/bin/linux-guard')
SHA='a'*64

def digest(p): return hashlib.sha256(p.read_bytes()).hexdigest()

class Admission(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='f02-admission-'); self.addCleanup(self.tmp.cleanup)
  self.home=Path(self.tmp.name);self.root=self.home/'router';self.live=self.root/'opt/broray'
  self.state=self.root/'opt/var/lib/broray';self.updater=self.root/'opt/var/lib/broray-updater'
  self.lock=self.root/'opt/var/lock/broray/global-operation.lock';self.legacy=self.root/'tmp/broray-global-operation.lock'
  self.live.mkdir(parents=True);self.processes=[]
  self.env={**os.environ,'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','BRORAY_ROOT':str(self.live),
   'BRORAY_STATE_ROOT':str(self.state),'BRORAY_ROUTES_API_LOCK':str(self.lock),
   'BRORAY_LEGACY_GLOBAL_LOCK':str(self.legacy),'BRORAY_OPS_UPDATER_ROOT':str(self.updater),
   'BRORAY_OPS_CODE_ROOT':str(CODE),'BRORAY_OPS_GUARD':str(GUARD),'BRORAY_OPS_ASH':'/bin/ash',
   'BRORAY_OPS_RAM_ROOT':str(self.home/'ram'),'TEST_SHA':SHA,'TEST_HOME':str(self.home)}
  for key in ['BRORAY_OPS_TEST','BRORAY_BACKGROUND_OPERATION_ID','BRORAY_BACKGROUND_OPERATION_TOKEN','BRORAY_BACKGROUND_LAUNCH_NONCE']:
   self.env.pop(key,None)
  self.sentinels={}
  for name in ['config/config.json','servers/a.json','subscriptions/a.json','routes/dot/config.json','runtime/xray']:
   p=self.live/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(b'PRIVATE_UNCHANGED\x00fixture');self.sentinels[p]=p.read_bytes()
  self.addCleanup(self.cleanup)
 def cleanup(self):
  for p in self.processes:
   try:
    if p.poll() is None: os.killpg(p.pid,signal.SIGKILL)
   except ProcessLookupError: pass
   p.wait(timeout=5)
  for p,b in self.sentinels.items():self.assertEqual(p.read_bytes(),b)
 def shell(self,body,env=None,timeout=25):
  return subprocess.run(['/bin/ash','-c','set -u\n. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"\n'+body],
    env=env or self.env,capture_output=True,text=True,timeout=timeout)
 def ok(self,body,env=None):
  p=self.shell(body,env);self.assertEqual(p.returncode,0,(p.stdout,p.stderr));return p
 def once(self,env=None):
  return self.shell('broray_ops_preflight_admit "$TEST_SHA" || exit $?; broray_ops_finish completed',env)
 def waitfile(self,path,p,seconds=15):
  end=time.monotonic()+seconds
  while time.monotonic()<end:
   if path.exists():return
   if p.poll() is not None:self.fail('Worker exited: '+str(p.returncode)+' '+p.stderr.read())
   time.sleep(.02)
  self.fail('Fixture wait timeout '+str(path))
 def hold(self,name='one',ack=True,env=None,start_gate=False):
  folder=self.home/name;folder.mkdir()
  e={**(env or self.env),'TEST_READY':str(folder/'ready'),'TEST_GO':str(self.home/'go'),'TEST_END':str(folder/'end')}
  body='set -u\n. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"\n'
  if start_gate:body+='while [ ! -e "$TEST_GO" ]; do sleep .05; done\n'
  if ack:
   body+='broray_ops_preflight_admit "$TEST_SHA" || exit $?\nprintf "%s\\n" "$BRORAY_BACKGROUND_OPERATION_ID" >"$TEST_READY"\n'
  else:
   body+='broray_ops_call platform-preflight-begin "$TEST_SHA" "$$" 0123456789abcdef0123456789abcdef >"$TEST_READY.tmp" || exit $?; mv "$TEST_READY.tmp" "$TEST_READY"\n'
  body+='n=0; while [ ! -e "$TEST_END" ] && [ "$n" -lt 400 ]; do n=$((n+1)); sleep .1; done\n'
  if ack:body+='broray_ops_finish completed\n'
  script=folder/'hold.sh';script.write_text(body)
  p=subprocess.Popen(['/bin/ash',str(script)],env=e,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,text=True,start_new_session=True)
  self.processes.append(p)
  if not start_gate:self.waitfile(folder/'ready',p)
  return p,folder
 def killed(self,p):
  os.killpg(p.pid,signal.SIGKILL);p.wait(timeout=5)
 def operation(self):
  self.assertTrue(self.lock.is_symlink());return self.lock.resolve().parent
 def readstate(self):return json.loads((self.operation()/'state.json').read_text())
 def records(self):return list((self.state/'operations').glob('op-*/state.json'))
 def freeze(self,folder):return {str(p.relative_to(folder)):p.read_bytes() for p in folder.rglob('*') if p.is_file()}
 def test_normal_identity_protected_finish(self):
  p,f=self.hold();state=self.readstate();owner=json.loads((self.operation()/'owner.json').read_text())
  self.assertEqual(state['source'],'UPDATER');self.assertEqual(state['cancelability'],'protected');self.assertTrue(state['acknowledged'])
  self.assertEqual(state['platformPreflight']['phase'],'PREPARED');self.assertFalse(state['platformPreflight']['mutationStarted'])
  self.assertEqual(owner['owner']['pid'],p.pid);self.assertTrue(owner['owner']['bootId']);self.assertTrue(owner['owner']['startTicks'])
  (f/'end').touch();self.assertEqual(p.wait(timeout=15),0);self.assertFalse(self.lock.is_symlink())
 def test_live_owner_preserved(self):
  p,f=self.hold();before=self.freeze(self.operation());q=self.once()
  self.assertNotEqual(q.returncode,0);self.assertIsNone(p.poll());self.assertEqual(self.freeze(self.operation()),before)
 def test_kill_after_ack_prepared_can_retry(self):
  p,f=self.hold();old=self.operation();self.killed(p)
  q=self.once();self.assertEqual(q.returncode,0,(q.stdout,q.stderr));self.assertFalse(self.lock.is_symlink())
  self.assertEqual(json.loads((old/'state.json').read_text())['state'],'recovered')
 def test_kill_before_ack_can_retry(self):
  p,f=self.hold(ack=False);old=self.operation();self.assertFalse(self.readstate()['acknowledged']);self.killed(p)
  q=self.once();self.assertEqual(q.returncode,0,(q.stdout,q.stderr));self.assertEqual(json.loads((old/'state.json').read_text())['state'],'recovered')
 def test_lost_begin_and_ack_reply_use_one_generation(self):
  original=(CODE/'lib/operation-client.sh').read_text().replace('broray_ops_call()','broray_ops_call_actual()',1)
  shim=self.home/'transport.sh';shim.write_text(original+'''
broray_ops_call() {
 local out rc marker
 marker="$TEST_HOME/drop-$1"
 rc=0; out="$(broray_ops_call_actual "$@")" || rc=$?
 case "$1" in platform-preflight-begin|ack)
  if [ "$rc" = 0 ] && [ ! -e "$marker" ]; then : >"$marker"; return 0; fi ;;
 esac
 [ -z "$out" ] || printf '%s\\n' "$out"
 return "$rc"
}
''')
  p=self.ok('. "'+str(shim)+'"\nbroray_ops_preflight_admit "$TEST_SHA" || exit $?; broray_ops_finish completed')
  self.assertEqual(len(self.records()),1);self.assertTrue((self.home/'drop-platform-preflight-begin').exists());self.assertTrue((self.home/'drop-ack').exists())
 def test_parallel_begins_one_winner(self):
  a,fa=self.hold('a',start_gate=True);b,fb=self.hold('b',start_gate=True);(self.home/'go').touch()
  end=time.monotonic()+20
  while time.monotonic()<end and not ((fa/'ready').exists() or (fb/'ready').exists()):time.sleep(.03)
  winners=[(p,f) for p,f in [(a,fa),(b,fb)] if (f/'ready').exists()];self.assertEqual(len(winners),1)
  loser=b if winners[0][0] is a else a;self.assertNotEqual(loser.wait(timeout=15),0)
  (winners[0][1]/'end').touch();self.assertEqual(winners[0][0].wait(timeout=15),0)
  self.assertEqual(len(self.records()),1)
 def test_legacy_pid_only_bytes_preserved(self):
  self.lock.mkdir(parents=True)
  for n,v in {'pid':'2147483646','scope':'routes','action':'dot:delete','bundle':'dns-over-tls','startedAt':'old'}.items():(self.lock/n).write_text(v+'\n')
  old=self.freeze(self.lock);p=self.once();self.assertNotEqual(p.returncode,0);self.assertEqual(self.freeze(self.lock),old)
 def test_actual_legacy_path_blocks(self):
  self.legacy.mkdir(parents=True);(self.legacy/'pid').write_text(str(os.getpid())+'\n')
  p=self.once();self.assertNotEqual(p.returncode,0);self.assertFalse(self.lock.is_symlink());self.assertTrue(self.legacy.is_dir())
 def test_live_updater_request_blocks(self):
  (self.updater/'request.lock').mkdir(parents=True)
  p=self.once();self.assertNotEqual(p.returncode,0);self.assertFalse(self.lock.is_symlink())
 def test_real_queue_blocks_not_code_slot_queue(self):
  (self.updater/'queue').mkdir(parents=True);(self.updater/'queue/pending.json').write_text('{}')
  p=self.once();self.assertNotEqual(p.returncode,0);self.assertFalse(self.lock.is_symlink())
 def test_live_route_progress_blocks_code_root_separate(self):
  d=self.live/'routes/operations';d.mkdir();(d/'fixture.json').write_text('{"kind":"routes","running":true,"resumable":false}')
  p=self.once();self.assertNotEqual(p.returncode,0);self.assertFalse(self.lock.is_symlink());self.assertFalse((self.live/'lib').exists())
 def test_paused_automation_explicit_preflight_only(self):
  self.state.mkdir(parents=True);auto=self.state/'background-automation.json';auto.write_text('{"schemaVersion":1,"paused":true}')
  before=auto.read_bytes();p=self.once();self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(auto.read_bytes(),before)
  q=self.shell('broray_ops_begin system subscriptions:scheduler subscriptions SUBSCRIPTION_AUTO cooperative');self.assertNotEqual(q.returncode,0)
  self.assertEqual(auto.read_bytes(),before)
 def test_bad_manifest_refuses_before_global(self):
  for sha in ['bad','F'*64,'a'*63]:
   p=self.once({**self.env,'TEST_SHA':sha});self.assertNotEqual(p.returncode,0);self.assertFalse(self.lock.is_symlink())
 def test_incompatible_guard_refuses_before_state(self):
  fake=self.home/'old-guard';fake.write_text('#!/bin/ash\necho broray-ops-guard/old\n');fake.chmod(0o755)
  p=self.once({**self.env,'BRORAY_OPS_GUARD':str(fake)});self.assertNotEqual(p.returncode,0);self.assertFalse(self.lock.is_symlink())
 def test_incompatible_live_guard_also_refuses(self):
  d=self.live/'bin';d.mkdir();p=d/'broray-ops-guard';p.write_text('#!/bin/ash\necho old-fcntl-generation\n');p.chmod(0o755)
  q=self.once();self.assertNotEqual(q.returncode,0);self.assertFalse(self.lock.is_symlink())
 def test_missing_transitive_library_refuses_before_state(self):
  slot=self.home/'slot';shutil.copytree(CODE/'lib',slot/'lib');(slot/'lib/operation-report-facts.sh').unlink()
  p=self.once({**self.env,'BRORAY_OPS_CODE_ROOT':str(slot)});self.assertNotEqual(p.returncode,0);self.assertFalse(self.lock.is_symlink())
 def test_generic_begin_cannot_create_cooperative_preflight(self):
  p=self.shell('broray_ops_begin system system:platform-preflight updater-platform UPDATER cooperative')
  self.assertNotEqual(p.returncode,0);self.assertFalse(self.lock.is_symlink())
 def test_generic_job_cannot_recover_protected_preflight(self):
  p,f=self.hold();old=self.operation();self.killed(p);before=self.freeze(old)
  q=self.shell('broray_ops_begin system servers:check servers USER cooperative');self.assertNotEqual(q.returncode,0);self.assertEqual(self.freeze(old),before)
  self.assertEqual(self.once().returncode,0)
 def test_changed_manifest_cannot_adopt_stale_preflight(self):
  p,f=self.hold();old=self.operation();self.killed(p);before=self.freeze(old)
  q=self.once({**self.env,'TEST_SHA':'b'*64});self.assertNotEqual(q.returncode,0);self.assertEqual(self.freeze(old),before)
  self.assertEqual(self.once().returncode,0)
 def test_cannot_cancel_or_open_commit_boundary(self):
  p,f=self.hold();record=json.loads((self.operation()/'owner.json').read_text());i,t=record['operationId'],record['token']
  for body in [f'broray_ops_call cancel {i}',f'broray_ops_call tick {i} {t} committing']:
   q=self.shell(body);self.assertNotEqual(q.returncode,0)
  self.assertIsNone(p.poll());self.assertFalse(self.readstate()['platformPreflight']['mutationStarted'])
 def test_terminal_flag_cannot_hide_started_mutation(self):
  p,f=self.hold();d=self.operation();self.killed(p)
  file=d/'state.json';s=json.loads(file.read_text());s['running']=False;s['state']='failed';s['platformPreflight']['phase']='INSTALLING';s['platformPreflight']['mutationStarted']=True;file.write_text(json.dumps(s))
  before=self.freeze(d);q=self.once();self.assertNotEqual(q.returncode,0);self.assertEqual(self.freeze(d),before)
 def test_unknown_domain_marker_preserved(self):
  p,f=self.hold();d=self.operation();self.killed(p);(d/'platform-mutation.json').write_text('UNKNOWN')
  q=self.once();self.assertNotEqual(q.returncode,0);self.assertTrue(self.lock.is_symlink());self.assertEqual((d/'platform-mutation.json').read_text(),'UNKNOWN')
 def test_crash_during_hidden_publication_is_not_admission(self):
  e={**self.env,'BRORAY_OPS_TEST':'1','BRORAY_OPS_ASH':'/bin/busybox','BRORAY_OPS_TEST_LAUNCH_CRASH':'fence'}
  q=self.once(e);self.assertNotEqual(q.returncode,0);self.assertFalse(self.lock.is_symlink())
  q=self.once();self.assertEqual(q.returncode,0,(q.stdout,q.stderr))
 def test_admission_only_never_claims_platform_ready(self):
  payload=CODE/'share/updater-platform';h=digest(payload/'SHA256SUMS')
  e={**self.env,'BRORAY_HANDOFF_ROOT_PREFIX':str(self.root),'BRORAY_HANDOFF_ASH':'/bin/ash','BRORAY_HANDOFF_PAYLOAD_ROOT':str(payload),'BRORAY_HANDOFF_TEST_MODE':'1'}
  p=subprocess.run(['/bin/ash',str(CODE/'lib/universal-platform-handoff.sh'),'preflight-admission',h],env=e,capture_output=True,text=True,timeout=30)
  self.assertEqual(p.returncode,0,(p.stdout,p.stderr));r=json.loads(p.stdout)
  self.assertEqual(r['code'],'PREFLIGHT_ADMISSION_ONLY');self.assertFalse(r['platformReady']);self.assertFalse(r['platformMutationAllowed'])
  self.assertFalse((self.root/'opt/libexec/broray-updater').exists());self.assertFalse((self.root/'opt/var/lib/broray-updater-preflight').exists())

 def authenticated_slot(self):
  slot=self.home/'authenticated-slot';shutil.copytree(CODE,slot/'app')
  shutil.copy2(GUARD,slot/'app/bin/broray-ops-guard');(slot/'app/bin/broray-ops-guard').chmod(0o755)
  manifest=slot/'SHA256SUMS';manifest.write_text(''.join(digest(p)+'  '+p.relative_to(slot).as_posix()+'\n' for p in sorted(slot.rglob('*')) if p.is_file()))
  return slot,digest(manifest),digest(slot/'app/share/updater-platform/SHA256SUMS')
 def bootstrap_admission(self,slot,manifest,platform):
  e={**self.env,'BRORAY_HANDOFF_ROOT_PREFIX':str(self.root),'BRORAY_HANDOFF_ASH':'/bin/ash'}
  e.pop('BRORAY_OPS_GUARD');e.pop('BRORAY_OPS_CODE_ROOT')
  return subprocess.run(['/bin/ash',str(ROOT/'bootstrap/prepare-persistent-updater.sh'),str(slot),manifest,platform,'--admission-only'],env=e,capture_output=True,text=True,timeout=40)
 def test_authenticated_bootstrap_without_installed_libraries(self):
  slot,m,p=self.authenticated_slot();r=self.bootstrap_admission(slot,m,p)
  self.assertEqual(r.returncode,0,(r.stdout,r.stderr));self.assertFalse(json.loads(r.stdout)['platformReady'])
  self.assertFalse((self.live/'lib').exists());self.assertFalse((self.root/'opt/libexec/broray-updater').exists())
 def test_authenticated_bootstrap_rejects_tampered_code(self):
  slot,m,p=self.authenticated_slot();(slot/'app/lib/operation-owner.sh').write_text('touch "$TEST_HOME/UNEXPECTED_CODE"\n')
  r=self.bootstrap_admission(slot,m,p);self.assertNotEqual(r.returncode,0)
  self.assertFalse((self.home/'UNEXPECTED_CODE').exists());self.assertFalse(self.lock.is_symlink())

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Admission))
 print('ADMISSION_RECEIPT '+json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'skipped':len(result.skipped),'routerAccess':False,'platformMutation':False}),flush=True)
 raise SystemExit(0 if result.wasSuccessful() else 1)
