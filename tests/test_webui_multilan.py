"""Actual S25 function flow; daemon/HTTP/process identity mocked; no router writes."""
import json,os,subprocess,unittest
from pathlib import Path
from test_network_multilan import Lan,ROOT
class Web(unittest.TestCase):
 fixture=Lan.fixture
 def setUp(self):
  Lan.setUp(self)
  for name in ['broray-runtime-prepare','lighttpd']:
   p=self.app/'bin'/name
   p.write_text('#!/bin/ash\n'+('exit 0\n' if name!='lighttpd' else 'if [ "$1" = -tt ];then [ "${TEST_CONFIG_FAIL:-0}" = 0 ];exit $?;fi\necho started >>"$BRORAY_ROOT/launches"\n'))
   p.chmod(0o755)
  self.defs=(ROOT/'runtime/init/S25broray-web').read_text().split('\ncase "${1:-}" in',1)[0]
  (self.app/'init-defs.sh').write_text(self.defs)
  self.env.update({'BRORAY_WEB_BASE':str(self.app),'BRORAY_WEB_SOURCE_BINARY':str(self.app/'bin/lighttpd'),'BRORAY_WEB_RUNTIME_BINARY':str(self.app/'runtime/broray-lighttpd')})
 def shell(self,command='start',extra=''):
  p=subprocess.run(['/bin/ash','-c','. "$BRORAY_ROOT/init-defs.sh"\nsleep(){ :; };status(){ return 0; };\n'+extra+'\n'+command],env=self.env,capture_output=True,timeout=25);return p
 def test_00_rollback_prepares_under_scope_but_daemon_does_not_inherit_it(self):
  Path('/opt/bin').mkdir(parents=True,exist_ok=True)
  if not Path('/opt/bin/ash').exists():Path('/opt/bin/ash').symlink_to('/bin/ash')
  guard=ROOT.parent/'.local/bin/linux-guard';state=self.app/'state';state.mkdir(mode=0o700)
  self.env.update({'BRORAY_OPS_GUARD':str(guard),'BRORAY_STATE_ROOT':str(state)})
  (self.app/'bin/broray-runtime-prepare').write_text('''#!/bin/ash
"$BRORAY_OPS_GUARD" --assert-held "$BRORAY_STATE_ROOT/operations.guard" || exit 90
echo prepared >"$BRORAY_ROOT/prepared"
''')
  (self.app/'bin/lighttpd').write_text('''#!/bin/ash
[ "$1" != -tt ] || exit 0
[ -z "${BRORAY_OPS_SCOPE_FD:-}" ] || exit 91
[ ! -e /proc/self/fd/6 ] || exit 92
echo started >"$BRORAY_ROOT/launches"
''')
  init=self.app/'init';init.mkdir();launcher=init/'S25broray-web'
  launcher.write_text('#!/bin/ash\n. "$BRORAY_ROOT/init-defs.sh"\nsleep(){ :; };status(){ return 0; };start\n');launcher.chmod(0o700)
  page=(ROOT/'runtime/app/lib/broray-page.sh').read_text()
  restore=page.split('    broray_system_uninstall_aux_services_restore() {',1)[1].split('    broray_system_uninstall_auth_retire()',1)[0]
  release=page.split('broray_system_uninstall_scope_release() {',1)[1].split('broray_system_uninstall_start()',1)[0]
  services=self.app/'services';services.write_text('S25broray-web\n')
  self.env.update({'BRORAY_INIT_ROOT':str(init),'BRORAY_LOG':str(self.app/'restore.log'),'uninstall_services':str(services)})
  code='broray_system_uninstall_scope_release() {'+release+'\nbroray_system_uninstall_aux_services_restore() {'+restore+'\nbroray_system_uninstall_aux_services_restore\n'
  p=subprocess.run([str(guard),'--scope',str(state/'operations.guard'),'/bin/ash','-c',code],env=self.env,capture_output=True,timeout=25)
  self.assertEqual(p.returncode,0,(p.stdout,p.stderr,(self.app/'restore.log').read_text()))
  self.assertEqual((self.app/'prepared').read_text(),'prepared\n');self.assertEqual((self.app/'launches').read_text(),'started\n')
 def test_start_two_private_keeps_saved_bind(self):
  before=self.settings.read_bytes();r=self.shell();self.assertEqual(r.returncode,0,(r.stdout,r.stderr));self.assertEqual((self.app/'run/lan-ip').read_text(),'192.168.2.1\n');self.assertIn('server.bind = "192.168.2.1"',self.conf.read_text());self.assertEqual(self.settings.read_bytes(),before)
 def test_start_explicit_web_pin_keeps_socks(self):
  self.settings.write_text('{"listenAddress":"192.168.2.1","webuiLanAddress":"192.168.3.1"}');before=self.settings.read_bytes();r=self.shell();self.assertEqual(r.returncode,0,(r.stdout,r.stderr));self.assertIn('server.bind = "192.168.3.1"',self.conf.read_text());self.assertEqual(self.settings.read_bytes(),before)
 def test_failed_config_test_preserves_old_conf(self):
  self.settings.write_text('{"webuiLanAddress":"192.168.3.1"}');self.env['TEST_CONFIG_FAIL']='1';before=self.conf.read_bytes();r=self.shell();self.assertNotEqual(r.returncode,0);self.assertEqual(self.conf.read_bytes(),before);self.assertFalse((self.app/'launches').exists())
 def test_duplicate_bind_not_overwritten(self):
  self.conf.write_text(self.conf.read_text()+'server.bind = "192.168.3.1"\n');before=self.conf.read_bytes();r=self.shell();self.assertNotEqual(r.returncode,0);self.assertEqual(self.conf.read_bytes(),before)
 def test_existing_confirmed_process_not_restarted(self):
  (self.app/'run/lan-ip').write_text('192.168.2.1\n');(self.app/'run/lighttpd.pid').write_text('12345\n');before=self.conf.read_bytes();r=self.shell(extra='broray_web_process_identity_twice(){ return 0; };broray_web_http_healthy(){ return 0; }');self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(self.conf.read_bytes(),before);self.assertFalse((self.app/'launches').exists())
 def test_alive_foreign_pid_not_removed(self):
  p=self.app/'run/lighttpd.pid';p.write_text('12345\n');before=self.conf.read_bytes();r=self.shell(extra='broray_web_process_identity_twice(){ return 1; };kill(){ return 0; }');self.assertNotEqual(r.returncode,0);self.assertEqual(p.read_text(),'12345\n');self.assertEqual(self.conf.read_bytes(),before)
 def test_running_bind_change_requires_restart_without_rewrite(self):
  self.settings.write_text('{"webuiLanAddress":"192.168.3.1"}');(self.app/'run/lan-ip').write_text('192.168.2.1\n');(self.app/'run/lighttpd.pid').write_text('12345\n');before=self.conf.read_bytes();r=self.shell(extra='broray_web_process_identity_twice(){ return 0; };broray_web_http_healthy(){ return 0; }');self.assertNotEqual(r.returncode,0);self.assertEqual((self.app/'run/lan-ip').read_text(),'192.168.2.1\n');self.assertEqual(self.conf.read_bytes(),before)
 def test_reboot_missing_run_retains_bind(self):
  self.shell();(self.app/'run/lan-ip').unlink();before=self.settings.read_bytes();r=self.shell();self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(self.settings.read_bytes(),before);self.assertEqual((self.app/'run/lan-ip').read_text(),'192.168.2.1\n')
 def test_explicit_pin_reclassified_protected_blocks_start(self):
  self.settings.write_text('{"webuiLanAddress":"192.168.2.1"}');self.fixture(('protected','private'));before=self.conf.read_bytes();r=self.shell();self.assertNotEqual(r.returncode,0);self.assertEqual(self.conf.read_bytes(),before);self.assertFalse((self.app/'launches').exists())
 def test_setup_and_postcheck_use_web_ip_not_socks(self):
  self.settings.write_text('{"listenAddress":"192.168.2.1","webuiLanAddress":"192.168.3.1"}');before=self.settings.read_bytes()
  for name,func,env in [('package-setup.sh','broray_setup_web_lan_ip','BRORAY_SETUP_TARGET'),('package-transaction.sh','broray_tx_web_lan_ip','BRORAY_TX_APP_ROOT')]:
   with self.subTest(name=name):
    src=(ROOT/'runtime/app/lib'/name).read_text();start=src.index(func+'()\n');end=src.index('\n)\n',start)+3;script=src[start:end]+'\n'+env+'="$BRORAY_ROOT"\n'+func
    p=subprocess.run(['/bin/ash','-c',script],env=self.env,capture_output=True,timeout=25);self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(p.stdout,b'192.168.3.1\n')
  self.assertEqual(self.settings.read_bytes(),before)
 def test_stop_and_identity_functions_unchanged(self):
  old=(Path(os.environ['BRORAY_TEST_BASELINE'])/'runtime/init/S25broray-web').read_text();new=self.defs
  for start,end in [('stop()\n','restart()\n'),('broray_web_process_signature_once()\n','broray_web_http_healthy()\n')]:self.assertEqual(old[old.index(start):old.index(end)],new[new.index(start):new.index(end)])
if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Web))
 print('STAGE11_WEB_REPORT='+json.dumps({'testsRun':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'routerAccessed':False,'daemonAndHttp':'fixtures','processIdentity':'stubbed in lifecycle scenarios'}));raise SystemExit(not result.wasSuccessful())
