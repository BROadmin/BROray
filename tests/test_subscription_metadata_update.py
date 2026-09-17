"""Actual owner/supervisor/commit with synthetic HTTP. Disposable Linux only."""
import base64,ctypes,json,os,unittest
from pathlib import Path
from test_subscription_jobs import SubscriptionJobs, ROOT
from test_subscription_http_metadata import FAKE,URI
class Updates(unittest.TestCase):
 setUp=SubscriptionJobs.setUp;clean_fixture=SubscriptionJobs.clean_fixture;record=SubscriptionJobs.record;states=SubscriptionJobs.states;shell=SubscriptionJobs.shell;job_script=SubscriptionJobs.job_script
 def response(self,headers='',body=URI,status='200'):
  self.env.update({'TEST_RESPONSES':str(self.temp/'responses.json'),'TEST_CALLS':str(self.temp/'http-calls.json')})
  p=self.app/'bin/curl';p.write_text(FAKE);p.chmod(0o755)
  Path(self.env['TEST_RESPONSES']).write_text(json.dumps([{'status':status,'headers':'HTTP/1.1 '+status+' TEST\r\n'+headers+'\r\n','body':base64.b64encode(body.encode()).decode()}]))
 def update(self,expected=0):return self.shell(self.job_script('broray_subscription_update test manual'),expected=expected,timeout=120)
 def test_success_keeps_manual_settings_and_commits_metadata(self):
  path=self.record(httpUserAgent='Client/1');self.response('profile-title: Provider\r\nprofile-update-interval: 12\r\nsubscription-userinfo: upload=1; download=2; total=100\r\n');self.update();data=json.loads(path.read_text());self.assertEqual(data['providerMetadata']['title'],'Provider');self.assertEqual(data['name'],'Test');self.assertEqual(data['updateIntervalMinutes'],60);self.assertEqual(data['providerMetadata']['suggestedUpdateMinutes'],720);self.assertEqual(data['providerMetadata']['updatedAt'],data['lastUpdatedAt']);self.assertEqual(data['httpUserAgent'],'Client/1');self.assertEqual(data['clientHwid'],'broray-1234567890abcdef1234567890abcdef');self.assertFalse(list((self.app/'tmp').glob('subscription-op-*')))
 def test_failed_refresh_keeps_metadata_catalog_and_hwid(self):
  path=self.record();self.response('profile-title: Previous\r\n');self.update();old=json.loads(path.read_text());files={p.name:p.read_bytes() for p in (self.app/'servers').glob('*.json')};self.response('x-hwid-limit: true\r\nprofile-title: Bad\r\n',status='403');self.update(expected=1);new=json.loads(path.read_text());self.assertEqual(new['providerMetadata'],old['providerMetadata']);self.assertEqual(new['clientHwid'],old['clientHwid']);self.assertEqual(files,{p.name:p.read_bytes() for p in (self.app/'servers').glob('*.json')});self.assertEqual(new['lastUpdateResult']['errorCode'],'SUBSCRIPTION_DEVICE_LIMIT_REACHED')
 def test_invalid_nodes_never_commit_provider_metadata(self):
  path=self.record(providerMetadata={'schemaVersion':1,'title':'Old'});self.response('profile-title: New\r\n',body='not-a-subscription');self.update(expected=1);self.assertEqual(json.loads(path.read_text())['providerMetadata']['title'],'Old')
 def test_success_missing_metadata_clears_old_values(self):
  path=self.record(providerMetadata={'schemaVersion':1,'title':'Old','usage':{'total':123}});self.response();self.update();self.assertNotIn('title',json.loads(path.read_text())['providerMetadata']);self.assertNotIn('usage',json.loads(path.read_text())['providerMetadata'])
 def test_body_base64_metadata_survives_supervision(self):
  path=self.record();body=base64.b64encode(('#profile-title: Body\n'+URI).encode()).decode();self.response('profile-title: HTTP\r\n',body=body);self.update();self.assertEqual(json.loads(path.read_text())['providerMetadata']['title'],'Body')
 def test_read_projection_does_not_write_state(self):
  path=self.record(providerMetadata={'schemaVersion':1,'title':'Info'},httpUserAgent='Client/1');before=path.read_bytes();p=self.shell('. "$BRORAY_ROOT/lib/subscription-service.sh"; broray_subscription_get test');self.assertEqual(json.loads(p.stdout)['providerMetadata']['title'],'Info');self.assertNotIn('clientHwid',json.loads(p.stdout));self.assertEqual(path.read_bytes(),before)
 def test_partial_settings_keep_user_agent_and_hwid(self):
  path=self.record(httpUserAgent='Client/1');body=self.temp/'settings.json';body.write_text('{"name":"Renamed"}');self.shell(self.job_script('broray_subscription_update_settings test "'+str(body)+'"'),timeout=90);d=json.loads(path.read_text());self.assertEqual(d['httpUserAgent'],'Client/1');self.assertEqual(d['clientHwid'],'broray-1234567890abcdef1234567890abcdef')
 def test_url_change_clears_old_provider_info(self):
  path=self.record(providerMetadata={'schemaVersion':1,'title':'Old'});body=self.temp/'settings.json';body.write_text('{"url":"https://93.184.216.34/new","httpUserAgent":""}');self.shell(self.job_script('broray_subscription_update_settings test "'+str(body)+'"'),timeout=90);self.assertNotIn('providerMetadata',json.loads(path.read_text()))
if __name__=='__main__':
 assert os.name!='nt';assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
 suite=unittest.defaultTestLoader.loadTestsFromTestCase(Updates)
 group=os.environ.get('STAGE07_UPDATE_GROUP','all');tests=list(suite)
 if group=='a':tests=tests[:4]
 elif group=='b':tests=tests[4:]
 selected=[t.id() for t in tests];suite=unittest.TestSuite(tests);result=unittest.TextTestRunner(verbosity=2,failfast=True).run(suite)
 print('STAGE07_UPDATE_REPORT='+json.dumps({'testsRun':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'skipped':len(result.skipped),'selected':selected,'realProtectedUpdate':True,'http':'fixture','routerAccessed':False}));raise SystemExit(0 if result.wasSuccessful() else 1)
