"""Actual ash, jq, timeout and file limits; synthetic ndmc, curl and DNS."""
import json,os,subprocess,unittest,time
from pathlib import Path
from test_subscription_http_metadata import Transport, HWID
VERSION={'vendor':'Keenetic','manufacturer':'Keenetic Ltd.','model':'Peak (KN-2710)','hw_id':'KN-2710','title':'5.1.1','release':'5.01.C.1.0-0','serial':'PRIVATE_CANARY','mac':'PRIVATE_CANARY','serviceTag':'PRIVATE_CANARY','ndw':{'version':'WRONG'}}
class Device(unittest.TestCase):
 response=Transport.response;shell=Transport.shell;calls=Transport.calls
 def setUp(self):
  Transport.setUp(self);self.version_file=self.app/'tmp/version';self.version_file.write_text(json.dumps(VERSION));self.env['TEST_VERSION']=str(self.version_file);self.env['TEST_DEVICE_CALLS']=str(self.app/'tmp/device-calls')
  p=self.app/'bin/ndmc';p.write_text('''#!/bin/ash
[ "$#" = 2 ] && [ "$1" = -c ] && [ "$2" = 'show version' ] || exit 90
printf 'show version\\n' >> "$TEST_DEVICE_CALLS"
case "${TEST_DEVICE_MODE:-normal}" in
 fail) printf PRIVATE_CANARY >&2;exit 1 ;;
 wait) trap '' TERM;sleep 10;exit 1 ;;
 huge) yes PRIVATE_CANARY | head -c 65536;exit 0 ;;
esac
cat "$TEST_VERSION"
''');p.chmod(0o755);self.env['BRORAY_INTERFACE_NDMC']=str(p)
 def fetch(self,enabled='true',ua='',url='https://provider.example.invalid/sub'):
  p=self.shell('''broray_subscription_resolve_public_ip() { BRORAY_SUB_RESOLVED_IP=93.184.216.34; }
rc=0; broray_subscription_fetch "$1" "$BRORAY_ROOT/tmp/download" "$2" "$3" "$4" || rc=$?
jq -nc --argjson rc "$rc" --arg code "$BRORAY_SUB_ERROR_CODE" '{rc:$rc,code:$code}' ''',url,HWID,ua,enabled)
  self.assertEqual(p.returncode,0,p.stderr);self.assertNotIn(b'PRIVATE_CANARY',p.stdout+p.stderr);return json.loads(p.stdout)
 def headers(self,index=0):
  a=self.calls()[index];return {a[i+1].split(':',1)[0].lower():a[i+1].split(':',1)[1].strip() for i in range(len(a)-1) if a[i]=='--header'}
 def snapshot(self):
  p=self.shell('broray_subscription_device_info');self.assertEqual(p.returncode,0,p.stderr);self.assertNotIn(b'PRIVATE_CANARY',p.stdout+p.stderr);self.assertEqual(list((self.app/'tmp').glob('device-info.*')),[]);return json.loads(p.stdout)
 def test_regression_enabled_sends_model(self):
  self.assertEqual(self.fetch()['rc'],0);self.assertEqual(self.headers().get('x-device-model'),'KN-2710');self.assertEqual(self.headers()['x-device-os'],'KeeneticOS');self.assertEqual(self.headers()['x-ver-os'],'5.1.1');self.assertEqual(self.headers()['x-app-version'],'3.1.1')
 def test_regression_nonboolean_rejected(self):
  p=self.app/'tmp/body';p.write_text('{"sendDeviceInfo":"true"}');r=self.shell('broray_subscription_validate_body "$1"',str(p));self.assertNotEqual(r.returncode,0)
 def test_off_no_read_no_headers(self):
  self.env['TEST_DEVICE_MODE']='wait';self.assertEqual(self.fetch('false')['rc'],0);self.assertNotIn('x-device-os',self.headers());self.assertNotIn('x-app-version',self.headers());self.assertFalse(Path(self.env['TEST_DEVICE_CALLS']).exists());self.assertEqual(self.headers()['x-hwid'],HWID)
 def test_default_legacy_fetch_is_off(self):
  p=Transport.fetch(self);self.assertEqual(p['rc'],0);self.assertNotIn('x-device-model',self.headers());self.assertFalse(Path(self.env['TEST_DEVICE_CALLS']).exists())
 def test_enabled_hwid_and_custom_ua_unchanged(self):
  self.fetch(ua='Android-compat/2');self.assertEqual(self.headers()['x-hwid'],HWID);self.assertIn('Android-compat/2',self.calls()[0]);self.assertEqual(self.headers()['x-device-os'],'KeeneticOS');self.assertEqual(self.headers()['x-app-version'],'3.1.1')
 def test_same_origin_redirect_keeps_info_one_read(self):
  self.responses=[];self.response('302','Location: /next\r\n');self.response();self.assertEqual(self.fetch()['rc'],0);self.assertEqual(self.headers(0),self.headers(1));self.assertEqual(Path(self.env['TEST_DEVICE_CALLS']).read_text(),'show version\n')
 def test_cross_origin_drops_info_not_hwid(self):
  self.responses=[];self.response('302','Location: https://other.example.invalid/sub\r\n');self.response();self.assertEqual(self.fetch()['rc'],0);self.assertIn('x-device-model',self.headers(0));self.assertNotIn('x-device-os',self.headers(1));self.assertNotIn('x-app-version',self.headers(1));self.assertEqual(self.headers(1)['x-hwid'],HWID)
 def test_cross_origin_return_does_not_restore(self):
  self.responses=[];self.response('302','Location: https://other.example.invalid/sub\r\n');self.response('302','Location: https://provider.example.invalid/back\r\n');self.response();self.fetch();self.assertNotIn('x-device-model',self.headers(2))
 def test_new_port_is_cross_origin(self):
  self.responses=[];self.response('302','Location: https://provider.example.invalid:444/sub\r\n');self.response();self.fetch();self.assertNotIn('x-device-model',self.headers(1))
 def test_firmware_refresh_does_not_change_hwid(self):
  self.fetch();v=dict(VERSION,title='5.2 Beta 1');self.version_file.write_text(json.dumps(v));self.fetch();self.assertEqual(self.headers(1)['x-ver-os'],'5.2 Beta 1');self.assertEqual(self.headers(0)['x-hwid'],self.headers(1)['x-hwid'])
 def test_missing_ndmc_does_not_block(self):
  self.env['BRORAY_INTERFACE_NDMC']=str(self.app/'absent');(self.app/'bin/ndmc').unlink();self.assertEqual(self.fetch()['rc'],0);self.assertNotIn('x-device-os',self.headers());self.assertEqual(self.headers()['x-app-version'],'3.1.1')
 def test_failed_ndmc_does_not_block(self):
  self.env['TEST_DEVICE_MODE']='fail';self.assertEqual(self.fetch()['rc'],0);self.assertNotIn('x-device-os',self.headers())
 def test_timeout_is_bounded(self):
  self.env['TEST_DEVICE_MODE']='wait';start=time.monotonic();self.assertEqual(self.fetch()['rc'],0);self.assertLess(time.monotonic()-start,12);self.assertNotIn('x-device-os',self.headers())
 def test_excessive_output_is_not_kept(self):
  self.env['TEST_DEVICE_MODE']='huge';self.assertEqual(self.snapshot(),{'appVersion':'3.1.1'})
 def test_text_show_version(self):
  self.version_file.write_text(' vendor: Keenetic\n model: Peak (KN-2710)\n hw_id: KN-2710\n title: 5.1.1\n release: 5.01.C.1\n serial: PRIVATE_CANARY\n');self.assertEqual(self.snapshot(),{'os':'KeeneticOS','model':'KN-2710','osVersion':'5.1.1','appVersion':'3.1.1'})
 def test_duplicate_text_field_not_chosen(self):
  self.version_file.write_text('vendor: Keenetic\nhw_id: KN-2710\nhw_id: KN-1811\n');self.assertNotIn('model',self.snapshot())
 def test_browser_device_not_used(self):
  self.env.update({'HTTP_USER_AGENT':'Android Redmi','HTTP_X_DEVICE_MODEL':'Redmi 23090RA98G','HTTP_X_DEVICE_OS':'Android'});self.fetch();self.assertEqual(self.headers()['x-device-model'],'KN-2710')
 def test_unknown_app_version_omitted(self):
  (self.app/'share/release/manifest.json').write_text('{"version":"bad\\nPRIVATE_CANARY"}');self.assertNotIn('appVersion',self.snapshot())
 def test_invalid_flag_does_not_start_io(self):
  self.assertEqual(self.fetch('true\nX-Bad: secret')['code'],'INVALID_DEVICE_INFO_SETTING');self.assertFalse(self.calls());self.assertFalse(Path(self.env['TEST_DEVICE_CALLS']).exists())
 def test_wrong_device_does_not_claim_keenetic(self):
  self.version_file.write_text('{"vendor":"Android","model":"Redmi 23090RA98G","title":"14"}');self.assertEqual(self.snapshot(),{'appVersion':'3.1.1'})
 def test_multiple_json_objects_omitted(self):
  self.version_file.write_text(json.dumps(VERSION)+'\n'+json.dumps(VERSION));self.assertEqual(self.snapshot(),{'appVersion':'3.1.1'})
 def test_mode_private_temp(self):
  self.snapshot();self.assertFalse(list((self.app/'tmp').glob('device-info.*')))
def normalizer_case(update,missing,expected):
 def test(self):
  v=dict(VERSION);v.update(update)
  for k in missing:v.pop(k,None)
  self.version_file.write_text(json.dumps(v));got=self.snapshot();self.assertEqual(got,dict(expected,appVersion='3.1.1'))
 return test
for name,update,missing,expected in [
 ('hw_only',{},['model'],{'os':'KeeneticOS','model':'KN-2710','osVersion':'5.1.1'}),
 ('model_only',{},['hw_id'],{'os':'KeeneticOS','model':'KN-2710','osVersion':'5.1.1'}),
 ('conflict',{'hw_id':'KN-1811'},[],{'os':'KeeneticOS','osVersion':'5.1.1'}),
 ('unknown_model',{'hw_id':'ki_rb','model':'Old model'},[],{'os':'KeeneticOS','osVersion':'5.1.1'}),
 ('release_fallback',{},['title'],{'os':'KeeneticOS','model':'KN-2710','osVersion':'5.01.C.1.0-0'}),
 ('bad_version',{'title':'5.1\r\nX-Evil: private','release':None},[],{'os':'KeeneticOS','model':'KN-2710'}),
 ('no_version',{},['title','release'],{'os':'KeeneticOS','model':'KN-2710'}),
 ('no_brand',{},['vendor','manufacturer'],{'os':'KeeneticOS','model':'KN-2710','osVersion':'5.1.1'}),
 ('bad_model',{'hw_id':'KN-2710\nPRIVATE_CANARY','model':'KN-2710\nPRIVATE_CANARY'},[],{'os':'KeeneticOS','osVersion':'5.1.1'}),
 ('ndw_not_firmware',{},['title','release'],{'os':'KeeneticOS','model':'KN-2710'})]:setattr(Device,'test_fields_'+name,normalizer_case(update,missing,expected))
if __name__=='__main__':
 suite=unittest.defaultTestLoader.loadTestsFromTestCase(Device)
 if os.environ.get('STAGE09_REPRO')=='1':suite=unittest.TestSuite(t for t in suite if 'test_regression_' in t.id())
 result=unittest.TextTestRunner(verbosity=2,failfast=os.environ.get('STAGE09_REPRO')!='1').run(suite)
 print('STAGE09_DEVICE_REPORT='+json.dumps({'testsRun':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'skipped':len(result.skipped),'mocked':['ndmc','curl','DNS'],'routerAccessed':False}));raise SystemExit(not result.wasSuccessful())
