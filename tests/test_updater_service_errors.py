"""Failure to observe a service is not evidence that the service was stopped."""
from pathlib import Path
import hashlib,os,subprocess,tempfile,unittest

ROOT=Path(os.environ.get('BRORAY_TEST_ROOT','/work/implementation'))
SOURCE=ROOT/'runtime/app/share/updater-platform/opt/libexec/broray-updater/broray-updater.sh'
class ServiceErrors(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='service-status-');self.addCleanup(self.tmp.cleanup);self.home=Path(self.tmp.name)
  source=SOURCE.read_text();start=source.index('\nservice_call()\n')+1;end=source.index('\n# A service restart',start);functions=source[start:end]
  print('SERVICE_STATUS_SOURCE '+hashlib.sha256(functions.encode()).hexdigest(),flush=True)
  self.script=self.home/'probe.sh';self.script.write_text('set -u\n'+functions+'\nservices_capture\n');self.hook=self.home/'hook';self.hook.write_text('#!/bin/ash\nexit 75\n');self.hook.chmod(0o755)
  self.env={**os.environ,'SERVICE_HOOK':str(self.hook),'CURRENT_OPERATION_DIR':str(self.home),'CURRENT_OPERATION_LOG':str(self.home/'operation.log')}
 def run_capture(self,extra=None):return subprocess.run(['/bin/ash',str(self.script)],env={**self.env,**(extra or {})},capture_output=True,text=True,timeout=5)
 def test_unknown_status_refused(self):
  r=self.run_capture();self.assertEqual(r.returncode,75,'unknown service state was recorded as stopped')
 def test_supervised_without_launcher_is_not_all_stopped(self):
  r=self.run_capture({'BRORAY_UPDATER_GENERATION':'generation-one'});self.assertEqual(r.returncode,75,'missing launcher must abort capture')
 def test_running_and_stopped_are_recorded(self):
  self.hook.write_text('#!/bin/ash\n[ "$2" = S24broray ] && exit 0\nexit 1\n');r=self.run_capture();self.assertEqual(r.returncode,0,r.stderr)
  rows=(self.home/'services.tsv').read_text().splitlines();self.assertEqual(len(rows),5);self.assertIn('S24broray\trunning',rows);self.assertEqual(sum(x.endswith('\tstopped') for x in rows),4)
 def test_other_status_error_is_not_stopped(self):
  self.hook.write_text('#!/bin/ash\nexit 2\n');self.assertEqual(self.run_capture().returncode,2)

class TargetWebRecovery(unittest.TestCase):
 """Real updater service orchestration; init/HTTP are a controlled fixture."""
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='target-web-');self.addCleanup(self.tmp.cleanup);self.home=Path(self.tmp.name)
  self.source=SOURCE.read_text()
  first=self.source.index('\nservice_call()\n');last=self.source.index('\n# A service restart',first)
  start=self.source.index('\nservices_stop_captured()\n');end=self.source.index('\nwebui_backend_health()\n',start)
  self.functions=self.source[first:last]+self.source[start:end]
  self.table=self.home/'services.tsv';self.table.write_text('S23broray-monitor\tstopped\nS24broray\tstopped\nS25broray-web\tstopped\nS27broray-auto-switch\tstopped\nS28broray-subscriptions\tstopped\n')
  self.before=self.table.read_bytes();self.log=self.home/'calls';self.hook=self.home/'hook'
  self.hook.write_text('''#!/bin/ash
printf '%s %s\\n' "$1" "$2" >>"$TEST_HOME/calls"
case "$1" in
 start) [ ! -e "$TEST_HOME/fail-start" ] || exit 74; touch "$TEST_HOME/$2.running" ;;
 stop) rm -f "$TEST_HOME/$2.running" ;;
 status) [ -e "$TEST_HOME/$2.running" ] ;;
 *) exit 2 ;;
esac
''');self.hook.chmod(0o755)
  self.env={**os.environ,'SERVICE_HOOK':str(self.hook),'CURRENT_OPERATION_DIR':str(self.home),'CURRENT_OPERATION_LOG':str(self.home/'operation.log'),'TEST_HOME':str(self.home)}
 def run_flow(self,command):
  code='set -u\n'+self.functions+'\nroutes_reconcile_wait(){ return 0; }; routes_restore_captured(){ return 0; };\n'+command+'\n'
  p=subprocess.run(['/bin/ash','-c',code],env=self.env,capture_output=True,text=True,timeout=5)
  self.assertEqual(self.table.read_bytes(),self.before,'rollback source state must remain exact')
  return p
 def calls(self):return self.log.read_text().splitlines() if self.log.exists() else []
 def test_00_stopped_legacy_web_must_start_on_target(self):
  p=self.run_flow('services_start_captured target');self.assertEqual(p.returncode,0,p.stderr)
  self.assertEqual(self.calls(),['start S25broray-web'])
 def test_target_health_rejects_web_still_down(self):
  self.assertNotEqual(self.run_flow('services_health_captured target').returncode,0)
  self.assertEqual(self.calls(),['status S25broray-web'])
 def test_target_start_error_propagates(self):
  (self.home/'fail-start').touch();self.assertNotEqual(self.run_flow('services_start_captured target').returncode,0)
 def test_failed_service_retains_its_name_and_rc(self):
  (self.home/'fail-start').touch()
  p=self.run_flow('services_start_captured target; rc=$?; printf "code=%s\\n" "${SERVICE_START_ERROR_CODE:-unset}"; exit "$rc"')
  self.assertNotEqual(p.returncode,0);self.assertIn('code=SERVICE_START_FAILED',p.stdout)
  self.assertIn('service=S25broray-web rc=74',(self.home/'operation.log').read_text())
 def test_reconcile_failure_is_distinct_and_does_not_restore(self):
  p=self.run_flow('routes_reconcile_wait(){ return 73; }; routes_restore_captured(){ touch "$TEST_HOME/unexpected-restore"; }; services_start_captured target; rc=$?; printf "code=%s\\n" "${SERVICE_START_ERROR_CODE:-unset}"; exit "$rc"')
  self.assertNotEqual(p.returncode,0);self.assertIn('code=ROUTE_RECONCILE_FAILED',p.stdout)
  self.assertFalse((self.home/'unexpected-restore').exists())
 def test_route_restore_failure_has_distinct_identity(self):
  p=self.run_flow('routes_restore_captured(){ return 74; }; services_start_captured target; rc=$?; printf "code=%s\\n" "${SERVICE_START_ERROR_CODE:-unset}"; exit "$rc"')
  self.assertNotEqual(p.returncode,0);self.assertIn('code=ROUTE_RESTORE_FAILED',p.stdout)
 def test_success_clears_previous_failure_identity(self):
  p=self.run_flow('SERVICE_START_ERROR_CODE=OLD_ERROR; services_start_captured target; printf "code=%s\\n" "$SERVICE_START_ERROR_CODE"')
  self.assertEqual(p.returncode,0,p.stderr);self.assertIn('code=\n',p.stdout)
 def test_restore_preserves_all_previously_stopped_services(self):
  p=self.run_flow('services_start_captured && services_health_captured');self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(self.calls(),[])
 def test_rollback_stops_target_web_without_starting_old_stopped_web(self):
  p=self.run_flow('services_start_captured target && services_stop_captured target && services_start_captured')
  self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(self.calls(),['start S25broray-web','stop S25broray-web'])
  self.assertFalse((self.home/'S25broray-web.running').exists())
 def test_running_web_starts_once(self):
  self.table.write_text(self.table.read_text().replace('S25broray-web\tstopped','S25broray-web\trunning'));self.before=self.table.read_bytes()
  p=self.run_flow('services_start_captured target && services_health_captured target');self.assertEqual(p.returncode,0,p.stderr)
  self.assertEqual(self.calls(),['start S25broray-web','status S25broray-web'])
 def test_missing_service_record_never_authorizes_target_start(self):
  self.table.write_text('S24broray\tstopped\n');self.before=self.table.read_bytes()
  self.assertNotEqual(self.run_flow('services_start_captured target').returncode,0);self.assertEqual(self.calls(),[])
 def test_missing_service_record_never_passes_target_health(self):
  self.table.write_text('S24broray\tstopped\n');self.before=self.table.read_bytes()
  self.assertNotEqual(self.run_flow('services_health_captured target').returncode,0)
 def test_whole_updater_shell_syntax_and_manifest(self):
  p=subprocess.run(['/bin/ash','-n',str(SOURCE)],capture_output=True,text=True,timeout=5);self.assertEqual(p.returncode,0,p.stderr)
  root=SOURCE.parents[3]
  for row in (root/'SHA256SUMS').read_text().splitlines():
   digest,name=row.split('  ',1);self.assertEqual(hashlib.sha256((root/name).read_bytes()).hexdigest(),digest,name)
 def test_target_mode_bound_to_update_recovery_and_health(self):
  self.assertIn('if ! services_start_captured target; then',self.source)
  self.assertIn('slot_health "$target_slot" target',self.source)
  self.assertIn('services_start_captured target >>"$CURRENT_OPERATION_LOG" 2>&1',self.source)
  self.assertIn('services_stop_captured target >>"$CURRENT_OPERATION_LOG" 2>&1',self.source)
  self.assertIn('services_health_captured "${2:-}" || return 1',self.source)

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
