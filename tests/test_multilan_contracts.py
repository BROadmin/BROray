"""Syntax and setup contracts; no full setup/updater script execution."""
import json,subprocess,unittest,os
from pathlib import Path
from test_webui_multilan import Web
from test_network_multilan import ROOT
class Contracts(unittest.TestCase):
 fixture=Web.fixture;setUp=Web.setUp
 def function(self,file,name):
  src=(ROOT/file).read_text();a=src.index(name+'()\n');b=src.index('\n}\n',a)+3;return src[a:b]
 def shell(self,script):return subprocess.run(['/bin/ash','-c',script],env=self.env,capture_output=True,timeout=25)
 def test_four_files_syntax(self):
  for rel in ['runtime/app/lib/network.sh','runtime/app/lib/package-setup.sh','runtime/app/lib/package-transaction.sh','runtime/init/S25broray-web']:
   with self.subTest(rel=rel):self.assertEqual(subprocess.run(['/bin/ash','-n',str(ROOT/rel)],capture_output=True,timeout=15).returncode,0)
 def test_setup_skip_performs_no_discovery(self):
  script=self.function('runtime/app/lib/package-setup.sh','configure_web_proxy')+'\nBRORAY_SETUP_SKIP_KEENETIC=1\nbroray_setup_web_lan_ip(){ echo BAD >&2;return 1; };configure_web_proxy'
  p=self.shell(script);self.assertEqual(p.returncode,0,p.stderr);self.assertNotIn(b'BAD',p.stderr)
 def test_initial_xray_function_unchanged(self):
  old=(Path(os.environ['BRORAY_TEST_BASELINE'])/'runtime/app/lib/package-setup.sh').read_text();new=(ROOT/'runtime/app/lib/package-setup.sh').read_text();a='configure_initial_xray()\n';b='configure_auto_switch()\n';self.assertEqual(old[old.index(a):old.index(b)],new[new.index(a):new.index(b)])
 def test_notice_uses_web_binding(self):
  self.settings.write_text('{"listenAddress":"192.168.2.1","webuiLanAddress":"192.168.3.1"}');script=self.function('runtime/app/lib/package-setup.sh','print_result')+'\nBRORAY_SETUP_PRODUCT=BROray;BRORAY_SETUP_VERSION=fixture\nbroray_setup_web_lan_ip(){ printf "192.168.3.1\\n"; };print_result';p=self.shell(script);self.assertEqual(p.returncode,0,p.stderr);self.assertIn(b'http://192.168.3.1:8080/',p.stdout)
 def test_empty_pin_means_auto(self):
  self.settings.write_text('{"listenAddress":"192.168.2.1","webuiLanAddress":""}');p=self.shell('. "$BRORAY_ROOT/lib/network.sh";broray_detect_webui_lan_ip');self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(p.stdout,b'192.168.2.1\n')
if __name__=='__main__':
 suite=unittest.TestSuite([unittest.defaultTestLoader.loadTestsFromTestCase(Web),unittest.defaultTestLoader.loadTestsFromTestCase(Contracts)])
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(suite)
 print('STAGE11_WEB_CONTRACT_REPORT='+json.dumps({'testsRun':r.testsRun,'failures':len(r.failures),'errors':len(r.errors),'routerAccessed':False,'daemonHttpIdentity':'fixtures','fullUpdaterRun':False}));raise SystemExit(not r.wasSuccessful())
