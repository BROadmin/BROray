import json,os,shutil,subprocess,tempfile,time,unittest
from pathlib import Path
APP=Path('/field/runtime/app')
class LighttpdRestore(unittest.TestCase):
 def setUp(self):
  self.t=tempfile.TemporaryDirectory();self.addCleanup(self.t.cleanup);self.p=Path(self.t.name)
  self.app=self.p/'app';shutil.copytree(APP/'lib',self.app/'lib')
  self.env={**os.environ,'BRORAY_ROOT':str(self.app),'BRORAY_BASE':str(self.app),'AUDIT':str(self.p)}
 def shell(self,code):return subprocess.run(['/bin/ash','-c',code],env=self.env,capture_output=True,text=True,timeout=20)
 def test_lighttpd_failed_restore_discards_original(self):
  init=self.p/'S80lighttpd';init.write_text('ENABLED=yes\nORIGINAL\n')
  self.env.update(BRORAY_LIGHTTPD_INIT=str(init),BRORAY_LIGHTTPD_GUARD_ROOT=str(self.p/'guard'))
  r=self.shell('''. "$BRORAY_ROOT/lib/lighttpd-guard.sh"
# Focus on the failure boundary after successful admission; validators are doubles.
broray_lighttpd_guard_baseline_valid() { guard_baseline_lighttpd=absent; guard_baseline_cgi=absent; guard_baseline_port80=none; }
broray_lighttpd_guard_transient_metadata_valid() { guard_info_version=fixture; return 0; }
broray_lighttpd_guard_files_valid() { return 0; }
broray_lighttpd_guard_receipt_valid() { return 0; }
broray_lighttpd_guard_disable_init() { echo ENABLED=no >"$BRORAY_LIGHTTPD_INIT"; }
broray_lighttpd_guard_stop_default() { return 1; }
broray_lighttpd_guard_adopt_transient "$AUDIT/baseline"
''')
  self.assertEqual(r.returncode,1,r.stderr)
  self.assertEqual(init.read_text(),'ENABLED=no\n');self.assertTrue((self.p/'guard').exists()); self.assertEqual((self.p/'guard/S80lighttpd.original').read_text(),'ENABLED=yes\nORIGINAL\n')
