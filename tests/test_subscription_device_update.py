"""Real native owner/supervisor/commit; synthetic HTTP and show-version reader."""
import json,os,ctypes,unittest
from pathlib import Path
from test_subscription_jobs import SubscriptionJobs
from test_subscription_metadata_update import Updates
from test_subscription_device_info import VERSION
class DeviceUpdate(unittest.TestCase):
 clean_fixture=SubscriptionJobs.clean_fixture;record=SubscriptionJobs.record;states=SubscriptionJobs.states;shell=SubscriptionJobs.shell;job_script=SubscriptionJobs.job_script;response=Updates.response;update=Updates.update
 def setUp(self):
  SubscriptionJobs.setUp(self);self.response();v=self.app/'tmp/version';v.write_text(json.dumps(VERSION));self.env['TEST_VERSION']=str(v)
  n=self.app/'bin/ndmc';n.write_text('#!/bin/ash\n[ "$#" = 2 ] && [ "$1" = -c ] && [ "$2" = "show version" ] || exit 90\ncat "$TEST_VERSION"\n');n.chmod(0o755);self.env['BRORAY_INTERFACE_NDMC']=str(n)
  (self.app/'share/release/manifest.json').write_text('{"version":"3.1.1"}')
 def calls(self):return json.loads(Path(self.env['TEST_CALLS']).read_text())
 def patch(self,body):
  p=self.app/'tmp/settings';p.write_text(json.dumps(body));return self.shell(self.job_script('broray_subscription_update_settings test "'+str(p)+'"'),timeout=90)
 def test_enabled_real_update_preserves_hwid(self):
  p=self.record(sendDeviceInfo=True,httpUserAgent='Client/2');before=json.loads(p.read_text());self.update();after=json.loads(p.read_text());a=self.calls()[0];self.assertIn('X-Device-Model: KN-2710',a);self.assertIn('X-Device-OS: KeeneticOS',a);self.assertIn('X-Ver-OS: 5.1.1',a);self.assertIn('X-App-Version: 3.1.1',a);self.assertIn('Client/2',a);self.assertEqual(after['clientHwid'],before['clientHwid']);self.assertEqual(after['lastUpdateStatus'],'success');self.assertTrue(after['sendDeviceInfo']);self.assertFalse(list((self.app/'tmp').glob('subscription-op-*')))
 def test_legacy_subscription_has_no_device_headers(self):
  p=self.record();old=json.loads(p.read_text());self.update();self.assertFalse(any('X-Device-' in s for s in self.calls()[0]));self.assertEqual(json.loads(p.read_text())['clientHwid'],old['clientHwid'])
 def test_toggle_and_partial_settings_preserve_choice(self):
  p=self.record(sendDeviceInfo=True);old=json.loads(p.read_text());self.patch({'name':'Renamed'});self.assertTrue(json.loads(p.read_text())['sendDeviceInfo']);self.patch({'sendDeviceInfo':False});self.update();new=json.loads(p.read_text());self.assertFalse(new['sendDeviceInfo']);self.assertEqual(old['clientHwid'],new['clientHwid']);self.assertFalse(any('X-Device-' in s for s in self.calls()[0]))
 def test_failed_discovery_still_commits_subscription(self):
  p=self.record(sendDeviceInfo=True);self.env['BRORAY_INTERFACE_NDMC']=str(self.app/'missing');(self.app/'bin/ndmc').unlink();self.update();self.assertEqual(json.loads(p.read_text())['lastUpdateStatus'],'success');self.assertFalse(any('X-Device-' in s for s in self.calls()[0]))
 def test_create_default_false_and_opt_in(self):
  for i,flag in enumerate([None,True]):
   body={'name':'Created'+str(i),'url':'https://93.184.216.34/sub','updateImmediately':False}
   if flag is not None:body['sendDeviceInfo']=flag
   p=self.app/'tmp/create';p.write_text(json.dumps(body));r=self.shell(self.job_script('broray_subscription_create "'+str(p)+'"'),timeout=90);data=json.loads(r.stdout);self.assertEqual(data['sendDeviceInfo'],flag is True)
if __name__=='__main__':
 assert os.name!='nt';assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
 tests=list(unittest.defaultTestLoader.loadTestsFromTestCase(DeviceUpdate));group=os.environ.get('STAGE09_UPDATE_GROUP','all');tests=tests[:3] if group=='a' else tests[3:] if group=='b' else tests
 selected=[t.id() for t in tests];r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(tests));print('STAGE09_UPDATE_REPORT='+json.dumps({'testsRun':r.testsRun,'failures':len(r.failures),'errors':len(r.errors),'skipped':len(r.skipped),'selected':selected,'nativeOwnerSupervisor':'real','httpAndNdmc':'fixture','routerAccessed':False}));raise SystemExit(not r.wasSuccessful())
