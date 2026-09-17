"""Protected updates on the cumulative source; only the HTTP transport is synthetic."""
import base64,ctypes,json,os,shutil,unittest
from pathlib import Path
from test_subscription_jobs import SubscriptionJobs
from test_subscription_metadata_update import Updates as MetadataUpdates
from test_subscription_vless_compat import profile,outbound,stream
UUID='11111111-2222-4333-8444-555555555555'
def uri(name,uuid=UUID):return 'vless://'+uuid+'@'+name+'.example.invalid:443?security=tls&type=tcp&sni=example.invalid#'+name
BAD='tuic://PRIVATE_CANARY_UNSUPPORTED\n'
class Partial(unittest.TestCase):
 setUp=SubscriptionJobs.setUp;clean_fixture=SubscriptionJobs.clean_fixture;record=SubscriptionJobs.record;states=SubscriptionJobs.states;shell=SubscriptionJobs.shell;job_script=SubscriptionJobs.job_script;response=MetadataUpdates.response;update=MetadataUpdates.update
 def seed(self,names,enabled=True):
  source=self.app/'seed-input';source.write_text('\n'.join(uri(n) for n in names)+'\n')
  self.shell('''. "$BRORAY_ROOT/lib/subscription-service.sh"
broray_subscription_extract_nodes "$BRORAY_ROOT/seed-input" "$BRORAY_ROOT/seed-nodes" || exit 1
broray_subscription_stage_nodes test "$BRORAY_ROOT/seed-nodes" "$BRORAY_ROOT/seed-stage" true
''',timeout=90)
  target=self.app/('servers' if enabled else 'config/disabled-subscription-servers/test');target.mkdir(parents=True,exist_ok=True)
  result={}
  for f in (self.app/'seed-stage').glob('*.json'):
   d=json.loads(f.read_bytes());p=target/f.name;shutil.copyfile(f,p);p.chmod(0o600);result[d['name']]=p
  self.assertEqual(set(result),set(names));return result
 def snapshot(self):return {p.relative_to(self.app).as_posix():p.read_bytes() for sub in ['servers','config/disabled-subscription-servers','run/server-quality'] for p in (self.app/sub).rglob('*.json')}
 def assert_clean(self):
  self.assertFalse(list((self.app/'tmp').glob('subscription-op-*')));self.assertFalse(list((self.app/'tmp').glob('server-subscription-sync.*')));self.assertFalse((self.temp/'global.lock').is_symlink())
 def test_partial_retains_active_updates_valid_and_preserves_foreign(self):
  path=self.record();old=self.seed(['old','same']);saved=old['old'].read_bytes();active=old['old'].stem;(self.app/'config/active-server').write_text(active+'\n');config=self.app/'config/config.json';config.write_bytes(b'{"qa":"unchanged-runtime"}');quality=self.app/'run/server-quality';quality.mkdir(parents=True,exist_ok=True);(quality/(active+'.json')).write_bytes(b'{"score":42}')
  for name,source in [('manual',{'type':'manual'}),('other',{'type':'subscription','subscriptionId':'other'})]:
   n=json.loads(saved);n.update(id=name,source=source);(self.app/'servers'/(name+'.json')).write_text(json.dumps(n))
  foreign={p:p.read_bytes() for p in [self.app/'servers/manual.json',self.app/'servers/other.json',quality/(active+'.json')]}
  self.response('profile-title: New provider info\r\n',body=uri('same','aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee')+'\n'+uri('new')+'\n'+BAD);self.update();d=json.loads(path.read_bytes());r=d['lastUpdateResult']
  self.assertEqual((d['lastUpdateStatus'],r['accepted'],r['retained'],r['removed'],r['catalogTotal'],d['serversReceived']),('partial',2,1,0,3,3));self.assertEqual(old['old'].read_bytes(),saved);self.assertEqual(json.loads(old['same'].read_bytes())['uuid'],'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee');self.assertEqual((self.app/'config/active-server').read_text(),active+'\n');self.assertEqual(config.read_bytes(),b'{"qa":"unchanged-runtime"}');self.assertTrue(all(p.read_bytes()==b for p,b in foreign.items()));self.assertEqual(d['providerMetadata']['title'],'New provider info');self.assertNotIn('PRIVATE_CANARY',json.dumps(r));self.assertEqual(self.states()[0]['state'],'completed');self.assert_clean()
 def test_partial_disabled_does_not_enable(self):
  path=self.record(enabled=False);old=self.seed(['old'],enabled=False)['old'];saved=old.read_bytes();self.response(body=uri('new')+'\n'+BAD);self.update();d=json.loads(path.read_bytes());self.assertEqual(d['lastUpdateResult']['retained'],1);self.assertEqual(old.read_bytes(),saved);self.assertFalse(list((self.app/'servers').glob('*.json')));self.assertEqual(len(list(old.parent.glob('*.json'))),2);self.assert_clean()
 def test_total_parse_failure_preserves_metadata_and_catalog(self):
  path=self.record(providerMetadata={'schemaVersion':1,'title':'Old info'});self.seed(['old']);before=self.snapshot();self.response('profile-title: Wrong replacement\r\n',body=BAD);self.update(expected=1);self.assertEqual(before,self.snapshot());d=json.loads(path.read_bytes());self.assertEqual(d['providerMetadata']['title'],'Old info');self.assertEqual(d['lastUpdateStatus'],'error');self.assert_clean()
 def test_complete_response_still_removes_missing_inactive(self):
  path=self.record();old=self.seed(['old'])['old'];self.response(body=uri('new')+'\n');self.update();r=json.loads(path.read_bytes())['lastUpdateResult'];self.assertFalse(old.exists());self.assertEqual((r['retained'],r['removed'],r['deletionPolicy']),(0,1,'replace'));self.assert_clean()
 def test_complete_response_cannot_remove_active(self):
  path=self.record();old=self.seed(['old'])['old'];(self.app/'config/active-server').write_text(old.stem+'\n');before=self.snapshot();self.response(body=uri('new')+'\n');self.update(expected=1);self.assertEqual(before,self.snapshot());self.assertEqual(json.loads(path.read_bytes())['lastUpdateResult']['errorCode'],'ACTIVE_SERVER_CONFLICT');self.assert_clean()
 def test_duplicates_conservatively_keep_unmatched(self):
  path=self.record();old=self.seed(['old'])['old'];saved=old.read_bytes();self.response(body=uri('new')+'\n'+uri('new')+'\n');self.update();r=json.loads(path.read_bytes())['lastUpdateResult'];self.assertEqual((r['accepted'],r['rejected'],r['retained'],r['removed']),(1,1,1,0));self.assertEqual(old.read_bytes(),saved);self.assert_clean()
 def test_union_limit_preserves_previous_snapshot(self):
  path=self.record();self.seed(['old','two']);before=self.snapshot();self.env['BRORAY_SUB_MAX_NODES']='2';self.response(body=uri('new')+'\n'+BAD);self.update(expected=1);self.assertEqual(before,self.snapshot());self.assertEqual(json.loads(path.read_bytes())['lastUpdateResult']['errorCode'],'PARTIAL_UPDATE_LIMIT');self.assert_clean()
 def test_partial_json_keeps_old_unknown_endpoint(self):
  path=self.record();old=self.seed(['old'])['old'];before=old.read_bytes();good=profile('ws');bad=profile();outbound(bad)['streamSettings']['sockopt']={'dialerProxy':'PRIVATE_CANARY'};self.response(body=json.dumps([good,bad]));self.update();r=json.loads(path.read_bytes())['lastUpdateResult'];self.assertEqual((r['accepted'],r['rejected'],r['retained'],r['removed']),(1,1,1,0));self.assertEqual(old.read_bytes(),before);self.assertNotIn('PRIVATE_CANARY',json.dumps(r));self.assert_clean()
if __name__=='__main__':
 assert os.name!='nt';assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
 tests=list(unittest.defaultTestLoader.loadTestsFromTestCase(Partial));g=os.environ.get('STAGE08_UPDATE_GROUP','all')
 if g in 'abcd' and len(g)==1:tests=tests['abcd'.index(g)*2:('abcd'.index(g)+1)*2]
 assert tests;selected=[t.id() for t in tests];result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(tests))
 print('STAGE08_UPDATE_REPORT='+json.dumps({'testsRun':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'skipped':len(result.skipped),'selected':selected,'realProtectedUpdate':True,'http':'fixture','routerAccessed':False}));raise SystemExit(0 if result.wasSuccessful() else 1)
