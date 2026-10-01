"""Exact network source in ash/dash; ndmc and ip are synthetic, no router."""
import json,os,shutil,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
class Lan(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='broray-lan-');self.addCleanup(self.tmp.cleanup);self.app=Path(self.tmp.name)
  for d in ['lib','bin','tmp','run','config/system']:(self.app/d).mkdir(parents=True,exist_ok=True)
  shutil.copyfile(ROOT/'runtime/app/lib/network.sh',self.app/'lib/network.sh')
  self.settings=self.app/'config/system/settings.json';self.settings.write_text(json.dumps({'listenAddress':'192.168.2.1','keep':'UNCHANGED'}))
  self.conf=self.app/'config/lighttpd.conf';self.conf.write_text('server.bind = "192.168.2.1"\nserver.port = 8080\n')
  self.env={**os.environ,'BRORAY_ROOT':str(self.app),'BRORAY_NETWORK_ROOT':str(self.app),'PATH':str(self.app/'bin')+':/usr/bin:/bin:/usr/sbin:/sbin'}
  self.env.pop('BRORAY_LAN_IP_OVERRIDE',None);self.env.pop('BRORAY_WEB_CONF',None)
  for name,body in [('ndmc','[ "$*" = "-c show running-config" ] || exit 99\ncat "$BRORAY_ROOT/running"\n[ "${TEST_NDMC_RC:-0}" = 0 ]'),('ip','[ "$*" = "-4 addr show" ] || exit 99\ncat "$BRORAY_ROOT/live"\n[ "${TEST_IP_RC:-0}" = 0 ]')]:
   p=self.app/'bin'/name;p.write_text('#!/bin/ash\n'+body+'\n');p.chmod(0o755)
  self.fixture()
  helper=self.app/'bin/broray-system-ndmc'
  helper.write_text((ROOT/'runtime/app/bin/broray-system-ndmc').read_text().replace('/bin/ndmc',str(self.app/'bin/ndmc')))
  helper.chmod(0o755)
 def fixture(self,roles=('private','private'),addresses=('192.168.2.1','192.168.3.1')):
  self.running=''.join('interface Segment'+str(i)+'\n    security-level '+role+'\n    ip address '+addr+' 255.255.255.0\n!\n' for i,(role,addr) in enumerate(zip(roles,addresses)))
  (self.app/'running').write_text(self.running)
  self.live='1: lo: <UP>\n    inet 127.0.0.1/8 scope host lo\n'+''.join(str(i+2)+': br'+str(i)+': <UP>\n    inet '+addr+'/24 scope global br'+str(i)+'\n' for i,addr in enumerate(addresses))
  (self.app/'live').write_text(self.live)
 def run_code(self,code='broray_detect_lan_ip'):
  p=subprocess.run(['/bin/ash','-c','. "$BRORAY_ROOT/lib/network.sh"; '+code],env=self.env,capture_output=True,timeout=25)
  self.assertEqual(list((self.app/'tmp').iterdir()),[],p.stderr);return p
 def detect(self,web=True,ok=True):
  p=self.run_code('if command -v broray_detect_webui_lan_ip >/dev/null; then broray_detect_webui_lan_ip; else broray_detect_lan_ip; fi' if web else 'broray_detect_lan_ip')
  self.assertEqual(p.returncode,0 if ok else 1,(p.stdout,p.stderr));return p.stdout.decode().strip() if ok else p.stderr.decode()
 def test_regression_two_private_saved_address(self):self.assertEqual(self.detect(),'192.168.2.1')
 def test_regression_override_not_private_is_rejected(self):self.fixture(('protected','private'));self.env['BRORAY_LAN_IP_OVERRIDE']='192.168.2.1';self.detect(ok=False)
 def test_three_private_preserves_bind(self):self.fixture(('private',)*3,('192.168.2.1','192.168.3.1','10.2.0.1'));self.assertEqual(self.detect(),'192.168.2.1')
 def test_stored_webui_pin_separate_from_socks(self):
  self.settings.write_text('{"listenAddress":"192.168.2.1","webuiLanAddress":"192.168.3.1"}');before=self.settings.read_bytes();self.assertEqual(self.detect(),'192.168.3.1');self.assertEqual(self.detect(web=False),'192.168.2.1');self.assertEqual(self.settings.read_bytes(),before)
 def test_persistent_pin_without_environment(self):
  self.settings.write_text('{"webuiLanAddress":"192.168.3.1"}');self.assertEqual(self.detect(),'192.168.3.1');self.assertEqual(self.detect(),'192.168.3.1')
 def test_explicit_pin_unavailable_does_not_fallback(self):self.settings.write_text('{"webuiLanAddress":"192.168.9.1"}');self.assertIn('PIN_NOT_PRIVATE_OR_LIVE',self.detect(ok=False))
 def test_conflicting_override_and_pin(self):self.settings.write_text('{"webuiLanAddress":"192.168.3.1"}');self.env['BRORAY_LAN_IP_OVERRIDE']='192.168.2.1';self.assertIn('EXPLICIT_PIN_CONFLICT',self.detect(ok=False))
 def test_existing_override_kept(self):self.env['BRORAY_LAN_IP_OVERRIDE']='192.168.2.1';self.assertEqual(self.detect(),'192.168.2.1')
 def test_override_must_not_bypass_failed_snapshot(self):self.env.update({'BRORAY_LAN_IP_OVERRIDE':'192.168.2.1','TEST_NDMC_RC':'1'});self.detect(ok=False)
 def test_transport_uses_settings_not_web_conf(self):self.conf.write_text('server.bind = "192.168.3.1"\n');self.assertEqual(self.detect(),'192.168.3.1');self.assertEqual(self.detect(web=False),'192.168.2.1')
 def test_missing_conf_falls_back_to_settings(self):self.conf.unlink();self.assertEqual(self.detect(),'192.168.2.1')
 def test_single_private_autodetect(self):self.settings.unlink();self.conf.unlink();self.fixture(('private','protected'));self.assertEqual(self.detect(),'192.168.2.1')
 def test_all_candidates_equal_no_guess(self):self.settings.unlink();self.conf.unlink();self.assertIn('LAN_SELECTION_REQUIRED',self.detect(ok=False))
 def test_stale_auto_address_one_replacement(self):self.fixture(('private',),('192.168.5.1',));self.assertEqual(self.detect(),'192.168.5.1')
 def test_stale_auto_address_multiple_replacements(self):self.fixture(('private','private'),('192.168.5.1','192.168.6.1'));self.assertIn('LAN_SELECTION_REQUIRED',self.detect(ok=False))
 def test_stale_hints_not_override_private_role(self):self.fixture(('public','protected'));self.detect(ok=False)
 def test_repeated_live_ip_fails_explicit_pin(self):self.env['BRORAY_LAN_IP_OVERRIDE']='192.168.2.1';(self.app/'live').write_text(self.live+'    inet 192.168.2.1/24 scope global extra\n');self.detect(ok=False)
 def test_same_address_public_and_private_fails(self):self.fixture(('private','public'),('192.168.2.1','192.168.2.1'));self.detect(ok=False)
 def test_duplicate_security_role_not_private(self):(self.app/'running').write_text(self.running.replace('security-level private','security-level private\n    security-level public'));self.detect(ok=False)
 def test_failed_ip_does_not_accept_partial_output(self):self.env['TEST_IP_RC']='1';self.detect(ok=False)
 def test_stderr_refuses(self):p=self.app/'bin/ndmc';p.write_text(p.read_text()+'echo warning >&2\n');self.detect(ok=False)
 def test_injected_explicit_pin_refused(self):self.settings.write_text(json.dumps({'webuiLanAddress':'192.168.2.1\nserver.bind="0.0.0.0"'}));self.detect(ok=False)
 def test_bad_settings_never_silently_ignored(self):self.settings.write_text('broken');self.assertIn('SETTINGS_INVALID',self.detect(ok=False))
 def test_settings_symlink_refused(self):target=self.app/'private';self.settings.rename(target);self.settings.symlink_to(target);self.detect(ok=False)
 def test_leading_zero_address_refused(self):self.env['BRORAY_LAN_IP_OVERRIDE']='192.168.002.1';self.detect(ok=False)
 def test_wildcard_loopback_or_public_never_admitted(self):
  for v in ['0.0.0.0','127.0.0.1','8.8.8.8']:
   with self.subTest(v=v):self.env['BRORAY_LAN_IP_OVERRIDE']=v;self.detect(ok=False)
 def test_invalid_explicit_type_refused(self):self.settings.write_text('{"webuiLanAddress":true}');self.detect(ok=False)
 def test_atomic_runtime_write(self):p=self.run_code('broray_save_lan_ip');self.assertEqual(p.returncode,0,p.stderr);self.assertEqual((self.app/'run/lan-ip').read_text(),'192.168.2.1\n');self.assertEqual((self.app/'run/lan-ip').stat().st_mode&0o777,0o600)
 def test_runtime_symlink_not_overwritten(self):target=self.app/'foreign';target.write_text('KEEP');(self.app/'run/lan-ip').symlink_to(target);p=self.run_code('broray_save_lan_ip');self.assertNotEqual(p.returncode,0);self.assertEqual(target.read_text(),'KEEP')
 def test_runtime_cache_not_used_as_authority(self):self.settings.unlink();self.conf.unlink();(self.app/'run/lan-ip').write_text('192.168.3.1');self.detect(ok=False)
 def test_two_settings_documents_refused(self):self.settings.write_text('{}\n{}');self.detect(ok=False)
 def test_no_raw_config_on_failure(self):self.env['TEST_NDMC_RC']='1';(self.app/'running').write_text(self.running+'password PRIVATE_CANARY\n');self.assertNotIn('PRIVATE_CANARY',self.detect(ok=False))
if __name__=='__main__':
 tests=list(unittest.defaultTestLoader.loadTestsFromTestCase(Lan))
 if os.environ.get('STAGE11_REPRO')=='1':tests=[t for t in tests if 'test_regression_' in t.id()]
 result=unittest.TextTestRunner(verbosity=2,failfast=False).run(unittest.TestSuite(tests))
 print('STAGE11_NETWORK_REPORT='+json.dumps({'testsRun':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'skipped':len(result.skipped),'ndmcAndIp':'synthetic fixtures','routerAccessed':False}))
 raise SystemExit(not result.wasSuccessful())
