"""Real shell/JQ with synthetic curl, DNS and inputs. No real router/provider."""
import base64,json,os,shutil,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
URI='vless://11111111-2222-4333-8444-555555555555@93.184.216.34:443?security=tls&type=tcp#Fixture\n'
HWID='broray-1234567890abcdef1234567890abcdef'
FAKE='''#!/usr/bin/python3
import os,sys,json,base64
from pathlib import Path
args=sys.argv[1:]
if '--version' in args: print('curl '+os.environ.get('TEST_CURL_VERSION','8.20.0'));sys.exit(0)
if '--help' in args: print('--max-filesize');sys.exit(0)
p=Path(os.environ['TEST_CALLS']); calls=json.loads(p.read_text()) if p.exists() else []
data=json.loads(Path(os.environ['TEST_RESPONSES']).read_text()); response=data[min(len(calls),len(data)-1)]
calls.append(args);p.write_text(json.dumps(calls))
Path(args[args.index('--dump-header')+1]).write_bytes(response['headers'].encode())
Path(args[args.index('--output')+1]).write_bytes(base64.b64decode(response['body']))
print(response['status'],end=''); print('PRIVATE_CANARY',file=sys.stderr)
sys.exit(response.get('rc',0))
'''
class Transport(unittest.TestCase):
 def setUp(self):
  self.t=tempfile.TemporaryDirectory(prefix='broray-stage07-');self.addCleanup(self.t.cleanup);self.app=Path(self.t.name)/'app';shutil.copytree(ROOT/'runtime/app/lib',self.app/'lib')
  for d in ['tmp','bin','share/release','config']: (self.app/d).mkdir(parents=True,exist_ok=True)
  self.env={**os.environ,'BRORAY_BASE':str(self.app),'BRORAY_ROOT':str(self.app),'BRORAY_SUB_BASE':str(self.app),'PATH':str(self.app/'bin')+':/usr/bin:/bin:/usr/sbin:/sbin','BRORAY_SUB_PROVIDER_METADATA_FILE':str(self.app/'tmp/meta.json'),'TEST_CALLS':str(self.app/'tmp/calls.json'),'TEST_RESPONSES':str(self.app/'tmp/responses.json')}
  self.meta=Path(self.env['BRORAY_SUB_PROVIDER_METADATA_FILE']); self.responses=[];self.response()
  p=self.app/'bin/curl';p.write_text(FAKE);p.chmod(0o755)
  (self.app/'share/release/manifest.json').write_text('{"version":"3.1.1"}')
 def response(self,status='200',headers='',body=URI,rc=0):
  self.responses.append({'status':status,'headers':'HTTP/1.1 '+status+' TEST\r\n'+headers+'\r\n','body':base64.b64encode(body.encode() if isinstance(body,str) else body).decode(),'rc':rc});Path(self.env['TEST_RESPONSES']).write_text(json.dumps(self.responses))
 def shell(self,script,*args):
  return subprocess.run(['/bin/ash','-c','. "$BRORAY_ROOT/lib/subscription-service.sh"\n'+script,'stage07',*args],env=self.env,capture_output=True,timeout=20)
 def fetch(self,ua='',url='https://provider.example.invalid/sub/PRIVATE_CANARY'):
  p=self.shell('''broray_subscription_resolve_public_ip() { BRORAY_SUB_RESOLVED_IP=93.184.216.34; }
rc=0; broray_subscription_fetch "$1" "$BRORAY_ROOT/tmp/download" "$2" "$3" || rc=$?
jq -nc --argjson rc "$rc" --arg code "$BRORAY_SUB_ERROR_CODE" --arg message "$BRORAY_SUB_ERROR_MESSAGE" --arg ct "${BRORAY_SUB_FETCH_CONTENT_TYPE:-}" '{rc:$rc,code:$code,message:$message,contentType:$ct}' ''',url,HWID,ua)
  self.assertEqual(p.returncode,0,p.stderr.decode(errors='replace'));out=json.loads(p.stdout);self.assertNotIn('PRIVATE_CANARY',out['message']);return out
 def calls(self):
  p=Path(self.env['TEST_CALLS']);return json.loads(p.read_text()) if p.exists() else []
 def collect(self,text,mode='body'):
  p=self.app/'tmp/input';p.write_bytes(text.encode() if isinstance(text,str) else text)
  result=self.shell('broray_subscription_metadata_collect "$1" "$2"',mode,str(p));self.assertEqual(result.returncode,0,result.stderr.decode(errors='replace'));return json.loads(self.meta.read_text())
 def extract(self,body):
  p=self.app/'tmp/input';p.write_bytes(body.encode() if isinstance(body,str) else body)
  r=self.shell('rc=0; broray_subscription_extract_nodes "$1" "$BRORAY_ROOT/tmp/nodes" || rc=$?; jq -nc --argjson rc "$rc" --arg code "$BRORAY_SUB_ERROR_CODE" \'{rc:$rc,code:$code}\'',str(p));self.assertEqual(r.returncode,0,r.stderr.decode(errors='replace'));return json.loads(r.stdout)
 def test_regression_device_limit_403(self):
  self.responses=[];self.response('403','x-hwid-max-devices-reached: true\r\n');self.assertEqual(self.fetch()['code'],'SUBSCRIPTION_DEVICE_LIMIT_REACHED')
 def test_regression_legacy_hwid_limit(self):
  self.responses=[];self.response('200','x-hwid-limit: true\r\n');self.assertEqual(self.fetch()['code'],'SUBSCRIPTION_DEVICE_LIMIT_REACHED')
 def test_regression_case_insensitive_content_type(self):
  self.responses=[];self.response(headers='content-type: text/plain\r\n');self.assertEqual(self.fetch()['contentType'],'text/plain')
 def test_regression_user_agent_version(self):
  self.assertEqual(self.fetch()['rc'],0);args=self.calls()[0];self.assertEqual(args[args.index('--user-agent')+1],'BROray/3.1.1')
 def test_regression_body_metadata(self):
  self.assertEqual(self.extract('#profile-title: Provider\n'+URI)['rc'],0);self.assertTrue(self.meta.exists(),'metadata was not captured');self.assertEqual(json.loads(self.meta.read_text())['title'],'Provider')
 def test_regression_outer_headers_base64(self):
  body='#profile-title: Outer\n'+base64.b64encode(URI.encode()).decode();self.assertEqual(self.extract(body)['rc'],0);self.assertEqual(json.loads(self.meta.read_text())['title'],'Outer')
 def test_custom_user_agent(self):
  self.assertEqual(self.fetch('ProviderClient/1')['rc'],0);args=self.calls()[0];self.assertEqual(args[args.index('--user-agent')+1],'ProviderClient/1')
 def test_curl_flags_and_hwid(self):
  self.fetch();a=self.calls()[0];self.assertEqual(a[:2],['--disable','--globoff']);self.assertIn('x-hwid: '+HWID,a);self.assertIn('--resolve',a);self.assertIn('--max-filesize',a);self.assertNotIn('--location',a);self.assertIn('--noproxy',a)
 def test_redirects_revalidate_and_preserve_hwid(self):
  self.responses=[];self.response('302','location: https://second.example.invalid/sub\r\nprofile-title: Wrong\r\n');self.response(headers='profile-title: Right\r\n');self.assertEqual(self.fetch()['rc'],0);self.assertEqual(len(self.calls()),2);self.assertTrue(all('x-hwid: '+HWID in a for a in self.calls()));self.assertEqual(json.loads(self.meta.read_text())['title'],'Right')
 def test_https_downgrade_rejected(self):
  self.responses=[];self.response('302','Location: http://provider.example.invalid/sub\r\n');self.assertNotEqual(self.fetch()['rc'],0);self.assertEqual(len(self.calls()),1)
 def test_relative_query_redirect(self):
  self.responses=[];self.response('302','Location: ?next=1\r\n');self.response();self.assertEqual(self.fetch()['rc'],0);self.assertEqual(self.calls()[1][-1],'https://provider.example.invalid/sub/PRIVATE_CANARY?next=1')
 def test_location_conflict(self):
  self.responses=[];self.response('302','Location: /one\r\nlocation: /two\r\n');self.assertNotEqual(self.fetch()['rc'],0);self.assertEqual(len(self.calls()),1)
 def test_redirect_limit(self):
  self.responses=[];self.response('302','Location: /again\r\n');self.assertNotEqual(self.fetch()['rc'],0);self.assertEqual(len(self.calls()),4)
 def test_interim_and_trailers_not_metadata(self):
  self.responses=[];self.response(headers='profile-title: Final\r\n');self.responses[0]['headers']='HTTP/1.1 103 Early\r\nx-hwid-limit: true\r\nprofile-title: Interim\r\n\r\n'+self.responses[0]['headers']+'profile-title: Trailer\r\n';Path(self.env['TEST_RESPONSES']).write_text(json.dumps(self.responses));self.assertEqual(self.fetch()['rc'],0);self.assertEqual(json.loads(self.meta.read_text())['title'],'Final')
 def test_new_curl_compression(self):
  self.fetch();self.assertIn('--compressed',self.calls()[0])
 def test_old_curl_identity(self):
  self.env['TEST_CURL_VERSION']='8.19.0';self.fetch();self.assertNotIn('--compressed',self.calls()[0]);self.assertIn('Accept-Encoding: identity',self.calls()[0])
 def test_old_curl_compressed_response_refused(self):
  self.env['TEST_CURL_VERSION']='8.19.0';self.responses=[];self.response(headers='Content-Encoding: gzip\r\n');self.assertEqual(self.fetch()['code'],'UNSUPPORTED_CONTENT')
 def test_headers_limit(self):
  self.responses=[];self.response(headers='X-Long: '+'x'*66000+'\r\n');self.assertEqual(self.fetch()['code'],'CONTENT_TOO_LARGE')
 def test_body_limit(self):
  self.env['BRORAY_SUB_MAX_BYTES']='128';self.responses=[];self.response(body='x'*129);self.assertEqual(self.fetch()['code'],'CONTENT_TOO_LARGE')
 def test_invalid_ua_not_sent(self):
  self.assertEqual(self.fetch('x\r\nCookie: bad')['code'],'INVALID_USER_AGENT');self.assertEqual(self.calls(),[])
 def test_body_overrides_header(self):
  self.collect('HTTP/1.1 200 OK\r\nprofile-title: HTTP\r\nprofile-update-interval: 12\r\n\r\n','http');m=self.collect('#profile-title: Body\n');self.assertEqual(m['title'],'Body');self.assertEqual(m['suggestedUpdateMinutes'],720)
 def test_success_without_metadata_clears_previous_info(self):
  self.collect('#profile-title: Old\n');self.fetch();self.assertNotIn('title',json.loads(self.meta.read_text()))
 def test_all_metadata_fields(self):
  m=self.collect('#profile-title: base64:'+base64.b64encode('Тест + 東京'.encode()).decode()+'\n#subscription-userinfo: upload=2; download=3; total=10; expire=1800000000\n#announce: Hello <b>\n#support-url: https://support.example.invalid\n#profile-web-page-url: https://page.example.invalid\n#announce-url: https://news.example.invalid\n#profile-update-interval: 1\n');self.assertEqual(m['title'],'Тест + 東京');self.assertEqual(m['usage'],{'upload':2,'download':3,'total':10,'expire':1800000000});self.assertEqual(m['suggestedUpdateMinutes'],60);self.assertEqual(m['announcement'],'Hello <b>')
 def test_decoded_body_overrides_outer_metadata(self):
  inner='#profile-title: Inner\n'+URI;self.assertEqual(self.extract('#profile-title: Outer\n'+base64.urlsafe_b64encode(inner.encode()).decode().rstrip('='))['rc'],0);self.assertEqual(json.loads(self.meta.read_text())['title'],'Inner')
 def test_duplicate_conflict_does_not_pick_last(self):
  m=self.collect('#profile-title: A\n#profile-title: B\n');self.assertNotIn('title',m);self.assertEqual(m['invalidFields'],['profile-title'])
 def test_identical_duplicates(self):
  self.assertEqual(self.collect('#profile-title: A\n#PROFILE-TITLE: A\n')['title'],'A')
 def test_provider_directives_never_applied(self):
  m=self.collect('#routing: PRIVATE_CANARY\n#update-always: true\n#custom-tunnel-config: PRIVATE_CANARY\n');self.assertEqual(m['ignoredDirectives'],['custom-tunnel-config','routing','update-always']);self.assertNotIn('PRIVATE_CANARY',json.dumps(m))
 def test_unknown_headers_never_stored(self):
  m=self.collect('HTTP/1.1 200 OK\r\nSet-Cookie: PRIVATE_CANARY\r\nAuthorization: PRIVATE_CANARY\r\n\r\n','http');self.assertNotIn('PRIVATE_CANARY',json.dumps(m))
 def test_plain_uri_and_base64_unchanged(self):
  for body in [URI,base64.b64encode(URI.encode())]: self.assertEqual(self.extract(body)['rc'],0);self.assertEqual((self.app/'tmp/nodes').read_text(),URI)
 def test_json_with_outer_metadata(self):
  p={'outbounds':[{'protocol':'vless','settings':{'vnext':[{'address':'93.184.216.34','port':443,'users':[{'id':'11111111-2222-4333-8444-555555555555'}]}]}}]};self.assertEqual(self.extract('#profile-title: JSON\n'+json.dumps(p))['rc'],0)
 def test_conflicting_hwid_headers_reject(self):
  self.responses=[];self.response(headers='x-hwid-limit: true\r\nx-hwid-limit: false\r\n');self.assertEqual(self.fetch()['code'],'HTTP_ERROR');self.assertFalse((self.app/'tmp/download').exists())
 def test_base64_interval(self):
  self.assertEqual(self.collect('#profile-update-interval: base64:MTI=\n')['suggestedUpdateMinutes'],720)
 def test_invalid_body_user_agent_has_specific_error(self):
  p=self.app/'tmp/request';p.write_text(json.dumps({'httpUserAgent':'x\r\nBad: y'}));r=self.shell('rc=0; broray_subscription_validate_body "$1" || rc=$?; printf "%s" "$BRORAY_SUB_ERROR_CODE"',str(p));self.assertEqual(r.stdout,b'INVALID_USER_AGENT')
 def test_http_status_without_hwid_flag_is_not_guessed(self):
  self.responses=[];self.response(status='404');self.assertEqual(self.fetch()['code'],'HTTP_ERROR')
 def test_failed_transfer_never_commits_metadata(self):
  self.responses=[];self.response(headers='profile-title: New\r\n',rc=28);self.assertEqual(self.fetch()['code'],'DOWNLOAD_TIMEOUT');self.assertNotIn('title',json.loads(self.meta.read_text()))
 def test_metadata_file_mode(self):
  self.collect('#profile-title: P\n');self.assertEqual(self.meta.stat().st_mode & 0o777,0o600)
 def test_raw_invalid_utf8_metadata_is_not_used(self):
  self.assertIn('profile-title',self.collect(b'#profile-title: \xff\n')['invalidFields'])
def denied(status,header,code):
 def test(self): self.responses=[];self.response(status,header+': true\r\n');self.assertEqual(self.fetch()['code'],code);self.assertFalse((self.app/'tmp/download').exists())
 return test
for status in ['200','401','403','404','429','500']:
 for header,code in [('x-hwid-limit','SUBSCRIPTION_DEVICE_LIMIT_REACHED'),('X-HWID-MAX-DEVICES-REACHED','SUBSCRIPTION_DEVICE_LIMIT_REACHED'),('x-hwid-not-supported','SUBSCRIPTION_DEVICE_ID_REJECTED')]: setattr(Transport,'test_denial_'+status+'_'+header,denied(status,header,code))
def invalid_meta(key,value):
 def test(self): m=self.collect('#'+key+': '+value+'\n');self.assertIn(key,m['invalidFields']);self.assertNotIn('PRIVATE_CANARY',json.dumps(m))
 return test
for i,(key,value) in enumerate([('profile-title','base64:/w=='),('profile-title','base64:AA=='),('profile-title','base64:not-valid!!!'),('profile-title','x'*129),('profile-update-interval','0'),('profile-update-interval','169'),('profile-update-interval','-1'),('profile-update-interval','1.5'),('subscription-userinfo','upload=1; upload=2'),('subscription-userinfo','total=9007199254740992'),('support-url','javascript:PRIVATE_CANARY'),('announce-url','https://user:PRIVATE_CANARY@example.com'),('announce','base64:SGkK')]): setattr(Transport,'test_invalid_meta_%02d'%i,invalid_meta(key,value))
if __name__=='__main__':
 suite=unittest.defaultTestLoader.loadTestsFromTestCase(Transport)
 if os.environ.get('STAGE07_REPRO'): suite=unittest.TestSuite(t for t in suite if 'test_regression_' in t.id())
 result=unittest.TextTestRunner(verbosity=2,failfast=not bool(os.environ.get('STAGE07_REPRO'))).run(suite)
 print('STAGE07_HTTP_REPORT='+json.dumps({'testsRun':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'skipped':len(result.skipped),'mocked':['curl','DNS resolver'],'routerAccessed':False}));raise SystemExit(0 if result.wasSuccessful() else 1)
