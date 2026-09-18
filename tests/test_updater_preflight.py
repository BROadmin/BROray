"""Persistent platform delivery before route boundary, with isolated service hooks."""
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
  self.sentinels=[]
  for relative in ['opt/broray/config/server','opt/broray/runtime/xray','opt/broray/current-sentinel','opt/etc/route-sentinel']:
   p=self.root/relative;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(b'unchanged\x00test');self.sentinels.append(p)
  self.addCleanup(self.unchanged_sentinels)
 def unchanged_sentinels(self):
  for p in self.sentinels:self.assertEqual(p.read_bytes(),b'unchanged\x00test')
 def snapshot(self):
  return {name:((self.root/name).read_bytes(),(self.root/name).stat().st_mode&0o777) if (self.root/name).is_file() else None for name in TARGETS}
 def call(self,sha=None,env=None):
  return subprocess.run(['/bin/ash',str(HANDOFF),'preflight',sha or self.sha],env=env or self.env,capture_output=True,text=True,timeout=45)
 def assert_no_delivery(self,p):
  self.assertNotEqual(p.returncode,0,p.stdout);self.assertEqual(self.snapshot(),self.expected_old)
  self.assertFalse(self.log.exists(),'service control before validation')
 def test_success_installs_and_starts_exact_platform(self):
  p=self.call();self.assertEqual(p.returncode,0,p.stderr)
  for name in TARGETS:self.assertEqual((self.root/name).read_bytes(),(self.payload/name).read_bytes());self.assertEqual((self.root/name).stat().st_mode&0o777,0o755)
  self.assertTrue((self.home/'running').exists());self.assertFalse((self.root/'opt/var/lock/broray/global-operation.lock').exists())
  report=json.loads((self.root/'opt/var/lib/broray-updater-preflight/status.json').read_text());self.assertEqual(report['code'],'PREFLIGHT_PLATFORM_READY')
 def test_wrong_expected_hash_fails_before_any_payload_execution(self):
  p=self.payload/UPDATER;p.write_text('#!/bin/ash\ntouch "$TEST_ROOT/executed"\necho broray-updater/5\n')
  self.rehash_payload();self.assert_no_delivery(self.call('0'*64));self.assertFalse((self.home/'executed').exists())
 def rehash_payload(self):
  manifest=self.payload/'SHA256SUMS';manifest.write_text(''.join(digest(self.payload/n)+'  '+n+'\n' for n in TARGETS));self.sha=digest(manifest)
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
 def test_copy_failure_restores_platform_and_can_retry(self):
  p=self.call(env={**self.env,'BRORAY_HANDOFF_FAIL_AFTER':'3'});self.assertNotEqual(p.returncode,0)
  self.assertEqual(self.snapshot(),self.expected_old,p.stderr);self.assertTrue((self.home/'running').exists())
  self.assertFalse((self.root/'opt/var/lock/broray/global-operation.lock').exists())
  retry=self.call();self.assertEqual(retry.returncode,0,retry.stderr)
 def test_daemon_start_failure_restores_platform(self):
  (self.home/'fail-start').touch();p=self.call();self.assertNotEqual(p.returncode,0)
  self.assertEqual(self.snapshot(),self.expected_old,p.stderr);self.assertTrue((self.home/'running').exists())
 def test_repeated_preflight_is_safe(self):
  self.assertEqual(self.call().returncode,0);before=self.snapshot();p=self.call();self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(self.snapshot(),before)
 def gate(self,launch=None):
  slot=self.home/'slot';target=slot/'app/share/updater-platform'
  if not target.exists():shutil.copytree(self.payload,target)
  source=self.home/'updater-library.sh';text=(PLATFORM/UPDATER).read_text();self.assertTrue(text.endswith('main "$@"\n'))
  source.write_text(text[:-len('main "$@"\n')])
  env={**self.env,'BRORAY_UPDATER_ROOT_PREFIX':str(self.root),'BRORAY_UPDATER_TEST_MODE':'1','GATE_SOURCE':str(source),'TEST_LAUNCH':launch or digest(self.payload/UPDATER),'TEST_SLOT':str(slot)}
  script='. "$GATE_SOURCE"; UPDATER_LAUNCH_SHA256=$TEST_LAUNCH; updater_platform_before_routes "$TEST_SLOT" || exit 1; printf route-boundary >"$TEST_ROOT/route-boundary"'
  return subprocess.run(['/bin/ash','-c',script],env=env,capture_output=True,text=True,timeout=15)
 def test_guard_accepts_only_current_loaded_and_installed_platform(self):
  p=self.call();self.assertEqual(p.returncode,0,p.stderr);g=self.gate();self.assertEqual(g.returncode,0,g.stderr)
  self.assertTrue((self.home/'route-boundary').exists())
 def test_guard_rejects_old_loaded_daemon_even_if_files_updated(self):
  self.assertEqual(self.call().returncode,0);g=self.gate('0'*64);self.assertNotEqual(g.returncode,0);self.assertFalse((self.home/'route-boundary').exists())
 def test_guard_rejects_missing_installed_updater(self):
  self.assertEqual(self.call().returncode,0);(self.root/UPDATER).unlink();g=self.gate();self.assertNotEqual(g.returncode,0);self.assertFalse((self.home/'route-boundary').exists())
 def test_guard_rejects_tampered_installed_component(self):
  self.assertEqual(self.call().returncode,0);(self.root/TARGETS[0]).write_text('wrong');g=self.gate();self.assertNotEqual(g.returncode,0);self.assertFalse((self.home/'route-boundary').exists())
 def test_guard_rejects_non_executable_component(self):
  self.assertEqual(self.call().returncode,0);(self.root/UPDATER).chmod(0o644);g=self.gate();self.assertNotEqual(g.returncode,0);self.assertFalse((self.home/'route-boundary').exists())
 def test_guard_is_before_actual_capture_and_service_stop(self):
  source=(PLATFORM/UPDATER).read_text().split('request_process()\n',1)[1].split('\nrollback_current()',1)[0]
  self.assertLess(source.index('updater_platform_before_routes "$slot_root"'),source.index('    routes_capture ||'))
  self.assertLess(source.index('updater_platform_before_routes "$slot_root"'),source.index('    if ! services_stop_captured;'))
 def interrupted(self,phase='installing'):
  state=self.root/'opt/var/lib/broray-updater-preflight';backup=state/'platform-backup';backup.mkdir(parents=True)
  rows=[]
  for name in TARGETS:
   src=self.root/name;dst=backup/name;dst.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(src,dst)
   rows.append('present\t'+name+'\t'+digest(dst)+'\n')
  (backup/'inventory.tsv').write_text(''.join(rows));(state/'phase').write_text(phase+'\n');(state/'daemon-was-running').write_text('true\n')
  (self.home/'running').unlink(missing_ok=True)
  return state,backup
 def test_interrupted_delivery_restores_before_new_attempt(self):
  self.interrupted();(self.root/TARGETS[0]).write_text('partial-new-copy')
  p=self.call(env={**self.env,'BRORAY_HANDOFF_FAIL_AFTER':'2'});self.assertNotEqual(p.returncode,0,p.stderr)
  self.assertEqual(self.snapshot(),self.expected_old);self.assertTrue((self.home/'running').exists())
 def test_interrupted_preparing_partial_backup_can_resume(self):
  state,backup=self.interrupted('preparing');(backup/'inventory.tsv').write_text('incomplete')
  p=self.call();self.assertEqual(p.returncode,0,p.stderr);self.assertTrue((self.home/'running').exists())
 def test_corrupt_interrupted_backup_is_not_used(self):
  self.interrupted();(self.root/TARGETS[0]).write_text('partial-new-copy')
  backup=self.root/'opt/var/lib/broray-updater-preflight/platform-backup';(backup/TARGETS[0]).write_text('corrupted-backup')
  before=self.snapshot();p=self.call();self.assertNotEqual(p.returncode,0);self.assertEqual(self.snapshot(),before);self.assertTrue(backup.exists())
 def slot_fixture(self):
  slot=self.home/'authenticated-slot';app=slot/'app'
  shutil.copytree(self.payload,app/'share/updater-platform');(app/'lib').mkdir()
  shutil.copy2(HANDOFF,app/'lib/universal-platform-handoff.sh');(app/'lib/universal-platform-handoff.sh').chmod(0o755)
  shutil.copy2(ROOT/'runtime/app/lib/routes-api-operation.sh',app/'lib/routes-api-operation.sh')
  manifest=slot/'SHA256SUMS';manifest.write_text(''.join(digest(p)+'  '+p.relative_to(slot).as_posix()+'\n' for p in sorted(slot.rglob('*')) if p.is_file()))
  return slot,digest(manifest)
 def bootstrap(self,slot,sha):
  return subprocess.run(['/bin/ash',str(ROOT/'bootstrap/prepare-persistent-updater.sh'),str(slot),sha,self.sha],env=self.env,capture_output=True,text=True,timeout=45)
 def test_authenticated_bootstrap_prepares_before_request(self):
  slot,sha=self.slot_fixture();p=self.bootstrap(slot,sha);self.assertEqual(p.returncode,0,p.stderr)
  for name in TARGETS:self.assertEqual((self.root/name).read_bytes(),(self.payload/name).read_bytes())
  self.assertFalse((self.root/'opt/var/lib/broray-updater/request.lock').exists())
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
 def delivery_entry(self,action,slot,sha):
  ctl=self.home/'request-fixture';ctl.write_text('''#!/bin/ash
[ "$1" = request ] || exit 9
actual="$(sha256sum "$TEST_UPDATER_PATH")" || exit 8
[ "${actual%% *}" = "$TEST_UPDATER_SHA" ] || exit 7
[ -f "$TEST_ROOT/running" ] || exit 6
printf '%s\\n' "$2" >>"$TEST_ROOT/enqueued"
''');ctl.chmod(0o755)
  env={**self.env,'BRORAY_BOOTSTRAP_TEST_UPDATERCTL':str(ctl),'TEST_UPDATER_PATH':str(self.root/UPDATER),'TEST_UPDATER_SHA':digest(self.payload/UPDATER)}
  return subprocess.run(['/bin/ash',str(ROOT/'bootstrap/update-with-preflight.sh'),action,str(slot),sha,self.sha],env=env,capture_output=True,text=True,timeout=45)
 def test_update_and_reinstall_entry_prepare_before_enqueue(self):
  slot,sha=self.slot_fixture()
  for action in ['update','reinstall']:
   p=self.delivery_entry(action,slot,sha);self.assertEqual(p.returncode,0,p.stderr)
  self.assertEqual((self.home/'enqueued').read_text().splitlines(),['update','reinstall'])
 def test_failed_delivery_never_enqueues_update(self):
  slot,sha=self.slot_fixture();p=self.delivery_entry('update',slot,'0'*64)
  self.assert_no_delivery(p);self.assertFalse((self.home/'enqueued').exists())
if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(UpdaterPreflight))
 raise SystemExit(not result.wasSuccessful())
