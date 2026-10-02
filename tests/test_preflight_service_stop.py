"""Legacy observation-only service stop must refuse before any signal or STOPPED.
No router access. All daemons and sentinels are owned test processes.
"""
from pathlib import Path
import os,json,subprocess,time,unittest,signal,threading,hashlib
from test_preflight_service_binding import ServiceBinding
from test_preflight_admission import CODE
SUP=Path('/work/.local/bin/linux-supervisor')
class BoundStop(ServiceBinding):
 def run_binding(self,tail="",prefix=""):
  body=prefix+'\nbroray_ops_preflight_admit "$TEST_SHA" || exit $?\nbroray_ops_preflight_stop_intent "$TEST_SHA" || exit $?\nbroray_ops_preflight_bind_service || exit $?\n'+tail
  return self.shell(body,timeout=60)
 def setUp(self):
  super().setUp();self.env['BRORAY_OPS_SUPERVISOR']=str(SUP)
  self.events=self.home/'events';self.env['SERVICE_EVENTS']=str(self.events)
  self.env['SERVICE_STATE']=str(self.updater)
 def start_service(self,ready=True,args=None,name='service',term=None,sleep='2'):
  script=self.live.parent/'libexec/broray-updater/broray-updater.sh';script.parent.mkdir(parents=True,exist_ok=True)
  term=term or 'printf "TERM\\n" >>"$SERVICE_EVENTS"; rm -f "$SERVICE_STATE/daemon.pid" "$SERVICE_STATE/daemon.ready"; rmdir "$SERVICE_STATE/daemon.lock"; exit 0'
  script.write_text('#!/bin/ash\ntrap \' '+term+' \' TERM\nwhile :; do sleep '+sleep+'; done\n');script.chmod(0o755)
  self.updater.mkdir(parents=True,exist_ok=True)
  p=subprocess.Popen(['/bin/ash',str(script),*(args or ['daemon'])],env=self.env,start_new_session=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  self.processes.append(p);time.sleep(.15);self.assertIsNone(p.poll())
  (self.updater/'daemon.pid').write_text(str(p.pid)+'\n');(self.updater/'daemon.lock').mkdir(exist_ok=True)
  if ready:(self.updater/'daemon.ready').write_text(str(p.pid)+'\n')
  return p,script
 def stop(self,extra='',prefix=''):
  return self.run_binding(extra+'\nbroray_ops_preflight_stop_service || exit $?\n',prefix)
 def assert_stopped(self):
  self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOPPED')
  self.assertTrue(self.lock.is_symlink());self.assertFalse((self.updater/'daemon.pid').exists())
 def native_marker(self):return json.loads((self.operation()/'platform-stop-supervision.json').read_text())
 def test_changed_pid_marker_refuses_before_signal(self):
  service,_=self.start_service();r=self.stop('printf 2147483646 >"$SERVICE_STATE/daemon.pid"')
  self.assertNotEqual(r.returncode,0);self.assertIsNone(service.poll());self.assertFalse(self.events.exists())
  self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT')
 def test_changed_birth_refuses_before_signal(self):
  service,_=self.start_service()
  r=self.stop('f="$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/platform-service.json"; jq \' .service.owner.startTicks="1" \' "$f" >"$f.tmp"; mv "$f.tmp" "$f"')
  self.assertNotEqual(r.returncode,0);self.assertIsNone(service.poll());self.assertFalse(self.events.exists())
 def test_changed_platform_refuses_and_detaches(self):
  service,_=self.start_service();r=self.stop('printf "\\n# changed\\n" >>"$BRORAY_ROOT/../libexec/broray-updater/broray-updater.sh"')
  self.assertNotEqual(r.returncode,0);self.assertIsNone(service.poll());self.assertFalse(self.events.exists())
  self.assertIn('TracerPid:\t0',Path(f'/proc/{service.pid}/status').read_text())
 def test_nonidle_child_is_never_signalled(self):
  service,_=self.start_service(sleep='20');r=self.stop()
  self.assertNotEqual(r.returncode,0);self.assertIsNone(service.poll());self.assertFalse(self.events.exists())
  self.assertIn('TracerPid:\t0',Path(f'/proc/{service.pid}/status').read_text())
 def test_live_service_cannot_be_marked_stopped(self):
  service,_=self.start_service()
  r=self.run_binding('broray_ops_call platform-preflight-service-stopped "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" "$BRORAY_PREFLIGHT_STOP_NONCE"')
  self.assertNotEqual(r.returncode,0);self.assertIsNone(service.poll());self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT')
 def test_fake_stopped_flag_does_not_bypass_service_check(self):
  service,_=self.start_service()
  r=self.run_binding('f="$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/state.json"; jq \' .platformPreflight.phase="STOPPED" \' "$f" >"$f.tmp"; mv "$f.tmp" "$f"; broray_ops_preflight_stop_service')
  self.assertNotEqual(r.returncode,0);self.assertIsNone(service.poll());self.assertFalse(self.events.exists());self.assertTrue(self.lock.is_symlink())
 def test_missing_binding_is_refused(self):
  service,_=self.start_service()
  r=self.shell('broray_ops_preflight_admit "$TEST_SHA" || exit $?; broray_ops_preflight_stop_intent "$TEST_SHA" || exit $?; broray_ops_preflight_stop_service',timeout=30)
  self.assertNotEqual(r.returncode,0);self.assertIsNone(service.poll());self.assertFalse(self.events.exists())
 def assert_bound_tree_quiet(self):
  marker=self.native_marker();op=self.operation()
  ledger=self.home/'ram/supervisors'/op.name/marker['supervisorId']/'children.json.current'
  children=json.loads(ledger.read_text())['children'];deadline=time.monotonic()+6
  while True:
   live=[]
   for row in children:
    try:fields=Path(f"/proc/{row['pid']}/stat").read_text().rsplit(') ',1)[1].split()
    except FileNotFoundError:continue
    if fields[19]==row['startTicks'] and fields[0] not in ['Z','X']:live.append(row['pid'])
   if not live:return
   self.assertLess(time.monotonic(),deadline,live);time.sleep(.05)
 def launch_stop(self,service):
  folder=self.home/'stop-worker';folder.mkdir();script=folder/'run.sh'
  script.write_text('set -eu\n. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"\nbroray_ops_preflight_admit "$TEST_SHA"\nbroray_ops_preflight_stop_intent "$TEST_SHA"\nbroray_ops_preflight_bind_service\nbroray_ops_preflight_stop_service\n')
  p=subprocess.Popen(['/bin/ash',str(script)],env=self.env,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,text=True,start_new_session=True)
  self.processes.append(p)
  self.waitfile(self.events,p,seconds=35)
  return p
 def assert_legacy_refusal(self,r,service=None):
  self.assertEqual(r.returncode,75,(r.stdout,r.stderr))
  self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT')
  self.assertTrue(self.lock.is_symlink());self.assertFalse(self.events.exists())
  record=json.loads((self.operation()/'platform-service.json').read_bytes())
  self.assertEqual(record['contract'],'broray-platform-service/1');self.assertFalse(record['signalsAuthorized'])
  for name in ['platform-service-authorized.json','platform-stop-supervision.json']:
   self.assertFalse((self.operation()/name).exists(),name)
  if service is not None:
   self.assertIsNone(service.poll());self.assertIn('TracerPid:\t0',Path(f'/proc/{service.pid}/status').read_text())
 def test_absent_legacy_service_requires_reboot_without_native(self):
  r=self.stop();self.assert_legacy_refusal(r)
  self.assertFalse((self.updater/'daemon.pid').exists())
  record=json.loads((self.operation()/'platform-service.json').read_bytes());self.assertIsNone(record['service']['owner'])
 def test_live_legacy_daemon_is_not_authorized_or_signalled(self):
  service,script=self.start_service();before=script.read_bytes();r=self.stop()
  self.assert_legacy_refusal(r,service);self.assertEqual(script.read_bytes(),before)
 def test_unrelated_xray_fixture_survives_legacy_refusal(self):
  x=subprocess.Popen(['/bin/sleep','60'],start_new_session=True);self.processes.append(x)
  service,_=self.start_service();r=self.stop();self.assert_legacy_refusal(r,service);self.assertIsNone(x.poll())
 def test_legacy_refusal_does_not_allow_generic_finish(self):
  service,_=self.start_service()
  r=self.run_binding('rc=0; broray_ops_preflight_stop_service || rc=$?; [ "$rc" = 75 ] || exit 91; broray_ops_finish completed')
  self.assertNotEqual(r.returncode,0,(r.stdout,r.stderr));self.assertTrue(self.lock.is_symlink())
  self.assertEqual(self.readstate()['platformPreflight']['phase'],'STOP_INTENT');self.assertIsNone(service.poll());self.assertFalse(self.events.exists())
 def test_repeated_legacy_stop_preserves_binding_without_signals(self):
  service,_=self.start_service()
  r=self.run_binding('''rc=0
broray_ops_preflight_stop_service || rc=$?
[ "$rc" = 75 ] || exit 91
cp "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/platform-service.json" "$TEST_HOME/binding-before"
rc=0; broray_ops_preflight_stop_service || rc=$?
exit "$rc"
''')
  self.assert_legacy_refusal(r,service)
  self.assertEqual((self.home/'binding-before').read_bytes(),(self.operation()/'platform-service.json').read_bytes())
 def test_legacy_ignored_term_is_never_signalled(self):
  service,_=self.start_service(term=':');start=time.monotonic();r=self.stop()
  self.assert_legacy_refusal(r,service);self.assertLess(time.monotonic()-start,38)
 def test_legacy_forking_term_handler_is_never_invoked(self):
  term='printf "TERM\\n" >>"$SERVICE_EVENTS"; /bin/ash -c "sleep .1"; exit 0'
  service,_=self.start_service(term=term);r=self.stop();self.assert_legacy_refusal(r,service)
 def test_lost_legacy_refusal_reply_cannot_authorize_retry(self):
  source=(CODE/'lib/operation-client.sh').read_text().replace('broray_ops_call()','broray_ops_call_original()',1)
  shim=self.home/'lost-refusal.sh';shim.write_text(source+'''
broray_ops_call() {
 local output rc
 rc=0; output="$(broray_ops_call_original "$@")" || rc=$?
 if [ "$1" = platform-preflight-stop-target ] && [ "$rc" = 75 ] && [ ! -e "$TEST_HOME/dropped-refusal" ]; then
  : >"$TEST_HOME/dropped-refusal"; return 75
 fi
 [ -z "$output" ] || printf '%s\\n' "$output"
 return "$rc"
}
''')
  service,_=self.start_service()
  r=self.run_binding('rc=0; broray_ops_preflight_stop_service || rc=$?; [ "$rc" = 75 ] || exit 91; broray_ops_preflight_stop_service',prefix='. "'+str(shim)+'"')
  self.assert_legacy_refusal(r,service);self.assertTrue((self.home/'dropped-refusal').exists())
 def test_current_daemon_rejects_unsupervised_direct_launch(self):
  script=self.live.parent/'libexec/broray-updater/broray-updater.sh';script.parent.mkdir(parents=True,exist_ok=True)
  original=(CODE/'share/updater-platform/opt/libexec/broray-updater/broray-updater.sh').read_bytes()
  script.write_bytes(original);script.chmod(0o755);before=self.freeze(self.root)
  env={**self.env,'BRORAY_UPDATER_ROOT_PREFIX':str(self.root),'BRORAY_UPDATER_PATH':'/usr/bin:/bin:/usr/sbin:/sbin','BRORAY_UPDATER_ASH':'/bin/ash'}
  r=subprocess.run(['/bin/ash',str(script),'daemon'],env=env,capture_output=True,text=True,timeout=20)
  self.assertEqual(r.returncode,75,r.stdout+r.stderr);self.assertIn('UPDATER_SUPERVISED_LAUNCH_REQUIRED',r.stderr)
  self.assertEqual(self.freeze(self.root),before);self.assertEqual(script.read_bytes(),original)

if __name__=='__main__':
 names=sorted(n for n in BoundStop.__dict__ if n.startswith('test_'))
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(BoundStop(n) for n in names))
 print('LEGACY_REFUSAL_RECEIPT '+json.dumps({'status':'PASS' if r.wasSuccessful() else 'FAIL','testsRun':r.testsRun,'routerAccess':False,'currentPositiveStopCoverage':'generation stop/authorization + installed generation stop'}),flush=True)
 raise SystemExit(not r.wasSuccessful())
