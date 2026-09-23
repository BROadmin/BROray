"""Authenticated preflight before/after a real boot, exact delivery and route guard.

The 120-second whole-operation test budget matches InstalledInit.init and a
measured 77.877-second valid QEMU transaction. Native readiness is unchanged.
Legacy running-file hooks are retained solely for imported baseline probes;
they cannot establish successful readiness in this suite.
"""
import hashlib,json,os,shutil,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
PLATFORM=ROOT/'runtime/app/share/updater-platform'
TARGETS=[line.split()[1] for line in (PLATFORM/'SHA256SUMS').read_text().splitlines()]
HANDOFF=ROOT/'runtime/app/lib/universal-platform-handoff.sh'
UPDATER='opt/libexec/broray-updater/broray-updater.sh'
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
class UpdaterPreflight(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='updater-preflight-');self.addCleanup(self.tmp.cleanup)
  self.home=Path(self.tmp.name);self.root=self.home/'router';self.payload=self.home/'payload'
  shutil.copytree(PLATFORM,self.payload)
  for name in TARGETS:
   p=self.payload/name;p.chmod(0o755)
   old=self.root/name;old.parent.mkdir(parents=True,exist_ok=True)
   old.write_text('#!/bin/ash\n# old '+name+'\nexit 0\n');old.chmod(0o755)
  # Native recovery deliberately uses the installed Entware shell.
  shell=self.root/'opt/bin/ash';shutil.copyfile('/bin/ash',shell);shell.chmod(0o755)
  self.sha=digest(self.payload/'SHA256SUMS');self.expected_old=self.snapshot()
  (self.home/'running').touch();self.log=self.home/'services.log'
  self.shim=self.home/'service';self.shim.write_text('''#!/bin/ash
printf '%s\\n' "$1" >>"$TEST_ROOT/services.log"
case "$1" in
 status|ready) [ -f "$TEST_ROOT/running" ] ;;
 stop) rm -f "$TEST_ROOT/running" ;;
 start) if [ -f "$TEST_ROOT/fail-start" ]; then rm "$TEST_ROOT/fail-start"; exit 1; fi; touch "$TEST_ROOT/running" ;;
 *) exit 2 ;;
esac
''');self.shim.chmod(0o755)
  self.stop=self.home/'stop';self.stop.write_text('#!/bin/ash\nexec "$TEST_SERVICE" stop\n');self.stop.chmod(0o755)
  self.ready=self.home/'ready';self.ready.write_text('#!/bin/ash\nexec "$TEST_SERVICE" ready\n');self.ready.chmod(0o755)
  self.env={**os.environ,'BRORAY_HANDOFF_ROOT_PREFIX':str(self.root),'BRORAY_HANDOFF_TEST_MODE':'1','BRORAY_HANDOFF_ASH':'/bin/ash','BRORAY_HANDOFF_PAYLOAD_ROOT':str(self.payload),'BRORAY_HANDOFF_PREFLIGHT_LOCK_LIBRARY':str(ROOT/'runtime/app/lib/routes-api-operation.sh'),'BRORAY_HANDOFF_INIT_HOOK':str(self.shim),'BRORAY_HANDOFF_DAEMON_READY_HOOK':str(self.ready),'BRORAY_HANDOFF_PREFLIGHT_STOP_HOOK':str(self.stop),'TEST_ROOT':str(self.home),'TEST_SERVICE':str(self.shim)}
  self.code_root=self.home/'verified-code'
  self.code_fixture(self.code_root)
  shutil.copytree(self.payload,self.code_root/'share/updater-platform')
  self.env.update(BRORAY_OPS_CODE_ROOT=str(self.code_root),BRORAY_OPS_ASH='/bin/ash',BRORAY_OPS_RAM_ROOT=str(self.home/'ops-ram'))
  for key in ['BRORAY_OPS_GUARD','BRORAY_OPS_GENERATION','BRORAY_OPS_TEST']:
   self.env.pop(key,None)
  self.sentinels=[]
  for relative in ['opt/broray/config/server','opt/broray/runtime/xray','opt/broray/current-sentinel','opt/etc/route-sentinel']:
   p=self.root/relative;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(b'unchanged\x00test');self.sentinels.append(p)
  self.addCleanup(self.unchanged_sentinels)

 def unchanged_sentinels(self):
  for p in self.sentinels:self.assertEqual(p.read_bytes(),b'unchanged\x00test')

 def snapshot(self):
  return {name:((self.root/name).read_bytes(),(self.root/name).stat().st_mode&0o777) if (self.root/name).is_file() else None for name in TARGETS}

 def call(self,sha=None,env=None):
  return subprocess.run(['/bin/ash',str(HANDOFF),'preflight',sha or self.sha],env=env or self.env,capture_output=True,text=True,timeout=120)

 def assert_no_delivery(self,p):
  self.assertNotEqual(p.returncode,0,p.stdout);self.assertEqual(self.snapshot(),self.expected_old)
  self.assertFalse(self.log.exists(),'service control before validation')

 def rehash_payload(self):
  manifest=self.payload/'SHA256SUMS';manifest.write_text(''.join(digest(self.payload/n)+'  '+n+'\n' for n in TARGETS));self.sha=digest(manifest)

 def gate(self,launch=None):
  slot=self.home/'slot';target=slot/'app/share/updater-platform'
  if not target.exists():shutil.copytree(self.payload,target)
  source=self.home/'updater-library.sh';text=(PLATFORM/UPDATER).read_text();self.assertTrue(text.endswith('main "$@"\n'))
  source.write_text(text[:-len('main "$@"\n')])
  env={**self.env,'BRORAY_UPDATER_ROOT_PREFIX':str(self.root),'BRORAY_UPDATER_TEST_MODE':'1','GATE_SOURCE':str(source),'TEST_LAUNCH':launch or digest(self.payload/UPDATER),'TEST_SLOT':str(slot)}
  script='. "$GATE_SOURCE"; UPDATER_LAUNCH_SHA256=$TEST_LAUNCH; updater_platform_before_routes "$TEST_SLOT" || exit 1; printf route-boundary >"$TEST_ROOT/route-boundary"'
  return subprocess.run(['/bin/ash','-c',script],env=env,capture_output=True,text=True,timeout=15)

 def interrupted(self,phase='installing'):
  state=self.root/'opt/var/lib/broray-updater-preflight';backup=state/'platform-backup';backup.mkdir(parents=True)
  rows=[]
  for name in TARGETS:
   src=self.root/name;dst=backup/name;dst.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(src,dst)
   rows.append('present\t'+name+'\t'+digest(dst)+'\n')
  (backup/'inventory.tsv').write_text(''.join(rows));(state/'phase').write_text(phase+'\n');(state/'daemon-was-running').write_text('true\n')
  (self.home/'running').unlink(missing_ok=True)
  return state,backup

 def code_fixture(self,app):
  shutil.copytree(ROOT/'runtime/app/lib',app/'lib');(app/'bin').mkdir()
  for name,fixture in [('broray-ops-guard','linux-guard'),('broray-updater-generation','linux-generation')]:
   src=Path('/work/.local/bin')/fixture
   self.assertTrue(src.is_file(),str(src));shutil.copyfile(src,app/'bin'/name);(app/'bin'/name).chmod(0o755)
 def slot_fixture(self):
  slot=self.home/'authenticated-slot';app=slot/'app'
  self.code_fixture(app);shutil.copytree(self.payload,app/'share/updater-platform')
  shutil.copyfile(ROOT/'runtime/release.json',slot/'release.json')
  manifest=slot/'SHA256SUMS';manifest.write_text(''.join(digest(p)+'  '+p.relative_to(slot).as_posix()+'\n' for p in sorted(slot.rglob('*')) if p.is_file()))
  return slot,digest(manifest)
 def bootstrap(self,slot,sha):
  return subprocess.run(['/bin/ash',str(ROOT/'bootstrap/prepare-persistent-updater.sh'),str(slot),sha,self.sha],env=self.env,capture_output=True,text=True,timeout=120)

 def delivery_entry(self,action,slot,sha):
  ctl=self.home/'request-fixture'
  ctl.write_text('''#!/bin/ash
[ "$#" = 5 ] && [ "$1" = request ] && [ "$3" = --prepared-target ] || exit 9
[ "$4" = "$TEST_CANDIDATE" ] && [ "$5" = "$TEST_ARCHIVE_SHA" ] || exit 8
actual="$(sha256sum "$TEST_UPDATER_PATH")" || exit 7
[ "${actual%% *}" = "$TEST_UPDATER_SHA" ] || exit 6
printf '%s %s %s\\n' "$2" "$4" "$5" >>"$TEST_ROOT/enqueued"
''');ctl.chmod(0o755)
  candidate=json.loads((slot/'release.json').read_text())['candidateId'];archive_sha='a'*64
  env={**self.env,'BRORAY_HANDOFF_TEST_MODE':'1','TEST_ROOT':str(self.home),'BRORAY_BOOTSTRAP_TEST_UPDATERCTL':str(ctl),
   'TEST_CANDIDATE':candidate,'TEST_ARCHIVE_SHA':archive_sha,'TEST_UPDATER_PATH':str(self.root/UPDATER),'TEST_UPDATER_SHA':digest(self.payload/UPDATER)}
  return subprocess.run(['/bin/ash',str(ROOT/'bootstrap/update-with-preflight.sh'),action,str(slot),sha,self.sha,archive_sha],env=env,capture_output=True,text=True,timeout=120)

class PreflightBeforeBoot(UpdaterPreflight):
 def test_wrong_expected_hash_fails_before_any_payload_execution(self):
  p=self.payload/UPDATER;p.write_text('#!/bin/ash\ntouch "$TEST_ROOT/executed"\necho broray-updater/5\n')
  self.rehash_payload();self.assert_no_delivery(self.call('0'*64));self.assertFalse((self.home/'executed').exists())

 def test_missing_file_refuses_without_installation(self):
  (self.payload/TARGETS[0]).unlink();self.assert_no_delivery(self.call())

 def test_wrong_file_hash_refuses_without_installation(self):
  (self.payload/TARGETS[0]).write_text('corrupt');self.assert_no_delivery(self.call())

 def test_non_executable_payload_refuses(self):
  (self.payload/TARGETS[0]).chmod(0o644);self.assert_no_delivery(self.call())

 def test_duplicate_manifest_row_refuses(self):
  manifest=self.payload/'SHA256SUMS';rows=manifest.read_text().splitlines();rows[1]=rows[0];manifest.write_text('\n'.join(rows)+'\n')
  self.sha=digest(manifest);self.assert_no_delivery(self.call())

 def test_symlink_destination_parent_refuses(self):
  parent=self.root/'opt/bin';other=self.root/'opt/renamed-bin';parent.rename(other);parent.symlink_to(other)
  self.assert_no_delivery(self.call())

 def test_request_lock_blocks_preflight(self):
  lock=self.root/'opt/var/lib/broray-updater/request.lock';lock.mkdir(parents=True)
  self.assert_no_delivery(self.call());self.assertTrue(lock.exists())

 def test_queue_without_request_lock_blocks_preflight(self):
  queue=self.root/'opt/var/lib/broray-updater/queue';queue.mkdir(parents=True);(queue/'pending.json').write_text('{}')
  self.assert_no_delivery(self.call());self.assertTrue((queue/'pending.json').exists())

 def test_bootstrap_wrong_runtime_manifest_is_read_only(self):
  slot,sha=self.slot_fixture();self.assert_no_delivery(self.bootstrap(slot,'0'*64))

 def test_bootstrap_tampered_handoff_never_executes(self):
  slot,sha=self.slot_fixture();(slot/'app/lib/universal-platform-handoff.sh').write_text('#!/bin/ash\ntouch "$TEST_ROOT/executed"\n')
  self.assert_no_delivery(self.bootstrap(slot,sha));self.assertFalse((self.home/'executed').exists())

 def test_unrelated_process_is_never_stopped(self):
  process=subprocess.Popen(['/bin/sleep','30']);self.addCleanup(lambda:process.poll() is None and process.terminate())
  proc=self.root/'proc'/str(process.pid);proc.mkdir(parents=True)
  (proc/'stat').write_bytes((Path('/proc')/str(process.pid)/'stat').read_bytes());(proc/'cmdline').write_bytes((Path('/proc')/str(process.pid)/'cmdline').read_bytes())
  state=self.root/'opt/var/lib/broray-updater';state.mkdir(parents=True);(state/'daemon.pid').write_text(str(process.pid)+'\n')
  env=dict(self.env);env.pop('BRORAY_HANDOFF_PREFLIGHT_STOP_HOOK');p=self.call(env=env)
  self.assertNotEqual(p.returncode,0);self.assertEqual(self.snapshot(),self.expected_old);self.assertIsNone(process.poll())
  process.terminate();process.wait(timeout=5)

 def test_failed_delivery_never_enqueues_update(self):
  slot,sha=self.slot_fixture();p=self.delivery_entry('update',slot,'0'*64)
  self.assert_no_delivery(p);self.assertFalse((self.home/'enqueued').exists())


 def pending(self,result):
  self.assertEqual(result.returncode,75,result.stdout+result.stderr);reply=json.loads(result.stdout)
  self.assertEqual(reply['errorCode'],'UPDATER_LEGACY_REBOOT_REQUIRED');self.assertEqual(reply['phase'],'REBOOT_REQUIRED')
  for name in ['platformReady','serviceStopped','activationAllowed','signalsAuthorized']:self.assertFalse(reply[name])
  op=self.root/'opt/var/lib/broray/operations'/reply['operationId']
  state=json.loads((op/'state.json').read_text());self.assertEqual(state['cancelability'],'protected');self.assertTrue(state['running'])
  self.assertEqual(state['platformPreflight']['expectedPlatformManifestSha256'],self.sha)
  self.assertTrue((op/'platform-migration/intent.record').is_file());self.assertTrue((op/'platform-bootguard/staged.receipt').is_file())
  self.assertTrue((self.root/'opt/var/lock/broray/global-operation.lock').is_symlink())
  self.assertFalse((self.root/'opt/var/lib/broray-updater/generations').exists())
  self.assertFalse((self.root/'opt/var/lib/broray-updater/request.lock').exists())
  self.assertFalse(self.log.exists(),'legacy fake init must not supply readiness')
  return op
 def test_legacy_requires_real_boot_before_delivery(self):
  slot,sha=self.slot_fixture();op=self.pending(self.bootstrap(slot,sha))
  from test_native_platform_backup import FILES
  for i,name in enumerate(FILES):
   self.assertEqual((op/'platform-migration'/('file-'+str(i))).read_bytes(),(self.payload/name).read_bytes())
  self.assertFalse((self.home/'enqueued').exists())
 def test_same_boot_retry_preserves_evidence_and_never_starts(self):
  op=self.pending(self.call());before={p.relative_to(op).as_posix():p.read_bytes() for p in op.rglob('*') if p.is_file()}
  platform=self.snapshot();self.assertEqual(self.pending(self.call()),op)
  self.assertEqual(self.snapshot(),platform)
  self.assertEqual({p.relative_to(op).as_posix():p.read_bytes() for p in op.rglob('*') if p.is_file()},before)
 def test_legacy_update_cannot_enqueue_before_reboot(self):
  slot,sha=self.slot_fixture();r=self.delivery_entry('update',slot,sha)
  self.assertNotEqual(r.returncode,0);self.assertIn('UPDATER_LEGACY_REBOOT_REQUIRED',r.stdout)
  self.assertFalse((self.home/'enqueued').exists())

class PreflightLoadedGuard(UpdaterPreflight):
 def setUp(self):
  super().setUp()
  # This fixture tests the read-only loaded/installed-byte boundary. Installation
  # and readiness are separately exercised through real bootstrap below.
  for name in TARGETS:shutil.copy2(self.payload/name,self.root/name)
 def test_guard_accepts_only_current_loaded_and_installed_platform(self):
  g=self.gate();self.assertEqual(g.returncode,0,g.stderr)
  self.assertTrue((self.home/'route-boundary').exists())

 def test_guard_rejects_old_loaded_daemon_even_if_files_updated(self):
  g=self.gate('0'*64);self.assertNotEqual(g.returncode,0);self.assertFalse((self.home/'route-boundary').exists())

 def test_guard_rejects_missing_installed_updater(self):
  (self.root/UPDATER).unlink();g=self.gate();self.assertNotEqual(g.returncode,0);self.assertFalse((self.home/'route-boundary').exists())

 def test_guard_rejects_tampered_installed_component(self):
  (self.root/TARGETS[0]).write_text('wrong');g=self.gate();self.assertNotEqual(g.returncode,0);self.assertFalse((self.home/'route-boundary').exists())

 def test_guard_rejects_non_executable_component(self):
  (self.root/UPDATER).chmod(0o644);g=self.gate();self.assertNotEqual(g.returncode,0);self.assertFalse((self.home/'route-boundary').exists())

 def test_guard_is_before_actual_capture_and_service_stop(self):
  source=(PLATFORM/UPDATER).read_text().split('request_process()\n',1)[1].split('\nrollback_current()',1)[0]
  self.assertLess(source.index('updater_platform_before_routes "$slot_root"'),source.index('    routes_capture ||'))
  self.assertLess(source.index('updater_platform_before_routes "$slot_root"'),source.index('    if ! services_stop_captured;'))


class PreflightAfterBoot(UpdaterPreflight):
 def setUp(self):
  from test_installed_init_control import InstalledInit
  from test_native_platform_install import FILES
  fixture=InstalledInit('test_installed_init_resumes_install_boundary_without_unsupervised_daemon')
  try:fixture.setUp()
  finally:self._cleanups.extend(fixture._cleanups);fixture._cleanups=[]
  self.parent_fixture=fixture;self.home=fixture.root;self.root=self.home/'router';self.payload=PLATFORM
  self.sha=digest(self.payload/'SHA256SUMS');self.expected_old=self.snapshot();self.log=self.home/'unused-legacy-hook.log'
  self.env={**os.environ,'BRORAY_HANDOFF_ROOT_PREFIX':str(self.root),'BRORAY_HANDOFF_ASH':'/bin/ash',
   'BRORAY_OPS_ASH':str(self.root/'opt/bin/ash'),'BRORAY_OPS_RAM_ROOT':str(self.home/'ram'),'TEST_ROOT':str(self.home)}
  for name in ['BRORAY_OPS_CODE_ROOT','BRORAY_OPS_GUARD','BRORAY_OPS_GENERATION','BRORAY_OPS_TEST']:self.env.pop(name,None)
  self.assertNotEqual(fixture.boot,fixture.e['oldBootId'])
  self.assertFalse((fixture.op/'platform-install.record').exists());self.assertFalse((fixture.updater/'generations').exists())
  binding=json.loads((fixture.op/'platform-bootguard.json').read_text());self.assertEqual(binding['expectedPlatformManifestSha256'],self.sha)
  self.assertEqual(set(FILES),set(TARGETS))
  for index,name in enumerate(FILES):self.assertEqual((fixture.op/'platform-migration'/('file-'+str(index))).read_bytes(),(self.payload/name).read_bytes())
  self.assertTrue(any(v[0]!=(self.payload/n).read_bytes() for n,v in self.expected_old.items() if v))
  self.slot,self.slot_sha=self.slot_fixture()
  self.addCleanup(self.remove_slot)
 def remove_slot(self):
  # Scoped fixture-created directory; no production/source directory.
  shutil.rmtree(self.slot)
  for n in ['slot','updater-library.sh','request-fixture','enqueued','route-boundary']:
   p=self.home/n
   if p.is_dir():shutil.rmtree(p)
   elif p.is_file():p.unlink()
 def completed(self,result):
  self.assertEqual(result.returncode,0,result.stdout+result.stderr);reply=json.loads(result.stdout)
  self.assertEqual(reply['phase'],'PREFLIGHT_COMPLETED');self.assertTrue(reply['platformReady']);self.assertFalse(reply['activationAllowed'])
  self.assertRegex(reply['commitReceiptSha256'],r'^[0-9a-f]{64}$')
  for name in TARGETS:
   p=self.root/name;self.assertEqual(p.read_bytes(),(self.payload/name).read_bytes());self.assertEqual(p.stat().st_mode&0o777,0o755)
  op=self.parent_fixture.op;state=json.loads((op/'state.json').read_text());self.assertEqual(state['state'],'completed');self.assertFalse(state['running'])
  lock=self.root/'opt/var/lock/broray/global-operation.lock';self.assertFalse(lock.exists() or lock.is_symlink())
  self.assertFalse((self.parent_fixture.updater/'request.lock').exists());self.assertFalse(self.parent_fixture.fetch.exists())
  self.assertEqual(list((self.parent_fixture.updater/'queue').glob('*.json')),[])
  domain=self.parent_fixture.updater/'generations'/reply['generationId']
  r=subprocess.run([str(self.parent_fixture.native),'control',str(domain),'STATUS',reply['generationId'],self.sha,op.name,self.parent_fixture.e['stopNonce']],capture_output=True,text=True,timeout=4)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr);live=json.loads(r.stdout)
  self.assertTrue(live['supervisedFromBirth']);self.assertTrue(live['platformReady']);self.assertEqual(live['state'],'RUNNING')
  self.assertEqual(live['generationId'],reply['generationId']);self.assertEqual(live['platformManifestSha256'],self.sha)
  print('PREFLIGHT_CONTRACT_RECEIPT '+json.dumps(dict(reply=reply,oldBootId=self.parent_fixture.e['oldBootId'],bootId=self.parent_fixture.boot,files=7,queueEmpty=True,sourceOfReadiness='authenticated generation STATUS')),flush=True)
  return reply
 def test_authenticated_bootstrap_prepares_before_request(self):
  self.completed(self.bootstrap(self.slot,self.slot_sha))
 def test_repeated_preflight_is_safe(self):
  first=self.completed(self.bootstrap(self.slot,self.slot_sha));platform=self.snapshot();state=(self.parent_fixture.op/'state.json').read_bytes()
  second=self.completed(self.bootstrap(self.slot,self.slot_sha));self.assertTrue(second['replayed'])
  self.assertEqual(second['generationId'],first['generationId']);self.assertEqual(second['commitReceiptSha256'],first['commitReceiptSha256'])
  self.assertEqual(self.snapshot(),platform);self.assertEqual((self.parent_fixture.op/'state.json').read_bytes(),state)
 def test_update_and_reinstall_entry_prepare_before_enqueue(self):
  candidate=json.loads((self.slot/'release.json').read_text())['candidateId']
  for action in ['update','reinstall']:
   r=self.delivery_entry(action,self.slot,self.slot_sha);self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertEqual((self.home/'enqueued').read_text().splitlines(),[action+' '+candidate+' '+'a'*64 for action in ['update','reinstall']])
  self.completed(self.bootstrap(self.slot,self.slot_sha))

if __name__=='__main__':
 classes=[PreflightAfterBoot] if Path('/work/migration-transfer.json').is_file() else [PreflightBeforeBoot,PreflightLoadedGuard]
 suite=unittest.TestSuite(c(n) for c in classes for n in c.__dict__ if n.startswith('test_'))
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(suite)
 raise SystemExit(not result.wasSuccessful())
