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

if __name__=='__main__':unittest.main(verbosity=2,failfast=True)
